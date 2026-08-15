# ShellBeam A2.5 Environment/Toolchain Fingerprint + Host Process Inspection Implementation Plan

> **Execution note:** Execute this plan on the existing linked worktree
> `/Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/design_agent-execution-layer`
> and branch `ai/execution-observation`. Do not create another worktree. Use the main
> agent only. Follow TDD: meaningful RED before production changes, then GREEN,
> refactor only while green, and verify before every commit.

**Goal:** Implement the approved A2.5 design as two lazy, bounded, secret-safe observation producers behind the existing single `local_shell` tool: versioned environment/toolchain fingerprinting and current-host process inspection.

**Architecture:** Add separate `core/app/adapter` modules for environment and process observation. Explicit `inspect.environment` requests capture or reuse bounded environment/toolchain snapshots; explicit `inspect.process` requests resolve either a current ShellBeam session or a current-user PID and then perform bounded host observation. Ordinary `start`/`poll` must not run tool probes, process-table enumeration, port inspection, or cache refresh. A2.4 evidence may optionally bind only a compatible A2.5 snapshot already frozen at admission by a non-probing cache lookup.

**Tech stack:** Go 1.26.x, existing ShellBeam modular-monolith boundaries, existing IPC v2/MCP v2 closed JSON schemas, fixed local `os/exec` adapters, Darwin/Linux process primitives, existing `devctl` structural/commit gates.

**Approved design:** `docs/superpowers/specs/2026-08-15-environment-toolchain-process-inspection-design.md`

---

## Global invariants and fixed limits

Use these constants unless a live repository contract with a stricter existing limit requires the stricter value:

### Environment

- snapshot schema version: `1`
- environment fingerprint version: `1`
- toolchain fingerprint version: `1`
- relevant environment names: reuse `project.MaxRelevantEnvironment` (`64`)
- built-in toolchain probes: exactly `go`, `node`, `python`, `java`, `rust`
- per-probe timeout: `2s`
- per-probe captured output: `512 bytes`
- in-memory compatible cache entries: `128`
- cache is derived/non-authoritative and does not auto-refresh on command execution
- default freshness: `cached`

### Process

- process observation schema version: `1`
- descendants: `128`
- traversal depth: `8`
- response/metadata budget: `64 KiB`
- observation deadline: `2s`
- optional port records: `64`
- no name-based target selection
- no environment/open-file-content/socket-payload reads

### Public request shapes

Keep the single `local_shell` MCP tool.

`inspect.environment`:

```json
{
  "action": "inspect.environment",
  "workspace_id": "optional ws_...",
  "freshness": "cached | refresh",
  "execution": {
    "mode": "shell | argv",
    "identity": "optional for shell, required for argv"
  }
}
```

If `execution` is omitted, use the daemon's configured shell execution context. `freshness` omitted means `cached`.

`inspect.process`:

```json
{
  "action": "inspect.process",
  "process_target": {
    "kind": "session",
    "session_id": "..."
  },
  "include_ports": false
}
```

or

```json
{
  "action": "inspect.process",
  "process_target": {
    "kind": "pid",
    "pid": 123
  },
  "include_ports": true
}
```

The output must never return raw PATH or environment values.

---

## Task 1 — Core A2.5 contracts, failures, and capability limits

