package receipt

import (
	"encoding/json"
	"fmt"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceObservationKind string

const (
	WorkspaceFreshlySampled WorkspaceObservationKind = "freshly_sampled"
	WorkspaceCached         WorkspaceObservationKind = "cached"
	WorkspaceUnreconciled   WorkspaceObservationKind = "unreconciled"
)

type WorkspaceBinding struct {
	RepositoryID workspace.RepositoryID `json:"repository_id,omitempty"`
	WorkspaceID  workspace.WorkspaceID  `json:"workspace_id,omitempty"`
}

type WorkspaceObservationRef struct {
	Kind                   WorkspaceObservationKind     `json:"kind"`
	Generation             string                       `json:"generation,omitempty"`
	Quality                workspace.ObservationQuality `json:"quality,omitempty"`
	ObservedAt             time.Time                    `json:"observed_at,omitempty"`
	DiagnosticCode         string                       `json:"diagnostic_code,omitempty"`
	ObservationInvalidated bool                         `json:"observation_invalidated,omitempty"`
}

type WorkspaceProvenance struct {
	SchemaVersion  int                     `json:"schema_version"`
	Binding        WorkspaceBinding        `json:"binding,omitempty"`
	Pre            WorkspaceObservationRef `json:"pre,omitempty"`
	Post           WorkspaceObservationRef `json:"post,omitempty"`
	ObservedChange bool                    `json:"observed_change"`

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

func NewWorkspaceProvenanceV2(binding WorkspaceBinding, pre, post WorkspaceObservationRef, observedChange bool) *WorkspaceProvenance {
	return &WorkspaceProvenance{SchemaVersion: 2, Binding: binding, Pre: pre, Post: post, ObservedChange: observedChange}
}

func (r WorkspaceObservationRef) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind                   WorkspaceObservationKind     `json:"kind"`
		Generation             string                       `json:"generation,omitempty"`
		Quality                workspace.ObservationQuality `json:"quality,omitempty"`
		ObservedAt             *time.Time                   `json:"observed_at,omitempty"`
		DiagnosticCode         string                       `json:"diagnostic_code,omitempty"`
		ObservationInvalidated bool                         `json:"observation_invalidated,omitempty"`
	}
	out := wire{Kind: r.Kind, Generation: r.Generation, Quality: r.Quality, DiagnosticCode: r.DiagnosticCode, ObservationInvalidated: r.ObservationInvalidated}
	if !r.ObservedAt.IsZero() {
		observedAt := r.ObservedAt
		out.ObservedAt = &observedAt
	}
	return json.Marshal(out)
}

func (r *WorkspaceObservationRef) UnmarshalJSON(data []byte) error {
	var in struct {
		Kind                   WorkspaceObservationKind     `json:"kind"`
		Generation             string                       `json:"generation"`
		Quality                workspace.ObservationQuality `json:"quality"`
		ObservedAt             *time.Time                   `json:"observed_at"`
		DiagnosticCode         string                       `json:"diagnostic_code"`
		ObservationInvalidated bool                         `json:"observation_invalidated"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*r = WorkspaceObservationRef{Kind: in.Kind, Generation: in.Generation, Quality: in.Quality, DiagnosticCode: in.DiagnosticCode, ObservationInvalidated: in.ObservationInvalidated}
	if in.ObservedAt != nil {
		r.ObservedAt = *in.ObservedAt
	}
	return nil
}

func (p WorkspaceProvenance) MarshalJSON() ([]byte, error) {
	switch p.SchemaVersion {
	case 1:
		return json.Marshal(workspaceProvenanceV1From(p))
	case 2:
		return json.Marshal(workspaceProvenanceV2{SchemaVersion: 2, Binding: p.Binding, Pre: p.Pre, Post: p.Post, ObservedChange: p.ObservedChange})
	default:
		return nil, fmt.Errorf("unsupported workspace provenance schema version %d", p.SchemaVersion)
	}
}

func (p *WorkspaceProvenance) UnmarshalJSON(data []byte) error {
	var header struct {
		SchemaVersion int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	switch header.SchemaVersion {
	case 1:
		var in workspaceProvenanceV1
		if err := json.Unmarshal(data, &in); err != nil {
			return err
		}
		*p = in.toCore()
		return nil
	case 2:
		var in workspaceProvenanceV2
		if err := json.Unmarshal(data, &in); err != nil {
			return err
		}
		*p = WorkspaceProvenance{SchemaVersion: 2, Binding: in.Binding, Pre: in.Pre, Post: in.Post, ObservedChange: in.ObservedChange}
		return nil
	default:
		return fmt.Errorf("unsupported workspace provenance schema version %d", header.SchemaVersion)
	}
}

type workspaceProvenanceV1 struct {
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

func workspaceProvenanceV1From(p WorkspaceProvenance) workspaceProvenanceV1 {
	return workspaceProvenanceV1{p.SchemaVersion, p.RepositoryID, p.WorkspaceID, p.PreGeneration, p.PostGeneration, p.PreQuality, p.PostQuality, p.PreObservedAt, p.PostObservedAt, p.PreDiagnosticCode, p.PostDiagnosticCode, p.ObservedChange}
}

func (p workspaceProvenanceV1) toCore() WorkspaceProvenance {
	return WorkspaceProvenance{SchemaVersion: p.SchemaVersion, RepositoryID: p.RepositoryID, WorkspaceID: p.WorkspaceID, PreGeneration: p.PreGeneration, PostGeneration: p.PostGeneration, PreQuality: p.PreQuality, PostQuality: p.PostQuality, PreObservedAt: p.PreObservedAt, PostObservedAt: p.PostObservedAt, PreDiagnosticCode: p.PreDiagnosticCode, PostDiagnosticCode: p.PostDiagnosticCode, ObservedChange: p.ObservedChange}
}

type workspaceProvenanceV2 struct {
	SchemaVersion  int                     `json:"schema_version"`
	Binding        WorkspaceBinding        `json:"binding"`
	Pre            WorkspaceObservationRef `json:"pre"`
	Post           WorkspaceObservationRef `json:"post"`
	ObservedChange bool                    `json:"observed_change"`
}
