package observation

import "testing"

func TestPrepareRequestValidatesBoundedObservationFacts(t *testing.T) {
	valid := PrepareRequest{Kind: EventProcessTerminal, SubjectRef: "receipt:session-1", Summary: "terminal", Correlation: Correlation{OperationID: "op-1", SessionID: "session-1"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []PrepareRequest{
		{},
		{Kind: "unknown", SubjectRef: "x"},
		{Kind: EventProcessTerminal, SubjectRef: "bad\nsubject"},
		{Kind: EventProcessTerminal, SubjectRef: "x", Correlation: Correlation{WorkspaceID: "bad"}},
	}
	for i, request := range invalid {
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid[%d] accepted: %#v", i, request)
		}
	}
}
