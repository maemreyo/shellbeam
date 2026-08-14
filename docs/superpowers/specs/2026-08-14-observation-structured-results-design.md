# ShellBeam Observation and Structured Results Design

## Status

Approved companion design for E21 Event Journal and E22 Structured Execution Results. This spec extends the [Agent Execution Observation Roadmap](./2026-08-14-agent-execution-observation-roadmap-design.md) and inherits all V1/E01-E20 authority, retry, evidence, workspace, output, privacy, and compatibility invariants.

## 1. Decision

ShellBeam adds a durable bounded event journal for efficient delta observation and a structured-results family for deterministic machine-readable execution facts. Both capabilities reduce repeated polling, log scanning, grep/tail loops, and context-window waste without moving reasoning into the daemon.

The event journal is a change feed, not canonical state. Structured results are projections with raw provenance, not a replacement for raw output or terminal receipts.

## 2. Goals

- Let a reconnecting agent ask what changed after a cursor and receive only a bounded delta.
- Make workspace, operation, output, evidence, artifact, manifest, and session-health changes observable without repeated full snapshots.
- Normalize supported machine-readable execution output into compact facts such as diagnostics and test results.
- Preserve exact links from every structured fact back to its producing operation/raw output/result artifact.
- Keep parser failures, journal gaps, retention, and unsupported adapters honest and typed.
- Preserve one MCP tool and the existing explicit-handle architecture.

## 3. Non-goals

S1 does not add:

- a workflow/event automation engine;
- server-side command scheduling;
- daemon-side root-cause analysis or fix recommendations;
- a conversation/session-memory log;
- arbitrary regex interpretation of human logs as truth;
- push subscriptions as a required core transport primitive;
- a second canonical output stream;
- unbounded event or diagnostic retention.

## 4. Event journal architecture

The journal is a bounded materialized view over a durable state-root change sequence. A canonical mutation covered by E21 cannot become externally visible without atomically establishing its observation obligation:

```text
canonical mutation
      ↓
commit visibility boundary
  authoritative state transition
  + state_root_epoch/change_seq
  + durable observation obligation
      ↓
asynchronous/idempotent journal materialization
      ↓
inspect(after_event_cursor=...)
```

`change_seq` is the durable observation-order authority. The journal is not event sourcing and is not needed to reconstruct canonical state. The implementation may use an outbox, WAL, or equivalent local transactional publication mechanism, but a crash between canonical persistence and event materialization must leave enough durable information to prove/materialize the missing sequence.

A journal event is never allowed to impose an additional synchronous fsync barrier merely for model convenience. Materialization is bounded projection work after the authoritative visibility boundary.

## 5. Event record contract

A version-1 event contains only bounded identification, ordering, and summary data. Conceptually:

```text
event_id
state_root_epoch
change_seq
kind
recorded_at
correlation_scope
repository_id?
workspace_id?
activity_id?
operation_id?
session_id?
workspace_generation?
subject_ref
summary
```

The public API may omit internal sequence/epoch representation behind an opaque `event_cursor`; the durable store retains enough information to prove continuity. `repository_id`/`workspace_id` are optional because absolute-cwd operations outside registered repositories are valid execution subjects.

Events reference canonical/derived records instead of copying receipts, output, diagnostics, artifacts, or provider payloads. Event summaries are bounded presentation aids and never stronger authority than the referenced subject.

## 6. Initial event kinds

The initial closed/versioned set is deliberately small:

```text
workspace_generation_changed
operation_admitted
process_started
output_available
process_terminal
evidence_recorded
evidence_validity_changed
artifact_observed
manifest_status_changed
session_health_changed
structured_results_changed
code_diagnostics_changed
```

`code_diagnostics_changed` is reserved for E29 integration and carries only a bounded summary/reference. Unsupported capability event kinds are not emitted. No event contains reasoning such as `agent_should_run_tests`, `probably_broken`, or a proposed fix.

## 7. Event-kind semantics

### 7.1 `workspace_generation_changed`

