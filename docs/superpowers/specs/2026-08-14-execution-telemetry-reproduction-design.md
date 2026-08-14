# ShellBeam Execution Telemetry and Reproduction Design

## Status

Approved companion design for E23 Execution Performance & Resource Telemetry and E24 Reproduction Capsule. This spec extends the [Agent Execution Observation Roadmap](./2026-08-14-agent-execution-observation-roadmap-design.md) and assumes the E21/E22 observation/provenance substrate defined in [Observation and Structured Results](./2026-08-14-observation-structured-results-design.md).

Resource enforcement is intentionally separated from resource observation and remains an experimental capability contract rather than a core E23 acceptance requirement.

## 1. Decision

ShellBeam records bounded empirical execution performance/resource facts and exposes reusable historical distributions keyed by compatible command/project/environment/toolchain identity. ShellBeam also creates immutable reproduction capsules that project the captured conditions and results of a specific execution without claiming that rerunning those conditions is guaranteed to reproduce the same behavior.

Telemetry informs the reasoning agent; it never selects, schedules, suppresses, or retries commands. Reproduction capsules bind provenance; they do not package source code, create environments, or automatically rerun commands.

## 2. Goals

- Let an agent learn how expensive canonical project commands have actually been on the current project/toolchain/environment.
- Preserve failed, timed-out, and successful samples without survivorship bias.
- Record platform-supported child-process resource facts with explicit quality/completeness.
- Keep telemetry capture outside the command-admission hot path.
- Provide one stable `repro_id` that references exact known source/command/manifest/environment/result facts for a completed execution.
- Make capture gaps explicit so a capsule is never confused with a hermetic reproducibility guarantee.
- Survive output/derived-data retention changes without falsifying historical provenance.

## 3. Non-goals

S2 does not add:

- an execution scheduler;
- automatic test-profile selection;
- automatic timeout tuning;
- performance-regression diagnosis;
- resource-limit enforcement as a core requirement;
- a container/VM/environment snapshot;
- source-code bundling;
- automatic command rerun/replay;
- capture of secret environment values or arbitrary stdin contents;
- a claim of deterministic reproducibility.

## 4. Per-execution performance record

Each terminal operation may produce exactly one logical version-1 `performance_record` per telemetry derivation contract. It uses the common optional correlation envelope; repository/workspace identities are absent when the operation was legitimately executed outside a registered workspace.

Conceptually:

```text
derivation_key
operation_id
receipt_digest
repository_id?
workspace_id?
activity_id?
project_command_id?
command_semantics_fingerprint
parameter_binding_fingerprint?
source_content_digest?
source_scope_digest?
environment_fingerprint?
toolchain_fingerprint?
wall_ms
output_bytes
input_accepted_bytes
input_delivered_bytes
terminal_outcome
timed_out
captured_at
lifecycle/completeness
```

`derivation_key` includes the authoritative receipt identity plus telemetry producer/schema/config semantics. A crash after record publication but before index/event acknowledgement upserts the same logical sample. One execution can never appear twice in a histogram because finalization was retried.

Wall duration and byte counters derive from existing authoritative execution records where possible and therefore require no OS resource probe. Performance records never alter terminal receipts.

## 5. Resource observation record

When the active platform/runtime can observe resource metrics for the owned process tree, the performance record or a linked `resource_record` may include:

```text
cpu_user_ms
cpu_system_ms
max_rss_bytes
read_bytes
write_bytes
process_count_peak
```

Every metric carries a quality classification rather than inheriting one vague record-wide exactness:

```text
exact
platform_reported
sampled
unavailable
```

Examples:

- an OS-provided aggregate CPU time for the owned process group may be `platform_reported` or `exact` under the platform adapter contract;
- periodically sampled RSS cannot be called exact maximum RSS;
- unsupported I/O counters are `unavailable`, not zero.

The provider/adapter contract documents what a metric means on each supported OS.

## 6. Ownership boundary for resource facts

Resource accounting binds only to processes ShellBeam currently owns or can prove belong to its supervised process tree/generation. ShellBeam must not infer ownership by process names or `ps | grep` matching.

