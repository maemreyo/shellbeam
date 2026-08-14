package store

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
)

const maxReproRecordBytes = 256 << 10

var storeReproIDPattern = regexp.MustCompile(`^repro_[0-9A-HJKMNP-TV-Z]{26}$`)

type reproCreateRecord struct {
	SchemaVersion      int          `json:"schema_version"`
	RequestFingerprint string       `json:"request_fingerprint"`
	Capsule            core.Capsule `json:"capsule"`
}

type reproEntry struct {
	record reproCreateRecord
	path   string
	size   int64
}

func (record reproCreateRecord) validate() error {
	if record.SchemaVersion != 1 || !validReproDigest(record.RequestFingerprint) {
		return fmt.Errorf("invalid repro create record")
	}
	return record.Capsule.Validate()
}

func (r *Repository) reproRoot() string {
	return filepath.Join(r.derivedRoot(), "repro")
}

func (r *Repository) reproCreateDir() string {
	return filepath.Join(r.reproRoot(), "creates")
}

func (r *Repository) reproCreatePath(createID string) string {
	return filepath.Join(r.reproCreateDir(), createID+".json")
}

func validReproDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validReproLookupID(value string) bool {
	return storeReproIDPattern.MatchString(value)
}

func (r *Repository) readReproCreateRecordUnlocked(createID string) (reproCreateRecord, error) {
	if _, err := operation.ParseID(createID); err != nil {
		return reproCreateRecord{}, fmt.Errorf("invalid repro create id")
	}
	var record reproCreateRecord
	if err := readPrivateJSON(r.reproCreatePath(createID), maxReproRecordBytes, &record); err != nil {
		return record, err
	}
	if err := record.validate(); err != nil {
		return reproCreateRecord{}, err
	}
	if record.Capsule.CreateID != createID {
		return reproCreateRecord{}, fmt.Errorf("repro create filename mismatch")
	}
	return record, nil
}

func (r *Repository) reproEntriesLocked() ([]reproEntry, error) {
	entries, err := os.ReadDir(r.reproCreateDir())
	if err != nil {
		return nil, err
	}
	if len(entries) > r.limits.MaxReproCapsules+16 {
		return nil, fmt.Errorf("repro create scan limit exceeded")
	}
	out := make([]reproEntry, 0, min(len(entries), r.limits.MaxReproCapsules))
	seenRepro := map[string]string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unsafe repro create entry")
		}
		createID := strings.TrimSuffix(entry.Name(), ".json")
		if _, err := operation.ParseID(createID); err != nil {
			return nil, fmt.Errorf("invalid repro create filename")
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > maxReproRecordBytes {
			return nil, fmt.Errorf("unsafe repro create entry")
		}
		record, err := r.readReproCreateRecordUnlocked(createID)
		if err != nil {
			return nil, err
		}
		if prior, ok := seenRepro[record.Capsule.ReproID]; ok && prior != createID {
			return nil, fmt.Errorf("repro_id_conflict")
		}
		seenRepro[record.Capsule.ReproID] = createID
		out = append(out, reproEntry{record: record, path: r.reproCreatePath(createID), size: info.Size()})
	}
	sortReproOldest(out)
	return out, nil
}

func sortReproOldest(entries []reproEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].record.Capsule, entries[j].record.Capsule
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreateID < b.CreateID
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
}

func planReproEvictions(entries []reproEntry, incomingSize int64, limits Limits, now time.Time) ([]reproEntry, error) {
	if incomingSize < 1 || incomingSize > limits.MaxReproBytes || incomingSize > maxReproRecordBytes {
		return nil, fmt.Errorf("repro_budget_exceeded")
	}
	removed := map[string]bool{}
	active := func() []reproEntry {
		out := make([]reproEntry, 0, len(entries))
		for _, entry := range entries {
			if !removed[entry.record.Capsule.CreateID] {
				out = append(out, entry)
			}
		}
		return out
	}
	if limits.MaxReproAge > 0 {
		for _, entry := range entries {
			if now.Sub(entry.record.Capsule.CreatedAt) > limits.MaxReproAge {
				removed[entry.record.Capsule.CreateID] = true
			}
		}
	}
	current := active()
	for len(current)+1 > limits.MaxReproCapsules {
		removed[current[0].record.Capsule.CreateID] = true
		current = active()
	}
	var total int64
	for _, entry := range current {
		total += entry.size
	}
	for total+incomingSize > limits.MaxReproBytes && len(current) > 0 {
		removed[current[0].record.Capsule.CreateID] = true
		total -= current[0].size
		current = active()
	}
	if total+incomingSize > limits.MaxReproBytes {
		return nil, fmt.Errorf("repro_budget_exceeded")
	}
	out := make([]reproEntry, 0, len(removed))
	for _, entry := range entries {
		if removed[entry.record.Capsule.CreateID] {
			out = append(out, entry)
		}
	}
	sortReproOldest(out)
	return out, nil
}
