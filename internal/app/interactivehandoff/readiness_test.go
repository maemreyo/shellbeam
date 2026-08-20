package interactivehandoff

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type fakeReadinessWatcher struct {
	events chan shellapp.WatchEvent
	closed atomic.Int32
	calls  *[]string
}

func newFakeReadinessWatcher(calls *[]string) *fakeReadinessWatcher {
	return &fakeReadinessWatcher{events: make(chan shellapp.WatchEvent, 1), calls: calls}
}
func (w *fakeReadinessWatcher) Wait(ctx context.Context) (shellapp.WatchEvent, error) {
	select {
	case event := <-w.events:
		return event, nil
	case <-ctx.Done():
		return shellapp.WatchEvent{}, ctx.Err()
	}
}
func (w *fakeReadinessWatcher) Close() error {
	if w.closed.Add(1) == 1 && w.calls != nil {
		*w.calls = append(*w.calls, "close_readiness")
	}
	return nil
}

type fakeReadinessPreparer struct {
	mu          sync.Mutex
	calls       *[]string
	recordClose bool
	shell       shellcore.ShellIdentity
	watchers    []*fakeReadinessWatcher
	requests    []ReadinessRequest
	err         error
}

func newFakeReadinessPreparer(calls *[]string) *fakeReadinessPreparer {
	return &fakeReadinessPreparer{calls: calls, shell: shellcore.ShellIdentity{Family: shellcore.ShellFish, RuntimeID: "shell-runtime-h4"}}
}

func (p *fakeReadinessPreparer) Prepare(_ context.Context, req ReadinessRequest) (PreparedReadiness, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls != nil {
		*p.calls = append(*p.calls, "prepare_readiness")
	}
	p.requests = append(p.requests, req)
	if p.err != nil {
		return PreparedReadiness{}, p.err
	}
	var closeCalls *[]string
	if p.recordClose {
		closeCalls = p.calls
	}
	watcher := newFakeReadinessWatcher(closeCalls)
	p.watchers = append(p.watchers, watcher)
	return PreparedReadiness{Shell: p.shell, Watcher: watcher}, nil
}

func (p *fakeReadinessPreparer) latest() *fakeReadinessWatcher {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.watchers) == 0 {
		return nil
	}
	return p.watchers[len(p.watchers)-1]
}
func (p *fakeReadinessPreparer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.watchers)
}

func TestAutomaticReadinessReclaimsAgentBeforeForwardOnlyPrivacyRelease(t *testing.T) {
	_, runtime, _, svc, readiness, calls, req := secretFixture(t, true)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	attached, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, shellHumanAttachSpec())
	if err != nil {
		t.Fatal(err)
	}
	watcher := readiness.latest()
	if watcher == nil {
		t.Fatal("readiness watcher not prepared")
	}
	now := time.Now().UTC()
	watcher.events <- shellapp.WatchEvent{
		Result: shellcore.RequirementResult{
			Requirement: shellcore.Requirement{Kind: shellcore.RequirementEnvironmentExportedNonempty, Name: req.Completion.Name},
			State:       shellcore.RequirementSatisfied, Quality: shellcore.RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now,
		},
		Boundary: shellcore.BoundaryProof{HandoffID: req.HandoffID, AuthorityEpoch: attached.State.AuthorityEpoch, Shell: readiness.shell, Quality: shellcore.BoundaryQualityShellPrompt, ObservedAt: now},
	}
	state := waitForH4State(t, svc, req.HandoffID, func(v handoff.State) bool {
		return v.Phase == handoff.PhaseAgentOwned && v.PrivacyRelease == handoff.PrivacyReleaseProven && v.CaptureState == handoff.CapturePublic
	})
	if state.TransferBoundary.Kind != handoff.BoundaryShell || !state.TransferBoundary.Established || state.AgentIngress != handoff.IngressWritable || state.HumanIngress != handoff.IngressFenced {
		t.Fatalf("automatic state=%#v", state)
	}
	assertCallBefore(t, *calls, "fence_human", "release_private")
	assertCallBefore(t, *calls, "prepare_readonly_control", "release_private")
	assertCallBefore(t, *calls, "inspect_agent", "release_private")
	if runtime.releases != 1 || !runtime.lastBoundary.Proof.ForwardOnly || runtime.lastBoundary.Proof.AuthorityEpoch != attached.State.AuthorityEpoch || runtime.lastBoundary.Proof.Shell != readiness.shell {
		t.Fatalf("release=%#v count=%d", runtime.lastBoundary, runtime.releases)
	}
	if watcher.closed.Load() == 0 {
		t.Fatal("successful readiness watcher not closed")
	}
}

