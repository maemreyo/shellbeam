# ShellBeam Execution Observation and Structured Results Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement A2.2/E21 Event Journal and E22 Structured Execution Results so agents can consume bounded durable deltas and deterministic machine-readable execution facts without weakening raw output, receipt, retry, or privacy authority.

**Architecture:** Extend the file-backed store with a durable per-transition observation obligation sequence. Canonical mutations prepare a small sequence obligation before becoming visible; event materialization is asynchronous/idempotent and may lag without ever claiming false continuity. Structured results are deterministic derived records keyed by immutable terminal-output provenance and exact adapter/schema/config identity; the first mechanical adapters are `go-test-json` for test-case/suite records and `go-vet-json` for native JSON diagnostics. Public access stays inside the single `local_shell` tool through `inspect.events` and `inspect.structured` branches with opaque signed continuation handles.

**Tech Stack:** Go 1.26.5; stdlib `crypto/hmac`, `crypto/rand`, `crypto/sha256`, `encoding/base64`, `encoding/json`; existing file-backed atomic store; existing IPC/MCP v2 closed schemas; Go 1.26 `go test -json` and `go vet -json`; no new runtime dependency.

## Global Constraints

- Approved design authority: `docs/superpowers/specs/2026-08-14-observation-structured-results-design.md` plus `docs/superpowers/specs/2026-08-14-agent-execution-observation-roadmap-design.md`.
- Prerequisite A2.1/Lazy Workspace Freshness is complete at `6519de755836705804c553884c968d440275024f` or a descendant with the same contracts.
- Work on `ai/execution-observation`; keep `ai/lazy-workspace-freshness` fixed at its A2.1 checkpoint. Do not rewrite or force-move either branch.
- Use one primary agent unless the user explicitly authorizes subagents.
- TDD is mandatory: focused RED -> minimal GREEN -> focused regression -> relevant dirty/check gate -> commit.
- Keep one MCP tool, `local_shell`. Add observation actions to the existing closed v2 tool schema; do not create a second MCP tool or hidden transport session.
- Raw output and immutable terminal receipts remain authoritative. Journal entries and structured results are derived observation surfaces and never rewrite child outcome, exit evidence, or exactly-once request identity.
- Ordinary start with no explicit structured adapter request must perform no journal scan and no parser/provider subprocess before spawn. Event-obligation persistence is bounded local durability work; event materialization and optional parsing may lag terminal publication.
- A canonical E21-covered mutation must never become visible without a durable observation obligation that names its sequence and subject. A crash may leave a prepared obligation without a canonical subject; recovery marks it aborted rather than inventing an event.
- Public `event_cursor` and structured-result continuation tokens are opaque, typed, target-bound, schema-bound, integrity-protected handles. Clients never decode or perform arithmetic on them.
- Event filtering preserves the underlying state-root sequence position; filtered views never create a second numbering domain.
- Cursor expiry/restart recovery is server-driven. If complete delta continuity cannot be proved, return `snapshot_required` plus a bounded snapshot and resume cursor from one consistency cut, or an explicit unavailable status.
- Structured derivation identity includes immutable source authority refs + producer ID/version + derivation schema version + config digest. Recovery always addresses the same logical derivation key.
- Mechanical authority is allowed only for native producer fields or deterministic normalization. Parsing semantically meaningful fields from prose downgrades the whole record to advisory; core MVP does not use heuristic parsing for verification truth.
- `go-test-json` maps native JSON test/package actions to test-case/test-suite records. It does not parse compiler text in the `Output` field into mechanical diagnostics.
- `go-vet-json` maps native JSON `posn`, `end`, analyzer key, and `message` fields into mechanical diagnostic records. Producer-reported file positions remain `ProviderReportedLocation` unless exact retained source bytes are already bound; E22 does not read source merely to upgrade a location.
- Current project-manifest v1 is not silently widened solely for E22. Adapter-selection source (1), project-command declaration, remains an integration seam for A2.5 typed project commands; A2.2 ships caller-explicit selection and exact direct-argv safe rules now.
- Parser input from raw output must be immutable by terminal contract: terminal receipt identity + session/output reference + exact `[start,end)` range + SHA-256 digest. Mutable artifact pathnames are not sufficient provenance.
- Store/journal/parser files are bounded. No unbounded output copies, source contents, stdin, env values, tokens, private keys, or arbitrary producer payloads enter event/structured records.
- Production files target 150–300 lines, require review above 350, hard cap 500. Test files require review above 600 and hard cap 800. Functions require review above 60 and hard cap 80. Interfaces normally contain 1–5 methods and hard cap 8.
- Keep `cmd/shellbeam` as the composition root; core owns pure contracts, app owns consumer ports/use cases, adapters own filesystem/process/format effects. Do not create generic `utils`, `helpers`, `common`, `shared`, `base`, `misc`, or plugin-bus packages.
- Run focused tests first. Broad gates are `go run ./tools/devctl test --dirty --base origin/main`, `go run ./tools/devctl check`, targeted `go test -race`, and one deliberate `go test ./... -count=1` at the final checkpoint.
- Commit only current-task scope; inspect staged names/stat/check; never use `--no-verify`.

