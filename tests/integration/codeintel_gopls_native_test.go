//go:build linux || darwin

package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	goplsadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/gopls"
	sourcefs "github.com/maemreyo/shellbeam/internal/adapter/codeintel/sourcefs"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestNativeGoplsEditLoopDiagnosticsAndNavigation(t *testing.T) {
	harness := newNativeCodeIntelHarness(t, nativeFixture(t))
	defer harness.Close(t)

	validMain := readNativeFile(t, harness.root, "main.go")
	invalidMain := strings.Replace(validMain, "greeter.Message()", "missingSymbol.Message()", 1)
	writeNativeFile(t, harness.root, "main.go", invalidMain)
	harness.sampler.Set(nativeSample(harness.workspace, modifiedChange("main.go")))

	start := time.Now()
	invalid := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles})
	t.Logf("native gopls cold changed-files diagnostics: %s", time.Since(start))
	assertNativeProviderProvenance(t, invalid.Provider)
	if invalid.Selection.Basis != workspacecore.SelectionWorkspaceDirty ||
		invalid.Selection.Freshness != workspacecore.SampleFreshlySampled ||
		invalid.Selection.Completeness != workspacecore.SelectionComplete {
		t.Fatalf("selection=%#v", invalid.Selection)
	}
	if len(invalid.Records) > harness.limits.Result.MaxRecords {
		t.Fatalf("diagnostic record bound exceeded: %d", len(invalid.Records))
	}
	diagnostic := requireDiagnosticContaining(t, invalid, "missingSymbol")
	if len(diagnostic.Message) > harness.limits.Result.MaxTextBytes {
		t.Fatalf("diagnostic message bound exceeded: %d", len(diagnostic.Message))
	}
	assertResolvedCurrent(t, diagnostic.Location, invalid.Records[0].SourceCorrelation)

	writeNativeFile(t, harness.root, "main.go", validMain)
	harness.sampler.Set(nativeSample(harness.workspace, modifiedChange("main.go")))
	start = time.Now()
	clean := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeChangedFiles})
	t.Logf("native gopls warm clean diagnostics: %s", time.Since(start))
	if clean.Status != core.StatusReady || len(clean.Records) != 0 {
		t.Fatalf("stale diagnostic remained current: status=%s records=%#v", clean.Status, clean.Records)
	}

	definitionQuery := positionedQuery(t, harness.root, "main.go", "NewGreeter", core.QueryDefinition)
	start = time.Now()
	definition := harness.Inspect(t, definitionQuery)
	t.Logf("native gopls warm definition: %s", time.Since(start))
	location := requireLocationTarget(t, definition, "definition")
	if location.Kind != core.LocationResolved || location.Resolved == nil {
		t.Fatalf("workspace definition was not promoted to canonical SourceRef: %#v", location)
	}
	assertResolvedRangeText(t, location, []byte(readNativeFile(t, harness.root, "lib/lib.go")), "NewGreeter")

	unicodeQuery := positionedQuery(t, harness.root, "main.go", "render(\"Thế giới\")", core.QueryReferences)
	start = time.Now()
	references := harness.Inspect(t, unicodeQuery)
	t.Logf("native gopls warm unicode references: %s", time.Since(start))
	if len(references.Records) < 2 {
		t.Fatalf("references=%#v", references.Records)
	}

	typeDefinition := harness.Inspect(t, positionedQuery(t, harness.root, "main.go", "greeter.Message", core.QueryTypeDefinition))
	if len(typeDefinition.Records) == 0 {
		t.Fatalf("type definition missing: %#v", typeDefinition)
	}
	typeSummary := harness.Inspect(t, positionedQuery(t, harness.root, "main.go", "greeter.Message", core.QueryTypeSummary))
	if len(typeSummary.Records) != 1 || typeSummary.Records[0].TypeSummary == nil || strings.TrimSpace(typeSummary.Records[0].TypeSummary.Text) == "" {
		t.Fatalf("type summary=%#v", typeSummary.Records)
	}

	symbols := harness.Inspect(t, core.Query{Kind: core.QuerySymbols, Scope: core.ScopeFile, Path: "main.go"})
	requireSymbol(t, symbols, "render")
	requireSymbol(t, symbols, "main")

	callers := harness.Inspect(t, positionedQuery(t, harness.root, "main.go", "render(name", core.QueryCallers))
	assertCallHierarchyHonesty(t, callers, "caller")
	callees := harness.Inspect(t, positionedQuery(t, harness.root, "main.go", "render(name", core.QueryCallees))
	assertCallHierarchyHonesty(t, callees, "callee")

	external := harness.Inspect(t, positionedQuery(t, harness.root, "main.go", "Println", core.QueryDefinition))
	assertExternalDefinitionIsProviderReported(t, external)
}

