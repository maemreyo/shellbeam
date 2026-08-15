package receipt

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestPersistentV4ReceiptRequiresPersistentIdentityAndSupportsRawEvidence(t *testing.T) {
	contract := &evidence.Contract{VerificationKind: evidence.VerificationBuild, SourceScope: evidence.SourceScopeFull, ExpectedOutputs: []project.Output{{Path: "dist/app", Kind: "file", Required: true, Digest: "sha256"}}}
	base := Receipt{SchemaVersion: 4, RequestFingerprint: "request", ExecutionFingerprint: "execution", State: session.Running, Persistent: true, SessionName: "dev-server", Evidence: contract}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid persistent v4 receipt rejected: %v", err)
	}
	missing := base
	missing.Persistent = false
	if err := missing.Validate(); err == nil {
		t.Fatal("v4 receipt without persistent identity accepted")
	}
	badName := base
	badName.SessionName = "../dev"
	if err := badName.Validate(); err == nil {
		t.Fatal("v4 receipt with invalid name accepted")
	}
}

func TestPersistentV4TypedReceiptUsesProjectBindingWithoutCompetingRawEvidence(t *testing.T) {
	binding := project.CommandBinding{
		SchemaVersion:         project.BindingSchemaVersion,
		ManifestDigest:        "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ManifestSchemaVersion: project.ManifestSchemaV2,
		CommandID:             "test",
		ParameterFingerprint:  "74234e98afe7498fb5daf1f36ac2d78acc339464f950703b8c019892f982b90b",
		ResolvedArgv:          []string{"go", "test", "./..."},
		LogicalCWD:            ".",
		ResolvedCWD:           "/repo",
	}
	base := Receipt{SchemaVersion: 4, RequestFingerprint: "request", ExecutionFingerprint: "execution", State: session.Running, Persistent: true, SessionName: "typed-dev", ProjectCommand: &binding}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid persistent typed receipt rejected: %v", err)
	}
	base.Evidence = &evidence.Contract{VerificationKind: evidence.VerificationTest, SourceScope: evidence.SourceScopeFull}
	if err := base.Validate(); err == nil {
		t.Fatal("persistent typed receipt accepted competing raw evidence")
	}
}
