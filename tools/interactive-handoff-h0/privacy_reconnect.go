package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func probeP6ReconnectNoReplay(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{"capture_pane_used": "false"}
	var raw strings.Builder

	perSessionEligible, _, _ := measureP5PerSessionObserver(ctx, env)
	if perSessionEligible {
		perSessionOK, perSessionFacts, perSessionRaw := measureP6PerSessionObserver(ctx, env)
		facts["candidate."+candidatePerSessionObserver+".p6"] = passFail(perSessionOK)
		mergePrefixedFacts(facts, "candidate."+candidatePerSessionObserver+".", perSessionFacts)
		raw.WriteString("[per_session_observer]\n")
		raw.WriteString(perSessionRaw)
	} else {
		facts["candidate."+candidatePerSessionObserver+".p6"] = "NOT_ELIGIBLE_P5"
	}

	perPaneEligible, _, perPaneP5Raw := measureP5SharedPerPane(ctx, env)
	if perPaneEligible {
		perPaneOK, perPaneFacts, perPaneRaw := measureP6SharedPerPane(ctx, env)
		facts["candidate."+candidateSharedPerPane+".p6"] = passFail(perPaneOK)
		mergePrefixedFacts(facts, "candidate."+candidateSharedPerPane+".", perPaneFacts)
		raw.WriteString("\n[shared_observer_with_per_pane_off]\n")
		raw.WriteString(perPaneRaw)
	} else {
		facts["candidate."+candidateSharedPerPane+".p6"] = "NOT_ELIGIBLE_P5"
		raw.WriteString("\n[shared_observer_with_per_pane_off]\nP6 skipped: P5 not eligible\n")
		raw.WriteString(perPaneP5Raw)
	}

	demuxEligible, _, _ := measureP5DaemonDemux(ctx, env)
	if demuxEligible {
		demuxOK, demuxFacts, demuxRaw := measureP6DaemonDemux(ctx, env)
		facts["candidate."+candidateSharedDaemonDemux+".p6"] = passFail(demuxOK)
		mergePrefixedFacts(facts, "candidate."+candidateSharedDaemonDemux+".", demuxFacts)
		raw.WriteString("\n[shared_observer_with_daemon_demux_simulation]\n")
		raw.WriteString(demuxRaw)
	} else {
		facts["candidate."+candidateSharedDaemonDemux+".p6"] = "NOT_ELIGIBLE_P5"
	}

	passing := 0
	for _, candidate := range privacyCandidateNames() {
		if facts["candidate."+candidate+".p6"] == "PASS" {
			passing++
		}
	}
	status := StatusPass
	summary := "at least one measured candidate reconnects private-from-first-byte without replaying gap/private history into the public path"
	if passing == 0 {
		status = StatusFail
		summary = "no measured candidate provides private reconnect without public history replay"
		facts["architecture_fork"] = "privacy_topology_required"
	}
	facts["p6_passing_candidates"] = candidateStatusList(facts, "p6", "PASS")
	return finishNativeProbe(env, ProbeResult{ID: "P6", Status: status, Summary: summary, Facts: facts}, raw.String())
}

func measureP6PerSessionObserver(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P6", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	paneA, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	old, err := f.attachControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, facts, "old private observer: " + err.Error() + "\n"
	}
	defer old.close()

	// P15 precursor: overlap two observers that are both private from attach.
	overlap, err := f.attachControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, facts, "overlap private observer: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, paneA, "A_SECRET_OVERLAP"); err != nil {
		overlap.close()
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, old, "A_SECRET_OVERLAP", 40*time.Millisecond); err != nil {
		overlap.close()
		return false, facts, "old overlap leak: " + err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, overlap, "A_SECRET_OVERLAP", 40*time.Millisecond); err != nil {
		overlap.close()
		return false, facts, "new overlap leak: " + err.Error() + "\n"
	}
	_ = overlap.close()
	facts["overlap_private"] = "true"

	if err := old.close(); err != nil {
		return false, facts, "kill old observer: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, paneA, "A_SECRET_DURING_GAP"); err != nil {
		return false, facts, err.Error() + "\n"
	}

	replacement, err := f.attachControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, facts, "replacement private observer: " + err.Error() + "\n"
	}
	defer replacement.close()
	if err := f.emitMarker(ctx, paneA, "A_SECRET_AFTER_RECONNECT"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkersAbsentFor(ctx, replacement, []string{"A_SECRET_DURING_GAP", "A_SECRET_AFTER_RECONNECT"}, 50*time.Millisecond); err != nil {
		return false, facts, "replacement private leak: " + err.Error() + "\n"
	}
	if _, err := replacement.command(ctx, "refresh-client -f !no-output"); err != nil {
		return false, facts, "release forward boundary: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, paneA, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := replacement.waitPaneOutput(ctx, paneA, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return false, facts, "public after reconnect boundary: " + err.Error() + "\n"
	}
	if replacement.anyPaneOutputContains("A_SECRET_") {
		return false, facts, "private history replayed after public release\n"
	}
	facts["private_reconnect_attach_flag"] = "no-output"
	facts["history_replayed"] = "false"
	fmt.Fprintf(&raw, "pane=%s\noverlap-private=true\ngap-secret-public=false\nreconnect-secret-public=false\npublic-after-boundary=true\n", paneA)
	return true, facts, raw.String()
}

