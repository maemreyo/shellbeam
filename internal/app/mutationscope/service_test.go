package mutationscope

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const appTestWorkspace = workspace.WorkspaceID("ws_01K00000000000000000000000")

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeStore struct {
	now          func() time.Time
	workspaces   []workspace.Workspace
	scopes       map[string]core.Scope
	identities   map[string][2]string
	receipts     map[string]core.MutationReceipt
	setCalls     int
	releaseCalls int
	setErr       error
	releaseErr   error
	listErr      error
}

func newFakeStore(now func() time.Time) *fakeStore {
	return &fakeStore{
		now:        now,
		workspaces: []workspace.Workspace{{ID: appTestWorkspace}},
		scopes:     map[string]core.Scope{}, identities: map[string][2]string{}, receipts: map[string]core.MutationReceipt{},
	}
}

func (s *fakeStore) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]workspace.Workspace(nil), s.workspaces...), nil
}
func (s *fakeStore) LoadMutationScope(_ context.Context, scopeID string) (core.Scope, bool, error) {
	got, ok := s.scopes[scopeID]
	if !ok || !s.now().Before(got.ExpiresAt) {
		return core.Scope{}, false, nil
	}
	return got, true, nil
}
func (s *fakeStore) ListMutationScopes(_ context.Context, activityID string, workspaceID workspace.WorkspaceID) ([]core.Scope, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]core.Scope, 0, len(s.scopes))
	for _, item := range s.scopes {
		if item.WorkspaceID != workspaceID || !s.now().Before(item.ExpiresAt) {
			continue
		}
		if activityID != "" && item.ActivityID != activityID {
			continue
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ScopeID < out[j].ScopeID })
	return out, nil
}
func (s *fakeStore) LoadMutationReceipt(_ context.Context, mutationID string) (core.MutationReceipt, bool, error) {
	got, ok := s.receipts[mutationID]
	return got, ok, nil
}
func (s *fakeStore) CommitMutationScopeSet(_ context.Context, want core.Scope, identity core.ScopeIdentity, intent core.MutationReceipt) error {
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	if bound, ok := s.identities[want.ScopeID]; ok && (bound[0] != want.ActivityID || bound[1] != string(want.WorkspaceID)) {
		return failure.New(failure.MutationScopeBindingConflict, map[string]string{"scope_id": want.ScopeID}, nil)
	}
	s.identities[want.ScopeID] = [2]string{identity.ActivityID, string(identity.WorkspaceID)}
	_, active := s.scopes[want.ScopeID]
	if active {
		active = s.now().Before(s.scopes[want.ScopeID].ExpiresAt)
	}
	intent.SetEffect = core.SetEffectCreated
	if active {
		intent.SetEffect = core.SetEffectReplaced
	}
	s.scopes[want.ScopeID] = want
	s.receipts[intent.MutationID] = intent
	return nil
}
func (s *fakeStore) CommitMutationScopeRelease(_ context.Context, scopeID string, intent core.MutationReceipt) error {
	s.releaseCalls++
	if s.releaseErr != nil {
		return s.releaseErr
	}
	if current, ok := s.scopes[scopeID]; ok && s.now().Before(current.ExpiresAt) {
		intent.Result = core.ResultReleased
		delete(s.scopes, scopeID)
	} else {
		intent.Result = core.ResultAlreadyAbsent
	}
	s.receipts[intent.MutationID] = intent
	return nil
}

func setReq(mutationID, scopeID string) SetRequest {
	return SetRequest{
		MutationID: mutationID, ScopeID: scopeID, ActivityID: "activity-a", WorkspaceID: appTestWorkspace,
		Mode: core.ModeMutate, Paths: []string{"tests/auth/**", "src/auth/**"},
	}
}

func failureCode(t *testing.T, err error) failure.Code {
	t.Helper()
	var f *failure.Failure
	if !errors.As(err, &f) {
		t.Fatalf("expected typed failure, got %T %v", err, err)
	}
	return f.Code
}

