package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (r *Repository) inputTraceRoot() string { return filepath.Join(r.derivedRoot(), "input-trace") }
func (r *Repository) inputTraceRecordDir() string {
	return filepath.Join(r.inputTraceRoot(), "records")
}
func (r *Repository) inputTraceRecordPath(key string) string {
	return filepath.Join(r.inputTraceRecordDir(), key+".json")
}
func (r *Repository) initInputTraceStore() error {
	for _, dir := range []string{r.inputTraceRoot(), r.inputTraceRecordDir()} {
		if err := ensurePrivateDir(dir); err != nil {
			return fmt.Errorf("input trace store: %w", err)
		}
	}
	return nil
}

func (r *Repository) PutInputTraceRecord(ctx context.Context, record trace.Record) error {
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
	if len(encoded) > trace.MaxPublicRecordBytes {
		return fmt.Errorf("input_trace_budget_exceeded")
	}
	r.inputTraceMu.Lock()
	defer r.inputTraceMu.Unlock()
	path := r.inputTraceRecordPath(record.DerivationKey)
	var current trace.Record
	if err := readPrivateJSON(path, trace.MaxPublicRecordBytes, &current); err == nil {
		if reflect.DeepEqual(current, record) {
			return nil
		}
		return fmt.Errorf("input_trace_record_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	entries, err := r.inputTraceEntriesLocked()
	if err != nil {
		return err
	}
	var evict []inputTraceEntry
	if len(entries) >= trace.MaxRetainedTraceRecords {
		evict = entries[:len(entries)-trace.MaxRetainedTraceRecords+1]
	}
	r.observationVisibilityMu.Lock()
	defer r.observationVisibilityMu.Unlock()
	seq, prepared := r.prepareInputTraceObservation(ctx, record)
	if prepared.Err != nil {
		return prepared.Err
	}
	for _, entry := range evict {
		if err := os.Remove(entry.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.abortObservationBestEffort(seq, observationAbortWriteFailed)
			return err
		}
	}
	result := r.writer.Create(path, record)
	if result.Err == nil {
		r.commitObservationBestEffort(seq)
		return nil
	}
	if errors.Is(result.Err, os.ErrExist) {
		var existing trace.Record
		if readPrivateJSON(path, trace.MaxPublicRecordBytes, &existing) == nil && reflect.DeepEqual(existing, record) {
			r.abortObservationBestEffort(seq, observationAbortConflict)
			return nil
		}
		r.abortObservationBestEffort(seq, observationAbortConflict)
		return fmt.Errorf("input_trace_record_conflict")
	}
	var existing trace.Record
	if result.Durability == app.AmbiguousChange && readPrivateJSON(path, trace.MaxPublicRecordBytes, &existing) == nil && reflect.DeepEqual(existing, record) {
		r.commitObservationBestEffort(seq)
		return nil
	}
	r.abortObservationBestEffort(seq, observationAbortWriteFailed)
	return result.Err
}

type inputTraceEntry struct {
	record trace.Record
	path   string
}

func (r *Repository) inputTraceEntriesLocked() ([]inputTraceEntry, error) {
	entries, err := os.ReadDir(r.inputTraceRecordDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > trace.MaxRetainedTraceRecords+16 {
		return nil, fmt.Errorf("input trace record scan limit exceeded")
	}
	out := make([]inputTraceEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe input trace record entry")
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !validTelemetryKey(key) {
			return nil, fmt.Errorf("invalid input trace filename")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > trace.MaxPublicRecordBytes {
			return nil, fmt.Errorf("unsafe input trace record entry")
		}
		var record trace.Record
		if err := readPrivateJSON(r.inputTraceRecordPath(key), trace.MaxPublicRecordBytes, &record); err != nil {
			return nil, err
		}
		if record.DerivationKey != key || record.Validate() != nil {
			return nil, fmt.Errorf("invalid input trace durable record")
		}
		out = append(out, inputTraceEntry{record: record, path: r.inputTraceRecordPath(key)})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := out[i].record.CaptureEnd, out[j].record.CaptureEnd
		if ai.Equal(aj) {
			return out[i].record.DerivationKey < out[j].record.DerivationKey
		}
		return ai.Before(aj)
	})
	return out, nil
}
func (r *Repository) GetInputTraceRecord(ctx context.Context, key string) (trace.Record, error) {
	if err := ctx.Err(); err != nil {
		return trace.Record{}, err
	}
	if !validTelemetryKey(key) {
		return trace.Record{}, fmt.Errorf("invalid input trace key")
	}
	r.inputTraceMu.Lock()
	defer r.inputTraceMu.Unlock()
	var record trace.Record
	if err := readPrivateJSON(r.inputTraceRecordPath(key), trace.MaxPublicRecordBytes, &record); err != nil {
		return record, err
	}
	if record.DerivationKey != key {
		return trace.Record{}, fmt.Errorf("input trace filename mismatch")
	}
	return record, record.Validate()
}
func (r *Repository) LoadInputTraceByOperation(ctx context.Context, operationID string) (trace.Record, bool, error) {
	if _, err := operation.ParseID(operationID); err != nil {
		return trace.Record{}, false, err
	}
	r.inputTraceMu.Lock()
	defer r.inputTraceMu.Unlock()
	entries, err := r.inputTraceEntriesLocked()
	if err != nil {
		return trace.Record{}, false, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].record.OperationID == operationID {
			return entries[i].record, true, nil
		}
	}
	return trace.Record{}, false, nil
}
func (r *Repository) InspectInputTrace(ctx context.Context, operationID string) (trace.Inspection, error) {
	record, ok, err := r.LoadInputTraceByOperation(ctx, operationID)
	if err != nil {
		return trace.Inspection{}, err
	}
	if !ok {
		return trace.Inspection{SchemaVersion: trace.SchemaVersion, Status: "not_found", OperationID: operationID}, nil
	}
	copy := record
	return trace.Inspection{SchemaVersion: trace.SchemaVersion, Status: "available", OperationID: operationID, TraceID: record.TraceID, Record: &copy}, nil
}
func (r *Repository) prepareInputTraceObservation(ctx context.Context, record trace.Record) (observation.ChangeSeq, app.StoreResult) {
	kind := observation.EventInputTraceRecorded
	if record.Truncated {
		kind = observation.EventInputTraceTruncated
	}
	request := observation.PrepareRequest{Kind: kind, Correlation: observation.Correlation{OperationID: record.OperationID, SessionID: record.SessionID}, SubjectRef: "input-trace:" + record.DerivationKey, Summary: fmt.Sprintf("input trace %s resources=%d", record.Outcome, record.Summary.ResourcesReturned)}
	return r.prepareExecutionObservation(ctx, request)
}
func (r *Repository) inputTraceSubjectPresent(ctx context.Context, subject string) (bool, error) {
	const prefix = "input-trace:"
	if !strings.HasPrefix(subject, prefix) {
		return false, fmt.Errorf("invalid input trace observation subject")
	}
	_, err := r.GetInputTraceRecord(ctx, strings.TrimPrefix(subject, prefix))
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
