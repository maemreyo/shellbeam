package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const (
	observationAbortWriteFailed = "canonical_write_failed"
	observationAbortMissing     = "canonical_missing"
	observationAbortConflict    = "canonical_conflict"
)

func (r *Repository) prepareAdmissionObservation(ctx context.Context, reservation operation.Reservation) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind: observation.EventOperationAdmitted, Correlation: correlationFromReservation(reservation),
		SubjectRef: "operation:" + string(reservation.OperationID), Summary: "operation admitted",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) PrepareProcessStartedObservation(ctx context.Context, operationID, sessionID string) app.StoreResult {
	request := observation.PrepareRequest{
		Kind: observation.EventProcessStarted, Correlation: r.correlationForSession(operationID, sessionID),
		SubjectRef: "session:" + sessionID + ":started", Summary: "process started",
	}
	seq, result := r.prepareExecutionObservation(ctx, request)
	return withObservationSeq(result, seq)
}

func (r *Repository) CommitObservationSequence(ctx context.Context, seq uint64) app.StoreResult {
	return r.CommitObservation(ctx, observation.ChangeSeq(seq))
}

func (r *Repository) AbortObservationSequence(ctx context.Context, seq uint64, reason string) app.StoreResult {
	return r.AbortObservation(ctx, observation.ChangeSeq(seq), reason)
}

func (r *Repository) prepareOutputObservation(ctx context.Context, id operation.SessionID, start, end int64) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind: observation.EventOutputAvailable, Correlation: r.correlationForSession("", string(id)),
		SubjectRef: fmt.Sprintf("output:%s:%d:%d", id, start, end), Summary: "output available",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) prepareTerminalObservation(ctx context.Context, rec receipt.Receipt) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind: observation.EventProcessTerminal, Correlation: r.correlationForSession(rec.OperationID, rec.SessionID),
		SubjectRef: "receipt:" + rec.SessionID, Summary: "process terminal",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) prepareExecutionObservation(ctx context.Context, request observation.PrepareRequest) (observation.ChangeSeq, app.StoreResult) {
	prepared, result := r.PrepareObservation(ctx, request)
	if prepared.Obligation.ChangeSeq != 0 {
		result.ObservationSeq = uint64(prepared.Obligation.ChangeSeq)
	}
	if result.Err != nil {
		return prepared.Obligation.ChangeSeq, result
	}
	return prepared.Obligation.ChangeSeq, app.StoreResult{Durability: result.Durability, ObservationSeq: uint64(prepared.Obligation.ChangeSeq)}
}

func correlationFromReservation(reservation operation.Reservation) observation.Correlation {
	return observation.Correlation{
		OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID),
		ActivityID: reservation.ActivityID, WorkspaceID: reservation.WorkspaceID,
	}
}

func (r *Repository) correlationForSession(operationID, sessionID string) observation.Correlation {
	correlation := observation.Correlation{OperationID: operationID, SessionID: sessionID}
	if operationID == "" {
		if snap, err := r.LoadSession(context.Background(), operation.SessionID(sessionID)); err == nil {
			correlation.OperationID = snap.OperationID
		}
	}
	if correlation.OperationID != "" {
		if reservation, err := r.LoadOperation(context.Background(), operation.ID(correlation.OperationID)); err == nil {
			correlation.ActivityID = reservation.ActivityID
			correlation.WorkspaceID = reservation.WorkspaceID
			if correlation.SessionID == "" {
				correlation.SessionID = string(reservation.SessionID)
			}
		}
	}
	return correlation
}

func (r *Repository) commitObservationBestEffort(seq observation.ChangeSeq) {
	if seq != 0 {
		_ = r.CommitObservation(context.Background(), seq)
	}
}

func (r *Repository) abortObservationBestEffort(seq observation.ChangeSeq, reason string) {
	if seq != 0 {
		_ = r.AbortObservation(context.Background(), seq, reason)
	}
}

func withObservationSeq(result app.StoreResult, seq observation.ChangeSeq) app.StoreResult {
	result.ObservationSeq = uint64(seq)
	return result
}

func (r *Repository) reservationFileMatches(path string, want operation.Reservation) bool {
	var got operation.Reservation
	return readStrict(path, &got) == nil && reflect.DeepEqual(got, want)
}

