#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Linux ]]; then
  echo 'hermetic_provider_a0 verdict=NOT_RUN reason=linux_required exit=3'
  exit 3
fi

BWRAP="${HERMETIC_A0_BWRAP:-}"
if [[ -z "$BWRAP" || ! -x "$BWRAP" ]]; then
  echo 'hermetic_provider_a0 verdict=FAIL reason=provider_binary_missing exit=1' >&2
  exit 1
fi
if [[ "$(id -u)" == 0 ]]; then
  echo 'hermetic_provider_a0 verdict=FAIL reason=unprivileged_runner_required exit=1' >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
OUT="$repo_root/.build/hermetic-a0"
FIX="$OUT/fixture"
HOST="$FIX/hostrepo"
CAPTURE="$FIX/capture"
SCRATCH="$FIX/scratch"
EVIL="$FIX/evilbin"
TOOL="$FIX/toolchain"
A0_UID="$(id -u)"
A0_GID="$(id -g)"
mkdir -p "$OUT"

expected_version='bubblewrap 0.11.2'
actual_version="$($BWRAP --version)"
[[ "$actual_version" == "$expected_version" ]] || { echo "unexpected provider version: $actual_version" >&2; exit 1; }
[[ ! -u "$BWRAP" && ! -g "$BWRAP" ]] || { echo 'setuid/setgid provider binary rejected' >&2; exit 1; }
printf 'provider_version=%s\n' "$actual_version" > "$OUT/provider-identity.txt"
printf 'provider_sha256=%s\n' "$(sha256sum "$BWRAP" | awk '{print $1}')" >> "$OUT/provider-identity.txt"
printf 'kernel=%s\n' "$(uname -srmo)" >> "$OUT/provider-identity.txt"
ldd "$BWRAP" > "$OUT/provider-ldd.txt"
while IFS= read -r lib; do sha256sum "$lib"; done < <(ldd "$BWRAP" | awk '{for(i=1;i<=NF;i++) if($i ~ /^\//) print $i}' | sort -u) > "$OUT/provider-runtime-manifest.txt"
sha256sum "$OUT/provider-runtime-manifest.txt" | awk '{print "provider_runtime_manifest_sha256=" $1}' >> "$OUT/provider-identity.txt"

pass=0
fail=0
log="$OUT/spike.log"
: > "$log"

record_pass() { printf 'PASS %-38s %s\n' "$1" "${2:-}" | tee -a "$log"; pass=$((pass+1)); }
record_fail() { printf 'FAIL %-38s %s\n' "$1" "${2:-}" | tee -a "$log" >&2; fail=$((fail+1)); }

if [[ -d "$FIX" ]]; then
  chmod -R u+w "$FIX" 2>/dev/null || true
  rm -rf "$FIX"
fi
mkdir -p "$HOST" "$CAPTURE" "$SCRATCH" "$EVIL" "$TOOL"
printf 'declared-v1\n' > "$HOST/declared.txt"
printf 'TOP-SECRET-FILE\n' > "$HOST/secret.txt"
printf '#!/bin/sh\necho HOST_EVIL_TOOL\n' > "$EVIL/eviltool"
chmod +x "$EVIL/eviltool"
cp "$HOST/declared.txt" "$CAPTURE/declared.txt"
ln -s /fixture/hostrepo/secret.txt "$CAPTURE/escape-link"
chmod 0755 "$HOST" "$CAPTURE" "$EVIL"
chmod 0644 "$HOST"/* "$CAPTURE/declared.txt"

# Qualification-time toolchain capture. Ordinary sandbox starts do not read live
# host /usr or /etc. The captured tree is content-addressed and mounted read-only.
mkdir -p "$TOOL/usr/bin" "$TOOL/lib" "$TOOL/lib64" "$TOOL/etc" "$TOOL/dev" "$TOOL/tmp" "$TOOL/work/input" "$TOOL/work/scratch"
ln -s usr/bin "$TOOL/bin"
copy_file_exact() {
  local src="$1"
  [[ -e "$src" ]] || return 0
  mkdir -p "$TOOL$(dirname "$src")"
  cp -L "$src" "$TOOL$src"
}
copy_binary() {
  local logical="$1" resolved lib
  resolved="$(readlink -f "$logical")"
  copy_file_exact "$resolved"
  while IFS= read -r lib; do copy_file_exact "$lib"; done < <(
    ldd "$resolved" 2>/dev/null | awk '{for(i=1;i<=NF;i++) if ($i ~ /^\//) print $i}' | sort -u
  )
  if [[ "$logical" == /bin/* && "$resolved" == /usr/bin/* && "${logical##*/}" != "${resolved##*/}" ]]; then
    ln -sf "${resolved##*/}" "$TOOL/usr/bin/${logical##*/}"
  fi
}
for bin in /bin/sh /bin/bash /bin/cat /bin/sleep /bin/true /bin/rm /usr/bin/env /usr/bin/getent /usr/bin/curl /usr/bin/yes /usr/bin/head; do
  copy_binary "$bin"
