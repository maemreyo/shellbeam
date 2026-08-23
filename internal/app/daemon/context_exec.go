package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"time"

	contextapp "github.com/maemreyo/shellbeam/internal/app/contextexec"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type ContextExecService interface {
	Execute(context.Context, contextcore.Request) (operation.ContextExecState, error)
	Reconcile(context.Context) ([]contextapp.RecoveryDecision, error)
}

type ContextExecCompositionOptions struct {
	Incarnation         string
	HelperExecutable    string
	NewOpaqueLaunchID   func() string
	NewHelperGeneration func() string
	TerminalScheduler   contextapp.TerminalScheduler
}

type contextExecRepository interface {
	Store
	DelegatedSessionStore
	ContextExecStore
}

type contextExecStoreAdapter struct{ store contextExecRepository }

func contextExecMutation(result StoreResult) contextapp.MutationResult {
	return contextapp.MutationResult{Durability: contextapp.Durability(result.Durability), Err: result.Err}
}
func (a contextExecStoreAdapter) ReserveOperation(ctx context.Context, v operation.Reservation) (operation.Reservation, bool, contextapp.MutationResult) {
	got, created, result := a.store.ReserveOperation(ctx, v)
	return got, created, contextExecMutation(result)
}
func (a contextExecStoreAdapter) ReadOutput(ctx context.Context, id operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	return a.store.ReadOutput(ctx, id, cursor, max)
}
func (a contextExecStoreAdapter) AppendOutput(ctx context.Context, id operation.SessionID, data []byte) (int, contextapp.MutationResult) {
	n, result := a.store.AppendOutput(ctx, id, data)
	return n, contextExecMutation(result)
}
func (a contextExecStoreAdapter) PublishTerminal(ctx context.Context, rec receipt.Receipt) contextapp.MutationResult {
	return contextExecMutation(a.store.PublishTerminal(ctx, rec))
}
func (a contextExecStoreAdapter) ReserveContextExec(ctx context.Context, v operation.ContextExecState) (operation.ContextExecState, bool, contextapp.MutationResult) {
	got, created, result := a.store.ReserveContextExec(ctx, v)
	return got, created, contextExecMutation(result)
}
func (a contextExecStoreAdapter) LookupContextExec(ctx context.Context, id string) (operation.ContextExecState, bool, error) {
	return a.store.LookupContextExec(ctx, id)
}
func (a contextExecStoreAdapter) AdvanceContextExec(ctx context.Context, id string, tr operation.ContextExecTransition) (operation.ContextExecState, contextapp.MutationResult) {
	got, result := a.store.AdvanceContextExec(ctx, id, tr)
	return got, contextExecMutation(result)
}
func (a contextExecStoreAdapter) BindHelperGeneration(ctx context.Context, id string, helper contextcore.HelperBinding, binding contextcore.ContextBinding, at time.Time, digest string) (operation.ContextExecState, contextapp.MutationResult) {
	got, result := a.store.BindHelperGeneration(ctx, id, helper, binding, at, digest)
	return got, contextExecMutation(result)
}
func (a contextExecStoreAdapter) AcquireContextExecLease(ctx context.Context, sid operation.SessionID, epoch delegated.AuthorityEpoch, id, fp string) (operation.ContextExecLease, bool, contextapp.MutationResult) {
	got, created, result := a.store.AcquireContextExecLease(ctx, sid, epoch, id, fp)
	return got, created, contextExecMutation(result)
}
func (a contextExecStoreAdapter) ReleaseContextExecLease(ctx context.Context, lease operation.ContextExecLease) contextapp.MutationResult {
	return contextExecMutation(a.store.ReleaseContextExecLease(ctx, lease))
}
func (a contextExecStoreAdapter) FindContextExecLease(ctx context.Context, sid operation.SessionID, epoch delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error) {
	return a.store.FindContextExecLease(ctx, sid, epoch)
}
func (a contextExecStoreAdapter) ListContextExecRecoveryCandidates(ctx context.Context) ([]operation.ContextExecState, error) {
	return a.store.ListContextExecRecoveryCandidates(ctx)
}

type contextExecAuthority struct {
	store    DelegatedSessionStore
	provider DelegatedRuntime
	privacy  delegatedapp.PrivacyInspector
	shell    shellapp.ShellProbe
}

