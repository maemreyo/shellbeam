# ShellBeam V1 Design

**Status:** Reviewed design and development baseline, ready for implementation planning
**Date:** 2026-08-13
**Tagline:** Your local shell, available to ChatGPT.

## 1. Purpose

ShellBeam gives ChatGPT Web access to the shell on a user's own macOS or Linux machine. ChatGPT remains the only reasoning and coding agent. ShellBeam does not implement planning, repository semantics, code intelligence, memory, skills, or another agent loop.

The product is intentionally narrow:

> ShellBeam is a reliable local process runtime exposed through one MCP tool and reached through OpenAI Secure MCP Tunnel.

With ordinary shell commands, ChatGPT can inspect repositories, edit files, run tests, start development servers, use Git and GitHub CLI, invoke Docker, and operate any other CLI installed for the current user.

## 2. Decisions

| Area                       | Decision                             |
| -------------------------- | ------------------------------------ |
| Product name               | ShellBeam                            |
| CLI/binary                 | `shellbeam`                          |
| User-facing plugin name    | ShellBeam — Local Shell for ChatGPT  |
| MCP tool                   | `local_shell`                        |
| Implementation language    | Go                                   |
| Initial platforms          | macOS and Linux                      |
| Reasoning agent            | ChatGPT only                         |
| Workflow layer             | ChatGPT Skills/Superpowers           |
| Execution authority        | Current OS user                      |
| Remote transport           | OpenAI Secure MCP Tunnel             |
| Tunnel target              | Local stdio command: `shellbeam mcp` |
| Privileged local transport | User-owned Unix domain socket        |
| Runtime owner              | Persistent per-user ShellBeam daemon |
| Tool count                 | One                                  |
| Windows support            | Deferred                             |

## 3. Non-goals

V1 will not provide:

- Pi Agent, another LLM, or another agent loop.
- Repository, Git, file, Docker, test, or GitHub-specific MCP tools.
- A command planner, semantic code index, or bundled workflow skills.
- A command blacklist or shell command parser.
- Containers, security profiles, dedicated OS accounts, or path allowlists.
- Windows, PowerShell, or ConPTY support.
- SSH, multi-machine routing, or team accounts.
- Recovery of a live command after the ShellBeam daemon exits.
- General binary or artifact transfer through MCP.
- A public marketplace distribution architecture.
- A web UI.
- Guaranteed containment of a descendant that deliberately escapes the owned POSIX process group with `setsid`/`setpgid`, or rollback of external effects launched through tools such as Docker and cloud CLIs.

## 4. Architecture and trust boundaries

```
ChatGPT Web + Skills
        |
        | MCP tool calls
        v
OpenAI-hosted tunnel endpoint
        |
        | outbound-only Secure MCP Tunnel
        v
tunnel-client
        |
        | stdio MCP
        v
shellbeam mcp             stateless bridge
        |
        | Unix socket; directory 0700, socket 0600, peer UID checked
        v
shellbeam daemon          session and process owner
        |
        | $SHELL -lc <command>
        v
POSIX process group / optional PTY
```

`shellbeam mcp` validates the MCP request, forwards a structured request over the local Unix socket, and maps the daemon response back to MCP. It holds no authoritative session or process state. It may be stopped and restarted without affecting running commands.

The persistent per-user daemon owns operation reservations, sessions, child processes, output capture, timeouts, signals, and receipts. A ChatGPT disconnect, MCP request cancellation, tunnel restart, or bridge restart does not terminate a command.

Loopback TCP is not an authorization boundary and is not used between the bridge and daemon. The runtime directory must be owned by the daemon UID and mode `0700`; the socket must be mode `0600`. On connection, the daemon verifies that the peer UID equals its own UID using `SO_PEERCRED` on Linux and `getpeereid` or the platform-equivalent peer credential API on macOS. A permission or identity mismatch is rejected before any request is decoded.

OpenAI Secure MCP Tunnel is transport only. It supports a local MCP target over stdio, so ShellBeam does not need a local HTTP listener or a custom remote transport.

## 5. MCP server contract

ShellBeam advertises exactly one tool, `local_shell`, and no resources or prompts in V1.

### 5.1 Server instructions

The first 512 characters of the MCP initialization `instructions` field must be self-contained and begin with this guidance:

> ShellBeam runs commands as the local OS user with full authority. Use it only for intended local execution. For start, create one operation\_id and reuse it for every retry; if the outcome is unknown, never create another. Poll with session\_id and cursor. For write, use next\_input\_offset; acceptance means queued, while the terminal receipt proves delivery. For kill, create one kill\_id and reuse it. Never infer command success from MCP success; require a terminal receipt and spawn/exit evidence.

The remaining instructions document the four actions, terminal states, retry rules, cursor behavior, and the fact that MCP transport success is not command success.

### 5.2 Tool input schema

The tool input schema is a closed union. Every branch uses `additionalProperties: false`; fields from another action are rejected rather than ignored.

```
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "local_shell input",
  "oneOf": [
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["action", "operation_id", "command", "cwd"],
      "properties": {
        "action": { "const": "start" },
        "operation_id": { "type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$" },
        "command": { "type": "string", "minLength": 1 },
        "cwd": { "type": "string", "pattern": "^/" },
        "tty": { "type": "boolean", "default": false },
        "yield_time_ms": { "type": "integer", "minimum": 0, "default": 10000 },
        "timeout_ms": { "type": "integer", "minimum": 0, "default": 0 },
        "max_output_bytes": { "type": "integer", "minimum": 0, "default": 20000 }
      }
    },
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["action", "session_id"],
      "properties": {
        "action": { "const": "poll" },
        "session_id": { "type": "string", "minLength": 1 },
        "cursor": { "type": "integer", "minimum": 0, "default": 0 },
        "yield_time_ms": { "type": "integer", "minimum": 0, "default": 0 },
        "max_output_bytes": { "type": "integer", "minimum": 0, "default": 20000 }
      }
    },
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["action", "session_id", "input_offset"],
      "properties": {
        "action": { "const": "write" },
        "session_id": { "type": "string", "minLength": 1 },
        "input_offset": { "type": "integer", "minimum": 0 },
        "chars": { "type": "string", "minLength": 1 },
        "eof": { "const": true }
      },
      "oneOf": [
        { "required": ["chars"], "not": { "required": ["eof"] } },
        { "required": ["eof"], "not": { "required": ["chars"] } }
      ]
    },
    {
      "type": "object",
      "additionalProperties": false,
      "required": ["action", "session_id", "kill_id"],
      "properties": {
        "action": { "const": "kill" },
        "session_id": { "type": "string", "minLength": 1 },
        "kill_id": { "type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$" },
        "signal": { "type": "string", "enum": ["INT", "TERM", "KILL"], "default": "TERM" }
      }
    }
  ]
}
```

The server also enforces configured upper bounds for yields, timeouts, command length, stdin payload and queue size, and response size; JSON Schema alone is not the resource policy.

### 5.3 Tool output and error schema

The tool publishes a closed `outputSchema` and returns the same data in `structuredContent`; the text `content` is only a short human-readable summary. Successful transport does not imply successful command execution.

Every response is one branch of a discriminated union:

- `ok=true`, `action=start|poll`: `operation_id` for `start`, `session_id`, state, outcome, byte cursor range, model-visible output, truncation flag, and the latest receipt.
- `ok=true`, `action=write`: `session_id`, `accepted_input_bytes`, authoritative `next_input_offset`, queued EOF state, and the latest receipt.
- `ok=true`, `action=kill`: `session_id`, `kill_id`, requested signal, signal-attempt state, session state, and the latest receipt.
- `ok=false`: an error object with stable `code`, concise `message`, actionable `hint`, `retryable`, and optional closed `details` specific to that code.

Unknown output fields are rejected in contract tests. Schema versions are explicit. A terminal command failure, timeout, kill, ambiguity, or failed spawn is represented by `ok=true` plus its receipt because the tool operation itself succeeded; pre-execution validation, authorization, capacity, persistence, IPC, and protocol failures use `ok=false` with MCP `isError=true`.

### 5.4 Tool annotations

