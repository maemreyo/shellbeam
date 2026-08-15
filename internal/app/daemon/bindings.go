package daemon

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) reservationForStart(req StartRequest, id operation.ID, intent operation.Intent, spec operation.ExecutionSpec) (operation.Reservation, error) {
	resolvedCWD := intent.ResolvedCWD
	if resolvedCWD == "" {
		resolvedCWD = req.CWD
	}
	logicalCWD := ""
	if req.WorkspaceID != "" {
		logicalCWD = intent.CWD
	}
	shell := ""
	if spec.Mode == operation.ExecutionModeShell {
		shell = spec.Shell
	}
	structuredAdapter, err := normalizedStructuredAdapter(req)
	if err != nil {
		return operation.Reservation{}, err
	}
	var frozenEvidence = req.Evidence
	if req.Evidence != nil {
		normalized, normalizeErr := req.Evidence.Normalize()
		if normalizeErr != nil {
			return operation.Reservation{}, normalizeErr
		}
		frozenEvidence = &normalized
	}
	base := operation.Reservation{
		OperationID: id, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, LogicalCWD: logicalCWD, StructuredAdapter: structuredAdapter, Evidence: frozenEvidence, Intent: cloneDeclaredIntent(req.Intent),
		ExecutionMode: spec.Mode, Executable: spec.Executable, Command: req.Command, Argv: append([]string(nil), req.Argv...),
		CWD: resolvedCWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, Shell: shell, DaemonIncarnation: s.options.Incarnation,
	}
	switch req.ProtocolVersion {
	case 0, 1:
		fingerprint, err := intent.Fingerprint()
		if err != nil {
			return operation.Reservation{}, err
		}
		base.SchemaVersion = 1
		base.Fingerprint = fingerprint
		return base, nil
	case 2:
		requestFingerprint, err := intent.RequestFingerprint()
		if err != nil {
			return operation.Reservation{}, err
		}
		executionFingerprint, err := intent.ExecutionFingerprint(spec.Executable)
		if err != nil {
			return operation.Reservation{}, err
		}
		observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID, Intent: req.Intent, StructuredAdapter: structuredAdapter, Evidence: frozenEvidence}).Fingerprint()
		if err != nil {
			return operation.Reservation{}, err
		}
		base.SchemaVersion = 2
		base.RequestFingerprint = requestFingerprint
		base.ExecutionFingerprint = executionFingerprint
		base.ObservationBindingFingerprint = observationFingerprint
		return base, nil
	default:
		return operation.Reservation{}, fmt.Errorf("unsupported protocol version")
	}
}

func (s *Service) receiptFor(l *liveSession, state session.State, outcome session.Outcome) receipt.Receipt {
	rec := receipt.Receipt{
		SchemaVersion: l.reservation.SchemaVersion, OperationID: l.operationID, SessionID: l.sessionID,
		DaemonIncarnation: s.options.Incarnation, ExecutionMode: string(l.reservation.ExecutionMode), Executable: l.reservation.Executable,
		Shell: l.reservation.Shell, CWD: l.reservation.CWD, TTY: l.reservation.TTY, TimeoutMS: l.reservation.TimeoutMS,
		State: state, Outcome: outcome,
	}
	if rec.SchemaVersion >= 2 {
		rec.RequestFingerprint = l.reservation.RequestFingerprint
		rec.ExecutionFingerprint = l.reservation.ExecutionFingerprint
		rec.ObservationBindingFingerprint = l.reservation.ObservationBindingFingerprint
		rec.ProjectCommand = l.reservation.ProjectCommand
		if rec.SchemaVersion == 2 && l.reservation.Evidence != nil {
			frozen := *l.reservation.Evidence
			frozen.ExpectedOutputs = append(frozen.ExpectedOutputs[:0:0], l.reservation.Evidence.ExpectedOutputs...)
			rec.Evidence = &frozen
		}
	} else {
		rec.Fingerprint = l.reservation.Fingerprint
	}
	return rec
}

func cloneDeclaredIntent(value *operation.DeclaredIntent) *operation.DeclaredIntent {
	if value == nil {
		return nil
	}
	copy := *value
	if value.MutatesSource != nil {
		v := *value.MutatesSource
		copy.MutatesSource = &v
	}
	if value.ExternalEffect != nil {
		v := *value.ExternalEffect
		copy.ExternalEffect = &v
	}
	return &copy
}
