//go:build darwin

package integration_test

import (
	"testing"
	"time"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalcore "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestInteractiveHandoffH2H4WithoutH3Composition(t *testing.T) {
	m := newH4SecretManualNative(t)
	started := startH2ManualSession(t, m)
	info, err := m.svc.InspectServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	support := info.Capabilities.InteractiveHandoff
	if support == nil || !support.ManualStandard || !support.Secret || support.TerminalPresentation != nil || support.AutomaticReadiness {
		t.Fatalf("H2+H4/no-H3 capabilities=%#v", support)
	}
	public, human := enterH4SecretHumanOwnership(t, m, started.SessionID, "handoff-h4-no-h3")
	if public.AttachArgv == nil || public.PrivacyState != handoff.PrivacyPrivate || human.state.Phase != handoff.PhaseHumanOwned || human.state.PrivacyState != handoff.PrivacyPrivate {
		t.Fatalf("manual secret fallback public=%#v human=%#v", public, human.state)
	}
	h4AbortTerminate(t, m, human, "no-h3")
}

func TestInteractiveHandoffH2H3H4CompositionKeepsPresentationAndPrivacyIndependent(t *testing.T) {
	m, launcher, activity, running := newH3H4ComposedNative(t)
	started := startH2ManualSession(t, m)
	info, err := m.svc.InspectServer(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	support := info.Capabilities.InteractiveHandoff
	if support == nil || !support.ManualStandard || !support.Secret || support.TerminalPresentation == nil {
		t.Fatalf("H2+H3+H4 capabilities=%#v", support)
	}
	req := handoff.Request{HandoffID: "handoff-h3-h4-composed", SessionID: started.SessionID, Reason: handoff.ReasonCredentialRequired, Privacy: handoff.PrivacySecret, Completion: handoff.Completion{Kind: handoff.CompletionManualReady}}
	presented, err := m.svc.RequestHandoffPublicWithPresentation(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if presented.Status != handoff.StatusHumanConnecting || presented.PrivacyState != handoff.PrivacyPrivate || launcher.calls.Load() != 1 || activity.calls.Load() != 1 || running.calls.Load() != 1 {
		t.Fatalf("presentation/private composition public=%#v activity=%d running=%d launch=%d", presented, activity.calls.Load(), running.calls.Load(), launcher.calls.Load())
	}
	_, human := enterH4SecretHumanOwnership(t, m, started.SessionID, req.HandoffID)
	if human.state.Phase != handoff.PhaseHumanOwned || human.state.PrivacyState != handoff.PrivacyPrivate || launcher.calls.Load() != 1 {
		t.Fatalf("manual attach after presentation human=%#v launches=%d", human.state, launcher.calls.Load())
	}
	h4AbortTerminate(t, m, human, "h3-h4")
}

func newH3H4ComposedNative(t *testing.T) (*h2ManualNative, *h3CompositionLauncher, *h3CompositionActivity, *h3CompositionRunning) {
	t.Helper()
	m := newH2ManualNative(t)
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	identity := h3TerminalIdentity("ghostty", "com.mitchellh.ghostty")
	running := &h3CompositionRunning{identity: &identity}
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
	catalog := h1DelegatedCatalog().WithInteractiveHandoff(h4SecretSupport())
	catalog = catalog.WithTerminalPresentation(capability.TerminalPresentationSupport{
		ResolutionSources: []string{"active", "recent", "bridge_affinity", "single_running"}, QualifiedLaunchers: []string{"ghostty"},
	})
	options := daemonapp.Options{
		Incarnation: "h3-h4-composition-integration", Shell: "/bin/sh", MaxQueuedInputBytes: 1 << 16,
		DelegatedRuntime: m.provider, Capabilities: catalog,
	}
	options.HandoffPresenterFactory = func(prover handoffapp.ExactClientProver) handoffapp.Presenter {
		launch := terminalapp.NewLaunchService(m.store, launcher, prover)
		return terminalapp.NewPresenter(resolver, launch, "/opt/shellbeam/bin/shellbeam", nil)
	}
	m.svc = daemonapp.NewService(m.store, &h1ImmediateOwner{}, options)
	return m, launcher, activity, running
}

func h4AbortTerminate(t *testing.T, m *h2ManualNative, human h2HumanAttach, suffix string) {
	t.Helper()
	aborted, err := m.svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{
		HandoffID: human.state.HandoffID, AuthorityEpoch: human.state.AuthorityEpoch,
		ControlID: "h4-composition-abort-" + suffix, Kind: handoff.HumanControlAbort,
	})
	if err != nil || aborted.Outcome != "aborted" || aborted.State.PrivacyState != handoff.PrivacyPrivate {
		t.Fatalf("abort=%#v err=%v", aborted, err)
	}
	terminated, err := m.svc.HandoffHumanControl(t.Context(), handoff.ControlSignal{
		HandoffID: aborted.State.HandoffID, AuthorityEpoch: aborted.State.AuthorityEpoch,
		ControlID: "h4-composition-terminate-" + suffix, Kind: handoff.HumanControlTerminate,
	})
	if err != nil || terminated.Outcome != "terminated" {
		t.Fatalf("terminate=%#v err=%v", terminated, err)
	}
	_ = waitH1Terminal(t, m.svc, human.state.SessionID)
}

var _ terminalcore.TerminalIdentity
