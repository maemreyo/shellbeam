package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestDaemonCatalogAdvertisesBoundedEvidenceLedger(t *testing.T) {
	catalog := daemonCatalog(capability.Limits{})
	if catalog.Features[capability.FeatureEvidenceLedger] != capability.Available || catalog.Features[capability.FeatureExpectedOutputs] != capability.Available {
		t.Fatalf("evidence features=%#v", catalog.Features)
	}
	if len(catalog.EvidenceSchemaVersions) != 1 || catalog.EvidenceSchemaVersions[0] != coreevidence.SchemaVersion || len(catalog.ArtifactObservationSchemaVersions) != 1 || catalog.ArtifactObservationSchemaVersions[0] != coreevidence.ArtifactSchemaVersion {
		t.Fatalf("evidence schemas=%v artifact schemas=%v", catalog.EvidenceSchemaVersions, catalog.ArtifactObservationSchemaVersions)
	}
	limits := catalog.Limits
	if limits.EvidenceInspectRecords != coreevidence.MaxInspectRecords || limits.EvidenceExpectedOutputs != coreevidence.MaxExpectedOutputs ||
		limits.EvidenceArtifactMetadataBytes != coreevidence.MaxArtifactMetadataBytes || limits.EvidenceArtifactDigestBytes != coreevidence.MaxArtifactDigestBytes ||
		limits.EvidenceTreeEntries != coreevidence.MaxTreeEntries || limits.EvidenceCursorBytes != coreevidence.MaxCursorBytes {
		t.Fatalf("evidence limits=%#v", limits)
	}
}

