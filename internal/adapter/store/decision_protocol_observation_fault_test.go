package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func TestObservationMaterializationCrashAfterRecordBeforeHighWaterRecoversUniqueBinding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	f := setupObservationFixture(t, root, "exp-observation-fault")
	before, err := f.store.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	binding := observationBinding(f, "bind-observation-fault", "cut_"+strings.Repeat("a", 64), time.Unix(70, 0))
	f.repo.writer.fail = func(point string) error {
		if point == "replace.rename" {
			return errors.New("injected high-water failure")
		}
		return nil
	}
	if _, _, err := f.repo.MaterializeExperimentObservationCAS(context.Background(), binding); err == nil {
		t.Fatal("injected high-water failure did not fail")
	}
	f.repo.writer.fail = nil

	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	after, err := store2.CurrentHighWater(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after != before+1 {
		t.Fatalf("recovered high-water=%d want=%d", after, before+1)
	}
	records, err := store2.ListEpisodeRecords(context.Background(), f.episode.EpisodeID, after)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	var got dp.ExperimentObservationBinding
	for _, record := range records {
		if record.Kind != dp.RecordExperimentObservationBinding {
			continue
		}
		count++
		if err := json.Unmarshal(record.Body, &got); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 || got.DerivationCutDigest != binding.DerivationCutDigest {
		t.Fatalf("count=%d binding=%#v", count, got)
	}
}
