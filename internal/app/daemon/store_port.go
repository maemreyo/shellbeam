// Package daemon owns ShellBeam action orchestration and live session synchronization.
package daemon

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
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
	Durability Durability
	Err        error
}

type Store interface {
	ReserveOperation(context.Context, operation.Reservation) (operation.Reservation, bool, StoreResult)
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	LoadSession(context.Context, operation.SessionID) (session.Snapshot, error)
	AdvanceSession(context.Context, session.Snapshot) StoreResult
	PublishTerminal(context.Context, receipt.Receipt) StoreResult
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	AppendOutput(context.Context, operation.SessionID, []byte) (int, StoreResult)
	ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error)
	Compact(context.Context, operation.SessionID) StoreResult
}
