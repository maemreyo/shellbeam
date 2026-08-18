package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const p14Cycles = 128

func probeP14MultiSessionPrivacyIsolation(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{"cycles": fmt.Sprint(p14Cycles)}
	var raw strings.Builder

	perSessionOK, perSessionFacts, perSessionRaw := stressP14PerSession(ctx, env)
	facts["candidate."+candidatePerSessionObserver+".p14"] = passFail(perSessionOK)
	mergePrefixedFacts(facts, "candidate."+candidatePerSessionObserver+".", perSessionFacts)
	raw.WriteString("[per_session_observer]\n" + perSessionRaw)

	sharedStatus, sharedFacts, sharedRaw := stressP14SharedPerPaneIfEligible(ctx, env)
	facts["candidate."+candidateSharedPerPane+".p14"] = sharedStatus
	mergePrefixedFacts(facts, "candidate."+candidateSharedPerPane+".", sharedFacts)
	raw.WriteString("\n[shared_observer_with_per_pane_off]\n" + sharedRaw)

	demuxOK, demuxFacts, demuxRaw := stressP14DaemonDemux(ctx, env)
	facts["candidate."+candidateSharedDaemonDemux+".p14"] = passFail(demuxOK)
	mergePrefixedFacts(facts, "candidate."+candidateSharedDaemonDemux+".", demuxFacts)
	raw.WriteString("\n[shared_observer_with_daemon_demux_simulation]\n" + demuxRaw)

	passing := perSessionOK || demuxOK || sharedStatus == "PASS"
	if !passing {
		facts["architecture_fork"] = "privacy_topology_required"
		return finishNativeProbe(env, ProbeResult{ID: "P14", Status: StatusFail, Summary: "no P4-P6-compatible topology preserves private A while keeping complete public B/C under 128-cycle stress", Facts: facts}, raw.String())
	}
	return finishNativeProbe(env, ProbeResult{ID: "P14", Status: StatusPass, Summary: "at least one P4-P6-compatible topology keeps private A absent while B/C remain complete through 128 privacy cycles", Facts: facts}, raw.String())
}

