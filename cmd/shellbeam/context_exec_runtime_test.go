//go:build darwin

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contextadapter "github.com/maemreyo/shellbeam/internal/adapter/contextexec"
	contextapp "github.com/maemreyo/shellbeam/internal/app/contextexec"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	evidencecore "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextExecRuntimeProvider struct {
	*countingDelegatedProvider
	observation delegatedapp.Observation
	write       func([]byte) error
}

func (p *contextExecRuntimeProvider) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	return p.observation, nil
}
func (p *contextExecRuntimeProvider) Write(_ context.Context, _ delegated.ProviderRef, data []byte) error {
	if p.write != nil {
		return p.write(append([]byte(nil), data...))
	}
	return nil
}

func TestContextExecDaemonRuntimeCreatesPrivateListenerBeforeOneShotShellArm(t *testing.T) {
	runtimeDir := shortContextExecRuntimeDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &contextExecRuntimeProvider{countingDelegatedProvider: &countingDelegatedProvider{}}
	req := contextExecDaemonArmFixture(t, runtimeDir)
	provider.observation = delegatedapp.Observation{
		Provider: delegated.ProviderIdentity{ID: req.ProviderRef.ProviderID, Version: req.ProviderRef.ProviderVersion}, ProviderCurrent: true,
		ProviderGeneration: req.Shell.Facts.ProviderGeneration, Owner: delegated.OwnerAgent, PanePID: req.Shell.Facts.PanePID,
		CurrentCommand: req.Shell.Facts.CurrentCommand, PaneTTY: req.Shell.Facts.PaneTTY, CWD: req.Shell.Facts.CWD,
	}
	var dialed atomic.Bool
	provider.write = func(script []byte) error {
		if !strings.Contains(string(script), "__context_exec_helper") || strings.Contains(string(script), req.Shell.ContextExecID) {
			t.Fatalf("unsafe helper script=%q", script)
		}
		conn, err := contextadapter.DialPrivate(runtimeDir, req.Helper.OpaqueLaunchID)
		if err != nil {
			t.Fatalf("listener did not exist before shell arm: %v", err)
		}
		dialed.Store(true)
		_ = conn.Close()
		return nil
	}
	r := newContextExecDaemonRuntime(ctx, provider, runtimeDir, "/bin/echo")
	r.observe = func(_ context.Context, pid int) (processcore.ProcessFact, error) {
		identity := processcore.Identity{Value: "pane-shell-identity"}
		return processcore.ProcessFact{PID: pid, Identity: &identity}, nil
	}
	r.BindContextExecCallbacks(noopContextExecRuntimeCallbacks())
	arm, err := r.ArmContextHelper(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !dialed.Load() || arm.OpaqueLaunchID != req.Helper.OpaqueLaunchID || arm.PaneShellPID != req.Shell.Facts.PanePID {
		t.Fatalf("arm=%#v dialed=%v", arm, dialed.Load())
	}
}

func TestContextExecDaemonRuntimeArmFailureRemovesPrivateSocket(t *testing.T) {
	runtimeDir := shortContextExecRuntimeDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	provider := &contextExecRuntimeProvider{countingDelegatedProvider: &countingDelegatedProvider{}}
	req := contextExecDaemonArmFixture(t, runtimeDir)
	provider.observation = delegatedapp.Observation{Provider: delegated.ProviderIdentity{ID: req.ProviderRef.ProviderID, Version: req.ProviderRef.ProviderVersion}, ProviderCurrent: true, ProviderGeneration: req.Shell.Facts.ProviderGeneration, Owner: delegated.OwnerAgent, PanePID: req.Shell.Facts.PanePID, CurrentCommand: req.Shell.Facts.CurrentCommand, PaneTTY: req.Shell.Facts.PaneTTY, CWD: req.Shell.Facts.CWD}
	provider.write = func([]byte) error { return errors.New("shell write failed") }
	r := newContextExecDaemonRuntime(ctx, provider, runtimeDir, "/bin/echo")
	r.observe = func(_ context.Context, pid int) (processcore.ProcessFact, error) {
		identity := processcore.Identity{Value: "pane-shell-identity"}
		return processcore.ProcessFact{PID: pid, Identity: &identity}, nil
	}
	r.BindContextExecCallbacks(noopContextExecRuntimeCallbacks())
	if _, err := r.ArmContextHelper(context.Background(), req); err == nil {
		t.Fatal("arm failure accepted")
	}
	if conn, err := contextadapter.DialPrivate(runtimeDir, req.Helper.OpaqueLaunchID); err == nil {
		_ = conn.Close()
		t.Fatal("private socket survived failed shell arm")
	}
}

func TestContextExecDaemonRuntimeMapsPrepareAndSpawnTruthOnlyThroughAppCallbacks(t *testing.T) {
	r := &contextExecDaemonRuntime{executable: "/bin/echo"}
	req := contextExecDaemonArmFixture(t, "/tmp")
	state := operation.ContextExecState{Request: contextcore.Request{ContextExecID: req.Shell.ContextExecID, SessionID: req.Shell.SessionID, AuthorityEpoch: req.Shell.Authority.Epoch}, RequestFingerprint: req.Helper.RequestFingerprint}
	var prepared, failedSpawn atomic.Int32
	callbacks := noopContextExecRuntimeCallbacks()
	callbacks.AuthorizePrepared = func(_ context.Context, got operation.ContextExecState, executable string) (operation.ContextExecState, contextapp.PreparedAuthorization, error) {
		prepared.Add(1)
		if executable != "/usr/bin/go" {
			t.Fatalf("prepared executable=%q", executable)
		}
		return got, contextapp.PreparedAuthorization{ChildOperationID: "cxop_child", ChildSessionID: "cxs_child", ResolvedExecutable: executable}, nil
	}
	callbacks.CanonicalizeNoChildFailure = func(_ context.Context, got operation.ContextExecState, truth contextapp.NoChildFailureTruth) (operation.ContextExecState, error) {
		failedSpawn.Add(1)
		if !truth.Spawn.Attempted || truth.Spawn.Succeeded || truth.ResolvedExecutable != "/usr/bin/go" || truth.FailureCode == "" {
			t.Fatalf("failure truth=%#v", truth)
		}
		return got, nil
	}
	server := r.serverFor(contextExecServeRequest{arm: req, callbacks: callbacks, parentIdentity: "pane"})
	next, execute, err := server.AuthorizePrepared(context.Background(), state, contextadapter.PreparedFrame{ProtocolVersion: contextadapter.ProtocolVersion, Kind: contextadapter.KindPrepared, ResolvedExecutable: "/usr/bin/go"})
	if err != nil || !execute.Authorized || execute.ResolvedExecutable != "/usr/bin/go" || prepared.Load() != 1 {
		t.Fatalf("execute=%#v err=%v prepared=%d", execute, err, prepared.Load())
	}
	if _, err := server.RecordSpawn(context.Background(), next, contextadapter.SpawnFrame{ProtocolVersion: contextadapter.ProtocolVersion, Kind: contextadapter.KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: execute.ResolvedExecutable, Spawn: contextadapterSpawnFailure()}); err != nil {
		t.Fatal(err)
	}
	if failedSpawn.Load() != 1 {
		t.Fatalf("failed spawn callbacks=%d", failedSpawn.Load())
	}
}

func contextExecDaemonArmFixture(t *testing.T, runtimeDir string) contextapp.HelperArmRequest {
	t.Helper()
	now := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	request := contextcore.Request{ContextExecID: "ctxexec_runtime_daemon", SessionID: "session_runtime_daemon", AuthorityEpoch: 4, Argv: []string{"go", "test"}, TimeoutMS: 1000, MaxOutputBytes: 4096}
	fp, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: request.SessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "dtmux_runtime_daemon", CreatedAt: now, UpdatedAt: now}
	shell := shellcore.ShellIdentity{Family: shellcore.ShellFish, RuntimeID: "fish_runtime_daemon"}
	expectation := contextcore.ContextExpectation{SessionID: request.SessionID, AuthorityEpoch: request.AuthorityEpoch, ProviderGeneration: "gen_runtime_daemon", ShellIdentity: "fish:fish_runtime_daemon", CWDObserved: "/tmp/project", PrivacyState: "standard"}
	helper := contextcore.HelperBinding{OpaqueLaunchID: "launch_runtime_daemon", Generation: "helper_runtime_daemon", RequestFingerprint: fp, ExecutablePath: "/bin/echo"}
	return contextapp.HelperArmRequest{ProviderRef: ref, Helper: helper, Expectation: expectation, Shell: shellArmRequestFixture(request, shell, runtimeDir)}
}

