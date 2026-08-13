package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
)

func TestActivityStoreRoundTripAndCorruptedRecordIsolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	good := activity.New(activity.ID("good-activity"), now)
	bad := activity.New(activity.ID("bad-activity"), now)
	if err := repository.SaveActivity(context.Background(), good); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveActivity(context.Background(), bad); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := repository.LoadActivity(context.Background(), good.ID)
	if err != nil || !found || loaded.ID != good.ID {
		t.Fatalf("loaded=%#v found=%v err=%v", loaded, found, err)
	}
	badPath := filepath.Join(root, "activities", string(bad.ID), "index.json")
	if err := os.WriteFile(badPath, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, found, err = repository.LoadActivity(context.Background(), good.ID)
	if err != nil || !found || loaded.ID != good.ID {
		t.Fatalf("good record poisoned by corrupt sibling: loaded=%#v found=%v err=%v", loaded, found, err)
	}
	if _, found, err := repository.LoadActivity(context.Background(), bad.ID); err == nil || found {
		t.Fatalf("corrupt activity found=%v err=%v", found, err)
	}
}

func TestActivityStoreMissingRecordIsNotAnError(t *testing.T) {
	repository, err := Open(filepath.Join(t.TempDir(), "state"), Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := repository.LoadActivity(context.Background(), activity.ID("missing"))
	if err != nil || found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}

func TestActivityStoreRejectsSymlinkActivityDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := Open(root, Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	id := activity.ID("symlink-activity")
	if err := os.Symlink(outside, filepath.Join(root, "activities", string(id))); err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveActivity(context.Background(), activity.New(id, time.Now().UTC())); err == nil {
		t.Fatal("symlink activity directory accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "index.json")); !os.IsNotExist(err) {
		t.Fatalf("activity write escaped state root: %v", err)
	}
}
