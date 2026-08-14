package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestObservationAmbiguousCreateNeverReusesLinkedSequence(t *testing.T) {
	for _, point := range []string{"create.open_dir", "create.dir_sync"} {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			repository := openObservationRepository(t, root)
			repository.writer = failAtomicWriter(point)
			prepared, result := repository.PrepareObservation(context.Background(), observationRequest("subject:ambiguous"))
			if result.Err == nil || result.Durability != app.AmbiguousChange || prepared.Obligation.ChangeSeq != 1 {
				t.Fatalf("prepared=%#v result=%#v", prepared, result)
			}
			if _, err := os.Stat(filepath.Join(root, "observations", "obligations", "00000000000000000001.json")); err != nil {
				t.Fatalf("linked obligation missing: %v", err)
			}
			if high, err := repository.ObservationHighWatermark(context.Background()); err != nil || high != 1 {
				t.Fatalf("high watermark=%d err=%v", high, err)
			}
			repository.writer = atomicWriter{}
			next, result := repository.PrepareObservation(context.Background(), observationRequest("subject:next"))
			if result.Err != nil || next.Obligation.ChangeSeq != 2 {
				t.Fatalf("next=%#v result=%#v", next, result)
			}
		})
	}
}

func TestObservationPreparedSurvivesRestartAndTempFileIsNeverPromoted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	prepared, result := repository.PrepareObservation(context.Background(), observationRequest("subject:prepared"))
	if result.Err != nil || prepared.Obligation.State != observation.ObligationPrepared {
		t.Fatalf("prepared=%#v result=%#v", prepared, result)
	}
	dir := filepath.Join(root, "observations", "obligations")
	if err := os.WriteFile(filepath.Join(dir, ".shellbeam-crash-temp"), []byte(`{"change_seq":999999}`), 0600); err != nil {
		t.Fatal(err)
	}
	reopened := openObservationRepository(t, root)
	if high, err := reopened.ObservationHighWatermark(context.Background()); err != nil || high != 1 {
		t.Fatalf("high watermark=%d err=%v", high, err)
	}
	listed, err := reopened.ListObservationObligations(context.Background(), 0, 10)
	if err != nil || len(listed) != 1 || listed[0].ChangeSeq != 1 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestObservationRejectsOversizedAndMalformedDurableRecords(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	dir := filepath.Join(root, "observations", "obligations")
	oversized := filepath.Join(dir, "00000000000000000001.json")
	if err := os.WriteFile(oversized, []byte(strings.Repeat("x", MaxObservationObligationBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, repository.limits); err == nil {
		t.Fatal("oversized obligation accepted on reopen")
	}
	if err := os.Remove(oversized); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-sequence.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, repository.limits); err == nil {
		t.Fatal("malformed obligation filename accepted")
	}
}

func TestObservationCreateFaultBeforeLinkDoesNotAdvanceHighWatermark(t *testing.T) {
	for _, point := range []string{"create.create_temp", "create.write", "create.file_sync", "create.close", "create.link"} {
		t.Run(point, func(t *testing.T) {
			repository := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
			repository.writer = failAtomicWriter(point)
			_, result := repository.PrepareObservation(context.Background(), observationRequest("subject:fault"))
			if result.Err == nil {
				t.Fatal("fault did not interrupt prepare")
			}
			if high, err := repository.ObservationHighWatermark(context.Background()); err != nil || high != 0 {
				t.Fatalf("point=%s high=%d err=%v", point, high, err)
			}
		})
	}
}

func TestObservationContextCancellationDoesNotMutate(t *testing.T) {
	repository := openObservationRepository(t, filepath.Join(t.TempDir(), "state"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, result := repository.PrepareObservation(ctx, observationRequest("subject:cancel")); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("result=%#v", result)
	}
	if high, _ := repository.ObservationHighWatermark(context.Background()); high != 0 {
		t.Fatalf("high watermark=%d", high)
	}
}

func TestObservationRejectsSymlinkRecordEntry(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "observations", "obligations", "00000000000000000001.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, repository.limits); err == nil {
		t.Fatal("symlink obligation record accepted")
	}
}