If daemon restart or supervisor loss creates an observation gap, the record states `partial`/`unavailable` for the affected metrics. Missing intervals are not synthesized.

Resource observation never expands the set of processes ShellBeam may signal or control.

## 7. Telemetry aggregation identity

Historical samples are never grouped solely by project command ID or human command text. Every compatible bucket includes an execution-semantics identity:

```text
repository_id? / explicit non-repository scope
command_semantics_fingerprint
command_definition_digest?       # when manifest-backed
parameter_binding_scope_fingerprint?
toolchain compatibility key
environment compatibility key
declared/effective scope class
platform/architecture when metric semantics require it
telemetry schema version
```

`project_command_id` is a display/correlation label only. If `test_full` changes resolved argv between manifest revisions, the changed command-definition/execution fingerprint creates a different history bucket even though the label is unchanged.

Parameterized commands use a bounded secret-safe parameter binding fingerprint or a versioned scope identity. Raw parameter values are not copied into telemetry indexes merely to establish a grouping key. Free-form values such as paths/test names cannot create an unbounded public index without quota enforcement.

Exact source content digest is retained per sample but is not normally part of the primary grouping key; summaries expose source heterogeneity instead of pretending every source revision was identical.

## 8. Compatibility keys

Toolchain/environment compatibility uses existing versioned fingerprint semantics. Samples produced under incompatible normalization schema versions are not merged.

Platform differences such as Linux versus macOS remain visible. Cross-platform aggregation is permitted only if a future explicit compatibility rule defines the compared metrics; MVP summaries should default to same-platform/same-architecture compatibility for resource metrics.

If history exists but cannot be safely aggregated under the requested key, inspection returns `telemetry_incompatible_history` or separate buckets rather than blending them.

## 9. Historical summary

An inspect response may expose a bounded aggregate such as:

```json
{
  "command_id": "test_full",
  "samples": 18,
  "window": {
    "first": "...",
    "last": "..."
  },
  "wall_ms": {
    "p50": 432000,
    "p95": 543000
  },
  "output_bytes": {
    "p50": 48123,
    "p95": 210991
  },
  "timeout_rate": 0.055,
  "outcome_counts": {
    "success": 15,
    "failure": 2,
    "timeout": 1
  }
}
```

Percentiles are computed deterministically under a versioned method. The summary reports distributions, not a future-duration prediction.

ShellBeam may expose latest-sample ratios/percentile positions mechanically, for example `latest_wall_ms / historical_p50`. It must not label the result `performance_regression=true` unless a future project policy explicitly defines that deterministic threshold.

## 10. Sample/key retention and compaction

Telemetry storage is bounded simultaneously by:

- total retained sample count and bytes;
- total distinct aggregation keys;
- per-repository distinct key count;
- recent samples per key;
- retained aggregate/histogram bytes;
- age.

The concrete limits are advertised in capability discovery/config and tested as hard ceilings. Eviction/aggregation is cohort-neutral: successful samples are not preferentially retained, failures/timeouts remain represented under the same retention policy, and outliers are not discarded merely for being outliers.

Older samples may compact into stable aggregate buckets only when declared semantics can be preserved. Deleting telemetry does not affect terminal receipts, operation idempotency, or evidence validity. A retained reproduction capsule may resolve a detailed telemetry reference as compacted while preserving its creation-time descriptor/tombstone.

## 11. Collection timing and performance budget

Telemetry does not slow ordinary admission:

- wall time and existing byte counters are captured from already-maintained execution facts;
- OS resource probes are enabled only when supported/cheap under the active capability contract;
- periodic sampling uses bounded frequency and work budgets;
- aggregation occurs outside spawn/poll hot paths;
- percentile/histogram summaries are cached or computed on explicit inspection, never on every output poll.

Resource-finalization work cannot delay terminal publication indefinitely. If optional resource capture cannot finish within its budget, terminal receipt remains authoritative and resource completeness is downgraded.

## 12. Experimental resource enforcement contract

Resource limits are distinct from observation. A future/experimental start request may conceptually declare:

```json
{
  "limits": {
    "memory_bytes": 4294967296,
    "processes": 256,
    "cpu_time_ms": 120000
  }
}
```

