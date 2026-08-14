# ShellBeam Experimental Safety and Input Observation Providers Design

## Status

Approved companion design for E26 Explicit Local Safety Checkpoint and E27 Dynamic Input Tracing. This spec extends the [Agent Execution Observation Roadmap](./2026-08-14-agent-execution-observation-roadmap-design.md).

Both capabilities are experimental/provider contracts. They are intentionally excluded from core E21-E25/E28 completion and release gates. This document freezes boundaries and failure semantics so later research cannot weaken core execution/evidence guarantees.

## 1. Decision

ShellBeam may integrate optional providers for explicit bounded local content checkpoints and best-effort execution input observation. Core ShellBeam owns capability discovery, request validation, identities, provenance, bounded metadata persistence, privacy/status presentation, and event integration. Providers own the sensitive/platform-specific snapshot, restore, and tracing mechanics.

Safety checkpoints are explicit local convenience primitives, not Git or automatic transactions. Input traces are advisory observations, not proof of complete dependencies unless a separately specified provider enforces a hermetic boundary.

## 2. Cross-cutting experimental invariants

1. Experimental provider absence never makes core execution incomplete.
2. Provider crash/failure cannot corrupt terminal receipts, operation idempotency, process ownership, or evidence authority.
3. Provider results declare capability, completeness, platform, and authority explicitly.
4. Sensitive checkpoint content stays local/provider-private and is never surfaced through ordinary inspect/repro records.
5. Best-effort trace absence is not proof of non-dependency.
6. No experimental provider silently gains stronger authority because it appears reliable in tests.

## 3. E26 purpose and non-goals

A safety checkpoint captures a caller-explicit bounded workspace path set before an experiment so selected content can later be compared or restored safely.

It is not:

- `git stash`, commit, reset, or branch management;
- an automatic checkpoint before every command;
- a full dirty-tree snapshot;
- a whole-workspace backup;
- an automatic rollback transaction;
- an authorization/lock mechanism;
- a guarantee that captured files contain no secrets.

## 4. Checkpoint creation request and idempotency

Checkpoint creation is an explicit exactly-once local mutation request:

```json
{
  "action": "checkpoint_create",
  "checkpoint_create_id": "chkcreate_...",
  "workspace_id": "ws_...",
  "activity_id": "PI-756",
  "paths": ["internal/runtime/**", "tests/runtime/**"]
}
```

The first accepted request durably binds its fingerprint before provider capture begins. Retry of the same `checkpoint_create_id` replays the durable result/checkpoint ID; conflicting scope/provider/options under the same ID return a typed conflict and never capture again.

Path scope is explicit and bounded. There is no `checkpoint_all_dirty=true` or implicit “snapshot whatever the command may mutate” mode. Pattern expansion has deterministic matcher/count/entry/byte/work ceilings. If complete requested capture cannot be established, creation reports exact unsupported paths/failure; it never truncates while calling the checkpoint complete.

## 5. Checkpoint metadata and private content identity

Public/core checkpoint metadata contains no deterministic content-derived hash of arbitrary captured bytes:

```text
checkpoint_id
checkpoint_create_id
provider_id/provider_version
workspace_id
activity_id?
source_generation
created_at
captured_path_count
excluded/unsupported path summaries
total_bytes
capture_quality
retention_state
opaque_entry_refs[]
```

Per-entry **provider-private** metadata preserves restore semantics:

```text
opaque_entry_ref
path
kind: file | directory_marker | symlink | absent
private_content_identity?   # never exposed through core/MCP
size?
mode/executable_bit?
symlink_text?               # only when policy permits public metadata; otherwise private
```

Raw bytes and content-addressed hashes, if the provider uses them internally, remain only in the provider's sensitive local store. Core/MCP/repro/event records use opaque random or locally keyed non-portable references. This prevents a public SHA/deterministic digest from becoming an offline dictionary oracle for low-entropy secrets embedded in captured files.

## 6. Path and file-type rules

