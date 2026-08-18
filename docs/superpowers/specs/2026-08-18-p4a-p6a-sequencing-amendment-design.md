# ShellBeam P4-A / P6-A Sequencing Amendment Design

Date: 2026-08-18
Status: approved/frozen sequencing checkpoint; execution of each DAG node remains gated by its own approved plan and prerequisite evidence; does not alter frozen Machine Truth / Verification / Engineering-State semantics
Scope: reorder read-only Code Intelligence and narrow DAP provider work ahead of EngineeringStateView and Mutation Transaction implementation while preserving mutation/evidence/authority boundaries

## 1. Decision

ShellBeam SHALL treat roadmap phase numbers as **capability families / priority buckets**, not as a strict total topological order.

The approved execution DAG is:

```text
P0  Resource/Hermetic foundation
        ↓
P1  Verification Semantics
        ↓
P4-A  Read-only Code Intelligence foundation
        ↓
P6-A  DAP qualification + debug-session core (Go/Delve V1)
        ↓
P2  EngineeringStateView
        ↓
P6-B  DAP harness/evidence integration
        ↓
P3  Mutation Transaction + minimal patch provider
        ↓
P4-B  mutation-capable semantic actions
```

P5 Browser full integration remains downstream of the foundation and is not pulled forward by this amendment. P7/P8 remain deferred.

This sequencing is an implementation-order amendment only. It does not weaken or reinterpret the frozen boundaries that:

- EngineeringStateView is a recomputable projection, not a new durable Change aggregate;
- Mutation Transaction owns mutation/effect attribution semantics;
- provider facts do not create new authority models;
- verification sufficiency is policy/evidence semantics, not provider convenience;
- resource governance decides actual admission/concurrency downstream of semantic constraints;
- ShellBeam never emits user-task completion truth.

## 2. Why P4-A moves before P2

P2 is a consumer of Code Intelligence facts. Current ShellBeam already has a substantial read-only semantic provider:

```text
inspect.code
Go/gopls provider
SourceRef retention/binding
diagnostics
symbols
definition/references/type-definition/type-summary
callers/callees
imports/resolved imports
provider identity/incarnation/version
bounded provider manager + query/result budgets
```

Building EngineeringStateView before improving these inputs risks freezing P2 around today’s lower-level UX and later expanding/reworking its source/diagnostic/relation projection.

P4-A therefore improves the facts and presentation contracts P2 will consume, without introducing source mutation.

## 3. P4-A — Read-only Code Intelligence foundation

### 3.1 V1 scope

P4-A V1 is **Go/gopls only**. It does not add Python/TypeScript/Rust providers merely because protocols exist.

P4-A consists of four bounded slices:

```text
P4-A1  model-facing source-location ergonomics
P4-A2  stable semantic affected-relation projection
P4-A3  reusable provider runtime/lifecycle/resource observation
P4-A4  practical inspect.code UX/benchmark acceptance
```

### 3.2 Source identity remains exact

`codeintel.SourceRef` remains the canonical exact source handle. P4-A SHALL NOT rebind old SourceRefs to current bytes or replace SourceRef identity with path/line coordinates.

P4-A adds a model-facing presentation projection over exact bound source bytes, conceptually:

```text
SourcePresentation
  source_ref_id
  source_generation
  logical_path / display_identity
  line_start
  column_start
  line_end?
  column_end?
  symbol?
  resolution_quality
  freshness / source_correlation
```

Rules:

- byte ranges remain the exact internal coordinate for a resolved SourceRef;
- line/column ranges are derived from those exact retained bytes, not recomputed against whatever file is current later;
- `source_generation` is a separate correlation fact, not silently inserted into SourceRef identity;
- symbol is optional presentation metadata and never changes identity;
- unresolved/provider-reported locations remain visibly lower-quality rather than being fabricated into exact SourceRefs.

P4-A SHOULD make the normal model-facing `inspect.code` output usable as `path:line-range + symbol + generation + exact source_ref`, while retaining deep/exact byte-range details underneath.

### 3.3 Stable semantic affected relations

P4-A may translate qualified existing semantic facts into the affected-relation vocabulary frozen by P1.

Initial candidates:

```text
reference
caller
callee
resolved import target
definition/type-definition where useful for navigation, not blast-radius inflation by default
```

The bridge SHALL preserve:

```text
basis
provider identity/version/incarnation
derivation authority
coverage/completeness
source generation
provenance refs
```

