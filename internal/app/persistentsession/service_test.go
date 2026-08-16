package persistentsession

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestEnsureCreatesBindingBeforeLaunchAndMarksLiveOnlyAfterReady(t *testing.T) {
	store := &fakeBindingStore{}
	launcher := &fakeLauncher{status: Status{SessionID: "persistent-session-a", GenerationID: "generation-a", State: session.Running, PID: 4242, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}}
	now := time.Date(2026, 8, 16, 6, 0, 0, 0, time.UTC)
	service := NewService(store, launcher, Options{
		Limits: Limits{MaxOutputBytes: 1024, MaxQueuedInputBytes: 128, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGrace: 25 * time.Millisecond},
		Now:    func() time.Time { return now }, NewGeneration: func() string { return "generation-a" }, NewEndpointRef: func() string { return "endpoint-a" },
	})
	reservation := persistentReservation("persistent-session-a", "persistent-op-a", "dev-server", now)
	result, err := service.Ensure(context.Background(), reservation, persistentSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.actions, []string{"find", "reserve", "advance"}) || !reflect.DeepEqual(launcher.actions, []string{"ensure"}) {
		t.Fatalf("store=%v launcher=%v", store.actions, launcher.actions)
	}
	if launcher.bindingAtEnsure.Lifecycle != core.LifecycleProvisioning || store.binding.Lifecycle != core.LifecycleLive {
		t.Fatalf("launch binding=%#v stored=%#v", launcher.bindingAtEnsure, store.binding)
	}
	if result.Binding.Lifecycle != core.LifecycleLive || result.Status.PID != 4242 || result.Attachment == nil {
		t.Fatalf("result=%#v", result)
	}
}

func TestEnsureRetryUsesExistingGenerationAndLauncherNeverGetsReplacementBinding(t *testing.T) {
	now := time.Date(2026, 8, 16, 7, 0, 0, 0, time.UTC)
	store := &fakeBindingStore{binding: core.Binding{
		SchemaVersion: core.SchemaVersion, SessionID: "persistent-session-a", OperationID: "persistent-op-a", SessionName: "dev-server", Persistent: true,
		Supervision: core.SupervisionPerSession, Continuity: core.ContinuityDaemonRestart, SupervisorGenerationID: "generation-existing", SupervisorEndpointRef: "endpoint-existing",
		Lifecycle: core.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now,
	}}
	launcher := &fakeLauncher{status: Status{SessionID: "persistent-session-a", GenerationID: "generation-existing", State: session.Running, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}}
	service := NewService(store, launcher, Options{
		Limits: Limits{MaxOutputBytes: 1024, MaxQueuedInputBytes: 128, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGrace: time.Millisecond},
		Now:    func() time.Time { return now.Add(time.Second) }, NewGeneration: func() string { return "generation-new-must-not-be-used" }, NewEndpointRef: func() string { return "endpoint-new-must-not-be-used" },
	})
	reservation := persistentReservation("persistent-session-a", "persistent-op-a", "dev-server", now)
	first, err := service.Ensure(context.Background(), reservation, persistentSpec())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Ensure(context.Background(), reservation, persistentSpec())
	if err != nil {
		t.Fatal(err)
	}
	if first.Binding.SupervisorGenerationID != "generation-existing" || second.Binding.SupervisorGenerationID != "generation-existing" {
		t.Fatalf("bindings first=%#v second=%#v", first.Binding, second.Binding)
	}
	for _, binding := range launcher.bindings {
		if binding.SupervisorGenerationID != "generation-existing" {
			t.Fatalf("launcher received replacement binding: %#v", binding)
		}
	}
}

func TestEnsureRejectsUnreadyWrongIdentityLostAndTTYWithoutAdvancingLive(t *testing.T) {
	now := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	reservation := persistentReservation("persistent-session-a", "persistent-op-a", "dev-server", now)
	for name, status := range map[string]Status{
		"starting":         {SessionID: string(reservation.SessionID), GenerationID: "generation-a", State: session.Starting},
		"wrong_session":    {SessionID: "persistent-session-other", GenerationID: "generation-a", State: session.Running},
		"wrong_generation": {SessionID: string(reservation.SessionID), GenerationID: "generation-other", State: session.Running},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeBindingStore{}
			launcher := &fakeLauncher{status: status}
			service := testService(store, launcher, now)
			if _, err := service.Ensure(context.Background(), reservation, persistentSpec()); err == nil {
				t.Fatal("unready or mismatched supervisor accepted")
			}
			if store.binding.Lifecycle == core.LifecycleLive {
				t.Fatal("binding advanced live without authenticated readiness")
			}
		})
	}

	lostStore := &fakeBindingStore{binding: persistentBindingForReservation(reservation, "generation-a", "endpoint-a", now, core.LifecycleLost)}
	if _, err := testService(lostStore, &fakeLauncher{}, now).Ensure(context.Background(), reservation, persistentSpec()); !errors.Is(err, failure.PersistentSessionOwnershipLost) {
		t.Fatalf("lost binding err=%v", err)
	}
	tty := persistentSpec()
	tty.TTY = true
	if _, err := testService(&fakeBindingStore{}, &fakeLauncher{}, now).Ensure(context.Background(), reservation, tty); err == nil {
		t.Fatal("persistent tty accepted")
	}
}

