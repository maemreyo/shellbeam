package structuredresult

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

func qualifiedPytestBinding(t *testing.T, root string, execution environment.ExecutionContext) PytestInvocationBindingV1 {
	t.Helper()
	binding, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{
		Argv:        []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
		ResolvedCWD: root, WorkspaceRoot: root, Execution: execution,
	}, &fakePytestPresenceObserver{})
	if err != nil || !qualified {
		t.Fatalf("qualified=%v err=%v", qualified, err)
	}
	return binding
}

func TestCaptureAuthorityBindsCanonicalPytestInvocationAndIntent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	binding := qualifiedPytestBinding(t, root, execution)
	producerDigest, err := binding.ProducerBindingDigest()
	if err != nil || len(producerDigest) != 64 {
		t.Fatalf("producer digest=%q err=%v", producerDigest, err)
	}
	intent := ArtifactCaptureIntent{
		SchemaVersion: ArtifactCaptureIntentSchemaV1,
		OperationID:   "pytest-op", SessionID: "pytest-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		AdapterID:         PytestJUnitAdapterID,
		DeclaredPathToken: binding.JUnitOutput.DeclaredPathToken, NormalizedWorkspacePath: binding.JUnitOutput.NormalizedWorkspacePath,
		ExpectedKind: CaptureExpectedRegularFile, MaxBlobBytes: DefaultMaxArtifactBlobBytes,
		ProducerBindingDigest: producerDigest,
		Baseline:              CaptureBaselineIdentity{State: CaptureBaselineAbsent, AuthorityDigest: strings.Repeat("a", 64)},
	}
	intentDigest, err := intent.Digest()
	if err != nil || len(intentDigest) != 64 {
		t.Fatalf("intent digest=%q err=%v", intentDigest, err)
	}
	authority := CaptureAuthority{SchemaVersion: CaptureAuthoritySchemaV1, PytestInvocation: &binding, Intent: intent}
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	captureDigest, err := authority.StructuredCaptureDigest()
	if err != nil || captureDigest != intentDigest {
		t.Fatalf("capture digest=%q intent=%q err=%v", captureDigest, intentDigest, err)
	}

	changed := binding
	changed.PytestAddoptsEnvironmentFact, err = NewEnvironmentPresenceFact(environment.ExecutionContext{Mode: "argv", Identity: "/opt/pytest"}, PytestAddoptsEnvironment, false)
	if err != nil {
		t.Fatal(err)
	}
	changedDigest, err := changed.ProducerBindingDigest()
	if err != nil || changedDigest == producerDigest {
		t.Fatalf("environment authority did not change producer digest: %q %q err=%v", producerDigest, changedDigest, err)
	}

	changed = binding
	changed.ConfigAddoptsOverride = "addopts=-q"
	changedDigest, err = changed.ProducerBindingDigest()
	if err != nil || changedDigest == producerDigest {
		t.Fatalf("addopts authority did not change producer digest: %q %q err=%v", producerDigest, changedDigest, err)
	}

	changed = binding
	changed.ArgumentFileState = "present"
	changedDigest, err = changed.ProducerBindingDigest()
	if err != nil || changedDigest == producerDigest {
		t.Fatalf("argument-file authority did not change producer digest: %q %q err=%v", producerDigest, changedDigest, err)
	}
}

func TestCaptureAuthorityRejectsMismatchedProviderBindingAndPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	binding := qualifiedPytestBinding(t, root, environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"})
	producerDigest, err := binding.ProducerBindingDigest()
	if err != nil {
		t.Fatal(err)
	}
	base := ArtifactCaptureIntent{
		SchemaVersion: ArtifactCaptureIntentSchemaV1, OperationID: "pytest-op", SessionID: "pytest-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		AdapterID: PytestJUnitAdapterID, DeclaredPathToken: binding.JUnitOutput.DeclaredPathToken,
		NormalizedWorkspacePath: binding.JUnitOutput.NormalizedWorkspacePath, ExpectedKind: CaptureExpectedRegularFile,
		MaxBlobBytes: DefaultMaxArtifactBlobBytes, ProducerBindingDigest: producerDigest,
		Baseline: CaptureBaselineIdentity{State: CaptureBaselineAbsent, AuthorityDigest: strings.Repeat("b", 64)},
	}
	for _, mutate := range []func(*ArtifactCaptureIntent){
		func(v *ArtifactCaptureIntent) { v.ProducerBindingDigest = strings.Repeat("c", 64) },
		func(v *ArtifactCaptureIntent) { v.NormalizedWorkspacePath = "other/junit.xml" },
		func(v *ArtifactCaptureIntent) { v.AdapterID = "go-test-json" },
	} {
		intent := base
		mutate(&intent)
		authority := CaptureAuthority{SchemaVersion: CaptureAuthoritySchemaV1, PytestInvocation: &binding, Intent: intent}
		if err := authority.Validate(); err == nil {
			t.Fatalf("mismatched capture authority accepted: %#v", intent)
		}
	}
}
