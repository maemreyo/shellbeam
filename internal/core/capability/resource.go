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
