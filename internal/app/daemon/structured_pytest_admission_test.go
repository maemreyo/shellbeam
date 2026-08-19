package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type pytestCapturePreparerStub struct {
	prepare app.StructuredCapturePreparation
	err     error
	calls   atomic.Int32
}

func (p *pytestCapturePreparerStub) PrepareStructuredCapture(context.Context, app.StructuredCapturePrepareRequest) (app.StructuredCapturePreparation, error) {
	p.calls.Add(1)
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
