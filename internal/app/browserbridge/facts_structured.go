package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

// StructuredFailureFacts runs the structured_failure_facts read plan.
//
// inspect.structured is operation-scoped, so the plan walks the activitys
// retained operation refs newest-first under a hard cap. Two coverage losses
// are possible and are reported separately: operations the activity already
// compacted away, and refs this walk did not reach.
func (p *Planner) StructuredFailureFacts(ctx context.Context, correlationID string) protocol.Response {
	act, failure, ok := p.activity(ctx, protocol.VerbStructuredFailureFacts, correlationID)
	if !ok {
		return failure
	}
	out := base(protocol.VerbStructuredFailureFacts, protocol.StatusOK)
	out.Coverage = coverageFor(act.CompactedOperations)
	walked := 0
	for i := len(act.Operations) - 1; i >= 0; i-- {
		if walked >= protocol.MaxStructuredOperations {
			out.Coverage.Truncated = true
			out.Coverage.TruncationReason = "operation_walk_capped"
			break
		}
		walked++
		ref := act.Operations[i]
		resp, err := p.reader.Read(ctx, ipc.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "bb-structured", Action: "inspect.structured",
			OperationID: ref.OperationID, TestStatus: structuredcore.TestFailed,
			MaxRecords: protocol.MaxFailingCasesPerOperation,
		})
		if err != nil {
			return unreachable(protocol.VerbStructuredFailureFacts)
		}
		if !resp.OK || resp.Structured == nil {
			continue
		}
		out.Structured = append(out.Structured, failureFacts(ref.OperationID, resp.Structured))
	}
	return out
}

type structuredInspect = structuredapp.InspectResult

func failureFacts(operationID string, in *structuredInspect) protocol.OperationFailureFacts {
	facts := protocol.OperationFailureFacts{
		OperationID:  operationID,
		Completeness: string(in.Completeness),
		TestPassed:   in.Summary.TestPassed,
		TestFailed:   in.Summary.TestFailed,
		Errors:       in.Summary.Errors,
	}
	if in.Producer != nil {
		facts.AdapterID = in.Producer.AdapterID
		facts.AdapterVersion = in.Producer.AdapterVersion
	}
	for _, record := range in.Records {
		if record.TestCase == nil {
			continue
		}
		if len(facts.FailingCases) >= protocol.MaxFailingCasesPerOperation {
			facts.CasesTruncated = true
			break
		}
		facts.Authority = string(record.Authority)
		facts.DerivationMethod = string(record.DerivationMethod)
		facts.FailingCases = append(facts.FailingCases, protocol.FailingCase{Name: record.TestCase.Name, Package: record.TestCase.Package, Status: string(record.TestCase.Status)})
	}
	if in.Summary.Truncated {
		facts.CasesTruncated = true
	}
	return facts
}
