# Runtime Lifecycle Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate post-admission ownerless sessions and make persistent runtime reconciliation converge after transient/fatal failure without capacity leaks or live/durable split-brain.

**Architecture:** Treat durable admission as the ownership boundary. Every error after that boundary must either publish a canonical terminal record or retain/re-establish a lifecycle owner. Persistent reconciliation becomes a supervised retry/classification loop; canonical terminal publication immediately updates the live projection, while persistent binding closure is retried/repaired independently.

**Tech Stack:** Go 1.26.x, ShellBeam file store, daemon lifecycle orchestration, persistent supervisor protocol, Go tests with existing store fault injection.

## Global Constraints

- Base is local `main` commit `c1961ed2a5fbf20177ab820b1971056517c1152a`, which is one commit ahead of fetched `origin/main`.
- Work only in `/Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/runtime-lifecycle-hardening`.
- Preserve direct-session zero-tax behavior: no persistent store/runtime calls on ordinary successful starts.
- Never signal a persistent child from stored PID/PGID after ownership proof is lost.
- Keep canonical terminal receipt immutable and exactly once.
- Use RED -> GREEN for every production change.
- No push or PR.

---

### Task 1: Finalize direct starts that fail after admission but before a live owner exists

**Files:**
- Modify: `internal/app/daemon/project_command.go`
- Modify: `internal/app/daemon/service.go`
- Test: `internal/app/daemon/project_command_test.go` or a focused new daemon test file

**Interfaces:**
- Consumes: existing `finishSpawnFailure(*liveSession)` and `publishUntilDurable(receipt.Receipt)`.
- Produces: `finalizeAdmittedStartFailure(...)`, usable before `activateLiveSession`.

- [x] **Step 1: Write the failing observation-prepare regression test**

Use a store wrapper that embeds the real repository and overrides only process-observation preparation:

```go
type failingProcessObservationStore struct {
    *storeadapter.Repository
}

func (s *failingProcessObservationStore) PrepareProcessStartedObservation(context.Context, string, string) app.StoreResult {
    return app.StoreResult{Durability: app.NoDurableChange, Err: errors.New("injected process observation failure")}
}
```

Start with `MaxSessions: 1`, assert the first Start returns the injected failure, its durable session becomes terminal with a receipt, then assert a second ordinary Start succeeds.

