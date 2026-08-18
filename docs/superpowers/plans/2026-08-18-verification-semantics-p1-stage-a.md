# Verification Semantics P1 Stage A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the first read-mostly P1 vertical slice: repository-pinned verification policy, durable external policy authority, mechanically derived affected surface, verification obligations, policy gaps, and one bounded `inspect.verification` view that answers what must be verified without auto-running verification commands.

**Architecture:** Add one new `internal/core/verification` domain shared by both P1 stages. Repository policy bytes live at `.shellbeam/verification-policy.toml`; they are parsed by an adapter and canonicalized into core types. Policy authority is never inferred from repository bytes: activated canonical snapshots, activation records, waivers, and waiver revocations are daemon-owned durable facts. Affected surface is a recomputable projection over workspace/activity delta plus a narrow filesystem/Go relation provider. The application service joins those facts to deterministically derive obligations and policy gaps. Stage A exposes policy preview/authority mutation plus read-only verification inspection through the existing single `local_shell` tool, but it does not schedule tests, builds, browsers, race suites, or any other verification provider.

**Tech Stack:** Go 1.26.5, standard-library core types, existing `github.com/pelletier/go-toml/v2` only in adapters, existing workspace/activity/project/store/IPC/MCP architecture, JSON Schema 2020-12.

**Spec:** `docs/superpowers/specs/2026-08-18-affected-surface-verification-evidence-sufficiency-design.md`

**Next plan:** [Verification Semantics P1 Stage B](./2026-08-18-verification-semantics-p1-stage-b.md) starts only after this plan's Task 7 checkpoint/handoff passes.

**Traceability Gate:** `docs/superpowers/plans/2026-08-18-verification-semantics-p1-traceability.json` is normative plan-review metadata. Before **Task 0** begins, run `python3 scripts/check-verification-p1-plan-traceability.py`; it MUST report `core=24/24 roadmap=4/4 review=11/11 deferred=7/7`. A failure blocks execution and requires plan amendment, not implementation improvisation.

## Global Constraints

- ShellBeam owns engineering-state semantics, not engineering reasoning.
- Verification optimizes for sufficient evidence, not maximal testing.
- Weakening/removing affected-surface information MUST NOT remove an otherwise applicable mandatory obligation unless an independent stronger policy/mechanical fact proves non-applicability.
- Cost MUST NOT participate in Stage A policy applicability or obligation derivation.
- `derivation_authority` and `coverage` are independent dimensions and are never collapsed into a scalar confidence score.
- `waived` is an `ObligationDisposition`, never an `EvidenceStatus`.
- Repository policy bytes are proposal/source facts, never approval authority.
- `policy_absent -> first policy` follows the same non-self-activation rule as policy replacement.
- A policy-changing source mutation cannot make its proposed policy authoritative for evaluating that same mutation.
- Policy Starter Profiles are templates only; no template silently becomes runtime policy and no template invents NFR targets.
- Policy-gap detection requires mechanically attributable classification; no filename/prose/LLM heuristic may create a sensitive-surface class.
- `Activity` remains the highest correlation primitive; do not introduce a durable Change aggregate.
- Stage A is no-auto-execution: `inspect.verification` may observe bounded repository facts but MUST NOT start project commands/tests/builds/load/race/E2E/full suites.
- `local_shell` remains the only MCP tool.
- Core packages use standard library only; TOML parsing stays in adapters.
- Production hard cap 500 lines/file, test hard cap 800, function hard cap 80, interface hard cap 8 methods.
- Preserve unrelated dirty state; no push/PR/merge unless explicitly requested.
- Every task uses focused RED -> minimum GREEN -> `go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json` -> tracked commit gate.

---

## File Structure Locked by This Plan

New core domain:

```text
internal/core/verification/
  identity.go          deterministic IDs/digests
  relation.go          affected subject/relation/authority/coverage
  policy.go            canonical materialized policy/rules/evidence requirements
  authority.go         activation/waiver/revocation authority facts
  obligation.go        obligation disposition, policy gap, Stage-A inspection types
  validation.go        closed-vocabulary validation + canonical normalization
  *_test.go
```

Application layer:

```text
internal/app/verification/
  ports.go             workspace/source/activity/policy-store/relation-provider ports
  policy_service.go    proposal/preview/activation/waiver authority semantics
  affected.go          recomputable affected-surface projection
  obligations.go       deterministic policy matching + policy gaps
  inspect.go           bounded Stage-A aggregate
  *_test.go
```

Adapters:

```text
internal/adapter/verification/
  policy_loader.go     .shellbeam/verification-policy.toml strict loader
  starter_profiles.go  deterministic prototype/team/production drafts
  go_relations.go      bounded filesystem + Go package/import relation facts
  *_test.go

internal/adapter/store/
  verification_policy.go
  verification_policy_test.go
```

Transport/composition:

```text
internal/adapter/ipc/verification_protocol_v2.go + verification_server/client tests
internal/adapter/mcp/verification_input.go + verification_call.go + focused tests
internal/app/bridge/verification.go + focused tests
internal/core/capability/verification.go + focused tests
cmd/shellbeam/verification.go + focused composition tests
api/schema/ipc-v2.json
api/schema/mcp-input-v2.json
api/schema/mcp-output-v2.json
```

The `internal/core/evidence` package is not modified in Stage A. Evidence satisfaction belongs to Stage B.

---

### Task 0: Freeze the practical baseline and benchmark harness

**Pre-Task-0 hard gate:** run `python3 scripts/check-verification-p1-plan-traceability.py`. Expected exactly: `PASS core=24/24 roadmap=4/4 review=11/11 deferred=7/7`. Do not create/modify Task-0 deliverables if it fails.

**Files:**
- Create: `scripts/benchmark-verification-p1.sh`
- Create: `docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md`
- Test: `tests/contract/markdown_test.go` only if the evidence document requires existing markdown-link coverage; do not add a test that freezes `selection=full` as desired behavior.

**Interfaces:**
- Produces exactly one machine-readable benchmark JSON object with fields `scenario`, `source_fingerprint`, `checkpoint_selection`, `checkpoint_wall_ms`, `commit_gate_selection`, `commit_gate_wall_ms`, `measurement_quality`, `status`. Historical approximate timing uses the same `checkpoint_wall_ms` field plus `measurement_quality=historical_approx`; no alternate `*_approx` field exists.
- Scenario 0 canonical name: `docs_only_four_markdown_specs`.
- Baseline operation/fingerprint remain exactly `checkpoint-verify-specs-20260818` / `8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e` in the evidence document.

- [ ] **Step 1: Write the benchmark script with a non-mutating default mode**

Create a script whose default action only prints the pinned historical baseline; `--measure-current` runs current commands against the caller's already-prepared dirty/staged state and never edits files itself:

```bash
#!/usr/bin/env bash
set -euo pipefail
mode="${1:-baseline}"
case "$mode" in
  baseline)
    printf '%s\n' '{"scenario":"docs_only_four_markdown_specs","source_fingerprint":"8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e","checkpoint_selection":"full","checkpoint_wall_ms":480000,"commit_gate_selection":"affected:contract:markdown","commit_gate_wall_ms":null,"measurement_quality":"historical_approx","status":"historical_baseline"}'
    ;;
  --measure-current)
    python3 - "${SHELLBEAM_BASE_REF:-origin/main}" <<'PYBENCH'
import json, subprocess, sys, time
base = sys.argv[1]
def run(kind, args):
    started = time.monotonic_ns()
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    wall_ms = (time.monotonic_ns() - started) // 1_000_000
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout + proc.stderr)
        raise SystemExit(proc.returncode)
    lines = [line for line in proc.stdout.splitlines() if line.lstrip().startswith("{")]
    if not lines:
        raise SystemExit(f"{kind}: missing devctl JSON")
    payload = json.loads(lines[-1])
    return payload, wall_ms
commit, commit_ms = run("commit_gate", ["go","run","./tools/devctl","commit-gate","--base",base,"--json"])
checkpoint, checkpoint_ms = run("checkpoint", ["go","run","./tools/devctl","verify","--checkpoint","--base",base,"--json"])
print(json.dumps({
    "scenario":"docs_only_four_markdown_specs",
    "source_fingerprint":checkpoint["source_fingerprint"],
    "checkpoint_selection":checkpoint["selection"],
    "checkpoint_wall_ms":checkpoint_ms,
    "commit_gate_selection":commit["selection"],
    "commit_gate_wall_ms":commit_ms,
    "measurement_quality":"measured_local",
    "status":"measured",
}, separators=(",",":"), sort_keys=True))
PYBENCH
    ;;
  *) echo "usage: $0 [baseline|--measure-current]" >&2; exit 2 ;;
esac
```

- [ ] **Step 2: Write the evidence document**

Record:

```text
scenario: docs_only_four_markdown_specs
historical operation: checkpoint-verify-specs-20260818
historical source fingerprint: 8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e
checkpoint selection: full
checkpoint elapsed: approximately 8 minutes on first cold/local run
pre-commit selection: affected -> contract:markdown
success criterion: preserve documentation correctness evidence while P1 inspection does not require broad Go package verification when policy + affected authority prove docs-only applicability
```

