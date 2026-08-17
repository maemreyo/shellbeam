package repro

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCreateFingerprintBindsTargetAndPolicyButNotResponseTiming(t *testing.T) {
	request := CreateRequest{CreateID: "repro-create-1", OperationID: "op-1", Policy: CapturePolicy{DependentDerivations: CaptureCurrent}}
	first, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := request.Fingerprint()
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("fingerprints first=%q second=%q err=%v", first, second, err)
	}
	changed := request
	changed.OperationID = "op-2"
	other, err := changed.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("target operation not bound")
	}
	changed = request
	changed.Policy.DependentDerivations = "unknown"
	if _, err := changed.Fingerprint(); err == nil {
		t.Fatal("unknown capture policy accepted")
	}
}

func TestCapsuleValidatesIndependentCaptureAndImmutableReferenceDescriptors(t *testing.T) {
	capsule := validCapsule()
	if err := capsule.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := capsule
	bad.Capture.Environment = CaptureExact
	if err := bad.Validate(); err == nil {
		t.Fatal("exact environment capture accepted without environment fingerprint")
	}
	bad = capsule
	bad.Results[0].OriginalAvailability = "future"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown creation availability accepted")
	}
	bad = capsule
	bad.Results = append(bad.Results, make([]ReferenceDescriptor, MaxReferenceDescriptors)...)
	if err := bad.Validate(); err == nil {
		t.Fatal("unbounded reference descriptors accepted")
	}
}

func TestCapsuleHasNoRawEnvironmentInputSourceOrCheckpointContentFields(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Capsule{}), reflect.TypeOf(EnvironmentDescriptor{}), reflect.TypeOf(InputDescriptor{}), reflect.TypeOf(SourceDescriptor{})} {
		for _, forbidden := range []string{"EnvironmentValues", "RawEnvironment", "Stdin", "StdinHash", "SourceContents", "CheckpointContents", "CheckpointIdentity"} {
			if _, ok := typ.FieldByName(forbidden); ok {
				t.Fatalf("%s exposes forbidden field %s", typ.Name(), forbidden)
			}
		}
	}
	data, err := json.Marshal(validCapsule())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"environment_values", "stdin_hash", "source_contents", "checkpoint_contents", "reproducible"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("serialized capsule exposes forbidden concept %q: %s", forbidden, data)
		}
	}
}

func TestResolutionStateIsSeparateFromCreationDescriptor(t *testing.T) {
	if _, ok := reflect.TypeOf(ReferenceDescriptor{}).FieldByName("ResolutionState"); ok {
		t.Fatal("dynamic resolution state persisted in immutable creation descriptor")
	}
	for _, state := range []ResolutionState{ResolutionAvailable, ResolutionCompacted, ResolutionPurged, ResolutionUnavailable} {
		if !state.Valid() {
			t.Fatalf("resolution state rejected: %q", state)
		}
	}
}

func validCapsule() Capsule {
	return Capsule{
		SchemaVersion: SchemaVersion, CreateID: "repro-create-1", ReproID: "repro_01K00000000000000000000000",
		CreatedAt: time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), CaptureCutDigest: strings.Repeat("a", 64),
		Execution: ExecutionDescriptor{
			OperationID: "op-1", SessionID: "session-1", ReceiptDigest: strings.Repeat("b", 64),
			CommandSemanticsFingerprint: strings.Repeat("c", 64), ExecutionMode: "argv", Executable: "go",
			ResolvedArgv: []string{"go", "test", "./..."}, CommandDetails: CaptureExact,
		},
		Source: SourceDescriptor{
			RepositoryID: "repo_01K00000000000000000000000", WorkspaceID: "ws_01K00000000000000000000000",
			WorkspaceGeneration: "gen_" + strings.Repeat("d", 64), Quality: CapturePartial,
		},
		Environment: EnvironmentDescriptor{EnvironmentQuality: CaptureUnknown, ToolchainQuality: CaptureUnknown},
		Input:       InputDescriptor{AcceptedBytes: 0, DeliveredBytes: 0, Complete: true, ContentIdentity: CaptureUnavailable},
		Results:     []ReferenceDescriptor{{RefID: "structured:abc", RecordKind: "structured_result", ProducerID: "go-test-json", ProducerVersion: 1, SchemaVersion: 1, Digest: strings.Repeat("e", 64), Summary: "test results", OriginalAvailability: AvailabilityTerminal}},
		Capture: CaptureMatrix{
			Source: CapturePartial, Command: CaptureExact, Toolchain: CaptureUnknown, Environment: CaptureUnknown,
			FilesystemExternal: CaptureUnknown, NetworkDependencies: CaptureUnknown, ExternalServices: CaptureUnknown,
			TimeRandomness: CaptureUnknown, Input: CapturePartial, Results: CaptureComplete,
		},
	}
}

func TestTypedProjectCommandDescriptorIsBoundedAndValidated(t *testing.T) {
	capsule := validCapsule()
	capsule.Execution.ProjectCommandBindingDigest = strings.Repeat("1", 64)
	capsule.Execution.ProjectManifestDigest = strings.Repeat("2", 64)
	capsule.Execution.ProjectCommandID = "test_package"
	capsule.Execution.ParameterBindingFingerprint = strings.Repeat("3", 64)
	capsule.Execution.ParameterProviders = []ParameterProviderDescriptor{{ParameterID: "package", ProviderID: "go-repo-package", ProviderVersion: 1}}
	capsule.Project = ProjectDescriptor{ManifestDigest: strings.Repeat("2", 64), Quality: CaptureExact}
	if err := capsule.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := capsule
	bad.Execution.ProjectCommandBindingDigest = "bad"
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid binding digest accepted")
	}
	bad = capsule
	bad.Execution.ParameterProviders = []ParameterProviderDescriptor{{ParameterID: "../bad", ProviderID: "go-repo-package", ProviderVersion: 1}}
	if err := bad.Validate(); err == nil {
		t.Fatal("invalid provider descriptor accepted")
	}
	bad = capsule
	bad.Execution.ProjectManifestDigest = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("typed command descriptor accepted without manifest digest")
	}
}

func TestE27InputTraceDoesNotAddRawProviderOrNegativeAuthorityToReproCapsule(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(Capsule{}), reflect.TypeOf(InputDescriptor{}), reflect.TypeOf(ReferenceDescriptor{})} {
		for i := 0; i < typ.NumField(); i++ {
			name := strings.ToLower(typ.Field(i).Name)
			for _, forbidden := range []string{"rawtrace", "providerpath", "socketpath", "environmentvalue", "networkpayload", "proveninput", "negativeobservation"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("%s exposed forbidden E27 field %s", typ.Name(), typ.Field(i).Name)
				}
			}
		}
	}
}
