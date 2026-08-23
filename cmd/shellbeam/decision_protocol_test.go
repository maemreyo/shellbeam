package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	decisionapp "github.com/maemreyo/shellbeam/internal/app/decisionprotocol"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type decisionWorkspaceListFake struct {
	workspaces []workspacecore.Workspace
	err        error
}

func (f decisionWorkspaceListFake) List(context.Context) ([]workspacecore.Workspace, error) {
	return append([]workspacecore.Workspace(nil), f.workspaces...), f.err
}

func (f decisionWorkspaceListFake) Inspect(_ context.Context, id string) (workspacecore.Workspace, error) {
	if f.err != nil {
		return workspacecore.Workspace{}, f.err
	}
	for _, workspace := range f.workspaces {
		if string(workspace.ID) == id {
			return workspace, nil
		}
	}
	return workspacecore.Workspace{}, context.Canceled
}

func TestDecisionProtocolRuntimeRequiresSingleWorkspaceContext(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	got, err := resolveDecisionWorkspace(context.Background(), "", decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}})
	if err != nil || got.ID != ws.ID {
		t.Fatalf("single workspace resolution got=%#v err=%v", got, err)
	}
	if _, err := resolveDecisionWorkspace(context.Background(), "", decisionWorkspaceListFake{}); err == nil {
		t.Fatal("zero-workspace context was guessed")
	}
	if _, err := resolveDecisionWorkspace(context.Background(), "", decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws, ws}}); err == nil {
		t.Fatal("ambiguous workspace context was guessed")
	}
}

func TestDecisionProtocolRuntimeCapabilityUsesBuiltInProviderContract(t *testing.T) {
	got := capability.Baseline(capability.Limits{}).WithDecisionProtocol(decisionProtocolSupport())
	if got.Features[capability.FeatureDecisionProtocol] != capability.Available || got.DecisionProtocol == nil {
		t.Fatalf("decision protocol not advertised: %#v", got.DecisionProtocol)
	}
	if len(got.DecisionProtocol.AuthorityProviders) != 1 || got.DecisionProtocol.AuthorityProviders[0] != decisionapp.ExplicitCallerAuthorityProviderID+".v1" {
		t.Fatalf("authority providers=%#v", got.DecisionProtocol.AuthorityProviders)
	}
}

func TestDecisionProtocolTrustedActorUsesServerOwnedUIDBinding(t *testing.T) {
	if got := trustedDecisionActorRef(501); got != "shellbeam:explicit_caller:uid:501" {
		t.Fatalf("actor_ref=%q", got)
	}
}

type decisionOperationsCapture struct {
	decisionProtocolOperations
	policyReq      decisionapp.PutPolicySnapshotRequest
	materializeReq decisionapp.MaterializeAuthorityRequest
}

func (f *decisionOperationsCapture) PutPolicySnapshot(_ context.Context, req decisionapp.PutPolicySnapshotRequest) (decisioncore.PolicySnapshot, error) {
	f.policyReq = req
	return decisioncore.PolicySnapshot{RepositoryID: req.RepositoryID}, nil
}

func (f *decisionOperationsCapture) MaterializeAuthority(_ context.Context, req decisionapp.MaterializeAuthorityRequest) (decisionapp.MaterializeAuthorityResult, error) {
	f.materializeReq = req
	return decisionapp.MaterializeAuthorityResult{Status: decisioncore.QualificationUnknown}, nil
}

func TestDecisionProtocolDispatchDerivesRepositoryAndTrustedAuthorityActor(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	ops := &decisionOperationsCapture{}
	runtime := &decisionProtocolRuntime{service: ops, workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, trustedPeerUID: func(context.Context) (uint32, bool) { return 501, true }}
	content := decisioncore.PolicyContent{PolicyID: "policy-runtime", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}}
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.policy.snapshot", "", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}}); err != nil {
		t.Fatal(err)
	}
	if ops.policyReq.RepositoryID != string(ws.RepositoryID) || ops.policyReq.Content.PolicyID != content.PolicyID {
		t.Fatalf("policy request=%#v", ops.policyReq)
	}
	class := decisioncore.AuthorityClass{Domain: "shellbeam", ClassID: "explicit_caller", Version: 1}
	scope := decisioncore.AuthorityScope{RepositoryID: string(ws.RepositoryID), EpisodeID: "ep-runtime", ActionKind: decisioncore.AuthorityActionCommitSelectionOverride}
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.authority.materialize", "", ipcadapter.DecisionRequestV1{AuthorityRequest: &ipcadapter.DecisionAuthorityMaterializeInputV1{RequiredAuthorityClass: class, RequiredScope: scope}}); err != nil {
		t.Fatal(err)
	}
	if ops.materializeReq.ActorRef != "shellbeam:explicit_caller:uid:501" || !ops.materializeReq.RequiredAuthorityClass.Equal(class) || ops.materializeReq.RequiredScope != scope {
		t.Fatalf("materialize request=%#v", ops.materializeReq)
	}
}

