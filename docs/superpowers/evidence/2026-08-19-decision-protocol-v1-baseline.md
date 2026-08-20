# Decision Protocol V1 Implementation Baseline

- implementation_base: `39de4426a95cfb58cbb99a75165b9feb5cc7169c`
- previous_implementation_base: `27207d94b097040b571081c8c49d9c09487460c5`
- plan_authoring_base: `27207d94b097040b571081c8c49d9c09487460c5`
- frozen_spec_sha256: `6cf49426243f26e8bec862c29651304ccc4abd5e1f91947f9899fe21fd72f7fa`
- go_version: `go version go1.26.6 darwin/arm64`
- full_suite: PASS
- targeted_admission_policy_store: PASS
- owner_overlap_since_plan_authoring: REVIEWED_THROUGH_39de4426a95cfb58cbb99a75165b9feb5cc7169c
- reviewed_owner_patch_sha256: `d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d`
- reviewed_owner_paths_sha256: `2f5ae958bce1c3d33ab3ea391e85c8d24cf48afdc140512cdbe41ae4635cd05f`

## Task 13 final acceptance verification — 2026-08-20

- head_before_final_acceptance_commit: `5a2f39600f9d46f60d9f87a4ed40af63f4c41b80`
- implementation_base_reconfirmed: `39de4426a95cfb58cbb99a75165b9feb5cc7169c`
- decision_protocol_focused_gate: PASS
  - command: `go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol ./internal/adapter/store ./internal/app/daemon ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./api/schema ./cmd/shellbeam -count=1`
  - key fresh timings: store `252.166s`, daemon `143.512s`, cmd/shellbeam `395.683s`
- decision_protocol_race_gate: PASS
  - command: `go test -race ./internal/app/decisionprotocol ./internal/adapter/store ./internal/app/daemon ./internal/adapter/ipc ./cmd/shellbeam -run 'DecisionProtocol|Experiment|Selection|Override' -count=1`
  - key fresh timings: app/decisionprotocol `2.178s`, store `8.356s`, daemon `5.974s`, ipc `3.433s`, cmd/shellbeam `31.935s`
- full_suite_final: PASS
  - command: `go test ./...`
  - key fresh timings: cmd/shellbeam `314.346s`, app/decisionprotocol `0.563s`, contract `0.968s`; remaining packages passed or were cached from the same latest source tree
- dirty_base_gate_final: PASS
  - command: `go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json`
  - base: `39de4426a95cfb58cbb99a75165b9feb5cc7169c`
  - source_fingerprint: `a1e1c63d010b29af41fe12709d86a1ae719de8c61411936f838f215172bd3704`
  - status: `passed`, exit_code: `0`
  - cmd/shellbeam post-suite: `329.909s`
- diff_check_final: PASS (`git diff --check`)
- traceability_final: `PASS invariants=48/48 sections=57/57 tasks=14/14`
- acceptance_observation_budget_regression: one bounded retry is permitted only for fresh workspace observation diagnostic `observation_budget_exceeded` at activation-generation and source-compatibility reads; repeated failure and every other non-fresh/mismatched snapshot remain fail-closed.
- acceptance_harness_pressure: non-restart Decision Protocol acceptance/security uses the real in-process daemon/store/IPC composition; external native process kill/reopen is reserved for restart tests. This removed avoidable native process/build pressure without changing production observation budgets or semantic coverage.
- oracle_analytics_production_contract: PASS; production projection omits Pass@N, candidate recall, selection regret, and other oracle metrics when candidates were not exhaustively oracle-evaluated.