Do not claim the historical eight-minute number is a stable future runtime.

- [ ] **Step 3: Run RED-equivalent baseline sanity**

Run:

```bash
bash scripts/benchmark-verification-p1.sh baseline
```

Expected: one valid JSON line containing the exact historical fingerprint and `checkpoint_selection=full`.

- [ ] **Step 4: Run markdown/dirty verification and commit**

```bash
go test ./tests/contract -run Markdown -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add scripts/benchmark-verification-p1.sh docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: freeze verification semantics baseline"
```

---

### Task 1: Define the closed P1 verification core contracts

**Files:**
- Create: `internal/core/verification/identity.go`
- Create: `internal/core/verification/relation.go`
- Create: `internal/core/verification/policy.go`
- Create: `internal/core/verification/authority.go`
- Create: `internal/core/verification/obligation.go`
- Create: `internal/core/verification/validation.go`
- Create: focused `*_test.go` files in the same package
- Create: `internal/core/capability/verification.go`
- Create: `internal/core/capability/verification_test.go`
- Modify: `internal/core/capability/catalog.go` only for the thin field/projection hook; keep it below the production hard cap
- Modify: `internal/core/capability/catalog_test.go`

**Interfaces:**

Core closed vocabulary:

```go
package verification

type DerivationAuthority string
const (
    AuthorityAuthoritative DerivationAuthority = "authoritative"
    AuthorityMechanical    DerivationAuthority = "mechanical"
    AuthorityAdvisory      DerivationAuthority = "advisory"
)

type Coverage string
const (
    CoverageComplete Coverage = "complete"
    CoverageBounded  Coverage = "bounded"
    CoveragePartial  Coverage = "partial"
    CoverageUnknown  Coverage = "unknown"
)

type SubjectKind string
const (
    SubjectPath         SubjectKind = "path"
    SubjectSourceRef    SubjectKind = "source_ref"
    SubjectPackage      SubjectKind = "package"
    SubjectProjectCmd   SubjectKind = "project_command"
    SubjectSurfaceClass SubjectKind = "policy_surface_class"
)

type RelationBasis string
const (
    BasisObservedMutation RelationBasis = "observed_source_mutation"
    BasisImportGraph      RelationBasis = "import_graph"
    BasisProjectPolicy    RelationBasis = "project_policy"
    BasisExplicitMapping  RelationBasis = "explicit_project_mapping"
)

type Subject struct {
    Kind  SubjectKind `json:"kind"`
    Value string      `json:"value"`
}

type ProviderRef struct {
    ID      string `json:"id"`
    Version int    `json:"version"`
}

type AffectedDomainKind string
const (
    DomainSourceSelection     AffectedDomainKind = "source_selection"
    DomainGoImportGraph       AffectedDomainKind = "go_import_graph"
    DomainPolicyClassification AffectedDomainKind = "policy_classification"
)

type AffectedDomain struct {
    DomainID            string              `json:"domain_id"`
    Kind                AffectedDomainKind  `json:"kind"`
    DerivationAuthority DerivationAuthority `json:"derivation_authority"`
    Coverage            Coverage            `json:"coverage"`
    Provider            *ProviderRef        `json:"provider,omitempty"`
    SourceGeneration    string              `json:"source_generation"`
    ProvenanceRefs      []string            `json:"provenance_refs,omitempty"`
    CapturedAt          time.Time           `json:"captured_at"`
    Caveats             []string            `json:"caveats,omitempty"`
}

type AffectedRelation struct {
    RelationID          string              `json:"relation_id"`
    From                Subject             `json:"from_subject"`
    To                  Subject             `json:"to_subject"`
    Kind                string              `json:"relation_kind"`
    Basis               RelationBasis       `json:"basis"`
    DerivationAuthority DerivationAuthority `json:"derivation_authority"`
    Coverage            Coverage            `json:"coverage"`
    Provider            *ProviderRef        `json:"provider,omitempty"`
    SourceGeneration    string              `json:"source_generation"`
    ProvenanceRefs      []string            `json:"provenance_refs,omitempty"`
    CapturedAt          time.Time           `json:"captured_at"`
    Caveats             []string            `json:"caveats,omitempty"`
}

type AffectedSurface struct {
    SchemaVersion     int                `json:"schema_version"`
    RepositoryID     string             `json:"repository_id"`
    WorkspaceID      string             `json:"workspace_id"`
    SourceGeneration string             `json:"source_generation"`
    Domains          []AffectedDomain   `json:"domains"`
    Relations        []AffectedRelation `json:"relations"`
    Diagnostics      []string           `json:"diagnostics,omitempty"`
}

type AffectedSurfaceSummary struct {
    RelationCount int                         `json:"relation_count"`
    Domains       []AffectedDomain            `json:"domains"`
    ByAuthority   map[DerivationAuthority]int `json:"by_authority"`
    ByCoverage    map[Coverage]int            `json:"by_coverage"`
    Diagnostics   []string                    `json:"diagnostics,omitempty"`
}

type ObligationDisposition string
const (
    DispositionRequiredNow  ObligationDisposition = "required_now"
    DispositionDeferred     ObligationDisposition = "deferred"
    DispositionOptional     ObligationDisposition = "optional"
    DispositionNotTriggered ObligationDisposition = "not_triggered"
    DispositionWaived       ObligationDisposition = "waived"
)

type EvidenceStatus string
const (
    EvidenceNotEvaluated EvidenceStatus = "not_evaluated"
    EvidenceSatisfied    EvidenceStatus = "satisfied"
    EvidenceFailed       EvidenceStatus = "failed"
    EvidenceInsufficient EvidenceStatus = "insufficient"
    EvidenceInconsistent EvidenceStatus = "inconsistent"
    EvidenceUnknown      EvidenceStatus = "unknown"
    EvidenceUnavailable  EvidenceStatus = "unavailable"
)

type GateStatus string
const (
    GateClear         GateStatus = "clear"
    GateBlocked       GateStatus = "blocked"
    GateIndeterminate GateStatus = "indeterminate"
)

type Phase string
const (
    PhaseInnerLoop  Phase = "inner_loop"
    PhaseCheckpoint Phase = "checkpoint"
    PhasePreMerge   Phase = "pre_merge"
    PhaseRelease    Phase = "release"
    PhaseNightly    Phase = "nightly"
    PhasePeriodic   Phase = "periodic"
)
```

Initial policy model:

