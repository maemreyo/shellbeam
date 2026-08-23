package contextexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

type splitAuthorityFake struct {
	snapshot      AuthoritySnapshot
	claimSnapshot ClaimAuthoritySnapshot
	snapshotCalls int
	claimCalls    int
}

func (f *splitAuthorityFake) Snapshot(context.Context, core.Request) (AuthoritySnapshot, error) {
	f.snapshotCalls++
	return f.snapshot, nil
}

func (f *splitAuthorityFake) ClaimSnapshot(context.Context, core.Request) (ClaimAuthoritySnapshot, error) {
	f.claimCalls++
	return f.claimSnapshot, nil
}

func claimAuthorityFromAdmission(snapshot AuthoritySnapshot) ClaimAuthoritySnapshot {
	return ClaimAuthoritySnapshot{
		Binding:                   snapshot.Binding,
		ProviderRef:               snapshot.ProviderRef,
		Observation:               snapshot.Observation,
		Authority:                 snapshot.Authority,
		PrivacyProviderGeneration: snapshot.PrivacyProviderGeneration,
		PrivacyActive:             snapshot.PrivacyActive,
		PrivacyReleasePending:     snapshot.PrivacyReleasePending,
		AgentIngressWritable:      snapshot.AgentIngressWritable,
		OwnershipTransferActive:   snapshot.OwnershipTransferActive,
	}
}

func claimContinuityForState(state operation.ContextExecState, claim ClaimAuthoritySnapshot) core.ShellContinuityExpectation {
	return core.ShellContinuityExpectation{
		SessionID:                state.Request.SessionID,
		AuthorityEpoch:           state.Request.AuthorityEpoch,
		ProviderGeneration:       state.Expectation.ProviderGeneration,
		ShellRuntimeIdentity:     state.Expectation.ShellIdentity,
		PaneShellPID:             claim.Observation.PanePID,
		PaneShellProcessIdentity: "proc_shell_claim_test",
		PaneTTY:                  claim.Observation.PaneTTY,
		HelperExecutableIdentity: state.Helper.ExecutablePath,
	}
}

func claimProofFor(continuity core.ShellContinuityExpectation) core.ShellContinuityProof {
	return core.ShellContinuityProof{
		SessionID:                continuity.SessionID,
		AuthorityEpoch:           continuity.AuthorityEpoch,
		ProviderGeneration:       continuity.ProviderGeneration,
		ShellRuntimeIdentity:     continuity.ShellRuntimeIdentity,
		PaneShellPID:             continuity.PaneShellPID,
		PaneShellProcessIdentity: continuity.PaneShellProcessIdentity,
		PaneTTY:                  continuity.PaneTTY,
		HelperPID:                continuity.PaneShellPID + 1,
		HelperExecutableIdentity: continuity.HelperExecutableIdentity,
		ForegroundProven:         true,
		ObservedAt:               time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
	}
}

func TestBindClaimUsesFreshNonShellAuthorityWhenHelperIsForeground(t *testing.T) {
	req := admissionRequest()
	state := helperRequestedState(t, req)
	prelaunch := admissionAuthority(t, req)
	claim := claimAuthorityFromAdmission(prelaunch)
	claim.Observation.CurrentCommand = "shellbeam"
	authority := &splitAuthorityFake{snapshot: prelaunch, claimSnapshot: claim}
	store := &admissionStoreFake{state: state, found: true}
	svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})

	final := core.ContextBinding{
		SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch,
		ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt",
		CWDObserved: state.Expectation.CWDObserved, PrivacyState: "standard",
	}
	continuity := claimContinuityForState(state, claim)
	proof := claimProofFor(continuity)
	got, err := svc.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, continuity, proof, time.Now().UTC(), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleHelperAuthenticated || authority.claimCalls != 1 || authority.snapshotCalls != 0 {
		t.Fatalf("bound=%#v claim_calls=%d snapshot_calls=%d", got, authority.claimCalls, authority.snapshotCalls)
	}
}

