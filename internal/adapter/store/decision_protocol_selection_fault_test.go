package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestSelectionCommitCrashAfterRecordBeforeHighWaterReplaysAfterReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, store, ep, cand := selectionFixture(t, root, "ep-selection-fault")
	intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-fault")
	failed := false
	r.writer.fail = func(point string) error {
		if !failed && point == "replace.rename" {
			failed = true
			return errors.New("injected high-water failure")
		}
		return nil
	}
	if _, _, err := store.CommitSelectionCAS(context.Background(), intent, commit); err == nil {
		t.Fatal("fault did not interrupt commit")
	}
	r.writer.fail = nil
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	retry := commit
	retry.CommitID = "commit-after-reopen"
	got, created, err := store2.CommitSelectionCAS(context.Background(), intent, retry)
	if err != nil || created || got.SemanticIntentFingerprint != commit.SemanticIntentFingerprint {
		t.Fatalf("got=%#v created=%v err=%v", got, created, err)
	}
}
