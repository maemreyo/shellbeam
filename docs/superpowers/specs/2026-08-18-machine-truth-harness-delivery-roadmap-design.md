# ShellBeam Machine Truth Harness Delivery Roadmap Design

Date: 2026-08-18
Status: approved architecture roadmap; P4-A/P6-A-before-P2 sequencing amendment approved/frozen; execution remains gated by each node's approved plan and prerequisite evidence
Baseline: latest `origin/main` at final semantic-freeze refresh, `33fe40999910a08410204993b9edb8f7e58698a5`
Scope: prioritized work needed to evolve ShellBeam into the Machine Truth Harness defined by the 2026-08-18 architecture specs

## 1. Decision

ShellBeam should evolve by strengthening its existing execution/observation/evidence substrate and then adding a thin brain-facing engineering-state layer.

The roadmap explicitly avoids two bad extremes:

```text
A. turn ShellBeam into an autonomous Codex clone
B. stop at "terminal MCP with more features"
```

The target is:

```text
frontier reasoning model
        │
        ▼
Machine Truth Harness
  engineering state
  verification semantics
  authority/evidence
  bounded context views
  trustworthy providers
        │
        ▼
local machine
```

## 2. Existing strengths to preserve

The current codebase already has mature or substantial primitives in:

- durable operation/session execution;
- exactly-once start identity;
- terminal receipt/evidence rules;
- workspace/worktree/Git identity;
- bounded output views;
- structured execution results;
- Event Journal;
- Evidence Ledger/expected outputs;
- telemetry/reproduction;
- project readiness/typed commands;
- process inspection;
- persistent named sessions;
- advisory mutation scopes;
- experimental safety checkpoints;
- dynamic input tracing;
- Go semantic code intelligence;
- rich local media;
- capability-gated hard Resource Enforcement, with platform/quality limits defined by its merged contract.

The roadmap SHALL reuse these rather than create parallel stores/ontologies.

## 3. Resource/Hermetic foundation now on main

PR #10 has merged the Resource Enforcement implementation and the frozen Resource Enforcement/Hermetic Boundary contracts into `main`. These are now dependencies of this roadmap rather than parallel speculative work.

Authoritative companion designs:

- [Resource Enforcement](./2026-08-18-resource-enforcement-design.md)
- [Hermetic Boundary](./2026-08-18-hermetic-boundary-design.md)
- [P4-A / P6-A Sequencing Amendment](./2026-08-18-p4a-p6a-sequencing-amendment-design.md)

Current integration posture:

- Resource Enforcement is a landed capability with the platform/maturity limits defined by its own contract; this roadmap reuses it and does not invent a second resource-limit model.
- Hermetic Boundary has a frozen architecture on `main`; Provider Qualification A0 has passed and frozen the bubblewrap V1 candidate/topology, while production Tasks 1–8 remain required before authoritative production hermetic evidence can be produced.
- hard enforcement remains distinct from observation;
- hermetic authority remains enforcement-backed and is never inferred from tracing completeness;
- if either foundation contract changes, this roadmap must be amended explicitly rather than silently diverging.

## 4. Priority ordering

Phase numbers are **capability families / priority buckets**, not a strict total topological order. The implementation DAG is:

```text
P0  stabilize Resource Enforcement + implement qualified Hermetic Boundary V1
 ↓
P1  Affected Surface + Verification Obligations + Evidence Sufficiency
 ↓
P4-A  read-only Code Intelligence foundation (Go/gopls V1)
 ↓
P6-A  DAP qualification + debug-session core (Go/Delve V1)
 ↓
P2  EngineeringStateView / context projection
 ↓
P6-B  DAP harness/evidence integration
 ↓
P3  Mutation Transaction + minimal patch provider
 ↓
P4-B  mutation-capable semantic actions through Mutation Transaction

P5  Browser full integration remains downstream of P0/P1/P2/P3.
P7  selective Git semantics / additional evidence providers remain later.
P8  only then reconsider durable envelope handles/project knowledge substrates if measured need exists.
```

The practical coding-harness path is therefore:

```text
finish P1
→ P4-A
→ P6-A
→ P2
→ P6-B
→ P3
→ P4-B
```

