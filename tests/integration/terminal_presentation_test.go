package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	terminaladapter "github.com/maemreyo/shellbeam/internal/adapter/terminalpresentation"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	terminalcore "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestTerminalPresentationResolverTruthMatrix(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 0, 0, 0, time.UTC)
	ghostty := h3TerminalIdentity("ghostty", "com.mitchellh.ghostty")
	wezterm := h3TerminalIdentity("wezterm", "com.github.wez.wezterm")

	t.Run("active beats recent", func(t *testing.T) {
		registry := h3Registry(t)
		mustObserveH3(t, registry, ghostty, now.Add(-10*time.Second))
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(&wezterm, now)}, &h3RunningSource{}, now)
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{}, terminalcore.SourceActive, "wezterm")
	})

	t.Run("browser frontmost preserves recent terminal", func(t *testing.T) {
		registry := h3Registry(t)
		mustObserveH3(t, registry, ghostty, now.Add(-10*time.Second))
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, &h3RunningSource{}, now)
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{}, terminalcore.SourceRecent, "ghostty")
	})

	t.Run("fresh bridge beats single running", func(t *testing.T) {
		registry := h3Registry(t)
		bridge := h3Evidence(ghostty, terminalcore.SourceBridgeAffinity, now.Add(-time.Second), now.Add(time.Minute), terminalcore.QualityValidated)
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, &h3RunningSource{identities: []terminalcore.TerminalIdentity{wezterm}}, now)
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{BridgeAffinity: &bridge}, terminalcore.SourceBridgeAffinity, "ghostty")
	})

	t.Run("stale bridge downgrades to single running", func(t *testing.T) {
		registry := h3Registry(t)
		bridge := h3Evidence(ghostty, terminalcore.SourceBridgeAffinity, now.Add(-time.Minute), now.Add(-time.Nanosecond), terminalcore.QualityValidated)
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, &h3RunningSource{identities: []terminalcore.TerminalIdentity{wezterm}}, now)
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{BridgeAffinity: &bridge}, terminalcore.SourceSingleRunning, "wezterm")
	})

	t.Run("multiple running terminals are ambiguous", func(t *testing.T) {
		registry := h3Registry(t)
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, &h3RunningSource{identities: []terminalcore.TerminalIdentity{ghostty, wezterm}}, now)
		got, err := resolver.Resolve(t.Context(), terminalapp.ResolveRequest{})
		if err != nil || got.Resolution.Selected != nil {
			t.Fatalf("resolution=%#v err=%v", got.Resolution, err)
		}
	})

	t.Run("recent freshness expires", func(t *testing.T) {
		registry := h3Registry(t)
		mustObserveH3(t, registry, ghostty, now.Add(-3*time.Minute))
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, &h3RunningSource{}, now)
		got, err := resolver.Resolve(t.Context(), terminalapp.ResolveRequest{})
		if err != nil || got.Resolution.Selected != nil {
			t.Fatalf("expired recent resolution=%#v err=%v", got.Resolution, err)
		}
	})

	t.Run("bridge hint is not stored preference", func(t *testing.T) {
		registry := h3Registry(t)
		running := &h3RunningSource{identities: []terminalcore.TerminalIdentity{wezterm}}
		resolver := h3Resolver(t, registry, &h3ActivitySource{current: h3Observation(nil, now)}, running, now)
		bridge := h3Evidence(ghostty, terminalcore.SourceBridgeAffinity, now, now.Add(time.Minute), terminalcore.QualityValidated)
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{BridgeAffinity: &bridge}, terminalcore.SourceBridgeAffinity, "ghostty")
		assertH3Selected(t, resolver, terminalapp.ResolveRequest{}, terminalcore.SourceSingleRunning, "wezterm")
	})
}

