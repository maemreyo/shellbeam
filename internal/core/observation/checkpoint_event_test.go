package observation

import "testing"

func TestCheckpointEventKindsAreStable(t *testing.T) {
	cases := []struct {
		kind EventKind
		want string
	}{
		{EventCheckpointCreated, "checkpoint_created"},
		{EventCheckpointRestoreStarted, "checkpoint_restore_started"},
		{EventCheckpointRestoreCompleted, "checkpoint_restore_completed"},
		{EventCheckpointExpired, "checkpoint_expired"},
	}
	for _, tc := range cases {
		if string(tc.kind) != tc.want {
			t.Fatalf("kind=%q want=%q", tc.kind, tc.want)
		}
	}
}
