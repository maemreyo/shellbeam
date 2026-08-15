# ShellBeam B1.0 Persistent Runtime and Named Sessions Design

Status: approved design; self-reviewed against the live `ai/execution-observation` branch after A2.6 completion and before implementation planning

Scope: B1.0 persistent non-TTY runtime only; opt-in per-session supervision, daemon-restart reattachment, optional stable names, bounded inspection, and existing one-tool control semantics

## 1. Purpose

ShellBeam already has durable operation/session receipts, byte-correct output cursors, retry-safe stdin offsets and kill IDs while a daemon owns the process, activity/workspace identity, Event Journal, structured results, telemetry/reproduction, evidence, environment/toolchain observation, current-host process inspection, optional port observation, and advisory mutation scopes.

The remaining B1 gap is narrower than the historical roadmap wording: a caller cannot yet ask ShellBeam to keep strong control authority for a long-running command across **daemon restart**. Today a direct session that outlives its daemon cannot be safely reclaimed from a persisted PID; unresolved direct sessions therefore become `abandoned/ambiguous` on restart.

B1.0 adds an opt-in persistent execution mode. Each persistent non-TTY session is owned by one small per-session supervisor process. The supervisor owns the child process group and execution-side control state; the daemon remains the canonical ShellBeam control plane and the only writer of canonical operation/session/receipt state. A restarted daemon may regain control only through an authenticated supervisor handshake that proves the exact persistent session identity and supervisor generation.

B1.0 must improve continuity without adding protocol choreography to ordinary execution. A normal `start` remains the existing direct-spawn path and performs no B1 supervisor, socket, registry, or recovery-spool work.

## 2. Existing capabilities retained, not reimplemented

B1.0 SHALL reuse rather than recreate these completed capabilities:

- durable operation reservation, session snapshots, terminal receipts, output cursors, input offsets, kill IDs, capacity/storage bounds, and abandoned/ambiguous restart semantics for direct sessions;
- activity/workspace correlation and lazy workspace provenance;
- Event Journal publication and snapshot recovery;
- structured-result, evidence, telemetry, reproduction, and expected-output pipelines;
- A2.5 current-host process inspection and optional listening-port observation;
- A2.6 advisory mutation scopes;
- one public MCP tool, `local_shell`.

In particular, B1.0 does **not** add a second process inspector or port scanner. After authenticated reattachment, the existing A2.5 process-observation path may consume the current child PID reported by the attached supervisor proxy. Without authenticated ownership proof, no persisted PID becomes current authority.

## 3. Goals

B1.0 SHALL:

- let a modern caller opt one non-TTY start into persistent supervision;
- optionally bind a durable human/model-friendly `session_name` to that session while keeping `session_id` authoritative;
- keep the child observable and controllable across daemon restart while its supervisor remains alive;
- preserve byte-correct output continuation, stdin offset semantics, kill-ID semantics, timeout semantics, and terminal evidence across daemon absence;
- ensure a lost daemon response cannot create a second supervisor or child for the same `operation_id`;
- reattach only after authenticated exact-session and exact-generation proof;
- make supervisor absence, corruption, incompatibility, or authentication failure fail closed without PID-based reclaim or signaling;
- keep canonical ShellBeam operation/session/receipt/Event-Journal state daemon/store-owned;
- expose persistent-session discovery through bounded modern inspection without requiring an explicit attach/detach ceremony;
- preserve ordinary direct-session restart behavior unchanged;
- keep ordinary compatible `start -> poll -> terminal` free of B1 supervisor/registry/spool work when persistence is not requested;
- keep the single `local_shell` MCP tool.

## 4. Non-goals

B1.0 does NOT:

- provide persistent PTY, terminal emulator, REPL, or interactive shell semantics;
- guarantee a running process survives host reboot;
- reclaim a child from PID, PGID, start time, command line, process name, port, or process-table heuristics;
- automatically restart a crashed supervisor or child;
- turn a session name into ownership, authorization, or idempotency authority;
- add session rename, name reuse, name purge, supervisor migration, or supervisor hot upgrade;
- make uninstall or daemon upgrade silently terminate persistent children;
- add a service/workflow definition language or scheduler;
- infer that a command should become persistent;
- add remote or multi-machine supervision;
- add containers, sandboxing, security profiles, or a different current-user authority model;
- add daemon-side planning or reasoning;
- add a second MCP tool.

