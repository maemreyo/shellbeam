package structuredresult

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/environment"
)

func TestGoAdapterSelectionIsExplicitOrExactDirectArgvOnly(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		argv     []string
		status   SelectionStatus
		adapter  string
	}{
		{"explicit test", "go-test-json", nil, SelectionSelected, "go-test-json"},
		{"explicit vet wins", "go-vet-json", []string{"go", "test", "-json", "./..."}, SelectionSelected, "go-vet-json"},
		{"unsupported explicit does not fallback", "junit", []string{"go", "test", "-json", "./..."}, SelectionUnsupported, "junit"},
		{"direct test", "", []string{"go", "test", "-json", "./..."}, SelectionSelected, "go-test-json"},
		{"direct vet", "", []string{"go", "vet", "-json", "./..."}, SelectionSelected, "go-vet-json"},
		{"flag not exact position", "", []string{"go", "test", "./...", "-json"}, SelectionNone, ""},
		{"shell pipeline is not direct argv", "", []string{"/bin/sh", "-lc", "go test -json ./... | tee out"}, SelectionNone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectAdapter(tc.explicit, tc.argv)
			if got.Status != tc.status || got.AdapterID != tc.adapter {
				t.Fatalf("selection=%#v", got)
			}
			if tc.status == SelectionUnsupported && got.ObservationCode != "structured_adapter_unsupported" {
				t.Fatalf("unsupported observation=%#v", got)
			}
		})
	}
}

func TestExplicitAdapterRequiresMatchingDirectProducerArgv(t *testing.T) {
	cases := []struct {
		name    string
		adapter string
		argv    []string
		ok      bool
	}{
		{"test json", "go-test-json", []string{"go", "test", "-json", "./..."}, true},
		{"test json later flag", "go-test-json", []string{"go", "test", "./...", "-json"}, true},
		{"test json equals", "go-test-json", []string{"go", "test", "-json=true", "./..."}, true},
		{"test missing json", "go-test-json", []string{"go", "test", "./..."}, false},
		{"test wrong producer", "go-test-json", []string{"go", "vet", "-json", "./..."}, false},
		{"vet json", "go-vet-json", []string{"go", "vet", "-json", "./..."}, true},
		{"vet missing json", "go-vet-json", []string{"go", "vet", "./..."}, false},
		{"empty argv", "go-test-json", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AdapterAcceptsArgv(tc.adapter, tc.argv); got != tc.ok {
				t.Fatalf("AdapterAcceptsArgv(%q, %#v)=%v want %v", tc.adapter, tc.argv, got, tc.ok)
			}
		})
	}
}

func TestPytestSelectionRequiresQualifiedBinding(t *testing.T) {
	root := t.TempDir()
	binding := qualifiedPytestBinding(t, root, environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"})
	argv := []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}
	if !PytestCandidateArgv(argv) {
		t.Fatal("qualified pytest argv was not a syntactic candidate")
	}
	if got := SelectAdapterWithPytest("", argv, nil); got.Status != SelectionNone {
		t.Fatalf("unqualified auto selection=%#v", got)
	}
	if got := SelectAdapterWithPytest("", argv, &binding); got.Status != SelectionSelected || got.AdapterID != PytestJUnitAdapterID {
		t.Fatalf("qualified auto=%#v", got)
	}
	if got := SelectAdapterWithPytest(PytestJUnitAdapterID, argv, nil); got.Status != SelectionUnsupported || got.ObservationCode != "structured_adapter_precondition_failed" {
		t.Fatalf("explicit unqualified=%#v", got)
	}
	if got := SelectAdapterWithPytest(PytestJUnitAdapterID, argv, &binding); got.Status != SelectionSelected {
		t.Fatalf("explicit qualified=%#v", got)
	}
}

