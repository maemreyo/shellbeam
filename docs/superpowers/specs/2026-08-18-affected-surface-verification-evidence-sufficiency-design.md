# ShellBeam Affected Surface, Verification Obligations, and Evidence Sufficiency Design

Date: 2026-08-18
Status: semantic freeze approved for the first Machine Truth Harness implementation slice; implementation planning may proceed
Scope: one coherent subsystem covering affected-surface derivation, policy matching, verification obligations, evidence sufficiency, verification economics, waiver/retry/quiescence semantics, and policy starter templates

## 1. Decision

ShellBeam SHALL add a verification-semantics subsystem whose core job is **not** to choose as many tests as possible and **not** to minimize test count.

The subsystem answers:

> Given a mechanically described engineering state and an approved repository policy, what verification obligations exist, what evidence is admissible for each obligation, and is the current evidence sufficient?

Its pipeline is:

```text
ENGINEERING STATE
       │
       ▼
AFFECTED SURFACE
relation + authority + coverage + provenance
       │
       ▼
POLICY MATCH
       │
       ▼
OBLIGATION DISPOSITION
required_now | deferred | optional | not_triggered | waived
       │
       │ required_now
       ▼
EVIDENCE REQUIREMENTS
       │
       ▼
ADMISSIBLE PROVIDERS
       │
       ▼
SUFFICIENCY CONSTRAINTS
       │
       ▼
COST/RESOURCE OPTIMIZATION
       │
       ▼
EXECUTION / OBSERVATION
       │
       ▼
EVIDENCE STATUS
not_evaluated | satisfied | failed | insufficient
inconsistent | unknown | unavailable
       │
       ▼
GATE FOLD
clear | blocked | indeterminate
```

Tests are merely one evidence provider.

## 2. Frozen principles

The subsystem freezes these principles:

1. **ShellBeam owns engineering-state semantics, not engineering reasoning.**
2. **ShellBeam optimizes verification for sufficient evidence, not maximal testing.**
3. **Uncertainty must increase verification conservatism, never decrease it.**
4. **Cost may optimize among sufficient evidence sets; it must never redefine sufficiency.**
5. **Test what the application owns; verify the assumptions it depends on.**
6. **A full suite is a phase-triggered gate, not an inner-loop reflex.**
7. **Failed evidence must be understood as evidence; rerun-to-pass cannot erase contradictory history.**
8. **No hard NFR target is invented by ShellBeam.**
9. **Policy-gap detection is allowed only from mechanically attributable classifications and remains advisory.**
10. **A verification operation is not terminal-complete while it retains undeclared live resources.**

## 3. Relationship to existing ShellBeam evidence

This subsystem extends but does not redefine the Evidence Ledger.

Existing evidence remains authority for bounded questions such as:

- did the admitted command mechanically succeed/fail/timeout/kill;
- were declared expected artifacts observed;
- what workspace/source/environment facts were bound;
- is immutable evidence current/stale/unknown under the evidence contract.

The new subsystem adds the missing policy question:

```text
Does this evidence satisfy a currently applicable engineering obligation?
```

It does NOT change terminal receipt truth.

It does NOT make language-semantic diagnostics equivalent to command evidence.

It does NOT silently reinterpret historical evidence under new policy without explicit policy-version matching.

## 4. Inputs

Verification semantics consume only attributable inputs:

```text
selected baseline/current source generation
observed changes
mechanical affected relations
caller-declared focus hints, separately labeled
approved materialized verification policy
environment/toolchain/resource facts
current evidence records
provider capabilities/qualification
engineering phase
explicit waivers
```

The subsystem MUST NOT infer requirements from user conversation prose unless the caller first materializes that intent into an explicit policy/configuration input under an attributable authority class.

## 5. Affected Surface

### 5.1 Purpose

Affected Surface is the versioned mechanical bridge from source/engineering changes to policy applicability.

The subsystem MUST NOT expose a single unqualified statement such as:

```text
affected = [foo, bar, baz]
```

without describing how each relation was derived and how complete/strong the derivation is.

### 5.2 AffectedRelation

Conceptual shape:

```text
AffectedRelation
  relation_id
  from_subject
  to_subject
  relation_kind
  basis
  derivation_authority
  coverage
  provider_id/version?
  source_generation
  provenance_refs[]
  captured_at
  caveats[]
```