Normative rules:

- absence of a limit means observe-only/default execution behavior;
- unsupported limits fail with `resource_limit_unsupported`; they are never silently ignored;
- a platform advertises a limit only when the OS primitive can enforce the documented semantics;
- partial or approximate enforcement is explicitly different from hard enforcement;
- existing wall-clock timeout remains a separate execution policy;
- a breached resource limit becomes a typed termination cause while exit/signal evidence still reports what the OS actually observed.

For example:

```text
termination_cause = resource_limit
resource_limit_kind = memory
```

ShellBeam must not fabricate exit code 137 merely because a memory-control subsystem caused termination.

Resource enforcement is experimental and is not required for E23 core completion.

## 13. Reproduction capsule model

A `repro_id` identifies an immutable projection of captured conditions for one admitted execution **at one explicit capture consistency cut**. It is not a tarball, source snapshot, or guarantee that rerunning will reproduce the outcome.

Explicit materialization also carries a caller-stable `repro_create_id`. The create request is an exactly-once local mutation: retry of the same ID replays the durable creation receipt and exact original capsule even if additional derived observations become available later. A conflicting request fingerprint never creates another capsule under that ID.

Conceptually:

```text
repro_create_id
repro_id
schema_version
created_at
capture_cut_digest
execution:
  operation_id
  receipt_digest
  command_semantics_fingerprint
  project_command_id?
  parameter_binding_fingerprint?
  resolved_argv OR shell fingerprint
  execution_mode
source:
  repository_id?
  workspace_id?
  source_content_digest?
  vcs_state_digest?
  workspace_generation?
  quality
project:
  manifest_digest?
  verification_policy_digest?
environment:
  environment_fingerprint?
  toolchain_fingerprint?
results:
  creation_descriptors[]
capture:
  completeness_by_dimension
```

`capture_cut_digest` identifies the exact eligible source/ref states observed for this materialization, including whether dependent derivations were pending, terminal, unavailable, or absent. A later independent create request may produce a different `repro_id`/cut if richer terminal derivations are now available; it never mutates the earlier capsule.

## 14. Capture-completeness matrix

Every capsule reports independently what ShellBeam actually captured. Example:

```json
{
  "capture": {
    "source": "exact",
    "command": "exact",
    "toolchain": "exact",
    "environment": "partial",
    "filesystem_external": "unknown",
    "network_dependencies": "unknown",
    "external_services": "unknown",
    "time_randomness": "unknown"
  }
}
```

Permitted dimension states include versioned concepts such as `exact`, `complete`, `partial`, `unknown`, `unavailable`, and `not_applicable` as appropriate to the concrete dimension.

The UI/API must not collapse this matrix into a boolean `reproducible=true`.

## 15. Source and command provenance

Where exact source content digest is available, the capsule binds it exactly. Where only a fast generation or partial source identity exists, the capsule remains valid as a historical record but its source quality is lower.

Command identity includes the original authoritative execution binding:

- direct argv: exact resolved argv and execution mode subject to existing privacy/size rules;
- fixed shell mode: canonical command fingerprint plus allowed bounded command metadata;
- typed project command: command ID, manifest digest, validated params, and resolved argv.

Retry of an existing operation always points to the originally bound execution; a later manifest/workspace change does not mutate the old capsule.

## 16. Environment/toolchain provenance

Capsules reuse the existing secret-safe environment/toolchain fingerprint contract. They never embed raw environment values or secret hashes.

If the environment fingerprint only records selected variable presence and selected toolchain versions, the capsule reports other environment dimensions as unknown rather than implying complete process-environment capture.

## 17. Input provenance and privacy

Default capsules do not contain stdin bytes or hashes of arbitrary stdin content. They may record:

```text
accepted_input_bytes
delivered_input_bytes
input_complete
input_content_identity = unavailable
```

A future explicit non-secret input-artifact mechanism requires a separate design/allowlist and is not implied by S2.

Capsules also exclude:

- source file contents;
- raw environment values;
- credentials/tokens/private keys;
- home-directory snapshots;
- raw network payloads;
- E26 checkpoint payloads.

## 18. Immutable descriptors, resolution state, and compaction

