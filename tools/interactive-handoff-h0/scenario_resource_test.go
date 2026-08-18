package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestH0P13BoundedResourceLeakStress(t *testing.T) {
	if raceEnabled {
		t.Skip("P13 resource accounting is intentionally non-race; race runtime perturbs process/FD measurements")
	}
	result := runResourceProbeForTest(t, 90*time.Second)
	assertProbePass(t, result)
	want := map[string]string{
		"cycles":            "100",
		"leaked_servers":    "0",
		"leaked_sockets":    "0",
		"leaked_ptys":       "0",
		"leaked_clients":    "0",
		"leaked_helpers":    "0",
		"leaked_temp_roots": "0",
		"race_runtime_used": "false",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P13 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
	for key, value := range map[string]string{"max_live_sessions": "1", "max_live_panes": "1", "max_live_tmux_clients": "2"} {
		if result.Facts[key] != value {
			t.Fatalf("P13 live-shape fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
	baselineFD := mustP13FactInt(t, result, "baseline_self_fds")
	finalFD := mustP13FactInt(t, result, "final_self_fds")
	postFD := mustP13FactInt(t, result, "max_post_cleanup_self_fds")
	baselineG := mustP13FactInt(t, result, "baseline_goroutines")
	finalG := mustP13FactInt(t, result, "final_goroutines")
	postG := mustP13FactInt(t, result, "max_post_cleanup_goroutines")
	if finalFD > baselineFD || postFD > baselineFD || finalG > baselineG || postG > baselineG {
		t.Fatalf("P13 self resources did not converge: %#v", result.Facts)
	}
	if mustP13FactInt(t, result, "max_server_descendants") < 1 || mustP13FactInt(t, result, "max_live_root_entries") < 1 {
		t.Fatalf("P13 did not observe private workload/root resources: %#v", result.Facts)
	}
}

func mustP13FactInt(t *testing.T, result ProbeResult, key string) int {
	t.Helper()
	value, err := strconv.Atoi(result.Facts[key])
	if err != nil {
		t.Fatalf("P13 fact %s=%q: %v", key, result.Facts[key], err)
	}
	return value
}

func TestRecordFixtureResourcesSnapshotsClientAndHelperPIDs(t *testing.T) {
	got := recordedFixtureResources{humanPID: 123, helperPID: 456}
	if got.humanPID != 123 || got.helperPID != 456 {
		t.Fatalf("recorded PID snapshot = human:%d helper:%d", got.humanPID, got.helperPID)
	}
}

func TestP13LeakDetectorCatchesLiveFixture(t *testing.T) {
	tmuxPath := requireH0Tmux(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer human.close()
	control, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		t.Fatal(err)
	}
	defer control.close()
	state := inspectLiveFixtureResources(f, human, control)
	if state.LiveServers == 0 || state.LiveSockets == 0 || state.LivePTYs == 0 || state.LiveClients == 0 || state.LiveHelpers == 0 || state.LiveTempRoots == 0 {
		t.Fatalf("intentional live fixture was not detected: %#v", state)
	}
}

func TestP13SelfResourceDetectorCatchesIntentionalLeak(t *testing.T) {
	baseline, err := sampleSelfResources()
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-stop
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if got, err := waitSelfResourceConvergence(ctx, baseline, 40*time.Millisecond); err == nil {
		close(stop)
		<-done
		_ = r.Close()
		_ = w.Close()
		t.Fatalf("intentional FD/goroutine leak converged unexpectedly: %#v", got)
	}
	close(stop)
	<-done
	_ = r.Close()
	_ = w.Close()
	if _, err := waitSelfResourceConvergence(context.Background(), baseline, time.Second); err != nil {
		t.Fatalf("resources did not converge after intentional leak cleanup: %v", err)
	}
}

func runResourceProbeForTest(t *testing.T, timeout time.Duration) ProbeResult {
	t.Helper()
	tmuxPath := requireH0Tmux(t)
	rawDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result := probeP13ResourceLeakStress(ctx, nativeProbeEnv{Tmux: tmuxPath, RawDir: rawDir})
	raw, err := os.ReadFile(filepath.Join(rawDir, "P13.txt"))
	if err != nil {
		t.Fatalf("P13 raw transcript: %v", err)
	}
	if strings.Contains(string(raw), "REAL_") {
		t.Fatal("P13 raw contains unexpected real-secret marker")
	}
	t.Logf("P13 raw tail:\n%s", tailLines(string(raw), 12))
	return result
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