func TestNativeGoplsSemanticContextWideningAndBounds(t *testing.T) {
	harness := newNativeCodeIntelHarness(t, nativeFixture(t))
	defer harness.Close(t)

	warm := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"})
	if warm.Provider.ProviderID != core.ProviderGoSemantic {
		t.Fatalf("warm provider=%#v", warm.Provider)
	}
	originalMod := readNativeFile(t, harness.root, "go.mod")
	changedMod := strings.Replace(originalMod, "example.com/codeintelfixture", "example.com/codeintelfixture_changed", 1)
	writeNativeFile(t, harness.root, "go.mod", changedMod)
	harness.sampler.Set(nativeSample(harness.workspace, modifiedChange("go.mod")))
	contextChanged := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"})
	if contextChanged.Provider.Incarnation != warm.Provider.Incarnation {
		t.Fatalf("go.mod source change unexpectedly restarted compatible provider: before=%q after=%q", warm.Provider.Incarnation, contextChanged.Provider.Incarnation)
	}
	if contextChanged.Status != core.StatusReady && contextChanged.Status != core.StatusPartial {
		t.Fatalf("semantic context change status=%s", contextChanged.Status)
	}

	writeNativeFile(t, harness.root, "go.mod", originalMod)
	harness.sampler.Set(nativeSample(harness.workspace, modifiedChange("go.mod")))
	restored := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"})
	if restored.Provider.Incarnation != warm.Provider.Incarnation {
		t.Fatalf("restored semantic context restarted compatible provider: %q != %q", restored.Provider.Incarnation, warm.Provider.Incarnation)
	}

	headOnly := workspacecore.ChangeRecord{
		PathTransition: workspacecore.PathNone, SourceTransition: workspacecore.SourceUnchanged, VCSTransition: workspacecore.VCSHead,
	}
	harness.sampler.Set(nativeSample(harness.workspace, headOnly))
	afterHead := harness.Inspect(t, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"})
	if afterHead.Provider.Incarnation != warm.Provider.Incarnation || afterHead.Status != core.StatusReady {
		t.Fatalf("HEAD-only transition disturbed semantic provider: before=%q after=%q status=%s", warm.Provider.Incarnation, afterHead.Provider.Incarnation, afterHead.Status)
	}

	harness.limits.Result.MaxRecords = 2
	boundedHarness := newNativeCodeIntelHarnessWithLimits(t, harness.root, harness.limits)
	defer boundedHarness.Close(t)
	bounded := boundedHarness.Inspect(t, core.Query{Kind: core.QuerySymbols, Scope: core.ScopeFile, Path: "main.go"})
	if len(bounded.Records) > 2 {
		t.Fatalf("record bound exceeded: %d", len(bounded.Records))
	}
	if bounded.Status != core.StatusPartial {
		t.Fatalf("bounded document symbols status=%s records=%d", bounded.Status, len(bounded.Records))
	}
	if err := boundedHarness.manager.Close(); err != nil {
		t.Fatalf("bounded provider close: %v", err)
	}
	if err := harness.manager.Close(); err != nil {
		t.Fatalf("semantic-context provider close: %v", err)
	}
}

type nativeCodeIntelHarness struct {
	root      string
	workspace workspacecore.Workspace
	sampler   *nativeSampler
	manager   *appcodeintel.ProviderManager
	service   *appcodeintel.Service
	limits    appcodeintel.ServiceLimits
}

func newNativeCodeIntelHarness(t *testing.T, root string) *nativeCodeIntelHarness {
	t.Helper()
	return newNativeCodeIntelHarnessWithLimits(t, root, nativeServiceLimits())
}

func newNativeCodeIntelHarnessWithLimits(t *testing.T, root string, limits appcodeintel.ServiceLimits) *nativeCodeIntelHarness {
	t.Helper()
	goplsPath := requireNativeGopls(t)
	workspace := nativeWorkspace(root)
	sampler := &nativeSampler{sample: nativeSample(workspace)}
	store, err := appcodeintel.NewSourceStore(appcodeintel.SourceStoreConfig{
		MaxEntries: 128, MaxRetainedBytes: 8 << 20, TTL: 5 * time.Minute, MaxTombstones: 256,
	})
	if err != nil {
		t.Fatal(err)
	}
	binder, err := sourcefs.NewBinder(store, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	config := goplsadapter.DefaultConfig()
	config.Executable = goplsPath
	config.DiagnosticWait = 3 * time.Second
	factory, err := goplsadapter.NewFactory(config)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := appcodeintel.NewProviderManager(factory, factory, appcodeintel.ProviderManagerLimits{
		MaxInstances: 2, MaxInFlight: 4, MaxInFlightPerProvider: 2, MaxQueueDepth: 4,
		QueueWait: 250 * time.Millisecond, IdleTTL: time.Minute,
		FailuresBeforeCooldown: 3, FailureWindow: time.Minute, Cooldown: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	lookup := nativeWorkspaceLookup{workspace: workspace}
	service, err := appcodeintel.NewService(lookup, sampler, nil, binder, manager, nativeCoherence{}, limits)
	if err != nil {
		_ = manager.Close()
		t.Fatal(err)
	}
	return &nativeCodeIntelHarness{root: root, workspace: workspace, sampler: sampler, manager: manager, service: service, limits: limits}
}

func (h *nativeCodeIntelHarness) Inspect(t *testing.T, query core.Query) core.Result {
	t.Helper()
	result, err := h.service.Inspect(t.Context(), appcodeintel.InspectRequest{WorkspaceID: string(h.workspace.ID), Query: query})
	if err != nil {
		var detail *appcodeintel.Error
		if errors.As(err, &detail) {
			t.Fatalf("inspect %s: %v code=%q cause=%v", query.Kind, err, detail.Code, detail.Cause)
		}
		t.Fatalf("inspect %s: %v code=%q", query.Kind, err, appcodeintel.ErrorCode(err))
	}
	return result
}

func (h *nativeCodeIntelHarness) Close(t *testing.T) {
	t.Helper()
	if err := h.manager.Close(); err != nil {
		t.Fatalf("close native gopls provider: %v", err)
	}
}

func nativeServiceLimits() appcodeintel.ServiceLimits {
	return appcodeintel.ServiceLimits{
		Delta:              workspacecore.DeltaLimits{MaxPaths: 64, MaxOutputBytes: 256 << 10, TimeoutMS: 1000},
		Result:             core.ResultLimits{MaxRecords: 128, MaxResponseBytes: 1 << 20, MaxTextBytes: 64 << 10, MaxRelatedLocations: 32},
		MaxSelectedSources: 64, MaxSelectedSourceBytes: 4 << 20, MaxDuration: 10 * time.Second,
	}
}

func requireNativeGopls(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("gopls")
	if err == nil {
		return path
	}
	if os.Getenv("SHELLBEAM_REQUIRE_NATIVE_GOPLS") == "1" {
		t.Fatalf("native E29 readiness requires gopls on PATH: %v", err)
	}
	t.Skipf("native gopls unavailable on PATH: %v", err)
	return ""
}

func nativeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", "codeintel_go"))); err != nil {
		t.Fatal(err)
	}
	return root
}

func nativeWorkspace(root string) workspacecore.Workspace {
	now := time.Now().UTC()
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID("ws_01K00000000000000000000010"),
		RepositoryID:  workspacecore.RepositoryID("repo_01K00000000000000000000010"),
		Label:         "native-codeintel",
		Root:          root,
		GitDir:        filepath.Join(root, ".git"),
		CreatedAt:     now,
		LastSeenAt:    now,
	}
}

type nativeWorkspaceLookup struct{ workspace workspacecore.Workspace }

func (l nativeWorkspaceLookup) Inspect(_ context.Context, raw string) (workspacecore.Workspace, error) {
	if raw != string(l.workspace.ID) {
		return workspacecore.Workspace{}, errors.New("workspace mismatch")
	}
	return l.workspace, nil
}

type nativeSampler struct {
	mu     sync.Mutex
	sample workspacecore.DeltaSample
}

func (s *nativeSampler) Sample(context.Context, workspacecore.WorkspaceID, workspacecore.DeltaLimits) workspacecore.DeltaSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sample
}

