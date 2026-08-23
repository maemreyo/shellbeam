package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	wrapperCandidate      = "github.com/atomicstack/gotmuxcc@v0.1.4"
	wrapperVerdict        = "FAIL"
	wrapperReason         = "P5_FIRST_BYTE_PRIVACY+PANE_OUTPUT_PARSE_ERROR"
	wrapperRecommendation = "own thin Control Mode adapter"
)

func validateStatus(s Status) error {
	switch s {
	case StatusPass, StatusFail, StatusNotRun:
		return nil
	default:
		return fmt.Errorf("unknown status %q", s)
	}
}

func probeNumber(id string) (int, bool) {
	if len(id) < 2 || id[0] != 'P' {
		return 0, false
	}
	n, err := strconv.Atoi(id[1:])
	if err != nil || n < 0 || n > 15 || probeID(n) != id {
		return 0, false
	}
	return n, true
}

func sortedResults(results []ProbeResult) []ProbeResult {
	out := append([]ProbeResult(nil), results...)
	sort.Slice(out, func(i, j int) bool {
		a, aok := probeNumber(out[i].ID)
		b, bok := probeNumber(out[j].ID)
		if !aok || !bok {
			return out[i].ID < out[j].ID
		}
		return a < b
	})
	return out
}

func validateReport(r Report) error {
	if r.SchemaVersion != reportSchemaVersion {
		return fmt.Errorf("report schema_version=%d want %d", r.SchemaVersion, reportSchemaVersion)
	}
	if len(r.Results) != 16 {
		return fmt.Errorf("report has %d probes, want 16", len(r.Results))
	}
	seen := make(map[string]struct{}, 16)
	for _, result := range r.Results {
		if _, ok := probeNumber(result.ID); !ok {
			return fmt.Errorf("unknown probe id %q", result.ID)
		}
		if _, exists := seen[result.ID]; exists {
			return fmt.Errorf("duplicate probe id %q", result.ID)
		}
		seen[result.ID] = struct{}{}
		if err := validateStatus(result.Status); err != nil {
			return fmt.Errorf("%s: %w", result.ID, err)
		}
	}
	for _, id := range requiredProbeIDs() {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("missing probe id %q", id)
		}
	}
	wantVerdict := verdict(r.Results)
	if r.Verdict != wantVerdict {
		return fmt.Errorf("report verdict=%q want %q", r.Verdict, wantVerdict)
	}
	return nil
}

func verdict(results []ProbeResult) Status {
	if len(results) != 16 {
		return StatusNotRun
	}
	seen := make(map[string]bool, 16)
	anyNotRun := false
	for _, result := range results {
		if _, ok := probeNumber(result.ID); !ok || seen[result.ID] {
			return StatusFail
		}
		seen[result.ID] = true
		if result.Status == StatusFail {
			return StatusFail
		}
		if result.Status == StatusNotRun {
			anyNotRun = true
		}
		if err := validateStatus(result.Status); err != nil {
			return StatusFail
		}
	}
	if anyNotRun {
		return StatusNotRun
	}
	return StatusPass
}

