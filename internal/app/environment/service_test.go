package environment

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type fakeHostObserver struct {
	calls int
	names [][]string
}

func (f *fakeHostObserver) Observe(_ context.Context, execution core.ExecutionContext, names []string) (core.FingerprintInput, error) {
	f.calls++
	f.names = append(f.names, append([]string(nil), names...))
	presence := make([]core.VariablePresence, 0, len(names))
	for _, name := range names {
		presence = append(presence, core.VariablePresence{Name: name, Present: name != "MISSING"})
	}
	return core.FingerprintInput{
		Platform:         core.Platform{OS: "darwin", Architecture: "arm64"},
		Execution:        execution,
		Path:             core.PathFingerprint("/bin:/usr/bin"),
		VariablePresence: presence,
	}, nil
}

type fakeManifestProvider struct {
	calls int
	view  ManifestView
}

func (f *fakeManifestProvider) Manifest(_ context.Context, workspaceID string) (ManifestView, error) {
	f.calls++
	view := f.view
	view.WorkspaceID = workspaceID
	return view, nil
}

type fakeToolchainProber struct {
	calls       []ToolchainRequest
	version     string
	unsupported map[string]bool
}

func (f *fakeToolchainProber) Probe(_ context.Context, kind, requestedIdentity string, declaration project.Toolchain) core.ToolchainObservation {
	request := ToolchainRequest{Kind: kind, RequestedIdentity: requestedIdentity, Declaration: declaration}
	f.calls = append(f.calls, request)
	if f.unsupported[kind] {
		return core.ToolchainObservation{Kind: kind, RequestedIdentity: requestedIdentity, Quality: core.ProbeUnavailable, DiagnosticCode: "toolchain_probe_unsupported"}
	}
	version := f.version
	if version == "" {
		version = "1.0"
	}
	return core.ToolchainObservation{
		Kind: request.Kind, RequestedIdentity: request.RequestedIdentity,
		ObservedIdentity: "/usr/bin/" + request.Kind, Version: version, Quality: core.ProbeComplete,
	}
}

func TestInspectCachesRefreshesAndUsesManifestSelections(t *testing.T) {
	host := &fakeHostObserver{}
	manifest := &fakeManifestProvider{view: ManifestView{
		ManifestDigest: strings.Repeat("a", 64),
		Manifest: project.Manifest{
			SchemaVersion:       project.ManifestSchemaV2,
			RelevantEnvironment: []string{"DATABASE_URL", "CI"},
			Toolchains: map[string]project.Toolchain{
				"go": {Version: "1.26", Manager: "asdf"},
			},
		},
	}}
	prober := &fakeToolchainProber{}
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	execution := core.ExecutionContext{Mode: "shell", Identity: "/opt/homebrew/bin/fish"}
	svc := NewService(host, manifest, prober, Options{MaxEntries: 4, Now: func() time.Time { return now }, DefaultExecution: execution})

	first, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_1"})
	if err != nil {
		t.Fatal(err)
	}
	if host.calls != 1 || manifest.calls != 1 || len(prober.calls) != len(core.SupportedToolchains()) {
		t.Fatalf("first calls host=%d manifest=%d probes=%d", host.calls, manifest.calls, len(prober.calls))
	}
	if !reflect.DeepEqual(host.names[0], []string{"CI", "DATABASE_URL", "SHELL", "TERM"}) {
		t.Fatalf("selected names=%v", host.names[0])
	}
	if first.ToolchainManager == nil || first.ToolchainManager.Kind != "declared" || first.ToolchainManager.Identity != "go=asdf" {
		t.Fatalf("manager=%#v", first.ToolchainManager)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("invalid first snapshot: %v", err)
	}

	second, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_1", Freshness: core.FreshnessCached})
	if err != nil {
		t.Fatal(err)
	}
	if second.SnapshotID != first.SnapshotID || host.calls != 1 || manifest.calls != 2 || len(prober.calls) != len(core.SupportedToolchains()) {
		t.Fatalf("cache miss second=%#v calls host=%d manifest=%d probes=%d", second, host.calls, manifest.calls, len(prober.calls))
	}

	now = now.Add(time.Second)
	refreshed, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_1", Freshness: core.FreshnessRefresh})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.SnapshotID == first.SnapshotID || refreshed.EnvironmentFingerprint != first.EnvironmentFingerprint || host.calls != 2 || len(prober.calls) != 2*len(core.SupportedToolchains()) {
		t.Fatalf("refresh=%#v calls=%d probes=%d", refreshed, host.calls, len(prober.calls))
	}
}

