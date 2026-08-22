package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	contextadapter "github.com/maemreyo/shellbeam/internal/adapter/contextexec"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	shelladapter "github.com/maemreyo/shellbeam/internal/adapter/shellintegration"
	contextapp "github.com/maemreyo/shellbeam/internal/app/contextexec"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	processcore "github.com/maemreyo/shellbeam/internal/core/process"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextExecDaemonRuntime struct {
	lifetime   context.Context
	provider   daemonapp.DelegatedRuntime
	runtimeDir string
	executable string
	observe    func(context.Context, int) (processcore.ProcessFact, error)
	listen     func(string, string) (net.Listener, string, error)
	verifyPeer contextadapter.PeerVerifier
	mu         sync.RWMutex
	callbacks  contextapp.RuntimeCallbacks
}

func newContextExecDaemonRuntime(ctx context.Context, provider daemonapp.DelegatedRuntime, runtimeDir, executable string) *contextExecDaemonRuntime {
	if ctx == nil {
		ctx = context.Background()
	}
	return &contextExecDaemonRuntime{
		lifetime: ctx, provider: provider, runtimeDir: filepath.Clean(runtimeDir), executable: filepath.Clean(executable),
		observe: processadapter.NewHostInspector().Observe, listen: contextadapter.ListenPrivate,
	}
}

func (r *contextExecDaemonRuntime) Qualified() bool {
	return r != nil && runtime.GOOS == "darwin" && r.provider != nil && r.observe != nil && r.listen != nil && filepath.IsAbs(r.runtimeDir) && filepath.IsAbs(r.executable) && contextadapter.NewPlatformLauncher(r.executable).Qualified()
}

func (r *contextExecDaemonRuntime) BindContextExecCallbacks(callbacks contextapp.RuntimeCallbacks) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.callbacks = callbacks
	r.mu.Unlock()
}

func (r *contextExecDaemonRuntime) runtimeCallbacks() (contextapp.RuntimeCallbacks, error) {
	r.mu.RLock()
	callbacks := r.callbacks
	r.mu.RUnlock()
	if callbacks.BindClaim == nil || callbacks.AuthorizePrepared == nil || callbacks.RecordSpawn == nil || callbacks.RecordTerminal == nil || callbacks.CanonicalizeNoChildFailure == nil {
		return contextapp.RuntimeCallbacks{}, fmt.Errorf("context exec runtime callbacks unavailable")
	}
	return callbacks, nil
}

func (r *contextExecDaemonRuntime) ArmContextHelper(ctx context.Context, req contextapp.HelperArmRequest) (shellapp.ContextHelperArm, error) {
	var zero shellapp.ContextHelperArm
	if !r.Qualified() {
		return zero, fmt.Errorf("context exec daemon runtime unavailable")
	}
	callbacks, err := r.runtimeCallbacks()
	if err != nil {
		return zero, err
	}
	if err := req.ProviderRef.Validate(); err != nil {
		return zero, err
	}
	if err := req.Helper.Validate(); err != nil {
		return zero, err
	}
	if err := req.Expectation.Validate(); err != nil {
		return zero, err
	}
	facts := req.Shell.Facts
	parent, err := r.observe(ctx, facts.PanePID)
	if err != nil || parent.PID != facts.PanePID || parent.Identity == nil || parent.Identity.Value == "" {
		return zero, fmt.Errorf("context exec pane shell identity unavailable")
	}
	listener, path, err := r.listen(r.runtimeDir, req.Helper.OpaqueLaunchID)
	if err != nil {
		return zero, err
	}
	serve := contextExecServeRequest{arm: req, callbacks: callbacks, parentIdentity: parent.Identity.Value}
	go r.serveOne(listener, path, serve)
	if err := r.armShell(ctx, req); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return zero, err
	}
	return shellapp.ContextHelperArm{
		ContextExecID: req.Shell.ContextExecID, SessionID: req.Shell.SessionID, AuthorityEpoch: req.Shell.Authority.Epoch,
		ProviderGeneration: facts.ProviderGeneration, Shell: req.Shell.ExpectedShell, PaneShellPID: facts.PanePID,
		PaneTTY: filepath.Clean(facts.PaneTTY), OpaqueLaunchID: req.Helper.OpaqueLaunchID, ArmedAt: time.Now().UTC(),
	}, nil
}

func (r *contextExecDaemonRuntime) armShell(ctx context.Context, req contextapp.HelperArmRequest) error {
	facts := req.Shell.Facts
	port := &delegatedShellCommandPort{provider: r.provider, ref: req.ProviderRef, providerGeneration: facts.ProviderGeneration, currentCommand: facts.CurrentCommand, panePID: facts.PanePID}
	adapter, err := shellAdapterFor(req.Shell.ExpectedShell.Family, shelladapter.Dependencies{Executable: r.executable, RuntimeDir: r.runtimeDir, Command: port})
	if err != nil {
		return err
	}
	armer, ok := adapter.(shellapp.ContextHelperArmer)
	if !ok {
		return fmt.Errorf("context exec shell adapter cannot arm helper")
	}
	return armer.ArmContextHelper(ctx, shellapp.ContextHelperArmSpec{Shell: req.Shell.ExpectedShell, OpaqueLaunchID: req.Helper.OpaqueLaunchID})
}