type decisionObservationStoreFake struct {
	reservation operation.Reservation
	receipt     receipt.Receipt
	highs       []observationcore.ChangeSeq
	highIndex   int
}

func (f *decisionObservationStoreFake) FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error) {
	return f.reservation, true, nil
}
func (f *decisionObservationStoreFake) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	return f.receipt, nil
}
func (f *decisionObservationStoreFake) ObservationHighWatermark(context.Context) (observationcore.ChangeSeq, error) {
	if len(f.highs) == 0 {
		return 0, nil
	}
	i := f.highIndex
	if i >= len(f.highs) {
		i = len(f.highs) - 1
	}
	f.highIndex++
	return f.highs[i], nil
}

type decisionEvidenceSourceFake struct {
	set verificationapp.CandidateResultSet
}

func (f decisionEvidenceSourceFake) Candidates(context.Context, verificationapp.CandidateQuery) (verificationapp.CandidateResultSet, error) {
	return f.set, nil
}

func TestDecisionProtocolVerificationReaderFreezesExactEvidenceCut(t *testing.T) {
	store := &decisionObservationStoreFake{reservation: operation.Reservation{OperationID: "op-target", SessionID: "session-target", WorkspaceID: "ws-1", ActivityID: "activity-1"}, highs: []observationcore.ChangeSeq{7, 7, 7}}
	source := decisionEvidenceSourceFake{set: verificationapp.CandidateResultSet{Coverage: verificationcore.CoverageComplete, Candidates: []verificationcore.EvidenceCandidate{{OperationID: "op-target"}, {OperationID: "op-other"}}}}
	reader := decisionVerificationSource{store: store, candidates: source}
	cut, err := reader.AcquireVerificationObservationCut(context.Background(), "op-target")
	if err != nil || cut.EvidenceIndexGeneration != 7 {
		t.Fatalf("cut=%#v err=%v", cut, err)
	}
	set, err := reader.QualifiedEvidenceForOperation(context.Background(), "op-target", cut)
	if err != nil {
		t.Fatal(err)
	}
	if set.Cut != cut || set.Coverage != verificationcore.CoverageComplete || len(set.Candidates) != 1 || set.Candidates[0].OperationID != "op-target" {
		t.Fatalf("qualified set=%#v", set)
	}
}

func TestDecisionProtocolVerificationReaderFailsClosedWhenEvidenceCutAdvances(t *testing.T) {
	store := &decisionObservationStoreFake{reservation: operation.Reservation{OperationID: "op-target", SessionID: "session-target", WorkspaceID: "ws-1"}, highs: []observationcore.ChangeSeq{7, 7, 8}}
	reader := decisionVerificationSource{store: store, candidates: decisionEvidenceSourceFake{set: verificationapp.CandidateResultSet{Coverage: verificationcore.CoverageComplete}}}
	cut, err := reader.AcquireVerificationObservationCut(context.Background(), "op-target")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.QualifiedEvidenceForOperation(context.Background(), "op-target", cut); err == nil {
		t.Fatal("advanced evidence cut was accepted")
	}
}

func TestDecisionProtocolReceiptReaderUsesDurableOperationReservation(t *testing.T) {
	want := receipt.Receipt{OperationID: "op-target", SessionID: "session-target"}
	store := &decisionObservationStoreFake{reservation: operation.Reservation{OperationID: "op-target", SessionID: "session-target"}, receipt: want}
	got, found, err := (decisionReceiptSource{store: store}).FindReceiptByOperation(context.Background(), "op-target")
	if err != nil || !found || got.OperationID != want.OperationID || got.SessionID != want.SessionID {
		t.Fatalf("receipt=%#v found=%v err=%v", got, found, err)
	}
}

