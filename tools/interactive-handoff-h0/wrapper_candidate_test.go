package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	gotmuxccModule  = "github.com/atomicstack/gotmuxcc"
	gotmuxccVersion = "v0.1.4"
	gotmuxccOrigin  = "440c9d00c0d094cc4dde1eb28ff3a534ceefd98b"
	gotmuxccSum     = "h1:WmFsKnomT+Zif4WxNfVH+zNu1dXLnhT0+1f1N+HJags="
)

type moduleDownload struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	Dir     string `json:"Dir"`
	Sum     string `json:"Sum"`
	Origin  struct {
		Hash string `json:"Hash"`
	} `json:"Origin"`
}

type wrapperProbeResult struct {
	Version                string `json:"version"`
	ClientPID              int    `json:"client_pid"`
	ReconnectPID           int    `json:"reconnect_pid"`
	ReconnectOK            bool   `json:"reconnect_ok"`
	PaneToggleOK           bool   `json:"pane_toggle_ok"`
	PaneDisableError       string `json:"pane_disable_error,omitempty"`
	PaneEnableError        string `json:"pane_enable_error,omitempty"`
	CommandErrorPropagated bool   `json:"command_error_propagated"`
	LargeOutputBytes       int    `json:"large_output_bytes"`
}

type wrapperQualification struct {
	Module                    string `json:"module"`
	Version                   string `json:"version"`
	Origin                    string `json:"origin"`
	Sum                       string `json:"sum"`
	License                   string `json:"license"`
	Verdict                   string `json:"verdict"`
	Reason                    string `json:"reason"`
	PrePrivateCanaryObserved  bool   `json:"pre_private_canary_observed"`
	PostPrivateCanaryObserved bool   `json:"post_private_canary_observed"`
	CloseReaped               bool   `json:"close_reaped"`
	ReconnectOK               bool   `json:"reconnect_ok"`
	PaneToggleOK              bool   `json:"pane_toggle_ok"`
	PaneDisableError          string `json:"pane_disable_error,omitempty"`
	PaneEnableError           string `json:"pane_enable_error,omitempty"`
	CommandErrorPropagated    bool   `json:"command_error_propagated"`
	LargeOutputBytes          int    `json:"large_output_bytes"`
	ExternalModuleCount       int    `json:"external_module_count"`
	CGODisabledBuild          bool   `json:"cgo_disabled_build"`
	RootModuleUnchanged       bool   `json:"root_module_unchanged"`
	Recommendation            string `json:"recommendation"`
}

func TestGotmuxccCandidateProbeSourcePresent(t *testing.T) {
	if _, err := os.Stat(filepath.Join("testdata", "gotmuxcc-v0.1.4", "main.go")); err != nil {
		t.Fatal(err)
	}
}