```go
type ProposalOrigin string
const (
    ProposalRepositoryAuthored ProposalOrigin = "repository_authored"
    ProposalStarterProfile    ProposalOrigin = "starter_profile"
    ProposalGenerated         ProposalOrigin = "generated_proposal"
)

type PolicySource string
const (
    // Frozen spec lists additional possible future source classes. P1 V1
    // deliberately implements the repository-pinned subset only: starter/generated
    // lineage survives separately as provenance and never changes effective source.
    PolicyRepositoryAuthored PolicySource = "repository_authored"
)

type PolicyContent struct {
    SchemaVersion int              `json:"schema_version"`
    PolicyID      string           `json:"policy_id"`
    Classifiers   []Classification `json:"classifications,omitempty"`
    Rules         []Rule           `json:"rules"`
}

type PolicyProposal struct {
    RepositoryID  string         `json:"repository_id"`
    Digest        string         `json:"policy_digest"`
    Origin        ProposalOrigin `json:"proposal_origin"`
    ProfileOrigin string         `json:"profile_origin,omitempty"`
    Content       PolicyContent  `json:"content"`
}

// PolicySnapshot is the immutable semantic object addressed by policy_digest.
// Digest is computed from Content only; authority/provenance never changes it.
type PolicySnapshot struct {
    RepositoryID string        `json:"repository_id"`
    Digest       string        `json:"policy_digest"`
    Content      PolicyContent `json:"content"`
}

// MaterializedPolicy is the effective runtime projection obtained by joining
// an immutable snapshot with a valid external PolicyActivation.
type MaterializedPolicy struct {
    Snapshot          PolicySnapshot `json:"snapshot"`
    Source            PolicySource   `json:"source"`
    ProfileOrigin     string         `json:"profile_origin,omitempty"`
    ApprovalRef       string         `json:"approval_ref"`
    ApprovalAuthority string         `json:"approval_authority"`
    ApprovedAt        time.Time      `json:"approved_at"`
}

type PolicySummary struct {
    PolicyID          string       `json:"policy_id"`
    Digest            string       `json:"policy_digest"`
    Source            PolicySource `json:"source"`
    ProfileOrigin     string       `json:"profile_origin,omitempty"`
    ApprovalRef       string       `json:"approval_ref,omitempty"`
    ApprovalAuthority string       `json:"approval_authority,omitempty"`
    ApprovedAt        time.Time    `json:"approved_at,omitempty"`
}

type PolicyProposalSummary struct {
    PolicyID      string         `json:"policy_id"`
    Digest        string         `json:"policy_digest"`
    Origin        ProposalOrigin `json:"proposal_origin"`
    ProfileOrigin string         `json:"profile_origin,omitempty"`
}

type Classification struct {
    ID           string   `json:"id"`
    Paths        []string `json:"paths"`
    SurfaceClass string   `json:"surface_class"`
}

type OwnershipClass string
const (
    OwnershipApplicationOwned OwnershipClass = "application_owned"
    OwnershipIntegrationOwned OwnershipClass = "integration_owned"
    OwnershipDelegated        OwnershipClass = "delegated"
)

type RiskClass string
const (
    RiskScaleDriven   RiskClass = "scale_driven"
    RiskRiskDriven    RiskClass = "risk_driven"
    RiskContextDriven RiskClass = "context_driven"
    RiskDelegated     RiskClass = "delegated"
)

type Rule struct {
    ID                       string                `json:"id"`
    Phases                   []Phase               `json:"phases"`
    MatchClasses             []string              `json:"match_classes,omitempty"`
    MatchPaths               []string              `json:"match_paths,omitempty"`
    Ownership                OwnershipClass        `json:"ownership"`
    RiskClass                RiskClass             `json:"risk_class,omitempty"`
    Required                 bool                  `json:"required"`
    SufficiencyBasis         string                `json:"sufficiency_basis"`
    MinimumAffectedAuthority DerivationAuthority   `json:"minimum_affected_authority"`
    Evidence                 []EvidenceRequirement `json:"evidence"`
}

type ProviderClass string
const (
    ProviderProjectCommand            ProviderClass = "project_command"
    ProviderStaticFormatCheck         ProviderClass = "static_format_check"
    ProviderFocusedBehaviorTest       ProviderClass = "focused_behavior_test"
    ProviderIntegrationTest           ProviderClass = "integration_test"
    ProviderTypecheckCompiler         ProviderClass = "typecheck_compiler"
    ProviderSchemaCompatibility       ProviderClass = "schema_compatibility"
    ProviderBrowserUserJourney        ProviderClass = "browser_user_journey"
    ProviderNativePlatformVerification ProviderClass = "native_platform_verification"
    ProviderArtifactDigest            ProviderClass = "artifact_digest"
    ProviderResourceMeasurement       ProviderClass = "resource_measurement"
    ProviderReleaseCheck              ProviderClass = "release_check"
)

type EnvironmentRequirement string
const (
    EnvironmentNone                EnvironmentRequirement = "none"
    EnvironmentSameCurrent         EnvironmentRequirement = "same_current"
    EnvironmentSameCurrentToolchain EnvironmentRequirement = "same_current_toolchain"
)

type StabilityRequirement string
const (
    StabilitySingleCurrentPass StabilityRequirement = "single_current_pass"
    StabilityNoContradiction   StabilityRequirement = "no_contradiction"
    StabilityFlakeProtocol     StabilityRequirement = "flake_protocol"
)

type FlakeProtocol struct {
    Runs        int `json:"runs"`
    MinPasses   int `json:"min_passes"`
    MaxFailures int `json:"max_failures"`
}


type ProviderExecutionSemantics struct {
    // nil means policy does not claim parallel safety.
    ParallelSafe           *bool    `json:"parallel_safe,omitempty"`
    SharedResources        []string `json:"shared_resources,omitempty"`
    ExclusiveResourceClass string   `json:"exclusive_resource_class,omitempty"`
    ExpectedWorkloadClass  string   `json:"expected_workload_class,omitempty"` // light|moderate|heavy|extreme; empty=unknown
}

type EvidenceRequirement struct {
    ID                 string                     `json:"id"`
    ProviderClass      ProviderClass              `json:"provider_class"`
    ProjectCommandID   string                     `json:"project_command_id,omitempty"`
    Params             map[string]string          `json:"params,omitempty"`
    MinimumAuthority   DerivationAuthority        `json:"minimum_authority"`
    RequireCurrent     bool                       `json:"require_current"`
    Environment        EnvironmentRequirement     `json:"environment"`
    Stability          StabilityRequirement       `json:"stability"`
    Flake              *FlakeProtocol             `json:"flake,omitempty"`
    RequireQuiescence  bool                       `json:"require_quiescence,omitempty"`
    Execution          ProviderExecutionSemantics `json:"execution,omitempty"`
}

type BoundEvidenceRequirement struct {
    Requirement                  EvidenceRequirement `json:"requirement"`
    ExpectedProjectBindingDigest string              `json:"expected_project_binding_digest,omitempty"`
}
```

If `ProjectCommandID` is present, the approved policy is explicitly classifying that exact project command+parameter binding as the provider instance for `ProviderClass`; Stage B must require the exact frozen project-command binding and must not infer the stronger class from the command name. `Params` are policy data, never model-inferred defaults. At obligation-derivation time the application resolves the exact current command binding (including declared/defaulted parameters) and stores only its digest in `BoundEvidenceRequirement`. A required parameter omitted by policy makes that requirement unresolved/invalid for activation rather than inviting ShellBeam to invent a value.

`Rule.MinimumAffectedAuthority` and `EvidenceRequirement.MinimumAuthority` are intentionally different dimensions. `MinimumAffectedAuthority` is the floor for affected-surface facts used to trigger/prove non-applicability of that rule; it never grades evidence. `EvidenceRequirement.MinimumAuthority` grades only evidence/provider facts used to satisfy the requirement.

`EvidenceRequirement.Execution` is policy-owned semantic metadata for the exact declared provider binding. It participates in `PolicyDigest`. Omitted values remain unknown; P1 never fills them from host pressure, command names, or historical telemetry. Validation rejects invalid resource IDs, duplicate shared resources, an exclusive resource class duplicated in `shared_resources`, and workload classes outside `light|moderate|heavy|extreme`. These fields are future Resource-Governor input only: P1 exposes them but never chooses a worker count or starts concurrent verification workers.

Stage-A `VerificationObligation.EvidenceStatus` always begins `not_evaluated` unless the evidence question itself is unavailable/unknown; Stage B owns actual evidence evaluation.

Stage A freezes the **complete P1 policy schema v1**, including environment/stability/flake/quiescence fields. Stage B implements their evaluators but MUST NOT add fields to policy schema v1; any later policy-language expansion requires a new policy schema version.

- [ ] **Step 1: Write failing enum/validation/digest tests**

Tests MUST prove:

```text
`TestAffectedRelationAuthorityCoverageIndependent`: authority and coverage validate independently
complete+advisory is legal
unknown+mechanical is legal
AffectedRelation requires source generation + provenance/basis and stable bounded subject values
`TestRelationIDIncludesDerivationSemantics`: same from/to/kind/generation but different basis/provider/provenance/authority/coverage -> different RelationID; timestamp/caveat-only changes -> same RelationID
AffectedDomain can be complete even with zero matching relations, preserving negative-query authority
`TestProviderExecutionSemanticsNeverChoosesUniversalConcurrency`: policy semantics round-trip/digest exactly, unknown stays unknown, and no worker-count field/API exists
AffectedSurfaceSummary is derived from domains/relations rather than separately authoritative
unknown ownership/risk class fails closed
waived cannot be parsed as EvidenceStatus
satisfied cannot be parsed as ObligationDisposition
mandatory Rule without sufficiency_basis is invalid
rule with no phase is invalid
rule with no evidence requirement is invalid when Required=true
flake_protocol without a valid bounded flake block is invalid
non-flake stability with a flake block is invalid
environment requirement outside none|same_current|same_current_toolchain is invalid
provider execution semantics preserve nil/unknown, reject invalid/duplicate resource claims, and never contain a worker-count field
changing any provider execution semantic changes PolicyDigest
`TestPolicyDigestCanonicalAndAuthorityIndependent`: policy digest is deterministic under canonical ordering
same PolicyContent has the same digest before/after activation projection
authority/proposal provenance cannot influence PolicyDigest
changing any rule/classification semantic changes PolicyDigest
unknown enum values fail closed
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/core/capability -count=1
```

Expected: FAIL because the package/capability does not exist.

- [ ] **Step 3: Implement minimum contracts and canonical normalization**

Implement deterministic sorting by stable IDs before digesting. Do not sort caller path-glob order inside one classifier/rule if ordering changes matching semantics; normalize only semantically unordered sets and reject duplicates.

Identity helpers:

```go
func PolicyDigest(content PolicyContent) (string, error)
func DomainID(kind AffectedDomainKind, provider *ProviderRef, generation string, provenanceRefs []string) (string, error)
type RelationIdentityInput struct {
    From                Subject
    To                  Subject
    Kind                string
    Basis               RelationBasis
    DerivationAuthority DerivationAuthority
    Coverage            Coverage
    Provider            *ProviderRef
    SourceGeneration    string
    ProvenanceRefs      []string
}
func RelationID(RelationIdentityInput) (string, error)
func ObligationID(policyDigest, ruleID, generation string, triggerRefs []string) (string, error)
func PolicyGapID(policyDigest, classID, generation string, surfaceRefs []string) (string, error)
```

`PolicyDigest` hashes canonical `PolicyContent` only. It MUST exclude repository ID, proposal origin/profile provenance, approval reference/authority/time, and the digest field itself. Activation binds that exact semantic digest; joining authority metadata later cannot alter it.

