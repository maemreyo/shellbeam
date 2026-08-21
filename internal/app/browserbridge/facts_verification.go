package browserbridge

import (
	"context"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// VerificationFacts reads verification per host-derived workspace and never
// aggregates workspaces with independent policy/source generations.
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
		v, found, err := p.reader.Verification(ctx, string(id), correlationID)
		if err != nil {
			return unreachable(protocol.VerbVerificationFacts)
		}
		if !found || v == nil {
			continue
		}
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
