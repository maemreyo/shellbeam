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
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

type countingDelegatedProvider struct {
	probeErr   error
	probeCalls int
}

func (p *countingDelegatedProvider) Identity() delegated.ProviderIdentity {
	return delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}
}
func (p *countingDelegatedProvider) ProviderRefForSession(string, time.Time) (delegated.ProviderRef, error) {
	return delegated.ProviderRef{}, nil
}
func (p *countingDelegatedProvider) Probe(context.Context) error { p.probeCalls++; return p.probeErr }
func (p *countingDelegatedProvider) Create(context.Context, delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	return delegatedapp.CreateResult{}, nil
}
func (p *countingDelegatedProvider) Reattach(context.Context, delegated.ProviderRef, delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, nil
}
func (p *countingDelegatedProvider) Write(context.Context, delegated.ProviderRef, []byte) error {
	return nil
}
func (p *countingDelegatedProvider) Signal(context.Context, delegated.ProviderRef, string) error {
	return nil
}
func (p *countingDelegatedProvider) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, nil
}
func (p *countingDelegatedProvider) Wait(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, nil
}
func (p *countingDelegatedProvider) Close(context.Context, delegated.ProviderRef) error { return nil }

func TestDelegatedCompositionIsDarwinOnlyAndDoesNotTouchProviderElsewhere(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	calls := 0
	factory := func(string, string) (daemonapp.DelegatedRuntime, error) {
		calls++
		return &countingDelegatedProvider{}, nil
	}
	runtime, got := composeDelegatedInteractive(t.Context(), "linux", "/state", "/run", base, factory)
	if runtime != nil || calls != 0 || got.Features[capability.FeatureDelegatedInteractive] != capability.Unavailable || got.DelegatedInteractive != nil {
		t.Fatalf("unqualified platform runtime=%T calls=%d capability=%#v", runtime, calls, got.DelegatedInteractive)
	}
}

func TestDelegatedCompositionFailureKeepsOrdinaryDaemonAvailableWithoutCapability(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	for _, tc := range []struct {
		name    string
		factory delegatedProviderFactory
	}{
		{"factory", func(string, string) (daemonapp.DelegatedRuntime, error) { return nil, errors.New("tmux missing") }},
		{"probe", func(string, string) (daemonapp.DelegatedRuntime, error) {
			return &countingDelegatedProvider{probeErr: errors.New("identity mismatch")}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, got := composeDelegatedInteractive(t.Context(), "darwin", "/state", "/run", base, tc.factory)
			if runtime != nil || got.Features[capability.FeatureDelegatedInteractive] != capability.Unavailable || got.DelegatedInteractive != nil {
				t.Fatalf("failure promoted delegated runtime=%T capability=%#v", runtime, got.DelegatedInteractive)
			}
		})
	}
}

func TestDelegatedCompositionFreezesQualifiedCapabilityWithoutInspectServerReprobe(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	provider := &countingDelegatedProvider{}
	factory := func(stateDir, runtimeDir string) (daemonapp.DelegatedRuntime, error) {
		if stateDir != "/state" || runtimeDir != "/run" {
			t.Fatalf("factory paths state=%q runtime=%q", stateDir, runtimeDir)
		}
		return provider, nil
	}
	runtime, got := composeDelegatedInteractive(t.Context(), "darwin", "/state", "/run", base, factory)
	if runtime != provider || provider.probeCalls != 1 {
		t.Fatalf("runtime=%T probes=%d", runtime, provider.probeCalls)
	}
	support := got.DelegatedInteractive
	if got.Features[capability.FeatureDelegatedInteractive] != capability.Available || support == nil {
		t.Fatalf("delegated capability=%#v", got)
	}
	if support.ProviderID != "tmux_control_mode" || support.ProviderVersion != 1 || support.Platform != "darwin" || support.MaxMutationRecords != storeadapter.DefaultMaxDelegatedMutationRecords || support.DaemonRestartContinuity || support.HostRebootContinuity {
		t.Fatalf("support=%#v", support)
	}
	if !containsCapabilityVersion(got.ReceiptSchemaVersions, 5) {
		t.Fatalf("receipt versions=%v", got.ReceiptSchemaVersions)
	}
	svc := daemonapp.NewService(nil, nil, daemonapp.Options{Capabilities: got, DelegatedRuntime: runtime})
	for i := 0; i < 2; i++ {
		info, err := svc.InspectServer(t.Context())
		if err != nil || info.Capabilities.DelegatedInteractive == nil {
			t.Fatalf("inspect server info=%#v err=%v", info, err)
		}
	}
	if provider.probeCalls != 1 {
		t.Fatalf("inspect.server reprobed provider: %d", provider.probeCalls)
	}
}

func containsCapabilityVersion(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestDelegatedProductionCompositionUsesQualifiedDarwinProviderWhenOptedIn(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("delegated H1 production capability is Darwin-only")
	}
	tmuxPath := os.Getenv("SHELLBEAM_H0_TMUX")
	if tmuxPath == "" {
		t.Skip("set SHELLBEAM_H0_TMUX to run native delegated composition qualification")
	}
	if !filepath.IsAbs(tmuxPath) {
		t.Fatalf("SHELLBEAM_H0_TMUX must be absolute: %q", tmuxPath)
	}
	stateDir := t.TempDir()
	runtimeDir, err := os.MkdirTemp("/tmp", "shellbeam-delegated-compose-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", filepath.Dir(tmuxPath)+string(os.PathListSeparator)+oldPath)
	gotRuntime, catalog := composeDelegatedInteractiveRuntime(t.Context(), stateDir, runtimeDir, capability.Baseline(capability.Limits{}))
	if gotRuntime == nil || catalog.DelegatedInteractive == nil || catalog.Features[capability.FeatureDelegatedInteractive] != capability.Available {
		t.Fatalf("production delegated composition unavailable: runtime=%T catalog=%#v", gotRuntime, catalog.DelegatedInteractive)
	}
	if catalog.DelegatedInteractive.ProviderID != "tmux_control_mode" || catalog.DelegatedInteractive.Platform != "darwin" || catalog.DelegatedInteractive.DaemonRestartContinuity || catalog.DelegatedInteractive.HostRebootContinuity {
		t.Fatalf("production support=%#v", catalog.DelegatedInteractive)
	}
}
