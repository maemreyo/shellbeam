package daemon_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const dpDaemonWorkspaceID = "ws_01K00000000000000000000007"

func setupDaemonDecisionExperiment(t *testing.T, repository *storeadapter.Repository, experimentID string) {
	t.Helper()
	setupDaemonDecisionExperimentForWorkspace(t, repository, experimentID, dpDaemonWorkspaceID)
}

func setupDaemonDecisionExperimentForWorkspace(t *testing.T, repository *storeadapter.Repository, experimentID, workspaceID string) {
	t.Helper()
	store := storeadapter.NewDecisionProtocolStore(repository)
	ctx := context.Background()
	episodeID := dp.EpisodeID("ep-daemon-admission")
	episode := dp.Episode{
		SchemaVersion:     1,
		EpisodeID:         episodeID,
		EpisodeKind:       dp.EpisodeDiagnosis,
		RepositoryID:      "repo-daemon",
		WorkspaceID:       workspaceID,
		Baseline:          dp.EpisodeBaseline{SourceGeneration: "gen_" + strings.Repeat("a", 64)},
		PolicyBinding:     dp.EpisodePolicyBinding{PolicyID: "policy-daemon", PolicyDigest: "pol_" + strings.Repeat("b", 64), ActivationRef: "act-daemon"},
		CreatedByActorRef: "actor-daemon",
		CreatedAt:         time.Unix(10, 0).UTC(),
	}
	if _, _, err := store.CreateEpisode(ctx, episode); err != nil && !strings.Contains(err.Error(), "different canonical body") {
		t.Fatal(err)
	}
	experiment := dp.Experiment{SchemaVersion: 1, ExperimentID: dp.ExperimentID(experimentID), EpisodeID: episodeID, DeclaredByActorRef: "actor-daemon", DeclaredAt: time.Unix(11, 0).UTC()}
	if _, _, err := store.DefineExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	digest, err := dp.PredictionSetDigest(nil)
	if err != nil {
		t.Fatal(err)
	}
	hw, err := store.CurrentHighWater(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seal := dp.ExperimentSeal{
		ExperimentID:                  experiment.ExperimentID,
		SourceGeneration:              episode.Baseline.SourceGeneration,
		SealedPredictionDigest:        digest,
		BaseProjectionCutRef:          dp.DecisionProjectionCutRef{EpisodeID: episodeID, CanonicalRecordHighWater: hw},
		BaseCandidateProjectionDigest: "proj_" + strings.Repeat("c", 64),
		SealedAt:                      time.Unix(12, 0).UTC(),
	}
	if _, _, err := store.SealExperimentCAS(ctx, seal); err != nil {
		t.Fatal(err)
	}
}

func newDecisionProtocolDaemon(t *testing.T, experiments ...string) (*app.Service, *fakeOwner) {
	t.Helper()
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range experiments {
		setupDaemonDecisionExperiment(t, repository, id)
	}
	owner := &fakeOwner{}
	resolver := &fakeAddressResolver{workspaceID: workspace.WorkspaceID(dpDaemonWorkspaceID), logicalCWD: "src", cwd: "/repo/src"}
	return app.NewServiceWithWorkspaceResolver(repository, owner, resolver, app.Options{Incarnation: "dp-daemon", Shell: "/bin/sh", MaxQueuedInputBytes: 100}), owner
}

func decisionRawStart(operationID, experimentID string) app.StartRequest {
	return app.StartRequest{ProtocolVersion: 2, OperationID: operationID, WorkspaceID: dpDaemonWorkspaceID, ExperimentID: experimentID, Command: "true", CWD: "src", YieldMS: 100}
}

func TestStartReplayOmittedThenExperimentConflictsBeforeLiveExperimentLookup(t *testing.T) {
	svc, owner := newDecisionProtocolDaemon(t, "exp-daemon-a")
	first := decisionRawStart("op-dp-omit-first", "")
	view, err := svc.Start(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	second := first
	second.ExperimentID = "exp-daemon-a"
	if _, err := svc.Start(context.Background(), second); err == nil {
		t.Fatal("omitted->experiment replay unexpectedly succeeded")
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestStartReplayExperimentThenDifferentExperimentConflicts(t *testing.T) {
	svc, owner := newDecisionProtocolDaemon(t, "exp-daemon-a", "exp-daemon-b")
	first := decisionRawStart("op-dp-exp-change", "exp-daemon-a")
	view, err := svc.Start(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	second := first
	second.ExperimentID = "exp-daemon-b"
	if _, err := svc.Start(context.Background(), second); err == nil {
		t.Fatal("experiment->different experiment replay unexpectedly succeeded")
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestStartReplayExperimentThenOmittedConflicts(t *testing.T) {
	svc, owner := newDecisionProtocolDaemon(t, "exp-daemon-a")
	first := decisionRawStart("op-dp-exp-remove", "exp-daemon-a")
	view, err := svc.Start(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, view.SessionID)
	second := first
	second.ExperimentID = ""
	if _, err := svc.Start(context.Background(), second); err == nil {
		t.Fatal("experiment->omitted replay unexpectedly succeeded")
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestStartReplaySameExperimentReturnsOriginalAdmission(t *testing.T) {
	svc, owner := newDecisionProtocolDaemon(t, "exp-daemon-a")
	req := decisionRawStart("op-dp-exp-same", "exp-daemon-a")
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, first.SessionID)
	replay, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.SessionID != first.SessionID || owner.starts.Load() != 1 {
		t.Fatalf("first=%s replay=%s starts=%d", first.SessionID, replay.SessionID, owner.starts.Load())
	}
}

func TestProjectCommandReplayUsesSameExperimentBindingRules(t *testing.T) {
	sequence := &typedSequence{}
	store := newTypedRecordingStore(t, sequence)
	workspaceID := typedStartRequest("probe", "./internal/app").WorkspaceID
	setupDaemonDecisionExperimentForWorkspace(t, store.Repository, "exp-project-a", workspaceID)
	setupDaemonDecisionExperimentForWorkspace(t, store.Repository, "exp-project-b", workspaceID)
	binding := daemonProjectBinding(t, []string{"go", "test", "./internal/app"})
	binder := &typedBinder{sequence: sequence, binding: binding}
	owner := &typedOrderOwner{sequence: sequence}
	svc := app.NewService(store, owner, app.Options{Incarnation: "dp-project", Shell: "/bin/sh", MaxQueuedInputBytes: 100, ProjectCommandBinder: binder})
	req := typedStartRequest("op-dp-project", "./internal/app")
	req.ExperimentID = "exp-project-a"
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, first.SessionID)
	if owner.starts.Load() != 1 {
		t.Fatalf("first starts=%d", owner.starts.Load())
	}
	replay, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if replay.SessionID != first.SessionID || owner.starts.Load() != 1 {
		t.Fatalf("same experiment replay first=%s replay=%s starts=%d", first.SessionID, replay.SessionID, owner.starts.Load())
	}
	changed := req
	changed.ExperimentID = "exp-project-b"
	if _, err := svc.Start(context.Background(), changed); err == nil {
		t.Fatal("project command experiment change unexpectedly replayed")
	}
	removed := req
	removed.ExperimentID = ""
	if _, err := svc.Start(context.Background(), removed); err == nil {
		t.Fatal("project command experiment removal unexpectedly replayed")
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("replay conflicts spawned owner %d times", owner.starts.Load())
	}
}

func TestDecisionProtocolPersistentStartRejectedBeforeSpawn(t *testing.T) {
	svc, owner := newDecisionProtocolDaemon(t, "exp-daemon-persistent")
	req := decisionRawStart("op-dp-persistent", "exp-daemon-persistent")
	req.Persistent = true
	req.SessionName = "dp-persistent"
	if _, err := svc.Start(context.Background(), req); err == nil {
		t.Fatal("persistent experiment start unexpectedly succeeded")
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("persistent experiment rejection spawned %d processes", owner.starts.Load())
	}
}
