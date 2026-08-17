//go:build linux || darwin

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type e27IntegrationProvider struct {
	snapshot traceapp.ProviderSnapshot
	err      error
	cleanups int
}

func (p *e27IntegrationProvider) Finalize(context.Context, trace.InstrumentationBinding) (traceapp.ProviderSnapshot, error) {
	return p.snapshot, p.err
}
func (p *e27IntegrationProvider) Cleanup(context.Context, trace.InstrumentationBinding) error {
	p.cleanups++
	return nil
}

type e27WorkspaceRoot struct{ root string }

func (w e27WorkspaceRoot) ResolveInputTraceWorkspace(context.Context, string) (string, error) {
	return w.root, nil
}

func TestE27InputTraceBroadeningPrivacyAndRestartGapIntegration(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	limits := storeadapter.Limits{MaxSessions: 8, MaxSessionOutput: 1 << 20, MaxTotalState: 8 << 20, ControlReserve: 4096}
	repository, err := storeadapter.Open(stateDir, limits)
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	dep := filepath.Join(workspaceRoot, "dep.txt")
	if err := os.WriteFile(dep, []byte("dep"), 0o600); err != nil {
		t.Fatal(err)
	}
	externalRoot := t.TempDir()
	external := filepath.Join(externalRoot, "LOWENTROPY-E27-INTEGRATION-SECRET.txt")
	if err := os.WriteFile(external, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	binding := e27IntegrationBinding("trace_01K00000000000000000000000")
	reservation, terminal := persistE27IntegrationOperation(t, repository, "e27-integration-broadening", "e27-integration-broadening-session", workspaceRoot, binding)
	beforeReceipt, err := repository.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	provider := &e27IntegrationProvider{snapshot: traceapp.ProviderSnapshot{
		TraceID: binding.TraceID, CaptureStart: now.Add(-time.Second), CaptureEnd: now, Coverage: binding.Coverage,
		Resources: []traceapp.ProviderResource{
			{ObservationClass: trace.ClassFilesystemReads, Path: dep},
			{ObservationClass: trace.ClassFilesystemReads, Path: external},
			{ObservationClass: trace.ClassExecutedBinaries, Path: "/usr/bin/true"},
		},
	}}
	materializer := traceapp.NewMaterializer(repository, provider, e27WorkspaceRoot{root: workspaceRoot})
	record, err := materializer.MaterializeTerminal(context.Background(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if record.Authority != trace.AuthorityAdvisory || record.ScopeKind != trace.ScopeObservedInput || !record.MayHaveUnobservedDependencies || record.Outcome != trace.OutcomePartial || provider.cleanups != 1 {
		t.Fatalf("broadening record=%#v cleanups=%d", record, provider.cleanups)
	}
	seen := map[string]bool{}
	for _, resource := range record.Resources {
		seen[string(resource.PathClass)+":"+resource.Identity] = true
	}
	if !seen["repo_relative:dep.txt"] || !seen["workspace_external_redacted:external-1"] || !seen["system_classified:usr"] {
		t.Fatalf("normalized resources=%#v", record.Resources)
	}
	afterReceipt, err := repository.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil || !reflect.DeepEqual(beforeReceipt, afterReceipt) {
		t.Fatalf("input trace rewrote child receipt before=%#v after=%#v err=%v", beforeReceipt, afterReceipt, err)
	}
	stored, found, err := repository.LoadInputTraceByOperation(context.Background(), string(reservation.OperationID))
	if err != nil || !found || stored.DerivationKey != record.DerivationKey {
		t.Fatalf("durable trace found=%v record=%#v err=%v", found, stored, err)
	}
	encoded, _ := json.Marshal(stored)
	for _, forbidden := range []string{externalRoot, external, "LOWENTROPY", "proven_input_scope", `"raw_events":`, "socket_path", "dylib_path"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("durable trace leaked/narrowed %q: %s", forbidden, encoded)
		}
	}
	assertE27IntegrationStatePrivacy(t, stateDir, []string{externalRoot, external, "LOWENTROPY-E27-INTEGRATION-SECRET"})

	assertE27RestartOwnershipGap(t, repository, stateDir, limits, workspaceRoot)
}

func assertE27RestartOwnershipGap(t *testing.T, repository *storeadapter.Repository, stateDir string, limits storeadapter.Limits, workspaceRoot string) {
	t.Helper()
	binding := e27IntegrationBinding("trace_01K00000000000000000000001")
	reservation, terminal := persistE27IntegrationOperation(t, repository, "e27-integration-gap", "e27-integration-gap-session", workspaceRoot, binding)
	restarted, err := storeadapter.Open(stateDir, limits)
	if err != nil {
		t.Fatal(err)
	}
	before, err := restarted.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	provider := &e27IntegrationProvider{err: failure.New(failure.InputTraceNotFound, map[string]string{"trace_id": binding.TraceID}, nil)}
	record, err := traceapp.NewMaterializer(restarted, provider, e27WorkspaceRoot{root: workspaceRoot}).MaterializeTerminal(context.Background(), terminal)
	if err != nil {
		t.Fatal(err)
	}
	if record.Outcome != trace.OutcomePartial || record.GapReason != trace.GapOwnershipLost || !record.MayHaveUnobservedDependencies || record.Authority != trace.AuthorityAdvisory {
		t.Fatalf("restart ownership gap=%#v", record)
	}
	after, err := restarted.LoadReceipt(context.Background(), reservation.SessionID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("restart trace gap rewrote child receipt before=%#v after=%#v err=%v", before, after, err)
	}
}

func persistE27IntegrationOperation(t *testing.T, repository *storeadapter.Repository, operationID string, sessionID operation.SessionID, cwd string, binding trace.InstrumentationBinding) (operation.Reservation, receipt.Receipt) {
	t.Helper()
	reservation := operation.Reservation{
		SchemaVersion: 2, OperationID: operation.ID(operationID), WorkspaceID: "ws_01K00000000000000000000000", LogicalCWD: ".", SessionID: sessionID,
		RequestFingerprint: strings.Repeat("1", 64), ExecutionFingerprint: strings.Repeat("2", 64), ExecutionMode: operation.ExecutionModeArgv,
		Executable: "/usr/bin/true", Argv: []string{"/usr/bin/true"}, CWD: cwd, DaemonIncarnation: "e27-integration", Trace: &binding, CreatedAt: time.Now().Add(-time.Second),
	}
	stored, created, result := repository.ReserveOperation(context.Background(), reservation)
	if result.Err != nil || !created {
		t.Fatalf("reserve created=%v stored=%#v result=%#v", created, stored, result)
	}
	zero := 0
	terminal := receipt.Receipt{
		SchemaVersion: 2, OperationID: operationID, SessionID: string(sessionID), RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint,
		DaemonIncarnation: reservation.DaemonIncarnation, ExecutionMode: string(operation.ExecutionModeArgv), Executable: reservation.Executable,
		CWD: cwd, State: session.Completed, Outcome: session.Success, OutputComplete: true, StdinClosed: true,
		Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
	}
	if err := terminal.Validate(); err != nil {
		t.Fatal(err)
	}
	if result := repository.PublishTerminal(context.Background(), terminal); result.Err != nil {
		t.Fatalf("publish terminal=%#v", result)
	}
	return stored, terminal
}

func e27IntegrationBinding(traceID string) trace.InstrumentationBinding {
	return trace.InstrumentationBinding{
		SchemaVersion: trace.SchemaVersion, TraceID: traceID, Mode: trace.ModeBestEffort, Status: trace.BindingActive,
		Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: trace.EffectEnvironmentAffecting,
		Coverage: trace.CoverageMatrix{
			FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial,
			FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial,
			EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial,
		},
	}
}

func assertE27IntegrationStatePrivacy(t *testing.T, root string, forbidden []string) {
	t.Helper()
	var data []byte
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			contents, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			data = append(data, contents...)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(data), value) {
			t.Fatalf("public durable state leaked %q", value)
		}
	}
}
