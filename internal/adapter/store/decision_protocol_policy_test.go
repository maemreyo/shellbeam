package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

func dpPolicySnapshot(t *testing.T, repo, id string, kinds ...decisionprotocol.EpisodeKind) decisionprotocol.PolicySnapshot {
	t.Helper()
	content := decisionprotocol.PolicyContent{PolicyID: id, EpisodeKinds: kinds, OverridePolicy: decisionprotocol.OverridePolicy{Allowed: false}}
	digest, err := decisionprotocol.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	return decisionprotocol.PolicySnapshot{SchemaVersion: 1, RepositoryID: repo, PolicyDigest: digest, Content: content}
}
func dpGen(c byte) string { return "gen_" + strings.Repeat(string(c), 64) }
func dpActivationCommit(id string, s decisionprotocol.PolicySnapshot, previous string) decisionprotocol.PolicyActivationCommit {
	return decisionprotocol.PolicyActivationCommit{Intent: decisionprotocol.PolicyActivationIntent{ActivationID: id, RepositoryID: s.RepositoryID, PreviousEffectivePolicyDigest: previous, ProposedPolicyDigest: s.PolicyDigest, ProposalGeneration: dpGen('a'), Authority: decisionprotocol.AuthorityExplicitCaller, ActorRef: "actor"}, ActivationGeneration: dpGen('b')}
}

