# ShellBeam Machine Truth Harness Architecture Design

Date: 2026-08-18
Status: semantic architecture freeze approved after review corrections; P1 implementation planning may proceed
Scope: ShellBeam evolution from execution/observation substrate into a specialized coding-harness substrate without moving engineering reasoning into the daemon

## 1. Decision

ShellBeam SHALL evolve as a **Machine Truth Harness** between a reasoning model and the local machine.

The reasoning model remains the sole owner of engineering intent, solution design, diagnosis, planning, task decomposition, trade-off judgment, and final user-facing completion claims. ShellBeam owns bounded engineering-state semantics: exact execution truth, observed mutations, source/workspace identity, mechanically derived affected-surface facts, user-approved engineering policy, verification obligations, evidence sufficiency, authority/resource envelopes, provenance, and provider lifecycle facts.

The architecture SHALL preserve this separation:

```text
Reasoning brain
ChatGPT / Claude / Gemini / future model
        │
        │ intent, judgment, decisions
        ▼
┌──────────────────────────────────────────────────────┐
│                     SHELLBEAM                        │
│                                                      │
│  CORRELATION / ENGINEERING-STATE PROJECTIONS         │
│  Activity -> EngineeringStateView                    │
│                                                      │
│  TRUTH PLANE                                         │
│  execution, receipts, source generations, mutations, │
│  evidence, process/resource/environment facts        │
│                                                      │
│  POLICY PLANE                                        │
│  verification obligations, mutation/resource         │
│  authority, project-approved requirements            │
│                                                      │
│  PROVIDER PLANE                                      │
│  shell, git, language intelligence, patch, browser,   │
│  debugger, build/test, media, future providers        │
└───────────────────────────┬──────────────────────────┘
                            │
                       local machine
```

ShellBeam SHALL NOT become an autonomous coding agent. It SHALL NOT add daemon-side planner, reflection loop, task decomposition, solution memory, autonomous retry strategy, or hidden workflow engine merely because those features could reduce model tool calls.

## 2. Architectural north star

The architecture freezes four guardrails:

1. **ShellBeam owns engineering-state semantics, not engineering reasoning.**
2. **ShellBeam optimizes verification for sufficient evidence, not maximal testing.**
3. **Uncertainty must increase verification conservatism, never decrease it.**
4. **Cost may optimize among sufficient evidence sets; it must never redefine sufficiency.**

These are long-lived constraints, not implementation hints.

A future design that violates one of them requires an explicit architecture revision, not an incidental implementation shortcut.

## 3. Why this layer exists

The current ShellBeam system already solves much of the execution-side harness problem:

- one-tool MCP transport;
- retry-safe operation identity;
- terminal receipts and exact child outcome semantics;
- bounded output and structured execution observations;
- workspace/source provenance;
- evidence records and expected-output observations;
- persistent sessions and process inspection;
- environment/toolchain observation;
- telemetry/reproduction primitives;
- code-intelligence facts;
- advisory mutation scopes;
- checkpoints and dynamic input tracing as bounded/experimental capabilities;
- rich local media;
- capability-gated hard Resource Enforcement now merged on `main`, with explicit platform/maturity boundaries.

The missing leverage is not another generic shell feature. The missing layer is the deterministic connective tissue that lets a model ask, in one bounded view:

```text
What engineering state is mechanically known?
What changed relative to the selected baseline?
What is mechanically affected, with what authority and coverage?
According to approved policy, what verification obligations exist?
Which obligations are required, deferred, not triggered, optional, or waived?
Which evidence is stale, satisfied, failed, inconsistent, unavailable, unknown, or insufficient?
What resources/authority were declared, enforced, and observed?
```

Without this layer the model repeatedly re-joins receipts, Git state, semantic queries, evidence, diagnostics, environment facts, and test history inside its context window. The model becomes an accidental database. ShellBeam should instead provide recomputable, provenance-rich engineering-state projections.

## 4. Retained baseline contracts

This architecture composes with, and does not replace, existing ShellBeam contracts.

The following remain authoritative:

