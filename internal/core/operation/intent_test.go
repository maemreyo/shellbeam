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

func TestArgvFingerprintsPreserveBoundariesAndExecutionBinding(t *testing.T) {
	intent := Intent{Argv: []string{"tool", "a b", "\"quoted\"", "*", "", "日本語", "--flag"}, CWD: "/tmp", TTY: true, TimeoutMS: 10}
	requestA, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	changed := intent
	changed.Argv = append([]string(nil), intent.Argv...)
	changed.Argv[1] = "a  b"
	requestB, err := changed.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if requestA == requestB {
		t.Fatal("argv boundary change did not change request fingerprint")
	}
	shell := Intent{Command: `tool a-b quoted wildcard empty unicode flag`, CWD: "/tmp", TTY: true, TimeoutMS: 10}
	shellRequest, err := shell.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if requestA == shellRequest {
		t.Fatal("argv and shell modes shared request fingerprint")
	}
	execA, err := intent.ExecutionFingerprint("/usr/bin/tool")
	if err != nil {
		t.Fatal(err)
	}
	execB, err := intent.ExecutionFingerprint("/opt/tool")
	if err != nil {
		t.Fatal(err)
	}
	if execA == execB {
		t.Fatal("effective executable did not change execution fingerprint")
	}
	if got, err := intent.ExecutionMode(); err != nil || got != ExecutionModeArgv {
		t.Fatalf("mode=%q err=%v", got, err)
	}
}

func TestShellV2FingerprintsRemainUpgradeCompatible(t *testing.T) {
	intent := Intent{Command: "printf hi", CWD: "/tmp", TTY: true, TimeoutMS: 10}
	request, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	execution, err := intent.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if request != "6d920130b300c6bf7487d8734fcb7094390e29f1571be28a4ea79d62d86a7ff9" {
		t.Fatalf("request=%s", request)
	}
	if execution != "cf903dfb875d0c2fd1c409ce69e70aad11801d426dc6d028e437ce9f65d2f3e4" {
		t.Fatalf("execution=%s", execution)
	}
}

func TestArgvExecutionFormValidation(t *testing.T) {
	invalid := []Intent{
		{CWD: "/tmp"},
		{Argv: []string{}, CWD: "/tmp"},
		{Argv: []string{""}, CWD: "/tmp"},
		{Command: "true", Argv: []string{"true"}, CWD: "/tmp"},
	}
	for _, got := range invalid {
		if _, err := got.RequestFingerprint(); err == nil {
			t.Fatalf("intent %#v accepted", got)
		}
	}
	if _, err := (Intent{Argv: []string{"printf", ""}, CWD: "/tmp"}).RequestFingerprint(); err != nil {
		t.Fatalf("empty nonzero argv element rejected: %v", err)
	}
}

func TestDeclaredIntentIsObservationMetadataOnly(t *testing.T) {
	yes, no := true, false
	declared := &DeclaredIntent{Kind: IntentKindTest, MutatesSource: &no, ExternalEffect: &yes}
	if err := declared.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (&DeclaredIntent{Kind: "unknown-kind"}).Validate(); err == nil {
		t.Fatal("unknown intent kind accepted")
	}
	a, err := (ObservationBinding{ActivityID: "a", Intent: declared}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	b, err := (ObservationBinding{ActivityID: "a", Intent: &DeclaredIntent{Kind: IntentKindTest, MutatesSource: &no, ExternalEffect: &no}}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := (ObservationBinding{ActivityID: "a"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || a == legacy {
		t.Fatalf("observation fingerprints not distinct: %s %s %s", a, b, legacy)
	}
	base := Intent{Command: "true", CWD: "/tmp"}
	requestA, _ := base.RequestFingerprint()
	requestB, _ := base.RequestFingerprint()
	if requestA != requestB {
		t.Fatal("declared intent changed request semantics")
	}
}