func measureP6SharedPerPane(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P6", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	old, err := attachSharedPerPanePrivate(ctx, f, panes.A)
	if err != nil {
		return false, facts, "old staged observer: " + err.Error() + "\n"
	}
	defer old.close()

	overlap, err := attachSharedPerPanePrivate(ctx, f, panes.A)
	if err != nil {
		return false, facts, "overlap staged observer: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.A, "A_SECRET_OVERLAP"); err != nil {
		overlap.close()
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, old, "A_SECRET_OVERLAP", 40*time.Millisecond); err != nil {
		overlap.close()
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, overlap, "A_SECRET_OVERLAP", 40*time.Millisecond); err != nil {
		overlap.close()
		return false, facts, err.Error() + "\n"
	}
	_ = overlap.close()
	facts["overlap_private"] = "true"

	if err := old.close(); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.A, "A_SECRET_DURING_GAP"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	replacement, err := attachSharedPerPanePrivate(ctx, f, panes.A)
	if err != nil {
		return false, facts, "replacement staged observer: " + err.Error() + "\n"
	}
	defer replacement.close()
	if err := f.emitMarker(ctx, panes.A, "A_SECRET_AFTER_RECONNECT"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := emitAndWaitReconnectBC(ctx, f, replacement, panes); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ensureMarkersAbsentFor(ctx, replacement, []string{"A_SECRET_DURING_GAP", "A_SECRET_AFTER_RECONNECT"}, 40*time.Millisecond); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := replacement.setPaneOutput(ctx, panes.A, true); err != nil {
		return false, facts, "A forward boundary: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.A, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := replacement.waitPaneOutput(ctx, panes.A, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if replacement.anyPaneOutputContains("A_SECRET_") {
		return false, facts, "private A history replayed after pane forward boundary\n"
	}
	facts["private_reconnect_sequence"] = "attach_no-output_then_A_off_then_clear_no-output"
	facts["history_replayed"] = "false"
	fmt.Fprintf(&raw, "A=%s B=%s C=%s\noverlap-private=true\nB-C-public-after-reconnect=true\nA-private-history-replayed=false\nA-public-after-boundary=true\n", panes.A, panes.B, panes.C)
	return true, facts, raw.String()
}

func emitAndWaitReconnectBC(ctx context.Context, f *nativeFixture, ctrl *controlClient, panes privacyPaneSet) error {
	if err := f.emitMarker(ctx, panes.B, "B_PUBLIC_RECONNECT"); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, "C_PUBLIC_RECONNECT"); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_PUBLIC_RECONNECT"); err != nil {
		return err
	}
	return ctrl.waitPaneOutput(ctx, panes.C, "C_PUBLIC_RECONNECT")
}

func attachSharedPerPanePrivate(ctx context.Context, f *nativeFixture, paneA string) (*controlClient, error) {
	ctrl, err := f.attachControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return nil, err
	}
	if err := ctrl.setPaneOutput(ctx, paneA, false); err != nil {
		_ = ctrl.close()
		return nil, err
	}
	if _, err := ctrl.command(ctx, "refresh-client -f !no-output"); err != nil {
		_ = ctrl.close()
		return nil, err
	}
	return ctrl, nil
}

type daemonDemuxProjection struct {
	private map[string]bool
	cursor  int
	public  strings.Builder
}

func newDaemonDemux(privatePanes map[string]bool) *daemonDemuxProjection {
	copyMap := make(map[string]bool, len(privatePanes))
	for pane, private := range privatePanes {
		copyMap[pane] = private
	}
	return &daemonDemuxProjection{private: copyMap}
}

