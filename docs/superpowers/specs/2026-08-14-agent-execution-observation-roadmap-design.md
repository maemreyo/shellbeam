# ShellBeam Agent Execution Observation Roadmap Design

## Status

Approved E21-E28 baseline extending the existing ShellBeam Agent Execution Layer, Workspace/Worktree/Git Identity, and Project Capability Manifest designs. The E29 + Lazy Workspace Freshness composition in this revision is review-ready and must pass the final design gate before implementation planning. This document remains the umbrella contract for enhancements E21-E29 and does not replace V1, V1 hardening, or E01-E20; it constrains how the new capabilities compose with them.

Companion designs:

- [Observation and Structured Results](./2026-08-14-observation-structured-results-design.md)
- [Execution Telemetry and Reproduction](./2026-08-14-execution-telemetry-reproduction-design.md)
- [Project Readiness and Typed Commands](./2026-08-14-project-readiness-typed-commands-design.md)
- [Experimental Safety and Input Observation Providers](./2026-08-14-experimental-provider-design.md)
- [Structured Code Intelligence](./2026-08-14-structured-code-intelligence-design.md)

## 1. Decision

ShellBeam continues to be a durable local execution substrate for a reasoning agent, not an agent, workflow engine, planner, sandbox, or Git replacement. The next architecture slice adds efficient observation, structured execution facts, empirical execution history, reproduction provenance, deterministic project readiness, and safe typed command binding while preserving the one-tool `local_shell` surface.

The design deliberately separates canonical execution truth from mechanically derived facts and advisory provider observations. New capabilities may reduce command count and context-window waste, but they must not move reasoning or policy decisions into the daemon.

E26 safety checkpoints and E27 dynamic input tracing are fully specified as experimental/provider contracts. They do not gate completion of the core E21-E25/E28/E29 roadmap.

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
├─ structured code-intelligence observations
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

1. **Journal is not truth.** Canonical state plus its durable observation-order assignment remains authoritative; journal records are rebuildable/compactable projections.
2. **Diagnostics report facts; they never recommend fixes.** Structured and semantic providers normalize observations without becoming a reasoning agent.
3. **Telemetry informs; it never chooses or schedules commands.** Historical cost is empirical input to the reasoning agent.
4. **Readiness observes; it never installs, repairs, starts services, or mutates the project.**
5. **Observed inputs cannot narrow evidence validity without a proven hermetic boundary.** Best-effort tracing may broaden suspicion, never prove irrelevance.
6. **Derived failure never rewrites execution truth.** A parser, telemetry writer, journal materializer, code-intelligence provider, or repro materializer failure cannot invalidate an already durable terminal receipt.
7. **No hidden workflow language.** Typed project commands bind data into one command invocation; they do not introduce conditionals, loops, matrices, dependency execution, or shell templating.
8. **Privacy is monotonic.** A new projection cannot expose data forbidden by the underlying authority contract, including secret values or checkpoint contents/deterministic hashes of arbitrary sensitive bytes.
9. **Internal correctness machinery is not mandatory model-facing workflow.** State-root epochs, change-sequence internals, derivation keys, provider protocol versions, checkpoint preimage identities, LSP document synchronization, AST node IDs, and detailed trace coverage matrices are hidden on the ordinary path and exposed only through explicit deep inspection when useful.
10. **Recovery is server-driven.** A cursor expiry, projection lag, or provider refresh should normally be resolved in one bounded inspect response with a current snapshot/resume handle rather than requiring multi-call protocol choreography by the agent.
11. **One capability should normally cost at most one intentional additional agent action.** Passive structured results/telemetry cost zero; explicit repro/checkpoint/trace/code-intelligence queries use one model-oriented request unless the agent deliberately drills into detail.
12. **Language intelligence is observation, not verification.** LSP/type-checker/index facts can accelerate the edit loop but cannot replace authoritative build/test/evidence semantics.

## 6. Common correlation and publication primitives

The roadmap uses two different correctness primitives. They share correlation/provenance conventions but are not collapsed into one abstraction because ordered change publication and idempotent derivation have different semantics.

### 6.1 Correlation envelope

Concrete records declare only identities meaningful to that record kind:

```text
record_id
record_kind
schema_version
correlation_scope: state_root | repository | workspace | activity | operation | session | provider
repository_id?
workspace_id?
activity_id?
operation_id?
session_id?
receipt_digest?
producer:
  producer_id
  producer_version
  capability_version
captured_at
source_refs[]
```

`repository_id` and `workspace_id` are not globally mandatory. A valid absolute-cwd operation such as `/tmp` may have neither. Workspace-only capabilities such as E26 still require an explicit workspace.