done
# NSS modules are dlopen() dependencies and do not necessarily appear in ldd.
for lib in /lib/*/libnss_files.so.2 /lib/*/libnss_dns.so.2 /lib/*/libresolv.so.2; do
  [[ -e "$lib" ]] && copy_file_exact "$lib"
done
printf 'hosts: files dns\n' > "$TOOL/etc/nsswitch.conf"
cp /etc/resolv.conf "$TOOL/etc/resolv.conf"
cp /etc/hosts "$TOOL/etc/hosts"
printf 'a0:x:1000:1000::/homeless:/bin/sh\n' > "$TOOL/etc/passwd"
printf 'a0:x:1000:\n' > "$TOOL/etc/group"
chmod -R a-w "$TOOL"
(
  cd "$TOOL"
  find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
  find . -type l -print0 | LC_ALL=C sort -z | while IFS= read -r -d '' link; do printf 'LINK %s -> %s\n' "$link" "$(readlink "$link")"; done
) > "$OUT/toolchain.manifest"
sha256sum "$OUT/toolchain.manifest" | awk '{print "toolchain_manifest_sha256=" $1}' | tee "$OUT/toolchain-id.txt"
du -sb "$TOOL" | awk '{print "toolchain_bytes=" $1}' | tee "$OUT/toolchain-bytes.txt"

BARGS=(
  --unshare-user
  --unshare-all
  --die-with-parent
  --disable-userns
  --assert-userns-disabled
  --ro-bind "$TOOL" /
  --dev /dev
  --tmpfs /tmp
  --ro-bind "$CAPTURE" /work/input
  --bind "$SCRATCH" /work/scratch
  --clearenv
  --setenv PATH /usr/bin:/bin
  --setenv HOME /homeless
  --setenv PWD /work
  --setenv LANG C.UTF-8
  --chdir /work
)

run_a0() {
  env TOP_SECRET='ambient-secret-must-not-cross' PATH="$EVIL:/usr/bin:/bin" \
    "$BWRAP" "${BARGS[@]}" -- "$@" < /dev/null
}

run_started_shell() {
  local script="$1"
  run_a0 /bin/sh -c 'printf "SANDBOX_STARTED\n"; eval "$1"' sh "$script"
}

expect_started_denial() {
  local name="$1" script="$2" out rc
  set +e
  out="$(run_started_shell "$script" 2>&1)"
  rc=$?
  set -e
  if [[ "$out" != *SANDBOX_STARTED* ]]; then
    record_fail "$name" "sandbox did not start: $(printf '%s' "$out" | tr '\n' ' ')"
    return
  fi
  if [[ "$rc" -eq 0 ]]; then
    record_fail "$name" 'operation unexpectedly succeeded'
  else
    record_pass "$name"
  fi
}

# Positive smoke must succeed before any denial result is meaningful.
if [[ "$(run_a0 /bin/sh -c 'printf SANDBOX_SMOKE')" == SANDBOX_SMOKE ]]; then
  record_pass sandbox_smoke
else
  record_fail sandbox_smoke 'provider setup failed'
  printf 'pass=%d fail=%d\n' "$pass" "$fail" | tee "$OUT/spike-summary.txt"
  exit 1
fi

if ! command -v strace >/dev/null 2>&1; then
  record_fail ordinary_provider_no_inet_syscalls 'strace unavailable'
else
  trace="$OUT/provider-network.strace"
  strace -f -qq -e trace=network -o "$trace" env TOP_SECRET=trace PATH="$EVIL:/usr/bin:/bin" \
    "$BWRAP" "${BARGS[@]}" -- /bin/true < /dev/null
  if grep -Eq 'AF_INET6?|sockaddr_in' "$trace"; then
    record_fail ordinary_provider_no_inet_syscalls "unexpected INET syscall: $(grep -E 'AF_INET6?|sockaddr_in' "$trace" | head -3 | tr '\n' ';')"
  else
    record_pass ordinary_provider_no_inet_syscalls
  fi
fi

# Baseline outer network must work so sandbox denial is meaningful.
if curl -fsS --connect-timeout 3 --max-time 5 http://1.1.1.1/ >/dev/null 2>&1; then
  record_pass outer_network_baseline
else
  record_fail outer_network_baseline 'outer container could not reach 1.1.1.1'
fi

if [[ "$(run_a0 /bin/cat /work/input/declared.txt)" == 'declared-v1' ]]; then
  record_pass declared_capture_read
else
  record_fail declared_capture_read
fi

expect_started_denial undeclared_file_denied 'cat /fixture/hostrepo/secret.txt >/dev/null'

expect_started_denial symlink_escape_denied 'cat /work/input/escape-link >/dev/null'

expect_started_denial dotdot_escape_denied 'cd /work/input && cat ../../fixture/hostrepo/secret.txt >/dev/null'

if run_a0 /bin/sh -c 'test ! -e /homeless && test ! -e /root && test ! -e /run/user/1000'; then
  record_pass home_runtime_absent
else
  record_fail home_runtime_absent
fi

if run_a0 /bin/sh -c 'test ! -e /proc && test ! -e /sys && test ! -e /run'; then
  record_pass ambient_proc_sys_run_absent
else
  record_fail ambient_proc_sys_run_absent
fi

expect_started_denial captured_input_readonly 'printf changed > /work/input/declared.txt'

if run_a0 /bin/sh -c 'printf scratch-ok > /work/scratch/result.txt' && [[ "$(cat "$SCRATCH/result.txt")" == scratch-ok ]]; then
  record_pass private_scratch_writable
else
  record_fail private_scratch_writable
fi
rm -f "$SCRATCH/result.txt"

printf 'declared-v2-host-mutated\n' > "$HOST/declared.txt"
if [[ "$(run_a0 /bin/cat /work/input/declared.txt)" == 'declared-v1' ]]; then
  record_pass immutable_capture_vs_host_mutation
else
  record_fail immutable_capture_vs_host_mutation
fi

if run_a0 /usr/bin/env | LC_ALL=C sort > "$OUT/env.actual"; then
  cat > "$OUT/env.expected" <<'ENVEOF'
HOME=/homeless
LANG=C.UTF-8
PATH=/usr/bin:/bin
PWD=/work
ENVEOF
  if cmp -s "$OUT/env.expected" "$OUT/env.actual"; then
    record_pass fixed_environment_exact
  else
    record_fail fixed_environment_exact "expected=$(tr '\n' ';' < "$OUT/env.expected") actual=$(tr '\n' ';' < "$OUT/env.actual")"
  fi
else
  record_fail fixed_environment_exact 'env command failed'
fi

expect_started_denial host_path_injection_denied 'command -v eviltool >/dev/null 2>&1'

if run_a0 /bin/sh -c 'if [ -n "${TOP_SECRET+x}" ]; then exit 1; fi'; then
  record_pass inherited_secret_env_absent
else
  record_fail inherited_secret_env_absent
fi

if run_a0 /bin/sh -c 'if read x; then exit 1; else exit 0; fi'; then
  record_pass stdin_closed
else
  record_fail stdin_closed
fi

expect_started_denial network_connect_denied '/usr/bin/curl -fsS --connect-timeout 1 --max-time 2 http://1.1.1.1/ >/dev/null 2>&1'

expect_started_denial dns_denied '/usr/bin/getent hosts example.com >/dev/null 2>&1'

rm -f "$SCRATCH/grandchild-leak"
if run_a0 /bin/sh -c 'if value=$(cat /fixture/hostrepo/secret.txt 2>/dev/null); then printf "%s" "$value" > /work/scratch/grandchild-leak; fi; wait'; then
  if [[ ! -e "$SCRATCH/grandchild-leak" ]]; then
    record_pass descendant_escape_denied
  else
    record_fail descendant_escape_denied
  fi
else
  record_fail descendant_escape_denied 'sandbox command failed'
fi

# Host source bytes must remain outside the namespace and unchanged by sandbox writes.
host_hash_before=$(sha256sum "$HOST/secret.txt" | awk '{print $1}')
run_a0 /bin/sh -c 'printf private > /work/scratch/write-only-here; rm -f /work/scratch/write-only-here'
host_hash_after=$(sha256sum "$HOST/secret.txt" | awk '{print $1}')
if [[ "$host_hash_before" == "$host_hash_after" ]]; then
  record_pass host_source_not_mutated
else
  record_fail host_source_not_mutated
fi

# Provider crash: kill bwrap itself. PID namespace + die-with-parent must leave no
# process whose exact argv[0] is the marker. Do not grep full command strings:
# the parent shell script itself contains the marker text and would false-positive.
find_exact_argv0() {
  local want="$1" p first
  for p in /proc/[0-9]*; do
    [[ -r "$p/cmdline" ]] || continue
    first=''
    IFS= read -r -d '' first < "$p/cmdline" 2>/dev/null || true
    if [[ "$first" == "$want" ]]; then
      printf '%s\n' "${p##*/}"
    fi
  done
}
env TOP_SECRET=crash-test PATH="$EVIL:/usr/bin:/bin" \
  "$BWRAP" "${BARGS[@]}" -- /bin/bash -c 'bash -c "exec -a hermetic-a0-grandchild /bin/sleep 60" & wait' < /dev/null &
