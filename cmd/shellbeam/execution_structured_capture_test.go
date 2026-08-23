package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestPytestStructuredCaptureRuntimeMaterializesAndParsesPinnedJUnit(t *testing.T) {
	restoreEnv := unsetEnvironmentForTest(t, structuredapp.PytestAddoptsEnvironment)
	defer restoreEnv()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := openStructuredCaptureTestStore(t)
	workspace := saveStructuredCaptureWorkspace(t, store, root)
	runtime, err := newExecutionObservationRuntime(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown(context.Background())

	opID := operation.ID("pytest-runtime-op")
	sessionID := operation.SessionID("pytest-runtime-session")
	argv := []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}
	preparation, err := runtime.capture.PrepareStructuredCapture(context.Background(), daemonapp.StructuredCapturePrepareRequest{
		OperationID: opID, SessionID: sessionID, WorkspaceID: string(workspace.ID), Argv: argv, CWD: root,
		ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/pytest",
	})
	if err != nil || preparation.AdapterID != structuredapp.PytestJUnitAdapterID || preparation.CaptureDigest == "" || !preparation.Owned {
		t.Fatalf("preparation=%#v err=%v", preparation, err)
	}
	junit := `<testsuite name="pytest" tests="1" failures="0" errors="0" skipped="0"><testcase classname="pkg.Test" name="test_ok" time="0.001"/></testsuite>`
	if err := os.WriteFile(filepath.Join(root, "reports", "junit.xml"), []byte(junit), 0o600); err != nil {
		t.Fatal(err)
	}
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: opID, SessionID: sessionID, WorkspaceID: string(workspace.ID), StructuredAdapter: preparation.AdapterID, StructuredCaptureDigest: preparation.CaptureDigest, ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/pytest", Argv: argv, CWD: root}
	capture := runtime.capture.AcquireTerminal(context.Background(), reservation)
	if capture.State != structuredapp.TerminalCaptureAcquired || capture.Source() == nil {
		t.Fatalf("capture=%#v", capture)
	}
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: string(opID), SessionID: string(sessionID), Fingerprint: "fp", DaemonIncarnation: "daemon", State: session.Failed, Outcome: session.Failure, OutputComplete: true}
	if err := runtime.capture.ScheduleTerminal(context.Background(), rec, capture); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var derivation structuredcore.Derivation
	for time.Now().Before(deadline) {
		got, found, findErr := store.FindOperationDerivation(context.Background(), string(opID))
		if findErr != nil {
			t.Fatal(findErr)
		}
		if found && got.Lifecycle == structuredcore.LifecycleTerminal {
			derivation = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if derivation.Lifecycle != structuredcore.LifecycleTerminal || derivation.Producer.AdapterID != structuredapp.PytestJUnitAdapterID || derivation.ParseOutcome != structuredcore.ParseComplete || derivation.SemanticsCoverage == nil {
		t.Fatalf("derivation=%#v", derivation)
	}
	records, err := store.ListRecords(context.Background(), derivation.DerivationKey, structuredapp.RecordQuery{Limit: 16})
	if err != nil {
		t.Fatal(err)
	}
	var foundCase bool
	for _, record := range records {
		if record.TestCase != nil && record.TestCase.Name == "test_ok" && record.TestCase.Status == structuredcore.TestPassed {
			foundCase = true
		}
	}
	if !foundCase {
		t.Fatalf("records=%#v", records)
	}
}

func TestHostStructuredPresenceObserverPersistsOnlyApprovedPresenceFacts(t *testing.T) {
	old, had := os.LookupEnv(structuredapp.PytestAddoptsEnvironment)
	if err := os.Setenv(structuredapp.PytestAddoptsEnvironment, "-q SECRET_DO_NOT_PERSIST"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if had {
			_ = os.Setenv(structuredapp.PytestAddoptsEnvironment, old)
		} else {
			_ = os.Unsetenv(structuredapp.PytestAddoptsEnvironment)
		}
	}()
	fact, err := (hostStructuredPresenceObserver{}).ObserveEnvironmentPresence(context.Background(), structExecution(), structuredapp.PytestAddoptsEnvironment)
	if err != nil || !fact.Present || strings.Contains(fact.AuthorityDigest, "SECRET_DO_NOT_PERSIST") {
		t.Fatalf("fact=%#v err=%v", fact, err)
	}
	jestFact, err := (hostStructuredPresenceObserver{}).ObserveEnvironmentPresence(context.Background(), environmentcore.ExecutionContext{Mode: "argv", Identity: "/usr/bin/jest"}, structuredapp.JestJasmineEnvironment)
	if err != nil || jestFact.Name != structuredapp.JestJasmineEnvironment {
		t.Fatalf("jest fact=%#v err=%v", jestFact, err)
	}
	if _, err := (hostStructuredPresenceObserver{}).ObserveEnvironmentPresence(context.Background(), structExecution(), "SHELLBEAM_AGENT"); err == nil {
		t.Fatal("arbitrary environment name unexpectedly observed")
	}
}

func structExecution() environmentcore.ExecutionContext {
	return environmentcore.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
}

func openStructuredCaptureTestStore(t *testing.T) *storeadapter.Repository {
	t.Helper()
	store, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 32 << 20, ControlReserve: 1024})
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func saveStructuredCaptureWorkspace(t *testing.T, store *storeadapter.Repository, root string) workspacecore.Workspace {
	t.Helper()
	now := time.Now().UTC()
	repo := workspacecore.Repository{SchemaVersion: workspacecore.SchemaVersion, ID: "repo_01M09A27JCSE71BXSP477EKN34", CommonDir: filepath.Join(root, ".git"), CreatedAt: now, LastSeenAt: now}
	if err := store.SaveRepository(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	workspace := workspacecore.Workspace{SchemaVersion: workspacecore.SchemaVersion, ID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q", RepositoryID: repo.ID, Label: "pytest-runtime", Root: root, GitDir: filepath.Join(root, ".git"), CreatedAt: now, LastSeenAt: now}
	if err := store.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	return workspace
}
func unsetEnvironmentForTest(t *testing.T, name string) func() {
	t.Helper()
	old, had := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	return func() {
		if had {
			_ = os.Setenv(name, old)
		} else {
			_ = os.Unsetenv(name)
		}
	}
}

func TestJestStructuredCaptureRuntimePreparesQualifiedJSON(t *testing.T) {
	restoreEnv := unsetEnvironmentForTest(t, structuredapp.JestJasmineEnvironment)
	defer restoreEnv()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := openStructuredCaptureTestStore(t)
	workspace := saveStructuredCaptureWorkspace(t, store, root)
	runtime, err := newExecutionObservationRuntime(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown(context.Background())

	preparation, err := runtime.capture.PrepareStructuredCapture(context.Background(), daemonapp.StructuredCapturePrepareRequest{
		OperationID: "jest-runtime-op", SessionID: "jest-runtime-session", WorkspaceID: string(workspace.ID),
		StructuredAdapter: structuredapp.JestJSONAdapterID,
		Argv:              []string{"jest", "--runInBand", "--json", "--outputFile=reports/jest.json"}, CWD: root,
		ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/jest",
	})
	if err != nil || preparation.AdapterID != structuredapp.JestJSONAdapterID || preparation.CaptureDigest == "" || !preparation.Owned {
		t.Fatalf("preparation=%#v err=%v", preparation, err)
	}
	stored, findErr := store.FindCaptureAuthority(context.Background(), "jest-runtime-op")
	if findErr != nil || stored.Authority.JestInvocation == nil || stored.Authority.JestInvocation.JasmineEnvironmentFact.Present {
		t.Fatalf("authority=%#v err=%v", stored, findErr)
	}

	fixturePath := filepath.Join("..", "..", "tests", "fixtures", "jest-json", "real-doc-fixtures", "jest-30.4.2", "pass.json")
	payload, readErr := os.ReadFile(fixturePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	payload = []byte(strings.ReplaceAll(string(payload), "/private/jest-fixture", filepath.ToSlash(root)))
	if err := os.WriteFile(filepath.Join(root, "reports", "jest.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: "jest-runtime-op", SessionID: "jest-runtime-session", WorkspaceID: string(workspace.ID), StructuredAdapter: preparation.AdapterID, StructuredCaptureDigest: preparation.CaptureDigest, ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/jest", Argv: []string{"jest", "--runInBand", "--json", "--outputFile=reports/jest.json"}, CWD: root}
	capture := runtime.capture.AcquireTerminal(context.Background(), reservation)
	if capture.State != structuredapp.TerminalCaptureAcquired || capture.Source() == nil {
		t.Fatalf("capture=%#v", capture)
	}
	rec := receipt.Receipt{SchemaVersion: 1, OperationID: "jest-runtime-op", SessionID: "jest-runtime-session", Fingerprint: "fp", DaemonIncarnation: "daemon", State: session.Failed, Outcome: session.Failure, OutputComplete: true}
	if err := runtime.capture.ScheduleTerminal(context.Background(), rec, capture); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		derivation, found, err := store.FindOperationDerivation(context.Background(), "jest-runtime-op")
		if err != nil {
			t.Fatal(err)
		}
		if found && derivation.Lifecycle == structuredcore.LifecycleTerminal {
			if derivation.Producer.AdapterID != structuredapp.JestJSONAdapterID || derivation.ParseOutcome != structuredcore.ParseComplete {
				t.Fatalf("derivation=%#v", derivation)
			}
			inspected, inspectErr := runtime.structured.Inspect(context.Background(), structuredapp.InspectRequest{OperationID: "jest-runtime-op", MaxRecords: 16})
			if inspectErr != nil {
				t.Fatal(inspectErr)
			}
			if inspected.Status != structuredapp.InspectTerminal || inspected.SourceKind != structuredcore.StructuredInputArtifactBlob || inspected.SourceState != structuredapp.InputSourceRetained || inspected.SemanticsCoverage == nil || inspected.SemanticsCoverage.Namespace != "jest" || inspected.SemanticsCoverage.Family != "v30" || inspected.ObservedEntries == nil || inspected.ObservedEntries.Files != 1 || inspected.ObservedEntries.Entries != 1 {
				t.Fatalf("inspect=%#v", inspected)
			}
			encoded, _ := json.Marshal(inspected)
			if bytes.Contains(encoded, []byte(root)) || bytes.Contains(encoded, []byte(`"content"`)) {
				t.Fatalf("private artifact path/content leaked: %s", encoded)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("jest derivation did not become terminal")
}

func TestJestStructuredCaptureRuntimeRejectsJasmineEnvironment(t *testing.T) {
	old, had := os.LookupEnv(structuredapp.JestJasmineEnvironment)
	if err := os.Setenv(structuredapp.JestJasmineEnvironment, "1"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if had {
			_ = os.Setenv(structuredapp.JestJasmineEnvironment, old)
		} else {
			_ = os.Unsetenv(structuredapp.JestJasmineEnvironment)
		}
	}()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	store := openStructuredCaptureTestStore(t)
	workspace := saveStructuredCaptureWorkspace(t, store, root)
	runtime, err := newExecutionObservationRuntime(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.shutdown(context.Background())

	preparation, err := runtime.capture.PrepareStructuredCapture(context.Background(), daemonapp.StructuredCapturePrepareRequest{
		OperationID: "jest-jasmine-reject", SessionID: "jest-jasmine-session", WorkspaceID: string(workspace.ID),
		StructuredAdapter: structuredapp.JestJSONAdapterID,
		Argv:              []string{"jest", "--json", "--outputFile=reports/jest.json"}, CWD: root,
		ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/jest",
	})
	if err == nil || preparation.AdapterID != "" || preparation.Owned {
		t.Fatalf("preparation=%#v err=%v", preparation, err)
	}
}
