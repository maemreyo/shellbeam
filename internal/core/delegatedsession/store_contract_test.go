package delegatedsession

import (
	"testing"
	"time"
)

func TestBindingCanonicalLifecycleAndProviderRefValidation(t *testing.T) {
	now := time.Date(2026, 8, 18, 17, 30, 0, 0, time.UTC)
	binding := Binding{
		SchemaVersion: BindingSchemaVersion,
		SessionID:     "session-delegated-1", OperationID: "op-delegated-1", SessionName: "dev",
		SessionMode: ModeDelegatedInteractive, AuthorityEpoch: 1, DesiredOwner: OwnerAgent,
		ProviderID: "tmux_control_mode", ProviderVersion: 1,
		Lifecycle: LifecycleProvisioning, CreatedAt: now, UpdatedAt: now,
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("binding rejected: %v", err)
	}
	if got := binding.ProviderIdentity(); got != (ProviderIdentity{ID: "tmux_control_mode", Version: 1}) {
		t.Fatalf("provider=%#v", got)
	}
	ref := ProviderRef{SchemaVersion: ProviderRefSchemaVersion, SessionID: binding.SessionID, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion, Ref: "provider_ref_01", CreatedAt: now, UpdatedAt: now}
	if err := ref.Validate(); err != nil {
		t.Fatalf("provider ref rejected: %v", err)
	}

	bad := binding
	bad.AuthorityEpoch = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("zero epoch accepted")
	}
	bad = binding
	bad.Lifecycle = "unknown"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown lifecycle accepted")
	}
	bad = binding
	bad.ProviderVersion = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("zero provider version accepted")
	}
}

func TestMutationRecordStateMachineVocabulary(t *testing.T) {
	now := time.Date(2026, 8, 18, 17, 30, 0, 0, time.UTC)
	id := MutationIdentity{SessionID: "session-delegated-1", Epoch: 1, Kind: MutationWrite, Offset: 0, Fingerprint: "fp_01"}
	for _, state := range []MutationState{MutationReserved, MutationDelivered, MutationCompleted, MutationFailed, MutationOutcomeUnknown} {
		rec := MutationRecord{SchemaVersion: MutationRecordSchemaVersion, Identity: id, State: state, CreatedAt: now, UpdatedAt: now}
		if err := rec.Validate(); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
	rec := MutationRecord{SchemaVersion: MutationRecordSchemaVersion, Identity: id, State: "unknown", CreatedAt: now, UpdatedAt: now}
	if err := rec.Validate(); err == nil {
		t.Fatal("unknown mutation state accepted")
	}
}
