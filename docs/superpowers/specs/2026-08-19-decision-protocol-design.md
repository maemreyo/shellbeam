# ShellBeam Decision Protocol Design

Date: 2026-08-19
Status: semantic freeze approved; implementation planning may proceed from merged main
Scope: one bounded Decision Protocol subsystem that persists candidate reasoning state, preregistered experiments, server-derived observation evaluation, declarative protocol floors, semantic selection CAS, budget ceilings, and qualified override authorization without turning ShellBeam into a planner, scheduler, or LLM runtime

## 1. Decision

ShellBeam SHALL add a **Decision Protocol** subsystem.

The subsystem exists to make a reasoning process durable and mechanically accountable without moving engineering reasoning into the daemon.

The core separation is:

```text
MODEL / REASONING AGENT
  owns hypotheses
  owns candidate content
  owns experiment choice
  owns semantic assessment
  owns engineering preference
            │
            ▼
DECISION PROTOCOL
  owns durable episode state
  owns preregistration
  owns protocol admissibility
  owns machine-grounded expectation evaluation
  owns budget admission
  owns semantic CAS / terminalization
  owns qualified override authorization
            │
            ▼
MACHINE TRUTH
  operations / receipts
  structured results
  verification evidence
  source/environment identity
  telemetry / reproducibility
            │
            ▼
VERIFICATION SEMANTICS
  affected surface
  obligations
  evidence sufficiency
  clear | blocked | indeterminate
```

The governing principle is:

> **Model owns engineering reasoning: hypotheses, experiments, and semantic choice. ShellBeam owns the protocol under which those choices may claim to be sufficiently challenged and machine-grounded.**

And:

> **ShellBeam never decides which hypothesis is true; it prevents a decision from claiming stronger epistemic status than its declared protocol and machine evidence justify.**

This subsystem is a protocol gate, not an autonomous agent controller.

## 2. Why this belongs in ShellBeam

A model can already voluntarily follow a loop such as:

```text
form alternatives
→ run probes
→ inspect evidence
→ revise belief
→ select
```

That is insufficient as a system property because it depends on conversation memory and model discipline.

Decision Protocol makes the following durable across model/session/provider boundaries:

- which alternatives were declared;
- which predictions were preregistered before observability;
- which machine operations belonged to an experiment;
- which complete attributable machine truth was evaluated;
- which requirements were satisfied, unsatisfied, or indeterminate;
- which candidate a model preferred;
- whether that preference was protocol-admissible to commit;
- whether an override was intentionally and validly authorized;
- which exact epistemic projection a terminal commit relied on.

The subsystem therefore converts a reasoning convention into an auditable protocol without becoming the reasoning engine.

## 3. Hard non-goals

V1 SHALL NOT:

- generate hypotheses or candidates;
- choose or recommend engineering experiments;
- call an LLM;
- schedule commands on behalf of `experiment.define` or policy evaluation;
- generate fixes;
- implement workflow sequencing or retry policy;
- create a general predicate/expression language;
- rank architecture quality mechanically;
- turn `VerifierAssessment` into engineering Evidence;
- permit multiple observation-producing executions inside one experiment;
- permit multiple mutation candidates to race in one workspace;
- infer caller authority from self-declared repository roles;
- implement a generic ShellBeam-owned authority-role lattice;
- invent a generic attestation-revocation ledger;
- guess oracle-gap metrics when losing candidates were not fully evaluated.

Decision Protocol extends machine-truth coordination; it does not replace the existing Machine Truth Harness roadmap boundary.

## 4. Architectural principles

1. **Append canonical facts; derive lifecycle.** Mutable state-machine objects are forbidden for canonical Decision Protocol truth.
2. **Policy is bounded declarative data.** No action sequence, retry, Boolean tree, or workflow DSL.
3. **Preference is not commitment.** A model may propose a selection even when commit is blocked.
4. **Commit is hard-gated only when an explicit activated Decision Policy governs the episode.**
5. **Protocol floor and budget ceiling are separate.** Cost never turns an unsatisfied requirement into a satisfied one.
6. **Prediction declaration happens-before experiment observability.**
7. **Observation truth is server-derived and non-cherry-pickable.**
8. **Machine evaluation concerns declared expectations, not semantic hypothesis truth.**
9. **Semantic projection identity is distinct from audit-history identity.**
10. **Override records exception intent; it never rewrites failed/indeterminate requirements as satisfied.**
11. **Explicitness is not authority.** Override authority requires qualified attestation.
12. **Unknown or unavailable qualification fails closed for future authorization.**
13. **Structural/source-integrity failures are never overrideable.**
14. **Close-unresolved remains the truthful terminal escape path.**

## 5. Canonical record set

The V1 canonical set is:

```text
DecisionPolicySnapshot
DecisionPolicyActivation

DecisionEpisode

DecisionCandidate

DecisionExperiment
ExperimentSeal
PredictionBinding
ExperimentExecutionLink
ExperimentObservationBinding
ExperimentClosure
ExperimentAbort

VerifierAssessment

SelectionProposal

DecisionAuthorityAttestation
DecisionOverride
SelectionCommit

DecisionClosure
```

All canonical records are immutable after durable creation.

The canonical store assigns every durable Decision Protocol record a monotonic `canonical_record_seq` in a Decision Protocol canonical-ledger envelope. The sequence is store-owned ordering metadata, not a caller field and not part of the record's semantic body. An episode replay cut consists of the exact bound policy references plus episode-scoped canonical records whose `canonical_record_seq` is at or below the cut high-water.

This canonical ordering authority is distinct from the rebuildable event journal/change sequence used for model-facing change observation.

Lifecycle/state is projected from record existence and compatibility, never maintained by in-place mutation.

`VerifierAssessment` and `DecisionAuthorityAttestation` are separate ontologies and MUST NOT implement, satisfy, or convert into `EvidenceCandidate`.

## 6. Policy snapshot and activation identity

### 6.1 `DecisionPolicySnapshot`

Conceptually:

```text
DecisionPolicySnapshot {
  schema_version: 1

  repository_id
  policy_digest

  content {
    policy_id
    episode_kinds[]
    requirements[]
    budget
    override_policy
  }
}
```

`policy_digest` hashes canonical `content` only. It MUST NOT hash activation time, activation actor, activation reference, workspace generation, or other event identity.

### 6.2 `DecisionPolicyActivation`

```text
DecisionPolicyActivation {
  activation_id
  repository_id

  policy_digest
  proposal_generation
  activation_generation

  authority: explicit_caller
  actor_ref
  activated_at
}
```

V1 Decision Policy activation uses the existing ShellBeam activation authority shape exactly:

```text
authority = explicit_caller
```

Decision Authority Attestations do **not** authorize policy activation in V1. Decision Protocol override authority is a different domain and MUST NOT reuse `DerivationAuthority`.

A newly created governed episode never accepts an arbitrary historical `activation_ref` from the caller. At `decision.create`, ShellBeam resolves the **current effective applicable** `DecisionPolicyActivation` for the repository and episode kind, and binds that activation to the new episode.

The caller MAY supply expected effective `policy_digest` and/or `activation_ref` as CAS guards. A mismatch fails with `POLICY_CONFLICT`; it does not cause ShellBeam to bind the requested historical activation.

### 6.3 Episode policy immutability

Each episode binds the exact server-resolved current effective activation:

```text
policy_digest
activation_ref
```

for its whole lifetime.

Historical activations remain audit facts only. They cannot be selected to weaken or otherwise alter the policy governing a newly created episode.

Neither weaker nor stronger later policy silently changes an existing episode.

V1 has no policy rebind operation.

A different policy requires a new episode, optionally linked by `predecessor_episode_id`.