## 5. Core authority model

B1.0 separates three authorities.

### 5.1 Operation/session identity authority

The existing durable `operation_id -> session_id` reservation remains canonical. `operation_id` remains the retry identity for start. `session_id` remains the only stable control identity for `poll`, `write`, `kill`, receipt lookup, and process correlation.

`session_name` is a convenience alias only. It never substitutes for `session_id` in control mutation semantics and never proves process ownership.

### 5.2 Execution ownership authority

For a persistent session, the per-session supervisor is the only component allowed to own and directly control the child process group. It owns:

- child spawn and process-group ownership;
- stdin delivery and EOF ordering;
- signal execution and timeout escalation;
- output capture into a bounded private recovery spool;
- child wait/reap and terminal execution evidence;
- the private control socket and supervisor-generation identity.

The daemon controls a persistent child only through an authenticated attachment to that exact supervisor.

### 5.3 Canonical ShellBeam state authority

The daemon plus existing store remain the only writers of canonical:

- operation reservations;
- session snapshots;
- canonical output log;
- terminal receipts;
- Event Journal obligations/events;
- evidence/telemetry/structured/reproduction state.

The supervisor may write only its private recovery material in the per-user runtime area. It never writes canonical operation/session/receipt/Event-Journal files directly.

## 6. Architectural choice

B1.0 uses **one supervisor process per persistent session**.

Rejected alternatives:

1. A single shared supervisor host for all persistent sessions would create a second daemon-like authority, enlarge the failure blast radius, and complicate upgrade/lifecycle semantics.
2. PID/start-time reclaim cannot prove ownership of stdin, output, signal, or reap authority and violates the existing fail-closed restart model.

The supervisor is not a second shipped product. The existing `shellbeam` executable supplies a private internal supervisor entry point that is not advertised as a public user command. Ordinary direct starts never enter it.

## 7. Public `local_shell` surface

B1.0 remains inside the existing `local_shell` tool.

### 7.1 `start`

Modern `start` adds two optional fields:

```json
{
  "action": "start",
  "operation_id": "op-dev-server-1",
  "command": "go run ./cmd/server",
  "cwd": "/absolute/repo/path",
  "persistent": true,
  "session_name": "dev-server"
}
```

Rules:

- absent `persistent` is equivalent to `false` and preserves the existing direct-spawn path;
- `session_name` is allowed only when `persistent=true`;
- `persistent=true` with `tty=true` is rejected in B1.0; it never silently falls back to direct TTY execution;
- persistent mode is execution-affecting and is frozen into start identity/fingerprint semantics;
- `session_name`, when present, is frozen into the reservation metadata/request identity so a retry cannot silently rename the session;
- retry of the same `operation_id` replays the same persistent binding and never spawns another supervisor or child;
- changed command, persistent mode, name, timeout, cwd, argv, or other already-frozen request metadata under the same `operation_id` follows the existing conflict boundary rather than creating new work.

A persistent start response may add only public-safe facts:

```text
persistent: true
session_name: dev-server
ownership: supervised
continuity: daemon_restart
```

Supervisor endpoint paths, capability secrets, bootstrap material, private generation tokens, and private spool locations are never public response fields.

### 7.2 `poll`, `write`, and `kill`

Their public addressing semantics remain unchanged:

- `poll` requires `session_id` and output cursor;
- `write` requires `session_id` and `input_offset` and preserves the existing offset+length+payload-hash duplicate/conflict semantics;
- `kill` requires `session_id`, stable `kill_id`, and signal and preserves the existing same-ID/same-signal replay semantics.

B1.0 does **not** add a public `write_id` and does not let these control actions accept `session_name`. A caller that only remembers a name resolves it once through inspection, obtains the authoritative `session_id`, then uses the existing control surface.

### 7.3 `inspect.sessions`

B1.0 adds one bounded modern read-only action:

```text
inspect.sessions
```

Supported filters are exact and closed:

- exact `session_name`;
- `activity_id`;
- `workspace_id`;
- session lifecycle/state;
- persistent-only boolean.

The action never scans the OS process table, filesystem tree, Git repository, or network. It reads bounded canonical persistent-session metadata plus the daemon's already-established live attachment cache.

Each row may expose:

