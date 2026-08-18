package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func probeP4PrivacyScope(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{
		"observation_topology": "unqualified",
	}
	var raw strings.Builder

	perSessionOK, perSessionFacts, perSessionRaw := measureP4PerSessionObserver(ctx, env)
	facts["candidate."+candidatePerSessionObserver+".p4"] = passFail(perSessionOK)
	mergePrefixedFacts(facts, "candidate."+candidatePerSessionObserver+".", perSessionFacts)
	raw.WriteString("[per_session_observer]\n")
	raw.WriteString(perSessionRaw)

	sharedOK, sharedFacts, sharedRaw := measureP4SharedPerPaneObserver(ctx, env)
	facts["candidate."+candidateSharedPerPane+".p4"] = passFail(sharedOK)
	mergePrefixedFacts(facts, "shared_per_pane.", sharedFacts)
	facts["no_output_scope"] = sharedFacts["no_output_scope"]
	raw.WriteString("\n[shared_observer_with_per_pane_off]\n")
	raw.WriteString(sharedRaw)

	demuxOK, demuxFacts, demuxRaw := measureP4DaemonDemux(ctx, env)
	facts["candidate."+candidateSharedDaemonDemux+".p4"] = passFail(demuxOK)
	mergePrefixedFacts(facts, "daemon_demux.", demuxFacts)
	raw.WriteString("\n[shared_observer_with_daemon_demux_simulation]\n")
	raw.WriteString(demuxRaw)

	eligible := []string{}
	for _, candidate := range privacyCandidateNames() {
		if facts["candidate."+candidate+".p4"] == "PASS" {
			eligible = append(eligible, candidate)
		}
	}
	facts["p4_eligible_candidates"] = strings.Join(eligible, ",")
	status := StatusPass
	summary := "privacy scope measured; at least one candidate suppresses private A while preserving public B/C; topology remains unqualified pending P5/P6 and cross-platform evidence"
	if len(eligible) == 0 {
		status = StatusFail
		summary = "no measured privacy topology suppresses private A while preserving public B/C"
		facts["architecture_fork"] = "privacy_topology_required"
	}
	return finishNativeProbe(env, ProbeResult{ID: "P4", Status: status, Summary: summary, Facts: facts}, raw.String())
}

func measureP4PerSessionObserver(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	f, controls, panes, err := setupP4PerSession(ctx, env)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer f.close(context.Background())
	defer controls.close()
	if err := proveP4PerSessionDefault(ctx, f, controls, panes, facts); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := proveP4PerSessionPrivateScope(ctx, f, controls, panes); err != nil {
		return false, facts, err.Error() + "\n"
	}
	raw := fmt.Sprintf("panes=A:%s B:%s C:%s\nA-default=%s\nB-default=%s\nC-default=%s\nA-private-absent=true\nB-C-public=true\n",
		panes.A, panes.B, panes.C,
		facts["observer_a_default_panes"], facts["observer_b_default_panes"], facts["observer_c_default_panes"])
	return true, facts, raw
}

type p4PerSessionControls struct {
	a *controlClient
	b *controlClient
	c *controlClient
}

func (c p4PerSessionControls) close() {
	_ = c.a.close()
	_ = c.b.close()
	_ = c.c.close()
}

func setupP4PerSession(ctx context.Context, env nativeProbeEnv) (*nativeFixture, p4PerSessionControls, privacyPaneSet, error) {
	f, _, failure := newProbeFixture(ctx, env, "P4", "stty -echo; exec cat")
	if failure != nil {
		return nil, p4PerSessionControls{}, privacyPaneSet{}, fmt.Errorf("%s", failure.Summary)
	}
	if err := f.createSession(ctx, "h0-b", "stty -echo; exec cat"); err != nil {
		_ = f.close(context.Background())
		return nil, p4PerSessionControls{}, privacyPaneSet{}, fmt.Errorf("create h0-b: %w", err)
	}
	if err := f.createSession(ctx, "h0-c", "stty -echo; exec cat"); err != nil {
		_ = f.close(context.Background())
		return nil, p4PerSessionControls{}, privacyPaneSet{}, fmt.Errorf("create h0-c: %w", err)
	}
	panes, err := p4PerSessionPanes(ctx, f)
	if err != nil {
		_ = f.close(context.Background())
		return nil, p4PerSessionControls{}, privacyPaneSet{}, err
	}
	controls, err := p4AttachPerSessionControls(ctx, f)
	if err != nil {
		_ = f.close(context.Background())
		return nil, p4PerSessionControls{}, privacyPaneSet{}, err
	}
	return f, controls, panes, nil
}

