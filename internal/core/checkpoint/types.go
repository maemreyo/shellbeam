// Package checkpoint defines bounded public E26 safety-checkpoint contracts.
package checkpoint

import "time"

const (
	SchemaVersion = 1

	MaxCreateSelectors            = 32
	MaxSelectorBytes              = 1024
	MaxTotalSelectorBytes         = 8192
	MaxWalkEntries                = 8192
	MaxCapturedEntries            = 2048
	MaxRegularFileBytes     int64 = 8 << 20
	MaxCheckpointBytes      int64 = 64 << 20
	MaxRetainedCheckpoints        = 64
	MaxPrivateProviderBytes int64 = 1 << 30
	MaxRestorePaths               = 256
	MaxPublicEntryRefs            = 2048
	MaxPublicSummaries            = 64
	MaxOpaqueRefBytes             = 128
)

const MaxRetentionAge = 7 * 24 * time.Hour

type CaptureQuality string
type RetentionState string
type ConflictGuarantee string
type RestorePathOutcome string

const (
	CaptureComplete CaptureQuality = "complete"

	RetentionAvailable          RetentionState = "available"
	RetentionPartiallyCompacted RetentionState = "partially_compacted"
	RetentionExpired            RetentionState = "expired"

	ConflictBestEffort               ConflictGuarantee = "best_effort"
	ConflictAtomicConditionalReplace ConflictGuarantee = "atomic_conditional_replace"
	ConflictUnsupported              ConflictGuarantee = "unsupported"

	RestoreRestored    RestorePathOutcome = "restored"
	RestoreNoop        RestorePathOutcome = "noop"
	RestoreConflict    RestorePathOutcome = "conflict"
	RestoreUnsupported RestorePathOutcome = "unsupported"
	RestoreFailed      RestorePathOutcome = "failed"
)

type ProviderIdentity struct {
	ID      string `json:"provider_id"`
	Version int    `json:"provider_version"`
}

type ConflictDetection struct {
	RegularFile   ConflictGuarantee `json:"regular_file"`
	Symlink       ConflictGuarantee `json:"symlink"`
	AbsentToFile  ConflictGuarantee `json:"absent_to_file"`
	DirectoryTree ConflictGuarantee `json:"directory_tree"`
}

type CreateRequest struct {
	CreateID    string   `json:"checkpoint_create_id"`
	WorkspaceID string   `json:"workspace_id"`
	ActivityID  string   `json:"activity_id,omitempty"`
	Paths       []string `json:"paths"`
}

type RestoreRequest struct {
	RestoreID    string   `json:"restore_id"`
	CheckpointID string   `json:"checkpoint_id"`
	Paths        []string `json:"paths"`
}

type PathSummary struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Checkpoint struct {
	SchemaVersion     int              `json:"schema_version"`
	CheckpointID      string           `json:"checkpoint_id"`
	CreateID          string           `json:"checkpoint_create_id"`
	Provider          ProviderIdentity `json:"provider"`
	WorkspaceID       string           `json:"workspace_id"`
	ActivityID        string           `json:"activity_id,omitempty"`
	SourceGeneration  string           `json:"source_generation"`
	CreatedAt         time.Time        `json:"created_at"`
	CapturedPathCount int              `json:"captured_path_count"`
	Excluded          []PathSummary    `json:"excluded,omitempty"`
	Unsupported       []PathSummary    `json:"unsupported,omitempty"`
	TotalBytes        int64            `json:"total_bytes"`
	CaptureQuality    CaptureQuality   `json:"capture_quality"`
	RetentionState    RetentionState   `json:"retention_state"`
	OpaqueEntryRefs   []string         `json:"opaque_entry_refs,omitempty"`
}

type RestorePathResult struct {
	Path    string             `json:"path"`
	Outcome RestorePathOutcome `json:"outcome"`
	Reason  string             `json:"reason,omitempty"`
}

type RestoreResult struct {
	SchemaVersion int                 `json:"schema_version"`
	RestoreID     string              `json:"restore_id"`
	CheckpointID  string              `json:"checkpoint_id"`
	Paths         []RestorePathResult `json:"paths"`
	Complete      bool                `json:"complete"`
}