---

## File Structure

### Shared/core contracts

- Create `internal/core/source/location.go` and tests — provider-neutral closed `SourceLocation` union needed by E22 now and E29 later.
- Create `internal/core/observation/event.go`, `target.go`, `cursor.go`, `obligation.go` and tests — event kinds, correlation/target identity, sequence obligation state, continuity/snapshot vocabulary.
- Create `internal/core/structuredresult/derivation.go`, `record.go`, `summary.go`, `cursor.go` and tests — deterministic derivation lifecycle, parse outcomes, authority, producer metadata, raw-output source ref, record kinds and bounded summaries.
- Modify `internal/core/failure/failure.go` and tests — stable observation/structured-result status codes.

### Durable store

- Create `internal/adapter/store/observation.go`, `observation_test.go`, `observation_fault_test.go` — monotonic durable prepared obligations, abort/reconcile, event projection storage, compaction checkpoint metadata.
- Create `internal/adapter/store/structured_results.go`, `structured_results_test.go` — deterministic derivation/result/tombstone persistence and idempotent replay.
- Modify `internal/adapter/store/repository.go`, `reservation.go`, `terminal.go`, `activities.go` and focused tests — prepare E21 obligations at canonical visibility boundaries.
- Extend store root creation with `observations/obligations`, `observations/events`, `structured-results`, and protected cursor-key storage.

### Observation application layer

- Create `internal/app/observation/ports.go`, `service.go`, `materializer.go`, `cursor.go`, `snapshot.go` and tests — bounded materialization, filtering, continuity proof, signed cursors, snapshot/resume recovery.
- Extend daemon/store ports only with small consumer-owned interfaces required to signal new committed observation sequences.

### Structured-result application/adapters

- Create `internal/app/structuredresult/ports.go`, `service.go`, `worker.go`, `selection.go` and tests — adapter selection, immutable input capture, lifecycle and terminal derivation orchestration.
- Create `internal/adapter/structured/gojson/test.go`, `test_test.go`, `vet.go`, `vet_test.go` — bounded native Go JSON adapters.
- Keep project-command declaration as a future producer of `DeclaredAdapter`; do not mutate manifest v1 in this plan.

### Public transport/composition

- Modify `internal/core/capability/catalog.go` and tests — advertise event journal/structured-results availability and limits.
- Modify `internal/adapter/ipc/protocol_v2.go`, client/server, tests — add `inspect.events`, `inspect.structured`, and optional `structured_adapter` start metadata.
- Modify `internal/adapter/mcp/input.go`, `call.go`, discovery/tests and `api/schema/ipc-v2.json`, `mcp-input-v2.json`, `mcp-output-v2.json` — same closed one-tool branches.
- Modify `internal/core/operation/intent.go` / observation binding tests — bind explicit structured-adapter selection as observation metadata, not child execution fingerprint.
- Modify `cmd/shellbeam/command_daemon.go`, runtime tests — compose observation materializer + structured worker without adding ordinary spawn provider tax.

---

### Task 1: Freeze E21/E22 core contracts

**Files:**
- Create: `internal/core/source/location.go`
- Create: `internal/core/source/location_test.go`
- Create: `internal/core/observation/event.go`
- Create: `internal/core/observation/event_test.go`
- Create: `internal/core/observation/target.go`
- Create: `internal/core/observation/target_test.go`
- Create: `internal/core/observation/obligation.go`
- Create: `internal/core/observation/obligation_test.go`
- Create: `internal/core/structuredresult/derivation.go`
- Create: `internal/core/structuredresult/derivation_test.go`
- Create: `internal/core/structuredresult/record.go`
- Create: `internal/core/structuredresult/record_test.go`
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`

**Interfaces:**
- Produces `observation.Event`, `ObservationObligation`, `Target`, `Continuity`, `source.SourceLocation`, `structuredresult.Derivation`, `RawOutputRef`, `Record`, and stable status codes consumed by Tasks 2–10.

- [ ] **Step 1: Write RED tests for the event/obligation closed vocabulary**

Use these public shapes:

```go
type ChangeSeq uint64

type EventKind string
const (
    EventOperationAdmitted EventKind = "operation_admitted"
    EventProcessStarted    EventKind = "process_started"
    EventOutputAvailable   EventKind = "output_available"
    EventProcessTerminal   EventKind = "process_terminal"
    EventStructuredChanged EventKind = "structured_results_changed"
)

