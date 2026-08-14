// Package codeintel defines provider-neutral structured code intelligence contracts.
package codeintel

import (
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	sharedsource "github.com/maemreyo/shellbeam/internal/core/source"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
	"github.com/oklog/ulid/v2"
)

const (
	MaxLogicalPathBytes     = sharedsource.MaxLogicalPathBytes
	MaxDisplayIdentityBytes = 1024
	TextEncodingUTF8        = "utf-8"
)

type SourceRefID string
type SourceOrigin string
type ResolutionQuality string

const (
	SourceRepository SourceOrigin = "repository"
	SourceWorkspace  SourceOrigin = "workspace"
	SourceDependency SourceOrigin = "dependency"
	SourceToolchain  SourceOrigin = "toolchain"
	SourceGenerated  SourceOrigin = "generated"
	SourceExternal   SourceOrigin = "external"

	ResolutionExact       ResolutionQuality = "exact"
	ResolutionObserved    ResolutionQuality = "observed"
	ResolutionUnavailable ResolutionQuality = "unavailable"
)

type SourceRef struct {
	ID                SourceRefID            `json:"source_ref_id"`
	Origin            SourceOrigin           `json:"origin"`
	RepositoryID      workspace.RepositoryID `json:"repository_id,omitempty"`
	WorkspaceID       workspace.WorkspaceID  `json:"workspace_id,omitempty"`
	LogicalPath       string                 `json:"logical_path,omitempty"`
	DisplayIdentity   string                 `json:"display_identity,omitempty"`
	ResolutionQuality ResolutionQuality      `json:"resolution_quality"`
	TextEncoding      string                 `json:"text_encoding"`
}

type ByteRange struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func (r ByteRange) Validate() error {
	if r.Start < 0 || r.End < r.Start {
		return fmt.Errorf("invalid byte range")
	}
	return nil
}

func ParseSourceRefID(v string) (SourceRefID, error) {
	if !strings.HasPrefix(v, "src_") {
		return "", fmt.Errorf("invalid source ref id")
	}
	raw := strings.TrimPrefix(v, "src_")
	if len(raw) != 26 {
		return "", fmt.Errorf("invalid source ref id")
	}
	if _, err := ulid.ParseStrict(raw); err != nil {
		return "", fmt.Errorf("invalid source ref id")
	}
	return SourceRefID(v), nil
}

func (r SourceRef) Validate() error {
	if _, err := ParseSourceRefID(string(r.ID)); err != nil {
		return err
	}
	if !validSourceOrigin(r.Origin) || !validResolutionQuality(r.ResolutionQuality) {
		return fmt.Errorf("invalid source ref metadata")
	}
	if r.RepositoryID != "" {
		if _, err := workspace.ParseRepositoryID(string(r.RepositoryID)); err != nil {
			return err
		}
	}
	if r.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(string(r.WorkspaceID)); err != nil {
			return err
		}
	}
	if r.LogicalPath != "" && !safeLogicalPath(r.LogicalPath) {
		return fmt.Errorf("invalid source logical path")
	}
	if r.DisplayIdentity != "" && !safeDisplayIdentity(r.DisplayIdentity) {
		return fmt.Errorf("invalid source display identity")
	}
	if r.TextEncoding != TextEncodingUTF8 {
		return fmt.Errorf("unsupported source encoding")
	}
	return nil
}

func validSourceOrigin(v SourceOrigin) bool {
	switch v {
	case SourceRepository, SourceWorkspace, SourceDependency, SourceToolchain, SourceGenerated, SourceExternal:
		return true
	default:
		return false
	}
}

func validResolutionQuality(v ResolutionQuality) bool {
	switch v {
	case ResolutionExact, ResolutionObserved, ResolutionUnavailable:
		return true
	default:
		return false
	}
}

func safeLogicalPath(v string) bool {
	if len(v) > MaxLogicalPathBytes || !safeUTF8Text(v) || strings.HasPrefix(v, "/") {
		return false
	}
	clean := path.Clean(v)
	return clean == v && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func safeDisplayIdentity(v string) bool {
	return len(v) <= MaxDisplayIdentityBytes && safeUTF8Text(v)
}

func safeUTF8Text(v string) bool {
	if v == "" || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// SourceLocation aliases the shared observation location contract so E22 and E29
// cannot drift into provider-specific path/range semantics.
type (
	LocationKind             = sharedsource.LocationKind
	SourceLocation           = sharedsource.SourceLocation
	ResolvedSourceLocation   = sharedsource.ResolvedSourceLocation
	ProviderReportedLocation = sharedsource.ProviderReportedLocation
	Origin                   = sharedsource.Origin
	NormalizationQuality     = sharedsource.NormalizationQuality
)

const (
	LocationResolved         = sharedsource.LocationResolved
	LocationProviderReported = sharedsource.LocationProviderReported

	OriginRepository = sharedsource.OriginRepository
	OriginDependency = sharedsource.OriginDependency
	OriginToolchain  = sharedsource.OriginToolchain
	OriginExternal   = sharedsource.OriginExternal

	NormalizationExact       = sharedsource.NormalizationExact
	NormalizationPartial     = sharedsource.NormalizationPartial
	NormalizationUnavailable = sharedsource.NormalizationUnavailable
)