This is an explicit sequencing amendment, not an architecture-boundary change. Detailed scope is frozen in [P4-A / P6-A Sequencing Amendment](./2026-08-18-p4a-p6a-sequencing-amendment-design.md). Browser/provider qualification spikes may occur independently, but full harness integration must respect the dependencies above.

## 5. P0 — Resource Enforcement and Hermetic Boundary

### Goals

- stabilize and consume the merged hard Resource Enforcement capability according to its native/platform qualification boundaries;
- implement the frozen Hermetic Boundary provider contract under the Provider Qualification A0 result before any authoritative dependency narrowing;
- expose declared/enforced/observed resource/authority dimensions in a form reusable by later providers;
- verify cleanup convergence so future browser/debugger/test providers cannot leak processes/resources indefinitely.

### Why first

Verification economics is incomplete if ShellBeam cannot observe/govern the machine cost of verification.

Browser and debugger providers increase process/memory/lifecycle complexity and should not be introduced before the underlying ownership/resource boundary is trustworthy.

### Not required to begin read-only spec work

P1/P2 design can proceed while P0 follow-on work proceeds, but production integration must continue to reuse the merged Resource Enforcement contract and the frozen Hermetic Boundary contract.

## 6. P1 — Verification semantic core

Implement as one coherent subsystem, not three independent features:

```text
Affected Surface
  +
Verification Obligations
  +
Evidence Sufficiency
```

Reference design:

- [Affected Surface, Verification Obligations, and Evidence Sufficiency](./2026-08-18-affected-surface-verification-evidence-sufficiency-design.md)

### First useful slice

Prefer a narrow, high-confidence slice rather than an ambitious universal dependency engine.

Suggested initial scope:

- repository/workspace source generation baseline;
- observed changed paths;
- deterministic Go package/import relations already available or cheaply derivable;
- explicit project path/subsystem classifications;
- project command/evidence metadata;
- materialized policy with a small closed rule schema;
- separate obligation-disposition and evidence-status dimensions plus evidence matching;
- `not_triggered`/`waived` disposition semantics and `not_evaluated`/`unknown`/`unavailable` evidence semantics;
- aggregate `clear|blocked|indeterminate` gate reporting that never counts waivers as evidence satisfaction;
- no auto-execution at first.

### Why no auto-execution first

Read-only derivation/inspection lets us validate semantics against real coding sessions without coupling mistakes to command scheduling.

A reasoning model can continue to choose/run commands through existing `local_shell` while consuming better obligation/evidence state.

### Success measure

The first slice is valuable if it materially reduces:

- unnecessary full-suite/special-test runs;
- repeated model reconstruction of "what verification remains";
- stale evidence mistakes;
- blind reruns;
- over-testing caused by imagined NFRs;

without increasing missed mandatory verification under known policy.

## 7. P1A — Materialized policy and starter templates

This work belongs to P1 semantics.

Implement:

- closed/versioned repository-pinned verification policy;
- preview/materialization flow;
- policy digest/provenance and separate approval/activation authority records;
- effective-policy versus proposed-policy semantics for self-amending policy changes;
- explicit `policy_absent -> first proposed policy -> external activation -> subsequent effective cut` bootstrap semantics;
- durable immutable/auditable policy approval/activation authority records;
- optional Policy Starter Profiles / Verification Posture Templates;
- explicit `policy_absent` state;
- waiver identity/scope/expiry/provenance with pre-existing authority rules for policy-change gates;
- mechanically declared sensitivity/classification inputs;
- advisory policy-gap projection.

Do not implement:

- silent template selection;
- silent template upgrades;
- invented NFR targets;
- filename/prose-based sensitivity heuristics in the daemon.

## 8. P1B — Evidence economics

Integrate verification semantics with existing evidence/telemetry/resource facts.

Implement only cost inputs whose authority can be described honestly.

Possible first dimensions:

```text
historical wall time
output bytes
observed CPU/RSS/process count when available
provider workload class
parallel-safety/shared-resource class
caller/provider-reported remote/model cost only when supplied
```

Cost optimization remains downstream of sufficiency.

Do not build a magic ML test selector.

## 9. P1C — Retry/quiescence semantics

Add mechanical handling for:

