package verification

import (
	"reflect"
	"strings"
	"testing"
	"time"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func sufficiencyGeneration(c byte) string { return "gen_" + strings.Repeat(string(c), 64) }
func sufficiencyDigest(c byte) string     { return strings.Repeat(string(c), 64) }
func sufficiencyEvidenceID(c byte) string { return "ev_" + strings.Repeat(string(c), 64) }

func sufficiencyCandidate(id byte, provider core.ProviderClass, authority core.DerivationAuthority, result core.CandidateResult) core.EvidenceCandidate {
	candidate := core.EvidenceCandidate{
		EvidenceID: sufficiencyEvidenceID(id), VerificationKind: evidence.VerificationTest,
		ProviderClass: provider, ProviderClassKnown: true,
		OperationID: "op-" + string(id), SessionID: "session-" + string(id), WorkspaceID: "ws_01K00000000000000000000000",
		SourceGeneration: sufficiencyGeneration('1'), SourceContentDigest: sufficiencyDigest('2'),
		ContractDigest: sufficiencyDigest('3'), SemanticContractDigest: sufficiencyDigest('3'),
		Authority: authority, AuthorityKnown: true, Freshness: core.CandidateCurrent, Result: result,
		CompletedAt: time.Unix(int64(id), 0).UTC(),
	}
	if provider == core.ProviderProjectCommand {
		candidate.ProjectCommandID = "test_package"
		candidate.ProjectBindingDigest = sufficiencyDigest('4')
	}
	return candidate
}

func sufficiencyRequirement(id string, provider core.ProviderClass) core.BoundEvidenceRequirement {
	return core.BoundEvidenceRequirement{Requirement: core.EvidenceRequirement{
		ID: id, ProviderClass: provider, MinimumAuthority: core.AuthorityMechanical,
		RequireCurrent: true, Environment: core.EnvironmentNone, Stability: core.StabilityNoContradiction,
	}}
}

func sufficiencyObligation(requirements ...core.BoundEvidenceRequirement) core.VerificationObligation {
	return core.VerificationObligation{
		SchemaVersion: 1, ObligationID: "obl_" + strings.Repeat("a", 64), PolicyDigest: "pol_" + strings.Repeat("b", 64),
		SourceRuleID: "security", TriggerRefs: []string{"trigger:security"}, AffectedScopeRefs: []string{"scope:auth"},
		Ownership: core.OwnershipApplicationOwned, RiskClass: core.RiskRiskDriven, RequiredPhase: core.PhaseCheckpoint,
		SufficiencyBasis: "declared security verification", MinimumAffectedAuthority: core.AuthorityMechanical,
		EvidenceRequirements: requirements, AppliesToGeneration: sufficiencyGeneration('1'),
		Disposition: core.DispositionRequiredNow, EvidenceStatus: core.EvidenceNotEvaluated,
	}
}

func available(classes ...core.ProviderClass) ProviderAvailability {
	out := ProviderAvailability{ByClass: map[core.ProviderClass]Availability{}, Reasons: map[core.ProviderClass]string{}}
	for _, class := range classes {
		out.ByClass[class] = AvailabilityAvailable
	}
	return out
}

func currentEnvironment(env, tool byte) *environment.Binding {
	binding := &environment.Binding{
		SnapshotID: "env_" + strings.Repeat("9", 64), EnvironmentFingerprint: sufficiencyDigest(env),
		EnvironmentFingerprintVersion: 1, CapturedAt: time.Unix(100, 0).UTC(),
	}
	if tool != 0 {
		binding.ToolchainFingerprint = sufficiencyDigest(tool)
		binding.ToolchainFingerprintVersion = 1
	}
	return binding
}

func oneRequirement(t *testing.T, got core.ObligationEvaluation) core.RequirementEvaluation {
	t.Helper()
	if len(got.RequirementResults) != 1 {
		t.Fatalf("requirement results=%#v", got.RequirementResults)
	}
	return got.RequirementResults[0]
}

func TestRequirementEvaluationBindsCurrentObligationWithoutMutatingEvidence(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	before := candidate
	got := EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil)
	result := oneRequirement(t, got)
	if result.Status != core.EvidenceSatisfied || result.PolicyDigest == "" || result.RuleID != "security" || result.ObligationID == "" || result.RequirementID != "integration" || result.EvaluationID == "" {
		t.Fatalf("evaluation=%#v", result)
	}
	if !reflect.DeepEqual(candidate, before) {
		t.Fatalf("historical evidence mutated: before=%#v after=%#v", before, candidate)
	}
}

