package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

type structuredInspectClient struct {
	last       bridge.Request
	startCalls int
}

func (c *structuredInspectClient) Forward(_ context.Context, req bridge.Request) (bridge.Response, error) {
	c.last = req
	if req.Action == "start" {
		c.startCalls++
	}
	if req.Action == "inspect.structured" {
		result := structuredapp.InspectResult{SchemaVersion: 1, OperationID: req.StructuredInspect.OperationID, Status: structuredapp.InspectTerminal, ParseOutcome: core.ParsePartial, Completeness: core.CompletenessPartial, CompletenessReason: core.CompletenessReasonPassRecordsElided, ObservedEntries: &core.ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 2, Pass: 1, Fail: 1}, SourceKind: core.StructuredInputArtifactBlob, SourceState: structuredapp.InputSourceRetained, SemanticsCoverage: &core.ProducerSemanticsCoverage{Namespace: "pytest", VocabularyVersion: 1, Format: "junit-xml", Family: "xunit2", MechanicallyObservable: []string{"coarse:pass"}, Unavailable: []string{"pytest:xpass_exact"}}, Records: []core.Record{mcpTransportFailureRecord()}, Summary: structuredapp.InspectSummary{DetailsStatus: structuredapp.DetailsAvailable, RecordsTotalExact: true}}
		return bridge.Response{Structured: &result}, nil
	}
	if req.Action == "inspect.server" {
		catalog := capability.Baseline(capability.Limits{})
		return bridge.Response{Server: &catalog}, nil
	}
	return bridge.Response{}, nil
}

func mcpTransportFailureRecord() core.Record {
	return core.Record{
		SchemaVersion: core.RecordSchemaVersionV3, RecordKind: core.RecordTestCase, Authority: core.AuthorityMechanical,
		DerivationMethod: core.DerivationNativeFieldMapping, Producer: core.Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}, OperationID: "op-1",
		SourceRef: core.RawInputRef(core.RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 1, SHA256: strings.Repeat("a", 64)}),
		TestCase:  &core.TestCase{Name: "fails", Status: core.TestFailed, FailureExcerpt: &core.FailureExcerpt{Namespace: "jest", VocabularyVersion: 1, Text: "failure"}},
	}
}

func TestStructuredInspectMCPV2ForwardsFiltersWithoutSpawn(t *testing.T) {
	client := &structuredInspectClient{}
	session, closeSession := currentSession(t, New(bridge.New(client), capability.Baseline(capability.Limits{})))
	defer closeSession()
	res, err := session.CallTool(context.Background(), &mcpgo.CallToolParams{Name: "local_shell", Arguments: json.RawMessage(`{"action":"inspect.structured","operation_id":"op-1","record_kind":"diagnostic","severity":"error","path":"internal/a.go","max_records":10}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError || client.startCalls != 0 || client.last.Action != "inspect.structured" || client.last.StructuredInspect.Filter.Path != "internal/a.go" {
		t.Fatalf("result=%#v request=%#v starts=%d", res, client.last, client.startCalls)
	}
	body, ok := res.StructuredContent.(map[string]any)
	if !ok || body["structured"] == nil {
		t.Fatalf("structured=%#v", res.StructuredContent)
	}
	structuredBody, _ := body["structured"].(map[string]any)
	if structuredBody["source_kind"] != "artifact_blob" || structuredBody["source_state"] != "retained" || structuredBody["semantics_coverage"] == nil || structuredBody["completeness_reason"] != "pass_records_elided" || structuredBody["observed_entries"] == nil {
		t.Fatalf("structured=%#v", structuredBody)
	}
	records, _ := structuredBody["records"].([]any)
	if len(records) != 1 {
		t.Fatalf("failure_excerpt records=%#v", structuredBody["records"])
	}
	record, _ := records[0].(map[string]any)
	testCase, _ := record["test_case"].(map[string]any)
	excerpt, _ := testCase["failure_excerpt"].(map[string]any)
	if excerpt["text"] != "failure" {
		t.Fatalf("failure_excerpt missing from MCP response: %#v", structuredBody["records"])
	}
	encoded, _ := json.Marshal(structuredBody)
	for _, forbidden := range []string{`"xpassed"`, `"xpass_count"`, `"error_phase"`, `"xfail_execution_state"`} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("invented completion claim %s in %s", forbidden, encoded)
		}
	}
}
