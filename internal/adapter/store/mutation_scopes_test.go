package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	scope "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const mutationWorkspace = workspace.WorkspaceID("ws_01K00000000000000000000000")

func mutationScopeFixture(id, mutationID string, declared time.Time, ttl time.Duration) (scope.Scope, scope.ScopeIdentity, scope.MutationReceipt) {
	s := scope.Scope{SchemaVersion: scope.SchemaVersion, ScopeID: id, ActivityID: "activity-a", WorkspaceID: mutationWorkspace, Mode: scope.ModeMutate, Paths: []string{"src/**"}, DeclaredAt: declared.UTC(), ExpiresAt: declared.Add(ttl).UTC(), RevisionID: mutationID}
	identity := scope.ScopeIdentity{SchemaVersion: scope.SchemaVersion, ScopeID: id, ActivityID: s.ActivityID, WorkspaceID: s.WorkspaceID, BoundAt: declared.UTC()}
	receipt := scope.MutationReceipt{SchemaVersion: scope.SchemaVersion, MutationID: mutationID, RequestFingerprint: strings.Repeat("a", 64), Result: scope.ResultSet, ScopeID: id, CommittedAt: declared.UTC(), ExpiresAt: s.ExpiresAt}
	return s, identity, receipt
}

func openMutationScopeRepo(t *testing.T, limits Limits) (*Repository, string) {
	t.Helper()
	if limits.MaxSessions == 0 {
		limits.MaxSessions = 8
	}
	if limits.MaxSessionOutput == 0 {
		limits.MaxSessionOutput = 1 << 20
	}
	if limits.MaxTotalState == 0 {
		limits.MaxTotalState = 16 << 20
	}
	if limits.ControlReserve == 0 {
		limits.ControlReserve = 1024
	}
	root := filepath.Join(t.TempDir(), "state")
	r, err := Open(root, limits)
	if err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestMutationScopeStoreRoundTripAndReplay(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil || got.Durability != app.DurableChange {
		t.Fatalf("commit=%#v", got)
	}
	loaded, found, err := r.LoadMutationScope(context.Background(), s.ScopeID)
	if err != nil || !found || loaded.RevisionID != "mutation-1" || !loaded.ExpiresAt.Equal(s.ExpiresAt) {
		t.Fatalf("scope=%#v found=%v err=%v", loaded, found, err)
	}
	bound, found, err := r.LoadMutationScopeIdentity(context.Background(), s.ScopeID)
	if err != nil || !found || bound.ActivityID != s.ActivityID || bound.WorkspaceID != s.WorkspaceID {
		t.Fatalf("identity=%#v found=%v err=%v", bound, found, err)
	}
	storedReceipt, found, err := r.LoadMutationReceipt(context.Background(), receipt.MutationID)
	if err != nil || !found || storedReceipt != receipt {
		t.Fatalf("receipt=%#v found=%v err=%v", storedReceipt, found, err)
	}
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatalf("replay=%#v", got)
	}
	replay, _, _ := r.LoadMutationScope(context.Background(), s.ScopeID)
	if !replay.ExpiresAt.Equal(s.ExpiresAt) {
		t.Fatalf("replay extended ttl: %v -> %v", s.ExpiresAt, replay.ExpiresAt)
	}
}

func TestMutationScopeStoreRejectsMutationMetadataAndBindingConflicts(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	changed := receipt
	changed.RequestFingerprint = strings.Repeat("b", 64)
	got := r.CommitMutationScopeSet(context.Background(), s, identity, changed)
	var f *failure.Failure
	if !errors.As(got.Err, &f) || f.Code != failure.MutationMetadataConflict {
		t.Fatalf("metadata conflict=%#v", got)
	}
	s2, identity2, receipt2 := mutationScopeFixture("scope-a", "mutation-2", now.Add(time.Second), scope.DefaultTTL)
	s2.ActivityID = "activity-b"
	identity2.ActivityID = "activity-b"
	receipt2.RequestFingerprint = strings.Repeat("c", 64)
	got = r.CommitMutationScopeSet(context.Background(), s2, identity2, receipt2)
	if !errors.As(got.Err, &f) || f.Code != failure.MutationScopeBindingConflict {
		t.Fatalf("binding conflict=%#v", got)
	}
}

