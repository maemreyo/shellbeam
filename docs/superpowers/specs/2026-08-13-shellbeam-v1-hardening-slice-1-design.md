# ShellBeam V1 Hardening Slice 1: Ownership and Immutable Evidence

## Status

Approved design derived from the independent V1 review. This slice changes only daemon singleton ownership, terminal receipt immutability, and operation-reservation recovery. Resource policy, poll/error behavior, release evidence, dirty selection, and V1.1 supervision remain out of scope.

## Problem

Daemon startup reconciles unresolved sessions before acquiring the Unix listener. A second daemon can publish abandoned receipts for sessions still owned by the live daemon, then fail with daemon_already_running. Terminal publication replaces receipt.json, so later reconciliation can overwrite terminal fact. Reservation writes metadata before the operation record; a crash between writes leaves starting state without a recoverable operation.

## Invariants

1. Reconciliation requires exclusive ownership of the runtime listener.
2. A second daemon returns daemon_already_running without modifying state.
3. A terminal receipt is append-once. Identical replay is idempotent; different content returns terminal_conflict and changes no persisted byte.
4. Recovery never creates another session or permits duplicate spawn for an operation ID.
5. Every persistence-boundary crash leaves no reservation or enough intent to recover the same operation/session pair.
6. Persisted PID or PGID data never grants process ownership.

## Design

### Listener-first startup

runDaemon opens the store, acquires the protected Unix listener, and only then calls AbandonUnresolved. If later setup fails, the listener is closed by its owner. The listener remains the V1 lifetime singleton lease.

`Listen` serializes the startup pathname transition with a short-lived advisory lock file inside the already-protected runtime directory: acquire lock -> inspect/probe `daemon.sock` -> reclaim only a proven-refused stale socket -> bind/configure the new listener and capture its identity -> release lock. `Server.Close` acquires the same transition lock only after listener shutdown and only around identity-checked pathname removal, so a new owner cannot rebind between the old owner's check and unlink. The lock is never retained by `Server`, never authorizes reconciliation, and is not a second lifetime ownership authority. This closes concurrent reclaim/bind/unlink TOCTOU without changing daemon runtime ownership semantics.

### Append-once terminal publication

PublishTerminal serializes by session. It validates the candidate, reads any existing receipt, and returns durable success for an identical receipt, terminal_conflict without writes for different content, or creates and syncs the receipt when absent.

Metadata publication follows the immutable receipt. A crash after receipt durability but before terminal metadata is repaired from the receipt and never rewrites it.

### Recoverable reservation

The operation record is the start commit record. ReserveOperation writes the complete operation first, then creates or repairs starting metadata. Replay of a committed operation returns created=false, so the application never respawns. During daemon startup, committed operations with missing metadata are reconstructed before reconciliation and become abandoned/ambiguous; unlinked atomic temp files are removed as non-authoritative crash debris.

Legacy orphan starting metadata is fail-closed: reconciliation publishes one abandoned/ambiguous terminal result when enough evidence exists and never treats the orphan as spawn permission.

### Fault injection

A narrow package-private filesystem seam covers create, write, sync, close, rename, and directory-sync. Tests inject failures and reopen the repository. Production uses os operations.

## Verification

- Two-daemon test: loser returns daemon_already_running and the live session tree stays byte-identical.
- Terminal tests: identical replay succeeds; different replay returns terminal_conflict.
- Crash tests: reopen after every injected boundary and prove no duplicate creation or permanent starting state.
- Local gate: focused tests, dirty tests, checkpoint checks, go vet, and relevant race tests.
- This slice does not claim release readiness.
