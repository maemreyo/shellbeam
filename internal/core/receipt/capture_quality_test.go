package receipt

import "testing"

func TestDelegatedCaptureTruthVocabulary(t *testing.T) {
	valid := []struct {
		quality  CaptureQuality
		reasons  []CaptureReason
		complete bool
	}{
		{CaptureComplete, nil, true},
		{CapturePartial, []CaptureReason{CaptureReasonPrivateIntervalsOmitted}, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonTransportGap}, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonProviderLost}, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonPrivateIntervalsOmitted, CaptureReasonProviderLost, CaptureReasonTransportGap}, false},
	}
	for _, tc := range valid {
		if err := ValidateCaptureTruth(tc.quality, tc.reasons, tc.complete); err != nil {
			t.Fatalf("valid %#v rejected: %v", tc, err)
		}
	}
	invalid := []struct {
		quality  CaptureQuality
		reasons  []CaptureReason
		complete bool
	}{
		{CaptureComplete, []CaptureReason{CaptureReasonPrivateIntervalsOmitted}, true},
		{CapturePartial, []CaptureReason{CaptureReasonTransportGap}, false},
		{CaptureIncomplete, nil, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonPrivateIntervalsOmitted}, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonTransportGap, CaptureReasonTransportGap}, false},
		{CaptureIncomplete, []CaptureReason{CaptureReasonTransportGap, CaptureReasonProviderLost}, false},
		{CaptureQuality("future"), nil, false},
		{CaptureIncomplete, []CaptureReason{"future"}, false},
		{CaptureComplete, nil, false},
	}
	for i, tc := range invalid {
		if err := ValidateCaptureTruth(tc.quality, tc.reasons, tc.complete); err == nil {
			t.Fatalf("invalid[%d] accepted: %#v", i, tc)
		}
	}
}

func TestDelegatedAuthorityVocabulariesAreClosed(t *testing.T) {
	if err := ValidateInputAuthorityProvenance(InputAuthorityAgentOnly); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInputAuthorityProvenance(InputAuthorityHumanWriteGranted); err != nil {
		t.Fatal(err)
	}
	if err := ValidateInputAuthorityProvenance("future"); err == nil {
		t.Fatal("unknown provenance accepted")
	}
	if EvidenceAuthoritySessionLifecycleOnly != "session_lifecycle_only" {
		t.Fatalf("evidence authority=%q", EvidenceAuthoritySessionLifecycleOnly)
	}
}