type ObligationState string
const (
    ObligationPrepared  ObligationState = "prepared"
    ObligationCommitted ObligationState = "committed"
    ObligationAborted   ObligationState = "aborted"
)
```

`ObservationObligation.Validate()` rejects sequence zero, unknown kind/state, empty bounded subject refs, contradictory correlation identities, and unbounded summaries.

Run:

```bash
go test ./internal/core/observation -run 'Event|Obligation|Target' -count=1
```

Expected: FAIL because the package/types do not exist.

- [ ] **Step 2: Implement event, target, continuity and obligation validators**

Targets are a closed union with exactly one selector:

```go
type Target struct {
    Kind         TargetKind `json:"kind"`
    OperationID  string     `json:"operation_id,omitempty"`
    SessionID    string     `json:"session_id,omitempty"`
    ActivityID   string     `json:"activity_id,omitempty"`
    WorkspaceID  string     `json:"workspace_id,omitempty"`
    RepositoryID string     `json:"repository_id,omitempty"`
}
```

Absolute-cwd operation/session targets remain valid with no repository/workspace/activity ID.

- [ ] **Step 3: Write RED tests for shared source-location truth**

Define a closed union with only:

```go
type ProviderReportedLocation struct {
    Origin               Origin `json:"origin"`
    SanitizedLogicalPath string `json:"sanitized_logical_path,omitempty"`
    Line                 int    `json:"line,omitempty"`
    Column               int    `json:"column,omitempty"`
    EndLine              int    `json:"end_line,omitempty"`
    EndColumn            int    `json:"end_column,omitempty"`
    NormalizationQuality string `json:"normalization_quality"`
}

type ResolvedSourceLocation struct {
    SourceRefID string `json:"source_ref_id"`
    StartByte   int64  `json:"start_byte"`
    EndByte     int64  `json:"end_byte"`
}
```

E22 uses provider-reported locations unless an exact source ref already exists. Reject absolute/raw escaping paths as repository logical paths; allow explicit external/toolchain origin with redacted/basename-only presentation.

- [ ] **Step 4: Write RED tests for deterministic structured derivation/records**

Use closed enums for lifecycle `pending|processing|terminal`, terminal parse outcome `complete|partial|malformed|unavailable|budget_exceeded`, authority `mechanical|advisory`, derivation method `native_field_mapping|deterministic_normalization|heuristic_extraction`, and record kinds `diagnostic|test_case|test_suite|artifact_result`.

`RawOutputRef` binds:

```go
type RawOutputRef struct {
    SessionID string `json:"session_id"`
    StartByte int64  `json:"start_byte"`
    EndByte   int64  `json:"end_byte"`
    SHA256    string `json:"sha256"`
}
```

`DerivationKey()` hashes canonical producer/schema/config/source-ref identity and excludes lifecycle/presentation/pagination state.

- [ ] **Step 5: Implement minimal core contracts and failure/status additions**

Reserve/add:

```text
event_cursor_invalid
event_cursor_expired
event_continuity_unavailable
structured_adapter_unavailable
structured_adapter_unsupported
structured_result_malformed
structured_result_partial
structured_result_budget_exceeded
structured_result_not_found
```

Observation codes remain tool-observation failures/statuses and never child outcomes.

- [ ] **Step 6: Run focused tests and commit**

```bash
go test ./internal/core/source ./internal/core/observation ./internal/core/structuredresult ./internal/core/failure -count=1
git add internal/core/source internal/core/observation internal/core/structuredresult internal/core/failure
git diff --cached --check
git commit -m "feat: define execution observation contracts"
```

---

### Task 2: Add the durable state-root observation obligation sequence

**Files:**
- Create: `internal/adapter/store/observation.go`
- Create: `internal/adapter/store/observation_test.go`
- Create: `internal/adapter/store/observation_fault_test.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/root_test.go`
- Modify: `internal/adapter/store/fault_test.go`

**Interfaces:**
- Produces store-local APIs:

```go
type PreparedObservation struct { Obligation observation.ObservationObligation }
func (r *Repository) PrepareObservation(context.Context, observation.PrepareRequest) (PreparedObservation, app.StoreResult)
func (r *Repository) CommitObservation(context.Context, observation.ChangeSeq) app.StoreResult
func (r *Repository) AbortObservation(context.Context, observation.ChangeSeq, string) app.StoreResult
func (r *Repository) ObservationHighWatermark(context.Context) (observation.ChangeSeq, error)
func (r *Repository) ListObservationObligations(context.Context, observation.ChangeSeq, int) ([]observation.ObservationObligation, error)
```

- [ ] **Step 1: Write RED persistence tests for monotonic sequence and restart recovery**

Test concurrent prepares, process restart/reopen, prepared/committed/aborted validation, symlink rejection, max record size, and exact sequence order. Persist each obligation as one atomic bounded file under `observations/obligations/<20-digit-seq>.json`; the highest valid filename is the durable high watermark.

- [ ] **Step 2: Implement atomic prepared-obligation creation**

Under a dedicated observation mutex, derive `next=max(in-memory high watermark)+1`, write with create-only atomic publication and parent sync, then update in-memory high watermark. Do not write a second high-watermark file on the hot path. Reopen scans bounded filenames once to recover the maximum sequence.

- [ ] **Step 3: Implement committed/aborted state transitions idempotently**

`CommitObservation(seq)` and `AbortObservation(seq, reasonCode)` replace the same bounded obligation record. Identical replay is a no-op; contradictory terminal state is a conflict. Abort reason is a stable bounded code, not raw error text.

- [ ] **Step 4: Add crash/fault injection**

Prove:

```text
prepared obligation durable + canonical mutation absent -> restart can see prepared
committed obligation replay -> one logical sequence
partial/temp observation file -> never promoted as valid obligation
```

Do not emit an event in this task.

- [ ] **Step 5: Run focused store/race tests and commit**

```bash
go test -race ./internal/adapter/store -run 'Observation|Fault|Root' -count=1
git add internal/adapter/store/observation.go internal/adapter/store/observation_test.go \
  internal/adapter/store/observation_fault_test.go internal/adapter/store/repository.go \
  internal/adapter/store/root_test.go internal/adapter/store/fault_test.go
