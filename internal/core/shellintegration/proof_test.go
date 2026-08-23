package shellintegration

import (
	"testing"
	"time"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func TestBoundaryAndPrivacyProofsAreIndependentAndCurrent(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 45, 0, 0, time.UTC)
	fish := ShellIdentity{Family: ShellFish, RuntimeID: "shell-runtime-1"}
	humanReady := BoundaryProof{HandoffID: "handoff-1", AuthorityEpoch: 4, Shell: ShellIdentity{Family: ShellUnknown, RuntimeID: "shell-runtime-1"}, Quality: BoundaryQualityHumanAttested, ObservedAt: now}
	if err := humanReady.Validate(); err != nil {
		t.Fatalf("human-attested transfer proof rejected: %v", err)
	}
	if !humanReady.CurrentFor("handoff-1", delegated.AuthorityEpoch(4), humanReady.Shell) {
		t.Fatal("current human-attested proof not current")
	}

	shellPrompt := BoundaryProof{HandoffID: "handoff-1", AuthorityEpoch: 4, Shell: fish, Quality: BoundaryQualityShellPrompt, ObservedAt: now}
	if err := shellPrompt.Validate(); err != nil {
		t.Fatalf("shell prompt proof rejected: %v", err)
	}
	release := PrivacyReleaseProof{HandoffID: "handoff-1", AuthorityEpoch: 4, Shell: fish, Boundary: "prompt-boundary-1", ForwardOnly: true, ObservedAt: now}
	if err := release.Validate(); err != nil {
		t.Fatalf("privacy release proof rejected: %v", err)
	}

	if shellPrompt.CurrentFor("handoff-1", 5, fish) {
		t.Fatal("stale epoch boundary proof accepted")
	}
	zsh := ShellIdentity{Family: ShellZsh, RuntimeID: "shell-runtime-2"}
	if shellPrompt.CurrentFor("handoff-1", 4, zsh) || release.CurrentFor("handoff-1", 4, zsh) {
		t.Fatal("nested/replaced shell drift accepted")
	}
	if release.CurrentFor("handoff-other", 4, fish) {
		t.Fatal("proof accepted for different handoff")
	}
}

func TestHumanAttestationCannotBecomePrivacyReleaseAndIdleProducesNoProof(t *testing.T) {
	now := time.Date(2026, 8, 20, 8, 45, 0, 0, time.UTC)
	unknown := ShellIdentity{Family: ShellUnknown, RuntimeID: "shell-runtime-unknown"}
	if err := (BoundaryProof{HandoffID: "handoff-1", AuthorityEpoch: 2, Shell: unknown, Quality: BoundaryQualityHumanAttested, ObservedAt: now}).Validate(); err != nil {
		t.Fatalf("manual transfer boundary rejected: %v", err)
	}
	if err := (PrivacyReleaseProof{HandoffID: "handoff-1", AuthorityEpoch: 2, Shell: unknown, Boundary: "human-ready", ForwardOnly: true, ObservedAt: now}).Validate(); err == nil {
		t.Fatal("human/unknown-shell attestation incorrectly released privacy")
	}
	if err := (BoundaryProof{}).Validate(); err == nil {
		t.Fatal("idle/zero boundary proof accepted")
	}
	if err := (PrivacyReleaseProof{}).Validate(); err == nil {
		t.Fatal("idle/zero privacy release proof accepted")
	}
}
