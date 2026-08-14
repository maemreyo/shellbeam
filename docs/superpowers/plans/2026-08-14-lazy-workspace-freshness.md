# ShellBeam Lazy Workspace Freshness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace eager per-command workspace observation with O(1) managed-shell cache invalidation plus explicit bounded workspace-delta sampling, while preserving activity baselines and exact-source evidence authority.

**Architecture:** Add a state-root `WorkspaceCoherenceTracker` that marks ShellBeam-known shell lifecycles without claiming filesystem containment, and a separate `WorkspaceDeltaSampler` that samples Git/workspace state only when a consumer requests it. Ordinary receipts become tagged `cached|unreconciled|freshly_sampled` provenance; activity baselines intentionally pay for a bounded pre-edit sample, while test/build/release evidence continues to use `ExactSourceSnapshot` rather than workspace samples.

**Tech Stack:** Go 1.26.5; existing ShellBeam core/app/adapter modular monolith; Git porcelain v2 `-z`; existing file-backed store and JSON Schema contract tests; existing `tools/devctl` verification driver.

## Global Constraints

- Approved design authority: `docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md`, `docs/superpowers/specs/2026-08-14-agent-execution-observation-roadmap-design.md`, and `docs/superpowers/specs/2026-08-14-structured-code-intelligence-design.md` at or after design commit `07d1664796d9b65136fcae5dd3150258c6a89b5c`.
- Start execution from the current `docs/agent-execution-layer-design` worktree. Do not reset, stash, clean, rebase, switch the primary checkout, or overwrite unrelated dirty work. Before each task, inspect `git status --short --branch` and the staged index.
- Use one primary agent unless the user explicitly authorizes subagents.
- TDD is mandatory: focused RED test -> minimal GREEN implementation -> focused regression -> relevant dirty/check gate.
- Ordinary `local_shell start` with no explicit activity baseline, freshness request, or identity preflight must run **zero Git subprocesses for workspace freshness**, start no watcher/provider, and perform only cheap binding plus O(1) coherence transitions.
- `state_root_shell_freshness_epoch` and `active_managed_shell_operations` are managed-shell cache-coherence facts only. They never prove that the filesystem is unchanged or that no external/escaped writer exists.
- `freshly_sampled` workspace facts are selection/advisory observations, not atomic filesystem cuts and not `ExactSourceSnapshot` evidence.
- Keep `cmd/shellbeam` as the sole composition root. App packages own use-case ports; core packages contain pure contracts; adapters own Git/OS effects. Do not create catch-all `utils`, `helpers`, `common`, `shared`, `base`, `misc`, or `models` packages.
- Production files target 150–300 lines, require review above 350, and may not exceed 500. Test files require review above 600 and may not exceed 800. Functions require review above 60 lines and may not exceed 80. Interfaces should normally contain 1–5 methods and may not exceed 8.
- Do not add a generic hook/plugin bus, filesystem watcher, editor/patch API, shadow filesystem, or language-semantic logic to the workspace layer.
- Do not change operation retry identity: workspace observations/coherence remain outside request/execution fingerprints, and replay of an existing `operation_id` must never respawn because workspace state changed.
- Budget exhaustion or malformed sampling must downgrade quality/completeness; it must never return an empty set labeled complete.
- Commit only files belonging to the current task. Before each commit run `git diff --cached --check`, inspect `git diff --cached --name-status`, and never bypass hooks with `--no-verify`.
- Use focused tests first. The repository’s current broad local gates are `go run ./tools/devctl test --dirty --base origin/main`, `go run ./tools/devctl check`, and targeted `go test -race` where concurrency is changed.

---

## File Structure

### Core contracts

- Create `internal/core/workspace/coherence.go` — immutable coherence barrier types and validation.
- Create `internal/core/workspace/delta.go` — selection/sampling vocabulary, `ChangeRecord`, `DeltaSample`, and validation.
- Modify `internal/core/receipt/workspace_provenance.go` — add tagged provenance schema v2 while retaining legacy nested schema v1 readability.
- Modify `internal/core/receipt/workspace_provenance_validation.go` — validate the v1/v2 closed branches.
- Modify `internal/core/activity/baseline.go` — record sample completeness needed to prove or downgrade activity deltas.

### Application layer

