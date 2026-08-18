package codeintel

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestServiceChangedFilesUsesWorkspaceDirtyWithoutActivity(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete,
		serviceModified("go.mod"), serviceModified("internal/app/service.go"))
	binder := &serviceBinder{current: map[string]BoundSource{
		"go.mod":                  serviceBound("src_01K00000000000000000000001", "go.mod", "module example\n"),
		"internal/app/service.go": serviceBound("src_01K00000000000000000000002", "internal/app/service.go", "package app\n"),
	}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic("src_01K00000000000000000000002", "undefined: ServerInfo"))}
	service := newServiceForTest(t, serviceDeps{
		workspace: workspace,
		sample:    sample,
		binder:    binder,
		provider:  provider,
	})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.Basis != workspacecore.SelectionWorkspaceDirty || result.Selection.Completeness != workspacecore.SelectionComplete {
		t.Fatalf("selection=%#v", result.Selection)
	}
	if got := boundPaths(provider.last.SelectedSources); !reflect.DeepEqual(got, []string{"go.mod", "internal/app/service.go"}) {
		t.Fatalf("selected=%v", got)
	}
	if len(provider.last.Sample.Changes) != 2 {
		t.Fatalf("provider lost full sample: %#v", provider.last.Sample)
	}
	if len(result.Records) != 1 || result.Records[0].SourceCorrelation != core.CorrelationCurrent {
		t.Fatalf("records=%#v", result.Records)
	}
}

func TestServiceActivityDeltaHidesInheritedDirtyButProviderGetsFullSample(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete,
		serviceModified("go.mod"), serviceModified("internal/app/service.go"))
	comparison := activitycore.Comparison{
		InheritedDirty:        []activitycore.PathFact{{Path: "go.mod", State: activitycore.PathModified}},
		ObservedSinceBaseline: []activitycore.PathFact{{Path: "internal/app/service.go", State: activitycore.PathModified}},
	}
	binder := &serviceBinder{current: map[string]BoundSource{
		"internal/app/service.go": serviceBound("src_01K00000000000000000000002", "internal/app/service.go", "package app\n"),
	}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic("src_01K00000000000000000000002", "undefined: ServerInfo"))}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, comparison: &comparison, binder: binder, provider: provider})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID), ActivityID: "activity-codeintel",
		Query: core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.Basis != workspacecore.SelectionActivityDelta || result.Selection.Completeness != workspacecore.SelectionComplete {
		t.Fatalf("selection=%#v", result.Selection)
	}
	if got := boundPaths(provider.last.SelectedSources); !reflect.DeepEqual(got, []string{"internal/app/service.go"}) {
		t.Fatalf("model-facing selected=%v", got)
	}
	if paths := samplePaths(provider.last.Sample); !reflect.DeepEqual(paths, []string{"go.mod", "internal/app/service.go"}) {
		t.Fatalf("hidden provider sample=%v", paths)
	}
}

func TestServiceDivergedActivityUsesExplicitWorkspaceFallbackWithoutChangingBasis(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete, serviceModified("main.go"))
	comparison := activitycore.Comparison{BaselineDiverged: true, DivergenceReason: "branch_changed"}
	binder := &serviceBinder{current: map[string]BoundSource{
		"main.go": serviceBound("src_01K00000000000000000000003", "main.go", "package main\n"),
	}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic("src_01K00000000000000000000003", "warning"))}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, comparison: &comparison, binder: binder, provider: provider})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID), ActivityID: "activity-codeintel",
		Query: core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.Basis != workspacecore.SelectionActivityDelta ||
		result.Selection.Completeness != workspacecore.SelectionDiverged ||
		result.Selection.Fallback != string(workspacecore.SelectionWorkspaceDirty) ||
		result.Status != core.StatusPartial {
		t.Fatalf("result=%#v", result)
	}
	if got := boundPaths(provider.last.SelectedSources); !reflect.DeepEqual(got, []string{"main.go"}) {
		t.Fatalf("fallback paths=%v", got)
	}
}

func TestServiceNoChangedPathsStillSynchronizesProvider(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete)
	provider := &serviceProvider{response: serviceProviderResponse()}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, binder: &serviceBinder{}, provider: provider})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || len(provider.last.SelectedSources) != 0 || result.Status != core.StatusReady {
		t.Fatalf("calls=%d request=%#v result=%#v", provider.calls, provider.last, result)
	}
}

