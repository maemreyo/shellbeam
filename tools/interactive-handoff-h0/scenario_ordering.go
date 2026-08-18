package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const p12WriterBytes = 4 << 20

func probeP12ACKOrderingAndBackpressure(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{"cross_client_total_ordering_claimed": "false"}
	ordering, orderingRaw, err := measureP12PrivacyOrdering(ctx, env)
	if err != nil {
		return orderingFailure(env, err)
	}
	for key, value := range ordering {
		facts[key] = value
	}
	backpressure, backpressureRaw, err := measureP12Backpressure(ctx, env)
	if err != nil {
		return orderingFailure(env, err)
	}
	for key, value := range backpressure {
		facts[key] = value
	}
	raw := orderingRaw + "\n" + backpressureRaw
	return finishNativeProbe(env, ProbeResult{ID: "P12", Status: StatusPass, Summary: "same-control-client ACK ordering is sufficient for qualified privacy transitions; cross-client total ordering is not claimed; backpressure topology is measured explicitly", Facts: facts}, raw)
}

func measureP12PrivacyOrdering(ctx context.Context, env nativeProbeEnv) (map[string]string, string, error) {
	f, cleanup, failure := newProbeFixture(ctx, env, "P12", "stty -echo; exec cat")
	if failure != nil {
		return nil, "", fmt.Errorf("%s", failure.Summary)
	}
	defer cleanup()
	pane, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return nil, "", err
	}
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return nil, "", err
	}
	defer ctrl.close()
	offACK, onACK, err := runP12PrivacySequence(ctx, f, ctrl, pane)
	if err != nil {
		return nil, "", err
	}
	indices, err := p12OrderingIndices(ctrl.eventsSnapshot(), offACK, onACK)
	if err != nil {
		return nil, "", err
	}
	facts := map[string]string{
		"during_private_visible": "false",
		"off_ack_command_number": strconv.Itoa(offACK.CommandNumber),
		"on_ack_command_number":  strconv.Itoa(onACK.CommandNumber),
		"same_client_order":      "before_output<off_ack<on_ack<after_output",
	}
	raw := fmt.Sprintf("ordering=before[%d]<off-ack[%d,cmd=%d]<on-ack[%d,cmd=%d]<after[%d]\nduring-visible=false\n", indices.before, indices.offACK, offACK.CommandNumber, indices.onACK, onACK.CommandNumber, indices.after)
	return facts, raw, nil
}

func runP12PrivacySequence(ctx context.Context, f *nativeFixture, ctrl *controlClient, pane string) (ControlEvent, ControlEvent, error) {
	ctrl.clearEvents()
	if err := f.emitMarker(ctx, pane, "P12_BEFORE"); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if err := ctrl.waitPaneOutput(ctx, pane, "P12_BEFORE"); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	offACK, err := ctrl.command(ctx, "refresh-client -f no-output")
	if err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if err := f.emitMarker(ctx, pane, "P12_DURING"); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if err := ensureMarkerAbsentFor(ctx, ctrl, "P12_DURING", 75*time.Millisecond); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	onACK, err := ctrl.command(ctx, "refresh-client -f !no-output")
	if err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if err := f.emitMarker(ctx, pane, "P12_AFTER"); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if err := ctrl.waitPaneOutput(ctx, pane, "P12_AFTER"); err != nil {
		return ControlEvent{}, ControlEvent{}, err
	}
	if ctrl.anyPaneOutputContains("P12_DURING") {
		return ControlEvent{}, ControlEvent{}, fmt.Errorf("private DURING output replayed after privacy release")
	}
	return offACK, onACK, nil
}

type p12Indices struct{ before, offACK, onACK, after int }

func p12OrderingIndices(events []ControlEvent, offACK, onACK ControlEvent) (p12Indices, error) {
	idx := p12Indices{before: -1, offACK: -1, onACK: -1, after: -1}
	for i, event := range events {
		switch {
		case event.Kind == EventPaneOutput && strings.Contains(event.Data, "P12_BEFORE"):
			if idx.before < 0 {
				idx.before = i
			}
		case event.Kind == EventCommandEnd && event.CommandNumber == offACK.CommandNumber:
			idx.offACK = i
		case event.Kind == EventCommandEnd && event.CommandNumber == onACK.CommandNumber:
			idx.onACK = i
		case event.Kind == EventPaneOutput && strings.Contains(event.Data, "P12_AFTER"):
			if idx.after < 0 {
				idx.after = i
			}
		}
	}
	if idx.before < 0 || idx.offACK < 0 || idx.onACK < 0 || idx.after < 0 || !(idx.before < idx.offACK && idx.offACK < idx.onACK && idx.onACK < idx.after) {
		return idx, fmt.Errorf("unexpected P12 event order: %#v", idx)
	}
	return idx, nil
}

