package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type pytestCapturePreparerStub struct {
	prepare     app.StructuredCapturePreparation
	err         error
	calls       atomic.Int32
	lastSession operation.SessionID
}

func (p *pytestCapturePreparerStub) PrepareStructuredCapture(_ context.Context, req app.StructuredCapturePrepareRequest) (app.StructuredCapturePreparation, error) {
	p.calls.Add(1)
	p.lastSession = req.SessionID
	return p.prepare, p.err
}
func (*pytestCapturePreparerStub) AbortStructuredCapture(context.Context, operation.ID, operation.SessionID) error {
	return nil
}

type pytestPreconditionOwner struct{ starts atomic.Int32 }

func (o *pytestPreconditionOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.starts.Add(1)
	return pytestPreconditionHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type pytestPreconditionHandle struct{}

func (pytestPreconditionHandle) Write([]byte) error { return nil }
func (pytestPreconditionHandle) CloseStdin() error  { return nil }
func (pytestPreconditionHandle) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (pytestPreconditionHandle) Wait(context.Context) receipt.ExitEvidence {
	code := 0
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (pytestPreconditionHandle) Close() error { return nil }

func openPytestAdmissionStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestExplicitPytestStructuredPreconditionFailsBeforeReservationAndSpawn(t *testing.T) {
	store := openPytestAdmissionStore(t)
	owner := &pytestPreconditionOwner{}
	preparer := &pytestCapturePreparerStub{err: errors.New("PYTEST_ADDOPTS present")}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredCapturePreparer: preparer})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "pytest-precondition-explicit", CWD: "/", StructuredAdapter: "pytest-junit-xml", Argv: []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}}
	_, err := svc.Start(context.Background(), req)
	if err == nil || !errors.Is(err, failure.InvalidInput) {
		t.Fatalf("err=%v", err)
	}
	if owner.starts.Load() != 0 || preparer.calls.Load() != 1 {
		t.Fatalf("starts=%d prepare=%d", owner.starts.Load(), preparer.calls.Load())
	}
	if _, loadErr := store.LoadOperation(context.Background(), operation.ID(req.OperationID)); loadErr == nil {
		t.Fatal("precondition failure reserved operation")
	}
}

func TestAutoPytestCandidateUnqualifiedFallsBackToOrdinaryExecution(t *testing.T) {
	store := openPytestAdmissionStore(t)
	owner := &pytestPreconditionOwner{}
	preparer := &pytestCapturePreparerStub{err: errors.New("producer authority unqualified")}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredCapturePreparer: preparer})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "pytest-precondition-auto", CWD: "/", Argv: []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, YieldMS: 100}
	view, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 1 || preparer.calls.Load() != 1 {
		t.Fatalf("starts=%d prepare=%d", owner.starts.Load(), preparer.calls.Load())
	}
	stored, loadErr := store.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if stored.StructuredAdapter != "" || stored.StructuredCaptureDigest != "" {
		t.Fatalf("stored=%#v", stored)
	}
	if view.SessionID == "" {
		t.Fatalf("view=%#v", view)
	}
}

func TestQualifiedPytestStructuredReplayPreservesCaptureDigestBinding(t *testing.T) {
	store := openPytestAdmissionStore(t)
	owner := &pytestPreconditionOwner{}
	digest := strings.Repeat("a", 64)
	preparer := &pytestCapturePreparerStub{prepare: app.StructuredCapturePreparation{AdapterID: "pytest-junit-xml", CaptureDigest: digest, Owned: true}}
	svc := app.NewService(store, owner, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredCapturePreparer: preparer})
	req := app.StartRequest{
		ProtocolVersion: 2, OperationID: "pytest-capture-replay", CWD: "/", StructuredAdapter: "pytest-junit-xml",
		Argv: []string{"pytest", "test_example.py", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, YieldMS: 100,
	}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID == "" {
		t.Fatalf("first=%#v", first)
	}
	stored, err := store.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.StructuredCaptureDigest != digest {
		t.Fatalf("stored capture digest=%q want=%q", stored.StructuredCaptureDigest, digest)
	}

	replayed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != first.SessionID || owner.starts.Load() != 1 || preparer.calls.Load() != 1 {
		t.Fatalf("replay=%#v first=%#v starts=%d prepares=%d", replayed, first, owner.starts.Load(), preparer.calls.Load())
	}
}

func TestDecisionExperimentPytestReplayPreservesExperimentAndCaptureBinding(t *testing.T) {
	store := openPytestAdmissionStore(t)
	setupDaemonDecisionExperiment(t, store, "exp-pytest-decision")
	owner := &pytestPreconditionOwner{}
	digest := strings.Repeat("b", 64)
	preparer := &pytestCapturePreparerStub{prepare: app.StructuredCapturePreparation{AdapterID: "pytest-junit-xml", CaptureDigest: digest, Owned: true}}
	resolver := &fakeAddressResolver{workspaceID: workspace.WorkspaceID(dpDaemonWorkspaceID), logicalCWD: "src", cwd: "/repo/src"}
	svc := app.NewServiceWithWorkspaceResolver(store, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredCapturePreparer: preparer})
	req := app.StartRequest{
		ProtocolVersion: 2, OperationID: "op-pytest-decision", WorkspaceID: dpDaemonWorkspaceID, ExperimentID: "exp-pytest-decision", CWD: "src", StructuredAdapter: "pytest-junit-xml",
		Argv: []string{"pytest", "test_example.py", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, YieldMS: 100,
	}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, first.SessionID)
	replay, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("same experiment + capture replay failed: %v", err)
	}
	if replay.SessionID != first.SessionID || owner.starts.Load() != 1 || preparer.calls.Load() != 1 {
		t.Fatalf("first=%s replay=%s starts=%d prepares=%d", first.SessionID, replay.SessionID, owner.starts.Load(), preparer.calls.Load())
	}
}

type resolvingExperimentStore struct {
	*storeadapter.Repository
	session operation.SessionID
}

func (s *resolvingExperimentStore) ResolveExperimentAdmissionSession(context.Context, dp.ExperimentID, operation.ID) (operation.SessionID, bool, error) {
	return s.session, true, nil
}

func TestDecisionExperimentStructuredCaptureResolvesSessionBeforePreparation(t *testing.T) {
	repository := openPytestAdmissionStore(t)
	setupDaemonDecisionExperiment(t, repository, "exp-pytest-session")
	store := &resolvingExperimentStore{Repository: repository, session: "recovered-session"}
	owner := &pytestPreconditionOwner{}
	preparer := &pytestCapturePreparerStub{prepare: app.StructuredCapturePreparation{AdapterID: "pytest-junit-xml", CaptureDigest: strings.Repeat("c", 64), Owned: true}}
	resolver := &fakeAddressResolver{workspaceID: workspace.WorkspaceID(dpDaemonWorkspaceID), logicalCWD: "src", cwd: "/repo/src"}
	svc := app.NewServiceWithWorkspaceResolver(store, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100, StructuredCapturePreparer: preparer})
	req := app.StartRequest{
		ProtocolVersion: 2, OperationID: "op-pytest-session", WorkspaceID: dpDaemonWorkspaceID, ExperimentID: "exp-pytest-session", CWD: "src", StructuredAdapter: "pytest-junit-xml",
		Argv: []string{"pytest", "test_example.py", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, YieldMS: 100,
	}
	view, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if preparer.lastSession != store.session || operation.SessionID(view.SessionID) != store.session {
		t.Fatalf("capture session=%q view session=%q want=%q", preparer.lastSession, view.SessionID, store.session)
	}
}