func TestPytestCandidateRequiresExactProducerAndFrozenAuthorityFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		ok   bool
	}{
		{"direct", []string{"pytest", "--junitxml=reports/junit.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, true},
		{"module", []string{"python", "-m", "pytest", "--junit-xml", "reports/junit.xml", "--override-ini=junit_family=xunit2", "--override-ini=addopts="}, true},
		{"missing junit", []string{"pytest", "-o", "junit_family=xunit2", "-o", "addopts="}, false},
		{"missing family", []string{"pytest", "--junitxml=x.xml", "-o", "addopts="}, false},
		{"missing addopts", []string{"pytest", "--junitxml=x.xml", "-o", "junit_family=xunit2"}, false},
		{"wrapper", []string{"uv", "run", "pytest", "--junitxml=x.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, false},
		{"argfile", []string{"pytest", "@args", "--junitxml=x.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PytestCandidateArgv(tc.argv); got != tc.ok {
				t.Fatalf("got=%v want=%v", got, tc.ok)
			}
		})
	}
}

func qualifiedJestSelectionBinding(t *testing.T) ProducerInvocationBinding {
	t.Helper()
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/jest"}
	fact, err := NewEnvironmentPresenceFact(execution, JestJasmineEnvironment, false)
	if err != nil {
		t.Fatal(err)
	}
	binding := JestInvocationBindingV1{
		SchemaVersion:          JestInvocationSchemaV1,
		ProducerForm:           JestProducerDirect,
		JSONFlag:               "--json",
		OutputFile:             CaptureOutputBinding{DeclaredPathToken: "reports/jest.json", NormalizedWorkspacePath: "reports/jest.json"},
		ExcludedFlagState:      JestExcludedFlagsAbsent,
		JasmineEnvironmentFact: fact,
		ArgumentFileState:      ArgumentFileStateNotExpanded,
		ArgumentFileEvidence:   JestV1ReleaseEvidence,
		ZeroMatchEmitsArtifact: true,
	}
	if !binding.QualifiedV1() {
		t.Fatalf("fixture did not qualify: %#v", binding)
	}
	return ProducerInvocationBinding{Kind: ProducerInvocationJest, JestInvocation: &binding}
}

func TestCaptureSelectionRequiresExactlyOneQualifiedProducerBinding(t *testing.T) {
	jest := qualifiedJestSelectionBinding(t)
	argv := []string{"jest", "--runInBand", "--json", "--outputFile=reports/jest.json"}
	if !JestCandidateArgv(argv) {
		t.Fatal("qualified jest argv was not a syntactic candidate")
	}
	if got := SelectAdapterWithCapture("", argv, nil); got.Status != SelectionNone {
		t.Fatalf("unqualified auto selection=%#v", got)
	}
	if got := SelectAdapterWithCapture("", argv, &jest); got.Status != SelectionSelected || got.AdapterID != JestJSONAdapterID || got.Source != "qualified_jest_invocation" {
		t.Fatalf("qualified jest auto=%#v", got)
	}
	if got := SelectAdapterWithCapture(JestJSONAdapterID, argv, nil); got.Status != SelectionUnsupported || got.ObservationCode != "structured_adapter_precondition_failed" {
		t.Fatalf("explicit unqualified jest=%#v", got)
	}
	if got := SelectAdapterWithCapture(JestJSONAdapterID, argv, &jest); got.Status != SelectionSelected || got.Source != "explicit" {
		t.Fatalf("explicit qualified jest=%#v", got)
	}

	pytestRoot := t.TempDir()
	pytest := qualifiedPytestBinding(t, pytestRoot, environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"})
	both := jest
	both.PytestInvocation = &pytest
	if both.Validate() == nil {
		t.Fatal("two producer branches unexpectedly validated")
	}
	if got := SelectAdapterWithCapture("", argv, &both); got.Status != SelectionNone {
		t.Fatalf("invalid multi-producer union selected=%#v", got)
	}
	if got := SelectAdapterWithCapture("", []string{"node", "script.js"}, nil); got.Status != SelectionNone {
		t.Fatalf("non-producer argv selected=%#v", got)
	}
}

func TestJestAdapterAcceptsOnlyQualifiedCandidateShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		ok   bool
	}{
		{"qualified shape", []string{"jest", "--json", "--outputFile=reports/jest.json"}, true},
		{"missing json", []string{"jest", "--outputFile=reports/jest.json"}, false},
		{"missing output", []string{"jest", "--json"}, false},
		{"excluded", []string{"jest", "--json", "--outputFile=reports/jest.json", "--bail"}, false},
		{"argfile", []string{"jest", "@args.txt", "--json", "--outputFile=reports/jest.json"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AdapterAcceptsArgv(JestJSONAdapterID, tc.argv); got != tc.ok {
				t.Fatalf("got=%v want=%v", got, tc.ok)
			}
		})
	}
}