func measureP12Backpressure(ctx context.Context, env nativeProbeEnv) (map[string]string, string, error) {
	blocked, released, err := p12AllControlOff(ctx, env)
	if err != nil {
		return nil, "", err
	}
	humanPrevents, err := p12HumanDisplayPreventsBackpressure(ctx, env)
	if err != nil {
		return nil, "", err
	}
	noOutputReads, err := p12NoOutputStillReads(ctx, env)
	if err != nil {
		return nil, "", err
	}
	if !blocked || !released || !humanPrevents || !noOutputReads {
		return nil, "", fmt.Errorf("backpressure policy mismatch: blocked=%t released=%t humanPrevents=%t noOutputReads=%t", blocked, released, humanPrevents, noOutputReads)
	}
	facts := map[string]string{
		"all_control_clients_off_backpressures": "true",
		"backpressure_released_on_on":           "true",
		"human_display_prevents_backpressure":   "true",
		"no_output_stops_tmux_reading":          "false",
		"bounded_writer_bytes":                  strconv.Itoa(p12WriterBytes),
	}
	raw := fmt.Sprintf("writer-bytes=%d\nall-control-off-blocked=true\non-released=true\nhuman-display-prevents=true\nno-output-stops-reading=false\n", p12WriterBytes)
	return facts, raw, nil
}

func p12AllControlOff(ctx context.Context, env nativeProbeEnv) (bool, bool, error) {
	f, cleanup, failure := newProbeFixture(ctx, env, "P12", "stty -echo; exec cat")
	if failure != nil {
		return false, false, fmt.Errorf("%s", failure.Summary)
	}
	defer cleanup()
	pane, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return false, false, err
	}
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, false, err
	}
	defer ctrl.close()
	if err := ctrl.setPaneOutput(ctx, pane, false); err != nil {
		return false, false, err
	}
	sentinel := filepath.Join(f.Root, "p12-off.done")
	if err := f.respawnPane(ctx, pane, boundedWriterCommand(sentinel)); err != nil {
		return false, false, err
	}
	blocked, err := fileAbsentFor(ctx, sentinel, 150*time.Millisecond)
	if err != nil {
		return false, false, err
	}
	if err := ctrl.setPaneOutput(ctx, pane, true); err != nil {
		return blocked, false, err
	}
	released := waitFileExists(ctx, sentinel, 5*time.Second) == nil
	return blocked, released, nil
}

func p12HumanDisplayPreventsBackpressure(ctx context.Context, env nativeProbeEnv) (bool, error) {
	f, cleanup, failure := newProbeFixture(ctx, env, "P12", "stty -echo; exec cat")
	if failure != nil {
		return false, fmt.Errorf("%s", failure.Summary)
	}
	defer cleanup()
	pane, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return false, err
	}
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, err
	}
	defer ctrl.close()
	human, err := f.attachHuman(ctx, true)
	if err != nil {
		return false, err
	}
	defer human.close()
	if _, err := f.waitClientByPID(ctx, human.PID()); err != nil {
		return false, err
	}
	if err := ctrl.setPaneOutput(ctx, pane, false); err != nil {
		return false, err
	}
	sentinel := filepath.Join(f.Root, "p12-human.done")
	if err := f.respawnPane(ctx, pane, boundedWriterCommand(sentinel)); err != nil {
		return false, err
	}
	return waitFileExists(ctx, sentinel, 5*time.Second) == nil, nil
}

func p12NoOutputStillReads(ctx context.Context, env nativeProbeEnv) (bool, error) {
	f, cleanup, failure := newProbeFixture(ctx, env, "P12", "stty -echo; exec cat")
	if failure != nil {
		return false, fmt.Errorf("%s", failure.Summary)
	}
	defer cleanup()
	pane, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return false, err
	}
	ctrl, err := f.attachControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, err
	}
	defer ctrl.close()
	sentinel := filepath.Join(f.Root, "p12-no-output.done")
	if err := f.respawnPane(ctx, pane, boundedWriterCommand(sentinel)); err != nil {
		return false, err
	}
	return waitFileExists(ctx, sentinel, 5*time.Second) == nil, nil
}

func boundedWriterCommand(sentinel string) string {
	count := p12WriterBytes / (64 << 10)
	return fmt.Sprintf("dd if=/dev/zero bs=65536 count=%d 2>/dev/null; printf done > %s; exec sleep 30", count, shellSingleQuote(sentinel))
}

func fileAbsentFor(ctx context.Context, path string, duration time.Duration) (bool, error) {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return false, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return true, nil
		case <-ticker.C:
		}
	}
}

func orderingFailure(env nativeProbeEnv, err error) ProbeResult {
	return finishNativeProbe(env, ProbeResult{ID: "P12", Status: StatusFail, Summary: err.Error()}, "")
}
