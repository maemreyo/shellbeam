package capability

import (
	"reflect"
	"testing"
)

func TestCatalogBaselineIsHonestAboutCurrentSupport(t *testing.T) {
	limits := Limits{
		CommandBytes:       32768,
		ResponseBytes:      262144,
		SessionOutputBytes: 268435456,
		RuntimeMS:          86400000,
		LiveSessions:       4,
		ActivityHistory:    0,
	}
	catalog := Baseline(limits)
	if catalog.ProtocolVersion != 2 {
		t.Fatalf("protocol=%d", catalog.ProtocolVersion)
	}
	if !reflect.DeepEqual(catalog.ReceiptSchemaVersions, []int{1, 2}) {
		t.Fatalf("receipt versions=%v", catalog.ReceiptSchemaVersions)
	}
	if !reflect.DeepEqual(catalog.ManifestVersions, []int{1, 2}) {
		t.Fatalf("manifest versions=%v", catalog.ManifestVersions)
	}
	if !reflect.DeepEqual(catalog.Limits, limits) {
		t.Fatalf("limits=%#v want %#v", catalog.Limits, limits)
	}
	for _, feature := range TargetFeatures() {
		want := Unavailable
		if feature == FeatureWorkspaceAddressing || feature == FeatureWorkspaceProvenance || feature == FeatureActivities ||
			feature == FeatureArgvMode || feature == FeatureProjectManifest {
			want = Available
		}
		if got := catalog.Features[feature]; got != want {
			t.Fatalf("feature %s advertised as %s want %s", feature, got, want)
		}
	}
}

func TestCatalogBaselineDoesNotAliasFeatureMaps(t *testing.T) {
	first := Baseline(Limits{})
	second := Baseline(Limits{})
	first.Features[FeatureWorkspaceAddressing] = Unavailable
	if second.Features[FeatureWorkspaceAddressing] != Available {
		t.Fatal("baseline catalogs share mutable feature map")
	}
}

func TestEventJournalCapabilityIsOnlyAdvertisedWhenComposed(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureEventJournal] != Unavailable || base.Features[FeatureEventSnapshotRecovery] != Unavailable || len(base.EventCursorSchemaVersions) != 0 {
		t.Fatalf("baseline event capability=%#v", base)
	}
	got := base.WithEventJournal(256, 2048, 64, true)
	if got.Features[FeatureEventJournal] != Available || got.Features[FeatureEventSnapshotRecovery] != Available {
		t.Fatalf("features=%#v", got.Features)
	}
	if !reflect.DeepEqual(got.EventCursorSchemaVersions, []int{1}) || got.Limits.EventJournalMaxEvents != 256 || got.Limits.EventCursorBytes != 2048 || got.Limits.EventSnapshotFacts != 64 {
		t.Fatalf("catalog=%#v", got)
	}
	if base.Features[FeatureEventJournal] != Unavailable || len(base.EventCursorSchemaVersions) != 0 {
		t.Fatal("WithEventJournal mutated baseline catalog")
	}
}

func TestStructuredResultsCapabilityIsExplicitAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureStructuredResults] != Unavailable || base.Features[FeatureStructuredLifecycle] != Unavailable || len(base.ResultCursorSchemaVersions) != 0 || len(base.StructuredAdapterIDs) != 0 || len(base.StructuredResultKinds) != 0 || base.StructuredLifecycle || base.Limits.StructuredInspectRecords != 0 {
		t.Fatalf("baseline leaked structured support: %#v", base)
	}
	got := base.WithStructuredResults([]string{"go-test-json", "go-vet-json"}, []string{"diagnostic", "test_case", "test_suite", "artifact_result"}, 128, true)
	if got.Features[FeatureStructuredResults] != Available || got.Features[FeatureStructuredLifecycle] != Available || !got.StructuredLifecycle || got.Limits.StructuredInspectRecords != 128 || !reflect.DeepEqual(got.ResultCursorSchemaVersions, []int{1}) || !reflect.DeepEqual(got.StructuredAdapterIDs, []string{"go-test-json", "go-vet-json"}) {
		t.Fatalf("catalog=%#v", got)
	}
	got.StructuredAdapterIDs[0] = "changed"
	if base.WithStructuredResults([]string{"go-test-json"}, []string{"diagnostic"}, 10, true).StructuredAdapterIDs[0] != "go-test-json" {
		t.Fatal("structured catalog aliases mutable adapter slice")
	}
}

func TestCodeIntelligenceCapabilityRequiresComposition(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureCodeIntelligence] != Unavailable {
		t.Fatalf("baseline advertised code intelligence: %#v", base.Features)
	}
	got := base.WithCodeIntelligence()
	if got.Features[FeatureCodeIntelligence] != Available {
		t.Fatalf("composed code intelligence unavailable: %#v", got.Features)
	}
	if base.Features[FeatureCodeIntelligence] != Unavailable {
		t.Fatal("WithCodeIntelligence mutated baseline")
	}
}

