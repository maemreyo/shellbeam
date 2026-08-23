package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const p15FaultPoints = 6

func probeP15ObserverOverlapPrivacyFault(ctx context.Context, env nativeProbeEnv) ProbeResult {
	facts := map[string]string{"fault_points": fmt.Sprint(p15FaultPoints)}
	var raw strings.Builder
	perOK, perFacts, perRaw := faultP15PerSession(ctx, env)
	facts["candidate."+candidatePerSessionObserver+".p15"] = passFail(perOK)
	mergePrefixedFacts(facts, "candidate."+candidatePerSessionObserver+".", perFacts)
	raw.WriteString("[per_session_observer]\n" + perRaw)
	sharedStatus, sharedFacts := p15SharedPerPaneStatus(ctx, env)
	facts["candidate."+candidateSharedPerPane+".p15"] = sharedStatus
	mergePrefixedFacts(facts, "candidate."+candidateSharedPerPane+".", sharedFacts)
	raw.WriteString("\n[shared_observer_with_per_pane_off]\nresult=" + sharedStatus + "\n")
	demuxOK, demuxFacts, demuxRaw := faultP15DaemonDemux(ctx, env)
	facts["candidate."+candidateSharedDaemonDemux+".p15"] = passFail(demuxOK)
	mergePrefixedFacts(facts, "candidate."+candidateSharedDaemonDemux+".", demuxFacts)
	raw.WriteString("\n[shared_observer_with_daemon_demux_simulation]\n" + demuxRaw)
	if !perOK && !demuxOK {
		facts["architecture_fork"] = "privacy_topology_required"
		return finishNativeProbe(env, ProbeResult{ID: "P15", Status: StatusFail, Summary: "no eligible topology survives observer overlap/replacement faults without private A exposure", Facts: facts}, raw.String())
	}
	return finishNativeProbe(env, ProbeResult{ID: "P15", Status: StatusPass, Summary: "eligible topology keeps every old/new observer private across six replacement fault classes while public B/C remain observable", Facts: facts}, raw.String())
}

func p15SharedPerPaneStatus(ctx context.Context, env nativeProbeEnv) (string, map[string]string) {
	facts := map[string]string{"blocked": "P5"}
	p5OK, _, _ := measureP5SharedPerPane(ctx, env)
	if !p5OK {
		return "NOT_ELIGIBLE_P6", facts
	}
	p6OK, _, _ := measureP6SharedPerPane(ctx, env)
	if !p6OK {
		facts["blocked"] = "P6"
		return "NOT_ELIGIBLE_P6", facts
	}
	facts["blocked"] = "P15_not_required_for_selected_topology"
	return "NOT_SELECTED", facts
}

type p15PerSessionState struct {
	f         *nativeFixture
	controls  p4PerSessionControls
	panes     privacyPaneSet
	observers []*controlClient
	current   *controlClient
}

func (s *p15PerSessionState) closeObservers() {
	seen := make(map[*controlClient]struct{}, len(s.observers))
	for _, observer := range s.observers {
		if observer == nil {
			continue
		}
		if _, ok := seen[observer]; ok {
			continue
		}
		seen[observer] = struct{}{}
		_ = observer.close()
	}
}

