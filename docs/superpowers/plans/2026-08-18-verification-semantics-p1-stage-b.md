# Verification Semantics P1 Stage B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete P1 by evaluating existing ShellBeam evidence against Stage-A obligations, preserving contradictory retry history, checking operation quiescence, projecting honest verification cost, folding `clear|blocked|indeterminate` gates, and proving the practical benchmark scenarios without adding an automatic verification scheduler.

**Architecture:** Stage B extends the Stage-A `internal/core/verification` domain rather than creating a second verification ontology. The application service consumes immutable Evidence Ledger records plus lazy evidence validity, telemetry summaries, bounded lifecycle/quiescence facts, policy authority, and Stage-A affected/obligation projections. Evidence is first normalized into verification candidates that preserve raw `VerificationKind`; semantic `ProviderClass` is known only when a qualified binding proves it. Current policy/rule/obligation identity is attached in derived requirement evaluation, not retroactively written into evidence. The sufficiency evaluator applies only policy-declared requirements; cost is projected only after admissibility and never changes sufficiency. Gate folding preserves waiver/evidence separation. The existing `inspect.verification` action advances to response schema v2; v1 remains readable/transport-compatible for Stage-A callers. No Stage-B task automatically launches a test, build, browser, race suite, load test, or full suite.

**Tech Stack:** Go 1.26.5, existing ShellBeam Evidence Ledger, telemetry, process inspection, persistent-session/runtime facts, verification Stage-A packages, existing IPC/MCP v2 and JSON Schema 2020-12.

**Spec:** `docs/superpowers/specs/2026-08-18-affected-surface-verification-evidence-sufficiency-design.md`

**Prerequisite:** [Verification Semantics P1 Stage A](./2026-08-18-verification-semantics-p1-stage-a.md) is fully implemented, its Task 7 real-daemon matrix/checkpoint passes, and the executor records the exact Stage-A source fingerprint before beginning this plan. Stage B must extend that checkpoint, not reimplement or bypass Stage-A authority/affected/obligation semantics. Re-run `python3 scripts/check-verification-p1-plan-traceability.py` at the handoff; any traceability failure blocks Stage B.

## Global Constraints

- All Stage-A global constraints remain in force.
- Evidence sufficiency is evaluated from retained facts; it does not rewrite canonical receipts or immutable evidence records.
- `waived` is never an evidence status and never increments `evidence_satisfied`.
- A gate may be `clear` with waived obligations only when every otherwise-blocking obligation is either literally evidence-satisfied or covered by a valid waiver; the breakdown MUST preserve both counts.
- Current/stale/unknown evidence validity comes from existing Evidence Ledger semantics; Stage B does not invent a second source-freshness store.
- Compatible `FAIL -> PASS` history is `inconsistent` until an approved deterministic flake protocol resolves it; newest-result-wins is forbidden.
- Evidence compatibility is mechanical. Do not use model confidence, natural-language similarity, or probabilistic residual risk.
- Quiescence is verification semantics only. Canonical command/session terminal truth remains unchanged.
- Quiescence subtracts only explicitly transferred ShellBeam-managed resource ownership; arbitrary live processes cannot be prose-waived into "transferred" state.
- Telemetry/resource cost is advisory optimization evidence. Missing cost stays unavailable and MUST NOT make evidence inadmissible or make a mandatory obligation disappear.
- Cost projection happens only after sufficiency. P1 V1 does **not** model OR-provider alternatives/substitution groups and does not optimize provider choice.
- Raw Evidence Ledger `verification_kind` is preserved as raw execution evidence and is never silently elevated into a semantic `ProviderClass`.
- Diagnostic rerun intent is caller-declared before execution and frozen into operation authority; post-hoc labels are rejected as provenance.
- Provider execution semantics (`parallel_safe`, shared/exclusive resource classes, workload class) are policy facts only. P1 exposes them but never selects a worker count or starts concurrent workers.
- Stage B does not auto-execute verification. It exposes bound requirement state plus historical cost/resource facts; the reasoning model/user owns optional/additional/escalated verification through existing `local_shell start` / project-command paths.
- `NOT_TRIGGERED` remains explicit policy output, not absence from the response.
- No NFR target or threshold is inferred from telemetry history.
- `local_shell` remains the only MCP tool.
- Production hard cap 500 lines/file, test hard cap 800, function hard cap 80, interface hard cap 8 methods.
- Every task uses focused RED -> minimum GREEN -> `go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json` -> tracked commit gate.

---

## File Structure Locked by This Plan

Extend the existing domain:

```text
internal/core/verification/
  evidence.go          normalized evidence candidate + compatibility identity
  sufficiency.go       evidence-set evaluation + gate fold
  cost.go              honest cost projection types only
  quiescence.go        verification-completion resource semantics
  *_test.go
```

Extend the application package:

```text
internal/app/verification/
  stability.go         compatible history / flake-protocol fold
  quiescence.go        current operation/session resource fold over core-only ports
  economics.go         bound-requirement historical cost/resource projection over core-only ports
  sufficiency.go       per-obligation evaluator + aggregate gate
  inspect.go           schema-v2 aggregate
  *_test.go

internal/adapter/verification/
  evidence_source.go   wraps existing app/evidence Inspector and emits core verification candidates
  quiescence_source.go wraps existing receipt/persistent/provider lifecycle facts behind a core-only app port
  telemetry_source.go  wraps existing app/telemetry Service for candidate operation IDs
  environment_source.go wraps existing app/environment Service and returns core environment binding
  *_test.go
```

No new canonical verification-evidence store is introduced. Stage B consumes:

```text
internal/core/evidence
internal/app/evidence
internal/core/telemetry
internal/app/telemetry
internal/core/persistentsession
```

Durable P1 additions remain only Stage-A policy snapshots/activation/waiver authority records plus any existing Evidence/Telemetry records.

---

### Task 1: Normalize existing Evidence Ledger records into verification candidates

**Files:**
- Create: `internal/core/verification/evidence.go`
- Create: `internal/core/verification/evidence_test.go`
- Create: `internal/adapter/verification/evidence_source.go`
- Create: `internal/adapter/verification/evidence_source_test.go`
- Modify: `internal/app/verification/ports.go`

**Interfaces:**

Core candidate contract:

```go
type CandidateFreshness string
const (
    CandidateCurrent CandidateFreshness = "current"
    CandidateStale   CandidateFreshness = "stale"
    CandidateUnknown CandidateFreshness = "unknown"
)

type CandidateResult string
const (
    CandidatePass       CandidateResult = "pass"
    CandidateFail       CandidateResult = "fail"
    CandidateIncomplete CandidateResult = "incomplete"
    CandidateAmbiguous  CandidateResult = "ambiguous"
)

type EvidenceCandidate struct {
    EvidenceID             string                    `json:"evidence_id"`
    VerificationKind       evidence.VerificationKind `json:"verification_kind"`
    ProviderClass          ProviderClass             `json:"provider_class,omitempty"`
    ProviderClassKnown     bool                      `json:"provider_class_known"`
    ProjectCommandID       string                    `json:"project_command_id,omitempty"`
    OperationID            string             `json:"operation_id"`
    SessionID              string             `json:"session_id"`
    ActivityID             string             `json:"activity_id,omitempty"`
    WorkspaceID            string             `json:"workspace_id"`
    SourceGeneration       string             `json:"source_generation,omitempty"`
    SourceContentDigest    string             `json:"source_content_digest,omitempty"`
    ProjectBindingDigest   string             `json:"project_binding_digest,omitempty"`
    ManifestDigest         string             `json:"manifest_digest,omitempty"`
    ContractDigest         string             `json:"contract_digest"`
    EnvironmentFingerprint      string             `json:"environment_fingerprint,omitempty"`
    EnvironmentFingerprintVersion int             `json:"environment_fingerprint_version,omitempty"`
    ToolchainFingerprint        string             `json:"toolchain_fingerprint,omitempty"`
    ToolchainFingerprintVersion int                `json:"toolchain_fingerprint_version,omitempty"`
    Authority              DerivationAuthority `json:"authority,omitempty"`
    AuthorityKnown         bool                `json:"authority_known"`
    Freshness              CandidateFreshness  `json:"freshness"`
    Result                 CandidateResult    `json:"result"`
    Attempt                *evidence.VerificationAttemptIntent `json:"verification_attempt,omitempty"`
    SemanticContractDigest string             `json:"semantic_contract_digest"`
    CompletedAt            time.Time          `json:"completed_at"`
}
```

The application port is core-only; the adapter wraps the existing `internal/app/evidence.Inspector` so `internal/app/verification` does not import a sibling app package:

```go
type EvidenceCandidateSource interface {
    Candidates(context.Context, CandidateQuery) (CandidateResultSet, error)
}

type CandidateQuery struct {
    WorkspaceID       string
    ActivityID        string
    ProjectCommandIDs []string
    MaxRecords        int
}

type CandidateResultSet struct {
    Candidates  []EvidenceCandidate
    Coverage    Coverage
    Diagnostics []string
}
```

`internal/adapter/verification/evidence_source.go` owns the dependency on `internal/app/evidence` and converts `evidence.InspectRecord` (`Record`, `Validity`, `CurrentSource`) into core verification candidates.

- [ ] **Step 1: Write failing candidate-mapping tests**

Cover:

```text
Evidence Record pass + Validity current -> candidate pass/current
Validity stale -> stale regardless of literal command pass
Validity unknown -> unknown freshness
Evidence Result fail -> candidate fail
Evidence Result incomplete -> candidate incomplete
Evidence Result ambiguous -> candidate ambiguous
project command ID preserved from frozen command authority
environment fingerprint copied only when existing evidence/environment binding can prove it
raw/unqualified evidence preserves literal `VerificationKind`; `ProviderClassKnown=false` and `AuthorityKnown=false`
`TestRawEvidenceKindDoesNotElevateProviderClass`: raw `test` is not `focused_behavior_test` or `integration_test`; raw `build` is not `typecheck_compiler`
typed project-command evidence may set `ProviderClass=project_command` only when exact frozen command authority is present
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification -run 'Candidate|EvidenceSource' -count=1
```

Expected: FAIL on missing candidate types/source.

- [ ] **Step 3: Implement evidence-to-candidate authority mapping**

The Evidence Ledger does not itself claim "integration test", "focused behavior test", "typecheck", or "browser journey" semantics. Mapping is deliberately conservative:

```text
always preserve Record.VerificationKind exactly

typed project-command evidence with frozen ProjectBindingDigest + exact ProjectCommandID
  -> ProviderClass=project_command, ProviderClassKnown=true,
     authority=mechanical for command identity only

raw evidence without that typed binding
  -> ProviderClassKnown=false
     AuthorityKnown=false unless another qualified provider fact exists
```

There are no invented "generic provider classes" for raw `test/build/...`. A current policy requirement may classify an **exact bound project command** as a stronger semantic provider class during `RequirementEvaluation`; that is a derived evaluation binding, not a mutation/elevation of the immutable evidence candidate. Stage B MUST NOT infer stronger semantics from `VerificationKind`, command names, argv, or historical behavior.

Candidate source sets `SourceGeneration` from `Record.Source.PostGeneration` when present, otherwise `PreGeneration`; `SourceContentDigest` comes only from the existing source binding; `ProjectBindingDigest`/`ManifestDigest` come from `Record.Command`; environment fields come only from `Record.EnvironmentBinding`. It loads the immutable operation reservation by `OperationID` to obtain `VerificationAttempt`. `SemanticContractDigest` is the existing immutable `Record.ContractDigest` because attempt provenance is stored outside `evidence.Contract`; it MUST equal the reservation evidence-contract digest when that reservation is available. A mismatch is corruption/unknown compatibility, never a new semantic cohort invented by P1.

- [ ] **Step 4: Bound reads and pagination**

Candidate source requests at most 128 existing evidence records per page and follows at most four Evidence Ledger pages for the relevant workspace/activity/project-command filters. It stops earlier only when the ledger continuation is exhausted. Hitting the four-page bound returns `CandidateResultSet{Coverage: bounded}` rather than silently dropping history.

`Coverage=bounded` is a first-class sufficiency input. When unseen retained evidence could change compatibility/stability (for example an older compatible FAIL may exist), a mandatory stability requirement MUST NOT become `satisfied`; it evaluates `unknown`/`insufficient` with a bounded-history reason. Cost/economics may still use bounded history because cost does not define sufficiency.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/core/verification internal/app/verification internal/adapter/verification
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/app/evidence -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/app/verification internal/adapter/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: adapt evidence for verification semantics"
```

---

### Task 2: Define mechanical evidence compatibility and contradictory-history semantics

**Files:**
- Create: `internal/core/verification/stability.go`
- Create: `internal/core/verification/stability_test.go`
- Create: `internal/app/verification/stability.go`
- Create: `internal/app/verification/stability_test.go`
- Reuse: Stage-A `internal/core/verification/policy.go` stability/flake vocabulary; only focused evaluator tests change
- Modify: `internal/core/evidence/types.go` and focused tests to define pre-execution rerun intent vocabulary
- Modify: `internal/core/operation/intent.go`, `internal/core/operation/project_command.go`, and `internal/core/operation/persistence.go` only to bind that intent into observation/request/reservation identity
- Modify: `internal/adapter/store/reservation.go` and focused reservation replay/compatibility tests
- Modify: `internal/app/daemon/types.go` and the existing start reservation builders
- Modify: `internal/adapter/ipc/protocol_v2.go` and focused IPC tests with thin optional `verification_attempt` fields for raw + typed start
- Modify: `internal/adapter/mcp/input.go`, `internal/adapter/mcp/request.go`, and focused MCP start tests to decode/validate/forward the same pre-execution field
- Modify: `api/schema/mcp-input-v2.json` and `api/schema/ipc-v2.json` for the closed optional attempt object; legacy protocol rejects the field
- Modify: `internal/adapter/verification/evidence_source.go` to join the immutable operation reservation by `OperationID` for attempt provenance

**Interfaces:**

Pre-execution rerun intent is frozen in operation authority, not attached after result observation:

```go
type RerunReason string
const (
    RerunDiagnoseFlake      RerunReason = "diagnose_flake"
    RerunFlakeQualification RerunReason = "flake_qualification"
)