### 6.2 Durable change-publication primitive

Every canonical mutation that participates in E21 observation is assigned a durable state-root observation position as part of the same visibility boundary:

```text
state_root_epoch
change_seq
source_transition_ref
materialization_state
```

`change_seq` is the durable observation-order authority. Journal entries are derived materializations at a sequence; they are not themselves authority. The implementation may use a WAL, transactional outbox, or equivalent local durability mechanism, but it must make the authoritative transition and its observation obligation indivisible from the perspective of recovery.

### 6.3 Deterministic derived-record primitive

Automatically derived records such as structured results, telemetry, and semantic observations use a stable logical identity:

```text
derivation_key
source_authority_refs[]
producer_id
producer_version
derivation_schema_version
derivation_config_digest
lifecycle: pending | processing | terminal
completeness
```

The key is deterministic for one logical derivation. Crash recovery upserts the same logical record instead of creating another sample/fact. Lifecycle/completeness may advance monotonically when the exact derivation contract permits it; a completed telemetry sample cannot become a second sample merely because finalization was retried.

`source_refs` point to canonical or immutable observed inputs such as raw-output ranges, receipts, evidence IDs, manifest/source digests, pinned artifact observations, or opaque source-reference handles. Derived records do not duplicate large blobs merely for convenience.

### 6.4 Canonical `SourceRef` / `SourceLocation` contract

Structured execution/code facts that need a source position share one location contract rather than inventing provider-specific path/column semantics. `source_ref_id` is a **server-issued opaque identity handle**, not an authorization capability. It is scoped to one ShellBeam state-root/source-view domain and binds immutably to one exact source representation once issued.

Normative identity rules:

- the same `source_ref_id` MUST NOT be rebound to different source bytes or a different source representation after a source change;
- a source reference may expire or lose its retained backing representation, but an expired ID is never recycled/reused for another source;
- expiry/unavailability is explicit (`source_ref_expired` / `source_ref_unavailable`) and never silently resolves the old ID against current bytes;
- provider document versions, LSP session/snapshot generations, index revisions, or parser-specific hashes are correlation proof machinery, not canonical source authority;
- `source_ref_id` and `resolution_quality` describe identity/resolution only; neither grants filesystem/process authorization.

Conceptually:

```text
SourceRef
  source_ref_id                  # opaque, server-issued, never rebound
  origin: repository | workspace | dependency | toolchain | generated | external
  source_view_id / epoch ref
  repository_id?
  workspace_id?
  logical_path?                  # safe normalized path when available
  display_identity?              # bounded/sanitized, never raw host path by default
  source_encoding: utf-8         # E29 v1 resolved-source contract
  resolution_quality: exact | observed | unavailable

SourceLocation = closed union

  ResolvedSourceLocation
    kind = resolved
    source_ref_id
    byte_range = [start, end)     # zero-based half-open offsets into exact UTF-8 bytes
    display_range?                # non-authoritative presentation coordinates + encoding

  ProviderReportedLocation
    kind = provider_reported
    origin
    provider_id/version
    display_identity?
    sanitized_logical_path?
    provider_original_range?      # bounded original coordinate + declared encoding
    normalization_quality: partial | uncertain | unavailable
```

`ResolvedSourceLocation` is emitted only when ShellBeam has the exact retained UTF-8 source representation required to validate/convert the range. A provider target in a dependency, toolchain, generated, or external source for which exact bytes are unavailable is represented as `ProviderReportedLocation`; ShellBeam never fabricates a canonical byte range. Any operation requiring a canonical position rejects a provider-reported location with `location_not_resolved` until the exact source representation is resolved.

Repository/workspace locations use safe relative logical paths. Dependency/toolchain/external targets use bounded logical/display identities where possible; host-specific absolute paths are redacted/classified by default. E29 v1 does not introduce a new public deterministic per-file content hash merely to identify a location. Existing predecessor namespace-level `source_content_digest` semantics remain unchanged.

The primitives never contain raw environment values, tokens, private keys, Git credentials, arbitrary source contents, raw checkpoint payloads, public deterministic hashes of unknown checkpoint contents, or newly introduced public deterministic hashes of arbitrary individual source files solely for location identity.

## 7. New enhancement IDs

The existing E01-E20 table is extended, not replaced:

