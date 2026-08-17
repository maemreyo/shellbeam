package inputtrace

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

type materializeRepo struct {
	reservation operation.Reservation
	receipt     receipt.Receipt
	record      *core.Record
	putErr      error
	puts        int
}

func (r *materializeRepo) LoadOperation(context.Context, operation.ID) (operation.Reservation, error) {
	return r.reservation, nil
}
func (r *materializeRepo) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	return r.receipt, nil
}
func (r *materializeRepo) PutInputTraceRecord(_ context.Context, v core.Record) error {
	r.puts++
	if r.putErr != nil {
		return r.putErr
	}
	copy := v
	r.record = &copy
	return nil
}
func (r *materializeRepo) LoadInputTraceByOperation(_ context.Context, id string) (core.Record, bool, error) {
	if r.record != nil && r.record.OperationID == id {
		return *r.record, true, nil
	}
	return core.Record{}, false, nil
}

type materializeProvider struct {
	snapshot            ProviderSnapshot
	finalErr            error
	finalizes, cleanups int
	cleanupAfterPuts    *int
}

func (p *materializeProvider) Finalize(context.Context, core.InstrumentationBinding) (ProviderSnapshot, error) {
	p.finalizes++
	return p.snapshot, p.finalErr
}
func (p *materializeProvider) Cleanup(context.Context, core.InstrumentationBinding) error {
	p.cleanups++
	if p.cleanupAfterPuts != nil && *p.cleanupAfterPuts == 0 {
		return errors.New("cleanup before durable put")
	}
	return nil
}

type materializeWorkspace struct{ root string }

func (w materializeWorkspace) ResolveInputTraceWorkspace(context.Context, string) (string, error) {
	return w.root, nil
}

func TestE27InputTraceMaterializeRequiresMatchingDurableTerminalAuthority(t *testing.T) {
	repo, provider, scheduled := materializeFixture(t)
	bad := scheduled
	bad.OutputBytes++
	_, err := NewMaterializer(repo, provider, materializeWorkspace{root: t.TempDir()}).MaterializeTerminal(context.Background(), bad)
	if err == nil || provider.finalizes != 0 || repo.puts != 0 {
		t.Fatalf("err=%v finalizes=%d puts=%d", err, provider.finalizes, repo.puts)
	}
}

