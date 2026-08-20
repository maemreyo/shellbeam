package verification

import "testing"

func TestVerificationObligationDispositionAndEvidenceStatusAreIndependent(t *testing.T) {
	g := "gen_1111111111111111111111111111111111111111111111111111111111111111"
	p := "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	id, err := ObligationID(p, "r1", g, []string{"rel_1"})
	if err != nil {
		t.Fatal(err)
	}
	o := VerificationObligation{
		SchemaVersion: 1, ObligationID: id, PolicyDigest: p, SourceRuleID: "r1",
		TriggerRefs: []string{"rel_1"}, AffectedScopeRefs: []string{"rel_1"},
		Ownership: OwnershipApplicationOwned, RequiredPhase: PhaseCheckpoint,
		SufficiencyBasis: "checkpoint", MinimumAffectedAuthority: AuthorityMechanical,
		EvidenceRequirements: []BoundEvidenceRequirement{{Requirement: EvidenceRequirement{ID: "e1", ProviderClass: ProviderStaticFormatCheck, MinimumAuthority: AuthorityMechanical, RequireCurrent: true, Environment: EnvironmentNone, Stability: StabilityNoContradiction}}},
		AppliesToGeneration:  g, Disposition: DispositionWaived, EvidenceStatus: EvidenceUnavailable, WaiverID: "wv_one",
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	o.WaiverID = ""
	if err := o.Validate(); err == nil {
		t.Fatal("waived obligation without waiver id accepted")
	}
}

func TestPolicyGapValidationRequiresMechanicalClassificationProvenance(t *testing.T) {
	g := "gen_1111111111111111111111111111111111111111111111111111111111111111"
	p := "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	id, err := PolicyGapID(p, "security", g, []string{"rel_1"})
	if err != nil {
		t.Fatal(err)
	}
	gap := PolicyGap{GapID: id, SurfaceRef: "internal/auth/a.go", DeclaredClass: "security_sensitive", ClassificationSource: "security", MissingPolicyClass: "security_sensitive", Authority: AuthorityMechanical, ProvenanceRefs: []string{"rel_1", "policy:" + p}}
	if err := gap.Validate(); err != nil {
		t.Fatal(err)
	}
	gap.Authority = AuthorityAdvisory
	if err := gap.Validate(); err == nil {
		t.Fatal("advisory policy gap accepted")
	}
}