**Files:**
- Create: `internal/core/environment/types.go`
- Create: `internal/core/environment/normalize.go`
- Create: `internal/core/environment/validation.go`
- Create: `internal/core/environment/types_test.go`
- Create: `internal/core/environment/normalize_test.go`
- Create: `internal/core/process/types.go`
- Create: `internal/core/process/validation.go`
- Create: `internal/core/process/types_test.go`
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`

### Step 1.1 — Write environment normalization RED tests

- [x] Step 1.1: Write environment normalization RED tests

Cover:

1. same normalized platform/execution/PATH digest+count/presence/manager facts under fingerprint version 1 => same environment fingerprint regardless input slice order;
2. a changed compatible fact changes the environment fingerprint;
3. changing the fingerprint version changes compatibility and identity;
4. timestamp/cache age/toolchain diagnostics never participate in environment fingerprint;
5. raw PATH is not a field on `PathObservation`; only digest/count/quality are representable;
6. variable presence stores `name + present`, with no value/hash field;
7. toolchain fingerprint is deterministic over sorted supported observations and excludes raw probe output/error text;
8. invalid digest/version/quality/oversized lists are rejected.

Run:

```bash
go test ./internal/core/environment
```

Expected: **RED** because the package/contracts do not exist.

### Step 1.2 — Implement minimum environment core

- [x] Step 1.2: Implement minimum environment core

Implement:

- `SnapshotSchemaVersion = 1`
- `FingerprintVersion = 1`
- `ToolchainFingerprintVersion = 1`
- `MaxRelevantVariables = project.MaxRelevantEnvironment`
- `MaxToolchainProbes = 5`
- `Quality`: `complete | partial | unavailable`
- `ProbeQuality`: `complete | unavailable`
- `Freshness`: `cached | refresh`
- `ExecutionContext{Mode, Identity}`
- `Platform{OS, Architecture}`
- `PathObservation{Digest, EntryCount, Quality}`
- `VariablePresence{Name, Present}`
- `ToolchainManager{Kind, Identity}`
- `ToolchainObservation{Kind, RequestedIdentity, ObservedIdentity, Version, Quality, DiagnosticCode}`
- `Snapshot{SchemaVersion, SnapshotID, CapturedAt, Quality, EnvironmentFingerprint, FingerprintVersion, ToolchainFingerprint, ToolchainFingerprintVersion, Platform, Execution, Path, VariablePresence, ToolchainManager, Toolchains}`
- `Binding{SnapshotID, EnvironmentFingerprint, EnvironmentFingerprintVersion, ToolchainFingerprint, ToolchainFingerprintVersion, CapturedAt}`
- deterministic SHA-256 canonicalization using explicit versioned structs and sorted copies;
- validation helpers that never require or accept raw environment values.

Fingerprint semantics:
- environment fingerprint: OS, arch, execution mode+identity, PATH digest+entry count, sorted presence bitmap, declared manager identity;
- toolchain fingerprint: sorted supported toolchain observations using kind/requested identity/observed identity/version/quality; no raw output or transient error text.

Run the environment package tests again; expect GREEN.

### Step 1.3 — Write process contract RED tests

- [x] Step 1.3: Write process contract RED tests

Cover:

- target is exactly one of `session` or positive `pid`;
- PID 0/negative and process-name selection are impossible/invalid;
- observation validates hard descendant/port limits;
- a process identity cannot claim stability without its stable evidence fields;
- quality values and diagnostic codes are closed;
- `ArgvView` exposes only bounded identity/count/truncation metadata, not arbitrary argument values.

Run:

```bash
go test ./internal/core/process
```

Expected: **RED**.

### Step 1.4 — Implement minimum process core

- [x] Step 1.4: Implement minimum process core

Implement:

- constants listed in Global invariants;
- `TargetKindSession`, `TargetKindPID`;
- `Target{Kind, SessionID, PID}`;
- `Quality{complete, partial, unavailable}`;
- `Relation{shellbeam_root, shellbeam_descendant, external}`;
- `State{running, sleeping, stopped, zombie, exited, unknown}`;
- `Identity{Value, StartTime}` where `Value` is a deterministic hash of stable supported process facts;
- `ArgvView{ExecutableIdentity, ArgumentCount, Truncated}` with no raw argument array;
- `ProcessFact`, `PortObservation`, `Observation`;
- validation that enforces limits and identity honesty.

Run GREEN.

### Step 1.5 — Add typed failures RED → GREEN

- [x] Step 1.5: Add typed failures RED → GREEN

Add public stable codes:

- `environment_observation_unavailable`
- `toolchain_probe_unavailable`
- `toolchain_probe_timeout`
- `toolchain_probe_unsupported`
- `process_not_found`
- `process_access_denied`
- `process_identity_changed`
- `process_observation_incomplete`
- `process_limit_exceeded`
- `port_observation_unavailable`

Only allow safe details such as `toolchain`, `pid`, `reason`, `feature`; never surface raw error text, argv, PATH, or env values.

Add A2.5 failure serialization/privacy tests first, run RED, implement, run GREEN:

```bash
go test ./internal/core/failure
```

### Step 1.6 — Capability contract RED → GREEN

- [x] Step 1.6: Capability contract RED → GREEN

Add exact A2.5 capability fields:

- environment snapshot/fingerprint/toolchain-fingerprint schema/version lists;
- supported built-in toolchain probe IDs;
- process observation schema versions;
- `PortObservationSupported`;
- environment/process hard limits.

Add separate builders:

- `WithEnvironmentObservation(...)`
- `WithProcessInspection(...)`

They must independently enable existing `FeatureEnvironmentFingerprint` and `FeatureProcessInspection`.

Test clone/deep-copy behavior and that invalid zero limits do not advertise a feature.

Run:

```bash
go test ./internal/core/capability ./internal/core/environment ./internal/core/process ./internal/core/failure
go test -race ./internal/core/capability ./internal/core/environment ./internal/core/process ./internal/core/failure
```

### Step 1.7 — Structural gate and commit

- [x] Step 1.7: Structural gate and commit

```bash
git diff --check
go run ./tools/devctl check
git add -- internal/core/environment internal/core/process internal/core/failure/failure.go internal/core/failure/failure_test.go internal/core/capability/catalog.go internal/core/capability/catalog_test.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git commit -m "feat: define environment and process observation contracts"
```

---

## Task 2 — Environment observation core/application and secret-safe cache

**Files:**
- Create: `internal/app/environment/service.go`
- Create: `internal/app/environment/cache.go`
- Create: `internal/app/environment/service_test.go`
- Create: `internal/app/environment/cache_test.go`
- Create: `internal/adapter/environment/host.go`
- Create: `internal/adapter/environment/host_test.go`
- Modify only if sharing a mechanical primitive is cleaner than duplication:
  - `internal/adapter/project/readiness_host.go`
  - `internal/adapter/project/readiness_host_test.go`

### Step 2.1 — RED tests for base host observation

- [x] Step 2.1: RED tests for base host observation

Create a fake environment source so tests never depend on the developer's real secrets.

Cover:

- OS/arch captured;
- shell/argv execution identity normalized;
- PATH digest/count deterministic and raw PATH absent from returned core record;
- empty PATH and repeated/empty entries have documented deterministic count semantics;
- selected environment names are sorted/deduplicated and presence-only;
- low-entropy sentinel values (`x`, `1234`, `secret`) never appear verbatim or as `sha256(value)` in marshaled public snapshot;
- manifest-selected names use the validated `Manifest.RelevantEnvironment` contract.

Run:

```bash
go test ./internal/adapter/environment ./internal/app/environment
```

Expected RED.

### Step 2.2 — Implement base adapter

- [x] Step 2.2: Implement base adapter

`internal/adapter/environment/host.go` owns mechanical host facts:

- `runtime.GOOS`, `runtime.GOARCH`;
- effective PATH read only to compute the versioned digest/count;
- `os.LookupEnv` presence checks for selected names;
- daemon shell identity / caller-supplied direct-exec identity;
- no persistence of raw PATH;
- no arbitrary env-value hashing.

Use a narrow interface so app tests inject fakes.

Do not add tool version execution yet.

### Step 2.3 — RED tests for app service/cache

- [x] Step 2.3: RED tests for app service/cache

Define `environment.InspectRequest`:

- optional `WorkspaceID`;
- `Freshness`;
- optional `ExecutionContext`.

Define ports:

- `HostObserver`;
- `ManifestProvider` returning validated project manifest/manifest digest when workspace-scoped;
- `ToolchainProber` (fake in this task; concrete next task).

Cover:

1. omitted freshness normalizes to `cached`;
2. first cached request captures once; compatible second request reuses exact snapshot ID without re-observation/probe;
3. refresh performs a new capture;
4. cache key changes for workspace manifest digest, selected names/toolchains, execution context, or fingerprint versions;
5. max cache entries is bounded and eviction is safe;
6. cache loss never fabricates historical authority;
7. manifest relevant env names and toolchain declarations are used; no parallel manifest schema;
8. `CachedBinding(...)` returns only a compatible already-cached binding and never captures/probes.

### Step 2.4 — Implement service/cache

- [x] Step 2.4: Implement service/cache

Use a bounded mutex-protected in-memory cache. No goroutine/watcher.

Default selection:
- trusted built-in presence names: a minimal fixed set (`CI`, `TERM`, `SHELL`);
- union with validated `Manifest.RelevantEnvironment`;
- toolchain probe selection is the fixed built-in adapter set, constrained/annotated by validated manifest declarations.

Manager identity:
- derive only from validated declared manager strings;
- normalize deterministically;
- never execute an arbitrary manager command.

`CachedBinding` is the admission-safe lookup used by Task 6; it must be O(1)/bounded and explicitly never call host observation or probes.

Run:

```bash
go test ./internal/app/environment ./internal/adapter/environment
go test -race ./internal/app/environment ./internal/adapter/environment
```

### Step 2.5 — Regression against Project Readiness

- [x] Step 2.5: Regression against Project Readiness

If a mechanical helper was extracted, prove existing readiness semantics remain unchanged:

```bash
go test ./internal/adapter/project ./internal/app/project
go test -race ./internal/adapter/project ./internal/app/project
```

Do not rename readiness-domain fingerprints into A2.5 fingerprints.

### Step 2.6 — Commit

- [x] Step 2.6: Commit

Run `devctl check`, staged diff check, commit-gate, then:

```bash
git commit -m "feat: capture secret-safe environment snapshots"
```

---

## Task 3 — Fixed built-in toolchain probes and cache refresh semantics

**Files:**
- Create: `internal/adapter/environment/probe.go`
- Create: `internal/adapter/environment/probe_test.go`
- Create: `internal/adapter/environment/probe_parsers.go`
- Create: `internal/adapter/environment/probe_parsers_test.go`
- Modify: `internal/app/environment/service.go`
- Modify: `internal/app/environment/service_test.go`

### Step 3.1 — Parser RED tests

- [x] Step 3.1: Parser RED tests

Table-test exact supported parsers:

- Go: fixed `go env GOVERSION` output such as `go1.26.5`;
- Node: fixed `node --version`, `vX.Y.Z`;
- Python: fixed `python3 --version`, `Python X.Y.Z`;
- Java: fixed `java -version`, first bounded version line;
- Rust: fixed `rustc --version`, `rustc X.Y.Z (...)`.

Reject malformed/ambiguous output. No heuristic parser fallback.

Run RED:

```bash
go test ./internal/adapter/environment
```

### Step 3.2 — Probe-runner RED tests

- [x] Step 3.2: Probe-runner RED tests

Inject a fake command runner and prove:

- fixed argv only;
- per-probe `2s` context bound;
- output limited to `512 bytes`;
- timeout -> `toolchain_probe_timeout`;
- missing executable -> unavailable;
- unsupported toolchain -> unsupported;
- parser failure never becomes child execution failure;
- error text/raw probe output is not stored in the observation.

### Step 3.3 — Implement fixed registry

- [x] Step 3.3: Implement fixed registry

Create a static registry with exactly the five supported adapters.

No manifest shell fragments. No package installation. No network.

`go` may share the existing fixed readiness mechanical probe helper if that can be done without changing readiness semantics.

### Step 3.4 — App cache/refresh integration RED → GREEN

- [x] Step 3.4: App cache/refresh integration RED → GREEN

Prove:

- compatible cached inspection performs zero probe calls;
- refresh performs the bounded selected probes;
- one probe failure yields partial snapshot, not whole-request failure;
- toolchain fingerprint changes on normalized version change;
- unsupported declared toolchain is represented as unavailable/unsupported, not executed heuristically;
- command start path is not referenced by probe orchestration.

Run:

```bash
go test ./internal/app/environment ./internal/adapter/environment
go test -race ./internal/app/environment ./internal/adapter/environment
```

### Step 3.5 — Commit

- [x] Step 3.5: Commit

```bash
go run ./tools/devctl check
git diff --check
git add -- internal/app/environment internal/adapter/environment
git diff --cached --check
go run ./tools/devctl commit-gate --json
git commit -m "feat: probe built-in toolchains lazily"
```

---

## Task 4 — Current-host process inspection with ShellBeam session resolution

**Files:**
- Modify: `internal/app/daemon/process_port.go`
- Modify: `internal/app/daemon/service.go`
- Create: `internal/app/daemon/process_inspect.go`
- Create: `internal/app/daemon/process_inspect_test.go`
- Create: `internal/app/process/service.go`
- Create: `internal/app/process/service_test.go`
- Create: `internal/adapter/process/inspect.go`
- Create: `internal/adapter/process/inspect_unix.go`
- Create: `internal/adapter/process/inspect_darwin.go`
- Create: `internal/adapter/process/inspect_linux.go`
- Create: `internal/adapter/process/inspect_test.go`
- Modify: `internal/adapter/process/owner_unix.go`
- Modify: `internal/adapter/process/pty_unix.go`

### Step 4.1 — RED tests for ShellBeam session resolver

- [x] Step 4.1: RED tests for ShellBeam session resolver

Add an optional concrete handle capability, not a new method on the existing `ProcessHandle` interface:

```go
type pidHandle interface { PID() int }
```

Tests first:

1. current live non-terminal session resolves to current daemon PID;
2. current terminal session never follows a reused PID as a live child;
3. session known only through durable store after daemon restart returns known-but-not-current with no PID claim;
4. unknown session stays distinct from arbitrary `process_not_found`;
5. existing fake `ProcessHandle` implementations still compile unchanged.

Then add `PID()` only on concrete POSIX/PTY handles and implement a daemon read-only session resolver.

Run:

```bash
go test ./internal/app/daemon ./internal/adapter/process
```

### Step 4.2 — RED tests for host adapter classification

- [x] Step 4.2: RED tests for host adapter classification

Use injectable filesystem/command/syscall ports where possible; use short real-PID integration tests only for stable POSIX facts.

Prove:

- explicit positive PID current-user process is observable;
- `ESRCH`/missing maps to `process_not_found`;
- `EPERM`/UID mismatch maps to `process_access_denied`, never not-found;
- process identity includes PID + stable start evidence when platform supplies it;
- re-read identity mismatch during one observation maps to `process_identity_changed`;
- executable identity is bounded;
- no child environment is read.

Linux:
- use `/proc/<pid>/stat`, `/proc/<pid>/status`, `/proc/<pid>/exe` only;
- do not read `/proc/<pid>/environ`.

Darwin:
- use fixed `ps`/POSIX queries with bounded output and `kill(pid, 0)` classification;
- no name-based search.

### Step 4.3 — RED tests for bounded traversal

- [x] Step 4.3: RED tests for bounded traversal

App service owns BFS limits.

Cover:

- root + descendants bounded to `MaxDescendants`;
- depth bounded to `MaxTraversalDepth`;
- metadata byte budget bounded;
- overall context deadline bounded;
- limit exhaustion yields partial observation + `truncated=true` + stable diagnostic, not unbounded continuation;
- deterministic parent-before-child order with PID tiebreaking;
- process-table enumeration occurs only inside explicit inspection.

### Step 4.4 — Implement host/process service

- [x] Step 4.4: Implement host/process service

Separate:

- adapter = host facts/classification;
- app = target resolution, BFS, budgets, result quality.

For a ShellBeam target:
- current live session: use resolved current PID and durable session relation;
- terminal or old-daemon session without current PID: return lower-quality known session result without probing that PID;
- never infer current liveness from persisted old PID state.

For explicit PID:
- require current-user authority;
- observe fresh every request;
- bind identity when supported.

`argv_view` must not publish arbitrary argument values. Publish bounded executable identity/count metadata only when safely available; omission is valid.

### Step 4.5 — Race and targeted verification

- [x] Step 4.5: Race and targeted verification

```bash
go test ./internal/app/daemon ./internal/app/process ./internal/adapter/process
go test -race ./internal/app/daemon ./internal/app/process ./internal/adapter/process
```

### Step 4.6 — Commit

- [x] Step 4.6: Commit

Run structural/staged gates, then:

```bash
git commit -m "feat: inspect bounded host process trees"
```

---

## Task 5 — Optional best-effort listening-port observation

**Files:**
- Create: `internal/adapter/process/ports.go`
- Create: `internal/adapter/process/ports_test.go`
- Modify: `internal/app/process/service.go`
- Modify: `internal/app/process/service_test.go`
- Modify: `internal/core/capability/catalog.go` only if runtime support flag needs final platform wiring

### Step 5.1 — RED port-isolation tests

- [x] Step 5.1: RED port-isolation tests

Cover:

- ports are never inspected unless `include_ports=true`;
- adapter unavailable -> base process observation succeeds partial with `port_observation_unavailable`;
- timeout -> base process observation succeeds;
- results bounded to `MaxPortRecords`;
- only local listening endpoint facts are emitted (`protocol`, endpoint class, port, pid);
- no remote scan/socket payload;
- duplicate records normalize deterministically.

### Step 5.2 — Implement fixed local adapter

- [x] Step 5.2: Implement fixed local adapter

Use a fixed local mechanism only:

- Darwin: bounded fixed `lsof` argv when available;
- Linux: same fixed local `lsof` path if available, otherwise unsupported/unavailable.

Never invoke a shell fragment. Never treat ports as execution/evidence authority.

### Step 5.3 — GREEN/race

- [x] Step 5.3: GREEN/race

```bash
go test ./internal/app/process ./internal/adapter/process
go test -race ./internal/app/process ./internal/adapter/process
```

### Step 5.4 — Commit

- [x] Step 5.4: Commit

```bash
git commit -m "feat: observe process listening ports best effort"
```

---

## Task 6 — Freeze optional A2.5 bindings for new A2.4 evidence

**Files:**
- Modify: `internal/core/operation/persistence.go`
- Modify: relevant operation persistence/validation/schema tests under `internal/core/operation`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/project_command.go`
- Modify: `internal/app/daemon/bindings.go`
- Modify: `internal/app/daemon/*_test.go`
- Modify: `internal/core/evidence/types.go`
- Modify: `internal/core/evidence/validation.go`
- Modify: `internal/core/evidence/types_test.go`
- Modify: `internal/app/evidence/service.go`
- Modify: `internal/app/evidence/service_test.go`
- Modify: operation/evidence JSON schemas that persist these records, discovered by `rg` before editing

