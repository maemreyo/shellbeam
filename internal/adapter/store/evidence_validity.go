package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
)

const MaxEvidenceValidityBytes = 256 << 10

func (r *Repository) PutEvidenceValidity(ctx context.Context, value core.ValidityObservation) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := value.Validate(); err != nil {
		return false, err
	}
	record, err := r.LoadEvidenceRecord(ctx, value.EvidenceID)
	if err != nil {
		return false, err
	}

	r.evidenceValidityMu.Lock()
	defer r.evidenceValidityMu.Unlock()
	previous, found, err := r.loadEvidenceValidityUnlocked(value.EvidenceID)
	if err != nil {
		return false, err
	}
	if found && reflect.DeepEqual(previous, value) {
		return false, nil
	}
	changed := found && previous.Validity != value.Validity

	var seq observation.ChangeSeq
	if changed {
		prepared, result := r.PrepareObservation(ctx, observation.PrepareRequest{
			Kind:        observation.EventEvidenceValidityChanged,
			Correlation: observation.Correlation{RepositoryID: record.Source.RepositoryID, WorkspaceID: record.WorkspaceID, ActivityID: record.ActivityID, OperationID: record.OperationID, SessionID: record.SessionID},
			SubjectRef:  "evidence:" + record.EvidenceID + ":validity",
			Summary:     "evidence validity changed",
		})
		if result.Err != nil {
			return false, result.Err
		}
		seq = prepared.Obligation.ChangeSeq
	}

	path := r.evidenceValidityPath(value.EvidenceID)
	var resultErr error
	if found {
		resultErr = r.writer.Replace(path, value).Err
	} else {
		resultErr = r.writer.Create(path, value).Err
	}
	if resultErr != nil {
		if changed {
			r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		}
		return false, resultErr
	}
	if changed {
		r.commitObservationBestEffort(seq)
	}
	return changed, nil
}

func (r *Repository) LoadEvidenceValidity(_ context.Context, id string) (core.ValidityObservation, bool, error) {
	if !evidenceIDPattern.MatchString(id) {
		return core.ValidityObservation{}, false, fmt.Errorf("invalid_evidence_id")
	}
	r.evidenceValidityMu.Lock()
	defer r.evidenceValidityMu.Unlock()
	return r.loadEvidenceValidityUnlocked(id)
}

func (r *Repository) loadEvidenceValidityUnlocked(id string) (core.ValidityObservation, bool, error) {
	var value core.ValidityObservation
	path := r.evidenceValidityPath(id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, false, nil
	}
	if err != nil {
		return value, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > MaxEvidenceValidityBytes {
		return value, false, fmt.Errorf("unsafe evidence validity")
	}
	if err := readStrict(path, &value); err != nil {
		return value, false, err
	}
	if value.EvidenceID != id || value.Validate() != nil {
		return value, false, fmt.Errorf("invalid evidence validity")
	}
	return value, true, nil
}

func (r *Repository) evidenceValidityPath(id string) string {
	return filepath.Join(r.root, "evidence", "validity", id+".json")
}
