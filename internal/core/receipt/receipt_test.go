package receipt

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestSuccessRequiresCompleteEvidence(t *testing.T) {
	r := Receipt{SchemaVersion: 1, State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: true, Code: ptr(0)}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.InputAcceptedBytes = 1
	if err := r.Validate(); err == nil {
		t.Fatal("accepted input not delivered")
	}
}

func ptr(v int) *int { return &v }

func TestReceiptV2AcceptsLegacyAndLazyWorkspaceProvenance(t *testing.T) {
	now := time.Now().UTC()
	base := Receipt{SchemaVersion: 2, RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon", State: session.Running}
	legacyPre := provenanceSnapshot(t, strings.Repeat("d", 40), workspace.QualityFresh)
	legacyPost := provenanceSnapshot(t, strings.Repeat("e", 40), workspace.QualityCached)
	base.WorkspaceProvenance = NewWorkspaceProvenance(legacyPre, legacyPost)
	assertReceiptJSONValid(t, base)

	generation := "gen_" + strings.Repeat("b", 64)
	base.WorkspaceProvenance = NewWorkspaceProvenanceV2(
		WorkspaceBinding{WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000")},
		WorkspaceObservationRef{Kind: WorkspaceCached, Generation: generation, Quality: workspace.QualityCached, ObservedAt: now},
		WorkspaceObservationRef{Kind: WorkspaceUnreconciled, ObservationInvalidated: true},
		false,
	)
	assertReceiptJSONValid(t, base)
}

func TestReceiptV2RejectsImpossibleLazyObservedChange(t *testing.T) {
	now := time.Now().UTC()
	generation := "gen_" + strings.Repeat("c", 64)
	r := Receipt{
		SchemaVersion: 2, RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon", State: session.Running,
		WorkspaceProvenance: NewWorkspaceProvenanceV2(
			WorkspaceBinding{WorkspaceID: workspace.WorkspaceID("ws_01K00000000000000000000000")},
			WorkspaceObservationRef{Kind: WorkspaceFreshlySampled, Generation: generation, Quality: workspace.QualityFresh, ObservedAt: now},
			WorkspaceObservationRef{Kind: WorkspaceFreshlySampled, Generation: generation, Quality: workspace.QualityFresh, ObservedAt: now.Add(time.Millisecond)},
			true,
		),
	}
	if err := r.Validate(); err == nil {
		t.Fatal("impossible observed change accepted")
	}
}

func assertReceiptJSONValid(t *testing.T, want Receipt) {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Receipt
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("receipt=%s validation=%v", encoded, err)
	}
}

func TestReceiptV2PersistsRawEvidenceContractAndV3RejectsCompetingRawEvidence(t *testing.T) {
	contract := &evidence.Contract{VerificationKind: evidence.VerificationBuild, SourceScope: evidence.SourceScopeFull, ExpectedOutputs: []project.Output{{Path: "dist/app", Kind: "file", Required: true, Digest: "sha256"}}}
	v2 := Receipt{SchemaVersion: 2, RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon", State: session.Running, Evidence: contract}
	if err := v2.Validate(); err != nil {
		t.Fatalf("v2 evidence receipt rejected: %v", err)
	}
	v1 := Receipt{SchemaVersion: 1, State: session.Running, Evidence: contract}
	if err := v1.Validate(); err == nil {
		t.Fatal("v1 receipt accepted evidence contract")
	}
	v3 := Receipt{SchemaVersion: 3, RequestFingerprint: "request", ExecutionFingerprint: "execution", State: session.Running, Evidence: contract}
	if err := v3.Validate(); err == nil {
		t.Fatal("v3 typed receipt accepted competing raw evidence contract")
	}
}
