package shellintegration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type probeFake struct {
	observation ShellIdentityObservation
	requests    []ProbeRequest
}

func (p *probeFake) Probe(_ context.Context, req ProbeRequest) (ShellIdentityObservation, error) {
	p.requests = append(p.requests, req)
	return p.observation, nil
}

type adapterFake struct {
	family   core.ShellFamily
	watcher  *watcherFake
	installs atomic.Int32
}

func (a *adapterFake) Family() core.ShellFamily { return a.family }
func (a *adapterFake) Install(_ context.Context, _ WatchRequest) (RequirementWatcher, error) {
	a.installs.Add(1)
	if a.watcher == nil {
		return nil, errors.New("watcher missing")
	}
	return a.watcher, nil
}

type watcherFake struct {
	mu     sync.Mutex
	event  WatchEvent
	wait   chan struct{}
	closed int
}

func (w *watcherFake) Wait(ctx context.Context) (WatchEvent, error) {
	if w.wait != nil {
		select {
		case <-w.wait:
		case <-ctx.Done():
			return WatchEvent{}, ctx.Err()
		}
	}
	return w.event, nil
}
func (w *watcherFake) Close() error    { w.mu.Lock(); defer w.mu.Unlock(); w.closed++; return nil }
func (w *watcherFake) closeCount() int { w.mu.Lock(); defer w.mu.Unlock(); return w.closed }

func TestLifecycleSelectsExactAdapterAndRemovesWatcherAfterSatisfied(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 42, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime-fish"}
	req := core.Requirement{Kind: core.RequirementEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}
	watcher := &watcherFake{event: WatchEvent{
		Result:   core.RequirementResult{Requirement: req, State: core.RequirementSatisfied, Quality: core.RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now},
		Boundary: core.BoundaryProof{HandoffID: "handoff-1", AuthorityEpoch: 4, Shell: shell, Quality: core.BoundaryQualityShellPrompt, ObservedAt: now},
	}}
	fish := &adapterFake{family: core.ShellFish, watcher: watcher}
	zsh := &adapterFake{family: core.ShellZsh, watcher: &watcherFake{}}
	probe := &probeFake{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
	svc, err := NewService(probe, fish, zsh)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Observe(context.Background(), ObserveRequest{
		HandoffID: "handoff-1", AuthorityEpoch: delegated.AuthorityEpoch(4),
		Facts:       ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "fish"},
		Requirement: req,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fish.installs.Load() != 1 || zsh.installs.Load() != 0 || watcher.closeCount() != 1 {
		t.Fatalf("fish=%d zsh=%d closed=%d", fish.installs.Load(), zsh.installs.Load(), watcher.closeCount())
	}
	if out.Result.State != core.RequirementSatisfied || out.Boundary == nil || !out.Boundary.CurrentFor("handoff-1", 4, shell) {
		t.Fatalf("out=%#v", out)
	}
}

func TestUnknownOrChangedShellDegradesManualWithoutBashFallback(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 43, 0, 0, time.UTC)
	req := core.Requirement{Kind: core.RequirementEnvironmentExportedNonempty, Name: "TOKEN"}
	bash := &adapterFake{family: core.ShellBash, watcher: &watcherFake{}}
	for _, state := range []IdentityState{IdentityUnknown, IdentityChanged} {
		probe := &probeFake{observation: ShellIdentityObservation{Identity: core.ShellIdentity{Family: core.ShellUnknown, RuntimeID: "runtime-unknown"}, State: state, ObservedAt: now}}
		svc, err := NewService(probe, bash)
		if err != nil {
			t.Fatal(err)
		}
		out, err := svc.Observe(context.Background(), ObserveRequest{HandoffID: "handoff-manual", AuthorityEpoch: 2, Facts: ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "nu"}, Requirement: req})
		if err != nil {
			t.Fatal(err)
		}
		if bash.installs.Load() != 0 || out.Result.State != core.RequirementUnavailable || out.Result.Quality != core.RequirementQualityManual || out.Boundary != nil {
			t.Fatalf("state=%s installs=%d out=%#v", state, bash.installs.Load(), out)
		}
	}
}

func TestLifecycleAllowsOnlyOneWatcherPerHandoffAndClosesOnCancellation(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 44, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime-fish"}
	block := make(chan struct{})
	watcher := &watcherFake{wait: block}
	fish := &adapterFake{family: core.ShellFish, watcher: watcher}
	probe := &probeFake{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
	svc, err := NewService(probe, fish)
	if err != nil {
		t.Fatal(err)
	}
	req := ObserveRequest{HandoffID: "handoff-one", AuthorityEpoch: 3, Facts: ProviderProcessFacts{SessionID: "session-1", ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_1", PanePID: 42, CurrentCommand: "fish"}, Requirement: core.Requirement{Kind: core.RequirementEnvironmentExportedNonempty, Name: "TOKEN"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := svc.Observe(ctx, req); done <- err }()
	deadline := time.Now().Add(time.Second)
	for fish.installs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if fish.installs.Load() != 1 {
		t.Fatal("watcher not installed")
	}
	if _, err := svc.Observe(context.Background(), req); err == nil {
		t.Fatal("duplicate watcher accepted")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	if watcher.closeCount() != 1 {
		t.Fatalf("close count=%d", watcher.closeCount())
	}
}