A capsule freezes a bounded creation-time descriptor for every captured reference:

```text
ref_id
record_kind
producer/schema identity
digest or opaque immutable identity where privacy permits
bounded creation-time summary
original_availability
```

This descriptor is immutable. Inspection separately reports the current dynamic resolution state:

```text
resolution_state: available | compacted | purged | unavailable
```

Compaction never rewrites the creation-time descriptor to make history look different. Any detail eligible for compaction while a capsule remains retained must leave a tombstone sufficient to report original identity/producer/schema/summary and current resolution honestly.

Checkpoint private-content identities are never copied into repro descriptors. If raw output/structured/telemetry detail compacts, the capsule remains inspectable through the descriptor plus tombstone, but it never regenerates a different producer version's output and presents it as the original.

## 19. Capsule creation policy

MVP supports:

1. **Explicit exactly-once creation:** caller supplies `repro_create_id`, target operation/receipt, and a bounded capture policy. The first accepted request freezes one consistency cut and durable result. Lost-response retry replays it.
2. **Evidence-triggered creation:** a validated verification/evidence policy may derive a deterministic create-request identity from the authoritative evidence/receipt and policy version, then uses the same exactly-once materialization path.

Before freezing the cut, the service observes the current eligible derivation lifecycles. A capture policy may either accept pending dimensions as pending/partial or require selected derivations to reach a terminal bounded state. Waiting is always bounded; timeout becomes an explicit capture gap rather than an unbounded terminalization dependency.

A lightweight internal `repro-capable` index may identify operations that could be materialized later. It is not itself an immutable capsule and does not create records for every trivial command.

## 20. Capsule inspection

`local_shell.inspect target=repro` returns bounded facts including:

- exact command identity available under the capsule;
- source/workspace/VCS provenance;
- manifest/toolchain/environment fingerprints;
- terminal outcome and receipt reference;
- structured-result/diagnostic summary;
- artifact identities;
- performance/resource observations;
- capture gaps and compacted references.

Inspection is read-only and never automatically reruns the command.

A future `repro compare` capability, if added, may produce a purely mechanical difference set such as source changed/toolchain same/environment presence changed/artifact digest changed. It cannot declare root cause.

## 21. Telemetry and repro relationship

One terminal operation may flow as:

```text
command admitted
      ↓
process execution
      ↓
receipt + raw output
      ↓
terminal finalization
      ├── structured results
      ├── performance sample
      ├── resource observations
      └── evidence/artifacts
                ↓
        optional repro capsule
                ↓
          repro_recorded event
```

Telemetry aggregates across executions. Reproduction freezes captured references for one execution. Later aggregate changes never mutate historical capsules.

## 22. Evidence interaction

Performance history does not decide evidence validity or verification completeness.

Resource usage does not change a pass/fail result unless a future validated project verification policy explicitly defines a deterministic required resource condition; that future policy requires its own contract.

A repro capsule may reference evidence but is not itself proof that the referenced verification is current. Evidence retains its own result/source/freshness/artifact/policy dimensions.

Examples that are valid simultaneously:

```text
test evidence = pass/current
repro environment capture = partial
```

or:

```text
command outcome = failure
repro source/toolchain capture = exact
```

## 23. Stable failure/status additions

S2 introduces or reserves:

- `telemetry_unavailable`;
- `telemetry_partial`;
- `telemetry_budget_exceeded`;
- `telemetry_incompatible_history`;
- `resource_observation_unavailable`;
- `resource_observation_partial`;
- `resource_limit_unsupported`;
- `repro_not_found`;
- `repro_materialization_unavailable`;
- `repro_source_unavailable`;
- `repro_reference_compacted`.

These observation/derived states do not rewrite child outcome or MCP success semantics.

## 24. Platform capability reporting

Capability discovery exposes resource metric support independently, for example:

```text
resource_observation:
  cpu_time: supported
  max_rss: supported
  io_bytes: unavailable
  process_count_peak: sampled
```

macOS and Linux may legitimately advertise different quality levels. Unsupported metrics are not filled with zeros and native evidence is required for any platform-specific support claim.

Cross-build only proves compilation.

