package receipt

import (
	"strings"
	"testing"

	hermetic "github.com/maemreyo/shellbeam/internal/core/hermetic"
	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestHermeticReceiptV2V3RequireMatchingBindingAndResult(t *testing.T) {
	for _, version := range []int{2, 3} {
		rec := hermeticReceiptFixture(version)
		if err := rec.Validate(); err != nil {
			t.Fatalf("v%d valid hermetic receipt rejected: %v", version, err)
		}
		missing := rec
		missing.HermeticResult = nil
		if err := missing.Validate(); err == nil {
			t.Fatalf("v%d binding without result accepted", version)
		}
		mismatch := rec
		copy := *rec.HermeticResult
		copy.BoundaryID = "hb_01K00000000000000000000099"
		mismatch.HermeticResult = &copy
		if err := mismatch.Validate(); err == nil {
			t.Fatalf("v%d mismatched result accepted", version)
		}
	}
}

func TestHermeticReceiptRejectedByLegacyAndPersistentSchemas(t *testing.T) {
	for _, version := range []int{1, 4} {
		rec := hermeticReceiptFixture(version)
		if version == 1 {
			rec.Fingerprint = "legacy"
			rec.RequestFingerprint, rec.ExecutionFingerprint = "", ""
		}
		if version == 4 {
			rec.Persistent = true
			rec.HermeticBinding.Request = hermetic.Request{Version: 1, Mode: hermetic.ModeRequired, RepoInputs: []string{"go.mod"}, Network: hermetic.NetworkOff, Environment: hermetic.EnvironmentFixedAllowlist, Stdin: hermetic.StdinClosed, Writes: hermetic.WritesEphemeralDiscard}
		}
		if err := rec.Validate(); err == nil {
			t.Fatalf("schema v%d accepted hermetic receipt", version)
		}
	}
}

func hermeticReceiptFixture(version int) Receipt {
	binding := &hermetic.BoundaryBinding{
		SchemaVersion:         hermetic.BoundaryBindingSchemaV1,
		BoundaryID:            "hb_01K00000000000000000000000",
		Request:               hermetic.Request{Version: 1, Mode: hermetic.ModeRequired, RepoInputs: []string{"go.mod"}, Network: hermetic.NetworkOff, Environment: hermetic.EnvironmentFixedAllowlist, Stdin: hermetic.StdinClosed, Writes: hermetic.WritesEphemeralDiscard},
		CaptureManifestSHA256: digest64('d'),
		Provider:              hermetic.ProviderIdentity{Provider: hermetic.ProviderBubblewrap, Version: hermetic.BubblewrapVersionV1, BinarySHA256: digest64('a'), RuntimeManifestSHA256: digest64('b')},
		Toolchain:             hermetic.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: digest64('c')},
	}
	result := &hermetic.BoundaryResult{SchemaVersion: hermetic.BoundaryResultSchemaV1, BoundaryID: binding.BoundaryID, Provider: binding.Provider, Toolchain: binding.Toolchain, EstablishedPreExec: true, Continuity: hermetic.ContinuityComplete}
	zero := 0
	rec := Receipt{SchemaVersion: version, OperationID: "op", SessionID: "session", RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon", State: session.Completed, Outcome: session.Success, TTY: false, TimeoutMS: 1, OutputBytes: 0, OutputComplete: true, InputAcceptedBytes: 0, InputDeliveredBytes: 0, StdinClosed: true, Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: true, Code: &zero}, Signal: SignalEvidence{}, HermeticBinding: binding, HermeticResult: result}
	if version == 3 {
		params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
		paramFingerprint, err := project.ParameterFingerprint(params)
		if err != nil {
			panic(err)
		}
		rec.ProjectCommand = &project.CommandBinding{
			SchemaVersion: project.BindingSchemaVersion, ManifestDigest: strings.Repeat("f", 64), ManifestSchemaVersion: project.ManifestSchemaV2,
			CommandID: "test", ParameterFingerprint: paramFingerprint, Parameters: params, ResolvedArgv: []string{"go", "test", "./internal/app"}, LogicalCWD: ".", ResolvedCWD: "/repo",
		}
	}
	return rec
}
func digest64(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