func TestCacheKeyChangesAndCachedBindingNeverObserves(t *testing.T) {
	host := &fakeHostObserver{}
	manifest := &fakeManifestProvider{view: ManifestView{
		ManifestDigest: strings.Repeat("b", 64),
		Manifest:       project.Manifest{SchemaVersion: project.ManifestSchemaV2, RelevantEnvironment: []string{"A"}},
	}}
	prober := &fakeToolchainProber{}
	execution := core.ExecutionContext{Mode: "argv", Identity: "/usr/bin/env"}
	now := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	svc := NewService(host, manifest, prober, Options{MaxEntries: 2, Now: func() time.Time { now = now.Add(time.Nanosecond); return now }, DefaultExecution: execution})

	first, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_2"})
	if err != nil {
		t.Fatal(err)
	}
	hostCalls, manifestCalls, probeCalls := host.calls, manifest.calls, len(prober.calls)
	binding, ok := svc.CachedBinding(BindingRequest{WorkspaceID: "ws_2", ManifestDigest: manifest.view.ManifestDigest, Execution: execution})
	if !ok || binding.SnapshotID != first.SnapshotID {
		t.Fatalf("cached binding=%#v ok=%v", binding, ok)
	}
	if host.calls != hostCalls || manifest.calls != manifestCalls || len(prober.calls) != probeCalls {
		t.Fatal("CachedBinding performed observation work")
	}

	manifest.view.ManifestDigest = strings.Repeat("c", 64)
	manifest.view.Manifest.RelevantEnvironment = []string{"B"}
	second, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_2"})
	if err != nil {
		t.Fatal(err)
	}
	if second.SnapshotID == first.SnapshotID || host.calls != hostCalls+1 {
		t.Fatalf("manifest change reused cache: %#v", second)
	}
	if _, ok := svc.CachedBinding(BindingRequest{WorkspaceID: "ws_2", ManifestDigest: strings.Repeat("b", 64), Execution: execution}); !ok {
		t.Fatal("compatible historical cache entry unexpectedly absent before eviction")
	}

	thirdExecution := core.ExecutionContext{Mode: "argv", Identity: "/usr/local/bin/env"}
	if _, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_2", Execution: &thirdExecution}); err != nil {
		t.Fatal(err)
	}
	if svc.CacheSize() != 2 {
		t.Fatalf("bounded cache size=%d", svc.CacheSize())
	}
	if _, ok := svc.CachedBinding(BindingRequest{WorkspaceID: "ws_2", ManifestDigest: strings.Repeat("b", 64), Execution: execution}); ok {
		t.Fatal("oldest cache entry survived bounded eviction")
	}
}

func TestInspectRepresentsUnsupportedDeclaredToolchain(t *testing.T) {
	host := &fakeHostObserver{}
	manifest := &fakeManifestProvider{view: ManifestView{
		ManifestDigest: strings.Repeat("d", 64),
		Manifest: project.Manifest{
			SchemaVersion: project.ManifestSchemaV2,
			Toolchains: map[string]project.Toolchain{
				"ruby": {Version: "3.4"},
			},
		},
	}}
	prober := &fakeToolchainProber{unsupported: map[string]bool{"ruby": true}}
	svc := NewService(host, manifest, prober, Options{MaxEntries: 4, Now: func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }, DefaultExecution: core.ExecutionContext{Mode: "shell", Identity: "/bin/sh"}})
	got, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_unsupported"})
	if err != nil {
		t.Fatal(err)
	}
	var ruby *core.ToolchainObservation
	for i := range got.Toolchains {
		if got.Toolchains[i].Kind == "ruby" {
			ruby = &got.Toolchains[i]
			break
		}
	}
	if ruby == nil || ruby.Quality != core.ProbeUnavailable || ruby.DiagnosticCode != "toolchain_probe_unsupported" || ruby.ObservedIdentity != "" || ruby.Version != "" {
		t.Fatalf("unsupported declaration not represented safely: %#v", ruby)
	}
	if got.Quality != core.QualityPartial {
		t.Fatalf("snapshot quality=%q", got.Quality)
	}
}

func TestRefreshChangesToolchainFingerprintWhenNormalizedVersionChanges(t *testing.T) {
	host := &fakeHostObserver{}
	manifest := &fakeManifestProvider{view: ManifestView{ManifestDigest: strings.Repeat("e", 64), Manifest: project.Manifest{SchemaVersion: project.ManifestSchemaV2}}}
	prober := &fakeToolchainProber{version: "1.0"}
	now := time.Date(2026, 8, 15, 12, 30, 0, 0, time.UTC)
	svc := NewService(host, manifest, prober, Options{MaxEntries: 4, Now: func() time.Time { now = now.Add(time.Nanosecond); return now }, DefaultExecution: core.ExecutionContext{Mode: "shell", Identity: "/bin/sh"}})
	first, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_versions"})
	if err != nil {
		t.Fatal(err)
	}
	prober.version = "2.0"
	second, err := svc.Inspect(context.Background(), InspectRequest{WorkspaceID: "ws_versions", Freshness: core.FreshnessRefresh})
	if err != nil {
		t.Fatal(err)
	}
	if first.ToolchainFingerprint == second.ToolchainFingerprint {
		t.Fatal("normalized toolchain version change did not change fingerprint")
	}
}
