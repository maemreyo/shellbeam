package main

import "fmt"

type Status string

const (
	StatusPass   Status = "PASS"
	StatusFail   Status = "FAIL"
	StatusNotRun Status = "NOT_RUN"
)

const (
	reportSchemaVersion = 1
	gateSchemaVersion   = 1
	gateKind            = "provider_qualification"
	frozenSpecCommit    = "5351215de2c02ac61ac82751c1680a35744047af"
)

var genuineGateIDs = []string{"P3", "P4", "P5", "P6", "P14", "P15"}
var requiredPlatforms = []string{"darwin", "linux"}

type ProbeResult struct {
	ID      string            `json:"id"`
	Status  Status            `json:"status"`
	Summary string            `json:"summary"`
	Facts   map[string]string `json:"facts,omitempty"`
}

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	GitHead       string        `json:"git_head"`
	GOOS          string        `json:"goos"`
	GOARCH        string        `json:"goarch"`
	GoVersion     string        `json:"go_version"`
	TmuxPath      string        `json:"tmux_path"`
	TmuxVersion   string        `json:"tmux_version"`
	TmuxSHA256    string        `json:"tmux_sha256"`
	Results       []ProbeResult `json:"results"`
	Verdict       Status        `json:"verdict"`
}

type ReportBinding struct {
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	TmuxPath     string `json:"tmux_path"`
	TmuxVersion  string `json:"tmux_version"`
	TmuxSHA256   string `json:"tmux_sha256"`
	Verdict      Status `json:"verdict"`
	ReportPath   string `json:"report_path"`
	ReportSHA256 string `json:"report_sha256"`
}

type QualificationGate struct {
	SchemaVersion       int             `json:"schema_version"`
	GateKind            string          `json:"gate_kind"`
	SpecCommit          string          `json:"spec_commit"`
	RequiredPlatforms   []string        `json:"required_platforms"`
	RequiredProbeIDs    []string        `json:"required_probe_ids"`
	GenuineGateIDs      []string        `json:"genuine_gate_ids"`
	PlatformReports     []ReportBinding `json:"platform_reports"`
	ProviderID          string          `json:"provider_id"`
	ProviderVersion     int             `json:"provider_version"`
	InputFenceMechanism string          `json:"input_fence_mechanism"`
	ObservationTopology string          `json:"observation_topology"`
	ControlAdapter      string          `json:"control_adapter"`
	H1Allowed           bool            `json:"h1_allowed"`
}

type BoundReport struct {
	Report       Report
	ReportSHA256 string
	Path         string
}

func probeID(i int) string {
	if i < 0 || i > 15 {
		return ""
	}
	return fmt.Sprintf("P%d", i)
}

func requiredProbeIDs() []string {
	ids := make([]string, 16)
	for i := range ids {
		ids[i] = probeID(i)
	}
	return ids
}
