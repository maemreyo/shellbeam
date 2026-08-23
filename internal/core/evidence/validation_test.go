package evidence

import "testing"

func TestMechanicalReceiptAuthorityAcceptsContextExecChildOwnedV1(t *testing.T) {
	if err := ValidateMechanicalReceiptAuthority("context_exec_child_owned_v1"); err != nil {
		t.Fatalf("context exec mechanical authority rejected: %v", err)
	}
	if err := ValidateMechanicalReceiptAuthority("session_lifecycle_only"); err == nil {
		t.Fatal("session lifecycle authority accepted as mechanical")
	}
}
