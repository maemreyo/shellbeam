# ShellBeam A2.6 Advisory Mutation Scopes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Implement durable TTL-bounded advisory mutation scopes so cooperating agents can declare repository-relative read/mutate intent and receive deterministic overlap warnings without locks, permissions, workflow semantics, or ordinary-command admission tax.

**Architecture:** Add a pure `internal/core/mutationscope` domain for canonical selectors, scope records, mutation receipts, and advisory folding. Persist active scope records plus compact identity/mutation tombstones in the existing private state root through `internal/adapter/store`, orchestrate exactly-once set/release/inspect in `internal/app/mutationscope`, and expose the feature only through new closed branches of the existing single `local_shell` IPC/MCP v2 surface. Scope evaluation is invoked only by explicit A2.6 actions or activity inspection; ordinary `start/poll/write/kill` never reads or evaluates scope state.

**Tech Stack:** Go 1.x repository baseline; existing ShellBeam atomic file store; E21 observation obligations/Event Journal; JSON Schema draft 2020-12; existing Unix IPC + official MCP adapter; no new dependency.

## Global Constraints

- Design authority: `docs/superpowers/specs/2026-08-15-advisory-mutation-scopes-design.md`.
- one MCP tool remains exactly `local_shell`; no second tool, resource, or prompt.
- Mutation scopes are advisory declarations only. They never block, delay, cancel, authorize, serialize, or infer ownership for commands, edits, Git operations, branch/worktree changes, or user actions.
- Scope schema version: `1`.
- Supported modes: exactly `read` and `mutate`.
- Selector v1 supports only `**`, exact normalized repository-relative paths, and terminal `path/**` subtrees. No arbitrary glob syntax, regex, filesystem expansion, symlink resolution, Git query, or provider call participates in matching.
- `path/**` includes `path` itself. `**` overlaps every valid selector.
- Maximum active scopes per activity: `16`; per workspace: `64`; paths per scope: `16`; selector bytes: `256`; advisories per response: `32`.
- Default TTL: `900000 ms`; maximum TTL: `1800000 ms`; `expires_at` is fixed at first successful set for a given `mutation_id` and exact retry never extends it.
- `scope_id` maximum: `128 bytes`; it stays durably bound to one activity/workspace across active, released, and expired states until a future explicit purge design.
- `mutation_id` is the exactly-once mutation identity. Same ID + same fingerprint replays the prior result; same ID + different fingerprint conflicts.
- older set retries never overwrite a newer committed scope revision.
- Expiration is lazy and based on `expires_at <= now`; no watcher/timer/background loop is required. Cleanup failure cannot resurrect an expired scope.
- Release of active scope changes state once. Release of absent/expired scope is exactly-once success with `already_absent=true` and no `mutation_scope_changed` event.
- Successful active-state set/release mutations integrate with E21 through one `mutation_scope_changed` observation obligation. Exact retries and TTL expiry emit no duplicate/timer event.
- Advisories are derived mechanical projections, sorted deterministically and never persisted as independent authority.
- Outside-scope advisories may only consume already-available bounded changed-path facts. A2.6 never triggers hidden Git/workspace sampling solely for scope evaluation.
- Scope state stays separate from activity operation history and process/session authority.
- Stored/public A2.6 data contains no raw command, stdin/output, source content, environment value/hash, credential, arbitrary OS error, or absolute workspace path.
- With A2.6 compiled/composed but unused, ordinary compatible start/poll/terminal performs zero scope-store reads/writes, zero overlap evaluation, zero additional durability barrier, and zero Git/process/provider subprocess solely for A2.6.
- Follow repository structural gates: production file <= 500 lines (warning >350), test file <= 800 (warning >600), function <= 80 lines (warning >60).
- No push, PR, or merge in this plan.

---

## File Structure

Create focused files:

- `internal/core/mutationscope/types.go` — schema constants, limits, mode, scope/receipt/advisory shapes.
- `internal/core/mutationscope/selector.go` — selector normalization, canonical sort, overlap.
- `internal/core/mutationscope/validation.go` — scope/mutation validation and request fingerprint input normalization.
- `internal/app/mutationscope/ports.go` — narrow persistence/observation interfaces.
- `internal/app/mutationscope/service.go` — exactly-once set/release/inspect orchestration.
- `internal/app/mutationscope/evaluator.go` — deterministic bounded advisory fold.
- `internal/adapter/store/mutation_scopes.go` — active/identity/mutation record storage with strict decode and atomic publication.
- `internal/core/capability/mutation_scopes.go` — capability composition for schema/limits.
- `internal/app/daemon/mutation_scope.go` — daemon-facing A2.6 interface/composition only.

