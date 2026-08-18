package operation

import (
	"testing"

	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

func hermeticRequestForTest(inputs ...string) *hermeticcore.Request {
	return &hermeticcore.Request{
		Version:     hermeticcore.RequestVersionV1,
		Mode:        hermeticcore.ModeRequired,
		RepoInputs:  inputs,
		Network:     hermeticcore.NetworkOff,
		Environment: hermeticcore.EnvironmentFixedAllowlist,
		Stdin:       hermeticcore.StdinClosed,
		Writes:      hermeticcore.WritesEphemeralDiscard,
	}
}

func TestHermeticRequestBindsRawRequestAndExecutionIdentity(t *testing.T) {
	base := baseIntent()
	legacyRequest := requestFingerprint(t, base)
	legacyExecution := executionFingerprint(t, base)

	first := base
	first.Hermetic = hermeticRequestForTest("go.mod", "cmd/**", "internal/**")
	second := base
	second.Hermetic = hermeticRequestForTest("internal/**", "go.mod", "cmd/**")
	if got, want := requestFingerprint(t, first), requestFingerprint(t, second); got == legacyRequest || got != want {
		t.Fatalf("request identity got=%s want=%s legacy=%s", got, want, legacyRequest)
	}
	if got, want := executionFingerprint(t, first), executionFingerprint(t, second); got == legacyExecution || got != want {
		t.Fatalf("execution identity got=%s want=%s legacy=%s", got, want, legacyExecution)
	}
}

func TestHermeticRequestRejectsLegacyAndInvalidRawIntent(t *testing.T) {
	legacy := baseIntent()
	legacy.Hermetic = hermeticRequestForTest("go.mod")
	if _, err := legacy.Fingerprint(); err == nil {
		t.Fatal("legacy v1 fingerprint accepted hermetic request")
	}
	invalid := baseIntent()
	invalid.Hermetic = hermeticRequestForTest("../secret")
	if _, err := invalid.RequestFingerprint(); err == nil {
		t.Fatal("invalid hermetic request fingerprinted")
	}
	if _, err := invalid.ExecutionFingerprint("/bin/sh"); err == nil {
		t.Fatal("invalid hermetic execution fingerprinted")
	}
}

func TestTypedHermeticRequestChangesIdentity(t *testing.T) {
	base := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	legacy, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	hermetic := base
	hermetic.Hermetic = hermeticRequestForTest("go.mod", "internal/**")
	got, err := hermetic.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if got == legacy {
		t.Fatal("typed hermetic request folded into legacy identity")
	}
}

func TestHermeticIntentRejectsInteractivePersistentOrStreamingStdin(t *testing.T) {
	base := baseIntent()
	base.Hermetic = hermeticRequestForTest("go.mod")
	cases := []Intent{
		func() Intent { x := base; x.TTY = true; return x }(),
		func() Intent { x := base; x.Persistent = true; x.SessionName = "h"; return x }(),
		func() Intent { x := base; x.StdinMode = StdinModeStream; return x }(),
	}
	for _, intent := range cases {
		if _, err := intent.RequestFingerprint(); err == nil {
			t.Fatalf("accepted contradictory hermetic intent: %#v", intent)
		}
	}
}

func TestTypedHermeticIntentRejectsInteractiveOrPersistent(t *testing.T) {
	base := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test", Hermetic: hermeticRequestForTest("go.mod")}
	cases := []TypedRequestIntent{
		func() TypedRequestIntent { x := base; x.TTY = true; return x }(),
		func() TypedRequestIntent { x := base; x.Persistent = true; x.SessionName = "h"; return x }(),
	}
	for _, intent := range cases {
		if _, err := intent.Fingerprint(); err == nil {
			t.Fatalf("accepted contradictory typed hermetic intent: %#v", intent)
		}
	}
}
