package receipt

import (
	"strings"
	"testing"

	project "github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestReceiptV3RequiresFrozenProjectCommandBinding(t *testing.T) {
	binding := receiptProjectCommandBinding(t)
	base := Receipt{
		SchemaVersion: 3,
		OperationID:   "typed-op", SessionID: "typed-session",
		RequestFingerprint: strings.Repeat("a", 64), ExecutionFingerprint: strings.Repeat("b", 64),
		DaemonIncarnation: "daemon", ExecutionMode: "argv", Executable: "go",
		CWD: binding.ResolvedCWD, State: session.Running, ProjectCommand: &binding,
	}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	without := base
	without.ProjectCommand = nil
	if err := without.Validate(); err == nil {
		t.Fatal("v3 receipt without frozen project command accepted")
	}
	bad := base
	copy := binding
	copy.ParameterFingerprint = "bad"
	bad.ProjectCommand = &copy
	if err := bad.Validate(); err == nil {
		t.Fatal("v3 receipt with malformed binding accepted")
	}
}

func TestReceiptV1V2RejectProjectCommandField(t *testing.T) {
	binding := receiptProjectCommandBinding(t)
	for _, schema := range []int{1, 2} {
		r := Receipt{SchemaVersion: schema, ProjectCommand: &binding, State: session.Running}
		if schema == 2 {
			r.RequestFingerprint = strings.Repeat("a", 64)
			r.ExecutionFingerprint = strings.Repeat("b", 64)
		}
		if err := r.Validate(); err == nil {
			t.Fatalf("schema %d accepted project command provenance", schema)
		}
	}
}

func receiptProjectCommandBinding(t *testing.T) project.CommandBinding {
	t.Helper()
	params := []project.ParameterBinding{{ID: "package", Kind: project.ParameterRepoPackage, Value: "./internal/app", Source: project.BindingSourceCaller, ProviderID: "go-repo-package", ProviderVersion: 1}}
	fingerprint, err := project.ParameterFingerprint(params)
	if err != nil {
		t.Fatal(err)
	}
	return project.CommandBinding{
		SchemaVersion:  project.BindingSchemaVersion,
		ManifestDigest: strings.Repeat("c", 64), ManifestSchemaVersion: project.ManifestSchemaV2,
		CommandID: "test_package", ParameterFingerprint: fingerprint, Parameters: params,
		ResolvedArgv: []string{"go", "test", "./internal/app"}, LogicalCWD: ".", ResolvedCWD: "/repo",
		PathObservationQuality: "",
	}
}
