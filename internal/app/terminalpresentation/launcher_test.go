package terminalpresentation

import (
	"reflect"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestBuildAttachArgvUsesExactExecutableAndValidatedHandoffID(t *testing.T) {
	got, err := BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-task5-safe_1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-task5-safe_1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv=%q want=%q", got, want)
	}
	if err := ValidateAttachArgv(got); err != nil {
		t.Fatalf("built argv invalid: %v", err)
	}
}

func TestBuildAttachArgvRejectsRelativeExecutableAndUnsafeHandoffID(t *testing.T) {
	for _, tc := range []struct {
		executable string
		handoffID  string
	}{
		{"shellbeam", "handoff-safe"},
		{"./shellbeam", "handoff-safe"},
		{"/bin/sh", "handoff-safe"},
		{"/usr/bin/python3", "handoff-safe"},
		{"/opt/shellbeam/bin/shellbeam\x00bad", "handoff-safe"},
		{"/opt/shellbeam/bin/shellbeam", "bad id; echo owned"},
		{"/opt/shellbeam/bin/shellbeam", ""},
	} {
		if _, err := BuildAttachArgv(tc.executable, tc.handoffID); err == nil {
			t.Fatalf("unsafe attach argv accepted: %+v", tc)
		}
	}
}

func TestValidateAttachArgvRejectsShellAndShapeMutation(t *testing.T) {
	valid := []string{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-safe"}
	cases := [][]string{
		nil,
		{"/bin/sh", "-c", "shellbeam session attach --handoff-id handoff-safe"},
		{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-safe", "extra"},
		{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id=handoff-safe"},
		{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "bad/id"},
	}
	for i, argv := range cases {
		if err := ValidateAttachArgv(argv); err == nil {
			t.Fatalf("case %d unexpectedly valid: %q", i, argv)
		}
	}
	copyArgv := append([]string(nil), valid...)
	if err := ValidateAttachArgv(copyArgv); err != nil {
		t.Fatalf("valid argv rejected: %v", err)
	}
}

func TestLaunchRequestValidatesExactIdentityAndDefensivelyCopiesArgv(t *testing.T) {
	identity := core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
	argv, _ := BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-safe")
	req, err := NewLaunchRequest(identity, argv)
	if err != nil {
		t.Fatal(err)
	}
	argv[0] = "/bin/sh"
	got := req.AttachArgv()
	if got[0] != "/opt/shellbeam/bin/shellbeam" {
		t.Fatalf("request aliases caller argv: %q", got)
	}
	got[0] = "/bin/false"
	if req.AttachArgv()[0] != "/opt/shellbeam/bin/shellbeam" {
		t.Fatal("request leaks mutable argv")
	}

	badIdentity := identity
	badIdentity.ExecutableName = "/Applications/Ghostty.app/Contents/MacOS/ghostty"
	if _, err := NewLaunchRequest(badIdentity, req.AttachArgv()); err == nil {
		t.Fatal("arbitrary executable path accepted as terminal identity")
	}
}

func TestLaunchResultRequiresConsistentAttemptOutcome(t *testing.T) {
	valid := []LaunchResult{
		{Attempted: false, Outcome: core.LaunchOutcomeFailed, ProviderID: "ghostty", Reason: "start_failed"},
		{Attempted: true, Outcome: core.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "client_not_proven"},
		{Attempted: true, Outcome: core.LaunchOutcomeClientProven, ProviderID: "ghostty", Reason: "exact_client_proven"},
	}
	for _, result := range valid {
		if err := result.Validate(); err != nil {
			t.Fatalf("valid result rejected: %+v err=%v", result, err)
		}
	}
	invalid := []LaunchResult{
		{},
		{Attempted: false, Outcome: core.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "unknown"},
		{Attempted: false, Outcome: core.LaunchOutcomeClientProven, ProviderID: "ghostty", Reason: "impossible"},
		{Attempted: true, Outcome: core.LaunchOutcomeFailed, ProviderID: "ghostty", Reason: "contradictory"},
		{Attempted: true, Outcome: core.LaunchOutcomeUnknown, ProviderID: "Ghostty", Reason: "bad_provider"},
	}
	for i, result := range invalid {
		if err := result.Validate(); err == nil {
			t.Fatalf("invalid result %d accepted: %+v", i, result)
		}
	}
}
