package verification

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeAffectedWorkspaceInspector struct {
	ws  workspace.Workspace
	err error
}

func (f *fakeAffectedWorkspaceInspector) Inspect(context.Context, string) (workspace.Workspace, error) {
	return f.ws, f.err
}

type fakeAffectedSampler struct {
	sample    workspace.DeltaSample
	gotLimits workspace.DeltaLimits
}

func (f *fakeAffectedSampler) Sample(_ context.Context, _ workspace.WorkspaceID, l workspace.DeltaLimits) workspace.DeltaSample {
	f.gotLimits = l
	return f.sample
}

type fakeAffectedActivity struct {
	comparison activity.Comparison
	err        error
	calls      int
}

func (f *fakeAffectedActivity) CompareWorkspace(context.Context, string, workspace.DeltaSample) (activity.Comparison, error) {
	f.calls++
	return f.comparison, f.err
}

type fakeRelationProvider struct {
	result     RelationResult
	calls      int
	paths      []string
	generation string
}

func (f *fakeRelationProvider) Derive(_ context.Context, _ workspace.Workspace, g string, paths []string) RelationResult {
	f.calls++
	f.generation = g
	f.paths = append([]string(nil), paths...)
	return f.result
}

func affectedDelta(paths []string, completeness workspace.SelectionCompleteness) workspace.DeltaSample {
	b := workspace.CoherenceBarrier{DaemonIncarnation: "daemon-1", Epoch: 1}
	return workspace.DeltaSample{SchemaVersion: workspace.DeltaSampleSchemaVersion, RepositoryID: serviceWorkspace().RepositoryID, WorkspaceID: serviceWorkspace().ID, Freshness: workspace.SampleFreshlySampled, Completeness: completeness, ObservedAt: time.Unix(20, 0).UTC(), Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", ResolvedPaths: append([]string(nil), paths...), BarrierBefore: b, BarrierAfter: b, RecordsObserved: len(paths), BytesObserved: int64(len(paths) * 8)}
}
func targetPaths(s core.AffectedSurface) []string {
	var out []string
	for _, r := range s.Relations {
		if r.Basis == core.BasisObservedMutation && r.To.Kind == core.SubjectPath {
			out = append(out, r.To.Value)
		}
	}
	sort.Strings(out)
	return out
}
func sourceDomain(t *testing.T, s core.AffectedSurface) core.AffectedDomain {
	t.Helper()
	for _, d := range s.Domains {
		if d.Kind == core.DomainSourceSelection {
			return d
		}
	}
	t.Fatal("source_selection missing")
	return core.AffectedDomain{}
}

func TestAffectedWorkspaceDirtyUsesCurrentResolvedPaths(t *testing.T) {
	sample := affectedDelta([]string{"inherited.go", "new.go"}, workspace.SelectionComplete)
	sampler := &fakeAffectedSampler{sample: sample}
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, sampler, nil, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, nil)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID)})
	if err != nil {
		t.Fatal(err)
	}
	paths := targetPaths(got.Surface)
	if len(paths) != 2 || paths[0] != "inherited.go" || paths[1] != "new.go" {
		t.Fatalf("paths=%v", paths)
	}
	d := sourceDomain(t, got.Surface)
	if d.Coverage != core.CoverageComplete || d.DerivationAuthority != core.AuthorityMechanical {
		t.Fatalf("domain=%#v", d)
	}
}

func TestAffectedActivityUsesObservedSinceBaselineNotInheritedDirty(t *testing.T) {
	sample := affectedDelta([]string{"inherited.go", "new.go"}, workspace.SelectionComplete)
	activitySel := &fakeAffectedActivity{comparison: activity.Comparison{InheritedDirty: []activity.PathFact{{Path: "inherited.go", State: activity.PathModified}}, ObservedSinceBaseline: []activity.PathFact{{Path: "new.go", State: activity.PathModified}}}}
	provider := &fakeRelationProvider{}
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, &fakeAffectedSampler{sample: sample}, activitySel, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, provider)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID), ActivityID: "activity-1"})
	if err != nil {
		t.Fatal(err)
	}
	paths := targetPaths(got.Surface)
	if len(paths) != 1 || paths[0] != "new.go" {
		t.Fatalf("paths=%v", paths)
	}
	if len(provider.paths) != 1 || provider.paths[0] != "new.go" {
		t.Fatalf("provider paths=%v", provider.paths)
	}
}

