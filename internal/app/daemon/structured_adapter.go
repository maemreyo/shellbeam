package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync/atomic"

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

func (s *Service) acquireStructuredCaptureTerminal(reservation operation.Reservation) structuredapp.TerminalCaptureResult {
	if reservation.StructuredCaptureDigest == "" {
		return structuredapp.TerminalCaptureResult{}
	}
	if s.options.StructuredCaptureTerminal == nil {
		return unavailableStructuredCaptureTerminal(reservation.StructuredCaptureDigest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), structuredapp.MaxTerminalAcquireDuration)
	defer cancel()
	const (
		capturePending int32 = iota
		captureCallerOwned
		captureTimedOut
	)
	var ownership atomic.Int32
	resultCh := make(chan structuredapp.TerminalCaptureResult, 1)
	go func() {
		result := s.options.StructuredCaptureTerminal.AcquireTerminal(ctx, reservation)
		result = normalizeStructuredCaptureTerminalResult(reservation.StructuredCaptureDigest, result)
		if ownership.CompareAndSwap(capturePending, captureCallerOwned) {
			resultCh <- result
			return
		}
		_ = result.Close()
	}()
	select {
	case result := <-resultCh:
		return result
	case <-ctx.Done():
		if ownership.CompareAndSwap(capturePending, captureTimedOut) {
			return unavailableStructuredCaptureTerminal(reservation.StructuredCaptureDigest)
		}
		return <-resultCh
	}
}

func normalizeStructuredCaptureTerminalResult(expected string, result structuredapp.TerminalCaptureResult) structuredapp.TerminalCaptureResult {
	if result.CaptureAuthorityID != expected || result.State == "" {
		_ = result.Close()
		return unavailableStructuredCaptureTerminal(expected)
	}
	if result.State == structuredapp.TerminalCaptureAcquired && result.Source() == nil {
		_ = result.Close()
		return unavailableStructuredCaptureTerminal(expected)
	}
	return result
}

func unavailableStructuredCaptureTerminal(digest string) structuredapp.TerminalCaptureResult {
	return structuredapp.TerminalCaptureResult{
		State: structuredapp.TerminalCaptureUnavailable, CaptureAuthorityID: digest,
		DiagnosticCode: "artifact_capture_unavailable",
	}
}

func (s *Service) scheduleStructuredCaptureTerminal(rec receipt.Receipt, result structuredapp.TerminalCaptureResult) {
	if result.State == "" {
		return
	}
	if s.options.StructuredCaptureTerminal == nil {
		_ = result.Close()
		return
	}
	if err := s.options.StructuredCaptureTerminal.ScheduleTerminal(context.Background(), rec, result); err != nil {
		_ = result.Close()
	}
}

func (s *Service) scheduleStructuredAfterReceipt(rec receipt.Receipt, reservation operation.Reservation, capture structuredapp.TerminalCaptureResult) {
	if reservation.StructuredCaptureDigest != "" {
		s.scheduleStructuredCaptureTerminal(rec, capture)
		return
	}
	s.scheduleStructuredTerminal(rec, reservation.StructuredAdapter)
}
