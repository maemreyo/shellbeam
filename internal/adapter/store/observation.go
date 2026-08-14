package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

const (
	MaxObservationObligationBytes = 16 << 10
	MaxObservationListRecords     = 1024
	maxObservationScanRecords     = 65536
)

var observationAbortReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

type PreparedObservation struct {
	Obligation observation.ObservationObligation `json:"obligation"`
}

func (r *Repository) initObservationStore() error {
	root := filepath.Join(r.root, "observations")
	if err := ensurePrivateDir(root); err != nil {
		return fmt.Errorf("observation root: %w", err)
	}
	dir := filepath.Join(root, "obligations")
	if err := ensurePrivateDir(dir); err != nil {
		return fmt.Errorf("observation obligations: %w", err)
	}
	sequences, err := observationSequences(dir)
	if err != nil {
		return err
	}
	if len(sequences) > 0 {
		r.observationHighWatermark = uint64(sequences[len(sequences)-1])
	}
	return nil
}

func (r *Repository) PrepareObservation(ctx context.Context, request observation.PrepareRequest) (PreparedObservation, app.StoreResult) {
	if err := ctx.Err(); err != nil {
		return PreparedObservation{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if err := request.Validate(); err != nil {
		return PreparedObservation{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	r.observationMu.Lock()
	defer r.observationMu.Unlock()
	if r.observationHighWatermark == ^uint64(0) {
		return PreparedObservation{}, app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("observation_sequence_exhausted")}
	}
	seq := observation.ChangeSeq(r.observationHighWatermark + 1)
	obligation := observation.ObservationObligation{
		SchemaVersion: observation.SchemaVersion,
		ChangeSeq:     seq,
		Kind:          request.Kind,
		State:         observation.ObligationPrepared,
		PreparedAt:    time.Now().UTC(),
		Correlation:   request.Correlation,
		SubjectRef:    request.SubjectRef,
		Summary:       request.Summary,
	}
	if err := obligation.Validate(); err != nil {
		return PreparedObservation{}, app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	prepared := PreparedObservation{Obligation: obligation}
	result := r.writer.Create(r.observationPath(seq), obligation)
	if result.Err == nil {
		r.observationHighWatermark = uint64(seq)
		return prepared, result
	}
	if r.observationFileMatches(seq, obligation) {
		r.observationHighWatermark = uint64(seq)
		return prepared, result
	}
	return PreparedObservation{}, result
}

func (r *Repository) CommitObservation(ctx context.Context, seq observation.ChangeSeq) app.StoreResult {
	return r.transitionObservation(ctx, seq, observation.ObligationCommitted, "")
}

func (r *Repository) AbortObservation(ctx context.Context, seq observation.ChangeSeq, reason string) app.StoreResult {
	if !observationAbortReasonPattern.MatchString(reason) {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("invalid_observation_abort_reason")}
	}
	return r.transitionObservation(ctx, seq, observation.ObligationAborted, reason)
}

func (r *Repository) transitionObservation(ctx context.Context, seq observation.ChangeSeq, state observation.ObligationState, reason string) app.StoreResult {
	if err := ctx.Err(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if seq == 0 {
		return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("invalid_observation_sequence")}
	}
	r.observationMu.Lock()
	defer r.observationMu.Unlock()
	current, err := r.readObservation(seq)
	if err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	if current.State == state {
		if state != observation.ObligationAborted || current.AbortReason == reason {
			return app.StoreResult{Durability: app.DurableChange}
		}
		return observationStateConflict()
	}
	if current.State != observation.ObligationPrepared {
		return observationStateConflict()
	}
	current.State = state
	current.AbortReason = reason
	if err := current.Validate(); err != nil {
		return app.StoreResult{Durability: app.NoDurableChange, Err: err}
	}
	return r.writer.Replace(r.observationPath(seq), current)
}

func (r *Repository) ObservationHighWatermark(ctx context.Context) (observation.ChangeSeq, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.observationVisibilityMu.RLock()
	defer r.observationVisibilityMu.RUnlock()
	r.observationMu.Lock()
	defer r.observationMu.Unlock()
	return observation.ChangeSeq(r.observationHighWatermark), nil
}

func (r *Repository) ListObservationObligations(ctx context.Context, after observation.ChangeSeq, limit int) ([]observation.ObservationObligation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > MaxObservationListRecords {
		return nil, fmt.Errorf("invalid_observation_list_limit")
	}
	r.observationMu.Lock()
	defer r.observationMu.Unlock()
	sequences, err := observationSequences(r.observationDir())
	if err != nil {
		return nil, err
	}
	out := make([]observation.ObservationObligation, 0, min(limit, len(sequences)))
	for _, seq := range sequences {
		if seq <= after {
			continue
		}
		record, err := r.readObservation(seq)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *Repository) observationDir() string {
	return filepath.Join(r.root, "observations", "obligations")
}

func (r *Repository) observationPath(seq observation.ChangeSeq) string {
	return filepath.Join(r.observationDir(), fmt.Sprintf("%020d.json", uint64(seq)))
}

func (r *Repository) observationFileMatches(seq observation.ChangeSeq, want observation.ObservationObligation) bool {
	got, err := r.readObservation(seq)
	return err == nil && reflect.DeepEqual(got, want)
}

func (r *Repository) readObservation(seq observation.ChangeSeq) (observation.ObservationObligation, error) {
	var record observation.ObservationObligation
	path := r.observationPath(seq)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) {
		return record, fmt.Errorf("unsafe observation obligation")
	}
	if info.Size() < 1 || info.Size() > MaxObservationObligationBytes {
		return record, fmt.Errorf("observation obligation size invalid")
	}
	if err := readStrict(path, &record); err != nil {
		return record, err
	}
	if record.ChangeSeq != seq {
		return record, fmt.Errorf("observation sequence mismatch")
	}
	if err := record.Validate(); err != nil {
		return record, err
	}
	return record, nil
}

func observationSequences(dir string) ([]observation.ChangeSeq, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxObservationScanRecords {
		return nil, fmt.Errorf("observation obligation scan limit exceeded")
	}
	sequences := make([]observation.ChangeSeq, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("unsafe observation obligation entry")
		}
		seq, ok := parseObservationFilename(entry.Name())
		if !ok {
			return nil, fmt.Errorf("invalid observation obligation filename")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > MaxObservationObligationBytes {
			return nil, fmt.Errorf("unsafe observation obligation entry")
		}
		var record observation.ObservationObligation
		if err := readStrict(filepath.Join(dir, entry.Name()), &record); err != nil {
			return nil, err
		}
		if record.ChangeSeq != seq || record.Validate() != nil {
			return nil, fmt.Errorf("invalid observation obligation record")
		}
		sequences = append(sequences, seq)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	return sequences, nil
}

func parseObservationFilename(name string) (observation.ChangeSeq, bool) {
	if len(name) != len("00000000000000000000.json") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	raw := strings.TrimSuffix(name, ".json")
	if len(raw) != 20 {
		return 0, false
	}
	seq, err := strconv.ParseUint(raw, 10, 64)
	return observation.ChangeSeq(seq), err == nil && seq > 0
}

func observationStateConflict() app.StoreResult {
	return app.StoreResult{Durability: app.NoDurableChange, Err: fmt.Errorf("observation_state_conflict")}
}
