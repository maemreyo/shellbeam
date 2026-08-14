package structuredresult

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestStructuredRawOutputRefAllowsEmptyTerminalRange(t *testing.T) {
	sum := sha256.Sum256(nil)
	ref := RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 0, SHA256: hex.EncodeToString(sum[:])}
	if err := ref.Validate(); err != nil {
		t.Fatalf("empty terminal range rejected: %v", err)
	}
	bad := ref
	bad.SessionID = "../session"
	if err := bad.Validate(); err == nil {
		t.Fatal("path-like session id accepted")
	}
	bad = ref
	bad.EndByte = -1
	if err := bad.Validate(); err == nil {
		t.Fatal("negative end byte accepted")
	}
}
