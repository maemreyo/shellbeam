package receipt

import (
	"fmt"
	"sort"
)

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

type CaptureTruth struct {
	Quality        CaptureQuality  `json:"capture_quality"`
	Reasons        []CaptureReason `json:"capture_reasons"`
	OutputComplete bool            `json:"output_complete"`
}

func CompleteCaptureTruth() CaptureTruth {
	return CaptureTruth{Quality: CaptureComplete, OutputComplete: true}
}

func (v CaptureTruth) Clone() CaptureTruth {
	v.Reasons = append([]CaptureReason(nil), v.Reasons...)
	return v
}

func (v CaptureTruth) Validate() error {
	return ValidateCaptureTruth(v.Quality, v.Reasons, v.OutputComplete)
}

func (v CaptureTruth) WithReason(reason CaptureReason) (CaptureTruth, error) {
	if err := v.Validate(); err != nil {
		return CaptureTruth{}, err
	}
	if _, ok := captureReasonRank(reason); !ok {
		return CaptureTruth{}, fmt.Errorf("invalid capture reason")
	}
	seen := make(map[CaptureReason]struct{}, len(v.Reasons)+1)
	reasons := make([]CaptureReason, 0, len(v.Reasons)+1)
	for _, existing := range v.Reasons {
		if _, ok := seen[existing]; !ok {
			seen[existing] = struct{}{}
			reasons = append(reasons, existing)
		}
	}
	if _, ok := seen[reason]; !ok {
		reasons = append(reasons, reason)
	}
	sort.Slice(reasons, func(i, j int) bool {
		left, _ := captureReasonRank(reasons[i])
		right, _ := captureReasonRank(reasons[j])
		return left < right
	})
	out := CaptureTruth{Reasons: reasons, OutputComplete: false, Quality: CaptureIncomplete}
	if len(reasons) == 1 && reasons[0] == CaptureReasonPrivateIntervalsOmitted {
		out.Quality = CapturePartial
	}
	if err := out.Validate(); err != nil {
		return CaptureTruth{}, err
	}
	return out, nil
}

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
	case CaptureReasonTransportGap:
		return 1, true
	case CaptureReasonProviderLost:
		return 2, true
	default:
		return 0, false
	}
}
