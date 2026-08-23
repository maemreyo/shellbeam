package observation

import "testing"

func TestHandoffEventKindsAreClosedVocabulary(t *testing.T) {
	for _, want := range []EventKind{
		EventHandoffRequested,
		EventHandoffAttached,
		EventHandoffHumanOwned,
		EventHandoffReclaimStarted,
		EventHandoffReclaimed,
		EventHandoffAborted,
		EventHandoffClientLost,
		EventHandoffExpired,
	} {
		seen := 0
		for _, got := range InitialEventKinds() {
			if got == want {
				seen++
			}
		}
		if seen != 1 {
			t.Fatalf("event kind %q occurrences=%d", want, seen)
		}
	}
}
