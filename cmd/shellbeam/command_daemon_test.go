package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	environmentadapter "github.com/maemreyo/shellbeam/internal/adapter/environment"
	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDaemonCatalogAdvertisesCodeIntelligence(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if got := catalog.Features[capability.FeatureCodeIntelligence]; got != capability.Available {
		t.Fatalf("code intelligence availability=%q", got)
	}
}

func TestDaemonCodeIntelligenceRuntimeStartsProviderOnlyOnInspect(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := daemonTestWorkspace(root)
	lookup := daemonWorkspaceLookup{workspace: workspace}
	provider := &countingCodeProviderFactory{}

	runtime, err := newCodeIntelligenceRuntimeWithProvider(lookup, nil, nil, nil, provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	if provider.startCount() != 0 {
		t.Fatalf("provider prewarmed during daemon composition: %d", provider.startCount())
	}
	_ = daemonCatalog(capability.Limits{})
	if provider.startCount() != 0 {
		t.Fatalf("capability advertisement started provider: %d", provider.startCount())
	}

	request := appcodeintel.InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"},
	}
	if _, err := runtime.Service.Inspect(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if provider.startCount() != 1 {
		t.Fatalf("first inspect provider starts=%d", provider.startCount())
	}
	if _, err := runtime.Service.Inspect(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if provider.startCount() != 1 {
		t.Fatalf("warm provider was not reused: starts=%d", provider.startCount())
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if provider.closeCount() != 1 {
		t.Fatalf("runtime close did not reap provider: closes=%d", provider.closeCount())
	}
}

type daemonWorkspaceLookup struct {
	workspace workspacecore.Workspace
}

func (l daemonWorkspaceLookup) Inspect(context.Context, string) (workspacecore.Workspace, error) {
	return l.workspace, nil
}

func daemonTestWorkspace(root string) workspacecore.Workspace {
	now := time.Now().UTC()
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PM"),
		RepositoryID:  workspacecore.RepositoryID("repo_01KZZ8AJJYRPX53ZX04P2NB9PM"),
		Label:         "daemon-test",
		Root:          root,
		GitDir:        filepath.Join(root, ".git"),
		CreatedAt:     now,
		LastSeenAt:    now,
	}
}

type countingCodeProviderFactory struct {
	mu         sync.Mutex
	starts     int
	providers  []*countingCodeProvider
	resolveErr error
}

func (f *countingCodeProviderFactory) Resolve(context.Context, workspacecore.Workspace, core.Query) (appcodeintel.ProviderStartOptions, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return appcodeintel.ProviderStartOptions{}, f.resolveErr
	}
	return appcodeintel.ProviderStartOptions{
		ProviderID:         core.ProviderGoSemantic,
		ExecutableIdentity: "fake_gopls_exec",
		ConfigFingerprint:  "cfg_daemon_test",
		BuildFingerprint:   "build_daemon_test",
	}, nil
}

func (f *countingCodeProviderFactory) Start(context.Context, workspacecore.Workspace, appcodeintel.ProviderStartOptions) (appcodeintel.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	provider := &countingCodeProvider{}
	f.starts++
	f.providers = append(f.providers, provider)
	return provider, nil
}

func (f *countingCodeProviderFactory) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts
}

func (f *countingCodeProviderFactory) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, provider := range f.providers {
		total += provider.closeCount()
	}
	return total
}

type countingCodeProvider struct {
	mu     sync.Mutex
	closes int
}

func (*countingCodeProvider) Metadata() core.ProviderMetadata {
	return core.ProviderMetadata{
		ProviderID:           core.ProviderGoSemantic,
		Incarnation:          "fake_daemon_provider",
		ExecutableVersion:    "v0.test",
		ConfigFingerprint:    "cfg_daemon_test",
		BuildFingerprint:     "build_daemon_test",
		BuildQuality:         "test",
		Coverage:             core.SyncExactForKnownPaths,
		SemanticScopeQuality: "workspace_root",
	}
}

func (p *countingCodeProvider) Query(context.Context, appcodeintel.ProviderRequest) (appcodeintel.ProviderResponse, error) {
	return appcodeintel.ProviderResponse{Status: core.StatusReady}, nil
}

func (p *countingCodeProvider) Close() error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()
	return nil
}

func (p *countingCodeProvider) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}

