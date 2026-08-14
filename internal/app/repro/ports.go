package repro

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	telemetry "github.com/maemreyo/shellbeam/internal/core/telemetry"
)

type Repository interface {
	LoadOperation(context.Context, operation.ID) (operation.Reservation, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	FindOperationDerivation(context.Context, string) (structured.Derivation, bool, error)
	FindPerformanceByOperation(context.Context, string) (telemetry.PerformanceRecord, bool, error)
	CreateRepro(context.Context, string, core.Capsule) (core.Capsule, bool, error)
	GetReproByCreateID(context.Context, string) (core.Capsule, bool, error)
	GetRepro(context.Context, string) (core.Capsule, bool, error)
}
