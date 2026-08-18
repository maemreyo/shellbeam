package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestH0P14MultiSessionPrivacyIsolation(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P14", probeP14MultiSessionPrivacyIsolation, 90*time.Second)
	if result.Status != StatusPass {
		t.Fatalf("P14=%s summary=%s facts=%#v", result.Status, result.Summary, result.Facts)
	}
	want := map[string]string{
		"cycles":                             "128",
		"candidate.per_session_observer.p14": "PASS",
		"candidate.shared_observer_with_per_pane_off.p14":                             "NOT_ELIGIBLE_P6",
		"candidate.shared_observer_with_daemon_demux_simulation.p14":                  "PASS",
		"candidate.per_session_observer.a_private_count":                              "0",
		"candidate.per_session_observer.b_complete":                                   "true",
		"candidate.per_session_observer.c_complete":                                   "true",
		"candidate.shared_observer_with_daemon_demux_simulation.raw_a_entered_parser": "true",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P14 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestH0P15ObserverOverlapPrivacyFault(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P15", probeP15ObserverOverlapPrivacyFault, 90*time.Second)
	if result.Status != StatusPass {
		t.Fatalf("P15=%s summary=%s facts=%#v", result.Status, result.Summary, result.Facts)
	}
	want := map[string]string{
		"fault_points":                                                                "6",
		"candidate.per_session_observer.p15":                                          "PASS",
		"candidate.shared_observer_with_per_pane_off.p15":                             "NOT_ELIGIBLE_P6",
		"candidate.shared_observer_with_daemon_demux_simulation.p15":                  "PASS",
		"candidate.per_session_observer.bc_public":                                    "true",
		"candidate.shared_observer_with_daemon_demux_simulation.raw_a_entered_parser": "true",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P15 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestP15PerSessionReapsReplacementObservers(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("direct child zombie accounting is only qualified on darwin/linux")
	}
	tmuxPath := requireH0Tmux(t)
	before := directZombieChildCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ok, facts, raw := faultP15PerSession(ctx, nativeProbeEnv{Tmux: tmuxPath, RawDir: t.TempDir()})
	cancel()
	if !ok {
		t.Fatalf("P15 per-session setup failed: facts=%#v raw=%s", facts, raw)
	}
	time.Sleep(50 * time.Millisecond)
	after := directZombieChildCount(t)
	if after != before {
		t.Fatalf("P15 per-session leaked replacement observer zombies: before=%d after=%d", before, after)
	}
}

func directZombieChildCount(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("ps", "-axo", "ppid=,state=").Output()
	if err != nil {
		t.Fatal(err)
	}
	parent := fmt.Sprint(os.Getpid())
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == parent && strings.HasPrefix(fields[1], "Z") {
			count++
		}
	}
	return count
}
