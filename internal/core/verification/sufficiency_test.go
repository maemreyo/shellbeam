package verification

import (
	"strings"
	"testing"
)

func TestRequirementEvaluationIDBindsCurrentIdentityAndSortedEvidenceRefs(t *testing.T) {
	input := EvaluationIdentityInput{
		PolicyDigest:  "pol_" + strings.Repeat("a", 64),
		RuleID:        "security",
		ObligationID:  "obl_" + strings.Repeat("b", 64),
		RequirementID: "integration",
		EvidenceRefs: []string{
			"ev_" + strings.Repeat("2", 64),
			"ev_" + strings.Repeat("1", 64),
		},
	}
	id, err := EvaluationID(input)
	if err != nil {
		t.Fatal(err)
	}
	reordered := input
	reordered.EvidenceRefs = []string{input.EvidenceRefs[1], input.EvidenceRefs[0]}
	again, err := EvaluationID(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if id != again || !isDerivedID(id, "eval_") {
		t.Fatalf("evaluation identity unstable id=%q again=%q", id, again)
	}
	reordered.RequirementID = "other"
	other, err := EvaluationID(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if other == id {
		t.Fatal("requirement identity did not affect evaluation id")
	}
}

func TestRequirementAndObligationEvaluationValidateClosedFacts(t *testing.T) {
	req := RequirementEvaluation{
		PolicyDigest: "pol_" + strings.Repeat("b", 64),
		RuleID:       "rule", ObligationID: "obl_" + strings.Repeat("c", 64), RequirementID: "req",
		Status: EvidenceSatisfied, EvidenceRefs: []string{"ev_" + strings.Repeat("d", 64)},
	}
	req.EvaluationID, _ = EvaluationID(EvaluationIdentityInput{
		PolicyDigest: req.PolicyDigest, RuleID: req.RuleID, ObligationID: req.ObligationID,
		RequirementID: req.RequirementID, EvidenceRefs: req.EvidenceRefs,
	})
	if err := req.Validate(); err != nil {
		t.Fatalf("valid requirement evaluation rejected: %v", err)
	}
	obl := ObligationEvaluation{ObligationID: req.ObligationID, EvidenceStatus: EvidenceSatisfied, RequirementResults: []RequirementEvaluation{req}, EvidenceRefs: append([]string(nil), req.EvidenceRefs...)}
	if err := obl.Validate(); err != nil {
		t.Fatalf("valid obligation evaluation rejected: %v", err)
	}
	bad := req
	bad.Status = "waived"
	if err := bad.Validate(); err == nil {
		t.Fatal("waived accepted as evidence evaluation status")
	}
}

func TestEvidenceCandidateKnownProviderPreservesLiteralAuthority(t *testing.T) {
	candidate := validEvidenceCandidate()
	candidate.ProviderClass = ProviderIntegrationTest
	candidate.ProviderClassKnown = true
	candidate.Authority = AuthorityAdvisory
	candidate.AuthorityKnown = true
	if err := candidate.Validate(); err != nil {
		t.Fatalf("known advisory provider candidate rejected: %v", err)
	}
}
