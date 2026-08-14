package observation

import (
	"fmt"
	"time"
)

type ObligationState string

const (
	ObligationPrepared  ObligationState = "prepared"
	ObligationCommitted ObligationState = "committed"
	ObligationAborted   ObligationState = "aborted"
)

type PrepareRequest struct {
	Kind        EventKind   `json:"kind"`
	Correlation Correlation `json:"correlation"`
	SubjectRef  string      `json:"subject_ref"`
	Summary     string      `json:"summary,omitempty"`
}

func (r PrepareRequest) Validate() error {
	if !validEventKind(r.Kind) || !safeText(r.SubjectRef, MaxSubjectBytes) || (r.Summary != "" && !safeText(r.Summary, MaxSummaryBytes)) {
		return fmt.Errorf("invalid observation prepare request")
	}
	return r.Correlation.Validate()
}

type ObservationObligation struct {
	SchemaVersion int             `json:"schema_version"`
	ChangeSeq     ChangeSeq       `json:"change_seq"`
	Kind          EventKind       `json:"kind"`
	State         ObligationState `json:"state"`
	PreparedAt    time.Time       `json:"prepared_at"`
	Correlation   Correlation     `json:"correlation"`
	SubjectRef    string          `json:"subject_ref"`
	Summary       string          `json:"summary,omitempty"`
	AbortReason   string          `json:"abort_reason,omitempty"`
}

func (o ObservationObligation) Validate() error {
	if o.SchemaVersion != SchemaVersion || o.ChangeSeq == 0 || !validEventKind(o.Kind) || o.PreparedAt.IsZero() || !safeText(o.SubjectRef, MaxSubjectBytes) || (o.Summary != "" && !safeText(o.Summary, MaxSummaryBytes)) {
		return fmt.Errorf("invalid observation obligation")
	}
	if err := o.Correlation.Validate(); err != nil {
		return err
	}
	switch o.State {
	case ObligationPrepared, ObligationCommitted:
		if o.AbortReason != "" {
			return fmt.Errorf("abort reason on non-aborted obligation")
		}
	case ObligationAborted:
		if !safeText(o.AbortReason, 128) {
			return fmt.Errorf("aborted obligation reason missing")
		}
	default:
		return fmt.Errorf("invalid observation obligation state")
	}
	return nil
}
