# ShellBeam E26 Experimental Safety Checkpoints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement E26 Explicit Local Safety Checkpoints as an opt-in, bounded, provider-backed local safety primitive with durable exactly-once create/restore semantics and no leakage of captured content or deterministic content identities into ShellBeam public state.

**Architecture:** Core ShellBeam owns request validation, durable mutation identity, bounded public checkpoint metadata, capability truth, transport mapping, Event Journal integration, and workspace invalidation. A `localfs/v1` experimental provider owns sensitive raw bytes, private content identities, no-follow capture/restore mechanics, provider-private replay ledgers, and retention cleanup under a user-only local store. The feature is disabled by default and never participates in ordinary `start`; provider failure degrades E26 to unavailable without weakening execution/receipt/evidence authority.

**Tech Stack:** Go 1.26.5; existing atomic JSON store; `golang.org/x/sys/unix` no-follow primitives where required; JSON Schema draft 2020-12; existing workspace observer/coherence/Event Journal/IPC/MCP composition. No database, second daemon, second MCP tool, Git mutation helper, or network/upload dependency.

## Global Constraints

- Work only in the existing linked worktree `/Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/design_agent-execution-layer` on `ai/execution-observation`; do not create another worktree/branch, rebase, reset, stash, push, merge, or open a PR.
- Main agent only. Preserve unrelated dirty bytes; investigate unexpected concurrent edits before staging.
- E26 remains experimental and optional. `experimental_checkpoints` defaults to `false`; disabled/failed provider composition leaves ordinary execution fully available and advertises checkpoint capability unavailable.
- One shipped `shellbeam` binary, one local daemon, one MCP tool (`local_shell`). No provider daemon/service process.
- The approved design locks the create action string as `checkpoint_create`. E26 v1 keeps that contract and uses the same explicit action family: `checkpoint_create`, `checkpoint_restore`, and `checkpoint_inspect`; the stable caller idempotency fields are `checkpoint_create_id` and `restore_id`.
- Create requires explicit `workspace_id`; absolute-cwd-only operations cannot use E26.
- Create selectors are bounded exact repository-relative paths or one terminal subtree suffix `path/**`. Bare `**`, `..`, absolute paths, backslashes, empty segments, and other glob metacharacters are rejected.
- Restore accepts exact repository-relative paths only. No restore glob, force-overwrite flag, or recursive directory-tree restore.
- Initial `localfs/v1` conflict guarantees: `regular_file=best_effort`, `symlink=best_effort`, `absent_to_file=best_effort`, `directory_tree=unsupported`. E26 v1 does not advertise `atomic_conditional_replace`.
- Hard limits: create selectors <= 32; selector <= 1024 bytes and total selector bytes <= 8192; walk entries <= 8192; captured entries <= 2048; regular file <= 8 MiB; checkpoint bytes <= 64 MiB; retained checkpoints <= 64; private provider bytes <= 1 GiB; retention age <= 7 days; restore paths <= 256; public entry refs <= 2048; public excluded/unsupported summaries <= 64.
- Public checkpoint IDs are `chk_` + ULID. `checkpoint_create_id` and `restore_id` use existing operation-ID grammar and are caller-stable idempotency keys.
- Core/public metadata contains no raw captured bytes, symlink text, provider-private content identity, deterministic content hash, absolute workspace root, arbitrary OS error, credential value, or raw private-store path.
- Provider-private content lives under a dedicated user-only root below the configured state dir, verified 0700 directories / 0600 files, no symlink traversal, and random opaque entry/blob refs rather than public content-addressed names.
- `.git` internals, nested submodule boundaries, ShellBeam state/runtime roots, sockets/devices/FIFOs, and explicit high-risk policy paths fail closed or are excluded exactly as specified. Arbitrary selected source may still contain secrets, so containment is the security boundary.
- Provider capture/restore is idempotent by core-assigned `checkpoint_id` / caller `restore_id`; retries may resume provider-private work but cannot create a second externally visible checkpoint or re-apply finalized path mutations.
- Restore establishes provider-private expected-current observations inside the single restore request. The agent never supplies hashes, preimages, or native atomicity tokens.
- Any path that cannot satisfy complete create causes create failure; never truncate and label complete.
- Multi-path restore returns durable per-path truth. Global success is impossible when any requested path is conflict/unsupported/failed.
- Restore mutations invalidate/re-observe ordinary workspace source generation/evidence machinery; E26 never declares Git clean or old evidence valid.
- Checkpoint raw/private content never enters repro, telemetry, evidence, Event Journal summaries, operation receipts, ordinary logs, package/export artifacts, or MCP text/structured output.
- Retention cleanup is lazy/bounded on explicit E26 actions, not a background timer and not ordinary-start work. A checkpoint with an in-progress durable restore is pinned.
- Native macOS E26 filesystem acceptance is required on this host. Linux cross-compile is compile evidence only; Linux native runtime is `NOT_RUN` unless actually executed on Linux.
- E27 Dynamic Input Tracing is out of scope and gets a separate plan after E26 reaches experimental-ready.
- Each implementation task follows RED -> minimal GREEN -> focused/race verification -> review -> commit. Final completion requires one exact source fingerprint across checkpoint/devctl/commit-gate/postcommit proof.