It SHALL NOT claim universal dependency completeness. Existing gopls call-hierarchy results that are mechanically derived but non-exhaustive remain non-exhaustive after translation.

P4-A does not replace the narrow P1 relation provider. It adds an independently versioned semantic relation provider that P1/P2 may consume when available; unavailable/partial semantic analysis widens uncertainty instead of deleting mandatory obligations.

### 3.4 Provider-neutral runtime/lifecycle/resource facts

Current `codeintel.ProviderManager` already owns code-intelligence-specific bounded instance count, in-flight limits, queueing, idle eviction, compatibility identity, cooldown and `Close()`. **It is not the generic provider runtime.** P4-A SHALL NOT generalize that type into a cross-provider manager and P6-A SHALL NOT depend on `internal/app/codeintel.ProviderManager`, `codeintel.ProviderRequest`, `codeintel.ProviderResponse`, or code-intelligence query/session policy.

P4-A3 MAY extract a small provider-neutral runtime **fact/envelope contract** used by gopls first and consumable by later providers:

```text
provider family / provider_id
provider incarnation
executable identity/version
workspace binding
process identity when observable
lifecycle: starting | live | closing | terminal | lost
declared/enforced resource authority refs
resource observations when available
started_at / last_used / terminal_at
cleanup/quiescence outcome
provider diagnostic refs
```

The shared contract MUST NOT own:

```text
Query()
provider pooling or queue discipline
retry/cooldown policy
LSP document/session state
DAP debug-session state
provider-specific request/response payloads
universal concurrency decisions
```

Code Intelligence and DAP retain independent protocol/session managers. They may publish the same provider-neutral machine facts, but neither protocol manager becomes the other's dependency. The reusable layer is an **observation/authority fact envelope**, not a universal provider framework or scheduler.

For gopls, P4-A must prove that warm provider reuse, idle eviction, crash/failure cooldown, daemon shutdown and provider replacement publish lifecycle/resource truth without creating a second process store or a second telemetry store. The implementation may adapt current direct LSP subprocess launch/observation plumbing as needed, but it MUST preserve `codeintel.ProviderManager` as codeintel policy rather than promoting it to a generic manager.

### 3.5 Explicit P4-A non-goals

P4-A SHALL NOT implement:

```text
rename
workspace/applyEdit
quick-fix/code-action execution
semantic refactor actions
source writes of any kind
new language providers without a separate qualification need
build/test correctness claims from language-semantic facts
Resource Governor worker-count decisions
```

Mutation-capable LSP/refactor functionality belongs to P4-B after P3.

## 4. P6-A — DAP qualification + debug-session core

### 4.1 Purpose

P6-A tests whether the Machine Truth provider boundary is genuinely reusable for a long-running interactive runtime observer before EngineeringStateView is built.

P6-A is intentionally **Go + Delve only**. Other DAP adapters remain future qualified providers.

Official Delve documentation describes `dlv dap` as a single-use DAP server that waits for launch/attach configuration, and exposes DAP launch/attach plus threads, stack trace, scopes, variables, evaluate and other capabilities. ShellBeam will reuse Delve rather than implement debugger internals.

References:

- https://github.com/go-delve/delve/blob/master/Documentation/api/dap/README.md
- https://github.com/go-delve/delve/blob/master/service/dap/server.go

### 4.2 Qualification comes before production semantics

P6-A SHALL begin with a provider qualification/feasibility gate, not with production API implementation.

Qualification freezes an exact Delve candidate and proves at least:

```text
executable identity + version
supported Go/toolchain/platform tuple
DAP launch mode needed by ShellBeam
local attach support boundary
startup/initialize/configuration handshake
breakpoint + continue/step behavior
threads/stack/scopes/variables behavior
panic/exception observation
source path behavior
bounded variable paging/materialization
shutdown/terminate/detach convergence
process-tree/resource cleanup
crash/disconnect recovery
protocol/version mismatch behavior
```

If no qualified Delve executable is available, P6-A production work is `NOT_RUN/blocked_by_provider_qualification`; ShellBeam MUST NOT silently install or trust an arbitrary debugger binary during ordinary runtime.

The planning workstation observation on 2026-08-18 is that `dlv` is not currently on `PATH`; this is evidence that the plan must not assume provider availability.

### 4.3 Debug-session identity and state

P6-A introduces a bounded debug-session core, conceptually:

