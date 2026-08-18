//go:build linux || darwin

package localfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	evidenceapp "github.com/maemreyo/shellbeam/internal/app/evidence"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

func TestScopeMeasurerIgnoresOutsideScopeChangesAndDetectsDeclaredInputChanges(t *testing.T) {
	root, private := captureFixture(t)
	provider := New(private)
	view, err := provider.Capture(context.Background(), captureRequest(root, []string{"go.mod"}, core.DefaultCaptureLimits()))
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest, err := view.Manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	contentDigest, err := view.Manifest.ContentDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Discard(context.Background(), view); err != nil {
		t.Fatal(err)
	}
	scope := core.ProvenInputScope{
		SchemaVersion: core.ProvenInputScopeSchemaV1, RepoInputs: []string{"go.mod"},
		CaptureManifestSHA256: manifestDigest, CaptureContentSHA256: contentDigest,
		Provider:    core.ProviderIdentity{Provider: core.ProviderBubblewrap, Version: core.BubblewrapVersionV1, BinarySHA256: strings.Repeat("a", 64), RuntimeManifestSHA256: strings.Repeat("b", 64)},
		Toolchain:   core.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: strings.Repeat("c", 64)},
		Environment: core.EnvironmentFixedAllowlist, Stdin: core.StdinClosed, Network: core.NetworkOff,
		AmbientInputs: []core.AmbientInputClass{core.AmbientClock, core.AmbientRandomness},
	}
	measurer, err := NewScopeMeasurer(core.DefaultCaptureLimits())
	if err != nil {
		t.Fatal(err)
	}
	request := evidenceapp.ProvenScopeObservationRequest{WorkspaceID: "ws_01K00000000000000000000000", WorkspaceRoot: root, SourceGeneration: "gen_" + strings.Repeat("f", 64), Scope: scope}

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("outside scope changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := measurer.ObserveProvenScope(context.Background(), request)
	if err != nil || got != contentDigest {
		t.Fatalf("outside-scope mutation digest=%q want=%q err=%v", got, contentDigest, err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module changed.example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := measurer.ObserveProvenScope(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if changed == contentDigest {
		t.Fatal("declared input mutation preserved proven-scope digest")
	}
}

func TestScopeMeasurerFailsClosedOnUnsafeRootInvalidScopeAndCancellation(t *testing.T) {
	measurer, err := NewScopeMeasurer(core.DefaultCaptureLimits())
	if err != nil {
		t.Fatal(err)
	}
	scope := core.ProvenInputScope{SchemaVersion: 1}
	if _, err := measurer.ObserveProvenScope(context.Background(), evidenceapp.ProvenScopeObservationRequest{WorkspaceID: "ws_01K00000000000000000000000", WorkspaceRoot: "relative", SourceGeneration: "gen_" + strings.Repeat("a", 64), Scope: scope}); err == nil {
		t.Fatal("unsafe scope request accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := measurer.ObserveProvenScope(ctx, evidenceapp.ProvenScopeObservationRequest{}); err == nil {
		t.Fatal("canceled scope measurement accepted")
	}
}