Modify existing transport/composition files only where required:

- `internal/core/observation/event.go`
- `internal/adapter/ipc/types.go`, `internal/adapter/ipc/server_unix.go`, relevant bridge/client mapping files
- `internal/adapter/mcp/input.go`, `internal/adapter/mcp/call.go`, relevant output/discovery mapping files
- `api/schema/ipc-v2.json`, `api/schema/mcp-input-v2.json`, `api/schema/mcp-output-v2.json`
- `cmd/shellbeam/command_daemon.go`
- activity-inspection projection files as identified by existing A1 inspect tests; do not expand `internal/core/activity.Activity` with active lease authority.

---

### Task 1: Core selector, record, advisory, and capability contracts

**Files:**
- Create: `internal/core/mutationscope/types.go`
- Create: `internal/core/mutationscope/selector.go`
- Create: `internal/core/mutationscope/validation.go`
- Create: `internal/core/mutationscope/selector_test.go`
- Create: `internal/core/mutationscope/advisory_test.go`
- Modify: `internal/core/failure/failure.go`
- Create: `internal/core/capability/mutation_scopes.go`
- Modify/Test: `internal/core/capability/catalog.go`, `internal/core/capability/catalog_test.go`

**Interfaces:**

```go
func NormalizeSelectors(in []string) ([]string, error)
func SelectorsOverlap(a, b []string) bool
func (s Scope) Validate() error
func (r MutationReceipt) Validate() error
func (c Catalog) WithMutationScopes(maxActivity, maxWorkspace, maxPaths, maxSelectorBytes, maxAdvisories int, defaultTTLMS, maxTTLMS int64) Catalog
```

Produce `Mode`, `Scope`, `ScopeIdentity`, `MutationReceipt`, `Advisory`, `InspectResult`, and hard-limit constants. Invalid/zero capability limits keep the feature unavailable.

- [x] **Step 1: Write selector RED tests.** Cover `**`, exact/exact, exact/subtree both orders, subtree ancestor/equal/disjoint, `path/**` including `path`, canonical sorting, duplicate rejection, and invalid absolute/traversal/backslash/control/empty-segment/malformed wildcard/oversized/invalid-UTF8 inputs.
- [x] **Step 2: Run RED.** `go test ./internal/core/mutationscope -run 'Selector|Normalize' -count=1`; expected compile/failing tests because package/API is absent.
- [x] **Step 3: Implement minimum selector core.** Pure string normalization only; reject rather than expand unknown glob syntax. No filesystem imports in selector implementation.
- [x] **Step 4: Write advisory/record RED tests.** Prove mode matrix, deterministic pair ordering, cause fingerprint stability across input order, truncation to 32, record/receipt validation, fixed limits, and safe identifier validation.
- [x] **Step 5: Run RED then implement minimal types/validation/evaluator helpers needed in core.** `go test ./internal/core/mutationscope -count=1` must transition RED -> GREEN.
- [x] **Step 6: Add typed failure/capability RED tests.** Require safe stable A2.6 categories and `FeatureMutationScopes` availability only after valid `WithMutationScopes` composition with exact schema/TTL/count limits.
- [x] **Step 7: Implement capability/failure contract and run GREEN/race.** `go test -race ./internal/core/mutationscope ./internal/core/capability ./internal/core/failure -count=1`.
- [x] **Step 8: Run `go run ./tools/devctl check`, `git diff --check`, stage only Task 1 files, run commit-gate, commit `feat: define advisory mutation scope contracts`.**

---

### Task 2: Durable active scopes, identity tombstones, and exactly-once mutation receipts

**Files:**
- Create: `internal/adapter/store/mutation_scopes.go`
- Create: `internal/adapter/store/mutation_scopes_test.go`
- Modify: `internal/adapter/store/repository.go`
- Test helper/fault seam: reuse existing atomic writer helpers; add only the narrow injection needed to prove durability ambiguity.

**Interfaces:**

```go
func (r *Repository) LoadMutationScopeIdentity(ctx context.Context, scopeID string) (mutationscope.ScopeIdentity, bool, error)
func (r *Repository) LoadMutationScope(ctx context.Context, scopeID string) (mutationscope.Scope, bool, error)
func (r *Repository) ListMutationScopes(ctx context.Context, activityID string, workspaceID workspace.WorkspaceID) ([]mutationscope.Scope, error)
func (r *Repository) LoadMutationReceipt(ctx context.Context, mutationID string) (mutationscope.MutationReceipt, bool, error)
func (r *Repository) CommitMutationScopeSet(ctx context.Context, scope mutationscope.Scope, identity mutationscope.ScopeIdentity, receipt mutationscope.MutationReceipt) app.StoreResult
func (r *Repository) CommitMutationScopeRelease(ctx context.Context, scopeID string, receipt mutationscope.MutationReceipt) app.StoreResult
```

