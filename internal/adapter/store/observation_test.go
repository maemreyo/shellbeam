package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestObservationPrepareConcurrentMonotonicAndRestartRecovery(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	const count = 32
	seqs := make(chan observation.ChangeSeq, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prepared, result := repository.PrepareObservation(context.Background(), observationRequest(fmt.Sprintf("subject:%02d", i)))
			if result.Err != nil || result.Durability != app.DurableChange {
				t.Errorf("prepare %d result=%#v", i, result)
				return
			}
			seqs <- prepared.Obligation.ChangeSeq
		}(i)
	}
	wg.Wait()
	close(seqs)
	var got []int
	for seq := range seqs {
		got = append(got, int(seq))
	}
	sort.Ints(got)
	for i := 1; i <= count; i++ {
		if got[i-1] != i {
			t.Fatalf("sequences=%v", got)
		}
	}
	if high, err := repository.ObservationHighWatermark(context.Background()); err != nil || high != count {
		t.Fatalf("high watermark=%d err=%v", high, err)
	}
	listed, err := repository.ListObservationObligations(context.Background(), 0, count)
	if err != nil || len(listed) != count {
		t.Fatalf("listed=%d err=%v", len(listed), err)
	}
	for i, record := range listed {
		if record.ChangeSeq != observation.ChangeSeq(i+1) || record.State != observation.ObligationPrepared {
			t.Fatalf("record[%d]=%#v", i, record)
		}
	}

	reopened := openObservationRepository(t, root)
	if high, err := reopened.ObservationHighWatermark(context.Background()); err != nil || high != count {
		t.Fatalf("reopened high watermark=%d err=%v", high, err)
	}
	next, result := reopened.PrepareObservation(context.Background(), observationRequest("subject:next"))
	if result.Err != nil || next.Obligation.ChangeSeq != count+1 {
		t.Fatalf("next=%#v result=%#v", next, result)
	}
}

func TestObservationCommitAbortReplayAndConflict(t *testing.T) {
	repository := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	first, result := repository.PrepareObservation(context.Background(), observationRequest("subject:commit"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result = repository.CommitObservation(context.Background(), first.Obligation.ChangeSeq); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result = repository.CommitObservation(context.Background(), first.Obligation.ChangeSeq); result.Err != nil {
		t.Fatalf("committed replay: %v", result.Err)
	}
	if result = repository.AbortObservation(context.Background(), first.Obligation.ChangeSeq, "canonical_missing"); result.Err == nil {
		t.Fatal("committed obligation accepted abort")
	}

	second, result := repository.PrepareObservation(context.Background(), observationRequest("subject:abort"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result = repository.AbortObservation(context.Background(), second.Obligation.ChangeSeq, "canonical_missing"); result.Err != nil {
		t.Fatal(result.Err)
	}
	if result = repository.AbortObservation(context.Background(), second.Obligation.ChangeSeq, "canonical_missing"); result.Err != nil {
		t.Fatalf("aborted replay: %v", result.Err)
	}
	if result = repository.AbortObservation(context.Background(), second.Obligation.ChangeSeq, "different_reason"); result.Err == nil {
		t.Fatal("aborted obligation accepted contradictory reason")
	}
	if result = repository.CommitObservation(context.Background(), second.Obligation.ChangeSeq); result.Err == nil {
		t.Fatal("aborted obligation accepted commit")
	}
	if result = repository.AbortObservation(context.Background(), second.Obligation.ChangeSeq, "raw error: /Users/me/.ssh/id_rsa"); result.Err == nil {
		t.Fatal("unsafe abort reason accepted")
	}

	listed, err := repository.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	if listed[0].State != observation.ObligationCommitted || listed[1].State != observation.ObligationAborted || listed[1].AbortReason != "canonical_missing" {
		t.Fatalf("states=%#v", listed)
	}
}

func TestObservationListIsExactOrderedAndBounded(t *testing.T) {
	repository := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	for i := 0; i < 5; i++ {
		if _, result := repository.PrepareObservation(context.Background(), observationRequest(fmt.Sprintf("subject:%d", i))); result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	listed, err := repository.ListObservationObligations(context.Background(), 2, 2)
	if err != nil || len(listed) != 2 || listed[0].ChangeSeq != 3 || listed[1].ChangeSeq != 4 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	for _, limit := range []int{0, -1, MaxObservationListRecords + 1} {
		if _, err := repository.ListObservationObligations(context.Background(), 0, limit); err == nil {
			t.Fatalf("limit %d accepted", limit)
		}
	}
}

func openObservationRepository(t *testing.T, root string) *Repository {
	t.Helper()
	repository, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func observationRequest(subject string) observation.PrepareRequest {
	return observation.PrepareRequest{
		Kind:       observation.EventOperationAdmitted,
		SubjectRef: subject,
		Summary:    "operation admitted",
		Correlation: observation.Correlation{
			OperationID: "op-1",
			SessionID:   "session-1",
		},
	}
}

func TestObservationTerminalTransitionSignalsMaterializer(t *testing.T) {
	repository := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	wakeups := repository.ObservationWakeups()
	prepared, result := repository.PrepareObservation(context.Background(), observationRequest("subject:wakeup"))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	select {
	case <-wakeups:
		t.Fatal("prepared observation woke the materializer before terminal resolution")
	default:
	}
	if result := repository.CommitObservation(context.Background(), prepared.Obligation.ChangeSeq); result.Err != nil {
		t.Fatal(result.Err)
	}
	select {
	case <-wakeups:
	case <-time.After(time.Second):
		t.Fatal("committed observation did not wake the materializer")
	}
}
