package terminalpresentation

import (
	"errors"
	"sync"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

var errInvalidForegroundObservation = errors.New("invalid terminal foreground observation")

type RecentRegistry struct {
	mu              sync.Mutex
	activeFreshness time.Duration
	recentFreshness time.Duration
	foregroundAt    time.Time
	active          *ForegroundObservation
	recent          *ForegroundObservation
}

func NewRecentRegistry(activeFreshness, recentFreshness time.Duration) (*RecentRegistry, error) {
	if activeFreshness <= 0 || recentFreshness <= 0 {
		return nil, errors.New("invalid terminal freshness")
	}
	return &RecentRegistry{activeFreshness: activeFreshness, recentFreshness: recentFreshness}, nil
}

func (r *RecentRegistry) Observe(observation ForegroundObservation) error {
	if r == nil {
		return errors.New("nil terminal recent registry")
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.foregroundAt.IsZero() && observation.ObservedAt.Before(r.foregroundAt) {
		return nil
	}
	r.foregroundAt = observation.ObservedAt
	if observation.Identity == nil {
		r.active = nil
		return nil
	}
	copy := cloneForeground(observation)
	r.active = &copy
	recent := cloneForeground(observation)
	r.recent = &recent
	return nil
}

func (r *RecentRegistry) Candidates(now time.Time) []core.Candidate {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]core.Candidate, 0, 1)
	if r.active != nil {
		candidate := candidateFromForeground(*r.active, core.SourceActive, r.activeFreshness)
		if candidate.Evidence.FreshAt(now) {
			return append(result, candidate)
		}
		r.active = nil
	}
	if r.recent != nil {
		candidate := candidateFromForeground(*r.recent, core.SourceRecent, r.recentFreshness)
		if candidate.Evidence.FreshAt(now) {
			return append(result, candidate)
		}
		r.recent = nil
	}
	return result
}

func cloneForeground(value ForegroundObservation) ForegroundObservation {
	copy := value
	if value.Identity != nil {
		identity := *value.Identity
		copy.Identity = &identity
	}
	return copy
}

func candidateFromForeground(value ForegroundObservation, source core.EvidenceSource, freshness time.Duration) core.Candidate {
	return core.Candidate{Evidence: core.Evidence{
		Identity:   *value.Identity,
		Source:     source,
		ObservedAt: value.ObservedAt,
		FreshUntil: value.ObservedAt.Add(freshness),
		Quality:    value.Quality,
	}}
}
