package bridge

import "testing"

func TestVerificationActionsRemainInsideLocalShellBridge(t *testing.T) {
	for _, action := range []string{"inspect.verification", "verification.policy.preview", "verification.policy.activate", "verification.waiver.set", "verification.waiver.revoke"} {
		if !IsVerificationAction(action) {
			t.Fatalf("verification action %q not recognized", action)
		}
	}
	for _, action := range []string{"start", "poll", "inspect.project", "task.complete"} {
		if IsVerificationAction(action) {
			t.Fatalf("non-verification action %q classified as verification", action)
		}
	}
}
