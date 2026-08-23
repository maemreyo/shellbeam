package store

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func testCaptureAuthority(t *testing.T, operationID, path string, baselineByte byte) structuredapp.CaptureAuthority {
	t.Helper()
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	fact, err := structuredapp.NewEnvironmentPresenceFact(execution, structuredapp.PytestAddoptsEnvironment, false)
	if err != nil {
		t.Fatal(err)
	}
	binding := structuredapp.PytestInvocationBindingV1{
		SchemaVersion:       structuredapp.PytestInvocationSchemaV1,
		ProducerForm:        structuredapp.PytestProducerDirect,
		JUnitOutput:         structuredapp.JUnitOutputBinding{DeclaredPathToken: path, NormalizedWorkspacePath: path},
		JUnitFamilyOverride: "junit_family=xunit2", ConfigAddoptsOverride: "addopts=", ArgumentFileState: structuredapp.PytestArgumentFileNone,
		PytestAddoptsEnvironmentFact: fact,
	}
	producerDigest, err := binding.ProducerBindingDigest()
	if err != nil {
		t.Fatal(err)
	}
	intent := structuredapp.ArtifactCaptureIntent{
		SchemaVersion: structuredapp.ArtifactCaptureIntentSchemaV1,
		OperationID:   operationID, SessionID: operationID + "-session",
		RepositoryID: "repo_01M09A27JCSE71BXSP477EKN34", WorkspaceID: "ws_01M0CJB0KMBXWM7C7YDFYHBT2Q",
		AdapterID:         structuredapp.PytestJUnitAdapterID,
		DeclaredPathToken: path, NormalizedWorkspacePath: path,
		ExpectedKind: structuredapp.CaptureExpectedRegularFile, MaxBlobBytes: structuredapp.DefaultMaxArtifactBlobBytes,
		ProducerBindingDigest: producerDigest,
		Baseline:              structuredapp.CaptureBaselineIdentity{SchemaVersion: structuredapp.CaptureBaselineSchemaV1, State: structuredapp.CaptureBaselineAbsent, AuthorityDigest: strings.Repeat(string(baselineByte), 64)},
	}
	return structuredapp.CaptureAuthority{SchemaVersion: structuredapp.CaptureAuthoritySchemaV1, ProducerInvocationBinding: structuredapp.ProducerInvocationBinding{Kind: structuredapp.ProducerInvocationPytest, PytestInvocation: &binding}, Intent: intent}
}

