package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/session"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestJestStructuredResultsPublicIPC(t *testing.T) {
	restoreJasmine := unsetEnvironmentForTest(t, structuredapp.JestJasmineEnvironment)
	defer restoreJasmine()
	// Agent/tooling environment is deliberately irrelevant to qualification.
	t.Setenv("SHELLBEAM_AGENT", "acceptance-probe")
	t.Setenv("CLAUDECODE", "1")

	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspaceRoot := initWorkspaceCLIRepo(t)
	if err := os.Mkdir(filepath.Join(workspaceRoot, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workspaceRoot, "public_jest.test.js"), `test('producer source is not trusted by shim', () => { throw new Error('not executed') })`)

	binDir := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "jest-json", "jest-30.4.2", "pass.json"))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	patchedFixture := filepath.Join(t.TempDir(), "pass.json")
	patched := strings.ReplaceAll(string(frozen), "/private/jest-fixture", filepath.ToSlash(workspaceRoot))
	if err := os.WriteFile(patchedFixture, []byte(patched), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELLBEAM_JEST_FIXTURE", patchedFixture)
	shim := `#!/bin/sh
set -eu
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--outputFile" ]; then out="$arg"; previous=""; continue; fi
  case "$arg" in
    --outputFile=*) out="${arg#*=}" ;;
    --outputFile) previous="--outputFile" ;;
  esac
done
[ -n "$out" ]
cp "$SHELLBEAM_JEST_FIXTURE" "$out"
printf 'jest shim: child exits nonzero while frozen JSON says success true\n'
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "jest"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	store := openA1Store(t, stateDir)
	workspace := saveStructuredCaptureWorkspace(t, store, workspaceRoot)
	request := ipcadapter.RequestV2{
		Action: "start", OperationID: "jest-public-ipc", WorkspaceID: string(workspace.ID), CWD: ".", StructuredAdapter: structuredapp.JestJSONAdapterID,
		Argv: []string{"jest", "--runInBand", "--json", "--outputFile=reports/jest.json", "public_jest.test.js"},
	}
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	first := callA1Terminal(t, client, request)
	if first.Receipt == nil || first.Receipt.Outcome != session.Failure {
		t.Fatalf("child receipt must remain failure truth: %#v", first.Receipt)
	}
	firstStructured := waitStructuredTerminal(t, client, request.OperationID)
	assertJestPublicIPCStructuredPass(t, firstStructured)
	stop()

	client, stop = startExecutionObservationDaemon(t, stateDir, runtimeDir)
	t.Cleanup(stop)
	second := callA1Terminal(t, client, request)
	if second.Receipt == nil || first.Receipt == nil || second.Receipt.SessionID != first.Receipt.SessionID || second.Receipt.Outcome != session.Failure {
		t.Fatalf("restart replay first=%#v second=%#v", first.Receipt, second.Receipt)
	}
	secondStructured := waitStructuredTerminal(t, client, request.OperationID)
	if secondStructured.DerivationKey != firstStructured.DerivationKey || secondStructured.SourceKind != core.StructuredInputArtifactBlob || secondStructured.SourceState != structuredapp.InputSourceRetained {
		t.Fatalf("restart structured first=%#v second=%#v", firstStructured, secondStructured)
	}

	assertUnqualifiedJestPublicIPCExecutesWithoutStructuredResult(t, client, request)
}

func TestJestStructuredResultsPublicIPCRealProducer(t *testing.T) {
	binDir := os.Getenv("SHELLBEAM_JEST_REAL_BIN_DIR")
	if binDir == "" {
		t.Skip("set SHELLBEAM_JEST_REAL_BIN_DIR from deliberate qualification install")
	}
	restoreJasmine := unsetEnvironmentForTest(t, structuredapp.JestJasmineEnvironment)
	defer restoreJasmine()
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspaceRoot := initWorkspaceCLIRepo(t)
	if err := os.Mkdir(filepath.Join(workspaceRoot, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workspaceRoot, "jest.config.cjs"), `module.exports = {}`)
	writeTestFile(t, filepath.Join(workspaceRoot, "real_jest.test.js"), `test('real producer pass', () => { expect(2 + 2).toBe(4) })`)

	store := openA1Store(t, stateDir)
	workspace := saveStructuredCaptureWorkspace(t, store, workspaceRoot)
	request := ipcadapter.RequestV2{
		Action: "start", OperationID: "jest-public-ipc-real", WorkspaceID: string(workspace.ID), CWD: ".", StructuredAdapter: structuredapp.JestJSONAdapterID,
		Argv: []string{"jest", "--runInBand", "--json", "--outputFile=reports/jest.json", "real_jest.test.js"},
	}
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	defer stop()
	terminal := callA1Terminal(t, client, request)
	if terminal.Receipt == nil || terminal.Receipt.Outcome != session.Success {
		t.Fatalf("real Jest receipt=%#v", terminal.Receipt)
	}
	assertJestPublicIPCStructuredPass(t, waitStructuredTerminal(t, client, request.OperationID))

	negative := request
	negative.OperationID = "jest-public-ipc-real-unqualified"
	negative.StructuredAdapter = ""
	negative.Argv = []string{"jest", "--runInBand", "--outputFile=reports/unqualified.json", "real_jest.test.js"}
	negativeTerminal := callA1Terminal(t, client, negative)
	if negativeTerminal.Receipt == nil || negativeTerminal.Receipt.Outcome != session.Success {
		t.Fatalf("unqualified child execution changed: %#v", negativeTerminal.Receipt)
	}
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: negative.OperationID + "-structured", Action: "inspect.structured", OperationID: negative.OperationID, MaxRecords: structuredapp.MaxListRecords,
	})
	if err != nil || !response.OK || response.Structured == nil || response.Structured.Status != structuredapp.InspectNotFound {
		t.Fatalf("unqualified real Jest created structured result: response=%#v err=%v", response, err)
	}
}

func assertUnqualifiedJestPublicIPCExecutesWithoutStructuredResult(t *testing.T, client *ipcadapter.Client, qualified ipcadapter.RequestV2) {
	t.Helper()
	negative := qualified
	negative.OperationID = "jest-public-ipc-unqualified"
	negative.StructuredAdapter = ""
	negative.Argv = []string{"jest", "--runInBand", "--outputFile=reports/unqualified.json", "public_jest.test.js"}
	terminal := callA1Terminal(t, client, negative)
	if terminal.Receipt == nil || terminal.Receipt.Outcome != session.Failure {
		t.Fatalf("unqualified child truth changed: %#v", terminal.Receipt)
	}
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: negative.OperationID + "-structured", Action: "inspect.structured", OperationID: negative.OperationID, MaxRecords: structuredapp.MaxListRecords,
	})
	if err != nil || !response.OK || response.Structured == nil || response.Structured.Status != structuredapp.InspectNotFound {
		t.Fatalf("unqualified Jest created structured authority: response=%#v err=%v", response, err)
	}
}

func assertJestPublicIPCStructuredPass(t *testing.T, got structuredapp.InspectResult) {
	t.Helper()
	if got.Status != structuredapp.InspectTerminal || got.Producer == nil || got.Producer.AdapterID != structuredapp.JestJSONAdapterID || got.ParseOutcome != core.ParseComplete || got.Completeness != core.CompletenessComplete || got.SourceKind != core.StructuredInputArtifactBlob || got.SourceState != structuredapp.InputSourceRetained || got.SemanticsCoverage == nil || got.SemanticsCoverage.Namespace != "jest" {
		t.Fatalf("structured=%#v", got)
	}
	pass := false
	for _, record := range got.Records {
		if record.Authority == core.AuthorityMechanical && record.TestCase != nil && record.TestCase.Status == core.TestPassed {
			pass = true
		}
		if record.TestCase != nil && record.TestCase.Status == core.TestError {
			t.Fatalf("Jest adapter invented core error: %#v", record.TestCase)
		}
	}
	if !pass {
		t.Fatalf("mechanical pass record missing: %#v", got.Records)
	}
}

func TestJestFrozenFixtureDoesNotLeakGeneratorRoot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", "jest-json", "jest-30.4.2", "pass.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/tmp/shellbeam-jest") || strings.Contains(string(data), "/Users/") {
		t.Fatalf("fixture leaked generator root")
	}
}
