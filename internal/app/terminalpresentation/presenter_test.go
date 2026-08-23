package terminalpresentation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type fakePresentationResolver struct {
	request ResolveRequest
	result  ResolveResult
	err     error
	calls   int
}

func (r *fakePresentationResolver) Resolve(_ context.Context, request ResolveRequest) (ResolveResult, error) {
	r.calls++
	r.request = request
	return r.result, r.err
}

type fakeLaunchCoordinator struct {
	handoffID  string
	resolution core.Resolution
	argv       []string
	err        error
	calls      int
}

func (l *fakeLaunchCoordinator) EnsurePresented(_ context.Context, handoffID string, resolution core.Resolution, argv []string) (LaunchRecord, error) {
	l.calls++
	l.handoffID = handoffID
	l.resolution = resolution
	l.argv = append([]string(nil), argv...)
	return LaunchRecord{}, l.err
}

func TestPresenterConvertsBridgeHintAndUsesExactInstalledAttachArgv(t *testing.T) {
	hint, err := core.NewBridgeAffinityHint(ghosttyIdentity(), time.Date(2026, 8, 19, 17, 0, 0, 0, time.UTC), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	candidate := core.Candidate{Evidence: core.Evidence{Identity: ghosttyIdentity(), Source: core.SourceBridgeAffinity, ObservedAt: hint.ObservedAt, FreshUntil: hint.FreshUntil, Quality: core.QualityValidated}}
	resolver := &fakePresentationResolver{result: ResolveResult{Resolution: core.Resolution{Selected: &candidate}}}
	launch := &fakeLaunchCoordinator{}
	presenter := NewPresenter(resolver, launch, "/opt/shellbeam/bin/shellbeam", nil)

	if err := presenter.Present(t.Context(), handoffapp.PresentationRequest{HandoffID: "handoff-safe", BridgeAffinity: &hint}); err != nil {
		t.Fatal(err)
	}
	if resolver.calls != 1 || resolver.request.BridgeAffinity == nil || resolver.request.BridgeAffinity.Source != core.SourceBridgeAffinity {
		t.Fatalf("resolver request=%#v", resolver.request)
	}
	wantArgv := []string{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-safe"}
	if launch.calls != 1 || launch.handoffID != "handoff-safe" || !reflect.DeepEqual(launch.argv, wantArgv) {
		t.Fatalf("launch calls=%d handoff=%q argv=%v", launch.calls, launch.handoffID, launch.argv)
	}
}

func TestPresenterNoSelectionReturnsDegradedUnavailableWithoutLaunch(t *testing.T) {
	resolver := &fakePresentationResolver{result: ResolveResult{Resolution: core.Resolution{}}}
	launch := &fakeLaunchCoordinator{}
	presenter := NewPresenter(resolver, launch, "/opt/shellbeam/bin/shellbeam", nil)
	err := presenter.Present(t.Context(), handoffapp.PresentationRequest{HandoffID: "handoff-safe"})
	if failure.Public(err).Code != failure.TerminalLauncherUnavailable || launch.calls != 0 {
		t.Fatalf("err=%v launch_calls=%d", err, launch.calls)
	}
}

func TestPresenterPassesQualifiedFallbackAndDoesNotMaskLaunchUnknown(t *testing.T) {
	now := time.Date(2026, 8, 19, 17, 0, 0, 0, time.UTC)
	fallback := core.Evidence{Identity: ghosttyIdentity(), Source: core.SourceFallback, ObservedAt: now, FreshUntil: now.Add(time.Minute), Quality: core.QualityQualified}
	candidate := core.Candidate{Evidence: fallback}
	resolver := &fakePresentationResolver{result: ResolveResult{Resolution: core.Resolution{Selected: &candidate}}}
	launchErr := failure.New(failure.TerminalLaunchUnknown, map[string]string{"provider_id": "ghostty", "reason": "client_not_proven"}, errors.New("private"))
	launch := &fakeLaunchCoordinator{err: launchErr}
	presenter := NewPresenter(resolver, launch, "/opt/shellbeam/bin/shellbeam", &fallback)
	err := presenter.Present(t.Context(), handoffapp.PresentationRequest{HandoffID: "handoff-safe"})
	if resolver.request.Fallback == nil || *resolver.request.Fallback != fallback || failure.Public(err).Code != failure.TerminalLaunchUnknown {
		t.Fatalf("resolver=%#v err=%v", resolver.request, err)
	}
}
