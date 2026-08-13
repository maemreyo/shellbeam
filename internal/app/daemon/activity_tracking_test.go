package daemon_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	activity "github.com/maemreyo/shellbeam/internal/core/activity"
)

func TestActivityLazyAdmitOccursOnceAfterAcceptedOperation(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	tracker := &fakeActivityTracker{}
	owner := &fakeOwner{}
	svc := app.NewServiceWithActivityTracker(store, owner, tracker, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "op-activity", ActivityID: "ZMR-111-validator", Command: "true", CWD: "/"}
	started, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if tracker.CallCount() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("tracker=%d starts=%d", tracker.CallCount(), owner.starts.Load())
	}
	if _, err := svc.Start(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if tracker.CallCount() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("replay tracker=%d starts=%d", tracker.CallCount(), owner.starts.Load())
	}
	result, err := terminal.StructuredResult()
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation.ActivityID != "ZMR-111-validator" {
		t.Fatalf("result=%#v", result.Operation)
	}
	admission := tracker.Last()
	if admission.ActivityID != activity.ID("ZMR-111-validator") || admission.OperationID != "op-activity" || admission.SessionID != started.SessionID {
		t.Fatalf("admission=%#v", admission)
	}
}

func TestActivityInvalidIDFailsBeforeReservationOrSpawn(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	tracker := &fakeActivityTracker{}
	owner := &fakeOwner{}
	svc := app.NewServiceWithActivityTracker(store, owner, tracker, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-invalid-activity", ActivityID: "bad/activity", Command: "true", CWD: "/"})
	if err == nil {
		t.Fatal("invalid activity accepted")
	}
	if tracker.CallCount() != 0 || owner.starts.Load() != 0 {
		t.Fatalf("tracker=%d starts=%d", tracker.CallCount(), owner.starts.Load())
	}
	if _, err := store.LoadOperation(context.Background(), "op-invalid-activity"); err == nil {
		t.Fatal("invalid activity reserved operation")
	}
}

type fakeActivityTracker struct {
	mu         sync.Mutex
	admissions []activity.Admission
}

func (t *fakeActivityTracker) Admit(_ context.Context, admission activity.Admission) (activity.Activity, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.admissions = append(t.admissions, admission)
	return activity.New(admission.ActivityID, time.Now().UTC()), nil
}
func (t *fakeActivityTracker) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.admissions)
}
func (t *fakeActivityTracker) Last() activity.Admission {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.admissions[len(t.admissions)-1]
}
