package daemon_test

import (
	"context"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
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
