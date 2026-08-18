package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestE27InputTraceCompositionDisabledDoesZeroProviderWork(t *testing.T) {
	state := filepath.Join(t.TempDir(), "not-created")
	base := capability.Baseline(capability.Limits{})
	composition, err := composeInputTracing(context.Background(), false, state, nil, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.Close(context.Background())
	if composition.Preparer != nil || composition.Worker != nil || composition.Inspector != nil {
		t.Fatalf("disabled composition=%#v", composition)
	}
	if composition.Catalog.Features[capability.FeatureInputTracing] != capability.Unavailable || composition.Catalog.InputTracing != nil {
		t.Fatalf("disabled catalog=%#v", composition.Catalog.InputTracing)
	}
	if _, err := os.Lstat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled E27 touched state: %v", err)
	}
}

// The healthy path is Darwin's alone: the only tracing provider interposes
// through dyld, and every other platform answers unsupported_platform by
// design. Asserting an available capability elsewhere tests the fallback for a
// promise it never made -- which is what failed on Linux CI. The unavailable
// path has its own test below.
func TestE27InputTraceCompositionHealthyDarwinIsTruthfulAndLazy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("input tracing has no provider on %s", runtime.GOOS)
	}
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	repo, err := storeadapter.Open(state, storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 64 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	base := capability.Baseline(capability.Limits{})
	composition, err := composeInputTracing(context.Background(), true, state, repo, nil, base)
	if err != nil {
		t.Fatal(err)
	}
	defer composition.Close(context.Background())
	if composition.Preparer == nil || composition.Worker == nil || composition.Inspector == nil {
		t.Fatalf("healthy composition=%#v", composition)
	}
	if composition.Catalog.Features[capability.FeatureInputTracing] != capability.Available || composition.Catalog.InputTracing == nil {
		t.Fatalf("catalog=%#v", composition.Catalog.InputTracing)
	}
	support := composition.Catalog.InputTracing
	if support.Provider.ID != "dyld-interpose" || support.Provider.Version != 1 || support.Platform != "darwin" || support.Maturity != "experimental" || support.PreExecCoverage || support.InstrumentationEffect != trace.EffectEnvironmentAffecting || support.Authority != trace.AuthorityAdvisory {
		t.Fatalf("support=%#v", support)
	}
	for _, coverage := range []trace.Coverage{support.Coverage.FilesystemReads, support.Coverage.FilesystemMetadataQueries, support.Coverage.DirectoryEnumerations, support.Coverage.FilesystemWrites, support.Coverage.ExecutedBinaries, support.Coverage.LoadedLibraries, support.Coverage.ChildProcesses} {
		if coverage != trace.CoveragePartial {
			t.Fatalf("overclaimed coverage=%q support=%#v", coverage, support)
		}
	}
	if support.Coverage.EnvironmentNamesObserved != trace.CoverageUnsupported || support.Coverage.NetworkAttempts != trace.CoverageUnsupported {
		t.Fatalf("unsupported classes=%#v", support.Coverage)
	}
	providerRoot := filepath.Join(state, "input-trace", "dyld-v1")
	if _, err := os.Lstat(providerRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Health eagerly created provider root: %v", err)
	}
}

func TestE27InputTraceCompositionProviderFailureLeavesDaemonCapabilityUnavailable(t *testing.T) {
	state := t.TempDir()
	if err := os.Chmod(state, 0755); err != nil {
		t.Fatal(err)
	}
	composition, err := composeInputTracing(context.Background(), true, state, nil, nil, capability.Baseline(capability.Limits{}))
	if err != nil {
		t.Fatalf("optional provider failure must not fail daemon composition: %v", err)
	}
	defer composition.Close(context.Background())
	if composition.Preparer != nil || composition.Worker != nil || composition.Inspector != nil || composition.Catalog.Features[capability.FeatureInputTracing] != capability.Unavailable {
		t.Fatalf("failed provider promoted E27: %#v", composition)
	}
	if _, err := os.Lstat(filepath.Join(state, "input-trace", "dyld-v1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unhealthy provider created private root: %v", err)
	}
}

func TestE27InputTraceCompositionCloseIsBounded(t *testing.T) {
	composition := inputTraceComposition{Catalog: capability.Baseline(capability.Limits{})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := composition.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInputTraceWorkspaceResolverUsesRegisteredWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	lookup := &inputTraceWorkspaceLookupFake{record: workspacecore.Workspace{ID: workspacecore.WorkspaceID("ws_01K00000000000000000000000"), Root: root}}
	resolver := inputTraceWorkspaceResolver{workspaces: lookup}
	got, err := resolver.ResolveInputTraceWorkspace(context.Background(), string(lookup.record.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got != root || len(lookup.calls) != 1 || lookup.calls[0] != string(lookup.record.ID) {
		t.Fatalf("root=%q calls=%v", got, lookup.calls)
	}
}

type inputTraceWorkspaceLookupFake struct {
	record workspacecore.Workspace
	calls  []string
}

func (f *inputTraceWorkspaceLookupFake) Inspect(_ context.Context, id string) (workspacecore.Workspace, error) {
	f.calls = append(f.calls, id)
	return f.record, nil
}
