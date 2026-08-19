package interactivehandoff

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestRequestClosedVocabularyAndH2Capability(t *testing.T) {
	validReasons := []Reason{ReasonCredentialRequired, ReasonAuthenticationRequired, ReasonHumanConfirmation, ReasonComplexInteractiveUI, ReasonManualIntervention}
	for _, reason := range validReasons {
		req := Request{HandoffID: "handoff-1", SessionID: "session-1", Reason: reason, Privacy: PrivacyStandard, Completion: Completion{Kind: CompletionManualReady}}
		if err := req.Validate(); err != nil {
			t.Fatalf("reason %q rejected: %v", reason, err)
		}
		if err := req.ValidateH2(); err != nil {
			t.Fatalf("H2 standard/manual rejected: %v", err)
		}
	}

	secret := Request{HandoffID: "handoff-secret", SessionID: "session-1", Reason: ReasonCredentialRequired, Privacy: PrivacySecret, Completion: Completion{Kind: CompletionManualReady}}
	if err := secret.Validate(); err != nil {
		t.Fatalf("recognized future secret vocabulary rejected syntactically: %v", err)
	}
	if err := secret.ValidateH2(); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("H2 secret err=%v want feature_unavailable", err)
	}

	automatic := Request{HandoffID: "handoff-auto", SessionID: "session-1", Reason: ReasonCredentialRequired, Privacy: PrivacyStandard, Completion: Completion{Kind: CompletionEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"}}
	if err := automatic.Validate(); err != nil {
		t.Fatalf("recognized future completion vocabulary rejected syntactically: %v", err)
	}
	if err := automatic.ValidateH2(); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("H2 automatic completion err=%v want feature_unavailable", err)
	}
}

func TestRequestRejectsUnknownOrMalformedVocabulary(t *testing.T) {
	base := Request{HandoffID: "handoff-1", SessionID: "session-1", Reason: ReasonManualIntervention, Privacy: PrivacyStandard, Completion: Completion{Kind: CompletionManualReady}}
	cases := []Request{
		{SessionID: base.SessionID, Reason: base.Reason, Privacy: base.Privacy, Completion: base.Completion},
		{HandoffID: base.HandoffID, Reason: base.Reason, Privacy: base.Privacy, Completion: base.Completion},
		{HandoffID: base.HandoffID, SessionID: base.SessionID, Reason: "future", Privacy: base.Privacy, Completion: base.Completion},
		{HandoffID: base.HandoffID, SessionID: base.SessionID, Reason: base.Reason, Privacy: "future", Completion: base.Completion},
		{HandoffID: base.HandoffID, SessionID: base.SessionID, Reason: base.Reason, Privacy: base.Privacy, Completion: Completion{Kind: "script"}},
		{HandoffID: base.HandoffID, SessionID: base.SessionID, Reason: base.Reason, Privacy: base.Privacy, Completion: Completion{Kind: CompletionEnvironmentExportedNonempty}},
		{HandoffID: base.HandoffID, SessionID: base.SessionID, Reason: base.Reason, Privacy: base.Privacy, Completion: Completion{Kind: CompletionManualReady, Name: "unexpected"}},
	}
	for i, req := range cases {
		if err := req.Validate(); err == nil {
			t.Fatalf("bad request[%d] accepted: %#v", i, req)
		}
	}
}

func TestH2FailureCodesProjectOnlySafeDetails(t *testing.T) {
	codes := []failure.Code{
		failure.HandoffConflict,
		failure.HandoffNotPending,
		failure.HandoffExpired,
		failure.HandoffClientLost,
		failure.HandoffReclaimBlocked,
		failure.HumanControlUnreachable,
		failure.HumanClientNotProven,
	}
	for _, code := range codes {
		pub := failure.Public(failure.New(code, map[string]string{"handoff_id": "handoff-1", "control_id": "control-1", "phase": "human_owned", "reason": "provider_mismatch", "private_socket": "/tmp/secret.sock"}, nil))
		if pub.Code != code || pub.Message == "" {
			t.Fatalf("code %q not publicly specified: %#v", code, pub)
		}
		if _, leaked := pub.Details["private_socket"]; leaked {
			t.Fatalf("code %q leaked private detail: %#v", code, pub.Details)
		}
	}
}
