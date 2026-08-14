package observation

import (
	"strings"
	"testing"
	"time"
)

func TestEventAndObligationClosedVocabulary(t *testing.T) {
	now := time.Now().UTC()
	correlation := Correlation{OperationID: "op-1", SessionID: "session-1", ActivityID: "activity-1", WorkspaceID: "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000"}
	for _, kind := range InitialEventKinds() {
		event := Event{SchemaVersion: 1, EventID: "evt-1", StateRootEpoch: "epoch-1", ChangeSeq: 1, Kind: kind, RecordedAt: now, Correlation: correlation, SubjectRef: "subject:1", Summary: "bounded"}
		if err := event.Validate(); err != nil {
			t.Fatalf("kind %q: %v", kind, err)
		}
		obligation := ObservationObligation{SchemaVersion: 1, ChangeSeq: 1, Kind: kind, State: ObligationPrepared, PreparedAt: now, Correlation: correlation, SubjectRef: "subject:1", Summary: "bounded"}
		if err := obligation.Validate(); err != nil {
			t.Fatalf("obligation %q: %v", kind, err)
		}
	}
	for _, invalid := range []ObservationObligation{
		{SchemaVersion: 1, Kind: EventOperationAdmitted, State: ObligationPrepared, PreparedAt: now, SubjectRef: "x"},
		{SchemaVersion: 1, ChangeSeq: 1, Kind: "unknown", State: ObligationPrepared, PreparedAt: now, SubjectRef: "x"},
		{SchemaVersion: 1, ChangeSeq: 1, Kind: EventOperationAdmitted, State: "unknown", PreparedAt: now, SubjectRef: "x"},
		{SchemaVersion: 1, ChangeSeq: 1, Kind: EventOperationAdmitted, State: ObligationPrepared, PreparedAt: now},
		{SchemaVersion: 1, ChangeSeq: 1, Kind: EventOperationAdmitted, State: ObligationPrepared, PreparedAt: now, SubjectRef: "x", Summary: strings.Repeat("x", MaxSummaryBytes+1)},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid obligation accepted: %#v", invalid)
		}
	}
}

func TestA4EventKindsAreInClosedVocabulary(t *testing.T) {
	seen := map[EventKind]bool{}
	for _, kind := range InitialEventKinds() {
		seen[kind] = true
	}
	for _, want := range []EventKind{EventTelemetryChanged, EventReproRecorded} {
		if !seen[want] {
			t.Fatalf("A4 event kind %q missing", want)
		}
	}
}
