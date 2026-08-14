package schema

import (
	"encoding/json"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"testing"
)

func TestEventInspectSchemasAreClosedAndBounded(t *testing.T) {
	validInputs := []string{
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"session","session_id":"session-1"},"after_event_cursor":"evtcur_v1_abc.def","max_events":1}`,
	}
	for _, raw := range validInputs {
		if err := validateEventSchemaJSON(t, MCPInputV2, raw); err != nil {
			t.Fatalf("valid MCP input rejected %s: %v", raw, err)
		}
	}
	invalidInputs := []string{
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1","session_id":"session-1"},"max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"unknown","operation_id":"op-1"},"max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"cursor":1,"max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"after_event_cursor":"outcur_v1_bad","max_events":64}`,
		`{"action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":257}`,
	}
	for _, raw := range invalidInputs {
		if err := validateEventSchemaJSON(t, MCPInputV2, raw); err == nil {
			t.Fatalf("invalid MCP input accepted %s", raw)
		}
	}
	ipc := `{"ipc_version":2,"kind":"request","request_id":"events","action":"inspect.events","target":{"kind":"operation","operation_id":"op-1"},"max_events":64}`
	if err := validateEventSchemaJSON(t, IPCV2, ipc); err != nil {
		t.Fatalf("valid IPC event request rejected: %v", err)
	}
	output := map[string]any{
		"schema_version": 2, "ok": true, "action": "inspect.events",
		"events": map[string]any{
			"events": []any{}, "next_event_cursor": "evtcur_v1_abc.def", "continuity": "complete", "truncated": false,
		},
	}
	encoded, _ := json.Marshal(output)
	if err := validateEventSchemaJSON(t, MCPOutputV2, string(encoded)); err != nil {
		t.Fatalf("valid MCP event output rejected: %v", err)
	}
}

func validateEventSchemaJSON(t *testing.T, name Name, raw string) error {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return err
	}
	return resolvedSchema(t, name).Validate(value)
}

func TestEventJournalCatalogExtensionsValidateOnlyWhenPresent(t *testing.T) {
	base := capability.Baseline(capability.Limits{})
	encoded, err := json.Marshal(map[string]any{"schema_version": 2, "ok": true, "action": "inspect.server", "server": base})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEventSchemaJSON(t, MCPOutputV2, string(encoded)); err != nil {
		t.Fatalf("baseline catalog rejected: %v", err)
	}
	composed := base.WithEventJournal(256, 2048, 64, true)
	encoded, err = json.Marshal(map[string]any{"schema_version": 2, "ok": true, "action": "inspect.server", "server": composed})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateEventSchemaJSON(t, MCPOutputV2, string(encoded)); err != nil {
		t.Fatalf("event catalog rejected: %v", err)
	}
}