func TestAffectedActivityBaselineDivergenceWidensAndDegradesCoverage(t *testing.T) {
	sample := affectedDelta([]string{"old.go", "new.go"}, workspace.SelectionComplete)
	activitySel := &fakeAffectedActivity{comparison: activity.Comparison{BaselineDiverged: true, DivergenceReason: "history_diverged"}}
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, &fakeAffectedSampler{sample: sample}, activitySel, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, nil)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID), ActivityID: "activity-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targetPaths(got.Surface)) != 2 {
		t.Fatalf("surface=%#v", got.Surface)
	}
	if d := sourceDomain(t, got.Surface); d.Coverage != core.CoveragePartial {
		t.Fatalf("domain=%#v", d)
	}
}

func TestAffectedUnavailableDeltaAndGenerationIsUnknownNotEmptyComplete(t *testing.T) {
	sample := affectedDelta(nil, workspace.SelectionUnavailable)
	sample.DiagnosticCode = "delta_unavailable"
	snap := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, RepositoryID: serviceWorkspace().RepositoryID, WorkspaceID: serviceWorkspace().ID, Quality: workspace.QualityUnavailable, ObservedAt: time.Unix(21, 0).UTC(), DiagnosticCode: "snapshot_unavailable"}
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, &fakeAffectedSampler{sample: sample}, nil, &fakeSnapshotter{result: snap}, nil)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Surface.SourceGeneration != "" || len(got.Surface.Relations) != 0 {
		t.Fatalf("surface=%#v", got.Surface)
	}
	if d := sourceDomain(t, got.Surface); d.Coverage != core.CoverageUnknown {
		t.Fatalf("domain=%#v", d)
	}
	if err := got.Surface.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAffectedCleanCompleteSelectionStillHasCompleteDomain(t *testing.T) {
	sample := affectedDelta(nil, workspace.SelectionComplete)
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, &fakeAffectedSampler{sample: sample}, nil, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, nil)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID)})
	if err != nil {
		t.Fatal(err)
	}
	if len(targetPaths(got.Surface)) != 0 || sourceDomain(t, got.Surface).Coverage != core.CoverageComplete {
		t.Fatalf("surface=%#v", got.Surface)
	}
}

func TestAffectedChangedPolicyFileRemainsAffectedPath(t *testing.T) {
	sample := affectedDelta([]string{".shellbeam/verification-policy.toml"}, workspace.SelectionComplete)
	svc := NewAffectedService(&fakeAffectedWorkspaceInspector{ws: serviceWorkspace()}, &fakeAffectedSampler{sample: sample}, nil, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, nil)
	got, err := svc.Derive(context.Background(), AffectedRequest{WorkspaceID: string(serviceWorkspace().ID)})
	if err != nil {
		t.Fatal(err)
	}
	if paths := targetPaths(got.Surface); len(paths) != 1 || paths[0] != ".shellbeam/verification-policy.toml" {
		t.Fatalf("paths=%v", paths)
	}
}

