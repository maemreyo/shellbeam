package project

type LoadState string

const (
	LoadAbsent  LoadState = "absent"
	LoadValid   LoadState = "valid"
	LoadInvalid LoadState = "invalid"

	CodeManifestAbsent = "project_manifest_absent"
)

type AgentBootstrap struct {
	Path       string `json:"path"`
	Provenance string `json:"provenance"`
}

const AgentBootstrapWorkspaceConvention = "workspace_convention"

type LoadResult struct {
	State                LoadState
	Parsed               *Parsed
	ManifestDigest       string
	DiscoveryFingerprint string
	DetectedFamilies     []string
	DiscoveryEvidence    []string
	AgentBootstrap       *AgentBootstrap
	Code                 string
}

type Status string

const (
	StatusAbsent    Status = "absent"
	StatusValid     Status = "valid"
	StatusInvalid   Status = "invalid"
	StatusReviewDue Status = "review_due"
)

type StatusInput struct {
	LoadState            LoadState
	SchemaVersion        int
	ManifestDigest       string
	ManifestFingerprint  string
	DiscoveryFingerprint string
	ReviewFingerprint    string
	Review               *Review
	DetectedFamilies     []string
	DiscoveryEvidence    []string
	Code                 string
	AgentBootstrap       *AgentBootstrap
}

type Inspection struct {
	Status               Status          `json:"status"`
	SchemaVersion        int             `json:"schema_version,omitempty"`
	ManifestDigest       string          `json:"manifest_digest,omitempty"`
	DiscoveryFingerprint string          `json:"discovery_fingerprint,omitempty"`
	ReviewFingerprint    string          `json:"review_fingerprint,omitempty"`
	DetectedFamilies     []string        `json:"detected_families,omitempty"`
	DiscoveryEvidence    []string        `json:"discovery_evidence,omitempty"`
	Confidence           string          `json:"confidence"`
	Provenance           string          `json:"provenance"`
	Code                 string          `json:"code,omitempty"`
	AgentBootstrap       *AgentBootstrap `json:"agent_bootstrap,omitempty"`
	Manifest             *Manifest       `json:"manifest,omitempty"`
}

func EvaluateStatus(input StatusInput) Status {
	switch input.LoadState {
	case LoadAbsent:
		return StatusAbsent
	case LoadValid:
		if input.Review != nil {
			if !input.Review.Current(input.ManifestFingerprint, input.DiscoveryFingerprint, input.SchemaVersion) {
				return StatusReviewDue
			}
			return StatusValid
		}
		if input.ReviewFingerprint != "" {
			if input.DiscoveryFingerprint != "" && input.ReviewFingerprint == input.DiscoveryFingerprint {
				return StatusValid
			}
			return StatusReviewDue
		}
		return StatusReviewDue
	default:
		return StatusInvalid
	}
}

func NewInspection(input StatusInput, manifest *Manifest) Inspection {
	status := EvaluateStatus(input)
	confidence := "high"
	provenance := "workspace_manifest"
	if status == StatusAbsent {
		confidence = "none"
		if len(input.DetectedFamilies) > 0 {
			confidence = "medium"
			provenance = "workspace_discovery"
		}
	} else if status == StatusInvalid {
		confidence = "low"
	}
	reviewFingerprint := input.ReviewFingerprint
	if input.Review != nil {
		reviewFingerprint = input.Review.DiscoveryFingerprint
	}
	return Inspection{
		Status: status, SchemaVersion: input.SchemaVersion, ManifestDigest: input.ManifestDigest,
		DiscoveryFingerprint: input.DiscoveryFingerprint, ReviewFingerprint: reviewFingerprint,
		DetectedFamilies: append([]string(nil), input.DetectedFamilies...), DiscoveryEvidence: append([]string(nil), input.DiscoveryEvidence...),
		Confidence: confidence, Provenance: provenance, Code: input.Code, AgentBootstrap: cloneAgentBootstrap(input.AgentBootstrap), Manifest: manifest,
	}
}

func cloneAgentBootstrap(value *AgentBootstrap) *AgentBootstrap {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
