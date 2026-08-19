package mcp

import (
	"encoding/json"
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestDelegatedMCPV2DecodeValidationAndBridgeMappingPreserveModeAndEpoch(t *testing.T) {
	cases := []struct {
		raw   string
		check func(input)
	}{
		{`{"action":"start","operation_id":"op","command":"cat","cwd":"/tmp","session_mode":"delegated_interactive","stdin_mode":"stream","timeout_mode":"unlimited"}`, func(v input) {
			if v.SessionMode != delegated.ModeDelegatedInteractive {
				t.Fatalf("start=%#v", v)
			}
		}},
		{`{"action":"write","session_id":"s","authority_epoch":3,"input_offset":0,"chars":"x"}`, func(v input) {
			if v.AuthorityEpoch != 3 {
				t.Fatalf("write=%#v", v)
			}
		}},
		{`{"action":"kill","session_id":"s","authority_epoch":4,"kill_id":"kill","signal":"TERM"}`, func(v input) {
			if v.AuthorityEpoch != 4 {
				t.Fatalf("kill=%#v", v)
			}
		}},
	}
	for _, tc := range cases {
		raw := []byte(tc.raw)
		in, err := decodeInputV2(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateForVersion(2, in, raw); err != nil {
			t.Fatalf("validate %s: %v", tc.raw, err)
		}
		tc.check(in)
		req := requestFromInput(2, in, raw)
		switch in.Action {
		case "start":
			if req.Start.SessionMode != delegated.ModeDelegatedInteractive || req.Start.StdinMode != operation.StdinModeStream || req.Start.TimeoutMode != operation.TimeoutModeUnlimited {
				t.Fatalf("bridge start=%#v", req.Start)
			}
		case "write":
			if req.Write.AuthorityEpoch != 3 {
				t.Fatalf("bridge write=%#v", req.Write)
			}
		case "kill":
			if req.Kill.AuthorityEpoch != 4 {
				t.Fatalf("bridge kill=%#v", req.Kill)
			}
		}
	}
}

func TestDelegatedMCPRejectsLegacyFieldsUnknownModeAndExplicitZeroEpoch(t *testing.T) {
	legacy := []byte(`{"action":"start","operation_id":"op","command":"cat","cwd":"/tmp","session_mode":"delegated_interactive"}`)
	in, err := decodeInputV2(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(1, in, legacy); err == nil {
		t.Fatal("v1 accepted delegated session_mode")
	}
	bad := [][]byte{
		[]byte(`{"action":"start","operation_id":"op","command":"cat","cwd":"/tmp","session_mode":"future_mode"}`),
		[]byte(`{"action":"start","operation_id":"op","command":"cat","cwd":"/tmp","session_mode":"delegated_interactive","tty":true}`),
		[]byte(`{"action":"write","session_id":"s","authority_epoch":0,"input_offset":0,"chars":"x"}`),
		[]byte(`{"action":"kill","session_id":"s","authority_epoch":0,"kill_id":"k"}`),
	}
	for _, raw := range bad {
		got, err := decodeInputV2(raw)
		if err != nil {
			continue
		}
		if err := validateForVersion(2, got, raw); err == nil {
			t.Fatalf("invalid MCP accepted: %s", raw)
		}
	}
}

func TestDelegatedControlViewProjectsAuthorityEpochOnlyWhenPresent(t *testing.T) {
	got := controlView(app.View{SessionID: "s", State: "running", AuthorityEpoch: 5})
	if got["authority_epoch"] != delegated.AuthorityEpoch(5) {
		t.Fatalf("view=%#v", got)
	}
	legacy := controlView(app.View{SessionID: "s", State: "running"})
	if _, ok := legacy["authority_epoch"]; ok {
		t.Fatalf("legacy leaked epoch: %#v", legacy)
	}
}

func TestLegacyDelegatedProjectionOmitsCapabilityAndModernReceipt(t *testing.T) {
	modern := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{
		ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096,
	})
	legacy := legacyCatalogView(modern)
	if _, ok := legacy.Features[capability.FeatureDelegatedInteractive]; ok || legacy.DelegatedInteractive != nil {
		t.Errorf("legacy catalog leaked delegated support: %#v", legacy)
	}
	for _, version := range legacy.ReceiptSchemaVersions {
		if version > 2 {
			t.Fatalf("legacy catalog leaked receipt v%d: %v", version, legacy.ReceiptSchemaVersions)
		}
	}

	v5 := &receipt.Receipt{SchemaVersion: 5, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1}
	result := successV1("poll", bridge.Response{View: app.View{SessionID: "s", State: "completed", Receipt: v5}})
	body, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("legacy structured content=%#v", result.StructuredContent)
	}
	wire, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["receipt"] != nil {
		t.Fatalf("legacy poll leaked modern receipt on wire: %s", wire)
	}
	if _, ok := decoded["authority_epoch"]; ok {
		t.Fatalf("legacy poll leaked authority epoch: %#v", body)
	}
}
