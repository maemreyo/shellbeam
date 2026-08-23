//go:build linux || darwin

package ipc

import (
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

func TestHermeticIPCV2AcceptsStartContract(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"hermetic","action":"start","operation_id":"hermetic-ipc","command":"true","cwd":"/tmp","hermetic":{"version":1,"mode":"required","repo_inputs":["go.mod","internal/**"],"network":"off","environment":"fixed_allowlist","stdin":"closed","writes":"ephemeral_discard"}}`)
	got, err := decodeRequestV2(bytesReaderV2(raw))
	if err != nil {
		t.Fatalf("decode hermetic IPC start: %v", err)
	}
	if err := validateRequestV2(got); err != nil {
		t.Fatalf("validate hermetic IPC start: %v", err)
	}
}

func TestHermeticIPCV2RejectsInvalidBoundaryContract(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"hermetic-invalid","action":"start","operation_id":"hermetic-ipc-invalid","command":"true","cwd":"/tmp","hermetic":{"version":1,"mode":"required","repo_inputs":["go.mod"],"network":"off","environment":"inherit","stdin":"closed","writes":"ephemeral_discard"}}`)
	if _, err := decodeRequestV2(bytesReaderV2(raw)); err == nil {
		t.Fatal("IPC accepted invalid hermetic boundary contract")
	}
}

func TestHermeticIPCBridgeCloneIsTransportSafe(t *testing.T) {
	hermetic := &hermeticcore.Request{
		Version: 1, Mode: hermeticcore.ModeRequired, RepoInputs: []string{"go.mod", "internal/**"},
		Network: hermeticcore.NetworkOff, Environment: hermeticcore.EnvironmentFixedAllowlist,
		Stdin: hermeticcore.StdinClosed, Writes: hermeticcore.WritesEphemeralDiscard,
	}
	wire := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: daemon.StartRequest{
		OperationID: "hermetic-clone", Command: "true", CWD: "/tmp", Hermetic: hermetic,
	}})
	if wire.Hermetic == nil || len(wire.Hermetic.RepoInputs) != 2 {
		t.Fatalf("wire hermetic=%#v", wire.Hermetic)
	}
	wire.Hermetic.RepoInputs[0] = "changed"
	if hermetic.RepoInputs[0] == "changed" {
		t.Fatal("IPC request aliased caller hermetic inputs")
	}
}

func TestHermeticIPCV2RejectsInteractiveOrPersistentV1(t *testing.T) {
	base := `"hermetic":{"version":1,"mode":"required","repo_inputs":["go.mod"],"network":"off","environment":"fixed_allowlist","stdin":"closed","writes":"ephemeral_discard"}`
	cases := []string{
		`{"ipc_version":2,"kind":"request","request_id":"h-tty","action":"start","operation_id":"h-tty","command":"true","cwd":"/tmp","tty":true,` + base + `}`,
		`{"ipc_version":2,"kind":"request","request_id":"h-persistent","action":"start","operation_id":"h-persistent","command":"true","cwd":"/tmp","persistent":true,"session_name":"h",` + base + `}`,
		`{"ipc_version":2,"kind":"request","request_id":"h-stdin","action":"start","operation_id":"h-stdin","command":"true","cwd":"/tmp","stdin_mode":"stream",` + base + `}`,
	}
	for _, rawText := range cases {
		if _, err := decodeRequestV2(bytesReaderV2([]byte(rawText))); err == nil {
			t.Fatalf("accepted contradictory hermetic request: %s", rawText)
		}
	}
}
