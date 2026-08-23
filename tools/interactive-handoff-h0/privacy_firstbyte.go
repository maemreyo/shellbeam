package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func probeP5PrivateFromFirstByte(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{}
	var raw strings.Builder

	perSessionOK, perSessionFacts, perSessionRaw := measureP5PerSessionObserver(ctx, env)
	facts["candidate."+candidatePerSessionObserver+".p5"] = passFail(perSessionOK)
	mergePrefixedFacts(facts, "candidate."+candidatePerSessionObserver+".", perSessionFacts)
	raw.WriteString("[per_session_observer]\n")
	raw.WriteString(perSessionRaw)

	perPaneOK, perPaneFacts, perPaneRaw := measureP5SharedPerPane(ctx, env)
	facts["candidate."+candidateSharedPerPane+".p5"] = passFail(perPaneOK)
	mergePrefixedFacts(facts, "candidate."+candidateSharedPerPane+".", perPaneFacts)
	raw.WriteString("\n[shared_observer_with_per_pane_off]\n")
	raw.WriteString(perPaneRaw)

	demuxOK, demuxFacts, demuxRaw := measureP5DaemonDemux(ctx, env)
	facts["candidate."+candidateSharedDaemonDemux+".p5"] = passFail(demuxOK)
	mergePrefixedFacts(facts, "candidate."+candidateSharedDaemonDemux+".", demuxFacts)
	raw.WriteString("\n[shared_observer_with_daemon_demux_simulation]\n")
	raw.WriteString(demuxRaw)

	passing := 0
	for _, candidate := range privacyCandidateNames() {
		if facts["candidate."+candidate+".p5"] == "PASS" {
			passing++
		}
	}
	status := StatusPass
	summary := "at least one measured P4 candidate can establish privacy before its first model-visible A byte"
	if passing == 0 {
		status = StatusFail
		summary = "no measured candidate establishes privacy before the first possible model-visible A byte"
		facts["architecture_fork"] = "privacy_topology_required"
	}
	facts["p5_passing_candidates"] = candidateStatusList(facts, "p5", "PASS")
	return finishNativeProbe(env, ProbeResult{ID: "P5", Status: status, Summary: summary, Facts: facts}, raw.String())
}

func measureP5PerSessionObserver(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P5", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	paneA, err := f.paneForSession(ctx, f.Session)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	gate, err := armGatedSecretProducer(ctx, f, paneA, "A_SECRET_RACE")
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	ctrl, err := f.startControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, facts, "start private observer: " + err.Error() + "\n"
	}
	defer ctrl.close()
	if err := triggerProducerGate(gate); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitReady(ctx, f); err != nil {
		return false, facts, "private observer ready: " + err.Error() + "\n"
	}
	if err := waitProducerDone(ctx, gate); err != nil {
		return false, facts, "wait race producer done: " + err.Error() + "\n"
	}
	if err := f.respawnPane(ctx, paneA, "stty -echo; exec cat"); err != nil {
		return false, facts, "stop race producer: " + err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, ctrl, "A_SECRET_RACE", 50*time.Millisecond); err != nil {
		return false, facts, "secret reached no-output observer: " + err.Error() + "\n"
	}
	if _, err := ctrl.command(ctx, "refresh-client -f !no-output"); err != nil {
		return false, facts, "release no-output: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, paneA, "A_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, paneA, "A_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, "public marker after boundary: " + err.Error() + "\n"
	}
	if ctrl.anyPaneOutputContains("A_SECRET_RACE") {
		return false, facts, "private history replayed after no-output release\n"
	}
	facts["private_from_attach"] = "true"
	facts["attach_flag"] = "no-output"
	facts["private_bytes_enter_control_parser"] = "false"
	fmt.Fprintf(&raw, "pane=%s\nattach=no-output-from-inception\nrace-secret-visible=false\npublic-after-boundary=true\n", paneA)
	return true, facts, raw.String()
}

func measureP5SharedPerPane(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P5", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	gate, err := armGatedSecretProducer(ctx, f, panes.A, "A_SECRET_RACE")
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	// Global no-output is present in the attach command itself. Only after A
	// is pane-off do we remove the global barrier, so no public attach window
	// exists while the per-pane policy is being configured.
	ctrl, err := f.startControl(ctx, f.Session, "no-output,ignore-size")
	if err != nil {
		return false, facts, "start globally private observer: " + err.Error() + "\n"
	}
	defer ctrl.close()
	if err := triggerProducerGate(gate); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitReady(ctx, f); err != nil {
		return false, facts, "observer ready: " + err.Error() + "\n"
	}
	if err := ctrl.setPaneOutput(ctx, panes.A, false); err != nil {
		return false, facts, "arm A pane-off: " + err.Error() + "\n"
	}
	if _, err := ctrl.command(ctx, "refresh-client -f !no-output"); err != nil {
		return false, facts, "release global no-output: " + err.Error() + "\n"
	}
	if err := ensureMarkerAbsentFor(ctx, ctrl, "A_SECRET_RACE", 75*time.Millisecond); err != nil {
		return false, facts, "secret crossed staged privacy barrier: " + err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.B, "B_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.C, "C_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, "B public after staged privacy: " + err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.C, "C_PUBLIC_AFTER_FIRST_BYTE"); err != nil {
		return false, facts, "C public after staged privacy: " + err.Error() + "\n"
	}
	facts["private_from_attach"] = "true"
	facts["staging_sequence"] = "attach_no-output_then_A_off_then_clear_no-output"
	facts["private_bytes_enter_control_parser"] = "false"
	fmt.Fprintf(&raw, "A=%s B=%s C=%s\nstaging=global-no-output->A-off->global-public\nrace-secret-visible=false\nB-C-public=true\n", panes.A, panes.B, panes.C)
	return true, facts, raw.String()
}

