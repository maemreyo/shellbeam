package persistentsession

import (
	"context"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type BindingStore interface {
	Find(context.Context, operation.SessionID) (core.Binding, bool, error)
	Reserve(context.Context, core.Binding) (core.Binding, bool, error)
	Advance(context.Context, core.Binding) error
}

type Attachment interface {
	Write([]byte) error
	CloseStdin() error
	Signal(string) receipt.SignalEvidence
	Wait(context.Context) receipt.ExitEvidence
	Close() error
	PID() int
}

type ControlAttachment interface {
	Attachment
	WriteInput(context.Context, int64, []byte, bool) (InputResult, error)
	SignalWithID(context.Context, string, string) (KillResult, error)
	ReadOutput(context.Context, int64, int) ([]byte, int64, int64, error)
	AcknowledgeOutput(context.Context, int64) error
	Status(context.Context) (Status, error)
	WaitStatus(context.Context, uint64, int) (Status, error)
	Terminal(context.Context) (TerminalEvidence, error)
}

type RecoveryAttachment interface {
	ControlAttachment
	RecoveryState(context.Context) (int64, int64, error)
	Cleanup(context.Context) error
}

type InputResult struct {
	AcceptedBytes int
	NextOffset    int64
	Duplicate     bool
	EOFDelivered  bool
}

type KillResult struct {
	KillID    string
	Signal    string
	Attempted bool
	Succeeded bool
	Needed    bool
}

type Status struct {
	SessionID           string
	GenerationID        string
	State               session.State
	Outcome             session.Outcome
	Change              uint64
	PID                 int
	OutputBytes         int64
	OutputAcknowledged  int64
	InputAcceptedBytes  int64
	InputDeliveredBytes int64
	NextInputOffset     int64
	StdinClosed         bool
	Spawn               receipt.SpawnEvidence
	Exit                receipt.ExitEvidence
	Signal              receipt.SignalEvidence
	FailureReason       string
}

type TerminalEvidence struct {
	SessionID           string
	GenerationID        string
	State               session.State
	Outcome             session.Outcome
	Spawn               receipt.SpawnEvidence
	Exit                receipt.ExitEvidence
	Signal              receipt.SignalEvidence
	TimedOut            bool
	OutputBytes         int64
	OutputComplete      bool
	InputAcceptedBytes  int64
	InputDeliveredBytes int64
	StdinClosed         bool
	FailureReason       string
}

type LaunchRequest struct {
	Binding core.Binding
	Spec    operation.ExecutionSpec
	Limits  Limits
}

type Launcher interface {
	Ensure(context.Context, LaunchRequest) (Attachment, Status, error)
}

type Limits struct {
	MaxOutputBytes        int64
	MaxQueuedInputBytes   int
	MaxInputRecords       int
	MaxInputMetadataBytes int
	MaxKillRecords        int
	TerminationGrace      time.Duration
}

type Options struct {
	Limits         Limits
	Now            func() time.Time
	NewGeneration  func() string
	NewEndpointRef func() string
}

type Result struct {
	Binding    core.Binding
	Attachment Attachment
	Status     Status
}
