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
	case <-time.After(100 * time.Millisecond):
		close(release)
		t.Fatal("independent persistent sessions were serialized by launcher")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("independent ensure err=%v", err)
		}
	}
}
