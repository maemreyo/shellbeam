package browserbridge

import "testing"

func TestRequestValidateAcceptsExactlyTheClosedVerbSet(t *testing.T) {
	for _, verb := range []Verb{VerbActivityFacts, VerbActivityEvents, VerbVerificationFacts, VerbStructuredFailureFacts} {
		req := Request{ProtocolVersion: ProtocolVersion, Verb: verb, CorrelationID: "chatgpt-wt-01"}
		if err := req.Validate(); err != nil {
			t.Fatalf("verb %q rejected: %v", verb, err)
		}
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbHello}).Validate(); err != nil {
		t.Fatalf("hello rejected: %v", err)
	}
}

func TestRequestValidateRejectsUnknownVerbAndActionLikeInput(t *testing.T) {
	for _, verb := range []Verb{"", "start", "inspect.activity", "write", "kill", "read_media"} {
		req := Request{ProtocolVersion: ProtocolVersion, Verb: verb, CorrelationID: "chatgpt-wt-01"}
		if err := req.Validate(); err == nil {
			t.Fatalf("verb %q accepted", verb)
		}
	}
}

func TestRequestValidateEnforcesPerVerbFields(t *testing.T) {
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityFacts}).Validate(); err == nil {
		t.Fatal("missing correlation_id accepted")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbHello, CorrelationID: "x"}).Validate(); err == nil {
		t.Fatal("hello with correlation_id accepted")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityFacts, CorrelationID: "x", Cursor: "c"}).Validate(); err == nil {
		t.Fatal("cursor accepted on a verb that has no cursor")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityEvents, CorrelationID: "x", Cursor: "c"}).Validate(); err != nil {
		t.Fatalf("cursor rejected on activity_events: %v", err)
	}
}

func TestRequestValidateRejectsIncompatibleProtocolVersion(t *testing.T) {
	if err := (Request{ProtocolVersion: 99, Verb: VerbActivityFacts, CorrelationID: "x"}).Validate(); err == nil {
		t.Fatal("unsupported protocol version accepted")
	}
}
