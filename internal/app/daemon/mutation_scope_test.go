package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var a26TestWorkspace = workspace.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PM")

type mutationScopeStoreStub struct {
	commitSet     StoreResult
	commitRelease StoreResult
}

func (*mutationScopeStoreStub) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return []workspace.Workspace{{ID: a26TestWorkspace}}, nil
}
func (*mutationScopeStoreStub) LoadMutationScope(context.Context, string) (core.Scope, bool, error) {
	return core.Scope{}, false, nil
}
func (*mutationScopeStoreStub) ListMutationScopes(context.Context, string, workspace.WorkspaceID) ([]core.Scope, error) {
	return nil, nil
}
func (*mutationScopeStoreStub) LoadMutationReceipt(context.Context, string) (core.MutationReceipt, bool, error) {
	return core.MutationReceipt{}, false, nil
}
func (s *mutationScopeStoreStub) CommitMutationScopeSet(context.Context, core.Scope, core.ScopeIdentity, core.MutationReceipt) StoreResult {
	return s.commitSet
}
func (s *mutationScopeStoreStub) CommitMutationScopeRelease(context.Context, string, core.MutationReceipt) StoreResult {
	return s.commitRelease
}

func TestMutationScopeStoreAdapterMapsDurabilityWithoutLosingTypedFailures(t *testing.T) {
	cases := []struct {
		name   string
		result StoreResult
		want   failure.Code
	}{
		{"ambiguous", StoreResult{Durability: AmbiguousChange, Err: errors.New("directory sync uncertain")}, failure.PersistenceAmbiguous},
		{"unavailable", StoreResult{Durability: NoDurableChange, Err: errors.New("disk unavailable")}, failure.PersistenceUnavailable},
		{"typed", StoreResult{Durability: DurableChange, Err: failure.New(failure.MutationMetadataConflict, map[string]string{"mutation_id": "mutation-1", "scope_id": "scope-a"}, nil)}, failure.MutationMetadataConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mutationScopeStoreStub{commitSet: tc.result}
			service := NewMutationScopeService(store, nil)
			if service == nil {
				t.Fatal("mutation scope service not composed")
			}
			_, err := service.Set(context.Background(), mutationapp.SetRequest{MutationID: "mutation-1", ScopeID: "scope-a", ActivityID: "activity-a", WorkspaceID: a26TestWorkspace, Mode: core.ModeMutate, Paths: []string{"src/**"}})
			if got := failure.Public(err).Code; got != tc.want {
				t.Fatalf("code=%q want=%q err=%v", got, tc.want, err)
			}
		})
	}
}

func TestMutationScopeStoreAdapterNilStoreLeavesFeatureUncomposed(t *testing.T) {
	if got := NewMutationScopeService(nil, nil); got != nil {
		t.Fatalf("nil store composed service: %#v", got)
	}
}

type mutationScopeInspectorStub struct {
	results map[workspace.WorkspaceID]core.InspectResult
	calls   []mutationapp.InspectRequest
}

func (s *mutationScopeInspectorStub) Inspect(_ context.Context, req mutationapp.InspectRequest) (core.InspectResult, error) {
	s.calls = append(s.calls, req)
	result, ok := s.results[req.WorkspaceID]
	if !ok {
		return core.InspectResult{}, fmt.Errorf("unexpected workspace %s", req.WorkspaceID)
	}
	return result, nil
}

func TestInspectActivityMutationScopesAggregatesDeterministicallyAndCapsTotals(t *testing.T) {
	ws1 := workspace.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PM")
	ws2 := workspace.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PN")
	result1 := activityMutationScopeResult(ws1, 0, 10, 20)
	result2 := activityMutationScopeResult(ws2, 10, 10, 20)
	inspector := &mutationScopeInspectorStub{results: map[workspace.WorkspaceID]core.InspectResult{ws1: result1, ws2: result2}}
	activity := activitycore.New(activitycore.ID("activity-a"), time.Unix(1000, 0).UTC())
	activity.WorkspaceIDs = []workspace.WorkspaceID{ws2, ws1}

	got, err := InspectActivityMutationScopes(context.Background(), inspector, activity)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspector.calls) != 2 || inspector.calls[0].WorkspaceID != ws1 || inspector.calls[1].WorkspaceID != ws2 {
		t.Fatalf("calls=%#v", inspector.calls)
	}
	for _, call := range inspector.calls {
		if call.ActivityID != "activity-a" {
			t.Fatalf("activity filter missing: %#v", call)
		}
	}
	if len(got.ActiveScopes) != core.MaxActiveScopesPerActivity || got.ActiveCount != 20 || !got.ScopesTruncated {
		t.Fatalf("active result=%#v", got)
	}
	if len(got.Advisories) != core.MaxAdvisories || got.AdvisoryCount != 40 || !got.AdvisoriesTruncated {
		t.Fatalf("advisory result=%#v", got)
	}
	if got.ActiveScopeLimit != core.MaxActiveScopesPerActivity || got.AdvisoryLimit != core.MaxAdvisories {
		t.Fatalf("limits=%#v", got)
	}
	for i := 1; i < len(got.ActiveScopes); i++ {
		prev, cur := got.ActiveScopes[i-1], got.ActiveScopes[i]
		if string(prev.WorkspaceID) > string(cur.WorkspaceID) || (prev.WorkspaceID == cur.WorkspaceID && prev.ScopeID > cur.ScopeID) {
			t.Fatalf("scopes not deterministic at %d: %#v", i, got.ActiveScopes)
		}
	}
}

func activityMutationScopeResult(ws workspace.WorkspaceID, offset, scopeCount, advisoryCount int) core.InspectResult {
	scopes := make([]core.Scope, 0, scopeCount)
	for i := 0; i < scopeCount; i++ {
		scopes = append(scopes, core.Scope{ScopeID: fmt.Sprintf("scope-%02d", offset+i), WorkspaceID: ws, ActivityID: "activity-a"})
	}
	advisories := make([]core.Advisory, 0, advisoryCount)
	for i := 0; i < advisoryCount; i++ {
		advisories = append(advisories, core.Advisory{WorkspaceID: ws, ScopeIDs: [2]string{fmt.Sprintf("scope-%02d", i), fmt.Sprintf("scope-%02d", i+1)}})
	}
	return core.InspectResult{ActiveScopes: scopes, Advisories: advisories, ActiveCount: scopeCount, AdvisoryCount: advisoryCount, ActiveScopeLimit: core.MaxActiveScopesPerActivity, AdvisoryLimit: core.MaxAdvisories}
}
