package capability

import (
	"reflect"
	"testing"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestE27InputTracingCapabilityIsExplicitPartialAndBounded(t *testing.T) {
	base := Baseline(Limits{})
	if base.Features[FeatureInputTracing] != Unavailable || base.InputTracing != nil {
		t.Fatalf("baseline leaked E27: %#v", base)
	}
	provider := trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}
	matrix := trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial}
	got := base.WithInputTracing(provider, "darwin", false, trace.EffectEnvironmentAffecting, matrix)
	if got.Features[FeatureInputTracing] != Available || got.InputTracing == nil {
		t.Fatalf("E27 unavailable: %#v", got)
	}
	if !reflect.DeepEqual(got.InputTracing.SchemaVersions, []int{trace.SchemaVersion}) || got.InputTracing.Provider != provider || got.InputTracing.Platform != "darwin" || got.InputTracing.Maturity != "experimental" || got.InputTracing.PreExecCoverage || got.InputTracing.InstrumentationEffect != trace.EffectEnvironmentAffecting || got.InputTracing.Coverage != matrix || got.InputTracing.Authority != trace.AuthorityAdvisory {
		t.Fatalf("support=%#v", got.InputTracing)
	}
	l := got.Limits
	if l.InputTraceRawEvents != trace.MaxRawEvents || l.InputTraceUniqueResources != trace.MaxUniqueResources || l.InputTracePublicResources != trace.MaxPublicResources || l.InputTraceExternalResources != trace.MaxExternalResources || l.InputTraceRawEventBytes != trace.MaxRawEventBytes || l.InputTracePrivateRawBytes != trace.MaxPrivateRawBytes || l.InputTracePublicRecordBytes != trace.MaxPublicRecordBytes || l.InputTraceRetainedRecords != trace.MaxRetainedTraceRecords || l.InputTraceCaptureDurationMS != trace.MaxTraceCaptureDuration.Milliseconds() || l.InputTraceStartupBudgetMS != trace.TraceStartupBudget.Milliseconds() || l.InputTraceWorkerQueueDepth != trace.WorkerQueueDepth {
		t.Fatalf("limits=%#v", l)
	}
}

func TestE27CapabilityRejectsOverclaimAndCloneDoesNotAlias(t *testing.T) {
	provider := trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}
	matrix := trace.CoverageMatrix{FilesystemReads: trace.CoverageCompleteForOwnedTree, ChildProcesses: trace.CoveragePartial}
	base := Baseline(Limits{})
	if got := base.WithInputTracing(provider, "darwin", false, trace.EffectEnvironmentAffecting, matrix); got.Features[FeatureInputTracing] != Unavailable || got.InputTracing != nil {
		t.Fatal("overclaim promoted")
	}
	matrix = trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial}
	original := base.WithInputTracing(provider, "darwin", false, trace.EffectEnvironmentAffecting, matrix)
	clone := original.Clone()
	clone.InputTracing.SchemaVersions[0] = 99
	clone.InputTracing.Provider.ID = "other"
	if original.InputTracing.SchemaVersions[0] != trace.SchemaVersion || original.InputTracing.Provider.ID != "dyld-interpose" {
		t.Fatal("clone aliased E27 support")
	}
}