bp=$!
sleep 0.5
find_exact_argv0 hermetic-a0-grandchild > "$OUT/crash-before.txt"
if [[ ! -s "$OUT/crash-before.txt" ]]; then
  record_fail provider_crash_cleanup 'exact marked grandchild never started'
else
  kill -KILL "$bp" 2>/dev/null || true
  wait "$bp" 2>/dev/null || true
  sleep 0.5
  find_exact_argv0 hermetic-a0-grandchild > "$OUT/crash-after.txt"
  if [[ -s "$OUT/crash-after.txt" ]]; then
    record_fail provider_crash_cleanup "exact marked grandchild survived bwrap kill: $(tr '\n' ',' < "$OUT/crash-after.txt")"
    while read -r pid; do kill -KILL "$pid" 2>/dev/null || true; done < "$OUT/crash-after.txt"
  else
    record_pass provider_crash_cleanup
  fi
fi

# Cold/warm startup.
now_ns() { date +%s%N; }
t0=$(now_ns); run_a0 /bin/true; t1=$(now_ns)
cold_us=$(( (t1-t0)/1000 ))
printf 'cold_us=%d\n' "$cold_us" > "$OUT/latency.txt"
start=$(now_ns)
for i in $(seq 1 50); do run_a0 /bin/true; done
stop=$(now_ns)
warm50_us=$(( (stop-start)/1000 ))
warm_avg_us=$(( warm50_us/50 ))
printf 'warm50_total_us=%d\nwarm_avg_us=%d\n' "$warm50_us" "$warm_avg_us" >> "$OUT/latency.txt"
record_pass startup_latency "cold_us=$cold_us warm_avg_us=$warm_avg_us"

