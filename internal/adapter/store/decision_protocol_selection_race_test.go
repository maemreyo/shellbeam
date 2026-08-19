package store

import (
	"context"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"path/filepath"
	"sync"
	"testing"
)

func TestSelectionCommitVsCloseUnresolvedRaceHasExactlyOneTerminalFact(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-terminal-race")
	intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-race")
	closure := unresolvedClosure(ep, commit.ProjectionDigest, "race")
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CommitSelectionCAS(context.Background(), intent, commit)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _, err := store.CloseEpisodeCAS(context.Background(), closure)
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		if r, ok := dp.ReasonOf(err); ok && (r == dp.ReasonEpisodeTerminalConflict || r == dp.ReasonTerminalSelectionConflict) {
			conflict++
			continue
		}
		t.Fatalf("unexpected error: %v", err)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	hw, err := store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(context.Background(), ep.EpisodeID, hw)
	if err != nil {
		t.Fatal(err)
	}
	terminal := 0
	for _, record := range records {
		if record.Kind == dp.RecordSelectionCommit || record.Kind == dp.RecordClosure {
			terminal++
		}
	}
	if terminal != 1 {
		t.Fatalf("terminal records=%d", terminal)
	}
}
