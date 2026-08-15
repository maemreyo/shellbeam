package process

import (
	"context"
	"errors"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/process"
)

type HostObserver interface {
	Observe(context.Context, int) (core.ProcessFact, error)
	Children(context.Context, []int) (map[int][]int, bool, error)
}

type SessionResolver interface {
	ResolveProcessSession(context.Context, string) (core.SessionResolution, error)
}

type PortObserver interface {
	Observe(context.Context, []int) ([]core.PortObservation, error)
}

type Request struct {
	Target       core.Target `json:"target"`
	IncludePorts bool        `json:"include_ports,omitempty"`
}

type InspectRequest = Request

type Options struct {
	Now   func() time.Time
	Ports PortObserver
}

type Service struct {
	host     HostObserver
	resolver SessionResolver
	now      func() time.Time
	ports    PortObserver
}

func NewService(host HostObserver, resolver SessionResolver, options Options) *Service {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{host: host, resolver: resolver, now: now, ports: options.Ports}
}

func (s *Service) Inspect(ctx context.Context, request Request) (core.Observation, error) {
	if err := request.Target.Validate(); err != nil {
		return core.Observation{}, failure.New(failure.InvalidInput, map[string]string{"field": "target"}, err)
	}
	observedAt := s.now().UTC()
	observation := core.Observation{SchemaVersion: core.SchemaVersion, ObservedAt: observedAt, Target: request.Target, Quality: core.QualityComplete}
	pid, relation, unavailable, err := s.resolveTarget(ctx, request.Target)
	if err != nil {
		return core.Observation{}, err
	}
	if unavailable {
		observation.Quality = core.QualityUnavailable
		observation.DiagnosticCodes = []string{core.DiagnosticObservationIncomplete}
		return observation, observation.Validate()
	}
	if s.host == nil {
		return core.Observation{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": itoa(pid), "reason": "observer_not_configured"}, nil)
	}
	boundedCtx, cancel := context.WithTimeout(ctx, core.MaxObservationDuration)
	defer cancel()
	root, err := s.host.Observe(boundedCtx, pid)
	if err != nil {
		return core.Observation{}, err
	}
	root.Relation = relation
	observation.Root = &root
	if err := appendBoundedDescendants(boundedCtx, s.host, &observation); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(boundedCtx.Err(), context.DeadlineExceeded) {
			markPartial(&observation, core.DiagnosticLimitExceeded, true)
		} else {
			markPartial(&observation, core.DiagnosticObservationIncomplete, false)
		}
	}
	if request.IncludePorts {
		s.appendPorts(boundedCtx, &observation)
	}
	if err := observation.Validate(); err != nil {
		return core.Observation{}, failure.New(failure.ProcessObservationIncomplete, map[string]string{"pid": itoa(pid), "reason": "invalid_observation"}, err)
	}
	return observation, nil
}

func (s *Service) resolveTarget(ctx context.Context, target core.Target) (int, core.Relation, bool, error) {
	if target.Kind == core.TargetPID {
		return target.PID, core.RelationExternal, false, nil
	}
	if s.resolver == nil {
		return 0, "", false, failure.New(failure.InvalidInput, map[string]string{"field": "session_id", "reason": "session_resolver_unavailable"}, nil)
	}
	resolved, err := s.resolver.ResolveProcessSession(ctx, target.SessionID)
	if err != nil {
		return 0, "", false, err
	}
	if !resolved.Known {
		return 0, "", false, failure.New(failure.InvalidInput, map[string]string{"field": "session_id", "reason": "session_not_found"}, nil)
	}
	if !resolved.Current || resolved.PID <= 0 {
		return 0, core.RelationShellBeamRoot, true, nil
	}
	return resolved.PID, core.RelationShellBeamRoot, false, nil
}