## 7. `DecisionEpisode`

```text
DecisionEpisode {
  schema_version

  episode_id
  episode_kind:
    DIAGNOSIS
    OPTION_SELECTION
    CLAIM_EVALUATION
    PLAN_SELECTION

  repository_id
  workspace_id
  predecessor_episode_id?

  baseline {
    source_generation
  }

  policy_binding {
    policy_id
    policy_digest
    activation_ref
  }

  created_by_actor_ref
  created_at
}
```

The baseline generation is the canonical **source-generation domain**. Audit/event generations, record counts, projection revisions, and operation sequence numbers do not stale an episode.

### 7.1 Episode lifecycle projection

```text
OPEN
= DecisionEpisode exists
  && no SelectionCommit
  && no DecisionClosure

COMMITTED
= SelectionCommit exists

CLOSED_UNRESOLVED
= DecisionClosure exists
```

`COMMITTED` and `CLOSED_UNRESOLVED` are terminal.

Exactly one terminal record may exist for an episode:

```text
count(SelectionCommit, DecisionClosure) <= 1
```

The per-episode terminal-transition serialization boundary covers normal commit, override commit, and close-unresolved races.

## 8. Candidate model and revision semantics

```text
DecisionCandidate {
  candidate_id
  episode_id

  semantic_claim
  candidate_kind?

  revises_candidate_id?

  declared_by_actor_ref
  declared_at
}
```

Candidate records are immutable.

A revision creates a new candidate:

```text
B
↓ revise
B2 { revises_candidate_id = B }
```

A revision is replacement semantics, not branching semantics.

`candidate.revise(parent)` requires the parent to be ACTIVE and performs an atomic semantic CAS over parent activeness.

Concurrent:

```text
R1 revise B → B2
R2 revise B → B3
```

may not produce two replacement children. If R1 wins, R2 returns `CANDIDATE_REVISION_CONFLICT`.

A sibling alternative must be created with `candidate.create`, not `candidate.revise`.

Candidate revision never implicitly inherits parent predictions. Existing machine facts remain historical truth; their semantic applicability to the revision remains reasoning-owned.

### 8.1 Lineage roots

Challenge requirements count distinct candidate revision-lineage roots, not revisions.

```text
A → A2
B → B2
```

means two alternative lineages, not four candidates for challenge counting.

## 9. Experiment canonical lifecycle

Experiment lifecycle is entirely append-derived.

Canonical records:

```text
DecisionExperiment
ExperimentSeal
ExperimentExecutionLink
ExperimentObservationBinding
ExperimentClosure
ExperimentAbort
```

V1 freezes the previously implicit record shapes as follows.

```text
DecisionExperiment {
  schema_version

  experiment_id
  episode_id

  declared_by_actor_ref
  declared_at
}
```

`DecisionExperiment` is intentionally minimal. Candidate participation is expressed by immutable `PredictionBinding` records rather than mutable candidate fields on the experiment.

```text
ExperimentExecutionLink {
  schema_version

  link_id
  experiment_id

  operation_id
  session_id
  workspace_id
  source_generation

  accepted_request_fingerprint
  accepted_execution_fingerprint
  accepted_observation_binding_fingerprint

  admitted_at
}
```

`ExperimentExecutionLink` is historical proof of the exact accepted first-admission operation identity. It is not reconstructable merely from `operation_id` after retention or later metadata changes.

```text
ExperimentClosure {
  schema_version

  closure_id
  experiment_id
  observation_binding_id

  closed_by_actor_ref
  closed_at
}
```

A normal closure always names the unique terminal `ExperimentObservationBinding` on which closure relied.

```text
ExperimentAbort {
  schema_version

  abort_id
  experiment_id

  phase:
    BEFORE_EXECUTION
    AFTER_EXECUTION_LINK

  execution_link_id?
  reason

  aborted_by_actor_ref
  aborted_at
}
```

`execution_link_id` is absent exactly for `BEFORE_EXECUTION` and required exactly for `AFTER_EXECUTION_LINK`. Abort does not mutate or delete any earlier record.

Projected state:

```text
DEFINED
= DecisionExperiment exists
  && no ExperimentSeal
  && no ExperimentAbort

SEALED
= ExperimentSeal exists
  && no ExperimentExecutionLink
  && no ExperimentClosure
  && no ExperimentAbort

OBSERVING
= ExperimentSeal exists
  && >= 1 ExperimentExecutionLink
  && no ExperimentClosure
  && no ExperimentAbort

CLOSED
= ExperimentClosure exists

ABORTED
= ExperimentAbort exists
```

`OBSERVING` has no canonical transition record. The first admitted `ExperimentExecutionLink` makes it true in projection.

No experiment may have both `ExperimentClosure` and `ExperimentAbort`.

## 10. Experiment definition never schedules work

Decision Protocol SHALL preserve this separation:

```text
experiment.define
↓
prediction.bind
↓
experiment.seal

--- separate caller action ---

normal ShellBeam execution admission
with experiment_id binding
↓
ExperimentExecutionLink
```

`experiment.define`, `experiment.seal`, `experiment.close`, policy evaluation, and decision inspection do not start commands.

The caller separately invokes the normal ShellBeam execution primitive.

For a command to count as protocol-governed experiment work, its experiment binding must be accepted at execution admission time.

### 10.1 Experiment binding is immutable replay identity

The accepted `experiment_id` is part of the operation's immutable observation/admission identity. V1 SHALL bind it into the existing `ObservationBindingFingerprint` or an exactly equivalent first-admission fingerprint with the same replay guarantees.

For one `operation_id`:

```text
first admission     replay request     result
omitted             E1                 conflict
E1                  E2                 conflict
E1                  omitted            conflict
E1                  E1                 replay-compatible
```

Existing-operation replay validates the frozen experiment binding from durable first-admission state before consulting current experiment metadata. A lost response cannot create a second interpretation of the same operation.

The successful operation reservation/admission and `ExperimentExecutionLink` creation are one recovery-indivisible admission boundary **before spawn**. Implementation may use one record, a transactional write set, prepared obligation, or equivalent storage mechanism, but it MUST NOT:

```text
admit/spawn operation
→ best-effort append ExperimentExecutionLink later
```

If durable experiment linkage cannot be established, admission fails before process spawn. This preserves `reservation-before-spawn` and prevents a retry from becoming a post-hoc linking path.

Post-hoc linking after result observability is forbidden.

## 11. V1 execution cardinality

V1 enforces:

```text
count(ExperimentExecutionLink) <= 1 per experiment
```

One experiment means one observation-producing ShellBeam execution.

If setup is needed, it is either:

- included inside the project command/script used by that execution; or
- performed beforehand as an ordinary operation outside the experiment.

A retry creates a new experiment:

```text
E1 = attempt 1
E2 = attempt 2
E3 = attempt 3
```

rather than appending repeated executions to E1.

Multi-operation experiment slots/roles/aggregation are deferred to V2.

## 12. Prediction binding and preregistration

```text
PredictionBinding {
  prediction_id
  episode_id
  experiment_id
  candidate_id

  role:
    REQUIRED_PREDICTION
    DISCRIMINATOR
    OBSERVATION_TARGET

  predicate

  source_generation
  committed_at
}
```

All prediction bindings are immutable.

`ExperimentSeal` freezes the prediction set. After seal:

```text
NO new prediction
NO prediction edit
NO prediction delete
```

The hard temporal invariant is:

> **Every prediction participating in protocol evaluation is durably committed before the experiment becomes observable.**

If a model changes the candidate/prediction after seal, it creates a new candidate revision and/or a new experiment. It never rewrites historical preregistration.

## 13. Typed observation predicate contract

