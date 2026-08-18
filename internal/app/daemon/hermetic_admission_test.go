package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestHermeticRequestWithoutQualifiedRuntimeFailsClosedBeforeSpawn(t *testing.T) {
	owner := &fakeOwner{}
	svc := app.NewService(nil, owner, app.Options{})
	_, err := svc.Start(context.Background(), app.StartRequest{
		ProtocolVersion: 2,
		OperationID:     "hermetic-no-runtime",
		WorkspaceID:     "ws_01K00000000000000000000000",
		CWD:             ".",
		Command:         "true",
		Hermetic:        daemonHermeticAdmissionRequest(),
	})
	if !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("error=%v want feature_unavailable", err)
	}
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Details["feature"] != "hermetic_boundary_v1" {
		t.Fatalf("wrong fail-closed feature error: %#v", typed)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("unqualified hermetic request spawned %d child processes", owner.starts.Load())
	}
}

func daemonHermeticAdmissionRequest() *hermetic.Request {
	return &hermetic.Request{
		Version: 1, Mode: hermetic.ModeRequired, RepoInputs: []string{"go.mod"},
		Network: hermetic.NetworkOff, Environment: hermetic.EnvironmentFixedAllowlist,
		Stdin: hermetic.StdinClosed, Writes: hermetic.WritesEphemeralDiscard,
	}
}

type fakeHermeticRuntime struct {
	prepareCalls int
	startCalls   int
	discardCalls int
	prepareErr   error
	startErr     error
	spawn        receipt.SpawnEvidence
	lastPrepare  app.HermeticPrepareRequest
	prepared     hermeticapp.PreparedExecution
	handle       app.ProcessHandle
}

func (r *fakeHermeticRuntime) Prepare(_ context.Context, req app.HermeticPrepareRequest) (hermeticapp.PreparedExecution, error) {
	r.prepareCalls++
	r.lastPrepare = req
	if r.prepareErr != nil {
		return hermeticapp.PreparedExecution{}, r.prepareErr
	}
	if req.WorkspaceID == "" || req.LogicalCWD == "" || req.Target.CWD == "" || req.Request.Version != 1 {
		return hermeticapp.PreparedExecution{}, errors.New("bad hermetic prepare request")
	}
	return r.prepared, nil
}
func (r *fakeHermeticRuntime) Start(context.Context, hermeticapp.PreparedExecution, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	r.startCalls++
	spawn := r.spawn
	if !spawn.Attempted && r.startErr == nil {
		spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: true}
	}
	return r.handle, spawn, r.startErr
}
func (r *fakeHermeticRuntime) Discard(context.Context, hermeticapp.PreparedExecution) error {
	r.discardCalls++
	return nil
}

type hermeticDaemonHandle struct {
	result hermetic.BoundaryResult
	exit   receipt.ExitEvidence
}

func (h *hermeticDaemonHandle) Write([]byte) error { return nil }
func (h *hermeticDaemonHandle) CloseStdin() error  { return nil }
func (h *hermeticDaemonHandle) Signal(signal string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Requested: signal, Attempted: true, Succeeded: true}
}
func (h *hermeticDaemonHandle) Wait(context.Context) receipt.ExitEvidence       { return h.exit }
func (h *hermeticDaemonHandle) Close() error                                    { return nil }
func (h *hermeticDaemonHandle) HermeticBoundaryResult() hermetic.BoundaryResult { return h.result }

