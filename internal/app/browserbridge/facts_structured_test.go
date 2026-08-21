package browserbridge

import (
	"context"
	"fmt"
	"testing"
	"time"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type structuredCall struct {
	operationID string
	testStatus  structuredcore.TestStatus
	maxRecords  int
}

type recordingStructuredReader struct {
	stubDaemonReader
	activity     *activitycore.Activity
	result       structuredapp.InspectResult
	calls        []structuredCall
	operationIDs []string
}

func (r *recordingStructuredReader) Activity(_ context.Context, _ string) (*activitycore.Activity, bool, error) {
	return r.activity, r.activity != nil, nil
}

func (r *recordingStructuredReader) Structured(_ context.Context, operationID string, testStatus structuredcore.TestStatus, maxRecords int) (*structuredapp.InspectResult, bool, error) {
	r.calls = append(r.calls, structuredCall{operationID: operationID, testStatus: testStatus, maxRecords: maxRecords})
	r.operationIDs = append(r.operationIDs, operationID)
	result := r.result
	result.OperationID = operationID
	return &result, true, nil
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
	if len(reader.calls) != 1 {
		t.Fatalf("expected one structured read, got %d", len(reader.calls))
	}
	if reader.calls[0].testStatus != structuredcore.TestFailed {
		t.Fatalf("test_status filter = %q, want fail", reader.calls[0].testStatus)
	}
	if reader.calls[0].maxRecords != protocol.MaxFailingCasesPerOperation {
		t.Fatalf("max_records = %d", reader.calls[0].maxRecords)
	}
	if reader.calls[0].operationID != "op-1" {
		t.Fatalf("operation id = %q", reader.calls[0].operationID)
	}
}