func TestGotmuxccCandidateIdentityAndFirstByteQualification(t *testing.T) {
	tmuxPath := requireH0Tmux(t)
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	beforeMod := mustFileSHA256(t, filepath.Join(root, "go.mod"))
	beforeSum := mustFileSHA256(t, filepath.Join(root, "go.sum"))
	candidate := downloadGotmuxcc(t)
	if candidate.Path != gotmuxccModule || candidate.Version != gotmuxccVersion || candidate.Sum != gotmuxccSum || candidate.Origin.Hash != gotmuxccOrigin {
		t.Fatalf("candidate identity mismatch: %#v", candidate)
	}
	licenseBytes, err := os.ReadFile(filepath.Join(candidate.Dir, "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(licenseBytes), "MIT License\n") {
		t.Fatalf("unexpected license: %q", string(licenseBytes[:min(32, len(licenseBytes))]))
	}

	probe, externalModules := buildIsolatedWrapperProbe(t, root)
	probeResult, preSeen, postSeen := runWrapperFirstByteProbe(t, tmuxPath, probe)
	rootUnchanged := beforeMod == mustFileSHA256(t, filepath.Join(root, "go.mod")) && beforeSum == mustFileSHA256(t, filepath.Join(root, "go.sum"))
	closeReaped := waitProcessGone(probeResult.ClientPID, time.Second) && waitProcessGone(probeResult.ReconnectPID, time.Second)
	qualified := wrapperQualification{
		Module: gotmuxccModule, Version: gotmuxccVersion, Origin: gotmuxccOrigin, Sum: gotmuxccSum, License: "MIT",
		Verdict: "FAIL", Reason: "P5_FIRST_BYTE_PRIVACY+PANE_OUTPUT_PARSE_ERROR", PrePrivateCanaryObserved: preSeen, PostPrivateCanaryObserved: postSeen,
		CloseReaped: closeReaped, ReconnectOK: probeResult.ReconnectOK, PaneToggleOK: probeResult.PaneToggleOK,
		PaneDisableError: probeResult.PaneDisableError, PaneEnableError: probeResult.PaneEnableError,
		CommandErrorPropagated: probeResult.CommandErrorPropagated, LargeOutputBytes: probeResult.LargeOutputBytes,
		ExternalModuleCount: externalModules, CGODisabledBuild: true, RootModuleUnchanged: rootUnchanged,
		Recommendation: "own thin Control Mode adapter",
	}
	writeWrapperQualification(t, root, qualified)

	if !preSeen {
		t.Fatal("candidate did not reproduce expected pre-no-output exposure window; cannot qualify the P5 finding")
	}
	if postSeen {
		t.Fatal("post-ACK no-output canary reached wrapper trace")
	}
	if !closeReaped || !probeResult.ReconnectOK || !probeResult.CommandErrorPropagated {
		t.Fatalf("wrapper lifecycle/error behavior=%#v", qualified)
	}
	if probeResult.PaneToggleOK || !strings.Contains(probeResult.PaneDisableError, "parse error") || !strings.Contains(probeResult.PaneEnableError, "parse error") {
		t.Fatalf("expected tmux 3.6a pane-output quoting incompatibility not proven: %#v", qualified)
	}
	if probeResult.LargeOutputBytes < 12*1024 || !rootUnchanged {
		t.Fatalf("wrapper dependency/output facts=%#v", qualified)
	}
}

func downloadGotmuxcc(t *testing.T) moduleDownload {
	t.Helper()
	out := mustRunCmd(t, "", nil, "go", "mod", "download", "-json", gotmuxccModule+"@"+gotmuxccVersion)
	var got moduleDownload
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func buildIsolatedWrapperProbe(t *testing.T, repoRoot string) (string, int) {
	t.Helper()
	work := filepath.Join(repoRoot, ".build", "interactive-handoff-h0", "gotmuxcc-v0.1.4")
	archive := filepath.Join(repoRoot, ".build", "interactive-handoff-h0", "wrapper")
	if err := os.RemoveAll(work); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archive, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join("testdata", "gotmuxcc-v0.1.4", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	goMod := "module shellbeam-h0-gotmuxcc-probe\n\ngo 1.26\n\nrequire " + gotmuxccModule + " " + gotmuxccVersion + "\n"
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunCmd(t, work, nil, "go", "mod", "download", gotmuxccModule+"@"+gotmuxccVersion)
	mustRunCmd(t, work, nil, "go", "mod", "tidy")
	graph := mustRunCmd(t, work, nil, "go", "mod", "graph")
	deps := mustRunCmd(t, work, nil, "go", "list", "-tags", "h0_gotmuxcc_probe", "-deps", "-json", ".")
	mods := mustRunCmd(t, work, nil, "go", "list", "-m", "-json", "all")
	why := mustRunCmd(t, work, nil, "go", "mod", "why", "-m", gotmuxccModule)
	for name, data := range map[string][]byte{"go-mod-graph.txt": graph, "go-list-deps.json": deps, "go-list-modules.json": mods, "go-mod-why.txt": why} {
		if err := os.WriteFile(filepath.Join(archive, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := filepath.Join(work, "probe")
	env := append(os.Environ(), "CGO_ENABLED=0")
	mustRunCmd(t, work, env, "go", "build", "-tags", "h0_gotmuxcc_probe", "-o", probe, ".")
	moduleLines := strings.Split(strings.TrimSpace(string(mustRunCmd(t, work, nil, "go", "list", "-m", "all"))), "\n")
	external := len(moduleLines) - 1
	if external < 0 {
		external = 0
	}
	return probe, external
}

func runWrapperFirstByteProbe(t *testing.T, tmuxPath, probe string) (wrapperProbeResult, bool, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, err := newProbeFixtureRoot()
	if err != nil {
		t.Fatal(err)
	}
	f, err := newNativeFixtureWithCommand(ctx, tmuxPath, root, "stty -echo; exec cat")
	if err != nil {
		t.Fatal(err)
	}
	defer f.close(context.Background())
	panes, err := f.paneIDs(ctx, f.Session)
	if err != nil || len(panes) != 1 {
		t.Fatalf("panes=%v err=%v", panes, err)
	}
	largeValue := strings.Repeat("L", 128)
	for i := 0; i < 128; i++ {
		name := fmt.Sprintf("@h0_large_%03d", i)
		if _, err := f.tmux(ctx, "set-option", "-g", name, largeValue); err != nil {
			t.Fatalf("set %s: %v", name, err)
		}
	}

	coord := t.TempDir()
	constructorReady, allowPrivate := filepath.Join(coord, "constructor-ready"), filepath.Join(coord, "allow-private")
	privateReady, allowFinish := filepath.Join(coord, "private-ready"), filepath.Join(coord, "allow-finish")
	tracePath := filepath.Join(coord, "gotmuxcc-trace.log")
	cmd := exec.CommandContext(ctx, probe, "--socket", f.SocketPath, "--pane", panes[0], "--constructor-ready", constructorReady, "--allow-private", allowPrivate, "--private-ready", privateReady, "--allow-finish", allowFinish)
	cmd.Env = append(os.Environ(), "TMUX=", "GOTMUXCC_TRACE=all", "GOTMUXCC_TRACE_FILE="+tracePath, "PATH="+filepath.Dir(tmuxPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := waitPathForTest(constructorReady, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	preCanary := "WRAPPER_PRE_PRIVATE_CANARY_6f7e22"
	if err := f.emitMarker(ctx, panes[0], preCanary); err != nil {
		t.Fatal(err)
	}
	preSeen := waitFileContains(tracePath, preCanary, time.Second)
	if err := os.WriteFile(allowPrivate, []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := waitPathForTest(privateReady, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	postCanary := "WRAPPER_POST_PRIVATE_CANARY_8c15d1"
	if err := f.emitMarker(ctx, panes[0], postCanary); err != nil {
		t.Fatal(err)
	}
	postSeen := waitFileContains(tracePath, postCanary, 150*time.Millisecond)
	if err := os.WriteFile(allowFinish, []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("probe: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	var result wrapperProbeResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		t.Fatalf("decode probe: %v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	return result, preSeen, postSeen
}

func writeWrapperQualification(t *testing.T, repoRoot string, value wrapperQualification) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(repoRoot, ".build", "interactive-handoff-h0", "wrapper", "result.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRunCmd(t *testing.T, cwd string, env []string, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return out
}

func mustFileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func waitPathForTest(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", path)
}

func waitFileContains(path, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), needle) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func waitProcessGone(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return !processAlive(pid)
}