type contextExecServeRequest struct {
	arm            contextapp.HelperArmRequest
	callbacks      contextapp.RuntimeCallbacks
	parentIdentity string
}

func (r *contextExecDaemonRuntime) serveOne(listener net.Listener, path string, req contextExecServeRequest) {
	if listener == nil {
		return
	}
	defer func() { _ = listener.Close(); _ = os.Remove(path) }()
	connCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		connCh <- conn
	}()
	var conn net.Conn
	select {
	case <-r.lifetime.Done():
		return
	case <-errCh:
		return
	case conn = <-connCh:
	}
	_ = listener.Close()
	_ = os.Remove(path)
	defer conn.Close()
	server := r.serverFor(req)
	state, err := server.Authenticate(r.lifetime, conn)
	if err != nil {
		return
	}
	spawned, received, err := server.ReceiveExecution(r.lifetime, conn, state)
	if err != nil || received.Terminal.ContextExecID == "" {
		return
	}
	_, _ = req.callbacks.RecordTerminal(r.lifetime, spawned, contextapp.TerminalTruth{
		Result: received.Terminal, StdoutBytes: int64(len(received.Stdout)), StderrBytes: int64(len(received.Stderr)), CombinedOutput: append([]byte(nil), received.Combined...),
	})
}

func (r *contextExecDaemonRuntime) serverFor(req contextExecServeRequest) *contextadapter.Server {
	arm := req.arm
	expectation := contextadapter.ClaimExpectation{
		Identity: contextadapter.ClaimIdentity{
			OpaqueLaunchID: arm.Helper.OpaqueLaunchID, ContextExecID: arm.Shell.ContextExecID, SessionID: arm.Shell.SessionID,
			AuthorityEpoch: arm.Shell.Authority.Epoch, Generation: arm.Helper.Generation, RequestFingerprint: arm.Helper.RequestFingerprint,
		},
		Helper: arm.Helper, Context: arm.Expectation,
	}
	verify := r.verifyPeer
	if verify == nil {
		verifier := contextadapter.HostPeerVerifier{
			ExpectedHelperExecutable: r.executable, ParentPID: arm.Shell.Facts.PanePID, ParentIdentity: req.parentIdentity,
			PaneTTY: arm.Shell.Facts.PaneTTY, Observe: r.observe,
		}
		verify = verifier.Verify
	}
	server := &contextadapter.Server{Expectation: expectation, VerifyPeer: verify, BindClaim: req.callbacks.BindClaim}
	server.AuthorizePrepared = func(ctx context.Context, state operation.ContextExecState, prepared contextadapter.PreparedFrame) (operation.ContextExecState, contextadapter.ExecuteFrame, error) {
		if prepared.FailureCode != "" {
			finalized, err := req.callbacks.CanonicalizeNoChildFailure(ctx, state, contextapp.NoChildFailureTruth{FailureCode: prepared.FailureCode})
			return finalized, contextadapter.ExecuteFrame{ProtocolVersion: contextadapter.ProtocolVersion, Kind: contextadapter.KindExecute, Authorized: false}, err
		}
		next, auth, err := req.callbacks.AuthorizePrepared(ctx, state, prepared.ResolvedExecutable)
		if err != nil {
			return state.Clone(), contextadapter.ExecuteFrame{}, err
		}
		return next, contextadapter.ExecuteFrame{ProtocolVersion: contextadapter.ProtocolVersion, Kind: contextadapter.KindExecute, Authorized: true, ChildOperationID: auth.ChildOperationID, ChildSessionID: auth.ChildSessionID, ResolvedExecutable: auth.ResolvedExecutable}, nil
	}
	server.RecordSpawn = func(ctx context.Context, state operation.ContextExecState, spawn contextadapter.SpawnFrame) (operation.ContextExecState, error) {
		if !spawn.Spawn.Succeeded {
			return req.callbacks.CanonicalizeNoChildFailure(ctx, state, contextapp.NoChildFailureTruth{ResolvedExecutable: spawn.ResolvedExecutable, Spawn: spawn.Spawn, FailureCode: spawn.Spawn.ErrorCode})
		}
		return req.callbacks.RecordSpawn(ctx, state, contextapp.SpawnTruth{ChildOperationID: spawn.ChildOperationID, ChildSessionID: spawn.ChildSessionID, ResolvedExecutable: spawn.ResolvedExecutable, Spawn: spawn.Spawn})
	}
	return server
}