```text
session_id
session_name?
operation_id
activity_id?
workspace_id?
state
outcome?
persistent
ownership_status: current | reattached | terminal | lost
created_at
updated_at
output_bytes
input_accepted_bytes?
input_delivered_bytes?
```

Results are deterministic, bounded, paginated with an opaque cursor bound to the request filters, and report continuation explicitly. `persistent_only` defaults to `true`; when explicitly `false`, direct canonical sessions may also be returned, but they never gain names or reattachment semantics merely by appearing in this projection.

There is no public `attach` or `detach` action. Supervisor attachment is daemon infrastructure, not model choreography.

## 8. Session-name contract

A B1.0 `session_name`:

- is valid UTF-8;
- is 1-128 bytes;
- is matched case-sensitively and byte-exactly;
- is not Unicode-normalized or case-folded by ShellBeam;
- contains no NUL/control character, `/`, or `\\`;
- has no leading or trailing Unicode whitespace;
- is not interpreted as a path, glob, regex, selector, command, or ownership label.

A retained name binding is unique within one ShellBeam state root. In B1.0 a terminal name is **not automatically reusable**. The same name may replay only its already-bound session. Rebinding, rename, purge, and reuse require a later explicit lifecycle design.

This deliberately trades name recycling for unambiguous historical lookup and retry semantics.

## 9. Durable canonical metadata

Persistent execution adds a small canonical binding/descriptor written by the daemon. It is separate from raw private supervisor recovery files and may be represented as a focused record or a versioned extension referenced by the existing reservation/session metadata.

The canonical persistent binding contains only public/control-plane-safe facts needed for exact reconciliation, including:

```text
schema_version
session_id
operation_id
session_name?
persistent=true
supervision=per_session
continuity=daemon_restart
supervisor_generation_id
supervisor_endpoint_ref
lifecycle: provisioning | live | terminal | lost
created_at
updated_at
```

`supervisor_endpoint_ref` is an opaque internal reference, not a public absolute path. The canonical binding does not contain the capability secret, raw output, stdin bytes, environment values, source content, or arbitrary OS error text.

The existing operation reservation remains the idempotency authority. The reservation itself MUST durably encode the admitted persistent intent (and optional name binding fingerprint/reference) before any child spawn. Therefore a crash after reservation but before the focused persistent descriptor exists is still classified as persistent provisioning, never as an ordinary direct session. The persistent binding cannot authorize a second spawn independently of that reservation.

## 10. Private supervisor runtime state

Sensitive/recovery material lives under a user-only per-session supervisor runtime directory, separate from canonical store namespaces. Directory/file/socket permissions are restricted to the current user.

Private runtime material may contain:

- capability secret;
- Unix control socket;
- supervisor generation and protocol metadata;
- bounded append-only recovery output spool;
- current input ledger state and accepted records;
- kill-attempt ledger;
- timeout/deadline state;
- frozen terminal recovery record;
- safe internal recovery checksums/sequence numbers.

This material is not an MCP/public-inspection surface. It may contain raw recovery bytes and input/output hashes needed for correct continuity because it stays within the private runtime boundary.

No capability secret is passed in command-line arguments, logged, placed in public environment diagnostics, copied into receipts, or copied into Event Journal summaries. Bootstrap uses a user-only private file or inherited descriptor/channel; the implementation plan shall prefer inherited descriptors/channels where they reduce process-metadata exposure without weakening crash recovery.

Host reboot may remove the runtime area. That is acceptable because B1.0 does not promise running-process continuity across host reboot.

## 11. Supervisor protocol

The daemon-supervisor protocol is private, local, versioned, closed-schema, and bounded. It is not MCP and is not exposed to the reasoning model.

The minimal protocol supports:

- authenticated handshake/status;
- output-range replay/status;
- stdin chars/EOF using the existing offset semantics;
- signal using the existing `kill_id` semantics;
- terminal/status snapshot;
- bounded wait/change notification.

The protocol does not accept arbitrary shell commands, source edits, Git operations, provider queries, or general RPC/plugin calls.

### 11.1 Authentication and generation binding

A reattach handshake must prove all of:

- matching `session_id`;
- matching supervisor generation identity from the canonical persistent binding;
- matching capability secret/challenge;
- compatible supervisor protocol version;
- same-user local socket boundary where platform support exists.

