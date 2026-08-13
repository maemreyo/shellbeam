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
