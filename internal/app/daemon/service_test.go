package daemon_test

import (
	"context"
	"errors"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"path/filepath"
	"sync/atomic"
	"testing"
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
	got.Features[capability.FeatureWorkspaceAddressing] = capability.Available
	again := svc.CapabilityCatalog()
	if again.Features[capability.FeatureWorkspaceAddressing] != capability.Unavailable {
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
