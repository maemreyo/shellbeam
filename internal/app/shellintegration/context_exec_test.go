package shellintegration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextLaunchProbe struct {
	mu          sync.Mutex
	observation ShellIdentityObservation
	requests    []ProbeRequest
}

func (p *contextLaunchProbe) Probe(_ context.Context, req ProbeRequest) (ShellIdentityObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	return p.observation, nil
}

func (p *contextLaunchProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *contextLaunchProbe) lastRequest() ProbeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

type contextLaunchAdapterFake struct {
	family core.ShellFamily
	mu     sync.Mutex
	calls  []ContextHelperLaunch
}

func (a *contextLaunchAdapterFake) Family() core.ShellFamily { return a.family }
func (*contextLaunchAdapterFake) Install(context.Context, WatchRequest) (RequirementWatcher, error) {
	return nil, errors.New("not used")
}
func (a *contextLaunchAdapterFake) LaunchContextHelper(_ context.Context, launch ContextHelperLaunch) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, launch)
	return nil
}
func (a *contextLaunchAdapterFake) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}
func (a *contextLaunchAdapterFake) lastLaunch() ContextHelperLaunch {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[len(a.calls)-1]
}

func TestContextHelperLaunchReprobesExactShellBeforeMutation(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5_fish"}
	probe := &contextLaunchProbe{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
	adapter := &contextLaunchAdapterFake{family: core.ShellFish}
	svc, err := NewService(probe, adapter)
	if err != nil {
		t.Fatal(err)
	}
	req := validContextHelperLaunchRequest(now, shell)

	if err := svc.LaunchContextHelper(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if probe.callCount() != 1 {
		t.Fatalf("probe calls=%d", probe.callCount())
	}
	probeReq := probe.lastRequest()
	if probeReq.Expected == nil || *probeReq.Expected != shell {
		t.Fatalf("fresh probe did not bind expected shell: %#v", probeReq)
	}
	if adapter.callCount() != 1 {
		t.Fatalf("launch calls=%d", adapter.callCount())
	}
	got := adapter.lastLaunch()
	if got.Shell != shell || got.OpaqueLaunchID != req.OpaqueLaunchID {
		t.Fatalf("launch=%#v", got)
	}
}

func TestContextHelperLaunchFailsBeforeMutationWithoutCurrentAgentPromptAuthority(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 1, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5_fish"}
	tests := []struct {
		name string
		edit func(*ContextHelperLaunchRequest)
		code failure.Code
	}{
		{
			name: "human owner",
			edit: func(v *ContextHelperLaunchRequest) { v.Authority.Owner = delegated.OwnerHuman },
			code: failure.ContextExecNotAgentOwned,
		},
		{
			name: "fenced authority",
			edit: func(v *ContextHelperLaunchRequest) { v.Authority.Fenced = true },
			code: failure.ContextExecNotAgentOwned,
		},
		{
			name: "stale epoch",
			edit: func(v *ContextHelperLaunchRequest) { v.Authority.Epoch++ },
			code: failure.ContextExecStaleGeneration,
		},
		{
			name: "non prompt boundary",
			edit: func(v *ContextHelperLaunchRequest) { v.Boundary.Quality = core.BoundaryQualityProcessBoundary },
			code: failure.ContextExecBoundaryUnproven,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probe := &contextLaunchProbe{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
			adapter := &contextLaunchAdapterFake{family: core.ShellFish}
			svc, err := NewService(probe, adapter)
			if err != nil {
				t.Fatal(err)
			}
			req := validContextHelperLaunchRequest(now, shell)
			tc.edit(&req)

			err = svc.LaunchContextHelper(context.Background(), req)
			var typed *failure.Failure
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("err=%#v want=%s", err, tc.code)
			}
			if probe.callCount() != 0 || adapter.callCount() != 0 {
				t.Fatalf("precondition failure mutated: probes=%d launches=%d", probe.callCount(), adapter.callCount())
			}
		})
	}
}

func TestContextHelperLaunchRejectsNestedShellDriftAfterBoundaryProof(t *testing.T) {
	now := time.Date(2026, 8, 21, 7, 2, 0, 0, time.UTC)
	expected := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5_fish"}
	probe := &contextLaunchProbe{observation: ShellIdentityObservation{
		Identity: core.ShellIdentity{Family: core.ShellUnknown, RuntimeID: "runtime_task5_zsh"},
		State:    IdentityChanged, ObservedAt: now,
	}}
	adapter := &contextLaunchAdapterFake{family: core.ShellFish}
	svc, err := NewService(probe, adapter)
	if err != nil {
		t.Fatal(err)
	}

	err = svc.LaunchContextHelper(context.Background(), validContextHelperLaunchRequest(now, expected))
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Code != failure.ContextExecBoundaryUnproven {
		t.Fatalf("err=%#v", err)
	}
	if probe.callCount() != 1 || adapter.callCount() != 0 {
		t.Fatalf("drift handling probes=%d launches=%d", probe.callCount(), adapter.callCount())
	}
}

func validContextHelperLaunchRequest(now time.Time, shell core.ShellIdentity) ContextHelperLaunchRequest {
	return ContextHelperLaunchRequest{
		ContextExecID: "ctxexec_task5_01",
		HandoffID:     "handoff_task5_01",
		Authority: delegated.EffectiveAuthority{
			Epoch: 4, Owner: delegated.OwnerAgent, Fenced: false,
		},
		Facts: ProviderProcessFacts{
			SessionID: "session_task5_01", ProviderID: "tmux_control_mode", ProviderVersion: 1,
			ProviderGeneration: "gen_task5_01", PanePID: 4242, CurrentCommand: string(shell.Family),
		},
		ExpectedShell: shell,
		Boundary: core.BoundaryProof{
			HandoffID: "handoff_task5_01", AuthorityEpoch: 4, Shell: shell,
			Quality: core.BoundaryQualityShellPrompt, ObservedAt: now,
		},
		OpaqueLaunchID: "launch_task5_01",
	}
}