| ID | Enhancement | Maturity / role |
| --- | --- | --- |
| E21 | Event Journal / cursor-based observation | Core; bounded projection over durable `change_seq`. |
| E22 | Structured Execution Results | Core; mechanical/advisory derived execution facts. |
| E23 | Execution Performance & Resource Telemetry | Core observation; resource enforcement remains experimental. |
| E24 | Reproduction Capsule | Core immutable capture-cut projection. |
| E25 | Project Readiness | Core deterministic project/environment observation. |
| E26 | Explicit Local Safety Checkpoint | Experimental provider; explicit bounded sensitive-content snapshot/conditional restore. |
| E27 | Dynamic Input Tracing | Experimental provider; advisory unless a future hermetic boundary is proven. |
| E28 | Typed Parameterized Project Commands | Core manifest/argv binding; no workflow language. |
| E29 | Structured Code Intelligence | Core provider contract; semantic diagnostics/navigation plus optional structural/index providers, never execution evidence. |

E29 is intentionally a fact/query surface rather than a code-editing subsystem. AST mutation/refactoring and generic query DSLs remain out of scope.

## 8. One-tool capability discovery

`local_shell` remains one closed versioned tool. Capability discovery advertises feature versions, maturity, providers, and limits before an agent relies on them. New capability families include conceptually:

```text
event_journal:
  version: 1
  maturity: stable

structured_results:
  version: 1
  adapters: [...]

execution_telemetry:
  version: 1
  resource_metrics: {...}

reproduction_capsules:
  version: 1

project_readiness:
  version: 1

typed_project_commands:
  version: 1
  parameter_kinds: [...]

code_intelligence:
  version: 1
  providers:
    semantic: [...]
    structural: [...]
    index: [...]
  queries: [diagnostics, symbols, definition, references, import_declarations, resolved_import_targets, type_definition, type_summary, ...]

safety_checkpoints:
  version: 1
  maturity: experimental
  providers: [...]

input_tracing:
  version: 1
  maturity: experimental
  providers: [...]
  authority: advisory
```

Maturity is explicit and never inferred from a version number. Missing providers/capabilities return honest unavailable status; they do not make basic command execution unavailable. Provider installation is never automatic.

The agent-facing vocabulary is model-oriented. Capability discovery does not require the agent to learn LSP method names, JSON-RPC framing, document versions/URIs, Tree-sitter node kinds, SCIP protobuf internals, or provider-specific process lifecycle.

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

New records remain domain-owned instead of accumulating in one generic observability manager. Conceptual ownership is:

```text
events/                       bounded materialized journal segments
derived/
  structured/                 E22 results
  telemetry/                  E23 records/summaries
  repro/                      E24 immutable descriptors/tombstones
  code/                       E29 semantic/structural/index result metadata
projects/
  readiness/                  E25 cache
providers/
  checkpoints/                E26 metadata only
  traces/                     E27 metadata only
```

Provider-private checkpoint content-store state remains outside generic derived storage because it contains sensitive local bytes.

Implementation package responsibilities should remain small and capability-oriented, for example:

```text
core/event
core/result
core/telemetry
core/repro
core/readiness
core/projectcommand
core/codeintel
app/observation
app/reproduction
app/readiness
app/codeintel
adapter/result/<format>
adapter/telemetry/<platform>
adapter/codeintel/lsp
adapter/codeintel/astgrep
adapter/codeintel/scip
provider/checkpoint/<implementation>
provider/trace/<implementation>
```

These are architectural responsibilities, not mandatory filenames. Plans must reconcile them with the codebase at implementation time and avoid unrelated refactoring, catch-all `utils/common/helpers`, or a single manager that owns execution, observation, and provider lifecycle.

## 12. Durability and crash semantics

An authoritative transition is never rolled back because a derived projection fails. The stronger rule is that any transition promised through E21 cannot become externally visible without its durable `change_seq`/observation obligation being committed under the same recovery boundary.

Examples:

- receipt canonical transition + durable change obligation committed; journal materialization crashes => command truth remains valid, high-watermark proves the journal is behind, and recovery rematerializes or requests a snapshot without claiming continuity;
- receipt durable + structured parser crash => command terminal truth remains valid; one deterministic derivation remains partial/unavailable instead of being duplicated;
- terminal receipt durable + telemetry persistence/index acknowledgement crash => retry upserts the same sample derivation; history never counts the execution twice;
- execution complete + repro materialization failure => execution remains valid; the repro-create mutation receipt reports its actual result;
- code-intelligence provider failure => source/execution truth remains valid; semantic observation is stale/unavailable and never fabricated.

Only data explicitly declared as a required evidence condition may make the associated evidence record `incomplete`; it still does not rewrite child exit or transport truth.

All new durable writers use crash-safe atomic publication appropriate to their authority class. Experimental provider crashes cannot corrupt receipt/idempotency state. Mutation-like actions such as checkpoint create/restore and explicit repro materialization use durable request identity/receipts so a lost response cannot cause a second externally visible mutation under retry.

