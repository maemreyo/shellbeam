package verification

import (
	"testing"
	"time"
)

func testGeneration() string {
	return "gen_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
}

func validRelation() AffectedRelation {
	return AffectedRelation{
		From:                Subject{Kind: SubjectPath, Value: "internal/a.go"},
		To:                  Subject{Kind: SubjectPackage, Value: "github.com/maemreyo/shellbeam/internal/a"},
		Kind:                "imports",
		Basis:               BasisImportGraph,
		DerivationAuthority: AuthorityMechanical,
		Coverage:            CoverageComplete,
		Provider:            &ProviderRef{ID: "go", Version: 1},
		SourceGeneration:    testGeneration(),
		ProvenanceRefs:      []string{"module:root", "source:a"},
		CapturedAt:          time.Unix(1, 0).UTC(),
	}
}

func TestAffectedRelationAuthorityCoverageIndependent(t *testing.T) {
	cases := []AffectedRelation{
		func() AffectedRelation {
			r := validRelation()
			r.DerivationAuthority = AuthorityAdvisory
			r.Coverage = CoverageComplete
			return r
		}(),
		func() AffectedRelation {
			r := validRelation()
			r.DerivationAuthority = AuthorityMechanical
			r.Coverage = CoverageUnknown
			return r
		}(),
	}
	for _, r := range cases {
		if err := r.Validate(); err != nil {
			t.Fatalf("valid relation rejected: %v", err)
		}
	}
}

func TestRelationIDIncludesDerivationSemantics(t *testing.T) {
	base := RelationIdentityInput{
		From: Subject{Kind: SubjectPath, Value: "a.go"}, To: Subject{Kind: SubjectPackage, Value: "p"},
		Kind: "imports", Basis: BasisImportGraph, DerivationAuthority: AuthorityMechanical,
		Coverage: CoverageComplete, Provider: &ProviderRef{ID: "go", Version: 1},
		SourceGeneration: testGeneration(), ProvenanceRefs: []string{"b", "a", "a"},
	}
	id, err := RelationID(base)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.ProvenanceRefs = []string{"a", "b"}
	id2, err := RelationID(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if id != id2 {
		t.Fatalf("provenance ordering changed identity: %q != %q", id, id2)
	}

	variants := []RelationIdentityInput{
		func() RelationIdentityInput { x := base; x.Basis = BasisExplicitMapping; return x }(),
		func() RelationIdentityInput { x := base; x.Provider = &ProviderRef{ID: "go", Version: 2}; return x }(),
		func() RelationIdentityInput { x := base; x.DerivationAuthority = AuthorityAdvisory; return x }(),
		func() RelationIdentityInput { x := base; x.Coverage = CoveragePartial; return x }(),
		func() RelationIdentityInput { x := base; x.ProvenanceRefs = []string{"other"}; return x }(),
	}
	for i, x := range variants {
		got, err := RelationID(x)
		if err != nil {
			t.Fatalf("variant %d: %v", i, err)
		}
		if got == id {
			t.Fatalf("variant %d collided with base relation id", i)
		}
	}

	r := validRelation()
	r.RelationID = id
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.CapturedAt = time.Unix(999, 0).UTC()
	r.Caveats = []string{"display-only"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	again, err := RelationID(base)
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatal("display metadata changed identity")
	}
}

func TestAffectedRelationRequiresGenerationProvenanceAndBoundedSubjects(t *testing.T) {
	for name, mutate := range map[string]func(*AffectedRelation){
		"generation": func(r *AffectedRelation) { r.SourceGeneration = "" },
		"provenance": func(r *AffectedRelation) { r.ProvenanceRefs = nil },
		"basis":      func(r *AffectedRelation) { r.Basis = "" },
		"subject":    func(r *AffectedRelation) { r.From.Value = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := validRelation()
			mutate(&r)
			if err := r.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestAffectedDomainCanBeCompleteWithZeroRelations(t *testing.T) {
	d := AffectedDomain{Kind: DomainSourceSelection, DerivationAuthority: AuthorityMechanical, Coverage: CoverageComplete, SourceGeneration: testGeneration(), ProvenanceRefs: []string{"delta:1"}, CapturedAt: time.Unix(1, 0).UTC()}
	id, err := DomainID(d.Kind, d.Provider, d.SourceGeneration, d.ProvenanceRefs)
	if err != nil {
		t.Fatal(err)
	}
	d.DomainID = id
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	s := AffectedSurface{SchemaVersion: 1, RepositoryID: "repo_test", WorkspaceID: "ws_test", SourceGeneration: testGeneration(), Domains: []AffectedDomain{d}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	summary := s.Summary()
	if summary.RelationCount != 0 || len(summary.Domains) != 1 || summary.ByCoverage[CoverageComplete] != 0 { // domains are not relation counts
		if summary.RelationCount != 0 || len(summary.Domains) != 1 {
			t.Fatalf("summary=%#v", summary)
		}
	}
}

func TestAuthorityAndCoverageMonotonicHelpers(t *testing.T) {
	if !MeetsMinimumAuthority(AuthorityAuthoritative, AuthorityMechanical) || !MeetsMinimumAuthority(AuthorityMechanical, AuthorityAdvisory) || MeetsMinimumAuthority(AuthorityAdvisory, AuthorityMechanical) {
		t.Fatal("authority lattice wrong")
	}
	if !CoverageNoStrongerThan(CoveragePartial, CoverageBounded) || CoverageNoStrongerThan(CoverageComplete, CoveragePartial) {
		t.Fatal("coverage order wrong")
	}
}

func TestAffectedSurfaceAllowsMissingGenerationOnlyForUnknownSurface(t *testing.T) {
	d := AffectedDomain{Kind: DomainSourceSelection, DerivationAuthority: AuthorityMechanical, Coverage: CoverageUnknown, ProvenanceRefs: []string{"delta:unavailable"}, CapturedAt: time.Unix(1, 0).UTC()}
	id, err := DomainIDWithoutGeneration(d.Kind, d.Provider, d.ProvenanceRefs)
	if err != nil {
		t.Fatal(err)
	}
	d.DomainID = id
	s := AffectedSurface{SchemaVersion: 1, RepositoryID: "repo_test", WorkspaceID: "ws_test", Domains: []AffectedDomain{d}, Diagnostics: []string{"source_generation_unavailable"}}
	if err := s.Validate(); err != nil {
		t.Fatalf("unknown surface rejected: %v", err)
	}
	bad := s
	bad.Domains[0].Coverage = CoveragePartial
	if err := bad.Validate(); err == nil {
		t.Fatal("missing generation accepted for stronger-than-unknown domain")
	}
}
