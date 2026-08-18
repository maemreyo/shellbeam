package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type composedHermeticRuntimeFake struct{}

func (*composedHermeticRuntimeFake) Prepare(context.Context, daemonapp.HermeticPrepareRequest) (hermeticapp.PreparedExecution, error) {
	return hermeticapp.PreparedExecution{}, errors.New("not used")
}
func (*composedHermeticRuntimeFake) Start(context.Context, hermeticapp.PreparedExecution, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	return nil, receipt.SpawnEvidence{}, errors.New("not used")
}
func (*composedHermeticRuntimeFake) Discard(context.Context, hermeticapp.PreparedExecution) error {
	return nil
}

type composedHermeticWorkspaceFake struct{}

func (composedHermeticWorkspaceFake) ResolveFresh(context.Context, string) (hermeticapp.WorkspaceContext, error) {
	return hermeticapp.WorkspaceContext{}, errors.New("not used")
}

type composedHermeticOwnerFake struct{}

func (composedHermeticOwnerFake) Start(context.Context, operation.ExecutionSpec, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	return nil, receipt.SpawnEvidence{}, errors.New("not used")
}
func (composedHermeticOwnerFake) StartPrivateHermetic(context.Context, hermeticapp.ProviderCommand, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error) {
	return nil, receipt.SpawnEvidence{}, nil, errors.New("not used")
}

func TestHermeticCompositionDisabledDoesZeroProviderWork(t *testing.T) {
	calls := 0
	base := capability.Baseline(capability.Limits{})
	runtime, got := composeHermeticBoundary(context.Background(), false, "/state", "/runtime", nil, composedHermeticOwnerFake{}, base, func(context.Context, string, string, hermeticapp.WorkspaceSource, hermeticPrivateStarter) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error) {
		calls++
		return &composedHermeticRuntimeFake{}, qualifiedHermeticSupport(), nil
	})
	if calls != 0 || runtime != nil {
		t.Fatalf("disabled composition did provider work calls=%d runtime=%T", calls, runtime)
	}
	if got.Features[capability.FeatureHermeticBoundaryV1] == capability.Available || got.HermeticBoundary != nil {
		t.Fatalf("disabled hermetic capability advertised: %#v", got.HermeticBoundary)
	}
}

func TestHermeticCompositionAdvertisesOnlyQualifiedRuntimeItWillUse(t *testing.T) {
	wantRuntime := &composedHermeticRuntimeFake{}
	calls := 0
	base := capability.Baseline(capability.Limits{})
	runtime, got := composeHermeticBoundary(context.Background(), true, "/state", "/runtime", composedHermeticWorkspaceFake{}, composedHermeticOwnerFake{}, base, func(_ context.Context, stateDir, runtimeDir string, _ hermeticapp.WorkspaceSource, _ hermeticPrivateStarter) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error) {
		calls++
		if stateDir != "/state" || runtimeDir != "/runtime" {
			t.Fatalf("paths state=%q runtime=%q", stateDir, runtimeDir)
		}
		return wantRuntime, qualifiedHermeticSupport(), nil
	})
	if calls != 1 || runtime != wantRuntime {
		t.Fatalf("calls=%d runtime=%T", calls, runtime)
	}
	if got.Features[capability.FeatureHermeticBoundaryV1] != capability.Available || got.HermeticBoundary == nil || !got.HermeticBoundary.ValidV1() {
		t.Fatalf("qualified hermetic capability not advertised: %#v", got.HermeticBoundary)
	}
}

func TestHermeticCompositionFailureOrOwnerWithoutPrivateStarterLeavesCapabilityUnavailable(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	for _, tc := range []struct {
		name    string
		owner   daemonapp.ProcessOwner
		factory hermeticRuntimeFactory
	}{
		{name: "factory failure", owner: composedHermeticOwnerFake{}, factory: func(context.Context, string, string, hermeticapp.WorkspaceSource, hermeticPrivateStarter) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error) {
			return nil, nil, errors.New("qualification failed")
		}},
		{name: "ordinary owner only", owner: &resourceCompositionOwner{}, factory: func(context.Context, string, string, hermeticapp.WorkspaceSource, hermeticPrivateStarter) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error) {
			t.Fatal("factory called without private starter")
			return nil, nil, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, got := composeHermeticBoundary(context.Background(), true, "/state", "/runtime", composedHermeticWorkspaceFake{}, tc.owner, base, tc.factory)
			if runtime != nil || got.Features[capability.FeatureHermeticBoundaryV1] == capability.Available || got.HermeticBoundary != nil {
				t.Fatalf("unsafe composition promoted runtime=%T support=%#v", runtime, got.HermeticBoundary)
			}
		})
	}
}

func TestHermeticProviderManifestIsStrictBoundedAndPrivate(t *testing.T) {
	state := t.TempDir()
	dir := filepath.Join(state, "hermetic-v1")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "provider.json")
	manifest := `{"schema_version":1,"bubblewrap_path":"/usr/bin/bwrap","toolchain_root":"/private/toolchain","provider":{"provider":"bubblewrap","version":"0.11.2","binary_sha256":"` + strings.Repeat("a", 64) + `","runtime_manifest_sha256":"` + strings.Repeat("b", 64) + `"},"toolchain":{"id":"go-1.26.6-linux-amd64","manifest_sha256":"` + strings.Repeat("c", 64) + `"}}`
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadHermeticProviderManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	if got.BubblewrapPath != "/usr/bin/bwrap" || got.ToolchainRoot != "/private/toolchain" || got.Provider.Provider != hermeticcore.ProviderBubblewrap {
		t.Fatalf("manifest=%#v", got)
	}

	if err := os.WriteFile(path, []byte(strings.TrimSuffix(manifest, "}")+`,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHermeticProviderManifest(state); err == nil {
		t.Fatal("unknown manifest field accepted")
	}
	for name, malformed := range map[string][]byte{
		"duplicate":    []byte(strings.Replace(manifest, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1)),
		"wrong_case":   []byte(strings.Replace(manifest, `"schema_version":1`, `"SchemaVersion":1`, 1)),
		"trailing":     []byte(manifest + ` {}`),
		"invalid_utf8": append([]byte(manifest[:len(manifest)-1]), 0xff, '}'),
	} {
		if err := os.WriteFile(path, malformed, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadHermeticProviderManifest(state); err == nil {
			t.Fatalf("%s manifest ambiguity accepted", name)
		}
	}
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadHermeticProviderManifest(state); err == nil {
		t.Fatal("group/world-readable provider manifest accepted")
	}
}