Checkpoint paths are normalized against the registered workspace.

- `..`/equivalent escape is rejected.
- Symlinks are captured by link text and are never followed outside the workspace for content capture.
- `.git` internal metadata and ShellBeam runtime/state paths are excluded by default.
- Sockets, devices, FIFOs, and unsupported special files are rejected/marked unsupported rather than byte-copied.
- Directory walking is bounded and does not follow escaping links.
- File size/total size limits are all-or-explicit-failure for requested complete capture.

## 7. Sensitive-content classification

Checkpoint storage is `local_sensitive_content`. Provider storage is user-only, local by default, excluded from ordinary package/export/repro flows, and never returned as raw content or public deterministic content identity through MCP inspection.

Known credential/private-key/runtime paths are excluded by policy, but ShellBeam cannot prove arbitrary selected source files contain no secret. Therefore privacy is enforced by **containment**, not by pretending selection can classify all secrets:

- raw bytes/private content-store hashes never leave the provider boundary;
- core gets opaque entry references only;
- checkpoint contents/identities are absent from repro/telemetry/evidence/journal payloads;
- cleanup follows sensitive-content deletion rules;
- privacy tests include low-entropy secret fixtures and assert their raw value, ordinary hash, and dictionary-comparable deterministic identity are absent from public records.

## 8. Restore model: explicit conditional restore

The initial contract does **not** claim a generic filesystem conditional-restore primitive. Restore is an explicit exactly-once mutation with caller-stable `restore_id`:

```text
restore_id
checkpoint_id
selected paths
provider-established expected-current observations
```

Retry of the same `restore_id` replays the durable per-path outcome. A conflicting request fingerprint under the same ID never applies another mutation.

Providers advertise conflict-detection semantics by operation/path class, not as a vague boolean:

```text
conflict_detection:
  regular_file: best_effort | atomic_conditional_replace
  symlink: best_effort | atomic_conditional_replace | unsupported
  absent_to_file: best_effort | atomic_conditional_replace | unsupported
  directory_tree: best_effort | atomic_conditional_replace | unsupported
```

`best_effort` means the provider compares a pinned/no-follow observation immediately before mutation but cannot prove an arbitrary external writer cannot win a remaining OS race. It must not claim “all concurrent edits conflict”. `atomic_conditional_replace` may be advertised only when the native provider can prove comparison+mutation atomicity for the exact path class under its documented mechanics.

No force-overwrite flag exists in the initial experimental contract. Path traversal uses no-follow/pinned-directory mechanics where the platform provider supports them; capability reporting states the actual guarantee.

## 9. Restore semantics for path states and submodules

- Existing regular file: restore may recreate the captured file/mode only under the provider's advertised conditional-restore semantics.
- Symlink: restore recreates link text without following it, only if the provider supports that path class safely.
- Captured `absent`: restore may remove/replace a newly-created selected path only under its expected-current and advertised atomicity contract.
- Directory tree: no recursive destructive restore unless the provider can prove the declared tree precondition/atomicity; otherwise unsupported or best-effort with no stronger claim.
- Unsupported/special paths remain unsupported/conflicts rather than best-effort byte mutation.

Initial E26 explicitly **rejects checkpoint scope crossing a Git submodule boundary**. Correct submodule restore would require pinning gitlink, checked-out submodule identity, dirty recursive source state, and concurrent parent/submodule transitions; that semantic expansion is deferred.

A multi-path restore returns a durable per-path outcome. Global success is impossible when any requested path did not meet its requested provider guarantee.

## 10. Git interaction boundary

Checkpoint create/inspect/restore does not mutate:

```text
HEAD
branch
index
stash
worktree registration
Git config
Git identity
remotes
```

Restored filesystem changes are observed by the ordinary workspace generation mechanism. Existing evidence becomes current/stale under the same normal source/evidence rules; checkpoint restore never asserts that Git is clean or that previous evidence became valid again.