func TestServiceSelectionBudgetReturnsPartialWithProvenRecords(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete,
		serviceModified("a.go"), serviceModified("b.go"), serviceModified("c.go"))
	binder := &serviceBinder{current: map[string]BoundSource{
		"a.go": serviceBound("src_01K00000000000000000000011", "a.go", "a"),
		"b.go": serviceBound("src_01K00000000000000000000012", "b.go", "b"),
		"c.go": serviceBound("src_01K00000000000000000000013", "c.go", "c"),
	}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic("src_01K00000000000000000000011", "a diagnostic"))}
	service := newServiceForTestWithLimits(t, serviceDeps{workspace: workspace, sample: sample, binder: binder, provider: provider}, serviceTestLimits(1))

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.StatusPartial || result.Selection.Completeness != workspacecore.SelectionPartial {
		t.Fatalf("result=%#v", result)
	}
	if len(provider.last.SelectedSources) != 1 || len(result.Records) != 1 {
		t.Fatalf("request=%#v records=%#v", provider.last, result.Records)
	}
}

func TestServiceTwoBarrierUnrelatedEpochChangeKeepsExactRecordButMarksSelectionPotentiallyStale(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete, serviceModified("foo.go"))
	original := serviceBound("src_01K00000000000000000000021", "foo.go", "package p\n")
	rebound := serviceBound("src_01K00000000000000000000022", "foo.go", "package p\n")
	binder := &serviceBinder{current: map[string]BoundSource{"foo.go": original}, rebound: map[string]BoundSource{"foo.go": rebound}}
	coherence := &serviceCoherence{barrier: workspacecore.CoherenceBarrier{DaemonIncarnation: "daemon", Epoch: 1}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic(string(original.Ref.ID), "warning"))}
	provider.onQuery = func() { coherence.barrier.Epoch = 2 }
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, binder: binder, provider: provider, coherence: coherence})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.Completeness != workspacecore.SelectionPotentiallyStale || len(result.Records) != 1 || result.Records[0].SourceCorrelation != core.CorrelationCurrent {
		t.Fatalf("result=%#v", result)
	}
}

func TestServiceTwoBarrierSelectedSourceReplacementDowngradesOnlyThatRecord(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete, serviceModified("foo.go"))
	original := serviceBound("src_01K00000000000000000000031", "foo.go", "package old\n")
	rebound := serviceBound("src_01K00000000000000000000032", "foo.go", "package new\n")
	binder := &serviceBinder{current: map[string]BoundSource{"foo.go": original}, rebound: map[string]BoundSource{"foo.go": rebound}}
	coherence := &serviceCoherence{barrier: workspacecore.CoherenceBarrier{DaemonIncarnation: "daemon", Epoch: 1}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic(string(original.Ref.ID), "warning"))}
	provider.onQuery = func() { coherence.barrier.Epoch = 2 }
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, binder: binder, provider: provider, coherence: coherence})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].SourceCorrelation != core.CorrelationSourceChangedDuringQuery {
		t.Fatalf("records=%#v", result.Records)
	}
	if result.Status != core.StatusPartial || result.Selection.Completeness != workspacecore.SelectionPotentiallyStale {
		t.Fatalf("result=%#v", result)
	}
}

func TestServiceFileQueryDoesNotRequireGitDeltaAvailability(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionUnavailable)
	sample.DiagnosticCode = "not_git"
	bound := serviceBound("src_01K00000000000000000000041", "main.go", "package main\n")
	binder := &serviceBinder{current: map[string]BoundSource{"main.go": bound}}
	provider := &serviceProvider{response: serviceProviderResponse(serviceDiagnostic(string(bound.Ref.ID), "warning"))}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, binder: binder, provider: provider})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.StatusReady || len(provider.last.SelectedSources) != 1 || provider.last.SelectedSources[0].Ref.LogicalPath != "main.go" {
		t.Fatalf("result=%#v request=%#v", result, provider.last)
	}
}