# 20-command output pressure, 64 KiB each.
for i in $(seq 1 20); do
  run_a0 /bin/sh -c 'yes X | head -c 65536' >/dev/null
 done
record_pass twenty_command_output_pressure

# Two concurrent sessions screening.
run_a0 /bin/sleep 2 & p1=$!
run_a0 /bin/sleep 2 & p2=$!
wait "$p1"; wait "$p2"
record_pass concurrency_two_screen

# Repeated lifecycle cleanup.
for i in $(seq 1 100); do run_a0 /bin/true; done
sleep 0.2
find_provider_pids() {
  local p exe
  for p in /proc/[0-9]*; do
    exe="$(readlink "$p/exe" 2>/dev/null || true)"
    [[ "$exe" == "$BWRAP" ]] && printf '%s\n' "${p##*/}"
  done
}
if [[ -n "$(find_provider_pids)" ]]; then
  record_fail repeated_cleanup_convergence 'provider process residue'
else
  record_pass repeated_cleanup_convergence
fi

# 60-second idle footprint. Sample complete descendant tree of bwrap at 1s/30s/59s.
tree_stats() {
  local root="$1"
  ps -eo pid=,ppid=,rss= | awk -v root="$root" '
    { pid[NR]=$1; ppid[NR]=$2; rss[NR]=$3 }
    END {
      member[root]=1
      changed=1
      while (changed) {
        changed=0
        for (i=1; i<=NR; i++) {
          if (member[ppid[i]] && !member[pid[i]]) { member[pid[i]]=1; changed=1 }
        }
      }
      count=0; sum=0
      for (i=1; i<=NR; i++) if (member[pid[i]]) { count++; sum+=rss[i] }
      printf "processes=%d rss_kib=%d\n", count, sum
    }'
}
run_a0 /bin/sleep 3 & c1=$!
run_a0 /bin/sleep 3 & c2=$!
sleep 1
c1_stats="$(tree_stats "$c1")"
c2_stats="$(tree_stats "$c2")"
printf 'sandbox1 %s\nsandbox2 %s\n' "$c1_stats" "$c2_stats" > "$OUT/concurrency2-footprint.txt"
combined="$(printf '%s\n%s\n' "$c1_stats" "$c2_stats" | awk -F'[ =]' '{p+=$2;r+=$4} END{printf "processes=%d rss_kib=%d",p,r}')"
printf 'combined %s\n' "$combined" >> "$OUT/concurrency2-footprint.txt"
wait "$c1"; wait "$c2"
record_pass concurrency_two_footprint "$combined"

