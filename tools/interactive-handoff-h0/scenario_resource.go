package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const p13Cycles = 100

type resourceState struct {
	LiveServers   int
	LiveSockets   int
	LivePTYs      int
	LiveClients   int
	LiveHelpers   int
	LiveTempRoots int
}

type recordedFixtureResources struct {
	serverPID int
	humanPID  int
	helperPID int
	socket    string
	root      string
	pty       *os.File
}

func probeP13ResourceLeakStress(ctx context.Context, env nativeProbeEnv) ProbeResult {
	if raceEnabled {
		return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusNotRun, Summary: "P13 resource accounting is intentionally not run under the Go race runtime", Facts: map[string]string{"race_runtime_used": "true"}}, "")
	}
	baseline, err := sampleSelfResources()
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusFail, Summary: "baseline resource sample: " + err.Error()}, "")
	}
	start := time.Now()
	var raw strings.Builder
	totals := resourceState{}
	maxCycle := time.Duration(0)
	maxLive := p13LiveShape{}
	maxLiveSelf := baseline
	maxPostSelf := baseline
	for i := 0; i < p13Cycles; i++ {
		measurement, err := runP13Cycle(ctx, env.Tmux, i, baseline)
		if err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusFail, Summary: fmt.Sprintf("cycle %d: %v", i, err)}, raw.String())
		}
		addResourceState(&totals, measurement.Residual)
		maxLive = maxP13LiveShape(maxLive, measurement.LiveShape)
		maxLiveSelf = maxSelfResourceSnapshot(maxLiveSelf, measurement.LiveSelf)
		maxPostSelf = maxSelfResourceSnapshot(maxPostSelf, measurement.PostSelf)
		if measurement.Duration > maxCycle {
			maxCycle = measurement.Duration
		}
		fmt.Fprintf(&raw, "cycle=%03d duration_ms=%d session=%d pane=%d tmux_client=%d server_desc=%d root_entries=%d live_fd=%d live_g=%d post_fd=%d post_g=%d server=%d socket=%d pty=%d client=%d helper=%d root=%d\n",
			i, measurement.Duration.Milliseconds(), measurement.LiveShape.Sessions, measurement.LiveShape.Panes, measurement.LiveShape.Clients,
			measurement.LiveShape.ServerDescendants, measurement.LiveShape.RootEntries, measurement.LiveSelf.FDs, measurement.LiveSelf.Goroutines,
			measurement.PostSelf.FDs, measurement.PostSelf.Goroutines, measurement.Residual.LiveServers, measurement.Residual.LiveSockets,
			measurement.Residual.LivePTYs, measurement.Residual.LiveClients, measurement.Residual.LiveHelpers, measurement.Residual.LiveTempRoots)
	}
	finalSelf, err := waitSelfResourceConvergence(ctx, baseline, time.Second)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusFail, Summary: "final harness resource convergence: " + err.Error()}, raw.String())
	}
	facts := p13Facts(totals, baseline, finalSelf, maxLive, maxLiveSelf, maxPostSelf, maxCycle, time.Since(start))
	if !resourceStateZero(totals) {
		return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusFail, Summary: fmt.Sprintf("resource leaks detected after %d cycles: %#v", p13Cycles, totals), Facts: facts}, raw.String())
	}
	return finishNativeProbe(env, ProbeResult{ID: "P13", Status: StatusPass, Summary: "100 serial cycles preserve exact tmux live shape and converge server/socket/PTY/client/helper/temp-root plus H0 FD/goroutine resources to baseline", Facts: facts}, raw.String())
}

type p13CycleMeasurement struct {
	Residual  resourceState
	LiveShape p13LiveShape
	LiveSelf  selfResourceSnapshot
	PostSelf  selfResourceSnapshot
	Duration  time.Duration
}

