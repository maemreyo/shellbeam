//go:build linux || darwin

package supervisor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestLauncherClaimsDurableMarkerBeforeExactlyOneSpawnAndRetryOnlyAttaches(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(bootstrap Bootstrap, inherited Capability) error {
		spawns.Add(1)
		layout, err := OpenPrivateState(runtimeRoot, bootstrap.SessionID, bootstrap.GenerationID)
		if err != nil {
			t.Fatalf("private state missing before spawn: %v", err)
		}
		storedCapability, capErr := LoadCapability(layout)
		if capErr != nil || !bytes.Equal(storedCapability.bytes(), inherited.bytes()) {
			t.Fatalf("inherited capability mismatch err=%v", capErr)
		}
		marker, err := loadLaunchMarker(layout)
		if err != nil || marker.SessionID != bootstrap.SessionID || marker.GenerationID != bootstrap.GenerationID {
			t.Fatalf("launch marker missing before spawn: %#v err=%v", marker, err)
		}
		return nil
	}
	attachment := newRuntimeFakeHandle()
	launcher.attach = func(_ context.Context, _ Layout, _ Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
		return attachment, persistentapp.Status{SessionID: sessionID, GenerationID: generationID, State: session.Running, PID: 4242, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}, nil
	}
	request := launcherRequest("launcher-session-a", "launcher-op-a", "generation-a")
	firstAttachment, firstStatus, err := launcher.Ensure(context.Background(), request)
	if err != nil || firstAttachment == nil || firstStatus.State != session.Running || spawns.Load() != 1 {
		t.Fatalf("first attachment=%v status=%#v spawns=%d err=%v", firstAttachment, firstStatus, spawns.Load(), err)
	}
	secondAttachment, secondStatus, err := launcher.Ensure(context.Background(), request)
	if err != nil || secondAttachment == nil || secondStatus.SessionID != firstStatus.SessionID || spawns.Load() != 1 {
		t.Fatalf("retry attachment=%v status=%#v spawns=%d err=%v", secondAttachment, secondStatus, spawns.Load(), err)
	}
}

func TestLauncherAmbiguousReadinessRetryNeverRespawns(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	launcher.attach = func(context.Context, Layout, Capability, string, string) (persistentapp.Attachment, persistentapp.Status, error) {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "not_ready"}, nil)
	}
	request := launcherRequest("launcher-session-ambiguous", "launcher-op-ambiguous", "generation-ambiguous")
	if _, _, err := launcher.Ensure(context.Background(), request); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("first ambiguous err=%v", err)
	}
	if _, _, err := launcher.Ensure(context.Background(), request); !errors.Is(err, failure.SupervisorUnavailable) {
		t.Fatalf("retry ambiguous err=%v", err)
	}
	if spawns.Load() != 1 {
		t.Fatalf("ambiguous retry respawned supervisor: %d", spawns.Load())
	}
}

func TestLauncherConcurrentEnsureSpawnsAtMostOnce(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	launcher.attach = func(_ context.Context, _ Layout, _ Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
		return newRuntimeFakeHandle(), persistentapp.Status{SessionID: sessionID, GenerationID: generationID, State: session.Running, PID: 4242, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}, nil
	}
	request := launcherRequest("launcher-session-concurrent", "launcher-op-concurrent", "generation-concurrent")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attachment, _, err := launcher.Ensure(context.Background(), request)
			if attachment != nil {
				_ = attachment.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ensure err=%v", err)
		}
	}
	if spawns.Load() != 1 {
		t.Fatalf("concurrent ensures spawned=%d", spawns.Load())
	}
}

func TestLauncherRejectsBindingExecutionMismatchWithoutPrivateStateOrSpawn(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	request := launcherRequest("launcher-session-invalid", "launcher-op-invalid", "generation-invalid")
	request.Spec.Executable = "relative-tool"
	if _, _, err := launcher.Ensure(context.Background(), request); err == nil {
		t.Fatal("relative frozen executable accepted")
	}
	if spawns.Load() != 0 {
		t.Fatalf("invalid request spawned=%d", spawns.Load())
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "supervisors", request.Binding.SessionID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid request created private state err=%v", err)
	}
}

func launcherRequest(sessionID, operationID, generation string) persistentapp.LaunchRequest {
	return persistentapp.LaunchRequest{
		Binding: core.Binding{
			SchemaVersion: core.SchemaVersion, SessionID: sessionID, OperationID: operationID, Persistent: true,
			Supervision: core.SupervisionPerSession, Continuity: core.ContinuityDaemonRestart,
			SupervisorGenerationID: generation, SupervisorEndpointRef: "endpoint-a", Lifecycle: core.LifecycleProvisioning,
			CreatedAt: time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC),
		},
		Spec:   operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"},
		Limits: persistentapp.Limits{MaxOutputBytes: 1024, MaxQueuedInputBytes: 128, MaxInputRecords: 16, MaxInputMetadataBytes: 8192, MaxKillRecords: 8, TerminationGrace: 25 * time.Millisecond},
	}
}

func shortLauncherRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "sl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func TestLauncherIndependentSessionsLaunchConcurrently(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan string, 2)
	release := make(chan struct{})
	launcher.spawnSupervisor = func(bootstrap Bootstrap, _ Capability) error {
		entered <- bootstrap.SessionID
		<-release
		return nil
	}
	launcher.attach = func(_ context.Context, _ Layout, _ Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
		return newRuntimeFakeHandle(), persistentapp.Status{SessionID: sessionID, GenerationID: generationID, State: session.Running, PID: 4242, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}, nil
	}
	errs := make(chan error, 2)
	go func() {
		_, _, err := launcher.Ensure(context.Background(), launcherRequest("launcher-independent-a", "launcher-independent-op-a", "generation-independent-a"))
		errs <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first independent session did not reach spawn")
	}
	go func() {
		_, _, err := launcher.Ensure(context.Background(), launcherRequest("launcher-independent-b", "launcher-independent-op-b", "generation-independent-b"))
		errs <- err
	}()
	select {
	case <-entered:
		close(release)
	// The deadline is a hang guard, not the measurement. A launcher that
	// serialized these would never let the second session reach spawn at all,
	// because the first is still blocked on release, so waiting longer cannot
	// turn a serialized run into a passing one. What a short deadline does
	// measure is how quickly a loaded machine happens to schedule the second
	// goroutine: at 100ms this failed on CI while passing locally, which is a
	// property of the runner rather than of the launcher.
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("independent persistent sessions were serialized by launcher")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("independent ensure err=%v", err)
		}
	}
}

func TestLauncherReattachLiveNeverSpawns(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	binding := launcherRequest("reattach-live", "reattach-live-op", "generation-reattach-live").Binding
	layout, err := PreparePrivateState(runtimeRoot, binding.SessionID, binding.SupervisorGenerationID, capability)
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	attachment := newRuntimeFakeHandle()
	launcher.attach = func(_ context.Context, got Layout, inherited Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
		if got.SessionDir != layout.SessionDir || !bytes.Equal(inherited.bytes(), capability.bytes()) {
			t.Fatal("reattach did not load exact private state")
		}
		return attachment, persistentapp.Status{
			SessionID: sessionID, GenerationID: generationID, State: session.Running, PID: 4242,
			Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		}, nil
	}
	got, status, err := launcher.Reattach(context.Background(), binding)
	if err != nil || got == nil || status.State != session.Running || status.PID != 4242 {
		t.Fatalf("attachment=%v status=%#v err=%v", got, status, err)
	}
	if spawns.Load() != 0 {
		t.Fatalf("reattach spawned supervisor: %d", spawns.Load())
	}
}

func TestLauncherReattachUsesVerifiedTerminalRecoveryWithoutSpawn(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	binding := launcherRequest("reattach-terminal", "reattach-terminal-op", "generation-reattach-terminal").Binding
	layout, err := PreparePrivateState(runtimeRoot, binding.SessionID, binding.SupervisorGenerationID, capability)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := OpenSpool(layout, 64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spool.AppendRange([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	code := 0
	record, err := SealTerminalRecord(capability, TerminalRecord{
		SchemaVersion: TerminalRecordSchemaVersion, ProtocolVersion: ProtocolVersion,
		SessionID: binding.SessionID, GenerationID: binding.SupervisorGenerationID,
		State: session.Completed, Outcome: session.Success,
		Spawn:       receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:        receipt.ExitEvidence{Reaped: true, Code: &code},
		OutputBytes: 2, OutputComplete: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTerminalRecord(layout, record); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	launcher.attach = func(context.Context, Layout, Capability, string, string) (persistentapp.Attachment, persistentapp.Status, error) {
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "socket_absent"}, nil)
	}
	attachment, status, err := launcher.Reattach(context.Background(), binding)
	if err != nil || attachment == nil || status.State != session.Completed || status.PID != 0 {
		t.Fatalf("attachment=%v status=%#v err=%v", attachment, status, err)
	}
	recovery, ok := attachment.(persistentapp.RecoveryAttachment)
	if !ok {
		t.Fatalf("attachment type=%T lacks recovery", attachment)
	}
	terminal, err := recovery.Terminal(context.Background())
	if err != nil || terminal.State != session.Completed || terminal.OutputBytes != 2 {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
	if spawns.Load() != 0 {
		t.Fatalf("terminal reattach spawned supervisor: %d", spawns.Load())
	}
}

func TestLauncherReattachGenerationMismatchFailsClosedWithoutSpawn(t *testing.T) {
	runtimeRoot := shortLauncherRoot(t)
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	binding := launcherRequest("reattach-mismatch", "reattach-mismatch-op", "generation-a").Binding
	if _, err := PreparePrivateState(runtimeRoot, binding.SessionID, binding.SupervisorGenerationID, capability); err != nil {
		t.Fatal(err)
	}
	binding.SupervisorGenerationID = "generation-b"
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	var spawns atomic.Int32
	launcher.spawnSupervisor = func(Bootstrap, Capability) error { spawns.Add(1); return nil }
	if attachment, _, err := launcher.Reattach(context.Background(), binding); err == nil || attachment != nil {
		t.Fatalf("mismatched generation reattached attachment=%v err=%v", attachment, err)
	}
	if spawns.Load() != 0 {
		t.Fatalf("generation mismatch spawned supervisor: %d", spawns.Load())
	}
}

func TestLauncherAttachNormalizesUnavailableTypedNilClient(t *testing.T) {
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewLauncher(LauncherOptions{RuntimeRoot: runtimeRoot, Executable: "/bin/echo", HandshakeTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(runtimeRoot, "typed-nil-session", "typed-nil-generation", capability)
	if err != nil {
		t.Fatal(err)
	}
	attachment, _, err := launcher.attach(context.Background(), layout, capability, "typed-nil-session", "typed-nil-generation")
	if err == nil {
		t.Fatal("missing supervisor socket unexpectedly attached")
	}
	if attachment != nil {
		t.Fatalf("unavailable attach returned typed-nil interface: %#v", attachment)
	}
}
