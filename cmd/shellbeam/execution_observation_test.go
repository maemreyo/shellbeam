package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	observationapp "github.com/maemreyo/shellbeam/internal/app/observation"
	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

func TestExecutionObservationDaemonComposesEventAndStructuredServices(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client := runA1Daemon(t, stateDir, runtimeDir)

	server, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "server", Action: "inspect.server",
	})
	if err != nil || !server.OK || server.Server == nil {
		t.Fatalf("inspect.server response=%#v err=%v", server, err)
	}
	for _, feature := range []capability.Feature{
		capability.FeatureEventJournal,
		capability.FeatureEventSnapshotRecovery,
		capability.FeatureStructuredResults,
		capability.FeatureStructuredLifecycle,
	} {
		if server.Server.Features[feature] != capability.Available {
			t.Fatalf("feature %s=%s", feature, server.Server.Features[feature])
		}
	}
	if got := server.Server.StructuredAdapterIDs; len(got) != 4 || got[0] != "go-test-json" || got[1] != "go-vet-json" || got[2] != structuredapp.PytestJUnitAdapterID || got[3] != structuredapp.JestJSONAdapterID {
		t.Fatalf("structured adapters=%v", got)
	}
	if !slices.Equal(server.Server.StructuredSchemaVersions, []int{1, 2, 3}) || !slices.Equal(server.Server.StructuredInputKinds, []string{"raw_output", "artifact_blob"}) || server.Server.Limits.StructuredArtifactBlobBytes != structuredapp.DefaultMaxArtifactBlobBytes {
		t.Fatalf("structured artifact capability=%#v", server.Server)
	}

	result := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-events", CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "printf 'hello\\n'"},
	})
	if result.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	events, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events", Action: "inspect.events",
		Target: &observation.Target{Kind: observation.TargetOperation, OperationID: "a22-events"}, MaxEvents: 16,
	})
	if err != nil || !events.OK || events.Events == nil {
		t.Fatalf("inspect.events response=%#v err=%v", events, err)
	}
	assertExecutionLifecycleEvents(t, events.Events.Events)

	structured, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "structured", Action: "inspect.structured",
		OperationID: "a22-events", MaxRecords: 16,
	})
	if err != nil || !structured.OK || structured.Structured == nil || structured.Structured.Status != "not_found" {
		t.Fatalf("inspect.structured response=%#v err=%v", structured, err)
	}
}

func assertExecutionLifecycleEvents(t *testing.T, events []observation.Event) {
	t.Helper()
	want := []observation.EventKind{
		observation.EventOperationAdmitted,
		observation.EventProcessStarted,
		observation.EventOutputAvailable,
		observation.EventProcessTerminal,
	}
	if len(events) < len(want) || len(events) > len(want)+1 {
		t.Fatalf("execution events=%#v", events)
	}
	for i, kind := range want {
		if events[i].Kind != kind {
			t.Fatalf("event[%d]=%s want %s", i, events[i].Kind, kind)
		}
	}
	if len(events) == len(want)+1 && events[len(want)].Kind != observation.EventTelemetryChanged {
		t.Fatalf("unexpected derived event=%#v", events[len(want)])
	}
}

func TestExecutionObservationRestartAndCompactionResumeWithoutSilentGap(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	target := &observation.Target{Kind: observation.TargetActivity, ActivityID: "activity-a22-restart"}
	client, stop := startExecutionObservationDaemon(t, stateDir, runtimeDir)
	oldCursor, firstCut := seedRestartAcceptance(t, client, target)
	stop()

	client, stop = startExecutionObservationDaemon(t, stateDir, runtimeDir)
	assertRestartDelta(t, client, target, oldCursor, firstCut)
	stop()
	compactRestartEvents(t, stateDir, firstCut)

	client, stop = startExecutionObservationDaemon(t, stateDir, runtimeDir)
	t.Cleanup(stop)
	resume, snapshotCut := snapshotResumeCursor(t, client, target, oldCursor)
	assertPostSnapshotTransition(t, client, target, resume, snapshotCut)
}