`from_subject` and `to_subject` are closed/versioned subject unions such as:

```text
path
source_ref
symbol
package/module
project command
artifact
runtime component
user journey
policy surface class
future qualified subject kinds
```

The first implementation SHOULD support only subject kinds that can be attributed deterministically from existing ShellBeam/project metadata.

### 5.3 `basis`

`basis` explains **how the relation was obtained**.

Initial vocabulary may include:

```text
observed_source_mutation
import_graph
call_graph
semantic_reference
config_binding
project_policy
filesystem_relation
runtime_trace
artifact_relation
explicit_project_mapping
```

No generic `ai_inferred` basis exists.

### 5.4 `derivation_authority`

Authority answers:

> How strong is the claim represented by this relation?

Initial values:

```text
authoritative
mechanical
advisory
```

Examples:

- exact observed mutation of a path can be authoritative/mechanical according to the existing source-transition contract;
- deterministic Go import graph relation can be mechanical;
- best-effort runtime trace relation is advisory unless a future hermetic provider contract explicitly proves stronger authority.

The subsystem MUST NOT promote advisory relations merely because coverage is high.

### 5.5 `coverage`

Coverage answers:

> How much of the provider's intended search/observation domain was actually covered?

Initial values:

```text
complete
bounded
partial
unknown
```

Coverage and authority are independent.

Examples:

```text
runtime trace:
  authority = advisory
  coverage = bounded

import graph for fully parsed module:
  authority = mechanical
  coverage = complete

semantic index after provider timeout:
  authority = mechanical
  coverage = partial
```

A consumer MUST NOT interpret `coverage=complete` as proof that the provider class itself is semantically exhaustive for all runtime dependencies.

### 5.6 Granularity rule

Affected coverage MUST be carried per relation/provider/domain when practical. A whole-surface summary may be exposed for convenience, but it cannot erase heterogeneous qualities.

Example:

```text
imports:            mechanical / complete
static callers:     mechanical / bounded
runtime reflection: advisory / unknown
config relations:   mechanical / partial
network dependency: unavailable
```

is preferable to:

```text
affected_surface_quality = partial
```

### 5.7 Uncertainty monotonicity

Verification derivation is monotonic under **loss or weakening of affected-surface information**, not under a scalar confidence score.

Normative invariant:

> Weakening or removing affected-surface information MUST NOT remove an otherwise applicable mandatory obligation unless an independent stronger policy/mechanical fact proves non-applicability.

Examples of weakening include:

```text
complete -> bounded -> partial -> unknown coverage
mechanical -> advisory derivation authority
loss of a previously proven relation
provider becoming unavailable
```

It is forbidden to derive:

```text
"dependency unknown" -> "skip verification"
```

A narrower obligation set is legal only when an independently stronger fact establishes non-applicability; absence of information is never that fact.

## 6. Ownership semantics

Policy applicability SHOULD distinguish three ownership classes:

```text
application_owned
integration_owned
delegated
```

### 6.1 Application-owned

The application implements the behavior/invariant.

Typical evidence may include focused behavior tests, compile/type checks, deterministic property checks, or integration tests depending on the obligation.

### 6.2 Integration-owned

The application does not implement the dependency internals but owns the way it configures/calls/interprets the dependency.

Examples:

```text
production transaction isolation is actually requested
both writes are inside one transaction
serialization failures are handled correctly
TLS configuration uses the declared mode
S3 failure semantics are handled as expected
```

### 6.3 Delegated

A provider/library owns the underlying implementation theorem.

ShellBeam policy SHOULD normally verify the application's reliance/assumption rather than re-proving the provider implementation.

Example:

```text
wrong:
  generate millions of UUIDs trying to witness a collision

right when collision handling matters:
  inject a deterministic collision and verify application behavior
```

`delegated` never means automatically irrelevant.

## 7. NFR classification

The policy model SHOULD support concern classification without inventing targets.

Initial classes:

```text
scale_driven
risk_driven
context_driven
delegated
```

### 7.1 Scale-driven

Examples: throughput, concurrency, capacity, horizontal scaling, multi-region availability.

These require declared product/scale targets before hard evidence thresholds exist.

### 7.2 Risk-driven

Examples: authorization, privacy, data loss, secret handling, durability, auditability.

These may be critical even for a tiny project.

### 7.3 Context-driven

Examples: supported platforms, accessibility, offline behavior, mobile bandwidth, battery/thermal behavior, developer-machine resource impact.