Emitted after ShellBeam observes a new accepted fast workspace generation. The event states the previous/current generation IDs when available and a bounded mechanical cause classification such as Git/index/worktree facts; it does not claim which actor caused the mutation.

### 7.2 `operation_admitted`

Emitted after exactly-once operation admission is durably established. It references the operation and bound workspace/activity metadata. It is not spawn evidence.

### 7.3 `process_started`

Emitted only after authoritative spawn success is available for the admitted session. It does not imply the command later succeeds.

### 7.4 `output_available`

Emitted when new canonical raw bytes become available. It contains an output reference and available-through cursor/byte position, not the bytes themselves.

Implementations may coalesce adjacent output-available notifications to protect journal budgets. Coalescing may reduce notification granularity but cannot hide terminal output availability from a subsequent snapshot/read.

### 7.5 `process_terminal`

Emitted after durable terminal publication and points to the immutable terminal receipt. Summary may include terminal state/outcome/exit status category but cannot supersede the receipt.

### 7.6 Evidence/artifact/manifest/session events

These record accepted transitions in their respective canonical/derived stores and point to the record or current status. `evidence_validity_changed` includes stable reason dimensions rather than narrative explanation.

## 8. Cursor contract

The public event cursor is opaque, typed, and namespaced, for example `evtcur_v1_...`. It logically binds the state-root epoch, durable observation position, target/filter identity, and cursor schema. Clients pass it back; they do not decode or perform arithmetic on it.

An observation request may conceptually specify:

```text
target = operation | session | activity | workspace | repository
after_event_cursor = evtcur_...
max_events = N
```

A state-root stream exists as an internal ordering authority; ordinary agents do not need to query or understand it. Public targets are filtered views over that sequence. A target filter must never renumber events into a second continuity domain.

Normal response:

```text
events[]
next_event_cursor
continuity: complete
truncated
```

When the supplied cursor cannot support a complete delta, the server performs bounded recovery in the same inspect response where possible:

```text
continuity: snapshot_required
snapshot: {... current bounded facts at cut N ...}
next_event_cursor: <resume cursor at the same cut N>
compacted_before?
```

The snapshot and resume cursor are produced from the same consistency cut. A transition assigned N+1 after that cut is therefore guaranteed to be observable after resume. Output cursors, event cursors, result pagination tokens, and other continuation handles are not interchangeable.

## 9. Ordering and continuity

The state-root `change_seq` provides one durable local observation order. It does not claim a stronger causal order than the authoritative transitions themselves.

An event is never published before the transition it represents is authoritative. Projection lag/failure is legal, but it is mechanically detectable:

```text
last_materialized_seq = 184
canonical_high_watermark = 187
=> continuity cannot be reported complete through 187
```

Recovery may rematerialize missing journal entries from durable observation obligations or return a snapshot/resume cut. It never invents historical ordering or reports a complete delta over an unproven gap.

Filtered activity/workspace/operation views preserve the underlying sequence position even when unrelated events are omitted from the response.

## 10. Retention and cursor expiry

Journal materialization is bounded by configured record count, bytes, and age. Active targets may receive a larger bounded budget, but no target has unbounded history. Retention applies to materialized events, not to the minimum durable high-watermark metadata required to detect projection gaps honestly.

If a client presents a cursor older than retained materialization or from a retired state-root epoch, ShellBeam does not make the agent perform a recovery dance. Where bounded snapshot inspection is supported it returns `snapshot_required` plus the current snapshot and a resume cursor from the same consistency cut. If a snapshot cannot be formed within budget, it returns a typed observation status and no false continuity claim.

Journal compaction never deletes authoritative terminal receipts/evidence merely to preserve a cursor. The durable observation sequence metadata may itself be compacted only when no retained cursor/snapshot contract can depend on the removed range.

## 11. Restart behavior

Daemon restart preserves state-root epoch/high-watermark and pending materialization obligations sufficiently to do one of two things:

- continue from the caller's durable cursor with provably complete filtered deltas; or
- return server-driven snapshot/resume recovery.

