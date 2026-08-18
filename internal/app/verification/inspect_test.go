package verification

import (
	"context"
	"testing"
	"time"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type inspectWorkspaceSource struct {
	ws  workspace.Workspace
	err error
}

func (s *inspectWorkspaceSource) Inspect(context.Context, string) (workspace.Workspace, error) {
	return s.ws, s.err
}

type inspectPolicyLoader struct {
	result PolicyLoadResult
	calls  int
}

func (s *inspectPolicyLoader) Load(context.Context, workspace.Workspace) PolicyLoadResult {
	s.calls++
	return s.result
}

type inspectEffectiveStore struct {
	activation      core.PolicyActivation
	activationFound bool
	snapshot        core.PolicySnapshot
	snapshotFound   bool
}

func (s *inspectEffectiveStore) CurrentActivation(context.Context, workspace.RepositoryID) (core.PolicyActivation, bool, error) {
	return s.activation, s.activationFound, nil
}
func (s *inspectEffectiveStore) LoadPolicySnapshot(context.Context, workspace.RepositoryID, string) (core.PolicySnapshot, bool, error) {
	return s.snapshot, s.snapshotFound, nil
}

type inspectAffected struct {
	result AffectedResult
	calls  int
}

func (s *inspectAffected) Derive(context.Context, AffectedRequest) (AffectedResult, error) {
	s.calls++
	return s.result, nil
}

type inspectObligations struct {
	result ObligationResult
	calls  int
	last   ObligationRequest
}

func (s *inspectObligations) Derive(_ context.Context, req ObligationRequest) (ObligationResult, error) {
	s.calls++
	s.last = req
	return s.result, nil
}

type inspectWaivers struct {
	values []core.VerificationWaiver
	calls  int
	last   WaiverScope
}

func (s *inspectWaivers) ActiveWaivers(_ context.Context, scope WaiverScope) ([]core.VerificationWaiver, error) {
	s.calls++
	s.last = scope
	return append([]core.VerificationWaiver(nil), s.values...), nil
}

type inspectProjectSource struct {
	inspection project.Inspection
	calls      int
}

func (s *inspectProjectSource) Inspect(context.Context, string) (project.Inspection, error) {
	s.calls++
	return s.inspection, nil
}

type inspectStarter struct {
	proposal   core.PolicyProposal
	rendered   string
	advisories []string
	calls      int
}

func (s *inspectStarter) Preview(_ context.Context, profile, repo string, _ *project.Manifest) (PolicyPreview, error) {
	s.calls++
	proposal := s.proposal
	return PolicyPreview{Proposal: &proposal, RenderedTOML: s.rendered, Advisories: append([]string(nil), s.advisories...)}, nil
}

func inspectWorkspace() workspace.Workspace { return serviceWorkspace() }
func inspectSurface(t *testing.T) core.AffectedSurface {
	t.Helper()
	return baseSurfaceForClassification(t, "docs/a.md", core.CoverageComplete)
}
func inspectProposal(t *testing.T, id string) core.PolicyProposal {
	t.Helper()
	p := core.PolicyContent{SchemaVersion: 1, PolicyID: id}
	d, err := core.PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return core.PolicyProposal{RepositoryID: string(inspectWorkspace().RepositoryID), Digest: d, Origin: core.ProposalRepositoryAuthored, Content: p}
}
func inspectEffective(t *testing.T, id string) (core.PolicyActivation, core.PolicySnapshot) {
	t.Helper()
	p := inspectProposal(t, id)
	a := core.PolicyActivation{SchemaVersion: 1, ActivationID: "act_" + id, IntentFingerprint: "ifp_x", RepositoryID: p.RepositoryID, PreviousEffectiveDigest: "absent", ProposedPolicyDigest: p.Digest, ProposalOrigin: core.ProposalRepositoryAuthored, ProposalGeneration: serviceGen('1'), ActivationGeneration: serviceGen('2'), Authority: AuthorityExplicitCaller, Actor: "tester", ActivatedAt: time.Unix(10, 0).UTC()}
	return a, core.PolicySnapshot{RepositoryID: p.RepositoryID, Digest: p.Digest, Content: p.Content}
}
func newInspectService(t *testing.T, load PolicyLoadResult, activation core.PolicyActivation, found bool, snapshot core.PolicySnapshot, snapshotFound bool) (*InspectionService, *inspectStarter, *inspectObligations, *inspectWaivers) {
	t.Helper()
	starter := &inspectStarter{}
	obligations := &inspectObligations{}
	waivers := &inspectWaivers{}
	svc := NewInspectionService(&inspectWorkspaceSource{ws: inspectWorkspace()}, &inspectPolicyLoader{result: load}, &inspectEffectiveStore{activation: activation, activationFound: found, snapshot: snapshot, snapshotFound: snapshotFound}, &inspectAffected{result: AffectedResult{Surface: inspectSurface(t)}}, obligations, waivers, &inspectProjectSource{}, starter)
	return svc, starter, obligations, waivers
}

func TestPolicyAbsentDoesNotSelectStarter(t *testing.T) {
	svc, starter, obligations, waivers := newInspectService(t, PolicyLoadResult{State: PolicyLoadAbsent}, core.PolicyActivation{}, false, core.PolicySnapshot{}, false)
	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: string(inspectWorkspace().ID), Phase: core.PhaseCheckpoint})
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyState != PolicyStateAbsent || got.EffectivePolicy != nil || got.ProposedPolicy != nil || len(got.Obligations) != 0 {
		t.Fatalf("got=%#v", got)
	}
	if starter.calls != 0 || obligations.calls != 0 || waivers.calls != 0 {
		t.Fatalf("starter=%d obligations=%d waivers=%d", starter.calls, obligations.calls, waivers.calls)
	}
}

