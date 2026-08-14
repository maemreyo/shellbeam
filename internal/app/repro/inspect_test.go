package repro

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/repro"
	"github.com/maemreyo/shellbeam/internal/core/session"
	structured "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestCreateCapturesReceiptProvenanceAndInputWithoutInventingEnvironment(t *testing.T) {
	repo := reproFixture(t)
	now := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	repo.receipt.State, repo.receipt.Outcome = session.Failed, session.Failure
	repo.receipt.InputAcceptedBytes, repo.receipt.InputDeliveredBytes = 7, 5
	repo.receipt.WorkspaceProvenance = receipt.NewWorkspaceProvenanceV2(
		receipt.WorkspaceBinding{RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000"},
		receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceFreshlySampled, Generation: "gen_" + strings.Repeat("4", 64), Quality: workspace.QualityFresh, ObservedAt: now},
		receipt.WorkspaceObservationRef{Kind: receipt.WorkspaceFreshlySampled, Generation: "gen_" + strings.Repeat("5", 64), Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Second)}, true,
	)
	repo.telemetry, repo.telemetryFound = telemetryFixtureForRepro(t, repo.receipt), true
	repo.telemetry.RepositoryID = "repo_01K00000000000000000000000"
	repo.telemetry.WorkspaceID = "ws_01K00000000000000000000000"
	repo.telemetry.SourceContentDigest = strings.Repeat("6", 64)
	repo.telemetry.EnvironmentFingerprint, repo.telemetry.EnvironmentSchemaVersion = strings.Repeat("7", 64), 1
	repo.telemetry.ToolchainFingerprint, repo.telemetry.ToolchainSchemaVersion = strings.Repeat("8", 64), 1
	repo.telemetry.ProjectCommandID = "test_full"
	repo.telemetry.ParameterBindingFingerprint = strings.Repeat("9", 64)
	if err := repo.telemetry.Validate(); err != nil {
		t.Fatal(err)
	}

	capsule, err := New(repo).Create(context.Background(), core.CreateRequest{CreateID: "repro-provenance-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	if capsule.Source.RepositoryID != "repo_01K00000000000000000000000" || capsule.Source.WorkspaceID != "ws_01K00000000000000000000000" || capsule.Source.WorkspaceGeneration != "gen_"+strings.Repeat("5", 64) || capsule.Source.SourceContentDigest != strings.Repeat("6", 64) || capsule.Source.VCSStateDigest != "" || capsule.Source.Quality != core.CapturePartial {
		t.Fatalf("source=%#v", capsule.Source)
	}
	if capsule.Environment.EnvironmentFingerprint != strings.Repeat("7", 64) || capsule.Environment.EnvironmentQuality != core.CaptureExact || capsule.Environment.ToolchainFingerprint != strings.Repeat("8", 64) || capsule.Environment.ToolchainQuality != core.CaptureExact {
		t.Fatalf("environment=%#v", capsule.Environment)
	}
	if capsule.Input.AcceptedBytes != 7 || capsule.Input.DeliveredBytes != 5 || capsule.Input.Complete || capsule.Input.ContentIdentity != core.CaptureUnavailable {
		t.Fatalf("input=%#v", capsule.Input)
	}
	if capsule.Execution.ProjectCommandID != "test_full" || capsule.Execution.ParameterBindingFingerprint != strings.Repeat("9", 64) {
		t.Fatalf("project command=%#v", capsule.Execution)
	}
}

func TestInspectPreservesCreationDescriptorsWhileResolutionChanges(t *testing.T) {
	repo := reproFixture(t)
	repo.structured, repo.structuredFound = structuredFixture(t, structured.LifecycleTerminal, structured.CompletenessComplete), true
	repo.telemetry, repo.telemetryFound = telemetryFixtureForRepro(t, repo.receipt), true
	svc := New(repo)
	capsule, err := svc.Create(context.Background(), core.CreateRequest{CreateID: "repro-inspect-1", OperationID: "op-repro-1", Policy: core.CapturePolicy{DependentDerivations: core.CaptureCurrent}})
	if err != nil {
		t.Fatal(err)
	}
	original := append([]core.ReferenceDescriptor(nil), capsule.Results...)
	repo.structured.Completeness = structured.CompletenessCompacted
	repo.telemetryFound = false

	got, err := svc.Inspect(context.Background(), capsule.ReproID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Capsule.Results, original) {
		t.Fatalf("immutable descriptors changed\nwant=%#v\ngot=%#v", original, got.Capsule.Results)
	}
	states := map[string]core.ResolutionState{}
	for _, ref := range got.References {
		states[ref.RecordKind] = ref.ResolutionState
	}
	if states["structured_result"] != core.ResolutionCompacted || states["execution_telemetry"] != core.ResolutionPurged {
		t.Fatalf("resolution states=%#v", states)
	}
}