func runP13Cycle(ctx context.Context, tmuxPath string, cycle int, baseline selfResourceSnapshot) (p13CycleMeasurement, error) {
	cycleStart := time.Now()
	root, err := newProbeFixtureRoot()
	if err != nil {
		return p13CycleMeasurement{}, err
	}
	f, err := newNativeFixtureWithCommand(ctx, tmuxPath, root, "stty -echo; exec cat")
	if err != nil {
		_ = os.RemoveAll(root)
		return p13CycleMeasurement{}, err
	}
	human, control, err := attachP13Clients(ctx, f)
	if err != nil {
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, err
	}
	recorded, err := recordFixtureResources(ctx, f, human, control)
	if err != nil {
		_ = human.close()
		_ = control.close()
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, err
	}
	liveShape, err := inspectP13LiveShape(ctx, f)
	if err != nil {
		_ = human.close()
		_ = control.close()
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, fmt.Errorf("inspect live shape: %w", err)
	}
	if err := validateP13LiveShape(liveShape); err != nil {
		_ = human.close()
		_ = control.close()
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, err
	}
	liveSelf, err := sampleSelfResources()
	if err != nil {
		_ = human.close()
		_ = control.close()
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, fmt.Errorf("sample live harness resources: %w", err)
	}
	if err := exerciseP13AuthorityToggle(ctx, f, human); err != nil {
		_ = human.close()
		_ = control.close()
		_ = f.close(context.Background())
		return p13CycleMeasurement{}, fmt.Errorf("authority toggle: %w", err)
	}
	if err := human.close(); err != nil {
		return p13CycleMeasurement{}, err
	}
	if err := control.close(); err != nil {
		return p13CycleMeasurement{}, err
	}
	if err := f.close(context.Background()); err != nil {
		return p13CycleMeasurement{}, err
	}
	residual, err := waitRecordedResourcesGone(ctx, recorded, 2*time.Second)
	if err != nil {
		return p13CycleMeasurement{Residual: residual}, fmt.Errorf("cycle %d cleanup: %w", cycle, err)
	}
	postSelf, err := waitSelfResourceConvergence(ctx, baseline, time.Second)
	if err != nil {
		return p13CycleMeasurement{Residual: residual, LiveShape: liveShape, LiveSelf: liveSelf}, fmt.Errorf("cycle %d harness convergence: %w", cycle, err)
	}
	return p13CycleMeasurement{Residual: residual, LiveShape: liveShape, LiveSelf: liveSelf, PostSelf: postSelf, Duration: time.Since(cycleStart)}, nil
}

func attachP13Clients(ctx context.Context, f *nativeFixture) (*humanClient, *controlClient, error) {
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		return nil, nil, err
	}
	if _, err := f.waitClientByPID(ctx, human.PID()); err != nil {
		_ = human.close()
		return nil, nil, err
	}
	control, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		_ = human.close()
		return nil, nil, err
	}
	return human, control, nil
}

func exerciseP13AuthorityToggle(ctx context.Context, f *nativeFixture, human *humanClient) error {
	facts, err := f.waitClientByPID(ctx, human.PID())
	if err != nil {
		return err
	}
	if err := f.setClientReadOnly(ctx, facts.Name, true); err != nil {
		return err
	}
	if _, err := f.waitClientReadOnly(ctx, facts.Name, true); err != nil {
		return err
	}
	if err := f.setClientReadOnly(ctx, facts.Name, false); err != nil {
		return err
	}
	_, err = f.waitClientReadOnly(ctx, facts.Name, false)
	return err
}

func recordFixtureResources(ctx context.Context, f *nativeFixture, human *humanClient, control *controlClient) (recordedFixtureResources, error) {
	identity, err := f.serverIdentity(ctx)
	if err != nil {
		return recordedFixtureResources{}, err
	}
	if control.cmd == nil || control.cmd.Process == nil {
		return recordedFixtureResources{}, errors.New("control helper has no process")
	}
	return recordedFixtureResources{
		serverPID: identity.PID,
		humanPID:  human.PID(),
		helperPID: control.cmd.Process.Pid,
		socket:    f.SocketPath,
		root:      f.Root,
		pty:       human.pty,
	}, nil
}