func p4PerSessionPanes(ctx context.Context, f *nativeFixture) (privacyPaneSet, error) {
	a, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return privacyPaneSet{}, err
	}
	b, err := f.paneForSession(ctx, "h0-b")
	if err != nil {
		return privacyPaneSet{}, err
	}
	c, err := f.paneForSession(ctx, "h0-c")
	if err != nil {
		return privacyPaneSet{}, err
	}
	if a == b || a == c || b == c {
		return privacyPaneSet{}, fmt.Errorf("pane ids not unique: %s %s %s", a, b, c)
	}
	return privacyPaneSet{A: a, B: b, C: c}, nil
}

func p4AttachPerSessionControls(ctx context.Context, f *nativeFixture) (p4PerSessionControls, error) {
	a, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return p4PerSessionControls{}, fmt.Errorf("attach A observer: %w", err)
	}
	b, err := f.attachControl(ctx, "h0-b", "ignore-size")
	if err != nil {
		_ = a.close()
		return p4PerSessionControls{}, fmt.Errorf("attach B observer: %w", err)
	}
	c, err := f.attachControl(ctx, "h0-c", "ignore-size")
	if err != nil {
		_ = a.close()
		_ = b.close()
		return p4PerSessionControls{}, fmt.Errorf("attach C observer: %w", err)
	}
	return p4PerSessionControls{a: a, b: b, c: c}, nil
}

func proveP4PerSessionDefault(ctx context.Context, f *nativeFixture, controls p4PerSessionControls, panes privacyPaneSet, facts map[string]string) error {
	controls.a.clearEvents()
	controls.b.clearEvents()
	controls.c.clearEvents()
	for _, item := range []struct{ pane, marker string }{{panes.A, "A_PUBLIC_1"}, {panes.B, "B_PUBLIC_1"}, {panes.C, "C_PUBLIC_1"}} {
		if err := f.emitMarker(ctx, item.pane, item.marker); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		ctrl                *controlClient
		pane, marker, label string
	}{{controls.a, panes.A, "A_PUBLIC_1", "A"}, {controls.b, panes.B, "B_PUBLIC_1", "B"}, {controls.c, panes.C, "C_PUBLIC_1", "C"}} {
		if err := item.ctrl.waitPaneOutput(ctx, item.pane, item.marker); err != nil {
			return fmt.Errorf("%s default output: %w", item.label, err)
		}
	}
	facts["observer_a_default_panes"] = strings.Join(controls.a.paneIDsObserved(), ",")
	facts["observer_b_default_panes"] = strings.Join(controls.b.paneIDsObserved(), ",")
	facts["observer_c_default_panes"] = strings.Join(controls.c.paneIDsObserved(), ",")
	return nil
}

func proveP4PerSessionPrivateScope(ctx context.Context, f *nativeFixture, controls p4PerSessionControls, panes privacyPaneSet) error {
	if _, err := controls.a.command(ctx, "refresh-client -f no-output"); err != nil {
		return fmt.Errorf("A no-output: %w", err)
	}
	controls.a.clearEvents()
	controls.b.clearEvents()
	controls.c.clearEvents()
	if err := f.emitMarker(ctx, panes.A, "A_PRIVATE_SCOPE"); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.B, "B_PUBLIC_SCOPE"); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, "C_PUBLIC_SCOPE"); err != nil {
		return err
	}
	if err := controls.b.waitPaneOutput(ctx, panes.B, "B_PUBLIC_SCOPE"); err != nil {
		return fmt.Errorf("B public while A private: %w", err)
	}
	if err := controls.c.waitPaneOutput(ctx, panes.C, "C_PUBLIC_SCOPE"); err != nil {
		return fmt.Errorf("C public while A private: %w", err)
	}
	return ensureMarkerAbsentFor(ctx, controls.a, "A_PRIVATE_SCOPE", 75*time.Millisecond)
}

