// Package daemon owns ShellBeam action orchestration and live session synchronization.
package daemon

import (
	"context"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type Durability string

const (
	NoDurableChange Durability = "none"
	DurableChange   Durability = "durable"
	AmbiguousChange Durability = "ambiguous"
)

type StoreResult struct {
	Durability     Durability
	ObservationSeq uint64
	Err            error
}

type Store interface {
	ReserveOperation(context.Context, operation.Reservation) (operation.Reservation, bool, StoreResult)
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error)
	ReserveTypedIntent(context.Context, operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, StoreResult)
	FindTypedIntent(context.Context, operation.ID) (operation.TypedIntentClaim, bool, error)
	CommitTypedBinding(context.Context, operation.ID, operation.Reservation) (operation.Reservation, bool, StoreResult)
	LoadSession(context.Context, operation.SessionID) (session.Snapshot, error)
	AdvanceSession(context.Context, session.Snapshot) StoreResult
	PublishTerminal(context.Context, receipt.Receipt) StoreResult
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	AppendOutput(context.Context, operation.SessionID, []byte) (int, StoreResult)
	ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error)
	Compact(context.Context, operation.SessionID) StoreResult
}

type DelegatedRecoveryState struct {
	NextInputOffset int64
}

type DelegatedSessionStore interface {
	ReserveDelegatedBinding(context.Context, delegated.Binding, delegated.ProviderRef) (delegated.Binding, bool, StoreResult)
	AdvanceDelegatedBinding(context.Context, delegated.Binding) StoreResult
	LoadDelegatedBinding(context.Context, operation.SessionID) (delegated.Binding, error)
	LoadDelegatedProviderRef(context.Context, operation.SessionID) (delegated.ProviderRef, error)
	LookupDelegatedMutation(context.Context, delegated.MutationIdentity) (delegated.MutationRecord, bool, error)
	ReserveDelegatedMutation(context.Context, delegated.MutationIdentity) (delegated.MutationRecord, bool, StoreResult)
	CompleteDelegatedMutation(context.Context, delegated.MutationIdentity, delegated.MutationState, string) (delegated.MutationRecord, StoreResult)
	ListDelegatedRecoveryCandidates(context.Context) ([]delegated.Binding, error)
	LoadDelegatedRecoveryState(context.Context, operation.SessionID) (DelegatedRecoveryState, error)
	DelegatedOutputBytes(context.Context, operation.SessionID) (int64, error)
	LoadDelegatedCaptureTruth(context.Context, operation.SessionID) (receipt.CaptureTruth, error)
	MarkDelegatedCaptureReason(context.Context, operation.SessionID, receipt.CaptureReason) (receipt.CaptureTruth, StoreResult)
}

type InteractiveHandoffStore interface {
	ReserveHandoff(context.Context, handoff.Request, handoff.State) (handoff.State, bool, StoreResult)
	AdvanceHandoff(context.Context, handoff.State) StoreResult
	LoadHandoff(context.Context, string) (handoff.Request, handoff.State, error)
	RecoverHandoff(context.Context, string) (handoff.State, StoreResult)
	FindHandoff(context.Context, string) (handoff.Request, handoff.State, bool, error)
	ReserveControlSignal(context.Context, handoff.ControlSignal) (handoff.ControlSignal, string, bool, StoreResult)
	CompleteControlSignal(context.Context, handoff.ControlSignal, string) (string, StoreResult)
	ListHandoffRecoveryCandidates(context.Context) ([]handoff.State, error)
	MarkHumanWriteAuthorityGranted(context.Context, operation.SessionID) StoreResult
	LoadInputAuthorityProvenance(context.Context, operation.SessionID) (string, error)
}

type PersistentSessionStore interface {
	ReservePersistentBinding(context.Context, persistent.Binding) (persistent.Binding, bool, StoreResult)
	AdvancePersistentBinding(context.Context, persistent.Binding) StoreResult
	LoadPersistentBinding(context.Context, operation.SessionID) (persistent.Binding, error)
	FindPersistentBinding(context.Context, operation.SessionID) (persistent.Binding, bool, error)
	FindPersistentBindingByName(context.Context, string) (persistent.Binding, bool, error)
	ListPersistentBindings(context.Context, persistent.InspectRequest) (persistent.BindingPage, error)
	ListPersistentRecoveryCandidates(context.Context) ([]persistent.Binding, error)
	AbandonPersistentSession(context.Context, persistent.Binding, string, string) StoreResult
}

type processObservationStore interface {
	PrepareProcessStartedObservation(context.Context, string, string) StoreResult
	CommitObservationSequence(context.Context, uint64) StoreResult
	AbortObservationSequence(context.Context, uint64, string) StoreResult
}
