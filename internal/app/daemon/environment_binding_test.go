package daemon_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type fakeCachedEnvironmentBindings struct {
	calls    int
	binding  environment.Binding
	ok       bool
	requests []operation.Reservation
}

func (f *fakeCachedEnvironmentBindings) CachedEnvironmentBinding(reservation operation.Reservation) (environment.Binding, bool) {
	f.calls++
	f.requests = append(f.requests, reservation)
	return f.binding, f.ok
}

func validEnvironmentBinding(t *testing.T) environment.Binding {
	t.Helper()
	input := environment.FingerprintInput{
		Platform:         environment.Platform{OS: "darwin", Architecture: "arm64"},
		Execution:        environment.ExecutionContext{Mode: "shell", Identity: "/bin/sh"},
		Path:             environment.PathFingerprint("/bin:/usr/bin"),
		VariablePresence: []environment.VariablePresence{{Name: "CI", Present: true}},
	}
	fingerprint, err := environment.EnvironmentFingerprint(input)
	if err != nil {
		t.Fatal(err)
	}
	captured := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	snapshot := environment.Snapshot{
		SchemaVersion:          environment.SnapshotSchemaVersion,
		CapturedAt:             captured,
		Quality:                environment.QualityComplete,
		EnvironmentFingerprint: fingerprint,
		FingerprintVersion:     environment.FingerprintVersion,
		Platform:               input.Platform,
		Execution:              input.Execution,
		Path:                   input.Path,
		VariablePresence:       input.VariablePresence,
	}
	snapshot.SnapshotID = environment.SnapshotID(captured, fingerprint, "")
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	binding := snapshot.Binding()
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestStartFreezesCachedEnvironmentBindingOnlyOnFirstAdmission(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	wait := make(chan struct{})
	svc := app.NewService(st, &pidOwner{pid: 4243, wait: wait}, app.Options{Incarnation: "env-bind", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	provider := &fakeCachedEnvironmentBindings{binding: validEnvironmentBinding(t), ok: true}
	svc.SetEnvironmentBindingProvider(provider)
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "env-binding-freeze", Command: "sleep", CWD: "/"}
	first, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != second.SessionID {
		t.Fatalf("retry session changed: %q -> %q", first.SessionID, second.SessionID)
	}
	if provider.calls != 1 {
		t.Fatalf("cached binding provider calls=%d want 1", provider.calls)
	}
	if len(provider.requests) != 1 || provider.requests[0].EnvironmentBinding != nil {
		t.Fatalf("provider request already bound: %#v", provider.requests)
	}
	provider.binding = environment.Binding{}
	provider.ok = false
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("replay reread mutable environment cache: calls=%d", provider.calls)
	}
	close(wait)
}

func TestStartDoesNotRecheckEnvironmentWhenFirstAdmissionHasNoCachedBinding(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	wait := make(chan struct{})
	svc := app.NewService(st, &pidOwner{pid: 4244, wait: wait}, app.Options{Incarnation: "env-miss", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	provider := &fakeCachedEnvironmentBindings{ok: false}
	svc.SetEnvironmentBindingProvider(provider)
	request := app.StartRequest{ProtocolVersion: 2, OperationID: "env-binding-miss", Command: "sleep", CWD: "/"}
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	provider.binding = validEnvironmentBinding(t)
	provider.ok = true
	if _, err := svc.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("cache miss replay was re-observed: calls=%d", provider.calls)
	}
	close(wait)
}

func TestTypedProjectCommandFreezesEnvironmentBindingOnce(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	commandBinding := daemonProjectBinding(t, []string{"go", "test", "-json", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: commandBinding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{
		Incarnation: "typed-env", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
		ProjectCommandBinder: binder,
	})
	provider := &fakeCachedEnvironmentBindings{binding: validEnvironmentBinding(t), ok: true}
	svc.SetEnvironmentBindingProvider(provider)

	req := typedStartRequest("typed-env-binding", "./internal/app")
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, first.SessionID)
	if provider.calls != 1 {
		t.Fatalf("cached binding provider calls=%d want 1", provider.calls)
	}
	stored, err := store.LoadOperation(context.Background(), "typed-env-binding")
	if err != nil {
		t.Fatal(err)
	}
	if stored.EnvironmentBinding == nil || stored.EnvironmentBinding.EnvironmentFingerprint != provider.binding.EnvironmentFingerprint {
		t.Fatalf("stored environment binding=%#v", stored.EnvironmentBinding)
	}

	provider.binding = environment.Binding{}
	provider.ok = false
	replayed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.SessionID != first.SessionID {
		t.Fatalf("typed replay session changed: %q -> %q", first.SessionID, replayed.SessionID)
	}
	if provider.calls != 1 {
		t.Fatalf("typed replay reread environment cache: calls=%d", provider.calls)
	}
}
