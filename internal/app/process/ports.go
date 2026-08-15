package process

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/process"
)

func (s *Service) appendPorts(ctx context.Context, observation *core.Observation) {
	if observation == nil || observation.Root == nil {
		return
	}
	if s.ports == nil {
		markPartial(observation, core.DiagnosticPortUnavailable, false)
		return
	}
	pids := make([]int, 0, len(observation.Descendants)+1)
	seen := make(map[int]struct{}, len(observation.Descendants)+1)
	addPID := func(pid int) {
		if pid <= 0 {
			return
		}
		if _, ok := seen[pid]; ok {
			return
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	addPID(observation.Root.PID)
	for _, fact := range observation.Descendants {
		addPID(fact.PID)
	}

	ports, err := s.ports.Observe(ctx, pids)
	if err != nil {
		markPartial(observation, core.DiagnosticPortUnavailable, false)
		return
	}
	if len(ports) > core.MaxPortRecords {
		ports = ports[:core.MaxPortRecords]
		markPartial(observation, core.DiagnosticLimitExceeded, true)
	}
	observation.Ports = append([]core.PortObservation(nil), ports...)
}
