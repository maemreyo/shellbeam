package operation

import "testing"

func TestFingerprintExcludesResponseTuning(t *testing.T) {
	a := Intent{Command: "printf hi", CWD: "/tmp", TTY: true, TimeoutMS: 10}
	b := a
	b.YieldMS = 999
	b.MaxOutputBytes = 1
	fa, err := a.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := b.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fa != fb {
		t.Fatalf("response tuning changed fingerprint")
	}
	b.Command += "!"
	fc, _ := b.Fingerprint()
	if fa == fc {
		t.Fatal("command did not change fingerprint")
	}
}

func TestIntentRequiresExactAbsoluteCWD(t *testing.T) {
	if _, err := (Intent{Command: "x", CWD: "relative"}).Fingerprint(); err == nil {
		t.Fatal("relative cwd accepted")
	}
}

func TestV2FingerprintsSeparateRequestExecutionObservationAndResponseControls(t *testing.T) {
	intent := Intent{Command: "printf hi", CWD: "/tmp", TTY: true, TimeoutMS: 10, YieldMS: 5, MaxOutputBytes: 100}
	requestA, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	responseChanged := intent
	responseChanged.YieldMS = 999
	responseChanged.MaxOutputBytes = 1
	requestB, err := responseChanged.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if requestA != requestB {
		t.Fatal("response controls changed request fingerprint")
	}
	execA, err := intent.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	execB, err := intent.ExecutionFingerprint("/bin/zsh")
	if err != nil {
		t.Fatal(err)
	}
	if execA == execB || execA == requestA {
		t.Fatalf("execution fingerprint did not bind effective shell: request=%s sh=%s zsh=%s", requestA, execA, execB)
	}
	if got, err := (ObservationBinding{}).Fingerprint(); err != nil || got != "" {
		t.Fatalf("empty observation binding = %q, %v", got, err)
	}
	obsA, err := (ObservationBinding{ActivityID: "activity-a"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	obsB, err := (ObservationBinding{ActivityID: "activity-b"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if obsA == "" || obsA == obsB {
		t.Fatalf("observation fingerprints not distinct: %q %q", obsA, obsB)
	}
}

func TestAddressRequestFingerprintUsesLogicalWorkspaceAddress(t *testing.T) {
	logical := Intent{Command: "true", WorkspaceID: "ws_01K00000000000000000000000", CWD: "src", ResolvedCWD: "/old/repo/src"}
	first, err := logical.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	logical.ResolvedCWD = "/moved/repo/src"
	second, err := logical.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("request fingerprint changed after resolution move: %s != %s", first, second)
	}
	execA, err := logical.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	logical.ResolvedCWD = "/another/repo/src"
	execB, err := logical.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if execA == execB {
		t.Fatal("execution fingerprint ignored resolved cwd")
	}
}

func TestAddressRequestFingerprintRejectsMixedOrEscapingAddress(t *testing.T) {
	for _, intent := range []Intent{{Command: "true", WorkspaceID: "ws_01K00000000000000000000000", CWD: "/abs"}, {Command: "true", WorkspaceID: "ws_01K00000000000000000000000", CWD: "../escape"}, {Command: "true", CWD: "relative"}} {
		if _, err := intent.RequestFingerprint(); err == nil {
			t.Fatalf("intent %#v accepted", intent)
		}
	}
}
