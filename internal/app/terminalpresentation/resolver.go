package terminalpresentation

import (
	"context"
	"errors"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const (
	UnavailableActivity = "activity"
	UnavailableRunning  = "running"
)

type ResolveRequest struct {
	ExistingClient *core.Evidence
	BridgeAffinity *core.Evidence
	Fallback       *core.Evidence
}

type ResolveResult struct {
	Resolution         core.Resolution
	UnavailableSources []string
}

type Resolver struct {
	registry         *RecentRegistry
	activity         ActivitySource
	running          RunningSource
	runningFreshness time.Duration
	now              func() time.Time
}

func NewResolver(registry *RecentRegistry, activity ActivitySource, running RunningSource, runningFreshness time.Duration, now func() time.Time) (*Resolver, error) {
	if registry == nil || runningFreshness <= 0 || now == nil {
		return nil, errors.New("invalid terminal resolver configuration")
	}
	return &Resolver{registry: registry, activity: activity, running: running, runningFreshness: runningFreshness, now: now}, nil
}

func (r *Resolver) Resolve(ctx context.Context, request ResolveRequest) (ResolveResult, error) {
	if r == nil {
		return ResolveResult{}, errors.New("nil terminal resolver")
	}
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}
	if err := validateRequestEvidence(request.ExistingClient, core.SourceExistingClient); err != nil {
		return ResolveResult{}, err
	}
	if err := validateRequestEvidence(request.BridgeAffinity, core.SourceBridgeAffinity); err != nil {
		return ResolveResult{}, err
	}
	if err := validateRequestEvidence(request.Fallback, core.SourceFallback); err != nil {
		return ResolveResult{}, err
	}
	now := r.now()
	candidates := make([]core.Candidate, 0, 6)
	if request.ExistingClient != nil {
		candidates = append(candidates, core.Candidate{Evidence: *request.ExistingClient})
	}
	unavailable := make([]string, 0, 2)
	if r.activity == nil {
		unavailable = append(unavailable, UnavailableActivity)
	} else if current, err := r.activity.Current(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ResolveResult{}, ctxErr
		}
		unavailable = append(unavailable, UnavailableActivity)
	} else if err := r.registry.Observe(current); err != nil {
		return ResolveResult{}, err
	}
	candidates = append(candidates, r.registry.Candidates(now)...)
	if request.BridgeAffinity != nil {
		candidates = append(candidates, core.Candidate{Evidence: *request.BridgeAffinity})
	}
	if r.running == nil {
		unavailable = append(unavailable, UnavailableRunning)
	} else if running, err := r.running.Running(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ResolveResult{}, ctxErr
		}
		unavailable = append(unavailable, UnavailableRunning)
	} else {
		for _, identity := range running {
			if err := identity.Validate(); err != nil {
				return ResolveResult{}, err
			}
			candidates = append(candidates, core.Candidate{Evidence: core.Evidence{
				Identity: identity, Source: core.SourceSingleRunning, ObservedAt: now,
				FreshUntil: now.Add(r.runningFreshness), Quality: core.QualityNative,
			}})
		}
	}
	if request.Fallback != nil {
		candidates = append(candidates, core.Candidate{Evidence: *request.Fallback})
	}
	resolution, err := core.Rank(now, candidates)
	if err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{Resolution: resolution, UnavailableSources: unavailable}, nil
}

func validateRequestEvidence(evidence *core.Evidence, expected core.EvidenceSource) error {
	if evidence == nil {
		return nil
	}
	if evidence.Source != expected {
		return errors.New("terminal evidence supplied to wrong resolver lane")
	}
	return evidence.Validate()
}
