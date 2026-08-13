package contract_test

import (
	"encoding/json"
	"testing"

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
	for _, name := range schema.Names() {
		data, _ := schema.Load(name)
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		if _, ok := v["additionalProperties"]; !ok {
			t.Fatalf("%s lacks root closure", name)
		}
	}
}
