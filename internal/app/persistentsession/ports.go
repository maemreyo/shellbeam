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

type Status struct {
	SessionID    string
	GenerationID string
	State        session.State
	Outcome      session.Outcome
	PID          int
	Spawn        receipt.SpawnEvidence
	Exit         receipt.ExitEvidence
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
