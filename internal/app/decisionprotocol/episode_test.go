package decisionprotocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const dpRepoID = "repo_01K00000000000000000000001"
const dpWorkspaceID = "ws_01K00000000000000000000001"

type fakeEpisodeLedger struct {
	records                 []core.CanonicalRecordEnvelope
	selectionCommitFailures int
}

func (f *fakeEpisodeLedger) append(kind core.RecordKind, body any) (core.CanonicalRecordEnvelope, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return core.CanonicalRecordEnvelope{}, err
	}
	env := core.CanonicalRecordEnvelope{SchemaVersion: 1, CanonicalRecordSeq: core.RecordSeq(len(f.records) + 1), Kind: kind, Body: b}
	if err := env.Validate(); err != nil {
		return core.CanonicalRecordEnvelope{}, err
	}
	f.records = append(f.records, env)
	return env, nil
}
func (f *fakeEpisodeLedger) AppendRecord(_ context.Context, k core.RecordKind, v any) (core.CanonicalRecordEnvelope, error) {
	return f.append(k, v)
}
func (f *fakeEpisodeLedger) LoadRecord(_ context.Context, seq core.RecordSeq) (core.CanonicalRecordEnvelope, bool, error) {
	if seq == 0 || int(seq) > len(f.records) {
		return core.CanonicalRecordEnvelope{}, false, nil
	}
	return f.records[seq-1], true, nil
}
func (f *fakeEpisodeLedger) CurrentHighWater(context.Context) (core.RecordSeq, error) {
	return core.RecordSeq(len(f.records)), nil
}
func (f *fakeEpisodeLedger) ListEpisodeRecords(_ context.Context, ep core.EpisodeID, cut core.RecordSeq) ([]core.CanonicalRecordEnvelope, error) {
	var out []core.CanonicalRecordEnvelope
	for _, r := range f.records {
		if r.CanonicalRecordSeq > cut {
			continue
		}
		var id core.EpisodeID
		switch r.Kind {
		case core.RecordEpisode:
			var v core.Episode
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordCandidate:
			var v core.Candidate
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordExperiment:
			var v core.Experiment
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordPredictionBinding:
			var v core.PredictionBinding
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordExperimentSeal, core.RecordExperimentExecutionLink, core.RecordExperimentObservationBinding, core.RecordExperimentClosure, core.RecordExperimentAbort:
			var experimentID core.ExperimentID
			switch r.Kind {
			case core.RecordExperimentSeal:
				var v core.ExperimentSeal
				_ = json.Unmarshal(r.Body, &v)
				experimentID = v.ExperimentID
			case core.RecordExperimentExecutionLink:
				var v core.ExperimentExecutionLink
				_ = json.Unmarshal(r.Body, &v)
				experimentID = v.ExperimentID
			case core.RecordExperimentObservationBinding:
				var v core.ExperimentObservationBinding
				_ = json.Unmarshal(r.Body, &v)
				experimentID = v.ExperimentID
			case core.RecordExperimentClosure:
				var v core.ExperimentClosure
				_ = json.Unmarshal(r.Body, &v)
				experimentID = v.ExperimentID
			case core.RecordExperimentAbort:
				var v core.ExperimentAbort
				_ = json.Unmarshal(r.Body, &v)
				experimentID = v.ExperimentID
			}
			for _, parent := range f.records {
				if parent.Kind != core.RecordExperiment {
					continue
				}
				var v core.Experiment
				_ = json.Unmarshal(parent.Body, &v)
				if v.ExperimentID == experimentID {
					id = v.EpisodeID
					break
				}
			}
		case core.RecordVerifierAssessment:
			var v core.VerifierAssessment
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordSelectionCommit:
			var v core.SelectionCommit
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		case core.RecordClosure:
			var v core.DecisionClosure
			_ = json.Unmarshal(r.Body, &v)
			id = v.EpisodeID
		}
		if id == ep {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeEpisodeLedger) CreateEpisode(_ context.Context, e core.Episode) (core.CanonicalRecordEnvelope, bool, error) {
	for _, r := range f.records {
		if r.Kind == core.RecordEpisode {
			var got core.Episode
			_ = json.Unmarshal(r.Body, &got)
			if got.EpisodeID == e.EpisodeID {
				return r, false, nil
			}
		}
	}
	env, err := f.append(core.RecordEpisode, e)
	return env, err == nil, err
}
func (f *fakeEpisodeLedger) CreateCandidate(_ context.Context, c core.Candidate) (core.CanonicalRecordEnvelope, bool, error) {
	env, err := f.append(core.RecordCandidate, c)
	return env, err == nil, err
}
func (f *fakeEpisodeLedger) ReviseCandidateCAS(context.Context, core.CandidateID, core.Candidate) (core.CanonicalRecordEnvelope, error) {
	return core.CanonicalRecordEnvelope{}, fmt.Errorf("unused")
}
func (f *fakeEpisodeLedger) FindEpisode(_ context.Context, id core.EpisodeID) (core.Episode, bool, error) {
	for _, r := range f.records {
		if r.Kind == core.RecordEpisode {
			var v core.Episode
			_ = json.Unmarshal(r.Body, &v)
			if v.EpisodeID == id {
				return v, true, nil
			}
		}
	}
	return core.Episode{}, false, nil
}
func (f *fakeEpisodeLedger) FindCandidate(_ context.Context, id core.CandidateID) (core.Candidate, bool, error) {
	for _, r := range f.records {
		if r.Kind != core.RecordCandidate {
			continue
		}
		var candidate core.Candidate
		if err := json.Unmarshal(r.Body, &candidate); err != nil {
			return core.Candidate{}, false, err
		}
		if candidate.CandidateID == id {
			return candidate, true, nil
		}
	}
	return core.Candidate{}, false, nil
}

type fakeDPWorkspaceInspector struct{ ws workspace.Workspace }

func (f fakeDPWorkspaceInspector) Inspect(context.Context, string) (workspace.Workspace, error) {
	return f.ws, nil
}

type fakeDPSourceSnapshotter struct{ snap workspace.FastSnapshot }

func (f fakeDPSourceSnapshotter) ObserveFresh(context.Context, string) workspace.FastSnapshot {
	return f.snap
}

func validDPWorkspaceAndSnapshot(t *testing.T, generationSalt string) (workspace.Workspace, workspace.FastSnapshot) {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	ws := workspace.Workspace{SchemaVersion: workspace.SchemaVersion, ID: workspace.WorkspaceID(dpWorkspaceID), RepositoryID: workspace.RepositoryID(dpRepoID), Label: "test", Root: "/repo", GitDir: "/repo/.git", Branch: "main", CreatedAt: now, LastSeenAt: now}
	base := workspace.FastSnapshot{SchemaVersion: workspace.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: strings.Repeat(generationSalt, 40), Ref: "refs/heads/main", Dirty: workspace.DirtySummary{Digest: strings.Repeat("d", 64)}, Quality: workspace.QualityFresh, ObservedAt: now}
	snap, err := workspace.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	return ws, snap
}
func currentDPPolicy(t *testing.T, store *fakePolicyStore) {
	t.Helper()
	content := core.PolicyContent{PolicyID: "p-new", EpisodeKinds: []core.EpisodeKind{core.EpisodeDiagnosis}, OverridePolicy: core.OverridePolicy{Allowed: false}}
	digest, err := core.PolicyDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	store.currentSnapshot = core.PolicySnapshot{SchemaVersion: 1, RepositoryID: dpRepoID, PolicyDigest: digest, Content: content}
	store.currentActivation = core.PolicyActivation{ActivationID: "act-new", RepositoryID: dpRepoID, PolicyDigest: digest, ProposalGeneration: "gen_" + strings.Repeat("a", 64), ActivationGeneration: "gen_" + strings.Repeat("b", 64), Authority: core.AuthorityExplicitCaller, ActorRef: "actor", ActivatedAt: time.Unix(10, 0).UTC()}
	store.currentOK = true
}
func episodeService(t *testing.T, policy *fakePolicyStore, ledger *fakeEpisodeLedger, snap workspace.FastSnapshot) *Service {
	t.Helper()
	ws, _ := validDPWorkspaceAndSnapshot(t, "a")
	return NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws: ws}, Snapshots: fakeDPSourceSnapshotter{snap: snap}})
}