Derived semantic IDs use SHA-256 over canonical JSON and prefixes `pol_`, `dom_`, `rel_`, `obl_`, `gap_` where a public identity is needed. `RelationID` hashes the full canonical semantic derivation (`from`, `to`, `kind`, `basis`, `provider`, `derivation_authority`, `coverage`, `source_generation`, sorted/deduplicated provenance refs). It excludes `CapturedAt`, display caveats, and the ID field itself. Two derivations of the same logical edge under different policy digests/classification IDs/providers/authority/coverage therefore receive different relation IDs. Caller retry IDs are not semantic hashes: activation IDs must match `act_[A-Za-z0-9_-]{1,120}` and waiver IDs `wv_[A-Za-z0-9_-]{1,121}`; they carry idempotency identity only.

Also implement explicit non-probabilistic comparison helpers:

```go
func MeetsMinimumAuthority(actual, required DerivationAuthority) bool
func CoverageNoStrongerThan(candidate, reference Coverage) bool
```

The V1 authority matrix is `authoritative` satisfies all requirements, `mechanical` satisfies mechanical/advisory, and `advisory` satisfies advisory only. This is an authority lattice, not a confidence score. Coverage information order is `complete -> bounded -> partial -> unknown` only for monotonic-widening checks.

- [ ] **Step 4: Add capability shape without promotion**

Define capability metadata version 1 in `internal/core/capability/verification.go` with bounded limits, but do not mark `verification_semantics` available in the production daemon until the public wiring task. `catalog.go` receives only the minimal catalog/projection hook because it is already ~433 lines:

```go
type VerificationSemanticsSupport struct {
    SchemaVersions []int `json:"schema_versions,omitempty"`
    PolicySchemaVersions []int `json:"policy_schema_versions,omitempty"`
    MaxDomains int `json:"max_domains,omitempty"`
    MaxRelations int `json:"max_relations,omitempty"`
    MaxObligations int `json:"max_obligations,omitempty"`
    MaxPolicyRules int `json:"max_policy_rules,omitempty"`
}
```

Initial hard limits: 16 affected domains, 512 relations, 256 obligations, 128 policy gaps, 128 rules, 128 classifications, 32 evidence requirements per rule.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/core/verification internal/core/capability
go test ./internal/core/verification ./internal/core/capability -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/core/verification internal/core/capability
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: define verification semantics contracts"
```

---

### Task 2: Load strict repository policy and generate starter-policy previews

**Files:**
- Create: `internal/adapter/verification/policy_loader.go`
- Create: `internal/adapter/verification/policy_loader_test.go`
- Create: `internal/adapter/verification/starter_profiles.go`
- Create: `internal/adapter/verification/starter_profiles_test.go`
- Modify: `internal/app/verification/ports.go` if created by this task; otherwise create it here
- Test fixtures under `internal/adapter/verification/testdata/`

**Interfaces:**

Repository file path is exactly:

```text
.shellbeam/verification-policy.toml
```

TOML schema v1:

```toml
schema_version = 1
policy_id = "team-policy"
profile_origin = "shellbeam/team@v1" # optional provenance only; excluded from policy digest/authority

[[classifications]]
id = "documentation"
paths = ["docs/**"]
surface_class = "documentation"

[[rules]]
id = "docs-contract"
phases = ["inner_loop", "checkpoint", "pre_merge"]
match_classes = ["documentation"]
ownership = "application_owned"
required = true
sufficiency_basis = "repository_markdown_contract"
minimum_affected_authority = "mechanical"

[[rules.evidence]]
id = "docs-markdown"
provider_class = "project_command"
project_command_id = "docs_contract"
minimum_authority = "mechanical"
require_current = true
environment = "none"
stability = "no_contradiction"
execution = { parallel_safe = true, expected_workload_class = "light" }
```

`risk_class` is optional and accepts only `scale_driven|risk_driven|context_driven|delegated` in schema v1. `ownership` accepts only `application_owned|integration_owned|delegated`.

Loader contract:

```go
type PolicyLoadState string
const (
    PolicyLoadAbsent      PolicyLoadState = "absent"
    PolicyLoadValid       PolicyLoadState = "valid"
    PolicyLoadInvalid     PolicyLoadState = "invalid"
    PolicyLoadUnsupported PolicyLoadState = "unsupported"
)

type PolicyLoadResult struct {
    State      PolicyLoadState
    Proposal   *verification.PolicyProposal
    RawDigest  string
    Code       string
}

type PolicyLoader interface {
    Load(context.Context, workspace.Workspace) PolicyLoadResult
}
```

- [ ] **Step 1: Write failing strict-loader tests**

Prove:

```text
absent file -> absent
regular contained file -> valid
symlink escaping repo -> invalid
>64 KiB -> invalid
unknown TOML field -> invalid
schema_version 2 -> unsupported
missing sufficiency_basis -> invalid
duplicate rule/classification/evidence IDs -> invalid
invalid glob or absolute/parent path -> invalid
invalid/unbounded params map -> invalid
invalid environment/stability/flake combination -> invalid
profile_origin/raw bytes may preserve proposal provenance but never set approval/activation authority fields
`TestStarterRenderedRoundTripBecomesRepositoryAuthoredWithProfileProvenance`: starter preview -> rendered TOML -> loader yields repository_authored + same profile_origin + same PolicyDigest
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/verification -run 'Policy|Loader|Starter' -count=1
```

- [ ] **Step 3: Implement strict loader**

Use `toml.NewDecoder(...).DisallowUnknownFields()` in the adapter. Reuse the project loader's contained-symlink pattern; do not import `internal/adapter/project` because adapters may not import sibling adapters. Convert raw TOML structs into canonical `verification.PolicyContent`, compute `PolicyDigest(Content)`, then return a non-authoritative `PolicyProposal{Origin: repository_authored}`. No approval fields exist in repository TOML.

- [ ] **Step 4: Implement starter preview generation from existing project declarations**

Starter profiles are deterministic transforms of **explicit repository verification declarations**, not command cost/name heuristics:

```text
prototype:
  import steps only from repository verification profile "coding" when present
  map them to inner_loop
  do not select commands merely because cost=fast

team:
  prototype rules
  plus steps explicitly listed in repository verification profile "checkpoint"
  map those steps to checkpoint only
  do not silently promote checkpoint to pre_merge

production:
  team rules
  plus steps explicitly listed in repository verification profile "release"
  map those steps to release only
  `TestStarterProfilesNeverInventNFRTargets`: no invented performance/availability/scale target
```

`source_scope` describes the evidence produced by a command; it does **not** become an affected-surface trigger. Starter rules created from these named repository profiles are phase-wide because the repository explicitly put the command in that phase profile. Path/class-triggered narrowing requires an explicit verification-policy classification/rule. P1 starter proposals may reference only project commands that the typed project-command path can bind exactly. For P1 V1 that means **manifest v2 + direct argv**. Parameterized argv commands remain supported; Task 3 extends the existing Binder just enough to support fixed zero-parameter argv commands as an exact binding. Shell-form commands remain unsupported by typed binding in P1 V1. Manifest-v1 profile steps and shell-form steps are omitted with deterministic advisories (`typed_binding_requires_manifest_v2:<command_id>` / `typed_binding_shell_unsupported:<command_id>`), never emitted as requirements that activation cannot resolve. Commands with required parameters that lack declared defaults are omitted with `parameter_declaration_required:<command_id>`. ShellBeam never invents parameter values.

If the repository has no qualifying named verification profiles after this binding-eligibility filter, preview returns a valid proposal with zero hard rules plus advisory concern notes; it MUST NOT invent commands.

Expose:

```go
func PreviewStarter(profile string, repositoryID string, manifest *project.Manifest) (verification.PolicyProposal, []string, error)
```

Preview returns `PolicyProposal{Origin: starter_profile, ProfileOrigin: shellbeam/<profile>@v1}` only for the ephemeral preview. Proposal origin/profile metadata never serve as approval proof and are excluded from `PolicyDigest`.

The frozen spec's `user_approved_profile|user_approved_generated_policy|future_admin_policy` values remain reserved possible source classes; P1 V1 does not claim them because its activation flow deliberately requires repository-pinned bytes. This is a V1 capability subset, not a reinterpretation of those source values.

`verification.policy.preview` must also return deterministic `rendered_toml` for the proposal. It does **not** write the repository. To use a starter profile, the model/user writes those bytes to `.shellbeam/verification-policy.toml` through ordinary source-edit tooling, then previews/activates the **repository-loaded** proposal. After that round-trip the loader intentionally reports `ProposalOrigin=repository_authored`; `profile_origin=shellbeam/<profile>@v1` survives only as provenance. P1 V1 activates only repository-loaded snapshots, so every effective `MaterializedPolicy.Source` is `repository_authored`. The activation record freezes the repository proposal origin plus optional `ProfileOrigin`; it never reconstructs `user_approved_profile` as a different authority/source class. No P1 verification action mutates source files.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/adapter/verification internal/app/verification
go test ./internal/adapter/verification ./internal/core/verification -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/adapter/verification internal/app/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: load verification policy proposals"
```

---

### Task 3: Persist policy snapshots, activation authority, waivers, and revocations