git diff --cached --check
git commit -m "feat: persist observation obligations"
```

---

### Task 3: Bind E21 obligations to canonical operation/session/output/terminal visibility

**Files:**
- Modify: `internal/adapter/store/reservation.go`
- Modify: `internal/adapter/store/terminal.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/reservation_recovery_test.go`
- Modify: `internal/adapter/store/terminal_test.go`
- Modify: `internal/adapter/store/terminal_immutability_test.go`
- Modify: `internal/adapter/store/repository_test.go`
- Modify: `internal/app/daemon/store_port.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/service_test.go`

**Interfaces:**
- `app.StoreResult` gains optional `ObservationSeq uint64` for post-commit materializer wake-up only; it does not affect durability truth.

- [ ] **Step 1: Write RED crash-boundary tests for operation admission**

For a newly created operation, require a prepared `operation_admitted` obligation to exist before the authoritative operation file becomes visible. If canonical publication fails, abort the prepared obligation. Retry of the same operation must not allocate a second committed logical event.

- [ ] **Step 2: Integrate process-start obligation before spawn with post-spawn resolution**

Prepare the `process_started` obligation immediately before `Owner.Start()` can create the child, then commit it only after `Owner.Start()` returns authoritative spawn success; abort it on spawn failure. This preserves sequence order even when the process adapter's capture goroutine publishes output before `Owner.Start()` returns. The subsequent `AdvanceSession(Running)` remains canonical session-state publication and must not allocate a second process-start obligation.

- [ ] **Step 3: Integrate output-range obligations**

Before a durable output append, prepare `output_available` with subject ref `output:<session>:<start>:<end>`. On successful append commit the obligation; on failure/ambiguous append leave/abort according to what file-size reconciliation can prove. Event materialization may later coalesce adjacent committed ranges.

- [ ] **Step 4: Integrate terminal obligation with immutable receipt publication**

Prepare `process_terminal` before first immutable receipt publication. Identical receipt replay reuses the existing terminal observation sequence and never creates a second terminal event.

- [ ] **Step 5: Add startup reconciliation for prepared obligations**

At store/daemon startup, reconcile prepared old-incarnation obligations against authoritative subjects. If the exact operation/session/output-range/receipt exists, mark committed; if it provably does not and no live writer can still publish it, mark aborted. Preserve order and never invent an event for an aborted sequence.

- [ ] **Step 6: Run crash/race tests and commit**

```bash
go test -race ./internal/adapter/store ./internal/app/daemon -run 'Observation|Admission|Terminal|Output|Retry|Reconcile' -count=1
git add internal/adapter/store/reservation.go internal/adapter/store/terminal.go internal/adapter/store/repository.go \
  internal/adapter/store/*recovery_test.go internal/adapter/store/terminal*_test.go internal/adapter/store/repository_test.go \
  internal/app/daemon/store_port.go internal/app/daemon/service.go internal/app/daemon/service_test.go
git diff --cached --check
git commit -m "feat: bind observation sequence to execution state"
```

---

### Task 4: Materialize the bounded Event Journal with honest continuity

**Files:**
- Create: `internal/app/observation/ports.go`
- Create: `internal/app/observation/service.go`
- Create: `internal/app/observation/service_test.go`
- Create: `internal/app/observation/materializer.go`
- Create: `internal/app/observation/materializer_test.go`
- Create: `internal/app/observation/cursor.go`
- Create: `internal/app/observation/cursor_test.go`
- Create: `internal/app/observation/snapshot.go`
- Create: `internal/adapter/store/events.go`
- Create: `internal/adapter/store/events_test.go`

**Interfaces:**
- Produces:

```go
type InspectRequest struct {
    Target           observation.Target
    AfterEventCursor string
    MaxEvents        int
}
type InspectResult struct {
    Events          []observation.Event
    NextEventCursor string
    Continuity      observation.Continuity
    Snapshot        *observation.Snapshot
    Truncated       bool
    CompactedBefore uint64
}
```

- [ ] **Step 1: Write RED event materialization tests**

Committed obligations materialize in `change_seq` order; aborted obligations advance continuity without an event; a prepared unresolved obligation stops `continuity=complete` beyond that gap. Duplicate materialization writes the same `event_id`/sequence only once.

- [ ] **Step 2: Implement event projection storage and output coalescing**

Persist bounded events under `observations/events/<seq>.json`. `output_available` may coalesce only adjacent ranges for the same session while preserving the latest underlying sequence/cursor progress. `process_terminal` is never hidden by coalescing.

- [ ] **Step 3: Write RED signed-cursor tests**

Persist one random 32-byte HMAC key under the state root with `0600`/no-follow semantics. Encode a bounded cursor payload containing schema, state-root epoch/key generation, underlying sequence, and canonical target fingerprint; prefix `evtcur_v1_`. Reject tamper, target mismatch, schema mismatch, malformed/base64 overflow, and retired-key/epoch mismatch.

- [ ] **Step 4: Implement filtered views preserving sequence progress**

Filter by operation/session/activity/workspace/repository correlation but advance cursor to the underlying inspected cut even when unrelated events are omitted. Never renumber filtered events.

- [ ] **Step 5: Implement server-driven snapshot/resume recovery**

If the cursor is older than retained events or a projection gap prevents complete delta, capture a bounded target snapshot and high-watermark cut under one service critical section, then issue a resume cursor for that exact cut. A later sequence N+1 must be visible after resume. If a snapshot provider cannot produce bounded current facts, return `event_continuity_unavailable` without claiming complete.

- [ ] **Step 6: Implement bounded retention metadata**

Compact materialized event files by count/bytes/age while retaining `compacted_through_seq`, current high watermark, and enough obligation checkpoint metadata to detect gaps. Never delete receipts/output/evidence merely to preserve event retention.

- [ ] **Step 7: Run focused/race tests and commit**

```bash
go test -race ./internal/core/observation ./internal/app/observation ./internal/adapter/store -run 'Event|Cursor|Continuity|Materializ|Snapshot|Retention' -count=1
git add internal/app/observation internal/adapter/store/events.go internal/adapter/store/events_test.go
git diff --cached --check
git commit -m "feat: materialize bounded execution events"
```

---

### Task 5: Expose `inspect.events` through IPC/MCP v2

**Files:**
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Create: `internal/adapter/ipc/event_inspect_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/event_inspect_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`

**Interfaces:**
- Adds one closed action branch:

```json
{
  "action": "inspect.events",
  "target": {"kind": "operation", "operation_id": "..."},
  "after_event_cursor": "evtcur_v1_...",
  "max_events": 64
}
```

- [ ] **Step 1: Write RED closed-schema and protocol tests**

Reject cross-action fields, multiple target IDs, unknown target kinds, output cursor in event cursor field, oversized max events, and malformed cursor. Preserve legacy v1 tool discovery unchanged.

- [ ] **Step 2: Wire one-tool IPC/MCP forwarding**

`inspect.events` never spawns a process. MCP transport success remains separate from observation continuity/status.

- [ ] **Step 3: Add capability discovery**

Advertise event-journal availability, cursor schema version, max events/response bytes, and snapshot-recovery support only when the observation service is composed.

- [ ] **Step 4: Run contract tests and commit**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./internal/core/capability -run 'Event|Cursor|Compatibility|Discovery' -count=1
git add api/schema internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/core/capability
git diff --cached --check
git commit -m "feat: inspect durable execution events"
```

---

### Task 6: Persist deterministic structured-result lifecycle and immutable raw-output inputs

**Files:**
- Create: `internal/adapter/store/structured_results.go`
- Create: `internal/adapter/store/structured_results_test.go`
- Create: `internal/app/structuredresult/ports.go`
- Create: `internal/app/structuredresult/service.go`
- Create: `internal/app/structuredresult/service_test.go`
- Create: `internal/app/structuredresult/input.go`
- Create: `internal/app/structuredresult/input_test.go`
- Modify: `internal/adapter/store/repository.go`

**Interfaces:**
- Produces:

```go
type InputBinder interface {
    BindTerminalOutput(context.Context, receipt.Receipt) (structuredresult.RawOutputRef, error)
    ReadOutputRange(context.Context, structuredresult.RawOutputRef, int64, int) ([]byte, error)
}

type Repository interface {
    PutDerivation(context.Context, structuredresult.Derivation) error
    GetDerivation(context.Context, string) (structuredresult.Derivation, error)
    PutRecords(context.Context, string, []structuredresult.Record) error
    ListRecords(context.Context, string, RecordQuery) ([]structuredresult.Record, error)
}
```

- [ ] **Step 1: Write RED raw-output input binding tests**

After terminal receipt publication, hash exactly `[0, receipt.OutputBytes)`, persist the digest/range/session association, and prove later adapter reads reject range/digest mismatch. Output compaction may make detail unavailable but cannot silently bind different bytes.

- [ ] **Step 2: Write RED deterministic derivation lifecycle tests**

`pending -> processing -> terminal` is monotonic. Terminal parse outcome is required only at terminal. Same derivation key replay is idempotent; same source under a different adapter version/config digest yields a different key by design.

- [ ] **Step 3: Implement bounded result persistence/tombstones**

Store derivation metadata separately from detailed records so record compaction can preserve derivation identity, producer/schema/config, source refs, authority, terminal lifecycle/outcome, summary and compaction state.

- [ ] **Step 4: Integrate `structured_results_changed` observation obligation**

A derived-result publication is not authoritative child state, but its durable derived-store transition gets a deterministic E21 obligation/event keyed by the same derivation identity. Retry cannot double-count the logical result set.

- [ ] **Step 5: Run focused tests and commit**

```bash
go test -race ./internal/core/structuredresult ./internal/app/structuredresult ./internal/adapter/store -run 'Structured|Derivation|OutputRef|Compaction' -count=1
git add internal/adapter/store/structured_results.go internal/adapter/store/structured_results_test.go \
  internal/app/structuredresult internal/adapter/store/repository.go
git diff --cached --check
git commit -m "feat: persist deterministic structured results"
```

---

### Task 7: Implement bounded native Go JSON adapters

**Files:**
- Create: `internal/adapter/structured/gojson/test.go`
- Create: `internal/adapter/structured/gojson/test_test.go`
- Create: `internal/adapter/structured/gojson/vet.go`
- Create: `internal/adapter/structured/gojson/vet_test.go`
- Create: `internal/adapter/structured/gojson/common.go`
- Create: `internal/app/structuredresult/selection.go`
- Create: `internal/app/structuredresult/selection_test.go`

**Interfaces:**
- Adapter contract:

```go
type Adapter interface {
    ID() string
    Version() int
    Parse(context.Context, structuredresult.RawOutputRef, Reader, Limits) (ParseResult, error)
}
```

- [ ] **Step 1: Write RED fixtures for `go-test-json`**

Cover pass/fail/skip package and named tests, package-level failure, malformed JSON, truncated last object, oversized Output string, excessive record count, and interleaved packages. Map only native fields `Action`, `Package`, `Test`, `Elapsed`, `Time`; ignore prose `Output` for mechanical diagnostic semantics.

- [ ] **Step 2: Implement streaming bounded `go-test-json` parser**

Use `json.Decoder` over the immutable output range with byte/depth/string/record/time budgets. Emit test-case/test-suite mechanical records and deterministic summary. Malformed/truncated input returns terminal parse outcome `partial|malformed|budget_exceeded` while preserving already valid bounded records according to completeness.

- [ ] **Step 3: Write RED fixtures for `go-vet-json`**

Use Go 1.26 native shape:

```json
{
  "example/pkg": {
    "printf": [{
      "posn": "/repo/main.go:5:27",
      "end": "/repo/main.go:5:29",
      "message": "fmt.Printf ..."
    }]
  }
}
```

Assert analyzer key becomes stable diagnostic code, message is bounded, and position/path normalization is deterministic. Repo-contained paths become repository-origin `ProviderReportedLocation`; escaping/dependency/toolchain paths are classified/redacted rather than fabricated as canonical source refs.

- [ ] **Step 4: Implement exact adapter selection**

Supported sources now:

```text
explicit structured_adapter = go-test-json | go-vet-json
or exact direct argv:
  go test -json ...
  go vet -json ...
```

Do not auto-select from arbitrary shell command text or pipelines. Unsupported explicit adapter returns `structured_adapter_unsupported` as observation metadata and does not prevent the child command from running unless future explicit policy requires it.

- [ ] **Step 5: Prove authority downgrade boundary**

Add a test adapter fixture where one semantic field is obtained through heuristic extraction and assert the whole record authority is advisory. Production Go adapters never use that path.

- [ ] **Step 6: Run focused tests and commit**

```bash
go test ./internal/adapter/structured/gojson ./internal/app/structuredresult -run 'Go|Adapter|Authority|Budget' -count=1
git add internal/adapter/structured/gojson internal/app/structuredresult/selection.go internal/app/structuredresult/selection_test.go
git diff --cached --check
git commit -m "feat: parse native Go structured output"
```

---

### Task 8: Bind structured adapter metadata and run terminal derivation asynchronously

**Files:**
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/intent_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/bindings.go`
- Modify: `internal/app/daemon/service.go`
- Create: `internal/app/structuredresult/worker.go`
- Create: `internal/app/structuredresult/worker_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`

**Interfaces:**
- `StartRequest` gains optional `StructuredAdapter string`.
- `operation.ObservationBinding` includes the normalized adapter selection; request/execution fingerprints remain unchanged.

- [ ] **Step 1: Write RED retry-fingerprint tests**

Changing only response controls remains replay-compatible. Changing explicit structured adapter on the same operation ID returns observation-metadata conflict and never respawns the child. Exact direct-argv auto-selection is deterministic from the already bound execution spec.

- [ ] **Step 2: Wire explicit adapter through IPC/MCP start**

Closed enum initially accepts `go-test-json|go-vet-json`. Shell commands may request an adapter explicitly; direct argv may omit it and use the safe built-in rule.

- [ ] **Step 3: Write RED terminal-worker lifecycle tests**

Terminal receipt publication completes before optional parser work. Immediately after child terminal, derivation may be `pending|processing`. Parser failure/crash does not mutate child receipt/outcome. Retry/restart resumes the same derivation key.

- [ ] **Step 4: Implement bounded worker orchestration**

After first terminal receipt publication, bind immutable raw-output ref, upsert pending derivation, schedule bounded worker, transition to processing, parse, atomically publish terminal derivation/records/summary, then signal event materialization. Worker count/queue/bytes/time are bounded; shutdown does not corrupt or duplicate derivations.

- [ ] **Step 5: Prove ordinary start no-provider tax**

Counting tests assert no parser worker, adapter process, journal scan, `go vet`, `go test`, `ssh`, or `gh` subprocess is launched before ordinary child spawn when no structured adapter is selected. Built-in parsing is in-process and begins only after terminal immutable input exists.

- [ ] **Step 6: Run focused/race tests and commit**

```bash
go test -race ./internal/core/operation ./internal/app/daemon ./internal/app/structuredresult ./internal/adapter/ipc ./internal/adapter/mcp -run 'Structured|Retry|Worker|NoTax' -count=1
git add internal/core/operation internal/app/daemon internal/app/structuredresult/worker.go \
  internal/app/structuredresult/worker_test.go internal/adapter/ipc internal/adapter/mcp api/schema
git diff --cached --check
git commit -m "feat: derive structured results after terminal"
```

---

### Task 9: Expose bounded `inspect.structured` summaries and records

**Files:**
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Create: `internal/adapter/ipc/structured_inspect_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/structured_inspect_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create: `internal/app/structuredresult/cursor.go`
- Create: `internal/app/structuredresult/cursor_test.go`

**Interfaces:**
- Adds:

```json
{
  "action": "inspect.structured",
  "operation_id": "...",
  "record_kind": "diagnostic",
  "severity": "error",
  "path": "internal/...",
  "test_status": "fail",
  "continuation": "rescur_v1_...",
  "max_records": 50
}
```

- [ ] **Step 1: Write RED lifecycle/summary inspection tests**

Inspect distinguishes no derivation, pending, processing, terminal complete, terminal partial/malformed/unavailable/budget-exceeded, and compacted details. Summary fields include errors/warnings/files/test counts/returned/total-or-lower-bound/truncated without inventing completeness.

- [ ] **Step 2: Implement bounded filtering/pagination**

Continuation tokens are opaque signed target/filter/schema-bound handles distinct from event/output cursors. Record filters are closed and bounded. Pagination never requires loading the entire result set into model context.

- [ ] **Step 3: Add transport/schema/capability wiring**

`inspect.structured` never spawns. Capability catalog advertises supported adapter IDs, result kinds, max records, and lifecycle support only when composed.

- [ ] **Step 4: Run contract tests and commit**

```bash
go test ./api/schema ./internal/app/structuredresult ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./internal/core/capability -run 'Structured|Cursor|Discovery|Compatibility' -count=1
git add api/schema internal/app/structuredresult/cursor* internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/core/capability
git diff --cached --check
git commit -m "feat: inspect structured execution results"
```

---

### Task 10: Compose A2.2, prove crash/restart continuity, and complete native acceptance

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/daemon_test.go`
- Create: `cmd/shellbeam/execution_observation_test.go`
- Modify: `dev/test-impact.toml` if new packages are not selected by current impact rules.
- Modify: `docs/testing/tunnel-e2e.md` only if manual tunnel acceptance needs new A2.2 steps.

**Interfaces:**
- Produces no new core contract; proves Tasks 1–9 end-to-end.

- [ ] **Step 1: Compose one observation service/materializer and one bounded structured worker**

Use the same state-root store. Start reconciliation/materialization after daemon listener ownership is secured and authoritative store reconciliation completes. Ordinary shell admission never waits for historical journal scan/materialization.

- [ ] **Step 2: Add native Event Journal acceptance**

With a real daemon, prove:

```text
start absolute-cwd operation -> operation_admitted -> process_started -> output_available -> process_terminal
inspect.events(operation target, cursor C) -> bounded ordered delta
filtered session/operation view works without workspace identity
restart -> old valid cursor either continues completely or returns snapshot_required, never silent gap
expired/compacted cursor -> snapshot + resume cursor at one cut
new transition after snapshot cut -> visible after resume
```

- [ ] **Step 3: Add native Go structured-result acceptance**

Run a temp Go module through real `local_shell` direct argv:

```text
go test -json ./...
go vet -json ./...
```

Include one failing test and one native vet diagnostic. Prove test records and vet diagnostic are mechanical, bounded, linked to exact terminal raw-output refs, and raw output/child exit remain independently correct.

- [ ] **Step 4: Add crash/fault isolation checkpoint**

Inject failures at prepared obligation, canonical commit, event materialization, structured derivation publication, event acknowledgement, and parser completion boundaries. Prove authoritative receipt correctness, detectable continuity gap, same derivation key recovery, and no duplicate logical events/results.

- [ ] **Step 5: Run no-tax and security/privacy checks**

Search persisted events/results/receipts for env/stdin/token/private-key fixtures and prove none are copied. Producer diagnostic messages remain bounded and documented as potentially sensitive producer text. Counting tests prove no observation scan/parser subprocess before ordinary spawn.

- [ ] **Step 6: Run exact repository checkpoint**

```bash
go test ./internal/core/observation ./internal/core/source ./internal/core/structuredresult \
  ./internal/adapter/store ./internal/app/observation ./internal/app/structuredresult \
  ./internal/app/daemon ./internal/adapter/structured/gojson ./internal/adapter/ipc \
  ./internal/adapter/mcp ./cmd/shellbeam ./tests/contract -count=1

go test -race ./internal/adapter/store ./internal/app/observation ./internal/app/structuredresult \
  ./internal/app/daemon ./internal/adapter/structured/gojson -count=1

go run ./tools/devctl test --dirty --base origin/main
go run ./tools/devctl check
go test ./... -count=1
go vet ./...
git diff --check origin/main...HEAD
```

Every command must exit 0. `[no tests to run]` is not proof of required behavior.

- [ ] **Step 7: Commit A2.2 acceptance checkpoint**

Stage only A2.2 files, inspect staged names/stat/check, then:

```bash
git commit -m "test: verify execution observation layer"
```

Do not push or open a PR unless the user explicitly requests it.

---

## Completion Gate

A2.2 is complete only when all of the following are proven on one exact source fingerprint:

- every E21-covered canonical mutation visible to clients has a durable observation obligation/sequence;
- prepared-but-uncommitted crash states reconcile without fabricated events or false complete continuity;
- filtered event views preserve underlying sequence position and operation/session targets work without workspace identity;
- cursor tamper/target/epoch/retention mismatch is explicit, and expired continuity recovers server-side with snapshot+resume when bounded recovery is available;
- terminal receipts/raw output stay authoritative and are unchanged by event/materializer/parser failure;
- structured lifecycle separates pending/processing from terminal parse outcome;
- deterministic derivation recovery cannot duplicate one logical result set;
- `go-test-json` yields bounded mechanical test case/suite facts without treating prose Output as mechanical diagnostics;
- `go-vet-json` yields bounded mechanical diagnostics from native JSON fields with honest provider-reported locations;
- heuristic semantic extraction cannot receive mechanical authority;
- optional structured parsing begins only after immutable terminal input binding and never blocks spawn/terminal receipt indefinitely;
- ordinary unstructured execution performs no journal scan/parser/provider subprocess before spawn;
- event/result retention is bounded and does not delete canonical receipts/output/evidence merely to preserve observation convenience;
- focused, race, dirty, architecture, security/privacy, full-suite and native real-daemon acceptance gates all pass.

At handoff report branch/worktree, commits, source fingerprint, event/structured limits, supported adapters, crash/restart evidence, checks executed, residual A2.3/A2.4/B1/B2 risks, and whether anything was pushed or installed.
