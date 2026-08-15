package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestExecutionEvidenceRuntimeRecoversDurableTerminalCandidate(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	contract := &core.Contract{VerificationKind: core.VerificationTest}
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: "runtime-recover", SessionID: "runtime-recover-session", RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/", Shell: "/bin/sh", DaemonIncarnation: "old", Evidence: contract, CreatedAt: now}
	if _, created, result := store.ReserveOperation(context.Background(), reservation); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	zero := 0
	rec := receipt.Receipt{SchemaVersion: 2, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, ObservationBindingFingerprint: reservation.ObservationBindingFingerprint, DaemonIncarnation: "old", ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: "/", State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}, Evidence: contract}
	if result := store.PublishTerminal(context.Background(), rec); result.Err != nil {
		t.Fatal(result.Err)
	}

	runtime, err := newExecutionEvidenceRuntime(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.startRecovery(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if record, found, findErr := store.FindEvidenceByOperation(context.Background(), reservation.OperationID); findErr != nil {
			t.Fatal(findErr)
		} else if found {
			if record.Result != core.ResultPass || record.VerificationKind != core.VerificationTest {
				t.Fatalf("record=%#v", record)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, found, err := store.FindEvidenceByOperation(context.Background(), reservation.OperationID); err != nil || !found {
		t.Fatalf("recovered found=%v err=%v", found, err)
	}
	if err := runtime.shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	candidates, err := store.ListEvidenceCandidates(context.Background(), 8)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("candidates=%#v err=%v", candidates, err)
	}
}

func TestEvidenceWorkerProxyRejectsSchedulingBeforeBind(t *testing.T) {
	proxy := &evidenceWorkerProxy{}
	if err := proxy.ScheduleTerminal(context.Background(), receipt.Receipt{}); err == nil {
		t.Fatal("unbound evidence proxy accepted scheduling")
	}
}

type evidenceWorkspaceRegistryFake struct {
	workspaces []workspacecore.Workspace
	err        error
}

func (r evidenceWorkspaceRegistryFake) ListWorkspaces(context.Context) ([]workspacecore.Workspace, error) {
	return append([]workspacecore.Workspace(nil), r.workspaces...), r.err
}

type evidenceFreshObserverFake struct {
	snapshot workspacecore.FastSnapshot
	calls    int
}

func (o *evidenceFreshObserverFake) ObserveFresh(context.Context, string) workspacecore.FastSnapshot {
	o.calls++
	return o.snapshot
}

func TestEvidenceCurrentStateProviderUsesOnlyFreshFastGeneration(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	workspaceID := workspacecore.WorkspaceID("ws_01K00000000000000000000000")
	repositoryID := workspacecore.RepositoryID("repo_01K00000000000000000000000")
	registry := evidenceWorkspaceRegistryFake{workspaces: []workspacecore.Workspace{{SchemaVersion: workspacecore.SchemaVersion, ID: workspaceID, RepositoryID: repositoryID, Label: "evidence", Root: root, GitDir: filepath.Join(root, ".git"), CreatedAt: now.Add(-time.Hour), LastSeenAt: now}}}
	generation := "gen_" + strings.Repeat("d", 64)
	observer := &evidenceFreshObserverFake{snapshot: workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: repositoryID, WorkspaceID: workspaceID, Generation: generation, Quality: workspacecore.QualityFresh, UpstreamQuality: workspacecore.QualityFresh, ObservedAt: now}}
	provider := newEvidenceCurrentStateProvider(registry, observer)
	state := provider.ObserveCurrent(context.Background(), core.Record{WorkspaceID: string(workspaceID)})
	if state.Source.WorkspaceID != string(workspaceID) || state.Source.Generation != generation || state.Source.Quality != core.SourceQualityFast || state.Source.SourceContentDigest != "" || state.Source.VCSStateDigest != "" || state.WorkspaceRoot != root || observer.calls != 1 {
		t.Fatalf("state=%#v calls=%d", state, observer.calls)
	}

	observer.snapshot.Quality = workspacecore.QualityUnavailable
	observer.snapshot.Generation = ""
	state = provider.ObserveCurrent(context.Background(), core.Record{WorkspaceID: string(workspaceID)})
	if state.Source.Quality != core.SourceQualityUnknown || state.Source.Generation != "" || state.Source.SourceContentDigest != "" || state.WorkspaceRoot != root {
		t.Fatalf("unavailable state=%#v", state)
	}
}

func TestDaemonActionsInspectEvidenceDelegatesOnlyAfterRuntimeBind(t *testing.T) {
	actions := &daemonActions{}
	if _, err := actions.InspectEvidence(context.Background(), evidenceapp.InspectRequest{}); err == nil {
		t.Fatal("unbound evidence inspection accepted")
	}
	repo := &inspectRepoForDaemonAction{}
	codec, err := evidenceapp.NewCursorCodec(observationCursorKeyForEvidenceTest())
	if err != nil {
		t.Fatal(err)
	}
	actions.evidence = evidenceapp.NewInspector(repo, nil, nil, codec)
	got, err := actions.InspectEvidence(context.Background(), evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{OperationID: "never-run"}, MaxRecords: 1})
	if err != nil || got.Status != evidenceapp.InspectNeverRun {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

type inspectRepoForDaemonAction struct{}

func (*inspectRepoForDaemonAction) ObservationHighWatermark(context.Context) (observation.ChangeSeq, error) {
	return 0, nil
}
func (*inspectRepoForDaemonAction) ListEvidenceIndexObligations(context.Context, observation.ChangeSeq, observation.ChangeSeq, int) ([]observation.ObservationObligation, error) {
	return nil, nil
}
func (*inspectRepoForDaemonAction) FindEvidenceByID(context.Context, string) (core.Record, bool, error) {
	return core.Record{}, false, nil
}
func (*inspectRepoForDaemonAction) FindEvidenceByOperation(context.Context, operation.ID) (core.Record, bool, error) {
	return core.Record{}, false, nil
}
func (*inspectRepoForDaemonAction) LoadEvidenceValidity(context.Context, string) (core.ValidityObservation, bool, error) {
	return core.ValidityObservation{}, false, nil
}
func (*inspectRepoForDaemonAction) PutEvidenceValidity(context.Context, core.ValidityObservation) (bool, error) {
	return false, nil
}

func observationCursorKeyForEvidenceTest() observation.CursorKeyMaterial {
	return observation.CursorKeyMaterial{StateRootEpoch: "epoch_11111111111111111111111111111111", Generation: "key_22222222222222222222222222222222", Secret: []byte(strings.Repeat("k", 32))}
}