func baseSurfaceForClassification(t *testing.T, path string, coverage core.Coverage) core.AffectedSurface {
	t.Helper()
	g := serviceGen('2')
	provider := &core.ProviderRef{ID: "workspace_delta", Version: 1}
	prov := []string{"selection:test"}
	did, err := core.DomainID(core.DomainSourceSelection, provider, g, prov)
	if err != nil {
		t.Fatal(err)
	}
	d := core.AffectedDomain{DomainID: did, Kind: core.DomainSourceSelection, DerivationAuthority: core.AuthorityMechanical, Coverage: coverage, Provider: provider, SourceGeneration: g, ProvenanceRefs: prov, CapturedAt: time.Unix(1, 0).UTC()}
	relIn := core.RelationIdentityInput{From: core.Subject{Kind: core.SubjectSourceRef, Value: g}, To: core.Subject{Kind: core.SubjectPath, Value: path}, Kind: "mutated_path", Basis: core.BasisObservedMutation, DerivationAuthority: core.AuthorityMechanical, Coverage: coverage, Provider: provider, SourceGeneration: g, ProvenanceRefs: []string{"path:" + path}}
	rid, err := core.RelationID(relIn)
	if err != nil {
		t.Fatal(err)
	}
	r := core.AffectedRelation{RelationID: rid, From: relIn.From, To: relIn.To, Kind: relIn.Kind, Basis: relIn.Basis, DerivationAuthority: relIn.DerivationAuthority, Coverage: relIn.Coverage, Provider: provider, SourceGeneration: g, ProvenanceRefs: relIn.ProvenanceRefs, CapturedAt: time.Unix(1, 0).UTC()}
	return core.AffectedSurface{SchemaVersion: 1, RepositoryID: string(serviceWorkspace().RepositoryID), WorkspaceID: string(serviceWorkspace().ID), SourceGeneration: g, Domains: []core.AffectedDomain{d}, Relations: []core.AffectedRelation{r}}
}
func materializedClassifierPolicy(t *testing.T, classifier core.Classification) core.MaterializedPolicy {
	t.Helper()
	p := core.PolicyContent{SchemaVersion: 1, PolicyID: "p", Classifiers: []core.Classification{classifier}}
	d, err := core.PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return core.MaterializedPolicy{Snapshot: core.PolicySnapshot{RepositoryID: string(serviceWorkspace().RepositoryID), Digest: d, Content: p}, Source: core.PolicyRepositoryAuthored, ApprovalRef: "act_1", ApprovalAuthority: "explicit_caller", ApprovedAt: time.Unix(1, 0).UTC()}
}

func TestEffectiveClassificationProjectsExactPolicyAndCoverage(t *testing.T) {
	base := baseSurfaceForClassification(t, "internal/auth/a.go", core.CoveragePartial)
	p := materializedClassifierPolicy(t, core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"})
	got, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: base, EffectivePolicy: p})
	if err != nil {
		t.Fatal(err)
	}
	var classRel *core.AffectedRelation
	for i := range got.Relations {
		if got.Relations[i].Kind == "classified_as" {
			classRel = &got.Relations[i]
		}
	}
	if classRel == nil || classRel.To.Value != "security_sensitive" || classRel.Coverage != core.CoveragePartial {
		t.Fatalf("relations=%#v", got.Relations)
	}
	found := false
	for _, d := range got.Domains {
		if d.Kind == core.DomainPolicyClassification {
			found = true
			if d.Coverage != core.CoveragePartial {
				t.Fatalf("domain=%#v", d)
			}
		}
	}
	if !found {
		t.Fatal("classification domain missing")
	}
}

func TestProposedPolicyCannotChangeEffectiveClassificationProjection(t *testing.T) {
	base := baseSurfaceForClassification(t, "internal/auth/a.go", core.CoverageComplete)
	p1 := materializedClassifierPolicy(t, core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"})
	_ = materializedClassifierPolicy(t, core.Classification{ID: "other", Paths: []string{"cmd/**"}, SurfaceClass: "other"})
	a, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: base, EffectivePolicy: p1})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: base, EffectivePolicy: p1})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Relations) != len(b.Relations) || a.Relations[len(a.Relations)-1].RelationID != b.Relations[len(b.Relations)-1].RelationID {
		t.Fatalf("effective projection changed a=%#v b=%#v", a, b)
	}
}

func TestEffectiveClassificationCapturedAtComesFromAffectedObservation(t *testing.T) {
	base := baseSurfaceForClassification(t, "internal/auth/a.go", core.CoverageComplete)
	want := base.Domains[0].CapturedAt
	p := materializedClassifierPolicy(t, core.Classification{ID: "security", Paths: []string{"internal/auth/**"}, SurfaceClass: "security_sensitive"})
	p.ApprovedAt = time.Time{}
	got, err := ApplyEffectiveClassifications(ClassificationProjectionRequest{BaseSurface: base, EffectivePolicy: p})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Domains {
		if d.Kind == core.DomainPolicyClassification && !d.CapturedAt.Equal(want) {
			t.Fatalf("captured=%v want=%v", d.CapturedAt, want)
		}
	}
}