func stressP14PerSession(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	f, controls, panes, err := setupP4PerSession(ctx, env)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer f.close(context.Background())
	defer controls.close()
	if _, err := controls.a.command(ctx, "refresh-client -f no-output"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	controls.a.clearEvents()
	controls.b.clearEvents()
	controls.c.clearEvents()
	for i := 0; i < p14Cycles; i++ {
		if err := emitStressABC(ctx, f, panes, stressMarker("A_SECRET_", i), stressMarker("B_PUBLIC_", i), stressMarker("C_PUBLIC_", i)); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := controls.b.waitPaneOutput(ctx, panes.B, stressMarker("B_PUBLIC_", i)); err != nil {
			return false, facts, "B public marker: " + err.Error() + "\n"
		}
		if err := controls.c.waitPaneOutput(ctx, panes.C, stressMarker("C_PUBLIC_", i)); err != nil {
			return false, facts, "C public marker: " + err.Error() + "\n"
		}
		if _, err := controls.a.command(ctx, "refresh-client -f !no-output"); err != nil {
			return false, facts, err.Error() + "\n"
		}
		publicA := stressMarker("A_PUBLIC_", i)
		if err := f.emitMarker(ctx, panes.A, publicA); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := controls.a.waitPaneOutput(ctx, panes.A, publicA); err != nil {
			return false, facts, "A public marker: " + err.Error() + "\n"
		}
		if _, err := controls.a.command(ctx, "refresh-client -f no-output"); err != nil {
			return false, facts, err.Error() + "\n"
		}
	}
	if err := emitCompletionSentinels(ctx, f, panes, controls.b, controls.c); err != nil {
		return false, facts, err.Error() + "\n"
	}
	aText := paneOutputText(controls.a, panes.A)
	bText := paneOutputText(controls.b, panes.B)
	cText := paneOutputText(controls.c, panes.C)
	aPrivateCount := strings.Count(aText, "A_SECRET_")
	bComplete := stressMarkersInOrder(bText, "B_PUBLIC_", p14Cycles)
	cComplete := stressMarkersInOrder(cText, "C_PUBLIC_", p14Cycles)
	facts["a_private_count"] = fmt.Sprint(aPrivateCount)
	facts["b_count"] = fmt.Sprint(strings.Count(bText, "B_PUBLIC_"))
	facts["c_count"] = fmt.Sprint(strings.Count(cText, "C_PUBLIC_"))
	facts["b_complete"] = fmt.Sprint(bComplete)
	facts["c_complete"] = fmt.Sprint(cComplete)
	ok := aPrivateCount == 0 && bComplete && cComplete
	return ok, facts, fmt.Sprintf("A-private-count=%d B-complete=%t C-complete=%t\n", aPrivateCount, bComplete, cComplete)
}

func stressP14SharedPerPaneIfEligible(ctx context.Context, env nativeProbeEnv) (string, map[string]string, string) {
	p5OK, _, p5Raw := measureP5SharedPerPane(ctx, env)
	if !p5OK {
		return "NOT_ELIGIBLE_P6", map[string]string{"blocked": "P5"}, "P14 skipped: P5 first-byte privacy failed\n" + p5Raw
	}
	p6OK, _, p6Raw := measureP6SharedPerPane(ctx, env)
	if !p6OK {
		return "NOT_ELIGIBLE_P6", map[string]string{"blocked": "P6"}, "P14 skipped: P6 recovery privacy failed\n" + p6Raw
	}
	ok, facts, raw := stressP14SharedPerPane(ctx, env)
	if !ok {
		return "FAIL", facts, raw
	}
	return "PASS", facts, raw
}

func stressP14SharedPerPane(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	f, cleanup, failure := newProbeFixture(ctx, env, "P14", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	ctrl, err := attachSharedPerPanePrivate(ctx, f, panes.A)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer ctrl.close()
	ctrl.clearEvents()
	for i := 0; i < p14Cycles; i++ {
		if err := emitStressABC(ctx, f, panes, stressMarker("A_SECRET_", i), stressMarker("B_PUBLIC_", i), stressMarker("C_PUBLIC_", i)); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.waitPaneOutput(ctx, panes.B, stressMarker("B_PUBLIC_", i)); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.waitPaneOutput(ctx, panes.C, stressMarker("C_PUBLIC_", i)); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.setPaneOutput(ctx, panes.A, true); err != nil {
			return false, facts, err.Error() + "\n"
		}
		publicA := stressMarker("A_PUBLIC_", i)
		if err := f.emitMarker(ctx, panes.A, publicA); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.waitPaneOutput(ctx, panes.A, publicA); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.setPaneOutput(ctx, panes.A, false); err != nil {
			return false, facts, err.Error() + "\n"
		}
	}
	if err := emitCompletionSentinelsShared(ctx, f, panes, ctrl); err != nil {
		return false, facts, err.Error() + "\n"
	}
	text := ctrl.publicPaneOutput(map[string]bool{panes.A: true})
	ok := !ctrl.paneOutputContains(panes.A, "A_SECRET_") && stressMarkersInOrder(text, "B_PUBLIC_", p14Cycles) && stressMarkersInOrder(text, "C_PUBLIC_", p14Cycles)
	facts["a_private_count"] = fmt.Sprint(strings.Count(paneOutputText(ctrl, panes.A), "A_SECRET_"))
	facts["b_complete"] = fmt.Sprint(stressMarkersInOrder(text, "B_PUBLIC_", p14Cycles))
	facts["c_complete"] = fmt.Sprint(stressMarkersInOrder(text, "C_PUBLIC_", p14Cycles))
	return ok, facts, fmt.Sprintf("A-private=%t B-C-complete=%t\n", !ctrl.paneOutputContains(panes.A, "A_SECRET_"), ok)
}

func stressP14DaemonDemux(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{"raw_a_entered_parser": "false"}
	f, cleanup, failure := newProbeFixture(ctx, env, "P14", "stty -echo; exec cat")
	if failure != nil {
		return false, facts, failure.Summary + "\n"
	}
	defer cleanup()
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	demux := newDaemonDemux(map[string]bool{panes.A: true})
	ctrl, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer ctrl.close()
	ctrl.clearEvents()
	for i := 0; i < p14Cycles; i++ {
		demux.setPrivate(panes.A, true)
		if err := emitStressABC(ctx, f, panes, stressMarker("A_SECRET_", i), stressMarker("B_PUBLIC_", i), stressMarker("C_PUBLIC_", i)); err != nil {
			return false, facts, err.Error() + "\n"
		}
		for _, item := range []struct{ pane, marker string }{{panes.A, stressMarker("A_SECRET_", i)}, {panes.B, stressMarker("B_PUBLIC_", i)}, {panes.C, stressMarker("C_PUBLIC_", i)}} {
			if err := ctrl.waitPaneOutput(ctx, item.pane, item.marker); err != nil {
				return false, facts, err.Error() + "\n"
			}
		}
		demux.ingest(ctrl)
		demux.setPrivate(panes.A, false)
		publicA := stressMarker("A_PUBLIC_", i)
		if err := f.emitMarker(ctx, panes.A, publicA); err != nil {
			return false, facts, err.Error() + "\n"
		}
		if err := ctrl.waitPaneOutput(ctx, panes.A, publicA); err != nil {
			return false, facts, err.Error() + "\n"
		}
		demux.ingest(ctrl)
	}
	if err := emitCompletionSentinelsShared(ctx, f, panes, ctrl); err != nil {
		return false, facts, err.Error() + "\n"
	}
	demux.ingest(ctrl)
	public := demux.publicString()
	rawA := paneOutputText(ctrl, panes.A)
	rawACount := strings.Count(rawA, "A_SECRET_")
	bComplete := stressMarkersInOrder(public, "B_PUBLIC_", p14Cycles)
	cComplete := stressMarkersInOrder(public, "C_PUBLIC_", p14Cycles)
	ok := !strings.Contains(public, "A_SECRET_") && rawACount == p14Cycles && bComplete && cComplete
	facts["raw_a_entered_parser"] = fmt.Sprint(rawACount == p14Cycles)
	facts["a_private_count"] = fmt.Sprint(strings.Count(public, "A_SECRET_"))
	facts["b_complete"] = fmt.Sprint(bComplete)
	facts["c_complete"] = fmt.Sprint(cComplete)
	return ok, facts, fmt.Sprintf("A-private-count=%d B-complete=%t C-complete=%t raw-A-entered-parser=%t\n", strings.Count(public, "A_SECRET_"), bComplete, cComplete, rawACount == p14Cycles)
}

func emitStressABC(ctx context.Context, f *nativeFixture, panes privacyPaneSet, a, b, c string) error {
	items := []struct{ pane, marker string }{{panes.A, a}, {panes.B, b}, {panes.C, c}}
	errCh := make(chan error, len(items))
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- f.emitMarker(ctx, item.pane, item.marker)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func stressMarker(prefix string, i int) string { return fmt.Sprintf("%s%03d", prefix, i) }

func stressMarkersInOrder(text, prefix string, count int) bool {
	cursor := 0
	for i := 0; i < count; i++ {
		marker := stressMarker(prefix, i)
		next := strings.Index(text[cursor:], marker)
		if next < 0 {
			return false
		}
		cursor += next + len(marker)
	}
	return true
}

func paneOutputText(ctrl *controlClient, pane string) string {
	var out strings.Builder
	for _, event := range ctrl.eventsSnapshot() {
		if event.Kind == EventPaneOutput && event.PaneID == pane {
			out.WriteString(event.Data)
		}
	}
	return out.String()
}

func emitCompletionSentinels(ctx context.Context, f *nativeFixture, panes privacyPaneSet, b, c *controlClient) error {
	if err := f.emitMarker(ctx, panes.B, "B_COMPLETE_0128"); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, "C_COMPLETE_0128"); err != nil {
		return err
	}
	if err := b.waitPaneOutput(ctx, panes.B, "B_COMPLETE_0128"); err != nil {
		return err
	}
	return c.waitPaneOutput(ctx, panes.C, "C_COMPLETE_0128")
}

func emitCompletionSentinelsShared(ctx context.Context, f *nativeFixture, panes privacyPaneSet, ctrl *controlClient) error {
	if err := f.emitMarker(ctx, panes.B, "B_COMPLETE_0128"); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, "C_COMPLETE_0128"); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, "B_COMPLETE_0128"); err != nil {
		return err
	}
	return ctrl.waitPaneOutput(ctx, panes.C, "C_COMPLETE_0128")
}