---

### Task 1: Core checkpoint contracts, failures, capability vocabulary, and budgets

**Files:**
- Create: `internal/core/checkpoint/types.go`
- Create: `internal/core/checkpoint/validation.go`
- Create: `internal/core/checkpoint/fingerprint.go`
- Test: `internal/core/checkpoint/types_test.go`
- Test: `internal/core/checkpoint/validation_test.go`
- Modify: `internal/core/failure/failure.go`
- Test: `internal/core/failure/checkpoint_test.go`
- Modify: `internal/core/capability/catalog.go`
- Create: `internal/core/capability/checkpoints.go`
- Test: `internal/core/capability/checkpoints_test.go`

**Interfaces:** `internal/core/checkpoint` defines schema version 1, the hard limits above, `CaptureQuality`, `RetentionState`, `ConflictGuarantee`, `ProviderIdentity`, `ConflictDetection`, `CreateRequest`, `RestoreRequest`, `Checkpoint`, `RestorePathResult`, and `RestoreResult`.

Required semantics:
- `CreateRequest.Validate/Normalize/Fingerprint` uses normalized sorted selectors; duplicate input is rejected rather than silently collapsed.
- `RestoreRequest.Validate/Normalize/Fingerprint` accepts exact paths only, sorts for fingerprint identity, rejects duplicate input.
- Public `Checkpoint.Validate` accepts only safe opaque refs and bounded summaries; it has no field that can carry raw bytes/private hashes/absolute roots.
- `RestoreResult.Validate` requires exact path uniqueness and `Complete=true` only when every requested path is `restored` or `noop`.
- `FeatureSafetyCheckpoints Feature = "safety_checkpoints"`.
- `CheckpointSupport` exposes schema versions, provider identity, exact conflict matrix, and `local_sensitive_content=true`.
- `Limits` gains explicit E26 limit fields; `Baseline` marks E26 unavailable; `Clone` deep-copies checkpoint support; `WithSafetyCheckpoints` advertises only a valid v1 provider/limit matrix.
- Reserve exactly the approved E26 failure codes: `checkpoint_provider_unavailable`, `checkpoint_create_conflict`, `checkpoint_scope_invalid`, `checkpoint_scope_too_large`, `checkpoint_path_unsupported`, `checkpoint_submodule_boundary_unsupported`, `checkpoint_budget_exceeded`, `checkpoint_not_found`, `checkpoint_expired`, `checkpoint_restore_request_conflict`, `checkpoint_restore_conflict`, `checkpoint_restore_partial`, `checkpoint_restore_failed`.

- [ ] **Step 1: Write RED checkpoint validation tests** covering IDs, selector grammar, exact restore paths, duplicates, escape/absolute/backslash/metacharacter rejection, bare `**`, budgets, retention/capture states, opaque refs, and partial restore completeness.
- [ ] **Step 2: Write RED capability/failure tests** for baseline unavailable, exact composed projection/deep clone/limits, and all stable failure codes with safe public detail vocabularies.
- [ ] **Step 3: Run RED**: `go test ./internal/core/checkpoint ./internal/core/capability ./internal/core/failure -run 'Checkpoint|SafetyCheckpoint' -count=1`. Expected: compile/test failure because E26 types/vocabulary do not exist.
- [ ] **Step 4: Implement the minimal core contracts** with deterministic JSON fingerprints and no provider/filesystem behavior.
- [ ] **Step 5: Run GREEN/race**: `go test ./internal/core/checkpoint ./internal/core/capability ./internal/core/failure -count=1`; `go test -race ./internal/core/checkpoint ./internal/core/capability ./internal/core/failure -count=1`; `git diff --check`.
- [ ] **Step 6: Commit** `feat: define experimental checkpoint contracts`.

