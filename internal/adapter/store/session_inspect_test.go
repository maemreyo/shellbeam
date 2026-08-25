package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestListSessionSummariesUsesReconciledActivityReservationIndexWithoutScanningUnrelatedHistory(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 22, 1, 0, 0, 0, time.UTC)
	binding := persistentBinding("activity-index-session", "activity-index-op", "activity-index", base)
	binding.ActivityID = "activity-index-fast"
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("persistent binding created=%v result=%#v", created, result)
	}
	if result := r.AdvanceSession(context.Background(), session.Snapshot{
		SchemaVersion: 1, OperationID: binding.OperationID, SessionID: binding.SessionID,
		DaemonIncarnation: "daemon-a", State: session.Running, OutputAvailable: true, UpdatedAt: base.Add(time.Second),
	}); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := r.repairCommittedOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r.root, "operations", "unrelated-history"), 0700); err != nil {
		t.Fatal(err)
	}

	page, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{ActivityID: binding.ActivityID, Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(page); !reflect.DeepEqual(got, []string{binding.SessionID}) {
		t.Fatalf("activity session ids=%v", got)
	}
}

func TestListSessionSummariesActivityIndexTracksReservationsCreatedAfterReconcile(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	if err := r.repairCommittedOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 22, 2, 0, 0, 0, time.UTC)
	binding := persistentBinding("activity-index-late-session", "activity-index-late-op", "activity-index-late", base)
	binding.ActivityID = "activity-index-late"
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("persistent binding created=%v result=%#v", created, result)
	}
	if result := r.AdvanceSession(context.Background(), session.Snapshot{
		SchemaVersion: 1, OperationID: binding.OperationID, SessionID: binding.SessionID,
		DaemonIncarnation: "daemon-a", State: session.Running, OutputAvailable: true, UpdatedAt: base.Add(time.Second),
	}); result.Err != nil {
		t.Fatal(result.Err)
	}
	if err := os.Mkdir(filepath.Join(r.root, "operations", "unrelated-history"), 0700); err != nil {
		t.Fatal(err)
	}

	page, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{ActivityID: binding.ActivityID, Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(page); !reflect.DeepEqual(got, []string{binding.SessionID}) {
		t.Fatalf("late activity session ids=%v", got)
	}
}

func TestListSessionSummariesDefaultsToPersistentAndIncludesDirectOnlyWhenRequested(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)

	binding := persistentBinding("persistent-inspect-session", "persistent-inspect-op", "dev-server", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("persistent binding created=%v result=%#v", created, result)
	}
	if result := r.AdvanceSession(context.Background(), session.Snapshot{
		SchemaVersion: 1, OperationID: binding.OperationID, SessionID: binding.SessionID,
		DaemonIncarnation: "daemon-a", State: session.Running, OutputAvailable: true, UpdatedAt: base.Add(time.Second),
	}); result.Err != nil {
		t.Fatal(result.Err)
	}

	direct := operation.Reservation{
		SchemaVersion: 2, OperationID: "direct-inspect-op", SessionID: "direct-inspect-session",
		RequestFingerprint:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExecutionFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ExecutionMode:        operation.ExecutionModeShell, Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", Shell: "/bin/sh",
		DaemonIncarnation: "daemon-a", CreatedAt: base.Add(time.Second),
	}
	if _, created, result := r.ReserveOperation(context.Background(), direct); result.Err != nil || !created {
		t.Fatalf("direct created=%v result=%#v", created, result)
	}
	if result := r.AdvanceSession(context.Background(), session.Snapshot{
		SchemaVersion: 1, OperationID: string(direct.OperationID), SessionID: string(direct.SessionID),
		DaemonIncarnation: "daemon-a", State: session.Running, OutputAvailable: true, UpdatedAt: base.Add(2 * time.Second),
	}); result.Err != nil {
		t.Fatal(result.Err)
	}

	defaults, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(defaults); !reflect.DeepEqual(got, []string{binding.SessionID}) {
		t.Fatalf("default ids=%v", got)
	}
	if defaults.Sessions[0].OwnershipStatus != persistent.OwnershipLost {
		t.Fatalf("canonical nonterminal ownership=%q", defaults.Sessions[0].OwnershipStatus)
	}

	all, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{PersistentOnly: persistentBool(false)})
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(all); !reflect.DeepEqual(got, []string{binding.SessionID, string(direct.SessionID)}) {
		t.Fatalf("all ids=%v", got)
	}
	if all.Sessions[1].Persistent || all.Sessions[1].SessionName != "" || all.Sessions[1].OwnershipStatus != persistent.OwnershipLost {
		t.Fatalf("direct summary=%#v", all.Sessions[1])
	}
}

func summarySessionIDs(page persistent.InspectPage) []string {
	out := make([]string, len(page.Sessions))
	for i := range page.Sessions {
		out[i] = page.Sessions[i].SessionID
	}
	return out
}

