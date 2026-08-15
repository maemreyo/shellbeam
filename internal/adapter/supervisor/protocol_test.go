package supervisor

import (
	"strings"
	"testing"
)

func TestDecodeRequestAcceptsOnlyClosedProtocolV1Kinds(t *testing.T) {
	valid := []string{
		`{"protocol_version":1,"kind":"handshake","session_id":"persistent-session-a","generation_id":"generation-a","handshake":{"challenge":"abc","proof":"def"}}`,
		`{"protocol_version":1,"kind":"status","session_id":"persistent-session-a","generation_id":"generation-a"}`,
		`{"protocol_version":1,"kind":"output","session_id":"persistent-session-a","generation_id":"generation-a","output":{"offset":0,"max_bytes":1024}}`,
		`{"protocol_version":1,"kind":"write","session_id":"persistent-session-a","generation_id":"generation-a","write":{"input_offset":0,"chars":"hello"}}`,
		`{"protocol_version":1,"kind":"write","session_id":"persistent-session-a","generation_id":"generation-a","write":{"input_offset":5,"eof":true}}`,
		`{"protocol_version":1,"kind":"signal","session_id":"persistent-session-a","generation_id":"generation-a","signal":{"kill_id":"kill-1","signal":"TERM"}}`,
		`{"protocol_version":1,"kind":"wait","session_id":"persistent-session-a","generation_id":"generation-a","wait":{"after_change":4,"wait_ms":1000}}`,
	}
	for _, raw := range valid {
		if _, err := DecodeRequest(strings.NewReader(raw)); err != nil {
			t.Fatalf("valid frame rejected %s: %v", raw, err)
		}
	}
}

func TestDecodeRequestRejectsUnknownVersionKindFieldsAndCrossKindPayloads(t *testing.T) {
	invalid := []string{
		`{"protocol_version":2,"kind":"status","session_id":"persistent-session-a","generation_id":"generation-a"}`,
		`{"protocol_version":1,"kind":"exec","session_id":"persistent-session-a","generation_id":"generation-a"}`,
		`{"protocol_version":1,"kind":"status","session_id":"persistent-session-a","generation_id":"generation-a","command":"rm -rf /"}`,
		`{"protocol_version":1,"kind":"status","session_id":"persistent-session-a","generation_id":"generation-a","output":{"offset":0,"max_bytes":1}}`,
		`{"protocol_version":1,"kind":"handshake","session_id":"persistent-session-a","generation_id":"generation-a"}`,
		`{"protocol_version":1,"kind":"write","session_id":"persistent-session-a","generation_id":"generation-a","write":{"input_offset":0,"chars":"x","eof":true}}`,
		`{"protocol_version":1,"kind":"signal","session_id":"persistent-session-a","generation_id":"generation-a","signal":{"kill_id":"kill-1","signal":"HUP"}}`,
		`{"protocol_version":1,"kind":"output","session_id":"persistent-session-a","generation_id":"generation-a","output":{"offset":-1,"max_bytes":1}}`,
		`{"protocol_version":1,"kind":"wait","session_id":"persistent-session-a","generation_id":"generation-a","wait":{"after_change":0,"wait_ms":999999}}`,
	}
	for _, raw := range invalid {
		if _, err := DecodeRequest(strings.NewReader(raw)); err == nil {
			t.Fatalf("invalid frame accepted: %s", raw)
		}
	}
}

func TestDecodeRequestRejectsOversizedOrTrailingFrames(t *testing.T) {
	oversized := `{"protocol_version":1,"kind":"write","session_id":"persistent-session-a","generation_id":"generation-a","write":{"input_offset":0,"chars":"` + strings.Repeat("x", MaxFrameBytes) + `"}}`
	if _, err := DecodeRequest(strings.NewReader(oversized)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	trailing := `{"protocol_version":1,"kind":"status","session_id":"persistent-session-a","generation_id":"generation-a"} {}`
	if _, err := DecodeRequest(strings.NewReader(trailing)); err == nil {
		t.Fatal("trailing json accepted")
	}
}