The universal shell can mutate or delete local data and affect external systems. Its annotations are deliberately conservative:

```
{
  "readOnlyHint": false,
  "destructiveHint": true,
  "openWorldHint": true,
  "idempotentHint": false
}
```

Individual operations have retry semantics described below, but the mixed-action tool as a whole must not claim to be idempotent.

## 6. Action semantics and retry safety

### 6.1 `start`

`operation_id` is a caller-generated idempotency key. ChatGPT must generate it once for an intended start and reuse it unchanged for every transport retry.

Before spawning a process, the daemon atomically serializes creation for that `operation_id` and:

1. Validates the caller fields and resolves caller defaults.
2. Computes a SHA-256 intent fingerprint over only execution-affecting caller data: exact command bytes, the exact validated absolute `cwd` string, `tty`, and effective `timeout_ms`. `operation_id`, timestamps, `yield_time_ms`, and per-response `max_output_bytes` are excluded.
3. Returns the existing operation immediately if one is already reserved, after comparing the stored intent fingerprint.
4. For a new operation, acquires a concurrent-session capacity token. If none is available, it returns `capacity_exceeded` without reserving an operation and without spawning.
5. Reserves bounded control-plane storage for the live session's metadata and terminal receipt. If the state/free-space budget cannot provide it, it releases capacity, returns `persistence_unavailable`, and creates no operation or process.
6. Resolves the shell and effective server-side execution limits, creates a globally unique time-sortable `session_id`, and durably records and syncs the complete `operation_id -> session_id + intent fingerprint + effective execution config` reservation.
7. If reservation or sync fails, releases the capacity and control-plane reservations, returns `persistence_unavailable`, and does not spawn.
8. Only after the reservation is durable, attempts to spawn the recorded shell with `-lc <command>` in a new process group. The reservations remain held until the session is terminal.

The result rules are:

- Same `operation_id` and same intent fingerprint: return the already-bound session and its current receipt; never spawn again. The caller may change `yield_time_ms` or `max_output_bytes` on a retry because these only tune that response.
- Same `operation_id` and different fingerprint: return `operation_conflict`.
- A reserved operation whose outcome cannot be proved after daemon failure becomes `abandoned` with outcome `ambiguous`; never automatically start a replacement.
- Spawn failure is a terminal `failed` receipt with `failure_reason=spawn_failed` and is returned on every retry of that operation; it is not misreported as a pre-execution tool error.

Retries are compared with the caller-intent fields stored in the first reservation, not with the daemon's current environment or configuration. A later shell or quota configuration change therefore cannot turn a valid replay into `operation_conflict` or alter the reserved session.

The call returns when the process reaches a terminal state or `yield_time_ms` elapses. It always returns `operation_id` and `session_id`, including when the command completes during the initial yield.

### 6.2 `poll`

`poll` is read-only and idempotent. It returns output beginning at `cursor`, bounded only in the response by `max_output_bytes`, plus `next_cursor`, current state, and the latest receipt. Repeating a poll with the same cursor returns the same canonical byte range while the session is retained.

A cursor equal to the current canonical length may wait for more output or a state change. A cursor beyond the current length returns `cursor_out_of_range` with the authoritative current end; it never waits for missing bytes. If output was removed by retention, `output_unavailable` returns the compact terminal receipt and does not pretend that an empty stream is the original output.

A positive `yield_time_ms` permits bounded long polling. Canceling the MCP request cancels only the wait, never the command.

### 6.3 `write`

Each session has a bounded FIFO input queue, a monotonically increasing `next_input_offset`, and one dedicated OS writer. Write admission is serialized; OS delivery is not performed while holding the session lock.

For `chars`:

- `input_offset == next_input_offset` and the whole UTF-8 payload fits the queue: atomically record `(kind=chars, offset, byte_length, sha256)`, enqueue the whole byte slice, and advance the offset by its byte length.
- The offset, length, and hash exactly match an accepted ledger entry: acknowledge a duplicate without enqueuing or writing again.
- `input_offset < next_input_offset` with no exact ledger match: return `input_conflict` with the authoritative offset.
- `input_offset > next_input_offset`: return `input_gap` with the authoritative offset.
- The whole payload does not fit the bounded queue: return retryable `input_backpressure`; record nothing and do not advance the offset.
- stdin has already accepted EOF: return `input_closed`.

For `eof=true`, `input_offset` must equal `next_input_offset`. On a non-TTY session, ShellBeam records a zero-byte EOF ledger entry, closes admission, and queues an ordered stdin-close marker after all prior bytes. An exact duplicate EOF is acknowledged. On a TTY session it returns `input_eof_unsupported` without accepting anything; terminal EOF is a line-discipline control sequence, so the caller must send an appropriate character such as `\u0004` when that is the intended terminal behavior.

The application-level response is all-or-nothing: `accepted_input_bytes` is either the entire `chars` byte length or zero, and `next_input_offset` is authoritative. The dedicated writer handles partial `write(2)` results internally until each accepted buffer is fully delivered; this prevents a transport retry from duplicating bytes and avoids exposing an invalid UTF-8 continuation as a new JSON string.

The receipt tracks accepted and delivered input byte counts plus stdin-closed state. If a permanent OS delivery error occurs, or the child closes stdin with accepted bytes still outstanding, ShellBeam records `failure_reason=input_delivery_failed`, terminates the live owned process group if necessary, drains/reaps it, and never reports outcome `success` even if the observed process exit code is zero.

The ledger stores offsets, lengths, kinds, and hashes, not stdin contents. V1 provides duplicate suppression while the owning daemon remains alive. After a daemon crash the entire nonterminal session is abandoned and further writes are rejected, so V1 does not falsely claim exactly-once input across crash recovery.

### 6.4 `kill`

`kill_id` is a caller-generated idempotency key scoped to the session. Before signaling, the daemon records `(kill_id, signal, attempt state)` in the live session ledger.

- The first `kill_id` attempt sends the requested signal once to the process group owned by the current daemon incarnation, not only the shell parent.
- The same `kill_id` and signal replays the recorded attempt and current receipt without sending again, including when the first response was lost.
- The same `kill_id` with another signal returns `kill_conflict`.
- A definitive `signal_failed` result is replayed for that ID. A later deliberate signal attempt requires a new `kill_id`.
- A session already terminal returns its receipt without signaling and records that no attempt was needed.

The requested signal, signal-attempt evidence, and observed exit evidence remain separate. Kill deduplication is guaranteed while the owning daemon lives; after daemon failure a nonterminal session is abandoned and no stale process identifier is signaled.

ShellBeam never signals a PID or PGID reconstructed only from disk.

## 7. State and result contract

Session states are monotonic:

- `starting`: the operation reservation exists and spawn is not yet resolved.
- `running`: the child was started and is owned by this daemon incarnation.
- `finalizing`: spawn or exit outcome is known, and any required output drain/input accounting is finishing or the terminal receipt is awaiting durable publication. It is nonterminal and accepts no new input or signal attempt.
- `completed`: the child was reaped with exit code 0 and output drain completed.
- `failed`: spawn failed, the child was reaped with a non-zero exit code, or it had already exited before accepted input could be completely delivered; required drain and receipt publication completed.
- `timed_out`: timeout initiated termination, the child was reaped, and output drain completed.
- `killed`: explicit kill, shutdown, or accepted-I/O failure initiated termination; the child was reaped and output drain completed.
- `abandoned`: the previous daemon exited before it durably proved a terminal result and V1 cannot reattach to the process.

An `abandoned` session has outcome `ambiguous`. It does not assert that the command did or did not run, finish, or produce external effects. ChatGPT must surface that uncertainty and must not invent a new `operation_id` to repeat the command automatically.

Final outcome values are `success`, `failure`, `timeout`, `killed`, and `ambiguous`. A successful MCP call is not evidence of command success. For a started command, only a durably published terminal receipt with `exit_evidence.reaped=true`, the recorded exit status, `output_complete=true`, and no incomplete accepted input is authoritative success. A spawn failure instead records `spawn_evidence.attempted=true` and `spawn_evidence.succeeded=false`. `abandoned` is terminal for V1 but never counts as successful execution.