func TestInspectionValidFirstPolicyIsProposalPendingButNotEffective(t *testing.T) {
	p := inspectProposal(t, "p1")
	svc, _, obligations, _ := newInspectService(t, PolicyLoadResult{State: PolicyLoadValid, Proposal: &p}, core.PolicyActivation{}, false, core.PolicySnapshot{}, false)
	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: string(inspectWorkspace().ID), Phase: core.PhaseInnerLoop})
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyState != PolicyStateProposalPending || got.ProposedPolicy == nil || got.ProposedPolicy.Digest != p.Digest || got.EffectivePolicy != nil {
		t.Fatalf("got=%#v", got)
	}
	if obligations.calls != 0 {
		t.Fatal("proposal evaluated as effective")
	}
}

func TestInspectionInvalidAndUnsupportedPolicyStayLiteral(t *testing.T) {
	for _, tc := range []struct {
		load PolicyLoadState
		want PolicyState
	}{{PolicyLoadInvalid, PolicyStateInvalid}, {PolicyLoadUnsupported, PolicyStateUnsupported}} {
		t.Run(string(tc.load), func(t *testing.T) {
			svc, _, _, _ := newInspectService(t, PolicyLoadResult{State: tc.load}, core.PolicyActivation{}, false, core.PolicySnapshot{}, false)
			got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: string(inspectWorkspace().ID), Phase: core.PhaseCheckpoint})
			if err != nil {
				t.Fatal(err)
			}
			if got.PolicyState != tc.want {
				t.Fatalf("state=%q", got.PolicyState)
			}
		})
	}
}

