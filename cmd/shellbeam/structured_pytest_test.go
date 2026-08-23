package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	pytestjunit "github.com/maemreyo/shellbeam/internal/adapter/structured/pytestjunit"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type frozenPytestReader struct {
	data []byte
	ref  core.StructuredInputRef
}

func frozenPytestInput(t *testing.T, path string) (*frozenPytestReader, core.StructuredInputRef) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	blob := core.ArtifactBlobRef{
		SchemaVersion: core.ArtifactBlobSchemaVersion, BlobID: "abl_" + strings.Repeat("a", 64), OperationID: "pytest-frozen-op", SessionID: "pytest-frozen-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q", DeclaredPath: "reports/junit.xml", NormalizedWorkspacePath: "reports/junit.xml",
		SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)), TerminalCut: core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: 2, ReceiptDigest: strings.Repeat("b", 64)}, ObservationCut: core.ObservationCutV1{SchemaVersion: core.ObservationCutSchemaVersion, Digest: strings.Repeat("c", 64)},
	}
	ref := core.ArtifactInputRef(blob)
	return &frozenPytestReader{data: data, ref: ref}, ref
}

func (r *frozenPytestReader) ReadInputRange(_ context.Context, ref core.StructuredInputRef, offset int64, max int) ([]byte, error) {
	if ref.Validate() != nil || ref.ArtifactBlob == nil || ref.ArtifactBlob.BlobID != r.ref.ArtifactBlob.BlobID || offset < 0 || max < 0 || offset > int64(len(r.data)) {
		return nil, errors.New("invalid frozen fixture range")
	}
	end := offset + int64(max)
	if end > int64(len(r.data)) {
		end = int64(len(r.data))
	}
	return append([]byte(nil), r.data[offset:end]...), nil
}
func (r *frozenPytestReader) DescribeInput(context.Context, core.StructuredInputRef) (structuredapp.InputContext, error) {
	return structuredapp.InputContext{OperationID: "pytest-frozen-op", DerivationKey: strings.Repeat("d", 64)}, nil
}

func frozenPytestLimits() structuredapp.Limits {
	return structuredapp.Limits{MaxBytes: 2 << 20, MaxRecords: 2048, MaxStringBytes: 128 << 10, MaxDepth: 64, MaxDuration: 2 * time.Second}
}
func frozenFixturePath(version, name string) string {
	return filepath.Join("..", "..", "tests", "fixtures", "pytest-junit", "pytest-"+version, name+".xml")
}