Normal process completion follows `running -> finalizing -> completed|failed`; timeout and termination follow `running -> finalizing -> timed_out|killed`. Failed spawn follows `starting -> finalizing -> failed`. If terminal publication cannot be synced, the daemon keeps the session in `finalizing`, retains evidence in memory, and retries with bounded backoff. If that daemon then exits, startup applies the V1 daemon-failure rule and records `abandoned/ambiguous` rather than inventing a terminal result.

## 8. Output capture and quotas

Each session uses a canonical append-only raw byte stream in `output.log`. `cursor` and `next_cursor` are byte offsets into that stream.

Three layers of limits have different meanings:

- `max_output_bytes` bounds bytes returned by one `start` or `poll` response. It never deletes or truncates the canonical log.
- `max_session_output_bytes` is a daemon-configured disk limit for one session's canonical log.
- `max_total_state_bytes`, `min_free_space_bytes`, and a per-live-session control-plane reservation form a daemon-wide admission budget across output, metadata, and receipts. Session/control and output reservations are serialized so concurrent writers cannot knowingly cross the configured reserve; output is never allowed to consume the receipt headroom reserved when the session starts.

stdout and stderr are merged in observed read order. PTY sessions naturally expose one combined stream. Response boundaries do not split a valid UTF-8 sequence. Invalid UTF-8 is replaced with `U+FFFD` only in model-visible text; cursor accounting uses unmodified raw bytes.

If the session disk limit is reached, the global storage budget would be crossed, the disk becomes full, or capture otherwise fails, ShellBeam:

1. Marks capture as failed in memory and best-effort durable metadata.
2. Sends `TERM` to the owned process group, waits the configured grace period, then sends `KILL` if necessary.
3. Continues draining and discarding unread pipe or PTY bytes until EOF so process completion cannot deadlock.
4. Reaps the child.
5. Only then publishes the terminal receipt.

The receipt records `output_complete=false` and:

- `failure_reason=output_limit_exceeded` when the per-session quota was reached.
- `failure_reason=storage_reserve_exhausted` when a new chunk would cross the daemon-wide state or free-space reserve.
- `failure_reason=output_capture_failed` for ENOSPC or another capture/storage failure.

The daemon checks the global reserve before accepting each output chunk and terminates the owned group before an actual ENOSPC condition where possible. ENOSPC remains a required fallback path because another process or filesystem behavior can consume space between the check and write. The control-plane reservation is sized to persist bounded terminal metadata and a receipt; if an external ENOSPC still prevents publication, the session remains `finalizing` and the daemon retries instead of exposing an unpersisted terminal state.

When capture remains intact, `output_complete=true` is published only after the child has been reaped and all stdout/stderr or PTY readers have reached EOF, including for non-zero exit, timeout, and explicit kill. No observed-exit terminal state is externally visible before both conditions hold. `abandoned` is the explicit exception: it reports that ShellBeam cannot obtain that evidence.

## 9. Persistence and receipts

The daemon stores state under a user-owned root:

```
<state-root>/
  daemon.json
  operations/<operation-id>.json
  sessions/<session-id>/
    metadata.json
    output.log
    receipt.json
```

Operation reservations and state transitions use atomic replacement, file sync, and parent-directory sync where the platform requires it. A terminal receipt is immutable once published. Retention may remove an old terminal session's output and bulky diagnostics, but it retains a compact session receipt and operation tombstone so a replay cannot spawn again and can return `output_unavailable` honestly. Operation tombstones are never automatically forgotten in V1; only the explicit user-authorized `shellbeam uninstall --purge` destroys idempotency history. Retention never selects a live or `finalizing` session. If compact history itself reaches the configured state budget, new starts fail closed with `persistence_unavailable`; `doctor` explains whether to raise the budget or intentionally purge instead of silently forgetting keys.

Every action response embeds the latest receipt snapshot. The final persisted receipt schema starts at version 1 and includes at least:

```
{
  "schema_version": 1,
  "operation_id": "op_...",
  "session_id": "01J...",
  "intent_fingerprint_sha256": "...",
  "command_sha256": "...",
  "daemon_incarnation_id": "inc_...",
  "shell": "/bin/zsh",
  "cwd": "/absolute/path",
  "tty": false,
  "timeout_ms": 0,
  "max_session_output_bytes": 268435456,
  "state": "completed",
  "outcome": "success",
  "started_at": "2026-08-13T11:30:00Z",
  "finished_at": "2026-08-13T11:30:02Z",
  "duration_ms": 2000,
  "output_bytes": 1842,
  "output_complete": true,
  "input_accepted_bytes": 12,
  "input_delivered_bytes": 12,
  "stdin_closed": false,
  "failure_reason": null,
  "termination_cause": null,
  "escalation": [],
  "kill_attempts": [],
  "spawn_evidence": {
    "attempted": true,
    "succeeded": true,
    "error_code": null
  },
  "exit_evidence": {
    "reaped": true,
    "exit_code": 0,
    "signal": null,
    "observed_at": "2026-08-13T11:30:02Z"
  }
}
```

`termination_cause` distinguishes timeout, shutdown, explicit kill, input-delivery failure, output limit, storage reserve, and capture failure from the process's observed exit. `escalation` is an ordered list of signal and timestamp records, for example `TERM` followed by `KILL`. `kill_attempts` records kill ID, requested signal, whether a syscall was attempted, and its stable result without exposing raw OS text. `exit_code` and observed `signal` are mutually exclusive when platform evidence permits. Before terminal publication, `output_complete`, `finished_at`, and final exit evidence may be null in receipt snapshots. A failed spawn has no child to reap, so its authoritative evidence is `spawn_evidence`; its `exit_evidence` remains null.

The command hash is always retained. Raw command text is stored in protected operation metadata because exact-request idempotency and local diagnostics require it; it is omitted from the receipt and from operator diagnostics by default. Terminal compaction may discard raw command text after preserving the intent fingerprint and terminal summary. Command output remains local unless returned through `start` or `poll`.

### V1 daemon-failure rule

V1 commands survive ChatGPT, request, bridge, and tunnel failures only while the daemon remains alive. On daemon startup, any prior nonterminal session is marked `abandoned/ambiguous`. The new daemon does not inspect a recorded PID or PGID to infer ownership, does not signal it, and does not automatically rerun it.

## 10. Process lifecycle

- Every command starts in a new POSIX process group.
- Timeout sends `TERM`, waits a configurable grace period, then sends `KILL` if the owned group remains active.
- A clean daemon shutdown applies the same sequence to every process group it owns and waits for reap and output drain before exiting.
- Capture or accepted-input delivery failure uses the same escalation path.
- Completion collection reaps every direct child and avoids zombies.
- Concurrent commands are bounded; a new start acquires capacity before durable reservation. Capacity rejection creates neither a session nor an operation reservation and is retryable with the same `operation_id`.
- Per-session input queues and daemon-wide queued-input bytes are bounded. Queue admission is atomic, and the dedicated writer is joined before terminal input accounting is published.
- Ownership in V1 exists only in memory for children started by the current daemon incarnation. Disk metadata is evidence, not an ownership capability.
- Signals target the owned POSIX process group. A descendant that deliberately creates a new session or process group can escape that boundary; V1 neither claims to contain it nor attempts unsafe PID discovery. External effects started by the command are likewise not rolled back.

## 11. Binary commands and service ownership

One Go binary provides:

```
shellbeam daemon
shellbeam mcp
shellbeam install
shellbeam uninstall
shellbeam status
shellbeam doctor
shellbeam version
```

### `daemon`

Runs the persistent process runtime on the protected Unix socket. It exposes no TCP listener in V1.

### `mcp`

Runs the stateless MCP stdio bridge intended as the `tunnel-client` command. Loss of stdin, stdout, or the tunnel connection terminates only the bridge, not daemon sessions.

### `install` and `uninstall`

- macOS: manage only the ShellBeam per-user LaunchAgent.
- Linux: manage only the ShellBeam per-user systemd unit.
- Never install as root or as a system-wide service in V1.
- Preserve user data on ordinary uninstall unless an explicit purge option is given.
- Do not install, supervise, bundle, configure credentials for, or uninstall `tunnel-client`.

