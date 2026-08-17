package operation

import (
	"strings"
	"testing"

	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestE27HistoricalTraceOffKeepsExactRawAndTypedFingerprints(t *testing.T) {
	ordinary := Intent{Command: "printf hi", CWD: "/tmp", TTY: true, TimeoutMS: 10}
	request, err := ordinary.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	execution, err := ordinary.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if request != "6d920130b300c6bf7487d8734fcb7094390e29f1571be28a4ea79d62d86a7ff9" || execution != "cf903dfb875d0c2fd1c409ce69e70aad11801d426dc6d028e437ce9f65d2f3e4" {
		t.Fatalf("historical raw fingerprints moved request=%s execution=%s", request, execution)
	}
	explicitOff := ordinary
	explicitOff.TraceMode = trace.ModeOff
	if got, _ := explicitOff.RequestFingerprint(); got != request {
		t.Fatalf("explicit trace off moved request fingerprint: %s != %s", got, request)
	}
	if got, _ := explicitOff.ExecutionFingerprint("/bin/sh"); got != execution {
		t.Fatalf("explicit trace off moved execution fingerprint: %s != %s", got, execution)
	}

	typed := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	typedFP, err := typed.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if typedFP != "8ab3a04868e469d1965932338e39ea2b5a0f16e8ffc4f5e006a880c463bab102" {
		t.Fatalf("historical typed fingerprint moved: %s", typedFP)
	}
	typed.TraceMode = trace.ModeOff
	if got, _ := typed.Fingerprint(); got != typedFP {
		t.Fatalf("explicit typed trace off moved fingerprint: %s != %s", got, typedFP)
	}
}

func TestE27TraceModeBindsCallerRequestButUninstrumentedBestEffortDoesNotInventExecutionSemantics(t *testing.T) {
	base := Intent{Command: "true", CWD: "/tmp"}
	baseRequest, _ := base.RequestFingerprint()
	baseExecution, _ := base.ExecutionFingerprint("/bin/sh")

	best := base
	best.TraceMode = trace.ModeBestEffort
	bestRequest, err := best.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	required := base
	required.TraceMode = trace.ModeRequired
	requiredRequest, err := required.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if bestRequest == baseRequest || requiredRequest == baseRequest || bestRequest == requiredRequest {
		t.Fatalf("trace request modes are not distinct base=%s best=%s required=%s", baseRequest, bestRequest, requiredRequest)
	}
	if got, err := best.ExecutionFingerprint("/bin/sh"); err != nil || got != baseExecution {
		t.Fatalf("uninstrumented best-effort invented execution semantics: %s %v", got, err)
	}
	if _, err := required.ExecutionFingerprint("/bin/sh"); err == nil {
		t.Fatal("required trace without frozen instrumentation produced execution fingerprint")
	}
}

func TestE27ActiveInstrumentationDigestChangesExecutionIdentityAndResponseControlsDoNot(t *testing.T) {
	base := Intent{Command: "true", CWD: "/tmp", TraceMode: trace.ModeBestEffort}
	bindingA := e27OperationTraceBinding("a", trace.EffectEnvironmentAffecting)
	digestA, err := bindingA.Digest()
	if err != nil {
		t.Fatal(err)
	}
	base.TraceExecutionDigest = digestA
	first, err := base.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	responseChanged := base
	responseChanged.YieldMS = 999
	responseChanged.MaxOutputBytes = 1
	second, err := responseChanged.ExecutionFingerprint("/bin/sh")
	if err != nil || second != first {
		t.Fatalf("response controls changed traced execution identity: %s %s %v", first, second, err)
	}

	bindingB := e27OperationTraceBinding("b", trace.EffectEnvironmentAffecting)
	digestB, _ := bindingB.Digest()
	changedFingerprint := base
	changedFingerprint.TraceExecutionDigest = digestB
	third, _ := changedFingerprint.ExecutionFingerprint("/bin/sh")
	if third == first {
		t.Fatal("instrumentation fingerprint did not change execution identity")
	}
	bindingC := e27OperationTraceBinding("a", trace.EffectNonInvasive)
	digestC, _ := bindingC.Digest()
	changedEffect := base
	changedEffect.TraceExecutionDigest = digestC
	fourth, _ := changedEffect.ExecutionFingerprint("/bin/sh")
	if fourth == first {
		t.Fatal("instrumentation effect did not change execution identity")
	}
}

func TestE27TypedTraceModeBindsCallerRequest(t *testing.T) {
	base := TypedRequestIntent{WorkspaceID: "ws_01K00000000000000000000000", ProjectCommandID: "test"}
	plain, _ := base.Fingerprint()
	best := base
	best.TraceMode = trace.ModeBestEffort
	bestFP, err := best.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	required := base
	required.TraceMode = trace.ModeRequired
	requiredFP, err := required.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if plain == bestFP || plain == requiredFP || bestFP == requiredFP {
		t.Fatalf("typed trace modes not bound plain=%s best=%s required=%s", plain, bestFP, requiredFP)
	}
}

func TestE27ExecutionEnvironmentAdditionsAreEphemeralControl(t *testing.T) {
	spec := ExecutionSpec{EnvironmentAdditions: []EnvironmentEntry{{Name: "SHELLBEAM_TRACE_ID", Value: "trace_01K00000000000000000000000"}}}
	if len(spec.EnvironmentAdditions) != 1 || spec.EnvironmentAdditions[0].Name != "SHELLBEAM_TRACE_ID" {
		t.Fatalf("environment additions=%#v", spec.EnvironmentAdditions)
	}
}

func e27OperationTraceBinding(hex string, effect trace.InstrumentationEffect) trace.InstrumentationBinding {
	return trace.InstrumentationBinding{
		SchemaVersion: trace.SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: trace.ModeBestEffort, Status: trace.BindingActive,
		Provider: trace.ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat(hex, 64), InstrumentationEffect: effect,
		Coverage: trace.CoverageMatrix{FilesystemReads: trace.CoveragePartial, FilesystemMetadataQueries: trace.CoveragePartial, DirectoryEnumerations: trace.CoveragePartial, FilesystemWrites: trace.CoveragePartial, ExecutedBinaries: trace.CoveragePartial, LoadedLibraries: trace.CoveragePartial, EnvironmentNamesObserved: trace.CoverageUnsupported, NetworkAttempts: trace.CoverageUnsupported, ChildProcesses: trace.CoveragePartial},
	}
}