func TestCreateEpisodeBindsServerResolvedCurrentEffectivePolicy(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	got, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ExpectedPolicyDigest: policy.currentSnapshot.PolicyDigest, ExpectedActivationRef: "act-new", ActorRef: "actor"})
	if err != nil {
		t.Fatal(err)
	}
	ep, ok, err := ledger.FindEpisode(context.Background(), "ep-1")
	if err != nil || !ok {
		t.Fatal("episode missing")
	}
	if ep.PolicyBinding.PolicyDigest != policy.currentSnapshot.PolicyDigest || ep.PolicyBinding.ActivationRef != "act-new" || ep.Baseline.SourceGeneration != snap.Generation || got.EpisodeState != core.EpisodeOpen {
		t.Fatalf("episode=%#v projection=%#v", ep, got)
	}
}

func TestCreateEpisodeHistoricalActivationGuardCannotSelectWeakerPolicy(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	_, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ExpectedPolicyDigest: policy.currentSnapshot.PolicyDigest, ExpectedActivationRef: "act-old", ActorRef: "actor"})
	if reason, ok := core.ReasonOf(err); !ok || reason != core.ReasonPolicyConflict {
		t.Fatalf("err=%v reason=%q", err, reason)
	}
	if _, ok, _ := ledger.FindEpisode(context.Background(), "ep-1"); ok {
		t.Fatal("episode created against historical guard")
	}
}

