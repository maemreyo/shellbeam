package daemon

import (
	"fmt"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
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
	if intent.WorkspaceID != "" {
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
	frozenEvidence, err := normalizedStartEvidence(req.Evidence)
	if err != nil {
		return operation.Reservation{}, err
	}
	base := operation.Reservation{
		OperationID: id, ActivityID: req.ActivityID, ExperimentID: req.ExperimentID, WorkspaceID: intent.WorkspaceID, LogicalCWD: logicalCWD, StructuredAdapter: structuredAdapter, Evidence: frozenEvidence, VerificationAttempt: cloneVerificationAttempt(req.VerificationAttempt), Intent: cloneDeclaredIntent(req.Intent),
		ExecutionMode: spec.Mode, Executable: spec.Executable, Command: req.Command, Argv: append([]string(nil), req.Argv...),
		CWD: resolvedCWD, TTY: req.TTY, TimeoutMS: req.TimeoutMS, Persistent: req.Persistent, SessionMode: req.SessionMode, SessionName: req.SessionName, Shell: shell, DaemonIncarnation: s.options.Incarnation,
		ResourceLimits: req.ResourceLimits.Clone(),
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
		requestIntent := intent
		requestIntent.WorkspaceID = req.WorkspaceID
		requestIntent.CWD = req.CWD
		requestFingerprint, err := requestIntent.RequestFingerprint()
		if err != nil {
			return operation.Reservation{}, err
		}
		executionIntent := intent
		executionIntent.TraceMode = trace.ModeOff
		executionIntent.TraceExecutionDigest = ""
		executionFingerprint, err := executionIntent.ExecutionFingerprint(spec.Executable)
		if err != nil {
			return operation.Reservation{}, err
		}
		observationFingerprint, err := (operation.ObservationBinding{ActivityID: req.ActivityID, ExperimentID: req.ExperimentID, Intent: req.Intent, StructuredAdapter: structuredAdapter, Evidence: frozenEvidence, VerificationAttempt: req.VerificationAttempt}).Fingerprint()
		if err != nil {
			return operation.Reservation{}, err
		}
		if req.SessionMode == delegated.ModeDelegatedInteractive {
			base.SchemaVersion = 5
			base.AuthorityEpoch = 1
		} else if req.Persistent {
			if intent.Resolved == nil || intent.TimeoutSource == "" {
				return operation.Reservation{}, fmt.Errorf("persistent execution policy was not resolved")
			}
			base.SchemaVersion = 4
			base.TimeoutMS = spec.TimeoutMS
			base.StdinMode = spec.StdinMode
			base.TimeoutSource = intent.TimeoutSource
			base.StdinModeSource = intent.Resolved.StdinSource()
		} else {
			base.SchemaVersion = 2
		}
		base.RequestFingerprint = requestFingerprint
		base.ExecutionFingerprint = executionFingerprint
		base.ObservationBindingFingerprint = observationFingerprint
		return base, nil
	default:
		return operation.Reservation{}, fmt.Errorf("unsupported protocol version")
	}
}

func normalizedStartEvidence(value *evidence.Contract) (*evidence.Contract, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := value.Normalize()
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

func (s *Service) receiptFor(l *liveSession, state session.State, outcome session.Outcome) receipt.Receipt {
	rec := receipt.Receipt{
		SchemaVersion: l.reservation.SchemaVersion, OperationID: l.operationID, SessionID: l.sessionID,
		DaemonIncarnation: s.options.Incarnation, ExecutionMode: string(l.reservation.ExecutionMode), Executable: l.reservation.Executable,
		Shell: l.reservation.Shell, CWD: l.reservation.CWD, TTY: l.reservation.TTY, TimeoutMS: l.reservation.TimeoutMS, Persistent: l.reservation.Persistent, SessionName: l.reservation.SessionName,
		State: state, Outcome: outcome,
	}
	if rec.SchemaVersion >= 2 {
		if rec.SchemaVersion == 4 {
			rec.StdinMode = string(l.reservation.StdinMode)
			rec.TimeoutSource = l.reservation.TimeoutSource
			rec.StdinModeSource = l.reservation.StdinModeSource
		}
		rec.RequestFingerprint = l.reservation.RequestFingerprint
		rec.ExecutionFingerprint = l.reservation.ExecutionFingerprint
		rec.ObservationBindingFingerprint = l.reservation.ObservationBindingFingerprint
		rec.ProjectCommand = l.reservation.ProjectCommand
		if rec.SchemaVersion == 2 || rec.SchemaVersion == 3 {
			rec.ResourceCleanup = l.resourceCleanup
		}
		if (rec.SchemaVersion == 2 || rec.SchemaVersion == 3) && l.reservation.HermeticBoundary != nil {
			rec.HermeticBinding = l.reservation.HermeticBoundary.Clone()
			if l.hermeticResult != nil {
				result := *l.hermeticResult
				rec.HermeticResult = &result
			} else {
				rec.HermeticResult = lostHermeticResult(l.reservation.HermeticBoundary)
			}
		}
		if (rec.SchemaVersion == 2 || (rec.SchemaVersion == 4 && l.reservation.ProjectCommand == nil)) && l.reservation.Evidence != nil {
			frozen := *l.reservation.Evidence
			frozen.ExpectedOutputs = append(frozen.ExpectedOutputs[:0:0], l.reservation.Evidence.ExpectedOutputs...)
			rec.Evidence = &frozen
		}
	} else {
		rec.Fingerprint = l.reservation.Fingerprint
	}
	if rec.SchemaVersion == 5 {
		rec.SessionMode = l.reservation.SessionMode
		rec.AuthorityEpoch = l.delegatedBinding.AuthorityEpoch
		rec.EvidenceAuthority = receipt.EvidenceAuthoritySessionLifecycleOnly
		rec.InputAuthorityProvenance = receipt.InputAuthorityAgentOnly
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
func cloneVerificationAttempt(value *evidence.VerificationAttemptIntent) *evidence.VerificationAttemptIntent {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