## 11. Checkpoint retention

Provider content and metadata use explicit bounded count/age/byte retention.

Checkpoint status distinguishes:

```text
available
partially_compacted
expired
```

Automatic eviction cannot race an active restore. An expired checkpoint cannot be restored best-effort from incomplete content. Eviction is allowed because checkpoints are safety convenience, not operation/idempotency authority.

## 12. E27 purpose and non-goals

Dynamic input tracing observes dependency-related activity actually seen during a specific execution. It is intended to help an agent investigate hidden inputs or weaknesses in declared affected selectors.

It is not:

- a proof that unobserved inputs are irrelevant;
- a sandbox;
- a build cache key by default;
- an automatic affected-test selector;
- a cross-platform uniform tracing guarantee;
- a replacement for declared/enforced hermetic inputs.

## 13. Trace request modes and observation classes

Tracing is opt-in and never part of an ordinary command merely because a provider exists:

```text
trace_mode = off | best_effort | required
```

### `off`

No tracing provider is started/attached and the ordinary execution fingerprint has no trace semantics.

### `best_effort`

Tracing may be unavailable/partial without preventing spawn **only when attachment is observationally non-invasive under the provider contract**. If the implementation uses a wrapper, preload, namespace change, startup ptrace hold, environment mutation, or another mechanism that can affect child behavior, the instrumentation identity is part of execution/environment provenance even when capture is best-effort.

### `required`

Required tracing is execution semantics. `trace_mode=required` and the requested provider/capability contract belong to the caller request fingerprint. The provider must establish coverage **before child exec/first instruction** and freeze its instrumentation fingerprint before spawn. If required instrumentation cannot be established within the explicit startup budget, the child does not spawn.

A provider may support independent observation classes:

```text
filesystem_reads
filesystem_metadata_queries
directory_enumerations
filesystem_writes
executed_binaries
loaded_libraries
environment_names_observed
network_attempts
```

Each class reports support/completeness/quality independently. Providers never expose one `complete=true` if any dependency channel required by the advertised completeness contract is unsupported or attached late. File contents, environment values, and network payloads are never captured under E27.

## 14. Trace authority

Default trace records are:

```text
authority = advisory
may_have_unobserved_dependencies = true
```

They produce `observed_input_scope`, never `proven_input_scope`.

A trace can inform the agent that an execution read or queried files/paths not represented by an existing selector. It cannot prove that a path absent from this execution's observed set is irrelevant to every future execution.

## 15. Why negative observation is insufficient

Unobserved dependencies may still arise from:

- code paths not exercised in the traced run;
- non-existent-path probes;
- directory membership/enumeration;
- environment state;
- clock/randomness;
- network/external services;
- kernel/platform behavior;
- dynamically loaded code/libraries;
- child processes outside tracer coverage;
- tracing gaps or provider restart;
- different input values selecting different branches.

Therefore trace absence never narrows evidence validity in E27.

## 16. Broadening-only interaction with evidence

Dynamic tracing may detect evidence-selector risk mechanically:

```text
declared affected selector includes A
trace observes additional dependency B
=> advisory: selector may be incomplete
```

The inverse is forbidden:

```text
trace did not observe C
=> C is irrelevant
```

Core evidence remains conservative exactly as specified by the Agent Execution Layer. Trace records cannot keep narrow evidence current across source changes.

## 17. Provider capability matrix

Capability discovery exposes tracing semantics per platform/provider, including whether coverage can be established pre-exec and whether instrumentation is behavior-affecting:

```json
{
  "input_tracing": {
    "provider_id": "linux-provider-v1",
    "maturity": "experimental",
    "pre_exec_coverage": true,
    "instrumentation_effect": "environment_affecting",
    "filesystem_reads": "complete_for_owned_tree",
    "file_metadata": "partial",
    "directory_enumeration": "supported",
    "environment": "unsupported",
    "network": "unsupported",
    "child_processes": "owned_tree",
    "platform": "linux",
    "authority": "advisory"
  }
}
```

