package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func (s *Service) scheduleTelemetryTerminal(rec receipt.Receipt) {
	if s.options.TelemetryWorker == nil {
		return
	}
	_ = s.options.TelemetryWorker.ScheduleTerminal(context.Background(), rec)
}