func TestServiceRejectsUnsafeProviderReportedLocation(t *testing.T) {
	workspace := serviceTestWorkspace()
	sample := serviceTestSample(workspace, workspacecore.SelectionComplete)
	provider := &serviceProvider{response: serviceProviderResponse(ProviderDiagnostic{
		Severity: core.SeverityWarning,
		Message:  "external",
		Location: core.SourceLocation{Kind: core.LocationProviderReported, ProviderReported: &core.ProviderReportedLocation{
			Origin: core.OriginDependency, SanitizedLogicalPath: "/Users/me/go/pkg/mod/secret.go",
			Line: 1, Column: 1, NormalizationQuality: core.NormalizationPartial,
		}},
	})}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: sample, binder: &serviceBinder{}, provider: provider})

	_, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles},
	})
	if err == nil {
		t.Fatal("unsafe provider-reported absolute path accepted")
	}
}

type serviceDeps struct {
	workspace  workspacecore.Workspace
	sample     workspacecore.DeltaSample
	comparison *activitycore.Comparison
	binder     *serviceBinder
	provider   *serviceProvider
	coherence  *serviceCoherence
}

func newServiceForTest(t *testing.T, deps serviceDeps) *Service {
	t.Helper()
	return newServiceForTestWithLimits(t, deps, serviceTestLimits(8))
}

func newServiceForTestWithLimits(t *testing.T, deps serviceDeps, limits ServiceLimits) *Service {
	t.Helper()
	lookup := &serviceWorkspaceLookup{workspace: deps.workspace}
	sampler := &serviceSampler{sample: deps.sample}
	selector := &serviceActivitySelector{comparison: deps.comparison}
	if deps.binder == nil {
		deps.binder = &serviceBinder{}
	}
	if deps.provider == nil {
		deps.provider = &serviceProvider{response: serviceProviderResponse()}
	}
	if deps.coherence == nil {
		deps.coherence = &serviceCoherence{barrier: workspacecore.CoherenceBarrier{DaemonIncarnation: "daemon", Epoch: 1}}
	}
	service, err := NewService(lookup, sampler, selector, deps.binder, deps.provider, deps.coherence, limits)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func serviceTestLimits(maxSelected int) ServiceLimits {
	return ServiceLimits{
		Delta:                  workspacecore.DeltaLimits{MaxPaths: 64, MaxOutputBytes: 1 << 20, TimeoutMS: 1000},
		Result:                 core.ResultLimits{MaxRecords: 32, MaxResponseBytes: 1 << 20, MaxTextBytes: 4096, MaxRelatedLocations: 8},
		MaxSelectedSources:     maxSelected,
		MaxSelectedSourceBytes: 1 << 20,
		MaxDuration:            time.Second,
	}
}

type serviceWorkspaceLookup struct {
	workspace workspacecore.Workspace
	err       error
}

func (f *serviceWorkspaceLookup) Inspect(_ context.Context, raw string) (workspacecore.Workspace, error) {
	if f.err != nil {
		return workspacecore.Workspace{}, f.err
	}
	if raw != string(f.workspace.ID) {
		return workspacecore.Workspace{}, errors.New("workspace mismatch")
	}
	return f.workspace, nil
}

type serviceSampler struct {
	sample workspacecore.DeltaSample
	calls  int
}

func (f *serviceSampler) Sample(_ context.Context, _ workspacecore.WorkspaceID, _ workspacecore.DeltaLimits) workspacecore.DeltaSample {
	f.calls++
	return f.sample
}

type serviceActivitySelector struct {
	comparison *activitycore.Comparison
	err        error
}

func (f *serviceActivitySelector) CompareWorkspace(_ context.Context, _ string, _ workspacecore.DeltaSample) (activitycore.Comparison, error) {
	if f.err != nil {
		return activitycore.Comparison{}, f.err
	}
	if f.comparison == nil {
		return activitycore.Comparison{BaselineDiverged: true, DivergenceReason: "evidence_unavailable"}, nil
	}
	return *f.comparison, nil
}

type serviceBinder struct {
	current map[string]BoundSource
	rebound map[string]BoundSource
	calls   map[string]int
}

func (f *serviceBinder) Bind(_ context.Context, _ workspacecore.Workspace, path string) (BoundSource, error) {
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[path]++
	if f.calls[path] > 1 && f.rebound != nil {
		if source, ok := f.rebound[path]; ok {
			return cloneBoundSource(source), nil
		}
	}
	if source, ok := f.current[path]; ok {
		return cloneBoundSource(source), nil
	}
	return BoundSource{}, &Error{Code: CodeSourceRefUnavailable}
}

func (f *serviceBinder) Resolve(id core.SourceRefID) (BoundSource, SourceRefState) {
	for _, source := range f.current {
		if source.Ref.ID == id {
			return cloneBoundSource(source), SourceRefCurrent
		}
	}
	return BoundSource{}, SourceRefUnavailable
}

type serviceProvider struct {
	response ProviderResponse
	err      error
	calls    int
	last     ProviderRequest
	onQuery  func()
}

func (f *serviceProvider) Query(_ context.Context, request ProviderRequest) (ProviderResponse, error) {
	f.calls++
	f.last = request
	f.last.SelectedSources = append([]BoundSource(nil), request.SelectedSources...)
	if f.onQuery != nil {
		f.onQuery()
	}
	return f.response, f.err
}

type serviceCoherence struct {
	barrier workspacecore.CoherenceBarrier
}

func (f *serviceCoherence) CaptureBarrier() workspacecore.CoherenceBarrier { return f.barrier }

func serviceTestWorkspace() workspacecore.Workspace {
	now := time.Unix(1_700_000_000, 0).UTC()
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
		RepositoryID:  workspacecore.RepositoryID("repo_01K00000000000000000000000"),
		Label:         "codeintel",
		Root:          "/tmp/codeintel",
		GitDir:        "/tmp/codeintel/.git",
		CreatedAt:     now,
		LastSeenAt:    now,
	}
}

