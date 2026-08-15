package operation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

func TestReservationJSONRoundTripPreservesOptionalEnvironmentBinding(t *testing.T) {
	binding := environment.Binding{
		SnapshotID:                    "env_" + strings.Repeat("a", 64),
		EnvironmentFingerprint:        strings.Repeat("b", 64),
		EnvironmentFingerprintVersion: environment.FingerprintVersion,
		CapturedAt:                    time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	want := Reservation{
		SchemaVersion: 2, OperationID: "op-env-roundtrip", SessionID: "sid-env-roundtrip",
		RequestFingerprint: strings.Repeat("c", 64), ExecutionFingerprint: strings.Repeat("d", 64),
		ObservationBindingFingerprint: strings.Repeat("e", 64),
		EnvironmentBinding:            &binding,
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Reservation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.EnvironmentBinding == nil || *got.EnvironmentBinding != binding {
		t.Fatalf("round-trip environment binding=%#v want %#v", got.EnvironmentBinding, binding)
	}

	legacy := []byte(`{"schema_version":1,"operation_id":"legacy","session_id":"legacy-s","fingerprint":"legacy-fp","created_at":"2026-08-15T00:00:00Z"}`)
	var old Reservation
	if err := json.Unmarshal(legacy, &old); err != nil {
		t.Fatal(err)
	}
	if old.EnvironmentBinding != nil {
		t.Fatalf("legacy reservation fabricated environment binding: %#v", old.EnvironmentBinding)
	}
}
