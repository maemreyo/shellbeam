package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const MaxEvidenceRecordBytes = 256 << 10

var evidenceIDPattern = regexp.MustCompile(`^ev_[0-9a-f]{64}$`)

type evidenceIndexRef struct {
	SchemaVersion int       `json:"schema_version"`
	EvidenceID    string    `json:"evidence_id"`
	OperationID   string    `json:"operation_id"`
	WorkspaceID   string    `json:"workspace_id,omitempty"`
	CompletedAt   time.Time `json:"completed_at"`
}

func (r *Repository) initEvidenceStore() error {
	for _, path := range []string{
		filepath.Join(r.root, "evidence"),
		filepath.Join(r.root, "evidence", "records"),
		filepath.Join(r.root, "evidence", "by-operation"),
		filepath.Join(r.root, "evidence", "by-workspace"),
		filepath.Join(r.root, "evidence", "candidates"),
	} {
		if err := ensurePrivateDir(path); err != nil {
			return fmt.Errorf("evidence store: %w", err)
		}
	}
	return nil
}

type evidenceCandidateRef struct {
	SchemaVersion int    `json:"schema_version"`
	OperationID   string `json:"operation_id"`
}

const maxEvidenceCandidateScan = 4096

func (r *Repository) EnsureEvidenceCandidate(_ context.Context, reservation operation.Reservation) error {
	if !reservation.EvidenceEligible() {
		return nil
	}
	if _, err := operation.ParseID(string(reservation.OperationID)); err != nil {
		return err
	}
	want := evidenceCandidateRef{SchemaVersion: 1, OperationID: string(reservation.OperationID)}
	path := r.evidenceCandidatePath(reservation.OperationID)
	var current evidenceCandidateRef
	if err := readStrict(path, &current); err == nil {
		if current != want {
			return fmt.Errorf("evidence_candidate_conflict")
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	result := r.writer.Create(path, want)
	if result.Err == nil {
		return nil
	}
	if errors.Is(result.Err, os.ErrExist) && readStrict(path, &current) == nil && current == want {
		return nil
	}
	return result.Err
}

func (r *Repository) ListEvidenceCandidates(ctx context.Context, limit int) ([]operation.ID, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > maxEvidenceCandidateScan {
		return nil, fmt.Errorf("invalid_evidence_candidate_limit")
	}
	dir := filepath.Join(r.root, "evidence", "candidates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxEvidenceCandidateScan {
		return nil, fmt.Errorf("evidence_candidate_scan_limit_exceeded")
	}
	ids := make([]operation.ID, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe evidence candidate entry")
		}
		raw := strings.TrimSuffix(entry.Name(), ".json")
		id, err := operation.ParseID(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid evidence candidate filename")
		}
		var ref evidenceCandidateRef
		if err := readStrict(filepath.Join(dir, entry.Name()), &ref); err != nil {
			return nil, err
		}
		if ref.SchemaVersion != 1 || ref.OperationID != raw {
			return nil, fmt.Errorf("invalid evidence candidate")
		}
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b operation.ID) int { return strings.Compare(string(a), string(b)) })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func (r *Repository) ClearEvidenceCandidate(_ context.Context, id operation.ID) error {
	if _, err := operation.ParseID(string(id)); err != nil {
		return err
	}
	path := r.evidenceCandidatePath(id)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return r.writer.syncParent("evidence_candidate_remove", filepath.Dir(path)).Err
}

func (r *Repository) evidenceCandidatePath(id operation.ID) string {
	return filepath.Join(r.root, "evidence", "candidates", string(id)+".json")
}

func (r *Repository) PutEvidenceRecord(ctx context.Context, record core.Record) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := record.Validate(); err != nil {
		return false, err
	}
	if _, err := operation.ParseID(record.OperationID); err != nil {
		return false, err
	}
	if _, err := operation.ParseSessionID(record.SessionID); err != nil {
		return false, err
	}
	if record.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(record.WorkspaceID); err != nil {
			return false, err
		}
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return false, err
	}
	if len(encoded) > MaxEvidenceRecordBytes {
		return false, fmt.Errorf("evidence_record_limit_exceeded")
	}

	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	path := r.evidenceRecordPath(record.EvidenceID)
	if existing, err := r.loadEvidenceRecordUnlocked(record.EvidenceID); err == nil {
		if !reflect.DeepEqual(existing, record) {
			return false, fmt.Errorf("evidence_record_conflict")
		}
		return false, r.ensureEvidenceIndexes(record)
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}

	seqs, err := r.prepareEvidenceObservations(ctx, record)
	if err != nil {
		return false, err
	}
	result := r.writer.Create(path, record)
	if result.Err != nil {
		if errors.Is(result.Err, os.ErrExist) {
			existing, readErr := r.loadEvidenceRecordUnlocked(record.EvidenceID)
			if readErr == nil && reflect.DeepEqual(existing, record) {
				r.abortEvidenceObservations(seqs, observationAbortConflict)
				return false, r.ensureEvidenceIndexes(record)
			}
		}
		if result.Durability == app.NoDurableChange || !r.evidenceRecordFileMatches(record) {
			r.abortEvidenceObservations(seqs, observationAbortWriteFailed)
		}
		return false, result.Err
	}
	if err := r.ensureEvidenceIndexes(record); err != nil {
		return false, err
	}
	for _, seq := range seqs {
		r.commitObservationBestEffort(seq)
	}
	return true, nil
}