func TestBindClaimRevalidatesFreshAuthorityBeforeAtomicHelperBinding(t *testing.T) {
	req := admissionRequest()
	state := helperRequestedState(t, req)
	store := &admissionStoreFake{state: state, found: true}
	authoritySnapshot := admissionAuthority(t, req)
	authority := &authorityFake{snapshot: authoritySnapshot}
	svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})
	final := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: state.Expectation.CWDObserved, PrivacyState: "standard"}
	claim := authoritySnapshot.ClaimAuthoritySnapshot
	continuity := claimContinuityForState(state, claim)
	proof := claimProofFor(continuity)
	at := time.Date(2026, 8, 21, 14, 5, 0, 0, time.UTC)
	verifier := strings.Repeat("c", 64)
	got, err := svc.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, continuity, proof, at, verifier)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lifecycle != core.LifecycleHelperAuthenticated || got.Context == nil || *got.Context != final || !got.BoundaryObservedAt.Equal(at) {
		t.Fatalf("bound state=%#v", got)
	}
	if store.bindCalls != 1 || store.boundHelper != *state.Helper || store.boundContext != final || store.boundVerifier != verifier || !store.boundAt.Equal(at) {
		t.Fatalf("bind call mismatch")
	}
	if authority.claimCalls != 1 || authority.calls != 0 {
		t.Fatalf("claim calls=%d snapshot calls=%d", authority.claimCalls, authority.calls)
	}
}

func TestBindClaimAcceptsFreshCWDAliasForSameDirectoryIdentity(t *testing.T) {
	req := admissionRequest()
	state := helperRequestedState(t, req)
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	aliasDir := filepath.Join(root, "alias")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Fatal(err)
	}
	state.Expectation.CWDObserved = aliasDir
	store := &admissionStoreFake{state: state, found: true}
	authoritySnapshot := admissionAuthority(t, req)
	authoritySnapshot.Observation.CWD = realDir
	authority := &authorityFake{snapshot: authoritySnapshot}
	svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})
	final := core.ContextBinding{
		SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity,
		BoundaryQuality: "shell_prompt", CWDObserved: aliasDir, PrivacyState: "standard",
	}
	continuity := claimContinuityForState(state, authoritySnapshot.ClaimAuthoritySnapshot)
	proof := claimProofFor(continuity)
	at := time.Date(2026, 8, 21, 14, 6, 0, 0, time.UTC)
	got, err := svc.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, continuity, proof, at, strings.Repeat("e", 64))
	if err != nil {
		t.Fatalf("same-directory cwd alias rejected: %v", err)
	}
	if got.Lifecycle != core.LifecycleHelperAuthenticated || store.bindCalls != 1 || store.boundContext.CWDObserved != aliasDir {
		t.Fatalf("bound state=%#v bind_calls=%d context=%#v", got, store.bindCalls, store.boundContext)
	}
}

type bindClaimMutation func(*ClaimAuthoritySnapshot, *core.ContextBinding, *core.HelperBinding, *core.ShellContinuityExpectation, *core.ShellContinuityProof)

