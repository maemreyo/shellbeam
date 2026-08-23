#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != Linux ]]; then
  echo 'hermetic_boundary_v1 verdict=NOT_RUN reason=linux_required exit=3'
  exit 3
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
export HERMETIC_A0_RUN_TASK7=1
./scripts/test-hermetic-provider-a0.sh

out="$repo_root/.build/hermetic-a0"
a0="$out/spike.log"
prod="$out/task7-production.log"
e27="$out/task7-e27.log"

require() {
  local file="$1" pattern="$2" label="$3"
  if ! grep -Fq -- "$pattern" "$file"; then
    printf 'FAIL %-42s missing marker %s in %s\n' "$label" "$pattern" "$file" >&2
    exit 1
  fi
}

require "$a0" 'PASS undeclared_file_denied' case_01
require "$prod" 'case_01_undeclared_repo_read_denied' case_01
require "$a0" 'PASS symlink_escape_denied' case_02
require "$a0" 'PASS dotdot_escape_denied' case_02
require "$prod" 'case_02_escape_paths_and_symlink_denied' case_02
require "$a0" 'PASS network_connect_denied' case_03
require "$a0" 'PASS dns_denied' case_03
require "$prod" 'case_03_network_and_dns_denied' case_03
require "$a0" 'PASS inherited_secret_env_absent' case_04
require "$a0" 'PASS fixed_environment_exact' case_04
require "$prod" 'case_04_inherited_secret_environment_absent' case_04
require "$a0" 'PASS host_path_injection_denied' case_05
require "$prod" 'case_05_host_path_injection_impossible' case_05
require "$a0" 'PASS descendant_escape_denied' case_06
require "$prod" 'case_06_descendant_tree_cannot_escape' case_06
require "$a0" 'PASS immutable_capture_vs_host_mutation' case_07
require "$prod" 'case_07_post_capture_host_mutation_cannot_change_input' case_07
require "$a0" 'PASS provider_crash_cleanup' case_08
require "$prod" 'case_08_provider_kill_never_promotes_scope' case_08
require "$prod" 'case_09_capture_budget_overflow_prevents_spawn' case_09
require "$a0" 'PASS host_source_not_mutated' case_10
require "$prod" 'case_10_sandbox_writes_do_not_mutate_host' case_10
require "$prod" 'case_11_only_successful_boundary_promotes_declared_scope' case_11
require "$e27" '--- PASS: TestE27InputTraceBindingCannotNarrowEvidenceSourceValidity' case_12
require "$a0" 'PASS repeated_cleanup_convergence' case_13
require "$prod" 'case_13_repeated_production_runs_converge_private_state' case_13

for marker in startup_latency twenty_command_output_pressure concurrency_two_footprint idle_60s_cleanup final_process_convergence provider_storage_cleanup; do
  require "$a0" "PASS $marker" "resource_$marker"
done
require "$a0" 'PASS task7_production_runtime_matrix' production_matrix
require "$a0" 'PASS task7_e27_nonhermetic_no_narrowing' e27_authority

for i in $(seq -w 1 13); do
  printf 'CASE %s PASS\n' "$i"
done
printf 'RESOURCE_STORAGE PASS\n'
echo 'hermetic_boundary_v1 verdict=PASS reason=native_linux_production_matrix exit=0'
