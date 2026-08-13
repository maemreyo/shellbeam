package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingRunner struct{}

func (failingRunner) Run(context.Context, string, ...string) error {
	return errors.New("activation failed")
}

func TestSystemdUnit(t *testing.T) {
	got, err := SystemdUnit("/opt/Shell Beam/shellbeam", "/home/u/.config/shellbeam/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ExecStart=\"/opt/Shell Beam/shellbeam\" daemon") || !strings.Contains(got, "UMask=0077") {
		t.Fatalf("%s", got)
	}
}
func TestLaunchdPlist(t *testing.T) {
	got, err := LaunchdPlist("/opt/shellbeam", "/tmp/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<string>daemon</string>") || !strings.Contains(got, "<integer>63</integer>") {
		t.Fatalf("%s", got)
	}
}

func TestInstallRollsBackExistingUnit(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "systemd", "user", "shellbeam.service")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Install(context.Background(), "linux", home, "/bin/true", "/tmp/config", failingRunner{}); err == nil {
		t.Fatal("expected error")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old" {
		t.Fatalf("%q %v", got, err)
	}
}
