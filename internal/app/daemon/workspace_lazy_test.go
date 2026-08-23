package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestWorkspaceLazyProvenanceNeverFreshSamplesOrdinaryShell(t *testing.T) {
	store := lazyStore(t)
	cached := daemonSnapshot(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", workspace.QualityCached)
	observer := &lazyObserver{binding: workspace.Binding{RepositoryID: cached.RepositoryID, WorkspaceID: cached.WorkspaceID}, cached: cached}
	coherence := &trackingCoherence{}
	svc := app.NewServiceWithExecutionContextAndCoherence(store, &fakeOwner{}, nil, observer, nil, coherence, lazyOptions())

	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "lazy-provenance", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, started.SessionID)
	if observer.freshCalls != 0 || observer.bindCalls != 1 || observer.cachedCalls != 1 {
		t.Fatalf("observer bind=%d cached=%d fresh=%d", observer.bindCalls, observer.cachedCalls, observer.freshCalls)
	}
	got := terminal.Receipt.WorkspaceProvenance
	if got == nil || got.SchemaVersion != 2 || got.Binding.WorkspaceID != cached.WorkspaceID || got.Pre.Kind != receipt.WorkspaceCached || got.Post.Kind != receipt.WorkspaceUnreconciled || !got.Post.ObservationInvalidated || got.ObservedChange {
		t.Fatalf("provenance=%#v", got)
	}
	assertLeaseCounts(t, coherence, 1, 1)
}

func TestManagedShellLeaseEndsOnceForImmediateTerminalPaths(t *testing.T) {
	cases := []struct {
		name string
		mode lifecycleMode
	}{
		{"spawn_failure", lifecycleSpawnFailure},
		{"exit_zero", lifecycleExitZero},
		{"exit_nonzero", lifecycleExitNonzero},
		{"capture_failure", lifecycleCaptureFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			coherence := &trackingCoherence{}
			svc := app.NewServiceWithExecutionContextAndCoherence(lazyStore(t), newLifecycleOwner(tc.mode), nil, nil, nil, coherence, lazyOptions())
			started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "lease-" + tc.name, Command: "true", CWD: "/"})
			if err != nil {
				t.Fatal(err)
			}
			if !started.State.Terminal() {
				_ = waitForTerminal(t, svc, started.SessionID)
			}
			assertLeaseCounts(t, coherence, 1, 1)
		})
	}
}

func TestManagedShellLeaseEndsOnceForTimeoutAndShutdown(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		coherence := &trackingCoherence{}
		owner := newLifecycleOwner(lifecycleBlocking)
		svc := app.NewServiceWithExecutionContextAndCoherence(lazyStore(t), owner, nil, nil, nil, coherence, lazyOptions())
		started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "lease-timeout", Command: "sleep", CWD: "/", TimeoutMS: 10})
		if err != nil {
			t.Fatal(err)
		}
		_ = waitForTerminal(t, svc, started.SessionID)
		assertLeaseCounts(t, coherence, 1, 1)
	})

	t.Run("shutdown", func(t *testing.T) {
		coherence := &trackingCoherence{}
		owner := newLifecycleOwner(lifecycleBlocking)
		svc := app.NewServiceWithExecutionContextAndCoherence(lazyStore(t), owner, nil, nil, nil, coherence, lazyOptions())
		started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "lease-shutdown", Command: "sleep", CWD: "/"})
		if err != nil {
			t.Fatal(err)
		}
		if started.State.Terminal() {
			t.Fatalf("started=%#v", started)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := svc.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
		assertLeaseCounts(t, coherence, 1, 1)
	})
}

func TestManagedShellLeaseSurvivesRequestCancellationUntilChildEnds(t *testing.T) {
	coherence := &trackingCoherence{}
	owner := newLifecycleOwner(lifecycleBlocking)
	store := lazyStore(t)
	svc := app.NewServiceWithExecutionContextAndCoherence(store, owner, nil, nil, nil, coherence, lazyOptions())
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := svc.Start(ctx, app.StartRequest{ProtocolVersion: 2, OperationID: "lease-cancel", Command: "sleep", CWD: "/", YieldMS: 1000})
		result <- err
	}()
	<-owner.started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("start error=%v", err)
	}
	assertLeaseCounts(t, coherence, 1, 0)
	owner.finish(0)
	waitLeaseEnds(t, coherence)
	assertLeaseCounts(t, coherence, 1, 1)
	stored, err := store.LoadOperation(context.Background(), "lease-cancel")
	if err != nil {
		t.Fatal(err)
	}
	_ = waitForTerminal(t, svc, string(stored.SessionID))
}

func lazyStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func lazyOptions() app.Options {
	return app.Options{Incarnation: "daemon-lazy", Shell: "/bin/sh", MaxQueuedInputBytes: 100, TerminationGrace: 10 * time.Millisecond}
}

type lazyObserver struct {
	binding                            workspace.Binding
	cached                             workspace.FastSnapshot
	bindCalls, cachedCalls, freshCalls int
}

func (o *lazyObserver) Bind(context.Context, string) workspace.Binding {
	o.bindCalls++
	return o.binding
}
func (o *lazyObserver) ObserveCached(context.Context, string) workspace.FastSnapshot {
	o.cachedCalls++
	return o.cached
}
func (o *lazyObserver) ObserveFresh(context.Context, string) workspace.FastSnapshot {
	o.freshCalls++
	panic("ordinary shell performed fresh workspace sampling")
}

type trackingCoherence struct {
	mu           sync.Mutex
	begins, ends int
}

func (c *trackingCoherence) BeginManagedShell() app.ManagedShellLease {
	c.mu.Lock()
	c.begins++
	c.mu.Unlock()
	return trackingLease{owner: c}
}
func (c *trackingCoherence) CaptureBarrier() workspace.CoherenceBarrier {
	c.mu.Lock()
	defer c.mu.Unlock()
	return workspace.CoherenceBarrier{DaemonIncarnation: "daemon-lazy", Epoch: uint64(c.begins + c.ends), ActiveManagedShellOperations: c.begins - c.ends}
}
func (c *trackingCoherence) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.begins, c.ends
}

type trackingLease struct{ owner *trackingCoherence }

func (l trackingLease) End() {
	l.owner.mu.Lock()
	l.owner.ends++
	l.owner.mu.Unlock()
}

func assertLeaseCounts(t *testing.T, coherence *trackingCoherence, begins, ends int) {
	t.Helper()
	gotBegins, gotEnds := coherence.counts()
	if gotBegins != begins || gotEnds != ends {
		t.Fatalf("lease counts begins=%d ends=%d want %d/%d", gotBegins, gotEnds, begins, ends)
	}
}
func waitLeaseEnds(t *testing.T, coherence *trackingCoherence) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, ends := coherence.counts()
		if ends == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("managed shell lease did not end")
}

type lifecycleMode int

const (
	lifecycleExitZero lifecycleMode = iota
	lifecycleExitNonzero
	lifecycleSpawnFailure
	lifecycleCaptureFailure
	lifecycleBlocking
)

type lifecycleOwner struct {
	mode    lifecycleMode
	handle  *lifecycleHandle
	started chan struct{}
}

func newLifecycleOwner(mode lifecycleMode) *lifecycleOwner {
	return &lifecycleOwner{mode: mode, handle: &lifecycleHandle{done: make(chan struct{})}, started: make(chan struct{})}
}
func (o *lifecycleOwner) Start(ctx context.Context, _ operation.ExecutionSpec, sink app.OutputSink) (app.ProcessHandle, receipt.SpawnEvidence, error) {
	close(o.started)
	if o.mode == lifecycleSpawnFailure {
		return nil, receipt.SpawnEvidence{Attempted: true}, errors.New("spawn failed")
	}
	if o.mode == lifecycleCaptureFailure {
		sink.CaptureFailed(errors.New("capture failed"))
		o.handle.finish(0)
	} else if o.mode == lifecycleExitZero {
		o.handle.finish(0)
	} else if o.mode == lifecycleExitNonzero {
		o.handle.finish(7)
	}
	return o.handle, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}
func (o *lifecycleOwner) finish(code int) { o.handle.finish(code) }

type lifecycleHandle struct {
	once sync.Once
	done chan struct{}
	mu   sync.Mutex
	code int
}

