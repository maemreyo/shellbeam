#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

out_dir="$repo_root/.build/resource-enforcement-native"
rm -rf "$out_dir"
mkdir -p "$out_dir"

verdict="FAIL"
reason="not_completed"
created_root=0
resource_root="${SHELLBEAM_RESOURCE_CGROUP_ROOT:-}"
mount_root="/sys/fs/cgroup"

write_summary() {
  local exit_code="$1"
  local kernel go_version
  kernel="$(uname -sr 2>/dev/null || true)"
  go_version="$(go version 2>/dev/null || true)"
  cat > "$out_dir/summary.json" <<JSON
{"schema_version":1,"verdict":"$verdict","reason":"$reason","exit_code":$exit_code,"platform":"$(uname -s 2>/dev/null || printf unknown)","kernel":"$kernel","go":"$go_version"}
JSON
}

cleanup_root() {
  if [[ "$created_root" != 1 || -z "$resource_root" || ! -d "$resource_root" ]]; then
    return 0
  fi
  set +e
  shopt -s nullglob
  local child events populated i
  for child in "$resource_root"/job-* "$resource_root"/probe-*; do
    [[ -d "$child" ]] || continue
    sudo sh -c "printf '1' > '$child/cgroup.kill'" >/dev/null 2>&1
    events="$child/cgroup.events"
    for i in $(seq 1 100); do
      populated="$(awk '$1=="populated" {print $2}' "$events" 2>/dev/null)"
      [[ "$populated" == 0 ]] && break
      sleep 0.02
    done
    sudo rmdir "$child" >/dev/null 2>&1
  done
  sudo rmdir "$resource_root" >/dev/null 2>&1
  shopt -u nullglob
  set -e
}

on_exit() {
  local rc=$?
  trap - EXIT
  cleanup_root
  if [[ "$rc" == 0 && "$verdict" != PASS ]]; then
    verdict="FAIL"
    reason="verdict_not_frozen"
    rc=1
  fi
  write_summary "$rc"
  printf 'resource_enforcement_native verdict=%s reason=%s exit=%s\n' "$verdict" "$reason" "$rc"
  exit "$rc"
}
trap on_exit EXIT

fail() {
  reason="$1"
  printf 'resource enforcement native failure: %s\n' "$reason" >&2
  return 1
}

if [[ "$(uname -s)" != Linux ]]; then
  verdict="NOT_RUN"
  reason="linux_required"
  exit 3
fi

if [[ ! -r "$mount_root/cgroup.controllers" || ! -r "$mount_root/cgroup.subtree_control" ]]; then
  fail "cgroup_v2_unavailable"
fi
if ! grep -qw memory "$mount_root/cgroup.controllers" || ! grep -qw pids "$mount_root/cgroup.controllers"; then
  fail "required_controllers_unavailable"
fi

if [[ -z "$resource_root" ]]; then
  command -v sudo >/dev/null 2>&1 || fail "sudo_unavailable_for_ci_provisioning"
  # The cgroup-v2 root is exempt from the no-internal-process rule. Enable the
  # two controllers needed by the dedicated ShellBeam test subtree.
  sudo sh -c "printf '+memory +pids\\n' > '$mount_root/cgroup.subtree_control'" || fail "parent_controller_enable_failed"
  resource_root="$mount_root/shellbeam-native-${GITHUB_RUN_ID:-local}-$$"
  sudo mkdir "$resource_root" || fail "delegated_root_create_failed"
  created_root=1
  uid="$(id -u)"
  gid="$(id -g)"
  sudo chown "$uid:$gid" "$resource_root" "$resource_root/cgroup.procs" "$resource_root/cgroup.threads" "$resource_root/cgroup.subtree_control" || fail "delegation_chown_failed"
  printf '+memory +pids\n' > "$resource_root/cgroup.subtree_control" || fail "delegated_controller_enable_failed"
fi

if [[ "$resource_root" != /* || ! -d "$resource_root" ]]; then
  fail "invalid_delegated_root"
fi
if [[ -n "$(cat "$resource_root/cgroup.procs")" ]]; then
  fail "delegated_root_not_empty"
fi
if ! grep -qw memory "$resource_root/cgroup.controllers" || ! grep -qw pids "$resource_root/cgroup.controllers"; then
  fail "delegated_controllers_unavailable"
fi
if ! grep -qw memory "$resource_root/cgroup.subtree_control" || ! grep -qw pids "$resource_root/cgroup.subtree_control"; then
  fail "delegated_controllers_not_enabled"
fi

# Prove the unprivileged test user can do exactly the leaf operations the
# provider requires. Remove the probe before provider qualification so the root
# remains dedicated and foreign-child free.
probe="$resource_root/probe-script"
mkdir "$probe" || fail "probe_create_failed"
for control in memory.max memory.oom.group pids.max cgroup.kill cgroup.events memory.events pids.events cgroup.type; do
  [[ -e "$probe/$control" ]] || fail "probe_control_missing"
done
printf 'max' > "$probe/memory.max" || fail "probe_memory_write_failed"
printf '0' > "$probe/memory.oom.group" || fail "probe_memory_group_write_failed"
printf 'max' > "$probe/pids.max" || fail "probe_pids_write_failed"
printf '1' > "$probe/cgroup.kill" || fail "probe_kill_failed"
rmdir "$probe" || fail "probe_remove_failed"

export SHELLBEAM_RESOURCE_CGROUP_ROOT="$resource_root"
printf 'go=%s\n' "$(go version)" > "$out_dir/environment.txt"
printf 'kernel=%s\n' "$(uname -sr)" >> "$out_dir/environment.txt"
printf 'controllers=%s\n' "$(tr '\n' ' ' < "$resource_root/cgroup.controllers")" >> "$out_dir/environment.txt"
printf 'subtree_control=%s\n' "$(tr '\n' ' ' < "$resource_root/cgroup.subtree_control")" >> "$out_dir/environment.txt"

reason="unit_resource_gate_failed"
if ! go test -json ./internal/adapter/process ./internal/app/daemon -run 'Resource' -count=1 | tee "$out_dir/unit.jsonl"; then
  fail "$reason"
fi

reason="native_integration_failed"
if ! go test -json ./tests/integration -run '^TestResourceEnforcementNative' -count=1 -timeout=180s | tee "$out_dir/integration.jsonl"; then
  fail "$reason"
fi

reason="owned_cgroup_creep"
if find "$resource_root" -mindepth 1 -maxdepth 1 -type d \( -name 'job-*' -o -name 'probe-*' \) -print -quit | grep -q .; then
  fail "$reason"
fi
if [[ "$(awk '$1=="populated" {print $2}' "$resource_root/cgroup.events")" != 0 ]]; then
  fail "delegated_root_still_populated"
fi

verdict="PASS"
reason="native_linux_acceptance_passed"
