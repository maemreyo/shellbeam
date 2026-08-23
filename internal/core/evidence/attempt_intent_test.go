package evidence

import (
	"strings"
	"testing"
)

func TestVerificationAttemptIntentValidation(t *testing.T) {
	id := "ev_" + strings.Repeat("a", 64)
	valid := []VerificationAttemptIntent{
		{},
		{RerunOfEvidenceID: id},
		{RerunOfEvidenceID: id, RerunReason: RerunDiagnoseFlake},
		{RerunOfEvidenceID: id, RerunReason: RerunFlakeQualification},
	}
	for i, attempt := range valid {
		if err := attempt.Validate(); err != nil {
			t.Fatalf("valid attempt %d rejected: %v", i, err)
		}
	}
	invalid := []VerificationAttemptIntent{
		{RerunReason: RerunDiagnoseFlake},
		{RerunOfEvidenceID: "bad"},
		{RerunOfEvidenceID: id, RerunReason: RerunReason("post_hoc")},
	}
	for i, attempt := range invalid {
		if err := attempt.Validate(); err == nil {
			t.Fatalf("invalid attempt %d accepted: %#v", i, attempt)
		}
	}
}
