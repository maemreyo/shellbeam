# ShellBeam Engineering State, Mutation Transactions, and Authority Envelopes Design

Date: 2026-08-18
Status: companion architecture semantics frozen; implementation remains sequenced after P1
Scope: model-facing engineering-state projections, mechanical scope/focus separation, source-location ergonomics, mutation transaction semantics, stale-evidence invalidation, authority/resource envelopes, and provider integration boundaries

## 1. Decision

ShellBeam SHALL add a model-facing **EngineeringStateView** as a recomputable projection over existing execution/source/evidence/policy authorities.

ShellBeam SHALL NOT make `Task` or `Change` a new canonical aggregate root in the first implementation.

ShellBeam SHOULD add first-class **Mutation Transaction semantics** while delegating actual text/source transformation to qualified providers.

ShellBeam SHALL distinguish declared, enforced, and observed authority/resource facts so a requested restriction is never misreported as an enforced or observed guarantee.

## 2. Relationship to the Machine Truth Harness

This design implements the connective layer described in:

- [Machine Truth Harness Architecture](./2026-08-18-machine-truth-harness-architecture-design.md)
- [Affected Surface, Verification Obligations, and Evidence Sufficiency](./2026-08-18-affected-surface-verification-evidence-sufficiency-design.md)

The key boundary remains:

```text
model decides what matters and what solution to attempt
ShellBeam provides attributable engineering state and trustworthy mutation/evidence semantics
```

## 3. EngineeringStateView

### 3.1 Purpose

The view exists to stop the model from repeatedly reconstructing engineering state by joining many separate low-level calls in its context window.

It is a projection, not a new truth store.

Conceptual shape:

```text
EngineeringStateView
  schema_version
  workspace
  activity?
  selected_baseline
  current_source
  observed_changes
  mechanical_scope
  focus_hints
  affected_surface
  diagnostics
  verification
  evidence_summary
  environment_summary
  resource_summary
  authority_summary
  provider_summary
  retention/freshness/truncation metadata
  deep_refs[]
```

Every field is derived from existing or new authoritative/mechanical/advisory sources.

### 3.2 Query identity

Initial inspection SHOULD be addressable by:

```text
workspace_id
activity_id?
base_generation? / baseline selector?
```

The first implementation does not require a durable `change_id`.

A later envelope handle may pin a baseline for resume/handoff, but the view contents remain recomputable.

### 3.3 Selected baseline

The caller may select a baseline that is mechanically resolvable under a closed contract, for example:

```text
workspace clean/base generation
explicit source generation
repository base ref resolved to an exact source identity
checkpoint source identity
future envelope baseline
```

If exact resolution is unavailable, the view reports the identity/quality explicitly.

ShellBeam MUST NOT infer a baseline from conversation intent such as "the start of my task" unless a caller-provided correlation handle already binds that baseline.

## 4. Mechanical scope vs caller focus

### 4.1 Mechanical scope

Mechanical scope contains subjects/relations ShellBeam can derive from attributable facts.

Possible sources:

- observed source mutations;
- source generation transitions;
- code-intelligence provider relations;
- repository/package graph;
- explicit project mappings;
- policy mappings;
- config bindings;
- qualified runtime observations.

Every relation preserves basis, authority, coverage, generation, and provenance from the verification design.

### 4.2 Caller focus hints

A reasoning model MAY provide bounded focus hints such as:

```text
paths
symbols
packages
project commands
user journeys
freeform opaque hint labels only when explicitly declared as caller judgment
```

These hints are never promoted into mechanical affected facts merely because they appear repeatedly.

A view SHOULD present:

```text
mechanical_scope:
  authority = mechanical/advisory per relation

focus_hints:
  authority = caller_declared
```

rather than one ambiguous `working_set` list.

### 4.3 No focus memory inference

ShellBeam MUST NOT learn or infer a persistent "likely working set" from model behavior, previous conversation, file-open frequency, or textual similarity inside the daemon.

A caller may persist its own hints through an explicit future substrate, but the authority remains caller-declared.

## 5. Source addressing and model ergonomics

Existing `SourceRef` remains canonical identity. The model-facing layer SHOULD improve display ergonomics without weakening identity.

A resolved display form may expose:

```text
path: internal/operation/resource.go
lines: 42-57
symbol: operation.ResourceLimitKind?
relation: definition
source_ref_id: opaque exact identity
source_generation: gen_...
```

Rules:

- path/line/symbol are presentation/addressing aids;
- `source_ref_id` or equivalent exact source identity remains canonical for immutable binding;
- line/column coordinates are always defined relative to the exact bound source bytes;
- an old source ref is never rebound to current bytes after mutation;
- model-friendly locations carry resolution quality and source freshness;
- byte offsets may remain available through deep inspection but SHOULD NOT be the only normal model-facing coordinate.