## 13. Delivery roadmap

New work is staged after the existing Agent Execution Layer foundation without forcing unrelated capabilities to wait for one another.

### A3a: shared observation correctness foundation

- capability/version/maturity negotiation extensions;
- optional correlation envelope for non-workspace operations;
- durable state-root `change_seq` and observation obligation;
- deterministic derived-record identity/lifecycle;
- bounded storage/retention primitives;
- state-root managed-shell cache invalidation (`WorkspaceCoherenceTracker`) plus lazy workspace-delta sampling;
- explicit separation of model-facing workspace/activity selection, provider synchronization scope, `SourceRef` correlation, and exact-source evidence authority;
- Agent Ergonomics / No Protocol Choreography contract.

A3a is the hard prerequisite for later automatically derived capabilities.

### A3b: execution observation

- E21 Event Journal materialization and server-driven snapshot recovery;
- E22 Structured Execution Results and immutable structured-input provenance.

### A3c: structured code intelligence

- E29A semantic diagnostics, initially Go/gopls over LSP;
- E29B definition/references/symbols/import declarations/resolved targets/type-definition/type-summary facts;
- optional query-only structural provider backed by ast-grep;
- optional precomputed SCIP consumption deferred until there is demonstrated need.

A3c depends on A3a, not on E22 completion. E29 diagnostics may reuse the E22 diagnostic presentation schema when E22 is available.

### A4: empirical execution knowledge

- E23 performance/resource observation;
- E24 Reproduction Capsule.

A4 depends on A3a. E21/E22/E29 references are optional integrations, not prerequisites for telemetry to exist.

### A5: declarative project readiness

- E25 Project Readiness;
- E28 Typed Parameterized Project Commands;
- optional readiness observations for declared code-intelligence providers.

A5 depends on A3a plus the existing project-manifest foundation, not on Event Journal or diagnostics completion.

### X1: experimental providers

- E26 Safety Checkpoint;
- E27 Dynamic Input Tracing;
- optional resource enforcement provider/capability from E23.

X1 never blocks release or completion claims for A3-A5.

Existing persistent-runtime/provider-integration work remains valid; implementation planning orders tasks by actual dependency rather than rewriting historical milestone numbering.

## 14. Performance requirements

Feature support must not accumulate into an invisible tax on every shell command. Performance gates are global before capability-specific.

### 14.1 Ordinary compatible start

With E21-E29 supported but no optional observation/provider feature requested, warm ordinary `start` must perform:

- zero code-intelligence/tracing/checkpoint provider startup;
- zero readiness refresh, telemetry aggregation, repro materialization, or structured-result scan;
- zero network/SSH/`gh` access;
- zero additional subprocesses beyond the predecessor warm-admission contract;
- zero Git subprocesses solely to refresh workspace pre/post provenance for an ordinary shell command that did not explicitly request an activity baseline, workspace freshness, or identity preflight; cheap registry binding plus O(1) managed-shell invalidation is the default;
- no extra synchronous durability barrier solely to materialize a journal event. The durable observation obligation is committed with the canonical transition mechanism; event projection is decoupled.

The release benchmark compares the same build on the same reference corpus with the roadmap capabilities disabled versus enabled-but-unused. Initial regression gates are **p95 incremental warm-admission <= 5 ms and p99 <= 10 ms**, while the complete operation must also remain inside the predecessor global workspace-assistance ceiling. These numbers are global deltas, not per-capability budgets that may be added together. A design change that cannot meet them must explicitly revise the benchmark contract before release rather than silently weakening the gate.

### 14.2 Explicit capability work

- Cached typed-project-command binding targets p95 <= 10 ms and performs no subprocess/network access.
- Journal delta inspection, structured-result reads, telemetry/history inspection, and code-intelligence result rendering enforce explicit record/byte/work ceilings. E29 additionally bounds selected files/source bytes, in-flight requests, provider queue depth, restart rate/cooldown, and provider resource observation/enforcement according to platform capability.
- Changed-file/workspace-delta queries pay for a bounded freshly sampled selection only when requested. Managed-shell overlap prevents cache promotion but does not make the sample unusable; workspace sampling is never relabeled as an exact source/evidence cut.
- Semantic provider cold start/indexing is explicit code-intelligence work and never part of ordinary `start`; capability-specific startup/query budgets and `initializing`/partial states are reported honestly.
- Required tracing has an explicit startup/instrumentation budget because it intentionally changes execution admission semantics.
- Checkpoint capture/restore has explicit path/byte/work budgets and is never implicit.