### Step 6.1 — Define the authority rule in tests first

- [ ] Step 6.1: Define the authority rule in tests first

The only evidence-eligible A2.5 binding is one already present in the compatible environment cache at operation admission.

Add an optional `environment.Binding` to the persisted operation reservation. Do **not** put it in the user request or recompute it on replay.

RED tests:

1. admission with no compatible cached binding does not capture/probe and stores no binding;
2. admission with compatible cached binding freezes snapshot/fingerprint versions and values;
3. replay retains original stored binding even if current cache changes;
4. typed project command admission obeys the same freeze rule;
5. binding lookup is non-probing (`CachedBinding` only);
6. reservation JSON round-trip preserves optional binding;
7. old reservations without binding remain valid.

Use a daemon option port such as:

```go
type EnvironmentBindingProvider interface {
    CachedBinding(context.Context, environment.BindingRequest) (environment.Binding, bool)
}
```

The provider must not expose a method capable of refresh through this admission path.

### Step 6.2 — Implement admission freeze minimally

- [ ] Step 6.2: Implement admission freeze minimally

Call only the non-probing cached lookup after execution spec binding and before reservation persistence. Matching key includes workspace selection + execution mode/identity + fingerprint versions.

Do not change request/execution/observation-binding fingerprint replay semantics.