- Create `internal/app/workspace/coherence.go` — in-memory `CoherenceTracker` and exactly-once managed-shell lease.
- Create `internal/app/workspace/delta_sampler.go` — bounded sampling orchestration, last-sample cache, managed-overlap/cache-eligibility classification.
- Modify `internal/app/workspace/observer.go` — split cheap binding/cached observation from fresh sampling.
- Modify `internal/app/activity/service.go` and `internal/app/activity/ports.go` — capture path-complete activity baselines through the explicit sampler and expose comparison for E29.
- Create `internal/app/daemon/workspace_coherence.go` — daemon-owned small ports for the tracker/lease.
- Modify `internal/app/daemon/workspace_observer.go`, `workspace_context.go`, and `service.go` — remove eager Git sampling from ordinary start/finalization and attach lazy provenance.

### Git adapter

- Modify `internal/adapter/git/cache.go` — expose a no-subprocess cached-snapshot lookup.
- Modify `internal/adapter/git/snapshot.go` — keep fast snapshot behavior but support cached-only access without refresh.
- Create `internal/adapter/git/delta.go` — explicit path-level porcelain-v2 sampler using `--untracked-files=all` within budget.
- Create `internal/adapter/git/delta_test.go` — dirty/revert/delete/branch/untracked/unborn/detached/sparse/submodule/path cases.

### Composition/contracts

- Modify `cmd/shellbeam/command_daemon.go` — construct one state-root tracker and delta sampler; wire the activity baseline source and daemon ports.
- Modify `api/schema/receipt-v2.json`, `api/schema/ipc-v2.json`, and `api/schema/mcp-output-v2.json` — model workspace provenance explicitly and accept legacy nested provenance schema v1 plus new tagged provenance schema v2.
- Modify existing receipt/activity/schema/daemon tests rather than creating a parallel compatibility layer.

---

### Task 1: Freeze coherence, delta, and lazy-provenance core contracts

**Files:**
- Create: `internal/core/workspace/coherence.go`
- Create: `internal/core/workspace/coherence_test.go`
- Create: `internal/core/workspace/delta.go`
- Create: `internal/core/workspace/delta_test.go`
- Modify: `internal/core/receipt/workspace_provenance.go`
- Modify: `internal/core/receipt/workspace_provenance_validation.go`
- Modify: `internal/core/receipt/workspace_provenance_test.go`
- Modify: `internal/core/activity/baseline.go`
- Modify: `internal/core/activity/baseline_test.go`

**Interfaces:**
- Produces: `workspace.CoherenceBarrier`, `workspace.SelectionBasis`, `workspace.SampleFreshness`, `workspace.SelectionCompleteness`, `workspace.ChangeRecord`, `workspace.DeltaSample`, and `receipt.WorkspaceProvenance` schema v2.
- Consumers: Tasks 2–8 and the E29 plan.

- [ ] **Step 1: Write RED tests for coherence and selection vocabulary**

Add table tests that reject impossible barriers and invalid closed enum values. Use these exact public shapes:

```go
type CoherenceBarrier struct {
    DaemonIncarnation             string `json:"daemon_incarnation"`
    Epoch                         uint64 `json:"state_root_shell_freshness_epoch"`
    ActiveManagedShellOperations int    `json:"active_managed_shell_operations"`
}

type SelectionBasis string
const (
    SelectionWorkspaceDirty SelectionBasis = "workspace_dirty"
    SelectionActivityDelta  SelectionBasis = "activity_delta"
)

type SampleFreshness string
const SampleFreshlySampled SampleFreshness = "freshly_sampled"

type SelectionCompleteness string
const (
    SelectionComplete         SelectionCompleteness = "complete"
    SelectionPartial          SelectionCompleteness = "partial"
    SelectionDiverged         SelectionCompleteness = "diverged"
    SelectionPotentiallyStale SelectionCompleteness = "potentially_stale"
    SelectionUnavailable      SelectionCompleteness = "unavailable"
)
```

Run:

```bash
go test ./internal/core/workspace -run 'Test(CoherenceBarrier|Selection)' -count=1
```

Expected: FAIL because the types/validation do not exist.

- [ ] **Step 2: Implement the minimal core types and validators**

Add `Validate()` methods that reject empty daemon incarnation, negative active counts, unknown enum values, absolute/empty change paths, and malformed rename old/new path pairs. Define the generic transition contract without language-specific fields:

```go
type ChangeRecord struct {
    PathTransition   PathTransition   `json:"path_transition"`
    OldPath          string           `json:"old_path,omitempty"`
    NewPath          string           `json:"new_path,omitempty"`
    SourceTransition SourceTransition `json:"source_transition"`
    VCSTransition    VCSTransition    `json:"vcs_transition"`
    Untracked        bool             `json:"untracked,omitempty"`
    Submodule        bool             `json:"submodule,omitempty"`
    TypeChanged      bool             `json:"type_changed,omitempty"`
}
```

Do **not** add `go_mod_changed`, `config_changed`, or other semantic-provider concepts.

- [ ] **Step 3: Write RED tests for tagged provenance v2 and legacy v1 readability**

The new branch must represent lazy truth without inventing timestamps/generations:

```go
type WorkspaceObservationKind string
const (
    WorkspaceFreshlySampled WorkspaceObservationKind = "freshly_sampled"
    WorkspaceCached         WorkspaceObservationKind = "cached"
    WorkspaceUnreconciled   WorkspaceObservationKind = "unreconciled"
)

type WorkspaceObservationRef struct {
    Kind                   WorkspaceObservationKind       `json:"kind"`
    Generation             string                         `json:"generation,omitempty"`
    Quality                workspace.ObservationQuality   `json:"quality,omitempty"`
    ObservedAt             time.Time                      `json:"observed_at,omitempty"`
    DiagnosticCode         string                         `json:"diagnostic_code,omitempty"`
    ObservationInvalidated bool                           `json:"observation_invalidated,omitempty"`
}
```

Keep the current nested workspace provenance schema v1 fields readable. Add a v2 constructor for new receipts and branch validation on `WorkspaceProvenance.SchemaVersion` rather than deleting legacy fields.

Run:

```bash
go test ./internal/core/receipt -run WorkspaceProvenance -count=1
```

Expected: FAIL until schema v2 validation/constructor exists.

- [ ] **Step 4: Implement workspace provenance schema v2**

New writes use:

```go
WorkspaceProvenance{
    SchemaVersion: 2,
    Binding: WorkspaceBinding{RepositoryID: repoID, WorkspaceID: workspaceID},
    Pre: WorkspaceObservationRef{Kind: WorkspaceCached},
    Post: WorkspaceObservationRef{Kind: WorkspaceUnreconciled, ObservationInvalidated: true},
}
```

Validation rules:

- `freshly_sampled` requires non-zero `ObservedAt`, valid generation when quality is not unavailable, and an allowed observation quality;
- `cached` requires an actual cached observation or explicit unavailable diagnostic; never fabricate a generation;
- `unreconciled` carries no generation/time and cannot claim `ObservedChange=true`;
- `ObservedChange` is legal only when both v2 sides are `freshly_sampled` and have distinct valid generations;
- nested schema v1 continues to validate exactly as before.

Do not bump the outer receipt schema solely for this nested version; the nested object is already independently versioned and optional.

- [ ] **Step 5: Extend activity baseline completeness**

Add:

```go
type Baseline struct {
    // existing fields...
    Completeness workspace.SelectionCompleteness `json:"completeness"`
}
```

`BaselineFrom` maps a complete path sample to `complete`; any truncated/partial/unavailable path sample remains degraded. `Compare` must return `BaselineDiverged=true` when the baseline cannot prove a complete path-level pre-edit set. Do not silently classify a truncated baseline as a complete activity delta.

Run:

```bash
go test ./internal/core/workspace ./internal/core/receipt ./internal/core/activity -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the core contracts**

```bash
git add internal/core/workspace/coherence.go internal/core/workspace/coherence_test.go \
  internal/core/workspace/delta.go internal/core/workspace/delta_test.go \
  internal/core/receipt/workspace_provenance.go internal/core/receipt/workspace_provenance_validation.go \
  internal/core/receipt/workspace_provenance_test.go internal/core/activity/baseline.go internal/core/activity/baseline_test.go
git diff --cached --check
git commit -m "feat: define lazy workspace freshness contracts"
```

---

### Task 2: Implement the state-root coherence tracker and exactly-once lease

**Files:**
- Create: `internal/app/workspace/coherence.go`
- Create: `internal/app/workspace/coherence_test.go`
- Create: `internal/app/daemon/workspace_coherence.go`
- Modify: `internal/app/daemon/service.go:19-80`

**Interfaces:**
- Consumes: `workspace.CoherenceBarrier` from Task 1.
- Produces: `workspaceapp.CoherenceTracker`, `workspaceapp.ManagedShellLease`, and daemon consumer-owned interfaces.

- [ ] **Step 1: Write concurrency RED tests for begin/end/invalidate**

Required concrete API:

```go
type CoherenceTracker struct { /* private state */ }