Only after proof succeeds may the daemon create a current persistent `ProcessHandle` proxy or expose current child PID to the existing process-inspection path.

A PID, PGID, process start time, executable name, socket filename, or port is never sufficient proof.

## 12. Persistent start state machine

A new persistent start follows this order:

```text
validate/fingerprint request
  -> reserve operation + session exactly once
  -> reserve optional name exactly once
  -> create canonical persistent binding in provisioning state
  -> create private runtime bootstrap material
  -> spawn one supervisor process
  -> supervisor spawns/owns child and establishes control socket
  -> authenticated readiness handshake succeeds
  -> canonical session becomes running/live
  -> return/replay normal ShellBeam view
```

No child may be spawned before the durable operation/session binding that prevents a retry from creating a second session.

If a crash occurs after canonical provisioning but before daemon acknowledgement, a later retry/startup reconciles the same binding. It never allocates a second supervisor generation unless a future separately designed recovery lifecycle explicitly allows replacement.

If provisioning cannot prove whether a supervisor/child became live, the result is ambiguous/lost; B1.0 does not spawn a replacement under the same operation merely to make progress.

## 13. Output continuity and canonicalization

The supervisor is the persistent session's output-capture owner. It writes child stdout/stderr to a bounded append-only private recovery spool with monotonically increasing byte offsets.

While a daemon is attached, output is canonicalized into the existing ShellBeam output store. Canonical publication uses an offset-aware reconciliation boundary:

1. supervisor supplies `(offset, bytes)` from its spool;
2. daemon/store compares the expected canonical extent;
3. exact unseen bytes are appended once;
4. already-present overlap is accepted only after byte equality is proven;
5. a verified partial overlap may append only the missing suffix;
6. mismatched overlap, impossible gap, corruption, or budget overflow fails closed and marks output incomplete rather than fabricating continuity;
7. supervisor advances canonical acknowledgement only after the daemon confirms the range is canonical.

This handles the critical crash window where canonical append succeeded but the daemon died before acknowledging the supervisor. Replay cannot duplicate bytes.

The private spool per session may never exceed the configured canonical session-output limit. If capture/spool quota is exhausted or durable recovery capture fails, the supervisor terminates its owned process group using the same output-incomplete safety intent as the direct runtime and freezes terminal evidence with `output_complete=false`.

After canonical terminal publication and successful acknowledgement of all retained output ranges, private spool recovery material may be removed according to bounded cleanup policy.

## 14. Stdin continuity

B1.0 preserves the existing public input contract rather than inventing a new mutation ID.

For persistent sessions, the supervisor owns the equivalent of the current `InputLedger` so that `input_offset` semantics survive daemon restart:

- same offset + same kind/length/hash replays as duplicate;
- same offset + different content/kind is `input_conflict`;
- offset greater than next expected is `input_gap`;
- bounded queued bytes enforce existing backpressure intent;
- EOF is ordered and idempotent for non-TTY sessions;
- accepted and delivered byte counts remain distinct;
- terminal success is impossible when accepted bytes are not proven delivered.

The supervisor retains sufficient accepted-record history for the lifetime of the persistent session to distinguish an exact retry from a conflicting old-offset write. B1.0 therefore advertises a hard maximum number/bytes of retained persistent input records. When that history capacity is exhausted, **new** input is rejected with a stable capacity/backpressure failure while already-recorded duplicates remain replayable. B1.0 never discards old input identity and then accepts an ambiguous retry as new.

Default B1.0 capability limits are:

- maximum retained input records per persistent session: 4096;
- maximum retained input-record metadata bytes per persistent session: 1 MiB;
- queued input bytes: the existing configured per-session input queue limit.

Implementations may expose lower configured limits, but never silently exceed the advertised values.

## 15. Kill and signal continuity

Persistent supervisors own the existing kill-attempt semantics across daemon restart:

- `kill_id` is required exactly as today;
- same ID + same signal replays the stored attempt/result;
- same ID + different signal is a conflict;
- a signal is sent only to the supervisor-owned process group;
- terminal/no-longer-needed attempts remain replayable without a second signal;
- loss of supervisor ownership proof disables signaling entirely.

The supervisor retains kill-attempt identity for the persistent session lifetime. Default maximum retained kill attempts is 256 per session. After that, new kill IDs are rejected with a stable capacity failure while retained IDs remain replayable.

