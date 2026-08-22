package daemon_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contextapp "github.com/maemreyo/shellbeam/internal/app/contextexec"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextExecProvider struct {
	*restartDelegatedRuntime
	currentCommand string
}

func (p *contextExecProvider) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	command := p.currentCommand
	if command == "" {
		command = "fish"
	}
	return delegatedapp.Observation{Provider: p.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_ctx", Owner: delegated.OwnerAgent, PanePID: 4242, CurrentCommand: command, PaneTTY: "/dev/ttys042", CWD: "/tmp/project"}, nil
}
func (*contextExecProvider) InspectPrivacy(context.Context, delegated.ProviderRef) (delegatedapp.PrivacyObservation, error) {
	return delegatedapp.PrivacyObservation{ProviderGeneration: "gen_ctx", ObservedAt: time.Now().UTC()}, nil
}

type contextExecShellProbe struct{}

func (contextExecShellProbe) Probe(_ context.Context, req shellapp.ProbeRequest) (shellapp.ShellIdentityObservation, error) {
	return shellapp.ShellIdentityObservation{Identity: shellcore.ShellIdentity{Family: shellcore.ShellFish, RuntimeID: "runtime_ctx"}, State: shellapp.IdentityExact, ObservedAt: time.Now().UTC()}, nil
}

type contextExecHelper struct {
	arms      int
	last      contextapp.HelperArmRequest
	callbacks contextapp.RuntimeCallbacks
}

func (h *contextExecHelper) BindContextExecCallbacks(callbacks contextapp.RuntimeCallbacks) {
	h.callbacks = callbacks
}

func (*contextExecHelper) Qualified() bool { return true }
func (h *contextExecHelper) ArmContextHelper(_ context.Context, req contextapp.HelperArmRequest) (shellapp.ContextHelperArm, error) {
	h.arms++
	h.last = req
	shell := req.Shell
	return shellapp.ContextHelperArm{ContextExecID: shell.ContextExecID, SessionID: shell.SessionID, AuthorityEpoch: shell.Authority.Epoch, ProviderGeneration: shell.Facts.ProviderGeneration, Shell: shell.ExpectedShell, PaneShellPID: shell.Facts.PanePID, PaneTTY: shell.Facts.PaneTTY, OpaqueLaunchID: shell.OpaqueLaunchID, ArmedAt: time.Now().UTC()}, nil
}

type countingContextExecShellProbe struct {
	calls     int
	failAfter int
}

func (p *countingContextExecShellProbe) Probe(_ context.Context, _ shellapp.ProbeRequest) (shellapp.ShellIdentityObservation, error) {
	p.calls++
	if p.failAfter > 0 && p.calls > p.failAfter {
		return shellapp.ShellIdentityObservation{}, errors.New("claim-time foreground command must not be shell-probed")
	}
	return shellapp.ShellIdentityObservation{Identity: shellcore.ShellIdentity{Family: shellcore.ShellFish, RuntimeID: "runtime_ctx"}, State: shellapp.IdentityExact, ObservedAt: time.Now().UTC()}, nil
}

func TestContextExecClaimSnapshotSkipsForegroundHelperShellProbe(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, _ := reserveDelegatedRecovery(t, st, "ctx_claim", delegated.LifecycleLive, "", 0)
	provider := &contextExecProvider{restartDelegatedRuntime: newRestartDelegatedRuntime(), currentCommand: "fish"}
	probe := &countingContextExecShellProbe{failAfter: 2}
	helper := &contextExecHelper{}
	svc, available := app.ComposeContextExec(st, provider, probe, helper, app.ContextExecCompositionOptions{
		Incarnation: "daemon_ctx_claim", HelperExecutable: "/opt/shellbeam/bin/shellbeam",
		NewOpaqueLaunchID: func() string { return "launch_ctx_claim" }, NewHelperGeneration: func() string { return "helper_ctx_claim" },
	})
	if !available || svc == nil || helper.callbacks.BindClaim == nil {
		t.Fatalf("available=%v service=%v callbacks=%#v", available, svc, helper.callbacks)
	}
	req := contextcore.Request{ContextExecID: "ctxexec_daemon_claim", SessionID: binding.SessionID, AuthorityEpoch: binding.AuthorityEpoch, Argv: []string{"go", "test"}, TimeoutMS: 1000, MaxOutputBytes: 4096}
	state, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != contextcore.LifecycleHelperRequested || state.Helper == nil || probe.calls != 2 {
		t.Fatalf("state=%#v probe_calls=%d", state, probe.calls)
	}

	provider.currentCommand = "shellbeam"
	final := contextcore.ContextBinding{
		SessionID: state.Expectation.SessionID, AuthorityEpoch: state.Expectation.AuthorityEpoch,
		ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt",
		CWDObserved: state.Expectation.CWDObserved, PrivacyState: state.Expectation.PrivacyState,
	}
	continuity := contextcore.ShellContinuityExpectation{
		SessionID: state.Request.SessionID, AuthorityEpoch: state.Request.AuthorityEpoch,
		ProviderGeneration: state.Expectation.ProviderGeneration, ShellRuntimeIdentity: state.Expectation.ShellIdentity,
		PaneShellPID: 4242, PaneShellProcessIdentity: "proc_shell_daemon_claim", PaneTTY: "/dev/ttys042",
		HelperExecutableIdentity: state.Helper.ExecutablePath,
	}
	proof := contextcore.ShellContinuityProof{
		SessionID: continuity.SessionID, AuthorityEpoch: continuity.AuthorityEpoch, ProviderGeneration: continuity.ProviderGeneration,
		ShellRuntimeIdentity: continuity.ShellRuntimeIdentity, PaneShellPID: continuity.PaneShellPID,
		PaneShellProcessIdentity: continuity.PaneShellProcessIdentity, PaneTTY: continuity.PaneTTY,
		HelperPID: 4243, HelperExecutableIdentity: continuity.HelperExecutableIdentity, ForegroundProven: true, ObservedAt: time.Now().UTC(),
	}
	bound, err := helper.callbacks.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, continuity, proof, time.Now().UTC(), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if bound.Lifecycle != contextcore.LifecycleHelperAuthenticated || probe.calls != 2 {
		t.Fatalf("bound=%#v probe_calls=%d", bound, probe.calls)
	}
}

