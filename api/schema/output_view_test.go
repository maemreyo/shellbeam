package schema

import "testing"

func TestOutputViewV2SchemasAcceptClosedBoundedRequestsAndResponses(t *testing.T) {
	selector := map[string]any{"kind": "search", "mode": "literal", "pattern": "boom", "case_sensitive": true, "max_matches": 10.0}
	validInputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": selector}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "out", "action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "raw_range", "start_byte": 2.0, "max_bytes": 32.0}}},
	}
	for _, tc := range validInputs {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected valid output request %#v: %v", tc.name, tc.value, err)
		}
	}

	view := map[string]any{
		"schema_version":   1.0,
		"session_id":       "s",
		"selector_kind":    "search",
		"retention_state":  "retained",
		"frozen_cut_bytes": 42.0,
		"raw_ranges":       []any{map[string]any{"start": 4.0, "end": 8.0}},
		"text":             "boom",
		"matches":          []any{map[string]any{"line": 2.0, "raw_range": map[string]any{"start": 4.0, "end": 8.0}, "excerpt": "boom"}},
		"partial":          true,
		"truncated":        false,
		"continuation":     "outcur_v1_abc.def",
	}
	validOutputs := []struct {
		name  Name
		value map[string]any
	}{
		{MCPOutputV2, map[string]any{"schema_version": 2.0, "ok": true, "action": "read_output", "output_view": view}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "response", "request_id": "out", "action": "read_output", "ok": true, "output_view": view}},
	}
	for _, tc := range validOutputs {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err != nil {
			t.Errorf("%s rejected valid output response: %v", tc.name, err)
		}
	}
}

func TestOutputViewV2SchemasRejectUnboundedOrCrossActionShapes(t *testing.T) {
	invalid := []struct {
		name  Name
		value map[string]any
	}{
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s"}},
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "raw_range", "max_bytes": 65537.0}}},
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "tail", "bytes": 1.0, "lines": 1.0}}},
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "search", "mode": "regex", "pattern": "x", "max_matches": 129.0}}},
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "search", "mode": "glob", "pattern": "x", "max_matches": 1.0}}},
		{MCPInputV2, map[string]any{"action": "read_output", "session_id": "s", "selector": map[string]any{"kind": "raw_range", "max_bytes": 1.0, "unknown": true}}},
		{MCPInputV2, map[string]any{"action": "poll", "session_id": "s", "selector": map[string]any{"kind": "raw_range", "max_bytes": 1.0}}},
		{IPCV2, map[string]any{"ipc_version": 2.0, "kind": "request", "request_id": "out", "action": "read_output", "session_id": "s", "cursor": 1.0, "selector": map[string]any{"kind": "raw_range", "max_bytes": 1.0}}},
	}
	for _, tc := range invalid {
		if err := resolvedSchema(t, tc.name).Validate(tc.value); err == nil {
			t.Errorf("%s accepted invalid output request %#v", tc.name, tc.value)
		}
	}
}
