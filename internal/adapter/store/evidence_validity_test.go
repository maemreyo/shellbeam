package store

import (
	"context"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
)

func TestEvidenceValidityPersistsLatestAndEmitsOnlyRealStatusChange(t *testing.T) {
	r := openEvidenceRepository(t, t.TempDir()+"/state")
	record := testEvidenceRecord()
	if created, err := r.PutEvidenceRecord(context.Background(), record); err != nil || !created {
		t.Fatalf("record created=%v err=%v", created, err)
	}
	before, err := r.ObservationHighWatermark(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	first := testValidityObservation(record.EvidenceID, core.SourceMatchFast, core.FreshnessCurrent, time.Now().UTC())
	changed, err := r.PutEvidenceValidity(context.Background(), first)
	if err != nil || changed {
		t.Fatalf("first changed=%v err=%v", changed, err)
	}
	if high, _ := r.ObservationHighWatermark(context.Background()); high != before {
		t.Fatalf("first observation emitted change event: before=%d high=%d", before, high)
	}

	same := first
	same.ObservedAt = first.ObservedAt.Add(time.Second)
	changed, err = r.PutEvidenceValidity(context.Background(), same)
	if err != nil || changed {
		t.Fatalf("same changed=%v err=%v", changed, err)
	}
	loaded, found, err := r.LoadEvidenceValidity(context.Background(), record.EvidenceID)
	if err != nil || !found || !loaded.ObservedAt.Equal(same.ObservedAt) {
		t.Fatalf("loaded=%#v found=%v err=%v", loaded, found, err)
	}

	stale := same
	stale.Validity.SourceMatch = core.SourceMatchMismatch
	stale.Validity.Freshness = core.FreshnessStale
	stale.ObservedAt = same.ObservedAt.Add(time.Second)
	changed, err = r.PutEvidenceValidity(context.Background(), stale)
	if err != nil || !changed {
		t.Fatalf("stale changed=%v err=%v", changed, err)
	}
	obligations, err := r.ListObservationObligations(context.Background(), before, 8)
	if err != nil || len(obligations) != 1 || obligations[0].Kind != observation.EventEvidenceValidityChanged || obligations[0].State != observation.ObligationCommitted {
		t.Fatalf("obligations=%#v err=%v", obligations, err)
	}
}

func testValidityObservation(evidenceID string, match core.SourceMatch, freshness core.Freshness, at time.Time) core.ValidityObservation {
	return core.ValidityObservation{SchemaVersion: core.ValiditySchemaVersion, EvidenceID: evidenceID, Validity: core.Validity{SourceMatch: match, Freshness: freshness, ArtifactMatch: core.ArtifactMatchUnknown, PolicyMatch: core.PolicyMatchUnknown}, CurrentSource: core.CurrentSource{WorkspaceID: "ws_01K00000000000000000000000", Generation: "gen_" + strings.Repeat("d", 64), Quality: core.SourceQualityFast}, ObservedAt: at}
}
