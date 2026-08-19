package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
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
	if support.ProviderID != "tmux_control_mode" || support.ProviderVersion != 1 || support.Platform != "darwin" || support.MaxMutationRecords != storeadapter.DefaultMaxDelegatedMutationRecords || !support.DaemonRestartContinuity || support.HostRebootContinuity {
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
	if catalog.DelegatedInteractive.ProviderID != "tmux_control_mode" || catalog.DelegatedInteractive.Platform != "darwin" || !catalog.DelegatedInteractive.DaemonRestartContinuity || catalog.DelegatedInteractive.HostRebootContinuity {
		t.Fatalf("production support=%#v", catalog.DelegatedInteractive)
	}
}

type delegatedStartupListStore struct {
	bindings []delegated.Binding
	calls    int
}

func (s *delegatedStartupListStore) ListDelegatedRecoveryCandidates(context.Context) ([]delegated.Binding, error) {
	s.calls++
	return append([]delegated.Binding(nil), s.bindings...), nil
}

func TestDelegatedDaemonStartupPreservesCandidatesWhenQualifiedRuntimeUnavailable(t *testing.T) {
	binding := delegated.Binding{SessionID: "delegated-skip"}
	store := &delegatedStartupListStore{bindings: []delegated.Binding{binding}}
	svc := daemonapp.NewService(nil, nil, daemonapp.Options{Capabilities: capability.Baseline(capability.Limits{})})
	if err := reconcileDelegatedDaemonStartup(t.Context(), store, svc); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || len(store.bindings) != 1 || store.bindings[0] != binding {
		t.Fatalf("calls=%d bindings=%#v", store.calls, store.bindings)
	}
}

func TestDelegatedDaemonStartupUsesQualifiedRuntimeAndBlocksMismatch(t *testing.T) {
	for _, tc := range []struct {
		name        string
		reattach    delegatedapp.Observation
		reattachErr error
		wantErr     bool
	}{
		{name: "live", reattach: delegatedapp.Observation{Provider: delegated.ProviderIdentity{ID: "tmux_control_mode", Version: 1}, ProviderCurrent: true, ProviderGeneration: "gen-cmd", Owner: delegated.OwnerAgent}},
		{name: "mismatch", reattachErr: failure.New(failure.DelegatedProviderMismatch, map[string]string{"provider_id": "tmux_control_mode"}, errors.New("generation mismatch")), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			repo, err := storeadapter.Open(root, storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.ReconcileAdmission(); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 19, 3, 0, 0, 0, time.UTC)
			reservation := operation.Reservation{SchemaVersion: 5, OperationID: operation.ID("cmd-delegated-op-" + tc.name), SessionID: operation.SessionID("cmd-delegated-session-" + tc.name), RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "cat", CWD: "/tmp", Shell: "/bin/sh", SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DaemonIncarnation: "old", CreatedAt: now}
			if _, created, got := repo.ReserveOperation(t.Context(), reservation); got.Err != nil || !created {
				t.Fatalf("reserve=%v %#v", created, got)
			}
			binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID), SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: delegated.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now}
			ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: binding.SessionID, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: "dtmux_cmd_" + tc.name, CreatedAt: now, UpdatedAt: now}
			storedBinding, created, got := repo.ReserveDelegatedBinding(t.Context(), binding, ref)
			if got.Err != nil || !created {
				t.Fatalf("binding=%v %#v", created, got)
			}
			storedBinding.Lifecycle = delegated.LifecycleLive
			storedBinding.UpdatedAt = now.Add(time.Nanosecond)
			if got := repo.AdvanceDelegatedBinding(t.Context(), storedBinding); got.Err != nil {
				t.Fatal(got.Err)
			}
			binding = storedBinding
			if got := repo.AdvanceSession(t.Context(), session.Snapshot{SchemaVersion: 1, OperationID: binding.OperationID, SessionID: binding.SessionID, DaemonIncarnation: "old", State: session.Running, OutputAvailable: true, UpdatedAt: now.Add(time.Nanosecond)}); got.Err != nil {
				t.Fatal(got.Err)
			}
			provider := &commandRestartProvider{countingDelegatedProvider: countingDelegatedProvider{}, observation: tc.reattach, reattachErr: tc.reattachErr}
			catalog := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: storeadapter.DefaultMaxDelegatedMutationRecords})
			svc := daemonapp.NewService(repo, nil, daemonapp.Options{Capabilities: catalog, DelegatedRuntime: provider, MaxQueuedInputBytes: 100})
			err = reconcileDelegatedDaemonStartup(t.Context(), repo, svc)
			if tc.wantErr {
				if !errors.Is(err, failure.DelegatedReconcileBlocked) || provider.reattachCalls != 1 {
					t.Fatalf("err=%v calls=%d", err, provider.reattachCalls)
				}
				return
			}
			if err != nil || provider.reattachCalls != 1 || provider.createCalls != 0 {
				t.Fatalf("err=%v reattach=%d create=%d", err, provider.reattachCalls, provider.createCalls)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := svc.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type commandRestartProvider struct {
	countingDelegatedProvider
	observation   delegatedapp.Observation
	reattachErr   error
	reattachCalls int
	createCalls   int
	detachCalls   int
}

func (p *commandRestartProvider) Create(ctx context.Context, req delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	p.createCalls++
	return p.countingDelegatedProvider.Create(ctx, req)
}
func (p *commandRestartProvider) Reattach(context.Context, delegated.ProviderRef, delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	p.reattachCalls++
	return p.observation, p.reattachErr
}
func (p *commandRestartProvider) Wait(ctx context.Context, _ delegated.ProviderRef) (delegatedapp.Observation, error) {
	<-ctx.Done()
	return delegatedapp.Observation{}, ctx.Err()
}
func (p *commandRestartProvider) Detach(context.Context, delegated.ProviderRef) error {
	p.detachCalls++
	return nil
}