type VerificationAttemptIntent struct {
    RerunOfEvidenceID string      `json:"rerun_of_evidence_id,omitempty"`
    RerunReason       RerunReason `json:"rerun_reason,omitempty"`
}
```

`verification_attempt` is an optional pre-execution field on both raw and typed `start`. Ordinary first run has both fields absent. `rerun_reason` requires a valid `rerun_of_evidence_id`; a `rerun_of` with empty reason is an ordinary rerun. The exact plumbing is frozen rather than left to the executor:

```go
// internal/core/evidence/types.go; this is execution-attempt provenance, not Evidence.Record history.
type VerificationAttemptIntent struct {
    RerunOfEvidenceID string      `json:"rerun_of_evidence_id,omitempty"`
    RerunReason       RerunReason `json:"rerun_reason,omitempty"`
}

// Add the same optional field to operation.TypedRequestIntent,
// operation.Reservation, daemon.StartRequest, and IPC/MCP start input.
VerificationAttempt *evidence.VerificationAttemptIntent `json:"verification_attempt,omitempty"`
```

The normalized attempt intent participates in pre-execution identity and is copied into `operation.Reservation` before spawn. Backward compatibility is explicit: existing nil-attempt raw/typed requests MUST retain their current request/observation fingerprint bytes and reservation schema versions (raw v2, typed v3, persistent v4). When `VerificationAttempt != nil`, use a new fingerprint-envelope version in `operation.ObservationBinding.Fingerprint` for raw evidence-bearing starts and in `TypedRequestIntent.Fingerprint` for typed starts; do **not** silently change legacy nil-attempt fingerprints. The optional reservation field is valid on current v2/v3/v4 records only when the corresponding new fingerprint envelope binds it. Store replay validates the field/fingerprint combination and treats any attempt change for an existing operation ID as metadata/request conflict. No reservation schema 5 is introduced merely for this additive provenance field.

The immutable Evidence Ledger `Record` schema is unchanged; `internal/adapter/verification/evidence_source.go` loads the corresponding operation reservation by `OperationID` and copies attempt provenance into `EvidenceCandidate`. If the reservation/attempt authority cannot be loaded, rerun provenance is unknown; the adapter MUST NOT infer it from timing/order. Post-hoc mutation of rerun reason is impossible by API design.

Compatibility identity deliberately excludes rerun reason while including a semantic evidence-contract digest that omits attempt metadata:

```go
type CompatibilityKeyInput struct {
    ProviderClass          string `json:"provider_class"`
    ProjectCommandID       string `json:"project_command_id,omitempty"`
    SourceGeneration       string `json:"source_generation,omitempty"`
    SourceContentDigest    string `json:"source_content_digest,omitempty"`
    ProjectBindingDigest   string `json:"project_binding_digest,omitempty"`
    SemanticContractDigest string `json:"semantic_contract_digest"`
    EnvironmentFingerprint      string `json:"environment_fingerprint,omitempty"`
    EnvironmentFingerprintVersion int `json:"environment_fingerprint_version,omitempty"`
    ToolchainFingerprint        string `json:"toolchain_fingerprint,omitempty"`
    ToolchainFingerprintVersion int    `json:"toolchain_fingerprint_version,omitempty"`
}

func CompatibilityKey(EvidenceCandidate) (string, bool)
```

Return `bool=false` when required compatibility facts are unavailable; unknown compatibility MUST NOT merge records into one stability cohort. `SemanticContractDigest` is the immutable Evidence Ledger contract digest; `verification_attempt` is deliberately outside that contract, so a diagnostic/qualification rerun remains comparable to its root run when all actual verification semantics are unchanged.

Stage B consumes the `StabilityRequirement` / `FlakeProtocol` vocabulary already frozen in Stage-A policy schema v1; it does not alter that schema. Hard V1 validation remains: `Runs` 2..10, `MinPasses` 1..Runs, `MaxFailures` 0..Runs. Evaluation uses all literal constraints and never converts them into a probability/confidence score.

- [ ] **Step 1: Write failing compatibility tests**

Prove changes to source generation/source digest, project binding digest, semantic evidence-contract digest, environment fingerprint/version, or toolchain fingerprint/version split compatibility. Display-only metadata/time/order and rerun reason do not. `TestRerunIntentFrozenBeforeExecutionAndDoesNotEraseContradiction` also proves nil-attempt legacy fingerprints remain byte-identical, raw + typed attempt-present fingerprints use the new envelope and change when attempt intent changes, reservation v2/v3/v4 replay cannot relabel it, and no post-result API can set it.

- [ ] **Step 2: Write failing stability-fold tests**

Required cases:

```text
PASS only -> satisfied candidate cohort
FAIL only -> failed cohort
`TestCompatibleFailThenPassIsInconsistent`: FAIL then PASS, compatible -> inconsistent
PASS then FAIL, compatible -> inconsistent
FAIL/PASS with different generation -> separate cohorts, no false inconsistency
`TestSourceMutationSeparatesEvidenceCohortsWithoutRewritingHistory`: G1 FAIL -> test/source edit -> G2 PASS retains both evidence refs, different compatibility keys, no same-cohort FAIL->PASS resolution claim
companion no-mutation G1 FAIL -> G1 PASS -> inconsistent
unknown compatibility -> cannot erase failure; result becomes insufficient/unknown according to requirement
no_contradiction rejects any compatible contradictory cohort
`TestDiagnosticRerunDoesNotEraseContradiction`: `diagnose_flake` explains why a rerun exists but never changes stability fold by itself
`TestApprovedFlakeProtocolRequiresQualifiedRuns`: `flake_qualification` runs count toward a declared flake protocol only when they reference compatible retained evidence; post-hoc/missing provenance cannot qualify
flake_protocol applies only when exact protocol is declared and enough compatible qualified runs exist
latest-pass alone never resolves contradiction
bounded/truncated relevant evidence history cannot prove a mandatory no-contradiction/single-pass stability requirement satisfied when unseen compatible evidence could change the result
```

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/evidence ./internal/core/operation ./internal/core/verification ./internal/app/daemon ./internal/app/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./internal/adapter/verification ./api/schema -run 'VerificationAttempt|Compatibility|Stability|Flake|Reservation' -count=1
```

- [ ] **Step 4: Implement deterministic fold**

Sort candidates by `CompletedAt`, then EvidenceID only for stable display; result does not depend on ordering. Diagnostic rerun reason is reported as provenance only. Only exact approved `flake_protocol` plus compatible runs carrying pre-execution `flake_qualification` intent may resolve stability according to the literal protocol. Flake output includes counts (`runs`, `passes`, `failures`, `incomplete`, `ambiguous`) and protocol identity. No confidence score.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/core/evidence internal/core/operation internal/core/verification internal/app/daemon internal/app/verification internal/adapter/store internal/adapter/ipc internal/adapter/mcp internal/adapter/verification
go test ./internal/core/evidence ./internal/core/operation ./internal/core/verification ./internal/app/daemon ./internal/app/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./internal/adapter/verification ./api/schema -count=1
go test -race ./internal/app/daemon ./internal/app/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp -run 'VerificationAttempt|Compatibility|Reservation' -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/evidence internal/core/operation internal/core/verification internal/app/daemon internal/app/verification internal/adapter/store internal/adapter/ipc internal/adapter/mcp internal/adapter/verification api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: preserve contradictory verification evidence"
```

---

### Task 3: Evaluate evidence requirements and per-obligation sufficiency

**Files:**
- Create: `internal/core/verification/sufficiency.go`
- Create: `internal/core/verification/sufficiency_test.go`
- Create: `internal/app/verification/sufficiency.go`
- Create: `internal/app/verification/sufficiency_test.go`
- Create: `internal/adapter/verification/environment_source.go`
- Create: `internal/adapter/verification/environment_source_test.go`
- Modify: `internal/app/verification/ports.go`

**Interfaces:**

Core evaluation details:

```go
type RequirementEvaluation struct {
    EvaluationID  string         `json:"evaluation_id"`
    PolicyDigest  string         `json:"policy_digest"`
    RuleID        string         `json:"rule_id"`
    ObligationID  string         `json:"obligation_id"`
    RequirementID string         `json:"requirement_id"`
    Status        EvidenceStatus `json:"status"`
    EvidenceRefs  []string       `json:"evidence_refs,omitempty"`
    ReasonCode    string         `json:"reason_code,omitempty"`
}

