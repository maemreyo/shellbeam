package store

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

const maxTelemetryRecordBytes = 64 << 10

type telemetryEntry struct {
	record           core.PerformanceRecord
	path             string
	size             int64
	compatibilityKey string
}

func (r *Repository) derivedRoot() string {
	return filepath.Join(r.root, "derived")
}

func (r *Repository) telemetryRoot() string {
	return filepath.Join(r.derivedRoot(), "telemetry")
}

func (r *Repository) telemetrySampleDir() string {
	return filepath.Join(r.telemetryRoot(), "samples")
}

func (r *Repository) telemetrySamplePath(derivationKey string) string {
	return filepath.Join(r.telemetrySampleDir(), derivationKey+".json")
}

func validTelemetryKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func (r *Repository) telemetryEntriesLocked() ([]telemetryEntry, error) {
	entries, err := os.ReadDir(r.telemetrySampleDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > r.limits.MaxTelemetrySamples+16 {
		return nil, fmt.Errorf("telemetry sample scan limit exceeded")
	}
	out := make([]telemetryEntry, 0, min(len(entries), r.limits.MaxTelemetrySamples))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe telemetry sample entry")
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		if !validTelemetryKey(key) {
			return nil, fmt.Errorf("invalid telemetry sample filename")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > maxTelemetryRecordBytes {
			return nil, fmt.Errorf("unsafe telemetry sample entry")
		}
		path := r.telemetrySamplePath(key)
		var record core.PerformanceRecord
		if err := readPrivateJSON(path, maxTelemetryRecordBytes, &record); err != nil {
			return nil, err
		}
		if record.DerivationKey != key {
			return nil, fmt.Errorf("telemetry derivation filename mismatch")
		}
		if err := record.Validate(); err != nil {
			return nil, err
		}
		compatibility, err := core.CompatibilityKey(record)
		if err != nil {
			return nil, err
		}
		out = append(out, telemetryEntry{record: record, path: path, size: info.Size(), compatibilityKey: compatibility})
	}
	sortTelemetryOldest(out)
	return out, nil
}

func sortTelemetryOldest(entries []telemetryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].record.CapturedAt.Equal(entries[j].record.CapturedAt) {
			return entries[i].record.DerivationKey < entries[j].record.DerivationKey
		}
		return entries[i].record.CapturedAt.Before(entries[j].record.CapturedAt)
	})
}

func planTelemetryEvictions(entries []telemetryEntry, incoming core.PerformanceRecord, incomingSize int64, limits Limits, now time.Time) ([]telemetryEntry, error) {
	if incomingSize < 1 || incomingSize > limits.MaxTelemetryBytes || incomingSize > maxTelemetryRecordBytes {
		return nil, fmt.Errorf("telemetry_budget_exceeded")
	}
	incomingKey, err := core.CompatibilityKey(incoming)
	if err != nil {
		return nil, err
	}
	removed := map[string]bool{}
	removeEntry := func(entry telemetryEntry) { removed[entry.record.DerivationKey] = true }
	active := func() []telemetryEntry {
		out := make([]telemetryEntry, 0, len(entries))
		for _, entry := range entries {
			if !removed[entry.record.DerivationKey] {
				out = append(out, entry)
			}
		}
		return out
	}

	if limits.MaxTelemetryAge > 0 {
		for _, entry := range entries {
			if age := now.Sub(entry.record.CapturedAt); age > limits.MaxTelemetryAge {
				removeEntry(entry)
			}
		}
	}

	matching := func(values []telemetryEntry, predicate func(telemetryEntry) bool) []telemetryEntry {
		out := make([]telemetryEntry, 0, len(values))
		for _, entry := range values {
			if predicate(entry) {
				out = append(out, entry)
			}
		}
		return out
	}

	current := active()
	sameKey := matching(current, func(entry telemetryEntry) bool { return entry.compatibilityKey == incomingKey })
	for len(sameKey)+1 > limits.MaxTelemetrySamplesPerKey {
		removeEntry(sameKey[0])
		sameKey = sameKey[1:]
	}

	if incoming.RepositoryID != "" {
		current = active()
		if !containsTelemetryKeyForRepo(current, incoming.RepositoryID, incomingKey) {
			for distinctTelemetryKeysForRepo(current, incoming.RepositoryID) >= limits.MaxTelemetryKeysPerRepository {
				key, ok := leastRecentlyObservedTelemetryKey(current, incoming.RepositoryID)
				if !ok {
					break
				}
				for _, entry := range current {
					if entry.compatibilityKey == key {
						removeEntry(entry)
					}
				}
				current = active()
			}
		}
	}

	current = active()
	if !containsTelemetryKey(current, incomingKey) {
		for distinctTelemetryKeys(current) >= limits.MaxTelemetryKeys {
			key, ok := leastRecentlyObservedTelemetryKey(current, "")
			if !ok {
				break
			}
			for _, entry := range current {
				if entry.compatibilityKey == key {
					removeEntry(entry)
				}
			}
			current = active()
		}
	}

	current = active()
	for len(current)+1 > limits.MaxTelemetrySamples {
		removeEntry(current[0])
		current = active()
	}

	current = active()
	var totalBytes int64
	for _, entry := range current {
		totalBytes += entry.size
	}
	for totalBytes+incomingSize > limits.MaxTelemetryBytes && len(current) > 0 {
		removeEntry(current[0])
		totalBytes -= current[0].size
		current = active()
	}
	if totalBytes+incomingSize > limits.MaxTelemetryBytes {
		return nil, fmt.Errorf("telemetry_budget_exceeded")
	}

	out := make([]telemetryEntry, 0, len(removed))
	for _, entry := range entries {
		if removed[entry.record.DerivationKey] {
			out = append(out, entry)
		}
	}
	sortTelemetryOldest(out)
	return out, nil
}

func containsTelemetryKey(entries []telemetryEntry, key string) bool {
	for _, entry := range entries {
		if entry.compatibilityKey == key {
			return true
		}
	}
	return false
}

func containsTelemetryKeyForRepo(entries []telemetryEntry, repositoryID, key string) bool {
	for _, entry := range entries {
		if entry.record.RepositoryID == repositoryID && entry.compatibilityKey == key {
			return true
		}
	}
	return false
}

func distinctTelemetryKeys(entries []telemetryEntry) int {
	keys := map[string]struct{}{}
	for _, entry := range entries {
		keys[entry.compatibilityKey] = struct{}{}
	}
	return len(keys)
}

func distinctTelemetryKeysForRepo(entries []telemetryEntry, repositoryID string) int {
	keys := map[string]struct{}{}
	for _, entry := range entries {
		if entry.record.RepositoryID == repositoryID {
			keys[entry.compatibilityKey] = struct{}{}
		}
	}
	return len(keys)
}

func leastRecentlyObservedTelemetryKey(entries []telemetryEntry, repositoryID string) (string, bool) {
	latest := map[string]time.Time{}
	for _, entry := range entries {
		if repositoryID != "" && entry.record.RepositoryID != repositoryID {
			continue
		}
		if current, ok := latest[entry.compatibilityKey]; !ok || entry.record.CapturedAt.After(current) {
			latest[entry.compatibilityKey] = entry.record.CapturedAt
		}
	}
	var selected string
	var selectedAt time.Time
	for key, capturedAt := range latest {
		if selected == "" || capturedAt.Before(selectedAt) || capturedAt.Equal(selectedAt) && key < selected {
			selected, selectedAt = key, capturedAt
		}
	}
	return selected, selected != ""
}
