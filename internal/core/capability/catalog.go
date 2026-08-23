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
	FeatureSafetyCheckpoints      Feature = "safety_checkpoints"
	FeatureRichLocalMedia         Feature = "rich_local_media"
	FeatureInputTracing           Feature = "input_tracing"
	FeatureResourceEnforcement    Feature = "resource_enforcement"
	FeatureDelegatedInteractive   Feature = "delegated_interactive"
	FeatureInteractiveHandoff     Feature = "interactive_handoff"
	FeatureContextExec            Feature = "context_exec"
	FeatureHermeticBoundaryV1     Feature = "hermetic_boundary_v1"
	FeatureVerificationSemantics  Feature = "verification_semantics"
)

type Limits struct {
	CommandBytes                         int   `json:"command_bytes"`
	ResponseBytes                        int   `json:"response_bytes"`
	SessionOutputBytes                   int64 `json:"session_output_bytes"`
	RuntimeMS                            int64 `json:"runtime_ms"`
	LiveSessions                         int   `json:"live_sessions"`
	ActivityHistory                      int   `json:"activity_history"`
	EventJournalMaxEvents                int   `json:"event_journal_max_events,omitempty"`
	EventCursorBytes                     int   `json:"event_cursor_bytes,omitempty"`
	EventSnapshotFacts                   int   `json:"event_snapshot_facts,omitempty"`
	StructuredInspectRecords             int   `json:"structured_inspect_records,omitempty"`
	StructuredArtifactBlobBytes          int64 `json:"structured_artifact_blob_bytes,omitempty"`
	StructuredPinnedArtifactHandles      int   `json:"structured_pinned_artifact_handles,omitempty"`
	StructuredMaterializationQueueDepth  int   `json:"structured_materialization_queue_depth,omitempty"`
	StructuredTerminalAcquireMS          int64 `json:"structured_terminal_acquire_ms,omitempty"`
	TelemetryMaxSamples                  int   `json:"telemetry_max_samples,omitempty"`
	TelemetryMetadataBytes               int64 `json:"telemetry_metadata_bytes,omitempty"`
	TelemetryMaxKeys                     int   `json:"telemetry_max_keys,omitempty"`
	TelemetryMaxKeysPerRepository        int   `json:"telemetry_max_keys_per_repository,omitempty"`
	TelemetryMaxSamplesPerKey            int   `json:"telemetry_max_samples_per_key,omitempty"`
	TelemetryRetentionAgeMS              int64 `json:"telemetry_retention_age_ms,omitempty"`
	TelemetryInspectSamples              int   `json:"telemetry_inspect_samples,omitempty"`
	ReproMaxCapsules                     int   `json:"repro_max_capsules,omitempty"`
	ReproMaxReferences                   int   `json:"repro_max_references,omitempty"`
	ReproMetadataBytes                   int   `json:"repro_metadata_bytes,omitempty"`
	ReadinessCacheTTLMS                  int64 `json:"readiness_cache_ttl_ms,omitempty"`
	ReadinessCacheEntries                int   `json:"readiness_cache_entries,omitempty"`
	OutputViewMaxReturnBytes             int   `json:"output_view_max_return_bytes,omitempty"`
	OutputViewMaxWorkBytes               int   `json:"output_view_max_work_bytes,omitempty"`
	OutputViewMaxLines                   int   `json:"output_view_max_lines,omitempty"`
	OutputViewMaxMatches                 int   `json:"output_view_max_matches,omitempty"`
	OutputViewMaxPatternBytes            int   `json:"output_view_max_pattern_bytes,omitempty"`
	OutputViewMaxContinuationBytes       int   `json:"output_view_max_continuation_bytes,omitempty"`
	EvidenceInspectRecords               int   `json:"evidence_inspect_records,omitempty"`
	EvidenceExpectedOutputs              int   `json:"evidence_expected_outputs,omitempty"`
	EvidenceArtifactMetadataBytes        int   `json:"evidence_artifact_metadata_bytes,omitempty"`
	EvidenceArtifactDigestBytes          int64 `json:"evidence_artifact_digest_bytes,omitempty"`
	EvidenceTreeEntries                  int   `json:"evidence_tree_entries,omitempty"`
	EvidenceCursorBytes                  int   `json:"evidence_cursor_bytes,omitempty"`
	EnvironmentRelevantVariables         int   `json:"environment_relevant_variables,omitempty"`
	EnvironmentToolchainProbes           int   `json:"environment_toolchain_probes,omitempty"`
	EnvironmentToolchainObservations     int   `json:"environment_toolchain_observations,omitempty"`
	EnvironmentProbeTimeoutMS            int64 `json:"environment_probe_timeout_ms,omitempty"`
	EnvironmentProbeOutputBytes          int   `json:"environment_probe_output_bytes,omitempty"`
	EnvironmentCacheEntries              int   `json:"environment_cache_entries,omitempty"`
	ProcessDescendants                   int   `json:"process_descendants,omitempty"`
	ProcessTraversalDepth                int   `json:"process_traversal_depth,omitempty"`
	ProcessObservationBytes              int   `json:"process_observation_bytes,omitempty"`
	ProcessObservationMS                 int64 `json:"process_observation_ms,omitempty"`
	ProcessPortRecords                   int   `json:"process_port_records,omitempty"`
	MutationScopeActivePerActivity       int   `json:"mutation_scope_active_per_activity,omitempty"`
	MutationScopeActivePerWorkspace      int   `json:"mutation_scope_active_per_workspace,omitempty"`
	MutationScopePathsPerScope           int   `json:"mutation_scope_paths_per_scope,omitempty"`
	MutationScopeSelectorBytes           int   `json:"mutation_scope_selector_bytes,omitempty"`
	MutationScopeAdvisories              int   `json:"mutation_scope_advisories,omitempty"`
	MutationScopeMinTTLMS                int64 `json:"mutation_scope_min_ttl_ms,omitempty"`
	MutationScopeDefaultTTLMS            int64 `json:"mutation_scope_default_ttl_ms,omitempty"`
	MutationScopeMaxTTLMS                int64 `json:"mutation_scope_max_ttl_ms,omitempty"`
	PersistentSessions                   int   `json:"persistent_sessions,omitempty"`
	PersistentSessionNameBytes           int   `json:"persistent_session_name_bytes,omitempty"`
	PersistentSessionInspectRows         int   `json:"persistent_session_inspect_rows,omitempty"`
	PersistentSessionInspectDefaultRows  int   `json:"persistent_session_inspect_default_rows,omitempty"`
	PersistentInputRecords               int   `json:"persistent_input_records,omitempty"`
	PersistentInputRecordMetadataBytes   int   `json:"persistent_input_record_metadata_bytes,omitempty"`
	PersistentKillRecords                int   `json:"persistent_kill_records,omitempty"`
	PersistentRecoverySpoolBytes         int64 `json:"persistent_recovery_spool_bytes,omitempty"`
	PersistentQueuedInputBytes           int   `json:"persistent_queued_input_bytes,omitempty"`
	PersistentReattachHandshakeTimeoutMS int64 `json:"persistent_reattach_handshake_timeout_ms,omitempty"`
	PersistentStartupReattachConcurrency int   `json:"persistent_startup_reattach_concurrency,omitempty"`
	PersistentStartupReattachBudgetMS    int64 `json:"persistent_startup_reattach_budget_ms,omitempty"`
	CheckpointCreateSelectors            int   `json:"checkpoint_create_selectors,omitempty"`
	CheckpointSelectorBytes              int   `json:"checkpoint_selector_bytes,omitempty"`
	CheckpointTotalSelectorBytes         int   `json:"checkpoint_total_selector_bytes,omitempty"`
	CheckpointWalkEntries                int   `json:"checkpoint_walk_entries,omitempty"`
	CheckpointCapturedEntries            int   `json:"checkpoint_captured_entries,omitempty"`
	CheckpointRegularFileBytes           int64 `json:"checkpoint_regular_file_bytes,omitempty"`
	CheckpointBytes                      int64 `json:"checkpoint_bytes,omitempty"`
	CheckpointRetained                   int   `json:"checkpoint_retained,omitempty"`
	CheckpointPrivateProviderBytes       int64 `json:"checkpoint_private_provider_bytes,omitempty"`
	CheckpointRetentionAgeMS             int64 `json:"checkpoint_retention_age_ms,omitempty"`
	CheckpointRestorePaths               int   `json:"checkpoint_restore_paths,omitempty"`
	CheckpointPublicEntryRefs            int   `json:"checkpoint_public_entry_refs,omitempty"`
	CheckpointPublicSummaries            int   `json:"checkpoint_public_summaries,omitempty"`
	InputTraceRawEvents                  int   `json:"input_trace_raw_events,omitempty"`
	InputTraceUniqueResources            int   `json:"input_trace_unique_resources,omitempty"`
	InputTracePublicResources            int   `json:"input_trace_public_resources,omitempty"`
	InputTraceExternalResources          int   `json:"input_trace_external_resources,omitempty"`
	InputTraceRawEventBytes              int   `json:"input_trace_raw_event_bytes,omitempty"`
	InputTracePrivateRawBytes            int   `json:"input_trace_private_raw_bytes,omitempty"`
	InputTracePublicRecordBytes          int   `json:"input_trace_public_record_bytes,omitempty"`
	InputTraceRetainedRecords            int   `json:"input_trace_retained_records,omitempty"`
	InputTraceCaptureDurationMS          int64 `json:"input_trace_capture_duration_ms,omitempty"`
	InputTraceStartupBudgetMS            int64 `json:"input_trace_startup_budget_ms,omitempty"`
	InputTraceWorkerQueueDepth           int   `json:"input_trace_worker_queue_depth,omitempty"`
}

