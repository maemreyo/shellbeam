package verification

import (
	"context"
	"errors"
	"testing"
	"time"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeWorkspaceLookup struct {
	values []workspace.Workspace
	err    error
}

func (f *fakeWorkspaceLookup) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), f.values...), f.err
}

type fakePolicyLoader struct {
	result PolicyLoadResult
	calls  int
}

func (f *fakePolicyLoader) Load(context.Context, workspace.Workspace) PolicyLoadResult {
	f.calls++
	return f.result
}

type fakeSnapshotter struct {
	result workspace.FastSnapshot
	calls  int
}

func (f *fakeSnapshotter) ObserveFresh(context.Context, string) workspace.FastSnapshot {
	f.calls++
	return f.result
}

type fakeProjectInspector struct {
	result project.Inspection
	err    error
	calls  int
}

func (f *fakeProjectInspector) Inspect(context.Context, string) (project.Inspection, error) {
	f.calls++
	return f.result, f.err
}

type fakeCommandResolver struct {
	err   error
	calls []string
}

func (f *fakeCommandResolver) Resolve(_ context.Context, workspaceID, commandID string, _ map[string]string) (project.CommandBinding, error) {
	f.calls = append(f.calls, workspaceID+":"+commandID)
	return project.CommandBinding{}, f.err
}

type fakeAuthorityStore struct {
	activation      core.PolicyActivation
	activationFound bool
	current         core.PolicyActivation
	currentFound    bool
	activateResult  core.ActivationWriteResult
	activateErr     error
	calls           []string
	snapshots       map[string]core.PolicySnapshot
	waivers         map[string]core.VerificationWaiver
	revocations     map[string]core.WaiverRevocation
}

func (f *fakeAuthorityStore) PutPolicySnapshot(_ context.Context, s core.PolicySnapshot) (bool, error) {
	f.calls = append(f.calls, "put_snapshot")
	if f.snapshots == nil {
		f.snapshots = map[string]core.PolicySnapshot{}
	}
	_, ok := f.snapshots[s.Digest]
	f.snapshots[s.Digest] = s
	return !ok, nil
}
func (f *fakeAuthorityStore) FindActivation(context.Context, workspace.RepositoryID, string) (core.PolicyActivation, bool, error) {
	f.calls = append(f.calls, "find_activation")
	return f.activation, f.activationFound, nil
}
func (f *fakeAuthorityStore) ActivatePolicyCAS(_ context.Context, c core.PolicyActivationCommit) (core.ActivationWriteResult, error) {
	f.calls = append(f.calls, "activate")
	if f.activateErr != nil {
		return core.ActivationWriteResult{}, f.activateErr
	}
	if f.activateResult.Record.ActivationID == "" {
		fp, _ := core.ActivationIntentFingerprint(c.Intent)
		f.activateResult = core.ActivationWriteResult{Record: core.PolicyActivation{SchemaVersion: 1, ActivationID: c.Intent.ActivationID, IntentFingerprint: fp, RepositoryID: c.Intent.RepositoryID, PreviousEffectiveDigest: c.Intent.PreviousEffectiveDigest, ProposedPolicyDigest: c.Intent.ProposedPolicyDigest, ProposalOrigin: c.ProposalOrigin, ProfileOrigin: c.ProfileOrigin, ProposalGeneration: c.Intent.ProposalGeneration, ActivationGeneration: c.ActivationGeneration, Authority: c.Intent.Authority, Actor: c.Intent.Actor, ActivatedAt: time.Unix(100, 0).UTC()}, Created: true, Effective: true}
	}
	return f.activateResult, nil
}
func (f *fakeAuthorityStore) CurrentActivation(context.Context, workspace.RepositoryID) (core.PolicyActivation, bool, error) {
	f.calls = append(f.calls, "current_activation")
	return f.current, f.currentFound, nil
}
func (f *fakeAuthorityStore) LoadPolicySnapshot(_ context.Context, _ workspace.RepositoryID, d string) (core.PolicySnapshot, bool, error) {
	f.calls = append(f.calls, "load_snapshot")
	s, ok := f.snapshots[d]
	return s, ok, nil
}
func (f *fakeAuthorityStore) FindWaiver(_ context.Context, _ workspace.RepositoryID, id string) (core.VerificationWaiver, bool, error) {
	f.calls = append(f.calls, "find_waiver")
	w, ok := f.waivers[id]
	return w, ok, nil
}
func (f *fakeAuthorityStore) PutWaiver(_ context.Context, in core.VerificationWaiverIntent) (core.WaiverWriteResult, error) {
	f.calls = append(f.calls, "put_waiver")
	fp, _ := core.WaiverIntentFingerprint(in)
	w := core.VerificationWaiver{SchemaVersion: 1, WaiverID: in.WaiverID, IntentFingerprint: fp, RepositoryID: in.RepositoryID, PolicyDigest: in.PolicyDigest, RuleID: in.RuleID, Phase: in.Phase, Generation: in.Generation, CheckpointID: in.CheckpointID, Authority: in.Authority, Actor: in.Actor, Reason: in.Reason, CreatedAt: time.Unix(100, 0).UTC(), ExpiresAt: in.ExpiresAt, ExpiresPhase: in.ExpiresPhase}
	if f.waivers == nil {
		f.waivers = map[string]core.VerificationWaiver{}
	}
	_, exists := f.waivers[in.WaiverID]
	f.waivers[in.WaiverID] = w
	return core.WaiverWriteResult{Record: w, Created: !exists, Replayed: exists, Active: true}, nil
}
func (f *fakeAuthorityStore) FindWaiverRevocation(_ context.Context, _ workspace.RepositoryID, id string) (core.WaiverRevocation, bool, error) {
	r, ok := f.revocations[id]
	return r, ok, nil
}
func (f *fakeAuthorityStore) PutWaiverRevocation(_ context.Context, in core.WaiverRevocationIntent) (core.RevocationWriteResult, error) {
	f.calls = append(f.calls, "put_revocation")
	fp, _ := core.RevocationIntentFingerprint(in)
	r := core.WaiverRevocation{SchemaVersion: 1, WaiverID: in.WaiverID, IntentFingerprint: fp, Authority: in.Authority, Actor: in.Actor, RevokedAt: time.Unix(100, 0).UTC()}
	if f.revocations == nil {
		f.revocations = map[string]core.WaiverRevocation{}
	}
	f.revocations[in.WaiverID] = r
	return core.RevocationWriteResult{Record: r, Created: true}, nil
}
func (f *fakeAuthorityStore) ListWaivers(context.Context, workspace.RepositoryID) ([]core.VerificationWaiver, []core.WaiverRevocation, error) {
	ws := make([]core.VerificationWaiver, 0, len(f.waivers))
	for _, w := range f.waivers {
		ws = append(ws, w)
	}
	rs := make([]core.WaiverRevocation, 0, len(f.revocations))
	for _, r := range f.revocations {
		rs = append(rs, r)
	}
	return ws, rs, nil
}

