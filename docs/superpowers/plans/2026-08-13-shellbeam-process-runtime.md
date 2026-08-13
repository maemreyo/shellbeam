# ShellBeam Process Runtime Implementation Plan

> **Requires:** Checkpoints 1–2 green. Execute test-first on `ai/v1-implementation`.

**Goal:** Own real POSIX child processes safely: new process groups, optional PTY, merged canonical output, bounded exactly-once admission for stdin retries, timeout/kill escalation, drain/reap, and evidence-first finalization.

**Boundary:** No socket, MCP, service manager, or daemon restart behavior in this checkpoint. Tests call the runtime port directly.

## Fixed runtime port

Create the consumer-owned port `internal/app/daemon/process_port.go`; implement it in `internal/adapter/process`:

```go
type Owner interface {
    Start(context.Context, core.ExecutionSpec, Sink) (Handle, core.SpawnEvidence, error)
}
type Handle interface {
    Enqueue(core.InputRecord, []byte) error
    Signal(context.Context, core.Signal) core.SignalEvidence
    Wait(context.Context) core.ExitEvidence
    Close() error
}
type Sink interface {
    Append(context.Context, []byte) error
    CaptureFailed(error)
}
```

`Handle` is in-memory capability owned by the current incarnation. It is never reconstructed from persisted PID/PGID.

### Task 1: Shell resolution and execution spec

**Files:** `internal/adapter/process/shell.go`, `shell_test.go`, `internal/core/operation/execution.go`, `execution_test.go`.

- [ ] Test configured shell, inherited absolute `$SHELL`, empty/non-absolute/missing/non-executable shell, exact `-lc` arguments, exact cwd, minimal inherited environment policy, and effective timeout.
- [ ] Resolve and validate shell before reservation data is created by the application use case; persist the resolved path in `ExecutionSpec`.
- [ ] Do not parse or rewrite command text.
- [ ] Commit `feat: resolve shell execution intent`.

### Task 2: Non-TTY process group and pipes

**Files:** `internal/adapter/process/owner_unix.go`, `pipes_unix.go`, `owner_unix_test.go`, `tests/integration/testdata/process/*`.

- [ ] Start helper subprocesses in a new process group with stdin pipe and one merged observed-order output stream. Use explicit deadlines and process helpers in the test binary; no arbitrary sleeps.
- [ ] Prove cwd, exit code, signal exit, descendant group membership, stdout/stderr capture, large output without deadlock, and context cancellation not killing the child.
- [ ] Capture spawn attempted/succeeded evidence separately from exit evidence.
- [ ] Commit `feat: own non-tty process groups`.

### Task 3: PTY adapter

**Files:** `internal/adapter/process/pty_unix.go`, `pty_unix_test.go`.

- [ ] Add `creack/pty@v1.1.24` and tests for `isatty` behavior, combined stream, terminal control character input, resize-not-supported contract, and PTY EOF/drain.
- [ ] PTY child also owns a fresh process group/session as required by platform behavior; test descendants receive group signals.
- [ ] Application EOF remains unsupported for TTY and is rejected before enqueue.
- [ ] Commit `feat: support interactive pty sessions`.

### Task 4: Canonical output capture and quota failure

**Files:** `internal/adapter/process/capture.go`, `capture_test.go`, `capture_fault_test.go`.

- [ ] Test chunk boundaries, sink short/failure, per-session limit, global reserve rejection, ENOSPC seam, and reader failure.
- [ ] On first capture failure, record stable reason once, initiate TERM/KILL lifecycle, continue draining and discarding until EOF, then reap.
- [ ] Never publish terminal state from a reader goroutine; send evidence to one session coordinator.
- [ ] Commit `feat: finalize output capture safely`.

### Task 5: Bounded stdin queue and retry ledger

**Files:** `internal/core/session/input.go`, `input_test.go`, `input_fuzz_test.go`, `internal/adapter/process/writer.go`, `writer_test.go`.

- [ ] Table-test exact duplicate, hash/length mismatch, old offset, gap, queue full, UTF-8 byte counts, EOF duplicate/order, TTY EOF rejection, terminal/finalizing rejection.
- [ ] Implement serialized all-or-nothing admission and a bounded FIFO. Ledger stores kind/offset/length/SHA-256 only.
- [ ] One writer goroutine handles partial writes/EINTR until each accepted buffer is delivered. It advances delivered bytes only after OS success and closes non-TTY stdin only after the ordered EOF marker.
- [ ] Permanent delivery failure notifies coordinator, terminates owned group, and prevents success even after exit code 0.
- [ ] Commit `feat: make session input retry safe`.

### Task 6: Idempotent kill and timeout escalation

**Files:** `internal/core/session/kill.go`, `kill_test.go`, `internal/adapter/process/signal_unix.go`, `signal_unix_test.go`, `timeout.go`, `timeout_test.go`.

- [ ] Test same kill ID/signal replay sends once, conflicting signal, terminal no-op, signal failure replay, and new ID deliberate escalation.
- [ ] Send only to the live handle's process group. Reject signaling after ownership capability is gone.
- [ ] Timeout: deadline -> TERM once -> grace timer -> KILL once if still live. Explicit KILL skips grace. Record initiator separately from observed exit.
- [ ] Commit `feat: deduplicate signals and timeouts`.

### Task 7: Session coordinator and terminal truth

**Files:** `internal/app/daemon/session.go`, `session_test.go`, `session_race_test.go`.

- [ ] Drive allowed paths: spawn failure; success; nonzero exit; timeout; explicit kill; capture failure; input failure; shutdown kill.
- [ ] State is externally `finalizing` after exit/cause known until child reaped, every reader EOF, writer accounting resolved, and store terminal publication returns durable.
- [ ] If terminal publication fails, retry with bounded exponential backoff and remain finalizing. If coordinator context ends, return unresolved evidence to daemon shutdown logic; never publish in memory only.
- [ ] Prove no goroutine leak and race-free concurrent poll/write/kill using synchronization hooks.
- [ ] Commit `feat: coordinate process finalization`.

### Task 8: Native process checkpoint

**Files:** `dev/test-impact.toml`, `docs/adr/0003-process-ownership.md`, `tests/integration/process_native_test.go`.

- [ ] ADR records live-handle capability, no persisted PID recovery, group signaling, PTY choice, and cancellation semantics.
- [ ] Run focused native process suite, `go test -race ./internal/adapter/process ./internal/app/daemon ./internal/core/...`, dirty verification, and leak/deadline stress loop.
- [ ] On Linux, cross-build `GOOS=darwin GOARCH=arm64 go test -c ./internal/adapter/process` but label it compile-only. macOS native tests remain required user/release evidence.
- [ ] Commit `test: prove process runtime checkpoint`.

## Completion gate

Checkpoint 3 requires real-host evidence for process groups, pipes, PTY, stdin retries, timeout/kill, drain and reap. It is **native runtime ready on the tested OS**, not daemon/MCP/V1 complete.