func TestExecutionTelemetryAndReproCapabilitiesAreExplicitAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureExecutionTelemetry] != Unavailable || base.Features[FeatureReproductionCapsules] != Unavailable || len(base.TelemetrySchemaVersions) != 0 || len(base.ReproSchemaVersions) != 0 || base.ResourceObservation != nil {
		t.Fatalf("baseline leaked A4 support: %#v", base)
	}
	telemetry := base.WithExecutionTelemetry(1024, 8<<20, 256, 64, 64, 7*24*60*60*1000, 128)
	if telemetry.Features[FeatureExecutionTelemetry] != Available || !reflect.DeepEqual(telemetry.TelemetrySchemaVersions, []int{1}) {
		t.Fatalf("telemetry capability=%#v", telemetry)
	}
	if telemetry.Limits.TelemetryMaxSamples != 1024 || telemetry.Limits.TelemetryMetadataBytes != 8<<20 || telemetry.Limits.TelemetryMaxKeys != 256 || telemetry.Limits.TelemetryMaxKeysPerRepository != 64 || telemetry.Limits.TelemetryMaxSamplesPerKey != 64 || telemetry.Limits.TelemetryRetentionAgeMS != 7*24*60*60*1000 || telemetry.Limits.TelemetryInspectSamples != 128 {
		t.Fatalf("telemetry limits=%#v", telemetry.Limits)
	}
	if telemetry.ResourceObservation == nil || telemetry.ResourceObservation.CPUTime != ResourceUnavailable || telemetry.ResourceObservation.MaxRSS != ResourceUnavailable || telemetry.ResourceObservation.IOBytes != ResourceUnavailable || telemetry.ResourceObservation.ProcessCountPeak != ResourceUnavailable {
		t.Fatalf("resource support overclaimed: %#v", telemetry.ResourceObservation)
	}
	repro := telemetry.WithReproductionCapsules(256, 32, 65536)
	if repro.Features[FeatureReproductionCapsules] != Available || !reflect.DeepEqual(repro.ReproSchemaVersions, []int{1}) || repro.Limits.ReproMaxCapsules != 256 || repro.Limits.ReproMaxReferences != 32 || repro.Limits.ReproMetadataBytes != 65536 {
		t.Fatalf("repro capability=%#v", repro)
	}
	if base.Features[FeatureExecutionTelemetry] != Unavailable || base.ResourceObservation != nil {
		t.Fatal("A4 composition mutated baseline")
	}
}

func TestA4CapabilityRejectsNonPositiveBounds(t *testing.T) {
	base := Baseline(Limits{})
	if got := base.WithExecutionTelemetry(0, 1, 1, 1, 1, 1, 1); got.Features[FeatureExecutionTelemetry] != Unavailable {
		t.Fatalf("invalid telemetry bounds advertised: %#v", got)
	}
	if got := base.WithReproductionCapsules(1, 0, 1); got.Features[FeatureReproductionCapsules] != Unavailable {
		t.Fatalf("invalid repro bounds advertised: %#v", got)
	}
}

func TestProjectReadinessCapabilityIsExplicitAndCloned(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureProjectReadiness] != Unavailable {
		t.Fatalf("baseline readiness=%q", base.Features[FeatureProjectReadiness])
	}
	catalog := base.WithProjectReadiness(30000, 256)
	if catalog.Features[FeatureProjectReadiness] != Available {
		t.Fatalf("readiness=%q", catalog.Features[FeatureProjectReadiness])
	}
	if !reflect.DeepEqual(catalog.ReadinessSchemaVersions, []int{1}) {
		t.Fatalf("schema versions=%v", catalog.ReadinessSchemaVersions)
	}
	if !reflect.DeepEqual(catalog.ReadinessRequirementKinds, []string{"toolchain", "executable", "environment_presence"}) {
		t.Fatalf("requirement kinds=%v", catalog.ReadinessRequirementKinds)
	}
	if catalog.Limits.ReadinessCacheTTLMS != 30000 || catalog.Limits.ReadinessCacheEntries != 256 {
		t.Fatalf("limits=%#v", catalog.Limits)
	}
	clone := catalog.Clone()
	clone.ReadinessSchemaVersions[0] = 99
	clone.ReadinessRequirementKinds[0] = "mutated"
	if catalog.ReadinessSchemaVersions[0] != 1 || catalog.ReadinessRequirementKinds[0] != "toolchain" {
		t.Fatalf("clone aliased readiness slices: %#v", catalog)
	}
}

