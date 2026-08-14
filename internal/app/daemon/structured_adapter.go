package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (s *Service) enrichStructuredAdapterAvailability(view *View, reservation operation.Reservation) {
	if view == nil || reservation.StructuredAdapter == "" {
		return
	}
	selection := structuredapp.SelectAdapter(reservation.StructuredAdapter, nil)
	if selection.Status != structuredapp.SelectionUnsupported {
		return
	}
	view.Advisories = append(view.Advisories, structuredAdapterUnsupportedAdvisory(reservation.StructuredAdapter, reservation.WorkspaceID))
}

func structuredAdapterUnsupportedAdvisory(adapter, workspaceID string) workspace.Advisory {
	sum := sha256.Sum256([]byte("structured_adapter_unsupported|" + adapter))
	return workspace.Advisory{
		Code: "structured_adapter_unsupported", Severity: "warning",
		Message:     "requested structured adapter is unavailable; child execution continued",
		WorkspaceID: workspace.WorkspaceID(workspaceID), CauseFingerprint: hex.EncodeToString(sum[:]),
	}
}

func (s *Service) scheduleStructuredTerminal(rec receipt.Receipt, adapter string) {
	if adapter == "" || s.options.StructuredWorker == nil || structuredapp.SelectAdapter(adapter, nil).Status != structuredapp.SelectionSelected {
		return
	}
	_ = s.options.StructuredWorker.ScheduleTerminal(context.Background(), rec, adapter)
}

func (s *Service) decorateNewStartView(view *View, err error, reservation operation.Reservation, observation workspaceObservation, hint *workspace.Hint) {
	if err != nil || view == nil {
		return
	}
	s.enrichWorkspaceContext(view, observation.context, hint)
	s.enrichStructuredAdapterAvailability(view, reservation)
}