func serviceTestSample(workspace workspacecore.Workspace, completeness workspacecore.SelectionCompleteness, changes ...workspacecore.ChangeRecord) workspacecore.DeltaSample {
	now := time.Unix(1_700_000_001, 0).UTC()
	return workspacecore.DeltaSample{
		SchemaVersion:   workspacecore.DeltaSampleSchemaVersion,
		RepositoryID:    workspace.RepositoryID,
		WorkspaceID:     workspace.ID,
		Freshness:       workspacecore.SampleFreshlySampled,
		Completeness:    completeness,
		ObservedAt:      now,
		Changes:         changes,
		BarrierBefore:   workspacecore.CoherenceBarrier{DaemonIncarnation: "daemon", Epoch: 1},
		BarrierAfter:    workspacecore.CoherenceBarrier{DaemonIncarnation: "daemon", Epoch: 1},
		RecordsObserved: len(changes),
		BytesObserved:   int64(len(changes) * 10),
		DiagnosticCode:  "",
	}
}

func serviceModified(path string) workspacecore.ChangeRecord {
	return workspacecore.ChangeRecord{
		PathTransition:   workspacecore.PathModified,
		SourceTransition: workspacecore.SourceBytesChanged,
		VCSTransition:    workspacecore.VCSNone,
		NewPath:          path,
	}
}

func serviceBound(id, path, content string) BoundSource {
	return BoundSource{
		Ref: core.SourceRef{
			ID:                core.SourceRefID(id),
			Origin:            core.SourceWorkspace,
			RepositoryID:      workspacecore.RepositoryID("repo_01K00000000000000000000000"),
			WorkspaceID:       workspacecore.WorkspaceID("ws_01K00000000000000000000000"),
			LogicalPath:       path,
			DisplayIdentity:   path,
			ResolutionQuality: core.ResolutionExact,
			TextEncoding:      core.TextEncodingUTF8,
		},
		Bytes: []byte(content),
	}
}

func serviceProviderResponse(diagnostics ...ProviderDiagnostic) ProviderResponse {
	return ProviderResponse{
		Status: core.StatusReady,
		Metadata: core.ProviderMetadata{
			ProviderID: "go_semantic", Incarnation: "provider_01K00000000000000000000000",
			BuildQuality: "observed", Coverage: core.SyncExactForKnownPaths,
		},
		Diagnostics: diagnostics,
	}
}

func serviceDiagnostic(sourceRefID, message string) ProviderDiagnostic {
	return ProviderDiagnostic{
		Severity: core.SeverityWarning,
		Message:  message,
		Location: core.SourceLocation{Kind: core.LocationResolved, Resolved: &core.ResolvedSourceLocation{
			SourceRefID: sourceRefID, StartByte: 0, EndByte: 1,
		}},
	}
}