### 7.4 Delegated

Examples: cryptographic primitive implementation, database engine internals, UUID implementation.

Again, application integration assumptions may still be verified.

## 8. Materialized verification policy

### 8.1 Runtime source of truth

Verification SHALL run against a **materialized repository-pinned policy**.

It SHALL NOT run directly against a mutable named maturity/profile label.

Conceptual identity:

```text
MaterializedVerificationPolicy
  schema_version
  policy_id
  policy_digest
  repository_scope
  source
  profile_origin?
  approval_ref
  approval_authority
  approved_at
  rules[]
```

Possible source values:

```text
repository_authored
user_approved_profile
user_approved_generated_policy
future_admin_policy
```

### 8.2 Policy Starter Profiles

ShellBeam MAY ship **Policy Starter Profiles** / **Verification Posture Templates** such as:

```text
prototype
team
production
```

They are templates, never runtime policy.

Flow:

```text
starter template
      ↓
materialized proposed policy
      ↓
preview
      ↓
user review / approval
      ↓
repository-pinned materialized policy
```

After approval, verification uses the materialized policy digest. If ShellBeam v3 changes the definition of `team`, an existing repository pinned to `shellbeam/team@v2` does not silently gain or lose gates.

### 8.3 Profiles cannot invent NFR targets

Even `production` MUST NOT silently create targets such as:

```text
P99 < 100 ms
10,000 concurrent users
99.99% availability
```

A starter template may instead encode concern posture such as:

```text
security-sensitive surfaces require an explicit matching policy
performance target remains undeclared unless user declares one
durability posture requires explicit declaration
load/stress verification remains not_triggered without a declared target
```

### 8.4 Absent policy

When no approved materialized policy exists:

```text
verification_policy = absent
```

ShellBeam may list available starter templates, but no template is automatically selected.

The absence of policy is explicit; it is not silently treated as `prototype`.

`policy_absent` is also an explicit **effective-policy state**. Creating the first repository policy is a policy-changing mutation and follows the same non-self-activation rule as replacing an existing policy:

```text
effective_policy: absent
proposed_policy:  P1
        ↓
separate authorized activation event
        ↓
effective_policy: P1 from a declared subsequent cut
```

The first proposed policy MUST NOT become authoritative merely because its source bytes appear in the repository. It becomes effective only through a separate authorized activation event, and it MUST NOT retroactively govern the mutation that introduced those bytes.

### 8.5 Policy self-amendment and activation authority

A source mutation that changes verification-policy bytes MUST NOT make the changed policy authoritative for evaluating that same mutation merely because the new bytes exist in the workspace.

The model distinguishes:

```text
effective_policy: P1
proposed_policy:  P2
```

where `P1` is the latest policy activated by authority established **before** the source mutation under evaluation, and `P2` is the materialized candidate derived from the changed policy bytes.

Normative rules:

1. A policy-changing mutation is evaluated under `P1` plus any already-effective meta-policy governing policy mutation/activation.
2. `P2` is proposal data until a separate approval/activation event binds its exact digest.
3. Repository bytes such as `approved_by = "user"`, commit messages, model prose, or fields introduced by `P2` are not approval authority by themselves.
4. The approval/activation event MUST be attributable to an authority source outside the source mutation being evaluated and MUST bind at least the previous effective-policy digest, proposed-policy digest, repository scope, actor/authority class, and event identity/time.
5. Activating `P2` MUST NOT retroactively erase obligations that governed the mutation which produced `P2`.
6. `P2` MUST NOT grant itself new waiver authority, activation authority, or evidence-admissibility rules for clearing its own activation gate.
7. A waiver applied to obligations governing policy mutation/activation uses authority from `P1`/the pre-existing meta-policy or a separately authorized external approval event; authority created only by `P2` is ineligible.
8. After valid activation, `P2` governs subsequent applicable source generations/checkpoints according to its activation record.

Conceptually:

```text
PolicyActivation
  activation_id
  repository_scope
  previous_effective_policy_digest: absent | digest
  proposed_policy_digest
  authority
  actor
  approved_at
  applies_from_generation/checkpoint
```

The exact write API is deferred, but this self-amendment boundary is not optional. ShellBeam may expose a proposed policy and its diff before activation, but it never treats self-authored policy text as its own approval proof.