func TestDecisionPolicySnapshotCreatesCanonicalLedgerRecordAndSecondaryIndex(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	created, err := store.PutPolicySnapshot(ctx, s)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil || hw != 1 {
		t.Fatalf("high-water=%d err=%v", hw, err)
	}
	rec, ok, err := store.LoadRecord(ctx, 1)
	if err != nil || !ok || rec.Kind != decisionprotocol.RecordPolicySnapshot {
		t.Fatalf("record=%#v ok=%v err=%v", rec, ok, err)
	}
	got, ok, err := store.LoadPolicySnapshot(ctx, "repo-a", s.PolicyDigest)
	if err != nil || !ok || got.PolicyDigest != s.PolicyDigest {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestDecisionPolicyActivationCreatesCanonicalLedgerRecordAndSecondaryIndexes(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	r.now = func() time.Time { return time.Unix(500, 0).UTC() }
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	_, _ = store.PutPolicySnapshot(ctx, s)
	res, err := store.ActivatePolicyCAS(ctx, dpActivationCommit("act-1", s, "absent"))
	if err != nil || !res.Created || !res.Effective {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil || hw != 2 {
		t.Fatalf("high-water=%d err=%v", hw, err)
	}
	rec, ok, err := store.LoadRecord(ctx, 2)
	if err != nil || !ok || rec.Kind != decisionprotocol.RecordPolicyActivation {
		t.Fatalf("record=%#v ok=%v err=%v", rec, ok, err)
	}
	ps, act, ok, err := store.CurrentEffectivePolicy(ctx, "repo-a", decisionprotocol.EpisodeDiagnosis)
	if err != nil || !ok || ps.PolicyDigest != s.PolicyDigest || act.ActivationID != "act-1" {
		t.Fatalf("ps=%#v act=%#v ok=%v err=%v", ps, act, ok, err)
	}
}

func TestDecisionPolicySnapshotConflictingDigestBodyFails(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	if _, err := store.PutPolicySnapshot(ctx, s); err != nil {
		t.Fatal(err)
	}
	bad := s
	bad.Content.PolicyID = "different"
	if _, err := store.PutPolicySnapshot(ctx, bad); err == nil {
		t.Fatal("conflicting snapshot accepted")
	}
}

func TestDecisionPolicyActivationFirstAndReplacementUseExplicitCallerCAS(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s1 := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	s2 := dpPolicySnapshot(t, "repo-a", "p2", decisionprotocol.EpisodeDiagnosis)
	_, _ = store.PutPolicySnapshot(ctx, s1)
	_, _ = store.PutPolicySnapshot(ctx, s2)
	one, err := store.ActivatePolicyCAS(ctx, dpActivationCommit("act-1", s1, "absent"))
	if err != nil || !one.Effective {
		t.Fatal(err)
	}
	twoC := dpActivationCommit("act-2", s2, s1.PolicyDigest)
	twoC.Intent.ProposalGeneration = dpGen('c')
	twoC.ActivationGeneration = dpGen('d')
	two, err := store.ActivatePolicyCAS(ctx, twoC)
	if err != nil || !two.Effective {
		t.Fatal(err)
	}
	stale := dpActivationCommit("act-stale", s1, "absent")
	if _, err := store.ActivatePolicyCAS(ctx, stale); err == nil {
		t.Fatal("stale CAS accepted")
	}
}

func TestDecisionPolicyActivationRequiresGenDigestAndExplicitPreviousDigestSentinel(t *testing.T) {
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	c := dpActivationCommit("act-1", s, "absent")
	c.Intent.ProposalGeneration = "1"
	if err := c.Validate(); err == nil {
		t.Fatal("scalar proposal generation accepted")
	}
	c = dpActivationCommit("act-1", s, "")
	if err := c.Validate(); err == nil {
		t.Fatal("empty previous digest accepted")
	}
}

func TestCurrentEffectivePolicyFiltersByEpisodeKind(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	_, _ = store.PutPolicySnapshot(ctx, s)
	_, _ = store.ActivatePolicyCAS(ctx, dpActivationCommit("act-1", s, "absent"))
	if _, _, ok, err := store.CurrentEffectivePolicy(ctx, "repo-a", decisionprotocol.EpisodePlanSelection); err != nil || ok {
		t.Fatalf("wrong kind ok=%v err=%v", ok, err)
	}
}

func TestDecisionPolicyActivationSecondaryRetainsRebuildableIntentFingerprint(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	if _, err := store.PutPolicySnapshot(ctx, s); err != nil {
		t.Fatal(err)
	}
	commit := dpActivationCommit("act-1", s, "absent")
	if _, err := store.ActivatePolicyCAS(ctx, commit); err != nil {
		t.Fatal(err)
	}
	var got decisionProtocolActivationMaterialization
	if err := readStrict(r.decisionProtocolActivationPath("repo-a", "act-1"), &got); err != nil {
		t.Fatal(err)
	}
	wantFP, err := decisionprotocol.PolicyActivationIntentFingerprint(commit.Intent)
	if err != nil {
		t.Fatal(err)
	}
	if got.IntentFingerprint != wantFP || got.PreviousEffectivePolicyDigest != "absent" || got.Record.ActivationID != "act-1" || got.CanonicalRecordSeq != 2 {
		t.Fatalf("materialization=%#v", got)
	}
}

func TestDecisionPolicyActivationReplayPreservesCanonicalRecordAndRejectsChangedIntent(t *testing.T) {
	r := openDecisionProtocolRepo(t, filepath.Join(t.TempDir(), "state"))
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	r.now = func() time.Time { return now }
	s := dpPolicySnapshot(t, "repo-a", "p1", decisionprotocol.EpisodeDiagnosis)
	if _, err := store.PutPolicySnapshot(ctx, s); err != nil {
		t.Fatal(err)
	}
	commit := dpActivationCommit("act-1", s, "absent")
	first, err := store.ActivatePolicyCAS(ctx, commit)
	if err != nil || !first.Created || !first.Effective {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	now = time.Unix(200, 0).UTC()
	replay, err := store.ActivatePolicyCAS(ctx, commit)
	if err != nil || !replay.Replayed || !replay.Effective || !replay.Record.ActivatedAt.Equal(first.Record.ActivatedAt) || replay.Record.ActivationGeneration != first.Record.ActivationGeneration {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	changed := commit
	changed.Intent.ActorRef = "other"
	if _, err := store.ActivatePolicyCAS(ctx, changed); err == nil {
		t.Fatal("same activation id with changed intent accepted")
	}
}
