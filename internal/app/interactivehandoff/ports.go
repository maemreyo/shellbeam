package interactivehandoff

import (
	"context"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const MaxWait = 30 * time.Second

type Store interface {
	FindHandoff(context.Context, string) (handoff.Request, handoff.State, bool, error)
	ReserveHandoff(context.Context, handoff.Request, handoff.State) (handoff.State, bool, error)
	AdvanceHandoff(context.Context, handoff.State) error
	LoadHandoff(context.Context, string) (handoff.Request, handoff.State, error)
	LoadDelegatedBinding(context.Context, operation.SessionID) (delegated.Binding, error)
	LoadDelegatedProviderRef(context.Context, operation.SessionID) (delegated.ProviderRef, error)
	ReserveControlSignal(context.Context, handoff.ControlSignal) (handoff.ControlSignal, string, bool, error)
	CompleteControlSignal(context.Context, handoff.ControlSignal, string) (string, error)
	MarkHumanWriteAuthorityGranted(context.Context, operation.SessionID) error
	LoadInputAuthorityProvenance(context.Context, operation.SessionID) (string, error)
}

type Runtime interface {
	Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error)
	AttachHuman(context.Context, delegated.ProviderRef, delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error)
	SetHumanWritable(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, bool) error
	FenceHumanIngress(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegated.AuthorityEpoch) (delegatedapp.IngressFenceProof, error)
	InspectHumanClient(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) (delegatedapp.HumanClientObservation, error)
	ArmWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) error
	PrepareReadOnlyLocalControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef) error
	Signal(context.Context, delegated.ProviderRef, string) error
}

type AgentIngressProof struct {
	AuthorityEpoch     delegated.AuthorityEpoch
	ProviderGeneration string
	Fenced             bool
}

type AgentIngressFencer interface {
	FenceAgentIngress(context.Context, string, delegated.AuthorityEpoch) (AgentIngressProof, error)
}

type LocalAttachResult struct {
	State      handoff.State
	Attachment delegatedapp.HumanAttachResult
}

type ControlResult struct {
	State   handoff.State
	Outcome string
}

type WaitRequest struct {
	HandoffID string
	Yield     time.Duration
}

type WaitResult struct {
	State    handoff.State
	TimedOut bool
}
