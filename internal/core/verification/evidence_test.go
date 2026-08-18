package verification

import (
	"strings"
	"testing"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

func validEvidenceCandidate() EvidenceCandidate {
	return EvidenceCandidate{
		EvidenceID: "ev_" + strings.Repeat("a", 64), VerificationKind: evidence.VerificationTest,
		OperationID: "op-1", SessionID: "session-1", WorkspaceID: "ws_01K00000000000000000000000",
		ContractDigest: strings.Repeat("b", 64), SemanticContractDigest: strings.Repeat("b", 64),
		Freshness: CandidateCurrent, Result: CandidatePass, CompletedAt: time.Unix(1, 0).UTC(),
	}
}

func TestEvidenceCandidateValidatesClosedFreshnessAndResultVocabulary(t *testing.T) {
	base := validEvidenceCandidate()
	if err := base.Validate(); err != nil {
		t.Fatalf("valid candidate rejected: %v", err)
	}
	for name, mutate := range map[string]func(*EvidenceCandidate){
		"freshness": func(v *EvidenceCandidate) { v.Freshness = "freshish" },
		"result":    func(v *EvidenceCandidate) { v.Result = "ok" },
		"kind":      func(v *EvidenceCandidate) { v.VerificationKind = "lint" },
		"contract":  func(v *EvidenceCandidate) { v.SemanticContractDigest = "bad" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatalf("invalid candidate accepted: %#v", candidate)
			}
		})
	}
}

func TestEvidenceCandidateKnownProjectProviderRequiresMechanicalTypedAuthority(t *testing.T) {
	candidate := validEvidenceCandidate()
	candidate.ProviderClass = ProviderProjectCommand
	candidate.ProviderClassKnown = true
	candidate.ProjectCommandID = "check"
	candidate.ProjectBindingDigest = strings.Repeat("c", 64)
	candidate.Authority = AuthorityMechanical
	candidate.AuthorityKnown = true
	if err := candidate.Validate(); err != nil {
		t.Fatalf("typed candidate rejected: %v", err)
	}
	candidate.AuthorityKnown = false
	if err := candidate.Validate(); err == nil {
		t.Fatal("known typed provider accepted without known authority")
	}
}