type decisionFreshSnapshotterFake struct {
	snapshot workspacecore.FastSnapshot
}

func (f decisionFreshSnapshotterFake) ObserveFresh(context.Context, string) workspacecore.FastSnapshot {
	return f.snapshot
}

func TestDecisionProtocolActivationGenerationUsesFreshRepositorySnapshot(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	snapshot, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	source := decisionActivationGenerationSource{workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, snapshots: decisionFreshSnapshotterFake{snapshot: snapshot}}
	got, err := source.CurrentActivationGeneration(context.Background(), string(ws.RepositoryID))
	if err != nil || got != snapshot.Generation {
		t.Fatalf("generation=%q err=%v", got, err)
	}
}

type decisionActivationSnapshotSequence struct {
	snapshots []workspacecore.FastSnapshot
	calls     int
}

func (f *decisionActivationSnapshotSequence) ObserveFresh(context.Context, string) workspacecore.FastSnapshot {
	f.calls++
	if len(f.snapshots) == 0 {
		return workspacecore.FastSnapshot{}
	}
	got := f.snapshots[0]
	if len(f.snapshots) > 1 {
		f.snapshots = f.snapshots[1:]
	}
	return got
}

func TestDecisionProtocolActivationGenerationRetriesOneObservationBudgetExceeded(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	fresh, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	budgetExceeded := fresh
	budgetExceeded.Quality = workspacecore.QualityUnavailable
	budgetExceeded.Generation = ""
	budgetExceeded.DiagnosticCode = "observation_budget_exceeded"
	observer := &decisionActivationSnapshotSequence{snapshots: []workspacecore.FastSnapshot{budgetExceeded, fresh}}
	source := decisionActivationGenerationSource{workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, snapshots: observer}
	got, err := source.CurrentActivationGeneration(context.Background(), string(ws.RepositoryID))
	if err != nil || got != fresh.Generation || observer.calls != 2 {
		t.Fatalf("generation=%q calls=%d err=%v", got, observer.calls, err)
	}
}

func TestDecisionProtocolActivationGenerationRetriesBudgetExceededOnlyOnce(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	budgetExceeded := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Quality: workspacecore.QualityUnavailable, ObservedAt: time.Unix(2, 0).UTC(), DiagnosticCode: "observation_budget_exceeded"}
	observer := &decisionActivationSnapshotSequence{snapshots: []workspacecore.FastSnapshot{budgetExceeded, budgetExceeded, budgetExceeded}}
	source := decisionActivationGenerationSource{workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, snapshots: observer}
	if _, err := source.CurrentActivationGeneration(context.Background(), string(ws.RepositoryID)); err == nil {
		t.Fatal("repeated observation budget failure was accepted")
	}
	if observer.calls != 2 {
		t.Fatalf("fresh observation calls=%d want=2", observer.calls)
	}
}

func TestDecisionProtocolActivationGenerationFailsClosedWithoutUniqueFreshRepositoryWorkspace(t *testing.T) {
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	for name, source := range map[string]decisionActivationGenerationSource{
		"missing":   {workspaces: decisionWorkspaceListFake{}, snapshots: decisionFreshSnapshotterFake{}},
		"ambiguous": {workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws, {SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000002", RepositoryID: ws.RepositoryID, Label: "two", Root: "/repo2", GitDir: "/repo2/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}}}, snapshots: decisionFreshSnapshotterFake{}},
		"not_fresh": {workspaces: decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, snapshots: decisionFreshSnapshotterFake{snapshot: workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Quality: workspacecore.QualityStale}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := source.CurrentActivationGeneration(context.Background(), string(ws.RepositoryID)); err == nil {
				t.Fatal("unsafe activation generation source was accepted")
			}
		})
	}
}

type decisionStructuredSourceFake struct{}

func (decisionStructuredSourceFake) InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error) {
	return structuredapp.InspectResult{}, nil
}