func TestTerminalPresentationGUIRetryMatrix(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 10, 0, 0, time.UTC)
	resolution := h3Resolution(h3TerminalIdentity("ghostty", "com.mitchellh.ghostty"), terminalcore.SourceSingleRunning, now)
	argv := []string{"/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-h3-retry"}

	t.Run("lost launcher response and duplicate retry never relaunch", func(t *testing.T) {
		store := &h3MemoryLaunchStore{}
		launcher := &h3LaunchExecutor{result: terminalapp.LaunchResult{Attempted: true, Outcome: terminalcore.LaunchOutcomeUnknown, ProviderID: "ghostty", Reason: "client_not_proven"}}
		prover := &h3ExactProver{answers: []bool{false, false}}
		svc := terminalapp.NewLaunchService(store, launcher, prover)
		for i := 0; i < 2; i++ {
			got, err := svc.EnsurePresented(t.Context(), "handoff-h3-retry", resolution, argv)
			if got.State != terminalcore.LaunchOutcomeUnknownState || failure.Public(err).Code != failure.TerminalLaunchUnknown {
				t.Fatalf("attempt %d record=%#v err=%v", i, got, err)
			}
		}
		if launcher.calls != 1 {
			t.Fatalf("unknown outcome relaunched GUI %d times", launcher.calls)
		}
	})

	t.Run("recovered launching inspects rather than blind retry", func(t *testing.T) {
		reservation, err := terminalapp.NewLaunchReservation("handoff-h3-retry", resolution.Selected.Evidence.Identity, argv)
		if err != nil {
			t.Fatal(err)
		}
		store := &h3MemoryLaunchStore{record: &reservation}
		launcher := &h3LaunchExecutor{}
		prover := &h3ExactProver{answers: []bool{false}}
		got, gotErr := terminalapp.NewLaunchService(store, launcher, prover).EnsurePresented(t.Context(), "handoff-h3-retry", resolution, argv)
		if got.State != terminalcore.LaunchOutcomeUnknownState || failure.Public(gotErr).Code != failure.TerminalLaunchUnknown || launcher.calls != 0 {
			t.Fatalf("record=%#v err=%v launch_calls=%d", got, gotErr, launcher.calls)
		}
	})

	t.Run("exact existing client promotes unknown without relaunch", func(t *testing.T) {
		reservation, err := terminalapp.NewLaunchReservation("handoff-h3-retry", resolution.Selected.Evidence.Identity, argv)
		if err != nil {
			t.Fatal(err)
		}
		reservation.State = terminalcore.LaunchOutcomeUnknownState
		reservation.FailureCode = failure.TerminalLaunchUnknown
		reservation.FailureReason = "client_not_proven"
		store := &h3MemoryLaunchStore{record: &reservation}
		launcher := &h3LaunchExecutor{}
		got, gotErr := terminalapp.NewLaunchService(store, launcher, &h3ExactProver{answers: []bool{true}}).EnsurePresented(t.Context(), "handoff-h3-retry", resolution, argv)
		if gotErr != nil || got.State != terminalcore.LaunchLaunchedAndClientProven || launcher.calls != 0 {
			t.Fatalf("record=%#v err=%v launch_calls=%d", got, gotErr, launcher.calls)
		}
	})

	t.Run("provider unavailable is durable known failure", func(t *testing.T) {
		store := &h3MemoryLaunchStore{}
		launchErr := failure.New(failure.TerminalLauncherUnavailable, map[string]string{"provider_id": "ghostty", "reason": "platform_or_runner_unavailable"}, nil)
		launcher := &h3LaunchExecutor{result: terminalapp.LaunchResult{Attempted: false, Outcome: terminalcore.LaunchOutcomeFailed, ProviderID: "ghostty", Reason: "launcher_unavailable"}, err: launchErr}
		svc := terminalapp.NewLaunchService(store, launcher, &h3ExactProver{})
		for i := 0; i < 2; i++ {
			got, err := svc.EnsurePresented(t.Context(), "handoff-h3-retry", resolution, argv)
			if got.State != terminalcore.LaunchFailed || failure.Public(err).Code != failure.TerminalLauncherUnavailable {
				t.Fatalf("attempt %d record=%#v err=%v", i, got, err)
			}
		}
		if launcher.calls != 1 {
			t.Fatalf("known unavailable provider relaunched %d times", launcher.calls)
		}
	})
}

func TestTerminalPresentationSharedResourceBoundAcross100Resolutions(t *testing.T) {
	now := time.Date(2026, 8, 20, 7, 20, 0, 0, time.UTC)
	registry := h3Registry(t)
	activity := newH3SharedActivity(now)
	running := &h3RunningSource{identities: []terminalcore.TerminalIdentity{h3TerminalIdentity("ghostty", "com.mitchellh.ghostty")}}
	resolver := h3Resolver(t, registry, activity, running, now)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	before := runtime.NumGoroutine()
	done := make(chan error, 1)
	go func() { done <- activity.Run(ctx, registry.Observe) }()
	<-activity.started
	withSharedSource := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		got, err := resolver.Resolve(t.Context(), terminalapp.ResolveRequest{})
		if err != nil || got.Resolution.Selected == nil {
			t.Fatalf("cycle %d resolution=%#v err=%v", i, got.Resolution, err)
		}
	}
	if activity.runCalls.Load() != 1 || activity.currentCalls.Load() != 100 || running.calls.Load() != 100 {
		t.Fatalf("run=%d current=%d running=%d", activity.runCalls.Load(), activity.currentCalls.Load(), running.calls.Load())
	}
	if after := runtime.NumGoroutine(); after > withSharedSource+2 {
		t.Fatalf("resolution cycles leaked goroutines: before=%d shared=%d after=%d", before, withSharedSource, after)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("shared activity source exit=%v", err)
	}
}

