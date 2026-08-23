package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type nativeProbeEnv struct {
	Tmux   string
	RawDir string
}

type nativeProbeFunc func(context.Context, nativeProbeEnv) ProbeResult

const p3Iterations = 1000

func probeP0PrivateServerIdentity(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	var raw strings.Builder
	before, err := snapshotDefaultSocket()
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "snapshot default tmux socket: " + err.Error()}, raw.String())
	}
	root, err := newProbeFixtureRoot()
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "create private fixture root: " + err.Error()}, raw.String())
	}
	f, err := newNativeFixture(ctx, env.Tmux, root)
	if err != nil {
		_ = os.RemoveAll(root)
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "start private tmux server: " + err.Error()}, raw.String())
	}
	defer f.close(context.Background())

	info, err := os.Stat(f.SocketPath)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "stat private socket: " + err.Error()}, raw.String())
	}
	if info.Mode().Perm()&0o077 != 0 {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: fmt.Sprintf("private socket permissions %#o expose group/other bits", info.Mode().Perm())}, raw.String())
	}
	rootClean := filepath.Clean(f.Root) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(f.SocketPath), rootClean) {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "private socket escaped H0 root"}, raw.String())
	}
	identity, err := f.serverIdentity(ctx)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "read server identity: " + err.Error()}, raw.String())
	}
	if identity.SocketPath != f.SocketPath || identity.Version == "" || identity.PID <= 0 {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: fmt.Sprintf("server identity mismatch: %#v", identity)}, raw.String())
	}
	pasteTime, err := f.tmux(ctx, "show-options", "-gv", "assume-paste-time")
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: "read assume-paste-time: " + err.Error()}, raw.String())
	}
	if strings.TrimSpace(string(pasteTime)) != "0" {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: fmt.Sprintf("assume-paste-time=%q want 0", strings.TrimSpace(string(pasteTime)))}, raw.String())
	}
	if err := assertDefaultSocketUnchanged(before); err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusFail, Summary: err.Error()}, raw.String())
	}
	facts["private_socket"] = f.SocketPath
	facts["socket_mode"] = fmt.Sprintf("%#o", info.Mode().Perm())
	facts["server_pid"] = strconv.Itoa(identity.PID)
	facts["server_version"] = identity.Version
	facts["assume_paste_time"] = "0"
	facts["default_server_untouched"] = "true"
	fmt.Fprintf(&raw, "socket=%s\nmode=%#o\npid=%d\nversion=%s\nassume-paste-time=0\ndefault-server-untouched=true\n", f.SocketPath, info.Mode().Perm(), identity.PID, identity.Version)
	return finishNativeProbe(env, ProbeResult{ID: "P0", Status: StatusPass, Summary: "private tmux server/socket identity is isolated and H0 control-key paste heuristic is disabled", Facts: facts}, raw.String())
}

func probeP1ExactHumanClientIdentity(ctx context.Context, env nativeProbeEnv) ProbeResult {
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P1", "exec /bin/sh")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	first, err := f.attachHuman(ctx, true)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: "attach first human client: " + err.Error()}, raw.String())
	}
	defer first.close()
	second, err := f.attachHuman(ctx, true)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: "attach second human client: " + err.Error()}, raw.String())
	}
	defer second.close()
	firstFacts, err := f.waitClientByPID(ctx, first.PID())
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: "resolve first exact client: " + err.Error()}, raw.String())
	}
	secondFacts, err := f.waitClientByPID(ctx, second.PID())
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: "resolve second exact client: " + err.Error()}, raw.String())
	}
	if firstFacts.Name == secondFacts.Name || firstFacts.TTY == secondFacts.TTY || firstFacts.PID == secondFacts.PID {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: fmt.Sprintf("clients are not exact/distinct: first=%#v second=%#v", firstFacts, secondFacts)}, raw.String())
	}
	if !firstFacts.ReadOnly || !secondFacts.ReadOnly {
		return finishNativeProbe(env, ProbeResult{ID: "P1", Status: StatusFail, Summary: fmt.Sprintf("initial read-only flags missing: first=%#v second=%#v", firstFacts, secondFacts)}, raw.String())
	}
	fmt.Fprintf(&raw, "first=%s|%s|%d|%t|%s\nsecond=%s|%s|%d|%t|%s\n", firstFacts.Name, firstFacts.TTY, firstFacts.PID, firstFacts.ReadOnly, firstFacts.Flags, secondFacts.Name, secondFacts.TTY, secondFacts.PID, secondFacts.ReadOnly, secondFacts.Flags)
	return finishNativeProbe(env, ProbeResult{
		ID:      "P1",
		Status:  StatusPass,
		Summary: "two real PTY clients have stable distinct name/tty/pid identity",
		Facts: map[string]string{
			"identity_fields": "client_name,client_tty,client_pid,client_readonly,client_flags",
			"first_client":    firstFacts.Name,
			"second_client":   secondFacts.Name,
		},
	}, raw.String())
}

