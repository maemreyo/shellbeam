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
moved_to_manager=0
resource_root="${SHELLBEAM_RESOURCE_CGROUP_ROOT:-}"
manager_root=""
mount_root="/sys/fs/cgroup"
original_cgroup=""

write_summary() {
  local exit_code="$1"
  local kernel go_version
  kernel="$(uname -sr 2>/dev/null || true)"
  go_version="$(go version 2>/dev/null || true)"
  cat > "$out_dir/summary.json" <<JSON
{"schema_version":1,"verdict":"$verdict","reason":"$reason","exit_code":$exit_code,"platform":"$(uname -s 2>/dev/null || printf unknown)","kernel":"$kernel","go":"$go_version"}
JSON
}

cleanup_populated_child() {
  local child="$1"
  local events="$child/cgroup.events"
  local populated i
  [[ -d "$child" ]] || return 0
  sudo sh -c "printf '1' > '$child/cgroup.kill'" >/dev/null 2>&1
  for i in $(seq 1 100); do
    populated="$(awk '$1=="populated" {print $2}' "$events" 2>/dev/null)"
    [[ "$populated" == 0 ]] && break
    sleep 0.02
  done
  sudo rmdir "$child" >/dev/null 2>&1
}

cleanup_root() {
  if [[ "$created_root" != 1 || -z "$resource_root" || ! -d "$resource_root" ]]; then
    return 0
  fi
  set +e
  shopt -s nullglob
  local child

  # CI is the operator/bootstrapper here. Move this shell back out before the
  # temporary manager cgroup is removed. Product code never reparents itself.
  if [[ "$moved_to_manager" == 1 && -n "$original_cgroup" && -e "$original_cgroup/cgroup.procs" ]]; then
    sudo sh -c "printf '%s\n' '$$' > '$original_cgroup/cgroup.procs'" >/dev/null 2>&1
    moved_to_manager=0
  fi

  for child in "$resource_root"/job-* "$resource_root"/probe-*; do
    cleanup_populated_child "$child"
  done
  if [[ -n "$manager_root" && -d "$manager_root" ]]; then
    cleanup_populated_child "$manager_root"
  fi
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

self_cgroup_path="$(awk -F: '$1 == "0" {print $3}' /proc/self/cgroup)"
if [[ -z "$self_cgroup_path" || "$self_cgroup_path" != /* ]]; then
  fail "self_cgroup_unavailable"
fi
original_cgroup="$mount_root$self_cgroup_path"
if [[ ! -e "$original_cgroup/cgroup.procs" ]]; then
  fail "self_cgroup_unavailable"
fi

if [[ -z "$resource_root" ]]; then
  command -v sudo >/dev/null 2>&1 || fail "sudo_unavailable_for_ci_provisioning"
  # Bootstrap a temporary delegation. The root stays process-empty so memory
  # and pids can be enabled for children. A reserved manager leaf contains the
  # test shell, making job-* cgroups siblings within the same delegation.
  sudo sh -c "printf '+memory +pids\\n' > '$mount_root/cgroup.subtree_control'" || fail "parent_controller_enable_failed"
  resource_root="$mount_root/shellbeam-native-${GITHUB_RUN_ID:-local}-$$"
  sudo mkdir "$resource_root" || fail "delegated_root_create_failed"
  created_root=1
  uid="$(id -u)"
  gid="$(id -g)"
  sudo chown "$uid:$gid" "$resource_root" "$resource_root/cgroup.procs" "$resource_root/cgroup.threads" "$resource_root/cgroup.subtree_control" || fail "delegation_chown_failed"
  printf '+memory +pids\n' > "$resource_root/cgroup.subtree_control" || fail "delegated_controller_enable_failed"
  manager_root="$resource_root/manager"
  mkdir "$manager_root" || fail "manager_create_failed"
  sudo sh -c "printf '%s\n' '$$' > '$manager_root/cgroup.procs'" || fail "manager_bootstrap_failed"
  moved_to_manager=1
fi

if [[ "$resource_root" != /* || ! -d "$resource_root" ]]; then
  fail "invalid_delegated_root"
fi
manager_root="$resource_root/manager"
if [[ ! -d "$manager_root" || "$(tr -d '[:space:]' < "$manager_root/cgroup.type")" != domain ]]; then
  fail "manager_unavailable"
fi
if ! grep -qx "$$" "$manager_root/cgroup.procs"; then
  fail "manager_not_current"
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

# Prove the unprivileged test user can configure a sibling leaf and migrate a
# descendant from manager into it. Provider qualification separately proves
# the actual clone3(CLONE_INTO_CGROUP) atomic-birth path.
probe="$resource_root/probe-script"
mkdir "$probe" || fail "probe_create_failed"
for control in memory.max memory.swap.max memory.oom.group pids.max cgroup.procs cgroup.kill cgroup.events memory.events pids.events cgroup.type; do
  [[ -e "$probe/$control" ]] || fail "probe_control_missing"
done
printf 'max' > "$probe/memory.max" || fail "probe_memory_write_failed"
printf '0' > "$probe/memory.swap.max" || fail "probe_memory_swap_write_failed"
printf '0' > "$probe/memory.oom.group" || fail "probe_memory_group_write_failed"
printf 'max' > "$probe/pids.max" || fail "probe_pids_write_failed"
/bin/sleep 30 &
probe_pid=$!
if ! printf '%s\n' "$probe_pid" > "$probe/cgroup.procs"; then
  kill "$probe_pid" >/dev/null 2>&1 || true
  wait "$probe_pid" >/dev/null 2>&1 || true
  fail "probe_process_migration_failed"
fi
printf '1' > "$probe/cgroup.kill" || fail "probe_kill_failed"
wait "$probe_pid" >/dev/null 2>&1 || true
rmdir "$probe" || fail "probe_remove_failed"

export SHELLBEAM_RESOURCE_CGROUP_ROOT="$resource_root"
printf 'go=%s\n' "$(go version)" > "$out_dir/environment.txt"
printf 'kernel=%s\n' "$(uname -sr)" >> "$out_dir/environment.txt"
printf 'controllers=%s\n' "$(tr '\n' ' ' < "$resource_root/cgroup.controllers")" >> "$out_dir/environment.txt"
printf 'subtree_control=%s\n' "$(tr '\n' ' ' < "$resource_root/cgroup.subtree_control")" >> "$out_dir/environment.txt"
printf 'manager=%s\n' "$(tr '\n' ' ' < "$manager_root/cgroup.procs")" >> "$out_dir/environment.txt"

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
if [[ "$(awk '$1=="populated" {print $2}' "$manager_root/cgroup.events")" != 1 ]]; then
  fail "manager_not_populated"
fi
if ! grep -qx "$$" "$manager_root/cgroup.procs"; then
  fail "manager_not_current"
fi

verdict="PASS"
reason="native_linux_acceptance_passed"
