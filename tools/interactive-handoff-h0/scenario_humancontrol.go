package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	f11Sequence = "\x1b[23~"
	f12Sequence = "\x1b[24~"
)

func probeP8WritableHumanControl(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P8", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	if _, err := f.tmux(ctx, "bind-key", "-n", "F12", "wait-for", "-S", "h0-p8-signal"); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "bind P8 signal", err)
	}
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		return humanControlFailure(env, "P8", raw.String(), "attach writable human", err)
	}
	defer human.close()
	if _, err := f.waitClientByPID(ctx, human.PID()); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "resolve human client", err)
	}
	before := len(human.outputString())
	waiter := startWaitFor(ctx, f, "h0-p8-signal")
	if err := human.writeString(f12Sequence); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "send P8 control key", err)
	}
	if err := waiter.wait(ctx); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "observe wait-for signal", err)
	}
	if err := human.writeString("P8_CHILD_ALIVE\n"); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "positive child marker", err)
	}
	if err := human.waitContains(ctx, "P8_CHILD_ALIVE"); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "foreground child positive control", err)
	}
	afterControl := outputSince(human, before)
	if strings.Contains(afterControl, f12Sequence) {
		return humanControlFailure(env, "P8", raw.String(), "foreground child received OOB control-key bytes", nil)
	}
	if err := proveShellCommandFallbackIsPaneInput(ctx, human); err != nil {
		return humanControlFailure(env, "P8", raw.String(), "shell fallback regression", err)
	}
	facts["signal_transport"] = "tmux_wait-for"
	facts["foreground_child_received_key"] = "false"
	facts["shell_prompt_required"] = "false"
	facts["shell_command_fallback"] = "pane_stdin_not_control_plane"
	facts["permanent_user_config_changed"] = "false"
	fmt.Fprintf(&raw, "transport=tmux-wait-for\nforeground=cat\ncontrol-key-in-pane=false\nshell-fallback-in-pane=true\nprivate-server-binding-only=true\n")
	return finishNativeProbe(env, ProbeResult{ID: "P8", Status: StatusPass, Summary: "writable human client reaches shell-independent OOB control while foreground child owns pane stdin", Facts: facts}, raw.String())
}

func probeP9ReadOnlyLocalControl(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P9", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	observer, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return humanControlFailure(env, "P9", raw.String(), "attach P9 observer", err)
	}
	defer observer.close()
	observer.clearEvents()
	if err := bindP9Controls(ctx, f); err != nil {
		return humanControlFailure(env, "P9", raw.String(), "bind P9 controls", err)
	}
	human, err := f.attachHuman(ctx, true)
	if err != nil {
		return humanControlFailure(env, "P9", raw.String(), "attach read-only human", err)
	}
	factsBefore, err := f.waitClientByPID(ctx, human.PID())
	if err != nil {
		_ = human.close()
		return humanControlFailure(env, "P9", raw.String(), "resolve read-only human", err)
	}
	if !factsBefore.ReadOnly {
		_ = human.close()
		return humanControlFailure(env, "P9", raw.String(), "human client is not read-only", nil)
	}
	if err := proveReadOnlyBlocksArbitraryBinding(ctx, f, human, observer); err != nil {
		_ = human.close()
		return humanControlFailure(env, "P9", raw.String(), "read-only arbitrary binding", err)
	}
	if err := proveReadOnlyDetach(ctx, f, human, factsBefore.Name, observer); err != nil {
		_ = human.close()
		return humanControlFailure(env, "P9", raw.String(), "read-only detach", err)
	}
	_ = human.close()
	localFacts, err := exerciseLocalControlSurface(ctx, f)
	if err != nil {
		return humanControlFailure(env, "P9", raw.String(), "local control surface", err)
	}
	for key, value := range localFacts {
		facts[key] = value
	}
	facts["arbitrary_binding_while_readonly"] = "blocked"
	facts["detach_while_readonly"] = "reachable"
	facts["ingress_proxy_introduced"] = "false"
	facts["pane_control_bytes_injected"] = "false"
	fmt.Fprintf(&raw, "read-only-arbitrary-binding=blocked\nread-only-detach=reachable\nlocal-actions=%s\ningress-proxy=false\npane-control-bytes=false\n", facts["local_actions"])
	return finishNativeProbe(env, ProbeResult{ID: "P9", Status: StatusPass, Summary: "read-only client can detach to a local control surface with resume/status/terminate reachable and no pane byte proxy", Facts: facts}, raw.String())
}

type waitForHandle struct{ done <-chan error }

func startWaitFor(ctx context.Context, f *nativeFixture, channel string) waitForHandle {
	done := make(chan error, 1)
	go func() {
		cmd := exec.CommandContext(ctx, f.Tmux, "-S", f.SocketPath, "-f", "/dev/null", "wait-for", channel)
		out, err := cmd.CombinedOutput()
		if err != nil {
			done <- fmt.Errorf("wait-for %s: %w: %s", channel, err, strings.TrimSpace(string(out)))
			return
		}
		done <- nil
	}()
	return waitForHandle{done: done}
}