func TestUnsatisfiedAutomaticReadinessNeverReclaimsOrReleasesPrivacy(t *testing.T) {
	_, runtime, _, svc, readiness, _, req := secretFixture(t, true)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	attached, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, shellHumanAttachSpec())
	if err != nil {
		t.Fatal(err)
	}
	watcher := readiness.latest()
	now := time.Now().UTC()
	watcher.events <- shellapp.WatchEvent{
		Result:   shellcore.RequirementResult{Requirement: shellcore.Requirement{Kind: shellcore.RequirementEnvironmentExportedNonempty, Name: req.Completion.Name}, State: shellcore.RequirementNotSatisfied, Quality: shellcore.RequirementQualityExactShellAdapter, SafeBoundary: true, ObservedAt: now},
		Boundary: shellcore.BoundaryProof{HandoffID: req.HandoffID, AuthorityEpoch: attached.State.AuthorityEpoch, Shell: readiness.shell, Quality: shellcore.BoundaryQualityShellPrompt, ObservedAt: now},
	}
	waitForWatcherClose(t, watcher)
	state, err := svc.Inspect(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != handoff.PhaseHumanOwned || state.PrivacyRelease != handoff.PrivacyReleasePending || state.CaptureState != handoff.CapturePrivate || runtime.releases != 0 {
		t.Fatalf("unsatisfied readiness changed authority/privacy: %#v releases=%d", state, runtime.releases)
	}
}

func waitForH4State(t *testing.T, svc *Service, id string, predicate func(handoff.State) bool) handoff.State {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := svc.Inspect(t.Context(), id)
		if err == nil && predicate(state) {
			return state
		}
		time.Sleep(5 * time.Millisecond)
	}
	state, _ := svc.Inspect(t.Context(), id)
	t.Fatalf("state did not converge: %#v", state)
	return handoff.State{}
}

func waitForWatcherClose(t *testing.T, watcher *fakeReadinessWatcher) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if watcher.closed.Load() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("watcher not closed")
}

func shellHumanAttachSpec() delegatedapp.HumanAttachSpec { return delegatedapp.HumanAttachSpec{} }

var _ ReadinessPreparer = (*fakeReadinessPreparer)(nil)
var _ shellapp.RequirementWatcher = (*fakeReadinessWatcher)(nil)
var _ = delegated.OwnerHuman

func TestReconcileSecretHumanOwnedReprovesPrivateAndDegradesAutomaticWatcherToManual(t *testing.T) {
	store, runtime, fencer, svc, _, _, req := secretFixture(t, true)
	if _, err := svc.Request(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachLocalHuman(t.Context(), req.HandoffID, shellHumanAttachSpec()); err != nil {
		t.Fatal(err)
	}

	// Simulate daemon/coordinator restart: durable store/provider survive, but all
	// in-memory privacy/readiness preparation is gone.
	beforeArm := len(runtime.armEpochs)
	restarted := New(store, runtime, fencer)
	restarted.EnableH4()
	readiness := newFakeReadinessPreparer(nil)
	restarted.SetReadiness(readiness)
	state, err := restarted.Reconcile(t.Context(), req.HandoffID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != handoff.PhaseHumanOwned || state.PrivacyRelease != handoff.PrivacyReleasePending || state.CaptureState != handoff.CapturePrivate {
		t.Fatalf("reconciled=%#v", state)
	}
	if len(runtime.armEpochs) != beforeArm+1 || runtime.armEpochs[len(runtime.armEpochs)-1] != state.AuthorityEpoch {
		t.Fatalf("privacy was not rebound/reproved after restart: epochs=%v", runtime.armEpochs)
	}
	if readiness.count() != 0 {
		t.Fatalf("restart injected automatic watcher while human writable: count=%d", readiness.count())
	}
}
