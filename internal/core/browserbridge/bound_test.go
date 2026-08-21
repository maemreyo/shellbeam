package browserbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundResponseLeavesSmallResponsesIntact(t *testing.T) {
	in := Response{ProtocolVersion: ProtocolVersion, SupportedVersions: SupportedVersions(), Verb: VerbActivityFacts, Status: StatusOK, Activity: &ActivityFacts{Found: true, OperationsRetained: 3}}
	raw, err := BoundResponse(in)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Coverage.Truncated {
		t.Fatal("small response marked truncated")
	}
	if out.Activity == nil || out.Activity.OperationsRetained != 3 {
		t.Fatal("content lost")
	}
}

func TestBoundResponseTruncatesExplicitlyAndStaysUnderHardCap(t *testing.T) {
	big := Response{ProtocolVersion: ProtocolVersion, SupportedVersions: SupportedVersions(), Verb: VerbStructuredFailureFacts, Status: StatusOK}
	for i := 0; i < MaxStructuredOperations; i++ {
		entry := OperationFailureFacts{OperationID: "op-" + strings.Repeat("x", 32), TestFailed: 40}
		for j := 0; j < MaxFailingCasesPerOperation; j++ {
			entry.FailingCases = append(entry.FailingCases, FailingCase{Name: strings.Repeat("case", 512), Package: strings.Repeat("pkg", 512), Status: "fail"})
		}
		big.Structured = append(big.Structured, entry)
	}
	raw, err := BoundResponse(big)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(raw) > MaxResponseBytes {
		t.Fatalf("response %d exceeds hard cap %d", len(raw), MaxResponseBytes)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Coverage.Truncated || out.Coverage.TruncationReason == "" {
		t.Fatal("truncation was silent")
	}
}