func parseFrozenPytest(t *testing.T, version, name string) structuredapp.ParseResult {
	t.Helper()
	reader, ref := frozenPytestInput(t, frozenFixturePath(version, name))
	result, err := (pytestjunit.Adapter{}).Parse(context.Background(), ref, reader, frozenPytestLimits())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func frozenCases(result structuredapp.ParseResult) []core.Record {
	var out []core.Record
	for _, r := range result.Records {
		if r.TestCase != nil {
			out = append(out, r)
		}
	}
	return out
}
func frozenSuites(result structuredapp.ParseResult) []core.Record {
	var out []core.Record
	for _, r := range result.Records {
		if r.TestSuite != nil {
			out = append(out, r)
		}
	}
	return out
}

func TestFrozenPytestQualificationFixtures(t *testing.T) {
	for _, version := range []string{"8.4.2", "9.1.1"} {
		t.Run(version+"/outcomes", func(t *testing.T) {
			result := parseFrozenPytest(t, version, "outcomes")
			cases := frozenCases(result)
			suites := frozenSuites(result)
			want := []core.TestStatus{core.TestPassed, core.TestFailed, core.TestSkipped, core.TestSkipped, core.TestPassed, core.TestFailed, core.TestError}
			if result.Outcome != core.ParseComplete || result.Completeness != core.CompletenessComplete || len(cases) != len(want) || len(suites) != 1 {
				t.Fatalf("result=%#v", result)
			}
			for i, status := range want {
				if cases[i].TestCase.Status != status {
					t.Fatalf("case[%d]=%#v", i, cases[i].TestCase)
				}
			}
			if cases[2].TestCase.ProducerDisposition == nil || cases[2].TestCase.ProducerDisposition.Code != "pytest:skip" || cases[3].TestCase.ProducerDisposition == nil || cases[3].TestCase.ProducerDisposition.Code != "pytest:xfail" {
				t.Fatalf("skip/xfail dispositions=%#v %#v", cases[2].TestCase.ProducerDisposition, cases[3].TestCase.ProducerDisposition)
			}
			for _, i := range []int{0, 1, 4, 5, 6} {
				if cases[i].TestCase.ProducerDisposition != nil {
					t.Fatalf("case[%d] invented disposition=%#v", i, cases[i].TestCase.ProducerDisposition)
				}
			}
			coverage := result.SemanticsCoverage
			if coverage == nil || !reflect.DeepEqual(coverage.Unavailable, []string{"pytest:error_phase", "pytest:xfail_execution_state", "pytest:xpass_exact"}) {
				t.Fatalf("coverage=%#v", coverage)
			}
			encoded, _ := json.Marshal(result.Records)
			for _, forbidden := range []string{"XPASS(strict)", "setup boom", "strict xpass reason"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("producer prose leaked: %s", forbidden)
				}
			}
		})
		t.Run(version+"/duplicate-entry", func(t *testing.T) {
			result := parseFrozenPytest(t, version, "duplicate-entry")
			cases := frozenCases(result)
			suites := frozenSuites(result)
			if result.Outcome != core.ParseComplete || len(cases) != 2 || len(suites) != 1 {
				t.Fatalf("result=%#v", result)
			}
			if cases[0].TestCase.Status != core.TestFailed || cases[1].TestCase.Status != core.TestError || cases[0].TestCase.ProducerAddress.Name != cases[1].TestCase.ProducerAddress.Name || cases[0].RecordID == cases[1].RecordID {
				t.Fatalf("duplicate entries=%#v", cases)
			}
			if cases[0].TestCase.ArtifactEntry.TestcaseOrdinal != 0 || cases[1].TestCase.ArtifactEntry.TestcaseOrdinal != 1 {
				t.Fatalf("entry ordinals=%#v %#v", cases[0].TestCase.ArtifactEntry, cases[1].TestCase.ArtifactEntry)
			}
			aggregate := suites[0].TestSuite.Aggregate
			wantTests := 1
			if version == "9.1.1" {
				wantTests = 2
			}
			if aggregate == nil || aggregate.Tests != wantTests || aggregate.Failures != 1 || aggregate.Errors != 1 {
				t.Fatalf("aggregate=%#v", aggregate)
			}
		})
	}
}

type pytestFixtureManifest struct {
	SchemaVersion int `json:"schema_version"`
	Fixtures      []struct {
		ProducerVersion string `json:"producer_version"`
		Fixture         string `json:"fixture"`
		SHA256          string `json:"sha256"`
	} `json:"fixtures"`
}

func TestFrozenPytestFixtureManifestSHA256(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "fixtures", "pytest-junit")
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest pytestFixtureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 1 || len(manifest.Fixtures) != 4 {
		t.Fatalf("manifest=%#v", manifest)
	}
	for _, entry := range manifest.Fixtures {
		b, err := os.ReadFile(filepath.Join(root, entry.Fixture))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatalf("fixture %s sha drift", entry.Fixture)
		}
	}
}

