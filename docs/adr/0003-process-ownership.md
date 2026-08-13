# ADR 0003: Live process ownership capability

Only the current daemon incarnation holds a `ProcessHandle`. The handle wraps the live child/process-group primitives and is never serialized. Non-TTY children use one process group, a stdin pipe, and a merged stdout/stderr pipe. TTY children use `creack/pty`; API EOF is rejected for PTY sessions.

Request cancellation cancels waiting only. Signals target the live process group through the handle. Final state is published after wait/reap, output EOF, writer accounting, and durable receipt. Persisted PID/PGID diagnostic data cannot recreate a handle.
