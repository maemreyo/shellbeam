# Decision Protocol Multi-Workspace Routing Amendment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Decision Protocol route safely in multi-workspace daemons through an outer `workspace_id` selector, preserve server-derived repository authority, and expose stable Decision-specific failures.

**Architecture:** Keep the nested Decision semantic DTO unchanged. Carry an optional outer selector through MCP → bridge → IPC, validate it at both transport boundaries, resolve it against registered workspace state in the daemon, and derive `repository_id` only from the resolved workspace. Add application-level typed lookup errors plus one Decision boundary projector to stable public failure codes; do not change Decision core state-machine semantics.

**Tech Stack:** Go, strict JSON schemas, ShellBeam MCP/IPC v2, Decision Protocol V1, Python traceability checker, Git worktrees.

**Spec:** `docs/superpowers/specs/2026-08-21-decision-protocol-multi-workspace-routing-amendment.md` (SHA-256 `8c79f38bf53cf76b71e83a60a78255e76ed0faf2443a8af2329963d2786be898`)

**Plan authoring base:** `a39447c` (approved amendment spec commit)

## Global Constraints

- Work only in `/Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/decision-protocol-multi-workspace` on `fix/decision-protocol-multi-workspace`.
- Do not run implementation/tests from the root repository worktree.
- Do not stop, kill, restart, or mutate the production daemon.
- The realistic acceptance probe must use an isolated runtime/state directory and isolated daemon process.
- `repository_id` remains server-derived; never add it to caller-writable Decision transport fields.
- Outer `workspace_id` is an address selector only; nested `decision.workspace_id` and `decision.repository_id` remain rejected.
- Backward compatibility is the **singleton fallback**: an omitted selector succeeds only when exactly one registered workspace exists.
- Traceability names **nested `decision.repository_id`** explicitly as caller-forbidden, alongside nested `decision.workspace_id`.
- Do not infer Decision routing from cwd, last-used workspace, session history, or caller repository identity.
- Do not modify Decision core state-machine semantics or weaken `docs/superpowers/specs/2026-08-19-decision-protocol-design.md`.
- Keep the original Decision V1 traceability checker passing unchanged.
- TDD is mandatory: each production change follows RED → verify RED → minimum GREEN → verify GREEN.
- Before each commit use `git -c core.hooksPath=.githooks commit ...`.
- Edit-loop verification uses focused tests plus `go run ./tools/devctl test --dirty --base origin/main --json`; release-style full fresh suites are reserved for the final completion gate.

---

### Task 0: Bind amendment traceability and prove the baseline

**Files:**
- Create: `docs/superpowers/plans/2026-08-21-decision-protocol-multi-workspace-routing-amendment-traceability.json`
- Create: `scripts/check-decision-protocol-multi-workspace-routing-amendment.py`
- Test: existing Decision/MCP/IPC/failure suites only; no production edits

**Interfaces:**
- Consumes: approved amendment spec SHA `8c79f38bf53cf76b71e83a60a78255e76ed0faf2443a8af2329963d2786be898`; original Decision V1 traceability checker.
- Produces: machine-readable mapping from amendment sections 1-10 to Tasks 0-4 and a checker that requires the transport/failure acceptance anchors.

- [ ] **Step 1: Add the amendment traceability metadata and checker**

The metadata must list Tasks 0-4, map every amendment section 1-10 to at least one task, and include these required acceptance anchors exactly:

`outer_workspace_selector`, `singleton_fallback`, `decision_context_unavailable`, `workspace_not_found`, `nested_workspace_rejected`, `nested_repository_rejected`, `decision_episode_not_found`, `decision_candidate_not_found`, `decision_experiment_not_found`, `decision_protocol_rejected`, `isolated_multi_workspace_probe`.

The checker must verify the spec SHA, task headings, section coverage, anchors, and that the original checker still reports PASS.

- [ ] **Step 2: Run traceability gates**

Run:

`python3 scripts/check-decision-protocol-v1-plan-traceability.py`

`python3 scripts/check-decision-protocol-multi-workspace-routing-amendment.py`