func inspectLiveFixtureResources(f *nativeFixture, human *humanClient, control *controlClient) resourceState {
	state := resourceState{}
	if f != nil {
		if identity, err := f.serverIdentity(context.Background()); err == nil && processAlive(identity.PID) {
			state.LiveServers = 1
		}
		if pathExists(f.SocketPath) {
			state.LiveSockets = 1
		}
		if pathExists(f.Root) {
			state.LiveTempRoots = 1
		}
	}
	if human != nil {
		if human.pty != nil {
			if _, err := human.pty.Stat(); err == nil {
				state.LivePTYs = 1
			}
		}
		if processAlive(human.PID()) {
			state.LiveClients = 1
		}
	}
	if control != nil && control.cmd != nil && control.cmd.Process != nil && processAlive(control.cmd.Process.Pid) {
		state.LiveHelpers = 1
	}
	return state
}

func inspectRecordedResources(r recordedFixtureResources) resourceState {
	state := resourceState{}
	if processAlive(r.serverPID) {
		state.LiveServers = 1
	}
	if pathExists(r.socket) {
		state.LiveSockets = 1
	}
	if r.pty != nil {
		if _, err := r.pty.Stat(); err == nil {
			state.LivePTYs = 1
		}
	}
	if processAlive(r.humanPID) {
		state.LiveClients = 1
	}
	if processAlive(r.helperPID) {
		state.LiveHelpers = 1
	}
	if pathExists(r.root) {
		state.LiveTempRoots = 1
	}
	return state
}

func waitRecordedResourcesGone(ctx context.Context, r recordedFixtureResources, timeout time.Duration) (resourceState, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := inspectRecordedResources(r)
		if resourceStateZero(state) {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-deadline.C:
			return state, fmt.Errorf("resources still live: %#v", state)
		case <-ticker.C:
		}
	}
}

func commandStillLive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	return cmd.ProcessState == nil || !cmd.ProcessState.Exited()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func pathExists(path string) bool { _, err := os.Stat(path); return err == nil }

func resourceStateZero(s resourceState) bool {
	return s.LiveServers == 0 && s.LiveSockets == 0 && s.LivePTYs == 0 && s.LiveClients == 0 && s.LiveHelpers == 0 && s.LiveTempRoots == 0
}

func addResourceState(dst *resourceState, src resourceState) {
	dst.LiveServers += src.LiveServers
	dst.LiveSockets += src.LiveSockets
	dst.LivePTYs += src.LivePTYs
	dst.LiveClients += src.LiveClients
	dst.LiveHelpers += src.LiveHelpers
	dst.LiveTempRoots += src.LiveTempRoots
}

func p13Facts(totals resourceState, baseline, finalSelf selfResourceSnapshot, maxLive p13LiveShape, maxLiveSelf, maxPostSelf selfResourceSnapshot, maxCycle, total time.Duration) map[string]string {
	return map[string]string{
		"cycles":         strconv.Itoa(p13Cycles),
		"leaked_servers": strconv.Itoa(totals.LiveServers), "leaked_sockets": strconv.Itoa(totals.LiveSockets),
		"leaked_ptys": strconv.Itoa(totals.LivePTYs), "leaked_clients": strconv.Itoa(totals.LiveClients), "leaked_helpers": strconv.Itoa(totals.LiveHelpers),
		"leaked_temp_roots": strconv.Itoa(totals.LiveTempRoots), "race_runtime_used": "false",
		"baseline_self_fds": strconv.Itoa(baseline.FDs), "final_self_fds": strconv.Itoa(finalSelf.FDs), "max_live_self_fds": strconv.Itoa(maxLiveSelf.FDs), "max_post_cleanup_self_fds": strconv.Itoa(maxPostSelf.FDs),
		"baseline_goroutines": strconv.Itoa(baseline.Goroutines), "final_goroutines": strconv.Itoa(finalSelf.Goroutines), "max_live_goroutines": strconv.Itoa(maxLiveSelf.Goroutines), "max_post_cleanup_goroutines": strconv.Itoa(maxPostSelf.Goroutines),
		"max_live_sessions": strconv.Itoa(maxLive.Sessions), "max_live_panes": strconv.Itoa(maxLive.Panes), "max_live_tmux_clients": strconv.Itoa(maxLive.Clients),
		"max_server_descendants": strconv.Itoa(maxLive.ServerDescendants), "max_live_root_entries": strconv.Itoa(maxLive.RootEntries),
		"max_cycle_ms": strconv.FormatInt(maxCycle.Milliseconds(), 10), "total_ms": strconv.FormatInt(total.Milliseconds(), 10),
	}
}
