package contextexec

import (
	"context"
	"time"

	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type Durability string

const (
	NoDurableChange Durability = "none"
	DurableChange   Durability = "durable"
	AmbiguousChange Durability = "ambiguous"
)

type MutationResult struct {
	Durability Durability
	Err        error
}

type ContextExecStore interface {
	ReserveOperation(context.Context, operation.Reservation) (operation.Reservation, bool, MutationResult)
	ReadOutput(context.Context, operation.SessionID, int64, int) ([]byte, int64, error)
	AppendOutput(context.Context, operation.SessionID, []byte) (int, MutationResult)
	PublishTerminal(context.Context, receipt.Receipt) MutationResult
	ReserveContextExec(context.Context, operation.ContextExecState) (operation.ContextExecState, bool, MutationResult)
	LookupContextExec(context.Context, string) (operation.ContextExecState, bool, error)
	AdvanceContextExec(context.Context, string, operation.ContextExecTransition) (operation.ContextExecState, MutationResult)
	BindHelperGeneration(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, MutationResult)
	AcquireContextExecLease(context.Context, operation.SessionID, delegated.AuthorityEpoch, string, string) (operation.ContextExecLease, bool, MutationResult)
	ReleaseContextExecLease(context.Context, operation.ContextExecLease) MutationResult
	FindContextExecLease(context.Context, operation.SessionID, delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error)
	ListContextExecRecoveryCandidates(context.Context) ([]operation.ContextExecState, error)
}

type PreparedAuthorization struct {
	ChildOperationID   operation.ID
	ChildSessionID     operation.SessionID
	ResolvedExecutable string
}

type SpawnTruth struct {
	ChildOperationID   operation.ID
	ChildSessionID     operation.SessionID
	ResolvedExecutable string
	Spawn              receipt.SpawnEvidence
}

type TerminalTruth struct {
	Result         core.Result
	StdoutBytes    int64
	StderrBytes    int64
	CombinedOutput []byte
}

type NoChildFailureTruth struct {
	ResolvedExecutable string
	Spawn              receipt.SpawnEvidence
	FailureCode        string
}

type ContextAuthority interface {
	Snapshot(context.Context, core.Request) (AuthoritySnapshot, error)
}

type HelperArmRequest struct {
	ProviderRef delegated.ProviderRef
	Helper      core.HelperBinding
	Expectation core.ContextExpectation
	Shell       shellapp.ContextHelperArmRequest
}

type RuntimeCallbacks struct {
	BindClaim                  func(context.Context, string, core.HelperBinding, core.ContextBinding, time.Time, string) (operation.ContextExecState, error)
	AuthorizePrepared          func(context.Context, operation.ContextExecState, string) (operation.ContextExecState, PreparedAuthorization, error)
	RecordSpawn                func(context.Context, operation.ContextExecState, SpawnTruth) (operation.ContextExecState, error)
	RecordTerminal             func(context.Context, operation.ContextExecState, TerminalTruth) (operation.ContextExecState, error)
	CanonicalizeNoChildFailure func(context.Context, operation.ContextExecState, NoChildFailureTruth) (operation.ContextExecState, error)
}

type RuntimeCallbackBinder interface {
	BindContextExecCallbacks(RuntimeCallbacks)
}

type TerminalScheduler interface {
	ScheduleContextTerminal(context.Context, receipt.Receipt, operation.Reservation) error
}

type HelperRuntime interface {
	Qualified() bool
	ArmContextHelper(context.Context, HelperArmRequest) (shellapp.ContextHelperArm, error)
}

type AuthoritySnapshot struct {
	Binding                   delegated.Binding
	ProviderRef               delegated.ProviderRef
	Observation               delegatedapp.Observation
	Authority                 delegated.EffectiveAuthority
	PrivacyProviderGeneration string
	PrivacyActive             bool
	PrivacyReleasePending     bool
	AgentIngressWritable      bool
	OwnershipTransferActive   bool
	Shell                     shellcore.ShellIdentity
}

type Options struct {
	Store               ContextExecStore
	Authority           ContextAuthority
	Helper              HelperRuntime
	TerminalScheduler   TerminalScheduler
	Now                 func() time.Time
	NewOpaqueLaunchID   func() string
	NewHelperGeneration func() string
	HelperExecutable    string
	DaemonIncarnation   string
}