For the bootstrap case, `previous_effective_policy_digest = absent` (or an equivalent closed sentinel representation). The activation record still binds the proposed digest, external authority, actor, repository scope, event identity/time, and the subsequent generation/checkpoint cut from which the first policy becomes effective.

## 9. Mechanically declared sensitivity and policy gaps

ShellBeam MAY detect a policy gap only when the sensitivity/classification has an attributable non-reasoning source.

Valid sources may include:

```text
repository policy path mapping
explicit subsystem classification
schema annotation
provider capability metadata
project manifest mapping
explicit caller-declared class
```

Example:

```text
project rule:
  internal/auth/** -> security_sensitive

change touches internal/auth/token.go
no approved security verification rule matches

=> policy_gap
```

The subsystem MUST NOT classify a file as security-sensitive merely because it contains words such as `password`, `credential`, `auth`, or because an LLM-like heuristic "feels" sensitive.

A policy gap is advisory:

```text
PolicyGap
  surface
  declared_class
  classification_source
  missing_policy_class
  authority
  provenance
```

It does not become a hard gate until policy is explicitly materialized/approved.

## 10. Verification obligations

### 10.1 Purpose

An obligation is the deterministic, inspectable result of matching approved policy against engineering state/affected surface.

Conceptual shape:

```text
VerificationObligation
  obligation_id
  policy_id/digest
  source_rule_id
  trigger_refs[]
  affected_scope_refs[]
  ownership
  risk_class?
  required_phase
  requirement
  evidence_requirements[]
  sufficiency_basis
  required_authority
  environment_requirement?
  resource_policy?
  concurrency_policy?
  escalation_rules[]
  applies_to_generation
  disposition
  disposition_provenance
  evidence_status
  evidence_status_provenance
  satisfaction_evidence[]
  waiver?
```

### 10.2 `sufficiency_basis`

Every mandatory obligation MUST explain why the required evidence class is considered sufficient.

Example:

```text
obligation: auth-handler-behavior
trigger: auth handler changed
required evidence: targeted_behavior_test
sufficiency_basis: application_owned_behavior
escalation: shared middleware affected -> auth integration suite
```

The system must be inspectable enough to answer:

> Why is this evidence class enough for this obligation?

not only:

> Which check should run?

### 10.3 Engineering phases

Initial phase vocabulary:

```text
inner_loop
checkpoint
pre_merge
release
nightly
periodic
```

A policy may define others only through versioned schema evolution.

A full suite is never universally implied by `pre_merge`; it is required only when the approved policy declares that trigger.

## 11. Obligation disposition, evidence status, and gate status

Applicability/disposition and evidence sufficiency are independent dimensions. A waiver changes whether an obligation blocks a gate; it does **not** turn missing or insufficient evidence into satisfied evidence.

### 11.1 `ObligationDisposition`

Closed initial vocabulary:

```text
required_now
deferred
optional
not_triggered
waived
```

#### `required_now`

Policy applies in the current phase. The obligation remains mandatory unless separately waived. Its `evidence_status` may be any evidence state, including `satisfied`.

#### `deferred`

The obligation exists but is not required until a later declared phase.

#### `optional`

The evidence/check may be useful but policy does not require it for the current gate.

#### `not_triggered`

The policy class has been considered and an attributable fact establishes non-applicability to the current affected surface/phase.

This disposition is important for preventing model-driven over-testing by imagination.

Example:

```text
load_test:
  disposition: not_triggered
  disposition_basis:
    no declared performance target
    no matching capacity-sensitive policy surface
  evidence_status: not_evaluated
```

#### `waived`

The requirement would otherwise be `required_now`, but an authorized actor has explicitly permitted it not to block within a bounded scope.

Waiver is not `not_triggered`, not `unavailable`, and not evidence satisfaction.

### 11.2 `EvidenceStatus`

Closed initial vocabulary:

```text
not_evaluated
satisfied
failed
insufficient
inconsistent
unknown
unavailable
```

Semantics:

- `not_evaluated`: no admissible evidence evaluation has been attempted/retained for this obligation at the bound policy/generation/environment cut;
- `satisfied`: current admissible evidence meets every hard sufficiency constraint;
- `failed`: admissible current evidence positively demonstrates failure under the obligation contract;
- `insufficient`: evidence exists but does not meet semantic coverage/authority/freshness/environment/stability constraints;
- `inconsistent`: compatible retained evidence materially contradicts itself under policy rules that cannot be ignored;
- `unknown`: ShellBeam can identify the evidence question but cannot determine its status from available facts;
- `unavailable`: the required provider/observation/environment cannot currently be performed under the contract.

