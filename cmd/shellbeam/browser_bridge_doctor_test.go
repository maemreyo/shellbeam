package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func TestBrowserBridgeCheckWarnsWhenManifestAbsent(t *testing.T) {
	check := browserBridgeCheck("linux", t.TempDir())
	if check.ID != "browser_bridge" {
		t.Fatalf("id = %q", check.ID)
	}
	if check.Status != control.Warn {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Hint, "browser-host install") {
		t.Fatalf("hint lacks remediation: %q", check.Hint)
	}
}

func TestBrowserBridgeCheckReportsPinnedExtensionIDAndProtocolVersion(t *testing.T) {
	home := t.TempDir()
	hostPath := filepath.Join(home, "shellbeam-browser-host")
	if err := os.WriteFile(hostPath, []byte("host"), 0o700); err != nil {
		t.Fatalf("write host: %v", err)
	}
	if _, err := bridgeapp.InstallManifest("linux", home, hostPath, "router@shellbeam.local"); err != nil {
		t.Fatalf("install: %v", err)
	}
	check := browserBridgeCheck("linux", home)
	if check.Status != control.Pass {
		t.Fatalf("status = %q, want pass", check.Status)
	}
	if !strings.Contains(check.Message, "router@shellbeam.local") {
		t.Fatalf("message omits pinned extension id: %q", check.Message)
	}
	if !strings.Contains(check.Message, "protocol 1") {
		t.Fatalf("message omits protocol version: %q", check.Message)
	}
}

func TestBrowserBridgeCheckWarnsWhenPinnedHostBinaryIsMissing(t *testing.T) {
	home := t.TempDir()
	hostPath := filepath.Join(home, "missing-shellbeam-browser-host")
	if _, err := bridgeapp.InstallManifest("linux", home, hostPath, "router@shellbeam.local"); err != nil {
		t.Fatalf("install: %v", err)
	}
	check := browserBridgeCheck("linux", home)
	if check.Status != control.Warn {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Message, "host binary missing") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestBrowserBridgeCheckNeverFailsTheReport(t *testing.T) {
	report := control.Report{SchemaVersion: 1, Checks: []control.Check{browserBridgeCheck("linux", t.TempDir())}}
	if report.ExitCode() != 0 {
		t.Fatal("absent optional bridge made doctor claim an unsafe boundary")
	}
}