func TestEvidenceLedgerRealDaemonRawArtifactsAndEvents(t *testing.T) {
	root, workspace, stateDir, runtimeDir := prepareEvidenceWorkspace(t)
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()
	assertEvidenceServerCapability(t, client)

	result := callA1Terminal(t, client, rawEvidenceStart("a24-raw-sha", workspace, "mkdir -p out && printf raw-sha > out/raw.txt", coreevidence.VerificationBuild,
		project.Output{Path: "out/raw.txt", Kind: "file", Digest: "sha256", Required: true}))
	assertA1ChildSuccess(t, result)
	view := waitEvidenceIPC(t, client, "a24-raw-sha")
	want := sha256.Sum256([]byte("raw-sha"))
	if view.Record.Result != coreevidence.ResultPass || len(view.Record.Artifacts) != 1 || view.Record.Artifacts[0].Digest != hex.EncodeToString(want[:]) {
		t.Fatalf("raw SHA evidence=%#v", view.Record)
	}
	assertEvidenceSourceNotExact(t, view)
	assertEvidenceEvents(t, client, "a24-raw-sha", 1)
	if _, err := os.Stat(filepath.Join(root, "out", "raw.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceLedgerRealDaemonMissingOptionalTreeSymlinkAndNoTax(t *testing.T) {
	_, workspace, stateDir, runtimeDir := prepareEvidenceWorkspace(t)
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()

	required := callA1Terminal(t, client, rawEvidenceStart("a24-required-missing", workspace, "true", coreevidence.VerificationBuild,
		project.Output{Path: "out/required.bin", Kind: "file", Required: true}))
	assertA1ChildSuccess(t, required)
	requiredEvidence := waitEvidenceIPC(t, client, "a24-required-missing")
	if requiredEvidence.Record.Result != coreevidence.ResultFail || required.Receipt == nil || required.Receipt.Outcome != session.Success {
		t.Fatalf("required evidence=%#v receipt=%#v", requiredEvidence.Record, required.Receipt)
	}

	optional := callA1Terminal(t, client, rawEvidenceStart("a24-optional-missing", workspace, "true", coreevidence.VerificationTest,
		project.Output{Path: "out/optional.bin", Kind: "file", Required: false}))
	assertA1ChildSuccess(t, optional)
	if got := waitEvidenceIPC(t, client, "a24-optional-missing"); got.Record.Result != coreevidence.ResultPass || got.Record.Artifacts[0].Status != coreevidence.ArtifactMissing {
		t.Fatalf("optional evidence=%#v", got.Record)
	}

	assertEvidenceTreeAndSymlink(t, client, workspace)
	assertEvidenceNoTax(t, client, stateDir)
}

func TestEvidenceLedgerRestartPersistsRecord(t *testing.T) {
	_, workspace, stateDir, runtimeDir := prepareEvidenceWorkspace(t)
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	result := callA1Terminal(t, client, rawEvidenceStart("a24-restart", workspace, "mkdir -p out && printf durable > out/restart.txt", coreevidence.VerificationTest,
		project.Output{Path: "out/restart.txt", Kind: "file", Digest: "sha256", Required: true}))
	assertA1ChildSuccess(t, result)
	before := waitEvidenceIPC(t, client, "a24-restart")
	stop()

	client, stop = startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()
	after := inspectEvidenceIPC(t, client, "a24-restart")
	if after.Status != evidenceapp.InspectAvailable || len(after.Records) != 1 || after.Records[0].Record.EvidenceID != before.Record.EvidenceID {
		t.Fatalf("restart evidence before=%#v after=%#v", before.Record, after)
	}
}

func TestEvidenceLedgerTypedBindingRetryIgnoresManifestMutation(t *testing.T) {
	root, workspace, stateDir, runtimeDir := prepareEvidenceWorkspace(t)
	manifestPath := writeEvidenceManifest(t, root, "out/typed.txt")
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()
	request := ipcadapter.RequestV2{Action: "start", OperationID: "a24-typed", WorkspaceID: string(workspace.ID), ProjectCommandID: "verify", Params: map[string]string{"tag": "acceptance"}}
	first := callA1Terminal(t, client, request)
	assertA1ChildSuccess(t, first)
	if first.Receipt == nil || first.Receipt.ProjectCommand == nil || len(first.Receipt.ProjectCommand.ExpectedOutputs) != 1 || first.Receipt.ProjectCommand.ExpectedOutputs[0].Path != "out/typed.txt" {
		t.Fatalf("typed receipt=%#v", first.Receipt)
	}
	before := waitEvidenceIPC(t, client, "a24-typed")
	writeTestFile(t, manifestPath, evidenceManifest("out/mutated.txt"))
	replayed := callA1Terminal(t, client, request)
	if replayed.Operation.SessionID != first.Operation.SessionID || replayed.Receipt == nil || replayed.Receipt.ProjectCommand.ExpectedOutputs[0].Path != "out/typed.txt" {
		t.Fatalf("typed replay reread mutable manifest: first=%#v replay=%#v", first.Receipt, replayed.Receipt)
	}
	after := inspectEvidenceIPC(t, client, "a24-typed")
	if after.Status != evidenceapp.InspectAvailable || len(after.Records) != 1 || after.Records[0].Record.EvidenceID != before.Record.EvidenceID {
		t.Fatalf("typed evidence changed across retry: before=%#v after=%#v", before.Record, after)
	}
}

func prepareEvidenceWorkspace(t *testing.T) (string, workspacecore.Workspace, string, string) {
	t.Helper()
	root := initWorkspaceCLIRepo(t)
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspace := attachA5AcceptanceWorkspace(t, root, stateDir)
	return root, workspace, stateDir, runtimeDir
}

func rawEvidenceStart(operationID string, workspace workspacecore.Workspace, command string, kind coreevidence.VerificationKind, outputs ...project.Output) ipcadapter.RequestV2 {
	return ipcadapter.RequestV2{Action: "start", OperationID: operationID, WorkspaceID: string(workspace.ID), CWD: ".", Command: command,
		Evidence: &coreevidence.Contract{VerificationKind: kind, SourceScope: coreevidence.SourceScopeFull, ExpectedOutputs: outputs}}
}

func waitEvidenceIPC(t *testing.T, client *ipcadapter.Client, operationID string) evidenceapp.InspectRecord {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		result := inspectEvidenceIPC(t, client, operationID)
		if result.Status == evidenceapp.InspectAvailable && len(result.Records) == 1 {
			return result.Records[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("evidence %s did not become available", operationID)
	return evidenceapp.InspectRecord{}
}

func inspectEvidenceIPC(t *testing.T, client *ipcadapter.Client, operationID string) evidenceapp.InspectResult {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: operationID + "-evidence", Action: "inspect.evidence", OperationID: operationID, MaxRecords: 1})
	if err != nil || !response.OK || response.Evidence == nil {
		t.Fatalf("inspect evidence %s response=%#v err=%v", operationID, response, err)
	}
	return *response.Evidence
}

func assertEvidenceSourceNotExact(t *testing.T, view evidenceapp.InspectRecord) {
	t.Helper()
	if view.CurrentSource.Quality == coreevidence.SourceQualityExact || view.Validity.SourceMatch == coreevidence.SourceMatchExact {
		t.Fatalf("fast-only source overclaimed exact: %#v", view)
	}
	if view.CurrentSource.Quality == coreevidence.SourceQualityFast && view.Validity.SourceMatch != coreevidence.SourceMatchUnknown &&
		(view.Validity.SourceMatch != coreevidence.SourceMatchFast || view.Validity.Freshness != coreevidence.FreshnessCurrent) {
		t.Fatalf("fast source validity=%#v", view)
	}
}

func assertEvidenceEvents(t *testing.T, client *ipcadapter.Client, operationID string, artifacts int) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: operationID + "-events", Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: operationID}, MaxEvents: 32})
		if err != nil {
			t.Fatal(err)
		}
		observed, recorded := 0, 0
		if response.OK && response.Events != nil {
			for _, event := range response.Events.Events {
				if event.Kind == observation.EventArtifactObserved {
					observed++
				}
				if event.Kind == observation.EventEvidenceRecorded {
					recorded++
				}
			}
		}
		if observed == artifacts && recorded == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("evidence events %s did not materialize", operationID)
}

