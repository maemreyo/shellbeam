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

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestDelegatedBindingPersistsPublicAndPrivateAuthoritySeparately(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := delegatedRepository(t, root, 8)
	res := task4DelegatedReservation("op-delegated-1", "session-delegated-1", "dev")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "secret_provider_ref_01")

	stored, created, got := r.ReserveDelegatedBinding(context.Background(), binding, ref)
	if got.Err != nil || !created || stored != binding {
		t.Fatalf("reserve stored=%#v created=%v result=%#v", stored, created, got)
	}
	replay, created, got := r.ReserveDelegatedBinding(context.Background(), binding, ref)
	if got.Err != nil || created || replay != binding {
		t.Fatalf("replay stored=%#v created=%v result=%#v", replay, created, got)
	}

	publicBytes, err := os.ReadFile(r.delegatedBindingPath(operation.SessionID(binding.SessionID)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicBytes), ref.Ref) || strings.Contains(string(publicBytes), "provider_ref") {
		t.Fatalf("public binding leaked private ref: %s", publicBytes)
	}
	var public map[string]any
	if err := json.Unmarshal(publicBytes, &public); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "session_id", "operation_id", "session_name", "session_mode", "authority_epoch", "desired_owner", "provider_id", "provider_version", "lifecycle", "created_at", "updated_at"} {
		if _, ok := public[key]; !ok {
			t.Fatalf("public binding missing %s: %s", key, publicBytes)
		}
	}
	loadedRef, err := r.LoadDelegatedProviderRef(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || loadedRef != ref {
		t.Fatalf("private ref=%#v err=%v", loadedRef, err)
	}

	changed := binding
	changed.ProviderVersion++
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), changed, ref); !errors.Is(got.Err, failure.DelegatedProviderMismatch) {
		t.Fatalf("provider conflict=%v", got.Err)
	}
}

func TestDelegatedBindingRequiresMatchingSchema5ReservationAndAdvances(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-delegated-2", "session-delegated-2", "")
	binding, ref := delegatedBindingAndRef(res, "provider_ref_02")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err == nil {
		t.Fatal("binding without reservation accepted")
	}
	reserveDelegatedOperation(t, r, res)
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}

	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = binding.UpdatedAt.Add(time.Second)
	if got := r.AdvanceDelegatedBinding(context.Background(), live); got.Err != nil {
		t.Fatalf("advance live=%v", got.Err)
	}
	loaded, err := r.LoadDelegatedBinding(context.Background(), operation.SessionID(binding.SessionID))
	if err != nil || loaded != live {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}

	stale := live
	stale.AuthorityEpoch = 0
	stale.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if got := r.AdvanceDelegatedBinding(context.Background(), stale); got.Err == nil {
		t.Fatal("epoch regression accepted")
	}
}

func delegatedRepository(t *testing.T, root string, mutationLimit int) *Repository {
	t.Helper()
	r, err := Open(root, Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 30, ControlReserve: 1024, MaxDelegatedMutationRecords: mutationLimit})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ReconcileAdmission(); err != nil {
		t.Fatal(err)
	}
	return r
}

func task4DelegatedReservation(opID, sessionID, name string) operation.Reservation {
	return operation.Reservation{
		SchemaVersion: 5, OperationID: operation.ID(opID), SessionID: operation.SessionID(sessionID),
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/tmp", Shell: "/bin/sh",
		SessionName: name, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DaemonIncarnation: "daemon",
	}
}

func reserveDelegatedOperation(t *testing.T, r *Repository, res operation.Reservation) {
	t.Helper()
	if _, created, got := r.ReserveOperation(context.Background(), res); got.Err != nil || !created {
		t.Fatalf("reserve operation created=%v result=%#v", created, got)
	}
}

func delegatedBindingAndRef(res operation.Reservation, privateRef string) (delegated.Binding, delegated.ProviderRef) {
	now := time.Date(2026, 8, 18, 17, 30, 0, 0, time.UTC)
	binding := delegated.Binding{SchemaVersion: delegated.BindingSchemaVersion, SessionID: string(res.SessionID), OperationID: string(res.OperationID), SessionName: res.SessionName, SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: delegated.OwnerAgent, ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleProvisioning, CreatedAt: now, UpdatedAt: now}
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: binding.SessionID, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: privateRef, CreatedAt: now, UpdatedAt: now}
	return binding, ref
}

var _ app.DelegatedSessionStore = (*Repository)(nil)

func TestDelegatedOwnerChangeRequiresNewAuthorityEpoch(t *testing.T) {
	r := delegatedRepository(t, filepath.Join(t.TempDir(), "state"), 8)
	res := task4DelegatedReservation("op-owner", "session-owner", "")
	reserveDelegatedOperation(t, r, res)
	binding, ref := delegatedBindingAndRef(res, "provider_ref_owner")
	if _, _, got := r.ReserveDelegatedBinding(context.Background(), binding, ref); got.Err != nil {
		t.Fatal(got.Err)
	}
	live := binding
	live.Lifecycle = delegated.LifecycleLive
	live.UpdatedAt = live.UpdatedAt.Add(time.Second)
	if got := r.AdvanceDelegatedBinding(context.Background(), live); got.Err != nil {
		t.Fatal(got.Err)
	}

	sameEpoch := live
	sameEpoch.DesiredOwner = delegated.OwnerHuman
	sameEpoch.UpdatedAt = sameEpoch.UpdatedAt.Add(time.Second)
	if got := r.AdvanceDelegatedBinding(context.Background(), sameEpoch); got.Err == nil {
		t.Fatal("owner changed without epoch rotation")
	}

	rotated := sameEpoch
	rotated.AuthorityEpoch = 2
	if got := r.AdvanceDelegatedBinding(context.Background(), rotated); got.Err != nil {
		t.Fatalf("rotated owner change rejected: %v", got.Err)
	}
}
