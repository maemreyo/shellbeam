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
	FeatureProjectReadiness       Feature = "project_readiness"
	FeatureTypedProjectCommands   Feature = "typed_project_commands"
	FeatureStructuredResults      Feature = "structured_results"
	FeatureStructuredLifecycle    Feature = "structured_lifecycle"
	FeatureEventJournal           Feature = "event_journal"
	FeatureEventSnapshotRecovery  Feature = "event_snapshot_recovery"
	FeatureCodeIntelligence       Feature = "code_intelligence"
	FeatureExecutionTelemetry     Feature = "execution_telemetry"
	FeatureReproductionCapsules   Feature = "reproduction_capsules"
)

type Limits struct {
	CommandBytes                   int   `json:"command_bytes"`
	ResponseBytes                  int   `json:"response_bytes"`
	SessionOutputBytes             int64 `json:"session_output_bytes"`
	RuntimeMS                      int64 `json:"runtime_ms"`
	LiveSessions                   int   `json:"live_sessions"`
	ActivityHistory                int   `json:"activity_history"`
	EventJournalMaxEvents          int   `json:"event_journal_max_events,omitempty"`
	EventCursorBytes               int   `json:"event_cursor_bytes,omitempty"`
	EventSnapshotFacts             int   `json:"event_snapshot_facts,omitempty"`
	StructuredInspectRecords       int   `json:"structured_inspect_records,omitempty"`
	TelemetryMaxSamples            int   `json:"telemetry_max_samples,omitempty"`
	TelemetryMetadataBytes         int64 `json:"telemetry_metadata_bytes,omitempty"`
	TelemetryMaxKeys               int   `json:"telemetry_max_keys,omitempty"`
	TelemetryMaxKeysPerRepository  int   `json:"telemetry_max_keys_per_repository,omitempty"`
	TelemetryMaxSamplesPerKey      int   `json:"telemetry_max_samples_per_key,omitempty"`
	TelemetryRetentionAgeMS        int64 `json:"telemetry_retention_age_ms,omitempty"`
	TelemetryInspectSamples        int   `json:"telemetry_inspect_samples,omitempty"`
	ReproMaxCapsules               int   `json:"repro_max_capsules,omitempty"`
	ReproMaxReferences             int   `json:"repro_max_references,omitempty"`
	ReproMetadataBytes             int   `json:"repro_metadata_bytes,omitempty"`
	ReadinessCacheTTLMS            int64 `json:"readiness_cache_ttl_ms,omitempty"`
	ReadinessCacheEntries          int   `json:"readiness_cache_entries,omitempty"`
	OutputViewMaxReturnBytes       int   `json:"output_view_max_return_bytes,omitempty"`
	OutputViewMaxWorkBytes         int   `json:"output_view_max_work_bytes,omitempty"`
	OutputViewMaxLines             int   `json:"output_view_max_lines,omitempty"`
	OutputViewMaxMatches           int   `json:"output_view_max_matches,omitempty"`
	OutputViewMaxPatternBytes      int   `json:"output_view_max_pattern_bytes,omitempty"`
	OutputViewMaxContinuationBytes int   `json:"output_view_max_continuation_bytes,omitempty"`
	EvidenceInspectRecords         int   `json:"evidence_inspect_records,omitempty"`
	EvidenceExpectedOutputs        int   `json:"evidence_expected_outputs,omitempty"`
	EvidenceArtifactMetadataBytes  int   `json:"evidence_artifact_metadata_bytes,omitempty"`
	EvidenceArtifactDigestBytes    int64 `json:"evidence_artifact_digest_bytes,omitempty"`
	EvidenceTreeEntries            int   `json:"evidence_tree_entries,omitempty"`
	EvidenceCursorBytes            int   `json:"evidence_cursor_bytes,omitempty"`
}

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

