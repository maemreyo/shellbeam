package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const maxAffectedRelations = 512

type AffectedRequest struct {
	WorkspaceID string
	ActivityID  string
}
type AffectedResult struct{ Surface core.AffectedSurface }

type AffectedService struct {
	workspaces WorkspaceInspector
	sampler    WorkspaceSampler
	activities ActivitySelector
	snapshots  SourceSnapshotter
	relations  RelationProvider
}

func NewAffectedService(w WorkspaceInspector, s WorkspaceSampler, a ActivitySelector, snap SourceSnapshotter, r RelationProvider) *AffectedService {
	return &AffectedService{workspaces: w, sampler: s, activities: a, snapshots: snap, relations: r}
}

type affectedSelection struct {
	paths       []string
	basis       string
	coverage    core.Coverage
	diagnostics []string
}

func (s *AffectedService) Derive(ctx context.Context, req AffectedRequest) (AffectedResult, error) {
	if s == nil || s.workspaces == nil || s.sampler == nil || s.snapshots == nil {
		return AffectedResult{}, fmt.Errorf("affected service unavailable")
	}
	ws, err := s.workspaces.Inspect(ctx, req.WorkspaceID)
	if err != nil {
		return AffectedResult{}, err
	}
	sample, selection, err := s.selectAffectedPaths(ctx, req, ws)
	if err != nil {
		return AffectedResult{}, err
	}
	generation := s.bindSourceGeneration(ctx, ws, sample, &selection)
	surface, err := buildSourceAffectedSurface(ws, sample, generation, selection)
	if err != nil {
		return AffectedResult{}, err
	}
	selection.diagnostics = s.appendDerivedRelations(ctx, ws, generation, selection.paths, &surface, selection.diagnostics)
	sort.Slice(surface.Relations, func(i, j int) bool { return surface.Relations[i].RelationID < surface.Relations[j].RelationID })
	surface.Diagnostics = sortedUniqueStrings(selection.diagnostics)
	if err := surface.Validate(); err != nil {
		return AffectedResult{}, err
	}
	return AffectedResult{Surface: surface}, nil
}

func (s *AffectedService) selectAffectedPaths(ctx context.Context, req AffectedRequest, ws workspace.Workspace) (workspace.DeltaSample, affectedSelection, error) {
	sample := s.sampler.Sample(ctx, ws.ID, workspace.DeltaLimits{MaxPaths: 256, MaxOutputBytes: workspace.DefaultDeltaMaxOutputBytes, TimeoutMS: workspace.MaxDeltaTimeoutMS})
	if err := sample.Validate(); err != nil {
		return workspace.DeltaSample{}, affectedSelection{}, fmt.Errorf("invalid workspace delta: %w", err)
	}
	if sample.RepositoryID != ws.RepositoryID || sample.WorkspaceID != ws.ID {
		return workspace.DeltaSample{}, affectedSelection{}, fmt.Errorf("delta workspace mismatch")
	}
	selection := affectedSelection{paths: append([]string(nil), sample.ResolvedPaths...), basis: "workspace_dirty", coverage: coverageFromSelection(sample.Completeness)}
	if req.ActivityID != "" {
		selection.basis = "activity_delta"
		if s.activities == nil {
			return workspace.DeltaSample{}, affectedSelection{}, fmt.Errorf("activity selector unavailable")
		}
		comparison, err := s.activities.CompareWorkspace(ctx, req.ActivityID, sample)
		if err != nil {
			return workspace.DeltaSample{}, affectedSelection{}, err
		}
		if comparison.BaselineDiverged {
			selection.coverage = weakenCoverage(selection.coverage)
			selection.diagnostics = append(selection.diagnostics, "activity_baseline_diverged:"+comparison.DivergenceReason)
		} else {
			selection.paths = pathsFromActivity(comparison.ObservedSinceBaseline)
		}
	}
	selection.paths = sortedUniquePaths(selection.paths, 256)
	if sample.Completeness == workspace.SelectionUnavailable {
		selection.coverage = core.CoverageUnknown
		if sample.DiagnosticCode != "" {
			selection.diagnostics = append(selection.diagnostics, "workspace_delta:"+sample.DiagnosticCode)
		}
	}
	return sample, selection, nil
}

func (s *AffectedService) bindSourceGeneration(ctx context.Context, ws workspace.Workspace, sample workspace.DeltaSample, selection *affectedSelection) string {
	fresh := s.snapshots.ObserveFresh(ctx, ws.Root)
	if fresh.Quality == workspace.QualityFresh && fresh.Generation != "" && fresh.RepositoryID == ws.RepositoryID && fresh.WorkspaceID == ws.ID && fresh.Validate() == nil {
		return fresh.Generation
	}
	selection.coverage = core.CoverageUnknown
	selection.diagnostics = append(selection.diagnostics, "source_generation_unavailable")
	return ""
}