func seedRestartAcceptance(t *testing.T, client *ipcadapter.Client, target *observation.Target) (string, observation.ChangeSeq) {
	t.Helper()
	first := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-restart-first", ActivityID: "activity-a22-restart",
		CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "printf 'first\\n'"},
	})
	assertA1ChildSuccess(t, first)
	if first.Receipt == nil {
		t.Fatal("first receipt missing")
	}
	page, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events-page", Action: "inspect.events", Target: target, MaxEvents: 2,
	})
	if err != nil || !page.OK || page.Events == nil || page.Events.Continuity != observation.ContinuityComplete || len(page.Events.Events) != 2 || !page.Events.Truncated {
		t.Fatalf("first event page=%#v app_error=%#v err=%v", page, page.Error, err)
	}
	assertSessionEventView(t, client, first.Receipt.SessionID)
	return page.Events.NextEventCursor, page.Events.Events[len(page.Events.Events)-1].ChangeSeq
}

func assertSessionEventView(t *testing.T, client *ipcadapter.Client, sessionID string) {
	t.Helper()
	view, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "session-events", Action: "inspect.events",
		Target: &observation.Target{Kind: observation.TargetSession, SessionID: sessionID}, MaxEvents: 16,
	})
	if err != nil || !view.OK || view.Events == nil {
		t.Fatalf("session event view=%#v err=%v", view, err)
	}
	assertExecutionLifecycleEvents(t, view.Events.Events)
	for _, event := range view.Events.Events {
		if event.Correlation.SessionID != sessionID || event.Correlation.OperationID != "a22-restart-first" {
			t.Fatalf("unexpected session correlation=%#v", event.Correlation)
		}
	}
}

func assertRestartDelta(t *testing.T, client *ipcadapter.Client, target *observation.Target, cursor string, firstCut observation.ChangeSeq) {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events-restarted", Action: "inspect.events",
		Target: target, AfterEventCursor: cursor, MaxEvents: 16,
	})
	if err != nil || !response.OK || response.Events == nil || response.Events.Continuity != observation.ContinuityComplete {
		t.Fatalf("restart delta=%#v err=%v", response, err)
	}
	if len(response.Events.Events) < 2 || len(response.Events.Events) > 3 || response.Events.Events[0].Kind != observation.EventOutputAvailable || response.Events.Events[1].Kind != observation.EventProcessTerminal || len(response.Events.Events) == 3 && response.Events.Events[2].Kind != observation.EventTelemetryChanged {
		t.Fatalf("restart delta kinds=%#v", response.Events.Events)
	}
	for _, event := range response.Events.Events {
		if event.ChangeSeq <= firstCut {
			t.Fatalf("restart replayed old event seq=%d cut=%d", event.ChangeSeq, firstCut)
		}
	}
}

func compactRestartEvents(t *testing.T, stateDir string, firstCut observation.ChangeSeq) {
	t.Helper()
	store := openA1Store(t, stateDir)
	compacted, err := store.CompactEvents(context.Background(), storeadapter.EventRetentionPolicy{
		MaxEvents: 1, MaxBytes: 1 << 20, MaxAge: 24 * time.Hour,
	})
	if err != nil || compacted.CompactedThroughSeq <= firstCut {
		t.Fatalf("compaction=%#v err=%v cut=%d", compacted, err, firstCut)
	}
}

func snapshotResumeCursor(t *testing.T, client *ipcadapter.Client, target *observation.Target, oldCursor string) (string, observation.ChangeSeq) {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events-snapshot", Action: "inspect.events",
		Target: target, AfterEventCursor: oldCursor, MaxEvents: 16,
	})
	if err != nil || !response.OK || response.Events == nil || response.Events.Continuity != observation.ContinuitySnapshotRequired || response.Events.Snapshot == nil || response.Events.NextEventCursor == "" {
		t.Fatalf("snapshot recovery=%#v err=%v", response, err)
	}
	return response.Events.NextEventCursor, response.Events.Snapshot.CapturedThroughSeq
}

func assertPostSnapshotTransition(t *testing.T, client *ipcadapter.Client, target *observation.Target, resume string, snapshotCut observation.ChangeSeq) {
	t.Helper()
	second := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-restart-second", ActivityID: "activity-a22-restart",
		CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "printf 'second\\n'"},
	})
	assertA1ChildSuccess(t, second)
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "events-after-snapshot", Action: "inspect.events",
		Target: target, AfterEventCursor: resume, MaxEvents: 16,
	})
	if err != nil || !response.OK || response.Events == nil || response.Events.Continuity != observation.ContinuityComplete {
		t.Fatalf("after snapshot delta=%#v err=%v", response, err)
	}
	assertExecutionLifecycleEvents(t, response.Events.Events)
	for _, event := range response.Events.Events {
		if event.ChangeSeq <= snapshotCut || event.Correlation.OperationID != "a22-restart-second" {
			t.Fatalf("post-snapshot event=%#v cut=%d", event, snapshotCut)
		}
	}
}