---

### Task 2: Durable create/restore claims and public checkpoint metadata

**Files:**
- Create: `internal/app/checkpoint/ports.go`
- Create: `internal/adapter/store/checkpoint_types.go`
- Create: `internal/adapter/store/checkpoint_paths.go`
- Create: `internal/adapter/store/checkpoint_create.go`
- Create: `internal/adapter/store/checkpoint_restore.go`
- Test: `internal/adapter/store/checkpoint_test.go`
- Test: `internal/adapter/store/checkpoint_fault_test.go`
- Modify: `internal/adapter/store/repository.go`

**Interfaces:** `internal/app/checkpoint.Repository` supports reserve/find/complete create by `checkpoint_create_id`, bind frozen source generation once, load/list public checkpoint metadata, reserve/load restore by `restore_id`, append-once deterministic per-path restore outcomes, complete restore result, and retention-state transition.

Durable core layout:
```text
checkpoints/v1/create/<checkpoint_create_id>.json
checkpoints/v1/by-id/<checkpoint_id>.json
checkpoints/v1/restore/<restore_id>/reservation.json
checkpoints/v1/restore/<restore_id>/paths/<ordinal>.json
checkpoints/v1/restore/<restore_id>/result.json
```
No file in this tree may contain raw captured bytes, symlink text, private hashes, absolute workspace root, or provider-private path.

- [ ] **Step 1: Write RED create-claim tests** proving one immutable create binding, exact replay, conflict-before-recapture, source-generation bind-once, immutable public metadata except retention, strict unknown/trailing/corrupt JSON rejection.
- [ ] **Step 2: Write RED restore tests** proving one immutable restore binding, exact replay, conflict-before-mutation, append-once per-path outcomes, deterministic ordinal/path binding, and valid partial/global completion semantics.
- [ ] **Step 3: Add RED fault tests** around claim create, source bind, public metadata publish, per-path publish, and final result rename; reopen repository after each injected atomic-writer fault and prove no duplicate/rollback.
- [ ] **Step 4: Run RED**: `go test ./internal/adapter/store -run 'Checkpoint' -count=1`.
- [ ] **Step 5: Implement using existing atomic writer/strict read patterns**, verified 0700 directories, 0600 JSON, bounded serialized sizes, no background goroutine.
- [ ] **Step 6: Run GREEN/race/regressions**: `go test ./internal/adapter/store -run 'Checkpoint|Reservation|MutationScope|Repro' -count=1`; `go test -race ./internal/adapter/store -run 'Checkpoint' -count=1`; `git diff --check`.
- [ ] **Step 7: Commit** `feat: persist checkpoint mutation claims`.

---

### Task 3: Application create orchestration and frozen workspace/provider binding

**Files:**
- Create: `internal/app/checkpoint/service.go`
- Create: `internal/app/checkpoint/create.go`
- Test: `internal/app/checkpoint/service_test.go`
- Test: `internal/app/checkpoint/create_test.go`

**Interfaces:**
```go
type WorkspaceContext struct {
    WorkspaceID string
    RepositoryID string
    Root string
    SourceGeneration string
}
type WorkspaceSource interface {
    ResolveFresh(context.Context, string) (WorkspaceContext, error)
    InvalidateAfterMutation(context.Context, string) error
}
type Provider interface {
    Identity() core.ProviderIdentity
    ConflictDetection() core.ConflictDetection
    Capture(context.Context, CaptureRequest) (CaptureResult, error)
    Restore(context.Context, ProviderRestoreRequest) (ProviderRestoreResult, error)
    Inspect(context.Context, string) (ProviderCheckpointStatus, error)
    Sweep(context.Context, SweepRequest) (SweepResult, error)
}
func New(repository Repository, workspace WorkspaceSource, provider Provider) *Service
func (s *Service) Create(context.Context, core.CreateRequest) (core.Checkpoint, error)
func (s *Service) Inspect(context.Context, string) (core.Checkpoint, error)
func (s *Service) Restore(context.Context, core.RestoreRequest) (core.RestoreResult, error)
```

