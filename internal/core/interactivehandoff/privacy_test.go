package interactivehandoff

import (
	"errors"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestH4PrivateStateKeepsTransferBoundaryIndependentFromPrivacyRelease(t *testing.T) {
	state := h4PrivateState()
	state.TransferBoundary = TransferBoundary{Kind: BoundaryHumanAttested, Established: true}
	state.PrivacyRelease = PrivacyReleasePending
	state.CaptureState = CapturePrivate
	if err := state.Validate(); err != nil {
		t.Fatalf("human-attested transfer with private capture rejected: %v", err)
	}
	if err := state.ValidateH4(); err != nil {
		t.Fatalf("H4 state rejected: %v", err)
	}
	if err := state.ValidateH2(); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("H2 accepted secret state: %v", err)
	}
}

func TestH4PrivateCaptureRequiresIndependentReleaseBeforePublic(t *testing.T) {
	pendingPublic := h4PrivateState()
	pendingPublic.PrivacyRelease = PrivacyReleasePending
	pendingPublic.CaptureState = CapturePublic
	if err := pendingPublic.Validate(); err == nil {
		t.Fatal("public capture accepted before privacy release proof")
	}

	provenStillPrivate := h4PrivateState()
	provenStillPrivate.PrivacyRelease = PrivacyReleaseProven
	provenStillPrivate.CaptureState = CapturePrivate
	if err := provenStillPrivate.ValidateH4(); err != nil {
		t.Fatalf("proof-before-topology-reopen transition rejected: %v", err)
	}

	provenPublic := h4PrivateState()
	provenPublic.PrivacyRelease = PrivacyReleaseProven
	provenPublic.CaptureState = CapturePublic
	if err := provenPublic.ValidateH4(); err != nil {
		t.Fatalf("public capture after privacy release rejected: %v", err)
	}
}

func TestH4RequestAcceptsSecretManualAndTypedAutomaticCompletion(t *testing.T) {
	requests := []Request{
		{HandoffID: "handoff-secret-manual", SessionID: "session-1", Reason: ReasonCredentialRequired, Privacy: PrivacySecret, Completion: Completion{Kind: CompletionManualReady}},
		{HandoffID: "handoff-secret-auto", SessionID: "session-1", Reason: ReasonCredentialRequired, Privacy: PrivacySecret, Completion: Completion{Kind: CompletionEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}},
	}
	for _, req := range requests {
		if err := req.ValidateH4(); err != nil {
			t.Fatalf("H4 request rejected: %v", err)
		}
	}
}

func TestH4FailureCodesProjectOnlySafeMetadata(t *testing.T) {
	codes := []failure.Code{
		failure.PrivateOutputBarrierFailed,
		failure.PrivacyReleaseUnproven,
		failure.ShellIntegrationUnavailable,
		failure.ShellIntegrationLost,
		failure.RequirementUnsupported,
		failure.RequirementNotSatisfied,
		failure.ShellIdentityChanged,
	}
	for _, code := range codes {
		pub := failure.Public(failure.New(code, map[string]string{
			"handoff_id": "handoff-1", "shell": "fish", "kind": "environment_exported_nonempty", "reason": "proof_unavailable",
			"value": "TOP_SECRET", "hash": "deadbeef", "path": "/bin/fish",
		}, nil))
		if pub.Code != code || pub.Message == "" {
			t.Fatalf("code %q missing public contract: %#v", code, pub)
		}
		for _, forbidden := range []string{"value", "hash", "path"} {
			if _, ok := pub.Details[forbidden]; ok {
				t.Fatalf("code %q leaked %s: %#v", code, forbidden, pub.Details)
			}
		}
	}
}

func h4PrivateState() State {
	return State{
		SchemaVersion:      StateSchemaVersion,
		HandoffID:          "handoff-1",
		SessionID:          "session-1",
		Phase:              PhaseReclaimPending,
		AuthorityEpoch:     delegated.AuthorityEpoch(4),
		DesiredOwner:       delegated.OwnerAgent,
		ProviderOwner:      delegated.OwnerNone,
		AgentIngress:       IngressFenced,
		HumanIngress:       IngressFenced,
		TransferBoundary:   TransferBoundary{Kind: BoundaryNone, Established: false},
		PrivacyState:       PrivacyPrivate,
		PrivacyRelease:     PrivacyReleasePending,
		CaptureState:       CapturePrivate,
		ProviderGeneration: "generation-1",
	}
}