func probeP2ExactClientFlagIsolation(ctx context.Context, env nativeProbeEnv) ProbeResult {
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P2", "exec /bin/sh")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	first, err := f.attachHuman(ctx, true)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "attach first human client: " + err.Error()}, raw.String())
	}
	defer first.close()
	second, err := f.attachHuman(ctx, true)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "attach second human client: " + err.Error()}, raw.String())
	}
	defer second.close()
	firstFacts, err := f.waitClientByPID(ctx, first.PID())
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "resolve first exact client: " + err.Error()}, raw.String())
	}
	secondFacts, err := f.waitClientByPID(ctx, second.PID())
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "resolve second exact client: " + err.Error()}, raw.String())
	}
	if err := f.setClientReadOnly(ctx, firstFacts.Name, false); err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "make exact first client writable: " + err.Error()}, raw.String())
	}
	firstAfter, err := f.waitClientReadOnly(ctx, firstFacts.Name, false)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "observe first client writable: " + err.Error()}, raw.String())
	}
	secondAfter, err := f.clientByName(ctx, secondFacts.Name)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "re-read second client: " + err.Error()}, raw.String())
	}
	if firstAfter.ReadOnly || !secondAfter.ReadOnly {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: fmt.Sprintf("flag isolation failed: first=%#v second=%#v", firstAfter, secondAfter)}, raw.String())
	}
	if err := f.setClientReadOnly(ctx, firstFacts.Name, true); err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "restore exact first client read-only: " + err.Error()}, raw.String())
	}
	if _, err := f.waitClientReadOnly(ctx, firstFacts.Name, true); err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "observe restored first client read-only: " + err.Error()}, raw.String())
	}
	if err := f.setClientReadOnly(ctx, "definitely-missing-client", false); err == nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "missing exact client target was accepted"}, raw.String())
	}
	if _, err := selectExactClient([]clientFacts{{Name: "dup", PID: 1}, {Name: "dup", PID: 2}}, "dup"); err == nil {
		return finishNativeProbe(env, ProbeResult{ID: "P2", Status: StatusFail, Summary: "ambiguous exact client identity was accepted"}, raw.String())
	}
	fmt.Fprintf(&raw, "first-before=%#v\nsecond-before=%#v\nfirst-writable=%#v\nsecond-still-readonly=%#v\nmissing-target=rejected\nambiguous-target=rejected\n", firstFacts, secondFacts, firstAfter, secondAfter)
	return finishNativeProbe(env, ProbeResult{
		ID:      "P2",
		Status:  StatusPass,
		Summary: "exact terminal client read-only state toggles without mutating another client; missing/ambiguous targets reject",
		Facts: map[string]string{
			"client_flag_control": "switch-client_-E_-c_exact-client_-r",
			"missing_target":      "rejected",
			"ambiguous_target":    "rejected",
		},
	}, raw.String())
}

