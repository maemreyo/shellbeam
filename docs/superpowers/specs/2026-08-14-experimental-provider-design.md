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

## 4. Checkpoint creation request

Conceptually:

```json
{
  "action": "checkpoint_create",
  "workspace_id": "ws_...",
  "activity_id": "PI-756",
  "paths": [
    "internal/runtime/**",
    "tests/runtime/**"
  ]
}
```

Path scope is explicit and bounded. There is no `checkpoint_all_dirty=true` or implicit “snapshot whatever the command may mutate” mode.

Glob/pattern expansion uses a deterministic documented matcher, maximum pattern count, maximum expanded entries, byte/work budgets, and workspace-root confinement. If the requested scope cannot be captured completely under the contract, creation fails or reports the exact unsupported paths; it never silently truncates while claiming a complete checkpoint.

## 5. Checkpoint metadata

A checkpoint public/metadata record includes:

```text
checkpoint_id
provider_id/provider_version
workspace_id
activity_id?
source_generation
created_at
captured_path_count
excluded/unsupported path summaries
total_bytes
checkpoint_content_identity
capture_quality
retention state
```

Per-entry provider metadata preserves enough semantics to restore selected ordinary paths safely:

```text
path
kind: file | directory_marker | symlink | absent
content_digest?
size?
mode/executable_bit?
symlink_text?
```

Raw file bytes live only in the provider's private local content store/CAS.

## 6. Path and file-type rules

Checkpoint paths are normalized against the registered workspace.

- `..`/equivalent escape is rejected.
- Symlinks are captured by link text and are never followed outside the workspace for content capture.
- `.git` internal metadata and ShellBeam runtime/state paths are excluded by default.
- Sockets, devices, FIFOs, and unsupported special files are rejected/marked unsupported rather than byte-copied.
- Directory walking is bounded and does not follow escaping links.
- File size/total size limits are all-or-explicit-failure for requested complete capture.

## 7. Sensitive-content classification

Checkpoint storage is classified `local_sensitive_content`.

Provider storage must be user-only, local by default, excluded from ordinary package/export/repro flows, and never returned as raw content through MCP inspection. Core stores only bounded metadata/digests/references necessary to identify checkpoint state.

Known credential/private-key/runtime paths are excluded by policy, but ShellBeam cannot guarantee an arbitrary selected source file does not itself contain a secret. Documentation and capability discovery must state this limitation.

Checkpoint contents are never included in reproduction capsules.

## 8. Restore model: compare-and-swap, not overwrite

Restore is explicit and conflict-safe. The provider restores a path only when the caller/provider can establish the expected current state required by the restore request.

Conceptual restore:

```text
checkpoint_id
selected paths
expected-current identities
```

For each path:

1. observe current path identity under the same normalized semantics;
2. compare it to the expected-current precondition;
3. if it matches, apply the checkpoint preimage atomically where practical;
4. if it differs, return `checkpoint_restore_conflict` and leave that path unchanged.

No force-overwrite flag exists in the initial experimental contract.

## 9. Restore semantics for path states

- Checkpoint captured an existing file: restore may recreate that exact captured file/mode when precondition matches.
- Checkpoint captured a symlink: restore recreates link text without following it, subject to workspace confinement rules.
- Checkpoint captured `absent`: restore may remove a newly-created path only when exact expected-current identity matches.
- Directory deletion/restoration is allowed only when the provider can prove the selected directory state under its contract and no unexpected concurrent children would be destroyed.
- Unsupported/special paths remain conflicts/unsupported rather than best-effort mutation.

A multi-path restore returns per-path results. Global success is impossible when any requested path was not restored.

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

## 13. Trace observation classes

A provider may support independent classes such as:

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

Each class reports its own support/completeness/quality. Providers do not expose a single `complete=true` when some dependency channels are unsupported.

Network payload contents and environment values are never captured under this contract.

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

Capability discovery exposes tracing support per platform/provider, for example:

```json
{
  "input_tracing": {
    "provider_id": "linux-provider-v1",
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

A macOS provider may expose a materially different matrix. Core does not normalize differences away or claim feature parity it cannot prove.

## 18. Trace record identity and provenance

A trace record binds:

```text
provider_id
provider_version
provider capability schema version
operation_id
receipt_digest?
owned process/supervisor generation
repository_id
workspace_id
source_content_digest?
toolchain/environment fingerprint refs?
capture_start
capture_end
coverage matrix
observed normalized path/resource sets
write observations
truncation/budget state
```

Trace records use the common derived-record provenance envelope with `authority=advisory` unless a future separate hermetic contract explicitly promotes them.

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

## 21. Ownership loss and restart

A trace is valid only for the execution/process generation the provider can actually observe.

If process ownership/supervisor continuity is lost, provider attachment restarts, or the provider cannot cover a child process, affected observation classes downgrade to partial/unknown. Core never stitches gaps into an apparently complete trace without proof.

Tracing does not expand signal/control authority.

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
checkpoint private content store/CAS
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
- `checkpoint_scope_invalid`;
- `checkpoint_scope_too_large`;
- `checkpoint_path_unsupported`;
- `checkpoint_budget_exceeded`;
- `checkpoint_not_found`;
- `checkpoint_expired`;
- `checkpoint_restore_conflict`;
- `checkpoint_restore_partial`;
- `checkpoint_restore_failed`.

E27 reserves:

- `input_trace_provider_unavailable`;
- `input_trace_unsupported`;
- `input_trace_partial`;
- `input_trace_budget_exceeded`;
- `input_trace_ownership_lost`;
- `input_trace_not_found`.

These failures describe provider actions/observations and do not rewrite unrelated child outcomes.

## 26. Security requirements

### 26.1 Checkpoint provider

- private store permissions are user-only;
- no automatic export/upload path exists in core;
- raw checkpoint bytes never appear in ordinary inspect, event, receipt, evidence, telemetry, or repro records;
- `.git` internals/runtime/private policy paths are excluded by default;
- symlink traversal cannot escape the workspace;
- provider cleanup follows sensitive-content deletion rules and reports failures.

### 26.2 Trace provider

- no file contents;
- no environment values;
- no network payloads;
- bounded path exposure/redaction;
- provider attach never grants broader process control;
- unsupported observation channels are explicit.

## 27. E26 validation strategy

Experimental checkpoint provider tests must cover:

- explicit file snapshot/restore round trip;
- executable bit/mode preservation;
- symlink link-text behavior and escape rejection;
- absent-path capture and safe removal semantics;
- special-file rejection;
- total/path-size budget enforcement;
- concurrent external edit causing restore conflict;
- mixed multi-path restore with per-path success/conflict results;
- no force overwrite path;
- no Git index/HEAD/stash/config/identity changes;
- provider crash during create/restore preserving core receipt/idempotency state;
- retention expiration and refusal to restore incomplete content;
- proof raw captured content never appears in public records/repro.

## 28. E27 validation strategy

Experimental tracing provider tests must cover:

- binding to the exact owned process/supervisor generation;
- every advertised observation class and completeness quality;
- child-process coverage claims;
- observed undeclared dependency detection;
- budget truncation downgrading completeness;
- provider restart/ownership gap downgrading completeness;
- no file/env/network payload capture;
- external path redaction/classification;
- cross-platform capability differences;
- explicit proof that an advisory trace cannot narrow evidence validity.

Native platform evidence is mandatory for platform-specific tracing claims. Cross-build is compile-only evidence.

## 29. Experimental readiness criteria

### 29.1 E26 experimental-ready

An implementation may be called experimental-ready on a provider/platform only when:

1. Explicit bounded scope round-trips supported file/symlink/mode states.
2. Concurrent edits conflict instead of being overwritten.
3. Partial multi-path restore reports per-path truth.
4. Symlink escapes, special files, and oversized scopes fail closed.
5. No Git repository-control state is mutated.
6. Captured contents remain local/private and cannot surface through normal inspect/repro APIs.
7. Restore-induced file changes advance ordinary workspace generation and invalidate evidence normally.
8. Provider crash cannot corrupt authoritative execution/idempotency state.

### 29.2 E27 experimental-ready

An implementation may be called experimental-ready on a provider/platform only when:

1. Trace binds only to an execution tree/generation the provider can actually observe.
2. Supported/unsupported channels are explicit.
3. Budget truncation and restart gaps downgrade completeness.
4. Newly observed undeclared dependencies can be surfaced as advisory facts.
5. Trace alone can never keep narrow evidence current across source changes.
6. No file contents, environment values, or network payloads are captured.
7. Platform differences are exposed honestly.

Experimental-ready is not a core/production-complete claim.

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
restore selected path with expected-current identity
  unchanged since expected observation -> restored
  concurrently edited -> conflict, untouched
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