Create ordering is normative: `validate -> replay existing create_id before fresh observation/provider work -> freeze provider/workspace/paths in durable reservation -> fresh workspace observation -> bind source generation -> provider Capture -> validate bounded public result -> complete public checkpoint metadata`.

- [ ] **Step 1: Write RED ordering/replay tests** proving exact retry happens before workspace/provider calls, root movement cannot recapture, conflicting create ID never observes/captures, provider identity is frozen, unavailable provider maps safely, and fresh source generation is mandatory before capture.
- [ ] **Step 2: Write RED failure/recovery tests** proving capture failure leaves durable same-ID recovery state, invalid/oversized provider result never publishes complete metadata, and retry never allocates a new checkpoint ID.
- [ ] **Step 3: Run RED**: `go test ./internal/app/checkpoint -run 'Create|Replay|Provider' -count=1`.
- [ ] **Step 4: Implement minimal create orchestration**. Generate `chk_` + ULID only when a durable create reservation wins.
- [ ] **Step 5: Run GREEN/race**: `go test ./internal/app/checkpoint -count=1`; `go test -race ./internal/app/checkpoint -count=1`; `git diff --check`.
- [ ] **Step 6: Commit** `feat: coordinate checkpoint creation`.

---
### Task 4: `localfs/v1` sensitive capture provider and bounded path expansion

**Files:**
- Create: `internal/adapter/checkpoint/localfs/provider_unix.go`
- Create: `internal/adapter/checkpoint/localfs/private_state_unix.go`
- Create: `internal/adapter/checkpoint/localfs/selection.go`
- Create: `internal/adapter/checkpoint/localfs/capture.go`
- Create: `internal/adapter/checkpoint/localfs/private_types.go`
- Test: `internal/adapter/checkpoint/localfs/provider_test.go`
- Test: `internal/adapter/checkpoint/localfs/selection_test.go`
- Test: `internal/adapter/checkpoint/localfs/capture_test.go`
- Test: `internal/adapter/checkpoint/localfs/privacy_test.go`

**Provider-private layout:**
```text
<state-dir>/checkpoint-content/v1/                0700
  checkpoints/<checkpoint_id>/                    0700
    manifest.json                                 0600, provider-private
    entries/<opaque_entry_ref>.bin                0600, regular-file bytes
    symlinks/<opaque_entry_ref>.json              0600, raw link text private
    absent/<opaque_entry_ref>.json                0600
    .capture-complete                             0600
  restores/<restore_id>/...                       Task 5
```

Provider-private manifest may use SHA-256/HMAC for internal comparison, but those identities never become public refs/filenames/API values.

- [ ] **Step 1: Write RED private-root tests** for 0700/0600 ownership/mode, parent/file symlink rejection, non-directory root, `O_NOFOLLOW` read/write, corrupt manifest fail-closed, and restart reopen.
- [ ] **Step 2: Write RED selection tests** for exact file/absent/symlink, `prefix/**`, deterministic sorting, no symlink following, `.git`/state/runtime exclusion, nested submodule boundary rejection, special files, selector/walk/file/entry/total limits, and bare-whole-workspace rejection.
- [ ] **Step 3: Write RED capture replay tests** proving same checkpoint ID resumes/replays private capture without new opaque refs for finalized entries; partial pending state resumes; complete manifest is immutable; changed root/intent fails closed.
- [ ] **Step 4: Write RED privacy tests** with low-entropy secret and its ordinary SHA-256; assert raw value/hash/private identity/raw symlink text/absolute root are absent from `CaptureResult` and public checkpoint JSON.
- [ ] **Step 5: Run RED**: `go test ./internal/adapter/checkpoint/localfs -run 'Private|Select|Capture|Privacy' -count=1`.
- [ ] **Step 6: Implement minimal provider** using no-follow descriptors and random opaque refs. No content dedup, whole-workspace scanning, Git subprocess, network, or automatic secret classifier.
- [ ] **Step 7: Run GREEN/race/native repetition**: `go test ./internal/adapter/checkpoint/localfs -count=3`; `go test -race ./internal/adapter/checkpoint/localfs -count=1`; `git diff --check`.
- [ ] **Step 8: Commit** `feat: capture private safety checkpoints`.

