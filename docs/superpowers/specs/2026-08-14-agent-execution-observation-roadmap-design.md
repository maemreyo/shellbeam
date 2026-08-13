# ShellBeam Agent Execution Observation Roadmap Design

## Status

Approved design extending the existing ShellBeam Agent Execution Layer, Workspace/Worktree/Git Identity, and Project Capability Manifest designs. This document is the umbrella contract for enhancements E21-E28. It does not replace V1, V1 hardening, or E01-E20; it constrains how the new capabilities compose with them.

Companion designs:

- [Observation and Structured Results](./2026-08-14-observation-structured-results-design.md)
- [Execution Telemetry and Reproduction](./2026-08-14-execution-telemetry-reproduction-design.md)
- [Project Readiness and Typed Commands](./2026-08-14-project-readiness-typed-commands-design.md)
- [Experimental Safety and Input Observation Providers](./2026-08-14-experimental-provider-design.md)

## 1. Decision

ShellBeam continues to be a durable local execution substrate for a reasoning agent, not an agent, workflow engine, planner, sandbox, or Git replacement. The next architecture slice adds efficient observation, structured execution facts, empirical execution history, reproduction provenance, deterministic project readiness, and safe typed command binding while preserving the one-tool `local_shell` surface.

The design deliberately separates canonical execution truth from mechanically derived facts and advisory provider observations. New capabilities may reduce command count and context-window waste, but they must not move reasoning or policy decisions into the daemon.

E26 safety checkpoints and E27 dynamic input tracing are fully specified as experimental/provider contracts. They do not gate completion of the core E21-E25/E28 roadmap.

## 2. Baseline contracts retained

The following existing contracts remain authoritative:

- MCP/tool success is not child success.
- Terminal receipt, spawn evidence, exit evidence, output drain, accepted-versus-delivered input, and ambiguity rules remain authoritative.
- `operation_id` remains exactly-once start intent; retries never create a second execution when the original effect is ambiguous.
- `session_id`, `activity_id`, `workspace_id`, `repository_id`, and `evidence_id` retain their existing meanings.
- A daemon that no longer owns a process never reconstructs signal authority from a persisted PID/PGID.
- Raw append-only captured output remains canonical for cursor/replay semantics.
- Evidence validity remains conservative and multidimensional.
- Project manifest inspection executes nothing and manifest status never blocks ordinary coding.
- Workspace mutation coordination remains advisory rather than locking.
- Capability absence, incomplete observation, truncation, cache age, and ambiguity are reported explicitly rather than guessed away.

## 3. Architectural model

New data flows are layered over canonical durable truth:

```text
Canonical durable truth
├─ operation/session state and terminal receipts
├─ raw output
├─ workspace/source generation and exact digests
├─ evidence ledger
├─ artifact observations
└─ project manifest/discovery state
          │
          ▼
Derived execution facts
├─ structured results and diagnostics
├─ performance/resource observations
├─ reproduction capsules
├─ project readiness
└─ experimental provider observations
          │
          ▼
Event Journal
bounded change feed / observation accelerator
```

The event journal is not replay authority. Derived records do not supersede the facts from which they were produced. Provider observations cannot gain stronger authority merely because they are precise or convenient.

## 4. Authority classes

Every new persisted or exposed fact declares one authority class:

### 4.1 Authoritative

Facts directly backed by ShellBeam's durable state transition or exact observation contract, for example terminal receipts, immutable evidence, exact digests, and an explicitly completed checkpoint restore mutation.

### 4.2 Mechanical

Deterministic projections whose provenance is known and versioned, for example a diagnostic normalized from native structured compiler output, a readiness comparison, or a performance aggregate. Mechanical records may be recomputed when all required source records remain available.

### 4.3 Advisory

Best-effort or experimental facts that must not determine correctness, for example non-hermetic dynamic input traces or incomplete platform resource observations.

No code path may implicitly promote `advisory -> mechanical` or `mechanical -> authoritative`.