func TestE27InputTraceMaterializePersistsAdvisoryRecordThenCleansPrivateState(t *testing.T) {
	root := t.TempDir()
	inside := root + "/dep.txt"
	if err := os.WriteFile(inside, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, provider, scheduled := materializeFixture(t)
	provider.cleanupAfterPuts = &repo.puts
	provider.snapshot = ProviderSnapshot{TraceID: repo.reservation.Trace.TraceID, CaptureStart: time.Now().Add(-time.Second), CaptureEnd: time.Now(), Coverage: repo.reservation.Trace.Coverage, Resources: []ProviderResource{{ObservationClass: core.ClassFilesystemReads, Path: inside}}}
	got, err := NewMaterializer(repo, provider, materializeWorkspace{root: root}).MaterializeTerminal(context.Background(), scheduled)
	if err != nil {
		t.Fatal(err)
	}
	if got.Authority != core.AuthorityAdvisory || got.ScopeKind != core.ScopeObservedInput || !got.MayHaveUnobservedDependencies || got.Outcome != core.OutcomePartial || len(got.Resources) != 1 || got.Resources[0].Identity != "dep.txt" {
		t.Fatalf("record=%#v", got)
	}
	if repo.puts != 1 || provider.finalizes != 1 || provider.cleanups != 1 {
		t.Fatalf("puts=%d finalizes=%d cleanups=%d", repo.puts, provider.finalizes, provider.cleanups)
	}
	again, err := NewMaterializer(repo, provider, materializeWorkspace{root: root}).MaterializeTerminal(context.Background(), scheduled)
	if err != nil || again.DerivationKey != got.DerivationKey || provider.finalizes != 1 || provider.cleanups != 2 {
		t.Fatalf("again=%#v err=%v finalizes=%d cleanups=%d", again, err, provider.finalizes, provider.cleanups)
	}
}

func TestE27InputTraceMaterializePutFailureKeepsPrivateStateForRetry(t *testing.T) {
	repo, provider, scheduled := materializeFixture(t)
	repo.putErr = errors.New("store down")
	_, err := NewMaterializer(repo, provider, nil).MaterializeTerminal(context.Background(), scheduled)
	if err == nil || provider.finalizes != 1 || provider.cleanups != 0 {
		t.Fatalf("err=%v finalizes=%d cleanups=%d", err, provider.finalizes, provider.cleanups)
	}
}

func TestE27InputTraceProviderLossPersistsUnavailableWithoutChangingReceipt(t *testing.T) {
	repo, provider, scheduled := materializeFixture(t)
	provider.finalErr = errors.New("collector gone")
	before := repo.receipt
	got, err := NewMaterializer(repo, provider, nil).MaterializeTerminal(context.Background(), scheduled)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != core.OutcomeUnavailable || got.Truncated || len(got.Resources) != 0 || repo.receipt != before || provider.cleanups != 1 {
		t.Fatalf("record=%#v receipt_changed=%v cleanups=%d", got, repo.receipt != before, provider.cleanups)
	}
}

func materializeFixture(t *testing.T) (*materializeRepo, *materializeProvider, receipt.Receipt) {
	t.Helper()
	zero := 0
	binding := serviceTraceBinding()
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: "e27-materialize", SessionID: "e27-materialize-session", WorkspaceID: "ws_01K00000000000000000000000", RequestFingerprint: strings.Repeat("1", 64), ExecutionFingerprint: strings.Repeat("2", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: "/tmp", Shell: "/bin/sh", DaemonIncarnation: "d", Trace: &binding, CreatedAt: time.Now().Add(-time.Second)}
	rec := receipt.Receipt{SchemaVersion: 2, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, DaemonIncarnation: "d", ExecutionMode: "shell", Executable: "/bin/sh", State: session.Completed, Outcome: session.Success, Shell: "/bin/sh", CWD: "/tmp", OutputComplete: true, StdinClosed: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	if err := rec.Validate(); err != nil {
		t.Fatal(err)
	}
	return &materializeRepo{reservation: reservation, receipt: rec}, &materializeProvider{}, rec
}

func TestE27InputTraceValidSnapshotHonorsFrozenPreExecCoverage(t *testing.T) {
	complete := core.CoverageMatrix{
		FilesystemReads: core.CoverageCompleteForOwnedTree, FilesystemMetadataQueries: core.CoverageCompleteForOwnedTree,
		DirectoryEnumerations: core.CoverageCompleteForOwnedTree, FilesystemWrites: core.CoverageCompleteForOwnedTree,
		ExecutedBinaries: core.CoverageCompleteForOwnedTree, LoadedLibraries: core.CoverageCompleteForOwnedTree,
		EnvironmentNamesObserved: core.CoverageCompleteForOwnedTree, NetworkAttempts: core.CoverageCompleteForOwnedTree,
		ChildProcesses: core.CoverageCompleteForOwnedTree,
	}
	binding := serviceTraceBinding()
	binding.Mode = core.ModeRequired
	binding.PreExecCoverageEstablished = true
	binding.Coverage = complete
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	snapshot := ProviderSnapshot{TraceID: binding.TraceID, CaptureStart: now.Add(-time.Second), CaptureEnd: now, Coverage: complete}
	if !validSnapshot(binding, snapshot) {
		t.Fatal("valid pre-exec complete snapshot was rejected")
	}
	binding.PreExecCoverageEstablished = false
	if validSnapshot(binding, snapshot) {
		t.Fatal("complete snapshot accepted without frozen pre-exec coverage")
	}
}
