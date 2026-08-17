# E27 Dynamic Input Tracing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement E27 Dynamic Input Tracing as an opt-in, advisory experimental provider that can observe bounded dependency-related activity for direct non-TTY execution without weakening ShellBeam's execution/evidence authority or ordinary-start performance.

**Architecture:** Add a provider-neutral E27 core/app contract around `trace_mode=off|best_effort|required`, immutable request/instrumentation bindings, bounded derived trace records, and one-tool inspection. The first provider is Darwin-only `dyld-interpose/v1`: ShellBeam lazily compiles an embedded C interpose shim into private state on the first explicit trace request, injects it only into that execution, receives bounded private Unix-datagram events, and materializes a redacted advisory record after the authoritative terminal receipt. This provider intentionally advertises only partial coverage, never `complete_for_owned_tree`; `required` therefore fails before spawn. Linux and unsupported Darwin execution classes remain explicitly unavailable rather than being normalized into a false cross-platform guarantee.

**Tech Stack:** Go 1.26+, existing ShellBeam durable store/daemon/IPC/MCP/Event Journal primitives, POSIX Unix datagram sockets, Darwin `DYLD_INSERT_LIBRARIES`/`__DATA,__interpose`, `/usr/bin/clang` used lazily only for explicit provider preparation, checked-in/embedded C source text, existing `tools/devctl` structural/affected/commit gates.

## Global Constraints

- Preserve one root Go module, one shipped `shellbeam` binary, one local daemon, and exactly one MCP tool named `local_shell`; do not ship a second helper binary or dylib.
- E27 is experimental/provider-scoped and advisory. It produces `observed_input_scope`, never `proven_input_scope`; `authority=advisory` and `may_have_unobserved_dependencies=true` are invariant.
- `trace_mode=off` is the default and performs zero tracer/provider work, zero runtime compilation, zero provider subprocesses, zero additional synchronous durability barriers, and no trace semantics in the ordinary execution fingerprint.
- `best_effort` may proceed untraced only when preparation failed before behavior-affecting instrumentation was applied; once instrumentation is active, its identity/effect is frozen in execution/environment provenance.
- `required` belongs to the caller request fingerprint and must establish the requested pre-exec contract before child spawn or fail before spawn. `dyld-interpose/v1` does not prove first-instruction/full-owned-tree coverage, so v1 required requests return `input_trace_required_unavailable` without spawning.
- Darwin `dyld-interpose/v1` advertises `instrumentation_effect=environment_affecting`, `pre_exec_coverage=false`, no `complete_for_owned_tree` class, and only the exact partial/unsupported matrix implemented and tested.
- E27 v1 supports only direct, non-TTY raw shell/argv and non-persistent typed project-command execution. Persistent and TTY trace requests are explicit unsupported/unavailable; ordinary persistent/TTY execution remains unchanged when tracing is off.
- Stable E27 failures are exactly `input_trace_provider_unavailable`, `input_trace_required_unavailable`, `input_trace_startup_budget_exceeded`, `input_trace_unsupported`, `input_trace_partial`, `input_trace_budget_exceeded`, `input_trace_late_attach`, `input_trace_ownership_lost`, and `input_trace_not_found`; public details use only reviewed bounded reason/provider/platform/budget identifiers.
- Linux E27 provider is unavailable in this plan. Linux compile-only evidence is not runtime/provider PASS. Linux native is `NOT_RUN` on a Darwin host.
- No file contents, environment values, stdin contents, network payloads, raw arbitrary external absolute paths, deterministic public path hashes, or private socket/dylib/log paths may enter public trace records, events, receipts, evidence, telemetry, repro, MCP output, or normal logs.
- Raw provider event bytes are private 0600 state, bounded during capture, and deleted after successful public materialization. Public derived records use existing private-store ownership rules and bounded retention.
- Tracing never broadens ShellBeam process-control authority and never rewrites an independently observed child outcome. Provider/materializer failure cannot make terminal publication ambiguous.
- Unsupported/late/restart/ownership-loss channels are explicit and can only downgrade completeness. Gaps are never stitched into apparent completeness.
- Global warm-admission regression remains p95 incremental <= 5 ms and p99 <= 10 ms for enabled-but-unused capabilities; these are global deltas, not additive per-feature budgets.
- Keep production files <= 500 lines and functions <= 80 lines under `devctl`; create focused E27 files rather than growing `input.go`, `call.go`, `protocol_v2.go`, `catalog.go`, `service.go`, or `command_daemon.go` past structural limits.

## Locked v1 public/provider vocabulary

