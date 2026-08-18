package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
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

func TestExecutionEvidenceRuntimeWiresRealProvenScopeRemeasurement(t *testing.T) {
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "scope-state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 16 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("module example\n")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceID := workspacecore.WorkspaceID("ws_01K00000000000000000000000")
	repositoryID := workspacecore.RepositoryID("repo_01K00000000000000000000000")
	now := time.Now().UTC()
	if err := store.SaveWorkspace(context.Background(), workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: workspaceID, RepositoryID: repositoryID, Label: "scope", Root: root, GitDir: filepath.Join(root, ".git"), CreatedAt: now.Add(-time.Hour), LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	fileSum := sha256.Sum256(content)
	oldGeneration := "gen_" + strings.Repeat("d", 64)
	manifest := hermetic.CaptureManifest{SchemaVersion: hermetic.CaptureManifestSchemaVersion, WorkspaceID: workspaceID, SourceGeneration: oldGeneration, Selectors: []string{"go.mod"}, Entries: []hermetic.CaptureEntry{{Path: "go.mod", Size: int64(len(content)), SHA256: hex.EncodeToString(fileSum[:])}}, TotalBytes: int64(len(content))}
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	contentDigest, err := manifest.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	scope := &hermetic.ProvenInputScope{SchemaVersion: 1, RepoInputs: []string{"go.mod"}, CaptureManifestSHA256: manifestDigest, CaptureContentSHA256: contentDigest, Provider: hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: strings.Repeat("a", 64), RuntimeManifestSHA256: strings.Repeat("b", 64)}, Toolchain: hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: strings.Repeat("c", 64)}, Environment: hermetic.EnvironmentFixedAllowlist, Stdin: hermetic.StdinClosed, Network: hermetic.NetworkOff, AmbientInputs: []hermetic.AmbientInputClass{hermetic.AmbientClock, hermetic.AmbientRandomness}}
	record := core.Record{SchemaVersion: core.SchemaVersion, EvidenceID: "ev_" + strings.Repeat("1", 64), OperationID: "cmd-scope-op", SessionID: "cmd-scope-session", WorkspaceID: string(workspaceID), VerificationKind: core.VerificationTest, ContractDigest: strings.Repeat("2", 64), ReceiptDigest: strings.Repeat("3", 64), Terminal: core.TerminalResult{Authoritative: true, Outcome: session.Success}, Result: core.ResultPass, Source: core.SourceBinding{RepositoryID: string(repositoryID), WorkspaceID: string(workspaceID), PostGeneration: oldGeneration, ObservationQuality: core.SourceQualityFast}, ProvenInputScope: scope, CompletedAt: now}
	if _, err := store.PutEvidenceRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	changedGeneration := "gen_" + strings.Repeat("e", 64)
	observer := &evidenceFreshObserverFake{snapshot: workspacecore.FastSnapshot{SchemaVersion: workspacecore.SnapshotSchemaVersion, RepositoryID: repositoryID, WorkspaceID: workspaceID, Generation: changedGeneration, Quality: workspacecore.QualityFresh, UpstreamQuality: workspacecore.QualityFresh, ObservedAt: now}}
	runtime, err := newExecutionEvidenceRuntime(store, observer)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown(context.Background())
	got, err := runtime.inspector.Inspect(context.Background(), evidenceapp.InspectRequest{Filter: evidenceapp.InspectFilter{OperationID: record.OperationID}, MaxRecords: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Records) != 1 || got.Records[0].Validity.SourceMatch != core.SourceMatchProvenScope || got.Records[0].Validity.Freshness != core.FreshnessCurrent {
		t.Fatalf("production evidence runtime did not wire proven scope: %#v", got)
	}
}
