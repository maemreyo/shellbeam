// Package telemetry derives bounded empirical execution telemetry from durable terminal truth.
package telemetry

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

type Repository interface {
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	LoadSession(context.Context, operation.SessionID) (session.Snapshot, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	PutPerformanceRecord(context.Context, core.PerformanceRecord) error
}
