package project

type LoadState string

const (
	LoadAbsent  LoadState = "absent"
	LoadValid   LoadState = "valid"
	LoadInvalid LoadState = "invalid"
)

type LoadResult struct {
	State          LoadState
	Parsed         *Parsed
	ManifestDigest string
	Code           string
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
	DiscoveryFingerprint string
	ReviewFingerprint    string
	Code                 string
}

type Inspection struct {
	Status               Status    `json:"status"`
	SchemaVersion        int       `json:"schema_version,omitempty"`
	ManifestDigest       string    `json:"manifest_digest,omitempty"`
	DiscoveryFingerprint string    `json:"discovery_fingerprint,omitempty"`
	ReviewFingerprint    string    `json:"review_fingerprint,omitempty"`
	Confidence           string    `json:"confidence"`
	Provenance           string    `json:"provenance"`
	Code                 string    `json:"code,omitempty"`
	Manifest             *Manifest `json:"manifest,omitempty"`
}

func EvaluateStatus(input StatusInput) Status {
	switch input.LoadState {
	case LoadAbsent:
		return StatusAbsent
	case LoadValid:
		if input.ReviewFingerprint != "" && input.DiscoveryFingerprint != "" && input.ReviewFingerprint != input.DiscoveryFingerprint {
			return StatusReviewDue
		}
		return StatusValid
	default:
		return StatusInvalid
	}
}

func NewInspection(input StatusInput, manifest *Manifest) Inspection {
	status := EvaluateStatus(input)
	confidence := "high"
	if status == StatusAbsent {
		confidence = "none"
	} else if status == StatusInvalid {
		confidence = "low"
	}
	return Inspection{
		Status: status, SchemaVersion: input.SchemaVersion, ManifestDigest: input.ManifestDigest,
		DiscoveryFingerprint: input.DiscoveryFingerprint, ReviewFingerprint: input.ReviewFingerprint,
		Confidence: confidence, Provenance: "workspace_manifest", Code: input.Code, Manifest: manifest,
	}
}
