package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestHermeticReservationAllowedOnlyForEphemeralV2V3(t *testing.T) {
	binding := storeHermeticBinding()
	resV2 := validHermeticReservationForSchema(2, binding)
	if err := validateReservation(resV2); err != nil {
		t.Fatalf("v2 hermetic reservation rejected: %v", err)
	}
	claim := validTypedIntentClaim(t, "typed-hermetic-reservation")
	resV3 := validTypedReservation(t, claim, "typed-hermetic-session")
	resV3.HermeticBoundary = binding.Clone()
	if err := validateReservation(resV3); err != nil {
		t.Fatalf("v3 hermetic reservation rejected: %v", err)
	}
	for _, version := range []int{1, 4} {
		res := validHermeticReservationForSchema(version, binding)
		if err := validateReservation(res); err == nil {
			t.Fatalf("v%d accepted hermetic reservation", version)
		}
	}
}

func TestHermeticReservationRejectsInvalidBinding(t *testing.T) {
	binding := storeHermeticBinding()
	binding.CaptureManifestSHA256 = ""
	res := validHermeticReservationForSchema(2, binding)
	if err := validateReservation(res); err == nil {
		t.Fatal("invalid hermetic binding accepted")
	}
}

func validHermeticReservationForSchema(version int, binding hermetic.BoundaryBinding) operation.Reservation {
	res := operation.Reservation{SchemaVersion: version, OperationID: "hermetic-res", SessionID: "hermetic-session", RequestFingerprint: "request", ExecutionFingerprint: "execution", ObservationBindingFingerprint: "observation", ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "daemon", CreatedAt: time.Now().UTC(), HermeticBoundary: &binding}
	if version == 1 {
		res.Fingerprint = "legacy"
		res.RequestFingerprint = ""
		res.ExecutionFingerprint = ""
		res.ObservationBindingFingerprint = ""
	}
	if version == 3 {
		// Force only the hermetic gate to be evaluated before existing typed requirements.
		res.ProjectCommand = nil
	}
	if version == 4 {
		res.Persistent = true
	}
	return res
}

func storeHermeticBinding() hermetic.BoundaryBinding {
	return hermetic.BoundaryBinding{SchemaVersion: 1, BoundaryID: "hb_01K00000000000000000000000", Request: hermetic.Request{Version: 1, Mode: hermetic.ModeRequired, RepoInputs: []string{"go.mod"}, Network: hermetic.NetworkOff, Environment: hermetic.EnvironmentFixedAllowlist, Stdin: hermetic.StdinClosed, Writes: hermetic.WritesEphemeralDiscard}, CaptureManifestSHA256: storeDigest('d'), CaptureContentSHA256: storeDigest('e'), Provider: hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: storeDigest('a'), RuntimeManifestSHA256: storeDigest('b')}, Toolchain: hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: storeDigest('c')}}
}
func storeDigest(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}

func TestHermeticReservationRoundTripsV2AndV3(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	binding := storeHermeticBinding()
	v2 := validHermeticReservationForSchema(2, binding)
	v2.SessionID = "hermetic-v2-session"
	v2.OperationID = "hermetic-v2-roundtrip"
	if _, created, got := r.ReserveOperation(context.Background(), v2); got.Err != nil || !created {
		t.Fatalf("v2 reserve created=%v result=%#v", created, got)
	}
	loadedV2, err := r.LoadOperation(context.Background(), v2.OperationID)
	if err != nil || loadedV2.HermeticBoundary == nil || !reflect.DeepEqual(*loadedV2.HermeticBoundary, binding) {
		t.Fatalf("v2 roundtrip loaded=%#v err=%v", loadedV2.HermeticBoundary, err)
	}

	claim := validTypedIntentClaim(t, "typed-hermetic-roundtrip")
	if _, created, got := r.ReserveTypedIntent(context.Background(), claim); got.Err != nil || !created {
		t.Fatalf("typed claim created=%v result=%#v", created, got)
	}
	v3 := validTypedReservation(t, claim, "typed-hermetic-roundtrip-session")
	v3.HermeticBoundary = binding.Clone()
	if _, created, got := r.CommitTypedBinding(context.Background(), claim.OperationID, v3); got.Err != nil || !created {
		t.Fatalf("v3 commit created=%v result=%#v", created, got)
	}
	loadedV3, err := r.LoadOperation(context.Background(), claim.OperationID)
	if err != nil || loadedV3.HermeticBoundary == nil || !reflect.DeepEqual(*loadedV3.HermeticBoundary, binding) {
		t.Fatalf("v3 roundtrip loaded=%#v err=%v", loadedV3.HermeticBoundary, err)
	}
}

func TestAbandonUnresolvedHermeticReservationPublishesLostBoundaryTruth(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state-abandon")
	r := openRecoveryRepository(t, root)
	binding := storeHermeticBinding()
	res := validHermeticReservationForSchema(2, binding)
	res.OperationID = "hermetic-abandon"
	res.SessionID = "hermetic-abandon-session"
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	r = openRecoveryRepository(t, root)
	if err := r.AbandonUnresolved(context.Background(), "new-daemon"); err != nil {
		t.Fatal(err)
	}
	rec, err := r.LoadReceipt(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.HermeticBinding == nil || rec.HermeticResult == nil {
		t.Fatalf("abandoned hermetic truth missing: %#v", rec)
	}
	if !reflect.DeepEqual(*rec.HermeticBinding, binding) || rec.HermeticResult.BoundaryID != binding.BoundaryID || rec.HermeticResult.Provider != binding.Provider || rec.HermeticResult.Toolchain != binding.Toolchain {
		t.Fatalf("abandoned hermetic identity mismatch: binding=%#v result=%#v", rec.HermeticBinding, rec.HermeticResult)
	}
	if rec.HermeticResult.EstablishedPreExec || rec.HermeticResult.Continuity != hermetic.ContinuityLost || rec.HermeticResult.Authoritative() {
		t.Fatalf("abandoned boundary retained authority: %#v", rec.HermeticResult)
	}
}