var _ contextapp.HelperRuntime = (*contextExecDaemonRuntime)(nil)
var _ contextapp.RuntimeCallbackBinder = (*contextExecDaemonRuntime)(nil)
var _ = contextcore.SchemaVersion

func composeContextExecCapability(catalog capability.Catalog, service daemonapp.ContextExecService) capability.Catalog {
	if service == nil || catalog.DelegatedInteractive == nil {
		return catalog
	}
	support := capability.ContextExecSupport{
		ProviderID: catalog.DelegatedInteractive.ProviderID, ProviderVersion: catalog.DelegatedInteractive.ProviderVersion, Platform: catalog.DelegatedInteractive.Platform,
		ShellAdapters:         []shellcore.ShellFamily{shellcore.ShellFish, shellcore.ShellZsh, shellcore.ShellBash},
		HelperProtocolVersion: contextadapter.ProtocolVersion,
		EvidenceAuthority:     contextcore.EvidenceAuthorityContextExecChildOwnedV1,
		EvidenceQualities:     []contextcore.EvidenceQuality{contextcore.EvidenceQualityUnproven, contextcore.EvidenceQualityIncomplete, contextcore.EvidenceQualityComplete, contextcore.EvidenceQualityAmbiguous},
		OutputAttribution:     contextcore.OutputAttributionHelperOwnedChildPipes,
		ResourceEnforcement:   capability.Unavailable, Hermetic: capability.Unavailable,
	}
	return catalog.WithContextExec(support)
}

func composeContextExecServiceCapability(ctx context.Context, store daemonapp.Store, provider daemonapp.DelegatedRuntime, runtimeDir, incarnation string, structured daemonapp.StructuredWorker, telemetry daemonapp.TelemetryWorker, evidence daemonapp.EvidenceWorker, catalog capability.Catalog) (daemonapp.ContextExecService, capability.Catalog) {
	service := composeContextExecService(ctx, store, provider, runtimeDir, incarnation, structured, telemetry, evidence)
	return service, composeContextExecCapability(catalog, service)
}

func composeContextExecService(ctx context.Context, store daemonapp.Store, provider daemonapp.DelegatedRuntime, runtimeDir, incarnation string, structured daemonapp.StructuredWorker, telemetry daemonapp.TelemetryWorker, evidence daemonapp.EvidenceWorker) daemonapp.ContextExecService {
	runtime, executable := composeContextExecRuntime(ctx, provider, runtimeDir)
	scheduler := contextExecTerminalScheduler{structured: structured, telemetry: telemetry, evidence: evidence}
	service, _ := daemonapp.ComposeContextExec(store, provider, shelladapter.NewUnixProbe(), runtime, daemonapp.ContextExecCompositionOptions{Incarnation: incarnation, HelperExecutable: executable, TerminalScheduler: scheduler})
	return service
}

func composeContextExecRuntime(ctx context.Context, provider daemonapp.DelegatedRuntime, runtimeDir string) (*contextExecDaemonRuntime, string) {
	if runtime.GOOS != "darwin" || provider == nil {
		return nil, ""
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, ""
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, ""
	}
	r := composeContextExecDaemonRuntime(ctx, runtime.GOOS, provider, runtimeDir, filepath.Clean(executable))
	if r == nil {
		return nil, ""
	}
	return r, filepath.Clean(executable)
}

type contextExecTerminalScheduler struct {
	structured daemonapp.StructuredWorker
	telemetry  daemonapp.TelemetryWorker
	evidence   daemonapp.EvidenceWorker
}

func (s contextExecTerminalScheduler) ScheduleContextTerminal(ctx context.Context, rec receipt.Receipt, reservation operation.Reservation) error {
	if s.structured != nil && reservation.StructuredAdapter != "" {
		if err := s.structured.ScheduleTerminal(ctx, rec, reservation.StructuredAdapter); err != nil {
			return err
		}
	}
	if s.telemetry != nil {
		if err := s.telemetry.ScheduleTerminal(ctx, rec); err != nil {
			return err
		}
	}
	if s.evidence != nil && reservation.EvidenceEligible() {
		if err := s.evidence.ScheduleTerminal(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func composeContextExecDaemonRuntime(ctx context.Context, platform string, provider daemonapp.DelegatedRuntime, runtimeDir, executable string) *contextExecDaemonRuntime {
	if platform != "darwin" || provider == nil || !filepath.IsAbs(runtimeDir) || !filepath.IsAbs(executable) {
		return nil
	}
	r := newContextExecDaemonRuntime(ctx, provider, runtimeDir, executable)
	if !r.Qualified() {
		return nil
	}
	return r
}

var _ contextapp.TerminalScheduler = contextExecTerminalScheduler{}