func TestHermeticRuntimeFreezesBindingAndPublishesMatchingTerminalResultWithoutOrdinarySpawn(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	provider := hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: daemonDigest('a'), RuntimeManifestSHA256: daemonDigest('b')}
	toolchain := hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: daemonDigest('c')}
	prepared := hermeticapp.PreparedExecution{BoundaryID: "hb_01K00000000000000000000000", Provider: provider, Toolchain: toolchain, CaptureManifestSHA256: daemonDigest('d'), CaptureContentSHA256: daemonDigest('e'), Command: hermeticapp.ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}, PrivateStateRoot: "/private/hb_01K00000000000000000000000", ScratchRoot: "/private/hb_01K00000000000000000000000/scratch"}
	zero := 0
	runtime := &fakeHermeticRuntime{prepared: prepared, handle: &hermeticDaemonHandle{result: hermetic.BoundaryResult{SchemaVersion: 1, BoundaryID: prepared.BoundaryID, Provider: provider, Toolchain: toolchain, EstablishedPreExec: true, Continuity: hermetic.ContinuityComplete}, exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}}
	owner := &fakeOwner{}
	catalog := capability.Baseline(capability.Limits{}).WithHermeticBoundary(daemonHermeticSupport())
	resolver := &fakeAddressResolver{cwd: "/repo"}
	svc := app.NewServiceWithWorkspaceResolver(st, owner, resolver, app.Options{Incarnation: "hermetic-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100, Capabilities: catalog, HermeticRuntime: runtime})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "hermetic-runtime", WorkspaceID: "ws_01K00000000000000000000000", CWD: ".", Command: "true", Hermetic: daemonHermeticAdmissionRequest(), YieldMS: 100}
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if owner.starts.Load() != 0 || runtime.prepareCalls != 1 || runtime.startCalls != 1 || runtime.discardCalls != 1 {
		t.Fatalf("owner=%d prepare=%d start=%d discard=%d", owner.starts.Load(), runtime.prepareCalls, runtime.startCalls, runtime.discardCalls)
	}
	stored, err := st.LoadOperation(context.Background(), "hermetic-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if stored.HermeticBoundary == nil || stored.HermeticBoundary.BoundaryID != prepared.BoundaryID || stored.HermeticBoundary.CaptureManifestSHA256 != prepared.CaptureManifestSHA256 {
		t.Fatalf("stored hermetic binding=%#v", stored.HermeticBoundary)
	}
	if terminal.Receipt == nil || terminal.Receipt.HermeticBinding == nil || terminal.Receipt.HermeticResult == nil {
		t.Fatalf("terminal hermetic truth missing: %#v", terminal.Receipt)
	}
	if !terminal.Receipt.HermeticResult.Authoritative() {
		t.Fatalf("terminal result not authoritative: %#v", terminal.Receipt.HermeticResult)
	}
	if terminal.Receipt.Exit.Code == nil || *terminal.Receipt.Exit.Code != 0 || terminal.Receipt.Exit.Signal != "" {
		t.Fatalf("literal exit rewritten: %#v", terminal.Receipt.Exit)
	}
}

func TestHermeticPrepareFailureDoesNotReserveOrSpawn(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeHermeticRuntime{prepareErr: errors.New("capture drift")}
	owner := &fakeOwner{}
	catalog := capability.Baseline(capability.Limits{}).WithHermeticBoundary(daemonHermeticSupport())
	svc := app.NewServiceWithWorkspaceResolver(st, owner, &fakeAddressResolver{cwd: "/repo"}, app.Options{Incarnation: "h", Shell: "/bin/sh", MaxQueuedInputBytes: 100, Capabilities: catalog, HermeticRuntime: runtime})
	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "hermetic-prepare-fail", WorkspaceID: "ws_01K00000000000000000000000", CWD: ".", Command: "true", Hermetic: daemonHermeticAdmissionRequest()})
	if err == nil {
		t.Fatal("prepare failure accepted")
	}
	if owner.starts.Load() != 0 || runtime.startCalls != 0 || runtime.discardCalls != 0 {
		t.Fatalf("unsafe effects owner=%d start=%d discard=%d", owner.starts.Load(), runtime.startCalls, runtime.discardCalls)
	}
	if _, loadErr := st.LoadOperation(context.Background(), "hermetic-prepare-fail"); loadErr == nil {
		t.Fatal("prepare failure reserved operation")
	}
}

func daemonHermeticSupport() capability.HermeticBoundarySupport {
	return capability.HermeticBoundarySupport{Version: 1, Maturity: "experimental", Provider: "bubblewrap", ProviderVersion: "0.11.2", Scope: "verification_only_ephemeral", Filesystem: "immutable_capture", Network: "off", Environment: "fixed_allowlist", Stdin: "closed", Writes: "ephemeral_discard", TimeRandomness: "ambient_nondeterministic", ChildTree: "enclosed", Placement: "pre_exec", PTY: "unsupported", PersistentSessions: "unsupported", Authority: "proven_input_scope"}
}
func daemonDigest(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}