func TestListSessionSummariesFiltersPaginationFrozenCutAndCursorBinding(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 6, 0, 0, 0, time.UTC)
	workspaceID := "ws_01K00000000000000000000000"

	seed := func(sessionID, operationID, name, activity string, at time.Time) {
		t.Helper()
		binding := persistentBinding(sessionID, operationID, name, at)
		binding.ActivityID = activity
		binding.WorkspaceID = workspaceID
		reservePersistentOperationWithMetadata(t, r, binding, at)
		if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
			t.Fatalf("reserve %s created=%v result=%#v", sessionID, created, result)
		}
		live := binding
		live.Lifecycle = persistent.LifecycleLive
		live.UpdatedAt = at.Add(time.Second)
		if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
			t.Fatal(result.Err)
		}
		if result := r.AdvanceSession(context.Background(), session.Snapshot{
			SchemaVersion: 1, OperationID: operationID, SessionID: sessionID, DaemonIncarnation: "daemon-a",
			State: session.Running, OutputAvailable: true, UpdatedAt: at.Add(2 * time.Second),
		}); result.Err != nil {
			t.Fatal(result.Err)
		}
	}

	seed("persistent-filter-a", "persistent-filter-op-a", "filter-a", "activity-a", base)
	seed("persistent-filter-b", "persistent-filter-op-b", "filter-b", "activity-a", base.Add(time.Second))
	seed("persistent-filter-c", "persistent-filter-op-c", "filter-c", "activity-b", base.Add(2*time.Second))

	request := persistent.InspectRequest{
		ActivityID: "activity-a", WorkspaceID: workspaceID, State: string(session.Running), Limit: 1,
	}
	first, err := r.ListSessionSummaries(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(first); !reflect.DeepEqual(got, []string{"persistent-filter-a"}) || first.Continuation == "" {
		t.Fatalf("first=%#v ids=%v", first, got)
	}

	seed("persistent-filter-d", "persistent-filter-op-d", "filter-d", "activity-a", base.Add(10*time.Second))
	request.Cursor = first.Continuation
	second, err := r.ListSessionSummaries(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(second); !reflect.DeepEqual(got, []string{"persistent-filter-b"}) || second.Continuation != "" {
		t.Fatalf("frozen second=%#v ids=%v", second, got)
	}

	name, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{SessionName: "filter-b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := summarySessionIDs(name); !reflect.DeepEqual(got, []string{"persistent-filter-b"}) {
		t.Fatalf("name filter ids=%v", got)
	}

	mismatched := request
	mismatched.State = string(session.Completed)
	if _, err := r.ListSessionSummaries(context.Background(), mismatched); err == nil {
		t.Fatal("cursor accepted under different filters")
	}

	tampered := request
	token := []byte(first.Continuation)
	if token[len(token)-1] == 'A' {
		token[len(token)-1] = 'B'
	} else {
		token[len(token)-1] = 'A'
	}
	tampered.Cursor = string(token)
	if _, err := r.ListSessionSummaries(context.Background(), tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}

func TestListSessionSummariesTerminalPersistentReservationWithoutBindingIsInspectable(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 6, 30, 0, 0, time.UTC)
	binding := persistentBinding("persistent-terminal-no-binding", "persistent-terminal-no-binding-op", "failed-before-binding", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	rec := receipt.Receipt{
		SchemaVersion: 4, OperationID: binding.OperationID, SessionID: binding.SessionID,
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		ObservationBindingFingerprint: strings.Repeat("c", 64), DaemonIncarnation: "daemon-terminal",
		ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/tmp",
		State: session.Failed, Outcome: session.Failure, Persistent: true, SessionName: binding.SessionName, OutputComplete: true,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: false},
	}
	if result := r.PublishTerminal(context.Background(), rec); result.Err != nil {
		t.Fatal(result.Err)
	}
	page, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{SessionName: binding.SessionName})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].OwnershipStatus != persistent.OwnershipTerminal || page.Sessions[0].State != string(session.Failed) {
		t.Fatalf("terminal no-binding summary=%#v", page.Sessions)
	}
}

func TestListSessionSummariesProjectsTerminalReceiptCounters(t *testing.T) {
	r := openRecoveryRepository(t, t.TempDir()+"/state")
	base := time.Date(2026, 8, 16, 7, 0, 0, 0, time.UTC)
	binding := persistentBinding("persistent-terminal-inspect", "persistent-terminal-inspect-op", "terminal-inspect", base)
	reservePersistentOperationWithMetadata(t, r, binding, base)
	if _, created, result := r.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	live := binding
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = base.Add(time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), live); result.Err != nil {
		t.Fatal(result.Err)
	}

	rec := receipt.Receipt{
		SchemaVersion: 4, OperationID: binding.OperationID, SessionID: binding.SessionID,
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		ObservationBindingFingerprint: strings.Repeat("c", 64), DaemonIncarnation: "daemon-terminal",
		ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/tmp",
		State: session.Failed, Outcome: session.Failure, Persistent: true, SessionName: binding.SessionName,
		OutputBytes: 17, OutputComplete: true, InputAcceptedBytes: 9, InputDeliveredBytes: 7,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
	}
	if result := r.PublishTerminal(context.Background(), rec); result.Err != nil {
		t.Fatal(result.Err)
	}
	terminal := live
	terminal.Lifecycle = persistent.LifecycleTerminal
	terminal.UpdatedAt = base.Add(2 * time.Second)
	if result := r.AdvancePersistentBinding(context.Background(), terminal); result.Err != nil {
		t.Fatal(result.Err)
	}

	page, err := r.ListSessionSummaries(context.Background(), persistent.InspectRequest{SessionName: binding.SessionName})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 {
		t.Fatalf("sessions=%#v", page.Sessions)
	}
	got := page.Sessions[0]
	if got.State != string(session.Failed) || got.Outcome != string(session.Failure) || got.OwnershipStatus != persistent.OwnershipTerminal ||
		got.OutputBytes != 17 || got.InputAcceptedBytes != 9 || got.InputDeliveredBytes != 7 {
		t.Fatalf("terminal summary=%#v", got)
	}
}
