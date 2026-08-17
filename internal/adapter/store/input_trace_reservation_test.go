package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestE27InputTraceSafeBindingRoundTripsAndValidatesAcrossModernReservations(t *testing.T) {
	binding := e27StoreTraceBinding("a")
	v2 := e27V2TraceReservation(binding)
	v3 := validTypedReservation(t, validTypedIntentClaim(t, "e27-v3"), "e27-v3-session")
	v3.Trace = &binding
	v4 := e27V4TraceReservation(binding)
	for _, reservation := range []operation.Reservation{v2, v3, v4} {
		if err := validateReservation(reservation); err != nil {
			t.Fatalf("schema %d rejected trace binding: %v", reservation.SchemaVersion, err)
		}
		encoded, err := json.Marshal(reservation)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "DYLD_INSERT_LIBRARIES") || strings.Contains(string(encoded), "socket_path") || strings.Contains(string(encoded), "environment_additions") {
			t.Fatalf("private spawn control leaked: %s", encoded)
		}
		var decoded operation.Reservation
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Trace == nil || !reflect.DeepEqual(*decoded.Trace, binding) {
			t.Fatalf("schema %d trace round-trip=%#v", reservation.SchemaVersion, decoded.Trace)
		}
	}
}

func TestE27InputTraceV1RejectsTraceAndModernReservationsRejectInvalidBinding(t *testing.T) {
	binding := e27StoreTraceBinding("a")
	v1 := operation.Reservation{SchemaVersion: 1, OperationID: "e27-v1", SessionID: "e27-v1-session", Fingerprint: "legacy", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d", Trace: &binding}
	if err := validateReservation(v1); err == nil {
		t.Fatal("v1 accepted E27 trace binding")
	}
	bad := e27V2TraceReservation(binding)
	bad.Trace.InstrumentationFingerprint = "not-a-digest"
	if err := validateReservation(bad); err == nil {
		t.Fatal("modern reservation accepted invalid trace binding")
	}
}

func TestE27InputTraceReservationReplayRejectsProviderRebinding(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	firstBinding := e27StoreTraceBinding("a")
	first := e27V2TraceReservation(firstBinding)
	stored, created, result := r.ReserveOperation(context.Background(), first)
	if result.Err != nil || !created || stored.Trace == nil {
		t.Fatalf("first reserve created=%v stored=%#v result=%#v", created, stored, result)
	}
	secondBinding := e27StoreTraceBinding("b")
	replay := first
	replay.Trace = &secondBinding
	if _, created, result := r.ReserveOperation(context.Background(), replay); result.Err == nil || created || !errors.Is(result.Err, failure.OperationMetadataConflict) {
		t.Fatalf("provider rebinding result created=%v err=%v", created, result.Err)
	}
	loaded, found, err := r.FindOperation(context.Background(), first.OperationID)
	if err != nil || !found || loaded.Trace == nil || !reflect.DeepEqual(*loaded.Trace, firstBinding) {
		t.Fatalf("stored binding changed loaded=%#v found=%v err=%v", loaded.Trace, found, err)
	}
}

func e27V2TraceReservation(binding trace.InstrumentationBinding) operation.Reservation {
	return operation.Reservation{
		SchemaVersion: 2, OperationID: "e27-v2", SessionID: "e27-v2-session", RequestFingerprint: strings.Repeat("1", 64), ExecutionFingerprint: strings.Repeat("2", 64), ObservationBindingFingerprint: "",
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d", Trace: &binding,
	}
}

func e27V4TraceReservation(binding trace.InstrumentationBinding) operation.Reservation {
	return operation.Reservation{
		SchemaVersion: 4, OperationID: "e27-v4", SessionID: "e27-v4-session", RequestFingerprint: strings.Repeat("3", 64), ExecutionFingerprint: strings.Repeat("4", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d", Persistent: true, Trace: &binding,
	}
}

func e27StoreTraceBinding(hex string) trace.InstrumentationBinding {
	return trace.InstrumentationBinding{
		SchemaVersion: trace.SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: trace.ModeBestEffort, Status: trace.BindingActive,
		Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat(hex, 64), InstrumentationEffect: trace.EffectEnvironmentAffecting,
		Coverage: trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial},
	}
}