### Step 6.3 — Evidence RED tests

- [ ] Step 6.3: Evidence RED tests

Extend new evidence records with optional:

- `environment_fingerprint`
- `environment_fingerprint_version`
- `toolchain_fingerprint`
- `toolchain_fingerprint_version`

Tests:

- frozen compatible reservation binding copied into new derived evidence;
- absent binding stays absent;
- incompatible/malformed versioned binding rejected rather than overclaimed;
- retry/re-derivation cannot reread current environment to alter the evidence;
- old persisted evidence remains unchanged/valid;
- environment/toolchain fields do not upgrade source/artifact/policy validity dimensions;
- evidence ID remains derived from existing receipt+contract authority and the frozen reservation makes derivation deterministic.

### Step 6.4 — Implement evidence copy/validation

- [ ] Step 6.4: Implement evidence copy/validation

No current environment read from `evidence.Service`.

Run:

```bash
go test ./internal/core/operation ./internal/app/daemon ./internal/core/evidence ./internal/app/evidence
go test -race ./internal/app/daemon ./internal/app/evidence
```

### Step 6.5 — Commit

- [ ] Step 6.5: Commit

```bash
git commit -m "feat: bind frozen environment facts to evidence"
```

---

## Task 7 — IPC v2, MCP v2, schemas, daemon wiring, and one-tool capability discovery

