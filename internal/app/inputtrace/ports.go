// Package inputtrace coordinates provider-neutral E27 trace preparation and finalization.
package inputtrace

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type PrepareRequest struct {
	Mode          core.Mode
	OperationID   string
	WorkspaceID   string
	ExecutionMode operation.ExecutionMode
	Executable    string
	CWD           string
}

type Preparer interface {
	Prepare(context.Context, PrepareRequest) (Prepared, error)
}

type Prepared interface {
	Binding() core.InstrumentationBinding
	EnvironmentAdditions() []operation.EnvironmentEntry
	Abort() error
}

type ProviderResource struct {
	ObservationClass core.ObservationClass
	Path             string
}

type ProviderSnapshot struct {
	TraceID       string
	CaptureStart  time.Time
	CaptureEnd    time.Time
	Coverage      core.CoverageMatrix
	Truncated     bool
	GapReason     string
	Resources     []ProviderResource
	RawEventCount int
}

type Finalizer interface {
	Finalize(context.Context, core.InstrumentationBinding) (ProviderSnapshot, error)
	Cleanup(context.Context, core.InstrumentationBinding) error
}

type Preparation struct {
	Handle               Prepared
	Binding              *core.InstrumentationBinding
	EnvironmentAdditions []operation.EnvironmentEntry
}