type Catalog struct {
	ProtocolVersion                   int                         `json:"shellbeam_protocol_version"`
	ReceiptSchemaVersions             []int                       `json:"receipt_schema_versions"`
	ManifestVersions                  []int                       `json:"project_manifest_schema_versions"`
	EventCursorSchemaVersions         []int                       `json:"event_cursor_schema_versions,omitempty"`
	ResultCursorSchemaVersions        []int                       `json:"result_cursor_schema_versions,omitempty"`
	StructuredAdapterIDs              []string                    `json:"structured_adapter_ids,omitempty"`
	StructuredResultKinds             []string                    `json:"structured_result_kinds,omitempty"`
	StructuredLifecycle               bool                        `json:"structured_lifecycle,omitempty"`
	TelemetrySchemaVersions           []int                       `json:"telemetry_schema_versions,omitempty"`
	ReproSchemaVersions               []int                       `json:"repro_schema_versions,omitempty"`
	ReadinessSchemaVersions           []int                       `json:"project_readiness_schema_versions,omitempty"`
	OutputViewSchemaVersions          []int                       `json:"output_view_schema_versions,omitempty"`
	EvidenceSchemaVersions            []int                       `json:"evidence_schema_versions,omitempty"`
	ArtifactObservationSchemaVersions []int                       `json:"artifact_observation_schema_versions,omitempty"`
	ReadinessRequirementKinds         []string                    `json:"project_readiness_requirement_kinds,omitempty"`
	TypedCommandVersions              []int                       `json:"typed_project_command_versions,omitempty"`
	TypedCommandManifestVersion       int                         `json:"typed_project_command_manifest_version,omitempty"`
	TypedCommandParameterKinds        []string                    `json:"typed_project_command_parameter_kinds,omitempty"`
	TypedCommandPackageProviders      []string                    `json:"typed_project_command_package_providers,omitempty"`
	ResourceObservation               *ResourceObservationSupport `json:"resource_observation,omitempty"`
	Features                          map[Feature]Availability    `json:"features"`
	Limits                            Limits                      `json:"limits"`
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
	FeatureProjectReadiness,
	FeatureTypedProjectCommands,
	FeatureStructuredResults,
	FeatureStructuredLifecycle,
	FeatureEventJournal,
	FeatureEventSnapshotRecovery,
	FeatureCodeIntelligence,
	FeatureExecutionTelemetry,
	FeatureReproductionCapsules,
}

func TargetFeatures() []Feature {
	return append([]Feature(nil), targetFeatures...)
}

func Baseline(limits Limits) Catalog {
	features := make(map[Feature]Availability, len(targetFeatures))
	for _, feature := range targetFeatures {
		features[feature] = Unavailable
	}
	features[FeatureWorkspaceAddressing] = Available
	features[FeatureWorkspaceProvenance] = Available
	features[FeatureActivities] = Available
	features[FeatureArgvMode] = Available
	features[FeatureProjectManifest] = Available
	return Catalog{
		ProtocolVersion:       2,
		ReceiptSchemaVersions: []int{1, 2},
		ManifestVersions:      []int{1, 2},
		Features:              features,
		Limits:                limits,
	}
}

func (c Catalog) Clone() Catalog {
	out := c
	out.ReceiptSchemaVersions = append([]int(nil), c.ReceiptSchemaVersions...)
	out.ManifestVersions = append([]int(nil), c.ManifestVersions...)
	out.EventCursorSchemaVersions = append([]int(nil), c.EventCursorSchemaVersions...)
	out.ResultCursorSchemaVersions = append([]int(nil), c.ResultCursorSchemaVersions...)
	out.StructuredAdapterIDs = append([]string(nil), c.StructuredAdapterIDs...)
	out.StructuredResultKinds = append([]string(nil), c.StructuredResultKinds...)
	out.TelemetrySchemaVersions = append([]int(nil), c.TelemetrySchemaVersions...)
	out.ReproSchemaVersions = append([]int(nil), c.ReproSchemaVersions...)
	out.ReadinessSchemaVersions = append([]int(nil), c.ReadinessSchemaVersions...)
	out.OutputViewSchemaVersions = append([]int(nil), c.OutputViewSchemaVersions...)
	out.EvidenceSchemaVersions = append([]int(nil), c.EvidenceSchemaVersions...)
	out.ArtifactObservationSchemaVersions = append([]int(nil), c.ArtifactObservationSchemaVersions...)
	out.ReadinessRequirementKinds = append([]string(nil), c.ReadinessRequirementKinds...)
	out.TypedCommandVersions = append([]int(nil), c.TypedCommandVersions...)
	out.TypedCommandParameterKinds = append([]string(nil), c.TypedCommandParameterKinds...)
	out.TypedCommandPackageProviders = append([]string(nil), c.TypedCommandPackageProviders...)
	if c.ResourceObservation != nil {
		resource := *c.ResourceObservation
		out.ResourceObservation = &resource
	}
	out.Features = make(map[Feature]Availability, len(c.Features))
	for feature, availability := range c.Features {
		out.Features[feature] = availability
	}
	return out
}

