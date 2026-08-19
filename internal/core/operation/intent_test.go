package operation

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

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

func TestStructuredAdapterOnlyChangesObservationBindingFingerprint(t *testing.T) {
	intent := Intent{Argv: []string{"go", "test", "-json", "./..."}, CWD: "/tmp"}
	requestA, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	execA, err := intent.ExecutionFingerprint("/usr/local/go/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	requestB, _ := intent.RequestFingerprint()
	execB, _ := intent.ExecutionFingerprint("/usr/local/go/bin/go")
	if requestA != requestB || execA != execB {
		t.Fatal("adapter metadata leaked into request/execution fingerprint")
	}
	base, err := (ObservationBinding{}).Fingerprint()
	if err != nil || base != "" {
		t.Fatalf("base=%q err=%v", base, err)
	}
	testFP, err := (ObservationBinding{StructuredAdapter: "go-test-json"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	vetFP, err := (ObservationBinding{StructuredAdapter: "go-vet-json"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if testFP == "" || testFP == vetFP {
		t.Fatalf("adapter observation fingerprints test=%q vet=%q", testFP, vetFP)
	}
	unknownFP, err := (ObservationBinding{StructuredAdapter: "junit"}).Fingerprint()
	if err != nil || unknownFP == "" || unknownFP == testFP {
		t.Fatalf("safe unsupported adapter was not bound: fp=%q err=%v", unknownFP, err)
	}
	if _, err := (ObservationBinding{StructuredAdapter: "../unknown"}).Fingerprint(); err == nil {
		t.Fatal("unsafe adapter id accepted")
	}
}

func TestEvidenceContractChangesOnlyObservationBindingFingerprint(t *testing.T) {
	intent := Intent{Argv: []string{"go", "test", "./..."}, CWD: "/tmp"}
	requestBefore, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	execBefore, err := intent.ExecutionFingerprint("/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	base, err := (ObservationBinding{ActivityID: "a"}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	contract := &evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull, ExpectedOutputs: []project.Output{{Path: "dist/report.json", Kind: "file", Required: true, Digest: "sha256"}}}
	bound, err := (ObservationBinding{ActivityID: "a", Evidence: contract}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if bound == "" || bound == base {
		t.Fatalf("evidence observation binding base=%q bound=%q", base, bound)
	}
	requestAfter, _ := intent.RequestFingerprint()
	execAfter, _ := intent.ExecutionFingerprint("/usr/bin/go")
	if requestBefore != requestAfter || execBefore != execAfter {
		t.Fatal("evidence metadata changed execution semantics")
	}

	changed := *contract
	changed.ExpectedOutputs = append([]project.Output(nil), contract.ExpectedOutputs...)
	changed.ExpectedOutputs[0].Path = "dist/other.json"
	other, err := (ObservationBinding{ActivityID: "a", Evidence: &changed}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if other == bound {
		t.Fatal("evidence contract mutation did not change observation fingerprint")
	}
}

func TestStructuredCaptureDigestPreservesLegacyObservationFingerprintsAndBindsModernAuthority(t *testing.T) {
	legacyActivity, err := (ObservationBinding{ActivityID: "activity-capture"}).Fingerprint()
	if err != nil || legacyActivity != "c1b2c61e35e39354efc7afbbaa4ade3ac109799df6935018e49d1fe3ee615a9e" {
		t.Fatalf("legacy activity fingerprint moved: %q err=%v", legacyActivity, err)
	}
	legacyAdapter, err := (ObservationBinding{StructuredAdapter: "go-test-json"}).Fingerprint()
	if err != nil || legacyAdapter != "3eba26c6cd014dbb33aa4d4ac40a8a369b5346b6777c198e52ba0f134e9c0870" {
		t.Fatalf("legacy adapter fingerprint moved: %q err=%v", legacyAdapter, err)
	}

	captureA := strings.Repeat("a", 64)
	captureB := strings.Repeat("b", 64)
	boundA, err := (ObservationBinding{StructuredAdapter: "pytest-junit-xml", StructuredCaptureDigest: captureA}).Fingerprint()
	if err != nil || boundA == "" || boundA == legacyAdapter {
		t.Fatalf("capture A fingerprint=%q err=%v", boundA, err)
	}
	boundB, err := (ObservationBinding{StructuredAdapter: "pytest-junit-xml", StructuredCaptureDigest: captureB}).Fingerprint()
	if err != nil || boundB == "" || boundB == boundA {
		t.Fatalf("capture B fingerprint=%q err=%v", boundB, err)
	}
	captureOnly, err := (ObservationBinding{StructuredCaptureDigest: captureA}).Fingerprint()
	if err != nil || captureOnly == "" {
		t.Fatalf("capture-only fingerprint=%q err=%v", captureOnly, err)
	}
	if _, err := (ObservationBinding{StructuredCaptureDigest: "not-a-digest"}).Fingerprint(); err == nil {
		t.Fatal("invalid structured capture digest accepted")
	}
}

func TestStructuredCaptureDigestDoesNotChangeRequestOrExecutionFingerprint(t *testing.T) {
	intent := Intent{Argv: []string{"pytest", "--junitxml=out.xml"}, CWD: "/tmp"}
	requestBefore, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	executionBefore, err := intent.ExecutionFingerprint("/usr/bin/pytest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (ObservationBinding{StructuredCaptureDigest: strings.Repeat("c", 64)}).Fingerprint(); err != nil {
		t.Fatal(err)
	}
	requestAfter, _ := intent.RequestFingerprint()
	executionAfter, _ := intent.ExecutionFingerprint("/usr/bin/pytest")
	if requestBefore != requestAfter || executionBefore != executionAfter {
		t.Fatalf("capture authority leaked into execution semantics request=%q/%q execution=%q/%q", requestBefore, requestAfter, executionBefore, executionAfter)
	}
}
