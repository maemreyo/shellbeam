# Observation Storage & Housekeeping Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ShellBeam's observation store self-recovering, bounded, fast to reopen, and operator-visible under low disk space while eliminating the identified test leaks.

**Architecture:** Keep per-obligation filenames as the crash-safe sequence authority, but separate metadata discovery from strict record decoding. Retention removes only materialized obligations, background housekeeping wires retention/event compaction/free-space warnings after readiness, and tests own all persistent resources they create.

**Tech Stack:** Go 1.26.x, file-backed store, `golang.org/x/sys/unix.Statfs`, existing ShellBeam atomic writer, existing observation materializer and event projection.

## Global Constraints

- Preserve the current file-per-obligation on-disk format in this slice.
- Highest durable obligation filename remains the authoritative high watermark.
- Low free space is warning-only; it must never reject daemon startup or admission.
- Prepared obligations are never collected.
- Collection never deletes records above `MaterializedThroughSeq`.
- No automatic deletion of existing orphaned developer-machine processes/directories.
- Do not stage unrelated `.DS_Store` or other pre-existing user changes.

---

### Task 1: Lock test resource ownership

**Files:**
- Modify: `cmd/shellbeam/state_ownership_test.go`
- Modify: `cmd/shellbeam/persistent_stdin_policy_test.go`

**Interfaces:**
- Consumes: existing `ownershipBinary()` shared `sync.Once` binary and `startPersistent(...)` helper.
- Produces: package-lifetime binary cleanup and per-test persistent-session cleanup.

- [ ] **Step 1: Run focused tests while counting external resources**

Run before/after counts around:

```sh
ps -axo command | grep '[s]hellbeam __supervisor' | wc -l
find /private/tmp -maxdepth 1 -type d -name 'shellbeam-ownership-bin-*' | wc -l
go test ./cmd/shellbeam -run 'TestPersistentSessionKeepsWritableStdinByDefault|TestStateDirectory' -count=1
```

Expected before fix: test may pass while counts increase. Expected with current WIP: counts must not increase.

- [ ] **Step 2: Verify package-scoped binary cleanup is tied to package lifetime**

Keep `ownershipBinaryRoot` and remove it from `TestMain` after `m.Run()`. Do not use `t.Cleanup` inside the `sync.Once` builder.

- [ ] **Step 3: Verify every successful persistent start registers a cleanup kill**

Keep `killPersistentOnCleanup(...)` attached inside `startPersistent(...)`; use a bounded background context and idempotent `KILL` request.

- [ ] **Step 4: Run focused tests twice and prove zero delta**

Expected: test PASS; supervisor and temp build-dir counts are unchanged after each run.

- [ ] **Step 5: Commit only after all storage tasks are green**

No standalone commit yet because this WIP already spans the same bug-fix slice.

---

### Task 2: Split observation metadata discovery from record decoding

**Files:**
- Modify: `internal/adapter/store/observation.go`
- Test: `internal/adapter/store/observation_fault_test.go`
- Test: `internal/adapter/store/observation_test.go`

**Interfaces:**
- Produces: `observationSequences(dir string) ([]observation.ChangeSeq, error)` as metadata-only safe discovery; `readObservation(seq)` remains strict content authority.
- Consumers: `initObservationStore`, `ListObservationObligations`, observation retention.

- [ ] **Step 1: Add failing test proving Open does not decode unrelated historical record content**

Create a valid highest record and an older syntactically corrupt record with a safe filename/mode/size. Reopen should recover the high watermark successfully, then `ListObservationObligations` that reaches the corrupt record must fail.

Representative assertions:

```go
reopened, err := Open(root, repository.limits)
if err != nil { t.Fatalf("metadata-only reopen: %v", err) }
if high, _ := reopened.ObservationHighWatermark(context.Background()); high != 2 { ... }
if _, err := reopened.ListObservationObligations(context.Background(), 0, 10); err == nil { ... }
```

- [ ] **Step 2: Run the new test and observe RED**

Run:

```sh
go test ./internal/adapter/store -run 'TestObservationOpenUsesMetadataButReadStillRejectsCorruptRecord' -count=1
```

Expected: FAIL because `Open` currently strict-decodes all records through `observationSequences`.

- [ ] **Step 3: Add failing regression for >65,536 safe filenames**

Avoid writing 65k JSON payloads. Create sparse/minimal safe regular files directly in a dedicated test and assert `Open` no longer returns `observation obligation scan limit exceeded`. The test may use `MaxObservationObligationBytes` bounds and only one valid highest record if startup is metadata-only.

