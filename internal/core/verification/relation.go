package verification

import (
	"fmt"
	"time"
)

type DerivationAuthority string

const (
	AuthorityAuthoritative DerivationAuthority = "authoritative"
	AuthorityMechanical    DerivationAuthority = "mechanical"
	AuthorityAdvisory      DerivationAuthority = "advisory"
)

type Coverage string

const (
	CoverageComplete Coverage = "complete"
	CoverageBounded  Coverage = "bounded"
	CoveragePartial  Coverage = "partial"
	CoverageUnknown  Coverage = "unknown"
)

type SubjectKind string

const (
	SubjectPath         SubjectKind = "path"
	SubjectSourceRef    SubjectKind = "source_ref"
	SubjectPackage      SubjectKind = "package"
	SubjectProjectCmd   SubjectKind = "project_command"
	SubjectSurfaceClass SubjectKind = "policy_surface_class"
)

type RelationBasis string

const (
	BasisObservedMutation RelationBasis = "observed_source_mutation"
	BasisImportGraph      RelationBasis = "import_graph"
	BasisProjectPolicy    RelationBasis = "project_policy"
	BasisExplicitMapping  RelationBasis = "explicit_project_mapping"
)

type Subject struct {
	Kind  SubjectKind `json:"kind"`
	Value string      `json:"value"`
}
type ProviderRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type AffectedDomainKind string

const (
	DomainSourceSelection      AffectedDomainKind = "source_selection"
	DomainGoImportGraph        AffectedDomainKind = "go_import_graph"
	DomainPolicyClassification AffectedDomainKind = "policy_classification"
)

type AffectedDomain struct {
	DomainID            string              `json:"domain_id"`
	Kind                AffectedDomainKind  `json:"kind"`
	DerivationAuthority DerivationAuthority `json:"derivation_authority"`
	Coverage            Coverage            `json:"coverage"`
	Provider            *ProviderRef        `json:"provider,omitempty"`
	SourceGeneration    string              `json:"source_generation"`
	ProvenanceRefs      []string            `json:"provenance_refs,omitempty"`
	CapturedAt          time.Time           `json:"captured_at"`
	Caveats             []string            `json:"caveats,omitempty"`
}

type AffectedRelation struct {
	RelationID          string              `json:"relation_id"`
	From                Subject             `json:"from_subject"`
	To                  Subject             `json:"to_subject"`
	Kind                string              `json:"relation_kind"`
	Basis               RelationBasis       `json:"basis"`
	DerivationAuthority DerivationAuthority `json:"derivation_authority"`
	Coverage            Coverage            `json:"coverage"`
	Provider            *ProviderRef        `json:"provider,omitempty"`
	SourceGeneration    string              `json:"source_generation"`
	ProvenanceRefs      []string            `json:"provenance_refs,omitempty"`
	CapturedAt          time.Time           `json:"captured_at"`
	Caveats             []string            `json:"caveats,omitempty"`
}

type AffectedSurface struct {
	SchemaVersion    int                `json:"schema_version"`
	RepositoryID     string             `json:"repository_id"`
	WorkspaceID      string             `json:"workspace_id"`
	SourceGeneration string             `json:"source_generation"`
	Domains          []AffectedDomain   `json:"domains"`
	Relations        []AffectedRelation `json:"relations"`
	Diagnostics      []string           `json:"diagnostics,omitempty"`
}

type AffectedSurfaceSummary struct {
	RelationCount int                         `json:"relation_count"`
	Domains       []AffectedDomain            `json:"domains"`
	ByAuthority   map[DerivationAuthority]int `json:"by_authority"`
	ByCoverage    map[Coverage]int            `json:"by_coverage"`
	Diagnostics   []string                    `json:"diagnostics,omitempty"`
}

func (s Subject) Validate() error {
	switch s.Kind {
	case SubjectPath, SubjectSourceRef, SubjectPackage, SubjectProjectCmd, SubjectSurfaceClass:
	default:
		return fmt.Errorf("invalid subject kind %q", s.Kind)
	}
	if !boundedToken(s.Value, 2048) {
		return fmt.Errorf("invalid subject value")
	}
	return nil
}
func (p ProviderRef) Validate() error {
	if !boundedToken(p.ID, 128) || p.Version < 1 {
		return fmt.Errorf("invalid provider")
	}
	return nil
}
func (a DerivationAuthority) Validate() error {
	switch a {
	case AuthorityAuthoritative, AuthorityMechanical, AuthorityAdvisory:
		return nil
	}
	return fmt.Errorf("invalid derivation authority %q", a)
}
func (c Coverage) Validate() error {
	switch c {
	case CoverageComplete, CoverageBounded, CoveragePartial, CoverageUnknown:
		return nil
	}
	return fmt.Errorf("invalid coverage %q", c)
}
func (b RelationBasis) Validate() error {
	switch b {
	case BasisObservedMutation, BasisImportGraph, BasisProjectPolicy, BasisExplicitMapping:
		return nil
	}
	return fmt.Errorf("invalid relation basis %q", b)
}
func (k AffectedDomainKind) Validate() error {
	switch k {
	case DomainSourceSelection, DomainGoImportGraph, DomainPolicyClassification:
		return nil
	}
	return fmt.Errorf("invalid domain kind %q", k)
}

