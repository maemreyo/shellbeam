package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistentcore "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPersistentSessionCatalogAdvertisesConfiguredBounds(t *testing.T) {
	base := daemonCatalog(capability.Limits{LiveSessions: 4, SessionOutputBytes: 4096})
	got := persistentSessionCatalog(base, 4, 4096, 512)
	if got.Features[capability.FeatureNamedSessions] != capability.Available {
		t.Fatalf("named sessions availability=%q", got.Features[capability.FeatureNamedSessions])
	}
	if got.Limits.PersistentSessions != 4 || got.Limits.PersistentRecoverySpoolBytes != 4096 || got.Limits.PersistentQueuedInputBytes != 512 {
		t.Fatalf("persistent limits=%#v", got.Limits)
	}
	if !got.PersistentNonTTY || got.PersistentTTY || got.PersistentContinuity != "daemon_restart" {
		t.Fatalf("persistent capability projection=%#v", got)
	}
}

func TestPersistentSessionRuntimeCompositionHasNoPrivateRuntimeSideEffects(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	store, err := storeadapter.Open(stateDir, storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 4096, MaxTotalState: 1 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	limits := persistentapp.Limits{
		MaxOutputBytes: 4096, MaxQueuedInputBytes: 512,
		MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8,
		TerminationGrace: 25 * time.Millisecond,
	}
	runtime, err := newPersistentSessionRuntime(store, runtimeRoot, "/bin/echo", limits)
	if err != nil || runtime == nil {
		t.Fatalf("runtime=%v err=%v", runtime, err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "supervisors")); !os.IsNotExist(err) {
		t.Fatalf("composition touched private supervisor runtime: %v", err)
	}
}

func TestDaemonPersistentRuntimeMapsReattachWithoutEnsure(t *testing.T) {
	binding := persistentcore.Binding{SchemaVersion: persistentcore.SchemaVersion, SessionID: "cmd-reattach-session", OperationID: "cmd-reattach-op", SessionName: "cmd-reattach", Persistent: true, Supervision: persistentcore.SupervisionPerSession, Continuity: persistentcore.ContinuityDaemonRestart, SupervisorGenerationID: "cmd-generation", SupervisorEndpointRef: "cmd-endpoint", Lifecycle: persistentcore.LifecycleProvisioning, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	store := &cmdPersistentBindingStore{binding: binding}
	launcher := &cmdPersistentLauncher{status: persistentapp.Status{SessionID: binding.SessionID, GenerationID: binding.SupervisorGenerationID, State: session.Running, PID: 6060, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}}
	service := persistentapp.NewService(store, launcher, persistentapp.Options{Limits: persistentapp.Limits{MaxOutputBytes: 4096, MaxQueuedInputBytes: 512, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGrace: time.Millisecond}})
	runtime := daemonPersistentRuntime{service: service}
	got, err := runtime.Reattach(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if got.Handle == nil || got.PID != 6060 || got.State != session.Running || !got.Spawn.Succeeded {
		t.Fatalf("reattach=%#v", got)
	}
	if launcher.ensureCalls != 0 || launcher.reattachCalls != 1 {
		t.Fatalf("ensure=%d reattach=%d", launcher.ensureCalls, launcher.reattachCalls)
	}
}

type cmdPersistentBindingStore struct{ binding persistentcore.Binding }

func (s *cmdPersistentBindingStore) Find(context.Context, operation.SessionID) (persistentcore.Binding, bool, error) {
	return s.binding, true, nil
}
func (s *cmdPersistentBindingStore) Reserve(context.Context, persistentcore.Binding) (persistentcore.Binding, bool, error) {
	return s.binding, false, nil
}
func (s *cmdPersistentBindingStore) Advance(_ context.Context, binding persistentcore.Binding) error {
	s.binding = binding
	return nil
}

type cmdPersistentAttachment struct{}

func (*cmdPersistentAttachment) Write([]byte) error { return nil }
func (*cmdPersistentAttachment) CloseStdin() error  { return nil }
func (*cmdPersistentAttachment) Signal(string) receipt.SignalEvidence {
	return receipt.SignalEvidence{}
}
func (*cmdPersistentAttachment) Wait(context.Context) receipt.ExitEvidence {
	return receipt.ExitEvidence{}
}
func (*cmdPersistentAttachment) Close() error { return nil }
func (*cmdPersistentAttachment) PID() int     { return 6060 }

type cmdPersistentLauncher struct {
	ensureCalls   int
	reattachCalls int
	status        persistentapp.Status
}

func (l *cmdPersistentLauncher) Ensure(context.Context, persistentapp.LaunchRequest) (persistentapp.Attachment, persistentapp.Status, error) {
	l.ensureCalls++
	return nil, persistentapp.Status{}, nil
}
func (l *cmdPersistentLauncher) Reattach(context.Context, persistentcore.Binding) (persistentapp.Attachment, persistentapp.Status, error) {
	l.reattachCalls++
	return &cmdPersistentAttachment{}, l.status, nil
}

var _ daemonapp.PersistentReattachRuntime = daemonPersistentRuntime{}

func TestReconcilePersistentDaemonStartupReadsActiveIndex(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 4096, MaxTotalState: 1 << 20, ControlReserve: 4096})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	reservation := operation.Reservation{SchemaVersion: 4, OperationID: "cmd-startup-op", SessionID: "cmd-startup-session", RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp", Shell: "/bin/sh", Persistent: true, SessionName: "cmd-startup", DaemonIncarnation: "old-daemon", CreatedAt: now}
	if _, created, result := store.ReserveOperation(context.Background(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	binding := persistentcore.Binding{SchemaVersion: persistentcore.SchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID), SessionName: reservation.SessionName, Persistent: true, Supervision: persistentcore.SupervisionPerSession, Continuity: persistentcore.ContinuityDaemonRestart, SupervisorGenerationID: "cmd-startup-generation", SupervisorEndpointRef: "cmd-startup-endpoint", Lifecycle: persistentcore.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now}
	if _, created, result := store.ReservePersistentBinding(context.Background(), binding); result.Err != nil || !created {
		t.Fatalf("binding created=%v result=%#v", created, result)
	}
	runtime := &cmdStartupRuntime{reattachErr: failure.New(failure.SupervisorAuthFailed, map[string]string{"reason": "proof"}, nil)}
	svc := daemonapp.NewService(store, nil, daemonapp.Options{Incarnation: "new-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 512, PersistentRuntime: runtime})
	if err := reconcilePersistentDaemonStartup(context.Background(), store, svc); err != nil {
		t.Fatal(err)
	}
	if runtime.ensureCalls.Load() != 0 || runtime.reattachCalls.Load() != 1 {
		t.Fatalf("ensure=%d reattach=%d", runtime.ensureCalls.Load(), runtime.reattachCalls.Load())
	}
	stored, err := store.LoadPersistentBinding(context.Background(), reservation.SessionID)
	if err != nil || stored.Lifecycle != persistentcore.LifecycleLost {
		t.Fatalf("binding=%#v err=%v", stored, err)
	}
	rec, err := store.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil || rec.State != session.Abandoned || rec.Outcome != session.Ambiguous || rec.FailureReason != "supervisor_auth_failed" {
		t.Fatalf("receipt=%#v err=%v", rec, err)
	}
}

type cmdStartupRuntime struct {
	ensureCalls   atomic.Int32
	reattachCalls atomic.Int32
	reattachErr   error
}

func (r *cmdStartupRuntime) Ensure(context.Context, operation.Reservation, operation.ExecutionSpec) (daemonapp.PersistentLaunch, error) {
	r.ensureCalls.Add(1)
	return daemonapp.PersistentLaunch{}, failure.New(failure.Internal, nil, nil)
}
func (r *cmdStartupRuntime) Reattach(context.Context, persistentcore.Binding) (daemonapp.PersistentReattach, error) {
	r.reattachCalls.Add(1)
	return daemonapp.PersistentReattach{}, r.reattachErr
}

var _ ipcadapter.SessionInspectActions = (*daemonActions)(nil)
