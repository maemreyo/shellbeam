package verification

import (
	"strings"
	"testing"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func stabilityCandidate(n int, result core.CandidateResult) core.EvidenceCandidate {
	hex := []byte(strings.Repeat("a", 64))
	hex[n%64] = "0123456789abcdef"[n%16]
	candidate := core.EvidenceCandidate{
		EvidenceID: "ev_" + string(hex), VerificationKind: evidence.VerificationTest,
		ProviderClass: core.ProviderProjectCommand, ProviderClassKnown: true, ProjectCommandID: "test_package",
		OperationID: "op-" + string(rune('a'+n)), SessionID: "session-" + string(rune('a'+n)), WorkspaceID: "ws_01K00000000000000000000000",
		SourceGeneration: "gen_" + strings.Repeat("1", 64), SourceContentDigest: strings.Repeat("2", 64),
		ProjectBindingDigest: strings.Repeat("3", 64), ContractDigest: strings.Repeat("4", 64), SemanticContractDigest: strings.Repeat("4", 64),
		Authority: core.AuthorityMechanical, AuthorityKnown: true,
		Freshness: core.CandidateCurrent, Result: result, CompletedAt: time.Unix(int64(n+1), 0).UTC(),
	}
	if err := candidate.Validate(); err != nil {
		panic(err)
	}
	return candidate
}

func completeCandidates(candidates ...core.EvidenceCandidate) CandidateResultSet {
	return CandidateResultSet{Candidates: candidates, Coverage: core.CoverageComplete}
}

func TestStabilityPassOnlySatisfiedAndFailOnlyFailed(t *testing.T) {
	pass := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(stabilityCandidate(1, core.CandidatePass)))
	if pass.Status != core.EvidenceSatisfied || len(pass.Cohorts) != 1 {
		t.Fatalf("pass fold=%#v", pass)
	}
	fail := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(stabilityCandidate(2, core.CandidateFail)))
	if fail.Status != core.EvidenceFailed || len(fail.Cohorts) != 1 {
		t.Fatalf("fail fold=%#v", fail)
	}
}

func TestCompatibleFailThenPassIsInconsistent(t *testing.T) {
	fail := stabilityCandidate(1, core.CandidateFail)
	pass := stabilityCandidate(2, core.CandidatePass)
	got := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(fail, pass))
	if got.Status != core.EvidenceInconsistent || len(got.Cohorts) != 1 || got.Cohorts[0].Counts.Passes != 1 || got.Cohorts[0].Counts.Failures != 1 {
		t.Fatalf("FAIL->PASS fold=%#v", got)
	}
	reversed := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(pass, fail))
	if reversed.Status != got.Status || reversed.Cohorts[0].Status != got.Cohorts[0].Status {
		t.Fatalf("result depended on input order: forward=%#v reversed=%#v", got, reversed)
	}
}

func TestSourceMutationSeparatesEvidenceCohortsWithoutRewritingHistory(t *testing.T) {
	fail := stabilityCandidate(1, core.CandidateFail)
	pass := stabilityCandidate(2, core.CandidatePass)
	pass.SourceGeneration = "gen_" + strings.Repeat("9", 64)
	pass.SourceContentDigest = strings.Repeat("8", 64)
	got := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(fail, pass))
	if got.Status == core.EvidenceInconsistent || len(got.Cohorts) != 2 || len(got.EvidenceRefs) != 2 {
		t.Fatalf("source mutation collapsed history: %#v", got)
	}
	withoutMutation := stabilityCandidate(3, core.CandidatePass)
	got = EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(fail, withoutMutation))
	if got.Status != core.EvidenceInconsistent || len(got.Cohorts) != 1 {
		t.Fatalf("same generation failed to preserve contradiction: %#v", got)
	}
}

func TestDiagnosticRerunDoesNotEraseContradiction(t *testing.T) {
	root := stabilityCandidate(1, core.CandidateFail)
	rerun := stabilityCandidate(2, core.CandidatePass)
	rerun.Attempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: root.EvidenceID, RerunReason: evidence.RerunDiagnoseFlake}
	got := EvaluateStability(core.StabilityNoContradiction, nil, completeCandidates(root, rerun))
	if got.Status != core.EvidenceInconsistent || got.DiagnosticReruns != 1 {
		t.Fatalf("diagnostic rerun erased contradiction: %#v", got)
	}
}

func TestApprovedFlakeProtocolRequiresQualifiedRuns(t *testing.T) {
	protocol := &core.FlakeProtocol{Runs: 3, MinPasses: 2, MaxFailures: 1}
	root := stabilityCandidate(1, core.CandidateFail)
	q1 := stabilityCandidate(2, core.CandidatePass)
	q2 := stabilityCandidate(3, core.CandidatePass)
	q3 := stabilityCandidate(4, core.CandidateFail)
	for _, candidate := range []*core.EvidenceCandidate{&q1, &q2, &q3} {
		candidate.Attempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: root.EvidenceID, RerunReason: evidence.RerunFlakeQualification}
	}
	got := EvaluateStability(core.StabilityFlakeProtocol, protocol, completeCandidates(root, q1, q2, q3))
	if got.Status != core.EvidenceSatisfied || got.Flake == nil || got.Flake.Counts.Runs != 3 || got.Flake.Counts.Passes != 2 || got.Flake.Counts.Failures != 1 || got.Flake.ProtocolID == "" {
		t.Fatalf("qualified flake protocol did not resolve: %#v", got)
	}

	missingRoot := q1
	missingRoot.EvidenceID = "ev_" + strings.Repeat("f", 64)
	missingRoot.Attempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: "ev_" + strings.Repeat("e", 64), RerunReason: evidence.RerunFlakeQualification}
	bad := EvaluateStability(core.StabilityFlakeProtocol, protocol, completeCandidates(root, missingRoot, q2, q3))
	if bad.Status == core.EvidenceSatisfied || bad.Flake == nil || bad.Flake.Counts.Runs != 2 {
		t.Fatalf("missing provenance qualified a flake run: %#v", bad)
	}
}

func TestUnknownCompatibilityAndBoundedHistoryCannotSatisfyStability(t *testing.T) {
	unknown := stabilityCandidate(1, core.CandidatePass)
	unknown.SourceGeneration = ""
	got := EvaluateStability(core.StabilitySingleCurrentPass, nil, completeCandidates(unknown))
	if got.Status != core.EvidenceUnknown || got.ReasonCode != "compatibility_unknown" {
		t.Fatalf("unknown compatibility satisfied stability: %#v", got)
	}
	bounded := CandidateResultSet{Candidates: []core.EvidenceCandidate{stabilityCandidate(2, core.CandidatePass)}, Coverage: core.CoverageBounded}
	got = EvaluateStability(core.StabilityNoContradiction, nil, bounded)
	if got.Status != core.EvidenceUnknown || got.ReasonCode != "bounded_evidence_history" {
		t.Fatalf("bounded history satisfied stability: %#v", got)
	}
}
