package evidence

import (
	"context"
	"errors"
	"fmt"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var ErrNoEvidenceContract = errors.New("no evidence contract")

type Service struct {
	repository Repository
	observer   ArtifactObserver
}

func NewService(repository Repository, observer ArtifactObserver) *Service {
	return &Service{repository: repository, observer: observer}
}

func (s *Service) DeriveTerminal(ctx context.Context, scheduled receipt.Receipt) (core.Record, bool, error) {
	authority, reservation, snapshot, err := s.loadTerminalAuthority(ctx, scheduled)
	if err != nil {
		return core.Record{}, false, err
	}
	contract, ok, err := contractFromReservation(reservation)
	if err != nil {
		return core.Record{}, false, err
	}
	if !ok {
		return core.Record{}, false, ErrNoEvidenceContract
	}
	if err := verifyReceiptContract(authority, reservation, contract); err != nil {
		return core.Record{}, false, err
	}

	artifacts := []core.ArtifactObservation(nil)
	if len(contract.ExpectedOutputs) > 0 {
		artifacts = s.observeArtifacts(ctx, reservation, contract, snapshot)
	}
	receiptDigest, err := receipt.Digest(authority)
	if err != nil {
		return core.Record{}, false, err
	}
	contractDigest, err := contract.Digest()
	if err != nil {
		return core.Record{}, false, err
	}
	evidenceID, err := core.EvidenceID(receiptDigest, contractDigest)
	if err != nil {
		return core.Record{}, false, err
	}
	command, err := commandAuthority(reservation)
	if err != nil {
		return core.Record{}, false, err
	}
	terminal := core.TerminalResult{Authoritative: true, Outcome: authority.Outcome}
	source := sourceBinding(authority, reservation)
	record := core.Record{
		SchemaVersion: core.SchemaVersion, EvidenceID: evidenceID,
		OperationID: authority.OperationID, SessionID: authority.SessionID,
		ActivityID: reservation.ActivityID, WorkspaceID: string(source.WorkspaceID),
		VerificationKind: contract.VerificationKind, SourceScope: contract.SourceScope,
		ContractDigest: contractDigest, Command: command, ReceiptDigest: receiptDigest,
		Terminal: terminal, Result: core.DeriveResult(terminal, artifacts), Source: source,
		Artifacts: artifacts, CompletedAt: snapshot.UpdatedAt.UTC(),
		EnvironmentBinding: cloneEnvironmentBinding(reservation.EnvironmentBinding),
	}
	if record.WorkspaceID == "" {
		record.WorkspaceID = reservation.WorkspaceID
	}
	if err := record.Validate(); err != nil {
		return core.Record{}, false, err
	}
	created, err := s.repository.PutEvidenceRecord(ctx, record)
	if err != nil {
		return core.Record{}, false, err
	}
	return record, created, nil
}

func (s *Service) loadTerminalAuthority(ctx context.Context, scheduled receipt.Receipt) (receipt.Receipt, operation.Reservation, session.Snapshot, error) {
	if s == nil || s.repository == nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, fmt.Errorf("evidence repository unavailable")
	}
	if err := scheduled.Validate(); err != nil || !scheduled.State.Terminal() {
		if err == nil {
			err = fmt.Errorf("scheduled receipt is not terminal")
		}
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	sid, err := operation.ParseSessionID(scheduled.SessionID)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	opID, err := operation.ParseID(scheduled.OperationID)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	stored, err := s.repository.LoadReceipt(ctx, sid)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	if err := stored.Validate(); err != nil || !stored.State.Terminal() {
		if err == nil {
			err = fmt.Errorf("stored receipt is not terminal")
		}
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	scheduledDigest, err := receipt.Digest(scheduled)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	storedDigest, err := receipt.Digest(stored)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	if scheduledDigest != storedDigest {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, fmt.Errorf("scheduled receipt authority mismatch")
	}
	reservation, err := s.repository.LoadOperation(ctx, opID)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	if err := receiptReservationConsistent(stored, reservation); err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	snapshot, err := s.repository.LoadSession(ctx, sid)
	if err != nil {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, err
	}
	if snapshot.OperationID != stored.OperationID || snapshot.SessionID != stored.SessionID || snapshot.State != stored.State || snapshot.Outcome != stored.Outcome || snapshot.UpdatedAt.IsZero() {
		return receipt.Receipt{}, operation.Reservation{}, session.Snapshot{}, fmt.Errorf("terminal session authority mismatch")
	}
	return stored, reservation, snapshot, nil
}

func receiptReservationConsistent(rec receipt.Receipt, reservation operation.Reservation) error {
	if string(reservation.OperationID) != rec.OperationID || string(reservation.SessionID) != rec.SessionID || reservation.SchemaVersion != rec.SchemaVersion {
		return fmt.Errorf("receipt reservation identity mismatch")
	}
	if reservation.SchemaVersion >= 2 {
		if reservation.RequestFingerprint != rec.RequestFingerprint || reservation.ExecutionFingerprint != rec.ExecutionFingerprint || reservation.ObservationBindingFingerprint != rec.ObservationBindingFingerprint {
			return fmt.Errorf("receipt reservation fingerprint mismatch")
		}
	} else if reservation.Fingerprint != rec.Fingerprint {
		return fmt.Errorf("legacy receipt reservation fingerprint mismatch")
	}
	return nil
}

func contractFromReservation(reservation operation.Reservation) (core.Contract, bool, error) {
	if reservation.Evidence != nil {
		normalized, err := reservation.Evidence.Normalize()
		return normalized, err == nil, err
	}
	if reservation.ProjectCommand != nil {
		binding := reservation.ProjectCommand
		if err := binding.Validate(); err != nil {
			return core.Contract{}, false, err
		}
		if binding.SchemaVersion == project.BindingSchemaV1 {
			return core.Contract{}, false, nil
		}
		kind, mapped := verificationFromProjectKind(binding.Kind)
		if !mapped && len(binding.ExpectedOutputs) == 0 {
			return core.Contract{}, false, nil
		}
		if !mapped {
			kind = core.VerificationArtifact
		}
		contract := core.Contract{VerificationKind: kind, SourceScope: core.SourceScope(binding.SourceScope), ExpectedOutputs: append([]project.Output(nil), binding.ExpectedOutputs...)}
		normalized, err := contract.Normalize()
		return normalized, err == nil, err
	}
	if reservation.Intent != nil {
		kind, ok := verificationFromIntent(reservation.Intent.Kind)
		if !ok {
			return core.Contract{}, false, nil
		}
		contract := core.Contract{VerificationKind: kind}
		return contract, true, nil
	}
	return core.Contract{}, false, nil
}

func verificationFromProjectKind(kind string) (core.VerificationKind, bool) {
	switch kind {
	case "format":
		return core.VerificationFormat, true
	case "test":
		return core.VerificationTest, true
	case "build":
		return core.VerificationBuild, true
	case "generate":
		return core.VerificationGenerate, true
	case "release":
		return core.VerificationRelease, true
	default:
		return "", false
	}
}

func verificationFromIntent(kind operation.IntentKind) (core.VerificationKind, bool) {
	switch kind {
	case operation.IntentKindFormat:
		return core.VerificationFormat, true
	case operation.IntentKindTest:
		return core.VerificationTest, true
	case operation.IntentKindBuild:
		return core.VerificationBuild, true
	case operation.IntentKindGenerate:
		return core.VerificationGenerate, true
	case operation.IntentKindRelease:
		return core.VerificationRelease, true
	default:
		return "", false
	}
}

func verifyReceiptContract(rec receipt.Receipt, reservation operation.Reservation, contract core.Contract) error {
	if reservation.SchemaVersion == 2 && reservation.Evidence != nil {
		if rec.Evidence == nil {
			return fmt.Errorf("receipt evidence contract missing")
		}
		left, err := reservation.Evidence.Digest()
		if err != nil {
			return err
		}
		right, err := rec.Evidence.Digest()
		if err != nil {
			return err
		}
		want, err := contract.Digest()
		if err != nil {
			return err
		}
		if left != right || left != want {
			return fmt.Errorf("receipt evidence contract mismatch")
		}
	}
	if reservation.ProjectCommand != nil {
		if rec.ProjectCommand == nil {
			return fmt.Errorf("receipt project binding missing")
		}
		left, err := reservation.ProjectCommand.Digest()
		if err != nil {
			return err
		}
		right, err := rec.ProjectCommand.Digest()
		if err != nil {
			return err
		}
		if left != right {
			return fmt.Errorf("receipt project binding mismatch")
		}
	}
	return nil
}

func commandAuthority(reservation operation.Reservation) (core.CommandAuthority, error) {
	out := core.CommandAuthority{RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, ObservationFingerprint: reservation.ObservationBindingFingerprint}
	if reservation.ProjectCommand != nil {
		digest, err := reservation.ProjectCommand.Digest()
		if err != nil {
			return core.CommandAuthority{}, err
		}
		out.ProjectCommandID = reservation.ProjectCommand.CommandID
		out.ProjectBindingDigest = digest
		out.ManifestDigest = reservation.ProjectCommand.ManifestDigest
	}
	return out, nil
}

func (s *Service) observeArtifacts(ctx context.Context, reservation operation.Reservation, contract core.Contract, snapshot session.Snapshot) []core.ArtifactObservation {
	root, ok := s.workspaceRoot(ctx, reservation.WorkspaceID)
	if !ok || s.observer == nil {
		return unavailableArtifacts(contract.ExpectedOutputs, snapshot.UpdatedAt)
	}
	observations, err := s.observer.Observe(ctx, root, contract.ExpectedOutputs)
	if err != nil {
		return unavailableArtifacts(contract.ExpectedOutputs, snapshot.UpdatedAt)
	}
	return observations
}

func (s *Service) workspaceRoot(ctx context.Context, workspaceID string) (string, bool) {
	if workspaceID == "" {
		return "", false
	}
	workspaces, err := s.repository.ListWorkspaces(ctx)
	if err != nil {
		return "", false
	}
	for _, item := range workspaces {
		if string(item.ID) == workspaceID && item.Validate() == nil {
			return item.Root, true
		}
	}
	return "", false
}

func unavailableArtifacts(outputs []project.Output, observedAt time.Time) []core.ArtifactObservation {
	result := make([]core.ArtifactObservation, 0, len(outputs))
	for _, output := range outputs {
		result = append(result, core.ArtifactObservation{SchemaVersion: core.ArtifactSchemaVersion, Path: output.Path, DeclaredKind: output.Kind, Required: output.Required, DigestMode: output.Digest, Status: core.ArtifactUnavailable, Quality: core.ObservationUnavailable, ObservedAt: observedAt.UTC()})
	}
	return result
}

func sourceBinding(rec receipt.Receipt, reservation operation.Reservation) core.SourceBinding {
	source := core.SourceBinding{WorkspaceID: reservation.WorkspaceID, ObservationQuality: core.SourceQualityUnknown}
	p := rec.WorkspaceProvenance
	if p == nil {
		return source
	}
	switch p.SchemaVersion {
	case 1:
		source.RepositoryID, source.WorkspaceID = string(p.RepositoryID), string(p.WorkspaceID)
		source.PreGeneration, source.PostGeneration, source.ObservedChange = p.PreGeneration, p.PostGeneration, p.ObservedChange
		if p.PostGeneration != "" && p.PostQuality != workspace.QualityUnavailable {
			source.ObservationQuality = core.SourceQualityFast
		}
	case 2:
		source.RepositoryID, source.WorkspaceID = string(p.Binding.RepositoryID), string(p.Binding.WorkspaceID)
		source.PreGeneration, source.PostGeneration, source.ObservedChange = p.Pre.Generation, p.Post.Generation, p.ObservedChange
		if p.Post.Generation != "" && p.Post.Quality != workspace.QualityUnavailable && !p.Post.ObservationInvalidated {
			source.ObservationQuality = core.SourceQualityFast
		}
	}
	return source
}
