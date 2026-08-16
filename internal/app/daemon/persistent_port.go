package daemon

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type PersistentLaunch struct {
	Handle ProcessHandle
	Spawn  receipt.SpawnEvidence
	PID    int
}

type PersistentRuntime interface {
	Ensure(context.Context, operation.Reservation, operation.ExecutionSpec) (PersistentLaunch, error)
}