func shellArmRequestFixture(request contextcore.Request, shell shellcore.ShellIdentity, _ string) shellapp.ContextHelperArmRequest {
	return shellapp.ContextHelperArmRequest{ContextExecID: request.ContextExecID, SessionID: request.SessionID, Authority: delegated.EffectiveAuthority{Epoch: request.AuthorityEpoch, Owner: delegated.OwnerAgent}, Facts: shellapp.ProviderProcessFacts{SessionID: request.SessionID, ProviderID: "tmux_control_mode", ProviderVersion: 1, ProviderGeneration: "gen_runtime_daemon", PanePID: 4242, CurrentCommand: "fish", PaneTTY: "/dev/ttys042", CWD: "/tmp/project"}, ExpectedShell: shell, OpaqueLaunchID: "launch_runtime_daemon"}
}

func noopContextExecRuntimeCallbacks() contextapp.RuntimeCallbacks {
	return contextapp.RuntimeCallbacks{
		BindClaim: func(context.Context, string, contextcore.HelperBinding, contextcore.ContextBinding, time.Time, string) (operation.ContextExecState, error) {
			return operation.ContextExecState{}, nil
		},
		AuthorizePrepared: func(_ context.Context, state operation.ContextExecState, executable string) (operation.ContextExecState, contextapp.PreparedAuthorization, error) {
			return state, contextapp.PreparedAuthorization{ResolvedExecutable: executable}, nil
		},
		RecordSpawn: func(_ context.Context, state operation.ContextExecState, _ contextapp.SpawnTruth) (operation.ContextExecState, error) {
			return state, nil
		},
		RecordTerminal: func(_ context.Context, state operation.ContextExecState, _ contextapp.TerminalTruth) (operation.ContextExecState, error) {
			return state, nil
		},
		CanonicalizeNoChildFailure: func(_ context.Context, state operation.ContextExecState, _ contextapp.NoChildFailureTruth) (operation.ContextExecState, error) {
			return state, nil
		},
	}
}

func shortContextExecRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cxrt-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Clean(dir)
}

func contextadapterSpawnFailure() receipt.SpawnEvidence {
	return receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "context_exec_unavailable"}
}

var _ net.Conn

func TestContextExecDaemonRuntimeCompositionIsDarwinOnly(t *testing.T) {
	provider := &countingDelegatedProvider{}
	runtimeDir := shortContextExecRuntimeDir(t)
	if got := composeContextExecDaemonRuntime(context.Background(), "linux", provider, runtimeDir, "/bin/echo"); got != nil {
		t.Fatalf("linux runtime advertised: %#v", got)
	}
	got := composeContextExecDaemonRuntime(context.Background(), "darwin", provider, runtimeDir, "/bin/echo")
	if got == nil || !got.Qualified() {
		t.Fatalf("darwin runtime unavailable: %#v", got)
	}
}

type countingContextStructuredWorker struct{ calls int }

func (w *countingContextStructuredWorker) ScheduleTerminal(context.Context, receipt.Receipt, string) error {
	w.calls++
	return nil
}

type countingContextTelemetryWorker struct{ calls int }

func (w *countingContextTelemetryWorker) ScheduleTerminal(context.Context, receipt.Receipt) error {
	w.calls++
	return nil
}

type countingContextEvidenceWorker struct{ calls int }

