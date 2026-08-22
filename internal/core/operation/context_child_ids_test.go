package operation

import (
	"strings"
	"testing"
)

func TestDeriveContextChildIDsAreStableDomainSeparatedAndParseable(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	opID, sessionID, err := DeriveContextChildIDs(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(opID), "cxop_2217184b108f20361887196d8e9a4db38c22d9f846a1518e2533099a8f4c0394"; got != want {
		t.Fatalf("operation id=%q want=%q", got, want)
	}
	if got, want := string(sessionID), "cxs_55a55dae10dd897d8f932f6e10d263c11f8df8d035775814fa559027dc0f2fcf"; got != want {
		t.Fatalf("session id=%q want=%q", got, want)
	}
	if _, err := ParseID(string(opID)); err != nil {
		t.Fatalf("operation id not parseable: %v", err)
	}
	if _, err := ParseSessionID(string(sessionID)); err != nil {
		t.Fatalf("session id not parseable: %v", err)
	}
	changedOp, changedSession, err := DeriveContextChildIDs(strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if changedOp == opID || changedSession == sessionID || string(opID)[5:] == string(sessionID)[4:] {
		t.Fatalf("child ids are not domain separated/divergent: %q %q %q %q", opID, sessionID, changedOp, changedSession)
	}
}

func TestDeriveContextChildIDsRejectsInvalidFingerprint(t *testing.T) {
	if _, _, err := DeriveContextChildIDs("not-a-sha256"); err == nil {
		t.Fatal("invalid request fingerprint accepted")
	}
}
