package localfs

import (
	"fmt"
	"sort"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

type entryKind string

const (
	entryFile      entryKind = "file"
	entryDirectory entryKind = "directory_marker"
	entrySymlink   entryKind = "symlink"
	entryAbsent    entryKind = "absent"
)

type selectedEntry struct {
	Path string
	Kind entryKind
}

type selectedCapture struct {
	Entries    []selectedEntry
	Excluded   []core.PathSummary
	TotalBytes int64
}

type privateManifest struct {
	SchemaVersion    int                `json:"schema_version"`
	CheckpointID     string             `json:"checkpoint_id"`
	WorkspaceID      string             `json:"workspace_id"`
	RepositoryID     string             `json:"repository_id"`
	ActivityID       string             `json:"activity_id,omitempty"`
	Root             string             `json:"root"`
	SourceGeneration string             `json:"source_generation"`
	Paths            []string           `json:"paths"`
	Complete         bool               `json:"complete"`
	Entries          []privateEntry     `json:"entries"`
	Excluded         []core.PathSummary `json:"excluded,omitempty"`
	TotalBytes       int64              `json:"total_bytes"`
}

type privateEntry struct {
	Path            string    `json:"path"`
	Kind            entryKind `json:"kind"`
	OpaqueRef       string    `json:"opaque_ref"`
	Size            int64     `json:"size,omitempty"`
	Mode            uint32    `json:"mode,omitempty"`
	PrivateIdentity string    `json:"private_identity,omitempty"`
}

type privateSymlink struct {
	SchemaVersion int    `json:"schema_version"`
	Text          string `json:"text"`
}

type privateAbsent struct {
	SchemaVersion int `json:"schema_version"`
}

type privateComplete struct {
	SchemaVersion int `json:"schema_version"`
}

func canonicalSelectors(paths []string) ([]string, error) {
	if len(paths) < 1 || len(paths) > core.MaxCreateSelectors {
		return nil, fmt.Errorf("invalid selector count")
	}
	out := append([]string(nil), paths...)
	total := 0
	seen := make(map[string]struct{}, len(out))
	for _, selector := range out {
		if err := validateSelector(selector); err != nil {
			return nil, err
		}
		total += len(selector)
		if total > core.MaxTotalSelectorBytes {
			return nil, fmt.Errorf("selector bytes exceed limit")
		}
		if _, ok := seen[selector]; ok {
			return nil, fmt.Errorf("duplicate selector")
		}
		seen[selector] = struct{}{}
	}
	sort.Strings(out)
	return out, nil
}