func buildSourceAffectedSurface(ws workspace.Workspace, sample workspace.DeltaSample, generation string, selection affectedSelection) (core.AffectedSurface, error) {
	provider := &core.ProviderRef{ID: "workspace_delta", Version: 1}
	provenance := []string{"selection_basis:" + selection.basis, "selection_paths:" + selectionFingerprint(selection.paths)}
	domain := core.AffectedDomain{Kind: core.DomainSourceSelection, DerivationAuthority: core.AuthorityMechanical, Coverage: selection.coverage, Provider: provider, SourceGeneration: generation, ProvenanceRefs: provenance, CapturedAt: sample.ObservedAt.UTC()}
	var err error
	if generation == "" {
		domain.DomainID, err = core.DomainIDWithoutGeneration(domain.Kind, provider, provenance)
	} else {
		domain.DomainID, err = core.DomainID(domain.Kind, provider, generation, provenance)
	}
	if err != nil {
		return core.AffectedSurface{}, err
	}
	surface := core.AffectedSurface{SchemaVersion: 1, RepositoryID: string(ws.RepositoryID), WorkspaceID: string(ws.ID), SourceGeneration: generation, Domains: []core.AffectedDomain{domain}}
	if generation == "" {
		return surface, nil
	}
	for _, affectedPath := range selection.paths {
		in := core.RelationIdentityInput{From: core.Subject{Kind: core.SubjectSourceRef, Value: generation}, To: core.Subject{Kind: core.SubjectPath, Value: affectedPath}, Kind: "mutated_path", Basis: core.BasisObservedMutation, DerivationAuthority: core.AuthorityMechanical, Coverage: selection.coverage, Provider: provider, SourceGeneration: generation, ProvenanceRefs: []string{"selection:" + domain.DomainID, "path:" + affectedPath}}
		id, err := core.RelationID(in)
		if err != nil {
			return core.AffectedSurface{}, err
		}
		surface.Relations = append(surface.Relations, core.AffectedRelation{RelationID: id, From: in.From, To: in.To, Kind: in.Kind, Basis: in.Basis, DerivationAuthority: in.DerivationAuthority, Coverage: in.Coverage, Provider: provider, SourceGeneration: generation, ProvenanceRefs: in.ProvenanceRefs, CapturedAt: sample.ObservedAt.UTC()})
	}
	return surface, nil
}

func (s *AffectedService) appendDerivedRelations(ctx context.Context, ws workspace.Workspace, generation string, paths []string, surface *core.AffectedSurface, diagnostics []string) []string {
	if generation == "" || s.relations == nil {
		return diagnostics
	}
	derived := s.relations.Derive(ctx, ws, generation, paths)
	surface.Domains = append(surface.Domains, derived.Domains...)
	diagnostics = append(diagnostics, derived.Diagnostics...)
	remaining := maxAffectedRelations - len(surface.Relations)
	if remaining < 0 {
		remaining = 0
	}
	if len(derived.Relations) > remaining {
		derived.Relations = derived.Relations[:remaining]
		diagnostics = append(diagnostics, "relation_surface_limit_exceeded")
		for i := range surface.Domains {
			if surface.Domains[i].Kind == core.DomainGoImportGraph && surface.Domains[i].Coverage != core.CoverageUnknown {
				surface.Domains[i].Coverage = core.CoveragePartial
			}
		}
	}
	surface.Relations = append(surface.Relations, derived.Relations...)
	return diagnostics
}

func coverageFromSelection(c workspace.SelectionCompleteness) core.Coverage {
	switch c {
	case workspace.SelectionComplete:
		return core.CoverageComplete
	case workspace.SelectionUnavailable:
		return core.CoverageUnknown
	default:
		return core.CoveragePartial
	}
}
func weakenCoverage(c core.Coverage) core.Coverage {
	if c == core.CoverageUnknown {
		return c
	}
	return core.CoveragePartial
}
func pathsFromActivity(facts []activity.PathFact) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		if f.Path != "" {
			out = append(out, f.Path)
		}
	}
	return out
}
func sortedUniquePaths(in []string, max int) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, v := range out {
		if v == "" || path.IsAbs(v) || v == ".." || strings.HasPrefix(v, "../") {
			continue
		}
		if n == 0 || out[n-1] != v {
			out[n] = v
			n++
			if n == max {
				break
			}
		}
	}
	return out[:n]
}
func selectionFingerprint(paths []string) string {
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func sortedUniqueStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, v := range out {
		if v == "" {
			continue
		}
		if n == 0 || out[n-1] != v {
			out[n] = v
			n++
		}
	}
	return out[:n]
}

