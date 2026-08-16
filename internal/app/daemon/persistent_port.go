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

// PersistentOutputResult is the bounded canonical append outcome for supervisor recovery.
type PersistentOutputResult struct {
	CanonicalExtent int64
	AppendedBytes   int
	Replay          bool
}

type persistentOutputStore interface {
	ReconcilePersistentOutput(context.Context, operation.SessionID, int64, []byte) (PersistentOutputResult, StoreResult)
}
