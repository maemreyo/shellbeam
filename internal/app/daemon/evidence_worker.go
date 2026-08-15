package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func (s *Service) scheduleEvidenceTerminal(rec receipt.Receipt, reservation operation.Reservation) {
	if s.options.EvidenceWorker == nil || !reservation.EvidenceEligible() {
		return
	}
	_ = s.options.EvidenceWorker.ScheduleTerminal(context.Background(), rec)
}