func (h waitForHandle) wait(ctx context.Context) error {
	select {
	case err := <-h.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func proveShellCommandFallbackIsPaneInput(ctx context.Context, human *humanClient) error {
	const literal = "shellbeam handoff ready"
	before := len(human.outputString())
	if err := human.writeString(literal + "\n"); err != nil {
		return err
	}
	if err := human.waitContains(ctx, literal); err != nil {
		return err
	}
	if !strings.Contains(outputSince(human, before), literal) {
		return errors.New("literal manual-ready command did not reach foreground child stdin")
	}
	return nil
}

func bindP9Controls(ctx context.Context, f *nativeFixture) error {
	if _, err := f.tmux(ctx, "bind-key", "-n", "F11", "wait-for", "-S", "h0-p9-arbitrary"); err != nil {
		return err
	}
	_, err := f.tmux(ctx, "bind-key", "-n", "F12", "detach-client")
	return err
}

func proveReadOnlyBlocksArbitraryBinding(ctx context.Context, f *nativeFixture, human *humanClient, observer *controlClient) error {
	before := len(observer.eventsSnapshot())
	if err := human.writeString(f11Sequence); err != nil {
		return err
	}
	short, cancel := context.WithTimeout(ctx, 125*time.Millisecond)
	defer cancel()
	if err := waitForChannel(short, f, "h0-p9-arbitrary"); err == nil {
		return errors.New("arbitrary wait-for binding fired while client was read-only")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if paneEventsContainSequence(observer.eventsSnapshot()[before:], f11Sequence) {
		return errors.New("read-only arbitrary control key reached pane output")
	}
	return nil
}

func proveReadOnlyDetach(ctx context.Context, f *nativeFixture, human *humanClient, clientName string, observer *controlClient) error {
	before := len(observer.eventsSnapshot())
	if err := human.writeString(f12Sequence); err != nil {
		return err
	}
	if err := waitHumanDetached(ctx, human); err != nil {
		return err
	}
	if err := waitClientAbsent(ctx, f, clientName); err != nil {
		return err
	}
	if paneEventsContainSequence(observer.eventsSnapshot()[before:], f12Sequence) {
		return errors.New("detach control key reached pane output")
	}
	return nil
}

func waitForChannel(ctx context.Context, f *nativeFixture, channel string) error {
	cmd := exec.CommandContext(ctx, f.Tmux, "-S", f.SocketPath, "-f", "/dev/null", "wait-for", channel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("wait-for %s: %w: %s", channel, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitHumanDetached(ctx context.Context, human *humanClient) error {
	select {
	case <-human.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitClientAbsent(ctx context.Context, f *nativeFixture, name string) error {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := f.clientByName(ctx, name)
		if err != nil && strings.Contains(err.Error(), "not found") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func outputSince(human *humanClient, before int) string {
	output := human.outputString()
	if before < 0 || before > len(output) {
		return output
	}
	return output[before:]
}

func paneEventsContainSequence(events []ControlEvent, sequence string) bool {
	for _, event := range events {
		if event.Kind == EventPaneOutput && strings.Contains(event.Data, sequence) {
			return true
		}
	}
	return false
}

type h0LocalControlSurface struct{ fixture *nativeFixture }

func exerciseLocalControlSurface(ctx context.Context, f *nativeFixture) (map[string]string, error) {
	surface := h0LocalControlSurface{fixture: f}
	if _, err := surface.status(ctx); err != nil {
		return nil, err
	}
	resumed, err := surface.resume(ctx)
	if err != nil {
		return nil, err
	}
	facts, err := f.waitClientByPID(ctx, resumed.PID())
	if err != nil {
		_ = resumed.close()
		return nil, err
	}
	if facts.ReadOnly {
		_ = resumed.close()
		return nil, errors.New("local resume did not reattach writable human client")
	}
	_ = resumed.close()
	actions := []string{"resume", "status", "terminate"}
	sort.Strings(actions)
	// Reachability of terminate is exercised last because it destroys the H0
	// fixture session. It is a local tmux mutation, not a pane-input bypass.
	if err := surface.terminate(ctx); err != nil {
		return nil, err
	}
	return map[string]string{
		"local_actions":             strings.Join(actions, ","),
		"local_status_exercised":    "true",
		"local_resume_exercised":    "true",
		"local_terminate_exercised": "true",
	}, nil
}

func (s h0LocalControlSurface) status(ctx context.Context) (serverIdentity, error) {
	return s.fixture.serverIdentity(ctx)
}

func (s h0LocalControlSurface) resume(ctx context.Context) (*humanClient, error) {
	return s.fixture.attachHuman(ctx, false)
}

func (s h0LocalControlSurface) terminate(ctx context.Context) error {
	_, err := s.fixture.tmux(ctx, "kill-session", "-t", s.fixture.Session)
	return err
}

func humanControlFailure(env nativeProbeEnv, id, raw, stage string, err error) ProbeResult {
	summary := stage
	if err != nil {
		summary += ": " + err.Error()
	}
	return finishNativeProbe(env, ProbeResult{ID: id, Status: StatusFail, Summary: summary}, raw)
}