It must not accept an old cursor and silently omit a restart gap. A new state-root epoch invalidates incompatible cursors explicitly rather than aliasing them to a different ordering domain.

A stateless MCP bridge carries explicit target/cursor handles only; hidden transport-session state is never required for continuity.

## 12. Structured-results family

The public abstraction is `structured_results`, not only diagnostics. Version 1 supports these record kinds:

- `diagnostic`;
- `test_case`;
- `test_suite`;
- `artifact_result`.

A future schema may add benchmark/performance result kinds, but they are not required for S1.

Raw captured bytes and authoritative child/receipt state remain canonical. Structured results are mechanical/advisory projections with provenance.

## 13. Common structured-result fields

Every structured-result set has a deterministic derivation identity and lifecycle before its kind-specific records:

```text
derivation_key
source_authority_refs[]
producer_id/version
derivation_schema_version
derivation_config_digest
lifecycle: pending | processing | terminal
parse_outcome?  # only when lifecycle=terminal
completeness
```

`derivation_key` is stable for one authoritative source input plus exact producer/schema/config semantics. Recovery upserts this logical result set. It cannot create a second independent result set merely because parsing completed before an index/event acknowledgement was persisted.

A diagnostic record is conceptually:

```json
{
  "record_kind": "diagnostic",
  "authority": "mechanical",
  "derivation_method": "native_field_mapping",
  "producer": {
    "adapter_id": "go-test-json",
    "adapter_version": 1,
    "capability_version": 1
  },
  "operation_id": "op_...",
  "severity": "error",
  "code": "compile",
  "message": "undefined: ServerInfo",
  "location": {
    "path": "internal/adapter/service/service.go",
    "line": 81,
    "column": 12
  },
  "source_ref": {
    "output_ref": "out_...",
    "byte_range": [1821, 1930]
  }
}
```

Concrete schemas use bounded strings, closed enums where stable, normalized paths when possible, and explicit unavailable states. Records never contain proposed changes, root-cause narratives, fix confidence, or remediation commands.

## 14. Result kinds

### 14.1 Diagnostic

Represents a compiler/linter/test-framework/static-analysis finding with severity, stable code/rule when available, message, primary location, bounded related locations, and source provenance.

### 14.2 Test case

Represents a mechanically identified test-case outcome such as pass/fail/skip/error plus bounded duration and suite/package association when the producer supplies them.

### 14.3 Test suite

Represents a suite/package/file-level test aggregation from the structured producer. It does not infer coverage or completeness beyond what the producer declares.

### 14.4 Artifact result

Represents a machine-readable producer result tied to an expected artifact, for example generation metadata or a structured build artifact fact. It does not replace the independent artifact observation/digest contract.

## 15. Adapter authority and derivation methods

Authority is a property of the derived record/set, not merely the adapter executable. A provider may support multiple derivation methods, for example:

```text
native_field_mapping
deterministic_normalization
heuristic_extraction
```

A record may be `authority=mechanical` only when **every semantic assertion carried by that record** is derived mechanically from the immutable authorized input under the versioned adapter contract. Direct mapping from a documented machine-readable producer field and deterministic normalization qualify.

If any semantically meaningful field such as location, code, severity, relationship, or test identity is extracted heuristically from opaque human prose, the whole record is downgraded to `authority=advisory`. MVP deliberately avoids per-field authority because it would make the consumer contract disproportionately complex.

Core E22 accepts native/deterministic mechanical adapters. Heuristic parsing may exist only as an explicitly advisory provider and can never establish verification/evidence truth.

## 16. Adapter selection

ShellBeam does not select an adapter by regexing arbitrary shell command strings or log text.

A structured adapter may be selected only by these versioned sources, in precedence order:

1. a validated project command explicitly declares a supported result adapter;
2. the caller explicitly requests a supported adapter for the operation;
3. direct-argv execution has an exact executable/argument contract covered by a built-in safe adapter rule.

A complex shell pipeline such as `go test -json ./... | tee ...` is not automatically treated as a pure native Go JSON stream unless the project/caller explicitly identifies a compatible structured channel under the supported contract.

