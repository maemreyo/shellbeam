package observation

import "testing"

func TestTargetRequiresExactlyOneValidSelector(t *testing.T) {
	valid := []Target{
		{Kind: TargetOperation, OperationID: "op-1"},
		{Kind: TargetSession, SessionID: "session-1"},
		{Kind: TargetActivity, ActivityID: "activity-1"},
		{Kind: TargetWorkspace, WorkspaceID: "ws_01K00000000000000000000000"},
		{Kind: TargetRepository, RepositoryID: "repo_01K00000000000000000000000"},
	}
	for _, target := range valid {
		if err := target.Validate(); err != nil {
			t.Fatalf("valid target %#v: %v", target, err)
		}
	}
	invalid := []Target{
		{},
		{Kind: TargetOperation},
		{Kind: TargetOperation, OperationID: "op-1", SessionID: "session-1"},
		{Kind: TargetWorkspace, WorkspaceID: "bad"},
		{Kind: "other", OperationID: "op-1"},
	}
	for _, target := range invalid {
		if err := target.Validate(); err == nil {
			t.Fatalf("invalid target accepted: %#v", target)
		}
	}
}