```text
DebugSession
  debug_session_id
  workspace_id
  provider identity/version/incarnation
  adapter connection identity
  launch_or_attach authority
  debuggee process identity
  source generation
  lifecycle
  stopped/running state
  stop reason
  resource/lifetime observation
  created_at / updated_at
```

This is debugger-session truth, not task state and not EngineeringStateView.

A debug session may be persistent across multiple debug actions while the owning provider/process remains live, but it must have explicit ownership, timeout/lifetime policy and terminal cleanup semantics.

### 4.4 P6-A normalized action surface and effect taxonomy

P6-A V1 is **read-only with respect to source bytes and target data mutation**, but it is **not side-effect-free with respect to debuggee execution state**. Every normalized action belongs to one closed runtime-effect class:

```text
DebugActionEffect

observe
control_execution
manage_breakpoint
launch_process
attach_process
session_lifecycle
```

The first public/provider-neutral surface SHOULD be limited to:

```text
debug.start            -> launch_process
debug.attach           -> attach_process
breakpoint.set          -> manage_breakpoint
breakpoint.remove       -> manage_breakpoint
debug.continue         -> control_execution
debug.step_over        -> control_execution
debug.step_in          -> control_execution
debug.step_out         -> control_execution
debug.threads          -> observe
debug.stack            -> observe
debug.scopes           -> observe
debug.variables        -> observe
debug.exception        -> observe
debug.close            -> session_lifecycle
```

A breakpoint-management request MUST NOT inherit observation authority merely because the protocol is DAP; the provider may need to halt/control a running target while changing breakpoints. Every non-`observe` action MUST bind explicit runtime/process authority before provider invocation and MUST expose its literal runtime effect/result. PID, path, debugger-session ID, or DAP capability alone is never authority.

Exact public action naming remains implementation-plan work, but all actions remain under the single `local_shell` tool/capability-negotiation architecture.

### 4.5 Launch, attach, and close ownership semantics

P6-A V1 freezes a conservative local ownership boundary:

```text
debug.start
  ShellBeam launches and owns the debuggee lifecycle for the debug session.
  Launch authority must be explicit and bound before spawn.

debug.attach
  local attach only;
  target must resolve to an exact current process identity already owned by
  a ShellBeam operation or persistent session;
  caller must provide an explicit attach-authority intent bound to that
  exact process/ownership identity before provider attach;
  an arbitrary same-user PID is NOT sufficient and arbitrary PID attach is
  deferred beyond P6-A V1.
```

Before attach, ShellBeam MUST re-resolve the target and fail closed if PID reuse, start-time/executable identity change, ownership loss, or other process-identity mismatch prevents exact correlation.

`debug.close` is not an opaque universal shutdown operation. V1 close policy is mode-specific:

```text
launched session:
  supported close policy = terminate_launched_debuggee
  debugger/session closes only after literal target termination outcome is known

attached session:
  supported close policy = detach_leave_debuggee_alive
  target termination is NOT part of P6-A V1 attach-close authority
  terminating an attached target requires a separately reviewed/authorized action
```

Close results MUST preserve literal debuggee disposition rather than collapse it into `closed=true`:

```text
debugger_closed: true | false
debuggee_disposition:
  terminated
  detached_running
  detached_stopped
  still_attached
  unknown
observation_quality / provenance
```

If the provider cannot prove the post-close debuggee state, it reports `unknown`; it MUST NOT infer termination/detach success from client disconnect alone.

### 4.6 Deferred target-data mutation paths

P6-A V1 SHALL NOT treat arbitrary DAP `evaluate` as harmless observation.

Delve exposes Evaluate and also supports state-changing debugger operations such as variable mutation; its DAP implementation includes Evaluate, SetVariable and WriteMemory request handling. Delve evaluation may also support call injection depending on expression/context. Therefore the first trustworthy ShellBeam boundary is:

```text
locals/variables/scopes           observe
evaluate arbitrary expression     DEFER from P6-A V1
setVariable                        DEFER
auto call injection               DEFER
writeMemory                        DEFER
provider config mutation via eval DEFER
```

If a later design adds evaluate or any target-data mutation request, it must receive a new explicit effect/authority classification rather than inherit either `observe` or ordinary execution-control authority.

### 4.7 Location binding versus debuggee source provenance

P6-A consumes P4-A source presentation/resolution contracts, but it MUST separate **location binding** from **debuggee build/source provenance**.

A DAP frame/source location may produce a `DebugLocationBinding`:

```text
provider-reported path/line
        ↓
exact repository/workspace resolution when provable
        ↓
SourceRef + exact retained bytes
        ↓
path:line-range presentation
workspace/source generation of those retained bytes
symbol/function when known
resolution quality
provider provenance
```

This proves only which exact retained bytes ShellBeam uses to interpret the reported location. **Exact `SourceRef`/path resolution MUST NOT imply that the debuggee executable was built from that source generation.**

P6-A therefore carries a separate `DebuggeeSourceProvenance` dimension, conceptually:

```text
executable/build artifact identity when known
source/build lineage identity when mechanically proven
compatibility quality:
  exact_build_lineage
  artifact_lineage_only
  unknown
provenance refs
```

Required honesty examples:

```text
launch mode=debug/test under a qualified ShellBeam-observed build flow
  -> MAY bind mechanically proven build/source lineage only when the build
     artifact/input contract actually provides it

launch mode=exec
  -> executable identity may be exact
  -> source/build provenance remains unknown unless independent artifact/build
     lineage proves it

debug.attach existing process
  -> location may resolve exactly to current workspace SourceRef
  -> debuggee source provenance remains unknown unless independent lineage proves it
```

If exact location resolution fails, ShellBeam retains a bounded provider-reported location with explicit quality; it does not bind the frame to current bytes by guess. If debuggee source provenance is unknown, it remains unknown even when location binding is exact.

P6-B evidence compatibility MUST consume `DebuggeeSourceProvenance` (plus the ordinary environment/provider dimensions) and MUST NOT infer build/source compatibility from `DebugLocationBinding` alone.

### 4.8 Ownership/resource/lifecycle

P6-A reuses existing operation/process/resource/persistent ownership primitives plus the minimal provider-runtime observation envelope established in P4-A.

It SHALL NOT create:

```text
debugger-specific process truth store
debugger-specific CPU/RSS ontology
debugger-specific source identity
debugger-specific generic evidence store
```

DAP-specific state is limited to protocol semantics such as breakpoints, thread/goroutine handles, frames, scopes, variables and stop/exception reasons.

## 5. P2 after P4-A/P6-A

P2 remains a projection layer. It does not absorb P4/P6 stores or become a new canonical root.

Because P4-A and P6-A now precede it, initial EngineeringStateView may consume richer inputs:

```text
STATIC TRUTH
  normalized source locations
  diagnostics
  symbols/navigation
  semantic affected relations

RUNTIME DEBUG TRUTH (when a session/ref is supplied)
  debug session/provider state
  stop reason
  stack/source locations
  bounded locals summary/deep refs
  exception facts

VERIFICATION TRUTH
  obligations
  evidence status/staleness/inconsistency
  gate

RESOURCE/AUTHORITY TRUTH
  provider/process lifecycle
  declared/enforced/observed resource facts
  provider/authority gaps
```

P2 still defaults to bounded summary + deep refs. It must not automatically start a debugger merely to fill the view.

## 6. P6-B — DAP harness/evidence integration

P6-B comes after the initial P2 projection because it connects debug observations into the wider harness semantics rather than merely exposing a debugger API.

P6-B may add:

```text
verification provider class for declared debug observation requirements
obligation -> exact debug provider binding
debug observation evidence/provenance
source/environment compatibility for debug evidence
quiescence/lifecycle requirement for evidence completion
EngineeringStateView debug evidence/deep refs
stale invalidation across source generation changes
```

Example:

```text
obligation:
  reproduce panic under declared fixture

provider:
  delve_debug_observation

evidence:
  exception = panic
  location = foo.go:71
  source_ref = src_...
  source_generation = G
  stack_ref = dbgstack_...
```

P6-B does not automatically choose to debug; the model/user still owns that engineering action unless a future separately reviewed execution policy says otherwise.

## 7. P3 and P4-B remain mutation boundary

P3 still owns:

```text
mutation preconditions
mutation authority/scope
provider invocation identity
actual observed source effects
post generation/lineage
stale-evidence invalidation
transaction retry/idempotency
```

Only after P3 may P4-B expose mutation-capable language-semantic actions such as:

```text
rename
workspace edit
code action
semantic refactor
```

Those actions are providers inside Mutation Transaction semantics, never a side door around it.

P6-A/P6-B do not make P3 a prerequisite because debugger execution control is runtime-process state, not source mutation. Any future debugger request that can mutate target state must receive its own effect/authority classification and is not automatically observational.

## 8. Browser relationship