type ObligationEvaluation struct {
    ObligationID        string                  `json:"obligation_id"`
    EvidenceStatus      EvidenceStatus          `json:"evidence_status"`
    RequirementResults  []RequirementEvaluation `json:"requirement_results"`
    EvidenceRefs        []string                `json:"evidence_refs,omitempty"`
}
```

Provider availability is a closed mechanical input, not an inference:

```go
type Availability string
const (
    AvailabilityAvailable   Availability = "available"
    AvailabilityUnavailable Availability = "unavailable"
    AvailabilityUnknown     Availability = "unknown"
)

type ProviderAvailability struct {
    ByClass map[ProviderClass]Availability
    Reasons map[ProviderClass]string
}
```

For a policy requirement bound to a `ProjectCommandID`, successful current binding resolution makes that **requirement/provider binding** available even when the semantic class is stronger than `project_command`, because the effective policy classifies that exact command binding. The underlying `EvidenceCandidate` remains `ProviderClass=project_command`; semantic elevation exists only inside the current `RequirementEvaluation`. Unbound specialized providers are `unavailable` in P1 unless an existing negotiated ShellBeam capability/provider supplies that exact class. Absence of capability information is `unknown`, not available.

Current-environment observation is also behind a core-only app port:

```go
type CurrentEnvironmentSource interface {
    CurrentBinding(context.Context, string) (environment.Binding, bool, error)
}
```

`internal/adapter/verification/environment_source.go` wraps the existing `internal/app/environment.Service` with `FreshnessRefresh` and returns only the core `environment.Binding`. `same_current` requires exact current environment fingerprint/version equality; `same_current_toolchain` additionally requires exact toolchain fingerprint/version equality. Observation unavailable stays unavailable/unknown according to the requirement; it never becomes a match.

V1 evidence requirement evaluation order is normative. Immutable historical evidence is **not** required to contain P1 policy/rule/obligation IDs; those identities belong to the derived current evaluation:

```text
1 bind current PolicyDigest + RuleID + ObligationID + RequirementID into RequirementEvaluation/EvaluationID
2 exact expected project-binding digest when `BoundEvidenceRequirement` carries one
3 semantic provider admissibility for this current requirement (without mutating candidate ProviderClass)
4 minimum evidence authority
5 current-source requirement
6 declared environment compatibility requirement
7 declared stability requirement
8 declared quiescence requirement (Task 4 plugs into this; until then requirement=true => unavailable)
9 cardinality/flake protocol
10 literal pass/fail/incomplete/ambiguous result
```

`EvaluationID` hashes the current policy/rule/obligation/requirement identity plus sorted evidence refs and the evaluation semantics version. It is a derived deep ref, not written back to the Evidence Ledger. Historical evidence that predates P1 remains eligible when its mechanical facts satisfy the current requirement.

- [ ] **Step 1: Write failing hard-constraint tests**

Prove:

```text
`TestRequirementEvaluationBindsCurrentObligationWithoutMutatingEvidence`: legacy evidence with no policy/rule/obligation IDs can satisfy a current requirement when mechanical facts match; evaluation carries current identities, evidence bytes remain unchanged
`TestCheapInsufficientEvidenceCannotSatisfyStrongerRequirement`: cheap PASS from wrong provider semantic class -> insufficient
right provider but advisory when mechanical required -> insufficient
stale PASS when require_current -> insufficient
current PASS with same_current environment required but binding absent/mismatched -> insufficient
current PASS with same_current_toolchain but toolchain fingerprint mismatched -> insufficient
current compatible FAIL -> failed
ambiguous/incomplete evidence -> insufficient/unknown, never pass
no candidate and provider class known available -> not_evaluated
required provider unavailable -> unavailable
compatible FAIL+PASS -> inconsistent
bounded candidate history that could hide contradiction -> unknown/insufficient, never satisfied
`TestRiskControlsAreLiteralRequirementsWithoutResidualRisk`: policy-declared risk-class controls are just literal EvidenceRequirements; no residual-risk function exists
`TestStaleEvidenceCannotSatisfyCurrentRequirement`
`TestEnvironmentDependentEvidenceRequiresDeclaredBinding`
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification -run 'Sufficiency|RequirementEvaluation|EnvironmentSource' -count=1
```

- [ ] **Step 3: Implement evaluator without cost inputs**

Signature:

```go
func EvaluateObligation(
    obligation VerificationObligation,
    candidates CandidateResultSet,
    providerAvailability ProviderAvailability,
    currentEnvironment *environment.Binding,
    quiescence map[string]QuiescenceObservation,
) ObligationEvaluation
```

No telemetry/cost parameter is accepted by this function. This type boundary mechanically enforces "cost cannot redefine sufficiency".

- [ ] **Step 4: Fold requirement statuses**

For a mandatory obligation:

```text
any failed requirement -> failed
else any inconsistent -> inconsistent
else any insufficient -> insufficient
else any unavailable -> unavailable
else any unknown -> unknown
else any not_evaluated -> not_evaluated
else all satisfied -> satisfied
```

A waiver is not an input to `EvaluateObligation`; waiver applies only in aggregate gate fold after evidence status is computed.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/core/verification internal/app/verification internal/adapter/verification
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/app/verification internal/adapter/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: evaluate verification evidence sufficiency"
```

---

### Task 4: Add verification quiescence without rewriting process/receipt truth

**Files:**
- Create: `internal/core/verification/quiescence.go`
- Create: `internal/core/verification/quiescence_test.go`
- Create: `internal/app/verification/quiescence.go`
- Create: `internal/app/verification/quiescence_test.go`
- Create: `internal/adapter/verification/quiescence_source.go`
- Create: `internal/adapter/verification/quiescence_source_test.go`
- Modify: `internal/app/verification/ports.go`
- Reuse: Stage-A policy schema v1 `RequireQuiescence` field; do not mutate the policy schema

**Interfaces:**

```go
type QuiescenceStatus string
const (
    QuiescenceComplete   QuiescenceStatus = "complete"
    QuiescenceIncomplete QuiescenceStatus = "incomplete"
    QuiescenceUnknown    QuiescenceStatus = "unknown"
    QuiescenceUnavailable QuiescenceStatus = "unavailable"
)

type ResourceRef struct {
    Kind string `json:"kind"` // process|port|persistent_session
    Ref  string `json:"ref"`
}

type QuiescenceObservation struct {
    SchemaVersion int              `json:"schema_version"`
    OperationID   string           `json:"operation_id"`
    SessionID     string           `json:"session_id"`
    Status        QuiescenceStatus `json:"status"`
    LiveResources []ResourceRef    `json:"live_resources,omitempty"`
    Transferred   []ResourceRef    `json:"transferred_resources,omitempty"`
    Unexpected    []ResourceRef    `json:"unexpected_resources,omitempty"`
    ObservedAt    time.Time        `json:"observed_at"`
    Quality       string           `json:"quality"`
}
```

