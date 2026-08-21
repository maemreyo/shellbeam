package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// VerificationFacts runs the verification_facts read plan.
//
// inspect.verification resolves a workspace before deriving an affected
// surface, so the plan must learn the workspaces from the activity record
// first. Results stay per workspace: each workspace has its own policy,
// policy generation and authority, so a summed count would answer no
// evaluable question.
func (p *Planner) VerificationFacts(ctx context.Context, correlationID string) protocol.Response {
	act, failure, ok := p.activity(ctx, protocol.VerbVerificationFacts, correlationID)
	if !ok {
		return failure
	}
	out := base(protocol.VerbVerificationFacts, protocol.StatusOK)
	out.Coverage = coverageFor(act.CompactedOperations)
	workspaces := act.WorkspaceIDs
	if len(workspaces) > protocol.MaxVerificationWorkspaces {
		workspaces = workspaces[:protocol.MaxVerificationWorkspaces]
		out.Coverage.Truncated = true
		out.Coverage.TruncationReason = "workspace_fan_out_capped"
	}
	for _, id := range workspaces {
		resp, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-verification", Action: "inspect.verification", WorkspaceID: string(id), ActivityID: correlationID})
		if err != nil {
			return unreachable(protocol.VerbVerificationFacts)
		}
		if !resp.OK || resp.Verification == nil {
			continue
		}
		v := resp.Verification
		out.Verification = append(out.Verification, protocol.WorkspaceVerification{
			WorkspaceID:      v.WorkspaceID,
			PolicyState:      string(v.PolicyState),
			GateStatus:       string(v.Gate.Status),
			SourceGeneration: v.SourceGeneration,
			Satisfied:        v.Gate.Breakdown.EvidenceSatisfied,
			Waived:           v.Gate.Breakdown.Waived,
			Blocking:         v.Gate.Breakdown.Blocking,
			Indeterminate:    v.Gate.Breakdown.Indeterminate,
		})
	}
	if len(out.Verification) == 0 && !out.Coverage.Truncated {
		return unavailable(protocol.VerbVerificationFacts, "no_workspace_verification")
	}
	return out
}