func TestHermeticMismatchedBoundaryStatusLosesAuthorityWithoutRewritingLiteralSignal(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state-loss"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	provider := hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: daemonDigest('a'), RuntimeManifestSHA256: daemonDigest('b')}
	toolchain := hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: daemonDigest('c')}
	prepared := hermeticapp.PreparedExecution{BoundaryID: "hb_01K00000000000000000000010", Provider: provider, Toolchain: toolchain, CaptureManifestSHA256: daemonDigest('d'), CaptureContentSHA256: daemonDigest('e'), Command: hermeticapp.ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}, PrivateStateRoot: "/private/hb_01K00000000000000000000010", ScratchRoot: "/private/hb_01K00000000000000000000010/scratch"}
	handle := &hermeticDaemonHandle{result: hermetic.BoundaryResult{SchemaVersion: 1, BoundaryID: "hb_01K00000000000000000000099", Provider: provider, Toolchain: toolchain, EstablishedPreExec: true, Continuity: hermetic.ContinuityComplete}, exit: receipt.ExitEvidence{Reaped: true, Signal: "killed"}}
	runtime := &fakeHermeticRuntime{prepared: prepared, handle: handle}
	catalog := capability.Baseline(capability.Limits{}).WithHermeticBoundary(daemonHermeticSupport())
	svc := app.NewServiceWithWorkspaceResolver(st, &fakeOwner{}, &fakeAddressResolver{cwd: "/repo"}, app.Options{Incarnation: "h-loss", Shell: "/bin/sh", MaxQueuedInputBytes: 100, Capabilities: catalog, HermeticRuntime: runtime})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "hermetic-loss", WorkspaceID: "ws_01K00000000000000000000000", CWD: ".", Command: "true", Hermetic: daemonHermeticAdmissionRequest(), YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.HermeticResult == nil {
		t.Fatalf("receipt=%#v", terminal.Receipt)
	}
	if terminal.Receipt.HermeticResult.Authoritative() || terminal.Receipt.HermeticResult.Continuity != hermetic.ContinuityLost || terminal.Receipt.HermeticResult.EstablishedPreExec {
		t.Fatalf("mismatched status retained authority: %#v", terminal.Receipt.HermeticResult)
	}
	if terminal.Receipt.Exit.Signal != "killed" || terminal.Receipt.Exit.Code != nil {
		t.Fatalf("literal signal rewritten: %#v", terminal.Receipt.Exit)
	}
}

func TestHermeticSpawnFailurePublishesLostBoundaryAndLiteralSpawnEvidence(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state-spawn-fail"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	provider := hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: daemonDigest('a'), RuntimeManifestSHA256: daemonDigest('b')}
	toolchain := hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: daemonDigest('c')}
	prepared := hermeticapp.PreparedExecution{BoundaryID: "hb_01K00000000000000000000020", Provider: provider, Toolchain: toolchain, CaptureManifestSHA256: daemonDigest('d'), CaptureContentSHA256: daemonDigest('e'), Command: hermeticapp.ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}, PrivateStateRoot: "/private/hb_01K00000000000000000000020", ScratchRoot: "/private/hb_01K00000000000000000000020/scratch"}
	runtime := &fakeHermeticRuntime{prepared: prepared, startErr: errors.New("bwrap pre-exec failure"), spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "provider_spawn_failed"}}
	owner := &fakeOwner{}
	catalog := capability.Baseline(capability.Limits{}).WithHermeticBoundary(daemonHermeticSupport())
	svc := app.NewServiceWithWorkspaceResolver(st, owner, &fakeAddressResolver{cwd: "/repo"}, app.Options{Incarnation: "h-spawn", Shell: "/bin/sh", MaxQueuedInputBytes: 100, Capabilities: catalog, HermeticRuntime: runtime})
	view, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "hermetic-spawn-fail", WorkspaceID: "ws_01K00000000000000000000000", CWD: ".", Command: "true", Hermetic: daemonHermeticAdmissionRequest(), YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if view.Receipt == nil {
		view = waitForTerminal(t, svc, view.SessionID)
	}
	if owner.starts.Load() != 0 || runtime.prepareCalls != 1 || runtime.startCalls != 1 || runtime.discardCalls != 1 {
		t.Fatalf("owner=%d prepare=%d start=%d discard=%d", owner.starts.Load(), runtime.prepareCalls, runtime.startCalls, runtime.discardCalls)
	}
	if view.Receipt == nil || view.Receipt.HermeticBinding == nil || view.Receipt.HermeticResult == nil {
		t.Fatalf("spawn failure receipt=%#v", view.Receipt)
	}
	if view.Receipt.HermeticResult.Authoritative() || view.Receipt.HermeticResult.Continuity != hermetic.ContinuityLost || view.Receipt.HermeticResult.EstablishedPreExec {
		t.Fatalf("spawn failure retained authority: %#v", view.Receipt.HermeticResult)
	}
	if !view.Receipt.Spawn.Attempted || view.Receipt.Spawn.Succeeded || view.Receipt.Spawn.ErrorCode != "provider_spawn_failed" {
		t.Fatalf("literal spawn evidence rewritten: %#v", view.Receipt.Spawn)
	}
}