func serviceWorkspace() workspace.Workspace {
	return workspace.Workspace{SchemaVersion: workspace.SchemaVersion, ID: workspace.WorkspaceID("ws_01K00000000000000000000000"), RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), Label: "test", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
}
func serviceGen(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return "gen_" + string(b)
}
func serviceProposal(t *testing.T, withCommand bool) core.PolicyProposal {
	t.Helper()
	rule := core.Rule{ID: "r1", Phases: []core.Phase{core.PhaseCheckpoint}, Ownership: core.OwnershipApplicationOwned, Required: true, SufficiencyBasis: "checkpoint", MinimumAffectedAuthority: core.AuthorityMechanical}
	if withCommand {
		rule.Evidence = []core.EvidenceRequirement{{ID: "ev", ProviderClass: core.ProviderProjectCommand, ProjectCommandID: "check", MinimumAuthority: core.AuthorityMechanical, RequireCurrent: true, Environment: core.EnvironmentNone, Stability: core.StabilityNoContradiction}}
	} else {
		rule.Evidence = []core.EvidenceRequirement{{ID: "ev", ProviderClass: core.ProviderStaticFormatCheck, MinimumAuthority: core.AuthorityMechanical, RequireCurrent: true, Environment: core.EnvironmentNone, Stability: core.StabilityNoContradiction}}
	}
	p := core.PolicyContent{SchemaVersion: 1, PolicyID: "p1", Rules: []core.Rule{rule}}
	d, err := core.PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return core.PolicyProposal{RepositoryID: string(serviceWorkspace().RepositoryID), Digest: d, Origin: core.ProposalRepositoryAuthored, Content: p}
}
func freshSnapshot(gen string) workspace.FastSnapshot {
	return workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, RepositoryID: serviceWorkspace().RepositoryID, WorkspaceID: serviceWorkspace().ID, Generation: gen, Head: "0123456789012345678901234567890123456789", Dirty: workspace.DirtySummary{Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, Quality: workspace.QualityFresh, ObservedAt: time.Unix(10, 0).UTC()}
}

