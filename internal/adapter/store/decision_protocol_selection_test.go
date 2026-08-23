package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func selectionFixture(t *testing.T, root, episodeID string) (*Repository, *DecisionProtocolStore, dp.Episode, dp.Candidate) {
	t.Helper()
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	episode := dpStoredEpisode(episodeID)
	if _, _, err := store.CreateEpisode(ctx, episode); err != nil {
		t.Fatal(err)
	}
	candidate := dpCandidate("cand-selection-"+episodeID, episodeID, "")
	candidate.DeclaredAt = time.Unix(2, 0).UTC()
	if _, _, err := store.CreateCandidate(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	return r, store, episode, candidate
}

func selectionIntentAndCommit(t *testing.T, episode dp.Episode, candidate dp.Candidate, key string) (dp.SelectionCommitIntent, dp.SelectionCommit) {
	t.Helper()
	intent := dp.SelectionCommitIntent{EpisodeID: episode.EpisodeID, CandidateID: candidate.CandidateID, ActorRef: "actor", PolicyDigest: episode.PolicyBinding.PolicyDigest, ProjectionDigest: "proj_" + strings.Repeat("d", 64), SourceGeneration: episode.Baseline.SourceGeneration}
	fp, err := dp.SelectionIntentFingerprint(intent)
	if err != nil {
		t.Fatal(err)
	}
	commit := dp.SelectionCommit{CommitID: "commit-" + key, EpisodeID: episode.EpisodeID, CandidateID: candidate.CandidateID, PolicyDigest: intent.PolicyDigest, ProjectionDigest: intent.ProjectionDigest, SourceGeneration: intent.SourceGeneration, IdempotencyKey: key, SemanticIntentFingerprint: fp, CommittedByActorRef: intent.ActorRef, CommittedAt: time.Unix(10, 0).UTC()}
	if err := commit.Validate(); err != nil {
		t.Fatal(err)
	}
	return intent, commit
}

func unresolvedClosure(ep dp.Episode, digest, reason string) dp.DecisionClosure {
	return dp.DecisionClosure{EpisodeID: ep.EpisodeID, Kind: dp.ClosureUnresolved, Reason: reason, UnresolvedDimensions: []string{"unknown"}, ActorRef: "actor", ProjectionDigest: digest, ClosedAt: time.Unix(20, 0).UTC()}
}

func TestSelectionProposalPersistsWithoutTerminalAuthority(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-proposal-store")
	proposal := dp.SelectionProposal{ProposalID: "proposal-store", EpisodeID: ep.EpisodeID, CandidateID: cand.CandidateID, ActorRef: "actor", Rationale: "prefer", CreatedAt: time.Unix(3, 0).UTC()}
	if _, created, err := store.RecordSelectionProposal(context.Background(), proposal); err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if _, created, err := store.RecordSelectionProposal(context.Background(), proposal); err != nil || created {
		t.Fatalf("replay created=%v err=%v", created, err)
	}
	terminal, err := store.repository.findDecisionEpisodeTerminalLockedForTest(ep.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if terminal {
		t.Fatal("proposal terminalized episode")
	}
}

func TestSelectionCommitIdempotentAcrossRepositoryReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	_, store, ep, cand := selectionFixture(t, root, "ep-idem")
	intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-reopen")
	first, created, err := store.CommitSelectionCAS(context.Background(), intent, commit)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	retry := commit
	retry.CommitID = "commit-retry"
	retry.CommittedAt = time.Unix(99, 0).UTC()
	got, created, err := store2.CommitSelectionCAS(context.Background(), intent, retry)
	if err != nil || created || got.CommitID != first.CommitID {
		t.Fatalf("got=%#v created=%v err=%v", got, created, err)
	}
}

func TestSelectionCommitSameKeyDifferentSemanticFingerprintConflicts(t *testing.T) {
	_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-idem-conflict")
	intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-conflict")
	if _, _, err := store.CommitSelectionCAS(context.Background(), intent, commit); err != nil {
		t.Fatal(err)
	}
	otherIntent := intent
	otherIntent.ActorRef = "actor-2"
	otherFP, err := dp.SelectionIntentFingerprint(otherIntent)
	if err != nil {
		t.Fatal(err)
	}
	other := commit
	other.SemanticIntentFingerprint = otherFP
	other.CommittedByActorRef = "actor-2"
	if _, _, err := store.CommitSelectionCAS(context.Background(), otherIntent, other); err == nil {
		t.Fatal("different semantic intent accepted")
	} else if r, ok := dp.ReasonOf(err); !ok || r != dp.ReasonIdempotencyConflict {
		t.Fatalf("err=%v reason=%s", err, r)
	}
}

func TestSelectionCommitAndCloseAreMutuallyExclusive(t *testing.T) {
	t.Run("commit_then_close", func(t *testing.T) {
		_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-commit-close")
		intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-cc")
		if _, _, err := store.CommitSelectionCAS(context.Background(), intent, commit); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CloseEpisodeCAS(context.Background(), unresolvedClosure(ep, commit.ProjectionDigest, "later")); err == nil {
			t.Fatal("close accepted after commit")
		} else if r, ok := dp.ReasonOf(err); !ok || r != dp.ReasonEpisodeTerminalConflict {
			t.Fatalf("err=%v reason=%s", err, r)
		}
	})
	t.Run("close_then_commit", func(t *testing.T) {
		_, store, ep, cand := selectionFixture(t, filepath.Join(t.TempDir(), "state"), "ep-close-commit")
		intent, commit := selectionIntentAndCommit(t, ep, cand, "idem-cctwo")
		if _, _, err := store.CloseEpisodeCAS(context.Background(), unresolvedClosure(ep, commit.ProjectionDigest, "stop")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CommitSelectionCAS(context.Background(), intent, commit); err == nil {
			t.Fatal("commit accepted after close")
		} else if r, ok := dp.ReasonOf(err); !ok || r != dp.ReasonEpisodeTerminalConflict {
			t.Fatalf("err=%v reason=%s", err, r)
		}
	})
}

func (r *Repository) findDecisionEpisodeTerminalLockedForTest(ep dp.EpisodeID) (bool, error) {
	r.decisionProtocolMu.Lock()
	defer r.decisionProtocolMu.Unlock()
	if err := r.recoverDecisionProtocolLocked(); err != nil {
		return false, err
	}
	terminal, err := r.findDecisionEpisodeTerminalLocked(ep)
	return terminal.commit != nil || terminal.closure != nil, err
}
