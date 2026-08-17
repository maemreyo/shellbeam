package inputtrace

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestE27LockedLimitsAndModeNormalization(t *testing.T) {
	if MaxRawEvents != 32768 || MaxUniqueResources != 2048 || MaxPublicResources != 512 || MaxExternalResources != 128 || MaxRawEventBytes != 4096 || MaxPrivateRawBytes != 8<<20 || MaxPublicRecordBytes != 512<<10 || MaxRetainedTraceRecords != 128 || MaxTraceCaptureDuration != time.Hour || TraceStartupBudget != 2*time.Second || WorkerQueueDepth != 32 {
		t.Fatalf("unexpected E27 limits")
	}
	for _, tc := range []struct{ in, want Mode }{{"", ModeOff}, {ModeOff, ModeOff}, {ModeBestEffort, ModeBestEffort}, {ModeRequired, ModeRequired}} {
		got, err := NormalizeMode(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeMode(%q)=%q,%v want %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := NormalizeMode("automatic"); err == nil {
		t.Fatal("unknown trace mode accepted")
	}
}

func TestE27PublicTypesCannotCarryPayloadOrPrivateProviderPaths(t *testing.T) {
	forbidden := []string{"content", "payload", "environmentvalue", "envvalue", "stdin", "socketpath", "dylibpath", "rawpath", "privatepath", "logpath", "pathhash"}
	for _, typ := range []reflect.Type{reflect.TypeOf(Resource{}), reflect.TypeOf(InstrumentationBinding{}), reflect.TypeOf(Record{}), reflect.TypeOf(Inspection{})} {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Fatalf("%s contains forbidden public field %s", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
}

func TestE27DarwinPartialBindingAndRecordValidate(t *testing.T) {
	matrix := darwinPartialMatrix()
	binding := InstrumentationBinding{
		SchemaVersion: SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: ModeBestEffort,
		Status: BindingActive, Provider: ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: EffectEnvironmentAffecting,
		PreExecCoverageEstablished: false, Coverage: matrix,
	}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	record := Record{
		SchemaVersion: SchemaVersion, DerivationKey: strings.Repeat("b", 64), TraceID: binding.TraceID,
		OperationID: "e27-op", SessionID: "e27-session", ReceiptDigest: strings.Repeat("c", 64), Mode: ModeBestEffort,
		Provider: binding.Provider, Platform: "darwin", InstrumentationFingerprint: binding.InstrumentationFingerprint,
		InstrumentationEffect: binding.InstrumentationEffect, Authority: AuthorityAdvisory, ScopeKind: ScopeObservedInput,
		MayHaveUnobservedDependencies: true, CaptureStart: now, CaptureEnd: now.Add(time.Second),
		PreExecCoverageEstablished: false, Coverage: matrix, Outcome: OutcomePartial,
		Resources: []Resource{{ObservationClass: ClassFilesystemReads, PathClass: PathRepoRelative, Identity: "internal/app/input.go"}},
		Summary:   Summary{ResourcesReturned: 1, ResourcesObserved: 1},
	}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
}

func darwinPartialMatrix() CoverageMatrix {
	return CoverageMatrix{
		FilesystemReads: CoveragePartial, FilesystemMetadataQueries: CoveragePartial, DirectoryEnumerations: CoveragePartial,
		FilesystemWrites: CoveragePartial, ExecutedBinaries: CoveragePartial, LoadedLibraries: CoveragePartial,
		EnvironmentNamesObserved: CoverageUnsupported, NetworkAttempts: CoverageUnsupported, ChildProcesses: CoveragePartial,
	}
}