type DelegatedInteractiveSupport struct {
	ProviderID              string `json:"provider_id"`
	ProviderVersion         int    `json:"provider_version"`
	Platform                string `json:"platform"`
	MaxMutationRecords      int    `json:"max_mutation_records"`
	DaemonRestartContinuity bool   `json:"daemon_restart_continuity"`
	HostRebootContinuity    bool   `json:"host_reboot_continuity"`
}

type Catalog struct {
	ProtocolVersion                   int                           `json:"shellbeam_protocol_version"`
	ReceiptSchemaVersions             []int                         `json:"receipt_schema_versions"`
	ManifestVersions                  []int                         `json:"project_manifest_schema_versions"`
	EventCursorSchemaVersions         []int                         `json:"event_cursor_schema_versions,omitempty"`
	ResultCursorSchemaVersions        []int                         `json:"result_cursor_schema_versions,omitempty"`
	StructuredSchemaVersions          []int                         `json:"structured_schema_versions,omitempty"`
	StructuredAdapterIDs              []string                      `json:"structured_adapter_ids,omitempty"`
	StructuredResultKinds             []string                      `json:"structured_result_kinds,omitempty"`
	StructuredInputKinds              []string                      `json:"structured_input_kinds,omitempty"`
	StructuredLifecycle               bool                          `json:"structured_lifecycle,omitempty"`
	TelemetrySchemaVersions           []int                         `json:"telemetry_schema_versions,omitempty"`
	ReproSchemaVersions               []int                         `json:"repro_schema_versions,omitempty"`
	ReadinessSchemaVersions           []int                         `json:"project_readiness_schema_versions,omitempty"`
	OutputViewSchemaVersions          []int                         `json:"output_view_schema_versions,omitempty"`
	EvidenceSchemaVersions            []int                         `json:"evidence_schema_versions,omitempty"`
	ArtifactObservationSchemaVersions []int                         `json:"artifact_observation_schema_versions,omitempty"`
	EnvironmentSnapshotSchemaVersions []int                         `json:"environment_snapshot_schema_versions,omitempty"`
	EnvironmentFingerprintVersions    []int                         `json:"environment_fingerprint_versions,omitempty"`
	ToolchainFingerprintVersions      []int                         `json:"toolchain_fingerprint_versions,omitempty"`
	EnvironmentToolchainProbeIDs      []string                      `json:"environment_toolchain_probe_ids,omitempty"`
	ProcessObservationSchemaVersions  []int                         `json:"process_observation_schema_versions,omitempty"`
	MutationScopeSchemaVersions       []int                         `json:"mutation_scope_schema_versions,omitempty"`
	PersistentSessionSchemaVersions   []int                         `json:"persistent_session_schema_versions,omitempty"`
	SupervisorProtocolVersions        []int                         `json:"supervisor_protocol_versions,omitempty"`
	PersistentNonTTY                  bool                          `json:"persistent_non_tty,omitempty"`
	PersistentTTY                     bool                          `json:"persistent_tty,omitempty"`
	PersistentContinuity              string                        `json:"persistent_continuity,omitempty"`
	HostRebootContinuity              bool                          `json:"host_reboot_continuity,omitempty"`
	PortObservationSupported          bool                          `json:"port_observation_supported,omitempty"`
	ReadinessRequirementKinds         []string                      `json:"project_readiness_requirement_kinds,omitempty"`
	TypedCommandVersions              []int                         `json:"typed_project_command_versions,omitempty"`
	TypedCommandManifestVersion       int                           `json:"typed_project_command_manifest_version,omitempty"`
	TypedCommandParameterKinds        []string                      `json:"typed_project_command_parameter_kinds,omitempty"`
	TypedCommandPackageProviders      []string                      `json:"typed_project_command_package_providers,omitempty"`
	ResourceObservation               *ResourceObservationSupport   `json:"resource_observation,omitempty"`
	ResourceEnforcement               *ResourceEnforcementSupport   `json:"resource_enforcement,omitempty"`
	HermeticBoundary                  *HermeticBoundarySupport      `json:"hermetic_boundary,omitempty"`
	VerificationSemantics             *VerificationSemanticsSupport `json:"verification_semantics,omitempty"`
	DecisionProtocol                  *DecisionProtocolSupport      `json:"decision_protocol,omitempty"`
	SafetyCheckpoints                 *CheckpointSupport            `json:"safety_checkpoints,omitempty"`
	Media                             *MediaSupport                 `json:"media,omitempty"`
	InputTracing                      *InputTracingSupport          `json:"input_tracing,omitempty"`
	DelegatedInteractive              *DelegatedInteractiveSupport  `json:"delegated_interactive,omitempty"`
	InteractiveHandoff                *InteractiveHandoffSupport    `json:"interactive_handoff,omitempty"`
	ContextExec                       *ContextExecSupport           `json:"context_exec,omitempty"`
	Runtime                           *RuntimeIdentity              `json:"runtime,omitempty"`
	Features                          map[Feature]Availability      `json:"features"`
	Limits                            Limits                        `json:"limits"`
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
	FeatureSafetyCheckpoints,
	FeatureRichLocalMedia,
	FeatureInputTracing,
	FeatureResourceEnforcement,
	FeatureDelegatedInteractive,
	FeatureInteractiveHandoff,
	FeatureContextExec,
	FeatureHermeticBoundaryV1,
	FeatureVerificationSemantics,
	FeatureDecisionProtocol,
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
	out.StructuredSchemaVersions = append([]int(nil), c.StructuredSchemaVersions...)
	out.StructuredAdapterIDs = append([]string(nil), c.StructuredAdapterIDs...)
	out.StructuredResultKinds = append([]string(nil), c.StructuredResultKinds...)
	out.StructuredInputKinds = append([]string(nil), c.StructuredInputKinds...)
	out.TelemetrySchemaVersions = append([]int(nil), c.TelemetrySchemaVersions...)
	out.ReproSchemaVersions = append([]int(nil), c.ReproSchemaVersions...)
	out.ReadinessSchemaVersions = append([]int(nil), c.ReadinessSchemaVersions...)
	out.OutputViewSchemaVersions = append([]int(nil), c.OutputViewSchemaVersions...)
	out.EvidenceSchemaVersions = append([]int(nil), c.EvidenceSchemaVersions...)
	out.ArtifactObservationSchemaVersions = append([]int(nil), c.ArtifactObservationSchemaVersions...)
	out.EnvironmentSnapshotSchemaVersions = append([]int(nil), c.EnvironmentSnapshotSchemaVersions...)
	out.EnvironmentFingerprintVersions = append([]int(nil), c.EnvironmentFingerprintVersions...)
	out.ToolchainFingerprintVersions = append([]int(nil), c.ToolchainFingerprintVersions...)
	out.EnvironmentToolchainProbeIDs = append([]string(nil), c.EnvironmentToolchainProbeIDs...)
	out.ProcessObservationSchemaVersions = append([]int(nil), c.ProcessObservationSchemaVersions...)
	out.MutationScopeSchemaVersions = append([]int(nil), c.MutationScopeSchemaVersions...)
	out.PersistentSessionSchemaVersions = append([]int(nil), c.PersistentSessionSchemaVersions...)
	out.SupervisorProtocolVersions = append([]int(nil), c.SupervisorProtocolVersions...)
	out.ReadinessRequirementKinds = append([]string(nil), c.ReadinessRequirementKinds...)
	out.TypedCommandVersions = append([]int(nil), c.TypedCommandVersions...)
	out.TypedCommandParameterKinds = append([]string(nil), c.TypedCommandParameterKinds...)
	out.TypedCommandPackageProviders = append([]string(nil), c.TypedCommandPackageProviders...)
	cloneCatalogSupports(&out, c)
	out.Features = make(map[Feature]Availability, len(c.Features))
	for feature, availability := range c.Features {
		out.Features[feature] = availability
	}
	return out
}

