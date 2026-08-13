// Package capability describes protocol support without probing by trial execution.
package capability

type Feature string
type Availability string

const (
	Available   Availability = "available"
	Unavailable Availability = "unavailable"
)

const (
	FeatureWorkspaceAddressing    Feature = "workspace_addressing"
	FeatureWorkspaceProvenance    Feature = "workspace_provenance"
	FeatureActivities             Feature = "activities"
	FeatureArgvMode               Feature = "argv_mode"
	FeatureOutputViews            Feature = "output_views"
	FeatureNamedSessions          Feature = "named_sessions"
	FeatureProcessInspection      Feature = "process_inspection"
	FeatureEvidenceLedger         Feature = "evidence_ledger"
	FeatureExpectedOutputs        Feature = "expected_outputs"
	FeatureEnvironmentFingerprint Feature = "environment_fingerprint"
	FeatureMutationScopes         Feature = "mutation_scopes"
	FeatureProjectManifest        Feature = "project_manifest"
)

type Limits struct {
	CommandBytes       int   `json:"command_bytes"`
	ResponseBytes      int   `json:"response_bytes"`
	SessionOutputBytes int64 `json:"session_output_bytes"`
	RuntimeMS          int64 `json:"runtime_ms"`
	LiveSessions       int   `json:"live_sessions"`
	ActivityHistory    int   `json:"activity_history"`
}

type Catalog struct {
	ProtocolVersion       int                      `json:"shellbeam_protocol_version"`
	ReceiptSchemaVersions []int                    `json:"receipt_schema_versions"`
	ManifestVersions      []int                    `json:"project_manifest_schema_versions"`
	Features              map[Feature]Availability `json:"features"`
	Limits                Limits                   `json:"limits"`
}

var targetFeatures = []Feature{
	FeatureWorkspaceAddressing,
	FeatureWorkspaceProvenance,
	FeatureActivities,
	FeatureArgvMode,
	FeatureOutputViews,
	FeatureNamedSessions,
	FeatureProcessInspection,
	FeatureEvidenceLedger,
	FeatureExpectedOutputs,
	FeatureEnvironmentFingerprint,
	FeatureMutationScopes,
	FeatureProjectManifest,
}

func TargetFeatures() []Feature {
	return append([]Feature(nil), targetFeatures...)
}

func Baseline(limits Limits) Catalog {
	features := make(map[Feature]Availability, len(targetFeatures))
	for _, feature := range targetFeatures {
		features[feature] = Unavailable
	}
	return Catalog{
		ProtocolVersion:       2,
		ReceiptSchemaVersions: []int{1},
		ManifestVersions:      []int{},
		Features:              features,
		Limits:                limits,
	}
}

func (c Catalog) Clone() Catalog {
	out := c
	out.ReceiptSchemaVersions = append([]int(nil), c.ReceiptSchemaVersions...)
	out.ManifestVersions = append([]int(nil), c.ManifestVersions...)
	out.Features = make(map[Feature]Availability, len(c.Features))
	for feature, availability := range c.Features {
		out.Features[feature] = availability
	}
	return out
}
