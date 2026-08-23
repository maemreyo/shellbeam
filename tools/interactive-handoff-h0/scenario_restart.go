package main

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type tmuxObjectIdentity struct {
	ServerPID int
	SessionID string
	WindowID  string
	PaneID    string
}

func probeP11CrashReconnectIdentity(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	f, cleanup, failure := newProbeFixture(ctx, env, "P11", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, "")
	}
	defer cleanup()
	initial, err := readTmuxObjectIdentity(ctx, f)
	if err != nil {
		return restartFailure(env, err)
	}
	if err := proveControlClientLossIdentity(ctx, f, initial); err != nil {
		return restartFailure(env, err)
	}
	facts["control_client_loss"] = "recoverable_same_object_identity"
	if err := proveHumanClientLossIdentity(ctx, f, initial); err != nil {
		return restartFailure(env, err)
	}
	facts["human_client_loss"] = "recoverable_same_object_identity"
	if err := proveObserverRestartIdentity(ctx, f, initial); err != nil {
		return restartFailure(env, err)
	}
	facts["observer_restart"] = "recoverable_same_object_identity"
	recreated, err := proveServerLossIsNewIncarnation(ctx, f, initial)
	if err != nil {
		return restartFailure(env, err)
	}
	facts["server_loss"] = "provider_lost"
	facts["friendly_name_recreation"] = "not_continuation"
	facts["server_incarnation_changed"] = "true"
	facts["object_ids_reused_after_server_loss"] = strconv.FormatBool(initial.SessionID == recreated.SessionID && initial.WindowID == recreated.WindowID && initial.PaneID == recreated.PaneID)
	raw := fmt.Sprintf("initial=%s\nclient-loss=stable\nhuman-loss=stable\nobserver-restart=stable\nrecreated=%s\nprovider-loss=true\n", formatObjectIdentity(initial), formatObjectIdentity(recreated))
	return finishNativeProbe(env, ProbeResult{ID: "P11", Status: StatusPass, Summary: "client/observer loss preserves tmux object identity; server loss creates a new provider incarnation even under the same friendly name", Facts: facts}, raw)
}

func readTmuxObjectIdentity(ctx context.Context, f *nativeFixture) (tmuxObjectIdentity, error) {
	server, err := f.serverIdentity(ctx)
	if err != nil {
		return tmuxObjectIdentity{}, err
	}
	out, err := f.tmux(ctx, "display-message", "-p", "-t", f.Session, "#{session_id}|#{window_id}|#{pane_id}")
	if err != nil {
		return tmuxObjectIdentity{}, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return tmuxObjectIdentity{}, fmt.Errorf("invalid tmux object identity %q", out)
	}
	return tmuxObjectIdentity{ServerPID: server.PID, SessionID: parts[0], WindowID: parts[1], PaneID: parts[2]}, nil
}

func proveControlClientLossIdentity(ctx context.Context, f *nativeFixture, want tmuxObjectIdentity) error {
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return err
	}
	if err := ctrl.close(); err != nil {
		return err
	}
	return requireSameObjectIdentity(ctx, f, want)
}

func proveHumanClientLossIdentity(ctx context.Context, f *nativeFixture, want tmuxObjectIdentity) error {
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		return err
	}
	if _, err := f.waitClientByPID(ctx, human.PID()); err != nil {
		_ = human.close()
		return err
	}
	if err := human.close(); err != nil {
		return err
	}
	return requireSameObjectIdentity(ctx, f, want)
}

func proveObserverRestartIdentity(ctx context.Context, f *nativeFixture, want tmuxObjectIdentity) error {
	first, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return err
	}
	if err := first.close(); err != nil {
		return err
	}
	second, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return err
	}
	defer second.close()
	return requireSameObjectIdentity(ctx, f, want)
}

func requireSameObjectIdentity(ctx context.Context, f *nativeFixture, want tmuxObjectIdentity) error {
	got, err := readTmuxObjectIdentity(ctx, f)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("object identity changed: got=%s want=%s", formatObjectIdentity(got), formatObjectIdentity(want))
	}
	return nil
}

func proveServerLossIsNewIncarnation(ctx context.Context, f *nativeFixture, old tmuxObjectIdentity) (tmuxObjectIdentity, error) {
	if _, err := f.tmux(ctx, "kill-server"); err != nil {
		return tmuxObjectIdentity{}, err
	}
	if _, err := f.serverIdentity(ctx); err == nil {
		return tmuxObjectIdentity{}, fmt.Errorf("server identity still available after kill-server")
	}
	args := []string{"-S", f.SocketPath, "-f", "/dev/null", "new-session", "-d", "-s", f.Session, "stty -echo; exec cat"}
	out, err := exec.CommandContext(ctx, f.Tmux, args...).CombinedOutput()
	if err != nil {
		return tmuxObjectIdentity{}, fmt.Errorf("recreate friendly session: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := f.tmux(ctx, "set-option", "-g", "assume-paste-time", "0"); err != nil {
		return tmuxObjectIdentity{}, err
	}
	got, err := readTmuxObjectIdentity(ctx, f)
	if err != nil {
		return tmuxObjectIdentity{}, err
	}
	if got.ServerPID == old.ServerPID {
		return tmuxObjectIdentity{}, fmt.Errorf("server PID did not change across provider loss")
	}
	return got, nil
}

func formatObjectIdentity(id tmuxObjectIdentity) string {
	return fmt.Sprintf("pid=%d session=%s window=%s pane=%s", id.ServerPID, id.SessionID, id.WindowID, id.PaneID)
}

func restartFailure(env nativeProbeEnv, err error) ProbeResult {
	return finishNativeProbe(env, ProbeResult{ID: "P11", Status: StatusFail, Summary: err.Error()}, "")
}
