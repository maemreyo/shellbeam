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

The journal is a bounded append-only projection produced after accepted state changes:

```text
canonical transition
       ↓
durable canonical record
       ↓
bounded journal publication
       ↓
inspect(after_event_cursor=...)
```

A canonical transition must never be rolled back because journal publication failed. If canonical state is durable and journal publication is unavailable, current state remains authoritative and clients can obtain a fresh snapshot.

The journal may be segmented/compacted internally. Public clients observe an opaque cursor contract only.

## 5. Event record contract

A version-1 event contains only bounded identification, ordering, and summary data. Conceptually:

```text
event_schema_version
event_id
sequence                     internal ordering fact; not cursor format
recorded_at
kind
repository_id?
workspace_id?
activity_id?
operation_id?
session_id?
workspace_generation?
subject_ref
summary
```

Large source records are referenced rather than copied. Events must not embed terminal receipts, raw output, source contents, complete diagnostics collections, environment values, or checkpoint payloads.

`summary` is a kind-specific closed object with strict byte/string/count limits.

## 6. Initial event kinds

The initial closed set is:

- `workspace_generation_changed`;
- `operation_admitted`;
- `process_started`;
- `output_available`;
- `process_terminal`;
- `structured_result_recorded`;
- `evidence_recorded`;
- `evidence_invalidated`;
- `artifact_observed`;
- `manifest_status_changed`;
- `session_health_changed`.

Later companion capabilities may add versioned kinds such as `repro_recorded`, `checkpoint_created`, or `input_trace_recorded` after negotiated support.

Events never use semantic/planning kinds such as `agent_should_run_tests`, `likely_root_cause`, `fix_recommended`, or `workspace_safe_to_modify`.

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

These record accepted transitions in their respective canonical/derived stores and point to the record or current status. `evidence_invalidated` includes stable reason dimensions rather than narrative explanation.

## 8. Cursor contract

The public event cursor is opaque, typed, and namespaced, for example `evtcur_v1_...`. Clients may compare only equality or pass it back to ShellBeam; they do not decode or perform arithmetic on it.

An observation request may conceptually specify:

```text
target = activity | workspace | repository
after_event_cursor = evtcur_...
max_events = N
```

The response contains:

```text
events[]
next_event_cursor
snapshot_generation
compacted_before?
continuity: complete | snapshot_required
truncated
```

Output cursors, event cursors, result pagination tokens, and other continuation handles are not interchangeable.

## 9. Ordering and continuity

The journal guarantees publication order within the local daemon/state root. It does not claim a stronger total causal order than the facts being observed.

An event is never published before the canonical transition it represents has succeeded. However, canonical success followed by journal publication failure is legal.

When continuity cannot be guaranteed, inspection reports it explicitly. Implementations may use reconciliation markers or rebuild a bounded journal segment from canonical metadata when mechanically safe, but they must never invent missing historical ordering.

## 10. Retention and cursor expiry

Journal retention is bounded by configured record count, bytes, and age. Active activities/workspaces may receive a larger bounded budget, but no target has unbounded history.

If a client presents a cursor older than retained continuity, ShellBeam reports:

```text
event_cursor_expired
snapshot_required = true
current_event_cursor = ...
```

This is an observation continuity condition, not corruption and not an execution failure. Where the inspect action permits it, ShellBeam still returns the current bounded snapshot so the client can continue from a new cursor.

Journal compaction never deletes authoritative terminal receipts/evidence merely to preserve a cursor.

## 11. Restart behavior

Daemon restart must preserve enough journal metadata to either:

- continue from a previously published durable cursor with honest continuity; or
- report that a snapshot is required.

It must not accept an old cursor and silently omit a restart gap while claiming a complete delta.

A stateless MCP bridge can reconnect and route explicit cursor/target handles to the same local daemon state without hidden transport-session state.

## 12. Structured-results family

The public abstraction is `structured_results`, not only diagnostics. Version 1 supports these record kinds:

- `diagnostic`;
- `test_case`;
- `test_suite`;
- `artifact_result`.

A future schema may add benchmark/performance result kinds, but they are not required for S1.

Raw captured bytes and authoritative child/receipt state remain canonical. Structured results are mechanical/advisory projections with provenance.

## 13. Common structured-result fields

Every structured result uses the common derived-record envelope and adds kind-specific facts. A diagnostic is conceptually:

```json
{
  "record_kind": "diagnostic",
  "authority": "mechanical",
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

The concrete schema uses bounded strings, closed enums where stable, repository-relative normalized paths when possible, and explicit absence/unavailable states when the producer cannot provide a field.

Structured records do not contain a proposed code change, fix confidence, root cause, remediation command, or agent narrative.

## 14. Result kinds

### 14.1 Diagnostic

Represents a compiler/linter/test-framework/static-analysis finding with severity, stable code/rule when available, message, primary location, bounded related locations, and source provenance.

### 14.2 Test case

Represents a mechanically identified test-case outcome such as pass/fail/skip/error plus bounded duration and suite/package association when the producer supplies them.

### 14.3 Test suite

Represents a suite/package/file-level test aggregation from the structured producer. It does not infer coverage or completeness beyond what the producer declares.

### 14.4 Artifact result

Represents a machine-readable producer result tied to an expected artifact, for example generation metadata or a structured build artifact fact. It does not replace the independent artifact observation/digest contract.

## 15. Adapter authority tiers

Adapters declare one of these producer tiers:

### 15.1 `native_structured`

The command/tool produces a documented machine-readable format directly, such as SARIF, JUnit, ESLint JSON, or `go test -json`. Normalization may map fields to the common schema without semantic interpretation.

### 15.2 `normalized_deterministic`

A versioned ShellBeam adapter converts a stable/specifiable producer format mechanically. The adapter contract and parser version are part of provenance.

### 15.3 `heuristic`

A provider infers structure from human-oriented text using patterns/heuristics. Heuristic records are `authority=advisory`; core verification/evidence logic cannot treat them as mechanical proof.

S1 core acceptance requires only the first two tiers. Heuristic parsers are not a shortcut for missing structured formats.

## 16. Adapter selection

ShellBeam does not select an adapter by regexing arbitrary shell command strings or log text.

A structured adapter may be selected only by these versioned sources, in precedence order:

1. a validated project command explicitly declares a supported result adapter;
2. the caller explicitly requests a supported adapter for the operation;
3. direct-argv execution has an exact executable/argument contract covered by a built-in safe adapter rule.

A complex shell pipeline such as `go test -json ./... | tee ...` is not automatically treated as a pure native Go JSON stream unless the project/caller explicitly identifies a compatible structured channel under the supported contract.

Unsupported or ambiguous adapter selection returns an explicit status rather than silently falling back to heuristic interpretation.

## 17. Parse status and malformed input

Every structured-result set reports a parse status:

```text
complete
partial
unavailable
malformed
```

Parser failure never changes the child's observed exit status and never discards raw captured output.

If structured results are optional, terminal publication proceeds independently and the structured-result set reports its actual status.

If a validated verification policy explicitly requires a structured result condition, `partial`, `malformed`, or unavailable required data makes the associated evidence `incomplete`; it still does not rewrite the child outcome.

## 18. Streaming and terminal parsing

Adapters may parse incrementally when the producer format is safely streamable and the overhead is bounded. Otherwise parsing runs in a terminal-finalization worker after enough canonical data is available.

Optional parsing does not hold the spawn path or terminal receipt indefinitely. Required evidence parsing is bounded by explicit work/time/record/byte limits; budget exhaustion is represented honestly.

Parser crashes or panics are isolated from authoritative execution state and result in an unavailable/partial derived record plus diagnostic crash evidence at the process/request boundary as appropriate.

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

Structured records are versioned derived state. They may be compacted earlier than immutable terminal receipts/evidence when retention requires, provided summaries/provenance remain honest.

A compacted structured record reference returns `compacted` or `not_available`; it is not regenerated from raw human output using a different adapter version and presented as the original record.

Deterministic rebuild is allowed only when the exact source record and exact adapter version/normalization semantics remain available, and the rebuilt record is identified as a rebuild of the same derivation contract.

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

- monotonic publication under concurrent canonical transitions;
- no event before canonical commit;
- fault injection after canonical commit/before journal append;
- expired cursor and snapshot-required recovery;
- daemon restart continuity versus explicit gap;
- output-event coalescing without losing output discoverability;
- retention count/byte/age limits;
- target scoping by repository/workspace/activity;
- cursor fuzzing/invalid opaque handles.

### 26.2 Structured-result tests

- native structured happy paths;
- deterministic normalization goldens;
- malformed/truncated/oversized inputs;
- duplicate diagnostic aggregation;
- path normalization/escape classification;
- binary/raw-output coexistence;
- parser work-budget exhaustion;
- adapter selection precedence;
- no heuristic fallback for unsupported shell pipelines;
- required versus optional evidence behavior;
- raw provenance before/after output compaction.

### 26.3 Crash/isolation tests

Prove that parser/journal failure cannot alter a durable terminal receipt, exactly-once operation state, or process ownership state.

## 27. Acceptance criteria

S1 is complete only when:

1. An agent can obtain a bounded delta after a valid event cursor without receiving an entire activity/workspace snapshot again.
2. An expired cursor requires an explicit snapshot and never silently loses continuity.
3. Journal publication failure cannot roll back or falsify canonical state.
4. A supported failing machine-readable command produces exact mechanical structured facts including failing files/locations where the producer supplies them.
5. Structured records link back to the producing operation and raw/result provenance.
6. Malformed or partial structured output remains distinct from child failure.
7. Unsupported/ambiguous adapter selection never falls back to heuristic truth.
8. Raw output remains canonical and cursor semantics are unchanged.
9. Bounded deduplication reduces repeated diagnostics without merging materially different findings.
10. Restart, retention, malformed input, and budget exhaustion all report exact quality/continuity states.
11. Existing V1/E01-E20 idempotency, terminal receipt, output, evidence, and privacy tests remain green.

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