func (c Catalog) WithEvidence(maxRecords, maxOutputs, metadataBytes int, digestBytes int64, treeEntries, cursorBytes int) Catalog {
	out := c.Clone()
	if maxRecords < 1 || maxOutputs < 1 || metadataBytes < 1 || digestBytes < 1 || treeEntries < 1 || cursorBytes < 1 {
		return out
	}
	out.Features[FeatureEvidenceLedger] = Available
	out.Features[FeatureExpectedOutputs] = Available
	out.EvidenceSchemaVersions = []int{1}
	out.ArtifactObservationSchemaVersions = []int{1}
	out.Limits.EvidenceInspectRecords = maxRecords
	out.Limits.EvidenceExpectedOutputs = maxOutputs
	out.Limits.EvidenceArtifactMetadataBytes = metadataBytes
	out.Limits.EvidenceArtifactDigestBytes = digestBytes
	out.Limits.EvidenceTreeEntries = treeEntries
	out.Limits.EvidenceCursorBytes = cursorBytes
	return out
}

func (c Catalog) WithOutputViews(maxReturnBytes, maxWorkBytes, maxLines, maxMatches, maxPatternBytes, maxContinuationBytes int) Catalog {
	out := c.Clone()
	if maxReturnBytes < 1 || maxWorkBytes < 1 || maxLines < 1 || maxMatches < 1 || maxPatternBytes < 1 || maxContinuationBytes < 1 {
		return out
	}
	out.Features[FeatureOutputViews] = Available
	out.OutputViewSchemaVersions = []int{1}
	out.Limits.OutputViewMaxReturnBytes = maxReturnBytes
	out.Limits.OutputViewMaxWorkBytes = maxWorkBytes
	out.Limits.OutputViewMaxLines = maxLines
	out.Limits.OutputViewMaxMatches = maxMatches
	out.Limits.OutputViewMaxPatternBytes = maxPatternBytes
	out.Limits.OutputViewMaxContinuationBytes = maxContinuationBytes
	return out
}

func (c Catalog) WithEventJournal(maxEvents, cursorBytes, snapshotFacts int, snapshotRecovery bool) Catalog {
	out := c.Clone()
	if maxEvents <= 0 || cursorBytes <= 0 || snapshotFacts <= 0 {
		return out
	}
	out.Features[FeatureEventJournal] = Available
	out.EventCursorSchemaVersions = []int{1}
	out.Limits.EventJournalMaxEvents = maxEvents
	out.Limits.EventCursorBytes = cursorBytes
	out.Limits.EventSnapshotFacts = snapshotFacts
	if snapshotRecovery {
		out.Features[FeatureEventSnapshotRecovery] = Available
	}
	return out
}

