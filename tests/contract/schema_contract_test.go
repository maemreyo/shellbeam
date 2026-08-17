package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	schema "github.com/maemreyo/shellbeam/api/schema"
)

func TestSchemaInventory(t *testing.T) {
	for _, name := range schema.Names() {
		data, err := schema.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		var h struct {
			Schema string `json:"$schema"`
			ID     string `json:"$id"`
			Type   string `json:"type"`
		}
		if err := json.Unmarshal(data, &h); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if h.Schema != "https://json-schema.org/draft/2020-12/schema" || h.ID == "" || h.Type != "object" {
			t.Fatalf("%s header=%#v", name, h)
		}
	}
}

func TestSchemasAreClosedAtRoot(t *testing.T) {
	// additionalProperties only sees this schema's own "properties"/"patternProperties";
	// it does not see into oneOf/anyOf branches. A root schema composed entirely via
	// oneOf (no root "properties") must close with unevaluatedProperties instead, or
	// additionalProperties:false there rejects every field, including valid ones.
	for _, name := range schema.Names() {
		data, _ := schema.Load(name)
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		_, hasAdditional := v["additionalProperties"]
		_, hasUnevaluated := v["unevaluatedProperties"]
		if !hasAdditional && !hasUnevaluated {
			t.Fatalf("%s lacks root closure", name)
		}
		if _, hasProps := v["properties"]; !hasProps && hasAdditional {
			t.Fatalf("%s uses additionalProperties at root without root properties; every field will be rejected (use unevaluatedProperties with oneOf/anyOf instead)", name)
		}
	}
}

// Regression test for a schema-composition bug found via a live MCP client
// (ChatGPT) rejecting every local_shell call: mcp-input-v1.json had root
// additionalProperties:false with no root properties, so its oneOf branches'
// fields were always "additional". Validate real payloads through the same
// library the SDK/clients use, not just the Go decoder's flat struct, since
// that decoder never exercises the declared JSON Schema at all.
func TestMCPInputSchemaValidatesRealPayloads(t *testing.T) {
	data, err := schema.Load(schema.MCPInputV1)
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}

	valid := []map[string]any{
		{"action": "start", "operation_id": "op-1", "command": "echo hi", "cwd": "/tmp"},
		{"action": "poll", "session_id": "s-1"},
		{"action": "write", "session_id": "s-1", "input_offset": 0, "chars": "hi"},
		{"action": "write", "session_id": "s-1", "input_offset": 0, "eof": true},
		{"action": "kill", "session_id": "s-1", "kill_id": "k-1"},
	}
	for _, p := range valid {
		if err := rs.Validate(p); err != nil {
			t.Errorf("expected valid, got error for %v: %v", p, err)
		}
	}

	invalid := []map[string]any{
		{"action": "start", "operation_id": "op-1", "command": "echo hi", "cwd": "/tmp", "bogus": 1},
		{"action": "start", "operation_id": "op-1", "command": "echo hi", "cwd": "/tmp", "kill_id": "k-1"},
		{"action": "write", "session_id": "s-1", "input_offset": 0, "chars": "hi", "eof": true},
	}
	for _, p := range invalid {
		if err := rs.Validate(p); err == nil {
			t.Errorf("expected invalid, got no error for %v", p)
		}
	}
}

func TestReadMediaFragmentsAreInventorySchemasNotBaseV2Exposure(t *testing.T) {
	for _, name := range []schema.Name{schema.MCPReadMediaInputV1, schema.MCPReadMediaOutputV1} {
		if _, err := schema.Load(name); err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
	}
	base, err := schema.Load(schema.MCPInputV2)
	if err != nil {
		t.Fatal(err)
	}
	if string(base) == "" {
		t.Fatal("empty v2 schema")
	}
	var doc map[string]any
	if err := json.Unmarshal(base, &doc); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(doc)
	if strings.Contains(string(body), `"read_media"`) {
		t.Fatal("base MCP v2 schema exposes read_media unconditionally")
	}
}
