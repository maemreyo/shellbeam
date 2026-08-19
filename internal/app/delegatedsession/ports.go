package delegatedsession

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type OutputSink interface {
	Append([]byte) error
}

type CreateRequest struct {
	ProviderRef core.ProviderRef
	SessionID   string
	OperationID string
	SessionName string
	Spec        operation.ExecutionSpec
	Output      OutputSink
}

type CreateResult struct {
	ProviderRef core.ProviderRef
	Observation Observation
}

type Observation struct {
	Provider           core.ProviderIdentity
	ProviderCurrent    bool
	ProviderGeneration string
	Owner              core.Owner
	Terminal           bool
	ExitCode           *int
	PanePID            int
	OutputBytes        int64
}

type Provider interface {
	Identity() core.ProviderIdentity
	ProviderRefForSession(string, time.Time) (core.ProviderRef, error)
	Probe(context.Context) error
	Create(context.Context, CreateRequest) (CreateResult, error)
	Reattach(context.Context, core.ProviderRef, OutputSink) (Observation, error)
	Write(context.Context, core.ProviderRef, []byte) error
	Signal(context.Context, core.ProviderRef, string) error
	Inspect(context.Context, core.ProviderRef) (Observation, error)
	Wait(context.Context, core.ProviderRef) (Observation, error)
	Close(context.Context, core.ProviderRef) error
}