**Files:**
- Create: `internal/adapter/store/verification_policy.go`
- Create: `internal/adapter/store/verification_policy_test.go`
- Create: `internal/app/verification/policy_service.go`
- Create: `internal/app/verification/policy_service_test.go`
- Modify: `internal/app/verification/ports.go`
- Modify: `internal/core/verification/authority_test.go`
- Create: `internal/adapter/verification/project_command_source.go`
- Create: `internal/adapter/verification/project_command_source_test.go`
- Modify: `internal/app/project/binder.go`
- Modify: `internal/app/project/binder_test.go`

**Interfaces:**

Authority writes are **intent-first**. Caller-stable intent is fingerprinted before the store generates any timestamp; daemon-generated time is never part of retry equality.

```go
type PolicyActivationIntent struct {
    ActivationID            string
    RepositoryID            string
    PreviousEffectiveDigest string // "absent" or digest
    ProposedPolicyDigest    string
    ProposalGeneration      string
    Authority               string
    Actor                   string
}

type PolicyActivationCommit struct {
    Intent               PolicyActivationIntent
    ProposalOrigin       ProposalOrigin
    ProfileOrigin        string
    ActivationGeneration string // daemon-observed exactly once on first-create
}

type VerificationWaiverIntent struct {
    WaiverID     string
    RepositoryID string
    PolicyDigest string
    RuleID       string
    Phase        Phase
    Generation   string
    CheckpointID string
    Authority    string
    Actor        string
    Reason       string
    ExpiresAt    time.Time
    ExpiresPhase Phase
}

type WaiverRevocationIntent struct {
    RepositoryID string // resolved by PolicyService from workspace_id; never trusted as a public caller-supplied authority selector
    WaiverID     string
    Authority    string
    Actor        string
}
```

Durable authority records include `IntentFingerprint` and the daemon timestamp created exactly once on first-create:

```go
type PolicyActivation struct {
    SchemaVersion           int       `json:"schema_version"`
    ActivationID            string    `json:"activation_id"`
    IntentFingerprint       string    `json:"intent_fingerprint"`
    RepositoryID            string    `json:"repository_id"`
    PreviousEffectiveDigest string    `json:"previous_effective_policy_digest"` // "absent" or digest
    ProposedPolicyDigest    string         `json:"proposed_policy_digest"`
    ProposalOrigin          ProposalOrigin `json:"proposal_origin"`
    ProfileOrigin           string         `json:"profile_origin,omitempty"`
    ProposalGeneration      string         `json:"proposal_generation"`
    ActivationGeneration    string    `json:"activation_generation"`
    Authority               string    `json:"authority"`
    Actor                   string    `json:"actor"`
    ActivatedAt             time.Time `json:"activated_at"` // daemon-recorded
}

type VerificationWaiver struct {
    SchemaVersion int       `json:"schema_version"`
    WaiverID      string    `json:"waiver_id"`
    IntentFingerprint string `json:"intent_fingerprint"`
    RepositoryID  string    `json:"repository_id"`
    PolicyDigest  string    `json:"policy_digest"`
    RuleID        string    `json:"rule_id"`
    Phase         Phase     `json:"phase"`
    Generation    string    `json:"generation,omitempty"`
    CheckpointID  string    `json:"checkpoint_id,omitempty"`
    Authority     string    `json:"authority"`
    Actor         string    `json:"actor"`
    Reason        string    `json:"reason"`
    CreatedAt     time.Time `json:"created_at"`
    ExpiresAt     time.Time `json:"expires_at,omitempty"`
    ExpiresPhase  Phase     `json:"expires_phase,omitempty"`
}

type WaiverRevocation struct {
    SchemaVersion int       `json:"schema_version"`
    WaiverID      string    `json:"waiver_id"`
    IntentFingerprint string `json:"intent_fingerprint"`
    Authority     string    `json:"authority"`
    Actor         string    `json:"actor"`
    RevokedAt     time.Time `json:"revoked_at"`
}
```

Store layout is create-once and auditable:

```text
<state>/verification/policies/<repository_id>/<policy_digest>.json
<state>/verification/activations/<repository_id>/<activation_id>.json
<state>/verification/effective/<repository_id>.json            # non-authoritative atomic index
<state>/verification/waivers/<repository_id>/<waiver_id>.json
<state>/verification/waiver_revocations/<repository_id>/<waiver_id>.json
```

No file containing an existing immutable ID may be replaced with different bytes.

Application ports expose replay/effective state explicitly; a historical successful activation is not necessarily current:

```go
type ActivationWriteResult struct {
    Record    PolicyActivation
    Created   bool
    Replayed  bool
    Effective bool
}
type WaiverWriteResult struct {
    Record   VerificationWaiver
    Created  bool
    Replayed bool
    Active   bool
}
type RevocationWriteResult struct {
    Record   WaiverRevocation
    Created  bool
    Replayed bool
}

type PolicyAuthorityStore interface {
    PutPolicySnapshot(context.Context, verification.PolicySnapshot) (bool, error)
    FindActivation(context.Context, workspace.RepositoryID, string) (verification.PolicyActivation, bool, error)
    ActivatePolicyCAS(context.Context, verification.PolicyActivationCommit) (verification.ActivationWriteResult, error)
    CurrentActivation(context.Context, workspace.RepositoryID) (verification.PolicyActivation, bool, error)
    LoadPolicySnapshot(context.Context, workspace.RepositoryID, string) (verification.PolicySnapshot, bool, error)
}

type WaiverAuthorityStore interface {
    FindWaiver(context.Context, workspace.RepositoryID, string) (verification.VerificationWaiver, bool, error)
    PutWaiver(context.Context, verification.VerificationWaiverIntent) (verification.WaiverWriteResult, error)
    FindWaiverRevocation(context.Context, workspace.RepositoryID, string) (verification.WaiverRevocation, bool, error)
    PutWaiverRevocation(context.Context, verification.WaiverRevocationIntent) (verification.RevocationWriteResult, error)
    ListWaivers(context.Context, workspace.RepositoryID) ([]verification.VerificationWaiver, []verification.WaiverRevocation, error)
}
```

Activation freshness and project-command cross-reference/binding are application concerns exposed through core-only ports:

```go
type SourceSnapshotter interface {
    ObserveFresh(context.Context, string) workspace.FastSnapshot // cwd/root
}
```

Project-command cross-reference/binding is an application concern exposed through core-only ports:

```go
type ProjectInspector interface {
    Inspect(context.Context, string) (project.Inspection, error)
}
type ProjectCommandResolver interface {
    Resolve(context.Context, string, string, map[string]string) (project.CommandBinding, error)
}
```

`internal/adapter/verification/project_command_source.go` wraps the existing `internal/app/project.Binder` to implement `ProjectCommandResolver`; `internal/app/verification` therefore does not import a sibling app package. Task 3 first extends that Binder in one deliberately narrow way: manifest-v2 **fixed zero-parameter direct argv** commands become bindable with `Parameters=[]`, `ParameterFingerprint=ParameterFingerprint([])`, exact manifest/argv/cwd/evidence metadata, and the existing `CommandBinding.Digest()`. Existing parameterized argv behavior is unchanged. Manifest-v1 commands remain rejected, and shell-form commands remain `ProjectCommandNotParameterized`/unsupported for typed P1 binding.

Tests MUST include `TestBinderBindsV2FixedArgvWithoutParameters`, a shell-form rejection case, a manifest-v1 rejection case, and replay/digest stability for the fixed argv binding. Before activation, every policy requirement with `ProjectCommandID != ""` MUST resolve through this exact Binder contract against a current valid/review-due Project Manifest. Missing command, unsupported command shape, required missing parameter, invalid/absent manifest, or unavailable repo-path/package validation makes the proposed policy ineligible for activation; the system never downgrades the requirement to an unbound provider class.

- [ ] **Step 1: Write failing immutable-store tests**