**Files:**
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Create: `internal/adapter/ipc/environment_process_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler_test.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/environment_process_test.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Create: `cmd/shellbeam/environment_process_inspection_test.go`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/environment_process_test.go`
- Modify schema catalog fixtures/tests that encode `capability.Catalog`

### Step 7.1 — Closed-schema RED tests

- [ ] Step 7.1: Closed-schema RED tests

Before production transport edits, add schema tests proving accepted and rejected shapes for both new actions.

Reject:
- unknown fields;
- invalid freshness;
- PID <= 0;
- target with both session and pid;
- name-based target;
- argv execution without identity;
- raw env/path/value fields;
- response with more descendants/ports than advertised limits.

Run RED:

```bash
go test ./api/schema
```

### Step 7.2 — IPC RED tests

- [ ] Step 7.2: IPC RED tests

Add narrow optional interfaces, preserving base `ipc.Actions`:

```go
type EnvironmentActions interface { InspectEnvironment(...) (...) }
type ProcessInspectionActions interface { InspectProcess(...) (...) }
```

Prove:
- missing optional interface => `feature_unavailable`;
- typed requests/results survive IPC round trip;
- existing action fake types need no new mandatory methods;
- response payload clearing clears new fields on error.

Run RED:

```bash
go test ./internal/adapter/ipc
```