## 5. Cross-cutting invariants

The following statements are normative:

1. **Journal is not truth.** Canonical records remain correct if journal publication fails or history is compacted.
2. **Diagnostics report facts; they never recommend fixes.** Root-cause reasoning remains with the model/user.
3. **Telemetry informs; it never chooses, schedules, or retries commands.** Historical duration is evidence, not policy.
4. **Readiness observes; it never installs, repairs, starts services, or mutates the project.**
5. **Observed inputs cannot narrow evidence validity without a proven hermetic boundary.** Best-effort tracing may broaden suspicion, never prove irrelevance.
6. **Derived failure never rewrites execution truth.** A parser, telemetry writer, journal writer, or repro materializer failure cannot invalidate an already durable terminal receipt.
7. **No hidden workflow language.** Typed project commands bind data into a single command invocation; they do not introduce conditionals, loops, matrices, dependency execution, or shell templating.
8. **Privacy is monotonic.** A new derived view cannot expose data that the underlying authoritative contract forbids, including secret values or checkpoint file contents.

## 6. Common derived-record provenance envelope

E22-E27 share a small versioned provenance envelope. Concrete schemas add type-specific fields but preserve these concepts:

```text
record_id
record_kind
schema_version
repository_id
workspace_id
activity_id?
operation_id?
receipt_digest?
producer:
  adapter_id
  adapter_version
  capability_version
captured_at
completeness: complete | partial | unavailable
authority: authoritative | mechanical | advisory
source_refs[]
```

`source_refs` point to canonical or previously derived records such as raw output byte ranges, receipts, evidence IDs, manifest digests, source digests, artifact digests, or provider records. Derived records do not duplicate large blobs merely for convenience.

The envelope never contains raw environment values, tokens, private keys, Git credentials, arbitrary source contents, or raw checkpoint payloads. Hashing unknown secret values is also forbidden.

## 7. New enhancement IDs

The existing E01-E20 table is extended, not replaced:

| ID | Capability | Decision |
| --- | --- | --- |
| E21 | Event Journal / cursor-based observation | Core; bounded change feed, never source of truth. |
| E22 | Structured Execution Results | Core; mechanical normalized facts with raw provenance. |
| E23 | Execution Performance & Resource Telemetry | Core observation; enforcement is experimental/separate. |
| E24 | Reproduction Capsule | Core; immutable provenance projection, never a reproducibility guarantee. |
| E25 | Project Readiness | Core; deterministic manifest-derived observation, no repair. |
| E26 | Explicit Local Safety Checkpoint | Experimental/provider; explicit bounded content snapshot and CAS restore. |
| E27 | Dynamic Input Tracing | Experimental/provider; advisory observed inputs unless hermetic enforcement exists. |
| E28 | Typed Parameterized Project Commands | Core; restricted argv binding, no workflow language. |

Nothing in E21-E28 requires a second MCP tool or daemon-side reasoning.

## 8. One-tool capability discovery

`local_shell` remains a closed versioned union. Capability discovery adds explicit version and maturity information. Conceptually:

```text
event_journal:
  version: 1
  maturity: stable
structured_results:
  version: 1
  adapters: [...]
execution_telemetry:
  version: 1
  resource_metrics: [...]
reproduction_capsules:
  version: 1
project_readiness:
  version: 1
typed_project_commands:
  version: 1
  parameter_kinds: [...]
safety_checkpoints:
  version: 1
  maturity: experimental
  provider: ...
input_tracing:
  version: 1
  maturity: experimental
  authority: advisory
```

Clients do not infer maturity from a version number. Unsupported optional fields are absent with an explicit capability/status marker; the bridge does not fabricate them.

## 9. Identity additions

Existing identities are not overloaded. New namespaces are introduced where required:

- opaque `event_cursor` for journal continuation;
- typed `record_id` for structured/telemetry/provider records;
- `repro_id` for a reproduction capsule;
- `checkpoint_id` for an explicit safety checkpoint;
- provider observation IDs for input traces and future provider records.