var bindClaimDriftCases = []struct {
	name   string
	code   failure.Code
	mutate bindClaimMutation
}{
	{name: "final cwd", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, c *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		c.CWDObserved = "/tmp/changed"
	}},
	{name: "final shell", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, c *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		c.ShellIdentity = "zsh:changed"
	}},
	{name: "helper generation", code: failure.ContextHelperAuthFailed, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, h *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		h.Generation = "other_generation"
	}},
	{name: "provider generation", code: failure.ContextExecStaleGeneration, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.Observation.ProviderGeneration = "other_gen"
		a.PrivacyProviderGeneration = "other_gen"
	}},
	{name: "privacy active", code: failure.ContextExecPrivacyBlocked, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.PrivacyActive = true
	}},
	{name: "privacy pending", code: failure.ContextExecPrivacyBlocked, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.PrivacyReleasePending = true
	}},
	{name: "owner", code: failure.ContextExecNotAgentOwned, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.Authority.Owner = delegated.OwnerHuman
		a.Authority.Fenced = true
	}},
	{name: "agent ingress", code: failure.ContextExecNotAgentOwned, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.AgentIngressWritable = false
	}},
	{name: "epoch", code: failure.ContextExecStaleGeneration, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.Authority.Epoch++
	}},
	{name: "continuity session", code: failure.ContextExecStaleGeneration, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.SessionID = "session_other"
		*p = claimProofFor(*c)
	}},
	{name: "continuity epoch", code: failure.ContextExecStaleGeneration, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.AuthorityEpoch++
		*p = claimProofFor(*c)
	}},
	{name: "continuity provider", code: failure.ContextExecStaleGeneration, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.ProviderGeneration = "other_gen"
		*p = claimProofFor(*c)
	}},
	{name: "continuity shell", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.ShellRuntimeIdentity = "zsh:other"
		*p = claimProofFor(*c)
	}},
	{name: "continuity helper", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.HelperExecutableIdentity = "/opt/shellbeam/bin/other"
		*p = claimProofFor(*c)
	}},
	{name: "proof process identity", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		p.PaneShellProcessIdentity = "proc_other"
	}},
	{name: "pane pid", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.PaneShellPID++
		*p = claimProofFor(*c)
	}},
	{name: "pane tty", code: failure.ContextExecBoundaryUnproven, mutate: func(_ *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, c *core.ShellContinuityExpectation, p *core.ShellContinuityProof) {
		c.PaneTTY = "/dev/ttys099"
		*p = claimProofFor(*c)
	}},
	{name: "fresh cwd", code: failure.ContextExecBoundaryUnproven, mutate: func(a *ClaimAuthoritySnapshot, _ *core.ContextBinding, _ *core.HelperBinding, _ *core.ShellContinuityExpectation, _ *core.ShellContinuityProof) {
		a.Observation.CWD = "/tmp/changed"
	}},
}

func TestBindClaimRejectsContextAuthorityOrContinuityDriftBeforeStoreBinding(t *testing.T) {
	req := admissionRequest()
	baseState := helperRequestedState(t, req)
	baseAuthority := admissionAuthority(t, req)
	baseContext := core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: baseState.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: baseState.Expectation.CWDObserved, PrivacyState: "standard"}

	for _, tc := range bindClaimDriftCases {
		t.Run(tc.name, func(t *testing.T) {
			state := baseState.Clone()
			claim := baseAuthority.ClaimAuthoritySnapshot
			final := baseContext
			helper := *state.Helper
			continuity := claimContinuityForState(state, claim)
			proof := claimProofFor(continuity)
			tc.mutate(&claim, &final, &helper, &continuity, &proof)
			store := &admissionStoreFake{state: state, found: true}
			authority := &authorityFake{snapshot: baseAuthority, claimSnapshot: claim}
			svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})
			_, err := svc.BindClaim(context.Background(), req.ContextExecID, helper, final, continuity, proof, time.Now(), strings.Repeat("d", 64))
			if !errors.Is(err, tc.code) {
				t.Fatalf("err=%v want=%s", err, tc.code)
			}
			if store.bindCalls != 0 {
				t.Fatalf("store bind called on drift")
			}
		})
	}
}

func helperRequestedState(t *testing.T, req core.Request) operation.ContextExecState {
	t.Helper()
	state := admissionReservedState(t, req)
	helper := core.HelperBinding{OpaqueLaunchID: "launch_claim_task6", Generation: "helper_generation_claim_task6", RequestFingerprint: state.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	state.Helper = &helper
	state.Lifecycle = core.LifecycleHelperRequested
	state.UpdatedAt = state.UpdatedAt.Add(time.Second)
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}