- compatible FAIL->PASS evidence inconsistency;
- explicit diagnostic rerun reason/protocol;
- undeclared live-resource detection for verification operations;
- declared ownership transfer to persistent/named sessions/providers;
- cleanup-incomplete evidence semantics separate from child receipt outcome.

This directly addresses coding-agent test process leaks and blind retry behavior.

## 10. P4-A — Read-only Code Intelligence foundation

Reference amendment:

- [P4-A / P6-A Sequencing Amendment](./2026-08-18-p4a-p6a-sequencing-amendment-design.md)

P4-A is sequenced immediately after P1 and before P2. V1 is Go/gopls only and remains read-only.

Implement in bounded slices:

```text
P4-A1 source presentation: path:line-range + symbol? + generation + exact SourceRef
P4-A2 semantic affected-relation bridge with authority/coverage/generation/provenance
P4-A3 reusable provider lifecycle/resource observation envelope for gopls
P4-A4 practical inspect.code UX/resource/leak benchmark acceptance
```

Do not implement mutation-capable LSP actions, rename, workspace edits, code actions, or semantic refactors in P4-A. Those remain P4-B after P3.

The source-presentation layer preserves exact retained SourceRef/byte identity underneath model-facing line/range coordinates. The semantic-relation bridge remains non-exhaustive where the underlying provider is non-exhaustive. P4-A3 may extract only a small provider-neutral runtime fact envelope; it MUST NOT generalize `codeintel.ProviderManager` or make DAP depend on codeintel query/pooling/session policy. Provider lifecycle work reuses existing process/resource/telemetry truth and does not create a universal provider scheduler.

## 11. P6-A — DAP qualification + debug-session core

P6-A is sequenced after P4-A and before P2. V1 is Go/Delve only.

Start with an exact provider qualification gate covering Delve identity/version/toolchain/platform compatibility, DAP handshake, launch/attach boundary, breakpoints, execution control, threads/stack/scopes/variables, panic/exception observation, source correlation, bounded materialization, shutdown/cleanup, crash/disconnect recovery and resource convergence.

Production P6-A remains blocked/`NOT_RUN` when no qualified Delve candidate is available; ordinary ShellBeam runtime must not silently install or trust an arbitrary debugger executable.

Initial normalized surface is effect-classified, not described as uniformly observational:

```text
observe:            threads / stack / scopes / variables / exception / stop reason
control_execution:  continue / step_over / step_in / step_out
manage_breakpoint:  breakpoint.set / breakpoint.remove
launch_process:     debug.start
attach_process:     debug.attach
session_lifecycle:  debug.close
```

Every non-observational action requires explicit runtime/process authority. P6-A V1 attach is limited to exact ShellBeam-owned process identity plus explicit attach authority; PID alone is never authority. Launched-session close terminates the launched target; attached-session close detaches/leaves the target alive; result truth preserves literal debuggee disposition or `unknown`.

P6-A V1 explicitly defers arbitrary DAP `evaluate`, `setVariable`, write-memory/call-injection, and other target-data mutation paths. They are not classified as harmless inspection or ordinary execution control.

Exact SourceRef/path location binding is separate from debuggee build/source provenance. A frame can resolve exactly to current workspace bytes while the debuggee was built from another generation; P6-A reports those dimensions independently, and P6-B evidence compatibility must consume mechanically proven debuggee source provenance rather than infer it from location resolution.

P6-A owns debug-session/provider/process/source/resource/lifetime truth only. It does not yet make debugger observations verification evidence; that belongs to P6-B.

## 12. P2 — EngineeringStateView

Reference design:

- [Engineering State, Mutation Transactions, and Authority Envelopes](./2026-08-18-engineering-state-mutation-authority-design.md)

### Initial goal

Provide one bounded inspection that joins the facts the model currently reconstructs manually:

```text
baseline/current generation
observed changes
mechanical scope
caller focus hints
affected surface
important diagnostics
verification obligations/sufficiency
stale/inconsistent evidence
environment/resource summary
provider/authority gaps
```

### Constraints

- projection, not giant durable aggregate;
- no narrative reasoning state;
- no task-complete claim;
- no vector database requirement;
- no always-on repository indexing requirement;
- explicit deep refs for drill-down;
- summary is bounded and truncation-aware.