func probeP3SameClientIngressFence(ctx context.Context, env nativeProbeEnv) ProbeResult {
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P3", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	if err := f.bindSameClientFence(ctx, "C-g"); err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "bind same-client fence key: " + err.Error()}, raw.String())
	}
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "attach writable human client: " + err.Error()}, raw.String())
	}
	defer human.close()
	facts, err := f.waitClientByPID(ctx, human.PID())
	if err != nil {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "resolve exact human client: " + err.Error()}, raw.String())
	}
	if facts.ReadOnly {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "human client unexpectedly starts read-only"}, raw.String())
	}

	for i := 0; i < p3Iterations; i++ {
		pre := fmt.Sprintf("H0_PRE_%04d", i)
		post := fmt.Sprintf("H0_POST_%04d", i)
		if err := human.writeString(pre + "\n"); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d pre write: %v", i, err)}, raw.String())
		}
		if err := human.waitContains(ctx, pre); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d positive-control observe: %v", i, err)}, raw.String())
		}
		if err := human.writeBytes([]byte{0x07}); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d fence key: %v", i, err)}, raw.String())
		}
		if _, err := f.waitClientReadOnly(ctx, facts.Name, true); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d fence ack: %v", i, err)}, human.outputString())
		}
		if err := human.writeString(post + "\n"); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d post-fence write: %v", i, err)}, human.outputString())
		}
		// H0-only reset on the same stream. With assume-paste-time=0, tmux
		// continues processing bindings for fast input. While read-only, normal
		// marker bytes are rejected but switch-client bindings remain allowed.
		if err := human.writeBytes([]byte{0x07}); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d reset key: %v", i, err)}, human.outputString())
		}
		if _, err := f.waitClientReadOnly(ctx, facts.Name, false); err != nil {
			return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("iteration %d writable ack: %v", i, err)}, human.outputString())
		}
	}
	output := human.outputString()
	if !strings.Contains(output, fmt.Sprintf("H0_PRE_%04d", p3Iterations-1)) {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "positive-control tail marker missing"}, output)
	}
	if strings.Contains(output, "H0_POST_") {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: "post-fence marker reached live pane output; native ingress fence not proven"}, output)
	}
	if got := strings.Count(output, "H0_PRE_"); got < p3Iterations {
		return finishNativeProbe(env, ProbeResult{ID: "P3", Status: StatusFail, Summary: fmt.Sprintf("positive-control count=%d want >=%d", got, p3Iterations)}, output)
	}
	return finishNativeProbe(env, ProbeResult{
		ID:      "P3",
		Status:  StatusPass,
		Summary: "same human-client stream rejected every post-fence marker after acknowledged read-only transition; no PTY/application quiescence claim",
		Facts: map[string]string{
			"input_fence_mechanism": "tmux_same_client_switch-client_-E_-r_assume-paste-time_0",
			"iterations":            strconv.Itoa(p3Iterations),
			"post_fence_admitted":   "0",
			"quiescence_claimed":    "false",
		},
	}, output)
}

func newProbeFixture(ctx context.Context, env nativeProbeEnv, id, command string) (*nativeFixture, func(), *ProbeResult) {
	root, err := newProbeFixtureRoot()
	if err != nil {
		result := ProbeResult{ID: id, Status: StatusFail, Summary: "create private fixture root: " + err.Error()}
		return nil, func() {}, &result
	}
	f, err := newNativeFixtureWithCommand(ctx, env.Tmux, root, command)
	if err != nil {
		_ = os.RemoveAll(root)
		result := ProbeResult{ID: id, Status: StatusFail, Summary: "start private tmux fixture: " + err.Error()}
		return nil, func() {}, &result
	}
	return f, func() { _ = f.close(context.Background()) }, nil
}

func newProbeFixtureRoot() (string, error) {
	root, err := os.MkdirTemp("/tmp", "sb-h0-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return "", err
	}
	return root, nil
}

func finishNativeProbe(env nativeProbeEnv, result ProbeResult, raw string) ProbeResult {
	if result.Facts == nil {
		result.Facts = map[string]string{}
	}
	if env.RawDir == "" {
		return result
	}
	payload := []byte(fmt.Sprintf("probe=%s\nstatus=%s\nsummary=%s\n%s", result.ID, result.Status, result.Summary, raw))
	if err := writeFileAtomic(filepath.Join(env.RawDir, result.ID+".txt"), payload, 0o600); err != nil {
		result.Status = StatusFail
		result.Summary = "write raw transcript: " + err.Error()
	}
	return result
}

type defaultSocketState struct {
	path    string
	exists  bool
	mode    os.FileMode
	size    int64
	modTime int64
}

func snapshotDefaultSocket() (defaultSocketState, error) {
	base := os.Getenv("TMUX_TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	state := defaultSocketState{path: filepath.Join(base, fmt.Sprintf("tmux-%d", os.Getuid()), "default")}
	info, err := os.Stat(state.path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.exists = true
	state.mode = info.Mode()
	state.size = info.Size()
	state.modTime = info.ModTime().UnixNano()
	return state, nil
}

func assertDefaultSocketUnchanged(before defaultSocketState) error {
	info, err := os.Stat(before.path)
	if !before.exists {
		if err == nil {
			return fmt.Errorf("default tmux socket unexpectedly appeared at %s", before.path)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("stat default tmux socket: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("default tmux socket changed/disappeared: %w", err)
	}
	if info.Mode() != before.mode || info.Size() != before.size || info.ModTime().UnixNano() != before.modTime {
		return fmt.Errorf("default tmux socket metadata changed")
	}
	return nil
}
