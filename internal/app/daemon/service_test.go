package daemon_test

import (
	"context"
	"errors"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeOwner struct{ starts atomic.Int32 }

func (f *fakeOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	f.starts.Add(1)
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type fakeHandle struct{}

func (fakeHandle) Write([]byte) error { return nil }
func (fakeHandle) CloseStdin() error  { return nil }
func (fakeHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (fakeHandle) Wait(context.Context) receipt.ExitEvidence {
	z := 0
	return receipt.ExitEvidence{Reaped: true, Code: &z}
}
func (fakeHandle) Close() error { return nil }

func TestStartRetrySpawnsOnce(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{OperationID: "op", Command: "true", CWD: "/", YieldMS: 100}
	a, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionID != b.SessionID || owner.starts.Load() != 1 {
		t.Fatalf("a=%#v b=%#v starts=%d", a, b, owner.starts.Load())
	}
}

func TestStartReturnsTypedInvalidInput(t *testing.T) {
	svc := app.NewService(nil, nil, app.Options{})
	_, err := svc.Start(context.Background(), app.StartRequest{OperationID: "bad id", Command: "true", CWD: "/"})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("Start error=%v, want invalid_input", err)
	}
	var typed *failure.Failure
	if !errors.As(err, &typed) || typed.Details["field"] != "operation_id" {
		t.Fatalf("typed Start error=%#v", typed)
	}
}

func TestWriteMissingSessionReturnsTypedInvalidInput(t *testing.T) {
	svc := app.NewService(nil, nil, app.Options{})
	_, err := svc.Write(context.Background(), app.WriteRequest{SessionID: "missing", Chars: "x"})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("Write error=%v, want invalid_input", err)
	}
}

func TestServiceCapabilityCatalogIsBaselineAndCopied(t *testing.T) {
	catalog := capability.Baseline(capability.Limits{CommandBytes: 32768, LiveSessions: 4})
	svc := app.NewService(nil, nil, app.Options{Capabilities: catalog})
	got := svc.CapabilityCatalog()
	if got.ProtocolVersion != 2 || got.Limits.CommandBytes != 32768 || got.Limits.LiveSessions != 4 {
		t.Fatalf("capability catalog=%#v", got)
	}
	got.Features[capability.FeatureWorkspaceAddressing] = capability.Unavailable
	again := svc.CapabilityCatalog()
	if again.Features[capability.FeatureWorkspaceAddressing] != capability.Available {
		t.Fatal("service leaked mutable capability feature map")
	}
}

func TestInspectServerDoesNotSpawn(t *testing.T) {
	owner := &fakeOwner{}
	catalog := capability.Baseline(capability.Limits{CommandBytes: 32768, LiveSessions: 4})
	svc := app.NewService(nil, owner, app.Options{Capabilities: catalog})
	got, err := svc.InspectServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("inspect spawned %d processes", owner.starts.Load())
	}
	if got.Capabilities.ProtocolVersion != 2 || got.Capabilities.Limits.CommandBytes != 32768 {
		t.Fatalf("server inspection=%#v", got)
	}
}

func TestV2StartPersistsBindingsAndResponseControlsReplayWithoutSpawn(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	first := app.StartRequest{ProtocolVersion: 2, OperationID: "op-v2", Command: "true", CWD: "/", YieldMS: 100, MaxOutputBytes: 10}
	a, err := svc.Start(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	replay := first
	replay.YieldMS = 999
	replay.MaxOutputBytes = 1
	b, err := svc.Start(context.Background(), replay)
	if err != nil {
		t.Fatal(err)
	}
	if a.SessionID != b.SessionID || owner.starts.Load() != 1 {
		t.Fatalf("a=%#v b=%#v starts=%d", a, b, owner.starts.Load())
	}
	terminal := waitForTerminal(t, svc, a.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.SchemaVersion != 2 || terminal.Receipt.RequestFingerprint == "" || terminal.Receipt.ExecutionFingerprint == "" || terminal.Receipt.Fingerprint != "" {
		t.Fatalf("v2 terminal receipt=%#v", terminal.Receipt)
	}
	stored, err := st.LoadOperation(context.Background(), "op-v2")
	if err != nil {
		t.Fatal(err)
	}
	if stored.SchemaVersion != 2 || stored.RequestFingerprint == "" || stored.ExecutionFingerprint == "" || stored.Fingerprint != "" {
		t.Fatalf("v2 reservation=%#v", stored)
	}
}

func TestV2StartChangedRequestConflictsWithoutSecondSpawn(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	first, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-v2-conflict", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-v2-conflict", Command: "false", CWD: "/"})
	if !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("changed request error=%v", err)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d want 1", owner.starts.Load())
	}
	waitForTerminal(t, svc, first.SessionID)
}