A budget overrun returns partial/unavailable/initializing status according to the capability contract; it never silently drops data while claiming completeness.

## 15. Security and privacy requirements

Automated privacy tests must prove that persisted/public records do not expose fixture values representing:

- raw environment values;
- access tokens and Git credentials;
- SSH/private-key material;
- stdin secrets;
- checkpoint file contents;
- raw network payloads;
- new public deterministic hashes of arbitrary individual source files introduced solely for E29 location identity.

External absolute paths are redacted or classified according to the concrete companion contract. E26 checkpoint storage is classified as sensitive local content and requires its own security review before an implementation is enabled by default.

## 16. Verification strategy

The umbrella verification matrix requires all applicable companion tests plus these cross-cutting groups:

1. **Contract/golden tests:** closed schemas, old/new fixtures, capability negotiation, unknown-version rejection.
2. **Durability/crash tests:** fault injection around derived publication proving terminal/idempotency truth survives failures.
3. **Cursor/retention tests:** compaction, expired cursors, daemon restart, gaps/reconciliation, bounded responses.
4. **Structured-result adversarial tests:** malformed/oversized native formats, path escape, binary output, duplicates, parser budgets.
5. **Telemetry/repro tests:** incompatible aggregation keys, failed/timeout samples, metric quality, compacted references, privacy matrix.
6. **Readiness/binding tests:** manifest v1/v2 compatibility, missing/incompatible prerequisites, secret-safe environment presence, param validation before spawn, retry after manifest change.
7. **Experimental provider safety tests:** conditional-restore conflict semantics, symlink/special-file behavior, tracing truncation/ownership gaps, and proof advisory traces cannot narrow evidence.
8. **Native platform gates:** real macOS and Linux evidence for platform-specific resource/provider semantics. Cross-builds are compile evidence only.
9. **Lazy workspace/provider coherence:** ordinary no-Git admission, managed-shell epoch/active overlap, TTL-cache invalidation, lazy tagged receipt provenance, activity/workspace selection, dirty->clean/delete/source-view transitions, provider sync coverage/restart/config incompatibility, untracked/sparse/submodule/ignored quality, and macOS case/Unicode path behavior.

Fuzz/property tests are expected for cursor decoding, parameter binding, path normalization, structured-result parsers, manifest v2 parsing, and checkpoint restore preconditions.

## 17. Core definition of done

E21-E25, E28, and the promoted E29 core surface are core-complete only when all of the following are true:

1. A reconnecting agent can obtain bounded deltas without reconstructing whole state and without manually orchestrating epoch/gap recovery.
2. A canonical transition cannot create an undetectable Event Journal gap; snapshot/resume uses one consistency cut.
3. A failing command with a supported structured producer yields mechanical diagnostic/result facts from immutable input provenance.
4. Crash retry cannot duplicate automatically derived diagnostics or telemetry samples.
5. Historical execution cost/resource facts are inspectable without ShellBeam making scheduling decisions and without unbounded aggregation-key growth.
6. One execution can produce an immutable reproduction provenance record with an explicit capture cut, stable creation descriptor, and honest post-compaction resolution state.
7. Project readiness reports manifest-declared prerequisites mechanically without installing or repairing anything.
8. Typed project commands replay existing `operation_id` bindings before reading current workspace/manifest/provider state and bind safely without shell templating or workflow semantics.
9. E29 returns bounded semantic diagnostics/navigation through model-oriented queries without exposing LSP/workspace/provider-sync choreography; changed-file selection, provider-sync scope, exact `SourceRef` correlation, and exact evidence remain separate contracts.
10. Provider absence/indexing/staleness never blocks ordinary shell execution, and ordinary shell workspace provenance uses cheap binding + managed-shell invalidation with lazy/unreconciled pre/post sampling instead of mandatory Git refresh.
11. Existing receipt/idempotency/ownership/evidence invariants remain green under new schema versions and crash injection.
12. Enabled-but-unused roadmap capabilities pass the global incremental admission benchmark and storage/work ceilings.
13. E26/E27 absence or experimental failure does not make core completion incomplete.

## 18. Experimental readiness

E26/E27 may be described only as `experimental-ready` when their companion provider acceptance criteria pass on the provider/platform under test. They are not part of a general ShellBeam production/core-complete claim until a separate design promotes them.

## 19. Non-goals

This roadmap does not add:

- an always-on workspace watcher or automatic diagnostics after every shell command;
- a generic lifecycle hook/plugin framework for internal coherence transitions;
- a ShellBeam-owned editor/patch engine, shadow filesystem, whole-repository provider mirror, or generic semantic-config engine;
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
