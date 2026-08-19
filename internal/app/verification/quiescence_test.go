package verification

import (
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func TestSufficiencyConsumesQuiescenceWithoutRewritingEvidence(t *testing.T) {
	requirement := sufficiencyRequirement("quiescence", core.ProviderIntegrationTest)
	requirement.Requirement.RequireQuiescence = true
	candidate := sufficiencyCandidate('c', core.ProviderIntegrationTest, core.AuthorityMechanical, core.CandidatePass)
	obligation := sufficiencyObligation(requirement)
	base := CandidateResultSet{Candidates: []core.EvidenceCandidate{candidate}, Coverage: core.CoverageComplete}
	complete := core.QuiescenceObservation{SchemaVersion: 1, OperationID: candidate.OperationID, SessionID: candidate.SessionID, Status: core.QuiescenceComplete, ObservedAt: time.Unix(100, 0).UTC(), Quality: core.QuiescenceQualityQualifiedLifecycle}
	for name, tc := range map[string]struct {
		obs    core.QuiescenceObservation
		want   core.EvidenceStatus
		reason string
	}{
		"complete": {complete, core.EvidenceSatisfied, ""},
		"incomplete": {func() core.QuiescenceObservation {
			v := complete
			v.Status = core.QuiescenceIncomplete
			v.Unexpected = []core.ResourceRef{{Kind: core.ResourceKindProcess, Ref: "pid:7"}}
			return v
		}(), core.EvidenceInsufficient, "undeclared_live_resources"},
		"unknown": {func() core.QuiescenceObservation { v := complete; v.Status = core.QuiescenceUnknown; return v }(), core.EvidenceUnknown, "quiescence_unknown"},
		"unavailable": {func() core.QuiescenceObservation {
			v := complete
			v.Status = core.QuiescenceUnavailable
			v.Quality = core.QuiescenceQualityUnavailable
			return v
		}(), core.EvidenceUnavailable, "quiescence_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			got := oneRequirement(t, EvaluateObligation(obligation, base, available(core.ProviderIntegrationTest), nil, map[string]core.QuiescenceObservation{candidate.OperationID: tc.obs}))
			if got.Status != tc.want || got.ReasonCode != tc.reason {
				t.Fatalf("got=%#v", got)
			}
		})
	}
	missing := oneRequirement(t, EvaluateObligation(obligation, base, available(core.ProviderIntegrationTest), nil, nil))
	if missing.Status != core.EvidenceUnavailable || missing.ReasonCode != "quiescence_unavailable" {
		t.Fatalf("missing=%#v", missing)
	}
}
