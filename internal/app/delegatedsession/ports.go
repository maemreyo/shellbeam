package delegatedsession

import (
	"context"
	"fmt"
	"io"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
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

type Detacher interface {
	Detach(context.Context, core.ProviderRef) error
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

type ProviderClientRef struct {
	Ref string `json:"ref"`
}

func (r ProviderClientRef) Validate() error {
	if !validProviderOpaque(r.Ref, 256) {
		return fmt.Errorf("invalid provider client ref")
	}
	return nil
}

type HumanAttachSpec struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Environment []string
}

type HumanAttachResult struct {
	ClientRef     ProviderClientRef
	ObservedOwner core.Owner
	Done          <-chan error
}

type HumanClientObservation struct {
	ClientRef          ProviderClientRef
	Present            bool
	ReadOnly           bool
	ObservedOwner      core.Owner
	ProviderGeneration string
}

type IngressFenceProof struct {
	ClientRef          ProviderClientRef
	AuthorityEpoch     core.AuthorityEpoch
	ProviderGeneration string
	Fenced             bool
}

type HumanControlSpec struct {
	HandoffID      string
	AuthorityEpoch core.AuthorityEpoch
}

func (s HumanControlSpec) Validate() error {
	if !validProviderOpaque(s.HandoffID, 128) {
		return fmt.Errorf("invalid handoff id")
	}
	return s.AuthorityEpoch.Validate()
}

type HumanProvider interface {
	AttachHuman(context.Context, core.ProviderRef, HumanAttachSpec) (HumanAttachResult, error)
	SetHumanWritable(context.Context, core.ProviderRef, ProviderClientRef, bool) error
	FenceHumanIngress(context.Context, core.ProviderRef, ProviderClientRef, core.AuthorityEpoch) (IngressFenceProof, error)
	InspectHumanClient(context.Context, core.ProviderRef, ProviderClientRef) (HumanClientObservation, error)
	ArmWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) error
	WaitWritableHumanControl(context.Context, core.ProviderRef, ProviderClientRef, HumanControlSpec) (handoff.HumanControlKind, error)
	PrepareReadOnlyLocalControl(context.Context, core.ProviderRef, ProviderClientRef) error
}

func validProviderOpaque(v string, max int) bool {
	if len(v) < 1 || len(v) > max {
		return false
	}
	for i := 0; i < len(v); i++ {
		b := v[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.' {
			continue
		}
		return false
	}
	return true
}