A waived obligation retains its literal evidence status. For example:

```text
native_linux_verification:
  disposition: waived
  evidence_status: unavailable
  waiver: waiver_...
```

It is forbidden to rewrite this as `evidence_status: satisfied` merely because the gate may proceed.

### 11.3 Aggregate `GateStatus`

A bounded aggregate MAY expose:

```text
clear
blocked
indeterminate
```

For the current policy/phase/generation cut:

- `clear`: every `required_now` obligation has `evidence_status=satisfied`, and every otherwise-mandatory exception is covered by a currently valid `waived` disposition;
- `blocked`: at least one non-waived mandatory obligation has `failed`, `insufficient`, or unresolved `inconsistent` evidence under the effective policy;
- `indeterminate`: no blocking failure/contradiction is known, but at least one non-waived mandatory obligation is `not_evaluated`, `unknown`, `unavailable`, or otherwise cannot yet establish satisfaction.

The aggregate MUST preserve counts/breakdown, for example:

```text
gate_status: clear
mandatory_breakdown:
  evidence_satisfied: 3
  waived: 1
  blocking: 0
  indeterminate: 0
```

It MUST NOT report the example above as `4 satisfied`.

## 12. Waiver semantics

A waiver MUST be machine-legible and provenance-rich.

Conceptual shape:

```text
VerificationWaiver
  waiver_id
  obligation_id/rule_scope
  authority
  actor
  reason
  created_at
  applies_to:
    repository/workspace/checkpoint/generation/phase scope
  expires:
    time / phase / source generation / checkpoint / explicit revoke
  policy_digest
```

Example:

```text
disposition: waived
evidence_status: unavailable
authority: user
reason: native Linux verification is performed only in CI
scope: current checkpoint
expires: pre_merge
```

Rules:

- waiver cannot silently mutate the underlying requirement;
- waiver expiration is deterministic;
- an expired waiver removes the waiver disposition and exposes the underlying mandatory applicability plus its unchanged literal evidence status again;
- a waiver created against one policy digest cannot silently waive a semantically changed rule after policy replacement unless the new policy explicitly preserves it;
- reason is auditable metadata, not a machine claim that the waiver is wise;
- only configured authority classes may waive mandatory obligations.

## 13. Evidence requirements and admissibility

An obligation may be satisfied by one or more evidence classes.

Conceptual requirement:

```text
EvidenceRequirement
  requirement_id
  semantic_coverage_class
  accepted_provider_classes[]
  minimum_authority
  source_freshness_requirement
  environment_requirement?
  artifact_requirement?
  stability_requirement?
  count/cardinality rules?
```

Examples of provider classes:

```text
static_format_check
typecheck/compiler
focused_behavior_test
integration_test
schema_compatibility
browser_user_journey
native_platform_verification
artifact_digest
resource_measurement
release_check
```

A cheap provider is not admissible merely because it produces a green status.

## 14. Evidence Sufficiency Problem

### 14.1 Hard constraints before cost

Evidence-set optimization is defined as:

```text
minimize Cost(V)

subject to:
  Coverage(V) >= RequiredCoverage
  PolicyDeclaredRiskControlsSatisfied(V) = true
  Authority(V) >= RequiredAuthority
  Freshness(V) = current as required
  Environment(V) satisfies requirement
  Stability(V) satisfies requirement
  Policy(V) = effective materialized policy for the evaluation cut
```

The exact mathematical representation may differ, but the ordering is normative:

```text
FIRST satisfy semantic correctness constraints
THEN minimize cost among admissible sufficient sets
```

It is forbidden to minimize cost first and then argue that the cheapest check is enough.

`PolicyDeclaredRiskControlsSatisfied(V)` is not a probability, confidence estimate, or ShellBeam-computed residual-risk score. It means that every concrete evidence/control requirement attached by the effective policy to applicable `risk_class` rules is satisfied by `V`. For example, an `authorization` rule may require both behavior-test evidence and integration-boundary evidence; ShellBeam checks those declared requirements rather than estimating that "residual risk is 7%".

### 14.2 Evidence sufficiency result