func NewCoherenceTracker(daemonIncarnation string) *CoherenceTracker
func (t *CoherenceTracker) BeginManagedShell() *ManagedShellLease
func (t *CoherenceTracker) Invalidate(reason string)
func (t *CoherenceTracker) CaptureBarrier() core.CoherenceBarrier

type ManagedShellLease struct { /* tracker + sync.Once */ }
func (l *ManagedShellLease) End()
```

Tests must prove:

1. begin increments epoch and active count;
2. `End()` decrements exactly once and increments epoch exactly once even if called twice/concurrently;
3. `Invalidate()` increments epoch without changing active count;
4. 100 concurrent begin/end pairs finish at active=0 with no race;
5. a new tracker with a new daemon incarnation starts with no inherited active leases.

Run:

```bash
go test -race ./internal/app/workspace -run CoherenceTracker -count=1
```

Expected: FAIL before implementation.

- [ ] **Step 2: Implement tracker with no persistence or background goroutine**

Use one mutex plus `sync.Once` per lease. Do not persist epoch/count and do not emit E21 events. Keep the reason input diagnostic-only or ignored after validation; it must not become an extensible hook dispatch mechanism.

- [ ] **Step 3: Define daemon consumer-owned ports**

In `internal/app/daemon/workspace_coherence.go`:

```go
type ManagedShellLease interface { End() }

type WorkspaceCoherence interface {
    BeginManagedShell() ManagedShellLease
    CaptureBarrier() workspace.CoherenceBarrier
}
```

Keep this interface at two methods. The daemon must not depend on the concrete app/workspace tracker type.

- [ ] **Step 4: Run focused race tests**

```bash
go test -race ./internal/app/workspace ./internal/app/daemon -run 'Coherence|ManagedShell' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/workspace/coherence.go internal/app/workspace/coherence_test.go internal/app/daemon/workspace_coherence.go internal/app/daemon/service.go
git diff --cached --check
git commit -m "feat: track managed shell freshness"
```

---

### Task 3: Split cheap workspace binding/cached reads from explicit fresh observation

**Files:**
- Modify: `internal/adapter/git/cache.go`
- Modify: `internal/adapter/git/cache_test.go`
- Modify: `internal/adapter/git/snapshot.go`
- Modify: `internal/adapter/git/snapshot_test.go`
- Modify: `internal/app/workspace/observer.go`
- Modify: `internal/app/workspace/observer_test.go`
- Modify: `internal/core/workspace/types.go`

**Interfaces:**
- Produces: `workspace.Binding`, `Observer.Bind`, `Observer.ObserveCached`, and adapter `SnapshotCached`.
- Consumers: daemon Task 4 and delta/activity code later.

- [ ] **Step 1: Write RED tests proving cached-only access never spawns Git**

Introduce a cheap binding type:

```go
type Binding struct {
    RepositoryID RepositoryID `json:"repository_id,omitempty"`
    WorkspaceID  WorkspaceID  `json:"workspace_id,omitempty"`
}
```

Observer API after this task:

```go
func (o *Observer) Bind(ctx context.Context, cwd string) Binding
func (o *Observer) ObserveCached(ctx context.Context, cwd string) FastSnapshot
func (o *Observer) ObserveFresh(ctx context.Context, cwd string) FastSnapshot
```

Use a counting fake Git runner/source. Verify `Bind` and `ObserveCached` make zero fresh-snapshot calls when the cache is absent/stale; `ObserveFresh` still performs explicit sampling.

Run:

```bash
go test ./internal/app/workspace ./internal/adapter/git -run 'Cached|Bind' -count=1
```

Expected: FAIL.

- [ ] **Step 2: Add no-subprocess cache peek in the Git adapter**

Add a cache method that returns the stored snapshot and current age without triggering a singleflight refresh. `SnapshotCached` behavior:

- no entry -> `QualityUnavailable`, diagnostic `workspace_cache_empty`;
- entry within TTL -> `QualityCached`;
- aged entry -> `QualityStale` with cache age retained;
- never call `readFreshSnapshot`.

Keep `SnapshotFresh` as the explicit bypass of warm cache.

- [ ] **Step 3: Refactor the workspace observer around binding first**

`Bind` resolves only the most-specific registered workspace from the registry and copies repository/workspace IDs. `ObserveCached` binds, then asks a `CachedSnapshotSource` if available. If the source does not implement cached lookup, return unavailable rather than falling back to a fresh sample.

- [ ] **Step 4: Run focused tests**

```bash
go test ./internal/adapter/git ./internal/app/workspace -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/git/cache.go internal/adapter/git/cache_test.go internal/adapter/git/snapshot.go internal/adapter/git/snapshot_test.go \
  internal/app/workspace/observer.go internal/app/workspace/observer_test.go internal/core/workspace/types.go