Internally a journal may use monotonic sequence numbers, but the public cursor is opaque and namespaced so clients cannot confuse it with output cursors or pagination tokens.

## 10. Version domains and compatibility

Versioning is capability-specific rather than one global data version:

```text
local_shell tool schema             negotiated next version as branches land
event journal schema                v1
derived record envelope             v1
structured results schema           v1
telemetry record schema             v1
reproduction capsule schema         v1
project manifest                    v2 for requirements + typed params
experimental provider contract      v1
```

The `local_shell` schema remains closed; new action/field families require negotiated schema evolution rather than silently widening an old schema.

Manifest v2 reads existing v1 fixed commands without changing their semantics. A v1 reader encountering v2 reports `unsupported` and preserves the file byte-for-byte. ShellBeam never auto-upgrades a manifest solely because a newer daemon is installed.

Terminal receipts and idempotency tombstones are authoritative historical state and are never rewritten into the new derived-record formats. Derived indexes/caches may rebuild when their source records remain available.

## 11. Storage and package boundaries

No catch-all `observability`, `common`, `utils`, or `helpers` subsystem should own these capabilities. Domain storage is conceptually separated:

```text
events/                 bounded journal segments
derived/
  structured/
  telemetry/
  repro/
projects/
  readiness cache
providers/
  checkpoint metadata
  trace metadata
```

Checkpoint content CAS is sensitive provider-owned content and is not placed in the generic derived-record store.

Implementation plans should prefer focused domain packages, for example:

```text
core/event
core/result
core/telemetry
core/repro
core/readiness
core/projectcommand
app/observation
app/readiness
app/reproduction
adapter/result/<format>
adapter/telemetry/<platform>
provider/checkpoint/<implementation>
provider/trace/<implementation>
```

These are architectural responsibilities, not mandatory filenames; plans must reconcile them with the codebase at implementation time and avoid unrelated refactoring.

## 12. Durability and crash semantics

An authoritative transition is never rolled back because a derived projection fails.

Examples:

- receipt durable + structured parser crash => command terminal truth remains valid; structured result becomes unavailable/partial;
- canonical state durable + journal append failure => truth remains valid; journal reports a gap/reconciliation requirement or the client snapshots current state;
- terminal receipt durable + telemetry persistence failure => receipt remains valid; telemetry is unavailable;
- execution complete + repro materialization failure => execution remains valid; repro is unavailable.

Only data explicitly declared as a required evidence condition may make the associated evidence record `incomplete`; it still does not rewrite child exit or transport truth.

All new durable writers use the existing crash-safe atomic publication rules appropriate to their authority class. Experimental provider crashes cannot corrupt receipt/idempotency state.

## 13. Delivery roadmap

New work is staged after the existing Agent Execution Layer A0/A1/A2 foundation:

### A3: observation substrate

- E21 Event Journal;
- E22 Structured Execution Results;
- common derived-record provenance envelope.

A3 is the principal prerequisite for later slices because subsequent capabilities may publish small journal events and reuse the same provenance contract.

### A4: empirical execution knowledge

- E23 performance/resource observation;
- E24 Reproduction Capsule.

A4 may proceed independently of A5 after A3.

### A5: declarative project readiness

- E25 Project Readiness;
- E28 Typed Parameterized Project Commands.

A5 may proceed independently of A4 after A3 and the existing project-manifest foundation.

### X1: experimental providers

- E26 Safety Checkpoint;
- E27 Dynamic Input Tracing;
- optional resource enforcement provider/capability from the E23 design.

X1 never blocks release or completion claims for A3-A5.

Existing B1 persistent-runtime and B2 provider-integration work remains valid; implementation planning must order tasks by actual dependencies rather than rewriting historical milestone numbering.

## 14. Performance requirements

Ordinary command admission must remain fast. When a caller does not request the new capabilities, warm `start` performs no journal scan, percentile aggregation, repro materialization, readiness refresh, tracing, checkpoint work, or arbitrary tool probe.