Unsupported or ambiguous adapter selection returns an explicit status rather than silently falling back to heuristic interpretation.

## 17. Lifecycle and terminal parse outcome

Structured derivation lifecycle is separate from parse outcome:

```text
lifecycle:
  pending
  processing
  terminal

parse_outcome when terminal:
  complete
  partial
  malformed
  unavailable
  budget_exceeded
```

An inspect immediately after child terminalization can therefore distinguish “parser has not run yet” from “this derivation is terminally unavailable”. Optional parser work may continue after terminal receipt publication under its bounded worker contract.

Parser failure never changes child exit status and never discards canonical raw output. If a validated evidence policy explicitly requires a structured result condition, a terminal non-complete outcome makes associated evidence `incomplete`; it still does not rewrite the child outcome.

## 18. Streaming and terminal parsing

Adapters may parse incrementally when the producer format is safely streamable and bounded. Otherwise parsing runs in a terminal-finalization worker after an immutable adapter input has been established.

An adapter input must be one of:

- a canonical retained raw-output reference plus exact byte range/identity; or
- an immutable observed result artifact/blob descriptor whose content identity, bytes/size, observation time/cut, and source operation association were pinned before parsing.

A mutable pathname such as `junit.xml` is not sufficient provenance. If ShellBeam cannot prove the parser consumed the operation's immutable observed artifact rather than a concurrently replaced file, authority/completeness is downgraded or the derivation is unavailable.

Optional parsing never blocks spawn and cannot hold terminal receipt publication indefinitely. Required evidence parsing uses explicit time/record/byte budgets.

## 19. Deduplication and bounded aggregation

Model-facing structured output is token-conscious. Repeated identical diagnostics may be mechanically aggregated with:

```text
fingerprint
occurrences
first_seen_ref
last_seen_ref
bounded locations
```

The fingerprint uses normalized stable fields such as adapter, code/rule, severity, message, and location identity. Records with materially different location/code/message are not merged merely because their text is similar.

A default inspection summary may contain:

```text
errors
warnings
files
test_passed
test_failed
records_returned
records_total_or_lower_bound
truncated
```

Pagination/selectors remain bounded and do not require returning the complete result set into model context.

## 20. Source path and provenance safety

Repository-owned result paths are normalized against the bound workspace. Paths escaping the workspace or referring to external/system locations are represented with explicit classification/redaction rather than blindly returned as trusted repo-relative paths.

Structured-result records do not read source files merely to enrich a diagnostic. The reasoning agent can use the location to inspect source separately.

Raw-output provenance is represented using `output_ref` plus a complete bounded byte range when available. If raw output is later compacted, the structured result remains valid as a derived fact while the source reference reports compacted/unavailable raw detail.

## 21. Observation API behavior

`local_shell.inspect`/compatible observation branches may return:

- current structured-result summary;
- bounded records by kind/severity/path/test status;
- a continuation token for additional records;
- journal events after an `event_cursor`;
- exactness/cache/truncation/continuity status.

The API returns facts, not a prose debugging handoff. The external reasoning agent decides which source files or commands to inspect next.

## 22. Persistence and compaction

Structured records are versioned derived state keyed by deterministic derivation identity. Publication/index/event acknowledgement may be retried after crash, but all retries address the same logical derivation. A terminal complete result cannot be double-counted or silently regenerated under a different adapter version.

Detailed records may compact earlier than immutable terminal receipts/evidence when retention requires, provided a bounded tombstone/summary preserves the original derivation identity, producer/schema, source references, authority, terminal lifecycle/outcome, and compaction state needed by retained repro/evidence references.

Deterministic rebuild is allowed only when the exact immutable source input and exact adapter/config semantics remain available. A rebuild is identified as the same logical derivation, not a fresh historical fact.

## 23. Stable failure/status additions

S1 introduces or reserves stable conditions including:

- `event_cursor_invalid`;
- `event_cursor_expired`;
- `event_continuity_unavailable`;
- `structured_adapter_unavailable`;
- `structured_adapter_unsupported`;
- `structured_result_malformed`;
- `structured_result_partial`;
- `structured_result_budget_exceeded`;
- `structured_result_not_found`.

