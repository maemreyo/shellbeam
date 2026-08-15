package evidence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type serviceRepo struct {
	reservation operation.Reservation
	terminal    receipt.Receipt
	snapshot    session.Snapshot
	workspaces  []workspace.Workspace
	records     []core.Record
}

func (r *serviceRepo) LoadOperation(context.Context, operation.ID) (operation.Reservation, error) {
	return r.reservation, nil
}
func (r *serviceRepo) LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error) {
	return r.terminal, nil
}
func (r *serviceRepo) LoadSession(context.Context, operation.SessionID) (session.Snapshot, error) {
	return r.snapshot, nil
}
func (r *serviceRepo) ListWorkspaces(context.Context) ([]workspace.Workspace, error) {
	return append([]workspace.Workspace(nil), r.workspaces...), nil
}
func (r *serviceRepo) PutEvidenceRecord(_ context.Context, record core.Record) (bool, error) {
	r.records = append(r.records, record)
	return true, nil
}

func TestDeriveTerminalRequiredArtifactMissingFailsEvidenceWithoutRewritingReceipt(t *testing.T) {
	root := t.TempDir()
	workspaceID := "ws_01K00000000000000000000000"
	contract := &core.Contract{VerificationKind: core.VerificationBuild, SourceScope: core.SourceScopeFull, ExpectedOutputs: []project.Output{{Path: "dist/app", Kind: "file", Required: true}}}
	repo := rawEvidenceServiceRepo(t, root, workspaceID, contract)
	svc := NewService(repo, NewObserver(DefaultLimits()))

	record, created, err := svc.DeriveTerminal(context.Background(), repo.terminal)
	if err != nil {
		t.Fatal(err)
	}
	if !created || record.Result != core.ResultFail || record.VerificationKind != core.VerificationBuild || len(record.Artifacts) != 1 || record.Artifacts[0].Status != core.ArtifactMissing {
		t.Fatalf("record=%#v created=%v", record, created)
	}
	if repo.terminal.Outcome != session.Success || repo.terminal.State != session.Completed || repo.terminal.Exit.Code == nil || *repo.terminal.Exit.Code != 0 {
		t.Fatalf("receipt rewritten: %#v", repo.terminal)
	}
	if record.Source.ObservationQuality != core.SourceQualityFast || record.Source.SourceContentDigest != "" || record.Source.VCSStateDigest != "" {
		t.Fatalf("source overclaim=%#v", record.Source)
	}
	if len(repo.records) != 1 || repo.records[0].EvidenceID != record.EvidenceID {
		t.Fatalf("records=%#v", repo.records)
	}
}

func TestDeriveTerminalRejectsScheduledReceiptThatDiffersFromDurableAuthority(t *testing.T) {
	repo := rawEvidenceServiceRepo(t, t.TempDir(), "", &core.Contract{VerificationKind: core.VerificationTest})
	svc := NewService(repo, NewObserver(DefaultLimits()))
	scheduled := repo.terminal
	scheduled.CWD = "/different"
	if _, _, err := svc.DeriveTerminal(context.Background(), scheduled); err == nil {
		t.Fatal("mismatched scheduled receipt accepted")
	}
	if len(repo.records) != 0 {
		t.Fatalf("records=%#v", repo.records)
	}
}

func TestDeriveTerminalUsesPersistedDeclaredIntentWhenExplicitContractAbsent(t *testing.T) {
	repo := rawEvidenceServiceRepo(t, t.TempDir(), "", nil)
	repo.reservation.Intent = &operation.DeclaredIntent{Kind: operation.IntentKindTest}
	svc := NewService(repo, NewObserver(DefaultLimits()))
	record, created, err := svc.DeriveTerminal(context.Background(), repo.terminal)
	if err != nil {
		t.Fatal(err)
	}
	if !created || record.VerificationKind != core.VerificationTest || record.Result != core.ResultPass {
		t.Fatalf("record=%#v created=%v", record, created)
	}
}

func TestContractFromReservationUsesFrozenTypedV2AndRejectsLegacyV1Reconstruction(t *testing.T) {
	binding := validProjectBinding(t, project.BindingSchemaVersion)
	binding.Kind = "test"
	binding.SourceScope = "full"
	binding.ExpectedOutputs = []project.Output{{Path: "dist/report.json", Kind: "file", Required: true}}
	contract, ok, err := contractFromReservation(operation.Reservation{SchemaVersion: 3, ProjectCommand: &binding})
	if err != nil || !ok || contract.VerificationKind != core.VerificationTest || len(contract.ExpectedOutputs) != 1 {
		t.Fatalf("contract=%#v ok=%v err=%v", contract, ok, err)
	}

	legacy := validProjectBinding(t, project.BindingSchemaV1)
	if _, ok, err := contractFromReservation(operation.Reservation{SchemaVersion: 3, ProjectCommand: &legacy}); err != nil || ok {
		t.Fatalf("legacy reconstructed: ok=%v err=%v", ok, err)
	}
}

