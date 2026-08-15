package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDaemonCatalogMutationScopesRequireComposition(t *testing.T) {
	base := daemonCatalog(capability.Limits{})
	if base.Features[capability.FeatureMutationScopes] == capability.Available || len(base.MutationScopeSchemaVersions) != 0 {
		t.Fatalf("base unexpectedly advertises mutation scopes: %#v", base)
	}
	catalog := mutationScopeCatalog(base)
	if catalog.Features[capability.FeatureMutationScopes] != capability.Available || len(catalog.MutationScopeSchemaVersions) != 1 || catalog.MutationScopeSchemaVersions[0] != 1 {
		t.Fatalf("catalog=%#v", catalog)
	}
	l := catalog.Limits
	if l.MutationScopeActivePerActivity != core.MaxActiveScopesPerActivity || l.MutationScopeActivePerWorkspace != core.MaxActiveScopesPerWorkspace || l.MutationScopePathsPerScope != core.MaxPathsPerScope || l.MutationScopeSelectorBytes != core.MaxSelectorBytes || l.MutationScopeAdvisories != core.MaxAdvisories || l.MutationScopeMinTTLMS != core.MinTTL.Milliseconds() || l.MutationScopeDefaultTTLMS != core.DefaultTTL.Milliseconds() || l.MutationScopeMaxTTLMS != core.MaxTTL.Milliseconds() {
		t.Fatalf("limits=%#v", l)
	}

	store := openMutationScopeCommandStore(t)
	if svc := daemonapp.NewMutationScopeService(store, nil); svc == nil {
		t.Fatal("real repository did not compose mutation scope service")
	}
}

type countingMutationScopeCoordinator struct{ sets, releases, inspects int }

func (c *countingMutationScopeCoordinator) Set(context.Context, mutationapp.SetRequest) (mutationapp.MutationResult, error) {
	c.sets++
	return mutationapp.MutationResult{}, nil
}
func (c *countingMutationScopeCoordinator) Release(context.Context, mutationapp.ReleaseRequest) (mutationapp.MutationResult, error) {
	c.releases++
	return mutationapp.MutationResult{}, nil
}
func (c *countingMutationScopeCoordinator) Inspect(context.Context, mutationapp.InspectRequest) (core.InspectResult, error) {
	c.inspects++
	return core.InspectResult{ActiveScopeLimit: core.MaxActiveScopesPerWorkspace, AdvisoryLimit: core.MaxAdvisories}, nil
}

func TestDaemonMutationScopesAreExplicitOnlyAndOrdinaryStartPollPayNoTax(t *testing.T) {
	coordinator := &countingMutationScopeCoordinator{}
	actions := &daemonActions{Actions: &noTaxCoreActions{}, mutationScopes: coordinator}
	if _, err := actions.Start(context.Background(), daemonapp.StartRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Poll(context.Background(), daemonapp.PollRequest{}); err != nil {
		t.Fatal(err)
	}
	if coordinator.sets != 0 || coordinator.releases != 0 || coordinator.inspects != 0 {
		t.Fatalf("ordinary path touched mutation scopes: %#v", coordinator)
	}
	if _, err := actions.InspectMutationScopes(context.Background(), mutationapp.InspectRequest{WorkspaceID: "ws_01KZZ8AJJYRPX53ZX04P2NB9PM"}); err != nil {
		t.Fatal(err)
	}
	if coordinator.inspects != 1 {
		t.Fatalf("explicit inspect calls=%d", coordinator.inspects)
	}
	if _, err := actions.SetMutationScope(context.Background(), mutationapp.SetRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.ReleaseMutationScope(context.Background(), mutationapp.ReleaseRequest{}); err != nil {
		t.Fatal(err)
	}
	if coordinator.sets != 1 || coordinator.releases != 1 {
		t.Fatalf("explicit mutation calls=%#v", coordinator)
	}

	unavailable := &daemonActions{Actions: &noTaxCoreActions{}}
	if _, err := unavailable.InspectMutationScopes(context.Background(), mutationapp.InspectRequest{WorkspaceID: "ws_01KZZ8AJJYRPX53ZX04P2NB9PM"}); !errors.Is(err, failure.FeatureUnavailable) {
		t.Fatalf("missing composition err=%v", err)
	}
}

type mutableMutationClock struct{ now time.Time }

func (c *mutableMutationClock) Now() time.Time { return c.now }

func TestDaemonActivityMutationScopeProjectionOmitsExpiredAndPreservesActivityHistory(t *testing.T) {
	store := openMutationScopeCommandStore(t)
	now := time.Now().UTC()
	ws := workspace.Workspace{SchemaVersion: workspace.SchemaVersion, ID: "ws_01KZZ8AJJYRPX53ZX04P2NB9PM", RepositoryID: "repo_01KZZ8AJJYRPX53ZX04P2NB9PM", Label: "mutation-scope-test", Root: t.TempDir(), GitDir: filepath.Join(t.TempDir(), ".git"), CreatedAt: now, LastSeenAt: now}
	if err := store.SaveWorkspace(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	activity := activitycore.New(activitycore.ID("activity-a"), now)
	activity.WorkspaceIDs = []workspace.WorkspaceID{ws.ID}
	if err := store.SaveActivity(context.Background(), activity); err != nil {
		t.Fatal(err)
	}
	activitySvc := activityapp.New(store, nil, activitycore.MaxOperationHistory)
	clock := &mutableMutationClock{now: now.Add(-2 * time.Second)}
	coordinator := daemonapp.NewMutationScopeService(store, clock)
	actions := &daemonActions{Actions: &noTaxCoreActions{}, activity: activitySvc, mutationScopes: coordinator}

	if _, err := actions.SetMutationScope(context.Background(), mutationapp.SetRequest{MutationID: "mutation-expired", ScopeID: "scope-expired", ActivityID: "activity-a", WorkspaceID: ws.ID, Mode: core.ModeMutate, Paths: []string{"expired/**"}, TTLMS: core.MinTTL.Milliseconds()}); err != nil {
		t.Fatal(err)
	}
	clock.now = now
	if _, err := actions.SetMutationScope(context.Background(), mutationapp.SetRequest{MutationID: "mutation-active", ScopeID: "scope-active", ActivityID: "activity-a", WorkspaceID: ws.ID, Mode: core.ModeRead, Paths: []string{"active/**"}}); err != nil {
		t.Fatal(err)
	}

	before, err := activitySvc.Inspect(context.Background(), "activity-a")
	if err != nil {
		t.Fatal(err)
	}
	got, err := actions.InspectActivityMutationScopes(context.Background(), "activity-a")
	if err != nil {
		t.Fatal(err)
	}
	after, err := activitySvc.Inspect(context.Background(), "activity-a")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("activity history mutated by scope inspection: before=%#v after=%#v", before, after)
	}
	if got.ActiveCount != 1 || len(got.ActiveScopes) != 1 || got.ActiveScopes[0].ScopeID != "scope-active" {
		t.Fatalf("projection=%#v", got)
	}
}

func openMutationScopeCommandStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	r, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1024, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
