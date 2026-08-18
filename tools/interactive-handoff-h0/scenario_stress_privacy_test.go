package main

import (
	"testing"
	"time"
)

func TestH0P14MultiSessionPrivacyIsolation(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P14", probeP14MultiSessionPrivacyIsolation, 90*time.Second)
	if result.Status != StatusPass {
		t.Fatalf("P14=%s summary=%s facts=%#v", result.Status, result.Summary, result.Facts)
	}
	want := map[string]string{
		"cycles":                             "128",
		"candidate.per_session_observer.p14": "PASS",
		"candidate.shared_observer_with_per_pane_off.p14":                             "NOT_ELIGIBLE_P6",
		"candidate.shared_observer_with_daemon_demux_simulation.p14":                  "PASS",
		"candidate.per_session_observer.a_private_count":                              "0",
		"candidate.per_session_observer.b_complete":                                   "true",
		"candidate.per_session_observer.c_complete":                                   "true",
		"candidate.shared_observer_with_daemon_demux_simulation.raw_a_entered_parser": "true",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P14 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}

func TestH0P15ObserverOverlapPrivacyFault(t *testing.T) {
	result := runPrivacyProbeForTest(t, "P15", probeP15ObserverOverlapPrivacyFault, 90*time.Second)
	if result.Status != StatusPass {
		t.Fatalf("P15=%s summary=%s facts=%#v", result.Status, result.Summary, result.Facts)
	}
	want := map[string]string{
		"fault_points":                                                                "6",
		"candidate.per_session_observer.p15":                                          "PASS",
		"candidate.shared_observer_with_per_pane_off.p15":                             "NOT_ELIGIBLE_P6",
		"candidate.shared_observer_with_daemon_demux_simulation.p15":                  "PASS",
		"candidate.per_session_observer.bc_public":                                    "true",
		"candidate.shared_observer_with_daemon_demux_simulation.raw_a_entered_parser": "true",
	}
	for key, value := range want {
		if result.Facts[key] != value {
			t.Fatalf("P15 fact %s=%q want %q; all=%#v", key, result.Facts[key], value, result.Facts)
		}
	}
}