Per-obligation evidence evaluation uses the `EvidenceStatus` vocabulary from §11.2:

```text
not_evaluated
satisfied
failed
insufficient
inconsistent
unknown
unavailable
```

Waiver is deliberately absent from this list because waiver is an obligation disposition/gate exception, not an evidence result.

`inconsistent` is distinct from failure of a single test. It means the retained evidence set contains materially contradictory evidence under a compatibility relation that policy says cannot be ignored.

## 15. Verification Cost

Cost is a vector, not a single `cheap/expensive` label.

Conceptually:

```text
VerificationCost
  wall_time
  cpu_time
  peak_memory
  process_count_peak
  io_bytes?
  network?
  local_resource_quality
  provider_cost?
  model_interaction_cost?
  flake_probability?
  historical_sample_quality
```

Authority rules:

- local resource measurements observed by ShellBeam carry their actual provider authority/quality;
- provider-reported billing/quota may be recorded as provider-reported;
- model token/quota cost may be caller-reported or unavailable;
- ShellBeam MUST NOT fabricate a precise model cost it cannot observe.

Historical cost is evidence for optimization, not a guarantee of future runtime.

## 16. Bounded parallelism

The default architecture is **resource-budgeted bounded parallelism**, not universal sequential execution and not unbounded automatic parallelism.

Verification/provider policy describes semantic concurrency safety:

```text
parallel_safe
shared_resources
exclusive_resource_class
expected_workload_class
```

Resource Governance decides currently admissible concurrency from:

```text
CPU budget / pressure
memory budget / availability
process budget
thermal/host facts when available
provider-specific caps
exclusive/shared resource claims
```

Examples:

```text
isolated cheap unit tests -> may run bounded parallel
browser E2E with shared profile -> serialize
race suite -> CPU/resource class may serialize
DB integration sharing one database -> serialize unless explicitly isolated
```

The verification subsystem MUST NOT hard-code a universal worker count.

## 17. Conditional escalation ladder

Verification is not a fixed sequence of gates.

The preferred behavior is:

```text
change
  ↓
affected surface
  ↓
cheapest high-signal admissible evidence for current obligations
  ↓
escalate only when policy/risk/scope/evidence quality requires it
```

Typical classes may look like:

```text
static/lint/type checks
focused checks
related integration
specialized E2E/race/load/stress/native checks
full/release suite
```

but no class is globally mandatory for every change.

A README-only change may require only documentation validation. A shared auth middleware change may trigger integration/user-journey checks immediately. A migration framework change may trigger broad database/startup verification despite a tiny textual diff.

## 18. Retry and contradictory evidence

### 18.1 No blind retry-to-pass

A failed check cannot be erased merely by an identical rerun.

For compatible runs with:

```text
same source generation
same relevant environment
same command/evidence contract
```

if results are:

```text
FAIL
PASS
```

then aggregate stability becomes at least:

```text
inconsistent
```

and mandatory verification is not automatically satisfied.

### 18.2 Diagnostic rerun

Policy may permit a rerun whose explicit reason is diagnosis/flake qualification.

The evidence model SHOULD distinguish:

```text
rerun_reason = diagnose_flake
```

from an unqualified rerun.

### 18.3 Flake qualification protocol

A repository may define a deterministic protocol such as N runs and threshold X. Only then may the verification fold treat a flaky result according to that approved rule.

The protocol itself is policy and must be versioned/materialized.

## 19. Test mutation semantics

The verification subsystem does not prevent a reasoning agent from editing tests, but it preserves evidence honesty.

Policy philosophy SHOULD state:

- a test may change because intended behavior changed;
- a test may change because the test is mechanically/provably invalid;
- changing a test merely to erase a legitimate failure is not verification satisfaction;
- evidence before/after test modification binds different source generations and cannot be collapsed.

ShellBeam does not decide whether a test edit is conceptually justified; it exposes the source/evidence transition so the reasoning model/user can review it.

## 20. Quiescence and live-resource completion

A verification operation may not become **verification-terminal-complete** while it retains undeclared live resources attributable to the operation/provider.

The invariant is:

```text
actual_live_resources
-
declared_transferred_or_persistent_resources
=
0
```

Examples of live resources may include:

```text
child/descendant processes
browser processes
containers
owned listening ports
watchers/daemons
future provider resources
```