Persist under dedicated private state-root directories, not `activities/` operation history. The store may split observation-aware private helpers from these public methods while preserving the app-facing contract.

- [x] **Step 1: Write strict round-trip/security RED tests.** Active scope, scope identity, compact mutation receipt; unknown fields/trailing JSON/corruption; symlink/collision protection; file/dir permissions; no absolute path/command/env/source fixtures in serialized bytes.
- [x] **Step 2: Write exactly-once store RED tests.** Concurrent duplicate mutation, same ID/different fingerprint, old-receipt replay after newer revision, active capacity counting only unexpired records, and active/identity binding survival across release/expiry.
- [x] **Step 3: Write fault RED tests.** Pre-publication failure => no claimed state; post-rename/ambiguous durability => ambiguity surfaced and retry must load before deciding; cleanup failure cannot affect logical expiration.
- [x] **Step 4: Run RED.** `go test ./internal/adapter/store -run 'MutationScope' -count=1`.
- [x] **Step 5: Implement dedicated directories/mutex and strict atomic persistence.** Extend `store.Limits` only with A2.6 limits needed for test/operator overrides; normalize defaults to spec hard values; do not couple to session capacity ledger.
- [x] **Step 6: Run GREEN/race/stress.** `go test -race ./internal/adapter/store -run 'MutationScope' -count=1`; then concurrent duplicate test `-count=20`.
- [x] **Step 7: Run structural/diff/commit gates and commit `feat: persist advisory mutation scopes safely`.**

---

### Task 3: App service for set, release, expiry, and bounded advisory inspection

**Files:**
- Create: `internal/app/mutationscope/ports.go`
- Create: `internal/app/mutationscope/service.go`
- Create: `internal/app/mutationscope/evaluator.go`
- Create: `internal/app/mutationscope/service_test.go`

**Interfaces:**

```go
func New(store Store, clock Clock) *Service
func (s *Service) Set(ctx context.Context, req SetRequest) (MutationResult, error)
func (s *Service) Release(ctx context.Context, req ReleaseRequest) (MutationResult, error)
func (s *Service) Inspect(ctx context.Context, req InspectRequest) (mutationscope.InspectResult, error)
```

Consume only narrow store/clock ports; observation obligations remain part of durable store publication rather than a second app-side truth. Inject the clock explicitly for TTL boundary tests.

- [x] **Step 1: Write RED request-validation/idempotency tests.** First set, exact replay with same mutation ID, mismatch conflict, replacement with new mutation ID, old retry after replacement, cross-activity/workspace rebinding rejection.
- [x] **Step 2: Write RED TTL/release tests.** Before expiry active; at expiry inactive; expired capacity freed; lazy cleanup failure leaves inactive truth; active release changes once; absent/expired release returns stable `already_absent=true`.
- [x] **Step 3: Write RED advisory tests.** read/read quiet; read/mutate and mutate/mutate overlap advise; disjoint quiet; activity ID never suppresses conflicts; order/cause stable; truncation exact; no persisted advisory requirement.
- [x] **Step 4: Run RED.** `go test ./internal/app/mutationscope -count=1`.
- [x] **Step 5: Implement minimal orchestration/evaluator.** Compute `expires_at` once before first successful mutation publication and persist/replay it; never recompute for retry.
- [x] **Step 6: Run GREEN/race.** `go test -race ./internal/app/mutationscope -count=1`.
- [x] **Step 7: Gate and commit `feat: coordinate advisory mutation scopes`.**

---

### Task 4: E21 `mutation_scope_changed` obligation and exactly-once event semantics

**Files:**
- Modify: `internal/core/observation/event.go`
- Modify/Test: `internal/core/observation/event_test.go`
- Modify: `internal/adapter/store/mutation_scopes.go`
- Test: `internal/adapter/store/mutation_scope_events_test.go`
- Modify app port/service only if needed to pass observation obligation intent to store without making journal materialization authoritative.

**Interfaces:**
- Add closed event kind `mutation_scope_changed`.
- Active-state set/release publication creates one recoverable E21 obligation under the same visibility boundary; no-op release/retry/expiry creates none.

