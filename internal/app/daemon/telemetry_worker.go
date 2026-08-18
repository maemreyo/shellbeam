package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func (s *Service) scheduleTelemetryTerminal(rec receipt.Receipt, resources *receipt.ResourceEvidence) {
	if s.options.TelemetryWorker == nil {
		return
	}
	if resources != nil {
		if worker, ok := s.options.TelemetryWorker.(TelemetryResourceWorker); ok {
			_ = worker.ScheduleTerminalWithResources(context.Background(), rec, resources)
			return
		}
	}
	_ = s.options.TelemetryWorker.ScheduleTerminal(context.Background(), rec)
}