A verification step may intentionally launch a dev server or daemon for later steps only if ownership is explicitly transferred to a named/persistent ShellBeam-managed identity under an applicable provider contract.

A crashed test leaving orphan Chrome processes is not terminal-complete verification merely because the test executable exited 0.

Quiescence failure SHOULD produce incomplete/unsatisfied evidence according to the obligation contract without rewriting the canonical child receipt.

## 21. Environment binding

Evidence whose semantics depend on environment MUST bind the relevant environment facts.

Performance/NFR evidence is especially sensitive.

A result such as:

```text
P99 = 83 ms
```

without workload, environment, source generation, and measurement semantics is not sufficient performance evidence for a declared target.

A performance evidence record SHOULD bind, as applicable:

```text
workload identity
concurrency
warmup/duration/sample count
source/build generation
OS/arch/toolchain
environment fingerprint
resource limits
provider/measurement quality
result distribution
```

## 22. Explicit NOT_TRIGGERED reporting

The model-facing view SHOULD explicitly report important expensive/specialized classes that were considered but not triggered by policy.

Example:

```text
NOT_TRIGGERED
  race suite
    reason: no application-owned concurrency surface matched
  browser E2E
    reason: no declared user-journey obligation matched
  load/stress
    reason: no declared performance/capacity target
```

This is not a ban on the reasoning model asking to run an optional check. It is a statement that the approved mandatory policy does not require it.

## 23. Aggregate verification view

A bounded inspect response should eventually expose something like:

```text
verification_policy:
  policy_id
  digest
  source
  profile_origin?

source_generation:
  current

affected_surface_summary:
  relations
  authority/coverage matrix
  uncertainty flags

gate_status: clear | blocked | indeterminate
mandatory_breakdown:
  evidence_satisfied: N
  waived: M
  blocking: K
  indeterminate: J

REQUIRED_NOW
  ✓ typecheck
      disposition: required_now
      evidence_status: satisfied
      evidence: ev_...
  ✗ daemon integration
      disposition: required_now
      evidence_status: not_evaluated
      reason: no current admissible evidence

DEFERRED
  full suite
      disposition: deferred
      evidence_status: not_evaluated
      phase: pre_merge

NOT_TRIGGERED
  browser E2E
      disposition: not_triggered
      evidence_status: not_evaluated
  race suite
  load test

WAIVED
  native Linux check
      disposition: waived
      evidence_status: unavailable
      authority: user
      expires: pre_merge

UNKNOWN/POLICY_GAPS
  ...

cost_summary:
  observed local resource history only where available
```

This view is a projection. It does not create a new canonical evidence store.

## 24. Failure/unknown semantics

The subsystem MUST fail honest rather than fail convenient.

Examples:

- missing semantic provider -> relation/evidence unavailable, not empty;
- stale evidence -> stale/insufficient, not pass;
- policy digest mismatch -> unknown/insufficient under current policy, not silently reinterpreted;
- partial affected coverage -> widen or expose unknown according to policy;
- unavailable native platform -> unavailable or valid waiver/defer path, not not-triggered;
- provider timeout -> partial/unavailable, not zero results;
- missing telemetry -> cost unknown, never assume cheap;
- missing model quota visibility -> model cost unavailable.

## 25. No automatic requirement invention

The daemon MUST NOT create hard requirements such as:

```text
100k concurrent users
P99 < 50ms
99.999% uptime
Argon2 parameter X
full E2E for every UI edit
race test for every database edit
```

unless those requirements come from an approved materialized policy source.

A reasoning model may propose such a requirement to the user. The requirement becomes a ShellBeam policy input only after an explicit materialization/approval step.

## 26. Policy starter posture examples

Starter templates may be shipped approximately as follows; exact syntax is deferred to implementation planning.

### 26.1 Prototype

```text
inner loop:
  cheapest high-signal checks applicable to affected surface
integration:
  only declared boundary-impact rules
E2E:
  only declared user journeys
load/stress:
  no hard target unless declared
full suite:
  no universal requirement; optional or configured checkpoint
security/durability:
  mechanically classified sensitive surfaces expose policy gaps
```

### 26.2 Team

```text
inner loop:
  targeted verification + stronger shared-boundary rules
checkpoint/pre_merge:
  repository-configured broad checks
specialized suites:
  only explicit triggers
waivers:
  require provenance + expiry
```

### 26.3 Production