Observation conditions do not become child-program failures. Retryability refers to the ShellBeam observation action and never implies that rerunning an externally effectful command is safe.

## 24. Performance budgets

Ordinary execution with no structured adapter request performs no journal scan and no expensive result materialization before spawn.

- Event append is bounded local work or bounded queued publication.
- Output-available events may be coalesced.
- Parser work has hard byte/record/depth/string/time budgets.
- Journal/structured inspection has hard response count/byte/work budgets.
- Percentile/telemetry work belongs to S2, not S1 parsing.

Budget exhaustion yields explicit partial/truncated status.

## 25. Privacy requirements

S1 records exclude:

- raw environment values;
- stdin contents;
- credentials/SSH/token material;
- arbitrary source file contents;
- unbounded raw logs.

Diagnostic messages originate from the producer and may themselves contain sensitive text. Implementations therefore apply the same bounded output/privacy treatment already required for command output, without pretending arbitrary producer messages are secret-free. This limitation must be documented.

## 26. Validation strategy

### 26.1 Journal tests

- authoritative transition + observation obligation survive every crash injection point;
- materialization crash after canonical commit is detected from high-watermark/obligation and cannot yield false `continuity=complete`;
- duplicate materialization retry is idempotent;
- operation/session targets work for absolute-cwd executions with no workspace/repository/activity identity;
- cursor target/epoch mismatch is rejected explicitly;
- expired/compacted cursor recovery returns snapshot and resume cursor from one consistency cut;
- transition N+1 after snapshot cut is visible after resume;
- filtered views preserve underlying cursor progress;
- `evidence_validity_changed` can represent stale->current as well as current->stale transitions.

### 26.2 Structured-result tests

- native structured and deterministic normalized adapters preserve exact immutable input provenance;
- mutable result artifact replacement cannot be attributed to the old operation;
- any heuristic semantic extraction downgrades the full record to advisory;
- lifecycle distinguishes pending/processing/terminal from terminal parse outcome;
- malformed/truncated/oversized producer input obeys budgets and preserves raw output;
- repeated diagnostics aggregate presentation without changing source truth;
- path escapes/external paths are classified/redacted;
- parser crash after durable record publication retries the same `derivation_key` rather than duplicating results.

### 26.3 Crash/isolation tests

Fault injection covers canonical commit, durable observation obligation, derived record publication, index update, event materialization, and acknowledgement boundaries. The proof target is that authoritative receipts remain correct, continuity is never overclaimed, and an automatically derived logical fact is not duplicated by recovery.

## 27. Acceptance criteria

E21/E22 are complete only when:

1. Canonical transitions covered by E21 always receive a durable observation sequence obligation at the same visibility boundary.
2. A journal projection gap is mechanically detectable after crash/restart and can never be silently reported complete.
3. Cursor expiry/restart recovery is server-driven and returns a snapshot/resume cursor from one consistency cut when within budget.
4. Event filtering supports operation/session observations even without workspace/repository/activity identity.
5. A supported failing structured command yields bounded mechanical diagnostics with immutable raw/artifact provenance.
6. Mechanical authority is impossible if any semantic field required heuristic extraction.
7. Structured lifecycle exposes pending/processing separately from terminal parse outcome.
8. Crash recovery cannot create duplicate logical result sets for one derivation.
9. Optional parsing/journal materialization does not add provider/subprocess work to ordinary spawn admission or an extra synchronous durability barrier solely for model presentation.
10. Raw output/terminal receipt remain authoritative and survive derived-feature failures unchanged.

## 28. Reference agent flow

A representative flow is:

```text
inspect activity -> event_cursor C
start project test command with structured adapter
poll/inspect after C
  -> process_terminal
  -> structured_result_recorded
  -> diagnostics summary: 7 errors / 3 files
inspect diagnostics for those files
read source locations as needed
```

The agent does not need to grep thousands of log lines to discover facts already supplied by a structured producer, but the raw output remains available for audit/debugging.
