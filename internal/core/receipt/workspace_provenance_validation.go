package receipt

import (
	"fmt"
	"strings"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (p WorkspaceProvenance) Validate() error {
	switch p.SchemaVersion {
	case 1:
		return p.validateV1()
	case 2:
		return p.validateV2()
	default:
		return fmt.Errorf("invalid workspace provenance metadata")
	}
}

func (p WorkspaceProvenance) validateV1() error {
	if !provenanceQualityValid(p.PreQuality) || !provenanceQualityValid(p.PostQuality) {
		return fmt.Errorf("invalid workspace provenance metadata")
	}
	if p.PreObservedAt.IsZero() || p.PostObservedAt.IsZero() {
		return fmt.Errorf("workspace provenance observation time missing")
	}
	if p.RepositoryID != "" {
		if _, err := workspace.ParseRepositoryID(string(p.RepositoryID)); err != nil {
			return err
		}
	}
	if p.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(string(p.WorkspaceID)); err != nil {
			return err
		}
	}
	if p.PreQuality != workspace.QualityUnavailable && !validProvenanceGeneration(p.PreGeneration) {
		return fmt.Errorf("invalid pre generation")
	}
	if p.PostQuality != workspace.QualityUnavailable && !validProvenanceGeneration(p.PostGeneration) {
		return fmt.Errorf("invalid post generation")
	}
	if p.ObservedChange && (p.PreGeneration == "" || p.PostGeneration == "" || p.PreGeneration == p.PostGeneration) {
		return fmt.Errorf("invalid observed change")
	}
	return nil
}

func (p WorkspaceProvenance) validateV2() error {
	if err := p.Binding.Validate(); err != nil {
		return err
	}
	if err := p.Pre.Validate(); err != nil {
		return fmt.Errorf("invalid pre observation: %w", err)
	}
	if err := p.Post.Validate(); err != nil {
		return fmt.Errorf("invalid post observation: %w", err)
	}
	if p.ObservedChange {
		if p.Pre.Kind != WorkspaceFreshlySampled || p.Post.Kind != WorkspaceFreshlySampled || p.Pre.Generation == p.Post.Generation {
			return fmt.Errorf("invalid observed change")
		}
	}
	return nil
}

func (b WorkspaceBinding) Validate() error {
	if b.RepositoryID != "" {
		if _, err := workspace.ParseRepositoryID(string(b.RepositoryID)); err != nil {
			return err
		}
	}
	if b.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(string(b.WorkspaceID)); err != nil {
			return err
		}
	}
	return nil
}

func (r WorkspaceObservationRef) Validate() error {
	switch r.Kind {
	case WorkspaceFreshlySampled:
		return r.validateSampled()
	case WorkspaceCached:
		return r.validateCached()
	case WorkspaceUnreconciled:
		return r.validateUnreconciled()
	default:
		return fmt.Errorf("invalid workspace observation kind")
	}
}

func (r WorkspaceObservationRef) validateSampled() error {
	if !provenanceQualityValid(r.Quality) || r.ObservedAt.IsZero() || r.ObservationInvalidated {
		return fmt.Errorf("invalid sampled observation metadata")
	}
	if r.Quality == workspace.QualityUnavailable {
		if r.Generation != "" || strings.TrimSpace(r.DiagnosticCode) == "" {
			return fmt.Errorf("invalid unavailable sampled observation")
		}
		return nil
	}
	if !validProvenanceGeneration(r.Generation) {
		return fmt.Errorf("invalid sampled generation")
	}
	return nil
}

func (r WorkspaceObservationRef) validateCached() error {
	if !provenanceQualityValid(r.Quality) || r.ObservationInvalidated {
		return fmt.Errorf("invalid cached observation metadata")
	}
	if r.Quality == workspace.QualityUnavailable {
		if r.Generation != "" || strings.TrimSpace(r.DiagnosticCode) == "" {
			return fmt.Errorf("invalid unavailable cached observation")
		}
		return nil
	}
	if r.ObservedAt.IsZero() || !validProvenanceGeneration(r.Generation) {
		return fmt.Errorf("cached observation missing sample")
	}
	return nil
}

func (r WorkspaceObservationRef) validateUnreconciled() error {
	if r.Generation != "" || r.Quality != "" || !r.ObservedAt.IsZero() {
		return fmt.Errorf("unreconciled observation claims sampled facts")
	}
	return nil
}

func provenanceQualityValid(q workspace.ObservationQuality) bool {
	switch q {
	case workspace.QualityFresh, workspace.QualityCached, workspace.QualityStale, workspace.QualityUnavailable:
		return true
	default:
		return false
	}
}

func validProvenanceGeneration(generation string) bool {
	return strings.HasPrefix(generation, "gen_") && len(generation) == 68
}