func (r *Repository) LoadEvidenceRecord(_ context.Context, id string) (core.Record, error) {
	if !evidenceIDPattern.MatchString(id) {
		return core.Record{}, fmt.Errorf("invalid_evidence_id")
	}
	r.evidenceMu.Lock()
	defer r.evidenceMu.Unlock()
	return r.loadEvidenceRecordUnlocked(id)
}

func (r *Repository) loadEvidenceRecordUnlocked(id string) (core.Record, error) {
	var record core.Record
	path := r.evidenceRecordPath(id)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > MaxEvidenceRecordBytes {
		return record, fmt.Errorf("unsafe evidence record")
	}
	if err := readStrict(path, &record); err != nil {
		return record, err
	}
	if record.EvidenceID != id {
		return record, fmt.Errorf("evidence identity mismatch")
	}
	if err := record.Validate(); err != nil {
		return record, err
	}
	return record, nil
}

func (r *Repository) FindEvidenceByOperation(ctx context.Context, id operation.ID) (core.Record, bool, error) {
	if _, err := operation.ParseID(string(id)); err != nil {
		return core.Record{}, false, err
	}
	var ref evidenceIndexRef
	err := readStrict(filepath.Join(r.root, "evidence", "by-operation", string(id)+".json"), &ref)
	if errors.Is(err, ErrNotFound) {
		return core.Record{}, false, nil
	}
	if err != nil {
		return core.Record{}, false, err
	}
	if err := validateEvidenceIndexRef(ref); err != nil || ref.OperationID != string(id) {
		return core.Record{}, false, fmt.Errorf("invalid evidence operation index")
	}
	record, err := r.LoadEvidenceRecord(ctx, ref.EvidenceID)
	if err != nil {
		return core.Record{}, false, err
	}
	if record.OperationID != string(id) {
		return core.Record{}, false, fmt.Errorf("evidence operation index mismatch")
	}
	return record, true, nil
}

func (r *Repository) ensureEvidenceIndexes(record core.Record) error {
	ref := evidenceIndexRef{SchemaVersion: 1, EvidenceID: record.EvidenceID, OperationID: record.OperationID, WorkspaceID: record.WorkspaceID, CompletedAt: record.CompletedAt}
	if err := r.ensureEvidenceIndex(filepath.Join(r.root, "evidence", "by-operation", record.OperationID+".json"), ref); err != nil {
		return err
	}
	if record.WorkspaceID == "" {
		return nil
	}
	dir := filepath.Join(r.root, "evidence", "by-workspace", record.WorkspaceID)
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	return r.ensureEvidenceIndex(filepath.Join(dir, record.EvidenceID+".json"), ref)
}