## 16. Timeout semantics

For persistent sessions, timeout authority moves with execution ownership into the supervisor.

The timeout deadline and termination-grace policy are frozen from the admitted start request before/at child spawn. Daemon restart:

- does not reset the timeout;
- does not extend the deadline;
- does not cancel escalation;
- does not create a second timeout actor.

The supervisor performs the existing TERM -> grace -> KILL intent against its owned process group and freezes the resulting signal/timeout evidence for canonical reconciliation.

If timeout state cannot be proven after supervisor ownership is lost, the daemon reports ambiguity rather than inventing timeout completion.

## 17. Terminal evidence and supervisor exit

Only the persistent supervisor waits/reaps the child.

Before a supervisor may exit normally after child termination, it freezes a private terminal recovery record containing the canonical-receipt inputs needed later, including:

- spawn evidence;
- exit/reap evidence;
- signal/timeout evidence;
- final output extent and completeness;
- input accepted/delivered counts and EOF state;
- terminal target/outcome inputs;
- supervisor/session/generation identity.

The terminal recovery record is generation-bound and integrity-protected with material derived from or verified through the private session capability. It is stored with the same user-only/no-follow runtime rules as the rest of supervisor recovery state. A later daemon MUST verify the exact session ID, supervisor generation, record integrity, and canonical binding before consuming a terminal record after the supervisor has exited.

If the daemon is connected, it reconciles this record immediately. If the daemon is absent, a later daemon verifies and consumes the frozen record through the exact persistent runtime binding and publishes the canonical ShellBeam terminal receipt exactly once through the existing store path.

A supervisor terminal record is recovery evidence, not a second canonical receipt file.

## 18. Daemon restart reconciliation

Daemon startup changes from one undifferentiated unresolved-session abandonment pass to a bounded classification pass.

### 18.1 Direct sessions

Non-persistent unresolved sessions preserve current behavior: daemon restart makes them terminal `abandoned/ambiguous` according to the existing reconciliation contract. B1.0 does not attempt to reclaim them.

### 18.2 Persistent sessions

For each non-terminal canonical persistent binding, startup performs one bounded reconciliation:

1. validate the canonical binding;
2. resolve the private endpoint/capability reference;
3. attempt an authenticated protocol handshake within the B1 reattach budget;
4. classify the session as one of:
   - `reattached`: supervisor proves the exact live session/generation;
   - `terminal`: exact private runtime identity has frozen terminal recovery evidence;
   - `lost`: exact ownership/recovery identity cannot be established;
5. reconcile canonical output/input/terminal facts as applicable;
6. publish safe lifecycle events exactly once.

A failed, missing, refused, malformed, incompatible, timed-out, or authentication-failed supervisor never triggers PID-based signaling or replacement spawn.

Startup reconciliation is bounded and parallelized. Defaults:

- per-supervisor handshake deadline: 2 seconds;
- maximum concurrent startup handshakes: 16;
- overall persistent-session startup reconciliation budget: 5 seconds.

If the overall budget expires, remaining unresolved persistent sessions are classified `lost/ambiguous`; the daemon may continue startup rather than becoming globally unavailable.

The daemon does not become ready for client actions until every retained unresolved session has been classified as direct-abandoned, persistent-reattached, persistent-terminal, or persistent-lost for that startup cut.

## 19. Daemon shutdown semantics

Graceful daemon shutdown must distinguish direct and persistent sessions.

- Direct sessions retain existing shutdown behavior.
- Persistent sessions are **detached**, not terminated merely because the daemon is stopping/restarting.
- The daemon closes attachment/control connections and lets the supervisor continue owning the child.
- Explicit `kill` still terminates the child/process group through the supervisor.
- The supervisor itself stays alive until child terminal evidence and required private recovery state are frozen.

`Service.Shutdown()` or equivalent orchestration must not accidentally send TERM/KILL to persistent children as a side effect of daemon lifecycle.

## 20. Supervisor loss

If the supervisor dies or ownership proof is otherwise irrecoverably lost while the child may still be alive:

- the canonical session becomes `abandoned/ambiguous`/`lost` according to the versioned public projection;
- ShellBeam does not chase the child via stored PID/PGID;
- ShellBeam sends no signal to a process discovered only from OS metadata;
- current-user manual OS cleanup remains possible outside ShellBeam;
- no automatic replacement supervisor or child is started.

