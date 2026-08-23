package schema

import "testing"

func TestDelegatedPrivateOutputSchemasAcceptPartialAndCanonicalCompositeTruth(t *testing.T) {
	for _, reasons := range [][]any{
		{"private_intervals_omitted"},
		{"private_intervals_omitted", "transport_gap", "provider_lost"},
	} {
		receipt := delegatedReceiptV5SchemaPayload()
		result := delegatedResultV5SchemaPayload(receipt)
		quality := "partial"
		if len(reasons) > 1 {
			quality = "incomplete"
		}
		for _, target := range []map[string]any{receipt, result["output"].(map[string]any)} {
			target["output_complete"] = false
			target["capture_quality"] = quality
			target["capture_reasons"] = append([]any(nil), reasons...)
		}
		if err := resolvedSchema(t, MCPOutputV2).Validate(map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}); err != nil {
			t.Fatalf("MCP rejected %v: %v", reasons, err)
		}
		if err := resolvedSchema(t, IPCV2).Validate(map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "private", "action": "start", "ok": true, "result": result}); err != nil {
			t.Fatalf("IPC rejected %v: %v", reasons, err)
		}
	}
}

func TestDelegatedPrivateOutputSchemasRejectNonCanonicalCaptureReasonOrder(t *testing.T) {
	receipt := delegatedReceiptV5SchemaPayload()
	result := delegatedResultV5SchemaPayload(receipt)
	bad := []any{"private_intervals_omitted", "provider_lost", "transport_gap"}
	for _, target := range []map[string]any{receipt, result["output"].(map[string]any)} {
		target["output_complete"] = false
		target["capture_quality"] = "incomplete"
		target["capture_reasons"] = bad
	}
	for _, tc := range []struct {
		name    string
		schema  Name
		payload map[string]any
	}{
		{"mcp", MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "start", "result": result}},
		{"ipc", IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "private", "action": "start", "ok": true, "result": result}},
	} {
		if err := resolvedSchema(t, tc.schema).Validate(tc.payload); err == nil {
			t.Errorf("%s accepted non-canonical reasons", tc.name)
		}
	}
}