`tunnel-client run` is a separate prerequisite and must remain healthy for ChatGPT discovery and calls. Its service management and runtime credential are owned by the user or operator.

### `doctor`

Checks:

- supported OS and architecture;
- shell availability;
- runtime/state directory ownership and permissions;
- daemon service state and Unix socket peer-auth round trip;
- MCP initialization, server instructions, schema, and tool discovery through `shellbeam mcp`;
- `tunnel-client` binary availability, configured profile diagnostics, and health/readiness when a profile is supplied.

`doctor` may invoke supported `tunnel-client` diagnostic commands, but it never reads, stores, prints, or manages the tunnel runtime credential. Diagnostics redact environment values, raw commands, command output, and credential-like data by default.

## 12. Configuration

V1 configuration is a small user-owned file plus command-line overrides:

- runtime socket and state roots;
- shell override;
- maximum concurrent sessions;
- default and maximum initial/poll yield;
- default and maximum response output bytes;
- maximum stdin bytes per call, queued bytes per session, and total queued input bytes;
- `max_session_output_bytes`;
- `max_total_state_bytes` and `min_free_space_bytes`;
- control-plane reserve per live session;
- terminal output/diagnostic retention duration; compact receipts and operation tombstones are not automatically expired;
- execution timeout bounds;
- termination grace period.

ShellBeam inherits the daemon service user's environment as established by launchd or systemd. It does not accept arbitrary environment maps through MCP in V1; callers may set per-command variables using ordinary shell syntax.

The shell is resolved once for a new reservation using this precedence: daemon command-line override, configuration file, a non-empty absolute executable from the daemon's `SHELL`, the current account's login shell, then `/bin/sh`. The resolved path is validated and persisted before spawn; retries reuse it rather than resolving again.

## 13. Security model

ShellBeam deliberately grants ChatGPT the practical authority of the OS user running the daemon.

Consequences:

- Commands can read user-accessible files and credentials.
- Commands can edit or delete files.
- Commands can access the network through installed programs.
- Commands can commit, push, publish, deploy, or invoke cloud CLIs when credentials are available.

Local protections establish a same-user boundary:

- No local TCP listener exists.
- The Unix socket parent is `0700`, the socket is `0600`, and peer UID is verified.
- The daemon accepts commands only from its own OS UID.
- Remote reachability exists only through the configured OpenAI tunnel identity and the local stdio bridge.
- Tool annotations and server instructions truthfully describe destructive and open-world authority.
- ShellBeam does not implement an ineffective command blacklist.
- Logs and diagnostics do not automatically include environment dumps or stdin contents.

This boundary prevents another OS user on the same machine from invoking ShellBeam. It does not defend against a malicious process already running as the same user; that process already has comparable access to the user's files, sockets, and credentials. Containers and security profiles are intentionally deferred because they would change the V1 contract from current-user authority.

## 14. Stable failures

Protocol/tool failures use stable codes:

- `invalid_request`
- `invalid_action`
- `invalid_cwd`
- `operation_conflict`
- `session_not_found`
- `session_not_running`
- `session_abandoned`
- `cursor_out_of_range`
- `input_conflict`
- `input_gap`
- `input_backpressure`
- `input_closed`
- `input_eof_unsupported`
- `kill_conflict`
- `signal_failed`
- `capacity_exceeded`
- `persistence_unavailable`
- `output_unavailable`
- `peer_unauthorized`
- `internal_error`

`spawn_failed`, `input_delivery_failed`, `output_limit_exceeded`, `storage_reserve_exhausted`, and `output_capture_failed` are terminal receipt failure reasons, not pre-execution tool errors. Input delivery and the latter three output/storage failures terminate a live command when ShellBeam cannot preserve the accepted I/O contract.

Failures include a concise model-readable explanation and a human-actionable hint without exposing secrets. An MCP/runtime error is never presented as a command exit result.

## 15. Testing strategy

### Unit and property tests

- Closed four-branch JSON Schema, exclusive `chars`/`eof` write variants, required kill IDs, and server-side bounds.
- Closed output/error union, `structuredContent` conformance, and schema-version compatibility.
- Canonical intent fingerprint stability and sensitivity, including proof that yield and response-size changes do not conflict.
- `operation_id` replay, conflict, capacity-before-reservation, reservation-before-spawn, persistence-failure no-spawn, and spawn-failure replay.
- Input offset duplicate/conflict/gap, all-or-nothing queue admission, backpressure, EOF, concurrent admission, and internal partial-syscall behavior.
- Kill-ID replay/conflict and separation of requested signal, attempted signal, and observed exit.
- Monotonic transitions through `finalizing`, durable immutable final receipts, and persistence-retry behavior.
- Cursor slicing, UTF-8 boundaries, beyond-end rejection, response truncation, and retained-receipt/output-unavailable behavior.
- Per-session quota, concurrent global-storage reserve, ENOSPC/capture failure mapping, and TERM/KILL escalation.
- Receipt schema version and required evidence fields.
- Retention never selecting live/finalizing sessions and compact tombstone replay never spawning again.

### Integration tests

- Exit 0, non-zero exit, and spawn failure propagation.
- `start` completing within yield and returning while still running.
- Lost `start` response followed by same-ID replay without a second process.
- Same `operation_id` with changed request returning `operation_conflict`.
- Lost `write` response followed by same-offset replay without duplicate bytes, including injected short OS writes across multi-byte UTF-8.
- stdin pipe and PTY round trips, pipe EOF ordering, TTY EOF rejection, queue backpressure, and child-closed-stdin failure evidence.
- Lost `kill` response followed by same-kill-ID replay without a duplicate signal, plus conflict and deliberate new-attempt behavior.
- `INT`, `TERM`, and `KILL` of owned process groups, with the deliberate process-group escape limitation documented and not overclaimed.
- Timeout and output-capture escalation, reap, and drain ordering.
- Large interleaved stdout/stderr output.
- Concurrent sessions, retryable capacity rejection without reservation, and admission after capacity becomes available.
- Durable-reservation write/sync failure proving that no spawn occurred, and terminal-receipt sync failure remaining `finalizing` until durable.
- Terminal output compaction followed by start replay and poll, proving no respawn and explicit `output_unavailable`.
- MCP bridge and tunnel disconnect/reconnect while a command continues.
- Another local UID cannot connect to the daemon socket.
- Daemon crash marks nonterminal operations abandoned without killing or rerunning a stale PID/PGID.

### Platform and end-to-end tests

- Current stable Ubuntu and macOS CI runners.
- Go race detector on supported CI lanes.
- MCP Inspector initialization, instructions, discovery, valid calls, and invalid calls.
- Real Secure MCP Tunnel stdio profile from ChatGPT Developer mode.
- LaunchAgent/systemd-user install, restart, status, doctor, and uninstall.
- `doctor` distinguishes ShellBeam daemon health from separate tunnel-client health.

## 16. V1 acceptance criteria

V1 is ready when:

1. A user installs one `shellbeam` binary on macOS or Linux.
2. `shellbeam install` manages a persistent per-user daemon and nothing belonging to `tunnel-client`.
3. A separately configured Secure MCP Tunnel stdio profile can run `shellbeam mcp`.
4. ChatGPT sees exactly one tool, `local_shell`, with closed action schemas and self-contained instructions.
5. Another OS user cannot invoke the daemon through its local socket.
6. ChatGPT can inspect and modify a local Git repository using ordinary shell commands.
7. A long-running command survives bridge/tunnel disconnection and resumes by `session_id` and cursor.
8. Replayed `start`, `write`, and `kill` requests do not duplicate their effects while the daemon lives.
9. Bounded stdin admission works with byte offsets; short OS writes never expose partial UTF-8, pipe EOF is ordered and idempotent, and accepted-but-undelivered input can never produce outcome `success`.
10. Per-session output quota, global storage reserve, or capture failure terminates the owned process group and produces an incomplete-output receipt.
11. No terminal receipt is visible before child reap, output drain, accepted-input accounting, and durable receipt publication; persistence delay is visible as `finalizing`.
12. Daemon crash produces `abandoned/ambiguous`, never automatic rerun or stale-PID ownership.
13. Every non-ambiguous terminal result contains authoritative spawn evidence and, when a child started, authoritative exit evidence.
14. Capacity and persistence rejection cannot create an unrecorded process, while a persisted failed spawn replays as the same terminal receipt.
15. Output retention preserves compact receipts and operation tombstones, so replay cannot respawn even after output removal.
16. Unit, integration, macOS, Linux, MCP Inspector, and tunnel end-to-end release gates pass.