func (r *Repository) runningSessionFileMatches(path string, want session.Snapshot) bool {
	var got session.Snapshot
	return readStrict(path, &got) == nil && got.OperationID == want.OperationID && got.SessionID == want.SessionID && got.State == session.Running
}

func (r *Repository) receiptFileMatches(path string, want receipt.Receipt) bool {
	var got receipt.Receipt
	return readStrict(path, &got) == nil && reflect.DeepEqual(got, want)
}

func outputSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func appendOutputBytes(path string, b []byte) (int, app.StoreResult) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	n, writeErr := f.Write(b)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: writeErr}
	}
	if n != len(b) {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: io.ErrShortWrite}
	}
	if syncErr != nil {
		return n, app.StoreResult{Durability: app.AmbiguousChange, Err: syncErr}
	}
	if closeErr != nil {
		return n, app.StoreResult{Durability: app.DurableChange, Err: closeErr}
	}
	return n, app.StoreResult{Durability: app.DurableChange}
}

func (r *Repository) finishOutputObservation(seq observation.ChangeSeq, path string, start, end int64, result app.StoreResult) {
	switch result.Durability {
	case app.DurableChange:
		r.commitObservationBestEffort(seq)
	case app.NoDurableChange:
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
	case app.AmbiguousChange:
		size, err := outputSize(path)
		if err == nil && size <= start {
			r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		}
		// A visible full/partial range remains prepared until reconciliation can prove the subject.
		_ = end
	}
}

func (r *Repository) reconcilePreparedExecutionObservations(ctx context.Context) error {
	var after observation.ChangeSeq
	for {
		batch, err := r.ListObservationObligations(ctx, after, MaxObservationListRecords)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, obligation := range batch {
			after = obligation.ChangeSeq
			if obligation.State != observation.ObligationPrepared || !reconcilableObservationKind(obligation.Kind) {
				continue
			}
			present, err := r.observationSubjectPresent(ctx, obligation)
			if err != nil {
				return err
			}
			var result app.StoreResult
			if present {
				result = r.CommitObservation(ctx, obligation.ChangeSeq)
			} else {
				result = r.AbortObservation(ctx, obligation.ChangeSeq, observationAbortMissing)
			}
			if result.Err != nil {
				return result.Err
			}
			if obligation.Kind == observation.EventMutationScopeChanged {
				r.removeMutationScopeObservationProofBestEffort(obligation.ChangeSeq)
			}
		}
		if len(batch) < MaxObservationListRecords {
			return nil
		}
	}
}

func reconcilableObservationKind(kind observation.EventKind) bool {
	switch kind {
	case observation.EventOperationAdmitted, observation.EventProcessStarted, observation.EventOutputAvailable, observation.EventProcessTerminal, observation.EventStructuredChanged, observation.EventTelemetryChanged, observation.EventReproRecorded, observation.EventEvidenceRecorded, observation.EventArtifactObserved, observation.EventEvidenceValidityChanged, observation.EventMutationScopeChanged, observation.EventPersistentSessionStarted, observation.EventPersistentSessionReattached, observation.EventPersistentSessionTerminal, observation.EventPersistentSessionLost, observation.EventCheckpointCreated, observation.EventCheckpointRestoreStarted, observation.EventCheckpointRestoreCompleted, observation.EventCheckpointExpired, observation.EventInputTraceRecorded, observation.EventInputTraceTruncated:
		return true
	default:
		return false
	}
}

func (r *Repository) observationSubjectPresent(ctx context.Context, obligation observation.ObservationObligation) (bool, error) {
	switch obligation.Kind {
	case observation.EventOperationAdmitted:
		return r.operationSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventProcessStarted:
		return r.processStartedSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventOutputAvailable:
		return r.outputSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventProcessTerminal:
		return r.receiptSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventStructuredChanged:
		return r.structuredSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventTelemetryChanged:
		return r.telemetrySubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventReproRecorded:
		return r.reproSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventEvidenceRecorded:
		return r.evidenceSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventArtifactObserved:
		return r.evidenceArtifactSubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventEvidenceValidityChanged:
		return r.evidenceValiditySubjectPresent(ctx, obligation.SubjectRef)
	case observation.EventMutationScopeChanged:
		return r.mutationScopeObservationSubjectPresent(obligation)
	case observation.EventPersistentSessionStarted, observation.EventPersistentSessionReattached, observation.EventPersistentSessionTerminal, observation.EventPersistentSessionLost:
		return r.persistentObservationSubjectPresent(ctx, obligation)
	case observation.EventCheckpointCreated, observation.EventCheckpointRestoreStarted, observation.EventCheckpointRestoreCompleted, observation.EventCheckpointExpired:
		return r.checkpointObservationSubjectPresent(ctx, obligation)
	case observation.EventInputTraceRecorded, observation.EventInputTraceTruncated:
		return r.inputTraceSubjectPresent(ctx, obligation.SubjectRef)
	default:
		return false, nil
	}
}