func (r *Repository) ensureEvidenceIndex(path string, want evidenceIndexRef) error {
	var current evidenceIndexRef
	if err := readStrict(path, &current); err == nil {
		if !reflect.DeepEqual(current, want) {
			return fmt.Errorf("evidence_index_conflict")
		}
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	result := r.writer.Create(path, want)
	if result.Err == nil {
		return nil
	}
	if errors.Is(result.Err, os.ErrExist) && readStrict(path, &current) == nil && reflect.DeepEqual(current, want) {
		return nil
	}
	return result.Err
}

func validateEvidenceIndexRef(ref evidenceIndexRef) error {
	if ref.SchemaVersion != 1 || !evidenceIDPattern.MatchString(ref.EvidenceID) || ref.CompletedAt.IsZero() {
		return fmt.Errorf("invalid evidence index")
	}
	if _, err := operation.ParseID(ref.OperationID); err != nil {
		return err
	}
	if ref.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(ref.WorkspaceID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) prepareEvidenceObservations(ctx context.Context, record core.Record) ([]observation.ChangeSeq, error) {
	correlation := observation.Correlation{RepositoryID: record.Source.RepositoryID, WorkspaceID: record.WorkspaceID, ActivityID: record.ActivityID, OperationID: record.OperationID, SessionID: record.SessionID}
	if strings.HasPrefix(record.Source.PostGeneration, "gen_") && len(record.Source.PostGeneration) == 68 {
		correlation.WorkspaceGeneration = record.Source.PostGeneration
	}
	seqs := make([]observation.ChangeSeq, 0, len(record.Artifacts)+1)
	for index, artifact := range record.Artifacts {
		prepared, result := r.PrepareObservation(ctx, observation.PrepareRequest{Kind: observation.EventArtifactObserved, Correlation: correlation, SubjectRef: fmt.Sprintf("evidence:%s:artifact:%d", record.EvidenceID, index), Summary: "artifact observed: " + string(artifact.Status)})
		if result.Err != nil {
			r.abortEvidenceObservations(seqs, observationAbortWriteFailed)
			return nil, result.Err
		}
		seqs = append(seqs, prepared.Obligation.ChangeSeq)
	}
	prepared, result := r.PrepareObservation(ctx, observation.PrepareRequest{Kind: observation.EventEvidenceRecorded, Correlation: correlation, SubjectRef: "evidence:" + record.EvidenceID, Summary: "evidence recorded: " + string(record.Result)})
	if result.Err != nil {
		r.abortEvidenceObservations(seqs, observationAbortWriteFailed)
		return nil, result.Err
	}
	return append(seqs, prepared.Obligation.ChangeSeq), nil
}

func (r *Repository) abortEvidenceObservations(seqs []observation.ChangeSeq, reason string) {
	for _, seq := range seqs {
		r.abortObservationBestEffort(seq, reason)
	}
}
func (r *Repository) evidenceRecordPath(id string) string {
	return filepath.Join(r.root, "evidence", "records", id+".json")
}
func (r *Repository) evidenceRecordFileMatches(want core.Record) bool {
	got, err := r.loadEvidenceRecordUnlocked(want.EvidenceID)
	return err == nil && reflect.DeepEqual(got, want)
}

func parseEvidenceArtifactSubject(subject string) (string, int, error) {
	parts := strings.Split(subject, ":")
	if len(parts) != 4 || parts[0] != "evidence" || parts[2] != "artifact" || !evidenceIDPattern.MatchString(parts[1]) {
		return "", 0, fmt.Errorf("invalid artifact evidence subject")
	}
	index, err := strconv.Atoi(parts[3])
	if err != nil || index < 0 || index >= core.MaxExpectedOutputs {
		return "", 0, fmt.Errorf("invalid artifact evidence subject")
	}
	return parts[1], index, nil
}
