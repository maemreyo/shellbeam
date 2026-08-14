package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

func (r *Repository) initReproStore() error {
	for _, dir := range []string{r.derivedRoot(), r.reproRoot(), r.reproCreateDir()} {
		if err := ensurePrivateDir(dir); err != nil {
			return fmt.Errorf("repro store: %w", err)
		}
	}
	return nil
}

func (r *Repository) CreateRepro(ctx context.Context, requestFingerprint string, capsule core.Capsule) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	if !validReproDigest(requestFingerprint) {
		return core.Capsule{}, false, fmt.Errorf("invalid repro request fingerprint")
	}
	if err := capsule.Validate(); err != nil {
		return core.Capsule{}, false, err
	}
	record := reproCreateRecord{SchemaVersion: 1, RequestFingerprint: requestFingerprint, Capsule: capsule}
	encoded, err := json.Marshal(record)
	if err != nil {
		return core.Capsule{}, false, err
	}
	incomingSize := int64(len(encoded) + 1)

	r.reproMu.Lock()
	defer r.reproMu.Unlock()
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	if current, err := r.readReproCreateRecordUnlocked(capsule.CreateID); err == nil {
		if current.RequestFingerprint != requestFingerprint {
			return core.Capsule{}, false, fmt.Errorf("operation_metadata_conflict")
		}
		return current.Capsule, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return core.Capsule{}, false, err
	}

	entries, err := r.reproEntriesLocked()
	if err != nil {
		return core.Capsule{}, false, err
	}
	evictions, err := planReproEvictions(entries, incomingSize, r.limits, r.now().UTC())
	if err != nil {
		return core.Capsule{}, false, err
	}
	evicted := map[string]bool{}
	for _, entry := range evictions {
		evicted[entry.record.Capsule.CreateID] = true
	}
	for _, entry := range entries {
		if evicted[entry.record.Capsule.CreateID] {
			continue
		}
		if entry.record.Capsule.ReproID == capsule.ReproID {
			return core.Capsule{}, false, fmt.Errorf("repro_id_conflict")
		}
	}

	// Keep prepared repro obligations invisible to live journal inspection until
	// the canonical create is known committed or aborted. Crash recovery still
	// observes the durable prepared record after process locks disappear.
	r.observationVisibilityMu.Lock()
	defer r.observationVisibilityMu.Unlock()
	seq, prepared := r.prepareReproObservation(ctx, capsule)
	if prepared.Err != nil {
		return core.Capsule{}, false, prepared.Err
	}
	if err := r.removeReproEntries(evictions); err != nil {
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		return core.Capsule{}, false, err
	}

	return r.writeReproCreateLocked(requestFingerprint, record, seq)
}

func (r *Repository) writeReproCreateLocked(requestFingerprint string, record reproCreateRecord, seq observation.ChangeSeq) (core.Capsule, bool, error) {
	capsule := record.Capsule
	result := r.writer.Create(r.reproCreatePath(capsule.CreateID), record)
	if result.Err == nil {
		r.commitObservationBestEffort(seq)
		return capsule, true, nil
	}
	if errors.Is(result.Err, os.ErrExist) {
		current, err := r.readReproCreateRecordUnlocked(capsule.CreateID)
		r.abortObservationBestEffort(seq, observationAbortConflict)
		if err == nil && current.RequestFingerprint == requestFingerprint {
			return current.Capsule, false, nil
		}
		if err == nil {
			return core.Capsule{}, false, fmt.Errorf("operation_metadata_conflict")
		}
		return core.Capsule{}, false, result.Err
	}
	if result.Durability == app.AmbiguousChange {
		current, readErr := r.readReproCreateRecordUnlocked(capsule.CreateID)
		if readErr == nil && current.RequestFingerprint == requestFingerprint && reflect.DeepEqual(current.Capsule, capsule) {
			r.commitObservationBestEffort(seq)
			return current.Capsule, true, nil
		}
	}
	r.abortObservationBestEffort(seq, observationAbortWriteFailed)
	return core.Capsule{}, false, result.Err
}

func (r *Repository) GetReproByCreateID(ctx context.Context, createID string) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	r.reproMu.Lock()
	defer r.reproMu.Unlock()
	record, err := r.readReproCreateRecordUnlocked(createID)
	if errors.Is(err, ErrNotFound) {
		return core.Capsule{}, false, nil
	}
	if err != nil {
		return core.Capsule{}, false, err
	}
	return record.Capsule, true, nil
}

func (r *Repository) GetRepro(ctx context.Context, reproID string) (core.Capsule, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.Capsule{}, false, err
	}
	if !validReproLookupID(reproID) {
		return core.Capsule{}, false, fmt.Errorf("invalid repro id")
	}
	r.reproMu.Lock()
	defer r.reproMu.Unlock()
	entries, err := r.reproEntriesLocked()
	if err != nil {
		return core.Capsule{}, false, err
	}
	for _, entry := range entries {
		if entry.record.Capsule.ReproID == reproID {
			return entry.record.Capsule, true, nil
		}
	}
	return core.Capsule{}, false, nil
}

func (r *Repository) prepareReproObservation(ctx context.Context, capsule core.Capsule) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind: observation.EventReproRecorded,
		Correlation: observation.Correlation{
			RepositoryID: capsule.Source.RepositoryID, WorkspaceID: capsule.Source.WorkspaceID,
			OperationID: capsule.Execution.OperationID, SessionID: capsule.Execution.SessionID,
		},
		SubjectRef: reproObservationSubject(capsule), Summary: "reproduction capsule recorded",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func reproObservationSubject(capsule core.Capsule) string {
	return "repro:" + capsule.CreateID + ":" + capsule.ReproID
}

func (r *Repository) removeReproEntries(entries []reproEntry) error {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if result := r.writer.syncParent("repro_remove", r.reproCreateDir()); result.Err != nil {
		return result.Err
	}
	return nil
}

func (r *Repository) reproSubjectPresent(ctx context.Context, subject string) (bool, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 3 || parts[0] != "repro" || !validReproLookupID(parts[2]) {
		return false, fmt.Errorf("invalid repro observation subject")
	}
	capsule, found, err := r.GetReproByCreateID(ctx, parts[1])
	if err != nil || !found {
		return false, err
	}
	return capsule.ReproID == parts[2], nil
}
