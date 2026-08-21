package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

const (
	failureExcerptRetentionSchemaVersion = 1
	maxFailureExcerptMarkersPerSession   = 65536
)

type failureExcerptRetentionMarker struct {
	SchemaVersion int    `json:"schema_version"`
	SessionID     string `json:"session_id"`
	DerivationKey string `json:"derivation_key"`
	RecordIndex   int    `json:"record_index"`
}

func (m failureExcerptRetentionMarker) Validate() error {
	if m.SchemaVersion != failureExcerptRetentionSchemaVersion || !validStructuredKey(m.DerivationKey) || m.RecordIndex < 0 || m.RecordIndex >= maxFailureExcerptMarkersPerSession {
		return fmt.Errorf("invalid_failure_excerpt_retention_marker")
	}
	if _, err := operation.ParseSessionID(m.SessionID); err != nil {
		return fmt.Errorf("invalid_failure_excerpt_retention_marker")
	}
	return nil
}

func (r *Repository) ensureFailureExcerptRetentionMarkersUnlocked(ctx context.Context, key string, records []core.Record) error {
	validatedSessions := make(map[string]struct{})
	for i, record := range records {
		if record.SchemaVersion != core.RecordSchemaVersionV3 || record.TestCase == nil || record.TestCase.FailureExcerpt == nil {
			continue
		}
		sessionID := record.SourceRef.SessionID()
		if _, ok := validatedSessions[sessionID]; !ok {
			if _, err := r.LoadSession(ctx, operation.SessionID(sessionID)); err != nil {
				return fmt.Errorf("failure_excerpt_source_session_unavailable: %w", err)
			}
			validatedSessions[sessionID] = struct{}{}
		}
		marker := failureExcerptRetentionMarker{
			SchemaVersion: failureExcerptRetentionSchemaVersion,
			SessionID:     record.SourceRef.SessionID(),
			DerivationKey: key,
			RecordIndex:   i,
		}
		if err := marker.Validate(); err != nil {
			return err
		}
		if err := r.ensureFailureExcerptMarker(marker); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ensureFailureExcerptMarker(marker failureExcerptRetentionMarker) error {
	sessionDir, err := ensurePrivateChildDir(r.failureExcerptRetentionRoot(), marker.SessionID)
	if err != nil {
		return err
	}
	derivationDir, err := ensurePrivateChildDir(sessionDir, marker.DerivationKey)
	if err != nil {
		return err
	}
	path := filepath.Join(derivationDir, strconv.Itoa(marker.RecordIndex)+".json")
	var current failureExcerptRetentionMarker
	if err := readPrivateJSON(path, maxStructuredMetadataBytes, &current); err == nil {
		if current.Validate() == nil && reflect.DeepEqual(current, marker) {
			return nil
		}
		return fmt.Errorf("failure_excerpt_retention_marker_conflict")
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if result := r.writer.Create(path, marker); result.Err != nil {
		return result.Err
	}
	return nil
}

func ensurePrivateChildDir(parent, name string) (string, error) {
	path := filepath.Join(parent, name)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return "", err
		}
		if err := syncPrivateDirectory(parent); err != nil {
			return "", err
		}
		return path, nil
	} else if err != nil {
		return "", err
	}
	if err := ensurePrivateDir(path); err != nil {
		return "", err
	}
	return path, nil
}

func (r *Repository) stripFailureExcerptsForSession(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(sessionID); err != nil {
		return err
	}
	r.structuredMu.Lock()
	defer r.structuredMu.Unlock()
	return r.stripFailureExcerptsForSessionUnlocked(ctx, sessionID)
}

func (r *Repository) stripFailureExcerptsForSessionUnlocked(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sessionDir := filepath.Join(r.failureExcerptRetentionRoot(), sessionID)
	derivations, err := os.ReadDir(sessionDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	totalMarkers := 0
	for _, entry := range derivations {
		if !entry.IsDir() || !validStructuredKey(entry.Name()) {
			return fmt.Errorf("invalid_failure_excerpt_retention_entry")
		}
		derivationDir := filepath.Join(sessionDir, entry.Name())
		markerEntries, err := os.ReadDir(derivationDir)
		if err != nil {
			return err
		}
		totalMarkers += len(markerEntries)
		if totalMarkers > maxFailureExcerptMarkersPerSession {
			return fmt.Errorf("failure_excerpt_retention_scan_limit_exceeded")
		}
		markers := make([]failureExcerptRetentionMarker, 0, len(markerEntries))
		for _, markerEntry := range markerEntries {
			if markerEntry.IsDir() || !strings.HasSuffix(markerEntry.Name(), ".json") {
				return fmt.Errorf("invalid_failure_excerpt_retention_entry")
			}
			var marker failureExcerptRetentionMarker
			if err := readPrivateJSON(filepath.Join(derivationDir, markerEntry.Name()), maxStructuredMetadataBytes, &marker); err != nil || marker.Validate() != nil || marker.SessionID != sessionID || marker.DerivationKey != entry.Name() {
				return fmt.Errorf("invalid_failure_excerpt_retention_marker")
			}
			markers = append(markers, marker)
		}
		if err := r.stripFailureExcerptMarkersUnlocked(sessionID, entry.Name(), markers); err != nil {
			return err
		}
		for _, markerEntry := range markerEntries {
			if err := os.Remove(filepath.Join(derivationDir, markerEntry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := os.Remove(derivationDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := syncPrivateDirectory(sessionDir); err != nil {
			return err
		}
	}
	if err := os.Remove(sessionDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncPrivateDirectory(r.failureExcerptRetentionRoot())
}

func (r *Repository) stripFailureExcerptMarkersUnlocked(sessionID, key string, markers []failureExcerptRetentionMarker) error {
	derivation, err := r.readDerivationUnlocked(key)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	set, err := readStructuredRecordSet(r.recordPath(key), derivation)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	changed := false
	for _, marker := range markers {
		if marker.RecordIndex >= len(set.Records) {
			return fmt.Errorf("failure_excerpt_retention_index_out_of_range")
		}
		record := &set.Records[marker.RecordIndex]
		if record.SourceRef.SessionID() != sessionID {
			return fmt.Errorf("failure_excerpt_retention_source_mismatch")
		}
		if record.TestCase == nil {
			return fmt.Errorf("failure_excerpt_retention_record_mismatch")
		}
		if record.TestCase.FailureExcerpt == nil {
			continue
		}
		testCase := *record.TestCase
		testCase.FailureExcerpt = nil
		record.TestCase = &testCase
		record.SchemaVersion = core.SchemaVersion
		changed = true
	}
	if !changed {
		return nil
	}
	set.SchemaVersion = core.SchemaVersion
	for _, record := range set.Records {
		if record.SchemaVersion == core.RecordSchemaVersionV3 {
			set.SchemaVersion = core.RecordSchemaVersionV3
			break
		}
	}
	if err := validateStructuredRecordSet(set, derivation); err != nil {
		return err
	}
	if result := r.writer.Replace(r.recordPath(key), set); result.Err != nil {
		return result.Err
	}
	return nil
}
