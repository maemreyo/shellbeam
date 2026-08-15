package project

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type recordingReadinessObserver struct {
	executableCalls  int
	environmentCalls int
	toolchainCalls   int
	executable       map[string]core.CheckStatus
	environment      map[string]core.CheckStatus
	toolchain        map[string]core.CheckStatus
	secret           string
}

func (r *recordingReadinessObserver) ObserveExecutable(_ context.Context, id string) core.ReadinessCheck {
	r.executableCalls++
	return core.ReadinessCheck{ID: id, Status: r.executable[id], Code: r.secret}
}

func (r *recordingReadinessObserver) ObserveEnvironmentPresence(_ context.Context, id string, required bool) core.ReadinessCheck {
	r.environmentCalls++
	return core.ReadinessCheck{ID: id, Required: required, Status: r.environment[id], Code: r.secret}
}

func (r *recordingReadinessObserver) ObserveToolchain(_ context.Context, _ string, id string, _ core.Toolchain) core.ReadinessCheck {
	r.toolchainCalls++
	return core.ReadinessCheck{ID: id, Status: r.toolchain[id], Code: r.secret, ProviderID: "go-host", ProviderVersion: 1}
}

func TestReadinessEvaluatesRequirementsCachesAndDoesNotLeakObserverValues(t *testing.T) {
	load := readinessV2Load(t)
	loader := &fakeLoader{result: load}
	observer := &recordingReadinessObserver{
		executable:  map[string]core.CheckStatus{"git": core.CheckAvailable, "docker": core.CheckMissing},
		environment: map[string]core.CheckStatus{"DATABASE_URL": core.CheckPresentNonEmpty, "AWS_PROFILE": core.CheckAbsent},
		toolchain:   map[string]core.CheckStatus{"go": core.CheckCompatible},
		secret:      "postgres://alice:secret@db/production",
	}
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	svc := NewWithReadiness(
		fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, loader, &fakeReviewStore{},
		ReadinessObservers{Executable: observer, Environment: observer, Toolchain: observer},
		ReadinessOptions{TTL: 30 * time.Second, MaxEntries: 4, Now: func() time.Time { return now }},
	)
	first, err := svc.Readiness(context.Background(), string(testProjectWorkspace().ID))
	if err != nil {
		t.Fatal(err)
	}
	if first.State != core.ReadinessReady || first.CacheQuality != core.CacheFresh || first.CacheAgeMS != 0 || len(first.Checks) != 5 {
		t.Fatalf("first=%#v", first)
	}
	if observer.executableCalls != 2 || observer.environmentCalls != 2 || observer.toolchainCalls != 1 {
		t.Fatalf("observer calls exec=%d env=%d toolchain=%d", observer.executableCalls, observer.environmentCalls, observer.toolchainCalls)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), observer.secret) {
		t.Fatalf("observer secret leaked: %s", encoded)
	}

	now = now.Add(5 * time.Second)
	second, err := svc.Readiness(context.Background(), string(testProjectWorkspace().ID))
	if err != nil {
		t.Fatal(err)
	}
	if second.CacheQuality != core.CacheCached || second.CacheAgeMS != 5000 || second.State != first.State {
		t.Fatalf("cached=%#v", second)
	}
	if observer.executableCalls != 2 || observer.environmentCalls != 2 || observer.toolchainCalls != 1 {
		t.Fatalf("cache re-probed observers exec=%d env=%d toolchain=%d", observer.executableCalls, observer.environmentCalls, observer.toolchainCalls)
	}

	now = now.Add(26 * time.Second)
	third, err := svc.Readiness(context.Background(), string(testProjectWorkspace().ID))
	if err != nil {
		t.Fatal(err)
	}
	if third.CacheQuality != core.CacheFresh || observer.executableCalls != 4 || observer.environmentCalls != 4 || observer.toolchainCalls != 2 {
		t.Fatalf("expired cache third=%#v calls=%#v", third, observer)
	}
	if loader.calls != 3 {
		t.Fatalf("explicit readiness must reload manifest for cache key: calls=%d", loader.calls)
	}
}