func TestTypedProjectCommandCapabilityIsExplicitVersionedAndCloned(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureTypedProjectCommands] != Unavailable || len(base.TypedCommandVersions) != 0 || base.TypedCommandManifestVersion != 0 || len(base.TypedCommandParameterKinds) != 0 || len(base.TypedCommandPackageProviders) != 0 {
		t.Fatalf("baseline leaked typed command support: %#v", base)
	}
	catalog := base.WithTypedProjectCommands([]string{"go"})
	if catalog.Features[FeatureTypedProjectCommands] != Available {
		t.Fatalf("feature=%q", catalog.Features[FeatureTypedProjectCommands])
	}
	if !reflect.DeepEqual(catalog.TypedCommandVersions, []int{1}) || catalog.TypedCommandManifestVersion != 2 {
		t.Fatalf("versions=%v manifest=%d", catalog.TypedCommandVersions, catalog.TypedCommandManifestVersion)
	}
	if !reflect.DeepEqual(catalog.TypedCommandParameterKinds, []string{"string", "enum", "integer", "repo_path", "repo_package"}) || !reflect.DeepEqual(catalog.TypedCommandPackageProviders, []string{"go"}) {
		t.Fatalf("typed capability=%#v", catalog)
	}
	if !reflect.DeepEqual(catalog.ReceiptSchemaVersions, []int{1, 2, 3}) {
		t.Fatalf("receipt versions=%v", catalog.ReceiptSchemaVersions)
	}
	clone := catalog.Clone()
	clone.TypedCommandVersions[0] = 99
	clone.TypedCommandParameterKinds[0] = "changed"
	clone.TypedCommandPackageProviders[0] = "changed"
	if catalog.TypedCommandVersions[0] != 1 || catalog.TypedCommandParameterKinds[0] != "string" || catalog.TypedCommandPackageProviders[0] != "go" {
		t.Fatalf("clone aliases typed command slices: %#v", catalog)
	}
}

func TestTypedProjectCommandCapabilityRequiresProviderAndDoesNotMutateBaseline(t *testing.T) {
	base := Baseline(Limits{})
	if got := base.WithTypedProjectCommands(nil); got.Features[FeatureTypedProjectCommands] != Unavailable {
		t.Fatalf("providerless typed command advertised: %#v", got)
	}
	_ = base.WithTypedProjectCommands([]string{"go"})
	if base.Features[FeatureTypedProjectCommands] != Unavailable || !reflect.DeepEqual(base.ReceiptSchemaVersions, []int{1, 2}) {
		t.Fatalf("composition mutated baseline: %#v", base)
	}
}

func TestOutputViewsCapabilityRequiresPositiveLimits(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureOutputViews] != Unavailable || len(base.OutputViewSchemaVersions) != 0 {
		t.Fatalf("baseline unexpectedly advertises output views: %#v", base)
	}
	catalog := base.WithOutputViews(65536, 1<<20, 512, 128, 4096, 2048)
	if catalog.Features[FeatureOutputViews] != Available {
		t.Fatalf("output views=%q", catalog.Features[FeatureOutputViews])
	}
	if len(catalog.OutputViewSchemaVersions) != 1 || catalog.OutputViewSchemaVersions[0] != 1 {
		t.Fatalf("schema versions=%v", catalog.OutputViewSchemaVersions)
	}
	if catalog.Limits.OutputViewMaxReturnBytes != 65536 || catalog.Limits.OutputViewMaxWorkBytes != 1<<20 || catalog.Limits.OutputViewMaxLines != 512 || catalog.Limits.OutputViewMaxMatches != 128 || catalog.Limits.OutputViewMaxPatternBytes != 4096 || catalog.Limits.OutputViewMaxContinuationBytes != 2048 {
		t.Fatalf("limits=%#v", catalog.Limits)
	}
	bad := base.WithOutputViews(0, 1, 1, 1, 1, 1)
	if bad.Features[FeatureOutputViews] != Unavailable {
		t.Fatal("invalid limits promoted output views")
	}
}