This is a deliberate safety trade-off: B1.0 guarantees daemon-restart continuity, not supervisor-crash recovery.

## 21. Host reboot boundary

B1.0 does not guarantee a running child survives or remains controllable across host reboot.

After reboot:

- a live supervisor proof is normally absent;
- PID/PGID values from previous state are not reclaimed;
- terminal evidence already canonicalized before reboot remains valid;
- private terminal recovery material that happens to survive may be consumed only when its exact binding/integrity rules remain satisfied;
- otherwise unresolved persistent sessions become lost/ambiguous.

No B1.0 documentation may market host-reboot process continuity.

## 22. Upgrade, install, and uninstall boundary

A daemon/binary upgrade:

- does not automatically kill persistent sessions;
- negotiates the private supervisor protocol with still-running older supervisors;
- may reattach only when the negotiated protocol remains compatible;
- treats incompatible supervisors as ownership-lost/ambiguous rather than signaling their child;
- does not hot-replace the supervisor in B1.0.

Uninstall/package removal must not silently terminate persistent sessions. A future explicit destructive `terminate all persistent sessions` lifecycle may be designed separately; it is not an uninstall side effect in B1.0.

## 23. Event Journal integration

B1.0 adds bounded safe lifecycle events for meaningful persistent-runtime transitions:

```text
persistent_session_started
persistent_session_reattached
persistent_session_terminal
persistent_session_lost
```

Rules:

- one event per committed meaningful transition;
- exact start/reconciliation retry does not duplicate an event;
- MCP reconnect does not create an event;
- supervisor heartbeat/status polling does not create events;
- events contain only safe IDs/state summaries and no capability, socket path, raw command/stdin/output/source/env/credential data;
- events are observation projections, not execution ownership authority.

## 24. Process inspection integration

A2.5 process inspection remains the only process-observation feature.

For a live persistent session, an authenticated attached supervisor proxy may implement the existing PID-bearing process-handle boundary. `ResolveProcessSession` may report `Current=true` and a child PID only while the daemon currently holds valid supervisor ownership proof.

After attachment loss, `Current=false` and `PID=0` must be returned even if a prior PID is stored privately or visible in the process table.

Existing optional port observation may operate on a currently proven child PID; B1.0 adds no port scanner.

## 25. Capability discovery

The existing `FeatureNamedSessions` becomes available only when all of these are composed and valid:

- persistent-session canonical registry/binding store;
- per-session supervisor runtime;
- private protocol/authentication;
- startup reattachment reconciliation;
- bounded `inspect.sessions` projection.

Capability discovery performs no supervisor spawn, process-table scan, filesystem walk, Git/provider/network work, or session handshake. Availability is a composition/configuration fact.

Modern capability output advertises at least:

```text
persistent_session_schema_versions: [1]
supervisor_protocol_versions: [1]
persistent_non_tty: true
persistent_tty: false
continuity: daemon_restart
host_reboot_continuity: false
max_persistent_sessions: <configured <= live session capacity>
max_session_name_bytes: 128
max_session_inspect_rows: 100
default_session_inspect_rows: 25
max_persistent_input_records: 4096
max_persistent_input_record_metadata_bytes: 1048576
max_persistent_kill_records: 256
max_recovery_spool_bytes: <configured session output limit>
max_queued_input_bytes: <existing configured limit>
reattach_handshake_timeout_ms: 2000
startup_reattach_concurrency: 16
startup_reattach_budget_ms: 5000
```

Legacy capability projections omit B1.0-only fields. Legacy request generations reject B1.0-only start/inspect fields/actions rather than silently running a requested persistent command as direct.

## 26. Stable failure model

B1.0 adds typed stable categories/reasons consistent with the existing failure boundary. Required distinguishable cases include:

- persistent session name conflict;
- persistent session capacity/history exhausted;
- persistent TTY unsupported;
- supervisor unavailable;
- supervisor protocol incompatible;
- supervisor authentication failed;
- supervisor state/generation conflict;
- persistent session ownership lost;
- persistent recovery output conflict/gap/corruption;
- persistent input history exhausted;
- persistent kill history exhausted.