---

### Task 5: Provider-private restore ledger and durable partial restore truth

**Files:**
- Create: `internal/adapter/checkpoint/localfs/observe.go`
- Create: `internal/adapter/checkpoint/localfs/restore.go`
- Create: `internal/adapter/checkpoint/localfs/restore_ledger.go`
- Test: `internal/adapter/checkpoint/localfs/restore_test.go`
- Test: `internal/adapter/checkpoint/localfs/restore_race_test.go`
- Create: `internal/app/checkpoint/restore.go`
- Test: `internal/app/checkpoint/restore_test.go`

Provider restore ordering:
`load immutable private manifest -> validate exact requested paths -> durable private restore claim -> capture expected-current observations for all paths -> deterministic path loop: replay finalized result or re-check expected-current immediately before mutation -> conflict/noop/best-effort mutation -> durable private path result -> return all durable results`.

Application restore ordering:
`validate -> replay core restore_id before provider/workspace calls -> load checkpoint/refuse expired -> reserve core restore before mutation -> resolve same registered workspace -> provider Restore -> record every per-path result -> complete restore -> invalidate workspace once if any path actually restored`.

- [ ] **Step 1: Write RED localfs restore tests** for regular bytes+mode, symlink link-text recreation without dereference, captured absent removal under expected-current check, noop, changed-current conflict untouched, unsupported directory-tree/special cases, no force path.
- [ ] **Step 2: Write RED concurrency tests** with hooks around observe/recheck/mutate to show best-effort detects pinned mismatches it can observe but never claims universal writer exclusion.
- [ ] **Step 3: Write RED provider replay/crash tests** for crash after one path, same restore ID resumes only unfinished paths, conflicting intent never mutates, corrupt private ledger fails closed.
- [ ] **Step 4: Write RED app durable replay tests** proving core claim precedes provider mutation, complete retry avoids provider, partial truth persists, conflict avoids provider, expiry/workspace mismatch fail before mutation.
- [ ] **Step 5: Run RED**: `go test ./internal/adapter/checkpoint/localfs ./internal/app/checkpoint -run 'Restore|Conflict|Replay' -count=1`.
- [ ] **Step 6: Implement minimally**. Regular files use same-directory staged 0600 file + fsync + rename after best-effort recheck; restore executable bit. Symlink replacement operates on link itself. Recursive directory-tree mutation remains unsupported.
- [ ] **Step 7: Run GREEN/race**: `go test ./internal/adapter/checkpoint/localfs ./internal/app/checkpoint -count=3`; `go test -race ./internal/adapter/checkpoint/localfs ./internal/app/checkpoint -count=1`; `git diff --check`.
- [ ] **Step 8: Commit** `feat: restore checkpoints conditionally`.

---

### Task 6: Retention, inspect, Event Journal integration, and workspace invalidation

**Files:**
- Create: `internal/adapter/checkpoint/localfs/retention.go`
- Test: `internal/adapter/checkpoint/localfs/retention_test.go`
- Create: `internal/app/checkpoint/inspect.go`
- Create: `internal/app/checkpoint/retention.go`
- Test: `internal/adapter/checkpoint/retention_test.go`
- Modify: `internal/core/observation/event.go`
- Modify: `internal/core/observation/event_test.go`
- Create: `internal/adapter/store/checkpoint_events.go`
- Test: `internal/adapter/store/checkpoint_events_test.go`
- Create: `cmd/shellbeam/checkpoint_workspace.go`
- Test: `cmd/shellbeam/checkpoint_workspace_test.go`

Event kinds: `checkpoint_created`, `checkpoint_restore_started`, `checkpoint_restore_completed`, `checkpoint_expired`. Event subject/summary contains only opaque checkpoint/restore IDs and bounded aggregate status, never path lists/content/private identities.