`complete_for_owned_tree` is legal only when the provider establishes coverage before child execution and inherits/attaches across the complete owned child tree without an unreported gap. A late attach automatically downgrades affected classes to partial/unknown.

A macOS provider may expose materially different semantics. Core does not normalize differences away. Ordinary agent responses summarize useful facts/coverage; the detailed matrix is deep-inspection metadata, not mandatory workflow choreography.

## 18. Trace record identity, instrumentation, and provenance

A trace record binds:

```text
derivation_key
provider_id/provider_version/capability_schema
trace_mode
instrumentation_fingerprint
instrumentation_effect
operation_id
receipt_digest?
owned process/supervisor generation
repository_id?
workspace_id?
source_content_digest?
toolchain/environment fingerprint refs?
capture_start / capture_end
pre_exec_coverage_established
coverage matrix
observed normalized path/resource sets
write observations
truncation/budget state
```

Trace records use the common deterministic derived-record primitive with `authority=advisory` unless a future separately reviewed hermetic contract promotes them.

If instrumentation can affect execution semantics, its fingerprint/effect is also included in execution/environment provenance so traced and untraced evidence are not silently treated as comparable. Provider restart/reattach under a different instrumentation identity creates an explicit coverage gap, never a stitched complete record.

## 19. Path classification and privacy

Observed filesystem paths are classified as:

```text
repo_relative
workspace_external_redacted
system_classified
```

Core/model-facing output should prefer repo-relative identities. Arbitrary home-directory absolute paths are not dumped into model context merely because a tracer saw them.

Tracing captures no file contents. Environment observation records names at most, never values. Network observation, if a provider later supports it, defaults to bounded coarse endpoint metadata and never payload capture.

## 20. Trace work budgets

Every provider request has hard bounds such as:

```text
max raw events
max unique paths
max returned records
max retained bytes
max external-path records
max capture duration
```

If a budget is exceeded:

```text
trace_truncated = true
completeness = partial
```

A truncated trace can still be useful advisory data but is never called complete.

## 21. Ownership loss, late attach, and restart

A trace is valid only for the process generation and time interval the provider can actually observe.

For any class advertised `complete_for_owned_tree`, the provider must prove pre-exec coverage and complete inheritance/attachment across all owned descendants. A missed first-exec interval, uncovered child, provider restart, supervisor continuity loss, or attachment failure downgrades the affected class to partial/unknown automatically.

Core never stitches gaps into apparent completeness. Required tracing that cannot establish the initial coverage boundary fails before child spawn; a failure after spawn becomes a trace/provider observation failure and does not fabricate command failure unless instrumentation itself independently caused an authoritative execution failure.

Tracing never expands which PIDs/PGIDs ShellBeam is permitted to signal/control.

## 22. Promotion path to evidence authority

S4 freezes the only legitimate future path from observed to proven inputs.

A future provider may advertise a `hermetic_boundary` only when it **enforces**, rather than merely observes, every dependency channel claimed by that versioned contract. A credible contract may require facts such as:

```text
filesystem namespace constrained
undeclared filesystem access denied
network disabled or fully declared
environment allowlisted/fixed
toolchain identity fixed
time/randomness policy declared
owned child tree fully enclosed
```

Only then may a separate future schema produce:

```text
proven_input_scope
authority = authoritative
```

Until all required hermetic conditions are established, E27 remains `observed_input_scope`/advisory. There is no `probably_authoritative` intermediate state.

Promotion requires a new reviewed design; implementing a more accurate tracer is not sufficient by itself.

## 23. Core/provider responsibility split

Core ShellBeam owns:

```text
provider discovery and maturity reporting
capability/version negotiation
request validation
checkpoint/trace IDs
common provenance envelope
bounded metadata/index references
typed failures/status
journal event integration
privacy/status presentation
```

Providers own:

```text
checkpoint private content store
snapshot mechanics
restore mechanics
OS tracing mechanisms
platform-specific low-level hooks
```

Provider-specific logic must not be embedded into the canonical operation state machine except for optional attachment/status boundaries. Provider failure cannot make terminal publication ambiguous unless the authoritative process execution itself is independently ambiguous.

## 24. Event Journal integration

Experimental providers may publish small bounded change-feed events after accepted provider transitions:

```text
checkpoint_created
checkpoint_restore_started
checkpoint_restore_completed
checkpoint_expired
input_trace_recorded
input_trace_truncated
```

These are journal events and inherit E21 semantics: they are not truth. Checkpoint metadata/provider state and ordinary workspace generation remain the records used to establish current facts.

A restore that changes files produces an ordinary workspace-generation change under the existing workspace observer.

## 25. Stable failures/status additions

E26 reserves:

- `checkpoint_provider_unavailable`;
- `checkpoint_create_conflict`;
- `checkpoint_scope_invalid`;
- `checkpoint_scope_too_large`;
- `checkpoint_path_unsupported`;
- `checkpoint_submodule_boundary_unsupported`;
- `checkpoint_budget_exceeded`;
- `checkpoint_not_found`;
- `checkpoint_expired`;
- `checkpoint_restore_request_conflict`;
- `checkpoint_restore_conflict`;
- `checkpoint_restore_partial`;
- `checkpoint_restore_failed`.

E27 reserves:

- `input_trace_provider_unavailable`;
- `input_trace_required_unavailable`;
- `input_trace_startup_budget_exceeded`;
- `input_trace_unsupported`;
- `input_trace_partial`;
- `input_trace_budget_exceeded`;
- `input_trace_late_attach`;
- `input_trace_ownership_lost`;
- `input_trace_not_found`.

These failures describe provider actions/observations. Required pre-exec trace setup can prevent spawn by contract; later provider observation failures do not rewrite an unrelated already-observed child outcome.

## 26. Security and ergonomics requirements

### 26.1 Checkpoint provider

- private store permissions are user-only;
- no automatic export/upload path exists in core;
- raw checkpoint bytes and deterministic provider-private content hashes never appear in ordinary inspect, event, receipt, evidence, telemetry, or repro records;
- public entry identity is opaque/non-dictionary-comparable;
- `.git` internals/runtime/private policy paths are excluded by default;
- initial scope rejects submodule-boundary crossing;
- path traversal uses provider-declared no-follow/pinned-directory mechanics and does not overclaim generic filesystem atomicity;
- provider cleanup follows sensitive-content deletion rules and reports failures.

### 26.2 Trace provider

- no file contents, environment values, or network payloads;
- bounded path exposure/redaction;
- provider attach never grants broader process-control authority;
- unsupported/late observation channels are explicit;
- behavior-affecting instrumentation is represented in execution/environment provenance.

### 26.3 Agent ergonomics

The normal agent never supplies checkpoint hashes/preimage identities, reasons about native atomicity primitives, or manually interprets a complete trace matrix. Checkpoint create/restore and trace opt-in are one model-oriented request each; the provider establishes internal preconditions and returns bounded restored/conflicted or observed/coverage summaries. Deep provider mechanics are available only through explicit inspection/debugging.

## 27. E26 validation strategy

Experimental checkpoint tests must cover:

- `checkpoint_create_id` lost-response retry returning the same durable checkpoint/result without recapture;
- conflicting create fingerprint under the same ID never capturing again;
- `restore_id` replay returning identical durable per-path outcomes after partial success/crash;
- conflicting restore request under the same ID never applying more mutations;
- file/mode/symlink/absent-path semantics under each advertised path-class guarantee;
- `best_effort` provider tests prove it does **not** claim universal concurrent-edit conflict detection;
- any `atomic_conditional_replace` claim has native race stress evidence for the exact advertised path class;
- symlink-parent/race/no-follow behavior under native macOS/Linux tests;
- initial submodule-boundary rejection;
- special-file and size/scope budget failure;
- no Git HEAD/index/stash/config/identity mutation;
- provider crash preserving core receipt/idempotency authority;
- retention expiration refusing incomplete restore;
- low-entropy secret fixture proving raw value, ordinary deterministic content hash, and provider-private content-store identity never surface through public records/repro.

