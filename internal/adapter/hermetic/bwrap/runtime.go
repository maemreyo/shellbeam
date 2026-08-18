package bwrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const defaultProviderStatusBudget = 5 * time.Second

type runtimeCapture interface {
	Capture(context.Context, string, core.Request) (hermeticapp.CapturedView, error)
	Discard(context.Context, hermeticapp.CapturedView) error
}
type privateStarter interface {
	StartPrivateHermetic(context.Context, hermeticapp.ProviderCommand, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error)
}
type runtimeOwned struct {
	capture  hermeticapp.CapturedView
	prepared hermeticapp.PreparedExecution
}

func clonePreparedExecution(value hermeticapp.PreparedExecution) hermeticapp.PreparedExecution {
	out := value
	out.Command = value.Command.Clone()
	return out
}

type Runtime struct {
	capture      runtimeCapture
	provider     hermeticapp.ExecutionProvider
	starter      privateStarter
	statusBudget time.Duration
	mu           sync.Mutex
	owned        map[string]runtimeOwned
}

func NewRuntime(ctx context.Context, capture *hermeticapp.CaptureService, provider hermeticapp.ExecutionProvider, starter privateStarter) (*Runtime, error) {
	return newQualifiedRuntime(ctx, capture, provider, starter, defaultProviderStatusBudget)
}

type runtimeSweeper interface {
	Sweep(context.Context) error
}

func newQualifiedRuntime(ctx context.Context, capture runtimeCapture, provider hermeticapp.ExecutionProvider, starter privateStarter, budget time.Duration) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	captureSweeper, ok := capture.(runtimeSweeper)
	if !ok {
		return nil, fmt.Errorf("hermetic capture recovery unavailable")
	}
	providerSweeper, ok := provider.(runtimeSweeper)
	if !ok {
		return nil, fmt.Errorf("hermetic boundary recovery unavailable")
	}
	if err := captureSweeper.Sweep(ctx); err != nil {
		return nil, fmt.Errorf("hermetic capture recovery failed")
	}
	if err := providerSweeper.Sweep(ctx); err != nil {
		return nil, fmt.Errorf("hermetic boundary recovery failed")
	}
	return newRuntime(capture, provider, starter, budget), nil
}

func newRuntime(capture runtimeCapture, provider hermeticapp.ExecutionProvider, starter privateStarter, budget time.Duration) *Runtime {
	return &Runtime{capture: capture, provider: provider, starter: starter, statusBudget: budget, owned: make(map[string]runtimeOwned)}
}
func (r *Runtime) Prepare(ctx context.Context, req daemonapp.HermeticPrepareRequest) (hermeticapp.PreparedExecution, error) {
	if r == nil || r.capture == nil || r.provider == nil || r.starter == nil || r.statusBudget <= 0 {
		return hermeticapp.PreparedExecution{}, fmt.Errorf("hermetic runtime unavailable")
	}
	view, err := r.capture.Capture(ctx, req.WorkspaceID, req.Request)
	if err != nil {
		return hermeticapp.PreparedExecution{}, err
	}
	prepared, err := r.provider.Prepare(ctx, hermeticapp.PrepareExecutionRequest{Request: req.Request, Capture: view, LogicalCWD: req.LogicalCWD, Target: req.Target})
	if err != nil {
		_ = r.capture.Discard(context.Background(), view)
		return hermeticapp.PreparedExecution{}, err
	}
	if err := prepared.ValidatePrivate(); err != nil || prepared.Command.StatusFD != 3 {
		_ = r.provider.Discard(context.Background(), prepared)
		_ = r.capture.Discard(context.Background(), view)
		if err != nil {
			return hermeticapp.PreparedExecution{}, err
		}
		return hermeticapp.PreparedExecution{}, fmt.Errorf("hermetic provider status fd unavailable")
	}
	r.mu.Lock()
	if _, exists := r.owned[prepared.BoundaryID]; exists {
		r.mu.Unlock()
		_ = r.provider.Discard(context.Background(), prepared)
		_ = r.capture.Discard(context.Background(), view)
		return hermeticapp.PreparedExecution{}, fmt.Errorf("hermetic boundary identity collision")
	}
	r.owned[prepared.BoundaryID] = runtimeOwned{capture: view, prepared: clonePreparedExecution(prepared)}
	r.mu.Unlock()
	return prepared, nil
}
func (r *Runtime) Start(ctx context.Context, prepared hermeticapp.PreparedExecution, sink daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	if err := r.requireOwned(prepared); err != nil {
		return nil, receipt.SpawnEvidence{ErrorCode: "provider_spawn_failed"}, err
	}
	inner, spawn, status, err := r.starter.StartPrivateHermetic(ctx, prepared.Command, sink)
	if err != nil {
		return nil, spawn, err
	}
	if inner == nil || status == nil || !spawn.Attempted || !spawn.Succeeded {
		if status != nil {
			_ = status.Close()
		}
		if inner != nil {
			_ = inner.Close()
		}
		return nil, receipt.SpawnEvidence{Attempted: true, ErrorCode: "provider_spawn_failed"}, fmt.Errorf("hermetic provider spawn incomplete")
	}
	monitor := newProviderStatusMonitor(status)
	if err := monitor.awaitReady(ctx, r.statusBudget); err != nil {
		_ = inner.Signal("KILL")
		_ = status.Close()
		_ = inner.Wait(context.Background())
		_ = inner.Close()
		return nil, receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "provider_spawn_failed"}, err
	}
	return &runtimeHandle{inner: inner, monitor: monitor, prepared: clonePreparedExecution(prepared), statusBudget: r.statusBudget}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}
