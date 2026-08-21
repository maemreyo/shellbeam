package browserbridge

import (
	"context"
	"fmt"
	"testing"
	"time"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type recordingStructuredReader struct {
	activity     *activitycore.Activity
	result       structuredapp.InspectResult
	requests     []ipc.RequestV2
	operationIDs []string
}

func (r *recordingStructuredReader) Read(_ context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	switch req.Action {
	case "inspect.activity":
		return ipc.ResponseV2{OK: true, Activity: r.activity}, nil
	case "inspect.structured":
		r.requests = append(r.requests, req)
		r.operationIDs = append(r.operationIDs, req.OperationID)
		result := r.result
		result.OperationID = req.OperationID
		return ipc.ResponseV2{OK: true, Structured: &result}, nil
	default:
		return ipc.ResponseV2{OK: false, Error: &ipc.Error{Code: "unexpected_action"}}, nil
	}
}

func TestStructuredFailureFactsWalksRetainedOperationsNewestFirstAndBounds(t *testing.T) {
	refs := make([]activitycore.OperationRef, 0, protocol.MaxStructuredOperations+4)
	for i := 0; i < protocol.MaxStructuredOperations+4; i++ {
		refs = append(refs, activitycore.OperationRef{OperationID: fmt.Sprintf("op-%d", i), SessionID: "s", ObservedAt: time.Unix(int64(i), 0).UTC()})
	}
	reader := &recordingStructuredReader{
		activity: &activitycore.Activity{ID: "wt", Operations: refs, CompactedOperations: 5},
		result: structuredapp.InspectResult{
			Status:   structuredapp.InspectTerminal,
			Producer: &structuredcore.Producer{AdapterID: "pytest-junit-xml", AdapterVersion: 1},
			Summary:  structuredapp.InspectSummary{TestFailed: 2, TestPassed: 8},
			Records: []structuredcore.Record{
				{RecordKind: structuredcore.RecordTestCase, Authority: structuredcore.AuthorityMechanical, DerivationMethod: structuredcore.DerivationDeterministicNormalize, TestCase: &structuredcore.TestCase{Name: "test_a", Package: "pkg", Status: structuredcore.TestFailed}},
			},
		},
	}
	resp := NewPlanner(reader).StructuredFailureFacts(context.Background(), "wt")

	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(reader.operationIDs) != protocol.MaxStructuredOperations {
		t.Fatalf("walked %d operations, cap is %d", len(reader.operationIDs), protocol.MaxStructuredOperations)
	}
	newest := fmt.Sprintf("op-%d", protocol.MaxStructuredOperations+3)
	if reader.operationIDs[0] != newest {
		t.Fatalf("walk did not start at the newest ref: %q", reader.operationIDs[0])
	}
	if resp.Coverage.CompactedOperations != 5 || resp.Coverage.HistoricalOperations != "partial" {
		t.Fatalf("compaction not surfaced: %+v", resp.Coverage)
	}
	if !resp.Coverage.Truncated {
		t.Fatal("walk cap not surfaced")
	}
	first := resp.Structured[0]
	if first.Authority != "mechanical" || first.DerivationMethod != "deterministic_normalization" || first.AdapterID != "pytest-junit-xml" {
		t.Fatalf("comparability fields lost: %+v", first)
	}
	if len(first.FailingCases) != 1 || first.FailingCases[0].Name != "test_a" {
		t.Fatalf("failing cases = %+v", first.FailingCases)
	}
}

func TestStructuredFailureFactsRequestsOnlyFailingRecords(t *testing.T) {
	reader := &recordingStructuredReader{activity: &activitycore.Activity{ID: "wt", Operations: []activitycore.OperationRef{{OperationID: "op-1", SessionID: "s"}}}, result: structuredapp.InspectResult{Status: structuredapp.InspectTerminal}}
	NewPlanner(reader).StructuredFailureFacts(context.Background(), "wt")
	if len(reader.requests) != 1 {
		t.Fatalf("expected one structured read, got %d", len(reader.requests))
	}
	if reader.requests[0].TestStatus != structuredcore.TestFailed {
		t.Fatalf("test_status filter = %q, want fail", reader.requests[0].TestStatus)
	}
	if reader.requests[0].MaxRecords != protocol.MaxFailingCasesPerOperation {
		t.Fatalf("max_records = %d", reader.requests[0].MaxRecords)
	}
}