func TestComposeDecisionProtocolRuntimeUsesCanonicalStoreAndExistingReadSides(t *testing.T) {
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	snapshot, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := composeDecisionProtocolRuntime(repository, decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, decisionFreshSnapshotterFake{snapshot: snapshot}, decisionStructuredSourceFake{}, decisionEvidenceSourceFake{})
	if err != nil {
		t.Fatal(err)
	}
	content := decisioncore.PolicyContent{PolicyID: "policy-runtime-compose", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}}
	response, err := runtime.DecisionProtocol(context.Background(), "decision.policy.snapshot", "", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}})
	if err != nil || response.Policy == nil {
		t.Fatalf("policy response=%#v err=%v", response, err)
	}
	stored, found, err := storeadapter.NewDecisionProtocolStore(repository).LoadPolicySnapshot(context.Background(), string(ws.RepositoryID), response.Policy.PolicyDigest)
	if err != nil || !found || stored.PolicyDigest != response.Policy.PolicyDigest {
		t.Fatalf("stored=%#v found=%v err=%v", stored, found, err)
	}
}

type decisionCapabilityBaseActions struct{}

func (decisionCapabilityBaseActions) Start(context.Context, daemonapp.StartRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (decisionCapabilityBaseActions) Poll(context.Context, daemonapp.PollRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (decisionCapabilityBaseActions) Write(context.Context, daemonapp.WriteRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (decisionCapabilityBaseActions) Kill(context.Context, daemonapp.KillRequest) (daemonapp.View, error) {
	return daemonapp.View{}, nil
}
func (decisionCapabilityBaseActions) InspectServer(context.Context) (daemonapp.ServerInfo, error) {
	return daemonapp.ServerInfo{Capabilities: capability.Baseline(capability.Limits{})}, nil
}

func TestDecisionProtocolCapabilityPublishesOnlyAfterRuntimeBinding(t *testing.T) {
	actions := &daemonActions{Actions: decisionCapabilityBaseActions{}}
	before, err := actions.InspectServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before.Capabilities.Features[capability.FeatureDecisionProtocol] != capability.Unavailable || before.Capabilities.DecisionProtocol != nil {
		t.Fatalf("unbound runtime advertised decision protocol: %#v", before.Capabilities.DecisionProtocol)
	}
	actions.decision = &decisionProtocolRuntime{service: &decisionOperationsCapture{}}
	after, err := actions.InspectServer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.Capabilities.Features[capability.FeatureDecisionProtocol] != capability.Available || after.Capabilities.DecisionProtocol == nil {
		t.Fatalf("bound runtime not advertised: %#v", after.Capabilities.DecisionProtocol)
	}
}

func TestBindDecisionProtocolRuntimePublishesOnlySuccessfulComposition(t *testing.T) {
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	snapshot, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	actions := &daemonActions{Actions: decisionCapabilityBaseActions{}}
	workspaces := decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}
	if err := bindDecisionProtocolRuntime(repository, actions, workspaces, decisionFreshSnapshotterFake{snapshot: snapshot}, nil, decisionEvidenceSourceFake{}); err == nil || actions.decision != nil {
		t.Fatalf("failed composition published runtime: decision=%#v err=%v", actions.decision, err)
	}
	if err := bindDecisionProtocolRuntime(repository, actions, workspaces, decisionFreshSnapshotterFake{snapshot: snapshot}, decisionStructuredSourceFake{}, decisionEvidenceSourceFake{}); err != nil {
		t.Fatal(err)
	}
	if actions.decision == nil {
		t.Fatal("successful composition did not publish decision runtime")
	}
	info, err := actions.InspectServer(context.Background())
	if err != nil || info.Capabilities.Features[capability.FeatureDecisionProtocol] != capability.Available {
		t.Fatalf("capability not published after bind: %#v err=%v", info.Capabilities.DecisionProtocol, err)
	}
}

func TestCandidateCommitRemainsUpstreamOfVerificationCompletion(t *testing.T) {
	repository, err := storeadapter.Open(filepath.Join(t.TempDir(), "state-candidate-commit"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ws := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01K00000000000000000000001", RepositoryID: "repo_01K00000000000000000000001", Label: "one", Root: "/repo", GitDir: "/repo/.git", CreatedAt: time.Unix(1, 0).UTC(), LastSeenAt: time.Unix(1, 0).UTC()}
	base := workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: ws.RepositoryID, WorkspaceID: ws.ID, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Ref: "refs/heads/main", Dirty: workspacecore.DirtySummary{Digest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Quality: workspacecore.QualityFresh, ObservedAt: time.Unix(2, 0).UTC()}
	snapshot, err := workspacecore.WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := composeDecisionProtocolRuntime(repository, decisionWorkspaceListFake{workspaces: []workspacecore.Workspace{ws}}, decisionFreshSnapshotterFake{snapshot: snapshot}, decisionStructuredSourceFake{}, decisionEvidenceSourceFake{})
	if err != nil {
		t.Fatal(err)
	}
	content := decisioncore.PolicyContent{PolicyID: "policy-clear", EpisodeKinds: []decisioncore.EpisodeKind{decisioncore.EpisodeDiagnosis}, OverridePolicy: decisioncore.OverridePolicy{Allowed: false}}
	policyResponse, err := runtime.DecisionProtocol(context.Background(), "decision.policy.snapshot", "", ipcadapter.DecisionRequestV1{Policy: &ipcadapter.DecisionPolicySnapshotInputV1{Content: content}})
	if err != nil || policyResponse.Policy == nil {
		t.Fatalf("policy=%#v err=%v", policyResponse, err)
	}
	proposalGeneration := "gen_cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.policy.activate", "", ipcadapter.DecisionRequestV1{ActivationID: "activation-clear", PolicyDigest: policyResponse.Policy.PolicyDigest, ProposalGeneration: proposalGeneration, ExpectedPreviousPolicyDigest: "absent", ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.create", "", ipcadapter.DecisionRequestV1{EpisodeID: "episode-clear", EpisodeKind: decisioncore.EpisodeDiagnosis, ActorRef: "actor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.candidate.create", "", ipcadapter.DecisionRequestV1{EpisodeID: "episode-clear", ActorRef: "actor", Candidate: &ipcadapter.DecisionCandidateInputV1{CandidateID: "candidate-clear", SemanticClaim: "candidate A"}}); err != nil {
		t.Fatal(err)
	}
	projectionResponse, err := runtime.DecisionProtocol(context.Background(), "decision.inspect", "", ipcadapter.DecisionRequestV1{EpisodeID: "episode-clear", CandidateID: "candidate-clear"})
	if err != nil || projectionResponse.Projection == nil || projectionResponse.Projection.Protocol.Gate != decisioncore.GateClear {
		t.Fatalf("projection=%#v err=%v", projectionResponse.Projection, err)
	}
	beforeObservation, err := repository.ObservationHighWatermark(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DecisionProtocol(context.Background(), "decision.selection.commit", "", ipcadapter.DecisionRequestV1{EpisodeID: "episode-clear", CandidateID: "candidate-clear", ActorRef: "actor", ExpectedPolicyDigest: policyResponse.Policy.PolicyDigest, ExpectedProjectionDigest: projectionResponse.Projection.ProjectionDigest, IdempotencyKey: "candidate-clear-commit"}); err != nil {
		t.Fatal(err)
	}
	afterObservation, err := repository.ObservationHighWatermark(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := repository.ListEvidenceCandidates(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if afterObservation != beforeObservation || len(candidates) != 0 {
		t.Fatalf("candidate commit leaked downstream state: observation %d->%d evidence_candidates=%#v", beforeObservation, afterObservation, candidates)
	}
}

type decisionInspectProjectionCapture struct {
	decisionProtocolOperations
	inspectCalls int
	projectCalls int
}

func (f *decisionInspectProjectionCapture) Inspect(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProjection, error) {
	f.inspectCalls++
	return decisioncore.DecisionProjection{ProjectionDigest: "proj_base"}, nil
}

func (f *decisionInspectProjectionCapture) Project(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProjection, error) {
	f.projectCalls++
	return decisioncore.DecisionProjection{ProjectionDigest: "proj_evaluated", Protocol: decisioncore.DecisionProtocolEvaluation{Gate: decisioncore.GateClear}}, nil
}

func TestDecisionInspectReturnsCommitEligibleEvaluatedProjection(t *testing.T) {
	ops := &decisionInspectProjectionCapture{}
	runtime := &decisionProtocolRuntime{service: ops}
	response, err := runtime.DecisionProtocol(context.Background(), "decision.inspect", "", ipcadapter.DecisionRequestV1{EpisodeID: "episode-inspect", CandidateID: "candidate-inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if ops.projectCalls != 1 || ops.inspectCalls != 0 || response.Projection == nil || response.Projection.Protocol.Gate != decisioncore.GateClear || response.Projection.ProjectionDigest != "proj_evaluated" {
		t.Fatalf("inspect response=%#v inspect_calls=%d project_calls=%d", response.Projection, ops.inspectCalls, ops.projectCalls)
	}
}
