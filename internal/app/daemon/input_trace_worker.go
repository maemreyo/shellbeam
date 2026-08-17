package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func (s *Service) scheduleInputTraceTerminal(rec receipt.Receipt, reservation operation.Reservation) {
	if s.options.InputTraceWorker == nil || reservation.Trace == nil {
		return
	}
	_ = s.options.InputTraceWorker.ScheduleTerminal(context.Background(), rec)
}
