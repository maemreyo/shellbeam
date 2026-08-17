package mcp

import "github.com/maemreyo/shellbeam/internal/core/capability"

func legacyCatalogView(c capability.Catalog) capability.Catalog {
	out := c.Clone()
	out.EventCursorSchemaVersions = nil
	out.ResultCursorSchemaVersions = nil
	out.StructuredAdapterIDs = nil
	out.StructuredResultKinds = nil
	out.StructuredLifecycle = false
	out.TelemetrySchemaVersions = nil
	out.ReproSchemaVersions = nil
	out.ReadinessSchemaVersions = nil
	out.OutputViewSchemaVersions = nil
	out.ReadinessRequirementKinds = nil
	out.TypedCommandVersions = nil
	out.TypedCommandManifestVersion = 0
	out.TypedCommandParameterKinds = nil
	out.TypedCommandPackageProviders = nil
	filteredReceipts := out.ReceiptSchemaVersions[:0]
	for _, version := range out.ReceiptSchemaVersions {
		if version <= 2 {
			filteredReceipts = append(filteredReceipts, version)
		}
	}
	out.ReceiptSchemaVersions = filteredReceipts
	out.ResourceObservation = nil
	out.Limits.TelemetryMaxSamples = 0
	out.Limits.TelemetryMetadataBytes = 0
	out.Limits.TelemetryMaxKeys = 0
	out.Limits.TelemetryMaxKeysPerRepository = 0
	out.Limits.TelemetryMaxSamplesPerKey = 0
	out.Limits.TelemetryRetentionAgeMS = 0
	out.Limits.TelemetryInspectSamples = 0
	out.Limits.ReproMaxCapsules = 0
	out.Limits.ReproMaxReferences = 0
	out.Limits.ReproMetadataBytes = 0
	out.Limits.ReadinessCacheTTLMS = 0
	out.Limits.ReadinessCacheEntries = 0
	out.Limits.OutputViewMaxReturnBytes = 0
	out.Limits.OutputViewMaxWorkBytes = 0
	out.Limits.OutputViewMaxLines = 0
	out.Limits.OutputViewMaxMatches = 0
	out.Limits.OutputViewMaxPatternBytes = 0
	out.Limits.OutputViewMaxContinuationBytes = 0
	out.Limits.EventJournalMaxEvents = 0
	out.Limits.EventCursorBytes = 0
	out.Limits.EventSnapshotFacts = 0
	out.Limits.StructuredInspectRecords = 0
	delete(out.Features, capability.FeatureEventJournal)
	delete(out.Features, capability.FeatureEventSnapshotRecovery)
	delete(out.Features, capability.FeatureStructuredResults)
	delete(out.Features, capability.FeatureStructuredLifecycle)
	delete(out.Features, capability.FeatureCodeIntelligence)
	delete(out.Features, capability.FeatureExecutionTelemetry)
	delete(out.Features, capability.FeatureReproductionCapsules)
	delete(out.Features, capability.FeatureProjectReadiness)
	delete(out.Features, capability.FeatureTypedProjectCommands)
	delete(out.Features, capability.FeatureOutputViews)
	delete(out.Features, capability.FeatureMutationScopes)
	out.MutationScopeSchemaVersions = nil
	stripLegacyPersistentCapabilities(&out)
	out.Limits.MutationScopeActivePerActivity = 0
	out.Limits.MutationScopeActivePerWorkspace = 0
	out.Limits.MutationScopePathsPerScope = 0
	out.Limits.MutationScopeSelectorBytes = 0
	out.Limits.MutationScopeAdvisories = 0
	out.Limits.MutationScopeMinTTLMS = 0
	out.Limits.MutationScopeDefaultTTLMS = 0
	out.Limits.MutationScopeMaxTTLMS = 0
	return out
}

func stripLegacyPersistentCapabilities(out *capability.Catalog) {
	out.PersistentSessionSchemaVersions = nil
	out.SupervisorProtocolVersions = nil
	out.PersistentNonTTY = false
	out.PersistentTTY = false
	out.PersistentContinuity = ""
	out.HostRebootContinuity = false
	delete(out.Features, capability.FeatureNamedSessions)
	out.Limits.PersistentSessions = 0
	out.Limits.PersistentSessionNameBytes = 0
	out.Limits.PersistentSessionInspectRows = 0
	out.Limits.PersistentSessionInspectDefaultRows = 0
	out.Limits.PersistentInputRecords = 0
	out.Limits.PersistentInputRecordMetadataBytes = 0
	out.Limits.PersistentKillRecords = 0
	out.Limits.PersistentRecoverySpoolBytes = 0
	out.Limits.PersistentQueuedInputBytes = 0
	out.Limits.PersistentReattachHandshakeTimeoutMS = 0
	out.Limits.PersistentStartupReattachConcurrency = 0
	out.Limits.PersistentStartupReattachBudgetMS = 0
}
