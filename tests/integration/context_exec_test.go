//go:build darwin

package integration_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	contextapp "github.com/maemreyo/shellbeam/internal/app/contextexec"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func TestContextExecHighAssuranceExactReplaySurvivesRestartWithoutSecondHelper(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	cwd := t.TempDir()
	repository := openH1Store(t, root)
	req := h5MatrixRequest("ctxexec_matrix_replay", "session_matrix_replay")
	authority := &h5MatrixAuthority{snapshots: []contextapp.AuthoritySnapshot{h5MatrixAuthoritySnapshot(req, cwd)}}
	h5MatrixSeedDelegatedState(t, repository, authority.snapshots[0])
	helper := &h5MatrixHelper{}
	svc := h5MatrixService(repository, authority, helper)

	first, err := svc.Execute(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Lifecycle != contextcore.LifecycleHelperRequested || first.Helper == nil || helper.armCalls != 1 || authority.snapshotCalls != 2 {
		t.Fatalf("first=%#v arms=%d snapshots=%d", first, helper.armCalls, authority.snapshotCalls)
	}

	replayed, err := svc.Execute(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.RequestFingerprint != first.RequestFingerprint || replayed.Lifecycle != first.Lifecycle || helper.armCalls != 1 || authority.snapshotCalls != 2 {
		t.Fatalf("replay=%#v arms=%d snapshots=%d", replayed, helper.armCalls, authority.snapshotCalls)
	}

	changed := req.Clone()
	changed.Argv = append(changed.Argv, "changed")
	if _, err := svc.Execute(t.Context(), changed); !errors.Is(err, failure.OperationConflict) {
		t.Fatalf("changed request err=%v want operation_conflict", err)
	}
	if helper.armCalls != 1 || authority.snapshotCalls != 2 {
		t.Fatalf("conflict caused side effects arms=%d snapshots=%d", helper.armCalls, authority.snapshotCalls)
	}

	reopened := openH1Store(t, root)
	persisted, found, err := reopened.LookupContextExec(t.Context(), req.ContextExecID)
	if err != nil || !found || persisted.Lifecycle != contextcore.LifecycleHelperRequested || persisted.RequestFingerprint != first.RequestFingerprint {
		t.Fatalf("persisted=%#v found=%v err=%v", persisted, found, err)
	}
	candidates, err := reopened.ListContextExecRecoveryCandidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Request.ContextExecID != req.ContextExecID || candidates[0].Lifecycle != contextcore.LifecycleHelperRequested {
		t.Fatalf("recovery candidates=%#v", candidates)
	}

	restartedAuthority := &h5MatrixAuthority{snapshots: []contextapp.AuthoritySnapshot{h5MatrixAuthoritySnapshot(req, cwd)}}
	restartedHelper := &h5MatrixHelper{}
	restarted := h5MatrixService(reopened, restartedAuthority, restartedHelper)
	decisions, err := restarted.Reconcile(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Disposition != contextapp.RecoveryAmbiguousDelivery || decisions[0].State.Lifecycle != contextcore.LifecycleAmbiguous {
		t.Fatalf("recovery decisions=%#v", decisions)
	}
	if restartedHelper.armCalls != 0 || restartedAuthority.snapshotCalls != 0 {
		t.Fatalf("recovery re-armed helper arms=%d snapshots=%d", restartedHelper.armCalls, restartedAuthority.snapshotCalls)
	}
}

var h5AdmissionMatrixCases = []struct {
	name       string
	want       failure.Code
	mutate     func([]contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot
	wantProbes int
}{
	{
		name: "stale epoch", want: failure.ContextExecStaleGeneration, wantProbes: 1,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			values[0].Authority.Epoch++
			return values
		},
	},
	{
		name: "human owned", want: failure.ContextExecNotAgentOwned, wantProbes: 1,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			values[0].Authority.Owner = delegated.OwnerHuman
			values[0].Authority.Fenced = true
			return values
		},
	},
	{
		name: "private capture", want: failure.ContextExecPrivacyBlocked, wantProbes: 1,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			values[0].PrivacyActive = true
			values[0].PrivacyReleasePending = true
			return values
		},
	},
	{
		name: "unknown shell", want: failure.ContextExecBoundaryUnproven, wantProbes: 1,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			values[0].Shell = shellcore.ShellIdentity{Family: shellcore.ShellUnknown, RuntimeID: "unknown_runtime"}
			return values
		},
	},
	{
		name: "nested shell drift", want: failure.ContextExecBoundaryUnproven, wantProbes: 2,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			second := values[0]
			second.Shell = shellcore.ShellIdentity{Family: shellcore.ShellZsh, RuntimeID: "zsh_nested_runtime"}
			return []contextapp.AuthoritySnapshot{values[0], second}
		},
	},
	{
		name: "provider generation drift", want: failure.ContextExecStaleGeneration, wantProbes: 2,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			second := values[0]
			second.Observation.ProviderGeneration = "gen_matrix_other"
			second.PrivacyProviderGeneration = "gen_matrix_other"
			return []contextapp.AuthoritySnapshot{values[0], second}
		},
	},
	{
		name: "cwd drift", want: failure.ContextExecBoundaryUnproven, wantProbes: 2,
		mutate: func(values []contextapp.AuthoritySnapshot) []contextapp.AuthoritySnapshot {
			second := values[0]
			second.Observation.CWD = filepath.Join(values[0].Observation.CWD, "other")
			return []contextapp.AuthoritySnapshot{values[0], second}
		},
	},
}

