package main

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

func probeP10ResizeIsolation(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{"resize_policy": "manual_explicit_human_adoption"}
	f, cleanup, failure := newProbeFixture(ctx, env, "P10", "stty -echo; exec cat")
	if failure != nil {
		return finishNativeProbe(env, *failure, "")
	}
	defer cleanup()
	pane, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return resizeFailure(env, err)
	}
	observer, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return resizeFailure(env, err)
	}
	defer observer.close()
	first, firstFacts, err := attachSizedHuman(ctx, f, 40, 120)
	if err != nil {
		return resizeFailure(env, err)
	}
	defer first.close()
	if err := f.resizeWindowManual(ctx, f.Session, 120, 40); err != nil {
		return resizeFailure(env, err)
	}
	if err := requirePaneSize(ctx, f, pane, 120, 40); err != nil {
		return resizeFailure(env, err)
	}
	facts["first_human_size"] = "120x40"
	facts["passive_observer_changed_size"] = "false"
	if err := f.setClientReadOnly(ctx, firstFacts.Name, true); err != nil {
		return resizeFailure(env, err)
	}
	if _, err := f.waitClientReadOnly(ctx, firstFacts.Name, true); err != nil {
		return resizeFailure(env, err)
	}
	if err := requirePaneSize(ctx, f, pane, 120, 40); err != nil {
		return resizeFailure(env, err)
	}
	facts["readonly_changed_size"] = "false"
	if err := first.close(); err != nil {
		return resizeFailure(env, err)
	}
	if err := requirePaneSize(ctx, f, pane, 120, 40); err != nil {
		return resizeFailure(env, err)
	}
	facts["detach_changed_size"] = "false"
	second, secondFacts, err := attachSizedHuman(ctx, f, 30, 90)
	if err != nil {
		return resizeFailure(env, err)
	}
	defer second.close()
	if err := f.resizeWindowManual(ctx, f.Session, 90, 30); err != nil {
		return resizeFailure(env, err)
	}
	if err := requirePaneSize(ctx, f, pane, 90, 30); err != nil {
		return resizeFailure(env, err)
	}
	facts["second_human_size"] = "90x30"
	facts["control_client_flags"] = "ignore-size"
	raw := fmt.Sprintf("policy=manual\nfirst=%s %dx%d\nreadonly=120x40\ndetach=120x40\nsecond=%s %dx%d\n", firstFacts.Name, firstFacts.Width, firstFacts.Height, secondFacts.Name, secondFacts.Width, secondFacts.Height)
	return finishNativeProbe(env, ProbeResult{ID: "P10", Status: StatusPass, Summary: "manual size ownership makes observer/read-only/detach size-stable; only explicit human adoption resizes", Facts: facts}, raw)
}

func attachSizedHuman(ctx context.Context, f *nativeFixture, rows, cols uint16) (*humanClient, clientFacts, error) {
	human, err := f.attachHuman(ctx, false)
	if err != nil {
		return nil, clientFacts{}, err
	}
	if err := human.setSize(rows, cols); err != nil {
		_ = human.close()
		return nil, clientFacts{}, err
	}
	facts, err := waitClientDimensions(ctx, f, human.PID(), int(cols), int(rows))
	if err != nil {
		_ = human.close()
		return nil, clientFacts{}, err
	}
	return human, facts, nil
}

func waitClientDimensions(ctx context.Context, f *nativeFixture, pid, width, height int) (clientFacts, error) {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		facts, err := f.waitClientByPID(ctx, pid)
		if err != nil {
			return clientFacts{}, err
		}
		if facts.Width == width && facts.Height == height {
			return facts, nil
		}
		select {
		case <-ctx.Done():
			return clientFacts{}, fmt.Errorf("client %d size=%dx%d want %dx%d: %w", pid, facts.Width, facts.Height, width, height, ctx.Err())
		case <-ticker.C:
		}
	}
}

func requirePaneSize(ctx context.Context, f *nativeFixture, pane string, width, height int) error {
	w, h, err := f.paneSize(ctx, pane)
	if err != nil {
		return err
	}
	if w != width || h != height {
		return fmt.Errorf("pane size=%dx%d want %dx%d", w, h, width, height)
	}
	return nil
}

func boolString(v bool) string { return strconv.FormatBool(v) }

func resizeFailure(env nativeProbeEnv, err error) ProbeResult {
	return finishNativeProbe(env, ProbeResult{ID: "P10", Status: StatusFail, Summary: err.Error()}, "")
}
