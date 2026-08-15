# ShellBeam B1.0 Persistent Runtime + Named Sessions Implementation Plan

> Execution rule: implement this plan with superpowers:executing-plans, main agent only. Do not dispatch subagents. Use TDD for every behavior task.

Goal: add opt-in persistent non-TTY sessions whose child/control authority survives ShellBeam daemon restart through one authenticated per-session supervisor, while preserving the existing direct path and its performance when persistent is absent/false.

Design authority: docs/superpowers/specs/2026-08-16-persistent-runtime-named-sessions-design.md at 4673bd8c0d7e3b2db5c70180faf760d359eb109d.

Worktree/branch: /Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/design_agent-execution-layer / ai/execution-observation.

## Global constraints

- No new worktree/branch. No push/PR/merge/rebase/reset/stash unless explicitly requested.
- Main agent only.
- One shipped binary. Supervisor is a private re-exec mode of the existing shellbeam executable, omitted from public help/docs/completions.
- Persistent mode is explicit opt-in; ordinary start must perform zero persistent-registry/supervisor/socket/spool/handshake work.
- B1.0 persistent TTY is rejected; no fallback.
- session_id remains control authority. session_name is lookup-only, 1-128 UTF-8 bytes, byte/case exact, no controls, slash/backslash, leading/trailing Unicode whitespace, no auto-rebind/reuse.
- Reattach requires exact session_id + supervisor generation + capability proof + compatible protocol. PID/PGID/start time/executable/socket name is never ownership proof.
- Canonical operation/session/output/receipt/Event Journal/evidence/telemetry/repro remain daemon/store-owned. Supervisor writes only bounded private recovery state.
- Keep existing public write(session_id,input_offset,chars|eof) and kill(session_id,kill_id,signal) semantics. No public write_id, attach, or detach.
- Hard defaults: inspect 25/100; input records 4096; input-record metadata 1 MiB; kill records 256; handshake 2s; startup concurrency 16; startup total budget 5s; spool <= configured session-output limit; persistent sessions count against live-session capacity.
- Capability discovery is composition-only and side-effect free.
- Native macOS acceptance is mandatory here. Linux native acceptance is required before claiming cross-platform production-ready; otherwise record NOT_RUN.
- Every implementation task: RED test -> prove expected failure -> minimal GREEN -> focused/race verification -> coherent commit.

## Responsibility map

- internal/core/persistentsession: protocol-neutral state, validation, inspect contracts/limits.
- internal/core/operation: freeze persistent/name into request identity and durable reservation.
- internal/adapter/store: canonical persistent binding/name index + offset-aware output reconciliation.
- internal/adapter/supervisor: private protocol/auth/socket/bootstrap/spool/ledgers/runtime/client.
- internal/app/persistentsession: orchestration, inspect projection, startup reconciliation.
- internal/app/daemon: narrow route direct vs persistent, control proxy, shutdown/process-inspection integration.
- cmd/shellbeam: composition + private same-binary supervisor entry point.
- IPC/MCP/schema: modern start fields + inspect.sessions only.
- Event Journal: four lifecycle event kinds, exactly once.

---

### Task 1 - Core contracts, request identity, failures, capability vocabulary

Files: create internal/core/persistentsession/{types.go,types_test.go}; modify internal/core/operation/{intent.go,intent_test.go,persistence.go}; internal/core/failure/failure.go; internal/core/capability/{catalog.go,catalog_test.go}; internal/app/daemon/{types.go,admission.go,bindings.go}; internal/adapter/store/{reservation.go,v2_reservation_test.go}.

- [x] Add lifecycle/ownership enums, validation, exact B1 constants, inspect request/result types.
- [x] RED tests for valid/invalid names, lifecycle validation, persistent TTY rejection, name-without-persistent rejection.
- [x] Extend operation.Intent, operation.Reservation, and daemon.StartRequest with Persistent bool, SessionName string.
- [x] Persistent modern starts use a new request-fingerprint encoding including both fields. Ordinary existing fingerprints stay byte-compatible.
- [x] Add reservation schema 4 for persistent starts. Schemas 1/2/3 keep their existing meanings; schema 3 remains typed-project-command. Schema 4 accepts shell/argv/project-command execution while durably encoding persistent intent/name before spawn.
- [x] Replay tests: same operation/same persistent metadata replays; changed mode/name conflicts; old schemas remain readable.
- [x] Add stable B1 failure codes/reasons without endpoint/capability/PID leakage.
- [x] Add Catalog.WithNamedSessions(...); baseline remains unavailable until real composition.
- [x] Run focused tests across core/daemon/store plus race on core packages.
- [x] Commit: feat: define persistent session contracts

