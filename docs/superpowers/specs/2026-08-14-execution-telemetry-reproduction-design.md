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

Each terminal operation may produce a version-1 `performance_record` using the common derived-record provenance envelope. Core fields are conceptually:

```text
record_id
operation_id
receipt_digest
repository_id
workspace_id
activity_id?
project_command_id?
command_fingerprint
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
completeness
```

Wall duration and byte counters derive from existing authoritative execution records where possible and can therefore be captured without OS resource probing.

Performance records do not alter terminal receipts and may be compacted independently according to derived-data retention rules.

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

Historical samples are not grouped solely by human command text. A compatible aggregation key includes at least:

```text
repository_id
project_command_id OR canonical command fingerprint
toolchain compatibility key
environment compatibility key
declared/effective scope class
telemetry schema version
```

Where a project command is parameterized, the aggregation contract additionally distinguishes parameter values or a versioned parameter-scope identity when different values materially change work. The initial implementation should prefer exact parameter identity over clever normalization.

Exact source content digest is retained per sample but is not part of the primary grouping key, because requiring identical source content would make empirical history nearly useless. The summary exposes sample heterogeneity rather than pretending all sources were identical.

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

## 10. Sample retention and compaction

Telemetry history is bounded by recent-sample count, age, and bytes. Older samples may be compacted into stable histogram/summary buckets when the implementation can preserve the declared aggregate semantics.

Retention must not introduce survivorship bias:

- successful samples are not preferentially retained over failures;
- failures/timeouts are counted until the same retention policy removes/aggregates their cohort;
- outliers are not discarded merely for being outliers.

Deleting or compacting telemetry does not affect terminal receipts, operation idempotency, evidence validity, or repro records except that a repro reference may report the linked detailed telemetry record as compacted.

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

A `repro_id` identifies an immutable projection of captured conditions for one admitted execution. It is not a tarball or content snapshot.

Conceptually:

```text
repro_id
schema_version
created_at
execution:
  operation_id
  receipt_digest
  command_fingerprint
  project_command_id?
  parameter_binding?
  resolved_argv OR shell fingerprint
  execution_mode
source:
  repository_id
  workspace_id
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
  output_ref?
  structured_result_refs[]
  evidence_refs[]
  artifact_observation_refs[]
  performance_record_ref?
  resource_record_ref?
capture:
  completeness_by_dimension
```

A capsule references existing records/digests instead of duplicating large content.

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

## 18. Result references and compaction

Capsules keep stable references/summaries when detailed result data later compacts.

If raw output has been compacted, inspection may report:

```text
output_ref:
  state = compacted
  digest = ...
  bytes = ...
```

If structured-result details have compacted, the capsule retains the recorded reference/state and available bounded summary. It does not regenerate a different parser version's result and present it as the original execution record.

Immutable receipt/evidence digests remain authoritative according to their own retention contract.

## 19. Capsule creation policy

MVP supports:

1. **Explicit creation:** user/agent requests a capsule for a known operation/receipt.
2. **Evidence-triggered creation:** a validated project verification/evidence policy requests the reproduction projection for a meaningful verification execution.

ShellBeam may maintain an internal lightweight `repro-capable` index for terminal operations with sufficient provenance, but it should not materialize a full capsule record for every trivial command such as `pwd`.

Automatic materialization for every operation is deferred unless real usage proves it worthwhile.

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

## 25. Persistence and migration

Telemetry/repro schemas are versioned derived state. Exact source/toolchain/environment fingerprint schema versions are included so incompatible normalization is detectable.

Telemetry summaries may rebuild from retained samples. Repro capsules are immutable historical projections: migrations may wrap/read old schema versions but do not reinterpret unknown old fields as newer stronger capture semantics.

A cleanup operation may compact/remove telemetry detail according to retention, but it must not make an old operation runnable again or erase receipt/idempotency authority.

## 26. Security/privacy validation

Fixture-based tests must prove persisted/public telemetry/repro records do not contain:

- raw env values;
- known secret fixture strings;
- private-key/token material;
- stdin fixture contents;
- source file contents not already permitted by existing output behavior;
- E26 checkpoint CAS contents.

External absolute paths are redacted/classified according to the existing workspace/privacy contract.

## 27. Validation strategy

### 27.1 Telemetry tests

- successful, failed, killed, and timeout samples all contribute correctly;
- compatible aggregation keys merge and incompatible keys remain separate;
- exact parameter bindings are distinguished when applicable;
- deterministic percentile/histogram goldens;
- retention does not preferentially remove failures/outliers;
- unsupported metrics are unavailable rather than zero;
- sampled metrics never report exact quality;
- daemon/supervisor observation gaps downgrade completeness;
- telemetry failure cannot alter terminal receipt.

### 27.2 Reproduction tests

- exact-source capsule binds the exact terminal operation and receipt;
- partial source/environment dimensions remain partial/unknown;
- output and structured-result compaction preserves honest references;
- manifest/typed-command retry changes do not mutate old capsule bindings;
- capsule creation contains no raw secrets/input/source payloads;
- explicit creation for a missing/ambiguous source record reports exact gaps;
- repro inspection never triggers execution.

### 27.3 Platform tests

Native Linux and macOS runs validate every advertised resource metric/quality claim. Cross-compilation is labeled compile-only.

### 27.4 Experimental resource-enforcement tests

Where a provider is implemented, prove supported limits are actually enforced under the documented semantics; unsupported limits fail before spawn or at declared validation boundary and are never silently ignored.

## 28. Core acceptance criteria

E23/E24 core are complete only when:

1. Two compatible executions of a canonical project command produce inspectable historical distributions.
2. Toolchain/environment/schema incompatibility is never blended silently.
3. Failure/timeout samples remain represented in history.
4. Resource metrics expose exact/platform-reported/sampled/unavailable quality honestly.
5. Resource-observation gaps cannot be presented as complete.
6. A terminal operation can produce a reproduction capsule referencing its receipt/source/manifest/environment/result records.
7. Capsule capture gaps are explicit and no boolean reproducibility guarantee exists.
8. Capsule inspection remains useful after eligible raw/derived detail compaction and reports what was lost.
9. Telemetry/repro write failure cannot damage authoritative execution/idempotency/evidence state.
10. Ordinary command admission does not materially regress when telemetry/repro inspection is unused.
11. Experimental resource enforcement is not required for a core-complete claim.

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
