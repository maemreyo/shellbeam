package main

import (
	"context"
	"errors"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type decisionWorkspaceSelectorFake struct {
	workspaces   []workspacecore.Workspace
	listErr      error
	inspectErr   error
	inspectCalls int
}

func (f *decisionWorkspaceSelectorFake) List(context.Context) ([]workspacecore.Workspace, error) {
	return append([]workspacecore.Workspace(nil), f.workspaces...), f.listErr
}

func (f *decisionWorkspaceSelectorFake) Inspect(_ context.Context, id string) (workspacecore.Workspace, error) {
	f.inspectCalls++
	if f.inspectErr != nil {
		return workspacecore.Workspace{}, f.inspectErr
	}
	for _, ws := range f.workspaces {
		if string(ws.ID) == id {
			return ws, nil
		}
	}
	return workspacecore.Workspace{}, failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": id}, errors.New("missing registered workspace"))
}

func decisionRoutingWorkspace(id, repositoryID, label string) workspacecore.Workspace {
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID(id),
		RepositoryID:  workspacecore.RepositoryID(repositoryID),
		Label:         label,
		Root:          "/" + label,
		GitDir:        "/" + label + "/.git",
		CreatedAt:     time.Unix(1, 0).UTC(),
		LastSeenAt:    time.Unix(1, 0).UTC(),
	}
}

func TestDecisionProtocolResolveWorkspaceSelectorMatrix(t *testing.T) {
	wsA := decisionRoutingWorkspace("ws_01K00000000000000000000001", "repo_01K00000000000000000000001", "repo-a")
	wsB := decisionRoutingWorkspace("ws_01K00000000000000000000002", "repo_01K00000000000000000000002", "repo-b")

	for name, tc := range map[string]struct {
		selector string
		items    []workspacecore.Workspace
		wantID   workspacecore.WorkspaceID
		wantCode failure.Code
	}{
		"explicit A":                 {selector: string(wsA.ID), items: []workspacecore.Workspace{wsA, wsB}, wantID: wsA.ID},
		"explicit B":                 {selector: string(wsB.ID), items: []workspacecore.Workspace{wsA, wsB}, wantID: wsB.ID},
		"ambiguous without selector": {items: []workspacecore.Workspace{wsA, wsB}, wantCode: failure.DecisionContextUnavailable},
		"zero without selector":      {wantCode: failure.DecisionContextUnavailable},
		"singleton fallback":         {items: []workspacecore.Workspace{wsA}, wantID: wsA.ID},
		"unknown selector":           {selector: "ws_01K00000000000000000000003", items: []workspacecore.Workspace{wsA, wsB}, wantCode: failure.WorkspaceNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &decisionWorkspaceSelectorFake{workspaces: tc.items}
			got, err := resolveDecisionWorkspace(context.Background(), tc.selector, fake)
			if tc.wantCode != "" {
				if err == nil {
					t.Fatalf("expected %s failure, got workspace=%#v", tc.wantCode, got)
				}
				if public := failure.Public(err); public.Code != tc.wantCode {
					t.Fatalf("public failure=%#v want=%s", public, tc.wantCode)
				}
				return
			}
			if err != nil || got.ID != tc.wantID {
				t.Fatalf("workspace=%#v err=%v want=%s", got, err, tc.wantID)
			}
		})
	}
}

func TestDecisionProtocolInvalidSelectorNeverInspectsRegistry(t *testing.T) {
	fake := &decisionWorkspaceSelectorFake{}
	_, err := resolveDecisionWorkspace(context.Background(), "not-a-workspace-id", fake)
	if err == nil {
		t.Fatal("invalid selector accepted")
	}
	if fake.inspectCalls != 0 {
		t.Fatalf("registry inspect calls=%d want=0", fake.inspectCalls)
	}
	if public := failure.Public(err); public.Code != failure.InvalidInput {
		t.Fatalf("public failure=%#v", public)
	}
}

func TestDecisionProtocolDispatchSelectorDerivesRepositoryServerSide(t *testing.T) {
	wsA := decisionRoutingWorkspace("ws_01K00000000000000000000001", "repo_01K00000000000000000000001", "repo-a")
	wsB := decisionRoutingWorkspace("ws_01K00000000000000000000002", "repo_01K00000000000000000000002", "repo-b")
	ops := &decisionOperationsCapture{}
	runtime := &decisionProtocolRuntime{
		service:    ops,
		workspaces: &decisionWorkspaceSelectorFake{workspaces: []workspacecore.Workspace{wsA, wsB}},
	}
	content := decisioncore.PolicyContent{
		PolicyID:       "policy-routing",
		EpisodeKinds:   []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis},
		OverridePolicy: decisioncore.OverridePolicy{Allowed: false},
	}
	_, err := runtime.DecisionProtocol(
		context.Background(),
		"decision.policy.snapshot",
		string(wsA.ID),
		ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ops.policyReq.RepositoryID, string(wsA.RepositoryID); got != want {
		t.Fatalf("repository_id=%q want=%q", got, want)
	}
}