func TestMutationScopeStoreOldRetryCannotRollbackNewRevision(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	first, identity, firstReceipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), first, identity, firstReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	second, _, secondReceipt := mutationScopeFixture("scope-a", "mutation-2", now.Add(time.Second), scope.DefaultTTL)
	second.Paths = []string{"src/auth/**"}
	secondReceipt.RequestFingerprint = strings.Repeat("b", 64)
	if got := r.CommitMutationScopeSet(context.Background(), second, identity, secondReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	if got := r.CommitMutationScopeSet(context.Background(), first, identity, firstReceipt); got.Err != nil {
		t.Fatalf("old retry=%#v", got)
	}
	active, found, err := r.LoadMutationScope(context.Background(), "scope-a")
	if err != nil || !found || active.RevisionID != "mutation-2" || active.Paths[0] != "src/auth/**" {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
}

func TestMutationScopeStoreReleaseAndAbsentReleaseAreDurable(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, setReceipt := mutationScopeFixture("scope-a", "mutation-set", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, setReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	release := scope.MutationReceipt{SchemaVersion: 1, MutationID: "mutation-release", RequestFingerprint: strings.Repeat("b", 64), Result: scope.ResultReleased, ScopeID: "scope-a", CommittedAt: now.Add(time.Second)}
	if got := r.CommitMutationScopeRelease(context.Background(), "scope-a", release); got.Err != nil {
		t.Fatalf("release=%#v", got)
	}
	if _, found, err := r.LoadMutationScope(context.Background(), "scope-a"); err != nil || found {
		t.Fatalf("released found=%v err=%v", found, err)
	}
	if _, found, err := r.LoadMutationScopeIdentity(context.Background(), "scope-a"); err != nil || !found {
		t.Fatalf("identity after release found=%v err=%v", found, err)
	}
	noOp := scope.MutationReceipt{SchemaVersion: 1, MutationID: "mutation-noop", RequestFingerprint: strings.Repeat("c", 64), Result: scope.ResultAlreadyAbsent, ScopeID: "scope-a", CommittedAt: now.Add(2 * time.Second)}
	if got := r.CommitMutationScopeRelease(context.Background(), "scope-a", noOp); got.Err != nil {
		t.Fatalf("absent release=%#v", got)
	}
	stored, found, err := r.LoadMutationReceipt(context.Background(), noOp.MutationID)
	if err != nil || !found || stored.Result != scope.ResultAlreadyAbsent {
		t.Fatalf("noop receipt=%#v found=%v err=%v", stored, found, err)
	}
}

func TestMutationScopeStoreConcurrentDuplicateSetCommitsOneState(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
				errs <- got.Err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	active, found, err := r.LoadMutationScope(context.Background(), "scope-a")
	if err != nil || !found || active.RevisionID != "mutation-1" {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
}

func TestMutationScopeStoreExpiryDoesNotConsumeActivityCapacity(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{MaxMutationScopesPerActivity: 1, MaxMutationScopesPerWorkspace: 4})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	first, identity, firstReceipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.MinTTL)
	if got := r.CommitMutationScopeSet(context.Background(), first, identity, firstReceipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	now = now.Add(scope.MinTTL)
	second, identity2, receipt2 := mutationScopeFixture("scope-b", "mutation-2", now, scope.DefaultTTL)
	receipt2.RequestFingerprint = strings.Repeat("b", 64)
	if got := r.CommitMutationScopeSet(context.Background(), second, identity2, receipt2); got.Err != nil {
		t.Fatalf("expired scope consumed capacity: %#v", got)
	}
	listed, err := r.ListMutationScopes(context.Background(), "activity-a", mutationWorkspace)
	if err != nil || len(listed) != 1 || listed[0].ScopeID != "scope-b" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestMutationScopeStoreSurvivesRepositoryReopen(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	reopened, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	reopened.now = func() time.Time { return now }
	active, found, err := reopened.LoadMutationScope(context.Background(), "scope-a")
	if err != nil || !found || active.RevisionID != "mutation-1" {
		t.Fatalf("reopen active=%#v found=%v err=%v", active, found, err)
	}
}

func TestMutationScopeStoreStrictDecodeAndSymlinkIsolation(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	identityPath := filepath.Join(root, "mutation-scopes", "identities", "scope-a.json")
	raw, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	obj["unknown_field"] = "boom"
	bad, _ := json.Marshal(obj)
	if err := os.WriteFile(identityPath, append(bad, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.LoadMutationScopeIdentity(context.Background(), "scope-a"); err == nil {
		t.Fatal("unknown field accepted")
	}

	r2, root2 := openMutationScopeRepo(t, Limits{})
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("sentinel\n"), 0600); err != nil {
		t.Fatal(err)
	}
	identityLink := filepath.Join(root2, "mutation-scopes", "identities", "scope-link.json")
	if err := os.Symlink(outside, identityLink); err != nil {
		t.Fatal(err)
	}
	s2, id2, rec2 := mutationScopeFixture("scope-link", "mutation-2", now, scope.DefaultTTL)
	rec2.RequestFingerprint = strings.Repeat("b", 64)
	if got := r2.CommitMutationScopeSet(context.Background(), s2, id2, rec2); got.Err == nil {
		t.Fatal("symlink identity accepted")
	}
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "sentinel\n" {
		t.Fatalf("write escaped state root: %q", content)
	}
}

func TestMutationScopeStoreAmbiguousStatePublicationReconcilesWithoutTTLChange(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-1", now, scope.DefaultTTL)
	replaceSyncs := 0
	r.writer.fail = func(point string) error {
		if point == "replace.dir_sync" {
			replaceSyncs++
			if replaceSyncs == 2 {
				return errors.New("inject state dir sync")
			}
		}
		return nil
	}
	first := r.CommitMutationScopeSet(context.Background(), s, identity, receipt)
	if first.Err == nil || first.Durability != app.AmbiguousChange {
		t.Fatalf("first=%#v replaceSyncs=%d", first, replaceSyncs)
	}
	r.writer.fail = nil
	if replay := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); replay.Err != nil {
		t.Fatalf("reconcile=%#v", replay)
	}
	active, found, err := r.LoadMutationScope(context.Background(), "scope-a")
	if err != nil || !found || !active.ExpiresAt.Equal(s.ExpiresAt) {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
}

func TestMutationScopeStoreRejectsUnsafeLookupIDs(t *testing.T) {
	r, _ := openMutationScopeRepo(t, Limits{})
	if _, _, err := r.LoadMutationScopeIdentity(context.Background(), "../scope"); err == nil {
		t.Fatal("unsafe scope identity lookup accepted")
	}
	if _, _, err := r.LoadMutationScope(context.Background(), "../scope"); err == nil {
		t.Fatal("unsafe scope lookup accepted")
	}
	if _, _, err := r.LoadMutationReceipt(context.Background(), "../mutation"); err == nil {
		t.Fatal("unsafe mutation receipt lookup accepted")
	}
	if _, err := r.ListMutationScopes(context.Background(), "", workspace.WorkspaceID("../workspace")); err == nil {
		t.Fatal("unsafe workspace lookup accepted")
	}
}

func TestMutationScopeStoreCorruptClaimFailsBeforePendingMutation(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-a", "mutation-corrupt", now, scope.DefaultTTL)
	claimPath := filepath.Join(root, "mutation-scopes", "mutations", receipt.MutationID+".json")
	if err := os.WriteFile(claimPath, []byte("{\"schema_version\":1,\"unknown\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt)
	if got.Err == nil {
		t.Fatal("corrupt mutation claim accepted")
	}
	if _, err := os.Lstat(filepath.Join(root, "mutation-scopes", "pending.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt claim created pending mutation: %v", err)
	}
}

func TestMutationScopeStorePrePublicationFailureLeavesNoClaimedState(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-pre", "mutation-pre", now, scope.DefaultTTL)
	r.writer.fail = func(point string) error {
		if point == "create.write" {
			return errors.New("inject pre-publication write failure")
		}
		return nil
	}
	got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt)
	if got.Err == nil || got.Durability != app.NoDurableChange {
		t.Fatalf("pre-publication result=%#v", got)
	}
	r.writer.fail = nil
	for _, path := range []string{
		filepath.Join(root, "mutation-scopes", "pending.json"),
		filepath.Join(root, "mutation-scopes", "identities", "scope-pre.json"),
		filepath.Join(root, "mutation-scopes", "mutations", "mutation-pre.json"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pre-publication failure left state %s: %v", path, err)
		}
	}
	if active, found, err := r.LoadMutationScope(context.Background(), "scope-pre"); err != nil || found {
		t.Fatalf("active=%#v found=%v err=%v", active, found, err)
	}
}

func TestMutationScopeStoreRejectsTrailingJSON(t *testing.T) {
	r, root := openMutationScopeRepo(t, Limits{})
	now := time.Unix(1000, 0).UTC()
	r.now = func() time.Time { return now }
	s, identity, receipt := mutationScopeFixture("scope-trailing", "mutation-trailing", now, scope.DefaultTTL)
	if got := r.CommitMutationScopeSet(context.Background(), s, identity, receipt); got.Err != nil {
		t.Fatal(got.Err)
	}
	identityPath := filepath.Join(root, "mutation-scopes", "identities", "scope-trailing.json")
	raw, err := os.ReadFile(identityPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("{}\n")...)
	if err := os.WriteFile(identityPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.LoadMutationScopeIdentity(context.Background(), "scope-trailing"); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