func TestSetDefaultsCanonicalizesAndExactReplayDoesNotExtendTTL(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)
	req := setReq("mutation-1", "scope-a")

	first, err := svc.Set(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || !first.CurrentRevision || first.Receipt.SetEffect != core.SetEffectCreated || store.setCalls != 1 {
		t.Fatalf("first=%#v calls=%d", first, store.setCalls)
	}
	if first.Scope == nil || first.Scope.RevisionID != req.MutationID {
		t.Fatalf("scope=%#v", first.Scope)
	}
	wantPaths := []string{"src/auth/**", "tests/auth/**"}
	if fmt.Sprint(first.Scope.Paths) != fmt.Sprint(wantPaths) {
		t.Fatalf("paths=%v want=%v", first.Scope.Paths, wantPaths)
	}
	wantExpiry := clock.now.Add(core.DefaultTTL)
	if !first.Scope.ExpiresAt.Equal(wantExpiry) || !first.Receipt.ExpiresAt.Equal(wantExpiry) || first.Receipt.RequestFingerprint == "" {
		t.Fatalf("first ttl/receipt=%#v", first)
	}

	clock.now = clock.now.Add(10 * time.Minute)
	retry := req
	retry.Paths = []string{"src/auth/**", "tests/auth/**"}
	second, err := svc.Set(context.Background(), retry)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || store.setCalls != 1 || second.Receipt.RequestFingerprint != first.Receipt.RequestFingerprint || !second.Receipt.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("retry=%#v calls=%d", second, store.setCalls)
	}

	store.workspaces = nil
	third, err := svc.Set(context.Background(), req)
	if err != nil || !third.Replayed || store.setCalls != 1 {
		t.Fatalf("replay after workspace removal=%#v err=%v calls=%d", third, err, store.setCalls)
	}
}

func TestSetSameMutationDifferentCanonicalRequestConflicts(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)
	req := setReq("mutation-conflict", "scope-a")
	if _, err := svc.Set(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.TTLMS = 60_000
	if _, err := svc.Set(context.Background(), changed); failureCode(t, err) != failure.MutationMetadataConflict {
		t.Fatalf("err=%v", err)
	}
	if store.setCalls != 1 {
		t.Fatalf("setCalls=%d", store.setCalls)
	}
}

func TestSetReplacementAndOldRetryNeverRollBackCurrentRevision(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)
	firstReq := setReq("mutation-old", "scope-a")
	first, err := svc.Set(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	newReq := setReq("mutation-new", "scope-a")
	newReq.Paths = []string{"src/auth/**"}
	newResult, err := svc.Set(context.Background(), newReq)
	if err != nil {
		t.Fatal(err)
	}
	if newResult.Receipt.SetEffect != core.SetEffectReplaced || newResult.Scope == nil || newResult.Scope.RevisionID != "mutation-new" {
		t.Fatalf("new=%#v", newResult)
	}

	oldReplay, err := svc.Set(context.Background(), firstReq)
	if err != nil {
		t.Fatal(err)
	}
	if !oldReplay.Replayed || oldReplay.Receipt.SetEffect != core.SetEffectCreated || oldReplay.CurrentRevision {
		t.Fatalf("old replay=%#v", oldReplay)
	}
	if oldReplay.Scope == nil || oldReplay.Scope.RevisionID != "mutation-new" || store.setCalls != 2 {
		t.Fatalf("current=%#v calls=%d", oldReplay.Scope, store.setCalls)
	}
	if first.Receipt.ExpiresAt != oldReplay.Receipt.ExpiresAt {
		t.Fatal("old replay changed receipt")
	}
}

