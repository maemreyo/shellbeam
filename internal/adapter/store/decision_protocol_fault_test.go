package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDecisionProtocolPolicySnapshotCrashAfterSecondariesBeforeHighWaterRecovers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", EpisodeDiagnosisForStoreTest())
	r.writer.fail = func(point string) error {
		if point == "replace.rename" {
			return errors.New("injected high-water failure")
		}
		return nil
	}
	if _, err := store.PutPolicySnapshot(ctx, s); err == nil {
		t.Fatal("injected high-water failure did not fail")
	}
	if _, err := os.Stat(r.decisionProtocolRecordPath(1)); err != nil {
		t.Fatalf("canonical record not durable before high-water failure: %v", err)
	}
	if _, err := os.Stat(r.decisionProtocolPolicyPath("repo-a", s.PolicyDigest)); err != nil {
		t.Fatalf("policy secondary not durable before high-water failure: %v", err)
	}
	r.writer.fail = nil
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	if hw, err := store2.CurrentHighWater(ctx); err != nil || hw != 1 {
		t.Fatalf("recovered high-water=%d err=%v", hw, err)
	}
	if got, ok, err := store2.LoadPolicySnapshot(ctx, "repo-a", s.PolicyDigest); err != nil || !ok || got.PolicyDigest != s.PolicyDigest {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestDecisionProtocolActivationCrashAfterSecondariesBeforeHighWaterRecovers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", EpisodeDiagnosisForStoreTest())
	if _, err := store.PutPolicySnapshot(ctx, s); err != nil {
		t.Fatal(err)
	}
	r.writer.fail = func(point string) error {
		if point == "replace.rename" {
			return errors.New("injected high-water failure")
		}
		return nil
	}
	if _, err := store.ActivatePolicyCAS(ctx, dpActivationCommit("act-1", s, "absent")); err == nil {
		t.Fatal("injected high-water failure did not fail")
	}
	if _, err := os.Stat(r.decisionProtocolRecordPath(2)); err != nil {
		t.Fatalf("activation canonical record not durable before high-water failure: %v", err)
	}
	if _, err := os.Stat(r.decisionProtocolActivationPath("repo-a", "act-1")); err != nil {
		t.Fatalf("activation secondary not durable before high-water failure: %v", err)
	}
	if _, err := os.Stat(r.decisionProtocolEffectivePath("repo-a", EpisodeDiagnosisForStoreTest())); err != nil {
		t.Fatalf("effective secondary not durable before high-water failure: %v", err)
	}
	r.writer.fail = nil
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	if hw, err := store2.CurrentHighWater(ctx); err != nil || hw != 2 {
		t.Fatalf("recovered high-water=%d err=%v", hw, err)
	}
	_, act, ok, err := store2.CurrentEffectivePolicy(ctx, "repo-a", EpisodeDiagnosisForStoreTest())
	if err != nil || !ok || act.ActivationID != "act-1" {
		t.Fatalf("act=%#v ok=%v err=%v", act, ok, err)
	}
}

func TestDecisionProtocolRecoveryRebuildsDeletedPolicyAndActivationSecondaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	s := dpPolicySnapshot(t, "repo-a", "p1", EpisodeDiagnosisForStoreTest())
	if _, err := store.PutPolicySnapshot(ctx, s); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ActivatePolicyCAS(ctx, dpActivationCommit("act-1", s, "absent")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.decisionProtocolPolicyRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.decisionProtocolActivationRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.decisionProtocolEffectiveRoot()); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.decisionProtocolPolicyRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.decisionProtocolActivationRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.decisionProtocolEffectiveRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	r2 := openDecisionProtocolRepo(t, root)
	store2 := NewDecisionProtocolStore(r2)
	got, act, ok, err := store2.CurrentEffectivePolicy(ctx, "repo-a", EpisodeDiagnosisForStoreTest())
	if err != nil || !ok || got.PolicyDigest != s.PolicyDigest || act.ActivationID != "act-1" {
		t.Fatalf("got=%#v act=%#v ok=%v err=%v", got, act, ok, err)
	}
}

func TestDecisionProtocolRecoveryRejectsOrphanPolicySecondary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	s := dpPolicySnapshot(t, "repo-a", "p1", EpisodeDiagnosisForStoreTest())
	path := r.decisionProtocolPolicyPath("repo-a", s.PolicyDigest)
	if err := ensurePrivateParent(path); err != nil {
		t.Fatal(err)
	}
	if res := r.writer.Create(path, s); res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := Open(root, r.limits); err == nil {
		t.Fatal("orphan policy secondary accepted without canonical authority")
	}
}

func TestDecisionProtocolRecoveryRejectsGapAboveHighWater(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openDecisionProtocolRepo(t, root)
	store := NewDecisionProtocolStore(r)
	ctx := context.Background()
	if _, err := store.AppendRecord(ctx, RecordEpisodeForStoreTest(), dpEpisode("ep-1")); err != nil {
		t.Fatal(err)
	}
	body := dpEpisode("ep-3")
	encoded, err := canonicalEnvelopeForStoreTest(3, RecordEpisodeForStoreTest(), body)
	if err != nil {
		t.Fatal(err)
	}
	if res := r.writer.Create(r.decisionProtocolRecordPath(3), encoded); res.Err != nil {
		t.Fatal(res.Err)
	}
	if _, err := Open(root, r.limits); err == nil {
		t.Fatal("canonical ledger gap accepted")
	}
}