func TestTypedHermeticRuntimePersistsV3BoundaryAndUsesFrozenProjectLogicalCWD(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	binding := daemonProjectBinding(t, []string{"go", "test", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	provider := hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: daemonDigest('a'), RuntimeManifestSHA256: daemonDigest('b')}
	toolchain := hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: daemonDigest('c')}
	prepared := hermeticapp.PreparedExecution{BoundaryID: "hb_01K00000000000000000000040", Provider: provider, Toolchain: toolchain, CaptureManifestSHA256: daemonDigest('d'), CaptureContentSHA256: daemonDigest('e'), Command: hermeticapp.ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--", "/usr/bin/go", "test", "./internal/app"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}, PrivateStateRoot: "/private/hb_01K00000000000000000000040", ScratchRoot: "/private/hb_01K00000000000000000000040/scratch"}
	zero := 0
	runtime := &fakeHermeticRuntime{prepared: prepared, handle: &hermeticDaemonHandle{result: hermetic.BoundaryResult{SchemaVersion: 1, BoundaryID: prepared.BoundaryID, Provider: provider, Toolchain: toolchain, EstablishedPreExec: true, Continuity: hermetic.ContinuityComplete}, exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}}
	owner := &fakeOwner{}
	catalog := capability.Baseline(capability.Limits{}).WithHermeticBoundary(daemonHermeticSupport())
	svc := app.NewService(store, owner, app.Options{Incarnation: "typed-hermetic", Shell: "/bin/sh", MaxQueuedInputBytes: 100, Capabilities: catalog, HermeticRuntime: runtime, ProjectCommandBinder: binder})
	req := typedStartRequest("typed-hermetic-runtime", "./internal/app")
	req.Hermetic = daemonHermeticAdmissionRequest()
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if owner.starts.Load() != 0 || runtime.prepareCalls != 1 || runtime.startCalls != 1 {
		t.Fatalf("owner=%d prepare=%d start=%d", owner.starts.Load(), runtime.prepareCalls, runtime.startCalls)
	}
	if runtime.lastPrepare.LogicalCWD != "." || runtime.lastPrepare.Target.Mode != operation.ExecutionModeArgv || runtime.lastPrepare.Target.CWD != "/repo" {
		t.Fatalf("prepare=%#v", runtime.lastPrepare)
	}
	stored, err := store.LoadOperation(context.Background(), "typed-hermetic-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 3 || stored.ProjectCommand == nil || stored.HermeticBoundary == nil {
		t.Fatalf("typed stored=%#v", stored)
	}
	if terminal.Receipt == nil || terminal.Receipt.SchemaVersion != 3 || terminal.Receipt.ProjectCommand == nil || terminal.Receipt.HermeticBinding == nil || terminal.Receipt.HermeticResult == nil || !terminal.Receipt.HermeticResult.Authoritative() {
		t.Fatalf("typed terminal=%#v", terminal.Receipt)
	}
}
