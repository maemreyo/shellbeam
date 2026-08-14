package project

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const maxReviewMetadataBytes = 128

type Review struct {
	RepositoryID          workspace.RepositoryID `json:"repository_id"`
	ManifestFingerprint   string                 `json:"manifest_fingerprint"`
	DiscoveryFingerprint  string                 `json:"discovery_fingerprint"`
	ManifestSchemaVersion int                    `json:"manifest_schema_version"`
	ReviewedAt            time.Time              `json:"reviewed_at"`
	ToolVersion           string                 `json:"tool_version"`
	ReviewerClass         string                 `json:"reviewer_class"`
	SourceClass           string                 `json:"source_class"`
}

func (r Review) Validate() error {
	if _, err := workspace.ParseRepositoryID(string(r.RepositoryID)); err != nil {
		return fmt.Errorf("invalid review repository: %w", err)
	}
	if !validFingerprint(r.ManifestFingerprint) || !validFingerprint(r.DiscoveryFingerprint) {
		return fmt.Errorf("invalid review fingerprint")
	}
	if r.ManifestSchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported reviewed manifest schema")
	}
	if r.ReviewedAt.IsZero() {
		return fmt.Errorf("review timestamp is required")
	}
	if !safeReviewText(r.ToolVersion) {
		return fmt.Errorf("invalid review tool version")
	}
	if !reviewClass(r.ReviewerClass) || !reviewClass(r.SourceClass) {
		return fmt.Errorf("invalid review metadata class")
	}
	return nil
}

func (r Review) Current(manifestFingerprint, discoveryFingerprint string, schemaVersion int) bool {
	return r.ManifestFingerprint == manifestFingerprint &&
		r.DiscoveryFingerprint == discoveryFingerprint &&
		r.ManifestSchemaVersion == schemaVersion
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func safeReviewText(value string) bool {
	if value == "" || len(value) > maxReviewMetadataBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func reviewClass(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r < 'a' || r > 'z' {
				return false
			}
			continue
		}
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func ChangedDuringResolveError() error {
	return projectError(CodeChangedDuringResolve, "project manifest changed during review")
}