func measureP4SharedPerPaneObserver(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{
		"no_output_scope": "unmeasured",
	}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P4", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, facts, "attach shared observer: " + err.Error() + "\n"
	}
	defer ctrl.close()
	ctrl.clearEvents()
	if err := emitABC(ctx, f, panes, "PUBLIC_1"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := waitABC(ctx, ctrl, panes, "PUBLIC_1"); err != nil {
		return false, facts, "default shared output: " + err.Error() + "\n"
	}
	facts["default_panes"] = strings.Join(ctrl.paneIDsObserved(), ",")

	if _, err := ctrl.command(ctx, "refresh-client -f no-output"); err != nil {
		return false, facts, "set no-output: " + err.Error() + "\n"
	}
	ctrl.clearEvents()
	if err := emitABC(ctx, f, panes, "NO_OUTPUT_SCOPE"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkersAbsentFor(ctx, ctrl, []string{"A_NO_OUTPUT_SCOPE", "B_NO_OUTPUT_SCOPE", "C_NO_OUTPUT_SCOPE"}, 75*time.Millisecond); err != nil {
		return false, facts, "no-output scope: " + err.Error() + "\n"
	}
	facts["no_output_scope"] = "control_client"
	if _, err := ctrl.command(ctx, "refresh-client -f !no-output"); err != nil {
		return false, facts, "clear no-output: " + err.Error() + "\n"
	}

	if err := ctrl.setPaneOutput(ctx, panes.A, false); err != nil {
		return false, facts, "per-pane A off: " + err.Error() + "\n"
	}
	ctrl.clearEvents()
	if err := emitABC(ctx, f, panes, "PANE_OFF"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_PANE_OFF"); err != nil {
		return false, facts, "B with A off: " + err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.C, "C_PANE_OFF"); err != nil {
		return false, facts, "C with A off: " + err.Error() + "\n"
	}
	perPaneOK := ensureMarkerAbsentFor(ctx, ctrl, "A_PANE_OFF", 75*time.Millisecond) == nil
	facts["per_pane_off_private_a"] = fmt.Sprintf("%t", perPaneOK)
	facts["per_pane_off_public_bc"] = "true"

	backpressureBlocked, backpressureReleased, backpressureRaw := measurePerPaneOffBackpressure(ctx, f, ctrl)
	facts["per_pane_off_stops_reading"] = fmt.Sprintf("%t", backpressureBlocked)
	facts["per_pane_off_backpressure_released_on_on"] = fmt.Sprintf("%t", backpressureReleased)
	raw.WriteString(backpressureRaw)

	fmt.Fprintf(&raw, "shared-panes=A:%s B:%s C:%s\ndefault=%v\nno-output-suppressed-all=true\nper-pane-A-off=%t\n", panes.A, panes.B, panes.C, ctrl.paneIDsObserved(), perPaneOK)
	return perPaneOK, facts, raw.String()
}

func measureP4DaemonDemux(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{"private_bytes_enter_control_parser": "true"}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P4", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, facts, "attach daemon-demux source observer: " + err.Error() + "\n"
	}
	defer ctrl.close()
	ctrl.clearEvents()
	if err := emitABC(ctx, f, panes, "DEMUX"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := waitABC(ctx, ctrl, panes, "DEMUX"); err != nil {
		return false, facts, "daemon demux source: " + err.Error() + "\n"
	}
	public := ctrl.publicPaneOutput(map[string]bool{panes.A: true})
	ok := !strings.Contains(public, "A_DEMUX") && strings.Contains(public, "B_DEMUX") && strings.Contains(public, "C_DEMUX") && ctrl.paneOutputContains(panes.A, "A_DEMUX")
	facts["private_bytes_enter_control_parser"] = fmt.Sprintf("%t", ctrl.paneOutputContains(panes.A, "A_DEMUX"))
	facts["public_projection_suppresses_a"] = fmt.Sprintf("%t", !strings.Contains(public, "A_DEMUX"))
	facts["public_projection_keeps_bc"] = fmt.Sprintf("%t", strings.Contains(public, "B_DEMUX") && strings.Contains(public, "C_DEMUX"))
	fmt.Fprintf(&raw, "A=%s B=%s C=%s\nraw-private-A=true\npublic-private-A=false\npublic-B-C=true\n", panes.A, panes.B, panes.C)
	return ok, facts, raw.String()
}

func (f *nativeFixture) makeThreePaneSet(ctx context.Context) (privacyPaneSet, error) {
	ids, err := f.paneIDs(ctx, f.Session)
	if err != nil {
		return privacyPaneSet{}, err
	}
	if len(ids) != 1 {
		return privacyPaneSet{}, fmt.Errorf("initial session has %d panes, want 1", len(ids))
	}
	paneB, err := f.splitPane(ctx, f.Session, "stty -echo; exec cat")
	if err != nil {
		return privacyPaneSet{}, err
	}
	paneC, err := f.splitPane(ctx, f.Session, "stty -echo; exec cat")
	if err != nil {
		return privacyPaneSet{}, err
	}
	return privacyPaneSet{A: ids[0], B: paneB, C: paneC}, nil
}

func emitABC(ctx context.Context, f *nativeFixture, panes privacyPaneSet, suffix string) error {
	for _, item := range []struct {
		pane   string
		marker string
	}{{panes.A, "A_" + suffix}, {panes.B, "B_" + suffix}, {panes.C, "C_" + suffix}} {
		if err := f.emitMarker(ctx, item.pane, item.marker); err != nil {
			return err
		}
	}
	return nil
}

func waitABC(ctx context.Context, ctrl *controlClient, panes privacyPaneSet, suffix string) error {
	for _, item := range []struct {
		pane   string
		marker string
	}{{panes.A, "A_" + suffix}, {panes.B, "B_" + suffix}, {panes.C, "C_" + suffix}} {
		if err := ctrl.waitPaneOutput(ctx, item.pane, item.marker); err != nil {
			return fmt.Errorf("wait %s: %w", item.marker, err)
		}
	}
	return nil
}

func ensureMarkerAbsentFor(ctx context.Context, ctrl *controlClient, marker string, duration time.Duration) error {
	return ensureMarkersAbsentFor(ctx, ctrl, []string{marker}, duration)
}

func ensureMarkersAbsentFor(ctx context.Context, ctrl *controlClient, markers []string, duration time.Duration) error {
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, marker := range markers {
			if ctrl.anyPaneOutputContains(marker) {
				return fmt.Errorf("marker %q appeared in control output", marker)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-ctrl.done:
			return errorsNewControlExit(ctrl)
		case <-ticker.C:
		}
	}
}

func errorsNewControlExit(ctrl *controlClient) error {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()
	if ctrl.readErr != nil {
		return ctrl.readErr
	}
	return fmt.Errorf("control client exited")
}

func measurePerPaneOffBackpressure(ctx context.Context, f *nativeFixture, ctrl *controlClient) (bool, bool, string) {
	paneD, err := f.splitPane(ctx, f.Session, "stty -echo; exec cat")
	if err != nil {
		return false, false, "backpressure split: " + err.Error() + "\n"
	}
	if err := ctrl.setPaneOutput(ctx, paneD, false); err != nil {
		return false, false, "backpressure off: " + err.Error() + "\n"
	}
	sentinel := filepath.Join(f.Root, "p4-backpressure.done")
	command := fmt.Sprintf("dd if=/dev/zero bs=65536 count=32 2>/dev/null; printf done > %s; exec sleep 30", sentinel)
	if _, err := f.tmux(ctx, "respawn-pane", "-k", "-t", paneD, command); err != nil {
		return false, false, "backpressure respawn: " + err.Error() + "\n"
	}
	time.Sleep(125 * time.Millisecond)
	_, statErr := os.Stat(sentinel)
	blocked := os.IsNotExist(statErr)
	if statErr != nil && !os.IsNotExist(statErr) {
		return false, false, "backpressure stat: " + statErr.Error() + "\n"
	}
	if err := ctrl.setPaneOutput(ctx, paneD, true); err != nil {
		return blocked, false, "backpressure on: " + err.Error() + "\n"
	}
	released := waitFileExists(ctx, sentinel, 5*time.Second) == nil
	return blocked, released, fmt.Sprintf("per-pane-off-backpressure-blocked=%t released-after-on=%t\n", blocked, released)
}

func waitFileExists(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for %s", path)
		case <-ticker.C:
		}
	}
}

func mergePrefixedFacts(dst map[string]string, prefix string, src map[string]string) {
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		dst[prefix+key] = src[key]
	}
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
