package terminalpresentation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type fakeTerminalLaunchStore struct {
	record        *LaunchRecord
	reserveCalls  int
	completeCalls int
	events        []string
}

func (s *fakeTerminalLaunchStore) ReserveTerminalLaunch(_ context.Context, want LaunchRecord) (LaunchRecord, bool, error) {
	s.reserveCalls++
	s.events = append(s.events, "reserve")
	if s.record != nil {
		return *s.record, false, nil
	}
	copy := want
	s.record = &copy
	return copy, true, nil
}

func (s *fakeTerminalLaunchStore) CompleteTerminalLaunch(_ context.Context, want LaunchRecord) (LaunchRecord, error) {
	s.completeCalls++
	s.events = append(s.events, "complete")
	copy := want
	s.record = &copy
	return copy, nil
}

type fakeLaunchExecutor struct {
	store  *fakeTerminalLaunchStore
	result LaunchResult
	err    error
	calls  int
}

func (l *fakeLaunchExecutor) Launch(_ context.Context, _ LaunchRequest) (LaunchResult, error) {
	l.calls++
	if l.store == nil || l.store.record == nil || l.store.record.State != core.LaunchLaunching {
		panic("GUI side effect occurred before durable launching reservation")
	}
	l.store.events = append(l.store.events, "launch")
	return l.result, l.err
}

type fakeExactClientProver struct {
	store   *fakeTerminalLaunchStore
	answers []bool
	err     error
	calls   int
}

func (p *fakeExactClientProver) ExactHumanClientPresent(_ context.Context, _ string) (bool, error) {
	p.calls++
	if p.store != nil {
		p.store.events = append(p.store.events, "proof")
	}
	if p.err != nil {
		return false, p.err
	}
	if len(p.answers) == 0 {
		return false, nil
	}
	answer := p.answers[0]
	p.answers = p.answers[1:]
	return answer, nil
}

func TestEnsurePresentedReservesBeforeLaunchAndPromotesOnlyWithExactClientProof(t *testing.T) {
	store := &fakeTerminalLaunchStore{}
	launcher := &fakeLaunchExecutor{store: store, result: LaunchResult{
		Attempted: true, Outcome: core.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "client_not_proven",
	}}
	prover := &fakeExactClientProver{store: store, answers: []bool{true}}
	svc := NewLaunchService(store, launcher, prover)

	got, err := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != core.LaunchLaunchedAndClientProven || launcher.calls != 1 || prover.calls != 1 {
		t.Fatalf("record=%#v launch_calls=%d proof_calls=%d", got, launcher.calls, prover.calls)
	}
	wantEvents := []string{"reserve", "launch", "proof", "complete"}
	if strings.Join(store.events, ",") != strings.Join(wantEvents, ",") {
		t.Fatalf("events=%v want=%v", store.events, wantEvents)
	}
}

func TestEnsurePresentedKnownFailureReplaysExactlyWithoutRelaunch(t *testing.T) {
	store := &fakeTerminalLaunchStore{}
	launchErr := failure.New(failure.TerminalLaunchFailed, map[string]string{"provider_id": "ghostty", "reason": "launcher_start_failed"}, errors.New("private launch detail"))
	launcher := &fakeLaunchExecutor{store: store, result: LaunchResult{
		Attempted: false, Outcome: core.LaunchOutcomeFailed, ProviderID: "ghostty", Reason: "launcher_start_failed",
	}, err: launchErr}
	prover := &fakeExactClientProver{store: store}
	svc := NewLaunchService(store, launcher, prover)

	first, firstErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	second, secondErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if first.State != core.LaunchFailed || second.State != core.LaunchFailed || launcher.calls != 1 || prover.calls != 0 {
		t.Fatalf("first=%#v second=%#v launch_calls=%d proof_calls=%d", first, second, launcher.calls, prover.calls)
	}
	for _, err := range []error{firstErr, secondErr} {
		public := failure.Public(err)
		if public.Code != failure.TerminalLaunchFailed || public.Details["provider_id"] != "ghostty" || public.Details["reason"] != "launcher_start_failed" {
			t.Fatalf("replayed failure=%#v", public)
		}
	}
}

func TestEnsurePresentedUnknownRetryNeverBlindRelaunchAndCanPromoteFromProof(t *testing.T) {
	store := &fakeTerminalLaunchStore{}
	launcher := &fakeLaunchExecutor{store: store, result: LaunchResult{
		Attempted: true, Outcome: core.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "client_not_proven",
	}}
	prover := &fakeExactClientProver{store: store, answers: []bool{false, false, true}}
	svc := NewLaunchService(store, launcher, prover)

	first, firstErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if first.State != core.LaunchOutcomeUnknownState || failure.Public(firstErr).Code != failure.TerminalLaunchUnknown {
		t.Fatalf("first=%#v err=%v", first, firstErr)
	}
	second, secondErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if second.State != core.LaunchOutcomeUnknownState || failure.Public(secondErr).Code != failure.TerminalLaunchUnknown {
		t.Fatalf("second=%#v err=%v", second, secondErr)
	}
	third, thirdErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if thirdErr != nil || third.State != core.LaunchLaunchedAndClientProven {
		t.Fatalf("third=%#v err=%v", third, thirdErr)
	}
	if launcher.calls != 1 || prover.calls != 3 {
		t.Fatalf("launch_calls=%d proof_calls=%d", launcher.calls, prover.calls)
	}
}

func TestEnsurePresentedRecoveredLaunchingInspectsInsteadOfLaunchingAgain(t *testing.T) {
	reservation, err := NewLaunchReservation("handoff-safe", ghosttyIdentity(), safeAttachArgv())
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeTerminalLaunchStore{record: &reservation}
	launcher := &fakeLaunchExecutor{store: store}
	prover := &fakeExactClientProver{store: store, answers: []bool{false}}
	svc := NewLaunchService(store, launcher, prover)

	got, gotErr := svc.EnsurePresented(t.Context(), "handoff-safe", ghosttyResolution(), safeAttachArgv())
	if got.State != core.LaunchOutcomeUnknownState || failure.Public(gotErr).Code != failure.TerminalLaunchUnknown {
		t.Fatalf("got=%#v err=%v", got, gotErr)
	}
	if launcher.calls != 0 || prover.calls != 1 {
		t.Fatalf("recovered launching retried GUI launch: launch=%d proof=%d", launcher.calls, prover.calls)
	}
}

func TestLaunchRecordStoresDigestsNotAttachPath(t *testing.T) {
	privateExecutable := "/Users/example/private/bin/shellbeam"
	record, err := NewLaunchReservation("handoff-safe", ghosttyIdentity(), []string{privateExecutable, "session", "attach", "--handoff-id", "handoff-safe"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), privateExecutable) || len(record.AttachTargetFingerprint) != 64 || len(record.AttemptID) != 64 {
		t.Fatalf("durable record leaks attach target or lacks digest: %s", encoded)
	}
}

func ghosttyIdentity() core.TerminalIdentity {
	return core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
}

func ghosttyResolution() core.Resolution {
	candidate := core.Candidate{Evidence: core.Evidence{Identity: ghosttyIdentity(), Source: core.SourceFallback, ObservedAt: testLaunchTime, FreshUntil: testLaunchTime.Add(testLaunchFreshness), Quality: core.QualityQualified}}
	return core.Resolution{Selected: &candidate}
}

var testLaunchTime = mustLaunchTime("2026-08-19T17:00:00Z")

const testLaunchFreshness = time.Minute

func mustLaunchTime(value string) (out time.Time) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func safeAttachArgv() []string {
	return []string{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-safe"}
}