func (h *lifecycleHandle) Write([]byte) error { return nil }
func (h *lifecycleHandle) CloseStdin() error  { return nil }
func (h *lifecycleHandle) Signal(string) receipt.SignalEvidence {
	h.finish(143)
	return receipt.SignalEvidence{Attempted: true, Succeeded: true}
}
func (h *lifecycleHandle) Wait(context.Context) receipt.ExitEvidence {
	<-h.done
	h.mu.Lock()
	code := h.code
	h.mu.Unlock()
	return receipt.ExitEvidence{Reaped: true, Code: &code}
}
func (h *lifecycleHandle) Close() error { return nil }
func (h *lifecycleHandle) finish(code int) {
	h.once.Do(func() {
		h.mu.Lock()
		h.code = code
		h.mu.Unlock()
		close(h.done)
	})
}

const managedShellStressWait = 10 * time.Second

func TestManagedShellLeaseStressReturnsToZero(t *testing.T) {
	for round := 0; round < 40; round++ {
		coherence := &trackingCoherence{}
		services := make([]*app.Service, 0, 5)
		owners := make([]*lifecycleOwner, 0, 5)
		for _, mode := range []lifecycleMode{lifecycleExitZero, lifecycleSpawnFailure, lifecycleBlocking, lifecycleBlocking, lifecycleBlocking} {
			owner := newLifecycleOwner(mode)
			owners = append(owners, owner)
			services = append(services, app.NewServiceWithExecutionContextAndCoherence(
				lazyStore(t), owner, nil, nil, nil, coherence, lazyOptions(),
			))
		}

		results := make(chan error, 5)
		go func() { results <- runImmediateLeaseCase(services[0], "stress-success") }()
		go func() { results <- runImmediateLeaseCase(services[1], "stress-spawn-failure") }()
		go func() { results <- runTimeoutLeaseCase(services[2], "stress-timeout") }()
		go func() { results <- runKillLeaseCase(services[3], owners[3], "stress-kill") }()
		go func() { results <- runShutdownLeaseCase(services[4], owners[4], "stress-shutdown") }()
		for i := 0; i < 5; i++ {
			if err := <-results; err != nil {
				t.Fatalf("round %d lifecycle: %v", round, err)
			}
		}
		if err := waitForManagedShellLeaseCounts(coherence, 5, 5, managedShellStressWait); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		begins, ends := coherence.counts()
		barrier := coherence.CaptureBarrier()
		if begins != 5 || ends != 5 || barrier.ActiveManagedShellOperations != 0 {
			t.Fatalf("round %d lease counts=%d/%d barrier=%#v", round, begins, ends, barrier)
		}
	}
}

func waitForManagedShellLeaseCounts(coherence *trackingCoherence, wantBegins, wantEnds int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		begins, ends := coherence.counts()
		if begins == wantBegins && ends == wantEnds {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("managed shell lease counts=%d/%d want %d/%d", begins, ends, wantBegins, wantEnds)
		}
		time.Sleep(time.Millisecond)
	}
}

func runImmediateLeaseCase(svc *app.Service, operationID string) error {
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: operationID, Command: "true", CWD: "/"})
	if err != nil {
		return fmt.Errorf("%s start: %w", operationID, err)
	}
	return nil
}

func runTimeoutLeaseCase(svc *app.Service, operationID string) error {
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: operationID, Command: "sleep", CWD: "/", TimeoutMS: 5})
	if err != nil {
		return fmt.Errorf("%s start: %w", operationID, err)
	}
	return nil
}

func runKillLeaseCase(svc *app.Service, owner *lifecycleOwner, operationID string) error {
	started, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: operationID, Command: "sleep", CWD: "/"})
	if err != nil {
		return fmt.Errorf("%s start: %w", operationID, err)
	}
	<-owner.started
	if _, err := svc.Kill(context.Background(), app.KillRequest{SessionID: started.SessionID, KillID: operationID + "-kill", Signal: "TERM"}); err != nil {
		return fmt.Errorf("%s kill: %w", operationID, err)
	}
	return nil
}

func runShutdownLeaseCase(svc *app.Service, owner *lifecycleOwner, operationID string) error {
	_, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: operationID, Command: "sleep", CWD: "/"})
	if err != nil {
		return fmt.Errorf("%s start: %w", operationID, err)
	}
	<-owner.started
	ctx, cancel := context.WithTimeout(context.Background(), managedShellStressWait)
	defer cancel()
	if err := svc.Shutdown(ctx); err != nil {
		return fmt.Errorf("%s shutdown: %w", operationID, err)
	}
	return nil
}