func cloneCatalogSupports(out *Catalog, c Catalog) {
	if c.ResourceObservation != nil {
		resource := *c.ResourceObservation
		out.ResourceObservation = &resource
	}
	if c.ResourceEnforcement != nil {
		enforcement := *c.ResourceEnforcement
		out.ResourceEnforcement = &enforcement
	}
	if c.HermeticBoundary != nil {
		hermetic := *c.HermeticBoundary
		out.HermeticBoundary = &hermetic
	}
	if c.VerificationSemantics != nil {
		verification := c.VerificationSemantics.clone()
		out.VerificationSemantics = &verification
	}
	out.DecisionProtocol = cloneDecisionProtocolSupport(c.DecisionProtocol)
	if c.SafetyCheckpoints != nil {
		support := *c.SafetyCheckpoints
		support.SchemaVersions = append([]int(nil), c.SafetyCheckpoints.SchemaVersions...)
		out.SafetyCheckpoints = &support
	}
	if c.Media != nil {
		support := c.Media.Clone()
		out.Media = &support
	}
	if c.InputTracing != nil {
		support := *c.InputTracing
		support.SchemaVersions = append([]int(nil), c.InputTracing.SchemaVersions...)
		out.InputTracing = &support
	}
	if c.DelegatedInteractive != nil {
		support := *c.DelegatedInteractive
		out.DelegatedInteractive = &support
	}
	if c.InteractiveHandoff != nil {
		support := c.InteractiveHandoff.Clone()
		out.InteractiveHandoff = &support
	}
	if c.ContextExec != nil {
		support := c.ContextExec.Clone()
		out.ContextExec = &support
	}
	if c.Runtime != nil {
		runtime := c.Runtime.Clone()
		out.Runtime = &runtime
	}
}

