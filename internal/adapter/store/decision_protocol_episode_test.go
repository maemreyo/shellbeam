package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func dpStoredEpisode(id string) decisionprotocol.Episode {
	return decisionprotocol.Episode{SchemaVersion: 1, EpisodeID: decisionprotocol.EpisodeID(id), EpisodeKind: decisionprotocol.EpisodeDiagnosis, RepositoryID: "repo-a", WorkspaceID: "ws-a", Baseline: decisionprotocol.EpisodeBaseline{SourceGeneration: "gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, PolicyBinding: decisionprotocol.EpisodePolicyBinding{PolicyID: "p1", PolicyDigest: "pol_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ActivationRef: "act-1"}, CreatedByActorRef: "actor", CreatedAt: time.Unix(1, 0).UTC()}
}
func dpCandidate(id, ep string, parent decisionprotocol.CandidateID) decisionprotocol.Candidate {
	return decisionprotocol.Candidate{CandidateID: decisionprotocol.CandidateID(id), EpisodeID: decisionprotocol.EpisodeID(ep), SemanticClaim: "claim " + id, RevisesCandidateID: parent, DeclaredByActorRef: "actor", DeclaredAt: time.Now().UTC()}
}

func TestReviseCandidateCASAllowsExactlyOneConcurrentReplacement(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	parent := dpCandidate("cand-parent", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, parent); err != nil {
		t.Fatal(err)
	}
	children := []decisionprotocol.Candidate{dpCandidate("cand-a", "ep-1", parent.CandidateID), dpCandidate("cand-b", "ep-1", parent.CandidateID)}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range children {
		child := children[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ReviseCandidateCAS(ctx, parent.CandidateID, child)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
			continue
		}
		if reason, ok := decisionprotocol.ReasonOf(err); ok && reason == decisionprotocol.ReasonCandidateRevisionConflict {
			conflict++
			continue
		}
		t.Fatalf("unexpected revision error: %v", err)
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestCandidateRevisionDoesNotInheritPredictions(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	parent := dpCandidate("cand-parent", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, parent); err != nil {
		t.Fatal(err)
	}
	pred := decisionprotocol.PredictionBinding{PredictionID: "pred-1", EpisodeID: "ep-1", ExperimentID: "exp-1", CandidateID: parent.CandidateID, Role: decisionprotocol.PredictionRequired, Predicate: decisionprotocol.ObservationPredicate{Kind: decisionprotocol.PredicateOperationOutcome, OperationOutcome: &decisionprotocol.OperationOutcomePredicate{ExpectedOutcome: decisionprotocol.OperationSuccess}}, SourceGeneration: dpStoredEpisode("ep-1").Baseline.SourceGeneration, CommittedAt: time.Unix(2, 0).UTC()}
	if _, err := store.AppendRecord(ctx, decisionprotocol.RecordPredictionBinding, pred); err != nil {
		t.Fatal(err)
	}
	child := dpCandidate("cand-child", "ep-1", parent.CandidateID)
	if _, err := store.ReviseCandidateCAS(ctx, parent.CandidateID, child); err != nil {
		t.Fatal(err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListEpisodeRecords(ctx, "ep-1", hw)
	if err != nil {
		t.Fatal(err)
	}
	predictionCandidates := []decisionprotocol.CandidateID{}
	for _, rec := range records {
		if rec.Kind == decisionprotocol.RecordPredictionBinding {
			var got decisionprotocol.PredictionBinding
			if err := json.Unmarshal(rec.Body, &got); err != nil {
				t.Fatal(err)
			}
			predictionCandidates = append(predictionCandidates, got.CandidateID)
		}
	}
	if len(predictionCandidates) != 1 || predictionCandidates[0] != parent.CandidateID {
		t.Fatalf("prediction candidates=%v", predictionCandidates)
	}
}

func TestSiblingAlternativeRequiresCreateNotRevise(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	a := dpCandidate("cand-a", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, a); err != nil {
		t.Fatal(err)
	}
	a2 := dpCandidate("cand-a2", "ep-1", a.CandidateID)
	if _, err := store.ReviseCandidateCAS(ctx, a.CandidateID, a2); err != nil {
		t.Fatal(err)
	}
	b := dpCandidate("cand-b", "ep-1", "")
	if _, created, err := store.CreateCandidate(ctx, b); err != nil || !created {
		t.Fatalf("sibling create created=%v err=%v", created, err)
	}
	if _, err := store.ReviseCandidateCAS(ctx, a.CandidateID, dpCandidate("cand-a3", "ep-1", a.CandidateID)); err == nil {
		t.Fatal("second replacement accepted as sibling")
	}
}

func TestReviseCandidateRejectsMissingParent(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	child := dpCandidate("cand-child", "ep-1", "cand-missing")
	if _, err := store.ReviseCandidateCAS(ctx, "cand-missing", child); err == nil {
		t.Fatal("missing parent accepted")
	}
}

func TestReviseCandidateRejectsCrossEpisodeRevision(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateEpisode(ctx, dpStoredEpisode("ep-2")); err != nil {
		t.Fatal(err)
	}
	parent := dpCandidate("cand-parent", "ep-1", "")
	if _, _, err := store.CreateCandidate(ctx, parent); err != nil {
		t.Fatal(err)
	}
	child := dpCandidate("cand-child", "ep-2", parent.CandidateID)
	if _, err := store.ReviseCandidateCAS(ctx, parent.CandidateID, child); err == nil {
		t.Fatal("cross-episode revision accepted")
	}
}
