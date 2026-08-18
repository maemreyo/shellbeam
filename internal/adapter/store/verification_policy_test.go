package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

func openVerificationRepo(t *testing.T, root string) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 32 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func testRepoID() workspace.RepositoryID {
	return workspace.RepositoryID("repo_01K00000000000000000000000")
}
func testPolicySnapshot(t *testing.T, id string) core.PolicySnapshot {
	t.Helper()
	p := core.PolicyContent{SchemaVersion: 1, PolicyID: id}
	d, err := core.PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return core.PolicySnapshot{RepositoryID: string(testRepoID()), Digest: d, Content: p}
}
func testActivationCommit(t *testing.T, id string, snap core.PolicySnapshot, prev, proposalGen, activationGen string) core.PolicyActivationCommit {
	t.Helper()
	return core.PolicyActivationCommit{Intent: core.PolicyActivationIntent{ActivationID: id, RepositoryID: string(testRepoID()), PreviousEffectiveDigest: prev, ProposedPolicyDigest: snap.Digest, ProposalGeneration: proposalGen, Authority: "explicit_caller", Actor: "tester"}, ProposalOrigin: core.ProposalRepositoryAuthored, ActivationGeneration: activationGen}
}
func gen(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return "gen_" + string(b)
}

func TestPolicySnapshotCreateOnceAndRestartPersistence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	s := testPolicySnapshot(t, "p1")
	created, err := r.PutPolicySnapshot(context.Background(), s)
	if err != nil || !created {
		t.Fatalf("put created=%v err=%v", created, err)
	}
	created, err = r.PutPolicySnapshot(context.Background(), s)
	if err != nil || created {
		t.Fatalf("replay created=%v err=%v", created, err)
	}
	r2 := openVerificationRepo(t, root)
	got, ok, err := r2.LoadPolicySnapshot(context.Background(), testRepoID(), s.Digest)
	if err != nil || !ok || got.Digest != s.Digest {
		t.Fatalf("restart got=%#v ok=%v err=%v", got, ok, err)
	}
}