git diff --cached --check
git commit -m "feat: separate workspace binding from sampling"
```

---

### Task 4: Remove eager Git sampling from ordinary daemon start/finalization

**Files:**
- Modify: `internal/app/daemon/service.go:113-290`
- Modify: `internal/app/daemon/workspace_observer.go`
- Modify: `internal/app/daemon/workspace_context.go`
- Modify: `internal/app/daemon/activity_tracker.go`
- Modify: `internal/app/daemon/service_test.go`
- Modify: `internal/app/daemon/workspace_provenance_test.go`
- Modify: `internal/app/daemon/workspace_address_test.go`

**Interfaces:**
- Consumes: Task 2 `WorkspaceCoherence`; Task 3 cheap bind/cached observer; Task 1 provenance v2.
- Produces: ordinary-shell hot path with exactly-one lease lifecycle and lazy receipt provenance.

- [ ] **Step 1: Write RED tests that fail if ordinary start calls fresh observation**

Create a fake observer whose `ObserveFresh` panics/counts, and assert a normal `Start`/terminal flow invokes only `Bind`/`ObserveCached`. Verify the terminal receipt contains:

```go
WorkspaceProvenance{
    SchemaVersion: 2,
    Binding:        ...,
    Pre:            WorkspaceObservationRef{Kind: WorkspaceCached /* or Unreconciled */},
    Post:           WorkspaceObservationRef{Kind: WorkspaceUnreconciled, ObservationInvalidated: true},
}
```

Run:

```bash
go test ./internal/app/daemon -run 'Workspace.*(Lazy|Provenance|NoFresh)' -count=1
```

Expected: FAIL under the current eager `captureWorkspace`/`attachWorkspaceProvenance` path.

- [ ] **Step 2: Write RED tests for exactly-one lease across all terminal paths**

Cover:

- owner spawn failure;
- normal exit 0;
- non-zero exit;
- timeout/kill path;
- output capture failure;
- service shutdown of a running session;
- request context cancellation while the child continues.

The fake tracker records begins/ends. Every created operation that reaches the spawn-attempt boundary must have one begin and exactly one matching end; request cancellation must **not** end the lease before the actual managed process lifecycle ends.

- [ ] **Step 3: Integrate the lease before `ProcessOwner.Start`**

Refactor `Start` in this order:

```text
validate/replay
resolve/freeze operation
reserve operation
cheap workspace binding
explicit activity-baseline admission when activity_id is present
BeginManagedShell()
ProcessOwner.Start()
```

Store the lease on `liveSession`. Spawn failure calls `lease.End()` before publishing its terminal receipt. Normal paths call `lease.End()` at the managed terminal/reap boundary before terminal publication. Protect duplicate finalization by the lease’s own `sync.Once`; do not add scattered booleans.

- [ ] **Step 4: Replace eager workspace observation with lazy provenance**

Remove ordinary calls to `Observe`/`ObserveFresh` from `captureWorkspace` and terminal `attachWorkspaceProvenance`. Build pre provenance from cached-only observation when one exists; otherwise use `unreconciled`. Post is unreconciled by default.

For replay + `workspace_hint`, use cached-only context. If no cached observation exists, omit the advisory rather than running Git just to produce it.

- [ ] **Step 5: Decouple activity workspace identity from the old pre-snapshot**

`admitActivity` receives the cheap `workspace.Binding`/resolved request workspace ID. It must not depend on `workspaceObservation.pre` merely to learn a workspace ID.

- [ ] **Step 6: Run race/focused tests**

```bash
go test -race ./internal/app/daemon -count=1
```

Expected: PASS with no extra Git/fresh-observation calls on ordinary start.

- [ ] **Step 7: Commit**

```bash
git add internal/app/daemon/service.go internal/app/daemon/workspace_observer.go \
  internal/app/daemon/workspace_context.go internal/app/daemon/activity_tracker.go \
  internal/app/daemon/service_test.go internal/app/daemon/workspace_provenance_test.go \
  internal/app/daemon/workspace_address_test.go
