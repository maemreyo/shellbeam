package shellintegration

import (
	"context"
	"fmt"
	"sync"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type ObserveRequest struct {
	HandoffID      string
	AuthorityEpoch delegated.AuthorityEpoch
	Facts          ProviderProcessFacts
	ExpectedShell  *core.ShellIdentity
	Requirement    core.Requirement
}

func (v ObserveRequest) Validate() error {
	if !validFactID(v.HandoffID, 128) {
		return fmt.Errorf("invalid shell observation handoff identity")
	}
	if err := v.AuthorityEpoch.Validate(); err != nil {
		return err
	}
	if err := v.Facts.Validate(); err != nil {
		return err
	}
	if v.ExpectedShell != nil {
		if err := v.ExpectedShell.Validate(); err != nil {
			return err
		}
	}
	return v.Requirement.Validate()
}

type ObserveResult struct {
	Shell    ShellIdentityObservation
	Result   core.RequirementResult
	Boundary *core.BoundaryProof
}

type Service struct {
	probe    ShellProbe
	adapters map[core.ShellFamily]Adapter
	mu       sync.Mutex
	active   map[string]struct{}
}

func NewService(probe ShellProbe, adapters ...Adapter) (*Service, error) {
	if probe == nil {
		return nil, fmt.Errorf("shell probe unavailable")
	}
	s := &Service{probe: probe, adapters: make(map[core.ShellFamily]Adapter), active: make(map[string]struct{})}
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, fmt.Errorf("nil shell adapter")
		}
		family := adapter.Family()
		if family != core.ShellFish && family != core.ShellZsh && family != core.ShellBash && family != core.ShellNushell {
			return nil, fmt.Errorf("unsupported shell adapter family")
		}
		if _, exists := s.adapters[family]; exists {
			return nil, fmt.Errorf("duplicate shell adapter family")
		}
		s.adapters[family] = adapter
	}
	return s, nil
}

func (s *Service) Observe(ctx context.Context, req ObserveRequest) (out ObserveResult, err error) {
	if s == nil || s.probe == nil {
		return out, fmt.Errorf("shell integration unavailable")
	}
	if err := req.Validate(); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !s.reserve(req.HandoffID) {
		return out, fmt.Errorf("shell requirement watcher already active")
	}
	defer s.release(req.HandoffID)

	observation, err := s.probe.Probe(ctx, ProbeRequest{Facts: req.Facts, Expected: req.ExpectedShell})
	if err != nil {
		return out, err
	}
	if err := observation.Validate(); err != nil {
		return out, err
	}
	out.Shell = observation
	adapter := s.adapters[observation.Identity.Family]
	if !observation.AdapterEligible() || adapter == nil {
		out.Result = core.RequirementResult{Requirement: req.Requirement, State: core.RequirementUnavailable, Quality: core.RequirementQualityManual, SafeBoundary: false, ObservedAt: observation.ObservedAt}
		return out, out.Result.Validate()
	}

	watchReq := WatchRequest{HandoffID: req.HandoffID, AuthorityEpoch: req.AuthorityEpoch, Shell: observation.Identity, Requirement: req.Requirement}
	watcher, err := adapter.Install(ctx, watchReq)
	if err != nil {
		return out, err
	}
	if watcher == nil {
		return out, fmt.Errorf("shell adapter returned nil watcher")
	}
	defer func() {
		if closeErr := watcher.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	event, err := watcher.Wait(ctx)
	if err != nil {
		return out, err
	}
	if err := event.Validate(watchReq); err != nil {
		return out, err
	}
	out.Result = event.Result
	boundary := event.Boundary
	out.Boundary = &boundary
	return out, nil
}

func (s *Service) reserve(handoffID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active[handoffID]; exists {
		return false
	}
	s.active[handoffID] = struct{}{}
	return true
}

func (s *Service) release(handoffID string) {
	s.mu.Lock()
	delete(s.active, handoffID)
	s.mu.Unlock()
}