func TestTerminalPresentationGhosttyNativeLauncherSmoke(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("SHELLBEAM_NATIVE_TERMINAL_SMOKE") != "1" {
		t.Skip("set SHELLBEAM_NATIVE_TERMINAL_SMOKE=1 on Darwin for promoted-provider GUI smoke")
	}
	if out, err := exec.Command("/usr/bin/lsappinfo", "find", "bundleid=com.mitchellh.ghostty").CombinedOutput(); err != nil || !strings.Contains(string(out), "ASN:") {
		t.Skipf("Ghostty is not running: %s err=%v", out, err)
	}
	before := h3GhosttyPIDs(t)
	dir := t.TempDir()
	wrapper := dir + "/shellbeam"
	marker := dir + "/native-smoke.txt"
	script := "#!/bin/sh\ndir=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd)\n" +
		"{ if [ -t 0 ]; then echo tty=1; else echo tty=0; fi; printf 'arg=%s\\n' \"$@\"; } > \"$dir/native-smoke.txt\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	argv, err := terminalapp.BuildAttachArgv(wrapper, "handoff-h3-native-smoke")
	if err != nil {
		t.Fatal(err)
	}
	request, err := terminalapp.NewLaunchRequest(h3TerminalIdentity("ghostty", "com.mitchellh.ghostty"), argv)
	if err != nil {
		t.Fatal(err)
	}
	launcher := terminaladapter.NewLauncher(string(terminalcore.PlatformDarwin), terminaladapter.ExecLaunchRunner{})
	result, err := launcher.Launch(t.Context(), request)
	if err != nil || !result.Attempted || result.Outcome != terminalcore.LaunchOutcomeUnknown {
		t.Fatalf("native launch result=%#v err=%v", result, err)
	}
	t.Cleanup(func() { h3TerminateNewGhostty(before) })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if data, readErr := os.ReadFile(marker); readErr == nil {
			text := string(data)
			for _, want := range []string{"tty=1", "arg=session", "arg=attach", "arg=--handoff-id", "arg=handoff-h3-native-smoke"} {
				if !strings.Contains(text, want) {
					t.Fatalf("native marker missing %q: %s", want, text)
				}
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("Ghostty did not execute exact ShellBeam attach argv within native smoke budget")
}

type h3ActivitySource struct {
	current terminalapp.ForegroundObservation
	calls   atomic.Int32
}

func (s *h3ActivitySource) Current(context.Context) (terminalapp.ForegroundObservation, error) {
	s.calls.Add(1)
	return s.current, nil
}
func (s *h3ActivitySource) Run(ctx context.Context, _ func(terminalapp.ForegroundObservation) error) error {
	<-ctx.Done()
	return ctx.Err()
}

type h3RunningSource struct {
	identities []terminalcore.TerminalIdentity
	calls      atomic.Int32
}

func (s *h3RunningSource) Running(context.Context) ([]terminalcore.TerminalIdentity, error) {
	s.calls.Add(1)
	return append([]terminalcore.TerminalIdentity(nil), s.identities...), nil
}

type h3SharedActivity struct {
	current      terminalapp.ForegroundObservation
	started      chan struct{}
	startOnce    sync.Once
	runCalls     atomic.Int32
	currentCalls atomic.Int32
}

func newH3SharedActivity(now time.Time) *h3SharedActivity {
	return &h3SharedActivity{current: h3Observation(nil, now), started: make(chan struct{})}
}
func (s *h3SharedActivity) Current(context.Context) (terminalapp.ForegroundObservation, error) {
	s.currentCalls.Add(1)
	return s.current, nil
}
func (s *h3SharedActivity) Run(ctx context.Context, _ func(terminalapp.ForegroundObservation) error) error {
	s.runCalls.Add(1)
	s.startOnce.Do(func() { close(s.started) })
	<-ctx.Done()
	return ctx.Err()
}

type h3MemoryLaunchStore struct{ record *terminalapp.LaunchRecord }

func (s *h3MemoryLaunchStore) ReserveTerminalLaunch(_ context.Context, want terminalapp.LaunchRecord) (terminalapp.LaunchRecord, bool, error) {
	if s.record != nil {
		return *s.record, false, nil
	}
	copy := want
	s.record = &copy
	return copy, true, nil
}
func (s *h3MemoryLaunchStore) CompleteTerminalLaunch(_ context.Context, want terminalapp.LaunchRecord) (terminalapp.LaunchRecord, error) {
	copy := want
	s.record = &copy
	return copy, nil
}

type h3LaunchExecutor struct {
	result terminalapp.LaunchResult
	err    error
	calls  int
}

func (l *h3LaunchExecutor) Launch(context.Context, terminalapp.LaunchRequest) (terminalapp.LaunchResult, error) {
	l.calls++
	return l.result, l.err
}

type h3ExactProver struct {
	answers []bool
	calls   int
}

func (p *h3ExactProver) ExactHumanClientPresent(context.Context, string) (bool, error) {
	p.calls++
	if len(p.answers) == 0 {
		return false, nil
	}
	answer := p.answers[0]
	p.answers = p.answers[1:]
	return answer, nil
}

func h3Registry(t *testing.T) *terminalapp.RecentRegistry {
	t.Helper()
	registry, err := terminalapp.NewRecentRegistry(5*time.Second, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func h3Resolver(t *testing.T, registry *terminalapp.RecentRegistry, activity terminalapp.ActivitySource, running terminalapp.RunningSource, now time.Time) *terminalapp.Resolver {
	t.Helper()
	resolver, err := terminalapp.NewResolver(registry, activity, running, 5*time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

func h3TerminalIdentity(providerID, bundleID string) terminalcore.TerminalIdentity {
	return terminalcore.TerminalIdentity{ProviderID: providerID, ProviderVersion: 1, Platform: terminalcore.PlatformDarwin, BundleID: bundleID, ExecutableName: providerID}
}

func h3Observation(identity *terminalcore.TerminalIdentity, observedAt time.Time) terminalapp.ForegroundObservation {
	return terminalapp.ForegroundObservation{Identity: identity, ObservedAt: observedAt, Quality: terminalcore.QualityNative}
}

func mustObserveH3(t *testing.T, registry *terminalapp.RecentRegistry, identity terminalcore.TerminalIdentity, observedAt time.Time) {
	t.Helper()
	if err := registry.Observe(h3Observation(&identity, observedAt)); err != nil {
		t.Fatal(err)
	}
}

func h3Evidence(identity terminalcore.TerminalIdentity, source terminalcore.EvidenceSource, observedAt, freshUntil time.Time, quality terminalcore.EvidenceQuality) terminalcore.Evidence {
	return terminalcore.Evidence{Identity: identity, Source: source, ObservedAt: observedAt, FreshUntil: freshUntil, Quality: quality}
}

func h3Resolution(identity terminalcore.TerminalIdentity, source terminalcore.EvidenceSource, now time.Time) terminalcore.Resolution {
	candidate := terminalcore.Candidate{Evidence: h3Evidence(identity, source, now, now.Add(time.Minute), terminalcore.QualityNative)}
	return terminalcore.Resolution{Selected: &candidate}
}

func assertH3Selected(t *testing.T, resolver *terminalapp.Resolver, request terminalapp.ResolveRequest, source terminalcore.EvidenceSource, providerID string) {
	t.Helper()
	got, err := resolver.Resolve(t.Context(), request)
	if err != nil || got.Resolution.Selected == nil {
		t.Fatalf("resolution=%#v err=%v", got.Resolution, err)
	}
	selected := got.Resolution.Selected.Evidence
	if selected.Source != source || selected.Identity.ProviderID != providerID {
		t.Fatalf("selected source=%q provider=%q want source=%q provider=%q", selected.Source, selected.Identity.ProviderID, source, providerID)
	}
}

func h3GhosttyPIDs(t *testing.T) map[int]struct{} {
	t.Helper()
	out, err := exec.Command("pgrep", "-x", "ghostty").Output()
	if err != nil && len(out) == 0 {
		return map[int]struct{}{}
	}
	result := map[int]struct{}{}
	for _, field := range strings.Fields(string(out)) {
		pid, parseErr := strconv.Atoi(field)
		if parseErr == nil {
			result[pid] = struct{}{}
		}
	}
	return result
}

func h3TerminateNewGhostty(before map[int]struct{}) {
	out, _ := exec.Command("pgrep", "-x", "ghostty").Output()
	pids := make([]int, 0)
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err == nil {
			if _, existed := before[pid]; !existed {
				pids = append(pids, pid)
			}
		}
	}
	sort.Ints(pids)
	for _, pid := range pids {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Signal(syscall.SIGTERM)
		}
	}
}