V1 has exactly four predicate kinds:

```text
OPERATION_OUTCOME
STRUCTURED_TEST_STATUS
STRUCTURED_DIAGNOSTIC_PRESENCE
VERIFICATION_RESULT
```

There is no expression language.

V1 explicitly forbids:

```text
json_path
arbitrary field selectors
regex
substring/fuzzy matching
AND / OR / NOT
nested conditions
count expressions
arbitrary comparators
workflow sequencing
```

A new fact family requires a new versioned predicate kind.

### 13.1 Operation outcome

```text
OperationOutcomePredicate {
  expected_outcome:
    SUCCESS
    FAILURE
    TIMEOUT
    KILLED
}
```

Truth comes from canonical terminal receipt outcome, never shell output prose.

Non-terminal execution produces `NOT_EVALUATED`. Canonically uncertain terminal outcomes produce `INDETERMINATE`, not forced mismatch.

### 13.2 Structured test status

```text
StructuredTestStatusPredicate {
  target:
    TEST_CASE | TEST_SUITE

  package?
  name

  expected_status:
    PASS | FAIL | SKIP | ERROR
}
```

Selectors are exact-match only.

A matching mechanical structured result compares actual status to expected status.

No matching exact test means `NOT_EVALUATED`, even if parsing is otherwise complete. “Test absent from result” is not “test ran and failed.”

Ambiguous cardinality yields `INDETERMINATE`; ShellBeam never chooses the favorable record.

### 13.3 Structured diagnostic presence

```text
StructuredDiagnosticPresencePredicate {
  code
  severity?

  expected:
    PRESENT | ABSENT
}
```

`code` is mandatory. Message regex/prose matching is forbidden in V1.

Absence may be determinately established only under complete structured derivation. Partial, malformed, unavailable, budget-exceeded, or insufficiently detailed derivation makes absence `INDETERMINATE`.

### 13.4 Verification result

```text
VerificationResultPredicate {
  verification_kind
  provider_class
  project_command_id?

  expected_result:
    PASS
    FAIL
    INCOMPLETE
    AMBIGUOUS
}
```

Determinate evaluation requires qualifying current evidence with known provider semantics, authority at least mechanical, baseline source generation, and valid contract/provider binding.

Advisory, stale, unknown-provider, or otherwise unqualified evidence yields `INDETERMINATE`.

The evaluator considers the **complete compatible qualified verification set attributable to the linked experiment operation at the frozen derivation cut**. Attribution follows canonical verification provenance for that operation and its qualified verification contract/attempt relationship; it is never an arbitrary workspace-wide historical search. It MUST NOT import unrelated old favorable evidence or select one favorable `EvidenceCandidate` while ignoring contradictory qualifying evidence within the attributable set.

Materially conflicting qualifying verification results yield:

```text
INDETERMINATE
reason = conflicting_verification_results
```

Trace/code-intelligence facts remain reasoning inputs in V1 and do not produce hard protocol prediction outcomes until a separately qualified predicate contract exists.

## 14. Prediction evaluation taxonomy

```text
PredictionEvaluationStatus:
  NOT_EVALUATED
  MATCH
  MISMATCH
  INDETERMINATE
```

Meaning:

```text
NOT_EVALUATED
= observation needed to test the prediction never occurred

MATCH
= attributable qualified observation determinately matched

MISMATCH
= attributable qualified observation determinately contradicted expected value

INDETERMINATE
= an attributable observation attempt exists but machine truth cannot safely resolve it
```

A `REQUIRED_PREDICTION` has an implicit contract consequence only when determinately mismatched:

```text
REQUIRED_PREDICTION + MISMATCH
→ DECLARED_REQUIRED_PREDICTION_MISMATCH
→ candidate-contract blocker
```

But:

```text
REQUIRED + NOT_EVALUATED
→ no implicit blocker

REQUIRED + INDETERMINATE
→ no implicit blocker
```

Whether enough predictions were actually evaluated is owned exclusively by the explicit `PREDICTION_EVALUATION` policy requirement.

ShellBeam never promotes this to “candidate false.”

## 15. Observation dimension and potential discrimination

ShellBeam derives an `ObservationDimensionKey` from:

```text
predicate kind
+
selector
+
predicate contract version
```

and excludes expected value.

Two candidate predictions can be potentially discriminating only when they concern the same dimension and declare different predicted outcome classes.

For example:

```text
A: TestRace expected PASS
B: TestRace expected FAIL
```

shares one dimension and differs in predicted outcome.

But:

```text
A: TestRace PASS
B: TestOther FAIL
```

is not treated as mechanically discriminating.

## 16. `ExperimentSeal` and seal-time discrimination cut

Conceptually:

```text
ExperimentSeal {
  experiment_id

  source_generation
  sealed_prediction_digest

  base_projection_cut_ref
  base_candidate_projection_digest

  potential_discrimination_pairs[] {
    target_candidate_id
    challenger_candidate_id
    dimension_key
  }

  sealed_at
}
```

A pair qualifies at seal only if:

- target and challenger are different candidate lineages;
- challenger is ACTIVE and not superseded;
- both are on the same canonical source generation;
- challenger has no candidate-contract blocker at the seal-time cut;
- both bind the same observation dimension;
- predicted outcome classes differ.

This eligibility is frozen at seal. If the experiment itself later produces a mismatch that blocks the challenger, that challenger remains a valid pre-observation challenger for this experiment.

A later experiment cannot use that now-blocked candidate as a fresh qualifying challenger.

The base projection cut must be replayable/reconstructable, not merely identified by an opaque digest, because realized discrimination must be isolated from concurrent episode changes.

### 16.1 Authoritative projection cut reference

V1 defines the conceptual cut as:

```text
DecisionProjectionCutRef {
  episode_id
  canonical_record_high_water
}
```

`canonical_record_high_water` names the authoritative Decision Protocol canonical-ledger prefix from which the participant projection can be replayed. Every canonical Decision Protocol record participating in projection has a durable ordering identity at or below that high-water.

The cut MUST NOT point to the event journal, event cursor, or another rebuildable/materialized projection. Those may expose changes, but they are not replay authority for Decision Protocol canonical truth.

`ExperimentSeal` stores both the replayable cut reference and `base_candidate_projection_digest`; the digest detects semantic mismatch while the cut supplies the authoritative replay domain.

## 17. Server-derived observation domain

A caller does not curate protocol-strength facts.

Once an operation is admission-linked to a sealed experiment:

```text
ExperimentExecutionLink
        ↓
complete attributable protocol-relevant machine truth
        ↓
server evaluation
        ↓
ExperimentObservationBinding
```

The invariant is:

> **Linked experiment execution creates an evaluation domain, not a caller-selected evidence list. All protocol-relevant attributable facts in that domain participate according to the typed expectation contract.**

Therefore the system prevents both:

```text
import unrelated old favorable evidence
```

and:

```text
hide unfavorable linked evidence
```

Any future `ObservationAnnotation` is advisory/explanatory metadata only and cannot alter the evaluation truth set.

## 18. `ExperimentObservationBinding`

The canonical binding is an evaluation bundle, not a list of selected facts:

```text
ExperimentObservationBinding {
  schema_version

  binding_id
  experiment_id
  operation_id

  source_generation

  observation_semantics_version
  derivation_cut_digest

  prediction_results[] {
    prediction_id

    status:
      NOT_EVALUATED
      MATCH
      MISMATCH
      INDETERMINATE

    reason_code?
    basis_refs[]
  }

  materialized_at
}
```

The server derives `prediction_results[]` and `basis_refs[]`.

There is exactly one result for every sealed prediction binding.

The caller cannot submit or omit prediction outcomes.

V1 additionally enforces:

