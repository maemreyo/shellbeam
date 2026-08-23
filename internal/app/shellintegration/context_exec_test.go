package shellintegration

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextArmProbe struct {
	mu          sync.Mutex
	observation ShellIdentityObservation
	requests    []ProbeRequest
}

func (p *contextArmProbe) Probe(_ context.Context, req ProbeRequest) (ShellIdentityObservation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, req)
	return p.observation, nil
}

func (p *contextArmProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *contextArmProbe) lastRequest() ProbeRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[len(p.requests)-1]
}

type contextArmAdapterFake struct {
	family core.ShellFamily
	mu     sync.Mutex
	calls  []ContextHelperArmSpec
}

func (a *contextArmAdapterFake) Family() core.ShellFamily { return a.family }
func (*contextArmAdapterFake) Install(context.Context, WatchRequest) (RequirementWatcher, error) {
	return nil, errors.New("not used")
}
func (a *contextArmAdapterFake) ArmContextHelper(_ context.Context, arm ContextHelperArmSpec) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, arm)
	return nil
}
func (a *contextArmAdapterFake) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.calls)
}
func (a *contextArmAdapterFake) lastArm() ContextHelperArmSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[len(a.calls)-1]
}

func TestContextHelperArmReprobesExactShellBeforeInstallingOneShotHook(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5a_fish"}
	probe := &contextArmProbe{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
	adapter := &contextArmAdapterFake{family: core.ShellFish}
	svc, err := NewService(probe, adapter)
	if err != nil {
		t.Fatal(err)
	}
	req := validContextHelperArmRequest(shell)

	arm, err := svc.ArmContextHelper(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if probe.callCount() != 1 {
		t.Fatalf("probe calls=%d", probe.callCount())
	}
	probeReq := probe.lastRequest()
	if probeReq.Expected == nil || *probeReq.Expected != shell || probeReq.Facts != req.Facts {
		t.Fatalf("fresh probe did not bind exact facts: %#v", probeReq)
	}
	if adapter.callCount() != 1 {
		t.Fatalf("arm calls=%d", adapter.callCount())
	}
	got := adapter.lastArm()
	if got.Shell != shell || got.OpaqueLaunchID != req.OpaqueLaunchID {
		t.Fatalf("adapter arm=%#v", got)
	}
	if arm.ContextExecID != req.ContextExecID || arm.SessionID != req.SessionID ||
		arm.AuthorityEpoch != req.Authority.Epoch || arm.ProviderGeneration != req.Facts.ProviderGeneration ||
		arm.Shell != shell || arm.PaneShellPID != req.Facts.PanePID || arm.PaneTTY != req.Facts.PaneTTY ||
		arm.OpaqueLaunchID != req.OpaqueLaunchID || arm.ArmedAt.IsZero() {
		t.Fatalf("arm=%#v", arm)
	}
}

func TestContextHelperArmFailsBeforeMutationWithoutCurrentAgentOrH5Facts(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 1, 0, 0, time.UTC)
	shell := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5a_fish"}
	tests := []struct {
		name string
		edit func(*ContextHelperArmRequest)
		code failure.Code
	}{
		{
			name: "human owner",
			edit: func(v *ContextHelperArmRequest) { v.Authority.Owner = delegated.OwnerHuman },
			code: failure.ContextExecNotAgentOwned,
		},
		{
			name: "fenced authority",
			edit: func(v *ContextHelperArmRequest) { v.Authority.Fenced = true },
			code: failure.ContextExecNotAgentOwned,
		},
		{
			name: "session mismatch",
			edit: func(v *ContextHelperArmRequest) { v.SessionID = "session_other" },
			code: failure.ContextExecUnavailable,
		},
		{
			name: "pane tty missing",
			edit: func(v *ContextHelperArmRequest) { v.Facts.PaneTTY = "" },
			code: failure.ContextExecBoundaryUnproven,
		},
		{
			name: "cwd missing",
			edit: func(v *ContextHelperArmRequest) { v.Facts.CWD = "" },
			code: failure.ContextExecBoundaryUnproven,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			probe := &contextArmProbe{observation: ShellIdentityObservation{Identity: shell, State: IdentityExact, ObservedAt: now}}
			adapter := &contextArmAdapterFake{family: core.ShellFish}
			svc, err := NewService(probe, adapter)
			if err != nil {
				t.Fatal(err)
			}
			req := validContextHelperArmRequest(shell)
			tc.edit(&req)

			_, err = svc.ArmContextHelper(context.Background(), req)
			var typed *failure.Failure
			if !errors.As(err, &typed) || typed.Code != tc.code {
				t.Fatalf("err=%#v want=%s", err, tc.code)
			}
			if probe.callCount() != 0 || adapter.callCount() != 0 {
				t.Fatalf("precondition failure mutated: probes=%d arms=%d", probe.callCount(), adapter.callCount())
			}
		})
	}
}

func TestContextHelperArmRejectsNestedShellDriftAfterFreshFacts(t *testing.T) {
	now := time.Date(2026, 8, 21, 11, 2, 0, 0, time.UTC)
	expected := core.ShellIdentity{Family: core.ShellFish, RuntimeID: "runtime_task5a_fish"}
	probe := &contextArmProbe{observation: ShellIdentityObservation{
		Identity: core.ShellIdentity{Family: core.ShellUnknown, RuntimeID: "runtime_task5a_zsh"},
		State:    IdentityChanged, ObservedAt: now,
	}}
	adapter := &contextArmAdapterFake{family: core.ShellFish}
	svc, err := NewService(probe, adapter)
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.ArmContextHelper(context.Background(), validContextHelperArmRequest(expected))
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Code != failure.ContextExecBoundaryUnproven {
		t.Fatalf("err=%#v", err)
	}
	if probe.callCount() != 1 || adapter.callCount() != 0 {
		t.Fatalf("drift handling probes=%d arms=%d", probe.callCount(), adapter.callCount())
	}
}

func TestContextHelperArmRequestHasNoStaleBoundaryOrHandoffSurface(t *testing.T) {
	typ := reflect.TypeOf(ContextHelperArmRequest{})
	for _, forbidden := range []string{"HandoffID", "Boundary", "Argv", "Command", "Env", "CWD"} {
		if _, ok := typ.FieldByName(forbidden); ok {
			t.Fatalf("arm request exposes forbidden field %s", forbidden)
		}
	}
}

func validContextHelperArmRequest(shell core.ShellIdentity) ContextHelperArmRequest {
	return ContextHelperArmRequest{
		ContextExecID: "ctxexec_task5a_01",
		SessionID:     "session_task5a_01",
		Authority: delegated.EffectiveAuthority{
			Epoch: 4, Owner: delegated.OwnerAgent, Fenced: false,
		},
		Facts: ProviderProcessFacts{
			SessionID: "session_task5a_01", ProviderID: "tmux_control_mode", ProviderVersion: 1,
			ProviderGeneration: "gen_task5a_01", PanePID: 4242, CurrentCommand: string(shell.Family),
			PaneTTY: "/dev/ttys042", CWD: "/tmp/task5a",
		},
		ExpectedShell:  shell,
		OpaqueLaunchID: "launch_task5a_01",
	}
}