func startExecutionObservationDaemon(t *testing.T, stateDir, runtimeDir string) (*ipcadapter.Client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}
	go func() { done <- runDaemon(ctx, args) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("daemon shutdown: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("daemon did not stop")
			}
		})
	}
	return ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock")), stop
}

func TestExecutionObservationNativeGoStructuredResultsPreserveChildTruth(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	moduleDir := t.TempDir()
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/a22\n\ngo 1.23\n")
	writeTestFile(t, filepath.Join(moduleDir, "calc.go"), "package a22\n\nfunc Add(a, b int) int { return a + b }\n")
	writeTestFile(t, filepath.Join(moduleDir, "calc_test.go"), `package a22

import "testing"

func TestPass(t *testing.T) { if Add(1, 1) != 2 { t.Fatal("bad add") } }
func TestFail(t *testing.T) { if Add(1, 2) != 4 { t.Fatal("intentional failure") } }
`)
	client := runA1Daemon(t, stateDir, runtimeDir)

	testResult := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-go-test", CWD: moduleDir,
		Argv: []string{"go", "test", "-json", "./..."},
	})
	assertChildFailureWithOutput(t, testResult)
	testRaw := readTerminalOutput(t, client, testResult)
	if !strings.Contains(testRaw, "TestFail") || !strings.Contains(testRaw, `"Action":"fail"`) {
		t.Fatalf("go test raw output missing native failure facts: %q", testRaw)
	}
	testStructured := waitStructuredTerminal(t, client, "a22-go-test")
	if testStructured.Producer == nil || testStructured.Producer.AdapterID != "go-test-json" || testStructured.ParseOutcome != structuredcore.ParseComplete || testStructured.Completeness != structuredcore.CompletenessComplete {
		t.Fatalf("go test structured=%#v", testStructured)
	}
	if testStructured.Summary.TestPassed < 1 || testStructured.Summary.TestFailed < 1 || testStructured.Summary.RecordsReturned < 2 {
		t.Fatalf("go test summary=%#v", testStructured.Summary)
	}
	for _, record := range testStructured.Records {
		if record.Authority != structuredcore.AuthorityMechanical || record.DerivationMethod != structuredcore.DerivationNativeFieldMapping || record.OperationID != "a22-go-test" {
			t.Fatalf("go test record authority=%#v", record)
		}
		rawRef, ok := record.SourceRef.Raw()
		if !ok || rawRef.SessionID != testResult.Receipt.SessionID || rawRef.StartByte != 0 || rawRef.EndByte != testResult.Receipt.OutputBytes || len(rawRef.SHA256) != 64 {
			t.Fatalf("go test source ref=%#v receipt=%#v", record.SourceRef, testResult.Receipt)
		}
	}

	writeTestFile(t, filepath.Join(moduleDir, "vet.go"), `package a22

import "fmt"

func VetProblem() { fmt.Printf("%d", "text") }
`)
	vetResult := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-go-vet", CWD: moduleDir,
		Argv: []string{"go", "vet", "-json", "./..."},
	})
	assertA1ChildSuccess(t, vetResult)
	if vetResult.Receipt == nil || vetResult.Receipt.OutputBytes < 1 || !vetResult.Receipt.OutputComplete {
		t.Fatalf("go vet receipt=%#v", vetResult.Receipt)
	}
	vetRaw := readTerminalOutput(t, client, vetResult)
	if !strings.Contains(vetRaw, `"printf"`) || !strings.Contains(vetRaw, "wrong type string") {
		t.Fatalf("go vet raw output missing native diagnostic: %q", vetRaw)
	}
	vetStructured := waitStructuredTerminal(t, client, "a22-go-vet")
	if vetStructured.Producer == nil || vetStructured.Producer.AdapterID != "go-vet-json" || vetStructured.ParseOutcome != structuredcore.ParseComplete || vetStructured.Completeness != structuredcore.CompletenessComplete {
		t.Fatalf("go vet structured=%#v", vetStructured)
	}
	if vetStructured.Summary.Errors < 1 || len(vetStructured.Records) < 1 {
		t.Fatalf("go vet summary=%#v records=%#v", vetStructured.Summary, vetStructured.Records)
	}
	for _, record := range vetStructured.Records {
		if record.RecordKind != structuredcore.RecordDiagnostic || record.Diagnostic == nil || record.Authority != structuredcore.AuthorityMechanical || record.DerivationMethod != structuredcore.DerivationNativeFieldMapping {
			t.Fatalf("go vet record=%#v", record)
		}
		rawRef, ok := record.SourceRef.Raw()
		if !ok || rawRef.SessionID != vetResult.Receipt.SessionID || rawRef.EndByte != vetResult.Receipt.OutputBytes || len(rawRef.SHA256) != 64 {
			t.Fatalf("go vet source ref=%#v receipt=%#v", record.SourceRef, vetResult.Receipt)
		}
	}
}