- MCP/tool success is not child success.
- Terminal command success requires durable terminal receipt plus the required spawn/exit/reap/output/input evidence.
- `operation_id` remains exactly-once execution-start intent.
- `session_id`, `activity_id`, `workspace_id`, `repository_id`, `evidence_id`, source references, and source generations retain their existing meanings unless a later versioned migration says otherwise.
- `activity_id` remains correlation, not ownership and not user-task semantics.
- Evidence records bind mechanical execution/artifact/source facts; they do not themselves judge semantic completeness.
- Structured code intelligence is observation, not execution evidence.
- Mutation scopes remain caller declarations/advisories, not locks, permissions, or inferred affected sets.
- Project readiness and typed commands remain deterministic/declarative rather than a workflow DSL.
- Advisory or partial observations are never promoted to stronger authority merely because they are precise or useful.
- Dynamic tracing that is not hermetic may broaden suspicion but cannot prove irrelevance.
- Ordinary compatible execution must remain usable when higher-level engineering-state features are absent or unavailable.

## 5. Authority planes

### 5.1 Truth Plane

The Truth Plane contains facts whose meaning is about what ShellBeam can directly prove or mechanically derive from explicit sources.

Examples:

```text
terminal receipt and child outcome
source/workspace generation
exact source digest when available
observed source mutation
artifact observation
environment/toolchain fingerprint
process/resource observation
semantic provider observation
structured diagnostic/test/build record
evidence record and evidence validity dimensions
checkpoint result
repro capsule
```

Truth Plane records MUST declare authority, provenance, freshness/generation applicability, and coverage/completeness when those dimensions are meaningful.

### 5.2 Policy Plane

The Policy Plane contains user/repository-approved engineering requirements and deterministic derivations from those requirements.

Examples:

```text
verification requirement
phase trigger
mutation authority
resource authority
provider permission
mechanically declared sensitive surface
waiver
accepted evidence class
required evidence authority
required environment
```

A policy fact is not machine truth about the world. It is truth about an approved engineering rule.

ShellBeam MAY derive obligations from declared policy plus mechanically attributable facts. It MUST NOT invent business acceptance targets, product SLOs, concurrency targets, availability targets, security parameters, or other requirements from prose heuristics or filename intuition.

### 5.3 Provider Plane

Providers perform or observe bounded work under ShellBeam contracts.

Examples:

```text
shell runtime
git adapter
language-semantic provider
patch/apply provider
Playwright provider
DAP provider
compiler/test/build provider
artifact/media provider
```

A provider does not create a new authority model. It must publish observations/effects through the common truth/policy/evidence contracts.

## 6. Activity remains the highest correlation primitive

ShellBeam SHALL NOT introduce a durable aggregate root called `Task` or `Change` in the first implementation of this architecture.

`activity_id` remains deliberately semantically weak:

> these records/operations are correlated by the caller as one engineering activity.

It does not mean:

```text
one user task
one solution attempt
one commit
one code change
one final answer
one unit of completion
```

This prevents ShellBeam from paying ontology costs that require understanding intent.

### 6.1 Engineering State is a projection

The first-class model-facing abstraction is an addressable/recomputable **EngineeringStateView**, not a canonical giant state object.

Conceptually:

```text
EngineeringStateView
  activity_id?
  workspace_id
  selected_baseline
  current_source_generation
  observed_change_view
  mechanical_scope
  caller_focus_hints
  affected_surface
  verification_obligations
  evidence_sufficiency
  stale_or_inconsistent_evidence
  diagnostics
  environment/resource summary
  authority summary
  truncation/coverage/provenance
```

The view MUST be materialized from authoritative/mechanical/advisory sources and MUST NOT duplicate them as independent canonical truth.

### 6.2 Future envelope handle

A future `envelope_id` MAY be introduced only as a durable identity + baseline handle when resume/handoff use cases require it. If introduced, its durable core should remain narrow, for example:

```text
EnvelopeIdentity
  envelope_id
  activity_id?
  workspace_id
  base_generation
  created_at
```

Working sets, affected surfaces, obligations, evidence state, diagnostics, and environment remain projections unless later evidence proves a separate durable aggregate is necessary.

## 7. No narrative reasoning state in ShellBeam

ShellBeam MUST NOT persist model-style narrative fields such as:

```text
unresolved: "maybe ResourceLimitKind is missing"
plan: "probably refactor package operation"
next_step: "try changing interface X"
likely_root_cause: "race in cache"
```

ShellBeam MAY expose mechanically grounded open facts instead:

```text
semantic diagnostic: undefined ResourceLimitKind
mandatory obligation: darwin native verification not_run
stale evidence: operation X invalidated by generation Y
provider observation: runtime trace coverage partial
policy gap: declared sensitive surface has no matching approved rule
```

The reasoning model converts those facts into hypotheses and actions.

## 8. Working-set semantics

The architecture distinguishes two things that must never be silently collapsed.

