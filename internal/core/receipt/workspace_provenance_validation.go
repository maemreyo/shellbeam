package receipt

import (
	"fmt"
	"strings"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (p WorkspaceProvenance) Validate() error {
	if p.SchemaVersion != 1 || !provenanceQualityValid(p.PreQuality) || !provenanceQualityValid(p.PostQuality) {
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