Ports remain core-only and deliberately do **not** pretend post-terminal Process Inspection can prove zero residue:

```go
type TerminalReceiptSource interface {
    LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
}
type PersistentBindingSource interface {
    ListPersistentBindings(context.Context, persistentsession.InspectRequest) (persistentsession.BindingPage, error)
}
type LifecycleQuiescenceSource interface {
    QuiescenceForOperation(context.Context, string) (verification.QuiescenceObservation, bool, error)
}
```

The current Process Inspection service returns `quality=unavailable` once a terminal session no longer has a current PID, so P1 MUST NOT use a missing terminal process root as proof of zero descendants. `internal/adapter/verification/quiescence_source.go` composes only facts that actually exist: canonical receipt cleanup failure, typed persistent-session ownership transfer, and optional lifecycle/provider quiescence proof when a qualified provider exposes it. The existing store repository can satisfy the receipt/persistent core-only ports directly; `internal/app/verification` imports neither sibling app packages nor adapter/store.

- [ ] **Step 1: Write failing resource-subtraction tests**

Core tests prove:

```text
`TestQuiescenceBlocksLeaksAndAllowsTypedTransfer`: explicit qualified lifecycle proof with zero unexpected resources -> complete
explicit lifecycle proof containing live child/port not transferred -> incomplete
receipt ResourceCleanup=incomplete -> incomplete even if child exit/result was success
declared named persistent session matching exact ShellBeam binding subtracts only resources identified by the qualified lifecycle proof as owned by that binding
arbitrary PID listed in prose/waiver cannot become transferred resource
no cleanup failure + no qualified zero-residue proof -> unknown, never complete
provider explicitly unavailable -> unavailable
```

- [ ] **Step 2: Write failing application/adapter tests against current runtime semantics**

Prove the current terminal Process Inspection behavior is handled honestly: a terminal session whose process target resolves unavailable MUST yield `unknown/unavailable`, not `complete`. Read canonical `receipt.ResourceCleanup`: `status=incomplete` is authoritative negative cleanup evidence, while a nil field is **not** positive cleanup proof because many handles do not implement cleanup reporting. Read persistent ownership only from existing ShellBeam persistent-session bindings; do not scan arbitrary host daemons.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/verification ./internal/app/verification -run Quiescence -count=1
```

- [ ] **Step 4: Implement bounded quiescence observation**

V1 semantics accept typed resources `process|port|persistent_session`, but positive `complete` requires a qualified lifecycle/provider fact with explicit coverage over the relevant resource kinds. Browser/container/provider-private resources remain `unsupported/unavailable` until their providers expose typed ownership. Do not add polling, background `/proc` scans, or a new process-lifetime tracker merely to manufacture P1 completeness. Current Resource Enforcement cleanup failure is consumed when present; absence of that failure is not upgraded to complete.

- [ ] **Step 5: Wire into sufficiency**

When `EvidenceRequirement.RequireQuiescence=true`:

```text
complete -> may satisfy this constraint
incomplete -> evidence_status=insufficient with reason undeclared_live_resources
unknown -> evidence_status=unknown
unavailable -> evidence_status=unavailable
```

The Evidence Ledger record and terminal receipt remain byte-for-byte unchanged.

- [ ] **Step 6: Run GREEN/race and commit**

```bash
gofmt -w internal/core/verification internal/app/verification internal/adapter/verification
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store -count=1
go test -race ./internal/app/verification -run Quiescence -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/app/verification internal/adapter/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: verify operation quiescence"
```

---

### Task 5: Project honest verification economics after sufficiency

**Files:**
- Create: `internal/core/verification/cost.go`
- Create: `internal/core/verification/cost_test.go`
- Create: `internal/app/verification/economics.go`
- Create: `internal/app/verification/economics_test.go`
- Create: `internal/adapter/verification/telemetry_source.go`
- Create: `internal/adapter/verification/telemetry_source_test.go`
- Modify: `internal/app/verification/ports.go`

**Interfaces:**

```go
type CostMetric struct {
    Quality string `json:"quality"` // exact|platform_reported|sampled|unavailable
    Latest  *int64 `json:"latest,omitempty"`
    P50     *int64 `json:"p50,omitempty"`
    P95     *int64 `json:"p95,omitempty"`
    Samples int    `json:"samples,omitempty"`
}

type VerificationCost struct {
    ProjectCommandID string     `json:"project_command_id,omitempty"`
    WallMS           CostMetric `json:"wall_ms"`
    OutputBytes      CostMetric `json:"output_bytes"`
    CPUUserMS        CostMetric `json:"cpu_user_ms"`
    CPUSystemMS      CostMetric `json:"cpu_system_ms"`
    MaxRSSBytes      CostMetric `json:"max_rss_bytes"`
    ProcessPeak      CostMetric `json:"process_count_peak"`
    ProviderCost     CostMetric `json:"provider_cost,omitempty"`
    ModelCost        CostMetric `json:"model_cost,omitempty"`
}

type BoundRequirementCost struct {
    ObligationID      string                     `json:"obligation_id"`
    RequirementID     string                     `json:"requirement_id"`
    ProviderClass     ProviderClass              `json:"provider_class"`
    ProjectCommandID  string                     `json:"project_command_id,omitempty"`
    Execution         ProviderExecutionSemantics `json:"execution"`
    Cost              VerificationCost           `json:"cost"`
}
```

The application port is operation-keyed and core-only:

```go
type CostHistory struct {
    OperationID       string
    CompatibilityKey  string
    Latest            *telemetry.PerformanceRecord
    Summary           *telemetry.Summary
    SamplesReturned   int
    SamplesAvailable  int
}

type CostHistorySource interface {
    Histories(context.Context, []string) (map[string]CostHistory, error) // keyed by operation_id
}
```

`internal/adapter/verification/telemetry_source.go` wraps the existing `internal/app/telemetry.Service`. For each bounded evidence-candidate operation ID it calls the real operation-keyed `telemetry.Inspect`, then deduplicates repeated compatibility cohorts by the returned compatibility key before projecting cost. Stage B does not assume a nonexistent telemetry query by project-command ID.

`ProviderCost` and `ModelCost` remain unavailable in P1 unless an existing provider/caller fact explicitly supplies them. Do not synthesize token/quota estimates.

- [ ] **Step 1: Write failing cost-projection tests**

Prove:

```text
`TestTelemetryCostProjectionPreservesObservedQuality`: existing telemetry wall/output summary -> p50/p95 projection with sample count
latest platform_reported CPU/RSS -> latest value with platform_reported quality; no invented resource percentile
latest sampled process peak -> latest value with sampled quality; no invented percentile
missing metric -> unavailable, not zero
incompatible telemetry cohorts are never merged
failed/timeout samples remain in existing wall/output historical population
`TestMissingProviderModelCostRemainsUnavailable`: model/provider cost absent -> unavailable
```

- [ ] **Step 2: Write failing bound-requirement cost/resource-semantics tests**

P1 V1 has no OR-provider/substitution ontology. Project one cost/resource view for each policy-declared bound requirement; do not rank providers or infer an alternative:

```text
`TestCostProjectionDoesNotSelectProviderAlternative`: one requirement -> one declared semantic provider/bound command -> one BoundRequirementCost; no choice/ranking API exists
`TestProviderExecutionSemanticsNeverChoosesUniversalConcurrency`: execution semantics survive policy materialization into the cost view exactly; nil/unknown remains unknown; no worker-count/admission field exists
`TestCostProjectionCannotChangeSufficiency`: adding/removing/changing Cost never changes RequirementEvaluation or GateEvaluation
known historical wall-time may be displayed for the bound provider
missing cost remains unavailable, not zero
current host pressure is not used to rewrite Execution semantics or evidence sufficiency
```

Automated conditional provider selection is explicitly deferred to a future policy schema with OR-alternatives/substitution groups. The reasoning model/user may inspect current sufficiency + these declared-provider cost/resource facts and decide whether to run optional/additional verification.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/verification ./internal/app/verification -run 'Cost|Economics|BoundRequirementCost' -count=1
```