func TestPytestStructuredResultsPublicIPC(t *testing.T) {
	restoreAddopts := unsetEnvironmentForTest(t, structuredapp.PytestAddoptsEnvironment)
	defer restoreAddopts()
	stateDir, runtimeDir := a1RuntimeDirs(t)
	workspaceRoot := initWorkspaceCLIRepo(t)
	if err := os.Mkdir(filepath.Join(workspaceRoot, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(workspaceRoot, "test_public_ipc.py"), `import pytest


def test_pass():
    assert True


def test_fail():
    assert False


@pytest.mark.skip(reason="skip")
def test_skip():
    pass


@pytest.mark.xfail(reason="xfail")
def test_xfail():
    assert False
`)
	binDir := os.Getenv("SHELLBEAM_PYTEST_REAL_BIN_DIR")
	if binDir == "" {
		binDir = t.TempDir()
		fixture, err := filepath.Abs(frozenFixturePath("9.1.1", "outcomes"))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SHELLBEAM_PYTEST_FIXTURE", fixture)
		shim := `#!/bin/sh
set -eu
out=""
for arg in "$@"; do
  case "$arg" in --junitxml=*) out="${arg#*=}" ;; esac
done
[ -n "$out" ]
cp "$SHELLBEAM_PYTEST_FIXTURE" "$out"
printf 'pytest shim: intentional failure\n'
exit 1
`
		path := filepath.Join(binDir, "pytest")
		if err := os.WriteFile(path, []byte(shim), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	store := openA1Store(t, stateDir)
	workspace := saveStructuredCaptureWorkspace(t, store, workspaceRoot)
	request := ipcadapter.RequestV2{
		Action: "start", OperationID: "pytest-public-ipc", WorkspaceID: string(workspace.ID), CWD: ".", StructuredAdapter: structuredapp.PytestJUnitAdapterID,
		Argv: []string{"pytest", "test_public_ipc.py", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
	}
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	first := callA1Terminal(t, client, request)
	assertChildFailureWithOutput(t, first)
	firstStructured := waitStructuredTerminal(t, client, request.OperationID)
	assertPytestPublicIPCStructured(t, firstStructured)
	stop()

	client, stop = startExecutionObservationDaemon(t, stateDir, runtimeDir)
	t.Cleanup(stop)
	second := callA1Terminal(t, client, request)
	if second.Receipt == nil || first.Receipt == nil || second.Receipt.SessionID != first.Receipt.SessionID {
		t.Fatalf("restart replay first=%#v second=%#v", first.Receipt, second.Receipt)
	}
	secondStructured := waitStructuredTerminal(t, client, request.OperationID)
	if secondStructured.DerivationKey != firstStructured.DerivationKey || secondStructured.SourceKind != core.StructuredInputArtifactBlob || secondStructured.SourceState != structuredapp.InputSourceRetained {
		t.Fatalf("restart structured first=%#v second=%#v", firstStructured, secondStructured)
	}

	assertUnqualifiedPytestPublicIPCFallsBack(t, client, request)
}

func assertUnqualifiedPytestPublicIPCFallsBack(t *testing.T, client *ipcadapter.Client, qualified ipcadapter.RequestV2) {
	t.Helper()
	negative := qualified
	negative.OperationID = "pytest-public-ipc-unqualified"
	negative.StructuredAdapter = ""
	negative.Argv = []string{"pytest", "test_public_ipc.py", "--junitxml=reports/unqualified.xml", "-o", "junit_family=xunit2"}
	assertChildFailureWithOutput(t, callA1Terminal(t, client, negative))
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: negative.OperationID + "-structured", Action: "inspect.structured",
		OperationID: negative.OperationID, MaxRecords: structuredapp.MaxListRecords,
	})
	if err != nil || !response.OK || response.Structured == nil || response.Structured.Status != structuredapp.InspectNotFound {
		t.Fatalf("unqualified pytest created structured authority: response=%#v err=%v", response, err)
	}
}

func assertPytestPublicIPCStructured(t *testing.T, got structuredapp.InspectResult) {
	t.Helper()
	if got.Status != structuredapp.InspectTerminal || got.Producer == nil || got.Producer.AdapterID != structuredapp.PytestJUnitAdapterID || got.ParseOutcome != core.ParseComplete || got.Completeness != core.CompletenessComplete || got.SourceKind != core.StructuredInputArtifactBlob || got.SourceState != structuredapp.InputSourceRetained || got.SemanticsCoverage == nil || got.SemanticsCoverage.Family != "xunit2" {
		t.Fatalf("structured=%#v", got)
	}
	var pass, fail, skip, xfail bool
	for _, record := range got.Records {
		if record.Authority != core.AuthorityMechanical {
			continue
		}
		if record.TestCase == nil {
			continue
		}
		switch record.TestCase.Status {
		case core.TestPassed:
			pass = true
		case core.TestFailed:
			fail = true
		case core.TestSkipped:
			skip = true
		}
		if record.TestCase.ProducerDisposition != nil && record.TestCase.ProducerDisposition.Code == "pytest:xfail" {
			xfail = true
		}
	}
	if !pass || !fail || !skip || !xfail {
		t.Fatalf("mechanical records missing pass=%v fail=%v skip=%v xfail=%v records=%#v", pass, fail, skip, xfail, got.Records)
	}
}