func TestCheapInsufficientEvidenceCannotSatisfyStrongerRequirement(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	cheap := sufficiencyCandidate('a', core.ProviderStaticFormatCheck, core.AuthorityMechanical, core.CandidatePass)
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{cheap}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceInsufficient || got.ReasonCode != "provider_semantics_mismatch" {
		t.Fatalf("cheap provider satisfied stronger requirement: %#v", got)
	}
}

func TestRequirementMinimumAuthorityRejectsAdvisoryProviderEvidence(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	advisory := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityAdvisory, core.CandidatePass)
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{advisory}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceInsufficient || got.ReasonCode != "evidence_authority_insufficient" {
		t.Fatalf("advisory evidence satisfied mechanical requirement: %#v", got)
	}
}

func TestProjectCommandBindingElevatesOnlyCurrentRequirementSemantics(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	requirement.Requirement.ProjectCommandID = "test_package"
	requirement.ExpectedProjectBindingDigest = sufficiencyDigest('4')
	candidate := sufficiencyCandidate('a', core.ProviderProjectCommand, core.AuthorityMechanical, core.CandidatePass)
	before := candidate
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, ProviderAvailability{}, nil))
	if got.Status != core.EvidenceSatisfied {
		t.Fatalf("bound project command did not satisfy current semantic class: %#v", got)
	}
	if !reflect.DeepEqual(candidate, before) || candidate.ProviderClass != core.ProviderProjectCommand {
		t.Fatalf("candidate provider class was rewritten: before=%#v after=%#v", before, candidate)
	}
	mismatch := candidate
	mismatch.ProjectBindingDigest = sufficiencyDigest('5')
	got = oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{mismatch}, Coverage: core.CoverageComplete}, ProviderAvailability{}, nil))
	if got.Status != core.EvidenceInsufficient || got.ReasonCode != "project_binding_mismatch" {
		t.Fatalf("old project binding satisfied current requirement: %#v", got)
	}
}

func TestStaleEvidenceCannotSatisfyCurrentRequirement(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	candidate.Freshness = core.CandidateStale
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceInsufficient || got.ReasonCode != "evidence_stale" {
		t.Fatalf("stale evidence satisfied current requirement: %#v", got)
	}
}

func TestEnvironmentDependentEvidenceRequiresDeclaredBinding(t *testing.T) {
	for _, tc := range []struct {
		name        string
		requirement core.EnvironmentRequirement
		mutate      func(*core.EvidenceCandidate)
		current     *environment.Binding
		reason      string
	}{
		{name: "candidate environment absent", requirement: core.EnvironmentSameCurrent, mutate: func(c *core.EvidenceCandidate) {}, current: currentEnvironment('6', 0), reason: "evidence_environment_unbound"},
		{name: "environment mismatch", requirement: core.EnvironmentSameCurrent, mutate: func(c *core.EvidenceCandidate) {
			c.EnvironmentFingerprint = sufficiencyDigest('7')
			c.EnvironmentFingerprintVersion = 1
		}, current: currentEnvironment('6', 0), reason: "environment_mismatch"},
		{name: "toolchain mismatch", requirement: core.EnvironmentSameCurrentToolchain, mutate: func(c *core.EvidenceCandidate) {
			c.EnvironmentFingerprint = sufficiencyDigest('6')
			c.EnvironmentFingerprintVersion = 1
			c.ToolchainFingerprint = sufficiencyDigest('8')
			c.ToolchainFingerprintVersion = 1
		}, current: currentEnvironment('6', '7'), reason: "toolchain_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
			requirement.Requirement.Environment = tc.requirement
			candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
			tc.mutate(&candidate)
			got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), tc.current))
			if got.Status != core.EvidenceInsufficient || got.ReasonCode != tc.reason {
				t.Fatalf("environment constraint=%#v", got)
			}
		})
	}

	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	requirement.Requirement.Environment = core.EnvironmentSameCurrent
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	candidate.EnvironmentFingerprint = sufficiencyDigest('6')
	candidate.EnvironmentFingerprintVersion = 1
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceUnavailable || got.ReasonCode != "current_environment_unavailable" {
		t.Fatalf("missing current environment=%#v", got)
	}
}