This is an ergonomic evolution of Structured Code Intelligence, not a new editor source model.

## 6. Diagnostics in engineering state

EngineeringStateView MAY surface bounded diagnostics from:

```text
structured command results
language-semantic providers
policy/evidence projections
provider lifecycle failures
```

Each diagnostic MUST retain its authority/source class.

The view SHALL NOT convert diagnostics into fix recommendations.

Example:

```text
diagnostic:
  message: undefined: operation.ResourceLimitKind
  source: semantic_provider
  authority: mechanical
  source_generation: gen_X
```

not:

```text
recommended_fix: add enum to operation package
```

## 7. Verification projection

EngineeringStateView consumes the verification subsystem as a projection.

It may expose:

```text
effective/proposed policy digest(s)
phase
mandatory obligation counts
obligation-disposition breakdown
required_now/deferred/not_triggered/waived
evidence-status breakdown
satisfied/failed/insufficient/inconsistent/unknown/unavailable/not_evaluated
gate_status: clear | blocked | indeterminate
stale/inconsistent evidence
estimated/observed cost summary
```

It MUST link to underlying obligation/evidence references rather than copying large evidence payloads.

ShellBeam does not expose `task_complete=true`.

The strongest completion-like statement is a gate statement with a literal breakdown, for example:

```text
verification gate for policy P, phase H, generation G is clear:
  evidence_satisfied = 3
  waived = 1
  blocking = 0
  indeterminate = 0
```

A waived obligation is never counted as evidence-satisfied. If every mandatory obligation has satisfied evidence and none is waived, the view may say that all mandatory obligations have satisfied evidence; otherwise it MUST preserve the distinction.

## 8. Mutation transaction purpose

ShellBeam currently can execute arbitrary editing mechanisms through the shell and can observe source transitions, checkpoints, mutation scopes, and code intelligence. The missing capability is a first-class contract that binds:

```text
what source identity was expected before mutation
what authority/scope was declared
what provider was used
what effect was actually observed
what source identity exists after mutation
which prior evidence became stale
```

This is a **Mutation Transaction**, not a text editor.

## 9. MutationTransaction

Conceptual shape:

```text
MutationTransaction
  mutation_id
  activity_id?
  workspace_id
  baseline_source_generation
  preconditions
  declared_authority
  declared_scope_refs[]
  checkpoint_ref?
  provider_request_ref
  provider_identity/version
  provider_result_ref
  observed_effect
  post_source_generation
  attribution_quality
  lineage_refs[]
  evidence_invalidation_summary
  terminal_state
```

`mutation_id` is retry/idempotency identity for the transaction under its eventual API contract.

The actual schema/version and public action names are deferred to implementation planning.

## 10. Preconditions

A mutation transaction SHOULD be able to require:

```text
expected source generation
expected exact SourceRef/digest for edited subjects
workspace identity
allowed path/scope set
no conflicting hard authority condition
optional checkpoint created
provider qualification/capability
```

If a precondition is not met, the mutation fails before applying an effect where the provider contract allows atomic rejection.

The transaction MUST NOT silently rebase a patch or reinterpret an old source reference against new bytes merely to increase success rate.

## 11. Mutation authority and Advisory Mutation Scopes

Existing advisory mutation scopes remain advisory caller declarations and SHALL NOT automatically become permissions.

A mutation transaction may reference them as coordination metadata.

Future hard mutation authority, if added, MUST be separately modeled from advisory scopes.

The view should distinguish:

```text
advisory declaration
hard policy permission
provider capability
actual observed mutation
```

No scope overlap automatically denies a mutation unless a future explicit hard-policy contract says so.

## 12. Provider owns edit mechanism; ShellBeam owns transaction semantics

ShellBeam should not implement a universal source editor/refactoring engine.

Possible providers:

```text
apply_patch executable/adapter
LSP workspace edit
language refactor provider
structured file transform
bounded script provider
Git operation provider where appropriate
```

Provider responsibilities may include:

```text
applying the requested transform
reporting provider-native diagnostics
returning affected paths/ranges when reliable
handling provider-specific conflict semantics
```

ShellBeam responsibilities:

```text
identity/preconditions
authority
checkpoint/protection
provider qualification/lifecycle
actual source observation
source generation transition
attribution quality
lineage
evidence invalidation
receipt/effect honesty
```

## 13. Effect attribution

The architecture distinguishes at least:

```text
observed_after_operation
mutation_transaction_effect
```

An arbitrary shell operation followed by a changed workspace only proves a temporal/source transition under existing provenance semantics. Concurrent external writers may make exact causal attribution impossible.

A qualified Mutation Transaction can provide stronger attribution because:

```text
baseline G1
provider request P
bounded authority/scope
provider terminal result
post observation G2
```

are one coordinated contract.

Even then, attribution strength depends on the enforcement/hermetic boundary.

Conceptual values may include:

```text
exact_transaction_effect
strong_bounded
observed_correlated
ambiguous
unknown
```

The implementation MUST NOT call attribution exact when external writers could have changed the same surface inside the transaction window without detection/exclusion.

## 14. Observed effect

The transaction SHOULD materialize a bounded effect summary from authoritative source observation:

```text
paths created/modified/deleted/renamed when mechanically known
changed ranges when exact derivation exists
source refs before/after
artifact/source generation transition
provider-reported effect separately labeled
```

Provider-reported changed ranges are not automatically authoritative if ShellBeam did not bind them to exact source representations.

## 15. Evidence invalidation

After a committed source mutation, the system SHOULD mechanically recompute current evidence validity using existing Evidence Ledger semantics plus the new policy/affected-surface binding.

Rules:

- evidence bound to an older incompatible source generation cannot satisfy current-generation obligations;
- the immutable evidence record remains retained under normal retention policy;
- stale evidence is never rewritten into failure merely because source changed;
- policy may allow evidence whose source scope is proven unaffected under an exact affected-set authority;
- partial/unknown affected analysis cannot be used to preserve old evidence conservatively; uncertainty widens invalidation/verification rather than narrowing it;
- a transaction may expose `evidence_invalidation_summary` as a projection, not duplicate evidence truth.

## 16. Checkpoint integration

A mutation transaction MAY request/create an existing Safety Checkpoint when the caller/policy requires it.

Checkpoint semantics remain those of the checkpoint subsystem.

The transaction only records:

```text
checkpoint requested/required?
checkpoint_id
checkpoint creation result
restore compatibility/provenance if later used
```

Failure to create a required checkpoint blocks the mutation transaction before provider application.

Optional checkpoint failure is reported explicitly and handled according to policy; it is not silently ignored.

## 17. Transaction terminal states

A future closed state model SHOULD distinguish at least:

```text
rejected_precondition
provider_failed
applied_observed
applied_ambiguous
no_effect
incomplete
```

Canonical child/provider receipts remain authority for process outcome.

Mutation terminal state is authority only for the mutation contract, not for functional correctness.

## 18. Authority Envelope

### 18.1 Purpose

ShellBeam needs a generic envelope that prevents a declared restriction from masquerading as an enforced or observed fact.

For each relevant dimension:

```text
AuthorityDimension
  requested/declared
  enforcement
  observation
  provenance
  quality/completeness
```

### 18.2 Example dimensions

Possible dimensions include:

```text
filesystem read/write scope
network access
process descendants
process lifetime
CPU budget
memory budget
I/O budget
wall-time timeout
port/listener authority
environment access
provider-specific external effects
```

The exact initial supported set should reuse Resource Enforcement/Hermetic Boundary work rather than invent a parallel schema.

### 18.3 Declared

What the caller/policy requested.

Example:

```text
network = denied
memory <= 2 GiB
write paths = internal/foo/**
```

Declaration alone is not a guarantee.

### 18.4 Enforced

What ShellBeam/platform/provider actually enforced.

Example:

```text
memory = Darwin provider hard limit
network = unsupported
filesystem write confinement = advisory only
```

### 18.5 Observed

What ShellBeam actually observed under the observation contract.

Example:

```text
peak memory = 847 MiB, platform_reported
network attempts = unsupported/unknown
process peak = 7
```

## 19. Authority honesty examples

Correct:

```text
network
  declared: denied
  enforced: unsupported
  observed: unknown
```

Incorrect:

```text
network: denied
```

Correct:

```text
memory
  declared: <= 2GiB
  enforced: native_hard_limit
  observed_peak: 847MiB platform_reported
```

Correct when observation is missing:

```text
memory
  declared: <= 2GiB
  enforced: native_hard_limit
  observed_peak: unavailable
```

## 20. Resource Governor boundary

The Resource Governor should consume verified resource/provider constraints and current host observations.

It may decide actual concurrency/admission quantities within declared policy.

It SHALL NOT decide semantic verification sufficiency.

The split is:

```text
Verification policy:
  these evidence providers are admissible
  these may/may not run concurrently semantically

Resource Governor:
  current machine can admit N of these now
```

## 21. Provider lifecycle and quiescence

All long-running providers introduced under this architecture SHOULD integrate with existing process/session ownership instead of inventing side channels.

A provider execution is verification-complete only when:

```text
provider result/evidence finalized
and
actual live resources - declared transferred resources = 0
```