func (d *daemonDemuxProjection) ingest(ctrl *controlClient) {
	events := ctrl.eventsSnapshot()
	if d.cursor > len(events) {
		d.cursor = len(events)
	}
	for _, event := range events[d.cursor:] {
		if event.Kind != EventPaneOutput || d.private[event.PaneID] {
			continue
		}
		d.public.WriteString(event.Data)
	}
	d.cursor = len(events)
}

func (d *daemonDemuxProjection) setPrivate(pane string, private bool) {
	d.private[pane] = private
}

func (d *daemonDemuxProjection) publicString() string { return d.public.String() }

func measureP6DaemonDemux(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{"private_bytes_enter_control_parser": "true"}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P6", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := proveDaemonDemuxOverlap(ctx, f, panes.A); err != nil {
		return false, facts, err.Error() + "\n"
	}
	facts["overlap_private"] = "true"
	if err := f.emitMarker(ctx, panes.A, "A_SECRET_DURING_GAP"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	public, err := proveDaemonDemuxReconnect(ctx, f, panes)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	if strings.Contains(public, "A_SECRET_") || !strings.Contains(public, "A_PUBLIC_AFTER_BOUNDARY") {
		return false, facts, "forward-only daemon demux projection violated\n"
	}
	facts["history_replayed_to_public_projection"] = "false"
	facts["forward_only_cursor"] = "true"
	fmt.Fprintf(&raw, "A=%s\noverlap-private=true\ngap-history-public=false\nreconnect-private-public=false\nforward-only-cursor=true\nA-public-after-boundary=true\n", panes.A)
	return true, facts, raw.String()
}

func proveDaemonDemuxOverlap(ctx context.Context, f *nativeFixture, paneA string) error {
	oldDemux := newDaemonDemux(map[string]bool{paneA: true})
	old, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return err
	}
	defer old.close()
	overlapDemux := newDaemonDemux(map[string]bool{paneA: true})
	overlap, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return err
	}
	defer overlap.close()
	if err := f.emitMarker(ctx, paneA, "A_SECRET_OVERLAP"); err != nil {
		return err
	}
	if err := old.waitPaneOutput(ctx, paneA, "A_SECRET_OVERLAP"); err != nil {
		return err
	}
	if err := overlap.waitPaneOutput(ctx, paneA, "A_SECRET_OVERLAP"); err != nil {
		return err
	}
	oldDemux.ingest(old)
	overlapDemux.ingest(overlap)
	if strings.Contains(oldDemux.publicString(), "A_SECRET_") || strings.Contains(overlapDemux.publicString(), "A_SECRET_") {
		return fmt.Errorf("daemon demux overlap leaked private A to public projection")
	}
	return nil
}

func proveDaemonDemuxReconnect(ctx context.Context, f *nativeFixture, panes privacyPaneSet) (string, error) {
	// Gate exists before replacement observer process creation.
	demux := newDaemonDemux(map[string]bool{panes.A: true})
	replacement, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return "", err
	}
	defer replacement.close()
	demux.ingest(replacement)
	if err := emitAndIngestDaemonReconnect(ctx, f, replacement, demux, panes); err != nil {
		return "", err
	}
	// Forward-only release: ingest all private events before flipping A public.
	demux.ingest(replacement)
	demux.setPrivate(panes.A, false)
	if err := f.emitMarker(ctx, panes.A, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return "", err
	}
	if err := replacement.waitPaneOutput(ctx, panes.A, "A_PUBLIC_AFTER_BOUNDARY"); err != nil {
		return "", err
	}
	demux.ingest(replacement)
	return demux.publicString(), nil
}

func emitAndIngestDaemonReconnect(ctx context.Context, f *nativeFixture, ctrl *controlClient, demux *daemonDemuxProjection, panes privacyPaneSet) error {
	if err := f.emitMarker(ctx, panes.A, "A_SECRET_AFTER_RECONNECT"); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.A, "A_SECRET_AFTER_RECONNECT"); err != nil {
		return err
	}
	demux.ingest(ctrl)
	if strings.Contains(demux.publicString(), "A_SECRET_") {
		return fmt.Errorf("replacement demux leaked private A")
	}
	if err := f.emitMarker(ctx, panes.B, "B_PUBLIC_RECONNECT"); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_PUBLIC_RECONNECT"); err != nil {
		return err
	}
	demux.ingest(ctrl)
	if !strings.Contains(demux.publicString(), "B_PUBLIC_RECONNECT") {
		return fmt.Errorf("public B missing from daemon projection")
	}
	return nil
}
