# ShellBeam Daemon and Unix-Socket IPC Implementation Plan

> **Requires:** Checkpoints 1–3 green. Execute test-first on the local implementation branch.

**Goal:** Make one persistent per-user daemon the sole process/session owner and expose a versioned closed JSON protocol over an authenticated Unix socket.

## Fixed protocol

Use HTTP/1.1 over a Unix domain socket with `POST /v1/local-shell`. Request/response bodies are the same four closed action envelopes used by MCP, wrapped in `ipc_version: 1`. Limits: 64 KiB headers, 1 MiB request body, configured response cap, explicit server read-header/read/write/idle timeouts. Keep-alive is allowed; peer UID is checked per accepted connection before decoding HTTP.

### Task 1: Versioned IPC envelopes and mapping

**Files:** `api/schema/ipc-v1.json`, `internal/adapter/ipc/protocol.go`, `protocol_test.go`, `internal/app/daemon/action.go`, `action_test.go`, `internal/app/bridge/client_port.go`.

- [ ] Failing golden tests reject unknown fields/actions/version/trailing JSON and preserve all stable error details.
- [ ] Define `ActionService` with `Start`, `Poll`, `Write`, `Kill`; IPC types map to core commands without importing process/store implementations.
- [ ] Transport/malformed errors are distinct from terminal command outcomes.
- [ ] Commit `feat: define local ipc contract`.

### Task 2: Runtime directory and socket safety

**Files:** `internal/adapter/ipc/runtime_unix.go`, `runtime_unix_test.go`.

- [ ] Test root ownership/mode `0700`, socket mode `0600`, symlink/collision, stale socket, already-live daemon, and cleanup only for the inode created by this process.
- [ ] Bind in verified user runtime directory; never fall back to loopback TCP or a world-accessible temp path.
- [ ] Probe stale socket safely before unlinking; if another daemon responds, fail `daemon_already_running`.
- [ ] Commit `feat: secure daemon unix socket`.

### Task 3: Same-UID peer authentication

**Files:** `internal/adapter/ipc/peer_linux.go`, `peer_darwin.go`, `peer_test.go`, `peer_linux_test.go`, `peer_darwin_test.go`.

- [ ] Linux uses `SO_PEERCRED`; Darwin uses `getpeereid`. Inject a verifier for portable contract tests.
- [ ] Reject missing credentials or UID mismatch before reading request bytes. Log only stable rejection code, not peer-provided data.
- [ ] Add build-tag compile tests for both platform implementations.
- [ ] Commit `feat: authenticate unix socket peers`.

### Task 4: Daemon action orchestration

**Files:** `internal/app/daemon/service.go`, `start.go`, `poll.go`, `write.go`, `kill.go`, `service_test.go`, `tests/integration/retry_integration_test.go`.

- [ ] Lost-response tests prove start same-ID spawns once, write same-offset enqueues once, kill same-ID signals once, and poll repeats canonical range.
- [ ] Start ordering is validate/default/fingerprint -> capacity/control admission -> durable reservation -> spawn. Capacity/persistence failures leave no operation and no process. Spawn failure becomes a durable terminal receipt.
- [ ] Poll wait uses session change notification and bounded timer; request cancellation ends only the wait.
- [ ] Write/kill reject abandoned/finalizing/terminal as specified while replaying exact prior accepted attempts.
- [ ] Commit `feat: orchestrate daemon actions safely`.

### Task 5: Startup reconciliation and V1 abandonment

**Files:** `internal/app/daemon/recover.go`, `recover_test.go`, `internal/adapter/store/reconcile.go`, `reconcile_test.go`.

- [ ] On a new random daemon incarnation, scan strict persisted records. Any prior `starting`, `running`, or `finalizing` becomes terminal `abandoned/ambiguous` through durable store publication.
- [ ] Never signal, wait for, attach to, or test liveness of persisted PID/PGID. Never spawn a replacement.
- [ ] Already durable terminal receipts replay unchanged. Corrupt/unknown records are quarantined logically and surfaced as `persistence_corrupt`; no destructive repair.
- [ ] Commit `feat: abandon unowned sessions on startup`.

### Task 6: HTTP-over-UDS server and client

**Files:** `internal/adapter/ipc/server.go`, `client.go`, `server_test.go`, `client_test.go`, `tests/integration/ipc_integration_test.go`.

- [ ] Test body/header limits, method/path/content type, slow client deadlines, cancellation, malformed/trailing JSON, unknown version, server shutdown, client reconnect, and stable error mapping.
- [ ] `net/http.Server` serves a custom authenticated listener. Client uses `http.Transport.DialContext` to the Unix socket and has no TCP dial fallback.
- [ ] Bridge/client restart during a running helper process does not affect the daemon-owned command.
- [ ] Commit `feat: serve versioned ipc over unix socket`.

### Task 7: Daemon lifecycle and shutdown

**Files:** `internal/app/daemon/run.go`, `shutdown.go`, `run_test.go`, `shutdown_test.go`.

- [ ] Single-instance lock, incarnation record, readiness after reconciliation/socket bind, SIGTERM/SIGINT handling, bounded graceful window, TERM/KILL owned groups, drain/reap/publication attempt.
- [ ] If shutdown ends before durable terminal proof, next startup marks unresolved sessions ambiguous.
- [ ] Remove only owned socket/lock artifacts; preserve state, logs, receipts, and tombstones.
- [ ] Commit `feat: own daemon lifecycle`.

### Task 8: Daemon/IPC checkpoint

**Files:** `dev/test-impact.toml`, `docs/adr/0004-unix-socket-ipc.md`, `tests/integration/ipc_security_test.go`.

- [ ] Run race tests for daemon/ipc/process/store, native same-UID tests, restart-abandonment fault test, and checkpoint verification.
- [ ] Cross-compile other OS protocol/platform files and label compile-only.
- [ ] Commit `test: prove daemon ipc checkpoint`.

## Completion gate

Checkpoint 4 requires a real daemon whose commands survive client/bridge disconnect, whose socket rejects unauthenticated peers, and whose restart converts uncertain prior work to durable ambiguity without touching stale PIDs. Report **daemon/IPC ready on tested OS**.