func TestCreateEpisodeExpectedPolicyMismatchReturnsPolicyConflict(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	_, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ExpectedPolicyDigest: "pol_" + strings.Repeat("f", 64), ExpectedActivationRef: "act-new", ActorRef: "actor"})
	if reason, ok := core.ReasonOf(err); !ok || reason != core.ReasonPolicyConflict {
		t.Fatalf("err=%v reason=%q", err, reason)
	}
}

func TestCreateEpisodeCapturesCanonicalSourceGenerationOnly(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-1", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	ep, _, _ := ledger.FindEpisode(context.Background(), "ep-1")
	if ep.Baseline.SourceGeneration != snap.Generation || !strings.HasPrefix(ep.Baseline.SourceGeneration, "gen_") {
		t.Fatalf("generation=%q", ep.Baseline.SourceGeneration)
	}
}

func TestInspectMarksSourceGenerationStaleWithoutUsingAuditCounters(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snapA := validDPWorkspaceAndSnapshot(t, "a")
	svcA := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snapA}})
	if _, err := svcA.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-stale", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	_, snapB := validDPWorkspaceAndSnapshot(t, "b")
	svcB := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snapB}})
	got, err := svcB.Inspect(context.Background(), "ep-stale", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceGenerationCompatibility != core.SourceGenerationStale || got.SourceCompatible {
		t.Fatalf("projection=%#v", got)
	}
}

func TestInspectRejectsBothTerminalKinds(t *testing.T) {
	policy := &fakePolicyStore{}
	currentDPPolicy(t, policy)
	ledger := &fakeEpisodeLedger{}
	ws, snap := validDPWorkspaceAndSnapshot(t, "a")
	svc := NewService(policy, nil, EpisodeDependencies{Mutations: ledger, Ledger: ledger, Workspaces: fakeDPWorkspaceInspector{ws}, Snapshots: fakeDPSourceSnapshotter{snap}})
	if _, err := svc.CreateEpisode(context.Background(), CreateEpisodeRequest{EpisodeID: "ep-terminal", Kind: core.EpisodeDiagnosis, RepositoryID: dpRepoID, WorkspaceID: dpWorkspaceID, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	commit := core.SelectionCommit{CommitID: "commit-1", EpisodeID: "ep-terminal", CandidateID: "cand-x", PolicyDigest: policy.currentSnapshot.PolicyDigest, ProjectionDigest: "proj_" + strings.Repeat("e", 64), SourceGeneration: snap.Generation, IdempotencyKey: "idem", SemanticIntentFingerprint: "sel_" + strings.Repeat("f", 64), CommittedByActorRef: "actor", CommittedAt: time.Unix(200, 0).UTC()}
	closure := core.DecisionClosure{EpisodeID: "ep-terminal", Kind: core.ClosureUnresolved, Reason: "unresolved", UnresolvedDimensions: []string{}, ActorRef: "actor", ProjectionDigest: "proj_" + strings.Repeat("e", 64), ClosedAt: time.Unix(201, 0).UTC()}
	if _, err := ledger.append(core.RecordSelectionCommit, commit); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.append(core.RecordClosure, closure); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Inspect(context.Background(), "ep-terminal", ""); err == nil {
		t.Fatal("both terminal kinds accepted")
	}
}