### Task 2 - Canonical persistent binding/name registry and bounded listing

Files: create internal/adapter/store/{persistent_sessions.go,persistent_session_cursor.go,persistent_sessions_test.go,persistent_sessions_fault_test.go}; modify repository.go, internal/app/daemon/store_port.go, internal/core/persistentsession/types.go.

- [x] RED tests for idempotent binding creation, permanent B1.0 name reservation, exact-name replay, conflicting name/session, corruption, deterministic list/pagination.
- [x] Canonical public-safe binding fields: schema/session/operation/activity/workspace/name/persistent/supervision/continuity/generation/opaque endpoint ref/lifecycle/timestamps.
- [x] State layout uses private atomic/no-follow helpers; name index key may hash name but record stores exact name and session to detect collision/mismatch.
- [x] Binding creation requires an existing persistent reservation; binding never authorizes a second spawn independently.
- [x] Add deterministic ListPersistentBindings with exact filters, default 25/max100, opaque cursor bound to normalized filters.
- [x] Fault matrix: reservation->name claim, name claim->binding ambiguity, conflicting retry, corrupt claim/binding.
- [x] Focused/race tests.
- [x] Commit: feat: persist canonical persistent sessions

### Task 3 - Private supervisor protocol, authentication, private state boundary

Files: create internal/adapter/supervisor/{protocol.go,protocol_test.go,auth.go,auth_test.go,private_state.go,private_state_test.go,terminal.go,terminal_test.go,socket_unix.go,socket_unix_test.go,owner_unix.go,peer_darwin.go,peer_linux.go}.

- [x] Closed protocol v1 kinds: handshake/status/output/write/signal/wait; reject unknown version/kind/fields.
- [x] High-entropy session+generation capability; secret never argv/public env/log/public error.
- [x] Challenge/proof via stdlib HMAC-SHA256; constant-time compare.
- [x] Generation-bound integrity-protected terminal record.
- [x] Runtime dir/socket/file user-only + no-follow/symlink replacement checks.
- [x] Private layout under <runtime>/supervisors/<session_id>/.
- [x] Tests for wrong session/generation/proof, socket replacement, unsafe permissions, malformed terminal record, secret-sentinel leakage.
- [x] go test + -race ./internal/adapter/supervisor.
- [x] Commit: feat: define private supervisor protocol

### Task 4 - Supervisor-owned child runtime, spool, input/kill ledgers, timeout, terminal freeze

Files: create internal/adapter/supervisor/{runtime.go,runtime_test.go,spool.go,spool_test.go,ledger.go,ledger_test.go,server.go,server_test.go}; create cmd/shellbeam/{command_supervisor.go,command_supervisor_test.go}; modify cmd/shellbeam/command.go; narrow reuse from internal/adapter/process only if required.

- [ ] RED tests: one child spawn, append-only byte offsets, input duplicate/conflict/gap after reopen, kill-ID replay/conflict, timeout without daemon, spool/output-limit failure, terminal freeze ordering.
- [ ] Supervisor exclusively owns persistent child process group, stdin, signal, timeout, wait/reap.
- [ ] Spool bounded by canonical output limit; corruption/gap never fabricated.
- [ ] Input ledger stores bounded identity metadata (kind,offset,length,sha256) + accepted/delivered/EOF state; no canonical raw stdin copy.
- [ ] Kill ledger retains max 256 identities and replays same ID/signal without resignal.
- [ ] Timeout freezes absolute deadline and TERM->grace->KILL state; daemon reconnect does not reset it.
- [ ] Before normal exit after child terminal: drain/freeze output, input accounting, spawn/exit/signal/timeout evidence, integrity-protected terminal record.
- [ ] Add private __supervisor dispatch to same binary, absent from public usage/help.
- [ ] Focused/race tests.
- [ ] Commit: feat: own persistent child runtime

### Task 5 - Daemon-side persistent launch orchestration, exactly-once start, no-tax direct path

Files: create internal/app/persistentsession/{ports.go,service.go,service_test.go}; create supervisor client/launcher; create internal/app/daemon/{persistent.go,persistent_test.go}; modify daemon service/process ports/types and cmd/shellbeam/command_daemon.go.