func (s *nativeSampler) Set(sample workspacecore.DeltaSample) {
	s.mu.Lock()
	s.sample = sample
	s.mu.Unlock()
}

type nativeCoherence struct{}

func (nativeCoherence) CaptureBarrier() workspacecore.CoherenceBarrier {
	return workspacecore.CoherenceBarrier{DaemonIncarnation: "native-codeintel", Epoch: 1}
}

func nativeSample(workspace workspacecore.Workspace, changes ...workspacecore.ChangeRecord) workspacecore.DeltaSample {
	barrier := nativeCoherence{}.CaptureBarrier()
	return workspacecore.DeltaSample{
		SchemaVersion:   workspacecore.DeltaSampleSchemaVersion,
		RepositoryID:    workspace.RepositoryID,
		WorkspaceID:     workspace.ID,
		Freshness:       workspacecore.SampleFreshlySampled,
		Completeness:    workspacecore.SelectionComplete,
		ObservedAt:      time.Now().UTC(),
		Changes:         append([]workspacecore.ChangeRecord(nil), changes...),
		BarrierBefore:   barrier,
		BarrierAfter:    barrier,
		CacheEligible:   true,
		RecordsObserved: len(changes),
	}
}

func modifiedChange(path string) workspacecore.ChangeRecord {
	return workspacecore.ChangeRecord{
		PathTransition: workspacecore.PathModified, NewPath: path,
		SourceTransition: workspacecore.SourceBytesChanged, VCSTransition: workspacecore.VCSNone,
	}
}

