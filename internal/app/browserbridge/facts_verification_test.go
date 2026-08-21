package browserbridge

import (
	"context"
	"fmt"
	"testing"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type recordingVerificationReader struct {
	activity             *activitycore.Activity
	byWorkspace          map[string]verificationapp.Inspection
	verificationRequests []ipc.RequestV2
}

func (r *recordingVerificationReader) Read(_ context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	switch req.Action {
	case "inspect.activity":
		return ipc.ResponseV2{OK: true, Activity: r.activity}, nil
	case "inspect.verification":
		r.verificationRequests = append(r.verificationRequests, req)
		v, ok := r.byWorkspace[req.WorkspaceID]
		if !ok {
			return ipc.ResponseV2{OK: false, Error: &ipc.Error{Code: "verification_unavailable"}}, nil
		}
		return ipc.ResponseV2{OK: true, VerificationResponseV2Fields: ipc.VerificationResponseV2Fields{Verification: &v}}, nil
	default:
		return ipc.ResponseV2{OK: false, Error: &ipc.Error{Code: "unexpected_action"}}, nil
	}
}

func TestVerificationFactsReturnsPerWorkspaceAndNeverAggregates(t *testing.T) {
	reader := &recordingVerificationReader{
		activity: &activitycore.Activity{ID: "wt", WorkspaceIDs: []workspace.WorkspaceID{"ws-1", "ws-2"}},
		byWorkspace: map[string]verificationapp.Inspection{
			"ws-1": {WorkspaceID: "ws-1", SourceGeneration: "g1", PolicyState: "active", Gate: verificationcore.GateEvaluation{Status: "blocked", Breakdown: verificationcore.GateBreakdown{EvidenceSatisfied: 7, Blocking: 1, Indeterminate: 2}}},
			"ws-2": {WorkspaceID: "ws-2", SourceGeneration: "g9", PolicyState: "policy_absent", Gate: verificationcore.GateEvaluation{Status: "indeterminate", Breakdown: verificationcore.GateBreakdown{Indeterminate: 4}}},
		},
	}
	resp := NewPlanner(reader).VerificationFacts(context.Background(), "wt")
	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(resp.Verification) != 2 {
		t.Fatalf("want two workspace entries, got %d", len(resp.Verification))
	}
	byID := map[string]protocol.WorkspaceVerification{}
	for _, entry := range resp.Verification {
		byID[entry.WorkspaceID] = entry
	}
	if byID["ws-1"].Blocking != 1 || byID["ws-1"].Satisfied != 7 || byID["ws-1"].Indeterminate != 2 {
		t.Fatalf("ws-1 counts = %+v", byID["ws-1"])
	}
	if byID["ws-2"].Indeterminate != 4 || byID["ws-2"].PolicyState != "policy_absent" {
		t.Fatalf("ws-2 = %+v", byID["ws-2"])
	}
	if byID["ws-1"].SourceGeneration == byID["ws-2"].SourceGeneration {
		t.Fatal("source generations were flattened")
	}
	for _, req := range reader.verificationRequests {
		if req.ActivityID != "wt" {
			t.Fatalf("verification read not activity-scoped: %+v", req)
		}
		if req.WorkspaceID == "" {
			t.Fatal("verification read issued without a host-derived workspace")
		}
	}
}

func TestVerificationFactsBoundsWorkspaceFanOut(t *testing.T) {
	many := make([]workspace.WorkspaceID, 0, protocol.MaxVerificationWorkspaces+3)
	for i := 0; i < protocol.MaxVerificationWorkspaces+3; i++ {
		many = append(many, workspace.WorkspaceID(fmt.Sprintf("ws-%d", i)))
	}
	reader := &recordingVerificationReader{activity: &activitycore.Activity{ID: "wt", WorkspaceIDs: many}, byWorkspace: map[string]verificationapp.Inspection{}}
	resp := NewPlanner(reader).VerificationFacts(context.Background(), "wt")
	if len(reader.verificationRequests) > protocol.MaxVerificationWorkspaces {
		t.Fatalf("issued %d reads, cap is %d", len(reader.verificationRequests), protocol.MaxVerificationWorkspaces)
	}
	if !resp.Coverage.Truncated || resp.Coverage.TruncationReason != "workspace_fan_out_capped" {
		t.Fatalf("fan-out cap was silent: %+v", resp.Coverage)
	}
}
