package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreproject "github.com/maemreyo/shellbeam/internal/core/project"
	coreworkspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestProjectCommandReviewLifecycleAndStaleFingerprint(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	manifestPath := writeProjectCLIManifest(t, repo, "schema_version=1\n[commands.test]\nargv=[\"true\"]\n")

	out, errOut, code := runWorkspaceCLI(t, "workspace", "attach", repo, "--label", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("attach code=%d stderr=%q", code, errOut)
	}
	var ws coreworkspace.Workspace
	if err := json.Unmarshal([]byte(out), &ws); err != nil {
		t.Fatal(err)
	}

	out, errOut, code = runWorkspaceCLI(t, "project", "inspect", "--workspace", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("inspect code=%d stderr=%q", code, errOut)
	}
	var inspected coreproject.Inspection
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Status != coreproject.StatusReviewDue || inspected.DiscoveryFingerprint == "" {
		t.Fatalf("unreviewed=%#v", inspected)
	}
	firstFingerprint := inspected.DiscoveryFingerprint

	out, errOut, code = runWorkspaceCLI(t, "project", "validate", "--workspace", string(ws.ID), "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("validate code=%d stderr=%q", code, errOut)
	}

	out, errOut, code = runWorkspaceCLI(t, "project", "review", "--workspace", "primary", "--fingerprint", firstFingerprint, "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("review code=%d stderr=%q", code, errOut)
	}
	var reviewed coreproject.Inspection
	if err := json.Unmarshal([]byte(out), &reviewed); err != nil {
		t.Fatal(err)
	}
	if reviewed.Status != coreproject.StatusValid || reviewed.ReviewFingerprint != firstFingerprint {
		t.Fatalf("reviewed=%#v", reviewed)
	}

	if err := os.WriteFile(manifestPath, []byte("# exact input changed\nschema_version = 1\n[commands.test]\nargv = [\"true\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut, code = runWorkspaceCLI(t, "project", "inspect", "--workspace", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("inspect drift code=%d stderr=%q", code, errOut)
	}
	var drifted coreproject.Inspection
	if err := json.Unmarshal([]byte(out), &drifted); err != nil {
		t.Fatal(err)
	}
	if drifted.Status != coreproject.StatusReviewDue || drifted.DiscoveryFingerprint == firstFingerprint {
		t.Fatalf("drifted=%#v", drifted)
	}

	_, errOut, code = runWorkspaceCLI(t, "project", "review", "--workspace", "primary", "--fingerprint", firstFingerprint, "--state-dir", stateDir, "--json")
	if code == 0 || !strings.Contains(errOut, coreproject.CodeChangedDuringResolve) {
		t.Fatalf("stale review code=%d stderr=%q", code, errOut)
	}
}

func TestProjectCommandInspectInvalidAsDataButValidateFails(t *testing.T) {
	repo := initWorkspaceCLIRepo(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	writeProjectCLIManifest(t, repo, "schema_version=1\nmystery=true\n")
	_, errOut, code := runWorkspaceCLI(t, "workspace", "attach", repo, "--label", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("attach code=%d stderr=%q", code, errOut)
	}

	out, errOut, code := runWorkspaceCLI(t, "project", "inspect", "--workspace", "primary", "--state-dir", stateDir, "--json")
	if code != 0 {
		t.Fatalf("inspect invalid code=%d stderr=%q", code, errOut)
	}
	var inspected coreproject.Inspection
	if err := json.Unmarshal([]byte(out), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.Status != coreproject.StatusInvalid {
		t.Fatalf("inspect invalid=%#v", inspected)
	}

	_, errOut, code = runWorkspaceCLI(t, "project", "validate", "--workspace", "primary", "--state-dir", stateDir, "--json")
	if code == 0 {
		t.Fatalf("validate invalid unexpectedly succeeded stderr=%q", errOut)
	}
}

func writeProjectCLIManifest(t *testing.T, repo, contents string) string {
	t.Helper()
	dir := filepath.Join(repo, ".shellbeam")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "project.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