```text
count(ExperimentObservationBinding) <= 1 per experiment
```

### 18.1 Derivation cut

Conceptually:

```text
derivation_cut_digest = hash(
  terminal receipt identity/result
  + relevant structured derivation key/version/config/completeness
  + complete compatible qualified verification evidence identities
  + observation semantics version
)
```

Equivalent immutable machine cut and semantics produce equivalent observation binding.

For `VERIFICATION_RESULT`, the verification identities included in this digest are the complete compatible qualified facts attributable to the linked operation under the frozen cut. Unrelated historical workspace evidence is outside the experiment observation domain even when it happens to share verification kind/provider class.

### 18.2 Observation materialization uniqueness

Observation materialization is a per-experiment uniqueness/CAS boundary. Concurrent materializers behave as:

```text
same experiment + same semantic derivation cut
→ replay the same durable ExperimentObservationBinding

same experiment + different semantic derivation cut
→ EXPERIMENT_OBSERVATION_BINDING_CONFLICT
```

“Same semantic derivation cut” requires the same linked operation identity, observation semantics version, derivation-cut digest, and normalized complete prediction result set. A different literal materialization attempt ID does not create a second canonical binding.

`experiment.close` and post-link abort settlement MUST reuse this same canonical binding path. Neither may create an alternative binding after another path has won uniqueness.

The binding may be lazily materialized, but before a selection commit relies on it the durable binding must exist.

## 19. Observation settling, close, and abort

### 19.1 `experiment.close`

`experiment.close(E1)` never runs a command.

It requires:

```text
linked operation terminal
+
relevant observation derivations at a terminal cut
```

then atomically persists or reuses:

```text
unique ExperimentObservationBinding
+
ExperimentClosure { observation_binding_id = that binding }
```

If the unique binding already exists for the same semantic derivation cut, close reuses it. A conflicting binding cut fails closed rather than replacing historical truth.

Unavailable/partial terminal provider state may still close with corresponding `INDETERMINATE` outcomes.

A still-processing provider yields `OBSERVATION_NOT_SETTLED`.

### 19.2 Abort before execution

Abort before any execution link creates `ExperimentAbort` and no observation domain exists.

### 19.3 Abort after execution link

Abort never erases attributable machine truth.

After an execution link exists:

```text
ABORTED
≠ observation settled
```

The experiment may project:

```text
experiment_state: ABORTED
observation_state: SETTLING
```

and later, once derivations reach a terminal cut:

```text
unique ExperimentObservationBinding
experiment_state: ABORTED
observation_state: SETTLED
```

Post-abort settlement uses the same per-experiment materialization uniqueness boundary as normal close.

Any deterministically derivable `REQUIRED_PREDICTION` mismatch remains a candidate-contract blocker.

Before selection commit, **every experiment with an execution link requires a terminal `ExperimentObservationBinding`**, whether the experiment is CLOSED or ABORTED.

This prevents abort from racing a delayed parser/evidence derivation and hiding a later unfavorable result.

`close_unresolved` remains available if observations cannot settle.

## 20. Realized discrimination

Potential discrimination is frozen at seal time.

Realized discrimination is experiment-local.

It SHALL NOT compare seal-time state with the later live episode projection after unrelated experiments have modified candidates.

Instead:

```text
seal-time replayable participant projection cut
+
this experiment's terminal ObservationBinding only
↓
local post-observation participant projection
```

Realized discrimination is true only when that local application changes the candidate expectation/contract outcome partition for a seal-qualified participant pair.

The calculation excludes:

- the `DISCRIMINATION` requirement's own status; and
- aggregate protocol-gate status.

This avoids circular self-proof and cross-experiment credit leakage.

## 21. Decision Policy

`DecisionPolicy` is deliberately boring declarative protocol data.

V1 requirement composition is implicit top-level AND only.

No nesting, OR/NOT, dependencies, sequencing, retries, conditions, or actions exist.

### 21.1 Requirement envelope

```text
DecisionRequirement {
  requirement_id

  kind:
    CANDIDATE_CHALLENGE
    PREDICTION_EVALUATION
    DISCRIMINATION
    VERIFIER_ASSESSMENT

  // exactly one matching typed payload
}
```

Validation:

```text
requirements <= 8
unique requirement_id
at most one requirement per kind in V1
exactly one typed payload
```

## 22. Candidate challenge requirement

```text
CandidateChallengeRequirement {
  minimum_distinct_lineages: 2
}
```

The requirement counts distinct declared revision-lineage roots.

The challenger does not need to remain active at commit. A good experiment may eliminate it and still demonstrate that the surviving candidate was challenged by a declared alternative.

The guarantee is intentionally weak:

> multiple alternative candidate lineages were declared.

It does not prove semantic quality, diversity, or independent reasoning.

## 23. Prediction evaluation requirement

```text
PredictionEvaluationRequirement {
  minimum_evaluated_predictions: 1

  roles:
    - REQUIRED_PREDICTION
    - DISCRIMINATOR
}
```

Evaluation is candidate-scoped:

```text
evaluate(episode_id, candidate_id)
```

not episode-global “selected candidate” inference.

A prediction counts as evaluated only when preregistered, execution-linked, server-attributable, and determinately `MATCH` or `MISMATCH`.

`NOT_EVALUATED` and `INDETERMINATE` do not satisfy the count.

This requirement asks whether the candidate touched machine reality enough; it does not ask whether its predictions were correct.

## 24. Discrimination requirement

```text
DiscriminationRequirement {
  minimum_qualifying_experiments: 1

  required_outcome:
    ATTEMPTED
    REALIZED
}
```

`ATTEMPTED` is satisfied by a seal-qualified discriminating experiment that was execution-linked, reached terminal observation evaluation, and closed.

`REALIZED` requires experiment-local realized discrimination as defined above.

General policy should prefer `ATTEMPTED`; `REALIZED` is a high-assurance floor and may legitimately remain unsatisfied when reality does not distinguish two options.

ShellBeam never chooses which experiment to run.

## 25. Verifier assessment requirement

`VerifierAssessment` is semantic preference metadata, not machine evidence.

Conceptual record:

```text
VerifierAssessment {
  assessment_id
  episode_id
  actor_ref

  declared_context_class:
    SAME_CONTEXT
    INDEPENDENT_CONTEXT
    INDEPENDENT_SAMPLE
    INDEPENDENT_MODEL
    HUMAN
    UNKNOWN

  qualified_context_class?:
    SAME_CONTEXT
    INDEPENDENT_CONTEXT
    INDEPENDENT_SAMPLE
    INDEPENDENT_MODEL
    HUMAN

  context_qualification? {
    provider_id
    provider_version
    capability_version
    qualification_cut_digest?
    qualified_at
  }

  declared_provider_identity?

  preferred_candidates[]
  semantic_rejections[]
  rationale

  created_at
}
```

`declared_context_class` and `declared_provider_identity` are caller-declared provenance only and have zero hard-gate authority by themselves.

When `decision.assessment.record` is admitted, a trusted transport/provider MAY mechanically qualify the historical context property. Only the server/provider may set `qualified_context_class` and `context_qualification`; caller input cannot populate those fields. The qualification is materialized into the immutable assessment at admission time rather than revalidated later as a changing current permission.

`qualified_context_class` is present if and only if `context_qualification` is present and valid. If no trusted qualifier exists or qualification is unknown/unavailable, both remain absent and the assessment remains a valid declaration without qualified context authority.

Policy:

```text
VerifierAssessmentRequirement {
  minimum_supporting_assessments: 1
  required_context_class?
  distinct_actor_refs: false
}
```

An assessment supports candidate B only if it is valid for the episode, B is in `preferred_candidates`, and any `required_context_class` exactly matches `qualified_context_class`.