- [ ] RED instrumentation proves ordinary start makes zero B1 service/store/launcher/socket/spool calls.
- [ ] Persistent retry with same operation_id => one reservation, one binding, one supervisor generation, one child, same session.
- [ ] Route to persistent only after request identity and reservation are frozen; ordinary spawnPreparedStart remains unchanged.
- [ ] Persistent order: reserve operation/session -> create provisioning binding/name -> private bootstrap -> launch same executable -> authenticated readiness -> mark live/running -> return.
- [ ] Ambiguous post-launch boundary never allocates replacement generation/child.
- [ ] Bootstrap secret delivered via inherited descriptor/channel; not argv.
- [ ] Persistent live session uses authenticated attachment-backed process-handle proxy; direct handle unchanged.
- [ ] Focused/race/no-tax tests.
- [ ] Commit: feat: launch opt-in persistent sessions

### Task 6 - Output canonicalization + persistent write/kill/terminal reconciliation

Files: create internal/adapter/store/{persistent_output.go,persistent_output_test.go}; extend app persistent service + daemon persistent routing/control tests.

- [ ] RED output matrix: unseen append, exact overlap replay, partial overlap append suffix, mismatch/gap conflict, append-success/ack-lost retry.
- [ ] Under store lock, compare canonical extent and bytes before append; never duplicate bytes or mark unproven completeness.
- [ ] Route persistent public write to supervisor ledger using same input-offset semantics; retained duplicates remain replayable after daemon restart.
- [ ] Route persistent public kill to supervisor kill ledger using existing kill_id; no current proof => no signal.
- [ ] On supervisor terminal: reconcile all retained output, verify terminal integrity/session/generation, publish existing canonical terminal receipt exactly once, then schedule existing structured/telemetry/evidence derivations once.
- [ ] Mark persistent binding terminal only from canonical/verified truth; private cleanup happens after canonical acknowledgement.
- [ ] Focused/race tests.
- [ ] Commit: feat: reconcile persistent session control

### Task 7 - Startup reattachment, graceful shutdown detach, process-inspection proof

Files: modify store reconcile; app persistent reconcile; daemon shutdown/process_inspect; command daemon; add focused tests.

- [ ] RED: direct unresolved restart behavior unchanged.
- [ ] RED: valid live supervisor => reattached same session; terminal record => canonical terminal; missing/refused/auth/generation/protocol/timeout => lost/ambiguous, no PID signal/relaunch.
- [ ] Split unresolved canonical sessions into direct vs persistent before abandonment.
- [ ] Startup bounded reconciliation: per-session 2s, concurrency16, total5s; classify every retained session before ready.
- [ ] Graceful daemon shutdown detaches persistent attachments only; direct sessions retain current TERM/grace/KILL path.
- [ ] A2.5 ResolveProcessSession: current PID only while authenticated attachment proof is current; after loss return Known=true, Current=false, PID=0.
- [ ] Repeat/race restart tests.
- [ ] Commit: feat: reattach persistent sessions on restart

### Task 8 - Public modern inspect.sessions, schema/capability, Event Journal integration

Files: create app inspect; modify observation event kinds; store events; IPC/MCP/schema; daemon composition/tests.

- [ ] RED closed-schema tests for modern start fields and inspect.sessions; legacy generations reject/omit B1 additions.
- [ ] inspect.sessions exact filters: session_name, activity_id, workspace_id, state, persistent_only default true, opaque continuation, max records 25 default/100 max.
- [ ] Inspect reads canonical metadata + already-established attachment cache only; no handshake/OS/Git/filesystem/network scan.
- [ ] Direct sessions may appear only when persistent_only=false; never gain names/reattach semantics.
- [ ] Capability becomes available only after canonical registry + supervisor runtime/auth + startup reconciliation + inspect projection are composed. Advertise exact versions/limits from spec.
- [ ] Add exactly-once safe events: persistent_session_started, persistent_session_reattached, persistent_session_terminal, persistent_session_lost; no heartbeat/reconnect event.
- [ ] MCP remains exactly one public tool; guidance says resolve forgotten name via inspect, then control by session_id.
- [ ] Schema/race/one-tool tests.
- [ ] Commit: feat: expose persistent session discovery