func (c Catalog) WithRuntime(identity RuntimeIdentity) Catalog {
	out := c.Clone()
	if err := identity.Validate(); err != nil {
		return out
	}
	cloned := identity.Clone()
	out.Runtime = &cloned
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
	out.StructuredSchemaVersions = []int{1}
	out.StructuredAdapterIDs = append([]string(nil), adapterIDs...)
	out.StructuredResultKinds = append([]string(nil), resultKinds...)
	out.StructuredLifecycle = lifecycle
	out.Limits.StructuredInspectRecords = maxRecords
	return out
}

func (c Catalog) WithStructuredArtifactInputs(maxBlobBytes int64, pinnedHandles, queueDepth int, terminalAcquireMS int64) Catalog {
	out := c.Clone()
	if out.Features[FeatureStructuredResults] != Available || maxBlobBytes < 1 || pinnedHandles < 1 || queueDepth < 1 || queueDepth > pinnedHandles || terminalAcquireMS < 1 {
		return out
	}
	out.StructuredSchemaVersions = []int{1, 2, 3}
	out.StructuredInputKinds = []string{"raw_output", "artifact_blob"}
	out.Limits.StructuredArtifactBlobBytes = maxBlobBytes
	out.Limits.StructuredPinnedArtifactHandles = pinnedHandles
	out.Limits.StructuredMaterializationQueueDepth = queueDepth
	out.Limits.StructuredTerminalAcquireMS = terminalAcquireMS
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
		CPUTime: ResourcePlatformReported, MaxRSS: ResourcePlatformReported, IOBytes: ResourceUnavailable, ProcessCountPeak: ResourceSampled,
	}
	return out
}

func (c Catalog) WithResourceEnforcement(support ResourceEnforcementSupport) Catalog {
	out := c.Clone()
	if !support.ValidV1() {
		return out
	}
	out.Features[FeatureResourceEnforcement] = Available
	copy := support
	out.ResourceEnforcement = &copy
	return out
}

func (c Catalog) WithHermeticBoundary(support HermeticBoundarySupport) Catalog {
	out := c.Clone()
	if !support.ValidV1() {
		return out
	}
	out.Features[FeatureHermeticBoundaryV1] = Available
	copy := support
	out.HermeticBoundary = &copy
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
