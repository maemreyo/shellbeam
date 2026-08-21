package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func openDecisionProtocolRepo(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 64 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func dpEpisode(id string) decisionprotocol.DecisionEpisode {
	return decisionprotocol.DecisionEpisode{
		SchemaVersion: 1, EpisodeID: decisionprotocol.EpisodeID(id), EpisodeKind: decisionprotocol.EpisodeDiagnosis,
		RepositoryID: "repo-test", WorkspaceID: "ws-test",
		Baseline:          decisionprotocol.EpisodeBaseline{SourceGeneration: "src-1"},
		PolicyBinding:     decisionprotocol.EpisodePolicyBinding{PolicyID: "p", PolicyDigest: "pol_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ActivationRef: "act-1"},
		CreatedByActorRef: "actor", CreatedAt: time.Unix(100, 0).UTC(),
	}
}

func TestDecisionProtocolLedgerAssignsMonotonicCanonicalRecordSeq(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	a, err := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-1"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-2"))
	if err != nil {
		t.Fatal(err)
	}
	if a.CanonicalRecordSeq != 1 || b.CanonicalRecordSeq != 2 {
		t.Fatalf("seqs=%d,%d", a.CanonicalRecordSeq, b.CanonicalRecordSeq)
	}
	if hw, err := store.CurrentHighWater(ctx); err != nil || hw != 2 {
		t.Fatalf("hw=%d err=%v", hw, err)
	}
}

func TestDecisionProtocolCutListsOnlyEpisodeRecordsAtOrBelowHighWater(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	first, _ := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-1"))
	_, _ = store.AppendRecord(ctx, decisionprotocol.RecordCandidate, decisionprotocol.DecisionCandidate{CandidateID: "cand-1", EpisodeID: "ep-1", SemanticClaim: "x", DeclaredByActorRef: "actor", DeclaredAt: time.Unix(101, 0).UTC()})
	_, _ = store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-2"))
	got, err := store.ListEpisodeRecords(ctx, "ep-1", first.CanonicalRecordSeq)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CanonicalRecordSeq != first.CanonicalRecordSeq {
		t.Fatalf("records=%#v", got)
	}
}

func TestDecisionProtocolLedgerDoesNotUseObservationChangeSeq(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	one, err := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-1"))
	if err != nil {
		t.Fatal(err)
	}
	if one.CanonicalRecordSeq != 1 {
		t.Fatalf("first seq=%d", one.CanonicalRecordSeq)
	}
	// The authority is the dedicated DP high-water file, not any observation/event counter.
	path := filepath.Join(r.root, "decision_protocol", "ledger", "high_water.json")
	var hw struct {
		SchemaVersion            int                        `json:"schema_version"`
		CanonicalRecordHighWater decisionprotocol.RecordSeq `json:"canonical_record_high_water"`
	}
	if err := readStrict(path, &hw); err != nil {
		t.Fatal(err)
	}
	if hw.CanonicalRecordHighWater != 1 {
		t.Fatalf("dp high-water=%d", hw.CanonicalRecordHighWater)
	}
}

func TestDecisionProtocolLedgerRecoveryRepairsContiguousRecordBeforeAllocatingNext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, err := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	// Simulate record 2 durable but high-water still 1. Index may be missing.
	body, _ := json.Marshal(dpEpisode("ep-2"))
	env := decisionprotocol.CanonicalRecordEnvelope{SchemaVersion: 1, CanonicalRecordSeq: 2, Kind: decisionprotocol.RecordEpisode, Body: body}
	if res := r.writer.Create(filepath.Join(root, "decision_protocol", "ledger", "records", "00000000000000000002.json"), env); res.Err != nil {
		t.Fatal(res.Err)
	}
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	if hw, err := store2.CurrentHighWater(ctx); err != nil || hw != 2 {
		t.Fatalf("recovered hw=%d err=%v", hw, err)
	}
	next, err := store2.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-3"))
	if err != nil {
		t.Fatal(err)
	}
	if next.CanonicalRecordSeq != 3 {
		t.Fatalf("next seq=%d", next.CanonicalRecordSeq)
	}
}

func TestDecisionProtocolLedgerFailsClosedWhenHighWaterRecordMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, err := store.AppendRecord(ctx, decisionprotocol.RecordEpisode, dpEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "decision_protocol", "ledger", "records", "00000000000000000001.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, r.limits); err == nil {
		t.Fatal("missing high-water record accepted")
	}
}
