package capability

type ResourceQuality string

const (
	ResourceExact            ResourceQuality = "exact"
	ResourcePlatformReported ResourceQuality = "platform_reported"
	ResourceSampled          ResourceQuality = "sampled"
	ResourceUnavailable      ResourceQuality = "unavailable"
)

type ResourceObservationSupport struct {
	CPUTime          ResourceQuality `json:"cpu_time"`
	MaxRSS           ResourceQuality `json:"max_rss"`
	IOBytes          ResourceQuality `json:"io_bytes"`
	ProcessCountPeak ResourceQuality `json:"process_count_peak"`
}

type EnforcementQuality string

const (
	EnforcementHard        EnforcementQuality = "hard"
	EnforcementUnsupported EnforcementQuality = "unsupported"
)

type ResourceEnforcementSupport struct {
	Version            int                `json:"version"`
	Maturity           string             `json:"maturity"`
	Provider           string             `json:"provider"`
	Scope              string             `json:"scope"`
	Placement          string             `json:"placement"`
	MemoryBytes        EnforcementQuality `json:"memory_bytes"`
	Processes          EnforcementQuality `json:"processes"`
	CPUTimeMS          EnforcementQuality `json:"cpu_time_ms"`
	PersistentSessions EnforcementQuality `json:"persistent_sessions"`
}

func (s ResourceEnforcementSupport) ValidV1() bool {
	return s.Version == 1 && s.Maturity == "experimental" && s.Provider != "" &&
		s.Scope == "owned_process_tree" && s.Placement == "pre_exec_atomic" &&
		s.MemoryBytes == EnforcementHard && s.Processes == EnforcementHard &&
		s.CPUTimeMS == EnforcementUnsupported && s.PersistentSessions == EnforcementUnsupported
}
