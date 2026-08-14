package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeWorkspaceLookup struct {
	values []workspace.Workspace
}

func (f fakeWorkspaceLookup) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), f.values...), nil
}

type fakeLoader struct {
	result core.LoadResult
	root   string
	calls  int
}

func (f *fakeLoader) Load(_ context.Context, root string) core.LoadResult {
	f.calls++
	f.root = root
	return f.result
}

func TestProjectStatusInspectUsesRegisteredWorkspaceRoot(t *testing.T) {
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &fakeLoader{result: core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: "raw"}}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{{
		ID: "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000", Root: "/repo",
	}}}, loader, &fakeReviewStore{})
	got, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if loader.calls != 1 || loader.root != "/repo" {
		t.Fatalf("loader calls=%d root=%q", loader.calls, loader.root)
	}
	if got.Status != core.StatusReviewDue || got.ManifestDigest != "raw" || got.DiscoveryFingerprint != parsed.Fingerprint || got.Manifest == nil {
		t.Fatalf("inspection=%#v", got)
	}
}

func TestProjectStatusInspectReturnsAbsentOrInvalidAsData(t *testing.T) {
	workspaceLookup := fakeWorkspaceLookup{values: []workspace.Workspace{{ID: "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000", Root: "/repo"}}}
	for _, result := range []core.LoadResult{
		{State: core.LoadAbsent},
		{State: core.LoadInvalid, Code: core.CodeParseError, ManifestDigest: "raw"},
	} {
		loader := &fakeLoader{result: result}
		got, err := New(workspaceLookup, loader, &fakeReviewStore{}).Inspect(context.Background(), "ws_01K00000000000000000000000")
		if err != nil {
			t.Fatal(err)
		}
		if result.State == core.LoadAbsent && got.Status != core.StatusAbsent {
			t.Fatalf("absent=%#v", got)
		}
		if result.State == core.LoadInvalid && (got.Status != core.StatusInvalid || got.Code != core.CodeParseError) {
			t.Fatalf("invalid=%#v", got)
		}
	}
}

func TestProjectStatusInspectRejectsUnknownWorkspaceWithoutLoading(t *testing.T) {
	loader := &fakeLoader{}
	svc := New(fakeWorkspaceLookup{}, loader, &fakeReviewStore{})
	_, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if !errors.Is(err, failure.WorkspaceNotFound) {
		t.Fatalf("err=%v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("loader called for unknown workspace: %d", loader.calls)
	}
}

func TestInspectionDoesNotExecuteManifestCommandThroughService(t *testing.T) {
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.never_run]\nshell=\"touch SENTINEL\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	loader := &fakeLoader{result: core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: "raw"}}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{{ID: "ws_01K00000000000000000000000", RepositoryID: "repo_01K00000000000000000000000", Root: "/repo"}}}, loader, &fakeReviewStore{})
	got, err := svc.Inspect(context.Background(), "ws_01K00000000000000000000000")
	if err != nil || got.Manifest == nil || loader.calls != 1 {
		t.Fatalf("inspection=%#v err=%v calls=%d", got, err, loader.calls)
	}
}

type fakeReviewStore struct {
	value     core.Review
	found     bool
	loadErr   error
	saved     core.Review
	saveCalls int
}

func (f *fakeReviewStore) LoadProjectReview(context.Context, workspace.RepositoryID) (core.Review, bool, error) {
	return f.value, f.found, f.loadErr
}

func (f *fakeReviewStore) SaveProjectReview(_ context.Context, value core.Review) error {
	f.saveCalls++
	f.saved = value
	return nil
}

func validProjectLoad(t *testing.T) core.LoadResult {
	t.Helper()
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	return core.LoadResult{
		State: core.LoadValid, Parsed: &parsed,
		ManifestDigest:       strings.Repeat("a", 64),
		DiscoveryFingerprint: parsed.Fingerprint,
	}
}

func testProjectWorkspace() workspace.Workspace {
	return workspace.Workspace{
		ID:           "ws_01K00000000000000000000000",
		RepositoryID: "repo_01K00000000000000000000000",
		Root:         "/repo",
	}
}

func currentProjectReview(load core.LoadResult) core.Review {
	return core.Review{
		RepositoryID:          "repo_01K00000000000000000000000",
		ManifestFingerprint:   load.Parsed.Fingerprint,
		DiscoveryFingerprint:  load.DiscoveryFingerprint,
		ManifestSchemaVersion: core.SchemaVersion,
		ReviewedAt:            time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC),
		ToolVersion:           "test",
		ReviewerClass:         "user",
		SourceClass:           "cli",
	}
}

