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
	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

func (r *Repository) initTelemetryStore() error {
	for _, dir := range []string{r.derivedRoot(), r.telemetryRoot(), r.telemetrySampleDir()} {
		if err := ensurePrivateDir(dir); err != nil {
			return fmt.Errorf("telemetry store: %w", err)
		}
	}
	return nil
}

func (r *Repository) PutPerformanceRecord(ctx context.Context, record core.PerformanceRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	incomingSize := int64(len(encoded) + 1)

	r.telemetryMu.Lock()
	defer r.telemetryMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	path := r.telemetrySamplePath(record.DerivationKey)
	var current core.PerformanceRecord
	if err := readPrivateJSON(path, maxTelemetryRecordBytes, &current); err == nil {
		if reflect.DeepEqual(current, record) {
			return nil
		}
		return fmt.Errorf("telemetry_record_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	entries, err := r.telemetryEntriesLocked()
	if err != nil {
		return err
	}
	evictions, err := planTelemetryEvictions(entries, record, incomingSize, r.limits, r.now().UTC())
	if err != nil {
		return err
	}
	seq, prepared := r.prepareTelemetryObservation(ctx, record)
	if prepared.Err != nil {
		return prepared.Err
	}
	if err := r.removeTelemetryEntries(evictions); err != nil {
		r.abortObservationBestEffort(seq, observationAbortWriteFailed)
		return err
	}

	result := r.writer.Create(path, record)
	if result.Err == nil {
		r.commitObservationBestEffort(seq)
		return nil
	}
	if errors.Is(result.Err, os.ErrExist) {
		var existing core.PerformanceRecord
		if err := readPrivateJSON(path, maxTelemetryRecordBytes, &existing); err == nil && reflect.DeepEqual(existing, record) {
			r.abortObservationBestEffort(seq, observationAbortConflict)
			return nil
		}
		r.abortObservationBestEffort(seq, observationAbortConflict)
		return fmt.Errorf("telemetry_record_conflict")
	}
	var existing core.PerformanceRecord
	if result.Durability == app.AmbiguousChange && readPrivateJSON(path, maxTelemetryRecordBytes, &existing) == nil && reflect.DeepEqual(existing, record) {
		r.commitObservationBestEffort(seq)
		return nil
	}
	r.abortObservationBestEffort(seq, observationAbortWriteFailed)
	return result.Err
}

func (r *Repository) GetPerformanceRecord(ctx context.Context, derivationKey string) (core.PerformanceRecord, error) {
	if err := ctx.Err(); err != nil {
		return core.PerformanceRecord{}, err
	}
	if !validTelemetryKey(derivationKey) {
		return core.PerformanceRecord{}, fmt.Errorf("invalid telemetry derivation key")
	}
	r.telemetryMu.Lock()
	defer r.telemetryMu.Unlock()
	var record core.PerformanceRecord
	if err := readPrivateJSON(r.telemetrySamplePath(derivationKey), maxTelemetryRecordBytes, &record); err != nil {
		return record, err
	}
	if record.DerivationKey != derivationKey {
		return core.PerformanceRecord{}, fmt.Errorf("telemetry derivation filename mismatch")
	}
	return record, record.Validate()
}

func (r *Repository) FindPerformanceByOperation(ctx context.Context, operationID string) (core.PerformanceRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.PerformanceRecord{}, false, err
	}
	if _, err := operation.ParseID(operationID); err != nil {
		return core.PerformanceRecord{}, false, err
	}
	r.telemetryMu.Lock()
	defer r.telemetryMu.Unlock()
	entries, err := r.telemetryEntriesLocked()
	if err != nil {
		return core.PerformanceRecord{}, false, err
	}
	var selected *core.PerformanceRecord
	for _, entry := range entries {
		if entry.record.OperationID != operationID {
			continue
		}
		candidate := entry.record
		if selected == nil || candidate.CapturedAt.After(selected.CapturedAt) || candidate.CapturedAt.Equal(selected.CapturedAt) && candidate.DerivationKey > selected.DerivationKey {
			selected = &candidate
		}
	}
	if selected == nil {
		return core.PerformanceRecord{}, false, nil
	}
	return *selected, true, nil
}

func (r *Repository) ListCompatiblePerformance(ctx context.Context, compatibilityKey string, limit int) ([]core.PerformanceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validTelemetryKey(compatibilityKey) || limit < 1 {
		return nil, fmt.Errorf("invalid telemetry history bounds")
	}
	limit = min(limit, r.limits.MaxTelemetrySamples)
	r.telemetryMu.Lock()
	defer r.telemetryMu.Unlock()
	entries, err := r.telemetryEntriesLocked()
	if err != nil {
		return nil, err
	}
	out := make([]core.PerformanceRecord, 0, min(limit, len(entries)))
	for index := len(entries) - 1; index >= 0 && len(out) < limit; index-- {
		if entries[index].compatibilityKey == compatibilityKey {
			out = append(out, entries[index].record)
		}
	}
	return out, nil
}

func (r *Repository) CountCompatiblePerformance(ctx context.Context, compatibilityKey string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !validTelemetryKey(compatibilityKey) {
		return 0, fmt.Errorf("invalid telemetry compatibility key")
	}
	r.telemetryMu.Lock()
	defer r.telemetryMu.Unlock()
	entries, err := r.telemetryEntriesLocked()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.compatibilityKey == compatibilityKey {
			count++
		}
	}
	return count, nil
}

func (r *Repository) prepareTelemetryObservation(ctx context.Context, record core.PerformanceRecord) (observation.ChangeSeq, app.StoreResult) {
	request := observation.PrepareRequest{
		Kind: observation.EventTelemetryChanged,
		Correlation: observation.Correlation{
			RepositoryID: record.RepositoryID, WorkspaceID: record.WorkspaceID, ActivityID: record.ActivityID,
			OperationID: record.OperationID, SessionID: record.SessionID,
		},
		SubjectRef: "telemetry:" + record.DerivationKey,
		Summary:    "execution telemetry changed",
	}
	return r.prepareExecutionObservation(ctx, request)
}

func (r *Repository) removeTelemetryEntries(entries []telemetryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if result := r.writer.syncParent("telemetry_remove", r.telemetrySampleDir()); result.Err != nil {
		return result.Err
	}
	return nil
}

func (r *Repository) telemetrySubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix = "telemetry:"
	if !strings.HasPrefix(subject, prefix) {
		return false, fmt.Errorf("invalid telemetry observation subject")
	}
	key := strings.TrimPrefix(subject, prefix)
	if !validTelemetryKey(key) {
		return false, fmt.Errorf("invalid telemetry observation subject")
	}
	_, err := r.GetPerformanceRecord(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