func TestContextExecHighAssuranceAdmissionMatrixFailsBeforeHelperArm(t *testing.T) {
	for _, tc := range h5AdmissionMatrixCases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			req := h5MatrixRequest("ctxexec_matrix_"+strings.ReplaceAll(tc.name, " ", "_"), "session_matrix_"+strings.ReplaceAll(tc.name, " ", "_"))
			values := []contextapp.AuthoritySnapshot{h5MatrixAuthoritySnapshot(req, cwd)}
			authority := &h5MatrixAuthority{snapshots: tc.mutate(values)}
			helper := &h5MatrixHelper{}
			svc := h5MatrixService(openH1Store(t, filepath.Join(t.TempDir(), "state")), authority, helper)
			if _, err := svc.Execute(t.Context(), req); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%s", err, tc.want)
			}
			if helper.armCalls != 0 {
				t.Fatalf("helper armed on rejected admission: %d", helper.armCalls)
			}
			if authority.snapshotCalls != tc.wantProbes {
				t.Fatalf("authority probes=%d want=%d", authority.snapshotCalls, tc.wantProbes)
			}
		})
	}
}

type h5MatrixStore struct {
	inner *storeadapter.Repository
}

func h5MatrixMutation(result daemonapp.StoreResult) contextapp.MutationResult {
	return contextapp.MutationResult{Durability: contextapp.Durability(result.Durability), Err: result.Err}
}