func (r *Repository) operationSubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix = "operation:"
	if !strings.HasPrefix(subject, prefix) {
		return false, fmt.Errorf("invalid operation observation subject")
	}
	id, err := operation.ParseID(strings.TrimPrefix(subject, prefix))
	if err != nil {
		return false, fmt.Errorf("invalid operation observation subject")
	}
	_, err = r.LoadOperation(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) processStartedSubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix, suffix = "session:", ":started"
	if !strings.HasPrefix(subject, prefix) || !strings.HasSuffix(subject, suffix) {
		return false, fmt.Errorf("invalid process-start observation subject")
	}
	id, parseErr := operation.ParseSessionID(strings.TrimSuffix(strings.TrimPrefix(subject, prefix), suffix))
	if parseErr != nil {
		return false, fmt.Errorf("invalid process-start observation subject")
	}
	snap, err := r.LoadSession(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if snap.State == session.Running || snap.State == session.Finalizing {
		return true, nil
	}
	if snap.State.Terminal() {
		rec, err := r.LoadReceipt(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return err == nil && rec.Spawn.Succeeded, err
	}
	return false, nil
}

func (r *Repository) outputSubjectPresent(ctx context.Context, subject string) (bool, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 4 || parts[0] != "output" {
		return false, fmt.Errorf("invalid output observation subject")
	}
	id, parseErr := operation.ParseSessionID(parts[1])
	if parseErr != nil {
		return false, fmt.Errorf("invalid output observation session")
	}
	start, startErr := strconv.ParseInt(parts[2], 10, 64)
	end, endErr := strconv.ParseInt(parts[3], 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end <= start {
		return false, fmt.Errorf("invalid output observation range")
	}
	size, err := outputSize(filepath.Join(r.root, "sessions", string(id), "output.log"))
	if err != nil {
		return false, err
	}
	if size >= end {
		return true, nil
	}
	rec, err := r.LoadReceipt(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil && rec.OutputBytes >= end, err
}

func (r *Repository) receiptSubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix = "receipt:"
	if !strings.HasPrefix(subject, prefix) {
		return false, fmt.Errorf("invalid receipt observation subject")
	}
	id, parseErr := operation.ParseSessionID(strings.TrimPrefix(subject, prefix))
	if parseErr != nil {
		return false, fmt.Errorf("invalid receipt observation subject")
	}
	_, err := r.LoadReceipt(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) evidenceSubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix = "evidence:"
	if !strings.HasPrefix(subject, prefix) {
		return false, fmt.Errorf("invalid evidence observation subject")
	}
	id := strings.TrimPrefix(subject, prefix)
	if !evidenceIDPattern.MatchString(id) {
		return false, fmt.Errorf("invalid evidence observation subject")
	}
	_, err := r.LoadEvidenceRecord(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (r *Repository) evidenceArtifactSubjectPresent(ctx context.Context, subject string) (bool, error) {
	id, index, err := parseEvidenceArtifactSubject(subject)
	if err != nil {
		return false, err
	}
	record, err := r.LoadEvidenceRecord(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return index < len(record.Artifacts), nil
}

func (r *Repository) evidenceValiditySubjectPresent(ctx context.Context, subject string) (bool, error) {
	const suffix = ":validity"
	if !strings.HasPrefix(subject, "evidence:") || !strings.HasSuffix(subject, suffix) {
		return false, fmt.Errorf("invalid evidence validity observation subject")
	}
	id := strings.TrimSuffix(strings.TrimPrefix(subject, "evidence:"), suffix)
	if !evidenceIDPattern.MatchString(id) {
		return false, fmt.Errorf("invalid evidence validity observation subject")
	}
	_, found, err := r.LoadEvidenceValidity(ctx, id)
	return found, err
}
