package codeintel

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

const MaxProviderTextBytes = 512

type SyncCoverage string

const (
	SyncExactForKnownPaths SyncCoverage = "exact_for_known_paths"
	SyncProviderManaged    SyncCoverage = "provider_managed"
	SyncPartial            SyncCoverage = "partial"
	SyncUnknown            SyncCoverage = "unknown"
)

type ProviderMetadata struct {
	ProviderID           string       `json:"provider_id"`
	Incarnation          string       `json:"provider_incarnation"`
	ExecutableVersion    string       `json:"provider_version,omitempty"`
	ConfigFingerprint    string       `json:"config_fingerprint,omitempty"`
	BuildFingerprint     string       `json:"build_fingerprint,omitempty"`
	BuildQuality         string       `json:"build_quality,omitempty"`
	Coverage             SyncCoverage `json:"sync_coverage"`
	SemanticScopeQuality string       `json:"semantic_scope_quality,omitempty"`
}

func (c SyncCoverage) Validate() error {
	switch c {
	case SyncExactForKnownPaths, SyncProviderManaged, SyncPartial, SyncUnknown:
		return nil
	default:
		return fmt.Errorf("invalid provider sync coverage %q", c)
	}
}

func (m ProviderMetadata) Validate() error {
	if m == (ProviderMetadata{}) {
		return nil
	}
	if !safeBoundedText(m.ProviderID, MaxProviderTextBytes) ||
		!safeBoundedText(m.Incarnation, MaxProviderTextBytes) ||
		m.Coverage.Validate() != nil {
		return fmt.Errorf("invalid provider metadata")
	}
	for _, value := range []string{
		m.ExecutableVersion,
		m.ConfigFingerprint,
		m.BuildFingerprint,
		m.BuildQuality,
		m.SemanticScopeQuality,
	} {
		if value != "" && !safeBoundedText(value, MaxProviderTextBytes) {
			return fmt.Errorf("invalid provider metadata text")
		}
	}
	return nil
}

func safeBoundedText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