### Task 9 - Native crash/restart/privacy/performance acceptance

Files: create tests/integration/persistent_runtime_test.go, cmd/shellbeam/persistent_runtime_acceptance_test.go, supervisor privacy tests; modify test-impact mapping only if proven necessary.

- [ ] Real binary isolated roots: start persistent long-running child + ordinary direct child; hard-kill daemon only.
- [ ] Restart daemon: persistent reattaches same session/name, direct uses existing abandoned/ambiguous semantics, output cursor byte-exact, write offset and kill-ID semantics continue.
- [ ] Child exits while daemon absent: supervisor reaps/freezes; restart publishes one canonical terminal receipt and downstream derivations once.
- [ ] Timeout fires while daemon absent; restart never extends deadline.
- [ ] Kill/corrupt supervisor/capability/generation/terminal state: classify lost/ambiguous; no PID reclaim/signal.
- [ ] Privacy sentinels absent from public receipts/bindings/Event Journal/MCP inspect/logs/errors; private secret/socket/bootstrap remain private.
- [ ] Ordinary compatible start->poll->terminal performs zero B1 registry/supervisor/socket/spool/ledger work and does not weaken existing <=5ms p95 / <=10ms p99 incremental admission gate.
- [ ] Measure/report persistent-start p50/p95/p99 without inventing threshold.
- [ ] Fresh focused -count=1, repeat -count=3, relevant -race, devctl test --dirty --base origin/main --json, devctl check --json.
- [ ] Native macOS B1 process/socket acceptance must pass. Record Linux native PASS only if actually run; otherwise NOT_RUN.
- [ ] Commit: test: verify persistent runtime continuity

### Task 10 - Exact-source checkpoint and clean handoff

- [ ] Mark Tasks 1-9 complete in this plan; ensure only intended plan bytes remain dirty.
- [ ] Fresh exact final gates: go mod verify; go test ./... -count=1; relevant full race including core/persistent/supervisor/store/app/daemon/ipc/mcp/cmd/integration; go run ./tools/devctl check --json; go run ./tools/devctl test --dirty --base origin/main --json; fresh B1 acceptance.
- [ ] Anti-goal scans: focused B1 daemon references only; one MCP AddTool; no hidden exec in core/app orchestration; no PID-only reclaim branch.
- [ ] Record exact source fingerprint and receipt/checkpoint evidence under ignored .build/b10/.
- [ ] Stage only final plan, git diff --cached --check, devctl commit-gate --json; commit-gate fingerprint must equal checkpoint fingerprint.
- [ ] Commit: test: checkpoint persistent runtime named sessions
- [ ] Postcommit devctl check --json same fingerprint; working tree empty.
- [ ] Final report includes branch/worktree/HEAD/commit chain/fingerprint/public fields/limits/restart continuity/privacy/no-tax/native-platform status/gates and push=NO, PR=NO, merge=NO.

## Completion gate

B1.0 is complete only when one exact final source tree proves:

1. ordinary starts keep the current direct owner and zero B1 work tax;
2. persistent intent/name is durably frozen before supervisor/child spawn;
3. retained names are bounded, byte-exact, lookup-only, never silently rebound;
4. retries/lost responses create exactly one supervisor generation/child per persistent operation;
5. reattach is authenticated exact session+generation proof; PID-only reclaim is absent;
6. output replay is byte-exact across crash windows; mismatch/gap fails closed;
7. input-offset and kill-ID semantics survive daemon restart without new public choreography;
8. timeout remains active while daemon absent;
9. child terminal while daemon absent becomes exactly one canonical terminal receipt;
10. graceful daemon shutdown detaches persistent sessions while direct shutdown semantics remain unchanged;
11. supervisor loss/auth/protocol/generation failure becomes lost/ambiguous and never signals unproven PID;
12. inspect.sessions is bounded/paginated, no attach/OS scan;
13. FeatureNamedSessions is truthful/composition-only/versioned/legacy-safe;
14. four persistent lifecycle events are exactly once and secret-safe;
15. process inspection reports PID only while ownership proof is current;
16. no second MCP tool, second shipped binary, shared supervisor daemon, PTY/REPL, host-reboot continuity, auto-restart, takeover, remote execution, or daemon planning is introduced;
17. final full/race/schema/privacy/native/no-tax/devctl/commit-gate/fingerprint evidence is green, with unavailable Linux native lane reported NOT_RUN.
