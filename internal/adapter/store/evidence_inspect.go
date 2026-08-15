package store

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
)

func (r *Repository) ListEvidenceIndexObligations(ctx context.Context, after, cut observation.ChangeSeq, limit int) ([]observation.ObservationObligation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > core.MaxInspectScanRecords || after > cut {
		return nil, fmt.Errorf("invalid_evidence_index_range")
	}
	high, err := r.ObservationHighWatermark(ctx)
	if err != nil {
		return nil, err
	}
	if cut > high {
		return nil, fmt.Errorf("evidence_index_cut_unavailable")
	}
	out := make([]observation.ObservationObligation, 0, min(limit, int(cut-after)))
	for seq := after + 1; seq <= cut && len(out) < limit; seq++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		record, readErr := r.readObservation(seq)
		if readErr != nil {
			return nil, fmt.Errorf("evidence_index_continuity: %w", readErr)
		}
		out = append(out, record)
	}
	return out, nil
}