func TestSetValidatesWorkspaceTTLModeSelectorsAndBinding(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)

	missing := setReq("mutation-missing", "scope-missing")
	store.workspaces = nil
	if _, err := svc.Set(context.Background(), missing); failureCode(t, err) != failure.WorkspaceNotFound {
		t.Fatalf("missing err=%v", err)
	}
	store.workspaces = []workspace.Workspace{{ID: appTestWorkspace}}

	cases := []SetRequest{
		func() SetRequest { r := setReq("mutation-short", "scope-short"); r.TTLMS = 999; return r }(),
		func() SetRequest { r := setReq("mutation-long", "scope-long"); r.TTLMS = 1_800_001; return r }(),
		func() SetRequest { r := setReq("mutation-mode", "scope-mode"); r.Mode = core.Mode("write"); return r }(),
		func() SetRequest {
			r := setReq("mutation-path", "scope-path")
			r.Paths = []string{"../escape"}
			return r
		}(),
	}
	for _, req := range cases {
		if _, err := svc.Set(context.Background(), req); failureCode(t, err) != failure.MutationScopeInvalid {
			t.Fatalf("req=%#v err=%v", req, err)
		}
	}

	valid := setReq("mutation-bind-a", "scope-bind")
	if _, err := svc.Set(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	conflict := setReq("mutation-bind-b", "scope-bind")
	conflict.ActivityID = "activity-b"
	if _, err := svc.Set(context.Background(), conflict); failureCode(t, err) != failure.MutationScopeBindingConflict {
		t.Fatalf("binding err=%v", err)
	}
}

func TestReleaseDerivesDurableResultAndExactRetryCannotRemoveNewScope(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)
	if _, err := svc.Set(context.Background(), setReq("mutation-set", "scope-a")); err != nil {
		t.Fatal(err)
	}

	releaseReq := ReleaseRequest{MutationID: "mutation-release", ScopeID: "scope-a"}
	first, err := svc.Release(context.Background(), releaseReq)
	if err != nil {
		t.Fatal(err)
	}
	if first.Receipt.Result != core.ResultReleased || first.Replayed || store.releaseCalls != 1 {
		t.Fatalf("release=%#v calls=%d", first, store.releaseCalls)
	}

	clock.now = clock.now.Add(time.Second)
	if _, err := svc.Set(context.Background(), setReq("mutation-reset", "scope-a")); err != nil {
		t.Fatal(err)
	}
	retry, err := svc.Release(context.Background(), releaseReq)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Replayed || retry.Receipt.Result != core.ResultReleased || store.releaseCalls != 1 {
		t.Fatalf("retry=%#v calls=%d", retry, store.releaseCalls)
	}
	if current, found, _ := store.LoadMutationScope(context.Background(), "scope-a"); !found || current.RevisionID != "mutation-reset" {
		t.Fatalf("new scope removed: %#v found=%v", current, found)
	}

	absent, err := svc.Release(context.Background(), ReleaseRequest{MutationID: "mutation-absent", ScopeID: "never-set"})
	if err != nil || absent.Receipt.Result != core.ResultAlreadyAbsent {
		t.Fatalf("absent=%#v err=%v", absent, err)
	}
}

func TestInspectTreatsExactExpiryAsInactive(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	svc := New(store, clock)
	req := setReq("mutation-expiring", "scope-expiring")
	req.TTLMS = 1000
	if _, err := svc.Set(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(time.Second)
	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: appTestWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveCount != 0 || len(got.ActiveScopes) != 0 || got.AdvisoryCount != 0 {
		t.Fatalf("inspect at expiry=%#v", got)
	}
}

func TestInspectActivityFilterStillReportsCrossActivityOverlap(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	store.scopes["scope-a"] = appScope("scope-a", "activity-a", core.ModeMutate, "src/**", clock.now)
	store.scopes["scope-b"] = appScope("scope-b", "activity-b", core.ModeMutate, "src/auth/**", clock.now)
	store.scopes["scope-c"] = appScope("scope-c", "activity-c", core.ModeRead, "docs/**", clock.now)
	svc := New(store, clock)

	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: appTestWorkspace, ActivityID: "activity-a"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveCount != 1 || len(got.ActiveScopes) != 1 || got.ActiveScopes[0].ScopeID != "scope-a" {
		t.Fatalf("scopes=%#v", got)
	}
	if got.AdvisoryCount != 1 || len(got.Advisories) != 1 || got.Advisories[0].ScopeIDs != [2]string{"scope-a", "scope-b"} {
		t.Fatalf("advisories=%#v", got.Advisories)
	}
	if got.ActiveScopeLimit != core.MaxActiveScopesPerActivity || got.AdvisoryLimit != core.MaxAdvisories {
		t.Fatalf("limits=%#v", got)
	}
}

func TestInspectAdvisoriesAreDeterministicAndHardTruncated(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("scope-%02d", i)
		store.scopes[id] = appScope(id, fmt.Sprintf("activity-%02d", i), core.ModeMutate, "**", clock.now)
	}
	svc := New(store, clock)
	first, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: appTestWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: appTestWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if first.AdvisoryCount != 45 || len(first.Advisories) != core.MaxAdvisories || !first.AdvisoriesTruncated {
		t.Fatalf("first counts=%#v", first)
	}
	if fmt.Sprint(first.Advisories) != fmt.Sprint(second.Advisories) {
		t.Fatal("advisory ordering changed across identical inspect")
	}
	if first.Advisories[0].ScopeIDs != [2]string{"scope-00", "scope-01"} {
		t.Fatalf("first advisory=%#v", first.Advisories[0])
	}
}