func TestDaemonOrdinaryPathsDoNotStartCodeProvider(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	store := openA1Store(t, stateDir)
	workspaceService := workspaceapp.New(store, gitadapter.New())
	workspaceRecord, err := workspaceService.Attach(context.Background(), initWorkspaceCLIRepo(t), "codeintel-no-tax")
	if err != nil {
		t.Fatal(err)
	}
	seed := activitycore.New(activitycore.ID("activity-codeintel-no-tax"), time.Now().UTC())
	if err := store.SaveActivity(context.Background(), seed); err != nil {
		t.Fatal(err)
	}
	provider := &countingCodeProviderFactory{}
	client, cancel, done := runCodeIntelDaemon(t, stateDir, runtimeDir, provider)

	assertProviderStarts := func(want int) {
		t.Helper()
		if got := provider.startCount(); got != want {
			t.Fatalf("provider starts=%d want=%d", got, want)
		}
	}
	server, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "code-server", Action: "inspect.server",
	})
	if err != nil || !server.OK || server.Server == nil || server.Server.Features[capability.FeatureCodeIntelligence] != capability.Available {
		t.Fatalf("inspect.server response=%#v err=%v", server, err)
	}
	assertProviderStarts(0)
	workspaceResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "code-workspace", Action: "inspect.workspace", WorkspaceID: string(workspaceRecord.ID),
	})
	if err != nil || !workspaceResponse.OK {
		t.Fatalf("inspect.workspace response=%#v err=%v", workspaceResponse, err)
	}
	assertProviderStarts(0)
	activityResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "code-activity", Action: "inspect.activity", ActivityID: string(seed.ID),
	})
	if err != nil || !activityResponse.OK {
		t.Fatalf("inspect.activity response=%#v err=%v", activityResponse, err)
	}
	assertProviderStarts(0)
	if _, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "code-project", Action: "inspect.project", WorkspaceID: string(workspaceRecord.ID),
	}); err != nil {
		t.Fatal(err)
	}
	assertProviderStarts(0)
	result := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "codeintel-no-tax-shell", WorkspaceID: string(workspaceRecord.ID), CWD: ".", Command: "pwd",
	})
	assertA1ChildSuccess(t, result)
	assertProviderStarts(0)

	query := core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "README"}
	for i := range 2 {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "code-inspect-" + string(rune('1'+i)), Action: "inspect.code",
			WorkspaceID: string(workspaceRecord.ID), CodeQuery: &query,
		})
		if err != nil || !response.OK || response.Code == nil {
			t.Fatalf("inspect.code response=%#v err=%v", response, err)
		}
		assertProviderStarts(1)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if provider.closeCount() != 1 {
		t.Fatalf("daemon shutdown provider closes=%d", provider.closeCount())
	}
}

func runCodeIntelDaemon(t *testing.T, stateDir, runtimeDir string, provider *countingCodeProviderFactory) (*ipcadapter.Client, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}
	go func() { done <- runDaemonWithCodeProvider(ctx, args, provider, provider) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))
	return ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock")), cancel, done
}

func TestDaemonCodeProviderUnavailableDoesNotBreakOrdinaryPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := daemonTestWorkspace(root)
	provider := &countingCodeProviderFactory{resolveErr: errors.New("gopls unavailable")}
	runtime, err := newCodeIntelligenceRuntimeWithProvider(daemonWorkspaceLookup{workspace: workspace}, nil, nil, nil, provider, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.Service.Inspect(t.Context(), appcodeintel.InspectRequest{
		WorkspaceID: string(workspace.ID),
		Query:       core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeFile, Path: "main.go"},
	})
	if appcodeintel.ErrorCode(err) != appcodeintel.CodeProviderUnavailable {
		t.Fatalf("provider unavailable err=%v code=%q", err, appcodeintel.ErrorCode(err))
	}
	if provider.startCount() != 0 {
		t.Fatalf("unavailable provider unexpectedly started=%d", provider.startCount())
	}
	if daemonCatalog(capability.Limits{}).Features[capability.FeatureCodeIntelligence] != capability.Available {
		t.Fatal("ordinary capability path became unhealthy after provider failure")
	}
}

func TestDaemonCatalogAdvertisesBoundedProjectReadiness(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if catalog.Features[capability.FeatureProjectReadiness] != capability.Available {
		t.Fatalf("readiness feature=%q", catalog.Features[capability.FeatureProjectReadiness])
	}
	if len(catalog.ReadinessSchemaVersions) != 1 || catalog.ReadinessSchemaVersions[0] != 1 {
		t.Fatalf("readiness schema versions=%v", catalog.ReadinessSchemaVersions)
	}
	if catalog.Limits.ReadinessCacheTTLMS != 30000 || catalog.Limits.ReadinessCacheEntries != 256 {
		t.Fatalf("readiness limits=%#v", catalog.Limits)
	}
}