func rawEvidenceServiceRepo(t *testing.T, root, workspaceID string, contract *core.Contract) *serviceRepo {
	t.Helper()
	zero := 0
	now := time.Now().UTC()
	fp := strings.Repeat("a", 64)
	reservation := operation.Reservation{SchemaVersion: 2, OperationID: "evidence-op", SessionID: "evidence-session", WorkspaceID: workspaceID, RequestFingerprint: fp, ExecutionFingerprint: strings.Repeat("b", 64), ObservationBindingFingerprint: strings.Repeat("c", 64), ExecutionMode: operation.ExecutionModeShell, Executable: "/bin/sh", Command: "true", CWD: root, Shell: "/bin/sh", Evidence: contract, CreatedAt: now}
	terminal := receipt.Receipt{SchemaVersion: 2, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), RequestFingerprint: reservation.RequestFingerprint, ExecutionFingerprint: reservation.ExecutionFingerprint, ObservationBindingFingerprint: reservation.ObservationBindingFingerprint, DaemonIncarnation: "daemon", ExecutionMode: string(operation.ExecutionModeShell), Executable: "/bin/sh", Shell: "/bin/sh", CWD: root, State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}, Evidence: contract}
	workspaces := []workspace.Workspace(nil)
	if workspaceID != "" {
		generation := "gen_" + strings.Repeat("d", 64)
		terminal.WorkspaceProvenance = &receipt.WorkspaceProvenance{SchemaVersion: 1, RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: workspace.WorkspaceID(workspaceID), PreGeneration: generation, PostGeneration: generation, PreQuality: workspace.QualityFresh, PostQuality: workspace.QualityFresh, PreObservedAt: now.Add(-time.Millisecond), PostObservedAt: now, ObservedChange: false}
		gitDir := filepath.Join(root, ".git")
		if err := os.MkdirAll(gitDir, 0o700); err != nil {
			t.Fatal(err)
		}
		workspaces = append(workspaces, workspace.Workspace{SchemaVersion: workspace.SchemaVersion, ID: workspace.WorkspaceID(workspaceID), RepositoryID: workspace.RepositoryID("repo_01K00000000000000000000000"), Label: "test", Root: root, GitDir: gitDir, CreatedAt: now.Add(-time.Hour), LastSeenAt: now})
	}
	return &serviceRepo{reservation: reservation, terminal: terminal, snapshot: session.Snapshot{SchemaVersion: 1, OperationID: string(reservation.OperationID), SessionID: string(reservation.SessionID), DaemonIncarnation: "daemon", State: session.Completed, Outcome: session.Success, OutputAvailable: true, UpdatedAt: now}, workspaces: workspaces}
}

func validProjectBinding(t *testing.T, version int) project.CommandBinding {
	t.Helper()
	params := []project.ParameterBinding{}
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	return project.CommandBinding{SchemaVersion: version, ManifestDigest: strings.Repeat("e", 64), ManifestSchemaVersion: project.ManifestSchemaV2, CommandID: "verify", ParameterFingerprint: fingerprint, Parameters: params, ResolvedArgv: []string{"go", "test", "./..."}, LogicalCWD: ".", ResolvedCWD: "/repo"}
}

func TestDeriveTerminalCopiesOnlyFrozenEnvironmentBinding(t *testing.T) {
	repo := rawEvidenceServiceRepo(t, t.TempDir(), "", &core.Contract{VerificationKind: core.VerificationTest})
	binding := environment.Binding{
		SnapshotID:                    "env_" + strings.Repeat("a", 64),
		EnvironmentFingerprint:        strings.Repeat("b", 64),
		EnvironmentFingerprintVersion: environment.FingerprintVersion,
		CapturedAt:                    time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	repo.reservation.EnvironmentBinding = &binding
	record, created, err := NewService(repo, NewObserver(DefaultLimits())).DeriveTerminal(context.Background(), repo.terminal)
	if err != nil {
		t.Fatal(err)
	}
	if !created || record.EnvironmentBinding == nil || *record.EnvironmentBinding != binding {
		t.Fatalf("record environment binding=%#v created=%v", record.EnvironmentBinding, created)
	}
	if len(repo.records) != 1 || repo.records[0].EnvironmentBinding == nil || *repo.records[0].EnvironmentBinding != binding {
		t.Fatalf("persisted record=%#v", repo.records)
	}

	repo.reservation.EnvironmentBinding.EnvironmentFingerprint = strings.Repeat("c", 64)
	if record.EnvironmentBinding.EnvironmentFingerprint != strings.Repeat("b", 64) {
		t.Fatal("derived evidence aliased mutable reservation binding")
	}
}

func TestDeriveTerminalRejectsMalformedFrozenEnvironmentBinding(t *testing.T) {
	repo := rawEvidenceServiceRepo(t, t.TempDir(), "", &core.Contract{VerificationKind: core.VerificationTest})
	repo.reservation.EnvironmentBinding = &environment.Binding{
		SnapshotID:                    "env_" + strings.Repeat("a", 64),
		EnvironmentFingerprint:        strings.Repeat("b", 64),
		EnvironmentFingerprintVersion: environment.FingerprintVersion + 1,
		CapturedAt:                    time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
	}
	if _, created, err := NewService(repo, NewObserver(DefaultLimits())).DeriveTerminal(context.Background(), repo.terminal); err == nil || created {
		t.Fatalf("malformed frozen environment binding accepted: created=%v err=%v", created, err)
	}
	if len(repo.records) != 0 {
		t.Fatalf("malformed binding persisted evidence: %#v", repo.records)
	}
}