func (d AffectedDomain) Validate() error {
	if err := d.Kind.Validate(); err != nil {
		return err
	}
	if err := d.DerivationAuthority.Validate(); err != nil {
		return err
	}
	if err := d.Coverage.Validate(); err != nil {
		return err
	}
	if d.DomainID != "" && !isDerivedID(d.DomainID, "dom_") {
		return fmt.Errorf("invalid domain id")
	}
	if d.Provider != nil {
		if err := d.Provider.Validate(); err != nil {
			return err
		}
	}
	if !validGeneration(d.SourceGeneration) || len(d.ProvenanceRefs) == 0 {
		return fmt.Errorf("domain requires generation and provenance")
	}
	if err := validateRefs(d.ProvenanceRefs, 128, 2048); err != nil {
		return err
	}
	if d.CapturedAt.IsZero() {
		return fmt.Errorf("domain captured_at required")
	}
	return validateStrings(d.Caveats, 32, 512)
}
func (r AffectedRelation) Validate() error {
	if err := r.From.Validate(); err != nil {
		return err
	}
	if err := r.To.Validate(); err != nil {
		return err
	}
	if !boundedToken(r.Kind, 128) {
		return fmt.Errorf("invalid relation kind")
	}
	if err := r.Basis.Validate(); err != nil {
		return err
	}
	if err := r.DerivationAuthority.Validate(); err != nil {
		return err
	}
	if err := r.Coverage.Validate(); err != nil {
		return err
	}
	if r.RelationID != "" && !isDerivedID(r.RelationID, "rel_") {
		return fmt.Errorf("invalid relation id")
	}
	if r.Provider != nil {
		if err := r.Provider.Validate(); err != nil {
			return err
		}
	}
	if !validGeneration(r.SourceGeneration) || len(r.ProvenanceRefs) == 0 {
		return fmt.Errorf("relation requires generation and provenance")
	}
	if err := validateRefs(r.ProvenanceRefs, 128, 2048); err != nil {
		return err
	}
	if r.CapturedAt.IsZero() {
		return fmt.Errorf("relation captured_at required")
	}
	return validateStrings(r.Caveats, 32, 512)
}
func (s AffectedSurface) Validate() error {
	if s.SchemaVersion != 1 || !boundedToken(s.RepositoryID, 128) || !boundedToken(s.WorkspaceID, 128) || !validGeneration(s.SourceGeneration) {
		return fmt.Errorf("invalid affected surface header")
	}
	if len(s.Domains) > 16 || len(s.Relations) > 512 {
		return fmt.Errorf("affected surface limit exceeded")
	}
	for i := range s.Domains {
		if err := s.Domains[i].Validate(); err != nil {
			return fmt.Errorf("domain %d: %w", i, err)
		}
	}
	for i := range s.Relations {
		if err := s.Relations[i].Validate(); err != nil {
			return fmt.Errorf("relation %d: %w", i, err)
		}
	}
	return validateStrings(s.Diagnostics, 64, 512)
}
func (s AffectedSurface) Summary() AffectedSurfaceSummary {
	out := AffectedSurfaceSummary{RelationCount: len(s.Relations), Domains: append([]AffectedDomain(nil), s.Domains...), ByAuthority: map[DerivationAuthority]int{}, ByCoverage: map[Coverage]int{}, Diagnostics: append([]string(nil), s.Diagnostics...)}
	for _, r := range s.Relations {
		out.ByAuthority[r.DerivationAuthority]++
		out.ByCoverage[r.Coverage]++
	}
	return out
}
func MeetsMinimumAuthority(actual, required DerivationAuthority) bool {
	ranks := map[DerivationAuthority]int{AuthorityAdvisory: 1, AuthorityMechanical: 2, AuthorityAuthoritative: 3}
	a, aok := ranks[actual]
	r, rok := ranks[required]
	return aok && rok && a >= r
}
func CoverageNoStrongerThan(candidate, reference Coverage) bool {
	ranks := map[Coverage]int{CoverageUnknown: 0, CoveragePartial: 1, CoverageBounded: 2, CoverageComplete: 3}
	c, cok := ranks[candidate]
	r, rok := ranks[reference]
	return cok && rok && c <= r
}