func marshalDeterministic(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func reportDigest(report Report) (string, error) {
	canonical := report
	canonical.Results = sortedResults(canonical.Results)
	b, err := marshalDeterministic(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func gateFromReports(reports []BoundReport) QualificationGate {
	bindings := make([]ReportBinding, 0, len(reports))
	for _, bound := range reports {
		r := bound.Report
		bindings = append(bindings, ReportBinding{
			GOOS:         r.GOOS,
			GOARCH:       r.GOARCH,
			TmuxPath:     r.TmuxPath,
			TmuxVersion:  r.TmuxVersion,
			TmuxSHA256:   r.TmuxSHA256,
			Verdict:      r.Verdict,
			ReportPath:   bound.Path,
			ReportSHA256: bound.ReportSHA256,
		})
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].GOOS != bindings[j].GOOS {
			return bindings[i].GOOS < bindings[j].GOOS
		}
		return bindings[i].GOARCH < bindings[j].GOARCH
	})
	gate := QualificationGate{
		SchemaVersion:       gateSchemaVersion,
		GateKind:            gateKind,
		SpecCommit:          frozenSpecCommit,
		RequiredPlatforms:   append([]string(nil), requiredPlatforms...),
		RequiredProbeIDs:    requiredProbeIDs(),
		GenuineGateIDs:      append([]string(nil), genuineGateIDs...),
		PlatformReports:     bindings,
		PlatformH1:          derivePlatformH1Qualifications(reports),
		ProviderID:          "tmux_control_mode",
		ProviderVersion:     1,
		InputFenceMechanism: consensusProbeFact(reports, "P3", "input_fence_mechanism"),
		ObservationTopology: consensusQualifiedObservationTopology(reports),
		ControlAdapter:      "raw_control_mode",
	}
	gate.H1Allowed = deriveH1Allowed(gate, reports)
	return gate
}

func consensusProbeFact(reports []BoundReport, probeID, key string) string {
	var value string
	for _, bound := range reports {
		var found string
		for _, result := range bound.Report.Results {
			if result.ID == probeID {
				found = result.Facts[key]
				break
			}
		}
		if found == "" {
			return "unqualified"
		}
		if value == "" {
			value = found
			continue
		}
		if found != value {
			return "unqualified"
		}
	}
	if value == "" {
		return "unqualified"
	}
	return value
}

func consensusQualifiedObservationTopology(reports []BoundReport) string {
	var selected string
	for _, bound := range reports {
		candidate := qualifiedObservationTopology(bound.Report)
		if candidate == "unqualified" {
			return "unqualified"
		}
		if selected == "" {
			selected = candidate
			continue
		}
		if selected != candidate {
			return "unqualified"
		}
	}
	if selected == "" {
		return "unqualified"
	}
	return selected
}

func qualifiedObservationTopology(report Report) string {
	for _, candidate := range []string{candidatePerSessionObserver, candidateSharedPerPane, candidateSharedDaemonDemux} {
		if privacyCandidatePassed(report, candidate) {
			return candidate
		}
	}
	return "unqualified"
}

func privacyCandidatePassed(report Report, candidate string) bool {
	for _, probeID := range []string{"P4", "P5", "P6", "P14", "P15"} {
		status := ""
		for _, result := range report.Results {
			if result.ID == probeID {
				status = result.Facts["candidate."+candidate+"."+strings.ToLower(probeID)]
				break
			}
		}
		if status != "PASS" {
			return false
		}
	}
	return true
}

func deriveH1Allowed(gate QualificationGate, reports []BoundReport) bool {
	if gate.SchemaVersion != gateSchemaVersion || gate.GateKind != gateKind || gate.SpecCommit != frozenSpecCommit {
		return false
	}
	if gate.ProviderID != "tmux_control_mode" || gate.ProviderVersion != 1 || gate.ControlAdapter != "raw_control_mode" {
		return false
	}
	if gate.InputFenceMechanism == "" || gate.InputFenceMechanism == "unqualified" || gate.ObservationTopology == "" || gate.ObservationTopology == "unqualified" {
		return false
	}
	if gate.InputFenceMechanism != consensusProbeFact(reports, "P3", "input_fence_mechanism") || gate.ObservationTopology != consensusQualifiedObservationTopology(reports) {
		return false
	}
	byPlatform := make(map[string]BoundReport, len(reports))
	for _, bound := range reports {
		if validateReport(bound.Report) != nil {
			return false
		}
		if _, exists := byPlatform[bound.Report.GOOS]; exists {
			return false
		}
		byPlatform[bound.Report.GOOS] = bound
	}
	for _, platform := range requiredPlatforms {
		bound, ok := byPlatform[platform]
		if !ok || bound.Report.Verdict != StatusPass {
			return false
		}
		for _, gateID := range genuineGateIDs {
			found := false
			for _, result := range bound.Report.Results {
				if result.ID == gateID {
					found = result.Status == StatusPass
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

func verifyGate(gate QualificationGate, reports []BoundReport) error {
	if gate.SchemaVersion != gateSchemaVersion {
		return fmt.Errorf("gate schema_version=%d want %d", gate.SchemaVersion, gateSchemaVersion)
	}
	if gate.GateKind != gateKind {
		return fmt.Errorf("gate_kind=%q want %q", gate.GateKind, gateKind)
	}
	if gate.SpecCommit != frozenSpecCommit {
		return fmt.Errorf("spec_commit=%q want %q", gate.SpecCommit, frozenSpecCommit)
	}
	if !equalStrings(gate.RequiredPlatforms, requiredPlatforms) {
		return errors.New("required_platforms mismatch")
	}
	if !equalStrings(gate.RequiredProbeIDs, requiredProbeIDs()) {
		return errors.New("required_probe_ids mismatch")
	}
	if !equalStrings(gate.GenuineGateIDs, genuineGateIDs) {
		return errors.New("genuine_gate_ids mismatch")
	}
	if len(gate.PlatformReports) != len(reports) {
		return fmt.Errorf("platform report bindings=%d reports=%d", len(gate.PlatformReports), len(reports))
	}

	bindingByKey := make(map[string]ReportBinding, len(gate.PlatformReports))
	for _, binding := range gate.PlatformReports {
		key := binding.GOOS + "/" + binding.GOARCH
		if _, exists := bindingByKey[key]; exists {
			return fmt.Errorf("duplicate platform binding %s", key)
		}
		bindingByKey[key] = binding
	}
	for _, bound := range reports {
		if err := validateReport(bound.Report); err != nil {
			return err
		}
		if len(bound.ReportSHA256) != 64 {
			return fmt.Errorf("invalid report digest for %s/%s", bound.Report.GOOS, bound.Report.GOARCH)
		}
		if _, err := hex.DecodeString(bound.ReportSHA256); err != nil {
			return fmt.Errorf("invalid report digest for %s/%s: %w", bound.Report.GOOS, bound.Report.GOARCH, err)
		}
		key := bound.Report.GOOS + "/" + bound.Report.GOARCH
		binding, ok := bindingByKey[key]
		if !ok {
			return fmt.Errorf("missing platform binding %s", key)
		}
		if binding.ReportSHA256 != bound.ReportSHA256 {
			return fmt.Errorf("unbound platform report digest for %s", key)
		}
		if binding.ReportPath != bound.Path || binding.Verdict != bound.Report.Verdict || binding.TmuxPath != bound.Report.TmuxPath || binding.TmuxVersion != bound.Report.TmuxVersion || binding.TmuxSHA256 != bound.Report.TmuxSHA256 {
			return fmt.Errorf("platform identity mismatch for %s", key)
		}
	}
	wantPlatformH1 := derivePlatformH1Qualifications(reports)
	if !equalPlatformH1(gate.PlatformH1, wantPlatformH1) {
		return fmt.Errorf("platform_h1 mismatch")
	}
	want := deriveH1Allowed(gate, reports)
	if gate.H1Allowed != want {
		return fmt.Errorf("h1_allowed=%t want derived %t", gate.H1Allowed, want)
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func renderMarkdown(gate QualificationGate, reports []BoundReport) ([]byte, error) {
	if err := verifyGate(gate, reports); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	fmt.Fprintln(&b, "# Interactive Handoff H0 tmux Qualification")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Spec commit: `%s`\n", gate.SpecCommit)
	fmt.Fprintf(&b, "- Provider: `%s` v%d\n", gate.ProviderID, gate.ProviderVersion)
	fmt.Fprintf(&b, "- H1_ALLOWED: `%t`\n", gate.H1Allowed)
	renderQualificationSummary(&b, gate, reports)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Optional wrapper qualification")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- Candidate: `%s`\n", wrapperCandidate)
	fmt.Fprintf(&b, "- Verdict: `%s` (advisory; not part of `H1_ALLOWED`)\n", wrapperVerdict)
	fmt.Fprintf(&b, "- Reason: `%s`\n", wrapperReason)
	fmt.Fprintf(&b, "- Recommendation: `%s`\n", wrapperRecommendation)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Platform | tmux | Verdict | Report SHA-256 |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, binding := range gate.PlatformReports {
		fmt.Fprintf(&b, "| %s/%s | `%s` | %s | `%s` |\n", binding.GOOS, binding.GOARCH, binding.TmuxVersion, binding.Verdict, binding.ReportSHA256)
	}
	renderLoadBearingEvidence(&b, reports)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## P0–P15")
	for _, bound := range reports {
		fmt.Fprintf(&b, "\n### %s/%s\n\n", bound.Report.GOOS, bound.Report.GOARCH)
		fmt.Fprintln(&b, "| Probe | Status | Summary |")
		fmt.Fprintln(&b, "|---|---|---|")
		for _, result := range sortedResults(bound.Report.Results) {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", result.ID, result.Status, strings.ReplaceAll(result.Summary, "|", "\\|"))
		}
	}
	return b.Bytes(), nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".h0-tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
