package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync/atomic"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/failure"
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
	if isStructuredCaptureAdapter(reservation.StructuredAdapter) {
		return
	}
	s.scheduleStructuredTerminal(rec, reservation.StructuredAdapter)
}

func (s *Service) prepareStructuredCaptureAdmission(ctx context.Context, req StartRequest, reservation *operation.Reservation, spec operation.ExecutionSpec) (StructuredCapturePreparation, error) {
	if reservation == nil {
		return StructuredCapturePreparation{}, failure.New(failure.Internal, nil, fmt.Errorf("structured capture reservation unavailable"))
	}
	explicitCapture := isStructuredCaptureAdapter(req.StructuredAdapter)
	autoCandidate := req.StructuredAdapter == "" && (structuredapp.PytestCandidateArgv(spec.Argv) || structuredapp.JestCandidateArgv(spec.Argv))
	if !explicitCapture && !autoCandidate {
		return StructuredCapturePreparation{}, nil
	}
	if s.options.StructuredCapturePreparer == nil {
		if explicitCapture {
			return StructuredCapturePreparation{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": structuredCaptureFeature(req.StructuredAdapter)}, nil)
		}
		return StructuredCapturePreparation{}, nil
	}
	preparation, err := s.options.StructuredCapturePreparer.PrepareStructuredCapture(ctx, StructuredCapturePrepareRequest{
		OperationID: reservation.OperationID, SessionID: reservation.SessionID, WorkspaceID: reservation.WorkspaceID,
		StructuredAdapter: req.StructuredAdapter, Argv: append([]string(nil), spec.Argv...), CWD: spec.CWD, ExecutionMode: spec.Mode, Executable: spec.Executable,
	})
	if err != nil {
		if explicitCapture {
			return preparation, failure.New(failure.InvalidInput, map[string]string{"field": "structured_adapter", "adapter": req.StructuredAdapter, "reason": "producer_precondition_failed"}, err)
		}
		return StructuredCapturePreparation{}, nil
	}
	if preparation.AdapterID == "" {
		if explicitCapture {
			return preparation, failure.New(failure.InvalidInput, map[string]string{"field": "structured_adapter", "adapter": req.StructuredAdapter, "reason": "producer_precondition_failed"}, fmt.Errorf("producer invocation did not establish qualified authority"))
		}
		reservation.StructuredAdapter = ""
		reservation.StructuredCaptureDigest = ""
	} else {
		if !isStructuredCaptureAdapter(preparation.AdapterID) || !operation.ValidStructuredAdapterID(preparation.AdapterID) || (explicitCapture && preparation.AdapterID != req.StructuredAdapter) {
			return preparation, failure.New(failure.Internal, nil, fmt.Errorf("invalid structured capture preparation"))
		}
		reservation.StructuredAdapter = preparation.AdapterID
		reservation.StructuredCaptureDigest = preparation.CaptureDigest
	}
	fingerprint, fpErr := structuredObservationFingerprint(req, *reservation)
	if fpErr != nil {
		return preparation, invalidIntentFailure(fpErr)
	}
	reservation.ObservationBindingFingerprint = fingerprint
	return preparation, nil
}

func isStructuredCaptureAdapter(adapter string) bool {
	return adapter == structuredapp.PytestJUnitAdapterID || adapter == structuredapp.JestJSONAdapterID
}

func structuredCaptureFeature(adapter string) string {
	switch adapter {
	case structuredapp.PytestJUnitAdapterID:
		return "pytest_structured_capture"
	case structuredapp.JestJSONAdapterID:
		return "jest_structured_capture"
	default:
		return "structured_capture"
	}
}

func structuredObservationFingerprint(req StartRequest, reservation operation.Reservation) (string, error) {
	binding := operation.ObservationBinding{ActivityID: req.ActivityID, ExperimentID: reservation.ExperimentID, Intent: req.Intent, StructuredAdapter: reservation.StructuredAdapter, StructuredCaptureDigest: reservation.StructuredCaptureDigest}
	if reservation.ProjectCommand == nil {
		binding.Evidence = reservation.Evidence
		binding.VerificationAttempt = reservation.VerificationAttempt
	}
	return binding.Fingerprint()
}

func (s *Service) abortStructuredCapture(ctx context.Context, reservation operation.Reservation) error {
	if s.options.StructuredCapturePreparer == nil {
		return nil
	}
	return s.options.StructuredCapturePreparer.AbortStructuredCapture(ctx, reservation.OperationID, reservation.SessionID)
}