func assertEvidenceTreeAndSymlink(t *testing.T, client *ipcadapter.Client, workspace workspacecore.Workspace) {
	t.Helper()
	terminal := callA1Terminal(t, client, rawEvidenceStart("a24-tree-link", workspace, "mkdir -p out/tree && printf alpha > out/tree/a.txt && ln -s tree/a.txt out/link", coreevidence.VerificationGenerate,
		project.Output{Path: "out/tree", Kind: "directory", Digest: "tree-sha256", Required: true}, project.Output{Path: "out/link", Kind: "symlink", Required: true}))
	assertA1ChildSuccess(t, terminal)
	view := waitEvidenceIPC(t, client, "a24-tree-link")
	if view.Record.Result != coreevidence.ResultPass || len(view.Record.Artifacts) != 2 || len(view.Record.Artifacts[0].Digest) != 64 || view.Record.Artifacts[1].LinkText != "tree/a.txt" {
		t.Fatalf("tree/symlink evidence=%#v", view.Record)
	}
}

func assertEvidenceNoTax(t *testing.T, client *ipcadapter.Client, stateDir string) {
	t.Helper()
	beforeCandidates := evidenceDirEntries(t, filepath.Join(stateDir, "evidence", "candidates"))
	beforeRecords := evidenceDirEntries(t, filepath.Join(stateDir, "evidence", "records"))
	plain := callA1Terminal(t, client, ipcadapter.RequestV2{Action: "start", OperationID: "a24-no-tax", CWD: "/tmp", Command: "true"})
	assertA1ChildSuccess(t, plain)
	result := inspectEvidenceIPC(t, client, "a24-no-tax")
	if result.Status != evidenceapp.InspectNeverRun || evidenceDirEntries(t, filepath.Join(stateDir, "evidence", "candidates")) != beforeCandidates || evidenceDirEntries(t, filepath.Join(stateDir, "evidence", "records")) != beforeRecords {
		t.Fatalf("ordinary command incurred evidence state work: inspect=%#v", result)
	}
}

func evidenceDirEntries(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func assertEvidenceServerCapability(t *testing.T, client *ipcadapter.Client) {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "a24-server", Action: "inspect.server"})
	if err != nil || !response.OK || response.Server == nil || response.Server.Features[capability.FeatureEvidenceLedger] != capability.Available || response.Server.Features[capability.FeatureExpectedOutputs] != capability.Available {
		t.Fatalf("server evidence capability=%#v err=%v", response, err)
	}
}

func writeEvidenceManifest(t *testing.T, root, output string) string {
	t.Helper()
	path := filepath.Join(root, ".shellbeam", "project.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, evidenceManifest(output))
	return path
}

func evidenceManifest(output string) string {
	return fmt.Sprintf(`schema_version = 2
[commands.verify]
argv = ["/bin/sh", "-c", "mkdir -p out && printf typed > out/typed.txt", "{tag}"]
cwd = "."
kind = "test"
source_scope = "full"
[commands.verify.params.tag]
kind = "string"
required = true
[[commands.verify.expected_outputs]]
path = %q
kind = "file"
digest = "sha256"
required = true
`, output)
}