func boundPaths(sources []BoundSource) []string {
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		paths = append(paths, source.Ref.LogicalPath)
	}
	sort.Strings(paths)
	return paths
}

func samplePaths(sample workspacecore.DeltaSample) []string {
	paths := make([]string, 0, len(sample.Changes))
	for _, change := range sample.Changes {
		path := change.NewPath
		if path == "" {
			path = change.OldPath
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func TestServicePromotesObservedWorkspaceNavigationLocationToExactSourceRef(t *testing.T) {
	workspace := serviceTestWorkspace()
	main := serviceBound("src_01K00000000000000000000051", "main.go", "package p\nvar _ = X\n")
	target := serviceBound("src_01K00000000000000000000052", "other.go", "package p\nvar X = 1\n")
	binder := &serviceBinder{current: map[string]BoundSource{"main.go": main, "other.go": target}}
	response := serviceProviderResponse()
	response.Locations = []ProviderLocation{{
		Name: "X", Relationship: "definition", Authority: core.AuthorityMechanical,
		Completeness: core.CompletenessProviderReported,
		Location: core.SourceLocation{Kind: core.LocationProviderReported, ProviderReported: &core.ProviderReportedLocation{
			Origin: core.OriginRepository, SanitizedLogicalPath: "other.go",
			Line: 2, Column: 5, EndLine: 2, EndColumn: 6, NormalizationQuality: core.NormalizationExact,
		}},
		Observation: &LocationObservation{LogicalPath: "other.go", Bytes: append([]byte(nil), target.Bytes...)},
	}}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: serviceTestSample(workspace, workspacecore.SelectionComplete), binder: binder, provider: &serviceProvider{response: response}})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDefinition, Path: "main.go", Line: 2, Column: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].LocationTarget == nil {
		t.Fatalf("records=%#v", result.Records)
	}
	location := result.Records[0].LocationTarget.Location
	if location.Resolved == nil || location.Resolved.SourceRefID != string(target.Ref.ID) {
		t.Fatalf("location=%+v", location)
	}
	if location.Resolved.StartByte != int64(len("package p\nvar ")) || location.Resolved.EndByte != int64(len("package p\nvar X")) {
		t.Fatalf("resolved range=%+v", location.Resolved)
	}
	if location.Resolved.Display == nil || location.Resolved.Display.Path != "other.go" || location.Resolved.Display.Line != 2 || location.Resolved.Display.Column != 5 || location.Resolved.Display.Preview != "var X = 1" {
		t.Fatalf("display navigation=%+v", location.Resolved.Display)
	}
}

func TestServiceDoesNotPromoteWorkspaceLocationWhenObservedBytesChanged(t *testing.T) {
	workspace := serviceTestWorkspace()
	main := serviceBound("src_01K00000000000000000000053", "main.go", "package p\nvar _ = X\n")
	current := serviceBound("src_01K00000000000000000000054", "other.go", "package p\nvar Y = 1\n")
	binder := &serviceBinder{current: map[string]BoundSource{"main.go": main, "other.go": current}}
	response := serviceProviderResponse()
	response.Locations = []ProviderLocation{{
		Name: "X", Relationship: "definition", Authority: core.AuthorityMechanical,
		Completeness: core.CompletenessProviderReported,
		Location: core.SourceLocation{Kind: core.LocationProviderReported, ProviderReported: &core.ProviderReportedLocation{
			Origin: core.OriginRepository, SanitizedLogicalPath: "other.go",
			Line: 2, Column: 5, EndLine: 2, EndColumn: 6, NormalizationQuality: core.NormalizationExact,
		}},
		Observation: &LocationObservation{LogicalPath: "other.go", Bytes: []byte("package p\nvar X = 1\n")},
	}}
	service := newServiceForTest(t, serviceDeps{workspace: workspace, sample: serviceTestSample(workspace, workspacecore.SelectionComplete), binder: binder, provider: &serviceProvider{response: response}})

	result, err := service.Inspect(t.Context(), InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDefinition, Path: "main.go", Line: 2, Column: 9},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].LocationTarget == nil || result.Records[0].LocationTarget.Location.ProviderReported == nil {
		t.Fatalf("location was incorrectly promoted: %#v", result.Records)
	}
}