This amendment does not pull P5 Browser full integration ahead of P3.

Browser may have its own qualification spikes, but the roadmap’s full harness integration remains downstream of the common resource/hermetic foundation, P1 verification semantics, P2 projection and P3 mutation/effect boundaries where browser interactions can produce externally visible effects.

## 9. Revised dependency DAG

```text
                         ┌──────────────┐
                         │    P4-A      │
                         │ read-only CI │
                         └──────┬───────┘
                                │
P0 ───────▶ P1 ─────────────────┼──────────────▶ P2
                                │                 │
                         ┌──────▼───────┐         │
                         │    P6-A      │         │
                         │ debug core   │         │
                         └──────┬───────┘         │
                                │                 │
                                └──────────────▶ P2
                                                  │
                                                  ▼
                                                P6-B
                                                  │
                                                  │
P2 ─────────────────────────────────────────────▶ P3 ─────▶ P4-B

P0/P1/P2/P3 ──────────────────────────────────────────────▶ P5 full integration
```

The useful implementation path for ChatGPT Web coding-harness value is:

```text
finish P1
→ P4-A
→ P6-A (Go/Delve only)
→ P2
→ P6-B
→ P3
→ P4-B
```

## 10. Planning decomposition

This amendment SHALL NOT be implemented as one giant branch/plan.

Create independent spec/plan cycles in this order:

```text
1. P4-A read-only Code Intelligence evolution
   - A1 source presentation
   - A2 semantic affected-relation bridge
   - A3 provider runtime/lifecycle/resource facts
   - A4 practical UX/benchmark acceptance

2. P6-A Go/Delve debug core
   - Task -1/A0 provider qualification
   - DAP transport/provider adapter
   - debug-session core + ownership/lifecycle
   - observational actions
   - P4-A source correlation
   - real-debugger acceptance/cleanup benchmark

3. P2 EngineeringStateView
   - consume P1 + P4-A + optional P6-A facts
   - no provider auto-start

4. P6-B DAP harness integration
   - verification provider/evidence semantics
   - EngineeringStateView evidence/deep refs

5. P3 Mutation Transaction

6. P4-B mutation-capable semantic providers
```

P2/P6-B/P3/P4-B implementation plans SHOULD NOT be frozen before P4-A/P6-A real-provider acceptance reveals the actual provider-runtime/source/deep-ref contracts they need.

## 11. Acceptance semantics for this sequencing amendment

The amendment is ready for implementation planning only if reviewers agree that:

1. phase numbers are not a strict total order;
2. P4-A is read-only and does not contain mutation-capable LSP actions;
3. P4-A V1 is Go/gopls only unless measured need justifies another provider;
4. exact SourceRef identity remains underneath path/line/symbol presentation;
5. semantic affected relations preserve authority/coverage/generation/provenance and never claim universal completeness;
6. provider-runtime reuse is a small provider-neutral observation/authority fact envelope, not a universal provider scheduler, and P6-A does not depend on/generalize `codeintel.ProviderManager`;
7. P6-A starts with an exact Delve qualification gate and does not assume installation;
8. every P6-A action has one closed `DebugActionEffect`; observation is distinct from execution control, breakpoint management, launch/attach, and session lifecycle;
9. every non-observational debug action requires explicit runtime/process authority and reports literal effects rather than inheriting observation authority;
10. P6-A V1 attach is limited to exact ShellBeam-owned process identity plus explicit attach authority; PID alone is never authority;
11. P6-A V1 close semantics are mode-specific: launched targets terminate, attached targets detach/are left alive, and literal post-close debuggee disposition remains visible/unknown when unproven;
12. P6-A V1 excludes arbitrary evaluate/setVariable/writeMemory/call-injection semantics;
13. exact SourceRef location binding is independent of `DebuggeeSourceProvenance`; neither implies the other;
14. P6-B evidence compatibility consumes debuggee build/source provenance and never infers it from location binding alone;
15. P6-A reuses process/source/resource/lifecycle truth rather than creating debugger-specific copies;
16. P2 may consume P4-A/P6-A facts but does not start those providers automatically;
17. P6-B, not P6-A, owns verification/evidence integration for debugger observations;
18. P3 remains the source-mutation transaction boundary;
19. P4-B remains downstream of P3;
20. Browser full integration is not pulled forward by this sequencing change;
21. P2/P6-B/P3/P4-B detailed implementation plans remain adaptive to measured P4-A/P6-A acceptance rather than being prematurely frozen.
