package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func TestReservePersistentBindingRequiresPersistentReservationAndReplaysExact(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	want := persistentBinding("persistent-session-a", "persistent-op-a", "dev-server", now)
	if _, created, got := r.ReservePersistentBinding(context.Background(), want); got.Err == nil || created {
		t.Fatalf("binding without reservation created=%v result=%#v", created, got)
	}
	reservePersistentOperation(t, r, want.SessionID, want.OperationID, want.SessionName, now)

	stored, created, got := r.ReservePersistentBinding(context.Background(), want)
	if got.Err != nil || !created {
		t.Fatalf("first binding created=%v result=%#v", created, got)
	}
	if !reflect.DeepEqual(stored, want) {
		t.Fatalf("stored=%#v want=%#v", stored, want)
	}
	replayed, created, got := r.ReservePersistentBinding(context.Background(), want)
	if got.Err != nil || created || !reflect.DeepEqual(replayed, want) {
		t.Fatalf("replay created=%v stored=%#v result=%#v", created, replayed, got)
	}
	loaded, err := r.LoadPersistentBinding(context.Background(), operation.SessionID(want.SessionID))
	if err != nil || !reflect.DeepEqual(loaded, want) {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
	found, ok, err := r.FindPersistentBinding(context.Background(), operation.SessionID(want.SessionID))
	if err != nil || !ok || !reflect.DeepEqual(found, want) {
		t.Fatalf("find ok=%v found=%#v err=%v", ok, found, err)
	}
	byName, ok, err := r.FindPersistentBindingByName(context.Background(), want.SessionName)
	if err != nil || !ok || !reflect.DeepEqual(byName, want) {
		t.Fatalf("find by name ok=%v found=%#v err=%v", ok, byName, err)
	}
	if _, ok, err := r.FindPersistentBindingByName(context.Background(), "missing-name"); err != nil || ok {
		t.Fatalf("missing name ok=%v err=%v", ok, err)
	}
}

func TestReservePersistentBindingRejectsReservationMismatchAndPermanentNameCollision(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	first := persistentBinding("persistent-session-a", "persistent-op-a", "dev-server", now)
	reservePersistentOperation(t, r, first.SessionID, first.OperationID, first.SessionName, now)
	if _, created, got := r.ReservePersistentBinding(context.Background(), first); got.Err != nil || !created {
		t.Fatalf("first created=%v result=%#v", created, got)
	}

	mismatched := first
	mismatched.SupervisorGenerationID = "generation-other"
	if _, created, got := r.ReservePersistentBinding(context.Background(), mismatched); !errors.Is(got.Err, failure.SupervisorStateConflict) || created {
		t.Fatalf("generation mismatch created=%v result=%#v", created, got)
	}

	second := persistentBinding("persistent-session-b", "persistent-op-b", first.SessionName, now.Add(time.Second))
	reservePersistentOperation(t, r, second.SessionID, second.OperationID, second.SessionName, now.Add(time.Second))
	if _, created, got := r.ReservePersistentBinding(context.Background(), second); !errors.Is(got.Err, failure.PersistentSessionNameConflict) || created {
		t.Fatalf("name collision created=%v result=%#v", created, got)
	}

	wrongName := persistentBinding("persistent-session-c", "persistent-op-c", "other-name", now.Add(2*time.Second))
	reservePersistentOperation(t, r, wrongName.SessionID, wrongName.OperationID, "frozen-name", now.Add(2*time.Second))
	if _, created, got := r.ReservePersistentBinding(context.Background(), wrongName); got.Err == nil || created {
		t.Fatalf("reservation name mismatch created=%v result=%#v", created, got)
	}
}

func TestPersistentBindingPrivateFilesRejectCorruptionAndSymlinkReplacement(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	now := time.Date(2026, 8, 16, 3, 0, 0, 0, time.UTC)
	want := persistentBinding("persistent-session-a", "persistent-op-a", "dev-server", now)
	reservePersistentOperation(t, r, want.SessionID, want.OperationID, want.SessionName, now)
	if _, created, got := r.ReservePersistentBinding(context.Background(), want); got.Err != nil || !created {
		t.Fatalf("created=%v result=%#v", created, got)
	}

	nameEntries, err := os.ReadDir(filepath.Join(root, "persistent-sessions", "names"))
	if err != nil || len(nameEntries) != 1 {
		t.Fatalf("name entries=%v err=%v", nameEntries, err)
	}
	namePath := filepath.Join(root, "persistent-sessions", "names", nameEntries[0].Name())
	if err := os.WriteFile(namePath, []byte("{corrupt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second := persistentBinding("persistent-session-b", "persistent-op-b", want.SessionName, now.Add(time.Second))
	reservePersistentOperation(t, r, second.SessionID, second.OperationID, second.SessionName, now.Add(time.Second))
	if _, created, got := r.ReservePersistentBinding(context.Background(), second); got.Err == nil || created {
		t.Fatalf("corrupt name claim overwritten created=%v result=%#v", created, got)
	}

	bindingPath := filepath.Join(root, "persistent-sessions", "bindings", want.SessionID+".json")
	copyPath := filepath.Join(t.TempDir(), "binding.json")
	data, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bindingPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(copyPath, bindingPath); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadPersistentBinding(context.Background(), operation.SessionID(want.SessionID)); err == nil {
		t.Fatal("symlink binding accepted")
	}
}

func persistentListingFixture(t *testing.T) (*Repository, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	bindings := []persistent.Binding{
		persistentBinding("persistent-session-c", "persistent-op-c", "gamma", base.Add(2*time.Second)),
		persistentBinding("persistent-session-a", "persistent-op-a", "alpha", base),
		persistentBinding("persistent-session-b", "persistent-op-b", "beta", base.Add(time.Second)),
		persistentBinding("persistent-session-d", "persistent-op-d", "delta", base.Add(2*time.Second)),
		persistentBinding("persistent-session-e", "persistent-op-e", "epsilon", base.Add(3*time.Second)),
	}
	bindings[1].ActivityID, bindings[1].WorkspaceID = "activity-a", "ws_01K00000000000000000000000"
	bindings[2].ActivityID, bindings[2].WorkspaceID = "activity-a", "ws_01K00000000000000000000000"
	for _, binding := range bindings {
		reservePersistentOperationWithMetadata(t, r, binding, binding.CreatedAt)
		if _, created, got := r.ReservePersistentBinding(context.Background(), binding); got.Err != nil || !created {
			t.Fatalf("reserve %s created=%v result=%#v", binding.SessionID, created, got)
		}
	}
	lostBinding := bindings[3]
	lostBinding.Lifecycle = persistent.LifecycleLost
	lostBinding.UpdatedAt = lostBinding.UpdatedAt.Add(time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), lostBinding); got.Err != nil {
		t.Fatalf("advance lost binding: %#v", got)
	}
	return r, root
}

func TestListPersistentBindingsPaginatesDeterministically(t *testing.T) {
	r, root := persistentListingFixture(t)
	request := persistent.InspectRequest{PersistentOnly: true, Limit: 2}
	first, err := r.ListPersistentBindings(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := bindingIDs(first); !reflect.DeepEqual(got, []string{"persistent-session-a", "persistent-session-b"}) || first.Continuation == "" {
		t.Fatalf("first ids=%v continuation=%q", got, first.Continuation)
	}
	request.Cursor = first.Continuation
	second, err := r.ListPersistentBindings(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := bindingIDs(second); !reflect.DeepEqual(got, []string{"persistent-session-c", "persistent-session-d"}) || second.Continuation == "" {
		t.Fatalf("second ids=%v continuation=%q", got, second.Continuation)
	}
	request.Cursor = second.Continuation
	third, err := r.ListPersistentBindings(context.Background(), request)
	if err != nil || !reflect.DeepEqual(bindingIDs(third), []string{"persistent-session-e"}) || third.Continuation != "" {
		t.Fatalf("third=%v continuation=%q err=%v", bindingIDs(third), third.Continuation, err)
	}
	reopened, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1024, MaxTotalState: 1 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ListPersistentBindings(context.Background(), persistent.InspectRequest{PersistentOnly: true, Limit: 1, Cursor: first.Continuation}); err != nil {
		t.Fatalf("cursor did not survive reopen: %v", err)
	}
}

func TestListPersistentBindingsFiltersAndBindsCursor(t *testing.T) {
	r, _ := persistentListingFixture(t)
	exact, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{SessionName: "beta", PersistentOnly: true})
	if err != nil || !reflect.DeepEqual(bindingIDs(exact), []string{"persistent-session-b"}) {
		t.Fatalf("exact=%v err=%v", bindingIDs(exact), err)
	}
	activity, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{ActivityID: "activity-a", WorkspaceID: "ws_01K00000000000000000000000", PersistentOnly: true})
	if err != nil || !reflect.DeepEqual(bindingIDs(activity), []string{"persistent-session-a", "persistent-session-b"}) {
		t.Fatalf("activity=%v err=%v", bindingIDs(activity), err)
	}
	lost, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{State: string(persistent.LifecycleLost), PersistentOnly: true})
	if err != nil || !reflect.DeepEqual(bindingIDs(lost), []string{"persistent-session-d"}) {
		t.Fatalf("lost=%v err=%v", bindingIDs(lost), err)
	}
	bound := persistent.InspectRequest{PersistentOnly: true, Limit: 1}
	page, err := r.ListPersistentBindings(context.Background(), bound)
	if err != nil || page.Continuation == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if _, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{SessionName: "alpha", PersistentOnly: true, Limit: 1, Cursor: page.Continuation}); failure.Public(err).Code != failure.InvalidInput {
		t.Fatalf("cursor accepted under changed filter: %v", err)
	}
	tampered := page.Continuation[:len(page.Continuation)-1] + "x"
	if _, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{PersistentOnly: true, Limit: 1, Cursor: tampered}); failure.Public(err).Code != failure.InvalidInput {
		t.Fatalf("tampered cursor accepted: %v", err)
	}
	if _, err := r.ListPersistentBindings(context.Background(), persistent.InspectRequest{PersistentOnly: true, Limit: persistent.MaxInspectRows + 1}); failure.Public(err).Code != failure.InvalidInput {
		t.Fatalf("oversized limit accepted: %v", err)
	}
}