- [ ] **Step 1: Write RED retention tests** for count/age/private-byte lazy sweep, deterministic oldest-first eviction, expired state, refusal of incomplete restore, active-restore pinning, cleanup failure, and zero background timer/goroutine.
- [ ] **Step 2: Write RED inspect tests** proving ordinary checkpoint inspection reads only public metadata plus provider availability/retention status and never raw content; it expired metadata remains inspectable while restore is refused.
- [ ] **Step 3: Write RED Event Journal tests** for exactly-once created/restore-started/restore-completed/expired events under retry/materializer failure, with checkpoint truth independent of event projection.
- [ ] **Step 4: Write RED workspace invalidation tests**: any `restored` path invalidates once; conflict/noop-only does not fabricate generation change; subsequent fresh observation reports actual generation through existing workspace machinery.
- [ ] **Step 5: Run RED**: `go test ./internal/adapter/checkpoint/localfs ./internal/app/checkpoint ./internal/core/observation ./internal/adapter/store ./cmd/shellbeam -run 'Checkpoint|EventKind|Workspace' -count=1`.
- [ ] **Step 6: Implement lazy retention and event obligations** by reusing existing observation obligation/materialization patterns; event success is never authoritative.
- [ ] **Step 7: Run GREEN/race/journal regressions**: `go test ./internal/app/checkpoint ./internal/adapter/checkpoint/localfs ./internal/core/observation ./internal/adapter/store ./cmd/shellbeam -count=1`; `go test -race ./internal/app/checkpoint ./internal/adapter/checkpoint/localfs ./internal/adapter/store -run 'Checkpoint' -count=1`; `go test ./internal/app/observation ./internal/adapter/store -run 'Event|Observation' -count=1`.
- [ ] **Step 8: Commit** `feat: journal and retain safety checkpoints`.

---

### Task 7: Closed IPC/MCP/schema surface through the single `local_shell`