```text
trace_mode: off | best_effort | required
trace status: unavailable | pending | terminal
trace outcome: complete | partial | budget_exceeded | provider_unavailable | unsupported | ownership_lost | materialization_failed
trace authority: advisory
path class: repo_relative | workspace_external_redacted | system_classified
coverage quality: unsupported | partial | complete_for_owned_tree
instrumentation effect: none | environment_affecting
provider: dyld-interpose/v1 (darwin only)
```

`dyld-interpose/v1` initial matrix:

```text
filesystem_reads             partial
filesystem_metadata_queries  partial
directory_enumerations       partial
filesystem_writes            partial
executed_binaries            partial
loaded_libraries             partial
environment_names_observed   unsupported
network_attempts              unsupported
child_processes               partial
pre_exec_coverage             false
```

Initial hard bounds:

```text
MaxRawEvents             = 32768
MaxUniqueResources       = 2048
MaxPublicResources       = 512
MaxExternalResources     = 128
MaxRawEventBytes         = 4096 per datagram
MaxPrivateRawBytes       = 8 MiB per trace
MaxPublicRecordBytes     = 512 KiB
MaxRetainedTraceRecords  = 128
MaxTraceCaptureDuration  = 1 hour
TraceStartupBudget       = 2 seconds
WorkerQueueDepth         = 32
```

---

### Task 1: Core E27 contracts, failures, capability vocabulary, and privacy invariants

**Files:**
- Create: `internal/core/inputtrace/types.go`
- Create: `internal/core/inputtrace/validation.go`
- Create: `internal/core/inputtrace/fingerprint.go`
- Test: `internal/core/inputtrace/types_test.go`
- Test: `internal/core/inputtrace/validation_test.go`
- Create: `internal/core/capability/input_trace.go`
- Test: `internal/core/capability/input_trace_test.go`
- Modify: `internal/core/capability/catalog.go` only for the new feature/limit fields
- Modify: `internal/core/failure/failure.go`
- Create: `internal/core/failure/input_trace_test.go`

**Interfaces:**
- Produces `inputtrace.Mode`, `Request`, `ProviderIdentity`, `InstrumentationBinding`, `CoverageMatrix`, `Resource`, `Record`, `Summary`, `Inspection`, and the exact v1 limits above. `InstrumentationBinding` includes a caller-safe opaque `TraceID`, normalized mode, provider identity, platform, instrumentation fingerprint/effect, pre-exec-coverage boolean, exact matrix, and activation/status metadata but no socket/dylib/raw-log path.
- Produces `capability.FeatureInputTracing` and `Catalog.WithInputTracing(...)` that only advertises valid experimental provider matrices.
- Produces the nine stable E27 failure codes from the approved spec.

- [ ] **Step 1: Write RED validation/privacy tests.** Cover mode normalization (omitted/`off` equivalent), provider identity, partial/unsupported matrix legality, `complete_for_owned_tree` rejection when pre-exec/full-owned-tree prerequisites are absent, instrumentation fingerprint/effect consistency, bounded resource arrays, path classes, redacted external identities, record lifecycle/outcome, advisory-only authority, `may_have_unobserved_dependencies=true`, truncation forcing partial, and reflection tests forbidding content/value/payload/private-path fields.
- [ ] **Step 2: Write RED capability/failure tests.** Baseline E27 unavailable; valid Darwin partial matrix advertises exact provider/platform/maturity/effect/limits; invalid matrix stays unavailable; `Clone` deep-copies E27 slices/maps; all nine failure codes expose only bounded safe detail keys.
- [ ] **Step 3: Run RED.** `go test ./internal/core/inputtrace ./internal/core/capability ./internal/core/failure -run 'InputTrace|Tracing' -count=1`. Expected compile/test failure because E27 contracts do not exist.
- [ ] **Step 4: Implement minimal pure contracts.** Use deterministic JSON+SHA-256 only for request/instrumentation/derivation metadata, never as public arbitrary-path identity. External paths use caller-opaque ordinals such as `external-1` in public records.
- [ ] **Step 5: Run GREEN/race/hygiene.** `go test ./internal/core/inputtrace ./internal/core/capability ./internal/core/failure -count=1`; `go test -race ./internal/core/inputtrace ./internal/core/capability ./internal/core/failure -count=1`; `git diff --check`; `go run ./tools/devctl check --json`.
- [ ] **Step 6: Commit.** `feat: define experimental input tracing contracts`.

---

### Task 2: Caller request fingerprints, frozen instrumentation binding, and durable reservation compatibility