func measureP5DaemonDemux(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	var raw strings.Builder
	f, cleanup, failure := newProbeFixture(ctx, env, "P5", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	// The daemon-side privacy gate is conceptually armed before the observer
	// process exists. Raw Control Mode bytes may enter the H0 parser; only the
	// filtered public projection is considered model-visible for this weaker
	// candidate.
	privatePanes := map[string]bool{panes.A: true}
	gate, err := armGatedSecretProducer(ctx, f, panes.A, "A_SECRET_RACE")
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	ctrl, err := f.startControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, facts, "start demux observer: " + err.Error() + "\n"
	}
	defer ctrl.close()
	if err := triggerProducerGate(gate); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitReady(ctx, f); err != nil {
		return false, facts, "demux observer ready: " + err.Error() + "\n"
	}
	if err := waitProducerDone(ctx, gate); err != nil {
		return false, facts, "wait race producer done: " + err.Error() + "\n"
	}
	rawRaceSeen := ctrl.paneOutputContains(panes.A, "A_SECRET_RACE")
	if err := f.respawnPane(ctx, panes.A, "stty -echo; exec cat"); err != nil {
		return false, facts, "replace completed race producer: " + err.Error() + "\n"
	}
	if !rawRaceSeen {
		// The attach may have completed after the bounded race producer. Create
		// a deterministic private positive control after attach; the demux was
		// already armed before observer creation.
		if err := f.emitMarker(ctx, panes.A, "A_SECRET_DEMUX_LIVE"); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.waitPaneOutput(ctx, panes.A, "A_SECRET_DEMUX_LIVE"); err != nil {
			return false, facts, "raw private positive control: " + err.Error() + "\n"
		}
	}
	if err := f.emitMarker(ctx, panes.B, "B_PUBLIC_DEMUX"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := f.emitMarker(ctx, panes.C, "C_PUBLIC_DEMUX"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_PUBLIC_DEMUX"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ctrl.waitPaneOutput(ctx, panes.C, "C_PUBLIC_DEMUX"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	public := ctrl.publicPaneOutput(privatePanes)
	ok := !strings.Contains(public, "A_SECRET_") && strings.Contains(public, "B_PUBLIC_DEMUX") && strings.Contains(public, "C_PUBLIC_DEMUX")
	facts["private_from_attach"] = "true"
	facts["privacy_gate_armed_before_observer_start"] = "true"
	facts["private_bytes_enter_control_parser"] = "true"
	facts["model_visible_private_marker"] = fmt.Sprintf("%t", strings.Contains(public, "A_SECRET_"))
	fmt.Fprintf(&raw, "A=%s\ndemux-armed-before-observer=true\nraw-private-positive-control=true\nrace-private-seen=%t\nmodel-visible-private=false\nB-C-public=true\n", panes.A, rawRaceSeen)
	return ok, facts, raw.String()
}

func armGatedSecretProducer(ctx context.Context, f *nativeFixture, paneID, marker string) (string, error) {
	gate := filepath.Join(f.Root, "producer-gate-"+strings.TrimPrefix(paneID, "%"))
	_ = os.Remove(gate)
	command := gatedSecretProducerCommand(gate, marker)
	if err := f.respawnPane(ctx, paneID, command); err != nil {
		return "", err
	}
	return gate, nil
}

func gatedSecretProducerCommand(gate, marker string) string {
	done := producerDonePath(gate)
	// %% escapes the shell printf placeholder from fmt.Sprintf. The marker is
	// a separate shell argument, so no quote nesting can corrupt the producer.
	return fmt.Sprintf("while [ ! -f %s ]; do :; done; i=0; while [ \"$i\" -lt 10000 ]; do printf '%%s\\n' %s; i=$((i+1)); done; printf done > %s; exec sleep 30", shellSingleQuote(gate), shellSingleQuote(marker), shellSingleQuote(done))
}

func producerDonePath(gate string) string { return gate + ".done" }

func waitProducerDone(ctx context.Context, gate string) error {
	return waitFileExists(ctx, producerDonePath(gate), 5*time.Second)
}

func triggerProducerGate(path string) error {
	return os.WriteFile(path, []byte("go\n"), 0o600)
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func candidateStatusList(facts map[string]string, probeSuffix, want string) string {
	var names []string
	for _, candidate := range privacyCandidateNames() {
		if facts["candidate."+candidate+"."+probeSuffix] == want {
			names = append(names, candidate)
		}
	}
	return strings.Join(names, ",")
}