func (w *countingContextEvidenceWorker) ScheduleTerminal(context.Context, receipt.Receipt) error {
	w.calls++
	return nil
}

func TestContextExecTerminalSchedulerKeepsEvidenceScopedToFrozenContracts(t *testing.T) {
	structured := &countingContextStructuredWorker{}
	telemetry := &countingContextTelemetryWorker{}
	evidence := &countingContextEvidenceWorker{}
	scheduler := contextExecTerminalScheduler{structured: structured, telemetry: telemetry, evidence: evidence}
	rec := receipt.Receipt{}
	if err := scheduler.ScheduleContextTerminal(context.Background(), rec, operation.Reservation{}); err != nil {
		t.Fatal(err)
	}
	if structured.calls != 0 || telemetry.calls != 1 || evidence.calls != 0 {
		t.Fatalf("plain calls structured=%d telemetry=%d evidence=%d", structured.calls, telemetry.calls, evidence.calls)
	}
	reservation := operation.Reservation{StructuredAdapter: "pytest", Evidence: &evidencecore.Contract{VerificationKind: evidencecore.VerificationTest}}
	if err := scheduler.ScheduleContextTerminal(context.Background(), rec, reservation); err != nil {
		t.Fatal(err)
	}
	if structured.calls != 1 || telemetry.calls != 2 || evidence.calls != 1 {
		t.Fatalf("contract calls structured=%d telemetry=%d evidence=%d", structured.calls, telemetry.calls, evidence.calls)
	}
}

type contextExecCapabilityService struct{}

func (contextExecCapabilityService) Execute(context.Context, contextcore.Request) (operation.ContextExecState, error) {
	return operation.ContextExecState{}, nil
}
func (contextExecCapabilityService) Reconcile(context.Context) ([]contextapp.RecoveryDecision, error) {
	return nil, nil
}

func TestComposeContextExecCapabilityRequiresComposedServiceAndH4(t *testing.T) {
	base := capability.Baseline(capability.Limits{}).
		WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).
		WithInteractiveHandoff(capability.InteractiveHandoffSupport{
			ManualStandard: true, Secret: true,
			Privacy:          &capability.HandoffPrivacySupport{SecretPrivateInterval: true, PrivacyReleaseSeparate: true, ObserverTopologyQualified: true, HumanInputPersisted: false},
			CaptureQualities: []receipt.CaptureQuality{receipt.CaptureComplete, receipt.CapturePartial, receipt.CaptureIncomplete},
		})
	if got := composeContextExecCapability(base, nil); got.Features[capability.FeatureContextExec] != capability.Unavailable || got.ContextExec != nil {
		t.Fatalf("nil service advertised context exec: %#v", got.ContextExec)
	}
	got := composeContextExecCapability(base, contextExecCapabilityService{})
	if got.Features[capability.FeatureContextExec] != capability.Available || got.ContextExec == nil {
		t.Fatalf("composed context exec unavailable: features=%#v support=%#v", got.Features, got.ContextExec)
	}
	if got.ContextExec.HelperProtocolVersion != contextadapter.ProtocolVersion || got.ContextExec.EvidenceAuthority != contextcore.EvidenceAuthorityContextExecChildOwnedV1 || got.ContextExec.ResourceEnforcement != capability.Unavailable || got.ContextExec.Hermetic != capability.Unavailable {
		t.Fatalf("context exec support=%#v", got.ContextExec)
	}
	if strings.Join([]string{string(got.ContextExec.ShellAdapters[0]), string(got.ContextExec.ShellAdapters[1]), string(got.ContextExec.ShellAdapters[2])}, ",") != "fish,zsh,bash" {
		t.Fatalf("shell adapters=%v", got.ContextExec.ShellAdapters)
	}
}