func TestComposeContextExecUsesCurrentProviderPrivacyAndShellTruth(t *testing.T) {
	st := openDelegatedStartStore(t)
	binding, ref := reserveDelegatedRecovery(t, st, "ctx", delegated.LifecycleLive, "", 0)
	provider := &contextExecProvider{restartDelegatedRuntime: newRestartDelegatedRuntime()}
	helper := &contextExecHelper{}
	svc, available := app.ComposeContextExec(st, provider, contextExecShellProbe{}, helper, app.ContextExecCompositionOptions{
		Incarnation: "daemon_ctx", HelperExecutable: "/opt/shellbeam/bin/shellbeam",
		NewOpaqueLaunchID: func() string { return "launch_ctx" }, NewHelperGeneration: func() string { return "helper_ctx" },
	})
	if !available || svc == nil {
		t.Fatalf("available=%v service=%v", available, svc)
	}
	req := contextcore.Request{ContextExecID: "ctxexec_daemon_compose", SessionID: binding.SessionID, AuthorityEpoch: binding.AuthorityEpoch, Argv: []string{"go", "test"}, TimeoutMS: 1000, MaxOutputBytes: 4096}
	state, err := svc.Execute(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if state.Lifecycle != contextcore.LifecycleHelperRequested || helper.arms != 1 {
		t.Fatalf("state=%#v arms=%d", state, helper.arms)
	}
	if helper.last.ProviderRef != ref || helper.last.Expectation.ProviderGeneration != "gen_ctx" || helper.last.Expectation.CWDObserved != "/tmp/project" || helper.last.Shell.ExpectedShell.RuntimeID != "runtime_ctx" {
		t.Fatalf("arm=%#v", helper.last)
	}
}

func TestComposeContextExecFailsClosedWhenPrivacyInspectorMissing(t *testing.T) {
	st := openDelegatedStartStore(t)
	provider := newRestartDelegatedRuntime()
	helper := &contextExecHelper{}
	if svc, available := app.ComposeContextExec(st, provider, contextExecShellProbe{}, helper, app.ContextExecCompositionOptions{Incarnation: "daemon_ctx", HelperExecutable: "/opt/shellbeam/bin/shellbeam"}); available || svc != nil {
		t.Fatalf("available=%v service=%v", available, svc)
	}
}

func TestDaemonContextExecAvailabilityAndFailClosedInternalDispatch(t *testing.T) {
	plain := app.NewService(nil, nil, app.Options{})
	if plain.ContextExecAvailable() {
		t.Fatal("context exec available without composed service")
	}
	req := contextcore.Request{ContextExecID: "ctxexec_internal_unavailable", SessionID: "session_internal_unavailable", AuthorityEpoch: 1, Argv: []string{"go"}, MaxOutputBytes: 1024}
	if _, err := plain.ExecuteContext(context.Background(), req); !errors.Is(err, failure.ContextExecUnavailable) {
		t.Fatalf("err=%v want context_exec_unavailable", err)
	}
	fake := contextExecInternalService{}
	wired := app.NewService(nil, nil, app.Options{ContextExec: fake})
	if !wired.ContextExecAvailable() {
		t.Fatal("composed context exec not available internally")
	}
	got, err := wired.ExecuteContext(context.Background(), req)
	if err != nil || got.Request.ContextExecID != req.ContextExecID {
		t.Fatalf("state=%#v err=%v", got, err)
	}
}

type contextExecInternalService struct{}

func (contextExecInternalService) Execute(_ context.Context, req contextcore.Request) (operation.ContextExecState, error) {
	return operation.ContextExecState{Request: req}, nil
}
func (contextExecInternalService) Reconcile(context.Context) ([]contextapp.RecoveryDecision, error) {
	return nil, nil
}