func readTerminalOutput(t *testing.T, client *ipcadapter.Client, result receipt.Result) string {
	t.Helper()
	if result.Receipt == nil {
		t.Fatal("terminal receipt missing")
	}
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: result.Operation.OperationID + "-raw", Action: "poll",
		SessionID: result.Operation.SessionID, Cursor: 0, YieldMS: 0, MaxOutputBytes: 1 << 20,
	})
	if err != nil || !response.OK || response.Result == nil {
		t.Fatalf("terminal raw output response=%#v err=%v", response, err)
	}
	if response.Result.Output.RawBytes != result.Receipt.OutputBytes || !response.Result.Output.OutputComplete {
		t.Fatalf("raw output metadata=%#v receipt=%#v", response.Result.Output, result.Receipt)
	}
	return response.Result.Output.Preview
}

func waitStructuredTerminal(t *testing.T, client *ipcadapter.Client, operationID string) structuredapp.InspectResult {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: operationID + "-structured", Action: "inspect.structured",
			OperationID: operationID, MaxRecords: structuredapp.MaxListRecords,
		})
		if err != nil || !response.OK || response.Structured == nil {
			t.Fatalf("inspect.structured %s response=%#v err=%v", operationID, response, err)
		}
		if response.Structured.Status == structuredapp.InspectTerminal {
			return *response.Structured
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("structured result %s did not become terminal", operationID)
	return structuredapp.InspectResult{}
}

func assertChildFailureWithOutput(t *testing.T, result receipt.Result) {
	t.Helper()
	if result.Receipt == nil || result.Child == nil || result.Child.State != receipt.ChildExited || result.Child.Outcome != session.Failure || result.Child.ExitCode == nil || *result.Child.ExitCode == 0 || result.Receipt.OutputBytes < 1 || !result.Receipt.OutputComplete {
		t.Fatalf("expected independent child failure with output: result=%#v", result)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionObservationPersistenceDoesNotCopyAmbientOrStdinSecrets(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	ambientToken := "A22_TOKEN_9f6ee8e05f284761"
	privateKey := "-----BEGIN PRIVATE KEY-----A22-DO-NOT-PERSIST-----END PRIVATE KEY-----"
	stdinSecret := "A22_STDIN_SECRET_c88aa18383b64a48"
	t.Setenv("SHELLBEAM_A22_TOKEN", ambientToken)
	t.Setenv("SHELLBEAM_A22_PRIVATE_KEY", privateKey)
	client := runA1Daemon(t, stateDir, runtimeDir)

	start, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "privacy-start", Action: "start",
		OperationID: "a22-privacy-stdin", CWD: "/tmp", Argv: []string{"/bin/sh", "-c", "cat >/dev/null"},
		// This session is written to, so it has to say so: an ordinary command
		// now has its input closed at spawn.
		StdinMode: operation.StdinModeStream,
		YieldMS:   25, MaxOutputBytes: 4096,
	})
	if err != nil || !start.OK || start.Result == nil || start.Result.Operation.SessionID == "" {
		t.Fatalf("privacy start=%#v err=%v", start, err)
	}
	sessionID := start.Result.Operation.SessionID
	write, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "privacy-write", Action: "write",
		SessionID: sessionID, InputOffset: 0, Chars: stdinSecret,
	})
	if err != nil || !write.OK || write.View == nil || write.View.NextInputOffset != int64(len(stdinSecret)) {
		t.Fatalf("privacy write=%#v err=%v", write, err)
	}
	eof, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "privacy-eof", Action: "write",
		SessionID: sessionID, InputOffset: int64(len(stdinSecret)), EOF: true,
	})
	if err != nil || !eof.OK || eof.View == nil || !eof.View.EOFQueued {
		t.Fatalf("privacy eof=%#v err=%v", eof, err)
	}
	privacyResult := pollA22Terminal(t, client, "a22-privacy-stdin", sessionID)
	assertA1ChildSuccess(t, privacyResult)

	moduleDir := t.TempDir()
	writeTestFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/privacy\n\ngo 1.23\n")
	writeTestFile(t, filepath.Join(moduleDir, "privacy_test.go"), "package privacy\n\nimport \"testing\"\nfunc TestSafe(t *testing.T) {}\n")
	structured := callA1Terminal(t, client, ipcadapter.RequestV2{
		Action: "start", OperationID: "a22-privacy-structured", CWD: moduleDir,
		Argv: []string{"go", "test", "-json", "./..."},
	})
	assertA1ChildSuccess(t, structured)
	_ = waitStructuredTerminal(t, client, "a22-privacy-structured")
	if _, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "privacy-events", Action: "inspect.events",
		Target: &observation.Target{Kind: observation.TargetOperation, OperationID: "a22-privacy-structured"}, MaxEvents: 16,
	}); err != nil {
		t.Fatal(err)
	}

	persisted := readStateTree(t, stateDir)
	for name, secret := range map[string]string{"ambient token": ambientToken, "private key": privateKey, "stdin": stdinSecret} {
		if bytes.Contains(persisted, []byte(secret)) {
			t.Fatalf("%s copied into persisted A2.2 state", name)
		}
	}
}