## 28. E27 validation strategy

Experimental tracing tests must cover:

- `trace_mode=off` starts no tracer/provider work;
- `required` is part of request fingerprint and provider-unavailable/startup-timeout prevents child spawn;
- every completeness claim begins coverage before child exec/first instruction;
- child-process inheritance/coverage claims under native stress tests;
- deliberate late attach/restart/ownership gap downgrades affected classes;
- behavior-affecting instrumentation changes execution/environment provenance and cannot silently compare with untraced evidence;
- non-invasive best-effort attachment failure does not block spawn;
- observed undeclared dependency broadens selector suspicion but never narrows evidence;
- budget truncation downgrades completeness;
- no file/env/network payload capture;
- external path redaction/classification;
- cross-platform capability differences remain explicit;
- ordinary agent response does not require detailed provider coverage-matrix choreography.

## 29. Experimental readiness criteria

### 29.1 E26 experimental-ready

A provider/platform may be called experimental-ready only when:

1. Exactly-once create/restore request replay is durable under lost responses/crash.
2. Explicit bounded scope round-trips supported path states.
3. Public metadata exposes no deterministic content identity for arbitrary captured bytes.
4. The provider reports conditional-restore guarantees by path class; `best_effort` is never described as universal concurrency safety.
5. Any advertised `atomic_conditional_replace` class passes native race/stress proof.
6. Partial multi-path restore reports durable per-path truth.
7. Symlink/special/submodule/oversized unsupported cases fail closed under the documented contract.
8. No Git repository-control state is mutated.
9. Captured contents remain local/private and cannot surface through normal inspect/repro APIs.
10. Restore-induced source changes advance ordinary workspace generation/evidence validity normally.

### 29.2 E27 experimental-ready

A provider/platform may be called experimental-ready only when:

1. Required tracing establishes instrumentation before child execution or fails before spawn.
2. `complete_for_owned_tree` is proven across the full owned process tree from the first execution interval.
3. Provider/instrumentation identity and behavior effects are captured in provenance.
4. Supported/unsupported/late channels are explicit and budget/restart gaps downgrade completeness.
5. Trace can identify observed undeclared dependencies but cannot narrow evidence validity.
6. No file/env/network payload contents are captured.
7. Cross-platform capability differences are surfaced rather than normalized away.
8. Provider failure cannot corrupt authoritative operation/idempotency state.

## 30. Explicit non-goals

S4 does not create:

```text
automatic undo
automatic transaction around shell commands
Git replacement
workspace locking
sandbox guarantee
container runtime
build cache
remote execution
hermeticity by observation
automatic affected-test selector
```

E26 remains an explicit local safety primitive. E27 remains an observation provider. Stronger authority requires a future design backed by enforcement rather than confidence.

## 31. Reference checkpoint flow

```text
agent selects two bounded source/test path groups
checkpoint create -> chk_...
agent performs explicit refactor experiment
workspace generation changes
agent inspects checkpoint diff/current state
restore selected path under provider-established expected-current observation
  mismatch proven -> conflict, untouched
  match + best_effort provider -> restore with explicitly limited race guarantee
  match + atomic_conditional_replace provider -> restore under advertised native guarantee
workspace observer records resulting generation
```

## 32. Reference trace flow

```text
agent starts a selected test with experimental tracing enabled
provider observes owned process tree under bounded capability matrix
trace reports files/directories actually observed + completeness
agent compares trace against declared affected selector
additional dependency found -> advisory selector-risk finding
unobserved paths are not declared irrelevant
```

These flows provide useful experimentation/debugging primitives without changing ShellBeam's core role or correctness authority.