- [ ] **Step 4: Implement telemetry projection**

Reuse existing `telemetry.Inspect`/`telemetry.CompatibilityKey`/summary semantics. Request at most 64 compatible samples through the existing inspect API per distinct candidate operation cohort. Project historical p50/p95 only for wall/output because those are the percentiles the current telemetry summary actually exposes; project CPU/RSS/process peak only from `Latest.Resources` with its literal metric quality. Copy `ProviderExecutionSemantics` only from the exact effective policy requirement; never derive it from telemetry or current resource pressure. Do not duplicate raw telemetry storage, invent resource percentiles, rank provider alternatives, or choose concurrency.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/core/verification internal/app/verification internal/adapter/verification
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/app/telemetry -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/app/verification internal/adapter/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: project verification economics"
```

---

### Task 6: Fold obligation evidence into explicit gate semantics

**Files:**
- Extend: `internal/core/verification/sufficiency.go`
- Extend: `internal/core/verification/sufficiency_test.go`
- Extend: `internal/app/verification/sufficiency.go`
- Extend: `internal/app/verification/sufficiency_test.go`

**Interfaces:**

```go
type GateBreakdown struct {
    EvidenceSatisfied int `json:"evidence_satisfied"`
    Waived            int `json:"waived"`
    Blocking          int `json:"blocking"`
    Indeterminate     int `json:"indeterminate"`
}

type GateEvaluation struct {
    Status      GateStatus    `json:"status"`
    Breakdown   GateBreakdown `json:"breakdown"`
    ReasonCodes []string      `json:"reason_codes,omitempty"`
}

func FoldGate(obligations []VerificationObligation, evaluations map[string]ObligationEvaluation) (GateEvaluation, error)
```

- [ ] **Step 1: Write failing gate truth-table tests**

Required table:

```text
required_now + satisfied -> evidence_satisfied +1
required_now + failed -> blocked
required_now + insufficient -> blocked
required_now + inconsistent -> blocked
required_now + not_evaluated -> indeterminate
required_now + unknown -> indeterminate
required_now + unavailable -> indeterminate
waived + failed -> waived +1, gate may remain clear if nothing else blocks; evidence_satisfied stays 0
waived + unavailable -> same
not_triggered/deferred/optional -> do not count as blocking current gate
`TestGateBreakdownNeverCountsWaiverAsEvidenceSatisfied`: 3 satisfied + 1 waived -> clear, breakdown exactly 3/1/0/0, never "4 satisfied"
empty applicable set under a valid effective policy -> clear with zero counts
policy_absent/invalid/unsupported is NOT passed to FoldGate; aggregate inspection reports indeterminate + explicit policy reason
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/app/verification -run 'Gate|Waived' -count=1
```

- [ ] **Step 3: Implement pure gate fold**

`FoldGate` accepts no telemetry/cost input. It validates that every `required_now`/`waived` obligation has an evaluation and rejects duplicate obligation IDs instead of silently double-counting.

`inspect.verification` wraps this pure fold: when there is no valid effective policy (`absent|invalid|unsupported|proposal_pending with no prior effective policy`), it returns `GateStatus=indeterminate` with a literal reason such as `policy_absent`; it MUST NOT call `FoldGate([])` and accidentally report a clear zero-obligation gate. If an older policy remains effective while a new proposal is pending, gate evaluation uses that older effective policy and reports proposal state separately.

- [ ] **Step 4: Run GREEN and commit**

```bash
gofmt -w internal/core/verification internal/app/verification
go test ./internal/core/verification ./internal/app/verification -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/app/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: fold verification gate status"
```

---

### Task 7: Upgrade `inspect.verification` to evidence-aware schema v2

**Files:**
- Modify: `internal/app/verification/inspect.go`
- Modify: `internal/app/verification/inspect_test.go`
- Modify: `internal/adapter/ipc/verification_protocol_v2.go` and focused verification transport tests
- Modify: `internal/adapter/mcp/verification_input.go`, `internal/adapter/mcp/verification_call.go`, and focused tests
- Modify: `internal/app/bridge/verification.go` verification response types
- Modify: `internal/core/capability/verification.go` and thin catalog projection only if schema-v2 metadata changes
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Extend focused IPC/MCP/schema tests

**Interfaces:**

Stage-B response schema version is 2. The public view is bounded and does not duplicate raw policy/evidence payloads:

```go
type ObligationView struct {
    ObligationID       string                   `json:"obligation_id"`
    SourceRuleID       string                   `json:"source_rule_id"`
    Disposition        ObligationDisposition    `json:"disposition"`
    EvidenceStatus     EvidenceStatus           `json:"evidence_status"`
    SufficiencyBasis   string                   `json:"sufficiency_basis"`
    RequirementResults []RequirementEvaluation  `json:"requirement_results"`
    EvidenceRefs       []string                 `json:"evidence_refs,omitempty"`
    WaiverID           string                   `json:"waiver_id,omitempty"`
    ReasonCodes        []string                 `json:"reason_codes,omitempty"`
}

