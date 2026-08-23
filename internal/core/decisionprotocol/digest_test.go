package decisionprotocol

import (
	"strings"
	"testing"
)

func TestProjectionDigestIgnoresEquivalentAuditRefsButIncludesVerifierSemanticState(t *testing.T) {
	a := ProjectionSemanticState{EpisodeID: "ep-1", CandidateID: "cand-1", Gate: GateClear, BasisRefs: []string{"op-a"}, VerifierState: []VerifierSemanticState{{ActorRef: "actor-1", QualifiedContextClass: ContextIndependentModel, PreferredCandidates: []CandidateID{"cand-1"}}}}
	b := a
	b.BasisRefs = []string{"op-b"}
	da, err := ProjectionDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := ProjectionDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("equivalent audit refs changed projection digest: %s != %s", da, db)
	}
	b.VerifierState = []VerifierSemanticState{{ActorRef: "actor-2", QualifiedContextClass: ContextHuman, PreferredCandidates: []CandidateID{"cand-1"}}}
	dc, err := ProjectionDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da == dc {
		t.Fatal("verifier semantic state did not change projection digest")
	}
}

func TestAuditDigestIncludesAuditRefs(t *testing.T) {
	a := AuditState{EpisodeID: "ep-1", CanonicalRecordSeqs: []RecordSeq{1}, BasisRefs: []string{"op-a"}}
	b := a
	b.BasisRefs = []string{"op-b"}
	da, err := AuditDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := AuditDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da == db {
		t.Fatal("audit digest ignored exact audit refs")
	}
}

func TestSelectionIntentFingerprintDistinguishesOverrideFromNormalCommit(t *testing.T) {
	normal := SelectionCommitIntent{EpisodeID: "ep-1", CandidateID: "cand-1", ActorRef: "actor-1", PolicyDigest: "pol_" + strings.Repeat("a", 64), ProjectionDigest: "proj_" + strings.Repeat("b", 64), SourceGeneration: "src-1"}
	override := normal
	override.Override = true
	override.OverrideRef = "override-1"
	a, err := SelectionIntentFingerprint(normal)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SelectionIntentFingerprint(override)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("normal and override commit fingerprints are equal")
	}
}

func TestObservationDimensionKeyExcludesExpectedOutcome(t *testing.T) {
	pass := ObservationPredicate{Kind: PredicateStructuredTestStatus, StructuredTestStatus: &StructuredTestStatusPredicate{Target: StructuredTargetTestCase, Package: "pkg", Name: "TestRace", ExpectedStatus: StructuredTestPass}}
	fail := pass
	fail.StructuredTestStatus = &StructuredTestStatusPredicate{Target: StructuredTargetTestCase, Package: "pkg", Name: "TestRace", ExpectedStatus: StructuredTestFail}
	a, err := ObservationDimensionKey(pass)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ObservationDimensionKey(fail)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("expected outcome changed dimension key: %s != %s", a, b)
	}
}

func TestPolicyDigestRejectsDuplicateRequirementKind(t *testing.T) {
	p := validPolicyContent()
	p.Requirements = append(p.Requirements, DecisionRequirement{RequirementID: "challenge-2", Kind: RequirementCandidateChallenge, CandidateChallenge: &CandidateChallengeRequirement{MinimumDistinctLineages: 3}})
	if _, err := PolicyDigest(p); err == nil {
		t.Fatal("duplicate requirement kind accepted")
	}
}

func TestPolicyDigestIgnoresRequirementInputOrder(t *testing.T) {
	p := validPolicyContent()
	p.Requirements = []DecisionRequirement{
		{RequirementID: "verify", Kind: RequirementVerifierAssessment, VerifierAssessment: &VerifierAssessmentRequirement{MinimumSupportingAssessments: 1}},
		{RequirementID: "challenge", Kind: RequirementCandidateChallenge, CandidateChallenge: &CandidateChallengeRequirement{MinimumDistinctLineages: 2}},
	}
	a, err := PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Requirements[0], p.Requirements[1] = p.Requirements[1], p.Requirements[0]
	b, err := PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("requirement order changed policy digest: %s != %s", a, b)
	}
}