## 17. Focused roadmap

### V1.1: crash recovery and strong process ownership

V1.1 has one theme: keep a command observable and controllable across daemon restart without trusting recycled process identifiers.

Each command is owned by a small per-session supervisor. The supervisor is a process-runtime component, not an agent: it owns the child process group, PTY or pipes, output log, input ledger, and final exit evidence. The daemon reconnects through a per-session Unix socket using a high-entropy capability stored with user-only permissions.

The daemon may reclaim a session only after a live supervisor proves the matching session identity and capability. It must never claim ownership from PID or PGID values read from disk. If the supervisor is absent, the capability handshake fails, or ownership cannot be proved, the session becomes `abandoned/ambiguous` and ShellBeam sends no signal to the recorded process identifier.

V1.1 must extend the V1 idempotency and receipt contracts rather than replace them. Host reboot recovery remains limited to terminal evidence already persisted by the supervisor.

### V1.2, only if evidence justifies it

Add bounded artifact access within `local_shell` only if end-to-end usage shows that screenshots, PDFs, or other binary outputs regularly cannot be handled through existing text output and user workflows. It must have explicit byte, path, type, and retention limits and must not grow into a general file-tool suite.

### Deferred

| Area                                   | Decision                                                                                                               |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Security profiles and containers       | Defer; they change the current-user authority contract.                                                                |
| SSH and multiple machines              | Defer; they add routing, identity, and new failure domains.                                                            |
| Hosted gateway and public distribution | Defer; Secure MCP Tunnel is for private connectivity, while public plugins require a stable public HTTPS MCP endpoint. |
| Windows and ConPTY                     | Defer; this is effectively another process-runtime implementation.                                                     |
| Agent workers or additional LLMs       | Remain out of scope unless the one-brain design fails a demonstrated use case.                                         |

## 18. Official references

