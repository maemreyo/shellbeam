//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"testing"

	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func socketCheck(t *testing.T, report control.Report) control.Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == "socket" {
			return check
		}
	}
	t.Fatal("report has no socket check")
	return control.Check{}
}

// TestRequireReadyMakesAnUnservingDaemonAFailure is what lets a startup gate
// wait correctly.
//
// The socket is published before startup recovery runs, so its presence does
// not mean the daemon is serving. Plain doctor reports that state as a warning
// and still exits 0, which is right for a human reading the report and wrong
// for a gate that only checks the exit status -- it would launch a tunnel
// against a daemon that is still reconciling.
func TestRequireReadyMakesAnUnservingDaemonAFailure(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	runtimeDir := filepath.Join(root, "run")
	for _, dir := range []string{stateDir, runtimeDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh", "--json"}

	lenient, err := doctorReport(args)
	if err != nil {
		t.Fatal(err)
	}
	if got := socketCheck(t, lenient); got.Status != control.Warn {
		t.Fatalf("socket check without --require-ready = %#v, want warn", got)
	}
	if lenient.ExitCode() != 0 {
		t.Fatalf("exit code without --require-ready = %d, want 0", lenient.ExitCode())
	}

	strict, err := doctorReport(append(args, requireReadyFlag))
	if err != nil {
		t.Fatal(err)
	}
	if got := socketCheck(t, strict); got.Status != control.Fail {
		t.Fatalf("socket check with --require-ready = %#v, want fail", got)
	}
	if strict.ExitCode() == 0 {
		t.Fatal("--require-ready exited 0 while the daemon was not serving")
	}
}

// TestRequireReadyPassesOnceTheDaemonServes closes the loop: the strict check
// has to actually clear, or a gate built on it would never proceed.
func TestRequireReadyPassesOnceTheDaemonServes(t *testing.T) {
	stateDir, runtimes := ownershipDirs(t, "run-a")
	daemon := launchDaemon(t, stateDir, runtimes[0])
	if !daemon.serving(t) {
		t.Fatalf("daemon never served: %s", daemon.output(t))
	}

	report, err := doctorReport([]string{
		"--state-dir", stateDir, "--runtime-dir", runtimes[0], "--shell", "/bin/sh", "--json", requireReadyFlag,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := socketCheck(t, report); got.Status != control.Pass {
		t.Fatalf("socket check against a serving daemon = %#v, want pass", got)
	}
	if report.ExitCode() != 0 {
		t.Fatalf("exit code against a serving daemon = %d, want 0", report.ExitCode())
	}
}