Example:

```text
required_context_class = INDEPENDENT_MODEL
qualified_context_class = absent
→ INDETERMINATE

caller declares HUMAN
qualified_context_class = absent
→ never trusted as HUMAN
```

When `required_context_class` is absent, the requirement does not infer independence from the declaration; it simply does not impose a context-class floor.

Requirement folding is deterministic:

```text
qualified supporting count >= minimum_supporting_assessments
→ SATISFIED

below minimum
+ at least one otherwise-supporting assessment whose required context qualification is unresolved/absent
→ INDETERMINATE

below minimum
+ no unresolved required-context qualification that could change the count
→ UNSATISFIED
```

Thus zero assessments is `UNSATISFIED`; an assessment determinately qualified as the wrong context class does not count; and an otherwise-supporting assessment with unavailable qualification prevents a false determinate rejection.

No confidence threshold, majority voting, or aggregate score exists in V1.

Unknown/unqualified required provenance yields `INDETERMINATE` only when it can still affect whether the minimum is met; it never silently downgrades the context requirement.

## 26. Requirement and candidate-contract statuses

```text
DecisionRequirementStatus:
  SATISFIED
  UNSATISFIED
  INDETERMINATE
```

`FAILED` is intentionally absent; these are protocol conditions, not test results.

Each evaluation is candidate-scoped:

```text
DecisionRequirementEvaluation {
  requirement_id
  kind
  status
  basis_refs[]
  reason_code
}
```

The separate candidate contract layer may contain blockers such as:

```text
DECLARED_REQUIRED_PREDICTION_MISMATCH
```

A determinately mismatched REQUIRED prediction is a blocker because the caller declared the role before observability, not because ShellBeam claims the hypothesis is false.

## 27. Protocol gate

Candidate-scoped gate evaluation:

```text
DecisionProtocolEvaluation {
  episode_id
  candidate_id

  requirement_evaluations[]
  candidate_contract_blockers[]

  gate
  blocking_requirement_digest
}
```

Fold:

```text
if any requirement UNSATISFIED
   or candidate-contract blocker:
       BLOCKED
else if any requirement INDETERMINATE:
       INDETERMINATE
else:
       CLEAR
```

Precedence:

```text
BLOCKED > INDETERMINATE > CLEAR
```

`blocking_requirement_digest` normalizes the exact current set of:

- unsatisfied requirements;
- indeterminate requirements; and
- candidate-contract blockers

that an override would need to cover.

Literal interchangeable audit IDs are not semantic digest material when they represent equivalent facts.

## 28. Eligibility, protocol gate, and commit preconditions are distinct

V1 keeps three domains separate.

### 28.1 Candidate/episode eligibility

Examples:

- source generation compatible;
- candidate active/not superseded;
- canonical state structurally valid.

### 28.2 Protocol gate

Examples:

- candidate challenge;
- prediction evaluation;
- discrimination;
- verifier assessment;
- candidate-contract blockers.

### 28.3 Commit-attempt preconditions

Examples:

- expected policy digest matches;
- expected projection digest matches;
- episode still OPEN;
- override authority currently qualified;
- override blocker scope still current.

A CAS mismatch is a request precondition failure, not an enduring episode state.

## 29. Budget ceiling

Budget admission is separate from the protocol gate.

```text
DecisionBudget {
  max_experiments_started?
  max_linked_operations?
  max_machine_wall_ms?
}
```

V1 intentionally excludes token cost, LLM dollars, provider-estimated future cost, and expected-value scoring because ShellBeam does not have reliable machine authority for them.

Semantics:

```text
budget exhausted + protocol CLEAR
→ commit still allowed

budget exhausted + protocol BLOCKED/INDETERMINATE
→ no new protocol-governed machine work
→ close_unresolved or authorized override remain possible
```

Budget exhaustion never satisfies a protocol requirement.

A strict `max_machine_wall_ms` claim requires execution admission to bound a linked operation by remaining enforceable wall budget. Otherwise implementation must label the quality honestly, e.g. observed ceiling rather than hard maximum.

## 30. Decision projection and digests

`DecisionProjection` is a pure deterministic view over canonical Decision Protocol records plus current machine truth/verification state required by the contract.

It exposes state/constraints and allowed protocol transitions, never a recommended engineering next action.

### 30.1 Semantic projection digest

`DecisionProjectionDigest` hashes normalized epistemic/protocol state, including:

- active/superseded candidate topology;
- expectation outcome classes;
- candidate eligibility;
- unresolved dimensions;
- blocking obligation/requirement statuses;
- protocol-gate status;
- normalized verifier semantic state, including preferred/rejected candidate semantics and qualified context classes relevant to the episode;
- source compatibility.

A newly admitted semantically distinct `VerifierAssessment` may therefore change `DecisionProjectionDigest` even when the before/after protocol gate is both `CLEAR`. This intentionally invalidates a stale selection CAS when reasoning-visible verifier state changed.

It excludes literal operation/evidence/assessment IDs when replacing them with equivalent facts leaves epistemic state unchanged.

### 30.2 Audit digest

`DecisionAuditDigest` includes exact historical identity, canonical ordering identity, and references.

Therefore repeated useless experiments may produce:

```text
audit A1 → A2 → A3
projection P7 == P7 == P7
```

This enables mechanical plateau observation without pretending a model score measures progress.

Decision Protocol may expose plateau signals/projection stability, but it does not recommend “generate another hypothesis” or any other reasoning action.

## 31. Source-generation integrity

V1 is fail-closed across canonical source-generation drift.

If an episode binds source generation G7 and canonical source becomes G8 before selection commit:

```text
selection_admissible = false
reason = STALE_EPISODE_SOURCE_GENERATION
```

This is not protocol-overrideable.

The caller creates a new episode on G8.

Decision Protocol does not implement rebase/replay semantics for candidates before Mutation Transaction exists.

## 32. Selection proposal

```text
SelectionProposal {
  proposal_id
  episode_id
  candidate_id

  actor_ref
  rationale?
  created_at
}
```

A proposal has zero transition authority.

A model may record preference while the protocol is blocked or indeterminate.

This preserves analytics such as:

```text
model preferred A
protocol prevented commit
later model preferred B
```

## 33. Selection commit CAS and idempotency

`decision.commit_selection` conceptually requires:

```text
episode_id
candidate_id

expected_policy_digest
expected_projection_digest

override_ref?
idempotency_key
```

`expected_projection_digest` is the semantic CAS token.

Audit-only writes such as an equivalent proposal do not create false conflicts when epistemic state is unchanged.

Server flow:

```text
load episode
require OPEN
recompute current candidate-scoped projection
require exact policy digest
require exact projection digest
require source compatibility
require candidate active
require all linked experiment observations settled/materialized
apply protocol gate
if gate not CLEAR:
  require exact admissible override
atomically append SelectionCommit
```

CAS drift yields `PROJECTION_CONFLICT` and the caller must re-inspect.

Canonical commit:

```text
SelectionCommit {
  commit_id

  episode_id
  candidate_id

  policy_digest
  projection_digest
  source_generation

  override_ref?
  override_authorization?

  idempotency_key
  semantic_intent_fingerprint

  committed_by_actor_ref
  committed_at
}
```

`semantic_intent_fingerprint` is the normalized fingerprint of the effective commit intent and MUST cover at least:

```text
episode_id
candidate_id
committed_by_actor_ref
policy_digest
projection_digest
source_generation
override status + override_ref
```

Normal and override commit intents therefore never share the same semantic fingerprint merely because they select the same candidate.