env PATH="$EVIL:/usr/bin:/bin" "$BWRAP" "${BARGS[@]}" -- /bin/sleep 60 < /dev/null &
idle_pid=$!
sleep 1
printf 't1 ' > "$OUT/idle-footprint.txt"; tree_stats "$idle_pid" >> "$OUT/idle-footprint.txt"
sleep 29
printf 't30 ' >> "$OUT/idle-footprint.txt"; tree_stats "$idle_pid" >> "$OUT/idle-footprint.txt"
sleep 29
printf 't59 ' >> "$OUT/idle-footprint.txt"; tree_stats "$idle_pid" >> "$OUT/idle-footprint.txt"
wait "$idle_pid"
record_pass idle_60s_cleanup "$(tr '\n' ';' < "$OUT/idle-footprint.txt")"

# Task 7 opt-in: exercise the production ShellBeam runtime against this exact
# native-qualified provider/toolchain fixture before A0 removes private state.
# The default Task 0 qualification path does not set this flag and is unchanged.
if [[ "${HERMETIC_A0_RUN_TASK7:-0}" == 1 ]]; then
  if env \
    HERMETIC_V1_NATIVE_REQUIRED=1 \
    HERMETIC_V1_NATIVE_BWRAP="$BWRAP" \
    HERMETIC_V1_NATIVE_TOOLCHAIN="$TOOL" \
    HERMETIC_V1_NATIVE_SECURITY_POLICY="${HERMETIC_A0_SECURITY_POLICY:-}" \
    go test ./cmd/shellbeam -run '^TestHermeticV1NativeProductionMatrix$' -count=1 -v 2>&1 | tee "$OUT/task7-production.log"; then
    record_pass task7_production_runtime_matrix
  else
    record_fail task7_production_runtime_matrix 'production native matrix failed'
  fi
  if go test ./internal/app/evidence -run '^TestE27InputTraceBindingCannotNarrowEvidenceSourceValidity$' -count=1 -v 2>&1 | tee "$OUT/task7-e27.log"; then
    record_pass task7_e27_nonhermetic_no_narrowing
  else
    record_fail task7_e27_nonhermetic_no_narrowing 'non-hermetic E27 authority gate failed'
  fi
fi

# No provider-private storage creep beyond bounded evidence files/fixture.
du -sb "$FIX" | tee "$OUT/fixture-bytes.txt" >/dev/null
if [[ -n "$(find_provider_pids)" ]]; then
  record_fail final_process_convergence
else
  record_pass final_process_convergence
fi

# Provider-private capture/toolchain/scratch must converge after the run.
fixture_bytes_before_cleanup="$(du -sb "$FIX" | awk '{print $1}')"
printf 'fixture_bytes_before_cleanup=%s\n' "$fixture_bytes_before_cleanup" > "$OUT/storage.txt"
chmod -R u+w "$FIX" 2>/dev/null || true
rm -rf "$FIX"
if [[ ! -e "$FIX" ]]; then
  record_pass provider_storage_cleanup
else
  record_fail provider_storage_cleanup
fi
printf 'evidence_bytes_after_cleanup=%s\n' "$(du -sb "$OUT" | awk '{print $1}')" >> "$OUT/storage.txt"

printf 'pass=%d fail=%d\n' "$pass" "$fail" | tee "$OUT/spike-summary.txt"
if (( fail != 0 )); then
  echo 'hermetic_provider_a0 verdict=FAIL reason=spike_failure exit=1'
  exit 1
fi
echo 'hermetic_provider_a0 verdict=PASS reason=native_linux_provider_qualified exit=0'