```text
security/durability concern classes:
  require explicit repository posture for declared sensitive surfaces
release:
  stronger phase gates as configured
performance/scale:
  still no invented targets
availability:
  still no invented target
```

These examples are bootstrap philosophy only. Runtime semantics use the approved materialized policy, never the template name.

## 27. Persistence/versioning

The first implementation SHOULD persist only durable entities that require identity/history:

```text
materialized policy + digest
policy approval/activation authority records
waivers
possibly obligation derivation records when needed for audit/replay
```

Policy approval/activation authority records MUST be immutable/auditable authority facts once committed to the durable record. They determine which materialized policy is authoritative and from which declared generation/checkpoint cut; they are not ephemeral projections from repository policy bytes.

Affected surfaces and aggregate sufficiency SHOULD remain deterministic projections where source records are retained.

If obligation derivation is persisted, it MUST bind:

```text
policy digest
affected-source derivation identities
source generation
producer/schema version
```

and must never become stronger authority than those inputs.

## 28. Model-facing ergonomics

The common path should optimize for one bounded inspection rather than many joins.

The model should not have to parse raw test logs or independently reconstruct why a verification class was selected.

At the same time, deep inspection must remain possible via stable references for:

```text
policy rule
obligation
relation provenance
evidence record
provider result
waiver
resource observation
```

Model-facing summaries MUST not hide uncertainty or authority transitions to save tokens.

## 29. Explicit non-goals

This subsystem does NOT:

- decide what code change to make;
- diagnose a failure root cause;
- automatically rewrite code/tests;
- schedule a general workflow DAG;
- invent product requirements;
- treat maturity templates as mutable runtime policy;
- make every special test class mandatory;
- retry failures until green;
- classify sensitive code with filename/prose heuristics;
- prove delegated provider internals;
- claim task completion;
- compute or invent probabilistic residual-risk/confidence scores to justify skipping verification;
- silently downgrade verification because the machine is busy or quota is low.

## 30. Acceptance semantics for implementation planning

A future implementation plan MUST include tests/contracts proving at least:

1. authority and coverage are independent dimensions on affected relations;
2. partial/unknown affected coverage cannot narrow mandatory obligations;
3. starter template changes do not alter an already pinned materialized policy;
4. templates do not synthesize NFR targets;
5. obligation disposition and evidence status are independent; a valid waiver may clear a gate while literal evidence remains `unavailable`/`insufficient`/`not_evaluated`;
6. aggregate gate reporting never counts waived obligations as evidence-satisfied obligations;
7. waiver scope/expiry/policy-digest behavior is deterministic;
8. a policy-changing mutation cannot activate the changed policy for evaluating itself without a separate authorized activation event;
9. a proposed policy cannot self-grant waiver/activation authority for its own activation gate;
10. policy gaps require mechanically attributable sensitivity/classification;
11. cost is optimized only after sufficiency/admissibility constraints;
12. policy-declared risk controls are checked as explicit requirements; no residual-risk/confidence score is invented;
13. a cheap but semantically insufficient provider cannot satisfy a stronger obligation;
14. FAIL->PASS compatible reruns produce inconsistent evidence unless an approved flake protocol resolves it;
15. stale evidence cannot satisfy current-generation obligations;
16. environment-dependent evidence requires the declared compatible environment dimensions;
17. full suite is phase/policy triggered rather than automatic;
18. quiescence detects undeclared live resources while allowing declared ownership transfer;
19. bounded parallelism obeys resource/provider constraints without hard-coded universal worker count;
20. delegated ownership still verifies configured/integration assumptions when policy declares them;
21. policy absence remains explicit and does not silently choose a profile;
22. `policy_absent -> first proposed policy` requires a separate authorized activation event, and the first policy never retroactively governs the mutation that introduced its source bytes;
23. policy approval/activation authority records are durable, immutable/auditable facts rather than ephemeral projections;
24. the subsystem never emits a `task_complete` truth claim.

## 31. Design boundary with follow-on work

This spec deliberately treats provider execution as downstream.

The next companion design owns:

- EngineeringStateView projection;
- mechanical scope versus caller focus hints;
- mutation transaction/baseline/effect attribution;
- stale-evidence invalidation after mutation;
- declared/enforced/observed authority envelopes;
- provider integration boundary.

See:

- [Engineering State, Mutation Transactions, and Authority Envelopes](./2026-08-18-engineering-state-mutation-authority-design.md)
