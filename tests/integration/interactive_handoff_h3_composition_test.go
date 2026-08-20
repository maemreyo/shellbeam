//go:build darwin

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalcore "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestInteractiveHandoffH2H3WithoutH4Composition(t *testing.T) {
	m, launcher, activity, running := newH3ComposedNative(t, true)
	started := startH2ManualSession(t, m)
	if activity.calls.Load() != 0 || running.calls.Load() != 0 || launcher.calls.Load() != 0 {
		t.Fatalf("ordinary execution touched H3 activity=%d running=%d launch=%d", activity.calls.Load(), running.calls.Load(), launcher.calls.Load())
	}

	info, err := m.svc.InspectServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	support := info.Capabilities.InteractiveHandoff
	if support == nil || !support.ManualStandard || support.TerminalPresentation == nil || support.Secret || support.AutomaticReadiness {
		t.Fatalf("H2+H3/no-H4 capabilities=%#v", support)
	}
	if got := support.TerminalPresentation.QualifiedLaunchers; !reflect.DeepEqual(got, []string{"ghostty"}) {
		t.Fatalf("qualified launchers=%v", got)
	}
	secret := handoff.Request{HandoffID: "handoff-h3-secret-rejected", SessionID: started.SessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	if _, err := m.svc.RequestHandoff(t.Context(), secret); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("H3 accidentally enabled H4 secret capability: %v", err)
	}

	req := h3CompositionRequest(started.SessionID, "handoff-h3-lost-response")
	first, err := m.svc.RequestHandoffPublicWithPresentation(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Treat the successful first return as a lost transport response. Replaying the
	// same durable request must resolve again but must never launch a second GUI.
	second, err := m.svc.RequestHandoffPublicWithPresentation(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != handoff.StatusHumanConnecting || !reflect.DeepEqual(second, first) || launcher.calls.Load() != 1 {
		t.Fatalf("first=%#v second=%#v launches=%d", first, second, launcher.calls.Load())
	}
	if activity.calls.Load() != 2 || running.calls.Load() != 2 {
		t.Fatalf("presentation resolution calls activity=%d running=%d", activity.calls.Load(), running.calls.Load())
	}

	manual, human := enterH2ManualHumanOwnership(t, m, started.SessionID, req.HandoffID)
	if manual.Status != handoff.StatusHumanConnecting || human.state.Phase != handoff.PhaseHumanOwned {
		t.Fatalf("manual fallback=%#v human=%#v", manual, human.state)
	}
	assertH2AgentWriteRejected(t, m.svc, started.SessionID, human.state.AuthorityEpoch)
	if launcher.calls.Load() != 1 {
		t.Fatalf("manual fallback caused duplicate GUI launch: %d", launcher.calls.Load())
	}
}

func TestInteractiveHandoffH3UnavailableProviderKeepsH2ManualFallback(t *testing.T) {
	m, launcher, activity, running := newH3ComposedNative(t, false)
	started := startH2ManualSession(t, m)
	req := h3CompositionRequest(started.SessionID, "handoff-h3-provider-unavailable")
	public, err := m.svc.RequestHandoffPublicWithPresentation(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{"shellbeam", "session", "attach", "--handoff-id", req.HandoffID}
	if public.Status != handoff.StatusHumanConnecting || !reflect.DeepEqual(public.AttachArgv, wantArgv) {
		t.Fatalf("manual fallback=%#v", public)
	}
	if launcher.calls.Load() != 0 || activity.calls.Load() != 0 || running.calls.Load() != 0 {
		t.Fatalf("unavailable provider touched H3 activity=%d running=%d launch=%d", activity.calls.Load(), running.calls.Load(), launcher.calls.Load())
	}
}

type h3CompositionLauncher struct{ calls atomic.Int32 }

func (l *h3CompositionLauncher) Launch(context.Context, terminalapp.LaunchRequest) (terminalapp.LaunchResult, error) {
	l.calls.Add(1)
	return terminalapp.LaunchResult{Attempted: true, Outcome: terminalcore.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "client_not_proven"}, nil
}

type h3CompositionActivity struct {
	now   time.Time
	calls atomic.Int32
}

func (s *h3CompositionActivity) Current(context.Context) (terminalapp.ForegroundObservation, error) {
	s.calls.Add(1)
	return terminalapp.ForegroundObservation{ObservedAt: s.now, Quality: terminalcore.QualityNative}, nil
}
func (s *h3CompositionActivity) Run(ctx context.Context, _ func(terminalapp.ForegroundObservation) error) error {
	<-ctx.Done()
	return ctx.Err()
}

type h3CompositionRunning struct {
	identity *terminalcore.TerminalIdentity
	calls    atomic.Int32
}

func (s *h3CompositionRunning) Running(context.Context) ([]terminalcore.TerminalIdentity, error) {
	s.calls.Add(1)
	if s.identity == nil {
		return nil, nil
	}
	return []terminalcore.TerminalIdentity{*s.identity}, nil
}

func newH3ComposedNative(t *testing.T, providerAvailable bool) (*h2ManualNative, *h3CompositionLauncher, *h3CompositionActivity, *h3CompositionRunning) {
	t.Helper()
	m := newH2ManualNative(t)
	now := time.Date(2026, 8, 20, 7, 30, 0, 0, time.UTC)
	identity := h3TerminalIdentity("ghostty", "com.mitchellh.ghostty")
	running := &h3CompositionRunning{}
	if providerAvailable {
		running.identity = &identity
	}
	activity := &h3CompositionActivity{now: now}
	registry, err := terminalapp.NewRecentRegistry(5*time.Second, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := terminalapp.NewResolver(registry, activity, running, 5*time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	launcher := &h3CompositionLauncher{}
	catalog := h1DelegatedCatalog().WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	if providerAvailable {
		catalog = catalog.WithTerminalPresentation(capability.TerminalPresentationSupport{ResolutionSources: []string{"active", "recent", "bridge_affinity", "single_running"}, QualifiedLaunchers: []string{"ghostty"}})
	}
	options := daemonapp.Options{
		Incarnation: "h3-composition-integration", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: m.provider, Capabilities: catalog,
	}
	if providerAvailable {
		options.HandoffPresenterFactory = func(prover handoffapp.ExactClientProver) handoffapp.Presenter {
			launch := terminalapp.NewLaunchService(m.store, launcher, prover)
			return terminalapp.NewPresenter(resolver, launch, "/opt/shellbeam/bin/shellbeam", nil)
		}
	}
	m.svc = daemonapp.NewService(m.store, &h1ImmediateOwner{}, options)
	return m, launcher, activity, running
}

func h3CompositionRequest(sessionID, handoffID string) handoff.Request {
	return handoff.Request{HandoffID: handoffID, SessionID: sessionID, Reason: handoff.ReasonManualIntervention, Privacy: handoff.PrivacyStandard, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
}
