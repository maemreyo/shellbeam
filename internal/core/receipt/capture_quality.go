package receipt

import "fmt"

type CaptureQuality string
type CaptureReason string

const (
	CaptureComplete   CaptureQuality = "complete"
	CapturePartial    CaptureQuality = "partial"
	CaptureIncomplete CaptureQuality = "incomplete"
)

const (
	CaptureReasonPrivateIntervalsOmitted CaptureReason = "private_intervals_omitted"
	CaptureReasonProviderLost            CaptureReason = "provider_lost"
	CaptureReasonTransportGap            CaptureReason = "transport_gap"
)

const (
	InputAuthorityAgentOnly               = "agent_only"
	InputAuthorityHumanWriteGranted       = "human_write_authority_granted"
	EvidenceAuthoritySessionLifecycleOnly = "session_lifecycle_only"
)

func ValidateInputAuthorityProvenance(v string) error {
	switch v {
	case InputAuthorityAgentOnly, InputAuthorityHumanWriteGranted:
		return nil
	default:
		return fmt.Errorf("invalid input authority provenance")
	}
}

func ValidateCaptureTruth(quality CaptureQuality, reasons []CaptureReason, outputComplete bool) error {
	if quality == CaptureComplete {
		if !outputComplete || len(reasons) != 0 {
			return fmt.Errorf("complete capture truth mismatch")
		}
		return nil
	}
	if outputComplete {
		return fmt.Errorf("non-complete capture cannot claim output complete")
	}
	seen := map[CaptureReason]bool{}
	previous := -1
	hasIncompleteCause := false
	for _, reason := range reasons {
		rank, ok := captureReasonRank(reason)
		if !ok || seen[reason] || rank <= previous {
			return fmt.Errorf("invalid capture reason set")
		}
		seen[reason] = true
		previous = rank
		if reason == CaptureReasonProviderLost || reason == CaptureReasonTransportGap {
			hasIncompleteCause = true
		}
	}
	switch quality {
	case CapturePartial:
		if len(reasons) != 1 || reasons[0] != CaptureReasonPrivateIntervalsOmitted {
			return fmt.Errorf("partial capture reason mismatch")
		}
	case CaptureIncomplete:
		if !hasIncompleteCause {
			return fmt.Errorf("incomplete capture lacks loss cause")
		}
	default:
		return fmt.Errorf("invalid capture quality")
	}
	return nil
}

func captureReasonRank(reason CaptureReason) (int, bool) {
	switch reason {
	case CaptureReasonPrivateIntervalsOmitted:
		return 0, true
	case CaptureReasonProviderLost:
		return 1, true
	case CaptureReasonTransportGap:
		return 2, true
	default:
		return 0, false
	}
}