### Measurement gate

Before introducing a durable envelope identity, measure whether `inspect.engineering_state(activity_id, workspace_id)` substantially reduces model calls/context bytes in practical tasks.

Only add a durable envelope handle if resume/handoff/baseline pinning cannot be solved cleanly without it.

## 13. P2A — Affected relation integration

The initial affected engine will be incomplete by design. P2 consumes P4-A semantic relations rather than redefining their source/authority model.

Additional relation providers may be added only where they buy measurable value:

- explicit config/project mappings;
- runtime trace as advisory relation;
- hermetic proven-input relation when available;
- artifact/command mappings.

Do not claim universal dependency completeness. Every provider preserves basis, authority, coverage, generation, and provenance.

## 14. P2B — Source/location projection

P2 consumes P4-A source presentation so EngineeringStateView usually exposes:

```text
path:line-range
symbol when known
relation kind
source generation
opaque exact SourceRef underneath
```

P2 does not introduce a second source-addressing model. Exact identity remains owned by SourceRef/source truth; presentation remains a projection.

## 15. P3 — Mutation Transaction

Add the transaction envelope before adding sophisticated editing capabilities.

### Core semantics

```text
expected baseline/source refs
mutation authority/scope
optional checkpoint
provider identity
provider result
actual post-source observation
effect attribution quality
post generation
lineage
stale-evidence invalidation
```

### Minimal first provider

A minimal `apply_patch`-style provider is preferable to building a custom editor.

Qualification must cover:

- stale source rejection;
- atomic/conflict behavior;
- path confinement/authority semantics;
- exact changed-source observation;
- bounded output;
- cleanup/lifecycle;
- cross-platform availability.

### Explicitly defer

- universal AST rewrite DSL;
- auto-rebase/patch conflict reasoning;
- custom refactor engine;
- editor buffer ownership;
- shadow filesystem;
- model-driven fix recommendations inside ShellBeam.

## 16. P3A — Evidence invalidation loop

Once mutations are transactional, connect them to verification state:

```text
mutation G1 -> G2
      ↓
affected-surface recompute
      ↓
current obligation recompute
      ↓
old evidence freshness/scope re-evaluation
      ↓
EngineeringStateView reflects required verification
```

This is the deterministic loop ShellBeam should own.

The reasoning model still decides the next engineering action.

## 17. P4-B — Mutation-capable Code Intelligence evolution

P4-A has already handled read-only Go/gopls source UX, semantic relations and provider-runtime integration before P2.

P4-B begins only after P3 Mutation Transaction semantics exist.

Candidates:

```text
rename
workspace edits
qualified code actions
semantic refactor providers
additional language providers where measured need justifies qualification
```

Every source-mutating semantic action runs through Mutation Transaction preconditions/authority/effect attribution/post-source observation. No LSP capability becomes a source-mutation side door.

Do not turn code intelligence into authoritative build/test evidence.

## 18. P5 — Browser provider

Reuse official/mature Playwright behind the provider boundary.

### Desired capabilities

- create/close bounded browser sessions;
- navigation;
- DOM/ARIA observation;
- screenshots/media;
- click/type/wait primitives;
- downloads/artifact lineage;
- console/network observations where explicitly supported;
- process/resource accounting;
- declared/enforced/observed authority;
- verification evidence for declared user journeys.

### Hard gates before shipping

- provider qualification;
- process cleanup/quiescence;
- bounded concurrency/resources;
- storage/download convergence;
- no hidden browser processes after terminal completion;
- stable target/addressing contract;
- browser facts remain provider observations/evidence rather than a new ontology.

### Why full integration remains after P1-P3

A browser provider becomes dramatically more useful when it plugs into:

```text
affected user journey
  -> obligation
  -> browser evidence requirement
  -> resource-governed execution
  -> evidence ledger
```

rather than being merely another automation tool.

## 19. P6-B — DAP harness/evidence integration

P6-A has already qualified Go/Delve and established the debug-session/provider core. P6-B comes after the initial P2 projection and integrates qualified debugger observations into verification/evidence semantics.

Add only where policy explicitly declares the need:

```text
obligation -> exact debug provider binding
debug observation evidence/provenance
source/environment compatibility
quiescence/lifecycle requirement for evidence completion
stale invalidation across source generations
EngineeringStateView debug evidence/deep refs
```

P6-B does not automatically decide to start a debugger. The model/user still owns that engineering action under the P1 no-auto-execution boundary unless a future separately reviewed policy changes it.

Other DAP ecosystems such as debugpy/js-debug/CodeLLDB require their own qualification before entering the trusted provider path.

## 20. P7 — Selective Git semantics

Do not build a Git replacement.

Add semantic Git operations only when they reduce recurring model parsing/context cost and can reuse existing workspace identity.

Candidates:

```text
bounded exact diff inspection
conflict state inspection
commit/tree identity
worktree/branch facts
normalized mutation receipts for selected Git operations
```

Git planning/rebase strategy remains reasoning-agent work.

## 21. P7A — Project Readiness/Environment usability

Practical benchmarking found Project Readiness/Environment facts less usable than other ShellBeam capabilities.

After core harness semantics stabilize, improve:

- clearer actionable model-facing readiness diagnostics without becoming a repair system;
- stronger environment/resource fact coverage where platforms support it;
- better linkage from readiness gaps to policy/provider availability;
- avoid automatic installation/repair/bootstrap.

This work should support, not precede, the verification semantics.

## 22. P8 — Project knowledge/memory: deliberately deferred

Repro capsules, evidence, events, and EngineeringStateView are not conversation memory.

Do not build autonomous repository memory yet.

Reconsider only if practical use shows repeated loss of high-value explicit engineering decisions that cannot be represented in repository docs/policy/artifacts.

If ever introduced, prefer a substrate where the reasoning model/user explicitly writes bounded project knowledge with provenance rather than daemon inference from conversations.

## 23. Multi-agent coordination: continue to defer autonomy

Mutation scopes remain useful for advisory coordination.

Do not add hidden agent workers, task scheduler, or distributed planner as part of this roadmap.

If multiple reasoning agents use ShellBeam, common truth/policy/evidence primitives should be the coordination substrate.

## 24. What should remain external/model-owned

The following remain outside ShellBeam unless a future architecture review explicitly changes the boundary:

```text
understanding user/business intent
FR/NFR invention or negotiation
solution architecture
failure diagnosis/root-cause reasoning
choosing a code design
choosing whether to ask the user
reflection
planning/task decomposition
commit/PR narrative
final claim that the user's task is done
```

## 25. What ShellBeam should increasingly own

```text
source/workspace identity
mechanical affected relations
approved policy identity
verification obligation derivation
evidence sufficiency/freshness/stability
mutation transaction semantics
resource/process lifecycle
provider qualification/lifecycle
authority honesty
bounded engineering-state projections
```

## 26. Practical evaluation program

Every major phase should be benchmarked on real day-to-day coding tasks, not only unit/contract tests.

Track at least:

```text
model tool-call count
model-visible output/context bytes
wall time
CPU/RSS/process peak when observable
orphan/leaked processes/resources
number of verification executions
full-suite/special-suite frequency
retry count
stale evidence mistakes
missed mandatory obligations
false-positive/wasteful obligations
manual user intervention
```

Do not optimize the harness solely for fewer tests or fewer calls if correctness coverage degrades.

## 27. Verification semantics benchmark scenarios

Initial practical scenarios SHOULD include:

0. **Docs-only regression baseline:** the pre-review four-Markdown-spec change was verified by ShellBeam operation `checkpoint-verify-specs-20260818` at source fingerprint `8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e`; checkpoint policy selected `selection=full` and took approximately eight minutes locally, while the staged pre-commit gate selected only `contract:markdown`. P1 must preserve the required documentation correctness contract while avoiding broad package verification when approved policy plus affected-surface authority prove it unnecessary; compare wall time, CPU/RSS/process cost, and selected evidence providers against this baseline.
1. one-file local behavior change;
2. tiny diff with broad shared-module blast radius;
3. config/database change with nonlocal impact;
4. delegated UUID/library behavior where deterministic integration failure injection is correct and provider stress is wasteful;
5. concurrency-owned code where race/concurrency evidence is legitimately triggered;
6. small app with high-risk authorization/data sensitivity but no scale requirement;
7. performance target absent -> load test explicitly not_triggered;
8. performance target declared -> environment/workload-bound evidence required;
9. FAIL->PASS rerun -> inconsistent evidence;
10. native-platform verification unavailable -> `evidence_status=unavailable` plus waiver/defer disposition as applicable, never pass;
11. partial affected analysis -> conservative widening;
12. test spawns leaked browser/process -> quiescence incomplete;
13. persistent fixture ownership transfer -> valid completion;
14. template update -> pinned repository policy unchanged;
15. policy file changes from P1 to weaker P2 -> P2 remains proposed until an external activation event; P1/meta-policy still governs the self-amending change.