- [OpenAI Secure MCP Tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels): private outbound tunnel behavior, stdio/HTTP targets, separate `tunnel-client` health, and public-distribution boundary.
- [Build your MCP server](https://developers.openai.com/plugins/build/mcp-server): tool names, descriptions, schemas, annotations, structured output, and self-contained server instructions.
- [Test and connect your MCP server](https://developers.openai.com/plugins/deploy/connect-chatgpt): MCP Inspector, schema and annotation inspection, prompt evals, and Secure MCP Tunnel testing.
- [ChatGPT Developer mode](https://developers.openai.com/api/docs/guides/developer-mode): connecting and testing custom MCP apps in ChatGPT.

## 19. Development policy

ShellBeam is a small, security-sensitive modular monolith. The development policy deliberately combines hard limits for objective drift with review thresholds for design judgment:

- Keep one Go module and one shipped binary. Do not introduce services, plugins, code generation pipelines, or distributed components without a demonstrated V1 requirement.
- Prefer the standard library and explicit composition. A dependency or abstraction must remove a named risk or implement a named boundary.
- Put correctness in executable contracts: state transitions, idempotency, persistence ordering, resource bounds, import barriers, and schemas must be machine-checked.
- Keep the inner edit loop focused and cache-friendly. Full fresh tests and cross-platform builds are authoritative release gates, not reflexive per-edit commands.
- If affected-scope selection is uncertain, fail closed by broadening the scope and explaining why; never silently skip a possible consumer.
- Make the smallest coherent change. Do not mix feature work with unrelated cleanup, renaming, dependency upgrades, or formatting churn.

Normative terms such as **must**, **must not**, and **only** below are gates. A **target** guides normal code; a **review threshold** requires an explicit design check but does not force a bad split.

## 20. Technology baseline

### 20.1 Go and platform baseline

| Area | Baseline |
| --- | --- |
| Language | Go 1.26 language/module level |
| Initial pinned toolchain | Go 1.26.5; update to a later stable patch through a reviewed toolchain PR |
| Module | One root `go.mod`; no workspace or nested modules in V1 |
| Targets | `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64` |
| C toolchain | `CGO_ENABLED=0` unless a measured requirement and platform plan justify an exception |
| Internal IPC | Versioned HTTP/1.1 + closed JSON envelopes over the protected Unix socket, using `net/http` with a Unix-socket dialer |
| Persistence | Versioned JSON metadata/receipts plus append-only raw output files; atomic replace, sync, and directory sync; no database in V1 |
| Configuration | Small TOML file, immutable after startup, with CLI override precedence and validation in one place |
| Logging | Standard-library `log/slog` with stable event names and mandatory redaction |

The initial `go.mod` should declare:

```go
go 1.26.0
toolchain go1.26.5
```

CI installs the declared toolchain explicitly rather than silently floating. Toolchain changes, OS minimums, persisted formats, and MCP protocol changes are separate reviewable changes.

### 20.2 Dependency policy

The approved starting set is intentionally narrow:

| Dependency | Scope | Reason |
| --- | --- | --- |
| `github.com/modelcontextprotocol/go-sdk/mcp` | Runtime, MCP adapter only | Official MCP Go SDK; initially pin v1.7.0 and negotiate protocol versions |
| `golang.org/x/sys/unix` | Runtime, platform adapters only | Peer credentials and narrow POSIX operations missing from the standard library |
| `github.com/creack/pty` | Runtime, process adapter only | Cross-platform Unix PTY creation and resize behavior |
| `github.com/oklog/ulid/v2` | Runtime, identity adapter only | Time-sortable session IDs with explicit cryptographic entropy |
| `github.com/pelletier/go-toml/v2` | Runtime, config adapter only | Strict TOML decoding for the small user configuration |
| `github.com/google/go-cmp/cmp` | Tests only | Readable structural comparisons without production coupling |

Rules:

- Pin exact versions in `go.mod`/`go.sum`; no floating install commands in CI.
- Keep MCP SDK types inside `internal/adapter/mcp`. Domain, application, IPC, and persistence contracts use ShellBeam-owned types.
- SDK lifecycle names and protocol revisions are adapter details. Contract and end-to-end tests protect ShellBeam when the SDK changes.
- A new runtime dependency requires a written capability, alternatives considered, license, maintenance/security review, binary-size impact, and removal plan. A transitive-heavy dependency needs stronger evidence.
- Prefer `flag` over Cobra, explicit constructors over a dependency-injection framework, `slog` over a logging framework, and direct files over an ORM/database.
- Do not add a global event bus, service locator, reflection-based validation, generic repository layer, or an abstraction whose only purpose is to make a one-line call look uniform.
- Pin developer tools using Go's `tool` directives or an isolated tools module; production code must not import them.

## 21. Repository organization and import barriers

### 21.1 Initial layout

```text
api/
  schema/                 # checked-in MCP, IPC, receipt, and config schemas
cmd/
  shellbeam/              # composition root and CLI dispatch only
internal/
  core/
    operation/            # idempotency intent and reservation invariants
    session/              # states, transitions, outcomes, and ownership
    receipt/              # evidence and immutable terminal result model
  app/
    daemon/               # start, poll, write, kill, retention use cases and ports
    bridge/               # stateless MCP-to-daemon use cases and ports
  adapter/
    mcp/                  # official SDK and MCP schema mapping
    ipc/                  # Unix-socket HTTP/JSON client and server
    process/              # exec, process group, PTY, signals, reap, and drain
    store/                # durable reservations, metadata, output, and receipts
    service/              # launchd and systemd-user installation
  config/                 # typed config, defaults, precedence, and validation
  observability/          # slog event definitions and redaction
  testkit/                # test-only fakes, fixtures, clocks, and fault injection
tools/
  devctl/                 # affected-scope, quality, build, and evidence driver
tests/
  contract/               # schemas, compatibility, architecture, and CLI contracts
  integration/            # real process, PTY, filesystem, and Unix-socket boundaries
  e2e/                     # service manager, MCP Inspector, and tunnel scenarios
dev/
  architecture.toml       # machine-readable allowed/denied import edges
  test-impact.toml        # non-Go change-to-suite mapping and global triggers
  quality-waivers.toml    # bounded, expiring exceptions
docs/
  adr/                    # decisions that change a durable boundary
```

Use capability names, not grab-bag names. Do not create `utils`, `helpers`, `common`, `shared`, `base`, `misc`, or a generic `models` package. Keep platform code beside the adapter it implements in `*_linux.go` and `*_darwin.go`; use build tags only where file suffixes are insufficient. Do not create `pkg/` unless ShellBeam intentionally publishes a supported Go library.

### 21.2 Enforced dependency direction

An import barrier is a machine-checked dependency rule, not a naming convention.

| From | May import | Must not import |
| --- | --- | --- |
| `internal/core/...` | Standard library and explicitly allowlisted core primitives | `app`, `adapter`, `cmd`, MCP SDK, OS/file/network implementations |
| `internal/app/...` | `core` and consumer-owned narrow port interfaces | `adapter`, `cmd`, SDK types, concrete filesystem/process/service implementations |
| `internal/adapter/...` | `app`, `core`, standard library, and its approved boundary dependency | A sibling adapter; wiring another adapter directly; `cmd` |
| `cmd/shellbeam` | `app`, `adapter`, config, observability | Business rules, state transitions, persistence algorithms |
| `internal/testkit` | Production packages needed by tests | Imports from production code |

Additional rules:

- `cmd/shellbeam` is the only production composition root. `main.go` parses the top-level command, builds dependencies, and maps the final exit code; it contains no use-case logic.
- Port interfaces live with the application consumer, not in a central interfaces package. Keep them narrow, normally one to five methods, and accept/return ShellBeam-owned types.
- Adapters communicate through an application port, never by importing each other. Wiring happens in the composition root.
- Every package has one owner and a package comment describing responsibility, invariants, allowed dependencies, blocking behavior, and error contract.
- Cross-package mutable state is forbidden. A session's mutable lifecycle has one synchronization owner; alternate write paths to the same state are design failures.
- `devctl check architecture` derives the import graph with `go list -deps -json` and enforces `dev/architecture.toml` in every dirty/checkpoint/release profile.
- An architecture-rule exception belongs in `quality-waivers.toml`; an inline ignore comment is not sufficient.

## 22. File size, modularity, and clean code

### 22.1 Size policy

Line counts are raw physical lines so the gate is deterministic. Generated files with a standard generated header, vendored code, testdata, schemas, and golden fixtures are excluded and must remain visibly separate.

| Unit | Normal target | Review threshold | Hard gate |
| --- | ---: | ---: | ---: |
| Production `.go` file | 150–300 lines | More than 350 | More than 500 |
| Test `.go` file | 200–500 lines | More than 600 | More than 800 |
| `cmd/shellbeam/main.go` | At most 100 lines | More than 100 | More than 150 |
| Function/method line span | At most 40 lines | More than 60 | More than 80 |
| Interface | 1–5 methods | More than 5 | More than 8 |
| Production package | At most 2,500 lines and 15 files | Above either threshold | Design review, not an automatic split |

The production-file answer is therefore: **500 lines maximum without a waiver, with review beginning at 350**. Tests may reach 800 because table cases and fixtures are often clearer together.

Hard-gate exceptions must be recorded in `dev/quality-waivers.toml` with file or symbol, reason, rejected alternatives, owner, issue, and an expiry no later than 30 days. Generated code uses a permanent category rather than a waiver. `devctl` fails on an expired waiver.

Limits must not be gamed with meaningless forwarding files, one-function packages, giant anonymous literals moved elsewhere, compressed formatting, or suffixes such as `helpers2.go`. Split by cohesive responsibility and stable change reason. If a 520-line state table is clearer and safer than fragmentation, use a short reviewed waiver.

### 22.2 Clean-code rules

- Make illegal states unrepresentable where practical. State transitions occur through named methods/functions that validate the previous state and emit evidence together.
- Separate pure decisions from effects. Fingerprints, cursor slicing, transition validation, quota decisions, and receipt construction should be deterministic core functions; filesystem and process calls stay in adapters.
- Use explicit constructors that validate dependencies. Avoid package `init` side effects and mutable global variables.
- Accept `context.Context` as the first parameter for bounded operations; do not store it in long-lived structs. Cancellation of a wait must not be confused with cancellation of a command.
- Wrap errors with `%w` and context. Map them to stable boundary codes once. Do not compare error strings or expose raw secrets, commands, output, or environment values.
- Do not panic for request, OS, persistence, or state errors. Panics indicate programmer invariants and are recovered only at process/request boundaries for crash evidence, never as normal control flow.
- Every goroutine has a named owner, cancellation path, and join/reap condition. No fire-and-forget goroutines, unbounded channels, or unbounded queues.
- Do not hold a mutex across a blocking OS call, disk sync, network/IPC call, or channel send. Document any lock order with more than one lock and test it under the race detector.
- Keep I/O visible in names and signatures. Constructors do not start background work unless named `Start`/`Run` and documented.
- Prefer concrete types. Introduce an interface at a real boundary or test seam, not merely because there may be a second implementation someday.
- Comments explain why, invariants, ownership, protocol constraints, and non-obvious failure ordering. They do not narrate obvious syntax.
- No boolean parameter that changes a function into a different operation; use an options type or separate named operation.
- No unrelated refactor in a behavior change. Structural cleanup is a separate change with its own evidence.

## 23. Developer command contract

`tools/devctl` is the single policy driver and is implemented in Go so it is portable and benefits from the Go build cache. An optional Makefile may contain short discoverable aliases only; it must not contain selection, build, or release logic.

| Command | Purpose | Default scope |
| --- | --- | --- |
| `go run ./tools/devctl fmt --dirty` | Format and import-fix changed Go files | Changed files only |
| `go run ./tools/devctl check --dirty` | Architecture, size, schema, vet, and static checks | Affected packages/contracts |
| `go run ./tools/devctl test --focused <pkg> --run <regexp>` | Fast red/green loop | One package and explicit test selection |
| `go run ./tools/devctl test --dirty` | Normal local verification | Affected packages and mapped suites, with Go test cache enabled |
| `go run ./tools/devctl build --local` | Produce a runnable host binary when needed | Current OS/architecture only |
| `go run ./tools/devctl verify --checkpoint` | Pre-review confidence gate | Dirty scope plus affected contracts/integration and targeted race checks |
| `go run ./tools/devctl verify --release` | Authoritative release proof | Full fresh tests, platform lanes, E2E, and release builds |
| `go run ./tools/devctl explain` | Show scope and reasons without executing | Current change set |

Every command supports a machine-readable JSON receipt containing toolchain, base revision, source/diff fingerprint, selected packages/suites, selection reasons, commands, exit codes, cache mode, and artifact paths. Human output stays concise. A receipt is evidence only for that exact source fingerprint.

The first implementation slice should establish `devctl`, import barriers, and test-impact manifests early; they are product-development infrastructure, not postponed polish.

## 24. Incremental build policy

Full builds are not part of the normal edit loop.

1. Run focused or dirty tests first. `go test` already compiles the selected packages and test binary, so a preceding `go build` duplicates work.
2. Build `shellbeam` only when manually exercising the binary, running an integration test that needs its path, or producing a release artifact.
3. Use the native Go build cache and module cache. `devctl` records orchestration decisions and evidence but does not invent a second compiler cache.
4. Reuse the current host artifact only when its manifest matches the toolchain, `GOOS`, `GOARCH`, build tags, `go.mod`/`go.sum`, and transitive source fingerprint.
5. Cross-compile the four target tuples only in release preparation. Native macOS and Linux tests remain required because cross-compilation does not verify process, PTY, peer-credential, launchd, or systemd behavior.

Rules:

- Normal local build: `go build -trimpath -buildvcs=true -o .build/bin/shellbeam ./cmd/shellbeam`.
- Never use `go build -a` in development or CI unless diagnosing the compiler/toolchain itself.
- Never run `go clean -cache`, `go clean -testcache`, or delete module caches as routine troubleshooting. Diagnose a proven corrupt entry first; prefer `-count=1` for a single uncached test run.
- Dirty tests keep normal Go test caching. Successful package-list test results are reusable when the executable and cacheable flags match.
- Preserve a persistent `GOCACHE` locally. CI caches build and module data by exact Go toolchain, OS, architecture, and `go.sum`; a cache miss changes speed, never correctness.
- Do not disable caching merely to create confidence. Freshness belongs to the release profile and targeted flake diagnosis.
- Do not strip symbols in development. Release size flags require measured benefit and retained build metadata.
- No network access is required after dependencies and tools are present; commands that may download must say so before execution.

## 25. Dirty and affected test selection

`test --dirty` is the default local command. It selects packages, not guessed individual test names: package-level selection preserves shared setup, external tests, init behavior, and test helpers. Exact `-run` selection is only for the explicit focused red/green loop.

### 25.1 Change set

The selector considers the union of:

1. committed changes since the merge-base with the configured base branch;
2. staged changes;
3. unstaged changes, including deletions;
4. untracked files that are not ignored.

The base revision and every included path are printed by `explain`. A `--working-tree-only` mode may speed an intermediate experiment, but its receipt cannot satisfy checkpoint or pull-request evidence.

### 25.2 Selection algorithm

1. Build and cache the package/test import graph using `go list -deps -test -json ./...`; key the graph by toolchain, build tags, module files, and package source manifests.
2. Map each changed Go file to its owning package. For a new or deleted file, use its directory plus the before/after graph.
3. A changed non-test Go file selects its package and all reverse transitive dependents that can compile for the active platform.
4. A changed `_test.go` file selects its owning package. A change under `internal/testkit`, fixtures, or shared golden data selects every declared consumer.
5. Map non-Go files, schemas, service templates, scripts, and documentation through `dev/test-impact.toml`.
6. Add impacted contract and integration suites. E2E suites are added only when their mapped boundary changes.
7. Deduplicate and order the result deterministically, then emit the reason for every selected unit.

### 25.3 Broadening and global triggers

| Change | Required broadening |
| --- | --- |
| `go.mod`, `go.sum`, toolchain directive, root build tags | All packages, but retain normal build/test cache |
| `tools/devctl`, architecture or test-impact policy | All policy contract tests plus a self-test fixture matrix |
| MCP input/output schema or adapter contract | MCP adapter, bridge, IPC contract, Inspector tests, prompt evals |
| Receipt/state/fingerprint/persistence contract | All direct consumers plus lifecycle and crash/fault integration suites |
| launchd/systemd templates or install commands | Service contract tests and native platform E2E for that platform |
| `*_linux.go` or `*_darwin.go` | Current-platform scope locally; mandatory matching OS CI lane |
| Documentation only | Markdown/link/schema-example checks; no Go tests unless embedded examples are executable |
| Selector cannot map a path or parse the graph | Broaden to the enclosing barrier; if still ambiguous, select all packages and state the cause |

The selector must never convert an internal failure into an empty set. It also cannot trust timestamps alone. Its fixture suite covers add, modify, rename, delete, untracked file, build tag, platform file, test helper, schema, module, and unknown-path cases.

## 26. Test profiles and quality gates

### 26.1 Profiles

| Profile | Contents | Normal use |
| --- | --- | --- |
| Focused | One package and one named/regex test; optionally one short fuzz seed | Red/green during a small slice |
| Dirty | Affected unit/package tests and mapped contracts/integration; cache enabled | Default after each coherent edit |
| Checkpoint | Dirty scope, architecture/schema gates, impacted integration, and race checks for touched concurrent packages | Before requesting review or handing off a task |
| Release | `go test -count=1` across all packages, all contract/integration suites, native OS E2E, MCP Inspector/tunnel evals, and four release builds | Once after release-candidate source freeze |
| Nightly | Full race suite, sustained fuzzing, stress, leak/deadlock checks, crash and disk/process fault injection | Scheduled health signal; does not replace release proof |

Do not run a full fresh suite after every edit, commit, or ordinary pull request. It spends time while adding little information over a proven affected graph. The release gate remains full and authoritative; the selector may also broaden a risky or ambiguous PR to full scope.

### 26.2 Test design

- Use table tests for state transitions and stable error mappings; every allowed and forbidden transition has an explicit case.
- Test idempotency and evidence with lost-response/retry scenarios, not only happy-path function calls.
- Keep core tests pure and fast. Use real temporary files, processes, PTYs, and Unix sockets at adapter/integration boundaries rather than mocking the behavior being validated.
- Use `t.TempDir`, `t.Cleanup`, explicit deadlines, deterministic randomness, and injectable clock/filesystem/spawner/signal seams. Avoid arbitrary sleeps; wait for observable state with a bounded deadline.
- Handwrite small fakes at consumer-owned ports. Do not generate broad mocks or assert private call order unless ordering is the contract.
- Run `-race` on packages that own goroutines, session state, output capture, input serialization, or capacity/storage budgets when they or their consumers change. Run the complete race suite nightly and for a release candidate on native OS lanes.
- Fuzz closed JSON envelopes, operation fingerprints, IDs, cursor/UTF-8 slicing, input ledgers, receipt decoding, and state-transition sequences. Commit every discovered crash as a deterministic regression seed.
- Add fault tests for partial write, interrupted sync, rename/sync failure, ENOSPC, storage-reserve races, spawn failure, signal failure, daemon death, and output-reader failure.
- Treat coverage as a diagnostic, not a vanity target. New behavior must cover success, stable failure, boundary, cancellation, and retry cases; critical state/idempotency/persistence transitions require exhaustive tables. Repository-wide percentage alone never proves done.
- A flaky test is a defect. Do not hide it with automatic retries. Quarantine requires an issue, owner, expiry, and proof that no release-critical invariant is skipped; critical lifecycle/security tests cannot be quarantined.
- Store failure artifacts in the test artifact directory and print only safe paths/summaries; never dump raw secrets, environment, commands, or captured output by default.

### 26.3 Test layering

1. Core unit/property/fuzz tests prove deterministic decisions.
2. Contract tests prove import barriers, schemas, versioning, CLI exits, and adapter mappings.
3. Integration tests prove process, PTY, filesystem, Unix-socket, concurrency, and failure ordering with real OS primitives.
4. Native platform tests prove peer credentials and service managers.
5. Credentialed E2E tests prove MCP Inspector and the real Secure MCP Tunnel/ChatGPT path.

Do not duplicate the same assertion at every layer. Each higher layer exists to validate a boundary unavailable below it.

## 27. Static quality, security, and operability

### 27.1 Fast quality pipeline

The dirty/checkpoint pipeline runs only the relevant scope where the tool supports package selection:

1. `gofmt` and pinned `goimports` on changed files;
2. import-barrier, file/function-size, waiver-expiry, schema, generated-file, and test-impact checks;
3. `go vet` on selected packages;
4. pinned `staticcheck` on selected packages;
5. dirty tests and mapped race/integration checks;
6. `go mod tidy -diff` and `go mod verify` when module files or dependencies change;
7. `govulncheck` when dependencies change, at checkpoint for security-sensitive paths, nightly, and release.

Secret scanning runs in CI and before a release. Tool findings must be fixed, narrowly configured, or covered by an expiring waiver; blanket disables are forbidden. Lint is not a substitute for tests or architecture checks.

### 27.2 Security and persistence rules

- Create runtime/state directories and files with restrictive permissions under `umask 077`; verify owner, type, mode, and symlink behavior before use.
- Use `openat`-style/narrow platform operations where needed to avoid path substitution. Never follow an unexpected symlink for sockets, reservations, logs, or receipts.
- Persist reservation-before-spawn and terminal-state ordering behind explicit store methods; no caller may assemble an atomic transition from several unrelated file calls.
- A persistence call exposes whether no durable change, a durable change, or an ambiguous change occurred. It must not flatten sync uncertainty into a generic error.
- Enforce request, response, session, total-state, concurrency, retention, and log budgets. Unbounded input, output, goroutines, queues, retries, and retention are defects.
- No command blacklist or heuristic secret parser. Security comes from the documented authority boundary, OS ownership, honest annotations, bounded resources, and redacted diagnostics.
- Telemetry is off by default. Any future telemetry is explicit opt-in, documented, bounded, and excludes commands, cwd, environment, stdin, output, credentials, and raw local paths.
- Run `govulncheck` because reachable-call analysis is more actionable than a version-only alert; still review dependency advisories and transitive changes.

### 27.3 Observability and diagnostics

- Use `slog` with stable event names, severity, daemon incarnation, operation/session ID where safe, action, state transition, duration, byte counts, and stable error/failure code.
- Command text, raw cwd, environment, stdin, output, credentials, tunnel tokens, and arbitrary OS error text are redacted by default.
- Operator logs and command output are separate stores with separate quotas and retention. Logging failure must be bounded and must not recursively log.
- `doctor` reports boundary-specific health: configuration, permissions, daemon, socket peer authentication, MCP bridge, and external tunnel are distinct checks.
- Add low-cost counters and timings internally, but do not expose a network metrics endpoint in V1.
- Benchmarks focus on output append/cursor reads, receipt updates, selector latency, and concurrent-session overhead. A benchmark only gates CI after a stable baseline and an explicit regression budget exist.

### 27.4 Versioning and compatibility

- Version MCP input/output, IPC envelopes, config, operation metadata, input ledger, and receipt independently where their compatibility differs.
- Decoders reject unknown major versions and unknown closed-union variants; they do not silently reinterpret data.
- Persisted-format changes require golden compatibility tests and an explicit migrate/read-old/reject decision before merge.
- Stable tool error codes and receipt failure reasons are additive within V1 unless a breaking protocol version is introduced.
- Record important boundary changes as ADRs: dependencies, import direction, persistence semantics, schema compatibility, process ownership, or platform support. Minor implementation choices do not need ceremony.

## 28. CI and release policy

### 28.1 Pull request/checkpoint CI

- Recompute affected scope from the target-branch merge-base; never trust a local receipt alone.
- Run architecture/schema/quality gates, affected package tests, impacted integration suites, and required OS lanes.
- Upload the selection receipt and safe failure artifacts. Reviewers can see why a package or suite did or did not run.
- A selector failure broadens scope and fails visibly if it still cannot prove coverage.
- Do not require a full fresh suite for every PR. Global triggers and high-risk contract changes may legitimately select all packages while still reusing safe caches.

### 28.2 Release candidate

After source freeze, one release workflow runs:

1. clean checkout and declared Go toolchain;
2. full uncached package and contract/integration tests with `-count=1`;
3. full race run on native macOS and Linux lanes;
4. native service/peer-credential tests;
5. MCP Inspector plus credentialed Secure MCP Tunnel prompt evals;
6. `CGO_ENABLED=0` release builds for macOS/Linux on amd64/arm64 with `-trimpath`, VCS metadata, and explicit version/commit metadata;
7. checksums, dependency inventory/SBOM, vulnerability result, and provenance manifest;
8. install, upgrade, doctor, uninstall-with-data-preserved, and rollback smoke tests.

Release evidence is valid only for the exact clean commit and toolchain. A dirty tree cannot produce an official release. Signing and notarization become mandatory when binaries are distributed beyond the private V1 workflow; their absence must never be hidden.

### 28.3 Dependency and maintenance cadence

- Automate a weekly report for stable Go patches, direct dependency releases, advisories, and CI action updates; do not auto-merge runtime changes.
- Update one dependency concern at a time with release notes, affected contracts, dirty/checkpoint evidence, and rollback.
- Run full nightly stress/fuzz/race independently of feature PRs so expensive health checks do not block the edit loop.
- Review waivers, flaky quarantines, retention defaults, performance budgets, and supported OS versions on a fixed monthly cadence.

## 29. AI-assisted development workflow

ShellBeam is built by one primary reasoning agent per task unless the user explicitly authorizes delegation. The repository must make safe, focused work possible without relying on conversational memory.

- Root `AGENTS.md` records product non-goals, module map, import barriers, critical invariants, approved commands, forbidden operations, and the evidence format. Package comments and local READMEs add only boundary-specific information.
- Each task starts with a small task capsule: goal, non-goals, acceptance cases, touched boundary, expected packages/suites, risk class, and stop conditions.
- Implement vertical slices test-first: one failing contract/test, minimum behavior, focused pass, then dirty pass. Expand scope only when crossing an interface or changing a shared invariant.
- Read and preserve the current dirty worktree. Do not reset, overwrite unrelated edits, create branches, commit, push, open PRs, install services, or contact external systems unless the user authorized that action.
- Do not run a full suite merely “to be safe.” Use `devctl explain`, focused tests, dirty tests, and checkpoint scope; use the release profile only for a release decision or an explicit diagnosis.
- Do not claim success from plausible reasoning. Report the exact command, exit code, selected scope/reasons, and source fingerprint. If the source changes afterward, rerun the smallest invalidated proof.
- Stop and ask when a requirement changes authority, destructive behavior, persistence compatibility, public schema, or user-visible semantics. Do not fill those gaps with a convenient assumption.
- Keep generated schemas, examples, golden receipts, docs, and implementation in the same coherent change so contract drift is impossible to merge.
- A requested dependency, waiver, barrier exception, test quarantine, or unrelated refactor is visible in the handoff; never smuggle it into a feature diff.
- Review output leads with invariant violations and user impact, not style preference. Findings require evidence, a concrete fix, and a verification target.

## 30. Definition of done

A normal implementation task is done when all applicable items are true:

1. The requested behavior and non-goals are explicit, and the implementation remains within V1 authority.
2. Public/internal schemas, receipts, stable codes, state transitions, and persistence ordering are updated together.
3. Import barriers pass; no SDK/platform/storage type leaked inward and no sibling adapter was coupled.
4. Files/functions satisfy size gates or have a current, specific waiver; the split preserves cohesion.
5. New behavior has focused tests for success, boundary, failure, cancellation, retry/idempotency, and evidence as applicable.
6. `test --dirty` and applicable `check --dirty`/checkpoint gates pass for the exact final source fingerprint.
7. The current host binary is built incrementally only if the acceptance scenario needs it; build evidence identifies toolchain and source.
8. Resource budgets, goroutine/process ownership, redaction, error mapping, and fault behavior are reviewed.
9. Documentation, schemas, fixtures, ADRs, and test-impact mappings changed with the code where required.
10. No unrelated diff, unapproved dependency, expired waiver, hidden quarantine, flaky retry, or unresolved high-severity finding remains.

A release is done only when the full release-candidate workflow in section 28.2 passes. Dirty/checkpoint evidence is intentionally insufficient for that claim.

## 31. Development references

- [Go release history](https://go.dev/doc/devel/release): stable toolchain and patch-release baseline.
- [Go 1.24 release notes](https://go.dev/doc/go1.24): cached executables for `go run`/`go tool` and build command improvements used by `devctl`.
- [`go test` command documentation](https://pkg.go.dev/cmd/go#hdr-Test_packages): package-list test caching and flags such as `-count=1`.
- [Go vulnerability management](https://go.dev/doc/security/vuln/): reachable vulnerability analysis with `govulncheck`.
- [Official MCP Go SDK releases](https://github.com/modelcontextprotocol/go-sdk/releases): pinned SDK and protocol lifecycle changes.
- [Build your MCP server](https://developers.openai.com/plugins/build/mcp-server): user-facing tool metadata, schemas, annotations, instructions, and structured output.
- [Test and connect your MCP server](https://developers.openai.com/plugins/deploy/connect-chatgpt): prompt evals, MCP Inspector, and Secure MCP Tunnel testing.