func TestReviewStateTracksExactManifestAndDiscoveryFingerprints(t *testing.T) {
	load := validProjectLoad(t)
	ws := fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}
	reviews := &fakeReviewStore{}
	svc := New(ws, &fakeLoader{result: load}, reviews)

	got, err := svc.Inspect(context.Background(), string(testProjectWorkspace().ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.StatusReviewDue || got.ReviewFingerprint != "" {
		t.Fatalf("unreviewed=%#v", got)
	}

	reviews.value, reviews.found = currentProjectReview(load), true
	got, err = svc.Inspect(context.Background(), string(testProjectWorkspace().ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.StatusValid || got.ReviewFingerprint != load.DiscoveryFingerprint {
		t.Fatalf("reviewed=%#v", got)
	}

	reviews.value.ManifestFingerprint = strings.Repeat("b", 64)
	got, err = svc.Inspect(context.Background(), string(testProjectWorkspace().ID))
	if err != nil || got.Status != core.StatusReviewDue {
		t.Fatalf("changed manifest=%#v err=%v", got, err)
	}
	reviews.value = currentProjectReview(load)
	reviews.value.DiscoveryFingerprint = strings.Repeat("c", 64)
	got, err = svc.Inspect(context.Background(), string(testProjectWorkspace().ID))
	if err != nil || got.Status != core.StatusReviewDue {
		t.Fatalf("changed discovery=%#v err=%v", got, err)
	}
}

func TestReviewPersistsOnlyExactCurrentFingerprint(t *testing.T) {
	load := validProjectLoad(t)
	reviews := &fakeReviewStore{}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, &fakeLoader{result: load}, reviews)
	request := ReviewRequest{
		Fingerprint:   load.DiscoveryFingerprint,
		ReviewedAt:    time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC),
		ToolVersion:   "test",
		ReviewerClass: "user",
		SourceClass:   "cli",
	}
	got, err := svc.Review(context.Background(), string(testProjectWorkspace().ID), request)
	if err != nil {
		t.Fatal(err)
	}
	if reviews.saveCalls != 1 || reviews.saved.RepositoryID != testProjectWorkspace().RepositoryID ||
		reviews.saved.ManifestFingerprint != load.Parsed.Fingerprint ||
		reviews.saved.DiscoveryFingerprint != load.DiscoveryFingerprint {
		t.Fatalf("saved=%#v calls=%d", reviews.saved, reviews.saveCalls)
	}
	if got.Status != core.StatusValid || got.ReviewFingerprint != load.DiscoveryFingerprint {
		t.Fatalf("review result=%#v", got)
	}

	reviews.saveCalls = 0
	request.Fingerprint = strings.Repeat("f", 64)
	if _, err := svc.Review(context.Background(), string(testProjectWorkspace().ID), request); !core.HasCode(err, core.CodeChangedDuringResolve) {
		t.Fatalf("stale review err=%v", err)
	}
	if reviews.saveCalls != 0 {
		t.Fatalf("stale fingerprint wrote review: %d", reviews.saveCalls)
	}
}

func TestReviewRejectsAbsentInvalidAndUnsupportedContent(t *testing.T) {
	ws := fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}
	for _, result := range []core.LoadResult{
		{State: core.LoadAbsent},
		{State: core.LoadInvalid, Code: core.CodeParseError, ManifestDigest: strings.Repeat("a", 64)},
		{State: core.LoadInvalid, Code: core.CodeUnsupported, ManifestDigest: strings.Repeat("a", 64)},
	} {
		reviews := &fakeReviewStore{}
		svc := New(ws, &fakeLoader{result: result}, reviews)
		_, err := svc.Review(context.Background(), string(testProjectWorkspace().ID), ReviewRequest{
			Fingerprint: strings.Repeat("a", 64),
			ReviewedAt:  time.Now().UTC(),
			ToolVersion: "test", ReviewerClass: "user", SourceClass: "cli",
		})
		if err == nil {
			t.Fatalf("review accepted result=%#v", result)
		}
		if reviews.saveCalls != 0 {
			t.Fatalf("invalid review persisted result=%#v", result)
		}
	}
}

type sequenceProjectLoader struct {
	results []core.LoadResult
	calls   int
}

func (l *sequenceProjectLoader) Load(context.Context, string) core.LoadResult {
	index := l.calls
	if index >= len(l.results) {
		index = len(l.results) - 1
	}
	l.calls++
	return l.results[index]
}

func TestReviewRechecksFingerprintBeforePersistence(t *testing.T) {
	first := validProjectLoad(t)
	second := first
	second.ManifestDigest = strings.Repeat("b", 64)
	second.DiscoveryFingerprint = strings.Repeat("b", 64)
	reviews := &fakeReviewStore{}
	loader := &sequenceProjectLoader{results: []core.LoadResult{first, second}}
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, loader, reviews)

	_, err := svc.Review(context.Background(), string(testProjectWorkspace().ID), ReviewRequest{
		Fingerprint:   first.DiscoveryFingerprint,
		ReviewedAt:    time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC),
		ToolVersion:   "test",
		ReviewerClass: "user",
		SourceClass:   "cli",
	})
	if !core.HasCode(err, core.CodeChangedDuringResolve) {
		t.Fatalf("mid-review drift err=%v", err)
	}
	if loader.calls < 2 {
		t.Fatalf("review did not re-read current manifest: calls=%d", loader.calls)
	}
	if reviews.saveCalls != 0 {
		t.Fatalf("mid-review drift persisted stale review: %d", reviews.saveCalls)
	}
}

func TestReviewReusesAcrossEquivalentWorktrees(t *testing.T) {
	load := validProjectLoad(t)
	repoID := workspace.RepositoryID("repo_01K00000000000000000000000")
	first := testProjectWorkspace()
	first.RepositoryID = repoID
	second := first
	second.ID = "ws_01K00000000000000000000001"
	second.Root = "/repo-linked"
	reviews := &fakeReviewStore{
		value: currentProjectReview(load),
		found: true,
	}
	reviews.value.RepositoryID = repoID
	svc := New(fakeWorkspaceLookup{values: []workspace.Workspace{first, second}}, &fakeLoader{result: load}, reviews)

	got, err := svc.Inspect(context.Background(), string(second.ID))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != core.StatusValid || got.ReviewFingerprint != load.DiscoveryFingerprint {
		t.Fatalf("equivalent worktree did not reuse review: %#v", got)
	}
}