## 28. Provider benchmark scenarios

Browser/DAP/patch providers SHOULD be tested for:

- cold/warm startup;
- repeated open/run/close convergence;
- process/resource leaks;
- crash recovery;
- output/artifact bounds;
- provider version mismatch;
- source-generation mismatch;
- authority failure and downgrade honesty;
- parallel sessions under bounded resource policy;
- host workspace/source mutation only where explicitly authorized.

## 29. Migration strategy

Prefer additive capability negotiation and opt-in project policy.

Suggested rollout:

```text
Stage A: read-only policy/affected/evidence inspection
Stage B: starter-template materialization + waivers
Stage C: P4-A read-only Code Intelligence/source/provider-runtime evolution
Stage D: P6-A qualified Go/Delve debug-session core
Stage E: engineering-state aggregate projection
Stage F: P6-B debugger evidence/harness integration
Stage G: mutation transactions
Stage H: P4-B mutation-capable semantic providers + later provider expansion
```

Optional verification execution helpers remain separately reviewed and caller/model-owned; this sequencing amendment does not introduce an automatic verification scheduler.

Automatic verification execution, if ever introduced, should be a later explicitly reviewed step after read-only semantics prove trustworthy.

## 30. Documentation/product ergonomics

The final product should make the system understandable to both frontier agents and humans.

Useful inspection language should preserve dimensions rather than flattening them:

```text
DISPOSITION
  REQUIRED NOW
  DEFERRED
  OPTIONAL
  NOT TRIGGERED
  WAIVED

EVIDENCE
  NOT EVALUATED
  SATISFIED
  FAILED
  INSUFFICIENT
  INCONSISTENT
  UNKNOWN
  UNAVAILABLE

GATE
  CLEAR
  BLOCKED
  INDETERMINATE
```

Useful explanations:

```text
why this obligation exists
why this evidence is sufficient
why an expensive class was not triggered
what authority/coverage is missing
what evidence became stale and why
what policy/template version is pinned
```

Avoid opaque optimizer output such as:

```text
confidence = 0.87 therefore skipped tests
```

unless a future explicit probabilistic policy design is approved.

## 31. Definition of architectural success

This roadmap succeeds when a frontier coding model connected through ShellBeam can spend materially less context/quota/time reconstructing machine state and running unnecessary verification while gaining stronger, inspectable guarantees about what was actually changed and verified.

The successful end state is not:

```text
ShellBeam writes the code for the model.
```

It is:

```text
The model reasons.
ShellBeam makes engineering state, authority, effects, and evidence hard to misunderstand.
```

## 32. Review gate

Before implementation planning begins, reviewers should explicitly approve or amend:

- the four architecture guardrails;
- Activity as the highest current correlation primitive;
- EngineeringStateView as projection rather than durable Change aggregate;
- the combined P1 verification subsystem boundary;
- materialized-policy/starter-template semantics;
- waiver semantics;
- authority + coverage separation;
- mutation-provider split;
- declared/enforced/observed authority model;
- sequencing relative to the merged Resource Enforcement contract and frozen Hermetic Boundary contract;
- Browser/DAP as providers rather than independent platforms;
- P4-A/P6-A-before-P2 sequencing with P4-B still downstream of P3;
- arbitrary DAP evaluate/target-state mutation excluded from P6-A observation V1;
- deliberate deferral of planner/memory/multi-agent autonomy.

Only after that review should the approved specs be split into implementation plans/tasks.