func TestEvidenceCapabilitiesRequireCompletePositiveLimits(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureEvidenceLedger] != Unavailable || base.Features[FeatureExpectedOutputs] != Unavailable || len(base.EvidenceSchemaVersions) != 0 {
		t.Fatalf("baseline unexpectedly advertises evidence: %#v", base)
	}
	catalog := base.WithEvidence(128, 64, 32768, 64<<20, 4096, 2048)
	if catalog.Features[FeatureEvidenceLedger] != Available || catalog.Features[FeatureExpectedOutputs] != Available {
		t.Fatalf("features=%#v", catalog.Features)
	}
	if len(catalog.EvidenceSchemaVersions) != 1 || catalog.EvidenceSchemaVersions[0] != 1 || len(catalog.ArtifactObservationSchemaVersions) != 1 || catalog.ArtifactObservationSchemaVersions[0] != 1 {
		t.Fatalf("schema versions evidence=%v artifact=%v", catalog.EvidenceSchemaVersions, catalog.ArtifactObservationSchemaVersions)
	}
	limits := catalog.Limits
	if limits.EvidenceInspectRecords != 128 || limits.EvidenceExpectedOutputs != 64 || limits.EvidenceArtifactMetadataBytes != 32768 || limits.EvidenceArtifactDigestBytes != 64<<20 || limits.EvidenceTreeEntries != 4096 || limits.EvidenceCursorBytes != 2048 {
		t.Fatalf("limits=%#v", limits)
	}
	if bad := base.WithEvidence(128, 0, 1, 1, 1, 1); bad.Features[FeatureEvidenceLedger] != Unavailable || bad.Features[FeatureExpectedOutputs] != Unavailable {
		t.Fatal("incomplete limits promoted evidence")
	}
}

func TestA25EnvironmentCapabilityIsExplicitVersionedAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureEnvironmentFingerprint] != Unavailable || len(base.EnvironmentSnapshotSchemaVersions) != 0 || len(base.EnvironmentFingerprintVersions) != 0 || len(base.ToolchainFingerprintVersions) != 0 || len(base.EnvironmentToolchainProbeIDs) != 0 {
		t.Fatalf("baseline leaked environment observation support: %#v", base)
	}
	catalog := base.WithEnvironmentObservation(64, 5, 16, 2000, 512, 128, []string{"go", "node", "python", "java", "rust"})
	if catalog.Features[FeatureEnvironmentFingerprint] != Available {
		t.Fatalf("environment feature=%q", catalog.Features[FeatureEnvironmentFingerprint])
	}
	if !reflect.DeepEqual(catalog.EnvironmentSnapshotSchemaVersions, []int{1}) || !reflect.DeepEqual(catalog.EnvironmentFingerprintVersions, []int{1}) || !reflect.DeepEqual(catalog.ToolchainFingerprintVersions, []int{1}) {
		t.Fatalf("environment versions=%#v", catalog)
	}
	if !reflect.DeepEqual(catalog.EnvironmentToolchainProbeIDs, []string{"go", "node", "python", "java", "rust"}) {
		t.Fatalf("toolchain probes=%v", catalog.EnvironmentToolchainProbeIDs)
	}
	limits := catalog.Limits
	if limits.EnvironmentRelevantVariables != 64 || limits.EnvironmentToolchainProbes != 5 || limits.EnvironmentToolchainObservations != 16 || limits.EnvironmentProbeTimeoutMS != 2000 || limits.EnvironmentProbeOutputBytes != 512 || limits.EnvironmentCacheEntries != 128 {
		t.Fatalf("environment limits=%#v", limits)
	}
	clone := catalog.Clone()
	clone.EnvironmentSnapshotSchemaVersions[0] = 99
	clone.EnvironmentToolchainProbeIDs[0] = "changed"
	if catalog.EnvironmentSnapshotSchemaVersions[0] != 1 || catalog.EnvironmentToolchainProbeIDs[0] != "go" {
		t.Fatal("environment capability clone aliases slices")
	}
	if invalid := base.WithEnvironmentObservation(64, 5, 16, 2000, 512, 128, nil); invalid.Features[FeatureEnvironmentFingerprint] != Unavailable {
		t.Fatal("probe-less environment observation advertised")
	}
}

func TestA25ProcessCapabilityIsExplicitVersionedAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureProcessInspection] != Unavailable || len(base.ProcessObservationSchemaVersions) != 0 || base.PortObservationSupported {
		t.Fatalf("baseline leaked process observation support: %#v", base)
	}
	catalog := base.WithProcessInspection(128, 8, 64<<10, 2000, 64, true)
	if catalog.Features[FeatureProcessInspection] != Available || !reflect.DeepEqual(catalog.ProcessObservationSchemaVersions, []int{1}) || !catalog.PortObservationSupported {
		t.Fatalf("process capability=%#v", catalog)
	}
	limits := catalog.Limits
	if limits.ProcessDescendants != 128 || limits.ProcessTraversalDepth != 8 || limits.ProcessObservationBytes != 64<<10 || limits.ProcessObservationMS != 2000 || limits.ProcessPortRecords != 64 {
		t.Fatalf("process limits=%#v", limits)
	}
	clone := catalog.Clone()
	clone.ProcessObservationSchemaVersions[0] = 99
	if catalog.ProcessObservationSchemaVersions[0] != 1 {
		t.Fatal("process capability clone aliases versions")
	}
	if invalid := base.WithProcessInspection(0, 8, 64<<10, 2000, 64, true); invalid.Features[FeatureProcessInspection] != Unavailable {
		t.Fatal("invalid process bounds advertised")
	}
}