func TestReattachReusesExistingGenerationAndAdvancesOnlyLiveProof(t *testing.T) {
	now := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	reservation := persistentReservation("persistent-session-r", "persistent-op-r", "dev-r", now)
	binding := persistentBindingForReservation(reservation, "generation-r", "endpoint-r", now, core.LifecycleProvisioning)
	store := &fakeBindingStore{binding: binding}
	launcher := &fakeLauncher{reattachStatus: Status{SessionID: binding.SessionID, GenerationID: binding.SupervisorGenerationID, State: session.Running, PID: 5151, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}}
	service := testService(store, launcher, now)
	result, err := service.Reattach(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if result.Binding.Lifecycle != core.LifecycleLive || store.binding.Lifecycle != core.LifecycleLive || result.Status.PID != 5151 {
		t.Fatalf("result=%#v stored=%#v", result, store.binding)
	}
	if !reflect.DeepEqual(launcher.actions, []string{"reattach"}) || launcher.bindings[0].SupervisorGenerationID != "generation-r" {
		t.Fatalf("actions=%v bindings=%#v", launcher.actions, launcher.bindings)
	}
}

func TestReattachTerminalProofLeavesProvisioningForTerminalReconciliation(t *testing.T) {
	now := time.Date(2026, 8, 16, 9, 30, 0, 0, time.UTC)
	reservation := persistentReservation("persistent-session-terminal-r", "persistent-op-terminal-r", "terminal-r", now)
	binding := persistentBindingForReservation(reservation, "generation-terminal-r", "endpoint-terminal-r", now, core.LifecycleProvisioning)
	store := &fakeBindingStore{binding: binding}
	launcher := &fakeLauncher{reattachStatus: Status{SessionID: binding.SessionID, GenerationID: binding.SupervisorGenerationID, State: session.Completed, Outcome: session.Success}}
	result, err := testService(store, launcher, now).Reattach(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status.State != session.Completed || result.Binding.Lifecycle != core.LifecycleProvisioning || store.binding.Lifecycle != core.LifecycleProvisioning {
		t.Fatalf("result=%#v stored=%#v", result, store.binding)
	}
}

func testService(store *fakeBindingStore, launcher *fakeLauncher, now time.Time) *Service {
	return NewService(store, launcher, Options{
		Limits: Limits{MaxOutputBytes: 1024, MaxQueuedInputBytes: 128, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGrace: time.Millisecond},
		Now:    func() time.Time { return now.Add(time.Second) }, NewGeneration: func() string { return "generation-a" }, NewEndpointRef: func() string { return "endpoint-a" },
	})
}

func persistentReservation(sessionID, operationID, name string, at time.Time) operation.Reservation {
	return operation.Reservation{
		SchemaVersion: 4, SessionID: operation.SessionID(sessionID), OperationID: operation.ID(operationID), SessionName: name, Persistent: true,
		RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ExecutionFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", Shell: "/bin/sh", CreatedAt: at,
	}
}

func persistentSpec() operation.ExecutionSpec {
	return operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"}
}

func persistentBindingForReservation(res operation.Reservation, generation, endpoint string, at time.Time, lifecycle core.Lifecycle) core.Binding {
	return core.Binding{
		SchemaVersion: core.SchemaVersion, SessionID: string(res.SessionID), OperationID: string(res.OperationID), ActivityID: res.ActivityID, WorkspaceID: res.WorkspaceID, SessionName: res.SessionName,
		Persistent: true, Supervision: core.SupervisionPerSession, Continuity: core.ContinuityDaemonRestart,
		SupervisorGenerationID: generation, SupervisorEndpointRef: endpoint, Lifecycle: lifecycle, CreatedAt: res.CreatedAt, UpdatedAt: at,
	}
}

type fakeBindingStore struct {
	mu      sync.Mutex
	binding core.Binding
	actions []string
}

func (s *fakeBindingStore) Find(_ context.Context, sessionID operation.SessionID) (core.Binding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = append(s.actions, "find")
	if s.binding.SessionID == "" {
		return core.Binding{}, false, nil
	}
	return s.binding, s.binding.SessionID == string(sessionID), nil
}
func (s *fakeBindingStore) Reserve(_ context.Context, want core.Binding) (core.Binding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = append(s.actions, "reserve")
	if s.binding.SessionID == "" {
		s.binding = want
		return want, true, nil
	}
	return s.binding, false, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": want.SessionID}, nil)
}
func (s *fakeBindingStore) Advance(_ context.Context, want core.Binding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = append(s.actions, "advance")
	s.binding = want
	return nil
}

type fakeAttachment struct{}

func (*fakeAttachment) Write([]byte) error                        { return nil }
func (*fakeAttachment) CloseStdin() error                         { return nil }
func (*fakeAttachment) Signal(string) receipt.SignalEvidence      { return receipt.SignalEvidence{} }
func (*fakeAttachment) Wait(context.Context) receipt.ExitEvidence { return receipt.ExitEvidence{} }
func (*fakeAttachment) Close() error                              { return nil }
func (*fakeAttachment) PID() int                                  { return 4242 }

type fakeLauncher struct {
	mu              sync.Mutex
	status          Status
	actions         []string
	bindings        []core.Binding
	bindingAtEnsure core.Binding
	err             error
	reattachStatus  Status
	reattachErr     error
}

func (l *fakeLauncher) Ensure(_ context.Context, request LaunchRequest) (Attachment, Status, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.actions = append(l.actions, "ensure")
	l.bindings = append(l.bindings, request.Binding)
	l.bindingAtEnsure = request.Binding
	if l.err != nil {
		return nil, Status{}, l.err
	}
	return &fakeAttachment{}, l.status, nil
}

func (l *fakeLauncher) Reattach(_ context.Context, binding core.Binding) (Attachment, Status, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.actions = append(l.actions, "reattach")
	l.bindings = append(l.bindings, binding)
	if l.reattachErr != nil {
		return nil, Status{}, l.reattachErr
	}
	return &fakeAttachment{}, l.reattachStatus, nil
}