func TestDaemonCatalogAdvertisesA25ObservationWithExactBounds(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if catalog.Features[capability.FeatureEnvironmentFingerprint] != capability.Available || catalog.Features[capability.FeatureProcessInspection] != capability.Available {
		t.Fatalf("A2.5 features env=%q process=%q", catalog.Features[capability.FeatureEnvironmentFingerprint], catalog.Features[capability.FeatureProcessInspection])
	}
	if len(catalog.EnvironmentSnapshotSchemaVersions) != 1 || catalog.EnvironmentSnapshotSchemaVersions[0] != environmentcore.SnapshotSchemaVersion ||
		len(catalog.EnvironmentFingerprintVersions) != 1 || catalog.EnvironmentFingerprintVersions[0] != environmentcore.FingerprintVersion ||
		len(catalog.ToolchainFingerprintVersions) != 1 || catalog.ToolchainFingerprintVersions[0] != environmentcore.ToolchainFingerprintVersion {
		t.Fatalf("environment versions=%#v", catalog)
	}
	wantProbes := []string{"go", "node", "python", "java", "rust"}
	if len(catalog.EnvironmentToolchainProbeIDs) != len(wantProbes) {
		t.Fatalf("toolchain probes=%v", catalog.EnvironmentToolchainProbeIDs)
	}
	for i := range wantProbes {
		if catalog.EnvironmentToolchainProbeIDs[i] != wantProbes[i] {
			t.Fatalf("toolchain probes=%v", catalog.EnvironmentToolchainProbeIDs)
		}
	}
	limits := catalog.Limits
	if limits.EnvironmentRelevantVariables != environmentcore.MaxRelevantVariables || limits.EnvironmentToolchainProbes != 5 || limits.EnvironmentToolchainObservations != environmentcore.MaxToolchainObservations ||
		limits.EnvironmentProbeTimeoutMS != environmentadapter.ProbeTimeout.Milliseconds() || limits.EnvironmentProbeOutputBytes != environmentadapter.MaxProbeOutputBytes || limits.EnvironmentCacheEntries != environmentapp.DefaultMaxCacheEntries {
		t.Fatalf("environment limits=%#v", limits)
	}
	if len(catalog.ProcessObservationSchemaVersions) != 1 || catalog.ProcessObservationSchemaVersions[0] != processcore.SchemaVersion || !catalog.PortObservationSupported {
		t.Fatalf("process capability=%#v", catalog)
	}
	if limits.ProcessDescendants != processcore.MaxDescendants || limits.ProcessTraversalDepth != processcore.MaxTraversalDepth || limits.ProcessObservationBytes != processcore.MaxObservationBytes ||
		limits.ProcessObservationMS != processcore.MaxObservationDuration.Milliseconds() || limits.ProcessPortRecords != processcore.MaxPortRecords {
		t.Fatalf("process limits=%#v", limits)
	}
}

func TestDaemonA25ObservationActionsAreWired(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	envResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "a25-env", Action: "inspect.environment",
		Freshness: environmentcore.FreshnessRefresh,
	})
	if err != nil || !envResponse.OK || envResponse.Environment == nil {
		t.Fatalf("inspect.environment response=%#v err=%v", envResponse, err)
	}
	if envResponse.Environment.SchemaVersion != environmentcore.SnapshotSchemaVersion || envResponse.Environment.Quality == environmentcore.QualityUnavailable || envResponse.Environment.EnvironmentFingerprint == "" {
		t.Fatalf("environment snapshot=%#v", envResponse.Environment)
	}

	pid := os.Getpid()
	procResponse, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "a25-proc", Action: "inspect.process",
		ProcessTarget: &processcore.Target{Kind: processcore.TargetPID, PID: pid},
	})
	if err != nil || !procResponse.OK || procResponse.Process == nil {
		t.Fatalf("inspect.process response=%#v err=%v", procResponse, err)
	}
	if procResponse.Process.SchemaVersion != processcore.SchemaVersion || procResponse.Process.Root == nil || procResponse.Process.Root.PID != pid {
		t.Fatalf("process observation=%#v", procResponse.Process)
	}
}
