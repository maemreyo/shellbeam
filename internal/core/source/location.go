// Package source defines provider-neutral source identity and location facts.
package source

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

const MaxLogicalPathBytes = 1024

type LocationKind string
type Origin string
type NormalizationQuality string

const (
	LocationResolved         LocationKind = "resolved"
	LocationProviderReported LocationKind = "provider_reported"

	OriginRepository Origin = "repository"
	OriginDependency Origin = "dependency"
	OriginToolchain  Origin = "toolchain"
	OriginExternal   Origin = "external"

	NormalizationExact       NormalizationQuality = "exact"
	NormalizationPartial     NormalizationQuality = "partial"
	NormalizationUnavailable NormalizationQuality = "unavailable"
)

type SourceLocation struct {
	Kind             LocationKind              `json:"kind"`
	Resolved         *ResolvedSourceLocation   `json:"resolved,omitempty"`
	ProviderReported *ProviderReportedLocation `json:"provider_reported,omitempty"`
}

type ResolvedSourceLocation struct {
	SourceRefID string `json:"source_ref_id"`
	StartByte   int64  `json:"start_byte"`
	EndByte     int64  `json:"end_byte"`
}

type ProviderReportedLocation struct {
	Origin               Origin               `json:"origin"`
	SanitizedLogicalPath string               `json:"sanitized_logical_path,omitempty"`
	Line                 int                  `json:"line,omitempty"`
	Column               int                  `json:"column,omitempty"`
	EndLine              int                  `json:"end_line,omitempty"`
	EndColumn            int                  `json:"end_column,omitempty"`
	NormalizationQuality NormalizationQuality `json:"normalization_quality"`
}

func (l SourceLocation) Validate() error {
	switch l.Kind {
	case LocationResolved:
		if l.Resolved == nil || l.ProviderReported != nil {
			return fmt.Errorf("invalid resolved source location")
		}
		return l.Resolved.Validate()
	case LocationProviderReported:
		if l.ProviderReported == nil || l.Resolved != nil {
			return fmt.Errorf("invalid provider-reported source location")
		}
		return l.ProviderReported.Validate()
	default:
		return fmt.Errorf("invalid source location kind")
	}
}

func (l ResolvedSourceLocation) Validate() error {
	if !validOpaqueID(l.SourceRefID, "src_") || l.StartByte < 0 || l.EndByte < l.StartByte {
		return fmt.Errorf("invalid resolved source location")
	}
	return nil
}

func (l ProviderReportedLocation) Validate() error {
	if !validOrigin(l.Origin) || !validNormalization(l.NormalizationQuality) {
		return fmt.Errorf("invalid provider-reported source metadata")
	}
	if l.SanitizedLogicalPath != "" && !safeLogicalPath(l.SanitizedLogicalPath) {
		return fmt.Errorf("invalid provider-reported path")
	}
	if (l.Line == 0) != (l.Column == 0) || l.Line < 0 || l.Column < 0 {
		return fmt.Errorf("invalid provider-reported start position")
	}
	if (l.EndLine == 0) != (l.EndColumn == 0) || l.EndLine < 0 || l.EndColumn < 0 {
		return fmt.Errorf("invalid provider-reported end position")
	}
	if l.EndLine > 0 && (l.Line == 0 || l.EndLine < l.Line || (l.EndLine == l.Line && l.EndColumn < l.Column)) {
		return fmt.Errorf("invalid provider-reported range")
	}
	return nil
}

func safeLogicalPath(v string) bool {
	if len(v) > MaxLogicalPathBytes || filepath.IsAbs(v) || v == "." || v == ".." {
		return false
	}
	clean := filepath.Clean(v)
	if clean != v || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validOpaqueID(v, prefix string) bool {
	return strings.HasPrefix(v, prefix) && len(v) > len(prefix) && len(v) <= 128
}

func validOrigin(v Origin) bool {
	switch v {
	case OriginRepository, OriginDependency, OriginToolchain, OriginExternal:
		return true
	default:
		return false
	}
}

func validNormalization(v NormalizationQuality) bool {
	switch v {
	case NormalizationExact, NormalizationPartial, NormalizationUnavailable:
		return true
	default:
		return false
	}
}