func (s h5MatrixStore) ReserveOperation(ctx context.Context, value operation.Reservation) (operation.Reservation, bool, contextapp.MutationResult) {
	got, created, result := s.inner.ReserveOperation(ctx, value)
	return got, created, h5MatrixMutation(result)
}
func (s h5MatrixStore) ReadOutput(ctx context.Context, id operation.SessionID, cursor int64, max int) ([]byte, int64, error) {
	return s.inner.ReadOutput(ctx, id, cursor, max)
}
func (s h5MatrixStore) AppendOutput(ctx context.Context, id operation.SessionID, data []byte) (int, contextapp.MutationResult) {
	n, result := s.inner.AppendOutput(ctx, id, data)
	return n, h5MatrixMutation(result)
}
func (s h5MatrixStore) PublishTerminal(ctx context.Context, value receipt.Receipt) contextapp.MutationResult {
	return h5MatrixMutation(s.inner.PublishTerminal(ctx, value))
}
func (s h5MatrixStore) ReserveContextExec(ctx context.Context, value operation.ContextExecState) (operation.ContextExecState, bool, contextapp.MutationResult) {
	got, created, result := s.inner.ReserveContextExec(ctx, value)
	return got, created, h5MatrixMutation(result)
}
func (s h5MatrixStore) LookupContextExec(ctx context.Context, id string) (operation.ContextExecState, bool, error) {
	return s.inner.LookupContextExec(ctx, id)
}
func (s h5MatrixStore) AdvanceContextExec(ctx context.Context, id string, transition operation.ContextExecTransition) (operation.ContextExecState, contextapp.MutationResult) {
	got, result := s.inner.AdvanceContextExec(ctx, id, transition)
	return got, h5MatrixMutation(result)
}
func (s h5MatrixStore) BindHelperGeneration(ctx context.Context, id string, helper contextcore.HelperBinding, binding contextcore.ContextBinding, at time.Time, digest string) (operation.ContextExecState, contextapp.MutationResult) {
	got, result := s.inner.BindHelperGeneration(ctx, id, helper, binding, at, digest)
	return got, h5MatrixMutation(result)
}
func (s h5MatrixStore) AcquireContextExecLease(ctx context.Context, sessionID operation.SessionID, epoch delegated.AuthorityEpoch, id, fingerprint string) (operation.ContextExecLease, bool, contextapp.MutationResult) {
	got, created, result := s.inner.AcquireContextExecLease(ctx, sessionID, epoch, id, fingerprint)
	return got, created, h5MatrixMutation(result)
}
func (s h5MatrixStore) ReleaseContextExecLease(ctx context.Context, lease operation.ContextExecLease) contextapp.MutationResult {
	return h5MatrixMutation(s.inner.ReleaseContextExecLease(ctx, lease))
}
func (s h5MatrixStore) FindContextExecLease(ctx context.Context, sessionID operation.SessionID, epoch delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error) {
	return s.inner.FindContextExecLease(ctx, sessionID, epoch)
}
func (s h5MatrixStore) ListContextExecRecoveryCandidates(ctx context.Context) ([]operation.ContextExecState, error) {
	return s.inner.ListContextExecRecoveryCandidates(ctx)
}

type h5MatrixAuthority struct {
	snapshots     []contextapp.AuthoritySnapshot
	snapshotCalls int
	claimCalls    int
}

func (a *h5MatrixAuthority) Snapshot(context.Context, contextcore.Request) (contextapp.AuthoritySnapshot, error) {
	a.snapshotCalls++
	index := a.snapshotCalls - 1
	if index >= len(a.snapshots) {
		index = len(a.snapshots) - 1
	}
	return a.snapshots[index], nil
}
func (a *h5MatrixAuthority) ClaimSnapshot(context.Context, contextcore.Request) (contextapp.ClaimAuthoritySnapshot, error) {
	a.claimCalls++
	return a.snapshots[len(a.snapshots)-1].ClaimAuthoritySnapshot, nil
}

type h5MatrixHelper struct {
	armCalls int
}

func (*h5MatrixHelper) Qualified() bool { return true }
func (h *h5MatrixHelper) ArmContextHelper(_ context.Context, request contextapp.HelperArmRequest) (shellapp.ContextHelperArm, error) {
	h.armCalls++
	shell := request.Shell
	return shellapp.ContextHelperArm{
		ContextExecID: shell.ContextExecID, SessionID: shell.SessionID, AuthorityEpoch: shell.Authority.Epoch,
		ProviderGeneration: shell.Facts.ProviderGeneration, Shell: shell.ExpectedShell,
		PaneShellPID: shell.Facts.PanePID, PaneTTY: shell.Facts.PaneTTY,
		OpaqueLaunchID: shell.OpaqueLaunchID, ArmedAt: time.Date(2026, 8, 22, 14, 0, 2, 0, time.UTC),
	}, nil
}

