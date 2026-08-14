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
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
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

func TestActivityAdmissionCompletesBeforeManagedShellLeaseAndSpawn(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	order := &activityOrderLog{}
	tracker := &orderingActivityTracker{order: order}
	coherence := &orderingActivityCoherence{order: order}
	owner := &orderingActivityOwner{order: order}
	svc := app.NewServiceWithExecutionContextAndCoherence(store, owner, nil, nil, tracker, coherence, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-activity-order", ActivityID: "activity-order", Command: "true", CWD: "/"})
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, started.SessionID)
	got := order.Snapshot()
	if len(got) < 3 || got[0] != "admit" || got[1] != "begin" || got[2] != "spawn" {
		t.Fatalf("order=%v", got)
	}
}

type activityOrderLog struct {
	mu     sync.Mutex
	events []string
}

func (l *activityOrderLog) Add(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}
func (l *activityOrderLog) Snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

type orderingActivityTracker struct{ order *activityOrderLog }

func (t *orderingActivityTracker) Admit(_ context.Context, admission activity.Admission) (activity.Activity, error) {
	t.order.Add("admit")
	return activity.New(admission.ActivityID, time.Now().UTC()), nil
}

type orderingActivityCoherence struct{ order *activityOrderLog }

func (c *orderingActivityCoherence) BeginManagedShell() app.ManagedShellLease {
	c.order.Add("begin")
	return orderingActivityLease{order: c.order}
}
func (c *orderingActivityCoherence) CaptureBarrier() workspace.CoherenceBarrier {
	return workspace.CoherenceBarrier{DaemonIncarnation: "d"}
}

type orderingActivityLease struct{ order *activityOrderLog }

func (l orderingActivityLease) End() { l.order.Add("end") }

type orderingActivityOwner struct{ order *activityOrderLog }

func (o *orderingActivityOwner) Start(context.Context, operation.ExecutionSpec, app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	o.order.Add("spawn")
	return fakeHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}
