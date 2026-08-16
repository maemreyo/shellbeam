package checkpoint

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const ReservationSchemaVersion = 1

var checkpointIDPattern = regexp.MustCompile(`^chk_[0-9A-HJKMNP-TV-Z]{26}$`)

type CreateReservation struct {
	SchemaVersion      int                   `json:"schema_version"`
	CreateID           string                `json:"checkpoint_create_id"`
	RequestFingerprint string                `json:"request_fingerprint"`
	CheckpointID       string                `json:"checkpoint_id"`
	Provider           core.ProviderIdentity `json:"provider"`
	WorkspaceID        string                `json:"workspace_id"`
	ActivityID         string                `json:"activity_id,omitempty"`
	Paths              []string              `json:"paths"`
	SourceGeneration   string                `json:"source_generation,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
}

type RestoreReservation struct {
	SchemaVersion      int       `json:"schema_version"`
	RestoreID          string    `json:"restore_id"`
	RequestFingerprint string    `json:"request_fingerprint"`
	CheckpointID       string    `json:"checkpoint_id"`
	WorkspaceID        string    `json:"workspace_id"`
	Paths              []string  `json:"paths"`
	StartedAt          time.Time `json:"started_at"`
}

type Repository interface {
	ReserveCheckpointCreate(context.Context, CreateReservation) (CreateReservation, *core.Checkpoint, bool, error)
	BindCheckpointSource(context.Context, string, string) (CreateReservation, error)
	CompleteCheckpointCreate(context.Context, string, core.Checkpoint) (core.Checkpoint, error)
	FindCheckpointByCreateID(context.Context, string) (CreateReservation, *core.Checkpoint, bool, error)
	LoadCheckpoint(context.Context, string) (core.Checkpoint, error)
	ListCheckpointMetadata(context.Context) ([]core.Checkpoint, error)
	MarkCheckpointRetention(context.Context, string, core.RetentionState) (core.Checkpoint, error)
	ReserveCheckpointRestore(context.Context, RestoreReservation) (RestoreReservation, *core.RestoreResult, bool, error)
	RecordCheckpointRestorePath(context.Context, string, int, core.RestorePathResult) error
	CompleteCheckpointRestore(context.Context, string, core.RestoreResult) (core.RestoreResult, error)
	LoadCheckpointRestore(context.Context, string) (RestoreReservation, []core.RestorePathResult, *core.RestoreResult, error)
}

func (r CreateReservation) Validate() error {
	if r.SchemaVersion != ReservationSchemaVersion || !checkpointIDPattern.MatchString(r.CheckpointID) || r.CreatedAt.IsZero() {
		return fmt.Errorf("invalid checkpoint create reservation")
	}
	if _, err := operation.ParseID(r.CreateID); err != nil {
		return fmt.Errorf("invalid checkpoint create id")
	}
	if !validDigest(r.RequestFingerprint) {
		return fmt.Errorf("invalid checkpoint request fingerprint")
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return err
	}
	normalized, err := (core.CreateRequest{CreateID: r.CreateID, WorkspaceID: r.WorkspaceID, ActivityID: r.ActivityID, Paths: r.Paths}).Normalize()
	if err != nil || !slices.Equal(normalized.Paths, r.Paths) {
		return fmt.Errorf("non-canonical checkpoint create reservation")
	}
	fingerprint, err := normalized.Fingerprint()
	if err != nil || fingerprint != r.RequestFingerprint {
		return fmt.Errorf("checkpoint create fingerprint mismatch")
	}
	if r.SourceGeneration != "" && !validGeneration(r.SourceGeneration) {
		return fmt.Errorf("invalid checkpoint source generation")
	}
	return nil
}

func (r RestoreReservation) Validate() error {
	if r.SchemaVersion != ReservationSchemaVersion || r.StartedAt.IsZero() || !validDigest(r.RequestFingerprint) {
		return fmt.Errorf("invalid checkpoint restore reservation")
	}
	if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return err
	}
	normalized, err := (core.RestoreRequest{RestoreID: r.RestoreID, CheckpointID: r.CheckpointID, Paths: r.Paths}).Normalize()
	if err != nil || !slices.Equal(normalized.Paths, r.Paths) {
		return fmt.Errorf("non-canonical checkpoint restore reservation")
	}
	fingerprint, err := normalized.Fingerprint()
	if err != nil || fingerprint != r.RequestFingerprint {
		return fmt.Errorf("checkpoint restore fingerprint mismatch")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validGeneration(value string) bool {
	if len(value) != 68 || !strings.HasPrefix(value, "gen_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "gen_"))
	return err == nil
}

type WorkspaceContext struct {
	WorkspaceID      string `json:"workspace_id"`
	RepositoryID     string `json:"repository_id"`
	Root             string `json:"-"`
	SourceGeneration string `json:"source_generation"`
}

type WorkspaceSource interface {
	ResolveFresh(context.Context, string) (WorkspaceContext, error)
	InvalidateAfterMutation(context.Context, string) error
}

type CaptureRequest struct {
	CheckpointID     string
	WorkspaceID      string
	RepositoryID     string
	ActivityID       string
	Root             string
	SourceGeneration string
	Paths            []string
}

type CaptureResult struct {
	CapturedPathCount int                 `json:"captured_path_count"`
	Excluded          []core.PathSummary  `json:"excluded,omitempty"`
	Unsupported       []core.PathSummary  `json:"unsupported,omitempty"`
	TotalBytes        int64               `json:"total_bytes"`
	CaptureQuality    core.CaptureQuality `json:"capture_quality"`
	OpaqueEntryRefs   []string            `json:"opaque_entry_refs,omitempty"`
}

type ProviderRestoreRequest struct {
	RestoreID    string
	CheckpointID string
	WorkspaceID  string
	Root         string
	Paths        []string
}

type ProviderRestoreResult struct {
	Paths []core.RestorePathResult
}

type ProviderCheckpointStatus struct {
	CheckpointID   string
	RetentionState core.RetentionState
	Available      bool
}

type SweepRequest struct {
	Now            time.Time
	MaxCheckpoints int
	MaxBytes       int64
	MaxAge         time.Duration
}

type SweepResult struct {
	ExpiredCheckpointIDs []string
	FreedBytes           int64
}

type Provider interface {
	Identity() core.ProviderIdentity
	ConflictDetection() core.ConflictDetection
	Capture(context.Context, CaptureRequest) (CaptureResult, error)
	Restore(context.Context, ProviderRestoreRequest) (ProviderRestoreResult, error)
	Inspect(context.Context, string) (ProviderCheckpointStatus, error)
	Sweep(context.Context, SweepRequest) (SweepResult, error)
}