**Files:**
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/project_command.go`
- Modify: `internal/core/operation/execution.go`
- Modify: `internal/core/operation/persistence.go`
- Create: `internal/core/operation/input_trace_test.go`
- Modify: `internal/adapter/store/reservation.go`
- Create: `internal/adapter/store/input_trace_reservation_test.go`
- Modify: `api/schema/operation-v2.json`
- Modify: `api/schema/operation-v3.json`
- Create: `api/schema/input_trace_operation_test.go`

**Interfaces:**
- `operation.Intent.TraceMode inputtrace.Mode` and `TypedRequestIntent.TraceMode inputtrace.Mode` participate in request fingerprints only when normalized mode is not `off`, preserving all historical off/omitted hashes exactly.
- `operation.ExecutionSpec.EnvironmentAdditions []EnvironmentEntry` is ephemeral spawn control and is never serialized in public/persisted reservation JSON.
- `operation.Reservation.Trace *inputtrace.InstrumentationBinding` stores only safe frozen public instrumentation/provenance metadata for schema v2/v3/v4 reservations; v1 rejects it.
- Active instrumentation contributes an exact binding digest to the execution fingerprint; an unavailable best-effort request does not invent instrumentation semantics.

- [ ] **Step 1: Write RED fingerprint compatibility tests.** Prove omitted/off keeps exact historical request/execution fingerprints; best-effort/required change request identity; typed/raw fingerprints bind trace mode; different active instrumentation fingerprints/effects change execution identity; response controls do not.
- [ ] **Step 2: Write RED reservation/schema tests.** v2/v3/v4 round-trip a safe trace binding; v1 rejects it; unknown/private environment additions cannot serialize; invalid binding fails strict store validation; same operation/request replay reuses the frozen stored binding and does not permit caller/provider rebinding.
- [ ] **Step 3: Run RED.** `go test ./internal/core/operation ./internal/adapter/store ./api/schema -run 'InputTrace|TraceMode|Historical' -count=1`.
- [ ] **Step 4: Implement minimal fingerprint/persistence changes.** Version only the non-off fingerprint payloads; do not alter old JSON payload shapes. Add a dedicated trace execution digest field/helper rather than exposing private provider paths.
- [ ] **Step 5: GREEN/race/regression.** `go test ./internal/core/operation ./internal/adapter/store ./api/schema -count=1`; `go test -race ./internal/core/operation ./internal/adapter/store -run 'InputTrace|Reservation|Replay' -count=1`; `git diff --check`.
- [ ] **Step 6: Commit.** `feat: bind input tracing execution identity`.

---

### Task 3: Provider-neutral pre-spawn orchestration and zero-tax daemon integration

**Files:**
- Create: `internal/app/inputtrace/ports.go`
- Create: `internal/app/inputtrace/service.go`
- Test: `internal/app/inputtrace/service_test.go`
- Create: `internal/app/daemon/input_trace.go`
- Create: `internal/app/daemon/input_trace_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/admission.go` only at the narrow request/bind seam
- Modify: `internal/app/daemon/bindings.go` only to freeze safe trace binding and effective execution fingerprint
- Modify: `internal/app/daemon/project_command.go` only to pass trace mode through typed requests

**Interfaces:**

```go
type Preparer interface {
    Prepare(context.Context, PrepareRequest) (Prepared, error)
}
type Prepared interface {
    Binding() inputtrace.InstrumentationBinding
    EnvironmentAdditions() []operation.EnvironmentEntry
    Abort() error
}
type Finalizer interface {
    Finalize(context.Context, inputtrace.InstrumentationBinding) (ProviderSnapshot, error)
}
```

`daemon.Options` gains an optional `InputTracePreparer` and `InputTraceWorker`. Preparation runs only after replay lookup proves the operation is new and only when normalized trace mode is non-off.

- [ ] **Step 1: Write RED orchestration tests.** `off` calls no trace interface; exact replay performs zero provider work; required unavailable/startup-timeout returns the reserved E27 failure before `ProcessOwner.Start`; best-effort prepare failure proceeds untraced with safe unavailable trace metadata; active preparation freezes binding and environment additions before spawn; reservation failure calls `Prepared.Abort`; TTY/persistent trace requests are explicit unsupported while off remains unchanged.
- [ ] **Step 2: Write RED no-tax tests.** Fake trace preparer panics if ordinary start reaches it; ordinary raw/typed/persistent start with trace omitted/off preserves existing owner/store ordering and fingerprints.
- [ ] **Step 3: Run RED.** `go test ./internal/app/inputtrace ./internal/app/daemon -run 'InputTrace|TraceMode|NoTax|Replay' -count=1`.
- [ ] **Step 4: Implement minimal pre-spawn orchestration.** Replay lookup remains before provider preparation. Required failures do not spawn. Best-effort unavailability injects no environment. Active bindings are copied into reservation before commit; commit-lost races abort the losing prepared provider instance and use stored authority.
- [ ] **Step 5: GREEN/race.** `go test ./internal/app/inputtrace ./internal/app/daemon -count=1`; `go test -race ./internal/app/inputtrace ./internal/app/daemon -run 'InputTrace|Start|Replay|NoTax' -count=1`; `git diff --check`.
- [ ] **Step 6: Commit.** `feat: prepare tracing before execution`.

---

### Task 4: Process environment injection with no ordinary-path semantic drift

**Files:**
- Modify: `internal/adapter/process/argv.go`
- Modify: `internal/adapter/process/owner_unix.go`
- Modify: `internal/adapter/process/pty_unix.go` only to reject/ignore impossible trace additions consistently; E27 v1 trace requests never reach TTY spawn
- Create: `internal/adapter/process/input_trace_env_test.go`

**Interfaces:**
- `operation.EnvironmentEntry{Key, Value}` is validated as an internal spawn-control pair; E27 uses only `DYLD_INSERT_LIBRARIES`, `SHELLBEAM_TRACE_SOCKET`, `SHELLBEAM_TRACE_PROTOCOL`, and `SHELLBEAM_TRACE_ID`.
- If `EnvironmentAdditions` is empty, `exec.Cmd.Env` remains nil and inherits the environment exactly as before.
- If non-empty, merge against `os.Environ()` deterministically with trace keys replacing same-named inherited keys; do not log or persist values.

- [ ] **Step 1: Write RED process tests.** Ordinary shell/argv leaves `cmd.Env=nil`; traced test binary sees the four additions; inherited non-trace variables remain; duplicate trace keys are rejected before spawn; no trace values appear in spawn error details; TTY path cannot accidentally accept active E27 additions.
- [ ] **Step 2: Run RED.** `go test ./internal/adapter/process -run 'InputTrace|EnvironmentInheritance|Argv' -count=1`.
- [ ] **Step 3: Implement the smallest env merge helper.** Keep command binding/executable resolution unchanged; only set `cmd.Env` when additions are non-empty.
- [ ] **Step 4: GREEN/race.** `go test ./internal/adapter/process -count=1`; `go test -race ./internal/adapter/process -run 'InputTrace|Environment|Owner' -count=1`; `git diff --check`.
- [ ] **Step 5: Commit.** `feat: inject bounded trace instrumentation environment`.

---

### Task 5: Darwin `dyld-interpose/v1` private compiler, collector, and bounded raw event protocol

**Files:**
- Create: `internal/adapter/inputtrace/dyld/provider_darwin.go`
- Create: `internal/adapter/inputtrace/dyld/provider_other.go`
- Create: `internal/adapter/inputtrace/dyld/compiler_darwin.go`
- Create: `internal/adapter/inputtrace/dyld/source_darwin.go`
- Create: `internal/adapter/inputtrace/dyld/collector_darwin.go`
- Create: `internal/adapter/inputtrace/dyld/protocol.go`
- Create: `internal/adapter/inputtrace/dyld/private_state_darwin.go`
- Test: `internal/adapter/inputtrace/dyld/compiler_darwin_test.go`
- Test: `internal/adapter/inputtrace/dyld/collector_darwin_test.go`
- Test: `internal/adapter/inputtrace/dyld/provider_darwin_test.go`
- Test: `internal/adapter/inputtrace/dyld/privacy_darwin_test.go`

**Interfaces:**
- `dyld.New(stateDir string) *Provider` does no compile/socket work.
- `Provider.Health(context.Context)` checks platform, `/usr/bin/clang`, private-root safety, and returns truthful availability without compiling the shim.
- `Prepare` lazily builds a private dylib from embedded C source under 0700/0600 state, computes instrumentation fingerprint from source/compiler/provider contract, opens a private 0600-equivalent Unix datagram endpoint, and returns active partial binding + ephemeral env additions.
- `Finalize` stops only that trace collector, closes/unlinks the socket, returns bounded private raw-event metadata/snapshot, and never touches process ownership.

**Private wire protocol:** fixed bounded datagram header (`version`, event class, flags, pid, path length) plus raw path bytes <= 4096. No file bytes, env values, socket payloads, or network payloads.

- [ ] **Step 1: Write RED private-root/compiler tests.** 0700 roots, symlink/non-owner/world-readable rejection, lazy compile only on explicit `Prepare`, compiler missing/failure -> typed provider unavailable, generated dylib never tracked/shipped, source/compiler identity changes instrumentation fingerprint, repeated prepare reuses exact verified private artifact.
- [ ] **Step 2: Write RED collector/budget tests.** Bound datagram size/raw event count/unique resources/private bytes/duration; malformed packets are counted/dropped safely; budget exceed marks truncated; send-after-finalize cannot resurrect a trace; private raw file is 0600 and never exposed publicly.
- [ ] **Step 3: Write RED native interpose tests.** Compile a tiny fixture binary and prove interception of representative `open`, metadata query, directory enumeration, write mutation, `execve`, and `dlopen`; prove system/hardened or uninjected descendants can create gaps and therefore matrix remains partial; no test may promote `complete_for_owned_tree`.
- [ ] **Step 4: Run RED.** `go test ./internal/adapter/inputtrace/dyld -run 'Compiler|Collector|Interpose|Privacy|Budget' -count=1`.
- [ ] **Step 5: Implement embedded C shim and provider.** Use `DYLD_INTERPOSE`, a reentrancy guard, constructor-opened AF_UNIX datagram socket, best-effort nonblocking sends, no environment-value observation, no network hook, no process signalling. Capture only path/resource metadata needed by the locked matrix.
- [ ] **Step 6: GREEN/native repetition/race.** `go test ./internal/adapter/inputtrace/dyld -count=3`; `go test -race ./internal/adapter/inputtrace/dyld -count=1`; `git diff --check`.
- [ ] **Step 7: Commit.** `feat: add darwin dyld input trace provider`.

---

### Task 6: Normalize/redact provider snapshots and persist bounded advisory trace records

**Files:**
- Create: `internal/app/inputtrace/materialize.go`
- Create: `internal/app/inputtrace/normalize.go`
- Test: `internal/app/inputtrace/materialize_test.go`
- Test: `internal/app/inputtrace/normalize_test.go`
- Create: `internal/adapter/store/input_trace.go`
- Create: `internal/adapter/store/input_trace_paths.go`
- Create: `internal/adapter/store/input_trace_test.go`
- Modify: `internal/adapter/store/repository.go` only for narrow E27 initialization/mutex fields
- Modify: `internal/core/observation/event.go` for `input_trace_recorded` / `input_trace_truncated`
- Create: `internal/core/observation/input_trace_event_test.go`
- Create: `internal/adapter/store/input_trace_events.go`
- Create: `internal/adapter/store/input_trace_events_test.go`

**Interfaces:**
- Materialization reloads durable terminal receipt + reservation, verifies trace binding authority, finalizes the provider snapshot, normalizes paths, derives a deterministic record key from receipt digest + safe provider/instrumentation/config metadata, then persists one immutable `inputtrace.Record`.
- Path normalization: canonical path under workspace root -> `repo_relative`; recognized system roots -> bounded `system_classified` category; all other absolute/unresolved paths -> sequential `workspace_external_redacted` identities (`external-1`, ...) with no raw path/hash.
- Store exposes `PutInputTraceRecord`, `LoadInputTraceByOperation`, and bounded `InspectInputTrace`; public retention max 128 records/512 KiB each, oldest-first. Private raw provider data is deleted only after durable public record commit.

- [ ] **Step 1: Write RED normalization/privacy tests.** Repo-relative normalization, `..`/symlink/root escape fails/redacts, system classification, external ordinal stability within one record but no cross-record dictionary identity, duplicate coalescing, resource limit/truncation, no raw home/temp/provider paths or secret-like fixture values in public JSON.
- [ ] **Step 2: Write RED authority/restart tests.** Only a durable matching terminal receipt can materialize; provider unavailable/ownership-lost/restart gap becomes partial/unavailable trace outcome without changing receipt; duplicate scheduling is idempotent; conflicting derived record is rejected; private raw deletion follows public durability.
- [ ] **Step 3: Write RED Event Journal tests.** Exactly-once small `input_trace_recorded`/`input_trace_truncated` obligations contain only operation/trace IDs and bounded counts/outcome, never resource paths; event failure never changes trace truth.
- [ ] **Step 4: Run RED.** `go test ./internal/app/inputtrace ./internal/adapter/store ./internal/core/observation -run 'InputTrace|TraceEvent|Redact' -count=1`.
- [ ] **Step 5: Implement minimal normalizer/store/materializer.** Never use trace absence to mutate evidence/currentness; record always has advisory authority and `may_have_unobserved_dependencies=true`.
- [ ] **Step 6: GREEN/race.** `go test ./internal/app/inputtrace ./internal/adapter/store ./internal/core/observation -count=1`; `go test -race ./internal/app/inputtrace ./internal/adapter/store -run 'InputTrace|Observation' -count=1`; `git diff --check`.
- [ ] **Step 7: Commit.** `feat: persist advisory input trace records`.

---

### Task 7: Terminal trace worker, inspection service, and execution/evidence separation proof

**Files:**
- Create: `internal/app/inputtrace/worker.go`
- Test: `internal/app/inputtrace/worker_test.go`
- Create: `internal/app/inputtrace/inspect.go`
- Test: `internal/app/inputtrace/inspect_test.go`
- Create: `internal/app/daemon/input_trace_worker.go`
- Create: `internal/app/daemon/input_trace_worker_test.go`
- Modify: terminal finalization call sites only to schedule the E27 worker after durable terminal publication
- Modify: `internal/core/repro/capsule_test.go` to add the E27 privacy/non-authority regression
- Modify: evidence tests only to prove trace absence/presence never narrows evidence validity

**Interfaces:**
- `InputTraceWorker.ScheduleTerminal(context.Context, receipt.Receipt) error` is bounded/nonblocking like structured/telemetry/evidence workers and schedules only reservations with non-off trace metadata.
- `inputtrace.Service.Inspect(ctx, operationID, maxResources)` returns `unavailable|pending|terminal`, safe summary, bounded resources, and detailed coverage only on explicit inspection.

- [ ] **Step 1: Write RED worker tests.** off reservation -> zero schedule; traced terminal schedules once after receipt durability; queue full/materializer failure does not change terminal result; replay does not duplicate trace truth/event; daemon restart with unfinished trace marks ownership-loss/partial rather than fabricating completion.
- [ ] **Step 2: Write RED evidence/repro authority tests.** Trace record can expose an extra observed repo-relative resource while `may_have_unobserved_dependencies` remains true; no code path marks evidence current/narrower because a resource was not observed; repro/evidence records contain trace refs/summary only if explicitly designed, never raw provider data.
- [ ] **Step 3: Run RED.** `go test ./internal/app/inputtrace ./internal/app/daemon ./internal/core/repro ./internal/app/evidence -run 'InputTrace|TraceWorker|Evidence' -count=1`.
- [ ] **Step 4: Implement worker/inspection scheduling.** Reuse the existing post-terminal worker pattern and bounded queue. Materialization remains non-authoritative and asynchronous.
- [ ] **Step 5: GREEN/race.** `go test ./internal/app/inputtrace ./internal/app/daemon ./internal/core/repro ./internal/app/evidence -count=1`; `go test -race ./internal/app/inputtrace ./internal/app/daemon -run 'InputTrace|Terminal|Worker' -count=1`; `git diff --check`.
- [ ] **Step 6: Commit.** `feat: materialize terminal input traces`.

---

### Task 8: Closed IPC/MCP v2, config/composition, truthful discovery, and one-tool surface

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `api/schema/config-v1.json`
- Create: `cmd/shellbeam/input_tracing.go`
- Create: `cmd/shellbeam/input_tracing_test.go`
- Modify: `cmd/shellbeam/command_daemon.go` only through focused composition helpers
- Create: `internal/adapter/ipc/input_trace.go`
- Create: `internal/adapter/ipc/input_trace_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go` only to add compact fields/action union entries
- Create: `internal/adapter/mcp/trace_input.go`
- Create: `internal/adapter/mcp/trace_call.go`
- Create: `internal/adapter/mcp/input_trace_test.go`
- Modify: `internal/adapter/mcp/input.go` / `call.go` only to delegate to the new helpers and remain <=500 lines
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create: `api/schema/input_trace_test.go`

**Config/public requests:**

```toml
experimental_input_tracing = false
```

```json
{"action":"start","operation_id":"op","cwd":"/repo","command":"go test ./...","trace_mode":"best_effort"}
{"action":"inspect.trace","operation_id":"op","max_resources":128}
```

- [ ] **Step 1: Write RED config/composition tests.** Default false; explicit true changes config hash; disabled discovery unavailable and performs no provider health/compile/private-root work; enabled Darwin healthy advertises exact partial matrix; missing clang/unsafe state leaves daemon healthy with E27 unavailable; Linux composition unavailable.
- [ ] **Step 2: Write RED closed-schema/IPC tests.** `trace_mode` is start-only v2, enum closed, default omission supported, v1 rejects, typed start accepts non-persistent non-TTY trace mode, `inspect.trace` exact fields/results, bounded resources, safe failures, no private fields.
- [ ] **Step 3: Write RED MCP one-tool tests.** Exactly one `local_shell`; start summary says only concise trace requested/active/unavailable status; `inspect.trace` exposes deep coverage/resources; structuredContent parity; no second tool/resource/prompt and no raw private/provider path leakage.
- [ ] **Step 4: Run RED.** `go test ./internal/config ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./api/schema -run 'InputTrace|TraceMode|OneTool|Config' -count=1`.
- [ ] **Step 5: Implement opt-in composition and routing.** Provider `Health` may inspect compiler/platform/state safety but must not compile/start sockets. Compilation/collector work starts only from explicit trace preparation.
- [ ] **Step 6: GREEN/race/dirty.** `go test ./internal/config ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./api/schema -count=1`; `go test -race ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp -run 'InputTrace|Start|OneTool|NoTax' -count=1`; `go run ./tools/devctl test --dirty --base origin/main --json`; `git diff --check`.
- [ ] **Step 7: Commit.** `feat: expose experimental dynamic input tracing`.

---

### Task 9: Native Darwin crash/privacy/gap/performance acceptance

**Files:**
- Create: `cmd/shellbeam/input_trace_acceptance_test.go`
- Create: `tests/integration/input_trace_test.go`
- Modify test helpers only unless acceptance exposes a production defect; any production defect gets its own RED proof and separate commit before acceptance continues.

- [ ] **Step 1: Add real-binary best-effort acceptance.** E27 enabled; traced dynamically linked fixture reads repo file, metadata-checks another, enumerates a directory, writes a bounded file, execs a child, loads a dylib; terminal receipt remains authoritative; `inspect.trace` returns advisory partial normalized resources and exact partial/unsupported matrix.
- [ ] **Step 2: Add off/required/unsupported acceptance.** `off`/omitted performs zero provider compiler/socket/private-state work; `required` fails before spawn with no child; TTY/persistent trace request is explicit unsupported while the same command with trace off still runs.
- [ ] **Step 3: Add gap/restart/crash acceptance.** System/hardened or otherwise non-injected descendant never promotes completeness; provider collector loss/restart becomes ownership-lost/partial; daemon/provider failure after child spawn cannot rewrite child outcome or corrupt operation replay.
- [ ] **Step 4: Add privacy acceptance.** Low-entropy filename/env/network-payload fixtures plus home/temp paths; scan public state, MCP output, events, evidence, telemetry, repro, logs for raw external path/private socket/dylib/raw payload/env values. Sensitive provider bytes may exist only under verified private raw state before materialization and are removed after durable public record.
- [ ] **Step 5: Add broadening-only acceptance.** Trace visibly reports an additional observed repo-relative dependency, while evidence/currentness remains conservative and there is no negative/unobserved-dependency authority field or selector narrowing.
- [ ] **Step 6: Add no-tax/performance acceptance.** Compare same build/config with E27 enabled-but-unused versus disabled using the stabilized large-sample methodology; require p95 incremental <=5 ms / p99 <=10 ms and zero compiler/provider/socket calls. Report explicit traced prepare/materialize p50/p95/p99 without inventing an unapproved threshold; required-start rejection must remain within the explicit 2s startup bound.
- [ ] **Step 7: Native/cross-platform gates.** Darwin native interpose/privacy/gap tests PASS. `GOOS=linux GOARCH=amd64 go test -exec=true` compile-only for all E27 packages. Linux provider/runtime is `NOT_RUN`/unavailable unless actually executed on Linux.
- [ ] **Step 8: Fresh verification.** `go test ./cmd/shellbeam ./tests/integration -run 'InputTrace|E27' -count=3`; `go test -race ./internal/core/inputtrace ./internal/app/inputtrace ./internal/adapter/inputtrace/dyld ./internal/adapter/store ./internal/adapter/process ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/daemon ./cmd/shellbeam ./tests/integration -run 'InputTrace|E27' -count=1`; `go run ./tools/devctl check --json`; `go run ./tools/devctl test --dirty --base origin/main --json`; `git diff --check`.
- [ ] **Step 9: Commit.** `test: verify experimental dynamic input tracing`.

---

### Task 10: Exact-source E27 experimental-ready checkpoint

**Files:**
- Modify only: `docs/superpowers/plans/2026-08-17-dynamic-input-tracing.md`
- Generated evidence: ignored `.build/e27/final-checkpoint.json`

> **Re-frozen checkpoint note (2026-08-17):** The first checkpoint commit `e118f27` was invalidated by a fresh post-checkpoint stress run that reproduced loss of accepted terminal DYLD datagrams. Follow-up `1828a06` adds a RED→GREEN socket-drain regression and drains accepted datagrams before collector finalization. Tasks 1–9 plus this corrective fix are now committed through `1828a06`; Task 10 is re-run from the beginning on these exact tracked bytes, with remaining gate receipts and postcommit proof only in ignored `.build/e27/final-checkpoint.json`. No tracked bytes are mutated after this re-freeze.

- [x] **Step 1: Confirm Tasks 1-9 committed.** Final tracked tree contains only intended Task 10 plan checkbox/note bytes; no provider overclaim, no second shipped artifact, no unrelated roadmap implementation.
- [x] **Step 2: Freeze final tracked plan bytes** before exact gates. Store source fingerprint only in ignored `.build/e27/final-checkpoint.json`; do not mutate tracked proof bytes after freeze.
- [ ] **Step 3: Run exact gates on frozen bytes.** `go mod verify`; `go test ./... -count=1`; full targeted E27+store/process/daemon race suite; `go run ./tools/devctl check --json`; `go run ./tools/devctl test --dirty --base origin/main --json`; focused E27 native acceptance; `git diff --check`.
- [ ] **Step 4: Mechanical anti-goal/privacy scans.** Prove one `AddTool`; one shipped binary/no tracked dylib; no provider work on ordinary start; no `complete_for_owned_tree` advertisement; no `proven_input_scope`; no file/env/network payload fields; no raw external/private paths in public schemas; no process-control expansion; no trace-driven evidence narrowing; no background provider/compiler startup.
- [ ] **Step 5: Record `.build/e27/final-checkpoint.json`.** Exact source fingerprint, precommit HEAD, gate operation/session receipts, provider/platform matrix, instrumentation effect/fingerprint semantics, off/required/unsupported status, no-tax and explicit trace percentiles, privacy/gap/restart evidence, Darwin native status, Linux compile/native status.
- [ ] **Step 6: Stage only final plan**, `git diff --cached --check`, run `go run ./tools/devctl commit-gate --base origin/main --json`, and machine-assert commit-gate fingerprint equals checkpoint fingerprint.
- [ ] **Step 7: Commit.** `test: checkpoint experimental dynamic input tracing`.
- [ ] **Step 8: Postcommit proof.** Rerun `devctl check`, require identical fingerprint, update ignored checkpoint with final HEAD/postcommit receipt, and require fully clean `git status --porcelain=v1 --untracked-files=all`.
- [ ] **Step 9: Final report.** Worktree/branch/HEAD/commit chain/fingerprint; exact Darwin provider matrix; request modes and unsupported classes; advisory/broadening-only authority; privacy; no-tax; crash/restart gap behavior; one binary/one tool; macOS native PASS; Linux native PASS only if actually run else NOT_RUN; `push=NO`, `PR=NO`, `merge=NO`.

## Completion Gate

E27 `dyld-interpose/v1` may be called `experimental-ready` on Darwin only when one exact final source tree proves all of the following:

1. `trace_mode=off`/omitted performs zero provider/compiler/socket work and preserves historical request/execution identity.
2. Best-effort tracing is explicitly behavior-affecting when active; its provider/instrumentation fingerprint is frozen before spawn and participates in effective execution/environment provenance.
3. Required tracing cannot satisfy this provider's first-instruction/full-tree prerequisite and therefore fails before spawn; no required request is silently downgraded to best-effort.
4. No class is advertised `complete_for_owned_tree`; exact partial/unsupported classes and child-process gaps are surfaced without cross-platform normalization.
5. A bounded native trace can report observed repo-relative filesystem/exec/library dependencies while retaining `authority=advisory` and `may_have_unobserved_dependencies=true`; absence never narrows evidence/currentness.
6. Provider budget, late/gap/restart/collector-loss states downgrade to partial/unavailable/ownership-lost and are never stitched into false completeness.
7. No file contents, environment values, stdin contents, network payloads, raw arbitrary external absolute paths, deterministic public path identities, private socket/dylib/log paths, or provider raw bytes leak through public records/events/receipts/evidence/telemetry/repro/MCP/logs.
8. Provider/materializer failure after spawn cannot rewrite or make ambiguous an independently durable terminal execution result, and operation replay remains authoritative.
9. E27 does not expand PID/PGID signalling/control authority, ship a second artifact, start background tracing on daemon boot, or introduce a second MCP tool.
10. Enabled-but-unused E27 stays within global p95 <=5 ms / p99 <=10 ms incremental admission with zero provider/compiler/socket calls.
11. Darwin native interpose/privacy/gap/crash acceptance is green; Linux compile-only is reported as compile evidence and Linux native remains `NOT_RUN` unless actually executed there.
12. Full/race/schema/privacy/native/devctl/commit-gate/postcommit evidence all match one exact source fingerprint and final git status is clean.
