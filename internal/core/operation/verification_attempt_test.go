package operation

import (
	"strings"
	"testing"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

func verificationAttemptEvidenceID(c byte) string { return "ev_" + strings.Repeat(string(c), 64) }

func TestVerificationAttemptNilPreservesLegacyFingerprints(t *testing.T) {
	contract := evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	raw, err := (ObservationBinding{ActivityID: "activity_task2", Evidence: &contract}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if want := "49085f2cac5901cb07ed3e136cd954f31294a8103857067735a35634ab20ae4f"; raw != want {
		t.Fatalf("raw nil-attempt fingerprint drifted: got=%s want=%s", raw, want)
	}
	typed := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package", Params: map[string]string{"package": "./internal/app"}, TimeoutMS: 5000}
	got, err := typed.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if want := "c60674101127516942a35c7d6317cf83eaca739d8da3eea66273a88423f401e1"; got != want {
		t.Fatalf("typed nil-attempt fingerprint drifted: got=%s want=%s", got, want)
	}
}

func TestVerificationAttemptBindsRawObservationFingerprint(t *testing.T) {
	contract := evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	a := &evidence.VerificationAttemptIntent{RerunOfEvidenceID: verificationAttemptEvidenceID('a'), RerunReason: evidence.RerunDiagnoseFlake}
	b := &evidence.VerificationAttemptIntent{RerunOfEvidenceID: verificationAttemptEvidenceID('a'), RerunReason: evidence.RerunFlakeQualification}
	first, err := (ObservationBinding{ActivityID: "activity_task2", Evidence: &contract, VerificationAttempt: a}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := (ObservationBinding{ActivityID: "activity_task2", Evidence: &contract, VerificationAttempt: b}).Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == "49085f2cac5901cb07ed3e136cd954f31294a8103857067735a35634ab20ae4f" {
		t.Fatalf("attempt not bound: first=%s second=%s", first, second)
	}
	if _, err := (ObservationBinding{ActivityID: "activity_task2", VerificationAttempt: a}).Fingerprint(); err == nil {
		t.Fatal("raw verification attempt accepted without evidence contract")
	}
}

func TestVerificationAttemptBindsTypedRequestFingerprint(t *testing.T) {
	base := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test_package", Params: map[string]string{"package": "./internal/app"}, TimeoutMS: 5000}
	a, b := base, base
	a.VerificationAttempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: verificationAttemptEvidenceID('a'), RerunReason: evidence.RerunDiagnoseFlake}
	b.VerificationAttempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: verificationAttemptEvidenceID('b'), RerunReason: evidence.RerunDiagnoseFlake}
	fa, err := a.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := b.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if fa == fb || fa == "c60674101127516942a35c7d6317cf83eaca739d8da3eea66273a88423f401e1" {
		t.Fatalf("typed attempt not bound: a=%s b=%s", fa, fb)
	}
	invalid := base
	invalid.VerificationAttempt = &evidence.VerificationAttemptIntent{RerunReason: evidence.RerunDiagnoseFlake}
	if _, err := invalid.Fingerprint(); err == nil {
		t.Fatal("invalid typed verification attempt accepted")
	}
}