git diff --cached --check
git commit -m "refactor: make shell workspace provenance lazy"
```

---

### Task 5: Implement bounded Git path-level workspace delta sampling

**Files:**
- Create: `internal/adapter/git/delta.go`
- Create: `internal/adapter/git/delta_test.go`
- Create: `internal/app/workspace/delta_sampler.go`
- Create: `internal/app/workspace/delta_sampler_test.go`
- Modify: `internal/adapter/git/repository.go`

**Interfaces:**
- Consumes: core `DeltaSample`/`ChangeRecord`, `CoherenceBarrier`.
- Produces:

```go
type DeltaSource interface {
    SampleDelta(context.Context, core.Workspace, core.DeltaLimits) core.DeltaSample
}

type DeltaSampler struct { /* registry, source, coherence, bounded last-sample cache */ }
func (s *DeltaSampler) Sample(ctx context.Context, workspaceID core.WorkspaceID, limits core.DeltaLimits) core.DeltaSample
```

- Consumers: Task 6 and E29.

- [ ] **Step 1: Write RED Git fixtures for path-level status**

Use temp repositories and assert exact machine-readable behavior for:

- tracked modification;
- staged-only change;
- untracked file nested inside a new directory;
- deletion;
- rename accepted as delete+add when rename detection is disabled;
- unmerged record;
- submodule record remains opaque;
- detached HEAD;
- unborn repository does not crash/pretend a normal commit SHA;
- huge untracked set exceeding the configured path limit returns `SelectionPartial`, not empty/complete.

Run:

```bash
go test ./internal/adapter/git -run Delta -count=1
```

Expected: FAIL.

- [ ] **Step 2: Implement explicit path-level Git sampling**

Use one bounded command shaped as:

```text
git --no-optional-locks -C <root> status --porcelain=v2 -z --branch --no-renames --untracked-files=all
```

Parse NUL-delimited records directly. Never run a recursive filesystem walk to infer Git state. Treat rename recognition as optional; delete+add is correctness-complete for downstream synchronization.

The adapter returns `SampleFreshness=freshly_sampled`, the observed HEAD/ref/detached/unborn state, current dirty path records, completeness, diagnostic code, and bytes/record accounting. If command/output/path budgets are exceeded, keep the bounded prefix only when the contract marks it `partial`; never label it complete.

- [ ] **Step 3: Write RED app tests for dirty->clean and clean->clean source-view transitions**

`DeltaSampler` keeps only a bounded last **sample summary**, not a whole-repository content mirror. Test:

```text
sample 1: foo.go modified
sample 2: git restore foo.go -> current dirty set empty
result 2: emits enough transition/state for downstream consumers to know a previously observed dirty path resolved
```

Also test clean branch A -> clean branch B marks `SourceViewMayHaveChanged=true`; a commit with unchanged worktree bytes records VCS movement without claiming a source-byte transition.

- [ ] **Step 4: Implement managed barrier/cache eligibility**

Capture the coherence barrier before and after sampling. A sample remains usable if active managed shells exist, but set `CacheEligible=false`. If the epoch changes during the sample, set completeness to `potentially_stale` or retry once only when the caller explicitly asks for complete selection and budget remains.

The app cache is bounded by workspace count and stores only sample metadata/changed-path records up to the configured limits.

- [ ] **Step 5: Add mode-quality seams without O(repo) hot scans**

Represent Git-visible selection separately from semantic completeness. Do not run `git status --ignored` or `git ls-files -v` on every sample. Add fields that allow future readiness probes to report sparse/assume-unchanged quality; when a known mode makes selection incomplete, downgrade honestly. Unknown ignored/generated semantic relevance remains a provider concern.

- [ ] **Step 6: Run focused tests**

```bash
go test -race ./internal/adapter/git ./internal/app/workspace ./internal/core/workspace -run 'Delta|Selection|Coherence' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/git/delta.go internal/adapter/git/delta_test.go internal/adapter/git/repository.go \
  internal/app/workspace/delta_sampler.go internal/app/workspace/delta_sampler_test.go
git diff --cached --check
git commit -m "feat: sample workspace deltas lazily"
```

---

### Task 6: Wire complete activity baselines and activity-scoped selection

**Files:**
- Modify: `internal/app/activity/ports.go`
- Modify: `internal/app/activity/service.go`
- Modify: `internal/app/activity/service_test.go`
- Create: `internal/app/activity/workspace_baseline.go`
- Create: `internal/app/activity/workspace_baseline_test.go`
- Modify: `cmd/shellbeam/command_daemon.go:42-55`

**Interfaces:**
- Consumes: `workspaceapp.DeltaSampler` from Task 5.
- Produces:

```go
type WorkspaceSampleSource interface {
    Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample
}