func TestReadinessV1OrNoRequirementsIsUnavailableWithoutHostProbes(t *testing.T) {
	observer := &recordingReadinessObserver{}
	for name, load := range map[string]core.LoadResult{
		"v1":       readinessV1Load(t),
		"v2 empty": readinessV2EmptyLoad(t),
	} {
		t.Run(name, func(t *testing.T) {
			svc := NewWithReadiness(
				fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, &fakeLoader{result: load}, nil,
				ReadinessObservers{Executable: observer, Environment: observer, Toolchain: observer}, ReadinessOptions{},
			)
			got, err := svc.Readiness(context.Background(), string(testProjectWorkspace().ID))
			if err != nil {
				t.Fatal(err)
			}
			if got.State != core.ReadinessUnavailable || len(got.Checks) != 0 {
				t.Fatalf("readiness=%#v", got)
			}
		})
	}
	if observer.executableCalls != 0 || observer.environmentCalls != 0 || observer.toolchainCalls != 0 {
		t.Fatalf("no-requirement readiness probed host: %#v", observer)
	}
}

func TestReadinessFoldPreservesRequiredFailureAndUnknown(t *testing.T) {
	for name, status := range map[string]core.CheckStatus{"known failure": core.CheckMissing, "unknown": core.CheckUnavailable} {
		t.Run(name, func(t *testing.T) {
			load := readinessV2Load(t)
			observer := &recordingReadinessObserver{
				executable:  map[string]core.CheckStatus{"git": status, "docker": core.CheckAvailable},
				environment: map[string]core.CheckStatus{"DATABASE_URL": core.CheckPresentNonEmpty, "AWS_PROFILE": core.CheckPresentNonEmpty},
				toolchain:   map[string]core.CheckStatus{"go": core.CheckCompatible},
			}
			svc := NewWithReadiness(fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, &fakeLoader{result: load}, nil,
				ReadinessObservers{Executable: observer, Environment: observer, Toolchain: observer}, ReadinessOptions{})
			got, err := svc.Readiness(context.Background(), string(testProjectWorkspace().ID))
			if err != nil {
				t.Fatal(err)
			}
			want := core.ReadinessNotReady
			if status == core.CheckUnavailable {
				want = core.ReadinessPartial
			}
			if got.State != want {
				t.Fatalf("status=%s readiness=%#v", status, got)
			}
		})
	}
}

func TestOrdinaryProjectInspectDoesNotProbeReadiness(t *testing.T) {
	load := readinessV2Load(t)
	observer := &recordingReadinessObserver{
		executable:  map[string]core.CheckStatus{"git": core.CheckAvailable, "docker": core.CheckAvailable},
		environment: map[string]core.CheckStatus{"DATABASE_URL": core.CheckPresentNonEmpty, "AWS_PROFILE": core.CheckPresentNonEmpty},
		toolchain:   map[string]core.CheckStatus{"go": core.CheckCompatible},
	}
	svc := NewWithReadiness(fakeWorkspaceLookup{values: []workspace.Workspace{testProjectWorkspace()}}, &fakeLoader{result: load}, nil,
		ReadinessObservers{Executable: observer, Environment: observer, Toolchain: observer}, ReadinessOptions{})
	if _, err := svc.Inspect(context.Background(), string(testProjectWorkspace().ID)); err != nil {
		t.Fatal(err)
	}
	if observer.executableCalls != 0 || observer.environmentCalls != 0 || observer.toolchainCalls != 0 {
		t.Fatalf("inspect probed readiness: %#v", observer)
	}
}

func readinessV2Load(t *testing.T) core.LoadResult {
	t.Helper()
	parsed, err := core.Parse([]byte(`schema_version=2
[toolchains.go]
version="1.26"
[requirements.toolchains.go]
required=true
[requirements.executables.git]
required=true
[requirements.executables.docker]
required=false
[requirements.environment]
required_presence=["DATABASE_URL"]
optional_presence=["AWS_PROFILE"]
[commands.test]
argv=["true"]
`))
	if err != nil {
		t.Fatal(err)
	}
	return core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: strings.Repeat("a", 64), DiscoveryFingerprint: parsed.Fingerprint}
}

func readinessV1Load(t *testing.T) core.LoadResult {
	t.Helper()
	parsed, err := core.Parse([]byte("schema_version=1\n[commands.test]\nargv=[\"true\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	return core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: strings.Repeat("b", 64), DiscoveryFingerprint: parsed.Fingerprint}
}

func readinessV2EmptyLoad(t *testing.T) core.LoadResult {
	t.Helper()
	parsed, err := core.Parse([]byte("schema_version=2\n[commands.test]\nargv=[\"true\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	return core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: strings.Repeat("c", 64), DiscoveryFingerprint: parsed.Fingerprint}
}