- [x] **Step 1: Write EventKind RED test** requiring the new closed vocabulary member.
- [x] **Step 2: Write store/app RED tests** for one set event, one active release event, zero extra on exact retry, zero on absent release, zero on lazy expiry, and materializer failure preserving committed scope truth.
- [x] **Step 3: Run RED.** `go test ./internal/core/observation ./internal/adapter/store ./internal/app/mutationscope -run 'MutationScope|EventKind' -count=1`.
- [x] **Step 4: Implement minimal observation preparation/commit integration by reusing existing observation obligation machinery.** Do not add a separate journal transaction model.
- [x] **Step 5: Run GREEN/race and existing observation regression suites.** `go test -race ./internal/core/observation ./internal/app/observation ./internal/adapter/store ./internal/app/mutationscope -count=1`.
- [x] **Step 6: Gate and commit `feat: journal mutation scope changes`.**

---

### Task 5: Closed IPC/MCP v2 actions, schemas, and capability discovery

**Files:**
- Modify relevant `internal/adapter/ipc/*.go`; create `internal/adapter/ipc/mutation_scopes_test.go`
- Modify relevant `internal/adapter/mcp/*.go`; create `internal/adapter/mcp/mutation_scopes_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create: `api/schema/mutation_scopes_test.go`
- Modify legacy capability projection only to strip A2.6 additions; never add legacy action branches.

**Interfaces:**
- Modern action names: `mutation_scope.set`, `mutation_scope.release`, `inspect.mutation_scopes`.
- `inspect.activity` modern response may include bounded active scopes/advisories/truncation supplied by daemon composition.

- [x] **Step 1: Write closed-schema RED tests.** Exact required fields; cross-action fields/unknown fields rejected; TTL/mode/ID/selectors/count/bytes represented; invalid extra selectors rejected; legacy generations reject/omit A2.6 fields.
- [x] **Step 2: Write IPC RED tests** for lossless typed routing/results and `feature_unavailable` when A2.6 actions are not composed.
- [x] **Step 3: Write MCP RED tests** for structuredContent/output-schema parity, safe text summary with no paths/command/raw source, capability limits, and exactly one registered `local_shell` tool.
- [x] **Step 4: Run RED.** `go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp -run 'MutationScope|OneTool|Schema' -count=1`.
- [x] **Step 5: Implement v2 transport/schema mappings minimally.** Keep legacy v1 unchanged and strip A2.6 capability fields from legacy projection.
- [x] **Step 6: Run GREEN/race/schema inventory.** `go test -race ./internal/adapter/ipc ./internal/adapter/mcp -count=1`; `go test ./api/schema -count=1`.
- [x] **Step 7: Gate and commit `feat: expose advisory mutation scopes`.**

---

### Task 6: Daemon composition and activity-inspection projection with zero ordinary-path hook

**Files:**
- Create: `internal/app/daemon/mutation_scope.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Create/Test: `cmd/shellbeam/mutation_scopes_test.go`
- Modify activity-inspection transport projection files as needed, without placing lease authority into `core/activity.Activity`.

**Interfaces:**
- Compose one `mutationscope.Service` over the repository and existing E21 store/clock.
- Advertise `FeatureMutationScopes` only when composition succeeds.
- Explicit A2.6 actions call this service; ordinary execution service has no dependency/callback into it.

- [x] **Step 1: Write composition RED test.** Real daemon capability unavailable without service and available with exact limits when real service/store is wired.
- [x] **Step 2: Write activity-inspection RED test.** Modern inspect returns bounded active scopes/advisories/truncation; expired entries omitted; activity history itself remains unchanged.
- [x] **Step 3: Write no-tax RED instrumentation test.** Inject/count scope store/service calls; ordinary `start -> poll -> terminal` while another scope exists must produce exactly zero A2.6 reads/writes/evaluations.
- [x] **Step 4: Run RED.** `go test ./cmd/shellbeam ./internal/app/daemon -run 'MutationScope|NoTax|Activity' -count=1`.
- [x] **Step 5: Implement daemon actions/composition only.** Do not add scope checks to start admission, workspace resolver, process owner, typed commands, evidence, or Git mutation paths.
- [x] **Step 6: Run GREEN/race and ordinary execution regressions.** `go test -race ./internal/app/daemon ./cmd/shellbeam -run 'MutationScope|NoTax|Start|Poll|Activity' -count=1`.
- [x] **Step 7: Gate and commit `feat: compose advisory mutation scope service`.**

---

### Task 7: Real-daemon advisory/non-blocking/restart/privacy acceptance

**Files:**
- Create: `cmd/shellbeam/mutation_scope_acceptance_test.go`
- Create: `tests/integration/mutation_scopes_test.go`
- Modify only test helpers unless acceptance exposes a production defect; any defect fix requires its own RED proof before production edit.

**Interfaces:**
- No new product API; proves final behavior through real daemon/IPC/MCP paths.