func pollA22Terminal(t *testing.T, client *ipcadapter.Client, operationID, sessionID string) receipt.Result {
	t.Helper()
	var result receipt.Result
	for attempt := 0; attempt < 20; attempt++ {
		response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: operationID + "-poll", Action: "poll",
			SessionID: sessionID, Cursor: 0, YieldMS: 250, MaxOutputBytes: 4096,
		})
		if err != nil || !response.OK || response.Result == nil {
			t.Fatalf("%s poll=%#v err=%v", operationID, response, err)
		}
		result = *response.Result
		if result.Operation.State == receipt.OperationTerminal {
			return result
		}
	}
	t.Fatalf("%s did not become terminal", operationID)
	return result
}

func readStateTree(t *testing.T, root string) []byte {
	t.Helper()
	var out []byte
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// The daemon is still writing while this walks. Durable writes land
			// through a .shellbeam- temporary that is renamed into place, and
			// retention collects records on its own schedule, so an entry can
			// stop existing between being listed and being read. A file that is
			// gone holds no secret to find, and failing the scan on it turns a
			// leak assertion into a race.
			if errors.Is(err, fs.ErrNotExist) && path != root {
				return nil
			}
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		out = append(out, data...)
		out = append(out, '\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

type materializerWakeProbe struct {
	calls chan struct{}
}

func (p *materializerWakeProbe) Materialize(context.Context) (observationapp.MaterializeResult, error) {
	select {
	case p.calls <- struct{}{}:
	default:
	}
	return observationapp.MaterializeResult{}, nil
}

func TestExecutionObservationMaterializerContinuesAfterWakeup(t *testing.T) {
	wakeups := make(chan struct{}, 1)
	probe := &materializerWakeProbe{calls: make(chan struct{}, 4)}
	runtime := &executionObservationRuntime{material: probe, wakeups: wakeups}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime.startMaterialization(ctx)

	waitMaterializerCall(t, probe.calls, "initial materialization")
	wakeups <- struct{}{}
	waitMaterializerCall(t, probe.calls, "post-commit wakeup")
}

func waitMaterializerCall(t *testing.T, calls <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatalf("%s did not run", phase)
	}
}

type materializerDelegateProbe struct {
	inner observationapp.MaterializerPort
	calls chan observationapp.MaterializeResult
}

func (p *materializerDelegateProbe) Materialize(ctx context.Context) (observationapp.MaterializeResult, error) {
	result, err := p.inner.Materialize(ctx)
	select {
	case p.calls <- result:
	default:
	}
	return result, err
}

func TestExecutionObservationRuntimeMaterializesCommitAfterInitialPass(t *testing.T) {
	stateDir, _ := a1RuntimeDirs(t)
	store := openA1Store(t, stateDir)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime, err := newExecutionObservationRuntime(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = runtime.shutdown(shutdownCtx)
	})
	probe := &materializerDelegateProbe{inner: runtime.material, calls: make(chan observationapp.MaterializeResult, 4)}
	runtime.material = probe
	runtime.startMaterialization(ctx)

	select {
	case initial := <-probe.calls:
		if initial.HighWatermark != 0 || initial.State.MaterializedThroughSeq != 0 {
			t.Fatalf("initial materialization=%#v", initial)
		}
	case <-time.After(time.Second):
		t.Fatal("initial materialization did not finish")
	}

	prepared, result := store.PrepareObservation(ctx, observation.PrepareRequest{
		Kind:       observation.EventOperationAdmitted,
		SubjectRef: "operation:background-materialization",
		Summary:    "operation admitted",
	})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result := store.CommitObservation(ctx, prepared.Obligation.ChangeSeq); result.Err != nil {
		t.Fatal(result.Err)
	}

	select {
	case materialized := <-probe.calls:
		if materialized.State.MaterializedThroughSeq != prepared.Obligation.ChangeSeq || materialized.HighWatermark != prepared.Obligation.ChangeSeq {
			t.Fatalf("post-commit materialization=%#v seq=%d", materialized, prepared.Obligation.ChangeSeq)
		}
	case <-time.After(time.Second):
		t.Fatal("post-commit observation was not materialized without inspect.events")
	}
	state, err := store.LoadEventProjectionState(ctx)
	if err != nil || state.MaterializedThroughSeq != prepared.Obligation.ChangeSeq {
		t.Fatalf("projection state=%#v err=%v", state, err)
	}
}