Persistent provider infrastructure (for example a language server cache/process) is allowed only when its ownership/lifecycle is explicitly provider-managed and not attributed as a leaked child of one verification operation.

## 22. Browser provider integration boundary

A future Playwright provider SHOULD expose ShellBeam-normalized facts/effects such as:

```text
browser/session identity
page/navigation operations
DOM/ARIA snapshot references
screenshots/media artifacts
download artifacts
console/network observations where supported
resource/process tree
source/build correlation supplied by caller/project
verification evidence for declared user journeys
```

ShellBeam should not implement a browser engine.

Browser provider claims must distinguish declared/enforced/observed network/filesystem authority.

## 23. DAP provider integration boundary

A future DAP provider SHOULD normalize:

```text
debug session/process identity
source correlation
breakpoints
stop reasons
threads/stack frames
locals/variables
evaluation results
exceptions
provider diagnostics
```

It must reuse source identity, process ownership, resource authority, and evidence contracts.

ShellBeam should not implement debugger semantics itself beyond the provider envelope.

## 24. Git provider boundary

Git currently remains largely reachable through shell execution plus workspace provenance.

A future semantic Git provider MAY add bounded normalized facts/actions when they materially reduce parsing/context waste, for example:

```text
exact diff view
commit/tree identity
conflict inspection
worktree/branch operation receipts
```

It must not become a second repository truth store or general autonomous merge planner.

## 25. Code Intelligence evolution

The existing query-only semantic provider remains valuable and SHOULD evolve incrementally:

- improve model-facing source locations;
- preserve exact `SourceRef` identity underneath;
- expose affected-relation provenance/authority/coverage useful to Affected Surface;
- add language providers only when qualified and needed;
- keep semantic facts separate from build/test evidence;
- do not add auto-edit/refactor behavior until it is expressed as a Mutation Transaction provider.

## 26. Context-window economics

EngineeringStateView exists partly to reduce model context/tool-call waste, but token minimization cannot erase authority.

The default view SHOULD be bounded and layered:

```text
summary first
stable deep refs
explicit omitted/truncated counts
no repeated huge blobs
```

A consumer should normally obtain the current engineering-state summary in one intentional inspection and drill into only the facts needed for reasoning.

## 27. Storage strategy

Prefer projections over duplicated persistence.

Persist only identities/state transitions that need durable semantics, such as:

```text
mutation transaction identity/receipt
future envelope identity + baseline if introduced
hard authority policy records
```

Do not persist copies of:

```text
current affected surface
current diagnostics
current evidence satisfaction
current environment summary
current focus view
```

when they can be deterministically recomputed under bounded retention.

If retention makes recomputation impossible, the view reports partial/unavailable rather than using stale copies as current truth.

## 28. Compatibility/degradation

The engineering-state capability must degrade explicitly when inputs/providers are absent.

Examples:

```text
code intelligence unavailable -> semantic scope unavailable, not empty
no verification policy -> policy_absent, no implicit template
resource metrics unavailable -> resource summary partial
trace advisory -> affected relation advisory
no mutation provider -> EngineeringStateView still works read-only
```

Ordinary `local_shell` execution remains functional.

## 29. Explicit non-goals

This design does NOT add:

- a durable user-task object;
- a durable giant Change aggregate;
- daemon-side reasoning about unresolved work;
- semantic working-set inference from conversation behavior;
- an editor buffer/shadow filesystem;
- automatic patch rebasing;
- automatic fix recommendations;
- a universal AST mutation DSL;
- a browser engine;
- a debugger engine;
- a Git replacement;
- a claim that requested authority was enforced when the provider cannot enforce it;
- a claim of exact mutation attribution without the required exclusion/hermetic authority.

## 30. Acceptance semantics for implementation planning

A future implementation plan MUST include evidence/tests for at least:

1. EngineeringStateView is a projection and does not duplicate canonical evidence/source truth;
2. `activity_id` remains correlation only;
3. no `task_complete`/narrative reasoning state is introduced;
4. mechanical scope and caller focus hints remain distinct authority classes;
5. model-facing line/path locations remain bound to exact source identity;
6. old SourceRefs never resolve against new bytes;
7. mutation transaction generation/source preconditions reject stale edits deterministically;
8. provider result cannot claim stronger mutation effect than post-source observation allows;
9. evidence stale/invalidation behavior is generation/policy/affected-authority aware;
10. advisory mutation scopes do not silently become permissions;
11. declared/enforced/observed authority remain separate even when one dimension is unavailable;
12. resource governor does not redefine verification sufficiency;
13. undeclared provider live resources block verification-complete semantics;
14. transferred persistent resources are explicit and separately owned;
15. missing providers yield unavailable/partial views rather than fabricated empty facts;
16. no implementation introduces an autonomous planner/workflow loop.
