package terminalpresentation

import (
	"sort"
	"time"
)

func Rank(now time.Time, candidates []Candidate) (Resolution, error) {
	fresh := make([]Candidate, 0, len(candidates))
	running := make(map[string]struct{})
	for _, candidate := range candidates {
		if err := candidate.Validate(); err != nil {
			return Resolution{}, err
		}
		if !candidate.Evidence.FreshAt(now) {
			continue
		}
		fresh = append(fresh, candidate)
		if candidate.Evidence.Source == SourceSingleRunning {
			running[candidate.Evidence.Identity.StableKey()] = struct{}{}
		}
	}

	eligible := fresh[:0]
	for _, candidate := range fresh {
		if candidate.Evidence.Source == SourceSingleRunning && len(running) != 1 {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return Resolution{}, nil
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		a, b := eligible[i].Evidence, eligible[j].Evidence
		pa, pb := sourcePriority(a.Source), sourcePriority(b.Source)
		if pa != pb {
			return pa < pb
		}
		if !a.ObservedAt.Equal(b.ObservedAt) {
			return a.ObservedAt.After(b.ObservedAt)
		}
		if a.Identity.ProviderID != b.Identity.ProviderID {
			return a.Identity.ProviderID < b.Identity.ProviderID
		}
		return a.Identity.StableKey() < b.Identity.StableKey()
	})
	selected := eligible[0]
	return Resolution{Selected: &selected}, nil
}

func sourcePriority(source EvidenceSource) int {
	switch source {
	case SourceExistingClient:
		return 0
	case SourceActive:
		return 1
	case SourceRecent:
		return 2
	case SourceBridgeAffinity:
		return 3
	case SourceSingleRunning:
		return 4
	case SourceFallback:
		return 5
	default:
		return 100
	}
}