- [x] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/app/daemon -run 'Test.*(ProcessObservation|SpawnFailure)' -count=1
```

Expected: FAIL because the first session remains `Starting` and the second start returns `capacity_exceeded`.

- [x] **Step 3: Add a single pre-live admitted failure finalizer**

Extract receipt publication from the existing spawn-failure path so it can safely handle an admitted `liveSession` that was never inserted into `Service.live` and has no process handle. Preserve workspace provenance and terminal workers.

Use it in `spawnPreparedStart` when `prepareProcessStartedObservation()` fails before `activateLiveSession()`.

- [x] **Step 4: Verify GREEN**

Run the focused daemon test, then:

```bash
go test ./internal/app/daemon -run 'Test.*(ProcessObservation|SpawnFailure)' -count=1
```

- [x] **Step 5: Commit**

```bash
git add internal/app/daemon
git commit -m "fix: finalize admitted direct start failures"
```

### Task 2: Close persistent bindings when a start fails after provisioning

**Files:**
- Modify: `internal/app/daemon/persistent_start.go`
- Modify: `internal/app/daemon/persistent_startup.go`
- Test: `internal/app/daemon/persistent_launch_test.go`
- Test: `internal/app/daemon/persistent_restart_test.go`

**Interfaces:**
- Consumes: `PersistentSessionStore.FindPersistentBinding`, `AdvancePersistentBinding`, existing `publishPersistentSpawnFailure`.
- Produces: a persistent-start failure convergence helper that leaves no `Provisioning`/`Live` binding after the Start call returns.

- [x] **Step 1: Write a RED test using a runtime that reserves a real provisioning binding and then returns an Ensure error**

The runtime must call `ReservePersistentBinding` before returning the injected error. Assert after Start returns:
- canonical session is terminal;
- capacity is free;
- binding is `Lost`;
- `ListPersistentRecoveryCandidates` does not include the session.

- [x] **Step 2: Verify RED**

```bash
go test ./internal/app/daemon -run 'TestPersistent.*Failure.*Binding' -count=1
```

Expected: current `c1961ed` leaves binding `Provisioning` and recovery marker active.

- [x] **Step 3: Converge existing binding to Lost after durable failed receipt**

After the failed receipt is durable, look up the persistent binding. If absent, keep current behavior. If `Provisioning` or `Live`, advance it monotonically to `Lost`; retry ambiguous/transient persistence until the binding is no longer active. Never signal the child.

- [x] **Step 4: Add restart repair before reattach**

Before `ReconcilePersistentStartup` reattaches an active binding, check canonical session metadata. If the session is already terminal, do not reattach. Converge the stale active binding away from `Provisioning`/`Live` and remove its recovery marker.

- [x] **Step 5: Verify focused tests**

```bash
go test ./internal/app/daemon -run 'TestPersistent.*(Failure.*Binding|Startup.*Terminal)' -count=1
```

- [x] **Step 6: Commit**

```bash
git add internal/app/daemon
git commit -m "fix: close persistent bindings after start failure"
```

### Task 3: Supervise the persistent reconciler instead of discarding its error

**Files:**
- Modify: `internal/app/daemon/persistent_reconcile.go`
- Test: `internal/app/daemon/persistent_reconcile_test.go`

**Interfaces:**
- Consumes: `reconcilePersistentSession`, `persistentStartupLossReason`, `PersistentSessionStore.AbandonPersistentSession`.
- Produces: `runPersistentReconciliation(...)` which either retries, classifies Lost, reaches Terminal, or exits only for deliberate cancellation/detach.

- [x] **Step 1: Write a RED transient-error retry test**

Make `Status()` fail once with an unclassified transient error, then return a valid running/terminal sequence. Assert one transient failure does not end reconciliation and the session eventually reaches canonical terminal.

- [x] **Step 2: Write a RED fatal ownership/conflict test**

Use an invalid supervisor generation or `PersistentRecoveryOutputConflict`. Assert the reconciler does not silently disappear with `Live` binding; it classifies the session lost/ambiguous, publishes canonical terminal evidence, and removes the active recovery marker without signaling the child.

- [x] **Step 3: Verify both tests fail on current code**

```bash
go test ./internal/app/daemon -run 'TestPersistentReconciliation.*(Retries|Lost)' -count=1
```

- [x] **Step 4: Implement the reconciliation supervisor loop**

Pseudo-code:

```go
for {
    err := s.reconcilePersistentSession(ctx, live, control, outputStore, bindingStore)
    if err == nil || ctx.Err() != nil {
        return
    }
    if reason, lost := persistentStartupLossReason(err); lost {
        if s.finalizePersistentRuntimeLoss(live, bindingStore, reason) == nil {
            return
        }
    }
    wait with bounded backoff, then retry
}
```

Check `ctx.Err()` before classification so graceful daemon shutdown remains detach, not loss.

- [x] **Step 5: Verify GREEN and existing conflict tests**

```bash
go test ./internal/app/daemon -run 'TestPersistentReconciliation' -count=1
```

- [x] **Step 6: Commit**

```bash
git add internal/app/daemon/persistent_reconcile.go internal/app/daemon/persistent_reconcile_test.go
git commit -m "fix: supervise persistent reconciliation lifecycle"
```

### Task 4: Make live Poll converge immediately after durable persistent terminal receipt

**Files:**
- Modify: `internal/app/daemon/types.go` or the existing `liveSession` definition file
- Modify: `internal/app/daemon/persistent_reconcile.go`
- Test: `internal/app/daemon/persistent_reconcile_test.go`

**Interfaces:**
- Produces: an idempotent live terminal projection/scheduling path; binding persistence remains retryable separately.

- [x] **Step 1: Write a RED binding-update fault test**

Wrap the persistent store so the first `AdvancePersistentBinding(...Terminal)` fails after `PublishTerminal` succeeds. Assert:
- durable receipt is terminal;
- `Poll()` returns terminal, not Running;
- terminal workers schedule exactly once;
- reconciliation later retries binding closure and binding becomes Terminal.

- [x] **Step 2: Verify RED**

```bash
go test ./internal/app/daemon -run 'TestPersistentTerminal.*Binding.*Failure' -count=1
```

Expected: current code leaves live state Running and the goroutine exits.

- [x] **Step 3: Project canonical terminal to live state immediately**

Right after successful `publishPersistentTerminal`, mirror terminal state/outcome/evidence into `live` and notify waiters. Use a `sync.Once` on `liveSession` for structured/telemetry/evidence scheduling so retrying binding closure cannot schedule duplicates.

- [x] **Step 4: Verify GREEN**

Run focused persistent terminal/reconciliation tests.

- [x] **Step 5: Commit**

```bash
git add internal/app/daemon
git commit -m "fix: converge persistent live state after terminal receipt"
```

### Task 5: Close the ambiguous metadata admission boundary

**Files:**
- Modify: `internal/adapter/store/reservation.go` and/or a focused store admission helper
- Modify: `internal/app/daemon/project_command.go` only if compensation belongs above the store boundary
- Test: `internal/adapter/store/fault_test.go`
- Test: focused daemon integration only if required

**Interfaces:**
- Must preserve the store contract that ambiguous durability never authorizes spawn.
- Must ensure a failed Start does not leave a capacity-owning `Starting` session without an owner.

- [x] **Step 1: Add the exact RED fault case**

Inject the metadata `replace.dir_sync` failure after reservation creation. With `MaxSessions: 1`, assert the failed admission is reconciled/terminalized in the same daemon lifetime and a second admission can succeed without restart.

- [x] **Step 2: Verify RED**

```bash
go test ./internal/adapter/store -run 'Test.*Metadata.*Ambiguous.*Capacity' -count=1
```

- [x] **Step 3: Implement same-process compensation without authorizing spawn**

Do not reinterpret `AmbiguousChange` as successful admission. Once the store can prove the intended session metadata exists after the ambiguous rename, durably terminalize that unowned session (or otherwise remove it from active admission truth) before returning the failure to the caller.

- [x] **Step 4: Verify all reservation fault boundaries**

```bash
go test ./internal/adapter/store -run 'TestReservationFaultBoundaries|Test.*Metadata.*Ambiguous.*Capacity' -count=1
```

- [x] **Step 5: Commit**

```bash
git add internal/adapter/store
git commit -m "fix: compensate ambiguous admitted session metadata"
```

### Task 6: Require a persistent recovery owner before Running

**Files:**
- Modify: `internal/app/daemon/persistent_reconcile.go`
- Modify: `internal/app/daemon/persistent_start.go`
- Modify: `internal/app/daemon/persistent_startup.go`
- Test: `internal/app/daemon/persistent_launch_test.go`

**Invariant:** a persistent session may not publish `Running` unless the launched/reattached handle and store have the complete recovery surfaces required by the reconciliation owner.

- [x] **Step 1: RED — generic persistent handle cannot become Running**

Replace the old fixture that accepted a PID-only `ProcessHandle` with a test that requires `SupervisorStateConflict`, a durable Failed receipt, zero signal/write fallback, local handle close, and immediate capacity reuse. Current code returns `err=nil`, proving the architecture hole.

- [x] **Step 2: Prepare lifecycle ownership before Running**

Add a preparation primitive that proves `RecoveryAttachment`, persistent output reconciliation, and persistent binding reconciliation before process-start observation / Running publication. Pass the prepared owner into `startPersistentReconciliation`; remove silent capability returns. Apply the same preparation on startup reattach.

- [x] **Step 3: Keep successful fixtures honest**

Success-path persistent test handles implement `RecoveryAttachment` and explicitly shut down their test service. Generic unauthenticated handles remain fail-closed and are never used as fallback control.

- [x] **Step 4: Verify persistent regression group**

```bash
go test ./internal/app/daemon -run 'TestPersistent' -count=1
```

- [x] **Step 5: Commit**

```bash
git add docs/superpowers/plans/2026-08-17-runtime-lifecycle-hardening.md internal/app/daemon
git commit -m "fix: require persistent recovery ownership before running"
```

### Task 7: Full lifecycle verification

**Files:**
- No production changes unless a regression is found and reproduced with a RED test first.

- [ ] **Step 1: Focused repeat**

```bash
go test ./internal/app/daemon ./internal/adapter/store -count=3
```

- [ ] **Step 2: Race**

```bash
go test -race ./internal/app/daemon ./internal/adapter/store
```

- [ ] **Step 3: Full suite**

```bash
go test ./...
```

- [ ] **Step 4: Repository checks**

Run the project-supported `devctl` test/check commands discovered in this worktree, using dirty/base flags only if required by the command contract.

- [ ] **Step 5: Inspect final diff/status**

```bash
git status --short --branch
git log --oneline --decorate -8
git diff main...HEAD --check
```
