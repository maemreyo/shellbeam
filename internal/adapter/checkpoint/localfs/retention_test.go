package localfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestSweepRetentionCountAgeAndBytesExpireOldestDeterministically(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		provider, root, base := retentionFixture(t)
		ids := []string{
			"chk_01K00000000000000000000001",
			"chk_01K00000000000000000000002",
			"chk_01K00000000000000000000003",
		}
		for i, id := range ids {
			captureRetentionCheckpoint(t, provider, root, id, base.Add(time.Duration(i)*time.Hour), []byte{byte('a' + byte(i))})
		}
		got, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
			Now: base.Add(4 * time.Hour), MaxCheckpoints: 2, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.ExpiredCheckpointIDs, []string{ids[0]}) || got.FreedBytes != 1 {
			t.Fatalf("count sweep=%#v", got)
		}
		assertProviderRetention(t, provider, ids[0], core.RetentionExpired, false)
		assertProviderRetention(t, provider, ids[1], core.RetentionAvailable, true)
	})

	t.Run("age", func(t *testing.T) {
		provider, root, base := retentionFixture(t)
		oldID := "chk_01K00000000000000000000011"
		newID := "chk_01K00000000000000000000012"
		captureRetentionCheckpoint(t, provider, root, oldID, base, []byte("old"))
		captureRetentionCheckpoint(t, provider, root, newID, base.Add(6*24*time.Hour), []byte("new"))
		got, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
			Now: base.Add(8 * 24 * time.Hour), MaxCheckpoints: 64, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.ExpiredCheckpointIDs, []string{oldID}) || got.FreedBytes != 3 {
			t.Fatalf("age sweep=%#v", got)
		}
		assertProviderRetention(t, provider, newID, core.RetentionAvailable, true)
	})

	t.Run("bytes", func(t *testing.T) {
		provider, root, base := retentionFixture(t)
		ids := []string{
			"chk_01K00000000000000000000021",
			"chk_01K00000000000000000000022",
			"chk_01K00000000000000000000023",
		}
		payloads := [][]byte{[]byte("aaa"), []byte("bbbb"), []byte("ccccc")}
		for i := range ids {
			captureRetentionCheckpoint(t, provider, root, ids[i], base.Add(time.Duration(i)*time.Hour), payloads[i])
		}
		got, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
			Now: base.Add(4 * time.Hour), MaxCheckpoints: 64, MaxBytes: 9, MaxAge: 7 * 24 * time.Hour,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.ExpiredCheckpointIDs, []string{ids[0]}) || got.FreedBytes != 3 {
			t.Fatalf("byte sweep=%#v", got)
		}
	})
}

func TestSweepPinsIncompleteRestoreAndExpiresNextOldest(t *testing.T) {
	provider, root, base := retentionFixture(t)
	pinned := "chk_01K00000000000000000000031"
	evictable := "chk_01K00000000000000000000032"

	first := filepath.Join(root, "a.txt")
	second := filepath.Join(root, "b.txt")
	if err := os.WriteFile(first, []byte("captured-a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("captured-b"), 0644); err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time { return base }
	req := captureRequest(root, []string{"a.txt", "b.txt"})
	req.CheckpointID = pinned
	if _, err := provider.Capture(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("changed-a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("changed-b"), 0644); err != nil {
		t.Fatal(err)
	}
	provider.afterRestorePath = func(ordinal int) error {
		if ordinal == 0 {
			return errors.New("injected crash")
		}
		return nil
	}
	restoreReq := restoreRequest(root, "restore-pinned", []string{"a.txt", "b.txt"})
	restoreReq.CheckpointID = pinned
	if _, err := provider.Restore(context.Background(), restoreReq); err == nil || err.Error() != "injected crash" {
		t.Fatalf("expected injected incomplete restore, err=%v", err)
	}
	provider.afterRestorePath = nil

	captureRetentionCheckpoint(t, provider, root, evictable, base.Add(time.Hour), []byte("x"))
	got, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
		Now: base.Add(2 * time.Hour), MaxCheckpoints: 1, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ExpiredCheckpointIDs, []string{evictable}) {
		t.Fatalf("pinned sweep=%#v", got)
	}
	assertProviderRetention(t, provider, pinned, core.RetentionAvailable, true)
}

func TestSweepTombstonesBeforeCleanupAndLeavesPartialOnCleanupFailure(t *testing.T) {
	provider, root, base := retentionFixture(t)
	id := "chk_01K00000000000000000000041"
	captureRetentionCheckpoint(t, provider, root, id, base, []byte("private"))

	provider.beforeRetentionCleanup = func(checkpointID string) error {
		manifest, err := provider.loadManifest(checkpointID)
		if err != nil {
			return err
		}
		if manifest.RetentionState != core.RetentionPartiallyCompacted {
			return errors.New("cleanup ran before partial tombstone")
		}
		return errors.New("injected cleanup failure")
	}
	_, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
		Now: base.Add(time.Hour), MaxCheckpoints: 0, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("cleanup failure accepted")
	}
	assertProviderRetention(t, provider, id, core.RetentionPartiallyCompacted, true)

	provider.beforeRetentionCleanup = nil
	got, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
		Now: base.Add(2 * time.Hour), MaxCheckpoints: 0, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ExpiredCheckpointIDs, []string{id}) || got.FreedBytes != 7 {
		t.Fatalf("resume cleanup=%#v", got)
	}
	assertProviderRetention(t, provider, id, core.RetentionExpired, false)
}

func TestRestoreRefusesExpiredCheckpointButInspectRemainsAvailable(t *testing.T) {
	provider, root, base := retentionFixture(t)
	id := "chk_01K00000000000000000000051"
	captureRetentionCheckpoint(t, provider, root, id, base, []byte("captured"))
	if _, err := provider.Sweep(context.Background(), checkpointapp.SweepRequest{
		Now: base.Add(time.Hour), MaxCheckpoints: 0, MaxBytes: 1 << 30, MaxAge: 7 * 24 * time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := provider.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if status.CheckpointID != id || status.RetentionState != core.RetentionExpired || status.Available {
		t.Fatalf("expired inspect=%#v", status)
	}
	request := restoreRequest(root, "restore-expired", []string{"file.txt"})
	request.CheckpointID = id
	if _, err := provider.Restore(context.Background(), request); !localFailureIs(err, failure.CheckpointExpired) {
		t.Fatalf("expired restore err=%v", err)
	}
}

func retentionFixture(t *testing.T) (*Provider, string, time.Time) {
	t.Helper()
	provider := newRestoreTestProvider(t)
	root := t.TempDir()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return provider, root, base
}

func captureRetentionCheckpoint(t *testing.T, provider *Provider, root, id string, at time.Time, data []byte) {
	t.Helper()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time { return at }
	req := captureRequest(root, []string{"file.txt"})
	req.CheckpointID = id
	if _, err := provider.Capture(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func assertProviderRetention(t *testing.T, provider *Provider, id string, state core.RetentionState, available bool) {
	t.Helper()
	got, err := provider.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckpointID != id || got.RetentionState != state || got.Available != available {
		t.Fatalf("inspect %s=%#v", id, got)
	}
}
