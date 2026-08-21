package verification

import (
	"strings"
	"testing"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	structuredresult "github.com/maemreyo/shellbeam/internal/core/structuredresult"
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

func TestEvidenceCandidateStructuredDetailIsValidatedButExcludedFromCompatibilityIdentity(t *testing.T) {
	base := compatibilityCandidate()
	key, ok := CompatibilityKey(base)
	if !ok {
		t.Fatal("base compatibility unavailable")
	}
	base.StructuredDetail = &StructuredEvidenceDetail{
		DerivationKey: strings.Repeat("a", 64), ParseOutcome: structuredresult.ParsePartial, Completeness: structuredresult.CompletenessPartial,
		CompletenessReason:     structuredresult.CompletenessReasonPassRecordsElided,
		ObservedEntries:        &structuredresult.ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 2, Pass: 1, Fail: 1},
		MechanicalTestStatuses: []structuredresult.TestStatus{structuredresult.TestFailed, structuredresult.TestPassed},
		SemanticsCoverage:      &structuredresult.ProducerSemanticsCoverage{Namespace: "jest", VocabularyVersion: 1, Format: "json", Family: "v30", MechanicallyObservable: []string{"coarse:pass"}, Unavailable: []string{"jest:error_status"}},
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	got, ok := CompatibilityKey(base)
	if !ok || got != key {
		t.Fatalf("read-time structured detail changed compatibility key got=%q want=%q", got, key)
	}
	base.StructuredDetail.MechanicalTestStatuses = []structuredresult.TestStatus{structuredresult.TestPassed, structuredresult.TestPassed}
	if err := base.Validate(); err == nil {
		t.Fatal("duplicate structured statuses accepted")
	}
}