The store atomically enforces the episode terminal transition **and** the durable idempotency mapping. The mapping is recovery authority, not process memory. No durable terminal commit may exist without its idempotency identity, and no successful idempotency mapping may point to a non-durable terminal result.

Idempotency rules:

```text
same idempotency key + same semantic intent fingerprint
→ replay same durable result

same idempotency key + different semantic intent fingerprint
→ IDEMPOTENCY_CONFLICT
```

After daemon restart, the same key/fingerprint replays the durable commit without re-running terminal authorization. A different fingerprint under the same key conflicts before creating another terminal record.

If another request already terminally committed the episode:

- exact same semantic commit may return durable `ALREADY_COMMITTED` success;
- different candidate, projection, policy, or override epistemic status returns `TERMINAL_SELECTION_CONFLICT`.

Normal and override commits to the same candidate are semantically different.

## 34. Close unresolved

```text
DecisionClosure {
  episode_id
  kind: UNRESOLVED

  reason
  unresolved_dimensions[]

  actor_ref
  projection_digest
  closed_at
}
```

`close_unresolved` is structurally available while the episode is OPEN.

It participates in the same atomic terminal-transition boundary as selection commit.

Race:

```text
commit_selection(B)
vs
close_unresolved()
```

has exactly one winner. The loser receives `EPISODE_TERMINAL_CONFLICT`.

“Always available” means a truthful option before terminalization, not a bypass around atomic terminal state.

## 35. Override policy

```text
OverridePolicy {
  allowed: true | false

  required_authority_class? {
    domain
    class_id
    version
  }
}
```

Validation is closed:

```text
allowed = true
→ required_authority_class REQUIRED

allowed = false
→ required_authority_class MUST be absent
```

V1 uses a whole-policy override rule. There is no per-requirement override matrix.

If override is allowed, it may cover protocol requirements and candidate-contract blockers only.

It never covers:

- stale source generation;
- policy/projection CAS conflict;
- terminal episode;
- superseded candidate;
- corrupt canonical state;
- invalid/unqualified authority;
- other structural integrity/precondition failures.

## 36. Authority domain model

Decision Protocol defines:

```text
DecisionActorAuthority
DecisionAuthorityAttestation
```

but V1 defines **no ordered authority lattice**.

`DerivationAuthority` (`advisory < mechanical < authoritative`) belongs to machine/evidence derivation and MUST NOT be reused for callers/actors.

Authority-class matching in V1 is exact:

```text
required domain/id/version
==
qualified attested domain/id/version
```

No implicit:

```text
owner > maintainer
admin ~= owner
prefix/wildcard equivalence
```

A future hierarchy requires a separately reviewed, versioned authority-relation contract.

## 37. `DecisionAuthorityAttestation`

```text
DecisionAuthorityAttestation {
  schema_version
  attestation_id

  actor_ref

  authority_class {
    domain
    class_id
    version
  }

  scope {
    repository_id
    episode_id?
    action_kind
  }

  resolver {
    provider_id
    provider_version
    capability_version
  }

  issued_at
  expires_at?

  provenance_ref
}
```

The attestation is immutable capability proof:

> actor X can exercise authority class Y in scope S, subject to current resolver qualification.

It is not override intent.

### 37.1 Trusted attestation materialization

Caller/model input MUST NOT create a canonical `DecisionAuthorityAttestation` body or assert its qualification result.

V1 exposes a provider-bound semantic path conceptually:

```text
decision.authority.materialize {
  actor_ref
  requested_authority_class
  requested_scope
}
        ↓
qualified authority resolver/provider
        ↓
MaterializeDecisionAuthorityQualification
        ↓ status = QUALIFIED only
server materializes immutable DecisionAuthorityAttestation
```

The materialization-time provider result is bounded separately from commit-time requalification because no `attestation_id` exists yet:

```text
MaterializeDecisionAuthorityQualification {
  status:
    QUALIFIED
    EXPIRED
    REVOKED
    SCOPE_MISMATCH
    CLASS_MISMATCH
    UNKNOWN
    UNAVAILABLE

  actor_ref?
  authority_class?
  scope? {
    repository_id
    episode_id?
    action_kind
  }

  resolver {
    provider_id
    provider_version
    capability_version
  }

  validated_at
  expires_at?
  qualification_cut_digest?
  provenance_ref?
}
```

Only `QUALIFIED` with complete valid actor/class/scope/resolver/provenance material may create the canonical attestation. The caller may request which actor/class/scope it needs proven, but the resolver/provider is authoritative for the returned actor, exact class, scope, provider contract, qualification cut, expiry, and provenance used to materialize the canonical attestation.

Results such as:

```text
EXPIRED
REVOKED
SCOPE_MISMATCH
CLASS_MISMATCH
UNKNOWN
UNAVAILABLE
```

MUST NOT produce a usable canonical attestation. An implementation may retain bounded audit diagnostics for the failed materialization attempt, but those diagnostics have no transition authority.

Materialization is not a substitute for commit-time authorization. `decision.selection.commit` still requalifies the referenced attestation at the terminal authorization point as specified below.

## 38. No generic V1 attestation-revocation record

Core V1 SHALL NOT add `DecisionAuthorityAttestationRevocation`.

The qualified resolver/provider owns current qualification state.

Current disqualification may arise from:

- provider-controlled revocation;
- external approval/identity state;
- expiry;
- invalid scope;
- provider disappearance/unavailability;
- provider-specific qualification rules.

If ShellBeam later becomes the issuer of an authority class, a local revocation ledger may exist as a provider implementation detail or separately reviewed extension. It is not required in generic core V1.

This avoids pretending ShellBeam owns external identity/repository authorization it cannot prove.

## 39. Authority qualification contract

Conceptually:

```text
QualifyDecisionAuthority {
  attestation_id

  expected_actor_ref
  required_authority_class

  required_scope {
    repository_id
    episode_id
    action_kind: COMMIT_SELECTION_OVERRIDE
  }
}
```

Bounded result:

```text
DecisionAuthorityQualification {
  status:
    QUALIFIED
    EXPIRED
    REVOKED
    SCOPE_MISMATCH
    CLASS_MISMATCH
    UNKNOWN
    UNAVAILABLE

  attestation_id
  authority_class?
  actor_ref?

  resolver {
    provider_id
    provider_version
    capability_version
  }

  validated_at
  qualification_cut_digest?
}
```

There is no prose parsing, confidence score, fuzzy role mapping, or “probably owner.”

Only `QUALIFIED` has transition authority.

All other statuses fail closed for a new override commit.

## 40. Override intent

```text
DecisionOverride {
  override_id

  episode_id
  candidate_id

  policy_digest
  projection_digest

  blocking_requirement_digest
  blocking_requirements[]

  actor_ref
  authority_attestation_ref

  reason
  created_at
}
```

`DecisionOverride` proves explicit intent to bypass the exact blockers for the exact candidate/projection context.

It does not cache future authorization and does not state that the attestation remains valid forever.

If the blocker set changes from K7 to K8 before commit:

```text
OVERRIDE_SCOPE_STALE
```

The caller must create a new override intent for the current blocker set.

## 41. Revalidate authority at commit authorization point

Override authority MUST be requalified at the terminal commit authorization point.

Flow:

```text
load override
verify exact episode/candidate/policy/projection/blocking digest
↓
resolve authority_attestation_ref NOW
↓
require exact actor/class/scope
↓
require status QUALIFIED
↓
only then authorize local terminal commit
```

If qualification is:

```text
EXPIRED
REVOKED
SCOPE_MISMATCH
CLASS_MISMATCH
UNKNOWN
UNAVAILABLE
```

commit returns `OVERRIDE_AUTHORITY_NOT_ADMISSIBLE`.

This is a commit precondition failure, not protocol-gate status, and is not itself overrideable.

