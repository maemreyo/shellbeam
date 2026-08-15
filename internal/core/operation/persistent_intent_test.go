package operation

import "testing"

func TestPersistentRequestFingerprintBindsModeAndNameWithoutChangingOrdinaryV2(t *testing.T) {
	ordinary := Intent{Command: "printf hi", CWD: "/tmp", TTY: false, TimeoutMS: 10}
	ordinaryFP, err := ordinary.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryFP != "4a57658e746aada961e737c4eec0b3443d477c37e0590c15d9839b435d65264f" {
		t.Fatalf("ordinary v2 fingerprint changed: %s", ordinaryFP)
	}

	persistent := ordinary
	persistent.Persistent = true
	persistent.SessionName = "dev-server"
	first, err := persistent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := persistent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == ordinaryFP || first != second {
		t.Fatalf("persistent fingerprint ordinary=%s first=%s second=%s", ordinaryFP, first, second)
	}

	changedName := persistent
	changedName.SessionName = "dev-server-2"
	changedNameFP, err := changedName.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if changedNameFP == first {
		t.Fatal("session name did not change request fingerprint")
	}

	noName := persistent
	noName.SessionName = ""
	noNameFP, err := noName.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if noNameFP == first {
		t.Fatal("optional session name was not bound")
	}

	ordinaryExec, err := ordinary.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	persistentExec, err := persistent.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryExec == persistentExec {
		t.Fatal("persistent execution semantics were not bound")
	}
}

func TestPersistentIntentValidationRejectsNameWithoutPersistentAndTTY(t *testing.T) {
	if _, err := (Intent{Command: "true", CWD: "/tmp", SessionName: "dev"}).RequestFingerprint(); err == nil {
		t.Fatal("session name without persistent accepted")
	}
	if _, err := (Intent{Command: "true", CWD: "/tmp", Persistent: true, TTY: true}).RequestFingerprint(); err == nil {
		t.Fatal("persistent tty accepted")
	}
}
