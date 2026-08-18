package verification

import "fmt"

type ObligationDisposition string

const (
	DispositionRequiredNow  ObligationDisposition = "required_now"
	DispositionDeferred     ObligationDisposition = "deferred"
	DispositionOptional     ObligationDisposition = "optional"
	DispositionNotTriggered ObligationDisposition = "not_triggered"
	DispositionWaived       ObligationDisposition = "waived"
)

type EvidenceStatus string

const (
	EvidenceNotEvaluated EvidenceStatus = "not_evaluated"
	EvidenceSatisfied    EvidenceStatus = "satisfied"
	EvidenceFailed       EvidenceStatus = "failed"
	EvidenceInsufficient EvidenceStatus = "insufficient"
	EvidenceInconsistent EvidenceStatus = "inconsistent"
	EvidenceUnknown      EvidenceStatus = "unknown"
	EvidenceUnavailable  EvidenceStatus = "unavailable"
)

type GateStatus string

const (
	GateClear         GateStatus = "clear"
	GateBlocked       GateStatus = "blocked"
	GateIndeterminate GateStatus = "indeterminate"
)

type Phase string

const (
	PhaseInnerLoop  Phase = "inner_loop"
	PhaseCheckpoint Phase = "checkpoint"
	PhasePreMerge   Phase = "pre_merge"
	PhaseRelease    Phase = "release"
	PhaseNightly    Phase = "nightly"
	PhasePeriodic   Phase = "periodic"
)

func (d ObligationDisposition) Validate() error {
	switch d {
	case DispositionRequiredNow, DispositionDeferred, DispositionOptional, DispositionNotTriggered, DispositionWaived:
		return nil
	}
	return fmt.Errorf("invalid obligation disposition %q", d)
}
func (s EvidenceStatus) Validate() error {
	switch s {
	case EvidenceNotEvaluated, EvidenceSatisfied, EvidenceFailed, EvidenceInsufficient, EvidenceInconsistent, EvidenceUnknown, EvidenceUnavailable:
		return nil
	}
	return fmt.Errorf("invalid evidence status %q", s)
}
func (s GateStatus) Validate() error {
	switch s {
	case GateClear, GateBlocked, GateIndeterminate:
		return nil
	}
	return fmt.Errorf("invalid gate status %q", s)
}
func (p Phase) Validate() error {
	switch p {
	case PhaseInnerLoop, PhaseCheckpoint, PhasePreMerge, PhaseRelease, PhaseNightly, PhasePeriodic:
		return nil
	}
	return fmt.Errorf("invalid phase %q", p)
}
