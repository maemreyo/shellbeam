// Package daemon owns ShellBeam action orchestration and live session synchronization.
package daemon

import (
	"context"
	decisionprotocol "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
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

type DecisionExperimentAdmissionStore interface {
	ReserveExperimentOperation(context.Context, operation.Reservation, decisionprotocol.ExperimentExecutionLink) (operation.Reservation, decisionprotocol.ExperimentExecutionLink, bool, StoreResult)
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