func positionedQuery(t *testing.T, root, path, needle string, kind core.QueryKind) core.Query {
	t.Helper()
	data := []byte(readNativeFile(t, root, path))
	offset := strings.Index(string(data), needle)
	if offset < 0 {
		t.Fatalf("needle %q missing from %s", needle, path)
	}
	line, column, err := core.ByteOffsetToDisplayPosition(data, int64(offset))
	if err != nil {
		t.Fatal(err)
	}
	return core.Query{Kind: kind, Path: path, Line: line, Column: column}
}

func readNativeFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeNativeFile(t *testing.T, root, path, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireDiagnosticContaining(t *testing.T, result core.Result, needle string) *core.Diagnostic {
	t.Helper()
	for i := range result.Records {
		if diagnostic := result.Records[i].Diagnostic; diagnostic != nil && strings.Contains(diagnostic.Message, needle) {
			return diagnostic
		}
	}
	t.Fatalf("diagnostic containing %q missing: status=%s records=%#v", needle, result.Status, result.Records)
	return nil
}

func assertResolvedCurrent(t *testing.T, location core.SourceLocation, correlation core.SourceCorrelation) {
	t.Helper()
	if location.Kind != core.LocationResolved || location.Resolved == nil || !strings.HasPrefix(location.Resolved.SourceRefID, "src_") || correlation != core.CorrelationCurrent {
		t.Fatalf("location/correlation not exact current: location=%#v correlation=%s", location, correlation)
	}
}

func requireLocationTarget(t *testing.T, result core.Result, relationship string) core.SourceLocation {
	t.Helper()
	for i := range result.Records {
		target := result.Records[i].LocationTarget
		if target != nil && (relationship == "" || target.Relationship == relationship) {
			return target.Location
		}
	}
	t.Fatalf("location target %q missing: %#v", relationship, result.Records)
	return core.SourceLocation{}
}

func requireSymbol(t *testing.T, result core.Result, name string) {
	t.Helper()
	for i := range result.Records {
		if symbol := result.Records[i].Symbol; symbol != nil && symbol.Name == name {
			return
		}
	}
	t.Fatalf("symbol %q missing: %#v", name, result.Records)
}

func assertExternalDefinitionIsProviderReported(t *testing.T, result core.Result) {
	t.Helper()
	location := requireLocationTarget(t, result, "definition")
	if location.Kind != core.LocationProviderReported || location.ProviderReported == nil {
		t.Fatalf("toolchain definition received fabricated canonical SourceRef: %#v", location)
	}
	if location.ProviderReported.Origin != core.OriginToolchain && location.ProviderReported.Origin != core.OriginDependency {
		t.Fatalf("unexpected external origin: %#v", location.ProviderReported)
	}
}

func assertNativeProviderProvenance(t *testing.T, metadata core.ProviderMetadata) {
	t.Helper()
	if metadata.ProviderID != core.ProviderGoSemantic || metadata.ExecutableVersion == "" ||
		metadata.ConfigFingerprint == "" || metadata.BuildFingerprint == "" || metadata.BuildQuality == "" ||
		metadata.Coverage == "" {
		t.Fatalf("provider provenance incomplete: %#v", metadata)
	}
}

func assertResolvedRangeText(t *testing.T, location core.SourceLocation, source []byte, want string) {
	t.Helper()
	if location.Resolved == nil {
		t.Fatalf("resolved location missing: %#v", location)
	}
	start, end := location.Resolved.StartByte, location.Resolved.EndByte
	if start < 0 || end < start || end > int64(len(source)) || string(source[start:end]) != want {
		t.Fatalf("resolved bytes=[%d,%d) text=%q want=%q", start, end, boundedSlice(source, start, end), want)
	}
	line, column, err := core.ByteOffsetToDisplayPosition(source, start)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := core.DisplayPositionToByteOffset(source, line, column)
	if err != nil {
		t.Fatal(err)
	}
	if roundTrip != start {
		t.Fatalf("canonical byte range did not round-trip: start=%d round_trip=%d", start, roundTrip)
	}
}

func boundedSlice(source []byte, start, end int64) string {
	if start < 0 || end < start || start > int64(len(source)) || end > int64(len(source)) {
		return "<out-of-range>"
	}
	return string(source[start:end])
}

func assertCallHierarchyHonesty(t *testing.T, result core.Result, relationship string) {
	t.Helper()
	found := false
	for i := range result.Records {
		target := result.Records[i].LocationTarget
		if target == nil || target.Relationship != relationship {
			continue
		}
		found = true
		if result.Records[i].Completeness == core.CompletenessExhaustive {
			t.Fatalf("%s hierarchy overclaimed exhaustive completeness: %#v", relationship, result.Records[i])
		}
	}
	if !found {
		t.Fatalf("%s hierarchy target missing: %#v", relationship, result.Records)
	}
}

func hasRelationship(result core.Result, relationship string) bool {
	for i := range result.Records {
		if target := result.Records[i].LocationTarget; target != nil && target.Relationship == relationship {
			return true
		}
	}
	return false
}