type InspectionV2 struct {
    SchemaVersion    int                              `json:"schema_version"` // 2
    Phase            verification.Phase               `json:"phase"`
    RepositoryID     string                           `json:"repository_id"`
    WorkspaceID      string                           `json:"workspace_id"`
    SourceGeneration string                           `json:"source_generation"`
    PolicyState      string                           `json:"policy_state"`
    EffectivePolicy  *verification.PolicySummary      `json:"effective_policy,omitempty"`
    ProposedPolicy   *verification.PolicyProposalSummary `json:"proposed_policy,omitempty"`
    Affected         verification.AffectedSurfaceSummary `json:"affected_surface"`
    Gate             verification.GateEvaluation      `json:"gate"`
    Obligations      []verification.ObligationView    `json:"obligations"`
    PolicyGaps       []verification.PolicyGap         `json:"policy_gaps,omitempty"`
    CostSummary      []verification.BoundRequirementCost `json:"cost_summary,omitempty"`
}
```

`ObligationView` does not copy raw evidence logs or full policy rules; stable IDs/references remain the deep-inspection path.

- [ ] **Step 1: Write failing schema-v1/v2 compatibility tests**

Prove Stage-A v1 response remains decodable where existing clients depend on it; v2 adds gate/evidence fields; unknown response fields still follow existing versioned transport rules; legacy catalog projection omits unsupported v2 metadata.

- [ ] **Step 2: Write failing model-facing honesty tests**

Fixtures MUST verify rendered structured content contains:

```text
NOT_TRIGGERED disposition for untriggered load/race/browser classes when policy explicitly considers them
WAIVED obligation with literal unavailable/failed evidence status
inconsistent retry status + evidence refs
stale evidence reason
cost unavailable rather than zero when telemetry is absent
policy gaps separately from mandatory obligations
`gate.status=clear` renders only verification-gate meaning and never `task_complete`, `work_complete`, `safe_to_finish`, or equivalent completion truth
```

- [ ] **Step 3: Run RED**

```bash
go test ./internal/app/verification ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'Verification|InspectionV2|Gate' -count=1
```

- [ ] **Step 4: Wire evidence, quiescence, cost, and gate evaluation**

Order inside `Inspect`:

```text
load effective/proposed policy authority
-> derive affected surface
-> derive obligations/policy gaps
-> read bounded evidence candidates
-> derive compatibility/stability
-> observe quiescence only for candidate evidence whose rule requires it
-> evaluate evidence requirements
-> fold gate
-> project cost/resource semantics for still-relevant policy-bound requirements (no provider-choice optimization)
-> return bounded v2 view
```

Cost projection happens after gate/sufficiency and cannot feed back into prior steps.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/core/capability
go test ./internal/app/verification ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./internal/core/capability -count=1
go test -race ./internal/app/verification ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/core/capability api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: inspect verification sufficiency"
```

---

### Task 8: Real-daemon P1 semantic matrix

**Files:**
- Extend: `cmd/shellbeam/verification_semantics_test.go`
- Create: `cmd/shellbeam/verification_sufficiency_test.go`
- Create test fixtures under `cmd/shellbeam/testdata/verification/` if needed

**Acceptance matrix:**

Implement explicit real-daemon tests for frozen scenarios:

```text
A. current exact docs-contract PASS -> satisfied/clear
B. same PASS stale after source generation change -> insufficient/blocked or indeterminate according to literal stale rule
C. wrong provider or wrong project-binding digest PASS -> insufficient
D. current compatible FAIL -> failed/blocked
E. compatible FAIL->PASS -> inconsistent/blocked
F. incompatible old FAIL + current PASS -> current cohort may satisfy; old failure remains retained evidence but not compatible contradiction
G. waived native-Linux requirement + unavailable evidence -> clear only if all other mandatory evidence satisfies, breakdown preserves waiver
H. partial affected surface -> mandatory obligation does not disappear
I. declared security class + no security rule -> advisory policy gap, not auto-gate
J. no performance target -> load/stress not_triggered, no load evidence required
K. qualified lifecycle provider reports leaked descendant -> insufficient even when child receipt/evidence literal result is pass; on a platform with no qualified lifecycle proof the same requirement is unknown/unavailable, never complete
L. qualified lifecycle proof + exact persistent-session ownership transfer -> allowed subtraction/completion; typed transfer without coverage proof does not invent completion
M. no telemetry -> cost unavailable, gate unchanged
N. cost/resource facts for a declared provider never change insufficiency or select an alternative provider
O. raw Evidence Ledger `test`/`build` stays raw; no implicit focused/integration/typecheck ProviderClass elevation
P. historical pre-P1 evidence can satisfy a current requirement through derived RequirementEvaluation identity; immutable evidence contains no fake policy/rule/obligation IDs
Q. provider execution semantics round-trip exactly, unknown stays unknown, and no universal worker count/admission decision exists
R. pre-execution `diagnose_flake` rerun provenance is retained but does not erase FAIL->PASS contradiction; only approved `flake_qualification` protocol can resolve according to policy
S. G1 FAIL -> test/source mutation -> G2 PASS stays two generations/cohorts with both refs; G1 FAIL -> no mutation -> G1 PASS is inconsistent
T. clear gate output contains no `task_complete|work_complete|safe_to_finish` truth claim
```

- [ ] **Step 1: Write RED integration matrix**

Use actual local daemon composition and temporary repositories. For evidence cases, run small deterministic project commands through existing `local_shell` paths so immutable Evidence Ledger records are created naturally; do not insert fake records directly into production store for the real-daemon tests. The matrix must contain named anchors `TestDelegatedOwnershipVerifiesIntegrationAssumptionWithoutProviderStress`, `TestProviderExecutionSemanticsNeverChoosesUniversalConcurrency`, `TestRequirementEvaluationBindsCurrentObligationWithoutMutatingEvidence`, `TestRerunIntentFrozenBeforeExecutionAndDoesNotEraseContradiction`, `TestSourceMutationSeparatesEvidenceCohortsWithoutRewritingHistory`, and `TestVerificationSurfaceForbidsCompletionTruthFields`.

- [ ] **Step 2: Run RED**

```bash
go test ./cmd/shellbeam -run 'Verification(Semantics|Sufficiency)' -count=1
```

- [ ] **Step 3: Fix only integration defects; run GREEN**

```bash
go test ./cmd/shellbeam -run 'Verification(Semantics|Sufficiency)' -count=1
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
go test -race ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
```

- [ ] **Step 4: Commit the semantic matrix**

```bash
git add cmd/shellbeam/verification_semantics_test.go cmd/shellbeam/verification_sufficiency_test.go cmd/shellbeam/testdata/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: verify p1 evidence sufficiency semantics"
```

---

### Task 9: Practical benchmark campaign and docs-only regression proof

**Files:**
- Extend: `scripts/benchmark-verification-p1.sh`
- Extend: `docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md`
- Create: `docs/superpowers/evidence/2026-08-18-verification-semantics-p1-results.md`

**Benchmark scenarios:**

Run and record at least the roadmap scenarios that P1 V1 can mechanically support now:

```text
0 docs-only four-Markdown shape
1 one-file local Go behavior change
2 tiny shared-package diff with wider import blast radius
3 config/nonlocal path classification example
4 delegated UUID assumption -> deterministic app integration command, no provider stress
5 application-owned concurrency rule -> race command triggered only by declared class/rule
6 small app authorization-sensitive path, no scale target
7 no performance target -> load not_triggered
8 declared performance requirement -> unavailable unless environment/workload-bound provider evidence is explicitly configured; no invented threshold
9 FAIL->PASS -> inconsistent
10 unavailable native platform + waiver/defer
11 partial affected analysis -> conservative widening
12 leaked descendant -> quiescence incomplete when a qualified lifecycle provider proves it; otherwise explicit unknown/unavailable
13 persistent ownership transfer -> subtract only under typed qualified coverage; otherwise explicit unknown/unavailable
14 starter template update -> pinned effective policy unchanged
15 P1 -> weaker P2 edit -> P1 remains effective until external activation at subsequent cut
16 policy absent -> first P1 proposal -> external activation -> later cut only
17 raw test/build evidence -> no implicit semantic ProviderClass elevation
18 diagnostic rerun -> provenance visible, contradiction unchanged without approved flake protocol
19 test/source mutation between FAIL and PASS -> distinct generations/evidence refs, no same-cohort retry resolution
20 provider execution semantics -> visible policy facts, no worker-count/provider-choice decision
```

The benchmark does not require every future Browser/DAP provider to exist. Unsupported provider classes must report `unavailable` honestly.