- Journal publication is bounded local work or bounded queued publication.
- Structured parsing runs streaming only when cheap or in terminal-finalization workers.
- Telemetry aggregation is asynchronous/bounded and not recomputed on every poll.
- Repro materialization never runs before spawn.
- Readiness/toolchain probes reuse bounded caches and are outside warm admission.
- `inspect` enforces record, byte, and work budgets and returns explicit partial/truncation markers.

A budget overrun never silently drops data while claiming completeness.

## 15. Security and privacy requirements

Automated privacy tests must prove that persisted/public records do not expose fixture values representing:

- raw environment values;
- access tokens and Git credentials;
- SSH/private-key material;
- stdin secrets;
- checkpoint file contents;
- raw network payloads.

External absolute paths are redacted or classified according to the concrete companion contract. E26 checkpoint storage is classified as sensitive local content and requires its own security review before an implementation is enabled by default.

## 16. Verification strategy

The umbrella verification matrix requires all applicable companion tests plus these cross-cutting groups:

1. **Contract/golden tests:** closed schemas, old/new fixtures, capability negotiation, unknown-version rejection.
2. **Durability/crash tests:** fault injection around derived publication proving terminal/idempotency truth survives failures.
3. **Cursor/retention tests:** compaction, expired cursors, daemon restart, gaps/reconciliation, bounded responses.
4. **Structured-result adversarial tests:** malformed/oversized native formats, path escape, binary output, duplicates, parser budgets.
5. **Telemetry/repro tests:** incompatible aggregation keys, failed/timeout samples, metric quality, compacted references, privacy matrix.
6. **Readiness/binding tests:** manifest v1/v2 compatibility, missing/incompatible prerequisites, secret-safe environment presence, param validation before spawn, retry after manifest change.
7. **Experimental provider safety tests:** CAS restore conflicts, symlink/special-file behavior, tracing truncation/ownership gaps, and proof advisory traces cannot narrow evidence.
8. **Native platform gates:** real macOS and Linux evidence for platform-specific resource/provider semantics. Cross-builds are compile evidence only.

Fuzz/property tests are expected for cursor decoding, parameter binding, path normalization, structured-result parsers, manifest v2 parsing, and checkpoint restore preconditions.

## 17. Core definition of done

E21-E25 and E28 are core-complete only when all of the following are true:

1. A reconnecting agent can obtain bounded activity/workspace deltas without reconstructing whole state each time.
2. A failing command with a supported structured producer yields exact mechanical diagnostic/result facts with raw provenance.
3. Historical execution cost/resource facts are inspectable without ShellBeam making scheduling decisions.
4. One execution can produce an inspectable reproduction provenance record whose unknown dimensions remain explicit.
5. Project readiness reports manifest-declared prerequisites mechanically without installing or repairing anything.
6. Typed project commands bind safely without shell templating, implicit graph execution, or workflow semantics.
7. Existing receipt/idempotency/ownership/evidence invariants remain green under new schema versions and crash injection.
8. Native macOS and Linux checkpoint CI is green for the applicable core capabilities.
9. Ordinary compatible execution has no material warm-admission latency regression.
10. E26/E27 absence or experimental failure does not make core completion incomplete.

## 18. Experimental readiness

E26/E27 may be described only as `experimental-ready` when their companion provider acceptance criteria pass on the provider/platform under test. They are not part of a general ShellBeam production/core-complete claim until a separate design promotes them.

## 19. Non-goals

This roadmap does not add:

- daemon-side planning or fix recommendations;
- automatic command scheduling/retry selection;
- a workflow DSL;
- automatic environment installation/repair;
- automatic undo around commands;
- Git replacement semantics;
- default sandbox/container execution;
- remote/fleet execution;
- a core vector database/index;
- hermeticity inferred from observation;
- automatic affected-test narrowing from dynamic traces.

The product remains an evidence-rich local execution substrate that lets the external reasoning agent reason from explicit facts rather than terminal noise.