## 25. Persistence, retention, and migration

Telemetry/repro schemas are versioned derived state. Exact source/toolchain/environment fingerprint schema versions remain explicit so incompatible normalization cannot be merged silently.

Telemetry summaries may rebuild from retained uniquely keyed samples. Repro capsules are immutable historical projections and have independent hard count/age/metadata-byte quotas. Purge is explicit/retention-driven and never affects operation idempotency authority.

A capsule's creation descriptors/tombstones live at least as long as the capsule itself. Detail stores may compact sooner, but inspection must still distinguish original availability from current `resolution_state`. Migration may wrap/read old capsule schemas but never reinterpret unknown old fields as a stronger capture claim.

## 26. Security/privacy validation

Fixture-based tests must prove persisted/public telemetry/repro records do not contain:

- raw env values;
- known secret fixture strings;
- private-key/token material;
- stdin fixture contents;
- source file contents not already permitted by existing output behavior;
- E26 checkpoint private content-store contents.

External absolute paths are redacted/classified according to the existing workspace/privacy contract.

## 27. Validation strategy

### 27.1 Telemetry tests

- one terminal receipt produces one logical sample under one derivation key even across crash/recovery at record/index/event acknowledgement boundaries;
- changed command definition under the same project-command label creates a separate bucket;
- parameter binding fingerprints are stable/secret-safe and key cardinality remains within hard global/per-repo limits;
- incompatible toolchain/environment/platform/schema histories never blend silently;
- failure/timeout/outlier cohorts survive retention fairly;
- resource observation quality and restart gaps are never overclaimed.

### 27.2 Reproduction tests

- explicit `repro_create_id` lost-response retry replays the exact original `repro_id` and capture cut after later derivations complete;
- conflicting create fingerprint under the same ID cannot rematerialize;
- creation cut records pending/terminal/absent dependent derivations exactly;
- later independent creation may produce a richer different cut without mutating the old capsule;
- immutable creation descriptors remain unchanged while dynamic resolution moves available->compacted/purged;
- every compactable referenced family leaves a sufficient tombstone for retained capsules;
- capsule count/age/byte quotas are hard-bounded;
- partial environment/input/external-service capture never becomes a reproducibility guarantee;
- private checkpoint identities/raw secret values never surface.

### 27.3 Platform tests

Native macOS/Linux tests establish each advertised resource metric quality. Cross-build is compile-only evidence.

### 27.4 Experimental resource-enforcement tests

Where implemented, supported limits are proven under documented native semantics; unsupported limits are rejected explicitly and never silently ignored.

## 28. Core acceptance criteria

E23/E24 core are complete only when:

1. Exactly one logical telemetry sample is counted per compatible terminal execution/derivation despite crash recovery.
2. Aggregation always includes execution-semantic identity; project command labels cannot merge changed commands.
3. Total key/sample/aggregate storage is hard bounded and retention does not preferentially erase failures/timeouts.
4. Toolchain/environment/schema/platform incompatibility is never blended silently.
5. Resource metrics expose exact/platform-reported/sampled/unavailable quality honestly and gaps cannot be called complete.
6. Explicit repro creation is exactly-once under `repro_create_id` and freezes one consistency cut.
7. A later richer capture never mutates an existing capsule.
8. Capsule creation-time descriptors are immutable and current resolution state is separate.
9. Capsule inspection remains useful after eligible detail compaction through required tombstones and honest loss reporting.
10. Telemetry/repro failures cannot damage authoritative execution/idempotency/evidence state.
11. Ordinary command admission performs no telemetry aggregation/repro work when these capabilities are unused.
12. Experimental resource enforcement is not required for a core-complete claim.

## 29. Reference agent flow

```text
inspect project command history
  test_affected: p50 4.2s / p95 8.7s
  test_full:     p50 7m12s / p95 9m03s
agent chooses the next verification based on task/evidence
run command
terminal receipt + structured results + telemetry
repro create operation_id=...
inspect repro in a later conversation
  source exact
  toolchain exact
  environment partial
  diagnostics/artifact/performance references available
```

ShellBeam supplies empirical/provenance facts; the external reasoning agent makes the strategy and debugging decisions.