### Step 7.3 — Bridge/MCP RED tests

- [ ] Step 7.3: Bridge/MCP RED tests

Prove:
- bridge maps both requests without lossy fields;
- MCP accepts both through `local_shell`;
- output summaries do not leak raw path/env values;
- `tools/list` still exposes exactly one tool named `local_shell`.

Run RED:

```bash
go test ./internal/app/bridge ./internal/adapter/mcp
```

### Step 7.4 — Implement schemas + transport

- [ ] Step 7.4: Implement schemas + transport

Add distinct process target object; do not reuse event `target`.

Add response payloads:
- `environment`
- `process`

Keep all JSON schemas `additionalProperties:false`.

### Step 7.5 — Wire daemon services/capabilities

- [ ] Step 7.5: Wire daemon services/capabilities

`cmd/shellbeam/command_daemon.go`:

- instantiate environment host/prober/service;
- provide project manifest lookup via existing validated project service/repository path;
- instantiate process host/service using daemon session resolver;
- bind optional environment cache provider to admission;
- advertise exact versions/limits/probe IDs and current platform port support;
- add `daemonActions.InspectEnvironment`;
- add `daemonActions.InspectProcess`.

No watcher/background loop.

### Step 7.6 — No-tax unit proof

- [ ] Step 7.6: No-tax unit proof