func newTestPolicyService(t *testing.T, proposal core.PolicyProposal, store *fakeAuthorityStore, snap *fakeSnapshotter, inspector *fakeProjectInspector, resolver *fakeCommandResolver) *PolicyService {
	t.Helper()
	return NewPolicyService(&fakeWorkspaceLookup{values: []workspace.Workspace{serviceWorkspace()}}, &fakePolicyLoader{result: PolicyLoadResult{State: PolicyLoadValid, Proposal: &proposal}}, store, store, snap, inspector, resolver)
}

func TestActivateReplayUsesStoredIntentBeforeProposalOrFreshObservation(t *testing.T) {
	p := serviceProposal(t, false)
	intent := core.PolicyActivationIntent{ActivationID: "act_retry", RepositoryID: string(serviceWorkspace().RepositoryID), PreviousEffectiveDigest: "absent", ProposedPolicyDigest: p.Digest, ProposalGeneration: serviceGen('1'), Authority: "explicit_caller", Actor: "tester"}
	fp, _ := core.ActivationIntentFingerprint(intent)
	stored := core.PolicyActivation{SchemaVersion: 1, ActivationID: intent.ActivationID, IntentFingerprint: fp, RepositoryID: intent.RepositoryID, PreviousEffectiveDigest: "absent", ProposedPolicyDigest: p.Digest, ProposalOrigin: core.ProposalRepositoryAuthored, ProposalGeneration: intent.ProposalGeneration, ActivationGeneration: serviceGen('2'), Authority: intent.Authority, Actor: intent.Actor, ActivatedAt: time.Unix(5, 0).UTC()}
	store := &fakeAuthorityStore{activation: stored, activationFound: true, activateResult: core.ActivationWriteResult{Record: stored, Replayed: true, Effective: true}}
	loader := &fakePolicyLoader{result: PolicyLoadResult{State: PolicyLoadInvalid}}
	snap := &fakeSnapshotter{}
	svc := NewPolicyService(&fakeWorkspaceLookup{values: []workspace.Workspace{serviceWorkspace()}}, loader, store, store, snap, &fakeProjectInspector{}, &fakeCommandResolver{})
	got, err := svc.Activate(context.Background(), ActivateRequest{ActivationID: "act_retry", WorkspaceID: string(serviceWorkspace().ID), ProposedPolicyDigest: p.Digest, ExpectedPreviousDigest: "absent", ProposalGeneration: serviceGen('1'), Authority: "explicit_caller", Actor: "tester"})
	if err != nil || !got.Replayed {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if loader.calls != 0 || snap.calls != 0 {
		t.Fatalf("replay observed loader=%d snapshot=%d", loader.calls, snap.calls)
	}
}

func TestPolicyCannotActivateForItsIntroducingGeneration(t *testing.T) {
	p := serviceProposal(t, false)
	store := &fakeAuthorityStore{}
	snap := &fakeSnapshotter{result: freshSnapshot(serviceGen('1'))}
	svc := newTestPolicyService(t, p, store, snap, &fakeProjectInspector{}, &fakeCommandResolver{})
	_, err := svc.Activate(context.Background(), ActivateRequest{ActivationID: "act_samegen", WorkspaceID: string(serviceWorkspace().ID), ProposedPolicyDigest: p.Digest, ExpectedPreviousDigest: "absent", ProposalGeneration: serviceGen('1'), Authority: "explicit_caller", Actor: "tester"})
	if err == nil {
		t.Fatal("same-generation policy activated")
	}
	for _, c := range store.calls {
		if c == "activate" {
			t.Fatal("store activation called")
		}
	}
}

func TestActivationRequiresExplicitCallerAndExactProjectBindings(t *testing.T) {
	p := serviceProposal(t, true)
	for name, authority := range map[string]string{"good": "explicit_caller", "bad": "policy_granted"} {
		t.Run(name, func(t *testing.T) {
			store := &fakeAuthorityStore{}
			snap := &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}
			inspector := &fakeProjectInspector{result: project.Inspection{Status: project.StatusValid, Manifest: &project.Manifest{SchemaVersion: 2}}}
			resolver := &fakeCommandResolver{}
			svc := newTestPolicyService(t, p, store, snap, inspector, resolver)
			_, err := svc.Activate(context.Background(), ActivateRequest{ActivationID: "act_" + name, WorkspaceID: string(serviceWorkspace().ID), ProposedPolicyDigest: p.Digest, ExpectedPreviousDigest: "absent", ProposalGeneration: serviceGen('1'), Authority: authority, Actor: "tester"})
			if authority == "explicit_caller" {
				if err != nil {
					t.Fatal(err)
				}
				if len(resolver.calls) != 1 || len(store.calls) < 2 || store.calls[len(store.calls)-2] != "put_snapshot" || store.calls[len(store.calls)-1] != "activate" {
					t.Fatalf("resolver=%v calls=%v", resolver.calls, store.calls)
				}
			} else if err == nil {
				t.Fatal("policy authority accepted")
			}
		})
	}
}

