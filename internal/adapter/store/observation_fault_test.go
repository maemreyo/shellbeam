package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

func TestObservationOpenUsesMetadataButReadStillRejectsCorruptRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, result := repository.PrepareObservation(ctx, observationRequest("subject:metadata-open")); result.Err != nil {
			t.Fatal(result.Err)
		}
	}
	if err := os.WriteFile(repository.observationPath(1), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(root, repository.limits)
	if err != nil {
		t.Fatalf("metadata-only reopen rejected an unrelated corrupt record: %v", err)
	}
	if high, err := reopened.ObservationHighWatermark(ctx); err != nil || high != 2 {
		t.Fatalf("high watermark after reopen = %d err=%v, want 2", high, err)
	}
	later, err := reopened.ListObservationObligations(ctx, 1, 10)
	if err != nil || len(later) != 1 || later[0].ChangeSeq != 2 {
		t.Fatalf("listing after corrupt history = %#v err=%v", later, err)
	}
	if _, err := reopened.ListObservationObligations(ctx, 0, 10); err == nil {
		t.Fatal("strict observation read accepted corrupt record content")
	}
}

func TestObservationOpenRecoversBeyondHistoricalScanLimit(t *testing.T) {
	const historicalScanLimit = 65536
	root := filepath.Join(t.TempDir(), "state")
	repository := openObservationRepository(t, root)
	sourceDir := filepath.Join(t.TempDir(), "sources")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const sourceCount = 256
	sources := make([]string, sourceCount)
	for i := range sources {
		sources[i] = filepath.Join(sourceDir, string(rune('a'+i%26))+"-"+strconv.Itoa(i))
		if err := os.WriteFile(sources[i], []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for seq := 1; seq <= historicalScanLimit+1; seq++ {
		path := repository.observationPath(observation.ChangeSeq(seq))
		if err := os.Link(sources[(seq-1)%sourceCount], path); err != nil {
			t.Fatalf("link seq %d: %v", seq, err)
		}
	}

	reopened, err := Open(root, repository.limits)
	if err != nil {
		t.Fatalf("reopen beyond historical scan limit: %v", err)
	}
	if high, err := reopened.ObservationHighWatermark(context.Background()); err != nil || high != historicalScanLimit+1 {
		t.Fatalf("high watermark = %d err=%v, want %d", high, err, historicalScanLimit+1)
	}
	next, result := reopened.PrepareObservation(context.Background(), observationRequest("subject:after-large-ledger"))
	if result.Err != nil || next.Obligation.ChangeSeq != historicalScanLimit+2 {
		t.Fatalf("next after oversized ledger = %#v result=%#v", next, result)
	}
}