func waitForTerminal(t *testing.T, svc *app.Service, sessionID string) app.View {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		view, err := svc.Poll(ctx, app.PollRequest{SessionID: sessionID, YieldMS: 100})
		if err != nil {
			t.Fatal(err)
		}
		if view.State.Terminal() {
			return view
		}
	}
}

type failingOutputOwner struct{}

func (failingOutputOwner) Start(ctx context.Context, _ operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if err := sink.Append(ctx, []byte("abcdef")); err != nil {
		return nil, receipt.SpawnEvidence{Attempted: true}, err
	}
	return failingOutputHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type failingOutputHandle struct{}

func (failingOutputHandle) Write([]byte) error { return nil }
func (failingOutputHandle) CloseStdin() error  { return nil }
func (failingOutputHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (failingOutputHandle) Wait(context.Context) receipt.ExitEvidence {
	code := 1
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (failingOutputHandle) Close() error { return nil }

func TestAgentExecutionA1StructuredResultSeparatesChildFailureAndExactOutputAccounting(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, failingOutputOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-v2-result", Command: "false", CWD: "/", MaxOutputBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, started.SessionID)
	view, err := svc.Poll(context.Background(), app.PollRequest{SessionID: started.SessionID, Cursor: 0, MaxOutputBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	got, err := view.StructuredResult()
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.State != receipt.OperationTerminal || got.Child == nil || got.Child.State != receipt.ChildExited || got.Child.Outcome != session.Failure {
		t.Fatalf("result=%#v", got)
	}
	if got.Child.ExitCode == nil || *got.Child.ExitCode != 1 {
		t.Fatalf("child=%#v", got.Child)
	}
	if got.Output.Preview != "abcd" || got.Output.RawBytes != 6 || got.Output.ReturnedBytes != 4 || got.Output.Cursor != 0 || got.Output.NextCursor != 4 || !got.Output.Truncated || !got.Output.OutputComplete {
		t.Fatalf("output=%#v", got.Output)
	}
}

type captureFailureOwner struct{}

func (captureFailureOwner) Start(ctx context.Context, _ operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	sink.CaptureFailed(errors.New("capture failed at /Users/alice/private TOKEN=supersecret"))
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func TestV2TerminalReceiptRedactsCaptureFailureDiagnostics(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, captureFailureOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-v2-redact", Command: "true", CWD: "/"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if terminal.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	if terminal.Receipt.FailureReason != "output_capture_failed" {
		t.Fatalf("unsafe failure reason=%q", terminal.Receipt.FailureReason)
	}
}

type eagerObservationOwner struct{}

func (eagerObservationOwner) Start(ctx context.Context, _ operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	if err := sink.Append(ctx, []byte("early")); err != nil {
		return nil, receipt.SpawnEvidence{Attempted: true}, err
	}
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

func TestObservationProcessStartedSequencePrecedesEagerOutput(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, eagerObservationOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "obs-eager-output", Command: "true", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, started.SessionID)
	obligations, err := st.ListObservationObligations(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []observation.EventKind{observation.EventOperationAdmitted, observation.EventProcessStarted, observation.EventOutputAvailable, observation.EventProcessTerminal}
	if len(obligations) != len(want) {
		t.Fatalf("obligations=%#v", obligations)
	}
	for i, kind := range want {
		if obligations[i].Kind != kind || obligations[i].State != observation.ObligationCommitted {
			t.Fatalf("obligation[%d]=%#v", i, obligations[i])
		}
	}
}

type observationSpawnFailureOwner struct{}

func (observationSpawnFailureOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	return nil, receipt.SpawnEvidence{Attempted: true, ErrorCode: "spawn_failed"}, errors.New("spawn failed")
}

func TestObservationSpawnFailureAbortsProcessStartedObligation(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	svc := app.NewService(st, observationSpawnFailureOwner{}, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "obs-spawn-fail", Command: "missing", CWD: "/", YieldMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !started.State.Terminal() {
		_ = waitForTerminal(t, svc, started.SessionID)
	}
	obligations, err := st.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(obligations) != 3 {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
	if obligations[0].Kind != observation.EventOperationAdmitted || obligations[0].State != observation.ObligationCommitted ||
		obligations[1].Kind != observation.EventProcessStarted || obligations[1].State != observation.ObligationAborted ||
		obligations[2].Kind != observation.EventProcessTerminal || obligations[2].State != observation.ObligationCommitted {
		t.Fatalf("obligations=%#v", obligations)
	}
}

func TestV2EvidenceContractPersistsAndConflictingRetryNeverRespawns(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	svc := app.NewService(st, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	contract := &evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-retry", Command: "true", CWD: "/", Evidence: contract, YieldMS: 100}
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	stored, err := st.LoadOperation(context.Background(), "evidence-retry")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Evidence == nil || stored.Evidence.VerificationKind != evidence.VerificationTest {
		t.Fatalf("stored evidence=%#v", stored.Evidence)
	}
	if terminal.Receipt == nil || terminal.Receipt.Evidence == nil || terminal.Receipt.Evidence.VerificationKind != evidence.VerificationTest {
		t.Fatalf("terminal receipt=%#v", terminal.Receipt)
	}
	replay := req
	replay.YieldMS = 0
	if _, err := svc.Start(context.Background(), replay); err != nil {
		t.Fatalf("same evidence replay: %v", err)
	}
	changed := req
	changed.Evidence = &evidence.Contract{VerificationKind: evidence.VerificationBuild, SourceScope: evidence.SourceScopeFull}
	if _, err := svc.Start(context.Background(), changed); !errors.Is(err, failure.OperationMetadataConflict) {
		t.Fatalf("evidence conflict err=%v", err)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestV2EvidenceExpectedOutputsRequireWorkspaceBeforeSpawn(t *testing.T) {
	owner := &fakeOwner{}
	svc := app.NewService(nil, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	contract := &evidence.Contract{VerificationKind: evidence.VerificationBuild, ExpectedOutputs: []project.Output{{Path: "dist/app", Kind: "file", Required: true, Digest: "sha256"}}}
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "evidence-no-workspace", Command: "true", CWD: "/", Evidence: contract})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestV2EvidenceRejectsProtocolV1BeforeSpawn(t *testing.T) {
	owner := &fakeOwner{}
	svc := app.NewService(nil, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 1, OperationID: "evidence-v1", Command: "true", CWD: "/", Evidence: &evidence.Contract{VerificationKind: evidence.VerificationTest}})
	if err == nil {
		t.Fatal("protocol v1 evidence accepted")
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestTypedProjectCommandRejectsCompetingRawEvidenceBeforeBind(t *testing.T) {
	owner := &fakeOwner{}
	svc := app.NewService(nil, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "typed-raw-evidence", WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test", Params: map[string]string{}, Evidence: &evidence.Contract{VerificationKind: evidence.VerificationTest}})
	if !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestV2DeclaredIntentPersistsForTerminalEvidenceAuthority(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	no := false
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "intent-evidence-authority", Command: "true", CWD: "/", Intent: &operation.DeclaredIntent{Kind: operation.IntentKindTest, MutatesSource: &no}}
	started, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, started.SessionID)
	stored, err := store.LoadOperation(context.Background(), "intent-evidence-authority")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Intent == nil || stored.Intent.Kind != operation.IntentKindTest || stored.Intent.MutatesSource == nil || *stored.Intent.MutatesSource {
		t.Fatalf("stored intent=%#v", stored.Intent)
	}
}