func TestActivationBindingFailureFailsClosed(t *testing.T) {
	p := serviceProposal(t, true)
	store := &fakeAuthorityStore{}
	svc := newTestPolicyService(t, p, store, &fakeSnapshotter{result: freshSnapshot(serviceGen('2'))}, &fakeProjectInspector{result: project.Inspection{Status: project.StatusValid, Manifest: &project.Manifest{SchemaVersion: 2}}}, &fakeCommandResolver{err: errors.New("binding unavailable")})
	if _, err := svc.Activate(context.Background(), ActivateRequest{ActivationID: "act_bind", WorkspaceID: string(serviceWorkspace().ID), ProposedPolicyDigest: p.Digest, ExpectedPreviousDigest: "absent", ProposalGeneration: serviceGen('1'), Authority: "explicit_caller", Actor: "tester"}); err == nil {
		t.Fatal("binding failure accepted")
	}
}

func TestWaiverScopeExpiryPolicyDigestDeterministic(t *testing.T) {
	p := serviceProposal(t, false)
	snapshot := core.PolicySnapshot{RepositoryID: string(serviceWorkspace().RepositoryID), Digest: p.Digest, Content: p.Content}
	current := core.PolicyActivation{SchemaVersion: 1, ActivationID: "act_current", RepositoryID: string(serviceWorkspace().RepositoryID), ProposedPolicyDigest: p.Digest, ActivatedAt: time.Unix(1, 0).UTC()}
	store := &fakeAuthorityStore{current: current, currentFound: true, snapshots: map[string]core.PolicySnapshot{p.Digest: snapshot}}
	svc := newTestPolicyService(t, p, store, &fakeSnapshotter{}, &fakeProjectInspector{}, &fakeCommandResolver{})
	svc.now = func() time.Time { return time.Unix(100, 0).UTC() }
	req := SetWaiverRequest{WaiverID: "wv_scope", WorkspaceID: string(serviceWorkspace().ID), PolicyDigest: p.Digest, RuleID: "r1", Phase: core.PhaseCheckpoint, Generation: serviceGen('2'), Authority: "explicit_caller", Actor: "tester", Reason: "accepted risk", ExpiresAt: time.Unix(200, 0).UTC()}
	got, err := svc.SetWaiver(context.Background(), req)
	if err != nil || !got.Active {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	active, err := svc.ActiveWaivers(context.Background(), WaiverScope{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Generation: req.Generation})
	if err != nil || len(active) != 1 {
		t.Fatalf("active=%v err=%v", active, err)
	}
	svc.now = func() time.Time { return time.Unix(200, 0).UTC() }
	active, err = svc.ActiveWaivers(context.Background(), WaiverScope{WorkspaceID: req.WorkspaceID, Phase: req.Phase, Generation: req.Generation})
	if err != nil || len(active) != 0 {
		t.Fatalf("expired=%v err=%v", active, err)
	}
}

func TestRevokeWaiverUsesWorkspaceRepositoryScope(t *testing.T) {
	p := serviceProposal(t, false)
	store := &fakeAuthorityStore{waivers: map[string]core.VerificationWaiver{
		"wv_one": {SchemaVersion: 1, WaiverID: "wv_one", RepositoryID: string(serviceWorkspace().RepositoryID)},
	}}
	svc := newTestPolicyService(t, p, store, &fakeSnapshotter{}, &fakeProjectInspector{}, &fakeCommandResolver{})
	_, err := svc.RevokeWaiver(context.Background(), RevokeWaiverRequest{WaiverID: "wv_one", WorkspaceID: string(serviceWorkspace().ID), Authority: "explicit_caller", Actor: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.revocations) != 1 {
		t.Fatal("revocation not stored")
	}
}
