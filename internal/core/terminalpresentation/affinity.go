package terminalpresentation

import (
	"errors"
	"time"
)

const MaxBridgeAffinityFreshness = time.Hour

type BridgeAffinityHint struct {
	Identity       TerminalIdentity `json:"identity"`
	ObservedAt     time.Time        `json:"observed_at"`
	FreshUntil     time.Time        `json:"fresh_until"`
	EvidenceSource EvidenceSource   `json:"evidence_source"`
}

func NewBridgeAffinityHint(identity TerminalIdentity, observedAt time.Time, freshness time.Duration) (BridgeAffinityHint, error) {
	if freshness <= 0 || freshness > MaxBridgeAffinityFreshness {
		return BridgeAffinityHint{}, errors.New("invalid bridge terminal affinity freshness")
	}
	hint := BridgeAffinityHint{
		Identity:       identity,
		ObservedAt:     observedAt,
		FreshUntil:     observedAt.Add(freshness),
		EvidenceSource: SourceBridgeAffinity,
	}
	if err := hint.Validate(); err != nil {
		return BridgeAffinityHint{}, err
	}
	return hint, nil
}

func (v BridgeAffinityHint) Validate() error {
	if err := v.Identity.Validate(); err != nil {
		return err
	}
	if v.EvidenceSource != SourceBridgeAffinity {
		return errors.New("invalid bridge terminal affinity evidence source")
	}
	if v.ObservedAt.IsZero() || v.FreshUntil.IsZero() || !v.FreshUntil.After(v.ObservedAt) {
		return errors.New("invalid bridge terminal affinity freshness")
	}
	if v.FreshUntil.Sub(v.ObservedAt) > MaxBridgeAffinityFreshness {
		return errors.New("bridge terminal affinity exceeds freshness bound")
	}
	return nil
}

func (v BridgeAffinityHint) Evidence() (Evidence, error) {
	if err := v.Validate(); err != nil {
		return Evidence{}, err
	}
	return Evidence{
		Identity:   v.Identity,
		Source:     SourceBridgeAffinity,
		ObservedAt: v.ObservedAt,
		FreshUntil: v.FreshUntil,
		Quality:    QualityValidated,
	}, nil
}
