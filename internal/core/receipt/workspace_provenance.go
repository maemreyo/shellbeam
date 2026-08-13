package receipt

import (
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceProvenance struct {
	SchemaVersion      int                          `json:"schema_version"`
	RepositoryID       workspace.RepositoryID       `json:"repository_id,omitempty"`
	WorkspaceID        workspace.WorkspaceID        `json:"workspace_id,omitempty"`
	PreGeneration      string                       `json:"pre_generation,omitempty"`
	PostGeneration     string                       `json:"post_generation,omitempty"`
	PreQuality         workspace.ObservationQuality `json:"pre_quality"`
	PostQuality        workspace.ObservationQuality `json:"post_quality"`
	PreObservedAt      time.Time                    `json:"pre_observed_at"`
	PostObservedAt     time.Time                    `json:"post_observed_at"`
	PreDiagnosticCode  string                       `json:"pre_diagnostic_code,omitempty"`
	PostDiagnosticCode string                       `json:"post_diagnostic_code,omitempty"`
	ObservedChange     bool                         `json:"observed_change"`
}

func NewWorkspaceProvenance(pre, post workspace.FastSnapshot) *WorkspaceProvenance {
	repositoryID := pre.RepositoryID
	if repositoryID == "" {
		repositoryID = post.RepositoryID
	}
	workspaceID := pre.WorkspaceID
	if workspaceID == "" {
		workspaceID = post.WorkspaceID
	}
	return &WorkspaceProvenance{
		SchemaVersion: 1, RepositoryID: repositoryID, WorkspaceID: workspaceID,
		PreGeneration: pre.Generation, PostGeneration: post.Generation,
		PreQuality: pre.Quality, PostQuality: post.Quality,
		PreObservedAt: pre.ObservedAt, PostObservedAt: post.ObservedAt,
		PreDiagnosticCode: pre.DiagnosticCode, PostDiagnosticCode: post.DiagnosticCode,
		ObservedChange: pre.Generation != "" && post.Generation != "" && pre.Generation != post.Generation,
	}
}