**Files:**
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Test: `api/schema/checkpoints_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Create: `internal/adapter/ipc/checkpoints.go`
- Test: `internal/adapter/ipc/checkpoints_test.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Test: `internal/adapter/mcp/checkpoints_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `internal/app/bridge/handler_test.go`

**Wire requests:**
```json
{"action":"checkpoint_create","checkpoint_create_id":"cp-create-1","workspace_id":"ws_...","activity_id":"PI-756","paths":["internal/runtime/**","tests/runtime/**"]}
{"action":"checkpoint_restore","restore_id":"restore-1","checkpoint_id":"chk_...","paths":["internal/runtime/file.go"]}
{"action":"checkpoint_inspect","checkpoint_id":"chk_..."}
```

- [ ] **Step 1: Write RED closed-schema tests** for exact action fields, unknown/cross-action rejection, selector/exact-path shapes, no absolute/private/raw content fields, legacy v1 rejection, legacy capability omission.
- [ ] **Step 2: Write RED IPC tests** for typed routing/result, safe failures, zero child spawn, and feature-unavailable when service absent.
- [ ] **Step 3: Write RED MCP tests** for one `local_shell`, structuredContent parity, safe summaries (`checkpoint_create: chk_...`, restore aggregate counts, inspect retention), no raw/private leakage, no second tool.
- [ ] **Step 4: Run RED**: `go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge -run 'Checkpoint|OneTool|Schema' -count=1`.
- [ ] **Step 5: Implement minimal v2 routing/schema mapping** following existing repro/mutation decomposition. No protocol v3; legacy v1 unchanged.
- [ ] **Step 6: Run GREEN/race/schema**: `go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge -count=1`; `go test -race ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge -run 'Checkpoint|OneTool' -count=1`; `git diff --check`.
- [ ] **Step 7: Commit** `feat: expose experimental safety checkpoints`.

---
### Task 8: Opt-in daemon composition, truthful capability, and zero ordinary-start tax

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `api/schema/config-v1.json`
- Create: `cmd/shellbeam/checkpoints.go`
- Test: `cmd/shellbeam/checkpoints_test.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`
- Modify `internal/app/daemon` only if a narrow consumer-owned action port is actually required; prefer `daemonActions` composition and keep ordinary execution service free of E26 behavior.

**Config:** `experimental_checkpoints = false`. No caller-selected provider in v1. Enabled+healthy composes `localfs/v1`; enabled+unsafe/unavailable leaves daemon serving ordinary execution with E26 unavailable.

- [ ] **Step 1: Write RED config tests** for default false, explicit true, strict unknown keys unchanged, config hash change, and no private path in public config.
- [ ] **Step 2: Write RED composition tests** for disabled unavailable/no private-root creation, enabled healthy exact support/limits, enabled provider failure daemon-still-healthy/feature-unavailable.
- [ ] **Step 3: Write RED no-tax instrumentation** proving ordinary `start -> poll -> terminal` with E26 enabled-but-unused performs zero checkpoint provider/repository/retention calls and creates no checkpoint-content root solely due to a shell command.
- [ ] **Step 4: Run RED**: `go test ./internal/config ./cmd/shellbeam ./internal/app/daemon -run 'Checkpoint|NoTax|Config|Capability' -count=1`.
- [ ] **Step 5: Implement opt-in composition** in `cmd/shellbeam`; explicit E26 actions may walk filesystem, ordinary start never does.
- [ ] **Step 6: Run GREEN/race/dirty**: `go test ./internal/config ./cmd/shellbeam ./internal/app/daemon -count=1`; `go test -race ./cmd/shellbeam ./internal/app/daemon -run 'Checkpoint|Start|Poll|NoTax' -count=1`; `go run ./tools/devctl test --dirty --base origin/main --json`; `git diff --check`.
- [ ] **Step 7: Commit** `feat: compose opt-in safety checkpoints`.

---

### Task 9: Native crash/privacy/Git-boundary acceptance and performance proof

**Files:**
- Create: `cmd/shellbeam/checkpoint_acceptance_test.go`
- Create: `tests/integration/checkpoint_test.go`
- Modify test helpers only unless acceptance exposes a production defect; any defect fix requires its own RED proof.

- [ ] **Step 1: Add real-binary create/restore acceptance** with isolated state/runtime/workspace roots and E26 enabled: regular file+mode, symlink, explicit absent path, bounded subtree, mutation, selected exact restore, and fresh workspace generation change.
- [ ] **Step 2: Add exactly-once lost-response/crash acceptance** using deterministic store/provider fault hooks plus daemon reconstruction: one create ID -> one checkpoint/ref set; one restore ID replays durable partial/full truth without reapplying finalized paths.
- [ ] **Step 3: Add conflict/safety matrix** for changed-current conflict, unsupported directory-tree restore, special file, nested submodule, symlink-parent/no-follow, oversize file/total/walk, expired checkpoint, active-restore retention pin.
- [ ] **Step 4: Add Git non-mutation acceptance** snapshotting HEAD/ref, index bytes, stash list, config identity, worktree registration, and identity config before/after E26 operations.
- [ ] **Step 5: Add privacy sentinel acceptance** with low-entropy secret + ordinary SHA-256; search public state, events, MCP output, evidence, telemetry, repro, logs; permit sensitive material only under verified private provider root.
- [ ] **Step 6: Add no-tax/performance acceptance**: E26 enabled-but-unused ordinary admission p95 incremental <= 5 ms / p99 <= 10 ms with zero provider calls; report explicit checkpoint create/restore p50/p95/p99 without inventing threshold.
- [ ] **Step 7: Native platform gates**: macOS filesystem/no-follow/race acceptance natively; Linux `GOOS=linux GOARCH=amd64 go test -exec=true` compile-only; Linux native `NOT_RUN` unless actually run.
- [ ] **Step 8: Fresh verification**: `go test ./cmd/shellbeam ./tests/integration -run 'Checkpoint|E26' -count=3`; `go test -race ./internal/core/checkpoint ./internal/app/checkpoint ./internal/adapter/checkpoint/localfs ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam ./tests/integration -run 'Checkpoint|E26' -count=1`; `go run ./tools/devctl check --json`; `go run ./tools/devctl test --dirty --base origin/main --json`; `git diff --check`.
- [ ] **Step 9: Commit** `test: verify experimental safety checkpoints`.

---

### Task 10: Exact-source E26 experimental-ready checkpoint

**Files:**
- Modify only: `docs/superpowers/plans/2026-08-16-experimental-safety-checkpoints.md`
- Generated evidence: ignored `.build/e26/`

**Frozen final-source note (2026-08-17):** Tasks 1-9 are committed through `e16f158`. Acceptance/exact gating exposed independent issues fixed separately in `4405e12` (wait-view structure), `ca619c7` (checkpoint fresh-observation budget retry), and `a3742ba` (no-tax measurement stability). E27 remains out of scope. Task 10 proof and the exact source fingerprint live only in ignored `.build/e26/final-checkpoint.json`; no post-freeze proof bytes are written back into this tracked plan.

- [x] **Step 1: Confirm Tasks 1-9 committed**, final tracked tree contains only intended plan checkbox/note bytes, no E27 implementation, and historical unchecked plans are not treated as missing E26 work.
- [x] **Step 2: Freeze final tracked plan bytes** before exact gates; note that fingerprint lives only in ignored `.build/e26/final-checkpoint.json` because tracked plan participates in `sourceFingerprint()`.
- [ ] **Step 3: Run exact gates on frozen bytes**: `go mod verify`; `go test ./... -count=1`; `go test -race ./internal/core/checkpoint ./internal/app/checkpoint ./internal/adapter/checkpoint/localfs ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./internal/app/daemon ./cmd/shellbeam ./tests/integration -count=1`; `go run ./tools/devctl check --json`; `go run ./tools/devctl test --dirty --base origin/main --json`; `go test ./cmd/shellbeam ./tests/integration -run 'Checkpoint|E26' -count=1`.
- [ ] **Step 4: Mechanical anti-goal/privacy scans** prove one MCP `AddTool`; no checkpoint call in ordinary start admission; no Git-control mutation in checkpoint packages; no network/upload dependency; no public deterministic content identity; no raw/private content schema fields; no background sweep loop; no `atomic_conditional_replace` advertisement.
- [ ] **Step 5: Record `.build/e26/final-checkpoint.json`** with exact source fingerprint, precommit HEAD, all gates/receipts, macOS native status, Linux compile/native status, ordinary no-tax percentiles, create/restore percentiles, provider matrix, public limits, privacy/Git-boundary status.
- [ ] **Step 6: Stage only final plan**, `git diff --cached --check`, run `go run ./tools/devctl commit-gate --base origin/main --json`, and machine-assert commit-gate fingerprint equals checkpoint fingerprint.
- [ ] **Step 7: Commit** `test: checkpoint experimental safety checkpoints`.
- [ ] **Step 8: Postcommit proof**: rerun `devctl check`, require same fingerprint, update ignored checkpoint with final HEAD/postcommit receipt, and require fully clean `git status --porcelain=v1 --untracked-files=all`.
- [ ] **Step 9: Final report**: worktree/branch/HEAD/chain/fingerprint, actions, provider matrix, hard limits, exactly-once replay, retention, workspace/evidence interaction, privacy containment, Git non-mutation, no-tax, macOS native PASS, Linux native PASS only if actually run else NOT_RUN, `push=NO`, `PR=NO`, `merge=NO`; E27 remains pending separate work.

## Completion Gate

E26 is `experimental-ready` on a platform only when one exact final source tree proves:

1. create/restore identity is durably bound before provider mutation, and lost-response retry cannot create a second checkpoint or re-apply finalized restore paths;
2. bounded scope round-trips supported regular file/mode, symlink, absent path, and subtree entries without implicit whole-workspace capture;
3. public/core/MCP/event/repro/evidence/telemetry state exposes no raw checkpoint content or deterministic content identity for arbitrary bytes;
4. `localfs/v1` reports only exact `best_effort`/`unsupported` path-class guarantees and never generic filesystem atomicity;
5. partial multi-path restore preserves durable per-path truth and `complete=false` whenever any requested path is conflict/unsupported/failed;
6. symlink, special file, submodule boundary, selector escape, budget, corrupt private state, and expiry cases fail closed;
7. E26 does not mutate Git HEAD/branch/index/stash/worktree/config/identity/remotes;
8. provider-private content is local/user-only, excluded from normal export paths, with bounded retention/pinning cleanup;
9. restored source invalidates/re-observes existing workspace generation/evidence state without claiming Git cleanliness/evidence revalidation;
10. default-disabled/unavailable/provider-failure states never block ordinary shell execution, and enabled-but-unused E26 passes p95 <= 5 ms / p99 <= 10 ms incremental admission with zero provider calls;
11. one MCP tool remains, legacy v1 is unchanged, capability projection is truthful, and final full/race/schema/native/privacy/devctl/commit-gate/fingerprint evidence is green;
12. Linux native evidence is `NOT_RUN` unless actually run natively; cross-build evidence is never promoted to runtime PASS.
