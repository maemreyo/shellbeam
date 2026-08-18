package schema

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestE27InputTraceSchemasExposeClosedStartInspectCapabilityAndEvents(t *testing.T) {
	for _, name := range []string{"ipc-v2.json", "mcp-input-v2.json"} {
		root := readE27Schema(t, name)
		start := findE27ActionBranch(root, "start")
		if start == nil {
			t.Fatalf("%s missing start branch", name)
		}
		mode := e27Property(start, "trace_mode")
		if mode == nil || !reflect.DeepEqual(stringList(mode["enum"]), []string{"off", "best_effort", "required"}) {
			t.Fatalf("%s trace_mode=%#v", name, mode)
		}
		inspect := findE27ActionBranchWithProperties(root, "inspect.trace", "operation_id", "max_resources")
		if inspect == nil {
			t.Fatalf("%s inspect.trace=%#v", name, inspect)
		}
	}
	for _, name := range []string{"ipc-v2.json", "mcp-output-v2.json"} {
		root := readE27Schema(t, name)
		if !hasE27Definition(root, "input_trace_record") || !hasE27Definition(root, "input_trace_inspection") {
			t.Fatalf("%s missing E27 defs", name)
		}
		encoded, _ := json.Marshal(root)
		text := string(encoded)
		for _, required := range []string{"input_trace_recorded", "input_trace_truncated", "input_tracing"} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing %q", name, required)
			}
		}
		for _, forbidden := range []string{"file_contents", "environment_values", "network_payload", "socket_path", "dylib_path", "raw_events"} {
			if strings.Contains(text, `"`+forbidden+`":`) {
				t.Fatalf("%s exposes forbidden field %q", name, forbidden)
			}
		}
		defs, _ := root["$defs"].(map[string]any)
		for key, value := range defs {
			if !strings.HasPrefix(key, "input_trace_") {
				continue
			}
			encoded, _ := json.Marshal(value)
			if strings.Contains(string(encoded), `"proven_input_scope":`) {
				t.Fatalf("%s trace definition %q may not publish proven_input_scope", name, key)
			}
		}
	}
}

func TestE27InputTraceConfigSchemaIsOptInBoolean(t *testing.T) {
	root := readE27Schema(t, "config-v1.json")
	props := root["properties"].(map[string]any)
	v := props["experimental_input_tracing"].(map[string]any)
	if v["type"] != "boolean" || v["default"] != false {
		t.Fatalf("config=%#v", v)
	}
}

func readE27Schema(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	return root
}
func findE27ActionBranch(v any, action string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if props, ok := x["properties"].(map[string]any); ok {
			if a, ok := props["action"].(map[string]any); ok {
				if a["const"] == action {
					return x
				}
				if vals := stringList(a["enum"]); len(vals) == 1 && vals[0] == action {
					return x
				}
			}
		}
		for _, child := range x {
			if got := findE27ActionBranch(child, action); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range x {
			if got := findE27ActionBranch(child, action); got != nil {
				return got
			}
		}
	}
	return nil
}

func findE27ActionBranchWithProperties(v any, action string, names ...string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if props, ok := x["properties"].(map[string]any); ok {
			if a, ok := props["action"].(map[string]any); ok && a["const"] == action {
				complete := true
				for _, name := range names {
					if _, ok := props[name]; !ok {
						complete = false
						break
					}
				}
				if complete {
					return x
				}
			}
		}
		for _, child := range x {
			if got := findE27ActionBranchWithProperties(child, action, names...); got != nil {
				return got
			}
		}
	case []any:
		for _, child := range x {
			if got := findE27ActionBranchWithProperties(child, action, names...); got != nil {
				return got
			}
		}
	}
	return nil
}

func e27Property(branch map[string]any, name string) map[string]any {
	props, _ := branch["properties"].(map[string]any)
	v, _ := props[name].(map[string]any)
	return v
}
func stringList(v any) []string {
	xs, _ := v.([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func hasE27Definition(root map[string]any, name string) bool {
	defs, _ := root["$defs"].(map[string]any)
	_, ok := defs[name]
	return ok
}

func TestE27InputTraceMCPOutputSchemaMatchesConciseStartAndDeepInspectShapes(t *testing.T) {
	result := map[string]any{
		"schema_version": 2.0,
		"operation":      map[string]any{"operation_id": "e27-schema", "session_id": "s-e27", "state": "running"},
		"child":          map[string]any{"state": "running", "timed_out": false},
		"output":         map[string]any{"canonical_stream": "combined", "raw_bytes": 0.0, "returned_bytes": 0.0, "cursor": 0.0, "next_cursor": 0.0, "truncated": false, "output_complete": false},
	}
	start := map[string]any{
		"schema_version": 2.0, "ok": true, "action": "start", "result": result,
		"input_trace": map[string]any{"requested_mode": "best_effort", "status": "pending", "trace_id": "trace_01K00000000000000000000000"},
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(start); err != nil {
		t.Fatalf("concise MCP start trace status rejected: %v", err)
	}
	deep := map[string]any{
		"schema_version": 2.0, "ok": true, "action": "inspect.trace",
		"input_trace": map[string]any{"schema_version": 1.0, "status": "pending", "operation_id": "e27-schema", "trace_id": "trace_01K00000000000000000000000"},
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(deep); err != nil {
		t.Fatalf("deep MCP trace inspection rejected: %v", err)
	}
	leakyStart := map[string]any{
		"schema_version": 2.0, "ok": true, "action": "start", "result": result,
		"input_trace": map[string]any{"requested_mode": "best_effort", "status": "pending", "raw_events": []any{}},
	}
	if err := resolvedSchema(t, MCPOutputV2).Validate(leakyStart); err == nil {
		t.Fatal("MCP start trace status accepted deep/private raw_events")
	}
}