func TestInspectionKeepsEffectiveP1WhileP2PendingAndBindsEffectiveClassifiers(t *testing.T) {
	a, snap := inspectEffective(t, "p1")
	p2 := inspectProposal(t, "p2")
	snap.Content.Classifiers = []core.Classification{{ID: "docs", Paths: []string{"docs/**"}, SurfaceClass: "documentation"}}
	snap.Digest = mustPolicyDigestInspect(t, snap.Content)
	a.ProposedPolicyDigest = snap.Digest
	svc, _, obligations, waivers := newInspectService(t, PolicyLoadResult{State: PolicyLoadValid, Proposal: &p2}, a, true, snap, true)
	obligations.result = ObligationResult{}
	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: string(inspectWorkspace().ID), ActivityID: "activity-1", Phase: core.PhaseCheckpoint})
	if err != nil {
		t.Fatal(err)
	}
	if got.PolicyState != PolicyStateProposalPending || got.EffectivePolicy == nil || got.EffectivePolicy.Digest != snap.Digest || got.ProposedPolicy == nil || got.ProposedPolicy.Digest != p2.Digest {
		t.Fatalf("got=%#v", got)
	}
	if obligations.calls != 1 || waivers.calls != 1 {
		t.Fatalf("obligations=%d waivers=%d", obligations.calls, waivers.calls)
	}
	if obligations.last.Policy.Snapshot.Digest != snap.Digest {
		t.Fatalf("matcher got=%s", obligations.last.Policy.Snapshot.Digest)
	}
	foundClass := false
	for _, r := range obligations.last.Surface.Relations {
		if r.Kind == "classified_as" && r.To.Value == "documentation" {
			foundClass = true
			if !containsString(r.ProvenanceRefs, "policy:"+snap.Digest) {
				t.Fatalf("provenance=%v", r.ProvenanceRefs)
			}
		}
	}
	if !foundClass {
		t.Fatal("effective classification missing")
	}
}

func TestInspectionEffectivePolicySurvivesInvalidRepositoryProposal(t *testing.T) {
	a, snap := inspectEffective(t, "p1")
	svc, _, obligations, _ := newInspectService(t, PolicyLoadResult{State: PolicyLoadInvalid}, a, true, snap, true)
	_, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: string(inspectWorkspace().ID), Phase: core.PhaseCheckpoint})
	if err != nil {
		t.Fatal(err)
	}
	if obligations.calls != 1 || obligations.last.Policy.Snapshot.Digest != snap.Digest {
		t.Fatalf("effective policy not used: %#v", obligations.last.Policy)
	}
}

func TestPolicyPreviewIsExplicitAndNeverActivatesStarter(t *testing.T) {
	repo := inspectProposal(t, "repo")
	loader := &inspectPolicyLoader{result: PolicyLoadResult{State: PolicyLoadValid, Proposal: &repo}}
	starterProposal := inspectProposal(t, "starter")
	starterProposal.Origin = core.ProposalStarterProfile
	starterProposal.ProfileOrigin = "shellbeam/team@v1"
	starter := &inspectStarter{proposal: starterProposal, rendered: "schema_version = 1\n", advisories: []string{"preview"}}
	projects := &inspectProjectSource{inspection: project.Inspection{Status: project.StatusValid, Manifest: &project.Manifest{SchemaVersion: 2}}}
	svc := NewInspectionService(&inspectWorkspaceSource{ws: inspectWorkspace()}, loader, &inspectEffectiveStore{}, &inspectAffected{result: AffectedResult{Surface: inspectSurface(t)}}, &inspectObligations{}, &inspectWaivers{}, projects, starter)
	plain, err := svc.PreviewPolicy(context.Background(), PreviewPolicyRequest{WorkspaceID: string(inspectWorkspace().ID)})
	if err != nil {
		t.Fatal(err)
	}
	if plain.State != PolicyLoadValid || plain.Proposal == nil || plain.Proposal.Digest != repo.Digest || starter.calls != 0 {
		t.Fatalf("plain=%#v starter=%d", plain, starter.calls)
	}
	profile, err := svc.PreviewPolicy(context.Background(), PreviewPolicyRequest{WorkspaceID: string(inspectWorkspace().ID), Profile: "team"})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Proposal == nil || profile.Proposal.Origin != core.ProposalStarterProfile || profile.RenderedTOML != "schema_version = 1\n" || starter.calls != 1 || projects.calls != 1 {
		t.Fatalf("profile=%#v starter=%d project=%d", profile, starter.calls, projects.calls)
	}
}

func mustPolicyDigestInspect(t *testing.T, p core.PolicyContent) string {
	t.Helper()
	d, err := core.PolicyDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
