package observation

import "testing"

func TestE27InputTraceEventKindsAreClosedVocabulary(t *testing.T) {
	for _, kind := range []EventKind{EventInputTraceRecorded, EventInputTraceTruncated} {
		found := false
		for _, candidate := range InitialEventKinds() {
			if candidate == kind {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing event kind %q", kind)
		}
	}
}