### 8.1 Mechanical scope

Mechanical scope is derived from versioned, attributable providers such as:

- exact observed mutations;
- package/import dependency graph;
- semantic definition/reference/call relationships;
- project-declared path or subsystem relations;
- configuration bindings;
- runtime trace observations with their explicit authority/coverage;
- filesystem relation rules.

Each relation reports its basis, derivation authority, coverage, source generation, and provenance.

### 8.2 Caller focus hints

The reasoning model may declare that certain paths/symbols/subjects are currently relevant.

These are caller-declared hints, not ShellBeam proof that the set is complete or affected.

A model-facing view MUST label them separately so a future model never confuses an old agent judgment with a mechanical dependency fact.

## 9. Verification is evidence semantics, not a test workflow

The verification pipeline is:

```text
Engineering State
      ↓
Affected Surface
      ↓
Policy Match
      ↓
Obligation Disposition
required_now | deferred | optional | not_triggered | waived
      ↓ required_now
Evidence Requirements
      ↓
Admissible Providers
      ↓
Sufficiency Constraints
      ↓
Cost/Resource Optimization
      ↓
Execution / Observation
      ↓
Evidence Status
not_evaluated | satisfied | failed | insufficient
inconsistent | unknown | unavailable
      ↓
Gate Fold
clear | blocked | indeterminate
```

Tests are one evidence provider among many. Static diagnostics, compilers, schema compatibility checks, artifact digests, browser journeys, native platform observations, resource measurements, and debugger observations may also satisfy specific evidence requirements when policy explicitly allows them.

The core verification semantics are defined in the companion design:

- [Affected Surface, Verification Obligations, and Evidence Sufficiency](./2026-08-18-affected-surface-verification-evidence-sufficiency-design.md)

## 10. Mutation semantics

ShellBeam SHOULD own mutation **transaction semantics**, not editing algorithms.

A future mutation transaction binds:

```text
before source identity
expected generation
approved mutation scope / authority
optional checkpoint
provider invocation identity
actual observed effects
after source identity
lineage
stale-evidence invalidation
```

The patch/edit provider may be `apply_patch`, an LSP workspace edit, a language refactor provider, a script, or another bounded mechanism.

The companion design defines engineering-state projections, mutation transactions, and authority envelopes:

- [Engineering State, Mutation Transactions, and Authority Envelopes](./2026-08-18-engineering-state-mutation-authority-design.md)

## 11. Authority envelopes

Long-term ShellBeam execution/provider contracts SHOULD distinguish:

```text
DECLARED
ENFORCED
OBSERVED
```

for relevant authority/resource dimensions.

Example:

```text
network
  declared: denied
  enforced: unsupported
  observed: unknown
```

must never be compressed into:

```text
network: denied
```

Likewise a declared memory budget is not an enforced limit, and an enforced limit is not an observed peak.

This distinction is essential for browsers, debuggers, language servers, test workers, and any hermetic/sandbox boundary.

## 12. Resource governance

Resource governance is a foundation, not a post-hoc optimization.

Verification and providers may declare workload/resource classes and parallel-safety constraints. A Resource Governor decides how much concurrency is currently admissible from observed machine state and enforced budgets.

The desired separation is:

```text
Verification/Provider policy:
  what may run together?

Resource Governor:
  how much may run now?
```

The architecture explicitly rejects both universal sequential execution and unbounded `-j auto`/`pytest -n auto` style execution.

Resource facts that ShellBeam can directly observe are authoritative/mechanical according to provider semantics. Model quota/token/tool cost is not fabricated; it is provider-reported, caller-reported, estimated with explicit authority, or unavailable.

## 13. Provider qualification

A provider SHALL NOT enter a trusted path merely because a package or executable exists.

Provider qualification should cover, as applicable:

- provenance and maintenance;
- version compatibility/stability;
- security/authority boundary;
- resource footprint;
- failure isolation;
- lifecycle/cleanup behavior;
- licensing/distribution constraints;
- exact observation semantics;
- authority and coverage semantics;
- artifact/source lineage;
- bounded output and truncation behavior.

Browser and debugger work should reuse mature ecosystems behind this boundary rather than rebuilding them.

## 14. Provider roadmap

The resource/hermetic foundation contracts are now repository dependencies of this architecture:

- [Resource Enforcement](./2026-08-18-resource-enforcement-design.md) is merged on `main`; its hard-enforcement/platform limits remain authoritative and must be reused rather than redefined here.
- [Hermetic Boundary](./2026-08-18-hermetic-boundary-design.md) is frozen on `main`; provider implementation/qualification remains follow-on work, and only enforcement-backed completion may promote dependency scope authority.