func h5MatrixSeedDelegatedState(t *testing.T, repository *storeadapter.Repository, snapshot contextapp.AuthoritySnapshot) {
	t.Helper()
	reservation := operation.Reservation{
		SchemaVersion: 5, OperationID: operation.ID(snapshot.Binding.OperationID), SessionID: operation.SessionID(snapshot.Binding.SessionID),
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: snapshot.Observation.CWD, Shell: "/bin/sh",
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 1, DaemonIncarnation: "integration-context-exec",
	}
	if _, created, result := repository.ReserveOperation(t.Context(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve delegated parent operation created=%v result=%#v", created, result)
	}
	binding := snapshot.Binding
	binding.AuthorityEpoch = 1
	binding.Lifecycle = delegated.LifecycleProvisioning
	binding.UpdatedAt = binding.CreatedAt
	if _, created, result := repository.ReserveDelegatedBinding(t.Context(), binding, snapshot.ProviderRef); result.Err != nil || !created {
		t.Fatalf("reserve delegated binding created=%v result=%#v", created, result)
	}
	binding.Lifecycle = delegated.LifecycleLive
	binding.AuthorityEpoch = snapshot.Binding.AuthorityEpoch
	binding.UpdatedAt = binding.UpdatedAt.Add(time.Second)
	if result := repository.AdvanceDelegatedBinding(t.Context(), binding); result.Err != nil {
		t.Fatalf("advance delegated binding: %#v", result)
	}
}

func h5MatrixService(repository *storeadapter.Repository, authority *h5MatrixAuthority, helper *h5MatrixHelper) *contextapp.Service {
	return contextapp.NewService(contextapp.Options{
		Store: h5MatrixStore{inner: repository}, Authority: authority, Helper: helper,
		Now:                 func() time.Time { return time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC) },
		NewOpaqueLaunchID:   func() string { return "launch_matrix_01" },
		NewHelperGeneration: func() string { return "helper_generation_matrix_01" },
		HelperExecutable:    "/opt/shellbeam/bin/shellbeam", DaemonIncarnation: "integration-context-exec",
	})
}

func h5MatrixRequest(id, sessionID string) contextcore.Request {
	return contextcore.Request{
		ContextExecID: id, SessionID: sessionID, AuthorityEpoch: 4,
		Argv: []string{"/usr/bin/printf", "ok"}, TimeoutMS: 1000, MaxOutputBytes: 4096,
	}
}

func h5MatrixAuthoritySnapshot(req contextcore.Request, cwd string) contextapp.AuthoritySnapshot {
	at := time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)
	binding := delegated.Binding{
		SchemaVersion: delegated.BindingSchemaVersion, SessionID: req.SessionID, OperationID: "parent_operation_matrix",
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: req.AuthorityEpoch, DesiredOwner: delegated.OwnerAgent,
		ProviderID: "tmux_control_mode", ProviderVersion: 1, Lifecycle: delegated.LifecycleLive, CreatedAt: at, UpdatedAt: at,
	}
	ref := delegated.ProviderRef{
		SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: req.SessionID, ProviderID: binding.ProviderID,
		ProviderVersion: binding.ProviderVersion, Ref: "provider_ref_matrix", CreatedAt: at, UpdatedAt: at,
	}
	observation := delegatedapp.Observation{
		Provider: binding.ProviderIdentity(), ProviderCurrent: true, ProviderGeneration: "gen_matrix_exact", Owner: delegated.OwnerAgent,
		PanePID: 4242, CurrentCommand: "zsh", PaneTTY: "/dev/ttys042", CWD: cwd,
	}
	return contextapp.AuthoritySnapshot{
		ClaimAuthoritySnapshot: contextapp.ClaimAuthoritySnapshot{
			Binding: binding, ProviderRef: ref, Observation: observation,
			Authority:                 delegated.EffectiveAuthority{Epoch: req.AuthorityEpoch, Owner: delegated.OwnerAgent},
			PrivacyProviderGeneration: "gen_matrix_exact", AgentIngressWritable: true,
		},
		Shell: shellcore.ShellIdentity{Family: shellcore.ShellZsh, RuntimeID: "zsh_matrix_runtime"},
	}
}