// ClassificationProjectionRequest deliberately has no proposal input.
type ClassificationProjectionRequest struct {
	BaseSurface     core.AffectedSurface
	EffectivePolicy core.MaterializedPolicy
}

func ApplyEffectiveClassifications(req ClassificationProjectionRequest) (core.AffectedSurface, error) {
	if err := req.BaseSurface.Validate(); err != nil {
		return core.AffectedSurface{}, err
	}
	digest, err := core.PolicyDigest(req.EffectivePolicy.Snapshot.Content)
	if err != nil {
		return core.AffectedSurface{}, err
	}
	if digest != req.EffectivePolicy.Snapshot.Digest || req.EffectivePolicy.Snapshot.RepositoryID != req.BaseSurface.RepositoryID {
		return core.AffectedSurface{}, fmt.Errorf("effective policy binding mismatch")
	}
	out := req.BaseSurface
	coverage := core.CoverageUnknown
	for _, d := range out.Domains {
		if d.Kind == core.DomainSourceSelection {
			coverage = d.Coverage
			break
		}
	}
	if out.SourceGeneration == "" {
		coverage = core.CoverageUnknown
	}
	provider := &core.ProviderRef{ID: "verification_policy", Version: 1}
	prov := []string{"policy:" + digest}
	domain := core.AffectedDomain{Kind: core.DomainPolicyClassification, DerivationAuthority: core.AuthorityMechanical, Coverage: coverage, Provider: provider, SourceGeneration: out.SourceGeneration, ProvenanceRefs: prov, CapturedAt: req.EffectivePolicy.ApprovedAt.UTC()}
	if domain.CapturedAt.IsZero() {
		domain.CapturedAt = time.Unix(1, 0).UTC()
	}
	if out.SourceGeneration == "" {
		domain.DomainID, err = core.DomainIDWithoutGeneration(domain.Kind, provider, prov)
	} else {
		domain.DomainID, err = core.DomainID(domain.Kind, provider, out.SourceGeneration, prov)
	}
	if err != nil {
		return core.AffectedSurface{}, err
	}
	out.Domains = append(out.Domains, domain)
	if out.SourceGeneration != "" {
		paths := affectedPathSubjects(out)
		for _, classifier := range req.EffectivePolicy.Snapshot.Content.Classifiers {
			for _, p := range paths {
				if !matchesPolicyPath(classifier.Paths, p) {
					continue
				}
				refs := []string{"policy:" + digest, "classification:" + classifier.ID}
				in := core.RelationIdentityInput{From: core.Subject{Kind: core.SubjectPath, Value: p}, To: core.Subject{Kind: core.SubjectSurfaceClass, Value: classifier.SurfaceClass}, Kind: "classified_as", Basis: core.BasisProjectPolicy, DerivationAuthority: core.AuthorityMechanical, Coverage: coverage, Provider: provider, SourceGeneration: out.SourceGeneration, ProvenanceRefs: refs}
				id, e := core.RelationID(in)
				if e != nil {
					return core.AffectedSurface{}, e
				}
				out.Relations = append(out.Relations, core.AffectedRelation{RelationID: id, From: in.From, To: in.To, Kind: in.Kind, Basis: in.Basis, DerivationAuthority: in.DerivationAuthority, Coverage: in.Coverage, Provider: provider, SourceGeneration: out.SourceGeneration, ProvenanceRefs: refs, CapturedAt: domain.CapturedAt})
			}
		}
	}
	sort.Slice(out.Relations, func(i, j int) bool { return out.Relations[i].RelationID < out.Relations[j].RelationID })
	if err := out.Validate(); err != nil {
		return core.AffectedSurface{}, err
	}
	return out, nil
}
func affectedPathSubjects(s core.AffectedSurface) []string {
	set := map[string]bool{}
	for _, r := range s.Relations {
		if r.From.Kind == core.SubjectPath {
			set[r.From.Value] = true
		}
		if r.To.Kind == core.SubjectPath {
			set[r.To.Value] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
func matchesPolicyPath(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if value == prefix || strings.HasPrefix(value, prefix+"/") {
				return true
			}
			continue
		}
		if ok, _ := path.Match(pattern, value); ok {
			return true
		}
	}
	return false
}