func TestActivationRetryPreservesFirstTimestampAndNeverRollsBackIndex(t *testing.T) {
	r := openVerificationRepo(t, filepath.Join(t.TempDir(), "state"))
	now := time.Unix(100, 0).UTC()
	r.now = func() time.Time { return now }
	s1 := testPolicySnapshot(t, "p1")
	s2 := testPolicySnapshot(t, "p2")
	for _, s := range []core.PolicySnapshot{s1, s2} {
		if _, err := r.PutPolicySnapshot(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	c1 := testActivationCommit(t, "act_one", s1, "absent", gen('1'), gen('2'))
	first, err := r.ActivatePolicyCAS(context.Background(), c1)
	if err != nil || !first.Created || !first.Effective {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	now = time.Unix(200, 0).UTC()
	replay, err := r.ActivatePolicyCAS(context.Background(), c1)
	if err != nil || !replay.Replayed || !replay.Effective || !replay.Record.ActivatedAt.Equal(first.Record.ActivatedAt) || replay.Record.ActivationGeneration != first.Record.ActivationGeneration {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	c2 := testActivationCommit(t, "act_two", s2, s1.Digest, gen('2'), gen('3'))
	second, err := r.ActivatePolicyCAS(context.Background(), c2)
	if err != nil || !second.Effective {
		t.Fatal(err)
	}
	old, err := r.ActivatePolicyCAS(context.Background(), c1)
	if err != nil || !old.Replayed || old.Effective {
		t.Fatalf("old=%#v err=%v", old, err)
	}
	current, ok, err := r.CurrentActivation(context.Background(), testRepoID())
	if err != nil || !ok || current.ActivationID != "act_two" {
		t.Fatalf("current=%#v ok=%v err=%v", current, ok, err)
	}
}

func TestActivationSameIDDifferentIntentConflicts(t *testing.T) {
	r := openVerificationRepo(t, filepath.Join(t.TempDir(), "state"))
	s := testPolicySnapshot(t, "p1")
	_, _ = r.PutPolicySnapshot(context.Background(), s)
	c := testActivationCommit(t, "act_same", s, "absent", gen('1'), gen('2'))
	if _, err := r.ActivatePolicyCAS(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.Intent.Actor = "other"
	if _, err := r.ActivatePolicyCAS(context.Background(), c); err == nil {
		t.Fatal("different intent replay accepted")
	}
}

func TestActivationOrphanRecoveryOnlyFromExpectedPredecessor(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	s := testPolicySnapshot(t, "p1")
	_, _ = r.PutPolicySnapshot(context.Background(), s)
	c := testActivationCommit(t, "act_orphan", s, "absent", gen('1'), gen('2'))
	res, err := r.ActivatePolicyCAS(context.Background(), c)
	if err != nil || !res.Effective {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "verification", "effective", string(testRepoID())+".json")); err != nil {
		t.Fatal(err)
	}
	res, err = r.ActivatePolicyCAS(context.Background(), c)
	if err != nil || !res.Replayed || !res.Effective {
		t.Fatalf("recovery=%#v err=%v", res, err)
	}
}

func TestWaiverRetryPreservesFirstTimestamp(t *testing.T) {
	r := openVerificationRepo(t, filepath.Join(t.TempDir(), "state"))
	now := time.Unix(100, 0).UTC()
	r.now = func() time.Time { return now }
	intent := core.VerificationWaiverIntent{WaiverID: "wv_one", RepositoryID: string(testRepoID()), PolicyDigest: "pol_" + string(make([]byte, 64)), RuleID: "r1", Phase: core.PhaseCheckpoint, Generation: gen('1'), Authority: "explicit_caller", Actor: "tester", Reason: "accepted risk"}
	intent.PolicyDigest = testPolicySnapshot(t, "x").Digest
	first, err := r.PutWaiver(context.Background(), intent)
	if err != nil || !first.Created || !first.Active {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	now = time.Unix(200, 0).UTC()
	again, err := r.PutWaiver(context.Background(), intent)
	if err != nil || !again.Replayed || !again.Record.CreatedAt.Equal(first.Record.CreatedAt) {
		t.Fatalf("again=%#v err=%v", again, err)
	}
	changed := intent
	changed.Reason = "different"
	if _, err := r.PutWaiver(context.Background(), changed); err == nil {
		t.Fatal("changed waiver intent accepted")
	}
}

func TestWaiverRevocationRetryPreservesFirstTimestamp(t *testing.T) {
	r := openVerificationRepo(t, filepath.Join(t.TempDir(), "state"))
	now := time.Unix(100, 0).UTC()
	r.now = func() time.Time { return now }
	waiver := core.VerificationWaiverIntent{WaiverID: "wv_one", RepositoryID: string(testRepoID()), PolicyDigest: testPolicySnapshot(t, "x").Digest, RuleID: "r1", Phase: core.PhaseCheckpoint, Generation: gen('1'), Authority: "explicit_caller", Actor: "tester", Reason: "accepted risk"}
	if _, err := r.PutWaiver(context.Background(), waiver); err != nil {
		t.Fatal(err)
	}
	intent := core.WaiverRevocationIntent{RepositoryID: string(testRepoID()), WaiverID: "wv_one", Authority: "explicit_caller", Actor: "tester"}
	first, err := r.PutWaiverRevocation(context.Background(), intent)
	if err != nil || !first.Created {
		t.Fatal(err)
	}
	now = time.Unix(200, 0).UTC()
	again, err := r.PutWaiverRevocation(context.Background(), intent)
	if err != nil || !again.Replayed || !again.Record.RevokedAt.Equal(first.Record.RevokedAt) {
		t.Fatalf("again=%#v err=%v", again, err)
	}
}

func TestPolicyActivationIsImmutableAuditableAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	s := testPolicySnapshot(t, "p1")
	_, _ = r.PutPolicySnapshot(context.Background(), s)
	c := testActivationCommit(t, "act_audit", s, "absent", gen('1'), gen('2'))
	res, err := r.ActivatePolicyCAS(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if res.Record.IntentFingerprint == "" || res.Record.ActivatedAt.IsZero() {
		t.Fatalf("record=%#v", res.Record)
	}
	path := filepath.Join(root, "verification", "activations", string(testRepoID()), "act_audit.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestWaiverRevocationIsRepositoryScoped(t *testing.T) {
	r := openVerificationRepo(t, filepath.Join(t.TempDir(), "state"))
	repoA := testRepoID()
	repoB := workspace.RepositoryID("repo_01K00000000000000000000001")
	policy := testPolicySnapshot(t, "x").Digest
	for _, repo := range []workspace.RepositoryID{repoA, repoB} {
		intent := core.VerificationWaiverIntent{WaiverID: "wv_same", RepositoryID: string(repo), PolicyDigest: policy, RuleID: "r1", Phase: core.PhaseCheckpoint, Generation: gen('1'), Authority: "explicit_caller", Actor: "tester", Reason: "accepted risk"}
		if _, err := r.PutWaiver(context.Background(), intent); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.PutWaiverRevocation(context.Background(), core.WaiverRevocationIntent{RepositoryID: string(repoB), WaiverID: "wv_same", Authority: "explicit_caller", Actor: "tester"}); err != nil {
		t.Fatal(err)
	}
	_, revokedA, err := r.FindWaiverRevocation(context.Background(), repoA, "wv_same")
	if err != nil || revokedA {
		t.Fatalf("repo A revoked=%v err=%v", revokedA, err)
	}
	_, revokedB, err := r.FindWaiverRevocation(context.Background(), repoB, "wv_same")
	if err != nil || !revokedB {
		t.Fatalf("repo B revoked=%v err=%v", revokedB, err)
	}
}

func TestActivationCrashAfterRecordBeforeIndexRecoversWithoutRestamp(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	s := testPolicySnapshot(t, "p1")
	_, _ = r.PutPolicySnapshot(context.Background(), s)
	now := time.Unix(100, 0).UTC()
	r.now = func() time.Time { return now }
	c := testActivationCommit(t, "act_crash", s, "absent", gen('1'), gen('2'))
	r.writer = atomicWriter{fail: func(point string) error {
		if point == "replace.rename" {
			return errors.New("injected index crash")
		}
		return nil
	}}
	if _, err := r.ActivatePolicyCAS(context.Background(), c); err == nil {
		t.Fatal("injected index failure did not fail")
	}
	stored, found, err := r.FindActivation(context.Background(), testRepoID(), "act_crash")
	if err != nil || !found {
		t.Fatalf("stored found=%v err=%v", found, err)
	}
	now = time.Unix(200, 0).UTC()
	r.writer = atomicWriter{}
	got, err := r.ActivatePolicyCAS(context.Background(), c)
	if err != nil || !got.Replayed || !got.Effective || !got.Record.ActivatedAt.Equal(stored.ActivatedAt) {
		t.Fatalf("recovery=%#v err=%v", got, err)
	}
}

func TestCurrentActivationFailsClosedOnCorruptOrMissingIndexTarget(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	repo := testRepoID()
	if err := os.WriteFile(effectivePath(r, repo), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.CurrentActivation(context.Background(), repo); err == nil {
		t.Fatal("corrupt index accepted")
	}
	idx := verificationEffectiveIndex{SchemaVersion: 1, ActivationID: "act_missing", PolicyDigest: testPolicySnapshot(t, "p1").Digest}
	if got := atomicJSON(effectivePath(r, repo), idx); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, _, err := r.CurrentActivation(context.Background(), repo); err == nil {
		t.Fatal("missing activation target accepted")
	}
}

func TestPolicySnapshotExistingIdentityWithDifferentAuthorityBytesConflicts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openVerificationRepo(t, root)
	s := testPolicySnapshot(t, "p1")
	if _, err := r.ensureVerificationRepoDir("policies", testRepoID()); err != nil {
		t.Fatal(err)
	}
	other := s
	other.RepositoryID = "repo_01K00000000000000000000001"
	if got := atomicCreateJSON(policyPath(r, testRepoID(), s.Digest), other); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := r.PutPolicySnapshot(context.Background(), s); err == nil {
		t.Fatal("conflicting immutable policy bytes accepted")
	}
}
