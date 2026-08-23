//go:build darwin

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	delegatedtmux "github.com/maemreyo/shellbeam/internal/adapter/delegatedtmux"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func requireH1NativeTmux(t *testing.T) string {
	t.Helper()
	path := os.Getenv("SHELLBEAM_H0_TMUX")
	if path == "" {
		t.Skip("set SHELLBEAM_H0_TMUX to run H1 delegated integration acceptance")
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("SHELLBEAM_H0_TMUX must be absolute: %q", path)
	}
	return path
}

func openH1Store(t *testing.T, root string) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(root, storeadapter.Limits{
		MaxSessions: 16, MaxSessionOutput: 1 << 20, MaxTotalState: 32 << 20, ControlReserve: 4096,
		MaxDelegatedMutationRecords: storeadapter.DefaultMaxDelegatedMutationRecords,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	return store
}

func h1DelegatedCatalog() capability.Catalog {
	return capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{
		ProviderID: delegatedtmux.ProviderID, ProviderVersion: delegatedtmux.ProviderVersion, Platform: "darwin",
		MaxMutationRecords: storeadapter.DefaultMaxDelegatedMutationRecords, DaemonRestartContinuity: true,
	})
}

func waitH1Output(t *testing.T, svc *daemonapp.Service, sessionID, want string) daemonapp.View {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		view, err := svc.Poll(t.Context(), daemonapp.PollRequest{SessionID: sessionID, MaxOutputBytes: 8192})
		if err == nil && strings.Contains(view.Output, want) {
			return view
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("output %q not observed", want)
	return daemonapp.View{}
}

func waitH1Terminal(t *testing.T, svc *daemonapp.Service, sessionID string) daemonapp.View {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, err := svc.Poll(t.Context(), daemonapp.PollRequest{SessionID: sessionID, MaxOutputBytes: 8192})
		if err != nil {
			t.Fatal(err)
		}
		if view.State.Terminal() {
			return view
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s did not reach terminal state", sessionID)
	return daemonapp.View{}
}

func assertOneH1TmuxSessionAndPane(t *testing.T, tmuxPath, providerRoot string, ref delegated.ProviderRef) {
	t.Helper()
	statePath := filepath.Join(providerRoot, "provider-state", ref.Ref+".json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		SocketPath string `json:"socket_path"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.SocketPath == "" {
		t.Fatalf("provider state missing socket path: %s", raw)
	}
	for label, args := range map[string][]string{
		"sessions": {"-S", state.SocketPath, "list-sessions", "-F", "#{session_id}"},
		"panes":    {"-S", state.SocketPath, "list-panes", "-a", "-F", "#{pane_id}"},
	} {
		out, err := exec.Command(tmuxPath, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %s: %v: %s", label, err, out)
		}
		lines := strings.Fields(strings.TrimSpace(string(out)))
		if len(lines) != 1 {
			t.Fatalf("tmux %s=%q want exactly one object", label, out)
		}
	}
}

func countProviderStateFiles(t *testing.T, providerRoot string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(providerRoot, "provider-state"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func containsH1CaptureReason(values []receipt.CaptureReason, want receipt.CaptureReason) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type h1CountingDelegatedProvider struct {
	inner           *delegatedtmux.Provider
	failFirstCreate bool
	failedCreate    atomic.Int32
	creates         atomic.Int32
	writes          atomic.Int32
}

func (p *h1CountingDelegatedProvider) Identity() delegated.ProviderIdentity {
	return p.inner.Identity()
}
func (p *h1CountingDelegatedProvider) ProviderRefForSession(id string, at time.Time) (delegated.ProviderRef, error) {
	return p.inner.ProviderRefForSession(id, at)
}
func (p *h1CountingDelegatedProvider) Probe(ctx context.Context) error { return p.inner.Probe(ctx) }
func (p *h1CountingDelegatedProvider) Create(ctx context.Context, req delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	p.creates.Add(1)
	result, err := p.inner.Create(ctx, req)
	if err == nil && p.failFirstCreate && p.failedCreate.CompareAndSwap(0, 1) {
		return delegatedapp.CreateResult{}, failure.New(failure.DelegatedProviderLost, map[string]string{
			"session_id": req.SessionID, "provider_id": delegatedtmux.ProviderID, "reason": "response_lost_after_create",
		}, nil)
	}
	return result, err
}
func (p *h1CountingDelegatedProvider) Reattach(ctx context.Context, ref delegated.ProviderRef, sink delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	return p.inner.Reattach(ctx, ref, sink)
}
func (p *h1CountingDelegatedProvider) Write(ctx context.Context, ref delegated.ProviderRef, data []byte) error {
	p.writes.Add(1)
	return p.inner.Write(ctx, ref, data)
}
func (p *h1CountingDelegatedProvider) Signal(ctx context.Context, ref delegated.ProviderRef, signal string) error {
	return p.inner.Signal(ctx, ref, signal)
}
func (p *h1CountingDelegatedProvider) Inspect(ctx context.Context, ref delegated.ProviderRef) (delegatedapp.Observation, error) {
	return p.inner.Inspect(ctx, ref)
}
func (p *h1CountingDelegatedProvider) Wait(ctx context.Context, ref delegated.ProviderRef) (delegatedapp.Observation, error) {
	return p.inner.Wait(ctx, ref)
}
func (p *h1CountingDelegatedProvider) Close(ctx context.Context, ref delegated.ProviderRef) error {
	return p.inner.Close(ctx, ref)
}
func (p *h1CountingDelegatedProvider) Detach(ctx context.Context, ref delegated.ProviderRef) error {
	return p.inner.Detach(ctx, ref)
}

type h1TripwireDelegatedProvider struct{ calls atomic.Int32 }

func (p *h1TripwireDelegatedProvider) touched() error {
	p.calls.Add(1)
	return errors.New("ordinary path touched delegated provider")
}
func (p *h1TripwireDelegatedProvider) Identity() delegated.ProviderIdentity {
	p.calls.Add(1)
	return delegated.ProviderIdentity{ID: delegatedtmux.ProviderID, Version: delegatedtmux.ProviderVersion}
}
func (p *h1TripwireDelegatedProvider) ProviderRefForSession(string, time.Time) (delegated.ProviderRef, error) {
	return delegated.ProviderRef{}, p.touched()
}
func (p *h1TripwireDelegatedProvider) Probe(context.Context) error { return p.touched() }
func (p *h1TripwireDelegatedProvider) Create(context.Context, delegatedapp.CreateRequest) (delegatedapp.CreateResult, error) {
	return delegatedapp.CreateResult{}, p.touched()
}
func (p *h1TripwireDelegatedProvider) Reattach(context.Context, delegated.ProviderRef, delegatedapp.OutputSink) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, p.touched()
}
func (p *h1TripwireDelegatedProvider) Write(context.Context, delegated.ProviderRef, []byte) error {
	return p.touched()
}
func (p *h1TripwireDelegatedProvider) Signal(context.Context, delegated.ProviderRef, string) error {
	return p.touched()
}
func (p *h1TripwireDelegatedProvider) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, p.touched()
}
func (p *h1TripwireDelegatedProvider) Wait(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	return delegatedapp.Observation{}, p.touched()
}
func (p *h1TripwireDelegatedProvider) Close(context.Context, delegated.ProviderRef) error {
	return p.touched()
}
func (p *h1TripwireDelegatedProvider) Detach(context.Context, delegated.ProviderRef) error {
	return p.touched()
}

type h1ImmediateOwner struct{ starts atomic.Int32 }

func (o *h1ImmediateOwner) Start(_ context.Context, _ operation.ExecutionSpec, sink daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, error) {
	o.starts.Add(1)
	if sink != nil {
		_ = sink.Append(context.Background(), []byte("direct\n"))
	}
	return &h1ImmediateHandle{}, receipt.SpawnEvidence{Attempted: true, Succeeded: true}, nil
}

type h1ImmediateHandle struct{}

func (*h1ImmediateHandle) Write([]byte) error                   { return nil }
func (*h1ImmediateHandle) CloseStdin() error                    { return nil }
func (*h1ImmediateHandle) Signal(string) receipt.SignalEvidence { return receipt.SignalEvidence{} }
func (*h1ImmediateHandle) Wait(context.Context) receipt.ExitEvidence {
	zero := 0
	return receipt.ExitEvidence{Reaped: true, Code: &zero}
}
func (*h1ImmediateHandle) Close() error { return nil }

type h1PersistentRuntime struct {
	store *storeadapter.Repository
	calls atomic.Int32
}

func (r *h1PersistentRuntime) Ensure(ctx context.Context, reservation operation.Reservation, _ operation.ExecutionSpec) (daemonapp.PersistentLaunch, error) {
	r.calls.Add(1)
	now := reservation.CreatedAt.UTC()
	binding := persistent.Binding{
		SchemaVersion: persistent.SchemaVersion, SessionID: string(reservation.SessionID), OperationID: string(reservation.OperationID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID, SessionName: reservation.SessionName,
		Persistent: true, Supervision: persistent.SupervisionPerSession, Continuity: persistent.ContinuityDaemonRestart,
		SupervisorGenerationID: "h1-no-tax-generation", SupervisorEndpointRef: "h1-no-tax-endpoint",
		Lifecycle: persistent.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	stored, _, result := r.store.ReservePersistentBinding(ctx, binding)
	if result.Err != nil {
		return daemonapp.PersistentLaunch{}, result.Err
	}
	if stored.Lifecycle == persistent.LifecycleProvisioning {
		stored.Lifecycle = persistent.LifecycleLive
		stored.UpdatedAt = stored.UpdatedAt.Add(time.Nanosecond)
		if got := r.store.AdvancePersistentBinding(ctx, stored); got.Err != nil {
			return daemonapp.PersistentLaunch{}, got.Err
		}
	}
	handle := &h1PersistentHandle{sessionID: string(reservation.SessionID), generation: stored.SupervisorGenerationID, pid: 4242}
	return daemonapp.PersistentLaunch{Handle: handle, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, PID: handle.pid}, nil
}

type h1PersistentHandle struct {
	sessionID  string
	generation string
	pid        int
}

func (h *h1PersistentHandle) PID() int                                { return h.pid }
func (*h1PersistentHandle) Write([]byte) error                        { return nil }
func (*h1PersistentHandle) CloseStdin() error                         { return nil }
func (*h1PersistentHandle) Signal(string) receipt.SignalEvidence      { return receipt.SignalEvidence{} }
func (*h1PersistentHandle) Wait(context.Context) receipt.ExitEvidence { return receipt.ExitEvidence{} }
func (*h1PersistentHandle) Close() error                              { return nil }
func (*h1PersistentHandle) WriteInput(_ context.Context, offset int64, data []byte, eof bool) (persistentapp.InputResult, error) {
	return persistentapp.InputResult{AcceptedBytes: len(data), NextOffset: offset + int64(len(data)), EOFDelivered: eof}, nil
}
func (*h1PersistentHandle) SignalWithID(_ context.Context, killID, signalName string) (persistentapp.KillResult, error) {
	return persistentapp.KillResult{KillID: killID, Signal: signalName, Attempted: true, Succeeded: true, Needed: true}, nil
}
func (*h1PersistentHandle) ReadOutput(context.Context, int64, int) ([]byte, int64, int64, error) {
	return nil, 0, 0, nil
}
func (*h1PersistentHandle) AcknowledgeOutput(context.Context, int64) error { return nil }
func (h *h1PersistentHandle) Status(context.Context) (persistentapp.Status, error) {
	return persistentapp.Status{SessionID: h.sessionID, GenerationID: h.generation, State: session.Running, PID: h.pid, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}, nil
}
func (h *h1PersistentHandle) WaitStatus(ctx context.Context, _ uint64, _ int) (persistentapp.Status, error) {
	<-ctx.Done()
	return persistentapp.Status{}, ctx.Err()
}
func (*h1PersistentHandle) Terminal(context.Context) (persistentapp.TerminalEvidence, error) {
	return persistentapp.TerminalEvidence{}, failure.New(failure.SupervisorUnavailable, map[string]string{"reason": "still_running"}, nil)
}
func (*h1PersistentHandle) RecoveryState(context.Context) (int64, int64, error) { return 0, 0, nil }
func (*h1PersistentHandle) Cleanup(context.Context) error                       { return nil }

var _ persistentapp.RecoveryAttachment = (*h1PersistentHandle)(nil)
var _ delegatedapp.Provider = (*h1CountingDelegatedProvider)(nil)
var _ delegatedapp.Detacher = (*h1CountingDelegatedProvider)(nil)
var _ delegatedapp.Provider = (*h1TripwireDelegatedProvider)(nil)
var _ delegatedapp.Detacher = (*h1TripwireDelegatedProvider)(nil)

type h1NativeMatrix struct {
	tmuxPath     string
	providerRoot string
	store        *storeadapter.Repository
	provider     *delegatedtmux.Provider
	counting     *h1CountingDelegatedProvider
	owner        *h1ImmediateOwner
	svc          *daemonapp.Service
}

func newH1NativeMatrix(t *testing.T) *h1NativeMatrix {
	t.Helper()
	tmuxPath := requireH1NativeTmux(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	store := openH1Store(t, stateDir)
	providerRoot := filepath.Join(stateDir, "delegated-tmux")
	provider, err := delegatedtmux.New(delegatedtmux.DarwinQualifiedConfig(providerRoot, tmuxPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Probe(t.Context()); err != nil {
		t.Fatal(err)
	}
	counting := &h1CountingDelegatedProvider{inner: provider, failFirstCreate: true}
	owner := &h1ImmediateOwner{}
	svc := daemonapp.NewService(store, owner, daemonapp.Options{
		Incarnation: "h1-integration", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: counting, Capabilities: h1DelegatedCatalog(),
	})
	return &h1NativeMatrix{tmuxPath: tmuxPath, providerRoot: providerRoot, store: store, provider: provider, counting: counting, owner: owner, svc: svc}
}

func h1AssertResponseLossReplay(t *testing.T, m *h1NativeMatrix) (daemonapp.View, operation.Reservation) {
	t.Helper()
	req := daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "h1-integration-response-loss", CWD: "/tmp",
		Command:     "IFS= read -r one; printf 'ONE:%s\\n' \"$one\"; IFS= read -r two; printf 'TWO:%s\\n' \"$two\"; exit 0",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited,
		YieldMS: 25, MaxOutputBytes: 8192,
	}
	if _, err := m.svc.Start(t.Context(), req); err == nil {
		t.Fatal("first provider response-loss start unexpectedly succeeded")
	}
	reservation, err := m.store.LoadOperation(t.Context(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := m.store.LoadDelegatedBinding(t.Context(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := m.store.LoadDelegatedProviderRef(t.Context(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Lifecycle != delegated.LifecycleProvisioning || m.counting.creates.Load() != 1 || m.owner.starts.Load() != 0 {
		t.Fatalf("first attempt binding=%#v creates=%d owner_starts=%d", binding, m.counting.creates.Load(), m.owner.starts.Load())
	}
	started, err := m.svc.Start(t.Context(), req)
	if err != nil {
		t.Fatalf("retry err=%#v public=%#v creates=%d writes=%d", err, failure.Public(err), m.counting.creates.Load(), m.counting.writes.Load())
	}
	if started.SessionID != string(reservation.SessionID) || started.State != session.Running || started.AuthorityEpoch != 1 || m.counting.creates.Load() != 2 || m.owner.starts.Load() != 0 {
		t.Fatalf("replayed start=%#v creates=%d owner_starts=%d", started, m.counting.creates.Load(), m.owner.starts.Load())
	}
	assertOneH1TmuxSessionAndPane(t, m.tmuxPath, m.providerRoot, ref)
	return started, reservation
}

func h1AssertEpochReplayAndCleanTerminal(t *testing.T, m *h1NativeMatrix, started daemonapp.View, reservation operation.Reservation) {
	t.Helper()
	first := daemonapp.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 1, InputOffset: 0, Chars: "alpha\n"}
	written, err := m.svc.Write(t.Context(), first)
	if err != nil || written.NextInputOffset != int64(len(first.Chars)) || m.counting.writes.Load() != 1 {
		t.Fatalf("first write=%#v err=%v provider_writes=%d", written, err, m.counting.writes.Load())
	}
	waitH1Output(t, m.svc, started.SessionID, "ONE:alpha")
	current, err := m.store.LoadDelegatedBinding(t.Context(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	current.AuthorityEpoch = 2
	current.UpdatedAt = time.Now().UTC()
	if !current.UpdatedAt.After(current.CreatedAt) {
		current.UpdatedAt = current.CreatedAt.Add(time.Nanosecond)
	}
	if got := m.store.AdvanceDelegatedBinding(t.Context(), current); got.Err != nil {
		t.Fatal(got.Err)
	}
	replayed, err := m.svc.Write(t.Context(), first)
	if err != nil || replayed.NextInputOffset != written.NextInputOffset || m.counting.writes.Load() != 1 {
		t.Fatalf("known old-epoch retry=%#v err=%v writes=%d", replayed, err, m.counting.writes.Load())
	}
	stale := daemonapp.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 1, InputOffset: written.NextInputOffset, Chars: "stale\n"}
	if _, err := m.svc.Write(t.Context(), stale); !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("unseen stale epoch err=%v", err)
	}
	second := daemonapp.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: 2, InputOffset: written.NextInputOffset, Chars: "beta\n"}
	secondView, err := m.svc.Write(t.Context(), second)
	if err != nil || secondView.NextInputOffset != second.InputOffset+int64(len(second.Chars)) || m.counting.writes.Load() != 2 {
		t.Fatalf("second write=%#v err=%v provider_writes=%d", secondView, err, m.counting.writes.Load())
	}
	terminal := waitH1Terminal(t, m.svc, started.SessionID)
	if terminal.Receipt == nil || terminal.Receipt.SchemaVersion != 5 || terminal.Receipt.State != session.Completed || terminal.Receipt.Outcome != session.Success || terminal.Receipt.AuthorityEpoch != 2 || !terminal.Receipt.OutputComplete || terminal.Receipt.CaptureQuality != receipt.CaptureComplete || !strings.Contains(terminal.Output, "TWO:beta") {
		t.Fatalf("clean terminal=%#v", terminal)
	}
	if reservation.EvidenceEligible() {
		t.Fatal("delegated reservation became ordinary evidence-eligible")
	}
}

func h1AssertProviderLossFailsClosed(t *testing.T, m *h1NativeMatrix) {
	t.Helper()
	req := daemonapp.StartRequest{
		ProtocolVersion: 2, OperationID: "h1-integration-provider-loss", CWD: "/tmp", Command: "while :; do sleep 1; done",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream, TimeoutMode: operation.TimeoutModeUnlimited,
		YieldMS: 25, MaxOutputBytes: 4096,
	}
	m.counting.failFirstCreate = false
	started, err := m.svc.Start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := m.store.LoadOperation(t.Context(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := m.store.LoadDelegatedProviderRef(t.Context(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	createsBeforeLoss := m.counting.creates.Load()
	if err := m.provider.Close(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	lost := waitH1Terminal(t, m.svc, started.SessionID)
	if lost.Receipt == nil || lost.Receipt.State != session.Abandoned || lost.Receipt.Outcome != session.Ambiguous || lost.Receipt.FailureReason != "provider_lost" || lost.Receipt.OutputComplete || !containsH1CaptureReason(lost.Receipt.CaptureReasons, receipt.CaptureReasonProviderLost) {
		t.Fatalf("provider loss=%#v", lost)
	}
	if _, err := m.svc.Write(t.Context(), daemonapp.WriteRequest{SessionID: started.SessionID, AuthorityEpoch: started.AuthorityEpoch, InputOffset: 0, Chars: "must-not-run\n"}); err == nil {
		t.Fatal("post-loss write unexpectedly accepted")
	}
	if m.counting.creates.Load() != createsBeforeLoss || countProviderStateFiles(t, m.providerRoot) != 0 {
		t.Fatalf("provider loss recreated or retained state: creates_before=%d creates_after=%d state_files=%d", createsBeforeLoss, m.counting.creates.Load(), countProviderStateFiles(t, m.providerRoot))
	}
}
