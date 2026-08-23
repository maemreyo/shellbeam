package decisionprotocol

import (
	"encoding/json"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func TestProjectionPlateauIgnoresAuditOnlyProgress(t *testing.T) {
	semantic := core.ProjectionSemanticState{EpisodeID: "ep-1", CandidateID: "a", Gate: core.GateBlocked, SourceCompatible: true, CandidateStates: []core.CandidateSemanticState{{CandidateID: "a", LineageRoot: "a", Active: true, Eligible: true}}}
	p1, err := core.ProjectionDigest(semantic)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := core.ProjectionDigest(semantic)
	if err != nil {
		t.Fatal(err)
	}
	a1, err := core.AuditDigest(core.AuditState{EpisodeID: "ep-1", CanonicalRecordSeqs: []core.RecordSeq{1, 2}, BasisRefs: []string{"exp-1"}})
	if err != nil {
		t.Fatal(err)
	}
	a2, err := core.AuditDigest(core.AuditState{EpisodeID: "ep-1", CanonicalRecordSeqs: []core.RecordSeq{1, 2, 3, 4}, BasisRefs: []string{"exp-1", "exp-2", "exp-3"}})
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 || a1 == a2 || core.CompareEpistemicProgress(p1, p2) != core.EpistemicProgressNone {
		t.Fatalf("p1=%s p2=%s a1=%s a2=%s", p1, p2, a1, a2)
	}
}

func TestDecisionProjectionContainsCapabilitiesButNoPlannerFields(t *testing.T) {
	projection := core.DecisionProjection{EpisodeID: "ep-1", EpisodeState: core.EpisodeOpen, EpisodeKind: core.EpisodeDiagnosis, AllowedProtocolTransitions: []string{"candidate.create", "experiment.define", "close_unresolved"}}
	payload, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"next_best_action", "recommended_experiment", "generate_more_hypotheses", "choose_candidate_B"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("planner field leaked: %s", text)
		}
	}
	if !strings.Contains(text, "allowed_protocol_transitions") {
		t.Fatalf("capabilities missing: %s", text)
	}
}