func TestLegacyPytestCaptureAuthorityLiteralReopensWithoutRewrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	const legacy = `{"schema_version":1,"authority":{"schema_version":1,"pytest_invocation":{"schema_version":1,"producer_form":"pytest","junit_output":{"declared_path_token":"reports/junit.xml","normalized_workspace_path":"reports/junit.xml"},"junit_family_override":"junit_family=xunit2","config_addopts_override":"addopts=","argument_file_state":"none","pytest_addopts_environment_fact":{"name":"PYTEST_ADDOPTS","present":false,"authority_schema_version":1,"execution":{"mode":"argv","identity":"/usr/bin/pytest"},"authority_digest":"ce194512848b8619473a1806043e4f235cbb24f2a841e7ec672eafbd69d1b0b4"}},"intent":{"schema_version":1,"operation_id":"capture-legacy-pin","session_id":"capture-legacy-pin-session","repository_id":"repo_01M09A27JCSE71BXSP477EKN34","workspace_id":"ws_01M0CJB0KMBXWM7C7YDFYHBT2Q","adapter_id":"pytest-junit-xml","declared_path_token":"reports/junit.xml","normalized_workspace_path":"reports/junit.xml","expected_kind":"regular_file","max_blob_bytes":16777216,"producer_binding_digest":"9af119dd5b9dc43b72aee11291a5e93416cc78ce1faacea9508d32d70c818e95","baseline":{"schema_version":1,"state":"absent","authority_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},"structured_capture_digest":"9606da56f1bad7ca876d37adf9b2ab9c0faa8e5d74341bb1ae3181852fd49adc","state":"prepared"}` + "\n"
	path := r.captureAuthorityPath(operation.ID("capture-legacy-pin"))
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened := openStructuredRepositoryAt(t, root)
	got, err := reopened.FindCaptureAuthority(context.Background(), operation.ID("capture-legacy-pin"))
	if err != nil {
		t.Fatal(err)
	}
	if got.StructuredCaptureDigest != "9606da56f1bad7ca876d37adf9b2ab9c0faa8e5d74341bb1ae3181852fd49adc" {
		t.Fatalf("capture digest=%q", got.StructuredCaptureDigest)
	}
	if got.Authority.PytestInvocation == nil {
		t.Fatal("legacy pytest invocation missing after reopen")
	}
	if got.Authority.ProducerInvocationBinding.Kind != structuredapp.ProducerInvocationPytest {
		t.Fatalf("legacy binding kind=%q want pytest", got.Authority.ProducerInvocationBinding.Kind)
	}
	producerDigest, err := got.Authority.PytestInvocation.ProducerBindingDigest()
	if err != nil || producerDigest != "9af119dd5b9dc43b72aee11291a5e93416cc78ce1faacea9508d32d70c818e95" {
		t.Fatalf("producer digest=%q err=%v", producerDigest, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != legacy {
		t.Fatalf("legacy authority bytes changed: err=%v\nwant=%s\ngot=%s", err, legacy, after)
	}
}

func TestReserveCaptureAuthorityPersistsCanonicalReplayAuthority(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	want := testCaptureAuthority(t, "capture-op", "reports/junit.xml", 'a')
	digest, err := want.StructuredCaptureDigest()
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := r.ReserveCaptureAuthority(context.Background(), want)
	if err != nil || !created || stored.State != structuredapp.CaptureAuthorityPrepared || stored.StructuredCaptureDigest != digest || !reflect.DeepEqual(stored.Authority, want) {
		t.Fatalf("stored=%#v created=%v err=%v", stored, created, err)
	}
	before, err := os.ReadFile(r.captureAuthorityPath(operation.ID("capture-op")))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), `"kind":"pytest"`) {
		t.Fatalf("new pytest authority drifted legacy wire shape: %s", before)
	}

	stored, created, err = r.ReserveCaptureAuthority(context.Background(), want)
	if err != nil || created || !reflect.DeepEqual(stored.Authority, want) {
		t.Fatalf("replay stored=%#v created=%v err=%v", stored, created, err)
	}
	after, err := os.ReadFile(r.captureAuthorityPath(operation.ID("capture-op")))
	if err != nil || string(before) != string(after) {
		t.Fatalf("replay rewrote authority bytes: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}

	reopened := openStructuredRepositoryAt(t, root)
	got, err := reopened.FindCaptureAuthority(context.Background(), operation.ID("capture-op"))
	if err != nil || !reflect.DeepEqual(got, stored) {
		t.Fatalf("reopen got=%#v want=%#v err=%v", got, stored, err)
	}
}

func TestReserveCaptureAuthorityConflictsOnImmutableAuthorityChanges(t *testing.T) {
	r := openStructuredRepository(t)
	base := testCaptureAuthority(t, "capture-conflict", "reports/junit.xml", 'a')
	if _, created, err := r.ReserveCaptureAuthority(context.Background(), base); err != nil || !created {
		t.Fatalf("initial created=%v err=%v", created, err)
	}

	cases := []struct {
		name   string
		mutate func(*structuredapp.CaptureAuthority)
	}{
		{"producer binding", func(a *structuredapp.CaptureAuthority) {
			a.PytestInvocation.PytestAddoptsEnvironmentFact, _ = structuredapp.NewEnvironmentPresenceFact(environment.ExecutionContext{Mode: "argv", Identity: "/opt/pytest"}, structuredapp.PytestAddoptsEnvironment, false)
			a.Intent.ProducerBindingDigest, _ = a.PytestInvocation.ProducerBindingDigest()
		}},
		{"path", func(a *structuredapp.CaptureAuthority) {
			a.PytestInvocation.JUnitOutput.DeclaredPathToken = "other/junit.xml"
			a.PytestInvocation.JUnitOutput.NormalizedWorkspacePath = "other/junit.xml"
			a.Intent.DeclaredPathToken = "other/junit.xml"
			a.Intent.NormalizedWorkspacePath = "other/junit.xml"
			a.Intent.ProducerBindingDigest, _ = a.PytestInvocation.ProducerBindingDigest()
		}},
		{"baseline", func(a *structuredapp.CaptureAuthority) { a.Intent.Baseline.AuthorityDigest = strings.Repeat("b", 64) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := testCaptureAuthority(t, "capture-conflict", "reports/junit.xml", 'a')
			tc.mutate(&changed)
			if _, created, err := r.ReserveCaptureAuthority(context.Background(), changed); err == nil || created {
				t.Fatalf("changed authority accepted: created=%v err=%v", created, err)
			}
		})
	}
}

func TestFindCaptureAuthorityFailsClosedOnUnknownOrCorruptMetadata(t *testing.T) {
	r := openStructuredRepository(t)
	if _, err := r.FindCaptureAuthority(context.Background(), operation.ID("missing-capture")); err == nil {
		t.Fatal("missing capture authority returned nil error")
	}
	path := r.captureAuthorityPath(operation.ID("corrupt-capture"))
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"unknown":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindCaptureAuthority(context.Background(), operation.ID("corrupt-capture")); err == nil {
		t.Fatal("corrupt capture authority accepted")
	}
}

func TestCaptureAuthorityStateTransitionsAreDurableMonotonicAndIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openStructuredRepositoryAt(t, root)
	collisionAuthority := testCaptureAuthority(t, "capture-state-collision", "reports/junit.xml", 'c')
	if _, _, err := r.ReserveCaptureAuthority(context.Background(), collisionAuthority); err != nil {
		t.Fatal(err)
	}
	got, err := r.MarkCaptureAuthorityState(context.Background(), operation.ID("capture-state-collision"), structuredapp.CaptureAuthorityManagedPathCollision)
	if err != nil || got.State != structuredapp.CaptureAuthorityManagedPathCollision || got.AllowsMechanicalCapture() {
		t.Fatalf("collision got=%#v err=%v", got, err)
	}
	got, err = r.MarkCaptureAuthorityState(context.Background(), operation.ID("capture-state-collision"), structuredapp.CaptureAuthorityManagedPathCollision)
	if err != nil || got.State != structuredapp.CaptureAuthorityManagedPathCollision {
		t.Fatalf("collision replay got=%#v err=%v", got, err)
	}
	if _, err := r.MarkCaptureAuthorityState(context.Background(), operation.ID("capture-state-collision"), structuredapp.CaptureAuthorityAbandoned); err == nil {
		t.Fatal("collision authority downgraded to abandoned")
	}

	abandonAuthority := testCaptureAuthority(t, "capture-state-abandon", "other/junit.xml", 'd')
	if _, _, err := r.ReserveCaptureAuthority(context.Background(), abandonAuthority); err != nil {
		t.Fatal(err)
	}
	got, err = r.MarkCaptureAuthorityState(context.Background(), operation.ID("capture-state-abandon"), structuredapp.CaptureAuthorityAbandoned)
	if err != nil || got.State != structuredapp.CaptureAuthorityAbandoned || got.AllowsMechanicalCapture() {
		t.Fatalf("abandon got=%#v err=%v", got, err)
	}
	if _, err := r.MarkCaptureAuthorityState(context.Background(), operation.ID("capture-state-abandon"), structuredapp.CaptureAuthorityManagedPathCollision); err == nil {
		t.Fatal("abandoned authority resurrected into collision state")
	}

	reopened := openStructuredRepositoryAt(t, root)
	collision, err := reopened.FindCaptureAuthority(context.Background(), operation.ID("capture-state-collision"))
	if err != nil || collision.State != structuredapp.CaptureAuthorityManagedPathCollision {
		t.Fatalf("reopened collision=%#v err=%v", collision, err)
	}
	abandoned, err := reopened.FindCaptureAuthority(context.Background(), operation.ID("capture-state-abandon"))
	if err != nil || abandoned.State != structuredapp.CaptureAuthorityAbandoned {
		t.Fatalf("reopened abandoned=%#v err=%v", abandoned, err)
	}
}