Expected: original `PASS invariants=48/48 sections=57/57 tasks=14/14` plus amendment PASS for all 10 sections and 5 tasks.

- [ ] **Step 3: Run unchanged focused baseline**

Run the focused baseline (do not broaden to fresh all-package suites in the edit loop):

`go test ./cmd/shellbeam -run 'DecisionProtocol' -count=1 -timeout=90s`

`go test ./internal/adapter/mcp -run 'DecisionProtocol' -count=1 -timeout=60s`

`go test ./internal/adapter/ipc -run 'DecisionProtocol' -count=1 -timeout=60s`

`go test ./internal/app/decisionprotocol -count=1 -timeout=60s`

`go test ./internal/core/failure -count=1 -timeout=60s`

Expected: all PASS before production edits. If this baseline fails, investigate before continuing.

- [ ] **Step 4: Commit the planning gate**

Run:

`git add docs/superpowers/plans/2026-08-21-decision-protocol-multi-workspace-routing-amendment.md docs/superpowers/plans/2026-08-21-decision-protocol-multi-workspace-routing-amendment-traceability.json scripts/check-decision-protocol-multi-workspace-routing-amendment.py`

`git -c core.hooksPath=.githooks commit -m "docs: plan decision multi-workspace routing"`

---

### Task 1: Carry the outer workspace selector through MCP, bridge, IPC, and schemas