func (r *Runtime) Discard(ctx context.Context, prepared hermeticapp.PreparedExecution) error {
	if r == nil {
		return fmt.Errorf("hermetic runtime unavailable")
	}
	r.mu.Lock()
	owned, ok := r.owned[prepared.BoundaryID]
	if !ok || !reflect.DeepEqual(owned.prepared, prepared) {
		r.mu.Unlock()
		return fmt.Errorf("hermetic runtime ownership mismatch")
	}
	delete(r.owned, prepared.BoundaryID)
	r.mu.Unlock()
	return errors.Join(r.provider.Discard(ctx, prepared), r.capture.Discard(ctx, owned.capture))
}
func (r *Runtime) requireOwned(prepared hermeticapp.PreparedExecution) error {
	if r == nil || r.starter == nil {
		return fmt.Errorf("hermetic runtime unavailable")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	owned, ok := r.owned[prepared.BoundaryID]
	if !ok || !reflect.DeepEqual(owned.prepared, prepared) {
		return fmt.Errorf("hermetic runtime ownership mismatch")
	}
	return nil
}

type runtimeHandle struct {
	inner        daemonapp.ProcessHandle
	monitor      *providerStatusMonitor
	prepared     hermeticapp.PreparedExecution
	statusBudget time.Duration
	waitOnce     sync.Once
	exit         receipt.ExitEvidence
	result       core.BoundaryResult
}

func (h *runtimeHandle) Write(data []byte) error                   { return h.inner.Write(data) }
func (h *runtimeHandle) CloseStdin() error                         { return h.inner.CloseStdin() }
func (h *runtimeHandle) Signal(name string) receipt.SignalEvidence { return h.inner.Signal(name) }
func (h *runtimeHandle) Close() error                              { return h.inner.Close() }
func (h *runtimeHandle) Wait(ctx context.Context) receipt.ExitEvidence {
	h.waitOnce.Do(func() {
		h.exit = h.inner.Wait(ctx)
		h.result = h.resultFromStatus(h.monitor.awaitDone(h.statusBudget), h.exit)
	})
	return h.exit
}
func (h *runtimeHandle) HermeticBoundaryResult() core.BoundaryResult { return h.result }
func (h *runtimeHandle) resultFromStatus(snap providerStatusSnapshot, exit receipt.ExitEvidence) core.BoundaryResult {
	result := core.BoundaryResult{SchemaVersion: core.BoundaryResultSchemaV1, BoundaryID: h.prepared.BoundaryID, Provider: h.prepared.Provider, Toolchain: h.prepared.Toolchain, EstablishedPreExec: snap.ChildPID > 0, Continuity: core.ContinuityLost}
	if snap.Err == nil && snap.CleanEOF && snap.ChildPID > 0 && snap.ExitCode != nil && exit.Reaped && exit.Code != nil && exit.Signal == "" && *snap.ExitCode == *exit.Code {
		result.Continuity = core.ContinuityComplete
	}
	return result
}
func (h *runtimeHandle) PID() int {
	if a, ok := h.inner.(interface{ PID() int }); ok {
		return a.PID()
	}
	return 0
}
func (h *runtimeHandle) ResourceLimitBreach() operation.ResourceLimitKind {
	if a, ok := h.inner.(interface {
		ResourceLimitBreach() operation.ResourceLimitKind
	}); ok {
		return a.ResourceLimitBreach()
	}
	return ""
}
func (h *runtimeHandle) ResourceCleanupIncomplete() string {
	if a, ok := h.inner.(interface{ ResourceCleanupIncomplete() string }); ok {
		return a.ResourceCleanupIncomplete()
	}
	return ""
}

var _ daemonapp.HermeticRuntime = (*Runtime)(nil)
var _ daemonapp.ProcessHandle = (*runtimeHandle)(nil)
