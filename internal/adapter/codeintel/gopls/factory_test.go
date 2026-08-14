package gopls

import (
	"context"
	"errors"
	"testing"

	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

func TestFactoryResolveSelectsExplicitOrUnambiguousGoDefaultWithoutStarting(t *testing.T) {
	starts := 0
	factory, err := newFactory(DefaultConfig(), factoryDeps{
		lookPath:           func(string) (string, error) { return "/tool/gopls", nil },
		executableIdentity: func(string) (string, error) { return "exec_identity", nil },
		isGoWorkspace:      func(string) bool { return true },
		startSession: func(context.Context, sessionStart) (semanticSession, error) {
			starts++
			return newFakeSession(), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ws := testWorkspace(t.TempDir())
	for _, query := range []core.Query{
		{Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic},
		{Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace},
	} {
		options, resolveErr := factory.Resolve(t.Context(), ws, query)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if options.ProviderID != core.ProviderGoSemantic || options.ExecutableIdentity != "exec_identity" ||
			options.ConfigFingerprint == "" || options.BuildFingerprint == "" {
			t.Fatalf("options=%+v", options)
		}
	}
	if starts != 0 {
		t.Fatalf("readiness resolution started provider %d times", starts)
	}
}

func TestFactoryResolveMissingExecutableDoesNotInstallOrStart(t *testing.T) {
	starts := 0
	factory, err := newFactory(DefaultConfig(), factoryDeps{
		lookPath:           func(string) (string, error) { return "", errors.New("missing") },
		executableIdentity: func(string) (string, error) { t.Fatal("identity should not run"); return "", nil },
		isGoWorkspace:      func(string) bool { return true },
		startSession: func(context.Context, sessionStart) (semanticSession, error) {
			starts++
			return nil, errors.New("unexpected")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, resolveErr := factory.Resolve(t.Context(), testWorkspace(t.TempDir()), core.Query{
		Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic,
	})
	if resolveErr == nil {
		t.Fatal("missing executable unexpectedly resolved")
	}
	if starts != 0 {
		t.Fatalf("provider started %d times", starts)
	}
}

func TestFactoryDefaultRequiresGoWorkspaceButExplicitProviderDoesNot(t *testing.T) {
	factory, err := newFactory(DefaultConfig(), factoryDeps{
		lookPath:           func(string) (string, error) { return "/tool/gopls", nil },
		executableIdentity: func(string) (string, error) { return "exec_identity", nil },
		isGoWorkspace:      func(string) bool { return false },
		startSession:       func(context.Context, sessionStart) (semanticSession, error) { return newFakeSession(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ws := testWorkspace(t.TempDir())
	if _, err := factory.Resolve(t.Context(), ws, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace}); err == nil {
		t.Fatal("non-Go workspace unexpectedly selected default gopls")
	}
	if _, err := factory.Resolve(t.Context(), ws, core.Query{
		Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic,
	}); err != nil {
		t.Fatalf("explicit provider should resolve: %v", err)
	}
}

func TestFactoryResolveCapturesCompatibilityInputsWithoutVersionProbe(t *testing.T) {
	config := DefaultConfig()
	config.ConfigFingerprint = "cfg_custom"
	config.BuildFingerprint = "build_custom"
	factory, err := newFactory(config, factoryDeps{
		lookPath:           func(string) (string, error) { return "/tool/gopls", nil },
		executableIdentity: func(path string) (string, error) { return "identity:" + path, nil },
		isGoWorkspace:      func(string) bool { return true },
		startSession:       func(context.Context, sessionStart) (semanticSession, error) { return newFakeSession(), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	options, err := factory.Resolve(t.Context(), testWorkspace(t.TempDir()), core.Query{
		Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := appcodeintel.ProviderStartOptions{
		ProviderID: core.ProviderGoSemantic, ExecutableIdentity: "identity:/tool/gopls",
		ConfigFingerprint: "cfg_custom", BuildFingerprint: "build_custom",
	}
	if options != want {
		t.Fatalf("options=%+v want=%+v", options, want)
	}
}
