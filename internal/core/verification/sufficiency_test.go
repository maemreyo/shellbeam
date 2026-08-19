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

func gateObligation(t *testing.T, suffix byte, disposition ObligationDisposition) VerificationObligation {
	t.Helper()
	generation := "gen_" + strings.Repeat("1", 64)
	policy := "pol_" + strings.Repeat("a", 64)
	rule := "gate_" + string(suffix)
	id, err := ObligationID(policy, rule, generation, []string{"trigger:" + string(suffix)})
	if err != nil {
		t.Fatal(err)
	}
	o := VerificationObligation{
		SchemaVersion: 1, ObligationID: id, PolicyDigest: policy, SourceRuleID: rule,
		TriggerRefs: []string{"trigger:" + string(suffix)}, AffectedScopeRefs: []string{"scope:" + string(suffix)},
		Ownership: OwnershipApplicationOwned, RequiredPhase: PhaseCheckpoint,
		SufficiencyBasis: "gate test", MinimumAffectedAuthority: AuthorityMechanical,
		EvidenceRequirements: []BoundEvidenceRequirement{{Requirement: EvidenceRequirement{ID: "req", ProviderClass: ProviderStaticFormatCheck, MinimumAuthority: AuthorityMechanical, RequireCurrent: true, Environment: EnvironmentNone, Stability: StabilityNoContradiction}}},
		AppliesToGeneration:  generation, Disposition: disposition, EvidenceStatus: EvidenceNotEvaluated,
	}
	if disposition == DispositionWaived {
		o.WaiverID = "wv_gate_" + string(suffix)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("obligation invalid: %v %#v", err, o)
	}
	return o
}

func gateEvaluation(t *testing.T, o VerificationObligation, status EvidenceStatus) ObligationEvaluation {
	t.Helper()
	e := ObligationEvaluation{ObligationID: o.ObligationID, EvidenceStatus: status}
	if err := e.Validate(); err != nil {
		t.Fatalf("evaluation invalid: %v %#v", err, e)
	}
	return e
}

func TestGateTruthTable(t *testing.T) {
	for name, tc := range map[string]struct {
		disposition                          ObligationDisposition
		evidence                             EvidenceStatus
		want                                 GateStatus
		sat, waived, blocking, indeterminate int
	}{
		"required satisfied":     {DispositionRequiredNow, EvidenceSatisfied, GateClear, 1, 0, 0, 0},
		"required failed":        {DispositionRequiredNow, EvidenceFailed, GateBlocked, 0, 0, 1, 0},
		"required insufficient":  {DispositionRequiredNow, EvidenceInsufficient, GateBlocked, 0, 0, 1, 0},
		"required inconsistent":  {DispositionRequiredNow, EvidenceInconsistent, GateBlocked, 0, 0, 1, 0},
		"required not evaluated": {DispositionRequiredNow, EvidenceNotEvaluated, GateIndeterminate, 0, 0, 0, 1},
		"required unknown":       {DispositionRequiredNow, EvidenceUnknown, GateIndeterminate, 0, 0, 0, 1},
		"required unavailable":   {DispositionRequiredNow, EvidenceUnavailable, GateIndeterminate, 0, 0, 0, 1},
		"waived failed":          {DispositionWaived, EvidenceFailed, GateClear, 0, 1, 0, 0},
		"waived unavailable":     {DispositionWaived, EvidenceUnavailable, GateClear, 0, 1, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			o := gateObligation(t, 'a', tc.disposition)
			e := gateEvaluation(t, o, tc.evidence)
			got, err := FoldGate([]VerificationObligation{o}, map[string]ObligationEvaluation{o.ObligationID: e})
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || got.Breakdown.EvidenceSatisfied != tc.sat || got.Breakdown.Waived != tc.waived || got.Breakdown.Blocking != tc.blocking || got.Breakdown.Indeterminate != tc.indeterminate {
				t.Fatalf("got=%#v", got)
			}
		})
	}
}

func TestGateBreakdownNeverCountsWaiverAsEvidenceSatisfied(t *testing.T) {
	obs := make([]VerificationObligation, 0, 4)
	evals := map[string]ObligationEvaluation{}
	for _, suffix := range []byte{'a', 'b', 'c'} {
		o := gateObligation(t, suffix, DispositionRequiredNow)
		obs = append(obs, o)
		evals[o.ObligationID] = gateEvaluation(t, o, EvidenceSatisfied)
	}
	w := gateObligation(t, 'd', DispositionWaived)
	obs = append(obs, w)
	evals[w.ObligationID] = gateEvaluation(t, w, EvidenceFailed)
	got, err := FoldGate(obs, evals)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GateClear || got.Breakdown != (GateBreakdown{EvidenceSatisfied: 3, Waived: 1}) {
		t.Fatalf("waiver became evidence satisfaction: %#v", got)
	}
}

func TestGateIgnoresNonApplicableDispositionsAndEmptyApplicableSetIsClear(t *testing.T) {
	obs := []VerificationObligation{gateObligation(t, 'a', DispositionDeferred), gateObligation(t, 'b', DispositionOptional), gateObligation(t, 'c', DispositionNotTriggered)}
	got, err := FoldGate(obs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GateClear || got.Breakdown != (GateBreakdown{}) {
		t.Fatalf("got=%#v", got)
	}
	empty, err := FoldGate(nil, nil)
	if err != nil || empty.Status != GateClear {
		t.Fatalf("empty=%#v err=%v", empty, err)
	}
}

func TestGateRequiresEvaluationForRequiredAndWaivedAndRejectsDuplicateObligations(t *testing.T) {
	r := gateObligation(t, 'a', DispositionRequiredNow)
	if _, err := FoldGate([]VerificationObligation{r}, nil); err == nil {
		t.Fatal("missing required evaluation accepted")
	}
	w := gateObligation(t, 'b', DispositionWaived)
	if _, err := FoldGate([]VerificationObligation{w}, nil); err == nil {
		t.Fatal("missing waived evaluation accepted")
	}
	e := gateEvaluation(t, r, EvidenceSatisfied)
	if _, err := FoldGate([]VerificationObligation{r, r}, map[string]ObligationEvaluation{r.ObligationID: e}); err == nil {
		t.Fatal("duplicate obligation accepted")
	}
}

func TestGateBlockedPrecedesIndeterminateAndReasonCodesAreDeterministic(t *testing.T) {
	blocked := gateObligation(t, 'a', DispositionRequiredNow)
	unknown := gateObligation(t, 'b', DispositionRequiredNow)
	evals := map[string]ObligationEvaluation{blocked.ObligationID: gateEvaluation(t, blocked, EvidenceFailed), unknown.ObligationID: gateEvaluation(t, unknown, EvidenceUnknown)}
	got, err := FoldGate([]VerificationObligation{unknown, blocked}, evals)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != GateBlocked || got.Breakdown.Blocking != 1 || got.Breakdown.Indeterminate != 1 {
		t.Fatalf("got=%#v", got)
	}
	if len(got.ReasonCodes) != 2 || got.ReasonCodes[0] > got.ReasonCodes[1] {
		t.Fatalf("reasons not deterministic: %#v", got.ReasonCodes)
	}
}