func TestSetReturnsOnlyAdvisoriesInvolvingCurrentScope(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	store.scopes["scope-other-a"] = appScope("scope-other-a", "activity-x", core.ModeMutate, "docs/**", clock.now)
	store.scopes["scope-other-b"] = appScope("scope-other-b", "activity-y", core.ModeMutate, "docs/api/**", clock.now)
	svc := New(store, clock)
	req := setReq("mutation-focus", "scope-focus")
	req.Paths = []string{"src/**"}
	got, err := svc.Set(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.AdvisoryCount != 0 || len(got.Advisories) != 0 {
		t.Fatalf("unrelated advisory leaked into set result: %#v", got.Advisories)
	}

	clock.now = clock.now.Add(time.Second)
	overlap := setReq("mutation-overlap", "scope-overlap")
	overlap.ActivityID = "activity-z"
	overlap.Paths = []string{"src/auth/**"}
	second, err := svc.Set(context.Background(), overlap)
	if err != nil {
		t.Fatal(err)
	}
	if second.AdvisoryCount != 1 || len(second.Advisories) != 1 || second.Advisories[0].ScopeIDs != [2]string{"scope-focus", "scope-overlap"} {
		t.Fatalf("focused advisories=%#v", second.Advisories)
	}
}

func TestPersistenceErrorsRemainTyped(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1000, 0).UTC()}
	store := newFakeStore(clock.Now)
	store.setErr = failure.New(failure.PersistenceAmbiguous, nil, errors.New("disk sync uncertain"))
	svc := New(store, clock)
	if _, err := svc.Set(context.Background(), setReq("mutation-ambiguous", "scope-a")); failureCode(t, err) != failure.PersistenceAmbiguous {
		t.Fatalf("err=%v", err)
	}
}

func appScope(id, activity string, mode core.Mode, selector string, now time.Time) core.Scope {
	return core.Scope{SchemaVersion: core.SchemaVersion, ScopeID: id, ActivityID: activity, WorkspaceID: appTestWorkspace, Mode: mode, Paths: []string{selector}, DeclaredAt: now, ExpiresAt: now.Add(core.DefaultTTL), RevisionID: "revision-" + id}
}

func TestFingerprintDoesNotDependOnSelectorInputOrder(t *testing.T) {
	left := setReq("mutation-fp", "scope-fp")
	right := left
	right.Paths = []string{"src/auth/**", "tests/auth/**"}
	ln, lf, err := normalizeSetRequest(left)
	if err != nil {
		t.Fatal(err)
	}
	rn, rf, err := normalizeSetRequest(right)
	if err != nil {
		t.Fatal(err)
	}
	if lf != rf || fmt.Sprint(ln.Paths) != fmt.Sprint(rn.Paths) {
		t.Fatalf("left=%#v %s right=%#v %s", ln, lf, rn, rf)
	}
	if !strings.HasPrefix(lf, "") || len(lf) != 64 {
		t.Fatalf("fingerprint=%q", lf)
	}
}
