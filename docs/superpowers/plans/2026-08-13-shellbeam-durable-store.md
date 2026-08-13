# ShellBeam Durable Store Implementation Plan

> **Requires:** Checkpoint 1 green. Use `superpowers:executing-plans` and `superpowers:test-driven-development`. One primary agent; local branch only.

**Goal:** Implement the secure, bounded, file-backed authority that durably reserves an operation before any spawn and publishes terminal receipts without lying about sync uncertainty.

**Boundary:** This checkpoint does not spawn a real process. `internal/app/daemon` exercises start/poll decisions against a fake process port so reservation and storage semantics are proven independently of OS lifecycle code.

## Fixed API

Create the consumer-owned port `internal/app/daemon/store_port.go`; implement it in `internal/adapter/store`:

```go
type Durability string
const (
    NoDurableChange Durability = "none"
    DurableChange   Durability = "durable"
    AmbiguousChange Durability = "ambiguous"
)
type Result struct { Durability Durability; Err error }

type Repository interface {
    ReserveOperation(context.Context, core.OperationReservation) (core.OperationReservation, bool, Result)
    LoadOperation(context.Context, core.OperationID) (core.OperationReservation, error)
    LoadSession(context.Context, core.SessionID) (core.SessionSnapshot, error)
    AdvanceSession(context.Context, core.SessionSnapshot) Result
    PublishTerminal(context.Context, core.TerminalReceipt) Result
    AppendOutput(context.Context, core.SessionID, []byte) (int, Result)
    ReadOutput(context.Context, core.SessionID, int64, int) ([]byte, int64, error)
    Compact(context.Context, core.SessionID) Result
}
```

No caller may synthesize one logical transition from multiple filesystem writes. The repository owns atomic replace, sync ordering, and typed uncertainty.

### Task 1: Freeze persisted envelopes and compatibility fixtures

**Files:** `api/schema/operation-v1.json`, `api/schema/session-v1.json`, `api/schema/receipt-v1.json`, `api/schema/embed.go`, `api/schema/embed_test.go`, `internal/core/operation/persistence.go`, `internal/core/session/persistence.go`, `internal/core/receipt/persistence.go`, their package tests, `tests/contract/schema_contract_test.go`, `tests/contract/persistence_golden_test.go`, `tests/contract/testdata/persistence/v1/*.json`.

- [ ] Write failing round-trip/golden tests for operation reservation, starting/running/finalizing snapshots, terminal receipt, and compact tombstone. Unknown fields, unknown major versions, invalid transition, missing evidence, and receipt success with incomplete output/input must fail.
- [ ] Add immutable `OperationReservation` with caller intent plus effective shell/config, daemon incarnation, control reservation bytes, created time, and session binding.
- [ ] Add snapshots that never store a recoverable PID/PGID authority. PID/PGID may appear only as non-authoritative diagnostic evidence explicitly labeled with incarnation.
- [ ] Add terminal/tombstone envelopes and strict JSON decoding via `DisallowUnknownFields` plus trailing-token rejection.
- [ ] Extend the typed embedded schema inventory with `OperationV1` and `SessionV1`; update the exact inventory count from five to seven and keep the existing schema IDs unchanged.
- [ ] Run focused tests, dirty tests, and commit `feat: define durable persistence envelopes`.

### Task 2: Secure state-root opening

**Files:** `internal/adapter/store/root.go`, `root_unix.go`, `root_test.go`, `root_linux_test.go`, `root_darwin_test.go`.

- [ ] Write failing tests for absent root, wrong owner, group/world permissions, symlink root/member, non-directory component, and regular-file collision.
- [ ] Create root/operations/sessions with `0700` under process umask `077`; create metadata/receipt files `0600` and output logs `0600`.
- [ ] Validate type, UID, and permissions before use. Use directory-relative operations and `O_NOFOLLOW`/platform equivalents from `x/sys/unix`; never follow an unexpected symlink.
- [ ] Return stable `unsafe_state_path` or `permission_denied` without mutating unsafe paths.
- [ ] Commit `feat: secure shellbeam state storage`.

### Task 3: Atomic file protocol and fault seam

**Files:** `internal/adapter/store/atomic.go`, `sync.go`, `fault.go`, `atomic_test.go`.

- [ ] Define a narrow injected filesystem seam covering create temp, write-all, file sync, rename, directory sync, statfs, and remove-temp.
- [ ] Test short write, write error, file-sync error, rename error, directory-sync error, and cleanup error. Assert `none`, `durable`, or `ambiguous` exactly.
- [ ] Implement same-directory temp write, chmod `0600`, full write, file sync, atomic rename, directory sync. A failed pre-rename step is `none`; a successful directory sync is `durable`; rename followed by failed/unknown directory sync is `ambiguous`.
- [ ] Never overwrite the last good file before a complete replacement exists.
- [ ] Commit `feat: add typed atomic persistence`.

