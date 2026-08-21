# Decision Protocol V1 Multi-Workspace Routing Acceptance

- date: `2026-08-21`
- branch: `fix/decision-protocol-multi-workspace`
- task_1_transport_commit: `09dabb1`
- task_2_runtime_commit: `72cad69`
- task_3_failure_projection_commit: `8ed5b83`
- production_connector_daemon_incarnation_before_probe: `01M0HX31D3510A3KT32TQWKFY8`
- production_connector_daemon_incarnation_after_probe: `01M0HX31D3510A3KT32TQWKFY8`

## Isolated native daemon acceptance

The probe built the current worktree source into a temporary binary and used only temporary state, runtime, socket, and Git repository paths. It started one temporary daemon process and registered two workspaces backed by two distinct repositories. Cleanup was scoped to that temporary process and temporary root only.

- operation: `dp-multiws-task4-isolated-probe-python2-20260821`
- terminal outcome: `success`, exit code `0`
- registered workspaces: `2`
- distinct repository identities: `2`
- selector A: PASS; policy snapshot repository derived server-side from workspace A
- selector B: PASS; policy snapshot repository derived server-side from workspace B
- no selector with two registered workspaces: `decision_context_unavailable`
- unknown valid selector: `workspace_not_found` with safe `workspace_id` detail
- nested `decision.repository_id`: `invalid_input`
- nested `decision.workspace_id`: `invalid_input`
- isolated daemon PID: `96061`
- probe isolation: temporary state/runtime/socket/repositories only
- cleanup scope: probe process and probe temporary root only

The ShellBeam connector daemon incarnation remained unchanged across the probe, and the probe never invoked production service stop/restart/kill operations or reused the production daemon state/runtime/socket.

## Automated native acceptance

`TestDecisionProtocolNativeMultiWorkspaceRouting` creates two independently registered real Git workspaces in isolated test state, starts an isolated daemon, and verifies selector A/B repository derivation plus ambiguity and unknown-selector handling through real IPC. Nested repository/workspace forgery rejection is covered by the raw-IPC isolated probe above and the transport contract tests.

The automated test also exposed and closed one E2E gap: the workspace application service returns its not-found sentinel without public details, so the Decision resolver now reprojects that sentinel as `workspace_not_found` with only the selected safe `workspace_id` detail.

## Final verification gates

- original Decision Protocol traceability: `PASS invariants=48/48 sections=57/57 tasks=14/14`
- amendment traceability: `PASS sections=10/10 tasks=5/5 anchors=11/11 original_traceability=PASS`
- `git diff --check`: PASS
- `go run ./tools/devctl check --base origin/main --json`: PASS
- final focused package gate: PASS
  - `cmd/shellbeam`: `468.175s`
  - `internal/adapter/mcp`: `1.867s`
  - `internal/adapter/ipc`: `5.323s`
  - `internal/app/decisionprotocol`: `1.874s`
  - `internal/core/failure`: `1.032s`
  - `tests/contract`: `2.436s`

A final structural check also caught `populateRequestFromInput` at 81 lines against the repository hard cap of 80. The existing kill-input mapping was extracted to `applyKillInput` without changing transport behavior; the MCP package test and `devctl check` both pass after that cohesion-only refactor.
