package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestReadsV1OperationAfterV2Upgrade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	legacy := []byte(`{"schema_version":1,"operation_id":"legacy","session_id":"legacy-s","fingerprint":"legacy-fp","command":"true","cwd":"/","tty":false,"timeout_ms":0,"shell":"/bin/sh","daemon_incarnation":"old","control_reservation_bytes":0,"created_at":"2026-08-13T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(root, "operations", "legacy.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := r.LoadOperation(context.Background(), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.EffectiveRequestFingerprint() != "legacy-fp" || got.RequestFingerprint != "" {
		t.Fatalf("legacy operation=%#v", got)
	}
}

func TestV2ReservationReplayAndConflicts(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	base := operation.Reservation{
		SchemaVersion: 2, OperationID: "op-v2", SessionID: "s-v2",
		RequestFingerprint: "request-a", ExecutionFingerprint: "exec-a",
		ObservationBindingFingerprint: "obs-a", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d",
	}
	stored, created, result := r.ReserveOperation(context.Background(), base)
	if result.Err != nil || !created {
		t.Fatalf("create stored=%#v created=%v result=%#v", stored, created, result)
	}
	replay := base
	replay.SessionID = "different-session-must-not-win"
	replay.ExecutionFingerprint = "different-current-resolution-must-not-win"
	stored, created, result = r.ReserveOperation(context.Background(), replay)
	if result.Err != nil || created || stored.SessionID != "s-v2" || stored.ExecutionFingerprint != "exec-a" {
		t.Fatalf("replay stored=%#v created=%v result=%#v", stored, created, result)
	}
	changedRequest := base
	changedRequest.RequestFingerprint = "request-b"
	if _, _, got := r.ReserveOperation(context.Background(), changedRequest); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("request conflict=%v", got.Err)
	}
	changedObservation := base
	changedObservation.ObservationBindingFingerprint = "obs-b"
	if _, _, got := r.ReserveOperation(context.Background(), changedObservation); !errors.Is(got.Err, failure.OperationMetadataConflict) {
		t.Fatalf("observation conflict=%v", got.Err)
	}
}

func TestV2ReservationPersistsSeparateFingerprints(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	want := operation.Reservation{SchemaVersion: 2, OperationID: "op-v2", SessionID: "s-v2", RequestFingerprint: "req", ExecutionFingerprint: "exec", ObservationBindingFingerprint: "obs", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d"}
	if _, created, got := r.ReserveOperation(context.Background(), want); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	data, err := os.ReadFile(filepath.Join(root, "operations", "op-v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"request_fingerprint", "execution_fingerprint", "observation_binding_fingerprint"} {
		if raw[key] == nil || raw[key] == "" {
			t.Fatalf("missing %s in %s", key, data)
		}
	}
	if _, exists := raw["fingerprint"]; exists {
		t.Fatalf("v2 persisted legacy fingerprint: %s", data)
	}
}

func TestReadsV1ReceiptAfterV2Upgrade(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	sessionDir := filepath.Join(root, "sessions", "legacy-receipt-session")
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schema_version":1,"operation_id":"legacy","session_id":"legacy-receipt-session","fingerprint":"legacy-fp","daemon_incarnation":"old","state":"failed","outcome":"failure","tty":false,"timeout_ms":0,"output_bytes":0,"output_complete":true,"input_accepted_bytes":0,"input_delivered_bytes":0,"stdin_closed":false,"spawn_evidence":{"attempted":true,"succeeded":true},"exit_evidence":{"reaped":true,"code":1},"signal_evidence":{"attempted":false,"succeeded":false}}`)
	if err := os.WriteFile(filepath.Join(sessionDir, "receipt.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := r.LoadReceipt(context.Background(), "legacy-receipt-session")
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaVersion != 1 || got.Fingerprint != "legacy-fp" || got.RequestFingerprint != "" {
		t.Fatalf("legacy receipt=%#v", got)
	}
}

func TestAbandonUnresolvedV2PreservesFingerprintBindings(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	res := operation.Reservation{
		SchemaVersion: 2, OperationID: "op-v2-abandon", SessionID: "s-v2-abandon",
		RequestFingerprint: "request", ExecutionFingerprint: "execution",
		ObservationBindingFingerprint: "observation", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "old",
	}
	if _, _, got := r.ReserveOperation(context.Background(), res); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := r.AbandonUnresolved(context.Background(), "new"); err != nil {
		t.Fatal(err)
	}
	rec, err := r.LoadReceipt(context.Background(), "s-v2-abandon")
	if err != nil {
		t.Fatal(err)
	}
	if rec.SchemaVersion != 2 || rec.RequestFingerprint != "request" || rec.ExecutionFingerprint != "execution" || rec.ObservationBindingFingerprint != "observation" || rec.Fingerprint != "" {
		t.Fatalf("abandoned v2 receipt=%#v", rec)
	}
}

func TestV2ReservationPersistsAndValidatesEnvironmentBinding(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	binding := environment.Binding{
		SnapshotID:                    "env_" + strings.Repeat("a", 64),
		EnvironmentFingerprint:        strings.Repeat("b", 64),
		EnvironmentFingerprintVersion: environment.FingerprintVersion,
		CapturedAt:                    time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}
	base := operation.Reservation{
		SchemaVersion: 2, OperationID: "op-v2-env", SessionID: "s-v2-env",
		RequestFingerprint: "req", ExecutionFingerprint: "exec", ObservationBindingFingerprint: "obs",
		Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "d",
		EnvironmentBinding: &binding,
	}
	stored, created, got := r.ReserveOperation(context.Background(), base)
	if got.Err != nil || !created || stored.EnvironmentBinding == nil || *stored.EnvironmentBinding != binding {
		t.Fatalf("stored=%#v created=%v result=%#v", stored, created, got)
	}
	loaded, err := r.LoadOperation(context.Background(), "op-v2-env")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EnvironmentBinding == nil || *loaded.EnvironmentBinding != binding {
		t.Fatalf("loaded environment binding=%#v", loaded.EnvironmentBinding)
	}

	bad := base
	bad.OperationID, bad.SessionID = "op-v2-env-bad", "s-v2-env-bad"
	bad.EnvironmentBinding = &environment.Binding{
		SnapshotID:                    "env_" + strings.Repeat("c", 64),
		EnvironmentFingerprint:        strings.Repeat("d", 64),
		EnvironmentFingerprintVersion: environment.FingerprintVersion + 1,
		CapturedAt:                    binding.CapturedAt,
	}
	if _, created, result := r.ReserveOperation(context.Background(), bad); result.Err == nil || created {
		t.Fatalf("malformed binding accepted: created=%v result=%#v", created, result)
	}

	legacy := operation.Reservation{
		SchemaVersion: 1, OperationID: "op-v1-env", SessionID: "s-v1-env",
		Fingerprint: "legacy", CWD: "/", Shell: "/bin/sh", EnvironmentBinding: &binding,
	}
	if _, created, result := r.ReserveOperation(context.Background(), legacy); result.Err == nil || created {
		t.Fatalf("v1 environment binding accepted: created=%v result=%#v", created, result)
	}
}
