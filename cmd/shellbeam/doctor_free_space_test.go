//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func freeSpaceCheck(t *testing.T, report control.Report) control.Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == "disk_space" {
			return check
		}
	}
	t.Fatal("report has no disk_space check")
	return control.Check{}
}

// doctorWithMinimum runs doctor against fresh directories under a configured
// free-space floor.
func doctorWithMinimum(t *testing.T, minimum int64) control.Report {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	for _, dir := range []string{stateDir, runtimeDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "config.toml")
	configText := fmt.Sprintf("schema_version = 1\nmax_concurrent_sessions = 4\nmin_free_space_bytes = %d\n", minimum)
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := doctorReport([]string{
		"--state-dir", stateDir, "--runtime-dir", runtimeDir,
		"--shell", "/bin/sh", "--config", configPath, "--json",
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// TestLowFreeSpaceWarnsAndStillExitsZero is the whole point of the check.
//
// A floor that refused to start the daemon would take ShellBeam away exactly
// when an operator most needs a shell to go and free some room, and it would do
// so over a threshold that is a guess about a machine the daemon does not own.
// The report has to say the disk is nearly full and then get out of the way.
func TestLowFreeSpaceWarnsAndStillExitsZero(t *testing.T) {
	// No volume has an exabyte free, so this floor is guaranteed to be breached.
	report := doctorWithMinimum(t, 1<<60)

	if got := freeSpaceCheck(t, report); got.Status != control.Warn {
		t.Fatalf("disk_space check under an unreachable floor = %#v, want warn", got)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("exit code = %d, want 0: low disk space must never gate startup", report.ExitCode())
	}
}

// TestAmpleFreeSpacePasses keeps the check from being a warning that is always
// on, which operators learn to ignore.
func TestAmpleFreeSpacePasses(t *testing.T) {
	report := doctorWithMinimum(t, 1<<20)

	if got := freeSpaceCheck(t, report); got.Status != control.Pass {
		t.Fatalf("disk_space check with room to spare = %#v, want pass", got)
	}
}
