package project

import (
	"testing"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestReviewRecordRequiresBoundedNonSecretMetadataAndExactFingerprints(t *testing.T) {
	now := time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC)
	record := Review{
		RepositoryID:          workspace.RepositoryID("repo_01K00000000000000000000000"),
		ManifestFingerprint:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DiscoveryFingerprint:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ManifestSchemaVersion: SchemaVersion,
		ReviewedAt:            now,
		ToolVersion:           "v0.1.0-dev",
		ReviewerClass:         "user",
		SourceClass:           "cli",
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	if !record.Current(record.ManifestFingerprint, record.DiscoveryFingerprint, SchemaVersion) {
		t.Fatal("exact review was not current")
	}
	if record.Current("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", record.DiscoveryFingerprint, SchemaVersion) {
		t.Fatal("changed manifest remained current")
	}
	if record.Current(record.ManifestFingerprint, "ddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", SchemaVersion) {
		t.Fatal("changed discovery inputs remained current")
	}
	if record.Current(record.ManifestFingerprint, record.DiscoveryFingerprint, SchemaVersion+1) {
		t.Fatal("changed schema remained current")
	}

	invalid := []Review{
		{},
		{RepositoryID: record.RepositoryID, ManifestFingerprint: record.ManifestFingerprint, DiscoveryFingerprint: record.DiscoveryFingerprint, ManifestSchemaVersion: SchemaVersion, ReviewedAt: now, ToolVersion: record.ToolVersion, SourceClass: "cli"},
		{RepositoryID: record.RepositoryID, ManifestFingerprint: record.ManifestFingerprint, DiscoveryFingerprint: record.DiscoveryFingerprint, ManifestSchemaVersion: SchemaVersion, ReviewedAt: now, ToolVersion: record.ToolVersion, ReviewerClass: "user"},
		{RepositoryID: record.RepositoryID, ManifestFingerprint: record.ManifestFingerprint, DiscoveryFingerprint: record.DiscoveryFingerprint, ManifestSchemaVersion: SchemaVersion, ReviewedAt: now, ReviewerClass: "user", SourceClass: "cli"},
		{RepositoryID: record.RepositoryID, ManifestFingerprint: "not-a-fingerprint", DiscoveryFingerprint: record.DiscoveryFingerprint, ManifestSchemaVersion: SchemaVersion, ReviewedAt: now, ToolVersion: record.ToolVersion, ReviewerClass: "user", SourceClass: "cli"},
		{RepositoryID: record.RepositoryID, ManifestFingerprint: record.ManifestFingerprint, DiscoveryFingerprint: record.DiscoveryFingerprint, ManifestSchemaVersion: SchemaVersion, ReviewedAt: now, ToolVersion: record.ToolVersion, ReviewerClass: "user@example.com", SourceClass: "cli"},
	}
	for i, value := range invalid {
		if err := value.Validate(); err == nil {
			t.Fatalf("invalid review %d accepted: %#v", i, value)
		}
	}
}
