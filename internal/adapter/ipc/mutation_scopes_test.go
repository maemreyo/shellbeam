package ipc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestA26RequestV2DecodesClosedMutationScopeShapes(t *testing.T) {
	raw := `{"ipc_version":2,"kind":"request","request_id":"set","action":"mutation_scope.set","mutation_id":"mutation-1","scope_id":"scope-a","activity_id":"activity-a","workspace_id":"ws_01K00000000000000000000000","mode":"mutate","paths":["src/**","tests/**"],"ttl_ms":900000}`
	req, err := decodeRequestV2(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if req.MutationID != "mutation-1" || req.ScopeID != "scope-a" || req.Mode != core.ModeMutate || len(req.Paths) != 2 || req.TTLMS != 900000 {
		t.Fatalf("set=%#v", req)
	}
	for _, bad := range []string{
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"mutation_scope.release","mutation_id":"r","scope_id":"s","workspace_id":"ws_01K00000000000000000000000"}`,
		`{"ipc_version":2,"kind":"request","request_id":"bad","action":"inspect.mutation_scopes","workspace_id":"ws_01K00000000000000000000000","paths":["src"]}`,
	} {
		if _, err := decodeRequestV2(strings.NewReader(bad)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("cross-action accepted %s: %v", bad, err)
		}
	}
}

func TestA26BridgeRequestV2MappingIsLossless(t *testing.T) {
	set := mutationapp.SetRequest{MutationID: "mutation-1", ScopeID: "scope-a", ActivityID: "activity-a", WorkspaceID: "ws_01K00000000000000000000000", Mode: core.ModeMutate, Paths: []string{"src/**", "tests/**"}, TTLMS: 900000}
	encoded := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "mutation_scope.set", MutationScopeSet: set})
	if encoded.MutationID != set.MutationID || encoded.ScopeID != set.ScopeID || encoded.Mode != set.Mode || encoded.TTLMS != set.TTLMS || strings.Join(encoded.Paths, ",") != strings.Join(set.Paths, ",") {
		t.Fatalf("encoded=%#v", encoded)
	}
	set.Paths[0] = "mutated"
	if encoded.Paths[0] != "src/**" {
		t.Fatal("selector slice aliased bridge request")
	}

	release := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "mutation_scope.release", MutationScopeRelease: mutationapp.ReleaseRequest{MutationID: "release-1", ScopeID: "scope-a"}})
	if release.MutationID != "release-1" || release.ScopeID != "scope-a" {
		t.Fatalf("release=%#v", release)
	}
	inspect := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "inspect.mutation_scopes", MutationScopeInspect: mutationapp.InspectRequest{WorkspaceID: "ws_01K00000000000000000000000", ActivityID: "activity-a"}})
	if inspect.WorkspaceID != "ws_01K00000000000000000000000" || inspect.ActivityID != "activity-a" {
		t.Fatalf("inspect=%#v", inspect)
	}
}

type a26MutationActions struct {
	a25BaseActions
	setReq     mutationapp.SetRequest
	releaseReq mutationapp.ReleaseRequest
	inspectReq mutationapp.InspectRequest
}

func (a *a26MutationActions) SetMutationScope(_ context.Context, req mutationapp.SetRequest) (mutationapp.MutationResult, error) {
	a.setReq = req
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	scope := core.Scope{SchemaVersion: 1, ScopeID: req.ScopeID, ActivityID: req.ActivityID, WorkspaceID: req.WorkspaceID, Mode: req.Mode, Paths: append([]string(nil), req.Paths...), DeclaredAt: now, ExpiresAt: now.Add(15 * time.Minute), RevisionID: req.MutationID}
	receipt := core.MutationReceipt{SchemaVersion: 1, MutationID: req.MutationID, RequestFingerprint: strings.Repeat("a", 64), Result: core.ResultSet, SetEffect: core.SetEffectCreated, ScopeID: req.ScopeID, CommittedAt: now, ExpiresAt: scope.ExpiresAt}
	return mutationapp.MutationResult{Receipt: receipt, Scope: &scope, CurrentRevision: true, AdvisoryLimit: core.MaxAdvisories}, nil
}
func (a *a26MutationActions) ReleaseMutationScope(_ context.Context, req mutationapp.ReleaseRequest) (mutationapp.MutationResult, error) {
	a.releaseReq = req
	return mutationapp.MutationResult{Receipt: core.MutationReceipt{SchemaVersion: 1, MutationID: req.MutationID, RequestFingerprint: strings.Repeat("b", 64), Result: core.ResultAlreadyAbsent, ScopeID: req.ScopeID, CommittedAt: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}, AdvisoryLimit: core.MaxAdvisories}, nil
}
func (a *a26MutationActions) InspectMutationScopes(_ context.Context, req mutationapp.InspectRequest) (core.InspectResult, error) {
	a.inspectReq = req
	return core.InspectResult{ActiveScopes: []core.Scope{}, Advisories: []core.Advisory{}, ActiveScopeLimit: core.MaxActiveScopesPerWorkspace, AdvisoryLimit: core.MaxAdvisories}, nil
}

func TestA26IPCRoutesTypedMutationScopeActionsAndMissingCompositionIsUnavailable(t *testing.T) {
	server := &Server{actions: &a26MutationActions{}}
	setReq := RequestV2{Action: "mutation_scope.set", MutationID: "mutation-1", ScopeID: "scope-a", ActivityID: "activity-a", WorkspaceID: "ws_01K00000000000000000000000", Mode: core.ModeMutate, Paths: []string{"src/**"}, TTLMS: 900000}
	var setResp ResponseV2
	if err := server.inspectV2(context.Background(), setReq, &setResp); err != nil {
		t.Fatal(err)
	}
	actions := server.actions.(*a26MutationActions)
	if setResp.Mutation == nil || actions.setReq.MutationID != "mutation-1" || actions.setReq.WorkspaceID != workspace.WorkspaceID(setReq.WorkspaceID) {
		t.Fatalf("resp=%#v req=%#v", setResp.Mutation, actions.setReq)
	}

	var inspectResp ResponseV2
	if err := server.inspectV2(context.Background(), RequestV2{Action: "inspect.mutation_scopes", WorkspaceID: setReq.WorkspaceID, ActivityID: "activity-a"}, &inspectResp); err != nil {
		t.Fatal(err)
	}
	if inspectResp.MutationScopes == nil || actions.inspectReq.ActivityID != "activity-a" {
		t.Fatalf("inspect=%#v req=%#v", inspectResp.MutationScopes, actions.inspectReq)
	}

	missing := &Server{actions: a25BaseActions{}}
	for _, req := range []RequestV2{{Action: "mutation_scope.set"}, {Action: "mutation_scope.release"}, {Action: "inspect.mutation_scopes", WorkspaceID: setReq.WorkspaceID}} {
		var resp ResponseV2
		if err := missing.inspectV2(context.Background(), req, &resp); !errors.Is(err, failure.FeatureUnavailable) {
			t.Fatalf("%s err=%v", req.Action, err)
		}
	}
}

func TestA26ClearResponseDropsMutationScopePayloads(t *testing.T) {
	mutation := &mutationapp.MutationResult{}
	scopes := &core.InspectResult{}
	resp := ResponseV2{Mutation: mutation, MutationScopes: scopes, ActiveMutationScopes: []core.Scope{{ScopeID: "scope-a"}}, MutationScopeAdvisories: []core.Advisory{{Code: "mutation_scope_overlap"}}, MutationScopesTruncated: true, MutationScopeAdvisoriesTruncated: true}
	clearResponseV2Payload(&resp)
	if resp.Mutation != nil || resp.MutationScopes != nil || resp.ActiveMutationScopes != nil || resp.MutationScopeAdvisories != nil || resp.MutationScopesTruncated || resp.MutationScopeAdvisoriesTruncated {
		t.Fatalf("payload survived clear: %#v", resp)
	}
}
