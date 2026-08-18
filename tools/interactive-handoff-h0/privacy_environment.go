package main

import (
	"context"
	"fmt"
	"strings"
)

const (
	p7SSHKey         = "SSH_AUTH_SOCK"
	p7DisplayKey     = "DISPLAY"
	p7SSHSession     = "/h0/session-A"
	p7DisplaySession = ":h0-session"
	p7SSHAttach      = "/h0/attach-B"
	p7DisplayAttach  = ":h0-attach"
)

type p7Pair struct {
	ssh     string
	display string
}

func probeP7EnvironmentPreservation(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P7", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, raw.String())
	}
	defer cleanup()
	if err := p7RequireUpdateEnvironment(ctx, f); err != nil {
		return p7Failure(env, raw.String(), err)
	}

	attachEnv := map[string]string{p7SSHKey: p7SSHAttach, p7DisplayKey: p7DisplayAttach}
	if err := p7SetSessionPair(ctx, f); err != nil {
		return p7Failure(env, raw.String(), err)
	}
	negative, err := p7AttachAndRead(ctx, f, false, attachEnv)
	if err != nil || negative != (p7Pair{p7SSHAttach, p7DisplayAttach}) {
		return p7Failure(env, raw.String(), fmt.Errorf("negative control without -E: pair=%#v err=%v", negative, err))
	}
	facts["negative_control_without_E"] = "mutated"

	if err := p7SetSessionPair(ctx, f); err != nil {
		return p7Failure(env, raw.String(), err)
	}
	positive, client, err := p7AttachPreservedAndKeepClient(ctx, f, attachEnv)
	if err != nil || positive != (p7Pair{p7SSHSession, p7DisplaySession}) {
		return p7Failure(env, raw.String(), fmt.Errorf("attach -E: pair=%#v err=%v", positive, err))
	}
	facts["attach_with_E"] = "preserved"
	defer client.close()

	switched, err := p7SwitchAndRead(ctx, f, client)
	if err != nil || switched != (p7Pair{p7SSHSession, p7DisplaySession}) {
		return p7Failure(env, raw.String(), fmt.Errorf("switch-client -E: pair=%#v err=%v", switched, err))
	}
	facts["switch_with_E"] = "preserved"
	_ = client.close()

	control, err := p7ControlAttachAndRead(ctx, f, attachEnv)
	if err != nil || control != (p7Pair{p7SSHSession, p7DisplaySession}) {
		return p7Failure(env, raw.String(), fmt.Errorf("control reconnect -E: pair=%#v err=%v", control, err))
	}
	facts["control_reconnect_with_E"] = "preserved"
	facts["tested_variables"] = p7SSHKey + "," + p7DisplayKey
	fmt.Fprintf(&raw, "negative-without-E=%s,%s\nattach-E=%s,%s\nswitch-E=%s,%s\ncontrol-reconnect-E=%s,%s\n", negative.ssh, negative.display, positive.ssh, positive.display, switched.ssh, switched.display, control.ssh, control.display)
	return finishNativeProbe(env, ProbeResult{ID: "P7", Status: StatusPass, Summary: "negative control proves attach environment mutation without -E; ShellBeam attachment/switch/recovery paths preserve session environment with -E", Facts: facts}, raw.String())
}

func p7RequireUpdateEnvironment(ctx context.Context, f *nativeFixture) error {
	update, err := f.tmux(ctx, "show-options", "-gv", "update-environment")
	if err != nil {
		return fmt.Errorf("read update-environment: %w", err)
	}
	text := string(update)
	if !strings.Contains(text, p7SSHKey) || !strings.Contains(text, p7DisplayKey) {
		return fmt.Errorf("tested tmux update-environment omits deterministic control variables")
	}
	return nil
}

func p7SetSessionPair(ctx context.Context, f *nativeFixture) error {
	if err := f.setSessionEnvironment(ctx, f.Session, p7SSHKey, p7SSHSession); err != nil {
		return err
	}
	return f.setSessionEnvironment(ctx, f.Session, p7DisplayKey, p7DisplaySession)
}

func p7ReadPair(ctx context.Context, f *nativeFixture) (p7Pair, error) {
	ssh, err := f.sessionEnvironment(ctx, f.Session, p7SSHKey)
	if err != nil {
		return p7Pair{}, err
	}
	display, err := f.sessionEnvironment(ctx, f.Session, p7DisplayKey)
	return p7Pair{ssh: ssh, display: display}, err
}

func p7AttachAndRead(ctx context.Context, f *nativeFixture, preserve bool, attachEnv map[string]string) (p7Pair, error) {
	client, err := f.attachHumanWithOptions(ctx, humanAttachOptions{Session: f.Session, ReadOnly: true, PreserveEnvironment: preserve, Environment: attachEnv})
	if err != nil {
		return p7Pair{}, err
	}
	defer client.close()
	if _, err := f.waitClientByPID(ctx, client.PID()); err != nil {
		return p7Pair{}, err
	}
	return p7ReadPair(ctx, f)
}

func p7AttachPreservedAndKeepClient(ctx context.Context, f *nativeFixture, attachEnv map[string]string) (p7Pair, *humanClient, error) {
	client, err := f.attachHumanWithOptions(ctx, humanAttachOptions{Session: f.Session, ReadOnly: true, PreserveEnvironment: true, Environment: attachEnv})
	if err != nil {
		return p7Pair{}, nil, err
	}
	if _, err := f.waitClientByPID(ctx, client.PID()); err != nil {
		client.close()
		return p7Pair{}, nil, err
	}
	pair, err := p7ReadPair(ctx, f)
	if err != nil {
		client.close()
		return p7Pair{}, nil, err
	}
	return pair, client, nil
}

func p7SwitchAndRead(ctx context.Context, f *nativeFixture, client *humanClient) (p7Pair, error) {
	facts, err := f.waitClientByPID(ctx, client.PID())
	if err != nil {
		return p7Pair{}, err
	}
	if err := f.setClientReadOnly(ctx, facts.Name, false); err != nil {
		return p7Pair{}, err
	}
	return p7ReadPair(ctx, f)
}

func p7ControlAttachAndRead(ctx context.Context, f *nativeFixture, attachEnv map[string]string) (p7Pair, error) {
	control, err := f.startControlWithEnvironment(ctx, f.Session, "no-output,ignore-size", attachEnv)
	if err != nil {
		return p7Pair{}, err
	}
	defer control.close()
	if err := control.waitReady(ctx, f); err != nil {
		return p7Pair{}, err
	}
	return p7ReadPair(ctx, f)
}

func p7Failure(env nativeProbeEnv, raw string, err error) ProbeResult {
	return finishNativeProbe(env, ProbeResult{ID: "P7", Status: StatusFail, Summary: err.Error()}, raw)
}