- [ ] **Step 4: Run oversized-ledger test and observe RED**

Expected: FAIL on the historical scan cap.

- [ ] **Step 5: Implement metadata-only `observationSequences`**

For each non-temp entry:

```go
if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() { ... }
seq, ok := parseObservationFilename(entry.Name())
info, err := entry.Info()
if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > MaxObservationObligationBytes { ... }
sequences = append(sequences, seq)
```

Remove the global `len(entries) > maxObservationScanRecords` refusal and remove `readStrict`/`record.Validate()` from sequence discovery. Keep sorting.

- [ ] **Step 6: Keep strict validation at consumption**

Do not weaken `readObservation`. `ListObservationObligations` must call `readObservation(seq)` only for records after `after`, and stop after `limit`.

- [ ] **Step 7: Run focused observation tests**

```sh
go test ./internal/adapter/store -run 'Observation|MaterializedObligation' -count=1
```

Expected: PASS.

---

### Task 3: Make retention recover oversized ledgers safely

**Files:**
- Modify: `internal/adapter/store/observation_retention.go`
- Test: `internal/adapter/store/observation_retention_test.go`
- Test: `cmd/shellbeam/observation_retention_readiness_test.go`

**Interfaces:**
- Consumes: metadata-only `observationSequences`, strict `readObservation`, projection state.
- Produces: `CollectMaterializedObligations(ctx, ObligationRetentionPolicy) (ObligationRetentionReport, error)` that remains usable above the old scan cap.

- [ ] **Step 1: Keep one filesystem-level regression above the historical scan cap**

Seed 65,537 safe entries once and prove `Open` recovers the exact high watermark and allocates `high+1`. Do not add a second 65k retention scan to the normal gate; it turns a correctness regression into multi-minute filesystem churn.

- [ ] **Step 2: Prove retention composition with focused bounded tests**

Run the small retention suite proving restart watermark, prepared-record preservation, projection cutoff, and deletion bound. Because retention consumes the same uncapped metadata discovery from Task 2, these tests cover retention semantics without duplicating the expensive 65k fixture.

- [ ] **Step 3: Keep safety predicates exact**

Inside collection:

```go
if seq > state.MaterializedThroughSeq { break }
if seq == newest { continue }
record, err := r.readObservation(seq)
if err != nil { /* leave it in place, do not guess-delete */ }
if record.State == observation.ObligationPrepared { continue }
```

A corrupt record is retained rather than deleted.

- [ ] **Step 4: Preserve restart watermark**

Run `TestCollectingObligationsKeepsTheWatermarkIntactAcrossReopen` and daemon restart readiness test. Expected: one newest watermark anchor may remain after a fully caught-up projection.

- [ ] **Step 5: Verify bounded backlog behavior**

Run retention bound test and real-daemon wiring test. Expected: first sweep deletes at most 1024; backlog reschedules faster until caught up.

---

### Task 4: Restore post-commit materializer wake-up

**Files:**
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/observation.go`
- Modify: `cmd/shellbeam/execution_observation.go`
- Test: `internal/adapter/store/observation_test.go`
- Test: `cmd/shellbeam/execution_observation_test.go`

**Interfaces:**
- Produces: `Repository.ObservationWakeups() <-chan struct{}` and a coalescing background materialization loop.

- [ ] **Step 1: Write RED store wake-up test**

A prepared obligation must not wake the worker. Transitioning it to `committed` must emit one wake-up within a bounded test deadline.

- [ ] **Step 2: Write RED runtime continuation test**

Use a fake `MaterializerPort`: wait for the startup pass, emit one wake-up, and require a second materialization call.

- [ ] **Step 3: Implement coalescing repository signal**

Initialize a size-one buffered channel in `Open`. Signal only after terminal observation state is durably visible or an idempotent terminal replay confirms it. Do not attach authority or a sequence payload to the channel.

- [ ] **Step 4: Implement sleeping materialization worker**

Run immediately once, then wait on the wake-up channel. If materialization errors, arm a one-minute retry; when healthy, do not poll. Context cancellation must terminate the worker.

- [ ] **Step 5: Add real-materializer regression**

Complete the initial pass, commit an observation afterwards, never call `inspect.events`, and assert `MaterializedThroughSeq` advances to that sequence.

- [ ] **Step 6: Run focused tests**

```sh
go test ./internal/adapter/store -run 'ObservationTerminalTransitionSignalsMaterializer' -count=1
go test ./cmd/shellbeam -run 'ExecutionObservation.*Materializ' -count=1
```

---

### Task 5: Wire warning-only free-space and observation housekeeping

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/doctor.go`
- Create/keep: `cmd/shellbeam/housekeeping.go`
- Create/keep: `cmd/shellbeam/freespace_runtime.go`
- Create/keep: `cmd/shellbeam/observation_retention_runtime.go`
- Create/keep tests: `cmd/shellbeam/doctor_free_space_test.go`, `cmd/shellbeam/freespace_runtime_test.go`, `cmd/shellbeam/observation_retention_readiness_test.go`
- Create/keep: `internal/adapter/store/freespace.go`, `internal/adapter/store/freespace_test.go`

