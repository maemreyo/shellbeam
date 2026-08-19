package terminalpresentation

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestLaunchStateMachineAllowsOnlyExplicitBeginAndTerminalCompletion(t *testing.T) {
	state, err := BeginLaunch(LaunchNotAttempted)
	if err != nil || state != LaunchLaunching {
		t.Fatalf("BeginLaunch()=(%q,%v) want launching,nil", state, err)
	}

	cases := []struct {
		outcome LaunchOutcome
		want    LaunchState
	}{
		{LaunchOutcomeClientProven, LaunchLaunchedAndClientProven},
		{LaunchOutcomeFailed, LaunchFailed},
		{LaunchOutcomeUnknown, LaunchOutcomeUnknownState},
	}
	for _, tc := range cases {
		got, err := CompleteLaunch(LaunchLaunching, tc.outcome)
		if err != nil || got != tc.want {
			t.Fatalf("CompleteLaunch(%q)=(%q,%v) want %q,nil", tc.outcome, got, err, tc.want)
		}
	}

	if _, err := BeginLaunch(LaunchLaunching); err == nil {
		t.Fatal("launching state must not start another GUI side effect")
	}
	if _, err := BeginLaunch(LaunchOutcomeUnknownState); err == nil {
		t.Fatal("unknown outcome must not blindly retry")
	}
	if _, err := CompleteLaunch(LaunchNotAttempted, LaunchOutcomeFailed); err == nil {
		t.Fatal("completion without durable launching reservation must fail")
	}
}

func TestLaunchStatesAndOutcomesRejectUnknownValues(t *testing.T) {
	for _, state := range []LaunchState{
		LaunchNotAttempted,
		LaunchLaunching,
		LaunchLaunchedAndClientProven,
		LaunchFailed,
		LaunchOutcomeUnknownState,
	} {
		if err := state.Validate(); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	if err := LaunchState("retrying").Validate(); err == nil {
		t.Fatal("unknown launch state accepted")
	}

	for _, outcome := range []LaunchOutcome{LaunchOutcomeClientProven, LaunchOutcomeFailed, LaunchOutcomeUnknown} {
		if err := outcome.Validate(); err != nil {
			t.Fatalf("outcome %q rejected: %v", outcome, err)
		}
	}
	if err := LaunchOutcome("maybe").Validate(); err == nil {
		t.Fatal("unknown launch outcome accepted")
	}
}

func TestTerminalPresentationFailureCodesAreStableAndDoNotExposePaths(t *testing.T) {
	cases := []struct {
		code      failure.Code
		retryable bool
	}{
		{failure.TerminalLauncherUnavailable, false},
		{failure.TerminalLaunchFailed, false},
		{failure.TerminalLaunchUnknown, true},
		{failure.TerminalIdentityAmbiguous, false},
	}
	for _, tc := range cases {
		got := failure.Public(failure.New(tc.code, map[string]string{
			"provider_id": "ghostty",
			"reason":      "native_probe_failed",
			"path":        "/Users/example/private/Ghostty.app",
		}, nil))
		if got.Code != tc.code || got.Message == "" || got.Retryable != tc.retryable {
			t.Fatalf("public failure=%+v for %q", got, tc.code)
		}
		if _, ok := got.Details["path"]; ok {
			t.Fatalf("private path leaked for %q: %+v", tc.code, got.Details)
		}
	}
}
