//go:build darwin

package dyld

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	traceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestE27CompilerIsLazyPrivateAndReused(t *testing.T) {
	state := e27PrivateState(t)
	provider := New(state)
	if _, err := os.Stat(providerRoot(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("New created provider state: %v", err)
	}
	health, err := provider.Health(context.Background())
	if err != nil || !health.Available {
		t.Fatalf("health=%#v err=%v", health, err)
	}
	if _, err := os.Stat(providerRoot(state)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Health created provider state: %v", err)
	}

	first, err := provider.Prepare(context.Background(), e27PrepareRequest("compiler-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Abort()
	firstBinding := first.Binding()
	firstDylib := e27EnvValue(first.EnvironmentAdditions(), "DYLD_INSERT_LIBRARIES")
	if firstDylib == "" || !strings.HasPrefix(firstDylib, providerRoot(state)+string(filepath.Separator)) {
		t.Fatalf("private dylib=%q", firstDylib)
	}
	info, err := os.Lstat(firstDylib)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("dylib info=%#v err=%v", info, err)
	}
	second, err := provider.Prepare(context.Background(), e27PrepareRequest("compiler-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Abort()
	if secondDylib := e27EnvValue(second.EnvironmentAdditions(), "DYLD_INSERT_LIBRARIES"); secondDylib != firstDylib {
		t.Fatalf("artifact not reused first=%q second=%q", firstDylib, secondDylib)
	}
	if second.Binding().InstrumentationFingerprint != firstBinding.InstrumentationFingerprint {
		t.Fatal("reused artifact changed instrumentation fingerprint")
	}
}

func TestE27CompilerIdentityAndSourceBindInstrumentationFingerprint(t *testing.T) {
	first := New(e27PrivateState(t))
	first.compilerIdentity = func(context.Context) (string, error) { return "clang-A", nil }
	a, err := first.Prepare(context.Background(), e27PrepareRequest("identity-a"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Abort()

	second := New(e27PrivateState(t))
	second.compilerIdentity = func(context.Context) (string, error) { return "clang-B", nil }
	b, err := second.Prepare(context.Background(), e27PrepareRequest("identity-b"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Abort()
	if a.Binding().InstrumentationFingerprint == b.Binding().InstrumentationFingerprint {
		t.Fatal("compiler identity did not bind instrumentation fingerprint")
	}

	third := New(e27PrivateState(t))
	third.source = third.source + "\n/* source variant */\n"
	c, err := third.Prepare(context.Background(), e27PrepareRequest("identity-c"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Abort()
	if a.Binding().InstrumentationFingerprint == c.Binding().InstrumentationFingerprint {
		t.Fatal("source identity did not bind instrumentation fingerprint")
	}
}

func TestE27CompilerMissingFailureAndUnsafePrivateRootAreTypedUnavailable(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T) *Provider
	}{
		{"missing-compiler", func(t *testing.T) *Provider {
			p := New(e27PrivateState(t))
			p.clangPath = "/definitely/missing/clang"
			return p
		}},
		{"compiler-failure", func(t *testing.T) *Provider {
			p := New(e27PrivateState(t))
			p.compile = func(context.Context, string, string, string) error { return errors.New("compile boom") }
			return p
		}},
		{"world-readable-state", func(t *testing.T) *Provider {
			root := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			return New(root)
		}},
		{"symlink-state", func(t *testing.T) *Provider {
			target := e27PrivateState(t)
			link := filepath.Join(t.TempDir(), "link")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return New(link)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := tc.make(t)
			_, err := provider.Prepare(context.Background(), e27PrepareRequest("fail-"+tc.name))
			if !errors.Is(err, failure.InputTraceProviderUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestE27CompilerProviderRejectsRequiredModeBeforeCreatingCollector(t *testing.T) {
	provider := New(e27PrivateState(t))
	req := e27PrepareRequest("required")
	req.Mode = trace.ModeRequired
	_, err := provider.Prepare(context.Background(), req)
	if !errors.Is(err, failure.InputTraceRequiredUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if provider.activeCount() != 0 {
		t.Fatalf("required mode created collector count=%d", provider.activeCount())
	}
}

func e27PrivateState(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func e27PrepareRequest(operationID string) traceapp.PrepareRequest {
	return traceapp.PrepareRequest{Mode: trace.ModeBestEffort, OperationID: operationID, ExecutionMode: operation.ExecutionModeArgv, Executable: "/tmp/fixture", CWD: "/tmp"}
}

func e27EnvValue(entries []operation.EnvironmentEntry, key string) string {
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Value
		}
	}
	return ""
}