func persistentBinding(sessionID, operationID, name string, at time.Time) persistent.Binding {
	return persistent.Binding{
		SchemaVersion: persistent.SchemaVersion,
		SessionID:     sessionID, OperationID: operationID, SessionName: name, Persistent: true,
		Supervision: persistent.SupervisionPerSession, Continuity: persistent.ContinuityDaemonRestart,
		SupervisorGenerationID: "generation-" + strings.TrimPrefix(sessionID, "persistent-session-"),
		SupervisorEndpointRef:  "endpoint-" + strings.TrimPrefix(sessionID, "persistent-session-"),
		Lifecycle:              persistent.LifecycleProvisioning, CreatedAt: at, UpdatedAt: at,
	}
}

func reservePersistentOperation(t *testing.T, r *Repository, sessionID, operationID, name string, at time.Time) {
	t.Helper()
	reservePersistentOperationWithMetadata(t, r, persistentBinding(sessionID, operationID, name, at), at)
}

func reservePersistentOperationWithMetadata(t *testing.T, r *Repository, binding persistent.Binding, at time.Time) {
	t.Helper()
	reservation := operation.Reservation{
		SchemaVersion: 4, OperationID: operation.ID(binding.OperationID), SessionID: operation.SessionID(binding.SessionID),
		ActivityID: binding.ActivityID, WorkspaceID: binding.WorkspaceID, SessionName: binding.SessionName, Persistent: true,
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "sleep 1", CWD: "/tmp", Shell: "/bin/sh",
		DaemonIncarnation: "daemon", CreatedAt: at,
	}
	if _, created, got := r.ReserveOperation(context.Background(), reservation); got.Err != nil || !created {
		t.Fatalf("reserve operation %s created=%v result=%#v", binding.OperationID, created, got)
	}
}