**Files:**
- Modify: `internal/adapter/mcp/input_fields.go`
- Modify: `internal/adapter/mcp/decision_protocol_input.go`
- Modify: `internal/adapter/mcp/request.go`
- Test: `internal/adapter/mcp/decision_protocol_test.go`
- Modify: `internal/adapter/ipc/client_v2_request.go`
- Modify: `internal/adapter/ipc/protocol_v2_fields.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Test: `internal/adapter/ipc/decision_protocol_test.go`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Modify generated/checker artifacts only if repository schema tooling requires them

**Interfaces:**
- Consumes: existing top-level `input.WorkspaceID`, `bridge.Request.WorkspaceID`, and `RequestV2.WorkspaceID` fields.
- Produces: optional outer selector preserved end-to-end for Decision actions; nested semantic DTO remains unchanged.

- [ ] **Step 1: Write RED MCP tests**

Add tests proving:

1. `decision.policy.snapshot` with valid outer `workspace_id` is accepted and `client.last.WorkspaceID` receives it.
2. invalid outer workspace ID is rejected as `invalid_input` with `field=workspace_id`.
3. nested `decision.workspace_id` and `decision.repository_id` remain rejected.
4. an unrelated outer field is still rejected by strict per-action validation.

Run:

`go test ./internal/adapter/mcp -run 'DecisionProtocol.*Workspace|DecisionProtocolMCPRejectsCrossActionAndServerOwnedFields' -count=1`

Expected RED: valid outer selector is rejected/not forwarded by current Decision field whitelist/mapping.

- [ ] **Step 2: Implement minimum MCP selector support**

Change Decision action fields from:

`return []string{"decision"}`

to:

`return []string{"decision", "workspace_id"}`.

In `validateDecisionMCPInput`, if `v.WorkspaceID != ""`, call `workspace.ParseWorkspaceID(v.WorkspaceID)` and return the existing safe Decision input error with `field=workspace_id` on failure.

In `populateRequestFromInput`, set `request.WorkspaceID = in.WorkspaceID` before cloning `request.Decision` and returning.

Do not add workspace/repository fields to `DecisionRequest`.

- [ ] **Step 3: Verify MCP GREEN**

Run the focused command from Step 1. Expected PASS.

- [ ] **Step 4: Write RED IPC/bridge tests**

Add tests proving:

1. `requestV2FromBridge(bridge.Request{Action: "decision.policy.snapshot", WorkspaceID: ws, Decision: ...})` preserves `WorkspaceID`.
2. strict IPC decode accepts valid outer `workspace_id` for a Decision action.
3. invalid outer workspace ID fails `invalid_input field=workspace_id`.
4. nested `workspace_id` / `repository_id` remain rejected.
5. unrelated Decision outer fields remain closed.

Run:

`go test ./internal/adapter/ipc -run 'DecisionProtocol.*Workspace|DecisionProtocolSecurity|DecisionProtocolRejects' -count=1`

Expected RED: current `protocol_v2_fields.go` allows only `decision`, and bridge request conversion drops `WorkspaceID`.

- [ ] **Step 5: Implement minimum IPC/bridge selector support**

In `applyBridgeRequestV2`, set `req.WorkspaceID = in.WorkspaceID` in the Decision action branch before mapping `req.Decision`.

In `actionFieldsV2`, allow `{"decision", "workspace_id"}` for all Decision actions.

In `validateRequestV2`, before `validateDecisionRequestV2`, parse a non-empty `v.WorkspaceID` with `workspace.ParseWorkspaceID`; map parse failure to `failure.InvalidInput` with `field=workspace_id`.

Keep `validateDecisionRawFieldsV2` and `DecisionRequestV1` unchanged so nested server-owned identities remain forbidden.

- [ ] **Step 6: Update strict schemas and verify schema parity**

Update only the Decision outer-envelope branches of `api/schema/mcp-input-v2.json` and `api/schema/ipc-v2.json` so `workspace_id` is optional beside `decision`. Do not add it to nested Decision schemas.

Run:

`go test ./internal/adapter/mcp ./internal/adapter/ipc ./tests/contract -count=1`

Expected PASS.

- [ ] **Step 7: Run dirty selection and commit**

Run:

`go run ./tools/devctl test --dirty --base origin/main --json`

Then stage only Task 1 files and commit:

`git -c core.hooksPath=.githooks commit -m "fix: route decision workspace selector through transport"`

---

### Task 2: Resolve Decision workspace authoritatively and derive repository identity server-side

**Files:**
- Modify: `cmd/shellbeam/decision_protocol.go`
- Test: `cmd/shellbeam/decision_protocol_test.go`
- Modify: `internal/core/failure/failure.go` (introduce only `decision_context_unavailable`; reuse existing `workspace_not_found`)
- Test: `internal/core/failure/failure_test.go`
- Modify: Decision IPC action interface only as needed to carry outer workspace selector to daemon actions without placing it inside `DecisionRequestV1`
- Test: related IPC daemon-action bridge test when the interface shape changes

**Interfaces:**
- Consumes: optional outer `workspace_id`; registered workspace list/inspect service.
- Produces: authoritative `workspace.Workspace`; repository identity always read from that server-owned record.

- [ ] **Step 1: Write RED context-failure and resolver tests**

First add a RED `internal/core/failure` test requiring `DecisionContextUnavailable Code = "decision_context_unavailable"` with no arbitrary public details. Then replace the singleton-only runtime test with a matrix that calls:

`resolveDecisionWorkspace(ctx, selector, workspaces)`.

Required cases:

- two registered workspaces A/B + selector A → A;
- two registered workspaces A/B + selector B → B;
- two registered workspaces + empty selector → typed/public `decision_context_unavailable` at the Decision boundary;
- zero registered + empty selector → `decision_context_unavailable`;
- explicit unknown selector → `workspace_not_found` with only safe `workspace_id` detail;
- singleton + empty selector → singleton unchanged;
- invalid selector never reaches registry inspection.

Add a dispatch test that supplies selector A while the nested policy snapshot has no repository field, then assert `PutPolicySnapshotRequest.RepositoryID == workspaceA.RepositoryID`.

Run:

`go test ./cmd/shellbeam -run 'DecisionProtocol.*Workspace|DecisionProtocol.*Repository' -count=1`

Expected RED because current helper has no selector and requires exactly one listed workspace.

- [ ] **Step 2: Implement the context failure code and selector-aware authoritative resolution**

Add only `DecisionContextUnavailable` to `internal/core/failure` in this task; the semantic Decision failure codes remain Task 3. Use this resolver shape:

`func resolveDecisionWorkspace(ctx context.Context, selector string, workspaces decisionWorkspaceList) (workspacecore.Workspace, error)`.

Algorithm:

1. nil workspace dependency → Decision context unavailable failure;
2. selector present → parse ID, call `Inspect(ctx, selector)`, validate returned record, require returned `Workspace.ID == selector`, return it;
3. selector absent → `List(ctx)`; exactly one validated record succeeds; zero or more than one fails closed as Decision context unavailable.

Do not use cwd or any implicit state.

- [ ] **Step 3: Carry selector into every Decision action that needs repository/workspace context**

Change the Decision daemon/IPC action path so the outer selector reaches `decisionProtocolRuntime.Dispatch` separately from `DecisionRequestV1`. Do not put it inside `DecisionRequestV1`.

At minimum, `decision.policy.snapshot`, `decision.policy.activate`, and `decision.create` must resolve the selected workspace before service dispatch. Any other Decision action that needs repository/workspace context must use the same resolver rather than infer identity from transport cwd/session state.

- [ ] **Step 4: Verify runtime GREEN and singleton compatibility**

Run:

`go test ./cmd/shellbeam ./internal/adapter/ipc -run 'DecisionProtocol' -count=1`

Expected PASS, including existing singleton tests.

- [ ] **Step 5: Run dirty selection and commit**

Run:

`go run ./tools/devctl test --dirty --base origin/main --json`

Commit staged Task 2 files:

`git -c core.hooksPath=.githooks commit -m "fix: resolve decision workspace selector authoritatively"`

---

### Task 3: Project stable Decision lookup and protocol-rejection failures

**Files:**
- Create or modify: `internal/app/decisionprotocol/errors.go`
- Modify: Decision application files that currently return generic unavailable lookup errors
- Test: focused Decision application tests for episode/candidate/experiment lookup typing
- Modify: `internal/core/failure/failure.go`
- Test: `internal/core/failure/failure_test.go`
- Modify: `cmd/shellbeam/decision_protocol.go`
- Test: `cmd/shellbeam/decision_protocol_test.go`

**Interfaces:**
- Consumes: typed Decision application lookup errors and core `decisionprotocol.ReasonError`.
- Produces stable public failures: `decision_episode_not_found`, `decision_candidate_not_found`, `decision_experiment_not_found`, `decision_protocol_rejected` with safe `reason` detail.

- [ ] **Step 1: Write RED public failure tests**

`DecisionContextUnavailable` already exists from Task 2. Add constants and public specs expectations for the remaining semantic failures:

`DecisionEpisodeNotFound Code = "decision_episode_not_found"`

`DecisionCandidateNotFound Code = "decision_candidate_not_found"`

`DecisionExperimentNotFound Code = "decision_experiment_not_found"`

`DecisionProtocolRejected Code = "decision_protocol_rejected"`.

Expected safe details:

- episode/candidate/experiment not found: no entity IDs required;
- protocol rejected: only `reason`.

Run:

`go test ./internal/core/failure -run 'Decision' -count=1`

Expected RED because these codes do not exist.

- [ ] **Step 2: Add typed application lookup errors before changing boundary projection**

Define stable application sentinels or typed errors for episode, candidate, and experiment not found in `internal/app/decisionprotocol/errors.go`, with `errors.Is` support.

Replace generic strings such as `"decision episode unavailable"`, `"assessment candidate unavailable in episode"`, and experiment-not-found equivalents only where they semantically mean the requested Decision entity does not exist. Keep dependency-unavailable/internal corruption errors generic so they still project to `internal`.

Add focused application tests proving the three entity classes are distinguishable without parsing error text.

- [ ] **Step 3: Add the Decision boundary projector**

Add one helper in `cmd/shellbeam/decision_protocol.go`:

`func projectDecisionProtocolError(err error) error`.

Projection order:

1. existing `*failure.Failure` passes through unchanged;
2. application episode/candidate/experiment not-found types → matching public code;
3. `decisioncore.AsReasonError(err)` → `failure.DecisionProtocolRejected` with `details["reason"] = string(reason.Reason)`;
4. everything else → unchanged error, which the existing failure boundary safely normalizes to `internal`.

Apply this helper exactly once at the Decision daemon boundary so every Decision action gets consistent projection.

- [ ] **Step 4: Write/verify RED→GREEN semantic failure tests**

Add runtime tests:

- nonexistent episode → `decision_episode_not_found`, not `internal`;
- nonexistent candidate → `decision_candidate_not_found`;
- nonexistent experiment → `decision_experiment_not_found`;
- a representative stale projection/policy `ReasonError` → `decision_protocol_rejected` with exact original stable reason code;
- an invalid transition/conflict `ReasonError` gets the same public code with its own reason code;
- arbitrary internal text/entity IDs do not enter public details.

Run:

`go test ./internal/app/decisionprotocol ./internal/core/failure ./cmd/shellbeam -run 'Decision|Reason|NotFound' -count=1`

Expected PASS.

- [ ] **Step 5: Run dirty selection and commit**

Run:

`go run ./tools/devctl test --dirty --base origin/main --json`

Commit staged Task 3 files:

`git -c core.hooksPath=.githooks commit -m "fix: expose stable decision protocol failures"`

---

### Task 4: Prove multi-workspace routing end to end without touching the production daemon

**Files:**
- Test: existing/new integration test under `cmd/shellbeam` or `internal/adapter/ipc` if it can exercise a real isolated daemon cheaply
- Create: bounded evidence artifact under `docs/superpowers/evidence/` only if repository conventions require durable probe evidence
- No production-daemon/service-management changes

**Interfaces:**
- Consumes: completed transport selector, authoritative resolver, stable failure projection.
- Produces: realistic isolated acceptance evidence for a daemon with 2+ registered workspaces.

- [ ] **Step 1: Add an automated multi-workspace integration test where practical**

The test must create two registered workspaces with distinct repository IDs, issue `decision.policy.snapshot` using outer selector A and then B, and prove the resulting policy snapshots carry repository A/B respectively. A no-selector request in the same daemon must fail `decision_context_unavailable`.

- [ ] **Step 2: Build an isolated probe binary without changing the production runtime owner**

Run source-bound build from this worktree, using a temporary output path and isolated runtime/state directories. Do not install or replace the production binary/service.

Use `mktemp -d` for every runtime/state/socket directory and trap cleanup of only those temporary paths/processes created by this probe.

- [ ] **Step 3: Run the realistic isolated daemon probe**

Start only the temporary daemon instance with isolated state. Register two temporary Git workspaces. Issue Decision MCP/IPC requests against that isolated socket:

1. policy snapshot + selector A succeeds and derives repo A;
2. policy snapshot + selector B succeeds and derives repo B;
3. same call without selector fails `decision_context_unavailable`;
4. unknown selector fails `workspace_not_found`;
5. nested `repository_id` and nested `workspace_id` remain `invalid_input`.

Capture only bounded safe evidence; do not reuse or mutate production daemon state.

- [ ] **Step 4: Final traceability and verification gates**

Run:

`python3 scripts/check-decision-protocol-v1-plan-traceability.py`

`python3 scripts/check-decision-protocol-multi-workspace-routing-amendment.py`

`git diff --check origin/main...HEAD`

`go test ./cmd/shellbeam ./internal/adapter/mcp ./internal/adapter/ipc ./internal/app/decisionprotocol ./internal/core/failure ./tests/contract -count=1`

`go run ./tools/devctl check --base origin/main --json`

Expected: all PASS.

- [ ] **Step 5: Commit final integration evidence/tests**

Stage only Task 4 files and commit:

`git -c core.hooksPath=.githooks commit -m "test: prove decision multi-workspace routing"`

## Plan Completion Gate

Before calling the amendment complete, verify all nine acceptance items in the spec directly. In particular, show evidence that the isolated daemon has at least two simultaneously registered workspaces and that `decision.policy.snapshot` succeeds only when the correct outer selector disambiguates the target. Confirm the production daemon PID/socket/state were never stopped, restarted, or mutated by the probe.