func faultP15PerSession(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{}
	state, err := newP15PerSessionState(ctx, env)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer state.f.close(context.Background())
	defer state.controls.close()
	defer state.closeObservers()
	if err := p15PerSessionStartup(ctx, state); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := p15PerSessionRapidReconnect(ctx, state); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := emitP15BC(ctx, state.f, state.controls, state.panes, "AFTER_OLD_DEATH"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := p15PerSessionServerExit(ctx, state); err != nil {
		return false, facts, err.Error() + "\n"
	}
	facts["bc_public"], facts["old_private"], facts["new_private"], facts["survivor_private"] = "true", "true", "true", "true"
	return true, facts, "old-private=true new-private=true survivor-private=true B-C-public=true fault-points=6\n"
}

func newP15PerSessionState(ctx context.Context, env nativeProbeEnv) (*p15PerSessionState, error) {
	f, controls, panes, err := setupP4PerSession(ctx, env)
	if err != nil {
		return nil, err
	}
	if _, err := controls.a.command(ctx, "refresh-client -f no-output"); err != nil {
		_ = f.close(context.Background())
		controls.close()
		return nil, err
	}
	return &p15PerSessionState{f: f, controls: controls, panes: panes, observers: []*controlClient{controls.a}, current: controls.a}, nil
}

func p15PerSessionStartup(ctx context.Context, s *p15PerSessionState) error {
	startup, err := s.f.startControl(ctx, s.f.Session, "no-output,ignore-size")
	if err != nil {
		return err
	}
	s.observers = append(s.observers, startup)
	if err := s.f.emitMarker(ctx, s.panes.A, "A_SECRET_STARTUP"); err != nil {
		return err
	}
	if err := startup.waitReady(ctx, s.f); err != nil {
		return err
	}
	if err := waitObserverPrivate(ctx, s.current, startup, "A_SECRET_STARTUP"); err != nil {
		return err
	}
	if err := emitP15BC(ctx, s.f, s.controls, s.panes, "BEFORE_OLD_DEATH"); err != nil {
		return err
	}
	gap, err := s.f.startControl(ctx, s.f.Session, "no-output,ignore-size")
	if err != nil {
		return err
	}
	s.observers = append(s.observers, gap)
	_ = s.current.close()
	_ = startup.close()
	if err := s.f.emitMarker(ctx, s.panes.A, "A_SECRET_PRE_ACK_GAP"); err != nil {
		return err
	}
	if err := gap.waitReady(ctx, s.f); err != nil {
		return err
	}
	if err := ensureMarkerAbsentFor(ctx, gap, "A_SECRET_PRE_ACK_GAP", 20*time.Millisecond); err != nil {
		return err
	}
	ack, err := s.f.startControl(ctx, s.f.Session, "no-output,ignore-size")
	if err != nil {
		return err
	}
	s.observers = append(s.observers, ack)
	if err := ack.waitReady(ctx, s.f); err != nil {
		return err
	}
	if err := s.f.emitMarker(ctx, s.panes.A, "A_SECRET_ACK_BEFORE_CLOSE"); err != nil {
		return err
	}
	if err := waitObserverPrivate(ctx, gap, ack, "A_SECRET_ACK_BEFORE_CLOSE"); err != nil {
		return err
	}
	_ = gap.close()
	s.current = ack
	return nil
}

func p15PerSessionRapidReconnect(ctx context.Context, s *p15PerSessionState) error {
	for i := 0; i < 16; i++ {
		next, err := s.f.attachControl(ctx, s.f.Session, "no-output,ignore-size")
		if err != nil {
			return err
		}
		s.observers = append(s.observers, next)
		marker := stressMarker("A_SECRET_RAPID_", i)
		if err := s.f.emitMarker(ctx, s.panes.A, marker); err != nil {
			return err
		}
		if err := waitObserverPrivate(ctx, s.current, next, marker); err != nil {
			return err
		}
		_ = s.current.close()
		s.current = next
	}
	return nil
}

func p15PerSessionServerExit(ctx context.Context, s *p15PerSessionState) error {
	if _, err := s.f.tmux(ctx, "kill-server"); err != nil {
		return err
	}
	for _, observer := range s.observers {
		if observer.anyPaneOutputContains("A_SECRET_") {
			return fmt.Errorf("private A appeared on model-visible per-session observer")
		}
	}
	return nil
}

type p15DemuxState struct {
	f           *nativeFixture
	panes       privacyPaneSet
	controls    []*controlClient
	projections []*daemonDemuxProjection
	current     *controlClient
	currentView *daemonDemuxProjection
}

func faultP15DaemonDemux(ctx context.Context, env nativeProbeEnv) (bool, map[string]string, string) {
	facts := map[string]string{"raw_a_entered_parser": "false"}
	state, cleanup, err := newP15DemuxState(ctx, env)
	if err != nil {
		return false, facts, err.Error() + "\n"
	}
	defer cleanup()
	if err := p15DemuxStartup(ctx, state); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := p15DemuxRapidReconnect(ctx, state); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := emitP15SharedBC(ctx, state.f, state.current, state.currentView, state.panes, "AFTER_OLD_DEATH"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if _, err := state.f.tmux(ctx, "kill-server"); err != nil {
		return false, facts, err.Error() + "\n"
	}
	if err := ensureDemuxesPrivate(state.projections); err != nil {
		return false, facts, err.Error() + "\n"
	}
	rawSeen := false
	for _, ctrl := range state.controls {
		rawSeen = rawSeen || ctrl.anyPaneOutputContains("A_SECRET_")
	}
	facts["raw_a_entered_parser"], facts["bc_public"] = fmt.Sprint(rawSeen), "true"
	return rawSeen, facts, "old-private-projection=true new-private-projection=true survivor-private=true B-C-public=true raw-A-entered-parser=true fault-points=6\n"
}

func newP15DemuxState(ctx context.Context, env nativeProbeEnv) (*p15DemuxState, func(), error) {
	f, cleanup, failure := newProbeFixture(ctx, env, "P15", "stty -echo; exec cat")
	if failure != nil {
		return nil, func() {}, fmt.Errorf("%s", failure.Summary)
	}
	panes, err := f.makeThreePaneSet(ctx)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	view := newDaemonDemux(map[string]bool{panes.A: true})
	old, err := f.attachControl(ctx, f.Session, "ignore-size")
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	state := &p15DemuxState{f: f, panes: panes, controls: []*controlClient{old}, projections: []*daemonDemuxProjection{view}, current: old, currentView: view}
	return state, func() {
		for _, c := range state.controls {
			_ = c.close()
		}
		cleanup()
	}, nil
}

func p15DemuxStartup(ctx context.Context, s *p15DemuxState) error {
	startup, startupView, err := s.startObserver(ctx, false)
	if err != nil {
		return err
	}
	if err := s.f.emitMarker(ctx, s.panes.A, "A_SECRET_STARTUP"); err != nil {
		return err
	}
	if err := s.current.waitPaneOutput(ctx, s.panes.A, "A_SECRET_STARTUP"); err != nil {
		return err
	}
	if err := startup.waitReady(ctx, s.f); err != nil {
		return err
	}
	if err := startup.waitPaneOutput(ctx, s.panes.A, "A_SECRET_STARTUP"); err != nil {
		return err
	}
	s.currentView.ingest(s.current)
	startupView.ingest(startup)
	if err := ensureDemuxesPrivate(s.projections); err != nil {
		return err
	}
	if err := emitP15SharedBC(ctx, s.f, startup, startupView, s.panes, "BEFORE_OLD_DEATH"); err != nil {
		return err
	}
	gap, gapView, err := s.startObserver(ctx, false)
	if err != nil {
		return err
	}
	_ = s.current.close()
	_ = startup.close()
	if err := s.observePrivateMarker(ctx, gap, gapView, "A_SECRET_PRE_ACK_GAP"); err != nil {
		return err
	}
	ack, ackView, err := s.startObserver(ctx, true)
	if err != nil {
		return err
	}
	if err := s.observePrivateMarkerOnPair(ctx, gap, gapView, ack, ackView, "A_SECRET_ACK_BEFORE_CLOSE"); err != nil {
		return err
	}
	_ = gap.close()
	s.current, s.currentView = ack, ackView
	return nil
}

func (s *p15DemuxState) startObserver(ctx context.Context, ready bool) (*controlClient, *daemonDemuxProjection, error) {
	view := newDaemonDemux(map[string]bool{s.panes.A: true})
	var ctrl *controlClient
	var err error
	if ready {
		ctrl, err = s.f.attachControl(ctx, s.f.Session, "ignore-size")
	} else {
		ctrl, err = s.f.startControl(ctx, s.f.Session, "ignore-size")
	}
	if err != nil {
		return nil, nil, err
	}
	s.controls = append(s.controls, ctrl)
	s.projections = append(s.projections, view)
	return ctrl, view, nil
}

func (s *p15DemuxState) observePrivateMarker(ctx context.Context, ctrl *controlClient, view *daemonDemuxProjection, marker string) error {
	if err := s.f.emitMarker(ctx, s.panes.A, marker); err != nil {
		return err
	}
	if err := ctrl.waitReady(ctx, s.f); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, s.panes.A, marker); err != nil {
		return err
	}
	view.ingest(ctrl)
	return ensureDemuxesPrivate(s.projections)
}

func (s *p15DemuxState) observePrivateMarkerOnPair(ctx context.Context, a *controlClient, av *daemonDemuxProjection, b *controlClient, bv *daemonDemuxProjection, marker string) error {
	if err := s.f.emitMarker(ctx, s.panes.A, marker); err != nil {
		return err
	}
	if err := a.waitPaneOutput(ctx, s.panes.A, marker); err != nil {
		return err
	}
	if err := b.waitPaneOutput(ctx, s.panes.A, marker); err != nil {
		return err
	}
	av.ingest(a)
	bv.ingest(b)
	return ensureDemuxesPrivate(s.projections)
}

func p15DemuxRapidReconnect(ctx context.Context, s *p15DemuxState) error {
	for i := 0; i < 16; i++ {
		next, nextView, err := s.startObserver(ctx, true)
		if err != nil {
			return err
		}
		marker := stressMarker("A_SECRET_RAPID_", i)
		if err := s.observePrivateMarkerOnPair(ctx, s.current, s.currentView, next, nextView, marker); err != nil {
			return err
		}
		_ = s.current.close()
		s.current, s.currentView = next, nextView
	}
	return nil
}

func waitObserverPrivate(ctx context.Context, a, b *controlClient, marker string) error {
	if err := ensureMarkerAbsentFor(ctx, a, marker, 20*time.Millisecond); err != nil {
		return err
	}
	return ensureMarkerAbsentFor(ctx, b, marker, 20*time.Millisecond)
}

func emitP15BC(ctx context.Context, f *nativeFixture, controls p4PerSessionControls, panes privacyPaneSet, suffix string) error {
	b, c := "B_PUBLIC_"+suffix, "C_PUBLIC_"+suffix
	if err := f.emitMarker(ctx, panes.B, b); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, c); err != nil {
		return err
	}
	if err := controls.b.waitPaneOutput(ctx, panes.B, b); err != nil {
		return err
	}
	return controls.c.waitPaneOutput(ctx, panes.C, c)
}

func emitP15SharedBC(ctx context.Context, f *nativeFixture, ctrl *controlClient, demux *daemonDemuxProjection, panes privacyPaneSet, suffix string) error {
	b, c := "B_PUBLIC_"+suffix, "C_PUBLIC_"+suffix
	if err := f.emitMarker(ctx, panes.B, b); err != nil {
		return err
	}
	if err := f.emitMarker(ctx, panes.C, c); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.B, b); err != nil {
		return err
	}
	if err := ctrl.waitPaneOutput(ctx, panes.C, c); err != nil {
		return err
	}
	demux.ingest(ctrl)
	if !strings.Contains(demux.publicString(), b) || !strings.Contains(demux.publicString(), c) {
		return fmt.Errorf("B/C missing from public demux")
	}
	return nil
}

func ensureDemuxesPrivate(projections []*daemonDemuxProjection) error {
	for _, projection := range projections {
		if strings.Contains(projection.publicString(), "A_SECRET_") {
			return fmt.Errorf("private A leaked into daemon public projection")
		}
	}
	return nil
}