## 42. Authorization linearization and external revocation race

ShellBeam does not claim a distributed transaction with GitHub, Jira, identity systems, or other external resolvers.

The successful resolver qualification used by `commit_selection` is the override authorization point.

If resolver returns `QUALIFIED` at `Tauth`, ShellBeam may persist the local terminal commit against that qualification cut.

External revocation after `Tauth` does not retroactively invalidate the historical local commit.

If local persistence fails and no durable `SelectionCommit` exists, a retry MUST revalidate current authority.

The sole exception is an idempotent replay of an already durable successful commit, which replays historical success rather than re-authorizing history.

## 43. Persisted override authorization cut

An override `SelectionCommit` retains:

```text
override_authorization {
  authority_attestation_ref

  authority_class {
    domain
    class_id
    version
  }

  actor_ref

  resolver {
    provider_id
    provider_version
    capability_version
  }

  validated_at
  qualification_cut_digest?
}
```

Historical audit can therefore answer:

- which attestation authorized the commit;
- which exact authority class was qualified;
- which actor it covered;
- which resolver contract performed qualification;
- when qualification occurred;
- which qualification cut was used.

Historical interpretation does not depend on the external provider still being online.

## 44. Expiry/revocation after commit

Historical truth and current authority projection are separate.

Example:

```text
10:00 attestation QUALIFIED
10:01 override SelectionCommit durable
11:00 attestation expires
```

At 11:00:

```text
selection_state = COMMITTED_WITH_OVERRIDE
historical authorization = qualified_at_commit
current attestation status = EXPIRED
```

These facts do not conflict.

Expiry/revocation after durable commit never rewrites terminal history.

## 45. Scope matching

V1 scope fields are typed and exact.

An attestation scoped to:

```text
repository_id = repo_A
episode_id = D42
action_kind = COMMIT_SELECTION_OVERRIDE
```

does not authorize repo_B, D43, policy activation, mutation authorization, or another action kind.

If `episode_id` is omitted, the resolver contract may qualify repository-wide authority, but ShellBeam still requires repository and action-kind compatibility.

No wildcard strings such as `repo_*` or `decision.*` exist in V1.

## 46. Override race semantics

Normal and override terminalization share the same per-episode terminal boundary.

Race:

```text
R1 normal commit B
R2 override commit B
```

If R1 wins, R2 returns `TERMINAL_SELECTION_CONFLICT`.

If R2 wins, R1 returns `TERMINAL_SELECTION_CONFLICT`.

They do not merge merely because candidate identity matches.

Invariant:

```text
same candidate
≠ same epistemic selection status
```

`COMMITTED_WITH_OVERRIDE` never projects as protocol-satisfied merely because the override was authorized.

## 47. Model-facing operations

Exact transport spelling may follow existing ShellBeam API conventions, but V1 semantic operations are bounded to these families.

### Policy

```text
decision.policy.snapshot
decision.policy.activate
```

### Episode

```text
decision.create
decision.inspect
decision.evaluate
decision.close_unresolved
```

`inspect/evaluate` are pure projection operations.

### Candidate

```text
decision.candidate.create
decision.candidate.revise
```

### Experiment

```text
decision.experiment.define
decision.prediction.bind
decision.experiment.seal

normal execution start with experiment binding

decision.experiment.close
decision.experiment.abort
```

Observation binding is server-derived. No caller API accepts caller-supplied `prediction_results[]`.

### Assessment/selection

```text
decision.assessment.record
decision.selection.propose
decision.override.create
decision.selection.commit
```

### Authority

```text
decision.authority.materialize
```

This operation requests provider-backed qualification and server materialization of an attestation; it does not accept a caller-authored attestation body. Authority qualification is also invoked again by the selection-commit path. Neither path is a generic model instruction to self-assert a role.

## 48. Model-facing projection

A bounded projection should expose at least:

```text
DecisionProjection {
  episode_id
  episode_state
  episode_kind

  policy_binding
  source_generation_compatibility

  candidates[] {
    candidate_id
    lineage_root
    active/superseded
    candidate_contract_status
    expectation_summary
  }

  experiments[] {
    experiment_id
    lifecycle_state
    observation_state
    potential_discrimination
    realized_discrimination
  }

  evaluation_for_candidate? {
    candidate_id
    requirement_evaluations[]
    candidate_contract_blockers[]
    protocol_gate
    blocking_requirement_digest
  }

  budget_admission
  plateau/projection_stability

  selection_state
  selection_candidate_id?
  committed_with_override?

  projection_digest
  audit_digest

  allowed_protocol_transitions[]
}
```

`allowed_protocol_transitions[]` describes state/protocol capability, not recommended action.

The projection MUST NOT emit:

```text
next_best_action
recommended_experiment
generate_more_hypotheses
choose_candidate_B
```

## 49. Integration with verification semantics

Decision Protocol consumes verification facts but does not replace verification semantics.

Candidate selection remains upstream of implementation correctness:

```text
Decision Protocol
→ protocol-authorized candidate selection
→ mutation / implementation
→ source changes
→ affected surface
→ obligations
→ evidence sufficiency
→ clear | blocked | indeterminate
```

Therefore:

```text
candidate committed
≠ implementation correct
≠ task done
```

Decision Protocol must not create a shortcut from semantic selection to task completion.

## 50. Production vs benchmark/oracle analytics

Production episodes optimize task outcome/cost and may eliminate candidates without fully materializing all losing options.

Therefore true oracle metrics may be unavailable.

ShellBeam MUST NOT infer:

```text
Oracle Pass@N
candidate recall
selection regret
```

unless every required candidate was evaluated under a qualifying equivalent oracle contract.

Benchmark/evaluation tooling may deliberately fully evaluate all candidates to make those metrics available.

When candidates are not exhaustively oracle-evaluated:

```text
oracle_metrics = unavailable
reason = candidates_not_exhaustively_evaluated
```

No interpolation or guess is allowed.

## 51. V1 sequencing and mutation boundary

V1 Decision Protocol is strongest for:

- debugging hypotheses;
- CI/root-cause diagnosis;
- review findings;
- plan/architecture factual assumption checking;
- read-only or minimally invasive probes.

V1 SHALL NOT implement three full mutation candidates concurrently in one workspace.

Mutation-capable candidate materialization waits for the separately designed Mutation Transaction/isolation boundary.

Until then, the safe shape is:

```text
multiple candidate hypotheses
→ preregistered probes
→ machine discrimination
→ one protocol-authorized candidate
→ normal mutation flow
```

## 52. Compatibility and rollout

Decision Protocol is additive.

Existing ShellBeam executions that do not bind a Decision Protocol experiment remain ordinary operations with unchanged semantics.

Repositories without an explicitly activated Decision Policy do not acquire implicit hard selection gates.

An episode exists only when a caller explicitly creates one and ShellBeam can bind the repository's current effective applicable activated policy. The caller cannot choose a weaker historical activation.

V1 rollout SHOULD proceed in layers:

1. canonical records and deterministic projection;
2. policy snapshot/activation and candidate/episode lifecycle;
3. sealed experiment + one-operation admission binding;
4. typed server-derived observation materialization;
5. candidate-scoped requirement/gate evaluation;
6. projection-digest CAS selection and close-unresolved terminalization;
7. verifier assessment provenance;
8. authority resolver interface and override authorization cut;
9. benchmark/analytics only after production semantics are stable.

No layer may weaken existing machine-truth authority or reinterpret advisory facts as hard protocol truth.

## 53. Error/result taxonomy guidance

Exact transport result envelopes follow existing ShellBeam conventions, but V1 needs stable semantic reason codes for at least:

```text
CANDIDATE_REVISION_CONFLICT
EXPERIMENT_ALREADY_SEALED
EXPERIMENT_EXECUTION_LIMIT_REACHED
EXPERIMENT_NOT_SEALED
OBSERVATION_NOT_SETTLED
EXPERIMENT_OBSERVATION_BINDING_CONFLICT
STALE_EPISODE_SOURCE_GENERATION
PROJECTION_CONFLICT
POLICY_CONFLICT
EPISODE_TERMINAL_CONFLICT
TERMINAL_SELECTION_CONFLICT
IDEMPOTENCY_CONFLICT
PROTOCOL_BLOCKED
PROTOCOL_INDETERMINATE
OVERRIDE_SCOPE_STALE
OVERRIDE_AUTHORITY_NOT_ADMISSIBLE
AUTHORITY_REQUIREMENT_UNAVAILABLE
```

Reason codes are machine-facing diagnostics, not recommendations for what engineering action to take next.

## 54. Concurrency invariants

Implementation must preserve these atomicity boundaries:

1. **Candidate replacement CAS:** exactly one concurrent revision may replace an ACTIVE parent.
2. **Experiment seal:** prediction set and seal-time discrimination cut are atomically frozen before an execution can link.
3. **Experiment execution admission:** at most one observation-producing operation link may be created in V1; experiment identity is frozen into first-admission replay identity and the durable link is recovery-indivisible with successful admission before spawn.
4. **Experiment terminal records:** close and abort are mutually exclusive.
5. **Observation materialization:** at most one canonical `ExperimentObservationBinding` exists per experiment; identical semantic cuts replay it and different cuts conflict.
6. **Episode terminalization:** SelectionCommit and DecisionClosure are mutually exclusive and serialized.
7. **Durable commit idempotency:** terminal commit and idempotency key/fingerprint mapping are one atomic recovery boundary.
8. **Normal vs override selection:** same candidate does not collapse distinct epistemic commit semantics.
9. **Projection CAS:** commit is authorized only against the exact current semantic projection requested by the caller.
10. **Override authorization:** qualification and local terminal commit use a clearly defined local authorization point; failed persistence requires requalification on retry.

## 55. Security and authority invariants

1. Caller-declared identity/role is not qualified authority by itself.
2. Caller-declared verifier context class is not qualified verifier provenance by itself.
3. Canonical `DecisionAuthorityAttestation` records have only trusted resolver/provider-backed producers; callers cannot author usable attestation bodies.
4. V1 performs exact authority-class matching only.
5. Unknown/unavailable resolver result fails closed.
6. An attestation proves capability; an override proves bounded intent.
7. Override intent is stale when blocker digest changes.
8. Override never bypasses source/structural/CAS integrity.
9. Historical successful authorization is not reinterpreted under current resolver state.
10. Durable idempotent replay does not re-authorize already committed history.
11. V1 core does not claim ownership of external provider revocation state.

## 56. Frozen V1 invariants

The semantic freeze consists of these invariants:

1. Canonical Decision Protocol truth is append-only; lifecycle is projected.
2. New governed episodes bind the server-resolved current effective applicable policy activation; callers cannot select historical weaker activations.
3. Episode policy snapshot/activation is durably bound and immutable for episode lifetime.
4. Candidate revision is immutable replacement with active-parent CAS.
5. Candidate revision does not implicitly inherit predictions.
6. Prediction declaration happens-before experiment observability.
7. Experiment definition/seal/close never schedules execution.
8. V1 permits at most one observation-producing execution link per experiment.
9. Experiment identity is part of immutable operation replay/observation identity; omitted/changed bindings conflict on the same `operation_id`.
10. Successful experiment-linked admission and durable `ExperimentExecutionLink` are recovery-indivisible before spawn.
11. Protocol observation truth is server-derived, complete for the linked observation domain, and non-cherry-pickable.
12. At most one canonical `ExperimentObservationBinding` exists per experiment; same semantic cut replays, different cut conflicts.
13. Every linked execution must have a terminal observation binding before selection commit, including post-link aborted experiments.
14. Verification-result observation sets are attributable to the linked operation and frozen derivation cut, not arbitrary workspace history.
15. ShellBeam evaluates typed declared expectations, never semantic hypothesis truth.
16. V1 predicate kinds are closed and versioned; there is no generic expression DSL.
17. REQUIRED mismatch is an implicit candidate-contract blocker only when determinately evaluated.
18. Unevaluated or indeterminate REQUIRED prediction does not silently become a blocker.
19. Candidate-scoped policy evaluation uses four bounded V1 requirement kinds: challenge, prediction evaluation, discrimination, verifier assessment.
20. Caller-declared verifier context has no hard-gate authority; exact required context matches only server/provider-qualified context materialized into the assessment.
21. Discrimination qualification is frozen at seal time against then-eligible candidate lineages.
22. Realized discrimination is experiment-local against an authoritative canonical-ledger replay cut and excludes its own aggregate requirement/gate status.
23. Verification predicates evaluate the complete compatible qualified attributable evidence set, not a favorable individual record.
24. Protocol fold is deterministic: BLOCKED > INDETERMINATE > CLEAR.
25. Budget admission is a separate ceiling and never changes protocol satisfaction.
26. Projection digest represents semantic epistemic/protocol state, including normalized verifier semantic state; audit digest represents exact history.
27. `expected_projection_digest` is the semantic CAS token for selection commit.
28. Source-generation mismatch fail-closes V1 selection and cannot be overridden.
29. Selection proposal has no transition authority.
30. Selection commit and close-unresolved share one atomic episode-terminal boundary.
31. Selection commit persists durable idempotency key + semantic intent fingerprint transactionally with terminalization.
32. Normal and override commits are epistemically distinct even for the same candidate.
33. Override never rewrites blockers as satisfied.
34. `OverridePolicy.allowed=true` requires an exact authority class; `allowed=false` forbids one.
35. DecisionAuthorityAttestation is immutable capability proof with only trusted resolver/provider-backed canonical producers; current qualification is resolver-derived.
36. V1 has no authority lattice, wildcard authority equivalence, or generic core revocation ledger.
37. Decision Policy activation authority is `explicit_caller`; Decision Authority Attestation does not authorize policy activation in V1.
38. Override authority is revalidated at terminal commit authorization time.
39. Only `QUALIFIED` exact-class/exact-scope authority may authorize a new override commit.
40. Override intent covers the exact current candidate/policy/projection/blocker set.
41. SelectionCommit persists the exact override authorization cut used to authorize history.
42. Later expiry/revocation does not rewrite a durable historical commit.
43. Failed/non-durable commit retry must revalidate authority; replay of durable idempotent success does not.
44. `VerifierAssessment` is a separate non-evidence ontology.
45. Policy/projection exposes constraints and allowed protocol transitions, never recommended engineering next actions.
46. Candidate committed does not imply implementation verified or task done.
47. Oracle-gap metrics are unavailable unless candidate evaluation is genuinely exhaustive under a qualifying oracle.
48. `CLOSED_UNRESOLVED` remains the truthful terminal escape from an unsatisfied or unresolvable protocol.

## 57. Final architectural statement

Decision Protocol turns ShellBeam from a harness that merely records what an agent did into a harness that can also enforce **how strongly an agent may claim a reasoning outcome has been challenged and grounded**.

It does so without moving creativity or engineering judgment into the daemon:

```text
Model:
  What might be true?

ShellBeam:
  Which declared expectations survived the exact machine truth
  under the activated protocol?

Model:
  Of the admissible options, which should I prefer?

ShellBeam:
  Is that preference protocol-authorized to become a durable commitment
  on this exact source/projection, and if not, what constraints remain?
```

That boundary is the V1 design.