### Task 4: Capacity and storage admission

**Files:** `internal/app/daemon/budget.go`, `budget_test.go`, `internal/adapter/store/budget.go`, `budget_test.go`.

- [ ] Table-test concurrent-session capacity, per-session output reservation, daemon state limit, filesystem free-space floor, and fixed control-plane headroom.
- [ ] Implement one mutex-owned admission ledger. `AcquireStart` atomically acquires session capacity and control bytes before operation reservation. Any failure releases all acquired units and creates no operation.
- [ ] `AcquireOutput(n)` rejects a complete chunk before write if it crosses session/global/free-space limits. It never consumes terminal receipt headroom.
- [ ] Reconciliation at startup computes disk use from verified regular files and rejects unexpected entries.
- [ ] Commit `feat: enforce capacity and storage budgets`.

### Task 5: Reservation-before-spawn repository

**Files:** `internal/app/daemon/store_port.go`, `internal/adapter/store/repository.go`, `operation.go`, `session.go`, `repository_test.go`, `operation_concurrency_test.go`.

- [ ] Test 100 concurrent same-ID/same-fingerprint reservations: exactly one new binding and one session ID. Test same ID/different fingerprint conflict. Test capacity/storage/persistence failure creates neither operation nor session.
- [ ] Serialize per-operation creation with a bounded keyed lock removed after use. Recheck disk inside the critical section.
- [ ] Persist the complete operation and starting snapshot before returning `created=true`; sync parent directories in correct order.
- [ ] On an ambiguous durability result, return `persistence_ambiguous`; never spawn and never delete evidence. A retry must load/reconcile before deciding.
- [ ] Commit `feat: reserve operations before execution`.

### Task 6: Output append, UTF-8 response slicing, and cursor rules

**Files:** `internal/adapter/store/output.go`, `output_test.go`, `internal/core/receipt/output.go`, `output_test.go`, `output_fuzz_test.go`.

- [ ] Test canonical raw bytes, invalid UTF-8 replacement only in visible text, response boundaries that do not split valid UTF-8, cursor equal/end/beyond end, max zero, and repeated read stability.
- [ ] Append only after budget admission; sync each bounded chunk before advancing durable metadata. Report output-limit, reserve-exhausted, and capture-failed distinctly.
- [ ] `ReadOutput` returns raw byte cursor positions while the response encoder emits valid UTF-8.
- [ ] Seed fuzzing with ASCII, multibyte boundaries, invalid sequences, and zero-length ranges.
- [ ] Commit `feat: persist canonical session output`.

### Task 7: Terminal publication and retention tombstones

**Files:** `internal/adapter/store/terminal.go`, `retention.go`, `terminal_test.go`, `retention_test.go`.

- [ ] Test receipt durable-before-visible ordering and fault every sync point. Pre-durable publication leaves the session `finalizing`; ambiguous publication never exposes invented success.
- [ ] `PublishTerminal` validates reap/spawn evidence, drain status, accepted-versus-delivered input, and output completeness before state mapping.
- [ ] Compact only terminal sessions: atomically publish a compact tombstone containing operation/session IDs, fingerprint, terminal outcome/evidence summary, output byte count/hash, and `output_available=false`; then remove output only after tombstone durability.
- [ ] V1 tombstones do not expire automatically. Explicit purge is not exposed by the shipped CLI.
- [ ] Commit `feat: publish durable receipts and tombstones`.

### Task 8: Store checkpoint proof

**Files:** `dev/test-impact.toml`, `tools/devctl/*_test.go`, `docs/adr/0002-file-store-durability.md`.

- [ ] Map store/schema changes to persistence, fault, cursor/fuzz-seed, and concurrency suites.
- [ ] ADR records file-store choice, sync protocol, durability tri-state, no database, and no PID recovery.
- [ ] Run `go test -race ./internal/adapter/store ./internal/app/daemon ./internal/core/...`, `go run ./tools/devctl verify --checkpoint --base main --json`, and `git status --short`.
- [ ] Commit `test: prove durable store checkpoint`.

## Completion gate

Checkpoint 2 is complete only when a durable operation binding always precedes a fake spawn authorization, every storage fault has a classified result, unsafe paths fail closed, output/cursors are byte-correct, and terminal/tombstone ordering is fault-tested. Report **durable-store ready**, not runtime ready.
