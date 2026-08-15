package supervisor

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestBootstrapClosedSchemaRoundTripWithoutCapability(t *testing.T) {
	want := Bootstrap{
		SchemaVersion: BootstrapSchemaVersion,
		RuntimeRoot:   "/tmp/shellbeam-501",
		SessionID:     "persistent-session-bootstrap",
		GenerationID:  "generation-bootstrap",
		Execution: BootstrapExecution{
			Mode:       operation.ExecutionModeShell,
			Shell:      "/bin/sh",
			Executable: "/bin/sh",
			Command:    "sleep 1",
			CWD:        "/tmp",
			TimeoutMS:  50,
		},
		MaxOutputBytes:        1024,
		MaxQueuedInputBytes:   128,
		MaxInputRecords:       16,
		MaxInputMetadataBytes: 8192,
		MaxKillRecords:        8,
		TerminationGraceMS:    25,
	}
	var encoded bytes.Buffer
	if err := EncodeBootstrap(&encoded, want); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded.String(), "capability") || strings.Contains(encoded.String(), "secret") {
		t.Fatalf("bootstrap exposed capability material: %s", encoded.String())
	}
	got, err := DecodeBootstrap(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != want.SessionID || got.GenerationID != want.GenerationID || got.Execution.Command != want.Execution.Command {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
}

func TestBootstrapRejectsUnknownFieldsTTYAndInvalidLimits(t *testing.T) {
	base := `{"schema_version":1,"runtime_root":"/tmp/runtime","session_id":"persistent-session-a","generation_id":"generation-a","execution":{"mode":"shell","shell":"/bin/sh","executable":"/bin/sh","command":"true","cwd":"/tmp","tty":false,"timeout_ms":0},"max_output_bytes":1024,"max_queued_input_bytes":128,"max_input_records":16,"max_input_metadata_bytes":8192,"max_kill_records":8,"termination_grace_ms":25}`
	cases := []string{
		strings.TrimSuffix(base, "}") + `,"capability":"secret-sentinel"}`,
		strings.Replace(base, `"tty":false`, `"tty":true`, 1),
		strings.Replace(base, `"max_kill_records":8`, `"max_kill_records":0`, 1),
		strings.Replace(base, `"shell":"/bin/sh","executable":"/bin/sh"`, `"shell":"bin/sh","executable":"bin/sh"`, 1),
	}
	for _, raw := range cases {
		if _, err := DecodeBootstrap(strings.NewReader(raw)); err == nil {
			t.Fatalf("invalid bootstrap accepted: %s", raw)
		}
	}
}
