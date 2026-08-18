package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportRequiresEveryP0ThroughP15ExactlyOnce(t *testing.T) {
	got := validateReport(Report{Results: []ProbeResult{{ID: "P0", Status: StatusPass}}})
	if got == nil {
		t.Fatal("partial report accepted")
	}

	r := passingReport("darwin", "arm64")
	r.Results = append(r.Results, ProbeResult{ID: "P0", Status: StatusPass})
	if err := validateReport(r); err == nil {
		t.Fatal("duplicate probe id accepted")
	}

	r = passingReport("darwin", "arm64")
	r.Results[0].ID = "P16"
	if err := validateReport(r); err == nil {
		t.Fatal("unknown probe id accepted")
	}
}

func TestVerdictFailsOnGateFailureAndNotRunOnMissingNativeLane(t *testing.T) {
	results := passingResults()
	results[indexOf(results, "P14")].Status = StatusFail
	if got := verdict(results); got != StatusFail {
		t.Fatalf("verdict=%q want FAIL", got)
	}

	results = passingResults()
	results[indexOf(results, "P8")].Status = StatusNotRun
	if got := verdict(results); got != StatusNotRun {
		t.Fatalf("verdict=%q want NOT_RUN", got)
	}
}

func TestVerifyGateRejectsCallerForgedH1Allowed(t *testing.T) {
	reports := passingNativeReports(t)
	reports[0].Report.Results[indexOf(reports[0].Report.Results, "P3")].Status = StatusFail
	reports[0] = bindReport(t, reports[0].Report)
	gate := gateFromReports(reports)
	gate.H1Allowed = true
	if err := verifyGate(gate, reports); err == nil {
		t.Fatal("forged h1_allowed accepted")
	}
}

func TestVerifyGateRejectsUnboundPlatformReportDigest(t *testing.T) {
	reports := passingNativeReports(t)
	gate := gateFromReports(reports)
	gate.PlatformReports[0].ReportSHA256 = strings.Repeat("0", 64)
	if err := verifyGate(gate, reports); err == nil {
		t.Fatal("unbound platform report digest accepted")
	}
}

func TestGateDoesNotAllowH1WithoutQualifiedFenceAndObservationTopology(t *testing.T) {
	reports := passingNativeReports(t)
	gate := gateFromReports(reports)
	if gate.H1Allowed {
		t.Fatal("h1 allowed without qualified provider mechanism/topology facts")
	}
}

func TestGateDoesNotAllowH1WhenProviderFactsDisagreeAcrossPlatforms(t *testing.T) {
	reports := passingNativeReports(t)
	qualifyProviderFacts(&reports[0].Report, candidatePerSessionObserver)
	qualifyProviderFacts(&reports[1].Report, candidateSharedDaemonDemux)
	reports[0] = bindReport(t, reports[0].Report)
	reports[1] = bindReport(t, reports[1].Report)
	gate := gateFromReports(reports)
	if gate.H1Allowed || gate.ObservationTopology != "unqualified" {
		t.Fatalf("cross-platform topology disagreement accepted: %#v", gate)
	}
}

func TestGateUsesFinalQualifiedTopologyNotP4MeasurementPlaceholder(t *testing.T) {
	reports := passingNativeReports(t)
	for i := range reports {
		qualifyProviderFacts(&reports[i].Report, candidatePerSessionObserver)
		reports[i] = bindReport(t, reports[i].Report)
	}
	gate := gateFromReports(reports)
	if gate.ObservationTopology != candidatePerSessionObserver || !gate.H1Allowed {
		t.Fatalf("gate=%#v", gate)
	}
}

func qualifyProviderFacts(report *Report, candidate string) {
	report.Results[indexOf(report.Results, "P3")].Facts = map[string]string{"input_fence_mechanism": "same_client_readonly_fence"}
	for _, probeID := range []string{"P4", "P5", "P6", "P14", "P15"} {
		result := &report.Results[indexOf(report.Results, probeID)]
		if result.Facts == nil {
			result.Facts = map[string]string{}
		}
		result.Facts["candidate."+candidate+"."+strings.ToLower(probeID)] = "PASS"
	}
	report.Results[indexOf(report.Results, "P4")].Facts["observation_topology"] = "unqualified"
}

func TestGateJSONRoundTripIsDeterministic(t *testing.T) {
	reports := passingNativeReports(t)
	gate := gateFromReports(reports)
	first, err := marshalDeterministic(gate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := marshalDeterministic(gate)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("gate rendering is not deterministic")
	}
	var decoded QualificationGate
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := verifyGate(decoded, reports); err != nil {
		t.Fatalf("round-tripped gate invalid: %v", err)
	}
}

func passingResults() []ProbeResult {
	out := make([]ProbeResult, 0, 16)
	for i := 0; i < 16; i++ {
		out = append(out, ProbeResult{ID: probeID(i), Status: StatusPass, Summary: "pass"})
	}
	return out
}

func indexOf(results []ProbeResult, id string) int {
	for i := range results {
		if results[i].ID == id {
			return i
		}
	}
	return -1
}

func passingReport(goos, goarch string) Report {
	return Report{
		SchemaVersion: 1,
		GitHead:       strings.Repeat("a", 40),
		GOOS:          goos,
		GOARCH:        goarch,
		GoVersion:     "go1.26.6",
		TmuxPath:      "/usr/bin/tmux",
		TmuxVersion:   "tmux 3.6a",
		TmuxSHA256:    strings.Repeat("b", 64),
		Results:       passingResults(),
		Verdict:       StatusPass,
	}
}

func passingNativeReports(t *testing.T) []BoundReport {
	t.Helper()
	return []BoundReport{
		bindReport(t, passingReport("darwin", "arm64")),
		bindReport(t, passingReport("linux", "amd64")),
	}
}

func bindReport(t *testing.T, report Report) BoundReport {
	t.Helper()
	b, err := marshalDeterministic(report)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return BoundReport{Report: report, ReportSHA256: hex.EncodeToString(sum[:]), Path: filepath.Join(".build", report.GOOS, "report.json")}
}

func TestWriteFileAtomicProducesExactBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gate.json")
	want := []byte("hello\n")
	if err := writeFileAtomic(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q want %q", got, want)
	}
}
