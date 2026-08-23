package mcp

import "testing"

const validHermeticJSON = `{"version":1,"mode":"required","repo_inputs":["go.mod","internal/**"],"network":"off","environment":"fixed_allowlist","stdin":"closed","writes":"ephemeral_discard"}`

func TestHermeticMCPV2AcceptsStartAndV1RejectsIt(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"hermetic-mcp","command":"true","cwd":"/tmp","hermetic":` + validHermeticJSON + `}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatalf("decode modern hermetic start: %v", err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatalf("modern protocol rejected hermetic request: %v", err)
	}
	if err := validateForVersion(1, in, raw); err == nil {
		t.Fatal("legacy protocol accepted hermetic request")
	}
}

func TestHermeticMCPV2RejectsInvalidBoundaryContract(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"hermetic-mcp-invalid","command":"true","cwd":"/tmp","hermetic":{"version":1,"mode":"required","repo_inputs":["go.mod"],"network":"allow","environment":"fixed_allowlist","stdin":"closed","writes":"ephemeral_discard"}}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatalf("decode invalid hermetic shape for semantic validation: %v", err)
	}
	if err := validateForVersion(2, in, raw); err == nil {
		t.Fatal("modern protocol accepted invalid hermetic boundary contract")
	}
}

func TestHermeticMCPCloneIsTransportSafe(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"hermetic-clone","command":"true","cwd":"/tmp","hermetic":` + validHermeticJSON + `}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	req := requestFromInput(2, in, raw)
	if req.Start.Hermetic == nil || len(req.Start.Hermetic.RepoInputs) != 2 {
		t.Fatalf("forwarded hermetic=%#v", req.Start.Hermetic)
	}
	req.Start.Hermetic.RepoInputs[0] = "changed"
	if in.Hermetic.RepoInputs[0] == "changed" {
		t.Fatal("MCP request aliased decoded hermetic inputs")
	}
}

func TestHermeticMCPRejectsInteractiveOrPersistentV1(t *testing.T) {
	cases := []string{
		`{"action":"start","operation_id":"hermetic-tty","command":"true","cwd":"/tmp","tty":true,"hermetic":` + validHermeticJSON + `}`,
		`{"action":"start","operation_id":"hermetic-persistent","command":"true","cwd":"/tmp","persistent":true,"session_name":"h","hermetic":` + validHermeticJSON + `}`,
		`{"action":"start","operation_id":"hermetic-stdin","command":"true","cwd":"/tmp","stdin_mode":"stream","hermetic":` + validHermeticJSON + `}`,
	}
	for _, rawText := range cases {
		raw := []byte(rawText)
		in, err := decodeInput(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateForVersion(2, in, raw); err == nil {
			t.Fatalf("accepted contradictory hermetic request: %s", rawText)
		}
	}
}