The implementation may map these to existing top-level categories where that preserves the repository failure vocabulary, but model-facing diagnostic codes/reasons must remain stable and testable.

Retryability describes the ShellBeam control action only. No supervisor/control failure means rerunning the underlying command with a new `operation_id` is safe.

## 27. Security and privacy

B1.0 is not a new security boundary against the current OS user; ShellBeam continues to execute with that user's authority.

B1.0 SHALL nevertheless prevent accidental authority confusion and public secret leakage:

- supervisor socket/runtime directories are user-only;
- same-user peer validation is used where supported;
- capability secrets are high entropy and session/generation bound;
- offline terminal recovery records have generation-bound integrity verification before canonical consumption;
- secrets never appear in public schemas, receipts, Event Journal summaries, logs, command lines, or model-safe errors;
- authentication/protocol errors return stable safe diagnostics rather than raw secret/path/socket text;
- a descriptor/capability mismatch fails closed;
- symlink/path replacement in supervisor runtime setup is rejected using the repository's existing no-follow/safe-path principles;
- no PID-only fallback exists.

## 28. Resource bounds and retention

B1.0 has bounded resource use.

Each persistent session consumes at most:

- one supervisor process;
- one private control socket;
- one bounded recovery spool up to the advertised canonical output limit;
- one bounded queued-input budget;
- bounded input and kill identity metadata;
- one small canonical persistent binding/name record.

Persistent sessions count against the existing live-session/capacity contract; B1.0 does not create a hidden second concurrency pool that bypasses `MaxSessions`.

Canonical terminal retention and output compaction preserve existing operation/receipt replay authority. Private supervisor runtime data is removed only after canonical reconciliation no longer needs it. Cleanup failure may leak bounded private files temporarily but never rewrites terminal truth or reactivates ownership.

## 29. Ordinary-path performance invariant

With B1.0 compiled/composed but not requested, an ordinary direct compatible `start -> poll -> terminal` performs:

- zero supervisor process spawns;
- zero supervisor socket creation/handshake;
- zero persistent registry/name lookup on start admission beyond validation of absent/false B1 request fields;
- zero recovery spool read/write;
- zero persistent input/kill ledger work;
- zero additional synchronous durability barrier solely for B1.

The repository's existing incremental admission benchmark remains authoritative: B1.0 must not weaken the <=5 ms p95 / <=10 ms p99 incremental roadmap gate for ordinary compatible start without a separately reviewed amendment.

Persistent starts may pay the explicit cost of one supervisor process, private runtime setup, and readiness handshake. The implementation plan must measure and report persistent-start p50/p95/p99, but B1.0 does not invent a threshold before native measurements exist.

## 30. Crash and ambiguity matrix

The implementation plan must fault these boundaries explicitly:

- after operation reservation, before persistent binding;
- after persistent binding, before supervisor spawn;
- after supervisor spawn, before readiness proof;
- after child spawn, before daemon receives readiness;
- after supervisor spools output, before canonical append;
- after canonical append, before supervisor acknowledgement;
- after stdin acceptance, before daemon response;
- after stdin delivery, before daemon observes delivery;
- after signal attempt, before daemon response;
- after child exit, before terminal recovery freeze;
- after terminal recovery freeze, before canonical receipt publication;
- after canonical receipt publication, before supervisor/runtime cleanup.

For every injected boundary the result must be one of:

- exact replay/reconciliation of already committed work;
- authenticated reattachment to the same live supervisor/session;
- canonical terminal replay;
- explicit lost/ambiguous state.

No fault may cause duplicate child spawn, duplicate signal under one `kill_id`, duplicate canonical output bytes, fabricated complete output, fabricated exit evidence, or PID-based reclaim.

## 31. Native acceptance

B1.0 is complete only when fresh exact-source verification proves at least:

1. ordinary non-persistent execution uses the existing direct owner and performs no B1 supervisor/registry/spool work;
2. persistent start under lost responses/retries creates exactly one supervisor and one child;
3. hard daemon death during a long-running persistent command leaves supervisor+child alive and a new daemon reattaches to the same `session_id`;
4. output cursor continuation after reattach is byte-exact with no duplicate/gap;
5. stdin `input_offset` duplicate/conflict/gap/backpressure behavior survives daemon restart;
6. `kill_id` duplicate/conflict semantics survive daemon restart and never signal without current authenticated ownership;
7. timeout fires while the daemon is absent and is not reset by reattach;
8. child exit while daemon is absent is reaped/frozen by the supervisor and later becomes one canonical terminal receipt;
9. supervisor crash/loss while child may live becomes lost/ambiguous and is never reclaimed from PID;
10. direct unresolved sessions still follow existing abandoned/ambiguous restart semantics;
11. persistent TTY is explicitly rejected;
12. `inspect.sessions` resolves a retained name to the same authoritative `session_id` with bounded pagination and no OS scan;
13. Event Journal lifecycle transitions are exactly once and heartbeat-free;
14. capability discovery advertises B1 only when the real persistent runtime is composed;
15. private capability/socket/bootstrap data never appears in MCP output, canonical receipts, Event Journal summaries, safe logs, or model-facing errors;
16. supervisor/runtime directories resist symlink/replacement attacks according to current local runtime safety rules;
17. relevant core/store/app/process/IPC/MCP/cmd/integration tests and race suites pass;
18. schema closure/legacy compatibility, `devctl check`, dirty/global test selection, staged diff checks, commit gate, and exact source-fingerprint proof pass;
19. native macOS and Linux process/socket acceptance pass before B1.0 is described as cross-platform production-ready.

## 32. Implementation boundaries and likely modules

The implementation plan should preserve current package boundaries rather than fold supervisor mechanics into one large daemon file. The expected responsibility split is:

- `internal/core/...`: persistent binding/name/protocol-neutral state and validation only;
- `internal/app/...`: persistent-session orchestration, reconciliation, and model-facing inspect projection;
- `internal/adapter/supervisor/...`: local process/socket/bootstrap/spool protocol implementation;
- `internal/adapter/store/...`: canonical persistent binding/name persistence and offset-aware canonical output reconciliation;
- `internal/app/daemon/...`: narrow routing between direct owner and persistent owner, plus persistent-aware shutdown/reattach proxy integration;
- `cmd/shellbeam/...`: composition and private internal supervisor process entry point;
- IPC/MCP/schema: closed modern `start` fields and `inspect.sessions` branch only.

Exact filenames are an implementation-plan decision. No unrelated refactor is authorized by this design.

## 33. Boundaries after B1.0

Separate reviewed designs are required before adding:

- persistent PTY/REPL/named terminal sessions;
- session rename, explicit name release/reuse/purge, or aliases-to-aliases;
- host-reboot running-process continuity;
- supervisor crash takeover or replacement;
- supervisor hot upgrade/migration;
- automatic child restart/service-manager semantics;
- remote/multi-machine supervision;
- containers/hermetic execution/security profiles;
- workflow scheduling or daemon planning.

B2 semantic-index/provider work, evidence invalidation optimization, E26 safety checkpoints, and E27 dynamic input tracing remain independent roadmap work and are not prerequisites for B1.0.

## 34. Definition of done

B1.0 is complete only when all of the following hold on one exact final source tree:

1. `persistent:true` is an explicit opt-in and ordinary starts remain the current direct path with no B1 work tax.
2. `session_id` remains authoritative; optional names are convenience-only, byte-exact, bounded, and never automatically rebound.
3. One per-session supervisor owns each persistent non-TTY child and no shared second daemon is introduced.
4. Daemon restart reattaches only through exact authenticated session+generation proof; PID-only reclaim is impossible by construction and tests.
5. Output, input-offset, kill-ID, timeout, and terminal-evidence semantics remain correct while the daemon is absent and after reattach.
6. Canonical ShellBeam store/receipts/events remain daemon-owned; supervisors write only bounded private recovery state.
7. Supervisor loss fails closed to lost/ambiguous and never auto-restarts or signals an unproven process.
8. Host-reboot process continuity, PTY/REPL, rename/reuse, and supervisor takeover remain explicitly out of scope.
9. `inspect.sessions` gives agents bounded name/session discovery without attach/detach choreography or OS scanning.
10. `FeatureNamedSessions` is truthful, versioned, bounded, legacy-safe, and available only with real B1 composition.
11. Direct-session restart semantics and every prior A1-A2.6 authority/evidence/privacy invariant remain green.
12. Fresh native/race/schema/privacy/performance/devctl/exact-fingerprint gates satisfy the implementation plan.
