//go:build linux || darwin

package main

import (
	"context"
	"testing"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestDecisionProtocolNativeMultiWorkspaceRouting(t *testing.T) {
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	store := openA1Store(t, stateDir)
	workspaces := workspaceapp.New(store, gitadapter.New())
	workspaceA, err := workspaces.Attach(context.Background(), initWorkspaceCLIRepo(t), "decision-multi-a")
	if err != nil {
		t.Fatal(err)
	}
	workspaceB, err := workspaces.Attach(context.Background(), initWorkspaceCLIRepo(t), "decision-multi-b")
	if err != nil {
		t.Fatal(err)
	}
	if workspaceA.RepositoryID == workspaceB.RepositoryID {
		t.Fatalf("repositories collapsed: A=%s B=%s", workspaceA.RepositoryID, workspaceB.RepositoryID)
	}

	daemon := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	defer daemon.hardKill(t)

	for _, tc := range []struct {
		name      string
		workspace string
		policyID  string
		wantRepo  string
	}{
		{"workspace A", string(workspaceA.ID), "decision-multi-policy-a", string(workspaceA.RepositoryID)},
		{"workspace B", string(workspaceB.ID), "decision-multi-policy-b", string(workspaceB.RepositoryID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := callDecisionNativeEnvelope(t, daemon, tc.workspace, decisioncore.PolicyContent{
				PolicyID: tc.policyID, EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false},
			})
			if !response.OK || response.Decision == nil || response.Decision.Policy == nil {
				t.Fatalf("response=%#v", response)
			}
			if got := response.Decision.Policy.RepositoryID; got != tc.wantRepo {
				t.Fatalf("repository_id=%q want=%q", got, tc.wantRepo)
			}
		})
	}

	ambiguous := callDecisionNativeEnvelope(t, daemon, "", decisioncore.PolicyContent{PolicyID: "decision-multi-ambiguous", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}})
	assertDecisionNativeFailure(t, ambiguous, failure.DecisionContextUnavailable)

	unknownID := "ws_01K00000000000000000000099"
	unknown := callDecisionNativeEnvelope(t, daemon, unknownID, decisioncore.PolicyContent{PolicyID: "decision-multi-unknown", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}})
	assertDecisionNativeFailure(t, unknown, failure.WorkspaceNotFound)
	if unknown.Error == nil || len(unknown.Error.Details) != 1 || unknown.Error.Details["workspace_id"] != unknownID {
		t.Fatalf("unknown selector details=%#v", unknown.Error)
	}
}

func callDecisionNativeEnvelope(t *testing.T, daemon *b1NativeDaemon, workspaceID string, content decisioncore.PolicyContent) ipcadapter.ResponseV2 {
	t.Helper()
	response, err := daemon.client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "decision-multi-" + content.PolicyID,
		Action: "decision.policy.snapshot", WorkspaceID: workspaceID,
		Decision: &ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertDecisionNativeFailure(t *testing.T, response ipcadapter.ResponseV2, code failure.Code) {
	t.Helper()
	if response.OK || response.Error == nil || response.Error.Code != string(code) {
		t.Fatalf("response=%#v want failure=%s", response, code)
	}
}
