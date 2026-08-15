package operation

import "testing"

func TestTypedPersistentRequestFingerprintBindsModeAndNameWithoutChangingOrdinaryV1(t *testing.T) {
	ordinary := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	ordinaryFP, err := ordinary.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryFP != "8ab3a04868e469d1965932338e39ea2b5a0f16e8ffc4f5e006a880c463bab102" {
		t.Fatalf("ordinary typed fingerprint changed: %s", ordinaryFP)
	}
	persistent := ordinary
	persistent.Persistent = true
	persistent.SessionName = "typed-dev"
	first, err := persistent.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == ordinaryFP {
		t.Fatal("persistent typed intent shared ordinary fingerprint")
	}
	changed := persistent
	changed.SessionName = "typed-dev-2"
	second, err := changed.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("typed persistent name did not change fingerprint")
	}
}

func TestTypedPersistentIntentRejectsTTYAndNameWithoutPersistent(t *testing.T) {
	base := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	named := base
	named.SessionName = "dev"
	if _, err := named.Fingerprint(); err == nil {
		t.Fatal("typed name without persistent accepted")
	}
	tty := base
	tty.Persistent = true
	tty.TTY = true
	if _, err := tty.Fingerprint(); err == nil {
		t.Fatal("typed persistent tty accepted")
	}
}