Prove semantic-snapshot create-once idempotency, conflicting content for the same digest fails closed, proposal provenance does not alter snapshot bytes/digest, activation preserves origin/profile provenance separately, and the intent-first retry contract. Named cases include `TestActivationRetryPreservesFirstTimestampAndNeverRollsBackIndex`, `TestWaiverRetryPreservesFirstTimestamp`, `TestWaiverRevocationRetryPreservesFirstTimestamp`, and `TestPolicyActivationIsImmutableAuditableAuthority`: same ID + same canonical caller intent replays the original durable record/time/**activation generation** without a fresh observation; same ID + different intent conflicts; orphan recovery may complete an index only while the current index is still the expected predecessor; if a later activation is current, retrying an older activation returns `Effective=false` historical result and MUST NOT roll the index backward. Also prove current-index mismatch/corruption fails closed, restart persistence, malformed stored records fail, and revocation never mutates the original waiver file.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/project ./internal/adapter/store ./internal/adapter/verification ./internal/app/verification -run 'Binder|Verification|Policy|Activation|Waiver' -count=1
```

- [ ] **Step 3: Implement activation self-amendment rules**

`PolicyService.Activate` receives the exact current proposal plus the source generation at which that proposal was observed:

```go
type ActivateRequest struct {
    ActivationID           string // caller-supplied retry/idempotency identity
    WorkspaceID            string
    ProposedPolicyDigest   string
    ExpectedPreviousDigest string // "absent" or exact digest
    ProposalGeneration     string
    Authority              string // V1: explicit_caller
    Actor                  string
}
```

`PolicyService.Activate` first canonicalizes the caller-stable `PolicyActivationIntent` (repository identity is resolved from workspace) and computes its intent fingerprint. It then calls `FindActivation` **before** reading current proposal bytes or observing a fresh generation. If the ID exists, same intent replays that historical record/result; different intent conflicts. Only a genuinely new ID proceeds to proposal validation, project-command binding checks, and `SourceSnapshotter.ObserveFresh(ctx, workspace.Root)`. Fresh quality must be authoritative enough and generation non-empty. V1 implements the frozen "subsequent cut" conservatively as a **different source generation** at first-create. The resulting `ActivationGeneration`, proposal origin/profile provenance, and daemon `ActivatedAt` are server-derived first-create facts, excluded from caller intent fingerprint and reused forever on retry. Checkpoint-based same-generation activation can be added only by a versioned extension. `ActivationID` carries idempotency identity only and no authority by itself.

Rules:

```text
current policy proposal digest must equal ProposedPolicyDigest
store's current effective digest must equal ExpectedPreviousDigest
for a new ActivationID only: fresh ActivationGeneration MUST be valid and MUST differ from ProposalGeneration
for an existing same-intent ActivationID: replay uses the originally recorded ActivationGeneration/ActivatedAt and performs no new activation-cut observation
activation authority must come from configured non-policy authority classes
policy bytes cannot introduce/grant their own activation authority
first policy uses ExpectedPreviousDigest="absent"
all project-command requirements must resolve to exact valid current bindings
semantic PolicySnapshot is persisted before the activation record; proposal origin/profile provenance is frozen into the activation record, not the digest
ActivatePolicyCAS atomically rechecks ExpectedPreviousDigest under the store lock before append/index update
same ActivationID + same canonical `PolicyActivationIntent` fingerprint replays the original record, including original ActivatedAt
same ActivationID + different intent fingerprint conflicts without changing effective policy
retry lookup happens by ActivationID before any new timestamp is generated
orphan index recovery is allowed only when the index still names ExpectedPreviousDigest; a later current activation is never rolled backward by retry
actor is bounded UTF-8 audit metadata (1..128 bytes) and is never authority by itself
```

Store implementation also maintains a non-authoritative atomic index:

```text
<state>/verification/effective/<repository_id>.json
```

The index contains only the current activation ID/digest for bounded lookup. The immutable activation record remains the authority fact. `PolicyActivationCommit` carries the already-observed first-create generation/provenance, but the store fingerprints **only `Commit.Intent`**. Under the store lock, it first looks up `ActivationID`; if absent it validates the intent and commit facts, persists `IntentFingerprint`, stamps `ActivatedAt` exactly once, writes the immutable record, and atomically replaces the index. A crash after record creation but before index replacement leaves an orphan non-effective record. Same-intent retry may complete that index update only if the index is still the recorded predecessor. If any later activation is already current, retry returns `ActivationWriteResult{Replayed:true, Effective:false}` with the original record and never rewinds the index. If this activation is current/recovered it returns `Effective:true`. `CurrentActivation` validates the index target before returning it.

After successful activation, that activation is the repository's current effective policy for later observations until another CAS activation supersedes it. Requiring the source transition before activation ensures the mutation generation that introduced/replaced the policy can never be evaluated by the policy it introduced, without inventing a new durable Change aggregate. Reverting later to identical bytes is a later observation after authority activation and does not resurrect the old authority state.

- [ ] **Step 4: Implement waiver validity without evidence rewriting**

`TestWaiverScopeExpiryPolicyDigestDeterministic` covers exact policy/rule/phase/generation/checkpoint/expiry matching. `SetWaiver` canonicalizes caller-stable `VerificationWaiverIntent` and performs `FindWaiver` before any timestamp generation. New IDs validate the referenced effective policy/rule, configured authority class, bounded reason, phase/generation/checkpoint scope, and deterministic expiry; the store stamps `CreatedAt` once. Same ID/same intent returns the original record/time; same ID/different intent conflicts. If a matching revocation already exists, replay returns `WaiverWriteResult{Replayed:true, Active:false}` and never removes/replaces the revocation. Waiver reason is required and bounded to 1..1024 UTF-8 bytes; actor is bounded as above. `RevokeWaiver` resolves `RepositoryID` from the request's `workspace_id`, then applies the same lookup/fingerprint-first rule with `WaiverRevocationIntent`; repository scope participates in the intent fingerprint so identical `WaiverID` values in different repositories cannot cross-revoke. Stamp `RevokedAt` once on first-create, create-once durable revocation, same-intent replay returns original timestamp. `ActiveWaivers(...)` returns current waiver facts but never changes evidence status.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/app/project internal/adapter/store internal/adapter/verification internal/app/verification internal/core/verification
go test ./internal/app/project ./internal/adapter/store ./internal/adapter/verification ./internal/app/verification ./internal/core/verification -count=1
go test -race ./internal/app/project ./internal/adapter/store ./internal/app/verification -run 'Binder|Verification|Policy|Activation|Waiver' -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/app/project internal/adapter/store internal/adapter/verification internal/app/verification internal/core/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: persist verification policy authority"
```

---

### Task 4: Derive bounded affected surface from workspace/activity facts

**Files:**
- Create: `internal/app/verification/affected.go`
- Create: `internal/app/verification/affected_test.go`
- Modify: `internal/app/verification/ports.go`
- Create: `internal/adapter/verification/go_relations.go`
- Create: `internal/adapter/verification/go_relations_test.go`

**Interfaces:**

Ports (reuse Stage-A `SourceSnapshotter` from Task 3):

```go
type WorkspaceLookup interface {
    Inspect(context.Context, string) (workspace.Workspace, error)
}
type WorkspaceSampler interface {
    Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample
}
type ActivitySelector interface {
    CompareWorkspace(context.Context, string, workspace.DeltaSample) (activity.Comparison, error)
}
type RelationResult struct {
    Domains     []verification.AffectedDomain
    Relations   []verification.AffectedRelation
    Diagnostics []string
}

type RelationProvider interface {
    Derive(context.Context, workspace.Workspace, string, []string) RelationResult
}
```

Affected request/result:

```go
type AffectedRequest struct {
    WorkspaceID string
    ActivityID  string
}

type AffectedResult struct {
    Surface verification.AffectedSurface
}
```

The affected pipeline is intentionally two-stage: derive the policy-independent base surface first, then apply classifications from the exact effective `MaterializedPolicy` when one exists. Proposed policy is never gate input.

The `source_selection` domain is emitted even when there are zero changed paths. Its authority/coverage comes from the existing workspace/activity selection contract. Observed mutation relations use `from=source_ref:<current generation>`, `to=path:<repo-relative path>`, and `basis=observed_source_mutation`.

- [ ] **Step 1: Write failing activity/workspace selection tests**

Prove:

```text
workspace dirty selection emits authoritative/mechanical path relations
activity selection uses ObservedSinceBaseline, not inherited dirty paths
baseline divergence degrades coverage and conservatively keeps affected paths
unavailable delta returns unknown/unavailable surface, never empty-complete
partial delta never claims complete coverage
changed policy file is present as an affected path relation
clean/zero-change complete selection still emits a complete source_selection domain with zero mutation relations
```

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/verification -run Affected -count=1
```

- [ ] **Step 3: Implement narrow Go relation provider without subprocesses**

Use standard library filesystem + `go/parser`/`go/token` in the adapter; inspection must not spawn `go list`. The first provider builds a bounded repository-local static import graph so affected analysis can find **dependents**, not only dependencies:

```text
changed .go path -> containing package directory
scan root-module Go files -> package -> repository-local import edges
changed package -> direct and transitive reverse importers until closure or hard bound
repository-local import target -> exact package directory only when root go.mod module mapping is deterministic
import relation authority = mechanical
coverage = complete only for the static root-module import domain when the whole eligible file set is scanned, parsed, mapped, and relation closure is untruncated
multiple/nested go.mod modules, unreadable/parse-failed files, limit exhaustion, or ambiguous module mapping degrade coverage to partial/bounded with diagnostics
runtime reflection/plugin/config/network dependencies are outside this provider domain and are never silently claimed complete
non-Go paths receive no invented import relation
```

Hard work limits: at most 256 changed paths, 2,048 Go files, 16 MiB total Go source bytes, 512 emitted relations, and 5 seconds total relation-provider budget. Hitting any bound preserves the discovered relations but degrades coverage; it never turns unknown dependents into `not_triggered`.

The provider always emits one `go_import_graph` domain record when Go analysis is applicable, including when no reverse-import relation matches. This domain's coverage/authority is the fact consumers use to decide whether absence of a relation is meaningful.

- [ ] **Step 4: Project classifications from the exact effective policy only**

`RelationProvider.Derive(...)` owns only policy-independent mechanical source/import derivation. It MUST NOT read repository proposal bytes or either policy summary. Add a separate pure projection:

```go
type ClassificationProjectionRequest struct {
    BaseSurface     verification.AffectedSurface
    EffectivePolicy verification.MaterializedPolicy
}
func ApplyEffectiveClassifications(ClassificationProjectionRequest) (verification.AffectedSurface, error)
```

For every exact path-glob match in `EffectivePolicy.Snapshot.Content.Classifiers`, emit:

```text
from: path
relation_kind: classified_as
to: policy_surface_class
basis: project_policy
derivation_authority: mechanical
coverage: no stronger than source_selection coverage
provenance: effective policy digest + classification ID
```

Also emit one `policy_classification` domain bound to the **exact effective policy digest**. The domain exists even with zero matches. Proposed policy P2 may have a separate preview-only classification projection returned by `verification.policy.preview`, but that preview MUST NOT feed `inspect.verification`, obligation matching, policy gaps, or gate evaluation until P2 has an external activation at a subsequent cut.

Named acceptance `TestProposedPolicyCannotChangeEffectiveClassificationProjection` covers both P2 removing a P1 classifier and P2 adding a new classifier: P1 classification relations/domains/obligations remain byte-stable until activation. Do not infer a class from path names outside declared mappings.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/app/verification internal/adapter/verification
go test ./internal/app/verification ./internal/adapter/verification -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/app/verification internal/adapter/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: derive verification affected surface"
```

---

### Task 5: Match policy into obligations and advisory policy gaps

**Files:**
- Create: `internal/app/verification/obligations.go`
- Create: `internal/app/verification/obligations_test.go`
- Extend: `internal/core/verification/obligation.go`
- Extend focused core tests

**Interfaces:**

```go
type VerificationObligation struct {
    SchemaVersion       int                   `json:"schema_version"`
    ObligationID        string                `json:"obligation_id"`
    PolicyDigest        string                `json:"policy_digest"`
    SourceRuleID        string                `json:"source_rule_id"`
    TriggerRefs         []string              `json:"trigger_refs"`
    AffectedScopeRefs   []string              `json:"affected_scope_refs"`
    Ownership           OwnershipClass        `json:"ownership"`
    RiskClass           RiskClass             `json:"risk_class,omitempty"`
    RequiredPhase       Phase                 `json:"required_phase"`
    SufficiencyBasis    string                `json:"sufficiency_basis"`
    MinimumAffectedAuthority DerivationAuthority `json:"minimum_affected_authority"`
    EvidenceRequirements []BoundEvidenceRequirement `json:"evidence_requirements"`
    AppliesToGeneration string                 `json:"applies_to_generation"`
    Disposition         ObligationDisposition `json:"disposition"`
    EvidenceStatus      EvidenceStatus        `json:"evidence_status"`
    EvidenceRefs        []string              `json:"evidence_refs,omitempty"`
    WaiverID            string                `json:"waiver_id,omitempty"`
}
```

Stage A always sets evidence status to `not_evaluated`, except when the evidence question itself cannot be formed because a declared provider class is unavailable; that case is `unavailable`. It never emits `satisfied` in Stage A.

V1 selector semantics are deterministic:

```text
MatchPaths empty AND MatchClasses empty -> phase-wide surface match
otherwise any MatchPath OR any MatchClass positive match -> surface match
AND/composite expressions are not in policy schema v1
```

Negative applicability is stricter: a rule may become `not_triggered` for surface mismatch only when every relevant selector domain can mechanically establish non-match. A complete `source_selection` domain can prove path-glob non-match; class non-match additionally requires a sufficiently strong `policy_classification` domain. `bounded|partial|unknown` or advisory-only information cannot prove non-match for a mandatory rule, so verification stays the same or widens with reason `applicability_uncertain_widened`.

Phase fold for pipeline phases is also closed:

```text
inner_loop < checkpoint < pre_merge < release
nightly and periodic are independent exact-match schedules
```

For a surface-matching required rule: exact current phase -> `required_now`; a later declared pipeline phase -> `deferred`; only already-passed pipeline phases or independent non-current schedules -> `not_triggered` by phase. Non-required matching rules are `optional` in their declared current phase and otherwise non-blocking/not-triggered. A policy wanting the same requirement at multiple phases lists each phase explicitly.

Advisory gap shape is also closed in core:

```go
type PolicyGap struct {
    GapID                string              `json:"gap_id"`
    SurfaceRef           string              `json:"surface_ref"`
    DeclaredClass        string              `json:"declared_class"`
    ClassificationSource string              `json:"classification_source"`
    MissingPolicyClass   string              `json:"missing_policy_class"`
    Authority            DerivationAuthority `json:"authority"`
    ProvenanceRefs       []string            `json:"provenance_refs"`
}
```

`PolicyGap` is inspectable advisory state only; it is never folded into the current gate unless a later approved policy materializes a matching mandatory rule.

- [ ] **Step 1: Write failing policy-match tests**

Required cases:

```text
phase-wide rule with no selectors at current phase -> required_now
any matching class OR path at current phase -> required_now
same surface rule for later pipeline phase -> deferred
past-only or independent non-current phase -> not_triggered
non-required matching rule at current phase -> optional
mechanically proven trigger non-match with complete relevant domains -> not_triggered
zero matching relations with partial/unknown domain -> NOT enough for not_triggered
`TestWaiverDispositionPreservesEvidenceStatus`: valid active waiver -> waived while evidence remains not_evaluated/unavailable
partial/unknown affected information cannot convert a previously applicable mandatory rule to not_triggered
`TestUncertainAffectedSurfaceCannotNarrowMandatoryObligation`
`TestFullSuiteExistsOnlyWhenPolicyTriggersIt`
`TestDelegatedOwnershipRulePreservesIntegrationAssumptionRequirement`: delegated ownership does not remove an explicitly declared integration-assumption evidence requirement
full suite exists only when an approved rule requires its provider/command
no performance target/rule -> no invented load obligation
```

- [ ] **Step 2: Write failing policy-gap tests**

A classification such as `internal/auth/** -> security_sensitive` plus no approved matching rule emits `PolicyGap`. A file merely named `password.go` with no classification emits no gap. Name the mechanical-source case `TestPolicyGapRequiresMechanicalClassification`.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/app/verification ./internal/core/verification -run 'Obligation|PolicyGap|Disposition' -count=1
```

- [ ] **Step 4: Implement deterministic matching**

Matching order is stable by rule ID. The matcher uses only the base surface plus classification projection from the exact effective policy digest; proposed-policy classifications are excluded. `Rule.MinimumAffectedAuthority` is enforced on trigger/non-applicability facts before disposition; it is unrelated to evidence `MinimumAuthority`. The matcher uses both relation facts and `AffectedDomain` quality; relation absence without a strong relevant domain is never proof of non-applicability. Unknown/partial information obeys the information-order rule: lack of proof of non-applicability cannot produce `not_triggered`. For every matching requirement with `ProjectCommandID`, resolve the exact current project binding from policy params/defaults and place its digest in `BoundEvidenceRequirement`; unresolved binding yields explicit unavailable/invalid policy detail rather than a weakened requirement. Waiver fold happens after applicability and never changes `EvidenceStatus`.

- [ ] **Step 5: Run GREEN and commit**

```bash
gofmt -w internal/app/verification internal/core/verification
go test ./internal/app/verification ./internal/core/verification -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/app/verification internal/core/verification
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: derive verification obligations"
```

---

### Task 6: Build the Stage-A aggregate service and one-tool APIs

**Files:**
- Create: `internal/app/verification/inspect.go`
- Create: `internal/app/verification/inspect_test.go`
- Create: `internal/app/bridge/verification.go`
- Create: `internal/app/bridge/verification_test.go`
- Modify: `internal/app/bridge/client_port.go` only for thin verification methods
- Create: `internal/adapter/ipc/verification_protocol_v2.go`
- Create: `internal/adapter/ipc/verification_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go` only for thin action/union registration; it is already ~458 lines
- Modify: current IPC server/client dispatch files only with thin routing hooks
- Create: `internal/adapter/mcp/verification_input.go`
- Create: `internal/adapter/mcp/verification_call.go`
- Create: `internal/adapter/mcp/verification_test.go`
- Modify: `internal/adapter/mcp/input.go` only for thin union/validation registration; it is already ~449 lines
- Modify: `internal/adapter/mcp/call.go` only for thin dispatch routing
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create: `cmd/shellbeam/verification.go`
- Create: `cmd/shellbeam/verification_test.go`
- Create: `tests/contract/verification_truth_boundary_test.go`
- Modify: `cmd/shellbeam/command_daemon.go` only to inject/register the verification service; keep business logic in `verification.go`

**Interfaces:**

Public actions remain under `local_shell`:

```json
{"action":"inspect.verification","workspace_id":"ws_...","phase":"inner_loop","activity_id":"optional"}
{"action":"verification.policy.preview","workspace_id":"ws_..."}
{"action":"verification.policy.preview","workspace_id":"ws_...","profile":"team"}
{"action":"verification.policy.activate","activation_id":"act_...","workspace_id":"ws_...","proposed_policy_digest":"...","expected_previous_policy_digest":"absent","proposal_generation":"gen_...","authority":"explicit_caller","actor":"trung"}
{"action":"verification.waiver.set","waiver_id":"wv_...","workspace_id":"ws_...","policy_digest":"...","rule_id":"native_linux","phase":"checkpoint","generation":"gen_...","authority":"explicit_caller","actor":"trung","reason":"native Linux verification occurs in CI"}
{"action":"verification.waiver.revoke","workspace_id":"ws_...","waiver_id":"wv_...","authority":"explicit_caller","actor":"trung"}
```

For `verification.policy.preview`, omitting `profile` strictly loads/previews the current repository policy file; providing `profile` generates a starter proposal/rendered TOML without writing it. Cross-action/ambiguous fields are rejected.

`explicit_caller` is the only built-in activation/waiver authority in P1 V1. It means an explicit `local_shell` authority event outside repository bytes; it does **not** claim the caller is an authenticated human. Policy files cannot create additional authority classes. A future authenticated user/admin authority requires a versioned authority provider rather than relabeling `explicit_caller`.

Inspection shape:

```go
type Inspection struct {
    SchemaVersion int                      `json:"schema_version"`
    Phase         verification.Phase       `json:"phase"`
    RepositoryID  string                   `json:"repository_id"`
    WorkspaceID   string                   `json:"workspace_id"`
    SourceGeneration string                `json:"source_generation"`
    EffectivePolicy *verification.PolicySummary `json:"effective_policy,omitempty"`
    ProposedPolicy  *verification.PolicyProposalSummary `json:"proposed_policy,omitempty"`
    PolicyState     string                 `json:"policy_state"` // absent|effective|proposal_pending|invalid|unsupported
    Affected        verification.AffectedSurfaceSummary `json:"affected_surface"`
    Obligations     []verification.VerificationObligation `json:"obligations"`
    PolicyGaps      []verification.PolicyGap `json:"policy_gaps,omitempty"`
}
```

Stage A deliberately omits aggregate `gate_status`; Stage B adds it after evidence evaluation exists. This prevents a read-only not-evaluated projection from presenting a misleading completion-like gate.

Completion truth boundary is normative across both stages. No core/public response struct or JSON schema may expose `task_complete`, `work_complete`, `safe_to_finish`, or an equivalent user-task completion boolean. `tests/contract/verification_truth_boundary_test.go` scans the production verification surface and schemas for those forbidden JSON keys/translation labels while allowing explicit negative-contract test strings. Name the contract `TestVerificationSurfaceForbidsCompletionTruthFields`. `GateStatus=clear` later means only that current mandatory verification obligations under the effective policy/cut are cleared; it never means the user's requested task/work is complete.

- [ ] **Step 1: Write failing closed-schema and no-spawn tests**

Prove each action rejects cross-action fields; `TestVerificationInspectionNeverSpawnsProviders` proves inspect/preview never call process start; invalid policy does not crash or silently become absent; legacy capability projection omits new fields; exactly one MCP tool remains registered; `TestPolicyAbsentDoesNotSelectStarter`; and `TestVerificationSurfaceForbidsCompletionTruthFields` proves core/public structs + JSON Schemas reject/omit `task_complete|work_complete|safe_to_finish`.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'Verification|Policy|Waiver' -count=1
```

- [ ] **Step 3: Wire services and schemas**

Compose the policy loader, authority store, workspace/activity sources, and relation provider once at daemon startup. Ordinary `start/poll/write/kill` paths MUST NOT call verification services. `inspect.verification` does not run project commands.

Before GREEN, run `go run ./tools/devctl check`; if any touched legacy file approaches a hard cap, move P1-specific code into the dedicated verification file rather than extending the legacy file. Do not split unrelated legacy code as part of P1.

- [ ] **Step 4: Promote capability only after transport/service tests pass**

Advertise `verification_semantics=available`, schema v1, policy schema v1, and the exact hard limits from Task 1. Legacy v1 catalog projection must omit P1-only metadata.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge cmd/shellbeam internal/core/capability
go test ./internal/app/verification ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam ./internal/core/capability -count=1
go test -race ./internal/app/verification ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git add internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge cmd/shellbeam internal/core/capability api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: expose verification obligations"
```

---

### Task 7: Stage-A real-daemon acceptance and semantic checkpoint

**Files:**
- Create: `cmd/shellbeam/verification_semantics_test.go`
- Extend: `docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md`
- Update this plan checkboxes only after evidence exists

**Acceptance fixtures:**

1. `TestPolicyAbsentDoesNotSelectStarter`: repository with no policy -> `policy_state=absent`, no starter silently selected;
2. `TestFirstPolicyRequiresExternalActivationSubsequentCut`: first policy bytes appear -> proposed only, no effective policy;
3. `TestPolicyCannotActivateForItsIntroducingGeneration`: activation attempted while the fresh workspace generation still equals proposal generation -> rejected;
4. source generation transitions then explicit activation -> policy becomes effective;
5. `TestProposedPolicyCannotSelfGrantActivationOrWaiverAuthority`: P1->weaker P2 source edit -> P2 proposed, P1 remains authoritative until valid later activation and cannot grant itself authority;
6. docs-only affected surface -> only docs policy rule required; Go package verification is not invented;
7. shared Go package change -> package/import relations carry independent authority+coverage;
8. declared security-sensitive path with no rule -> advisory policy gap;
9. `password.go` outside declared classification -> no security policy gap;
10. valid waiver -> obligation disposition `waived`, evidence status still `not_evaluated`/`unavailable`;
11. partial relation provider -> mandatory rule not narrowed;
12. no verification action causes a project command/test/build spawn;
13. manifest-v2 fixed zero-param argv project command binds exactly and can back an activated requirement; manifest-v1/shell-form remain explicitly unsupported for P1 typed binding;
14. `TestPinnedPolicyUnaffectedByStarterTemplateChange` plus round-trip fixture: starter rendered TOML becomes `repository_authored` while preserving `profile_origin` provenance; later starter-template changes do not alter pinned effective digest;
15. P2 adding/removing classifiers does not change P1 classification domain/relations/obligations until P2 activation (`TestProposedPolicyCannotChangeEffectiveClassificationProjection`);
16. relation derivations that differ only in policy digest/classification provenance/authority/coverage have different relation IDs;
17. activation same-intent retry preserves first daemon timestamp and never rolls current index backward after a later activation;
18. policy_absent remains explicit and no response exposes user-task completion truth.

- [ ] **Step 1: Write RED real-daemon acceptance tests**

Use a temporary attached Git repository and exact `.shellbeam/verification-policy.toml` fixtures. Do not rely only on in-memory fake services. Include named anchors `TestFirstPolicyRequiresExternalActivationSubsequentCut`, `TestProposedPolicyCannotSelfGrantActivationOrWaiverAuthority`, `TestPolicyActivationIsImmutableAuditableAuthority`, and `TestPinnedPolicyUnaffectedByStarterTemplateChange`.

- [ ] **Step 2: Run focused acceptance**

```bash
go test ./cmd/shellbeam -run VerificationSemantics -count=1
```

Expected before final wiring fixes: FAIL on at least one missing/incorrect integrated contract.

- [ ] **Step 3: Make minimum integration fixes and run GREEN**

```bash
go test ./cmd/shellbeam -run VerificationSemantics -count=1
go test ./internal/core/verification ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
go test -race ./internal/app/verification ./internal/adapter/verification ./internal/adapter/store -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
```

- [ ] **Step 4: Practical Task-0 comparison**

On a disposable worktree/fixture, produce an `inspect.verification` result for the docs-only four-Markdown shape and record:

```text
required rule/provider classes
affected relation count + authority/coverage
model-visible bytes
tool-call count
inspection wall time
whether broad Go/full-suite obligation was absent/not_triggered by approved policy
```

Do not claim runtime savings from inspection alone; Stage B benchmark measures actual evidence selection/sufficiency.

- [ ] **Step 5: Commit Stage-A acceptance**

```bash
git add cmd/shellbeam/verification_semantics_test.go docs/superpowers/evidence/2026-08-18-verification-semantics-p1-baseline.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: verify p1 verification obligations"
```

- [ ] **Step 6: Checkpoint handoff**

```bash
go run ./tools/devctl verify --checkpoint --base "${SHELLBEAM_BASE_REF:-origin/main}" --json
git status --short --branch
git log --oneline --decorate -8
```

Handoff must include exact checkpoint source fingerprint and any provider/coverage limitation discovered. Do not begin Stage B if Stage-A semantics contradict the frozen spec.

## Self-Review Checklist

- [ ] `python3 scripts/check-verification-p1-plan-traceability.py` passes `core=24/24 roadmap=4/4 review=11/11 deferred=7/7` before Task 0.
- [ ] Every Stage-A spec requirement has a task: policy source/authority split, first-policy bootstrap, self-amendment, starter templates, no invented NFR targets, affected authority×coverage, uncertainty monotonicity, policy gaps, dispositions, waivers, no auto-execution.
- [ ] No task treats `waived` as evidence.
- [ ] No task computes scalar confidence or residual-risk probability.
- [ ] No repository field such as `approved_by` is trusted as activation authority.
- [ ] Policy activation is impossible on the same proposal generation.
- [ ] No new durable Change aggregate exists.
- [ ] No new MCP tool exists.
- [ ] No production package exceeds project structural limits by plan design.
- [ ] Stage A does not claim `gate_status=clear`; evidence sufficiency is intentionally Stage B.
- [ ] Task 0 preserves the historical full-checkpoint baseline as evidence, not as desired behavior.