Add spies/counters around environment probe and process observer wiring and prove a normal `start` + `poll` does not call them.

This test must fail if future code accidentally adds A2.5 work to the hot path beyond the explicit non-probing cached binding lookup.

Run:

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/app/bridge ./internal/adapter/mcp ./cmd/shellbeam
go test -race ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam
```

### Step 7.7 — Capability/one-tool checks and commit

- [ ] Step 7.7: Capability/one-tool checks and commit

```bash
go run ./tools/devctl check
git diff --check
git add -- api/schema internal/adapter/ipc internal/app/bridge internal/adapter/mcp cmd/shellbeam internal/core/capability
git diff --cached --check
go run ./tools/devctl commit-gate --json
git commit -m "feat: expose environment and process inspection"
```

---

## Task 8 — Real-daemon acceptance, no-tax proof, privacy-negative tests

**Files:**
- Create: `cmd/shellbeam/environment_process_acceptance_test.go`
- Create or extend: `tests/integration/environment_process_test.go`
- Add bounded acceptance helper scripts under `.build/` only if the existing repository pattern requires them; do not commit transient receipts.

### Step 8.1 — Real daemon environment acceptance

- [ ] Step 8.1: Real daemon environment acceptance

Start a real isolated daemon/runtime and use IPC/MCP v2.

Prove:

1. `inspect.server` advertises both features, schema/fingerprint versions, five probe IDs, exact limits, and port support;
2. `inspect.environment` returns OS/arch, PATH digest/count, presence-only variables, environment + toolchain fingerprints;
3. repeat cached request reuses snapshot;
4. refresh creates a new capture path while equivalent normalized facts preserve same fingerprint;
5. raw PATH string does not appear in JSON;
6. sentinel env values do not appear;
7. SHA-256 of low-entropy sentinel values does not appear.

### Step 8.2 — Real daemon process acceptance

- [ ] Step 8.2: Real daemon process acceptance

Start a ShellBeam operation that remains running long enough to inspect.

Prove:

- inspect by ShellBeam session returns current root relation;
- inspect explicit current PID works;
- descendant traversal is bounded;
- after process terminal, session inspection does not follow a reused/arbitrary PID;
- after daemon restart, old session liveness is not replayed as current;
- `include_ports=false` causes no port adapter invocation;
- forced port adapter failure with `include_ports=true` leaves base process result usable.

### Step 8.3 — Real no-tax acceptance

- [ ] Step 8.3: Real no-tax acceptance

Instrument/fake fixed probe binaries where the repository acceptance harness permits, or use explicit counters injected into a real daemon test.

Run ordinary `start`/`poll` without any A2.5 inspect action and prove:

- zero toolchain probes;
- zero process-table enumeration;
- zero port inspection;
- zero cache refresh;
- no A2.5 snapshot persisted merely because command ran.

### Step 8.4 — Anti-goal/privacy scan

- [ ] Step 8.4: Anti-goal/privacy scan

Freshly scan source and schemas for forbidden patterns; review every hit rather than relying only on grep count.

At minimum inspect for:

```bash
rg -n 'os\.Environ|/environ|printenv|env\s*$|TOKEN=|PASSWORD=|SECRET=|sha256.*env|exec\.Command.*sh.*-c|inspect\.environment|inspect\.process' internal api cmd tests
```

Expected:
- no child env dump;
- no arbitrary secret hashing;
- no arbitrary shell toolchain probe;
- no second MCP tool;
- no watcher;
- no auto-install/update;
- no auto-rerun.

### Step 8.5 — Full affected verification

- [ ] Step 8.5: Full affected verification

```bash
go test ./internal/core/environment ./internal/app/environment ./internal/adapter/environment
go test ./internal/core/process ./internal/app/process ./internal/adapter/process
go test ./internal/app/daemon ./internal/core/evidence ./internal/app/evidence
go test ./internal/adapter/ipc ./internal/app/bridge ./internal/adapter/mcp ./api/schema ./cmd/shellbeam ./tests/integration
go test -race ./internal/app/environment ./internal/adapter/environment ./internal/app/process ./internal/adapter/process ./internal/app/daemon ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam ./tests/integration
```

### Step 8.6 — Commit acceptance slice

- [ ] Step 8.6: Commit acceptance slice

```bash
git diff --check
go run ./tools/devctl check
git add -- <only Task 8 source/test files>
git diff --cached --check
go run ./tools/devctl commit-gate --json
git commit -m "test: verify environment and process observation"
```

---

## Task 9 — Final checkpoint verification and exact fingerprint proof

No production edits should be planned in this task. If a gate fails, use systematic debugging, return to the owning task, make the smallest correct TDD fix, commit it, then restart this checkpoint on fresh source bytes.

### Step 9.1 — Confirm plan completion and clean intended tree

- [ ] Step 9.1: Confirm plan completion and clean intended tree

```bash
git status --short
git diff --check
rg -n '^- \[ \]' docs/superpowers/plans/2026-08-15-environment-toolchain-process-inspection.md
```

All implementation task checkboxes must be complete before final claim.

### Step 9.2 — Fresh exact gates

- [ ] Step 9.2: Fresh exact gates

Run in this order:

```bash
go mod verify
go test ./...
go test -race ./internal/core/environment ./internal/app/environment ./internal/adapter/environment
go test -race ./internal/core/process ./internal/app/process ./internal/adapter/process
go test -race ./internal/app/daemon ./internal/app/evidence ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam ./tests/integration
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base origin/main --json
```

If the repository's current `devctl` exact-fingerprint/checkpoint command differs, inspect `go run ./tools/devctl -h` and use the current supported equivalent. Do not guess or bypass it.

### Step 9.3 — Fresh capability/one-tool/privacy acceptance

- [ ] Step 9.3: Fresh capability/one-tool/privacy acceptance

Re-run:
- one-tool MCP `tools/list`;
- real-daemon `inspect.server`;
- env cached/refresh;
- process session/PID/restart;
- no-tax test;
- secret/PATH negative tests;
- port-failure isolation.

Do not reuse pre-change receipts.

### Step 9.4 — Stage only final checkpoint changes

- [ ] Step 9.4: Stage only final checkpoint changes

If the plan file is being updated with checked boxes as implementation proceeds, include only that plan/checkpoint metadata plus any explicitly intended final test metadata.

```bash
git diff --cached --check
go run ./tools/devctl commit-gate --json
```

### Step 9.5 — Record exact source fingerprint and commit

- [ ] Step 9.5: Record exact source fingerprint and commit

Use the repository's exact current source-fingerprint command. Record it in the plan/checkpoint section before the final commit.

Suggested final commit:

```text
test: checkpoint environment and process observation
```

### Step 9.6 — Prove identical post-commit source fingerprint

- [ ] Step 9.6: Prove identical post-commit source fingerprint

Immediately after the commit:

```bash
git status --porcelain=v1 --untracked-files=all
git rev-parse HEAD
```

Re-run the exact source fingerprint command on post-commit bytes and prove it is identical to the recorded pre-commit fingerprint.

Final report must include:

- final HEAD;
- commit chain for A2.5;
- clean/dirty state;
- exact source fingerprint;
- exact gate outcomes;
- real-daemon acceptance outcomes;
- confirmation `push=NO`, `PR=NO`, `merge=NO`.

---

## Stop conditions

Continue through all tasks unless a real blocker is demonstrated with fresh evidence:

- connector unavailable;
- approved design contradicts a live non-negotiable repository contract;
- required platform primitive cannot be safely isolated;
- repeated verification failure after systematic debugging.

Do not manufacture success. Do not weaken tests or gates. Do not reset/delete/rebase the worktree. Do not push, create a PR, or merge.
