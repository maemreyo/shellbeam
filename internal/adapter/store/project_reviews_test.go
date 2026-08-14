package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func testProjectReview(_ byte, reviewedAt time.Time) project.Review {
	return project.Review{
		RepositoryID:          workspace.RepositoryID("repo_01K00000000000000000000000"),
		ManifestFingerprint:   string(make([]byte, 64)),
		DiscoveryFingerprint:  string(make([]byte, 64)),
		ManifestSchemaVersion: project.SchemaVersion,
		ReviewedAt:            reviewedAt,
		ToolVersion:           "test",
		ReviewerClass:         "user",
		SourceClass:           "cli",
	}
}

func TestReviewStoreRoundTripAndConcurrentAtomicWrites(t *testing.T) {
	repository, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	records := []project.Review{
		testProjectReview('a', now),
		testProjectReview('b', now.Add(time.Second)),
	}
	records[0].ManifestFingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	records[0].DiscoveryFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	records[1].ManifestFingerprint = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	records[1].DiscoveryFingerprint = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		record := records[i%len(records)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repository.SaveProjectReview(context.Background(), record); err != nil {
				t.Errorf("save review: %v", err)
			}
		}()
	}
	wg.Wait()

	got, found, err := repository.LoadProjectReview(context.Background(), records[0].RepositoryID)
	if err != nil || !found {
		t.Fatalf("load found=%v err=%v", found, err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("stored review invalid: %v", err)
	}
	if got.ManifestFingerprint != records[0].ManifestFingerprint && got.ManifestFingerprint != records[1].ManifestFingerprint {
		t.Fatalf("torn review: %#v", got)
	}
	dir := filepath.Join(repository.root, "project_reviews")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("unexpected review store entries: %#v", entries)
	}
}

func TestReviewStoreMissingRecordIsNotAnError(t *testing.T) {
	repository, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := repository.LoadProjectReview(context.Background(), workspace.RepositoryID("repo_01K00000000000000000000000"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}