type observationTransitionRetryProbe struct {
	mu       sync.Mutex
	pending  int
	failures int
	calls    chan struct{}
}

func (p *observationTransitionRetryProbe) arm(pending, failures int) {
	p.mu.Lock()
	p.pending = pending
	p.failures = failures
	p.mu.Unlock()
}

func (p *observationTransitionRetryProbe) RetryObservationTransitions(context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case p.calls <- struct{}{}:
	default:
	}
	if p.pending == 0 {
		return 0, nil
	}
	if p.failures > 0 {
		p.failures--
		return p.pending, errors.New("injected observation transition retry failure")
	}
	p.pending = 0
	return 0, nil
}

type observationRetryTimerRequest struct {
	delay time.Duration
	ch    chan time.Time
}

func TestExecutionObservationMaterializerRetriesQueuedTransitionWithBackoff(t *testing.T) {
	materialCalls := make(chan struct{}, 8)
	material := &materializerWakeProbe{calls: materialCalls}
	materialWakeups := make(chan struct{}, 1)
	transitionWakeups := make(chan struct{}, 1)
	retries := &observationTransitionRetryProbe{calls: make(chan struct{}, 8)}
	timers := make(chan observationRetryTimerRequest, 4)
	runtime := &executionObservationRuntime{
		material:               material,
		wakeups:                materialWakeups,
		transitionRetries:      retries,
		transitionRetryWakeups: transitionWakeups,
		after: func(delay time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			timers <- observationRetryTimerRequest{delay: delay, ch: ch}
			return ch
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime.startMaterialization(ctx)

	waitTransitionRetryCall(t, retries.calls, "initial retry scan")
	waitMaterializerCall(t, materialCalls, "initial materialization")

	retries.arm(1, 2)
	transitionWakeups <- struct{}{}
	waitTransitionRetryCall(t, retries.calls, "first failed retry")
	first := waitObservationRetryTimer(t, timers)
	if first.delay != observationTransitionRetryInitialPeriod {
		t.Fatalf("first retry delay=%s want=%s", first.delay, observationTransitionRetryInitialPeriod)
	}
	materialWakeups <- struct{}{}
	transitionWakeups <- struct{}{}
	select {
	case <-retries.calls:
		t.Fatal("transition retry bypassed backoff after an unrelated wakeup")
	case <-time.After(100 * time.Millisecond):
	}

	first.ch <- time.Now()
	waitTransitionRetryCall(t, retries.calls, "second failed retry")
	second := waitObservationRetryTimer(t, timers)
	if second.delay != 2*observationTransitionRetryInitialPeriod {
		t.Fatalf("second retry delay=%s want=%s", second.delay, 2*observationTransitionRetryInitialPeriod)
	}

	second.ch <- time.Now()
	waitTransitionRetryCall(t, retries.calls, "successful retry")
	retries.mu.Lock()
	pending := retries.pending
	retries.mu.Unlock()
	if pending != 0 {
		t.Fatalf("pending transitions=%d want=0", pending)
	}
}

func waitTransitionRetryCall(t *testing.T, calls <-chan struct{}, phase string) {
	t.Helper()
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatalf("%s did not run", phase)
	}
}

func waitObservationRetryTimer(t *testing.T, timers <-chan observationRetryTimerRequest) observationRetryTimerRequest {
	t.Helper()
	select {
	case timer := <-timers:
		return timer
	case <-time.After(time.Second):
		t.Fatal("observation transition retry did not schedule backoff")
		return observationRetryTimerRequest{}
	}
}