The target provider evolution is:

1. stabilize the Resource Enforcement capability now merged on `main` and implement/qualify the Hermetic Boundary design now pinned on `main`;
2. add the verification-semantic subsystem, including the minimal relation plumbing required by P1;
3. add engineering-state projections/context views;
4. add mutation transaction semantics and a minimal patch provider;
5. improve code-intelligence source UX and broaden semantic providers where justified; this P4 evolution is distinct from the minimal Go relation facts P1 may reuse;
6. qualify a Playwright browser provider behind the common authority/resource/evidence envelope;
7. qualify DAP providers such as Delve/debugpy/js-debug/CodeLLDB behind the same envelope;
8. add selective Git/additional evidence-provider semantics where they materially reduce model-side parsing; reconsider broader memory/envelope roots only after measured need.

Detailed sequencing and acceptance boundaries are in:

- [Machine Truth Harness Delivery Roadmap](./2026-08-18-machine-truth-harness-delivery-roadmap-design.md)

## 15. Explicit non-goals

This architecture does NOT authorize ShellBeam to add:

- autonomous task planning;
- model inference inside the daemon to decide relevance or correctness;
- solution/reflection memory;
- conversation memory;
- hidden multi-agent workers;
- general workflow DSL;
- automatic requirement/NFR invention;
- silent maturity-profile policy changes;
- universal full-suite execution;
- automatic test weakening or rewrite-to-green behavior;
- blind retries;
- a custom browser engine;
- a custom debugger;
- a custom language server/compiler when mature providers exist;
- a shadow source tree solely to make the harness feel editor-like;
- claims of sandbox/hermeticity when enforcement/coverage is incomplete.

## 16. Architecture invariants

The following invariants are normative:

1. A derived projection never outranks its source authority.
2. Every affected relation exposes basis + derivation authority + coverage separately.
3. Unknown/partial affected coverage may widen verification but may never narrow it.
4. Policy gaps are advisory facts; they do not materialize hard requirements automatically.
5. Policy templates are bootstrap inputs only; an explicitly activated materialized policy is the runtime policy authority.
6. No profile/template upgrade silently changes an existing repository's gates.
7. A policy-changing mutation cannot make its proposed policy authoritative for evaluating itself; activation requires separate pre-existing/external authority, and the new policy cannot self-grant activation/waiver authority for that gate.
8. Verification cost optimization executes only after admissibility/sufficiency constraints are met.
9. Obligation disposition and evidence status are independent; in particular, `waived` never means evidence satisfied.
10. A failed check is never erased merely because an identical rerun passed.
11. A verification operation cannot become terminal-complete while retaining undeclared live resources.
12. Declared transferred/persistent resources are explicit ownership transfers, not leaks.
13. Model-facing projections must preserve authority/freshness/coverage rather than flattening uncertainty.
14. ShellBeam never claims the user task is complete; it may report a verification gate as clear/blocked/indeterminate with literal satisfied/waived/blocking/indeterminate counts, and may claim evidence satisfaction only where evidence is actually sufficient.
15. Provider absence must degrade honestly rather than causing the daemon to invent facts.
16. Existing ordinary `local_shell` execution remains available when the new harness layers are unused.

## 17. Compatibility strategy

The new layer SHOULD be additive and negotiated through existing capability/version mechanisms.

Initial implementations should prefer read-only projections and policy inspection before adding automatic execution/scheduling behavior.

No implementation phase should require all repositories to adopt a policy. A repository with no approved verification policy returns explicit `policy_absent`/`unknown` semantics and may be offered starter templates for user review.

Existing evidence, readiness, mutation-scope, code-intelligence, telemetry, reproduction, session, and checkpoint records remain canonical within their existing scopes.

## 18. Design-completion gate

This umbrella architecture is ready for implementation planning only when the companion designs jointly define:

- affected-relation authority and coverage semantics;
- policy materialization/versioning/profile-template semantics;
- obligation state machine including waiver provenance/expiry;
- evidence sufficiency/admissibility and retry inconsistency;
- resource/cost authority and bounded parallelism;
- quiescence/ownership-transfer semantics;
- engineering-state projection boundaries;
- mutation transaction effect attribution and stale-evidence invalidation;
- provider qualification and phased rollout boundaries.

The companion specs named above are intended to satisfy that gate. No production-code implementation is authorized by this document alone.
