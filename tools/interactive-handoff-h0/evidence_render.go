package main

import (
	"fmt"
	"io"
	"strings"
)

func finalH0Verdict(gate QualificationGate, reports []BoundReport) Status {
	byPlatform := make(map[string]Status, len(reports))
	for _, bound := range reports {
		byPlatform[bound.Report.GOOS] = bound.Report.Verdict
	}
	for _, platform := range requiredPlatforms {
		status, ok := byPlatform[platform]
		if !ok || status == StatusNotRun {
			return StatusNotRun
		}
		if status == StatusFail {
			return StatusFail
		}
	}
	if !gate.H1Allowed {
		return StatusFail
	}
	return StatusPass
}

func probeFacts(report Report, id string) map[string]string {
	for _, result := range report.Results {
		if result.ID == id {
			return result.Facts
		}
	}
	return nil
}

func factOrUnavailable(facts map[string]string, key string) string {
	if value := facts[key]; value != "" {
		return value
	}
	return "unavailable"
}

func renderQualificationSummary(w io.Writer, gate QualificationGate, reports []BoundReport) {
	final := finalH0Verdict(gate, reports)
	fmt.Fprintf(w, "- Final H0 verdict: `%s`\n", final)
	for _, qualification := range gate.PlatformH1 {
		fmt.Fprintf(w, "- H1_ALLOWED_%s: `%t`\n", strings.ToUpper(qualification.GOOS), qualification.Allowed)
		if qualification.GOOS == "darwin" {
			fmt.Fprintf(w, "- Darwin platform fence: `%s`\n", qualification.InputFenceMechanism)
			fmt.Fprintf(w, "- Darwin platform topology: `%s`\n", qualification.ObservationTopology)
		}
	}
	fmt.Fprintf(w, "- Input fence mechanism: `%s`\n", gate.InputFenceMechanism)
	fmt.Fprintf(w, "- Observation topology: `%s`\n", gate.ObservationTopology)
	fmt.Fprintf(w, "- Control adapter: `%s`\n", gate.ControlAdapter)
	switch final {
	case StatusPass:
		fmt.Fprintln(w, "- Gate reason: required native platform qualification and genuine gates passed.")
	case StatusNotRun:
		if qualification, ok := platformH1Qualification(gate, "darwin"); ok && qualification.Allowed {
			fmt.Fprintln(w, "- Gate reason: cross-platform qualification remains `NOT_RUN`; Darwin-only experimental H1 may advance while unqualified platforms remain unadvertised and fail-closed.")
		} else {
			fmt.Fprintln(w, "- Gate reason: one or more required native lanes are `NOT_RUN`; no platform-scoped H1 lane is currently qualified.")
		}
	default:
		fmt.Fprintln(w, "- Gate reason: provider qualification failed; architecture fork or requalification is required before H1 can open.")
	}
}

func renderLoadBearingEvidence(w io.Writer, reports []BoundReport) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Load-bearing provider evidence")
	for _, bound := range reports {
		fmt.Fprintf(w, "\n### %s/%s\n\n", bound.Report.GOOS, bound.Report.GOARCH)
		fmt.Fprintf(w, "- Raw report: `%s`\n", bound.Path)
		if bound.Report.Verdict != StatusPass {
			fmt.Fprintf(w, "- Native lane verdict: `%s`; provider facts were not inferred.\n", bound.Report.Verdict)
			continue
		}
		p4 := probeFacts(bound.Report, "P4")
		p5 := probeFacts(bound.Report, "P5")
		p7 := probeFacts(bound.Report, "P7")
		p8 := probeFacts(bound.Report, "P8")
		p9 := probeFacts(bound.Report, "P9")
		p12 := probeFacts(bound.Report, "P12")
		fmt.Fprintf(w, "- P4 eligible privacy candidates: `%s`\n", factOrUnavailable(p4, "p4_eligible_candidates"))
		fmt.Fprintf(w, "- P5 first-byte passing candidates: `%s`\n", factOrUnavailable(p5, "p5_passing_candidates"))
		fmt.Fprintf(w, "- per-pane off stops tmux reading: `%s`\n", factOrUnavailable(p4, "shared_per_pane.per_pane_off_stops_reading"))
		fmt.Fprintf(w, "- daemon demux private bytes enter parser: `%s`\n", factOrUnavailable(p5, "candidate.shared_observer_with_daemon_demux_simulation.private_bytes_enter_control_parser"))
		fmt.Fprintf(w, "- negative control without `-E`: `%s`\n", factOrUnavailable(p7, "negative_control_without_E"))
		fmt.Fprintf(w, "- attach with `-E`: `%s`; switch with `-E`: `%s`; control reconnect with `-E`: `%s`\n", factOrUnavailable(p7, "attach_with_E"), factOrUnavailable(p7, "switch_with_E"), factOrUnavailable(p7, "control_reconnect_with_E"))
		fmt.Fprintf(w, "- OOB transport: `%s`; foreground child received control key: `%s`\n", factOrUnavailable(p8, "signal_transport"), factOrUnavailable(p8, "foreground_child_received_key"))
		fmt.Fprintf(w, "- read-only detach to local control: `%s`; ingress proxy introduced: `%s`\n", factOrUnavailable(p9, "detach_while_readonly"), factOrUnavailable(p9, "ingress_proxy_introduced"))
		fmt.Fprintf(w, "- all-control-off backpressure: `%s`; human display prevents backpressure: `%s`; cross-client total ordering claimed: `%s`\n", factOrUnavailable(p12, "all_control_clients_off_backpressures"), factOrUnavailable(p12, "human_display_prevents_backpressure"), factOrUnavailable(p12, "cross_client_total_ordering_claimed"))
	}
}