func bindingIDs(page persistent.BindingPage) []string {
	ids := make([]string, 0, len(page.Bindings))
	for _, item := range page.Bindings {
		ids = append(ids, item.SessionID)
	}
	return ids
}

func TestAdvancePersistentBindingPreservesIdentityAndLifecycleMonotonicity(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	now := time.Date(2026, 8, 16, 3, 30, 0, 0, time.UTC)
	want := persistentBinding("persistent-session-advance", "persistent-op-advance", "advance", now)
	reservePersistentOperation(t, r, want.SessionID, want.OperationID, want.SessionName, now)
	if _, created, got := r.ReservePersistentBinding(context.Background(), want); got.Err != nil || !created {
		t.Fatalf("create=%v result=%#v", created, got)
	}
	live := want
	live.Lifecycle = persistent.LifecycleLive
	live.UpdatedAt = now.Add(time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), live); got.Err != nil {
		t.Fatalf("advance live: %#v", got)
	}
	terminal := live
	terminal.Lifecycle = persistent.LifecycleTerminal
	terminal.UpdatedAt = now.Add(2 * time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), terminal); got.Err != nil {
		t.Fatalf("advance terminal: %#v", got)
	}
	backward := terminal
	backward.Lifecycle = persistent.LifecycleLive
	backward.UpdatedAt = now.Add(3 * time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), backward); !errors.Is(got.Err, failure.SupervisorStateConflict) {
		t.Fatalf("terminal lifecycle reopened: %#v", got)
	}
	changed := terminal
	changed.SupervisorGenerationID = "generation-rebound"
	changed.UpdatedAt = now.Add(3 * time.Second)
	if got := r.AdvancePersistentBinding(context.Background(), changed); !errors.Is(got.Err, failure.SupervisorStateConflict) {
		t.Fatalf("generation rebound accepted: %#v", got)
	}
}