**Interfaces:**
- Produces: `AvailableBytes(dir string)`, `doctorFreeSpaceCheck`, `startFreeSpaceWatch`, `startObservationRetention`, `startHousekeeping`.

- [ ] **Step 1: Run existing WIP tests and confirm their intended behavior**

```sh
go test ./internal/adapter/store -run AvailableBytes -count=1
go test ./cmd/shellbeam -run 'FreeSpace|DaemonCollectsTheObservationLedger|CollectedLedgerStillRestarts' -count=1
```

- [ ] **Step 2: Verify Statfs semantics**

Keep `unix.Statfs` with `Bavail * Bsize`; return an error for a missing state directory.

- [ ] **Step 3: Verify doctor is warning-only**

Threshold `1<<60` must yield `control.Warn` while `report.ExitCode()` remains zero. Healthy threshold must `PASS`.

- [ ] **Step 4: Verify runtime watcher is edge-triggered**

Low-low-low emits one `free_space_low`; recovery emits one `free_space_recovered`; a second crossing emits a second low event. Probe errors do not emit a false low-space event.

- [ ] **Step 5: Verify housekeeping starts only after readiness**

Keep `startHousekeeping` at the prior `startRetention` position after `server.MarkReady()`.

- [ ] **Step 6: Review housekeeping constants for bounded work**

Keep observation deletion bound `1024`, normal interval `10m`, backlog interval `30s`, event retention `8192 records / 32 MiB / 7 days` unless an existing spec defines stricter values.

---

### Task 6: Performance and recovery evidence

**Files:**
- Test or temporary benchmark only; do not add production benchmarking code.

**Interfaces:**
- Validates Tasks 2–4 against the real long-running state directory without mutating it.

- [ ] **Step 1: Measure metadata discovery vs strict decode on a copy or read-only benchmark path**

Record entry count and wall time for sequence discovery/Open. Target: startup should no longer scale with JSON decoding of every historical obligation; on the inspected ~15k ledger it should drop from multi-second decode cost to directory-metadata scale.

- [ ] **Step 2: Prove old-cap recovery using automated tests**

The >65,536 reopen regression is the authoritative proof that the historical hard cap is gone; the focused retention suite proves collection semantics. No manual deletion is allowed to make either pass.

- [ ] **Step 3: Re-measure live external leaks without cleanup**

Record existing supervisor/temp-dir counts only. Do not kill/remove them. Run the focused tests and assert delta zero.

---

### Task 7: Repository verification and commit

**Files:**
- All files in Tasks 1–5 plus the design/spec updates.

**Interfaces:**
- Produces: one coherent bug-fix commit after proof.

- [ ] **Step 1: Format changed Go files**

```sh
gofmt -w <changed-go-files>
```

- [ ] **Step 2: Run focused packages**

```sh
go test ./internal/adapter/store ./cmd/shellbeam -count=1
```

- [ ] **Step 3: Run repository gate**

```sh
go run ./tools/devctl test --dirty --base origin/main
go run ./tools/devctl check --base origin/main
```

If `devctl check` still reports the pre-existing `runDaemonWithCodeProvider` >80-line violation, compare against `HEAD` and ensure this slice did not worsen it. Fix only if the current changes are responsible.

- [ ] **Step 4: Inspect diff and stage only intended files**

Exclude every `.DS_Store` and unrelated dirty file. Run `git diff --check` and `git diff --cached --check`.

- [ ] **Step 5: Commit**

```sh
git commit -m "fix: bound observation storage housekeeping"
```

- [ ] **Step 6: Do not push or open a PR unless explicitly requested.**
