package delegatedsession

import (
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestDecideMutationChecksKnownRetryBeforeCurrentAuthority(t *testing.T) {
	incoming := MutationIdentity{
		SessionID:   "01M0H1DELEGATEDSESSION000001",
		Epoch:       1,
		Kind:        MutationWrite,
		Offset:      12,
		Fingerprint: "fp-write-12",
	}
	known := &MutationRecord{Identity: incoming, Outcome: "delivered"}
	decision, err := DecideMutation(known, incoming, MutationContext{CurrentEpoch: 3, CurrentOwner: OwnerNone})
	if err != nil {
		t.Fatalf("known retry rejected by newer authority: %v", err)
	}
	if decision.Action != AdmissionReplay || decision.Record != *known {
		t.Fatalf("decision=%#v want exact replay %#v", decision, known)
	}
}

func TestDecideMutationRejectsKnownFingerprintConflictBeforeAuthority(t *testing.T) {
	incoming := MutationIdentity{
		SessionID:     "01M0H1DELEGATEDSESSION000001",
		Epoch:         1,
		Kind:          MutationKill,
		IdempotencyID: "kill-1",
		Offset:        -1,
		Fingerprint:   "fp-new",
	}
	knownIdentity := incoming
	knownIdentity.Fingerprint = "fp-old"
	_, err := DecideMutation(&MutationRecord{Identity: knownIdentity}, incoming, MutationContext{CurrentEpoch: 9, CurrentOwner: OwnerNone})
	if !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("conflict error=%v want operation_conflict", err)
	}
}

func TestDecideMutationAdmissionMatrix(t *testing.T) {
	base := MutationIdentity{
		SessionID:     "01M0H1DELEGATEDSESSION000001",
		Epoch:         4,
		Kind:          MutationSignal,
		IdempotencyID: "signal-4",
		Offset:        -1,
		Fingerprint:   "fp-signal",
	}

	old := base
	old.Epoch = 3
	if _, err := DecideMutation(nil, old, MutationContext{CurrentEpoch: 4, CurrentOwner: OwnerAgent}); !errors.Is(err, failure.StaleControlGeneration) {
		t.Fatalf("old epoch error=%v", err)
	}
	if _, err := DecideMutation(nil, base, MutationContext{CurrentEpoch: 4, CurrentOwner: OwnerNone}); !errors.Is(err, failure.SessionControlNotOwned) {
		t.Fatalf("wrong owner error=%v", err)
	}
	decision, err := DecideMutation(nil, base, MutationContext{CurrentEpoch: 4, CurrentOwner: OwnerAgent})
	if err != nil {
		t.Fatalf("current owned mutation rejected: %v", err)
	}
	if decision.Action != AdmissionReserve || decision.Record.Identity != base {
		t.Fatalf("decision=%#v want reserve incoming", decision)
	}
}

func TestMutationIdentityValidationSeparatesWriteOffsetFromControlID(t *testing.T) {
	write := MutationIdentity{SessionID: "session-1", Epoch: 1, Kind: MutationWrite, Offset: 0, Fingerprint: "fp-write"}
	if err := write.Validate(); err != nil {
		t.Fatalf("valid write rejected: %v", err)
	}
	control := MutationIdentity{SessionID: "session-1", Epoch: 1, Kind: MutationKill, IdempotencyID: "kill-1", Offset: -1, Fingerprint: "fp-kill"}
	if err := control.Validate(); err != nil {
		t.Fatalf("valid control mutation rejected: %v", err)
	}
	bad := []MutationIdentity{
		{SessionID: "session-1", Epoch: 1, Kind: MutationWrite, IdempotencyID: "write-id", Offset: 0, Fingerprint: "fp"},
		{SessionID: "session-1", Epoch: 1, Kind: MutationWrite, Offset: -1, Fingerprint: "fp"},
		{SessionID: "session-1", Epoch: 1, Kind: MutationKill, Offset: -1, Fingerprint: "fp"},
		{SessionID: "session-1", Epoch: 1, Kind: MutationKill, IdempotencyID: "kill-1", Offset: 0, Fingerprint: "fp"},
	}
	for i, candidate := range bad {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("bad[%d] accepted: %#v", i, candidate)
		}
	}
}
