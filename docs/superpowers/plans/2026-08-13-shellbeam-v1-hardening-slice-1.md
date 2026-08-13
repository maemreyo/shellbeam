# ShellBeam V1 Hardening Slice 1 Implementation Plan

Scope is limited to singleton daemon ownership and immutable/recoverable persistence. Follow RED, GREEN, refactor for every behavior.

## Task 1: Listener ownership before reconciliation

1. Add a failing two-daemon test: first daemon owns nonterminal state; second returns daemon_already_running without changing the state tree.
2. Confirm reconciliation-before-listener causes the failure.
3. Change runDaemon to acquire the listener before AbandonUnresolved.
4. Run focused cmd and IPC tests plus the test under race.

## Task 2: Append-once terminal receipt

1. Add a failing test that publishes A, attempts different B, expects terminal_conflict, and compares receipt and metadata bytes.
2. Add a failing idempotent replay test for A.
3. Implement per-session serialized create-once publication and semantic comparison.
4. Keep metadata repair separate from immutable receipt creation.
5. Run focused store and daemon tests under race.

## Task 3: Recoverable reservation

1. Add failing tests for committed operation with missing metadata and legacy orphan starting metadata.
2. Make the operation record the reservation commit record.
3. On replay, repair missing starting metadata and return created=false.
4. Reconcile legacy orphan metadata to one immutable abandoned/ambiguous result.
5. Reopen repositories in tests to prove durability.

## Task 4: Persistence fault matrix

1. Introduce the smallest package-private filesystem seam for create, write, file-sync, close, rename, open-directory, and directory-sync failure.
2. Fail each reservation and terminal boundary in table tests.
3. Reopen and assert an allowed durable state; replay cannot authorize duplicate creation.
4. Prove terminal conflicts modify neither receipt nor metadata.

## Task 5: Verification

1. Run gofmt on touched Go files.
2. Run focused tests after each task.
3. Run go run ./tools/devctl test --dirty --base main --json.
4. Run the current checkpoint command.
5. Run go vet ./... and targeted race tests for cmd, IPC, store, and daemon.
6. Inspect diff, size limits, and architecture barriers.
7. Commit after all evidence is green. Do not push or open a PR.