func TestRequirementLiteralFailureAmbiguousIncompleteAndContradiction(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	for _, tc := range []struct {
		result core.CandidateResult
		want   core.EvidenceStatus
	}{
		{core.CandidateFail, core.EvidenceFailed},
		{core.CandidateIncomplete, core.EvidenceInsufficient},
		{core.CandidateAmbiguous, core.EvidenceUnknown},
	} {
		candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, tc.result)
		got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
		if got.Status != tc.want {
			t.Fatalf("result=%s evaluation=%#v", tc.result, got)
		}
	}
	fail := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidateFail)
	pass := sufficiencyCandidate('b', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{fail, pass}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceInconsistent {
		t.Fatalf("compatible contradiction=%#v", got)
	}
}

func TestRequirementNoCandidateUsesMechanicalProviderAvailability(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	for _, tc := range []struct {
		availability Availability
		want         core.EvidenceStatus
		reason       string
	}{
		{AvailabilityAvailable, core.EvidenceNotEvaluated, "no_evidence"},
		{AvailabilityUnavailable, core.EvidenceUnavailable, "provider_unavailable"},
		{AvailabilityUnknown, core.EvidenceUnknown, "provider_availability_unknown"},
	} {
		providers := ProviderAvailability{ByClass: map[core.ProviderClass]Availability{core.ProviderIntegrationTest: tc.availability}}
		got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Coverage: core.CoverageComplete}, providers, nil))
		if got.Status != tc.want || got.ReasonCode != tc.reason {
			t.Fatalf("availability=%s got=%#v", tc.availability, got)
		}
	}
}

func TestBoundedEvidenceHistoryCannotSatisfyMandatoryStability(t *testing.T) {
	requirement := sufficiencyRequirement("integration", core.ProviderIntegrationTest)
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageBounded}, available(core.ProviderIntegrationTest), nil))
	if got.Status == core.EvidenceSatisfied || got.ReasonCode != "bounded_evidence_history" {
		t.Fatalf("bounded history satisfied mandatory requirement: %#v", got)
	}
}

func TestRiskControlsAreLiteralRequirementsWithoutResidualRisk(t *testing.T) {
	requirement := sufficiencyRequirement("security-control", core.ProviderIntegrationTest)
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	obligation := sufficiencyObligation(requirement)
	obligation.RiskClass = core.RiskRiskDriven
	got := EvaluateObligation(obligation, CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil)
	if got.EvidenceStatus != core.EvidenceSatisfied {
		t.Fatalf("literal risk control evaluation=%#v", got)
	}
}

func TestQuiescenceRequirementStaysUnavailableUntilTask4(t *testing.T) {
	requirement := sufficiencyRequirement("quiescence", core.ProviderIntegrationTest)
	requirement.Requirement.RequireQuiescence = true
	candidate := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	got := oneRequirement(t, EvaluateObligation(sufficiencyObligation(requirement), CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest), nil))
	if got.Status != core.EvidenceUnavailable || got.ReasonCode != "quiescence_not_implemented" {
		t.Fatalf("Task3 invented quiescence proof: %#v", got)
	}
}

func TestObligationEvidenceStatusFoldPrecedence(t *testing.T) {
	failReq := sufficiencyRequirement("fail", core.ProviderIntegrationTest)
	unknownReq := sufficiencyRequirement("unknown", core.ProviderStaticFormatCheck)
	fail := sufficiencyCandidate('a', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidateFail)
	ambiguous := sufficiencyCandidate('b', core.ProviderStaticFormatCheck, core.AuthorityMechanical, core.CandidateAmbiguous)
	got := EvaluateObligation(sufficiencyObligation(failReq, unknownReq), CandidateResultSet{Candidates: []core.EvidenceCandidate{fail, ambiguous}, Coverage: core.CoverageComplete}, available(core.ProviderIntegrationTest, core.ProviderStaticFormatCheck), nil)
	if got.EvidenceStatus != core.EvidenceFailed || len(got.RequirementResults) != 2 {
		t.Fatalf("obligation fold=%#v", got)
	}
}