- [ ] **Step 1: Extend benchmark script with explicit fixture subcommands**

Add:

```text
--scenario docs-only
--scenario local-go
--scenario shared-go
--scenario fail-pass
--scenario leak
--scenario first-policy
```

Each scenario creates a disposable temp Git repository/worktree, writes its own `.shellbeam/project.toml` and verification-policy fixture, invokes existing ShellBeam commands, captures `inspect.verification`, and removes the temp fixture on exit. The script MUST NOT mutate the developer's working repository except for its requested output evidence file.

- [ ] **Step 2: Capture metrics without inventing unavailable dimensions**

For each scenario record:

```text
model/tool calls required by benchmark harness
inspect response bytes
wall time
CPU/RSS/process peak only when ShellBeam telemetry exposes them
verification executions actually run
full-suite/special-suite frequency
stale/inconsistent evidence result
mandatory obligation misses (manual expected-set comparison)
false-positive/wasteful obligations (manual expected-set comparison)
leaked resource count after scenario
```

- [ ] **Step 3: Prove Scenario 0 behavior**

The result document must compare against historical baseline:

```text
historical checkpoint: selection=full, ~8 minute first cold/local observation
historical commit gate: contract:markdown
P1 inspection expected obligations: documentation contract only under the approved fixture policy
P1 must not invent Go full-suite/load/race/browser obligations
```

Because P1 V1 remains no-auto-execution, the benchmark MUST distinguish:

```text
obligation-selection improvement
from
actual command scheduler/runtime improvement
```

Do not claim P1 automatically shortened checkpoint runtime unless a separate user-approved integration later teaches `devctl verify` to consume P1 policy. That integration is outside this P1 plan.

- [ ] **Step 4: Run practical campaign**

```bash
bash scripts/benchmark-verification-p1.sh --scenario docs-only
bash scripts/benchmark-verification-p1.sh --scenario local-go
bash scripts/benchmark-verification-p1.sh --scenario shared-go
bash scripts/benchmark-verification-p1.sh --scenario fail-pass
bash scripts/benchmark-verification-p1.sh --scenario leak
bash scripts/benchmark-verification-p1.sh --scenario first-policy
```

Every command must exit 0 or the results document records the failed scenario and P1 is not marked complete.

- [ ] **Step 5: Run contract/dirty gates and commit**

```bash
go test ./tests/contract -run Markdown -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add scripts/benchmark-verification-p1.sh docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md docs/superpowers/evidence/2026-08-18-verification-semantics-p1-results.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: benchmark p1 verification semantics"
```

---

### Task 10: P1 completion checkpoint and anti-regression audit

**Files:**
- Modify plan checkboxes only after all proof exists
- No production changes permitted in this task except a narrowly discovered correctness fix that first receives a RED regression test and its own commit

**Required audits:**

```text
no ai_inferred affected-relation basis
no scalar confidence/residual-risk skip logic
no evidence_status=waived
no repository approved_by field trusted as authority
no auto-selected starter profile
no hard-coded P99/CCU/availability target in starter templates
no automatic verification command execution from inspect.verification
no latest-pass-wins retry fold
no canonical receipt mutation for quiescence
no cost parameter in core sufficiency/gate functions
no raw evidence VerificationKind implicitly elevated into a semantic ProviderClass
no proposed-policy classification relation used for current gate evaluation
no universal worker-count/concurrency decision in P1
no post-hoc diagnostic rerun metadata authority
no AdmissibleOption/OR-provider optimizer in P1 V1
no task_complete/work_complete/safe_to_finish production truth field or translation
one MCP tool only
```

- [ ] **Step 1: Run traceability + semantic source scans**

```bash
python3 scripts/check-verification-p1-plan-traceability.py
rg -n 'ai_inferred|ResidualRisk|confidence.*skip|evidence_status.*waived|approved_by|100000|99\.99|P99|task_complete|work_complete|safe_to_finish|AdmissibleOption|post.?hoc.*rerun|ProviderClass.*VerificationKind' internal cmd api docs/superpowers/evidence
```

Review every match; expected matches are tests/spec/evidence explaining forbidden behavior, not production logic implementing it.

- [ ] **Step 2: Run full P1 package verification**

```bash
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
go test -race ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git diff --check
```

- [ ] **Step 3: Run checkpoint verification**

```bash
go run ./tools/devctl verify --checkpoint --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
```

Record exact source fingerprint, selection, selected suites, wall time, and exit code.

- [ ] **Step 4: Verify VCS scope**

```bash
git status --short --branch
git log --oneline --decorate --max-count=20
git diff "${SHELLBEAM_BASE_REF:-origin/main}"...HEAD --stat
git diff "${SHELLBEAM_BASE_REF:-origin/main}"...HEAD --check
```

P1 completion requires no unrelated source mutation and no uncommitted work.

- [ ] **Step 5: Handoff**

Report:

```text
Stage-A and Stage-B commit list
final source fingerprint
checkpoint receipt/selection
Scenario-0 before/after obligation selection
all unavailable provider/resource dimensions
remaining P2 work explicitly deferred
```

`TestP1ScopeDoesNotClaimP2ThroughP8` is a plan-traceability contract: P2 EngineeringStateView, P3 Mutation Transaction, P4 Code Intelligence evolution, P5 Browser, P6 DAP, P7 selective Git, and P8 project knowledge/memory remain explicitly deferred. Do not claim any of them, Resource-Governor worker selection, or automatic verification scheduling are implemented by P1.

## Self-Review Checklist

- [ ] Evidence Ledger remains canonical for command evidence; P1 adds evaluation, not replacement storage.
- [ ] Every mandatory obligation carries `sufficiency_basis` from Stage A.
- [ ] Evidence authority/freshness/environment/stability/quiescence are hard constraints evaluated before cost.
- [ ] `PolicyDeclaredRiskControlsSatisfied` is represented only by concrete policy-declared evidence requirements, not probability.
- [ ] `FAIL -> PASS` compatible evidence becomes inconsistent unless an explicit flake protocol resolves it.
- [ ] Bounded/truncated relevant evidence history cannot prove absence of contradiction for mandatory stability.
- [ ] A waiver changes gate disposition only and never evidence status.
- [ ] Gate breakdown can report `3 satisfied + 1 waived` without saying `4 satisfied`.
- [ ] Quiescence subtracts only typed ShellBeam ownership transfer.
- [ ] Cost never enters `EvaluateObligation` or `FoldGate` signatures.
- [ ] Missing telemetry/model/provider cost remains unavailable.
- [ ] Stage B still does not auto-run verification.
- [ ] Practical benchmark distinguishes obligation selection from command scheduling/runtime.
- [ ] Raw Evidence `VerificationKind` is never implicitly elevated to a semantic ProviderClass.
- [ ] RequirementEvaluation, not immutable evidence, carries current policy/rule/obligation identity.
- [ ] Diagnostic rerun intent is frozen pre-execution and never resolves contradiction by itself.
- [ ] ProviderExecutionSemantics are exposed as policy facts; no worker count or actual Resource-Governor admission decision exists in P1.
- [ ] P1 V1 has no `AdmissibleOption`/OR-provider optimizer; model/user owns optional escalation.
- [ ] `task_complete|work_complete|safe_to_finish` never appear as production truth fields or model-facing translations.
- [ ] Policy self-amendment/first-policy authority rules remain unchanged from Stage A.