func (s *Service) CompareWorkspace(ctx context.Context, rawID string, sample workspace.DeltaSample) (core.Comparison, error)
```

- E29 uses `CompareWorkspace` to obtain activity-scoped candidate paths.

- [ ] **Step 1: Write RED tests for first-operation pre-edit baseline**

Prove that first admission of `activity_id` in a workspace calls the explicit sampler **before** `BeginManagedShell`/spawn, captures individual path facts and completeness, and later operations reuse the immutable activity baseline instead of recapturing it.

Test a baseline with 257 dirty paths against `MaxBaselinePathFacts=256`: the baseline must be partial/truncated and later comparison must not claim a complete activity delta.

- [ ] **Step 2: Implement `workspace_baseline.go` adapter inside the activity app**

Convert generic `workspace.ChangeRecord`/sample state into existing `activity.PathFact` values. Preserve only path-level facts needed for inherited/observed/resolved classification. Do not copy provider semantics into activity state.

- [ ] **Step 3: Add `CompareWorkspace`**

Map a new `workspace.DeltaSample` into `activity.Observation`, call the existing pure `core.Compare`, and return:

- inherited dirty paths;
- observed since baseline;
- resolved since baseline;
- divergence reason.

If the sample or baseline is partial/unavailable, set divergence/degraded quality rather than silently falling back to whole-workspace dirty selection.

- [ ] **Step 4: Wire the baseline source in the composition root**

Replace the current:

```go
activitySvc := activityapp.New(store, nil, activitycore.MaxOperationHistory)
```

with a real bounded baseline source backed by the shared delta sampler. Reuse the same state-root `CoherenceTracker` instance that the daemon uses.

- [ ] **Step 5: Run focused activity/daemon tests**

```bash
go test -race ./internal/core/activity ./internal/app/activity ./internal/app/daemon ./cmd/shellbeam -run 'Activity|Baseline|Workspace' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/activity/ports.go internal/app/activity/service.go internal/app/activity/service_test.go \
  internal/app/activity/workspace_baseline.go internal/app/activity/workspace_baseline_test.go \
  cmd/shellbeam/command_daemon.go
git diff --cached --check
git commit -m "feat: bind activity baselines to lazy samples"
```

---

### Task 7: Update wire schemas and backward-compatibility fixtures

**Files:**
- Modify: `api/schema/receipt-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/a1_inspect_test.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/mcp/a1_inspect_test.go`
- Modify: `internal/core/receipt/receipt_test.go`
- Modify: `tests/contract/acceptance_test.go`

**Interfaces:**
- Consumes: provenance schema v2 from Task 1.
- Produces: closed JSON wire contract accepting persisted provenance v1 and emitting v2 for new ordinary shell receipts.

- [ ] **Step 1: Add RED schema fixtures**

Fixtures must prove:

1. old receipt/provenance v1 JSON still validates/decodes;
2. new v2 `binding/pre/post` form validates;
3. `unreconciled` carrying generation/timestamp is rejected;
4. `freshly_sampled` missing observation metadata is rejected;
5. unknown provenance fields/unknown observation kind are rejected by the checked-in schemas.

Run:

```bash
go test ./api/schema ./internal/core/receipt -run 'Workspace|Provenance|Schema' -count=1
```

Expected: FAIL until schemas are updated.

- [ ] **Step 2: Update the checked-in schemas as a closed `oneOf`**

Do not loosen `additionalProperties`. First add the optional `workspace_provenance` field to the checked-in receipt-v2/IPC receipt definitions so the schema matches the existing Go receipt surface, then model nested `workspace_provenance.schema_version` 1 and 2 as separate closed branches. Keep outer receipt/protocol v2 compatibility unchanged.

- [ ] **Step 3: Update IPC/MCP golden tests**

A normal completed shell response must show `post.kind=unreconciled` and must not require Git-derived `post_generation`. An explicit freshness-producing test fixture may show `freshly_sampled` only when that path actually sampled.

- [ ] **Step 4: Run contract tests**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./tests/contract ./internal/core/receipt -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/schema/receipt-v2.json api/schema/ipc-v2.json api/schema/mcp-output-v2.json api/schema/a1_inspect_test.go \
  internal/adapter/ipc/protocol_v2_test.go internal/adapter/mcp/a1_inspect_test.go \
  internal/core/receipt/receipt_test.go tests/contract/acceptance_test.go
git diff --cached --check
git commit -m "test: version lazy workspace provenance"
```

