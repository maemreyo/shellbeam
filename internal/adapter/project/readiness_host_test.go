package project

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

func TestHostReadinessObservesExecutableAndEnvironmentPresenceWithoutValues(t *testing.T) {
	secret := "postgres://alice:secret@db/production"
	host := &HostReadiness{
		lookPath: func(name string) (string, error) {
			if name == "git" {
				return "/usr/bin/git", nil
			}
			return "", exec.ErrNotFound
		},
		lookupEnv: func(name string) (string, bool) {
			switch name {
			case "DATABASE_URL":
				return secret, true
			case "EMPTY_VALUE":
				return "", true
			default:
				return "", false
			}
		},
	}
	if got := host.ObserveExecutable(context.Background(), "git"); got.Status != core.CheckAvailable {
		t.Fatalf("git=%#v", got)
	}
	if got := host.ObserveExecutable(context.Background(), "docker"); got.Status != core.CheckMissing {
		t.Fatalf("docker=%#v", got)
	}
	present := host.ObserveEnvironmentPresence(context.Background(), "DATABASE_URL", true)
	if present.Status != core.CheckPresentNonEmpty || !present.Required {
		t.Fatalf("present=%#v", present)
	}
	if got := host.ObserveEnvironmentPresence(context.Background(), "EMPTY_VALUE", true); got.Status != core.CheckAbsent {
		t.Fatalf("empty=%#v", got)
	}
	if got := host.ObserveEnvironmentPresence(context.Background(), "MISSING", false); got.Status != core.CheckAbsent || got.Required {
		t.Fatalf("missing=%#v", got)
	}
	encoded, err := json.Marshal(present)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("environment value leaked: %s", encoded)
	}
}

func TestHostReadinessGoToolchainUsesRepositoryDeclarationAndBoundedProvider(t *testing.T) {
	root := t.TempDir()
	goMod := "module example.com/demo\n\ngo 1.23.0\ntoolchain go1.26.1\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	actual := "go1.26.5"
	host := &HostReadiness{
		lookPath:  func(name string) (string, error) { return "/usr/local/bin/" + name, nil },
		lookupEnv: os.LookupEnv,
		goVersion: func(context.Context) (string, error) { return actual, nil },
		readFile:  os.ReadFile,
	}
	got := host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{VersionSource: "go.mod"})
	if got.Status != core.CheckCompatible || got.ProviderID != "go-host" || got.ProviderVersion != 1 {
		t.Fatalf("compatible=%#v", got)
	}
	actual = "go1.25.9"
	got = host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{VersionSource: "go.mod"})
	if got.Status != core.CheckIncompatible {
		t.Fatalf("incompatible=%#v", got)
	}
	actual = "go1.26.5"
	got = host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{Version: "1.26"})
	if got.Status != core.CheckCompatible {
		t.Fatalf("direct version=%#v", got)
	}
	got = host.ObserveToolchain(context.Background(), root, "node", core.Toolchain{Version: "22"})
	if got.Status != core.CheckUnavailable {
		t.Fatalf("unsupported provider=%#v", got)
	}
}

func TestHostReadinessGoToolchainDistinguishesMissingUnknownAndRepositoryEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "go.mod")
	if err := os.WriteFile(outside, []byte("go 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	host := &HostReadiness{
		lookPath:  func(string) (string, error) { return "/usr/local/bin/go", nil },
		lookupEnv: os.LookupEnv,
		goVersion: func(context.Context) (string, error) { return "go1.26.5", nil },
		readFile:  os.ReadFile,
	}
	if got := host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{VersionSource: "go.mod"}); got.Status != core.CheckUnknown {
		t.Fatalf("escaping version source=%#v", got)
	}

	host.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if got := host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{Version: "1.26"}); got.Status != core.CheckMissing {
		t.Fatalf("missing go=%#v", got)
	}

	host.lookPath = func(string) (string, error) { return "", errors.New("permission denied") }
	if got := host.ObserveToolchain(context.Background(), root, "go", core.Toolchain{Version: "1.26"}); got.Status != core.CheckUnavailable {
		t.Fatalf("unavailable go=%#v", got)
	}
}