func (a contextExecAuthority) ClaimSnapshot(ctx context.Context, req contextcore.Request) (contextapp.ClaimAuthoritySnapshot, error) {
	binding, err := a.store.LoadDelegatedBinding(ctx, operation.SessionID(req.SessionID))
	if err != nil {
		return contextapp.ClaimAuthoritySnapshot{}, err
	}
	ref, err := a.store.LoadDelegatedProviderRef(ctx, operation.SessionID(req.SessionID))
	if err != nil {
		return contextapp.ClaimAuthoritySnapshot{}, err
	}
	obs, err := a.provider.Inspect(ctx, ref)
	if err != nil {
		return contextapp.ClaimAuthoritySnapshot{}, err
	}
	privacy, err := a.privacy.InspectPrivacy(ctx, ref)
	if err != nil {
		return contextapp.ClaimAuthoritySnapshot{}, err
	}
	authority := delegated.ReconcileAuthority(delegated.ReconcileInput{Epoch: binding.AuthorityEpoch, DesiredOwner: binding.DesiredOwner, ObservedOwner: obs.Owner, ProviderIdentity: obs.Provider, ProviderCurrent: obs.ProviderCurrent})
	writable := binding.Lifecycle == delegated.LifecycleLive && binding.DesiredOwner == delegated.OwnerAgent && authority.Owner == delegated.OwnerAgent && !authority.Fenced
	return contextapp.ClaimAuthoritySnapshot{
		Binding: binding, ProviderRef: ref, Observation: obs, Authority: authority,
		PrivacyProviderGeneration: privacy.ProviderGeneration, PrivacyActive: privacy.Active,
		PrivacyReleasePending: privacy.ReleasePending, AgentIngressWritable: writable,
		OwnershipTransferActive: !writable,
	}, nil
}

func (a contextExecAuthority) Snapshot(ctx context.Context, req contextcore.Request) (contextapp.AuthoritySnapshot, error) {
	claim, err := a.ClaimSnapshot(ctx, req)
	if err != nil {
		return contextapp.AuthoritySnapshot{}, err
	}
	obs := claim.Observation
	facts := shellapp.ProviderProcessFacts{SessionID: req.SessionID, ProviderID: obs.Provider.ID, ProviderVersion: obs.Provider.Version, ProviderGeneration: obs.ProviderGeneration, PanePID: obs.PanePID, CurrentCommand: obs.CurrentCommand, PaneTTY: obs.PaneTTY, CWD: obs.CWD}
	shellObs, err := a.shell.Probe(ctx, shellapp.ProbeRequest{Facts: facts})
	if err != nil {
		return contextapp.AuthoritySnapshot{}, err
	}
	return contextapp.AuthoritySnapshot{ClaimAuthoritySnapshot: claim, Shell: shellObs.Identity}, nil
}

func ComposeContextExec(store Store, provider DelegatedRuntime, shellProbe shellapp.ShellProbe, helper contextapp.HelperRuntime, options ContextExecCompositionOptions) (*contextapp.Service, bool) {
	repository, ok := store.(contextExecRepository)
	if !ok || provider == nil || shellProbe == nil || helper == nil || !helper.Qualified() || options.Incarnation == "" || !filepath.IsAbs(options.HelperExecutable) {
		return nil, false
	}
	privacy, ok := provider.(delegatedapp.PrivacyInspector)
	if !ok {
		return nil, false
	}
	opaque := options.NewOpaqueLaunchID
	if opaque == nil {
		opaque = func() string { return contextExecOpaque("launch_") }
	}
	generation := options.NewHelperGeneration
	if generation == nil {
		generation = func() string { return contextExecOpaque("helper_") }
	}
	svc := contextapp.NewService(contextapp.Options{Store: contextExecStoreAdapter{store: repository}, Authority: contextExecAuthority{store: repository, provider: provider, privacy: privacy, shell: shellProbe}, Helper: helper, NewOpaqueLaunchID: opaque, NewHelperGeneration: generation, HelperExecutable: filepath.Clean(options.HelperExecutable), DaemonIncarnation: options.Incarnation, TerminalScheduler: options.TerminalScheduler})
	return svc, true
}

func contextExecOpaque(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return prefix + hex.EncodeToString(raw[:])
}

func (s *Service) ContextExecAvailable() bool {
	return s != nil && s.contextExec != nil
}

func (s *Service) ExecuteContext(ctx context.Context, req contextcore.Request) (operation.ContextExecState, error) {
	if s == nil || s.contextExec == nil {
		return operation.ContextExecState{}, failure.New(failure.ContextExecUnavailable, map[string]string{"context_exec_id": req.ContextExecID, "session_id": req.SessionID, "reason": "context_exec_unavailable"}, nil)
	}
	return s.contextExec.Execute(ctx, req)
}

func (s *Service) ReconcileContextExec(ctx context.Context) ([]contextapp.RecoveryDecision, error) {
	if s == nil || s.contextExec == nil {
		return nil, nil
	}
	return s.contextExec.Reconcile(ctx)
}