---

### Task 8: Native behavior, no-tax, and concurrency checkpoint

**Files:**
- Modify: `cmd/shellbeam/a1_runtime_test.go`
- Modify: `cmd/shellbeam/daemon_test.go`
- Modify: `dev/test-impact.toml` if the new packages are not already mapped by the current impact rules.
- Add no production file unless a failing checkpoint exposes a plan/spec mismatch.

**Interfaces:**
- Verifies all previous tasks; produces no new public contract.

- [ ] **Step 1: Add native integration cases**

Run a real daemon + Git worktree and prove:

- ordinary `pwd`/`true` start has no workspace Git freshness subprocess and ends with unreconciled post provenance;
- short edit command cannot reuse its pre-command TTL snapshot as observed post-state;
- explicit sampler after edit sees the change;
- long-running shell may overlap sampling without making the sample unusable, but `cache_eligible=false`;
- dirty -> restore -> sample reports the resolved transition;
- clean branch switch is recognized as source-view-may-have-changed even when dirty set is empty;
- non-Git/absolute-cwd shell still runs with unavailable binding/sample;
- daemon restart starts coherence epoch/active count fresh and does not inherit old leases.

- [ ] **Step 2: Add the exactly-one end stress test**

Loop concurrent success, spawn failure, timeout, kill, and shutdown cases under `-race`. Assert final `active_managed_shell_operations==0` and no lease underflow/panic.

Run:

```bash
go test -race ./internal/app/workspace ./internal/app/daemon ./cmd/shellbeam -count=1
```

Expected: PASS.

- [ ] **Step 3: Prove the ordinary no-tax path**

Use a counting Git runner in a daemon-level test rather than wall-clock-only assertions. For an ordinary command without `activity_id`/freshness request, assert zero calls to `git status`, `git ls-files`, `ssh`, or `gh` attributable to workspace observation.

Keep benchmark evidence separately:

```bash
go test ./internal/app/daemon -run '^$' -bench 'Workspace|Start' -benchmem -count=5
```

Record p50/p95/p99 in the implementation receipt/review note; do not weaken the design’s <=5 ms p95 / <=10 ms p99 incremental roadmap gate without a design amendment.

- [ ] **Step 4: Run repository gates on the exact final tree**

```bash
go test ./internal/core/workspace ./internal/core/activity ./internal/core/receipt \
  ./internal/app/workspace ./internal/app/activity ./internal/app/daemon ./internal/adapter/git ./cmd/shellbeam -count=1
go test -race ./internal/app/workspace ./internal/app/daemon ./internal/adapter/git -count=1
go run ./tools/devctl test --dirty --base origin/main
go run ./tools/devctl check
git diff --check origin/main...HEAD
```

Expected: every command exits 0. Treat `[no tests to run]` only as absence of selected tests, never as proof for a required behavior test.

- [ ] **Step 5: Commit checkpoint evidence**

Stage only the implementation/test-impact files belonging to this plan, inspect staged names/stat/check, then:

```bash
git commit -m "test: verify lazy workspace freshness"
```

---

## Completion Gate

Lazy Workspace Freshness is ready for the E29 implementation plan only when all of these are proven on the exact final tree:

1. ordinary shell admission/finalization performs no Git freshness sampling unless an explicit activity/freshness/identity requirement asks for it;
2. every `BeginManagedShell()` lease has exactly one terminal `End()` across success, failure, cancellation, timeout, kill, and shutdown lifecycle paths;
3. daemon restart cannot inherit active counters/leases;
4. cached/unreconciled/freshly-sampled receipt provenance is truthful and legacy provenance remains readable;
5. explicit path-level Git sampling is bounded and returns partial/unavailable rather than empty-as-complete on budget/malformed cases;
6. activity baselines retain enough bounded path facts/completeness for `activity_delta`, or downgrade explicitly;
7. dirty->clean/delete/branch-switch/untracked/non-Git/macOS path semantics remain honest without a watcher or whole-filesystem scan;
8. exact-source test/build/release evidence remains a separate authority contract;
9. focused, race, dirty-test, and repository-check gates pass.