- [x] **Step 1: Add real daemon overlap acceptance.** Set two overlapping mutate scopes and prove one deterministic advisory; disjoint/read-read cases remain quiet.
- [x] **Step 2: Add non-blocking Git/shell acceptance.** In a temporary isolated Git repo with active scopes, execute representative file edit, `git switch`, stash, and reset-style operations through ordinary ShellBeam start; prove normal admission/outcome is unaffected by scopes.
- [x] **Step 3: Add restart/TTL acceptance.** Unexpired durable scope survives daemon service reconstruction; expired one is inactive; no process/session authority is reconstructed.
- [x] **Step 4: Add no-hidden-work acceptance.** Fake/guard Git/process/provider executables and prove set/release/inspect spawn none; ordinary start pays no A2.6 call tax.
- [x] **Step 5: Add privacy/adversarial acceptance.** Sentinel absolute path, command-like text, env/secret/source fixtures must not appear in A2.6 durable files, mutation receipts, Event Journal summaries, or MCP safe summaries except canonical data explicitly allowed elsewhere and not copied by A2.6.
- [x] **Step 6: Run focused acceptance x3 and race.** `go test ./cmd/shellbeam ./tests/integration -run 'MutationScope|A26' -count=3`; `go test -race ./internal/core/mutationscope ./internal/app/mutationscope ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/daemon ./cmd/shellbeam ./tests/integration -run 'MutationScope|A26' -count=1`.
- [x] **Step 7: Run anti-goal scan** proving no scope lock/permission/admission gate, command parsing/inference, watcher/timer loop, hidden Git/provider call, or second MCP tool was added.
- [x] **Step 8: Gate and commit `test: verify advisory mutation scopes`.**

---

### Task 8: Final A2.6 checkpoint and exact-source proof

**Files:**
- Modify only this plan to mark completed steps and record checkpoint rationale/evidence location. Keep generated receipts under ignored `.build/`.

**Interfaces:**
- Produces final verified A2.6 commit and exact source fingerprint evidence; no new runtime behavior.

- [x] **Step 1: Confirm every Task 1-7 checkbox is complete, `git status` contains only intended final plan update, and no unrelated historical plan checkbox is treated as A2.6 work.**
- [x] **Step 2: Run fresh exact gates on final source bytes:** `go mod verify`; `go test ./... -count=1`; relevant full race scopes for core/app/store/ipc/mcp/daemon/cmd/integration; `go run ./tools/devctl check`; `go run ./tools/devctl test --dirty --base origin/main --json`.
- [x] **Step 3: Run fresh A2.6 acceptance/capability/one-tool/privacy gates.** Include focused real-daemon acceptance and scoped source scan.
- [x] **Step 4: Record checkpoint evidence under ignored `.build/a26/final-checkpoint.json` with pre-commit source fingerprint, gate statuses, receipt paths, and current HEAD. Do not embed the fingerprint into a tracked file that participates in its own hash.**

  Checkpoint note: the exact source fingerprint is stored only under ignored `.build/a26/final-checkpoint.json` because the tracked plan participates in `sourceFingerprint()`; embedding the digest here would make the proof self-referential.
- [x] **Step 5: Stage only final plan bytes; `git diff --cached --check`; run `go run ./tools/devctl commit-gate --json`; require commit-gate fingerprint to equal the checkpoint fingerprint.**
- [x] **Step 6: Commit `test: checkpoint advisory mutation scopes` through repository hooks.**
- [x] **Step 7: Re-run `devctl check`/fingerprint on post-commit tree and prove it is identical to the staged/checkpoint fingerprint. Record final HEAD in `.build/a26/final-checkpoint.json`.**
- [x] **Step 8: Final `git status --porcelain=v1 --untracked-files=all` must be empty; report A2.6 commit chain, exact fingerprint, limits/actions, no-tax/non-blocking/restart evidence, and `push=NO`, `PR=NO`, `merge=NO`.**

## Stop Conditions

Stop and investigate before proceeding if any of these occur:

- a RED test fails for a reason unrelated to the missing A2.6 behavior;
- any implementation proposal requires ordinary start admission to inspect scopes;
- a scope becomes a lock/permission/ownership authority;
- retry with the same mutation ID can extend TTL or overwrite a newer revision;
- expiry correctness depends on a timer/background goroutine;
- selector evaluation requires filesystem/Git/provider access;
- a persisted/public A2.6 record would contain raw absolute workspace path, command, source, stdin/output, environment value/hash, credential, or arbitrary OS error;
- capability says available without real service composition;
- MCP tool count exceeds one;
- structural/devctl/race/contract gates repeatedly fail without understood root cause.
