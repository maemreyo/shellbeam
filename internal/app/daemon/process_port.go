package daemon

import (
	"context"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type OutputSink interface {
	Append(context.Context, []byte) error
	CaptureFailed(error)
}
type ProcessOwner interface {
	Start(context.Context, operation.ExecutionSpec, OutputSink) (ProcessHandle, receipt.SpawnEvidence, error)
}
type ProcessHandle interface {
	Write([]byte) error
	CloseStdin() error
	Signal(string) receipt.SignalEvidence
	Wait(context.Context) receipt.ExitEvidence
	Close() error
}