func (c Catalog) WithStructuredResults(adapterIDs, resultKinds []string, maxRecords int, lifecycle bool) Catalog {
	out := c.Clone()
	if maxRecords < 1 || len(adapterIDs) == 0 || len(resultKinds) == 0 {
		return out
	}
	out.Features[FeatureStructuredResults] = Available
	if lifecycle {
		out.Features[FeatureStructuredLifecycle] = Available
	}
	out.ResultCursorSchemaVersions = []int{1}
	out.StructuredAdapterIDs = append([]string(nil), adapterIDs...)
	out.StructuredResultKinds = append([]string(nil), resultKinds...)
	out.StructuredLifecycle = lifecycle
	out.Limits.StructuredInspectRecords = maxRecords
	return out
}

func (c Catalog) WithCodeIntelligence() Catalog {
	out := c.Clone()
	out.Features[FeatureCodeIntelligence] = Available
	return out
}

func (c Catalog) WithExecutionTelemetry(maxSamples int, metadataBytes int64, maxKeys, maxKeysPerRepository, maxSamplesPerKey int, retentionAgeMS int64, inspectSamples int) Catalog {
	out := c.Clone()
	if maxSamples < 1 || metadataBytes < 1 || maxKeys < 1 || maxKeysPerRepository < 1 || maxSamplesPerKey < 1 || retentionAgeMS < 1 || inspectSamples < 1 {
		return out
	}
	out.Features[FeatureExecutionTelemetry] = Available
	out.TelemetrySchemaVersions = []int{1}
	out.Limits.TelemetryMaxSamples = maxSamples
	out.Limits.TelemetryMetadataBytes = metadataBytes
	out.Limits.TelemetryMaxKeys = maxKeys
	out.Limits.TelemetryMaxKeysPerRepository = maxKeysPerRepository
	out.Limits.TelemetryMaxSamplesPerKey = maxSamplesPerKey
	out.Limits.TelemetryRetentionAgeMS = retentionAgeMS
	out.Limits.TelemetryInspectSamples = inspectSamples
	out.ResourceObservation = &ResourceObservationSupport{
		CPUTime: ResourceUnavailable, MaxRSS: ResourceUnavailable, IOBytes: ResourceUnavailable, ProcessCountPeak: ResourceUnavailable,
	}
	return out
}

func (c Catalog) WithProjectReadiness(ttlMS int64, maxEntries int) Catalog {
	out := c.Clone()
	if ttlMS < 1 || maxEntries < 1 {
		return out
	}
	out.Features[FeatureProjectReadiness] = Available
	out.ReadinessSchemaVersions = []int{1}
	out.ReadinessRequirementKinds = []string{"toolchain", "executable", "environment_presence"}
	out.Limits.ReadinessCacheTTLMS = ttlMS
	out.Limits.ReadinessCacheEntries = maxEntries
	return out
}

func (c Catalog) WithTypedProjectCommands(packageProviders []string) Catalog {
	out := c.Clone()
	if len(packageProviders) == 0 {
		return out
	}
	for _, provider := range packageProviders {
		if provider == "" {
			return out
		}
	}
	out.Features[FeatureTypedProjectCommands] = Available
	out.TypedCommandVersions = []int{1}
	out.TypedCommandManifestVersion = 2
	out.TypedCommandParameterKinds = []string{"string", "enum", "integer", "repo_path", "repo_package"}
	out.TypedCommandPackageProviders = append([]string(nil), packageProviders...)
	foundV3 := false
	for _, version := range out.ReceiptSchemaVersions {
		foundV3 = foundV3 || version == 3
	}
	if !foundV3 {
		out.ReceiptSchemaVersions = append(out.ReceiptSchemaVersions, 3)
	}
	return out
}

func (c Catalog) WithReproductionCapsules(maxCapsules, maxReferences, metadataBytes int) Catalog {
	out := c.Clone()
	if maxCapsules < 1 || maxReferences < 1 || metadataBytes < 1 {
		return out
	}
	out.Features[FeatureReproductionCapsules] = Available
	out.ReproSchemaVersions = []int{1}
	out.Limits.ReproMaxCapsules = maxCapsules
	out.Limits.ReproMaxReferences = maxReferences
	out.Limits.ReproMetadataBytes = metadataBytes
	return out
}
