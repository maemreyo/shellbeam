package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

type fakeCurrentEnvironmentInspector struct {
	request  environmentapp.InspectRequest
	snapshot environment.Snapshot
	err      error
}

func (f *fakeCurrentEnvironmentInspector) Inspect(_ context.Context, request environmentapp.InspectRequest) (environment.Snapshot, error) {
	f.request = request
	return f.snapshot, f.err
}

func currentEnvironmentSnapshot() environment.Snapshot {
	return environment.Snapshot{
		SchemaVersion: environment.SnapshotSchemaVersion, SnapshotID: "env_" + strings.Repeat("a", 64), CapturedAt: time.Unix(1, 0).UTC(), Quality: environment.QualityComplete,
		EnvironmentFingerprint: strings.Repeat("b", 64), FingerprintVersion: environment.FingerprintVersion,
		ToolchainFingerprint: strings.Repeat("c", 64), ToolchainFingerprintVersion: environment.ToolchainFingerprintVersion,
		Platform: environment.Platform{OS: "darwin", Architecture: "arm64"}, Execution: environment.ExecutionContext{Mode: "shell", Identity: "/bin/zsh"},
		Path:       environment.PathObservation{Digest: strings.Repeat("d", 64), EntryCount: 2, Quality: environment.QualityComplete},
		Toolchains: []environment.ToolchainObservation{{Kind: "go", RequestedIdentity: "1.26", ObservedIdentity: "/usr/bin/go", Version: "1.26.5", Quality: environment.ProbeComplete}},
	}
}

func TestEnvironmentSourceRefreshesAndReturnsOnlyValidatedBinding(t *testing.T) {
	fake := &fakeCurrentEnvironmentInspector{snapshot: currentEnvironmentSnapshot()}
	binding, ok, err := NewEnvironmentSource(fake).CurrentBinding(context.Background(), "ws_01K00000000000000000000000")
	if err != nil || !ok {
		t.Fatalf("binding=%#v ok=%v err=%v", binding, ok, err)
	}
	if fake.request.WorkspaceID != "ws_01K00000000000000000000000" || fake.request.Freshness != environment.FreshnessRefresh {
		t.Fatalf("inspect request=%#v", fake.request)
	}
	if binding.EnvironmentFingerprint != fake.snapshot.EnvironmentFingerprint || binding.ToolchainFingerprint != fake.snapshot.ToolchainFingerprint {
		t.Fatalf("binding=%#v snapshot=%#v", binding, fake.snapshot)
	}
}

func TestEnvironmentSourceDoesNotInventBindingWhenObservationUnavailableOrInvalid(t *testing.T) {
	unavailable := &fakeCurrentEnvironmentInspector{err: errors.New("environment unavailable")}
	if binding, ok, err := NewEnvironmentSource(unavailable).CurrentBinding(context.Background(), "ws_01K00000000000000000000000"); err == nil || ok || binding.EnvironmentFingerprint != "" {
		t.Fatalf("unavailable binding=%#v ok=%v err=%v", binding, ok, err)
	}
	invalid := currentEnvironmentSnapshot()
	invalid.EnvironmentFingerprint = "bad"
	if binding, ok, err := NewEnvironmentSource(&fakeCurrentEnvironmentInspector{snapshot: invalid}).CurrentBinding(context.Background(), "ws_01K00000000000000000000000"); err == nil || ok || binding.EnvironmentFingerprint != "" {
		t.Fatalf("invalid binding=%#v ok=%v err=%v", binding, ok, err)
	}
}
