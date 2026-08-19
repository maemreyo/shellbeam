package mcp

import "testing"

func TestPreflightV2InvalidJSONIsBounded(t *testing.T) {
	action, details := preflightV2Input([]byte(`{"action":"start"`))
	if action != "" || details["reason"] != "invalid_json" || len(details) != 1 {
		t.Fatalf("action=%q details=%#v", action, details)
	}
}
