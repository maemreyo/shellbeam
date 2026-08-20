package structuredresult

import (
	"context"
	"path/filepath"
	"testing"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

type recordingJestPresenceObserver struct {
	present bool
	names   []string
}

func (o *recordingJestPresenceObserver) ObserveEnvironmentPresence(_ context.Context, execution environment.ExecutionContext, name string) (EnvironmentPresenceFact, error) {
	o.names = append(o.names, name)
	return NewEnvironmentPresenceFact(execution, name, o.present)
}

func jestRequest(root string, argv []string) JestInvocationRequest {
	return JestInvocationRequest{
		Argv: argv, ResolvedCWD: filepath.Join(root, "pkg"), WorkspaceRoot: root,
		Execution: environment.ExecutionContext{Mode: "argv", Identity: "/repo/node_modules/.bin/jest"},
	}
}

func qualifyJest(t *testing.T, req JestInvocationRequest, observer *recordingJestPresenceObserver) JestInvocationBindingV1 {
	t.Helper()
	binding, qualified, err := QualifyJestInvocation(context.Background(), req, observer)
	if err != nil || !qualified {
		t.Fatalf("qualified=%v err=%v binding=%#v", qualified, err, binding)
	}
	return binding
}

func TestJestInvocationQualifiesDirectProducerAndOutputForms(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	inside := filepath.Join(root, "reports", "absolute.json")
	cases := []struct {
		name       string
		argv       []string
		declared   string
		normalized string
	}{
		{"equals", []string{"jest", "--json", "--outputFile=reports/a.json"}, "reports/a.json", "pkg/reports/a.json"},
		{"split", []string{"jest", "--json", "--outputFile", "reports/b.json"}, "reports/b.json", "pkg/reports/b.json"},
		{"node modules bin", []string{"node_modules/.bin/jest", "--json", "--outputFile=reports/c.json"}, "reports/c.json", "pkg/reports/c.json"},
		{"absolute inside", []string{"jest", "--json", "--outputFile=" + inside}, inside, "reports/absolute.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observer := &recordingJestPresenceObserver{}
			binding := qualifyJest(t, jestRequest(root, tc.argv), observer)
			if binding.SchemaVersion != JestInvocationSchemaV1 || binding.ProducerForm != JestProducerDirect || binding.JSONFlag != "--json" {
				t.Fatalf("binding=%#v", binding)
			}
			if binding.OutputFile.DeclaredPathToken != tc.declared || binding.OutputFile.NormalizedWorkspacePath != tc.normalized {
				t.Fatalf("output=%#v", binding.OutputFile)
			}
			if binding.ExcludedFlagState != JestExcludedFlagsAbsent || binding.ArgumentFileState != ArgumentFileStateNotExpanded || binding.ArgumentFileEvidence != JestV1ReleaseEvidence {
				t.Fatalf("authority fields=%#v", binding)
			}
			if binding.ZeroMatchEmitsArtifact || len(observer.names) != 1 || observer.names[0] != JestJasmineEnvironment {
				t.Fatalf("zero-match=%v observations=%v", binding.ZeroMatchEmitsArtifact, observer.names)
			}
		})
	}
}

func TestJestInvocationRejectsWrappersMissingFlagsAndExpansionPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	outside := filepath.Join(filepath.Dir(root), "outside.json")
	invalid := [][]string{
		{"jest", "--outputFile=out.json"},
		{"jest", "--json"},
		{"npm", "test", "--", "--json", "--outputFile=out.json"},
		{"npx", "jest", "--json", "--outputFile=out.json"},
		{"yarn", "jest", "--json", "--outputFile=out.json"},
		{"pnpm", "jest", "--json", "--outputFile=out.json"},
		{"bun", "jest", "--json", "--outputFile=out.json"},
		{"node", "./node_modules/jest/bin/jest.js", "--json", "--outputFile=out.json"},
		{"jest", "--json", "--outputFile=~/out.json"},
		{"jest", "--json", "--outputFile=$REPORT_DIR/out.json"},
		{"jest", "--json", "--outputFile=" + outside},
	}
	for _, argv := range invalid {
		observer := &recordingJestPresenceObserver{}
		_, qualified, err := QualifyJestInvocation(context.Background(), jestRequest(root, argv), observer)
		if err != nil || qualified {
			t.Fatalf("argv=%q qualified=%v err=%v", argv, qualified, err)
		}
	}
	request := jestRequest(root, []string{"jest", "--json", "--outputFile=out.json"})
	request.Execution = environment.ExecutionContext{Mode: "shell", Identity: "/bin/sh"}
	_, qualified, err := QualifyJestInvocation(context.Background(), request, &recordingJestPresenceObserver{})
	if err != nil || qualified {
		t.Fatalf("shell execution qualified=%v err=%v", qualified, err)
	}
}

func TestJestInvocationRejectsPayloadShapeAndCompletenessFlags(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	for _, flag := range []string{
		"--listTests", "--collectTests", "--watch", "--watchAll", "--bail", "-b", "--onlyFailures", "-o", "--shard=1/2", "--testResultsProcessor=processor.js",
	} {
		argv := []string{"jest", "--json", "--outputFile=out.json", flag}
		_, qualified, err := QualifyJestInvocation(context.Background(), jestRequest(root, argv), &recordingJestPresenceObserver{})
		if err != nil || qualified {
			t.Fatalf("flag=%q qualified=%v err=%v", flag, qualified, err)
		}
	}
}

// --listTests is a separate payload schema: with --outputFile Jest writes a
// path list even without --json, so it must never enter the JSON adapter.
func TestJestListTestsIsUnqualifiedEvenWhenOutputFileWouldBeHonored(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	argv := []string{"jest", "--listTests", "--outputFile=out.json"}
	_, qualified, err := QualifyJestInvocation(context.Background(), jestRequest(root, argv), &recordingJestPresenceObserver{})
	if err != nil || qualified {
		t.Fatalf("qualified=%v err=%v", qualified, err)
	}
}

func TestJestInvocationHonorsOptionArityAndTermination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	binding := qualifyJest(t, jestRequest(root, []string{
		"jest", "--testNamePattern", "--bail", "--json", "--outputFile=first.json", "--outputFile", "second.json", "--", "--watch", "--outputFile=ignored.json",
	}), &recordingJestPresenceObserver{})
	if binding.OutputFile.DeclaredPathToken != "second.json" || binding.OutputFile.NormalizedWorkspacePath != "pkg/second.json" {
		t.Fatalf("output=%#v", binding.OutputFile)
	}

	_, qualified, err := QualifyJestInvocation(context.Background(), jestRequest(root, []string{
		"jest", "--testNamePattern", "safe", "--", "--json", "--outputFile=too-late.json",
	}), &recordingJestPresenceObserver{})
	if err != nil || qualified {
		t.Fatalf("post-terminator authority qualified=%v err=%v", qualified, err)
	}
}

func TestJestJasminePresenceAuthorityIsCriticalAndAgentVariablesAreNotObserved(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	request := jestRequest(root, []string{"jest", "--json", "--outputFile=out.json"})
	absent := &recordingJestPresenceObserver{}
	binding := qualifyJest(t, request, absent)
	fact := binding.JasmineEnvironmentFact
	if fact.Name != JestJasmineEnvironment || fact.Present || fact.Execution != request.Execution || len(fact.AuthorityDigest) != 64 {
		t.Fatalf("fact=%#v", fact)
	}
	if len(absent.names) != 1 || absent.names[0] != JestJasmineEnvironment {
		t.Fatalf("agent/color environment was consulted: %v", absent.names)
	}
	present := &recordingJestPresenceObserver{present: true}
	_, qualified, err := QualifyJestInvocation(context.Background(), request, present)
	if err != nil || qualified || len(present.names) != 1 || present.names[0] != JestJasmineEnvironment {
		t.Fatalf("present qualified=%v names=%v err=%v", qualified, present.names, err)
	}
}

func TestJestProducerBindingDigestBindsQualifiedFacts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	base := qualifyJest(t, jestRequest(root, []string{"jest", "--json", "--outputFile=out.json"}), &recordingJestPresenceObserver{})
	baseDigest, err := base.ProducerBindingDigest()
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*JestInvocationBindingV1){
		func(b *JestInvocationBindingV1) { b.OutputFile.NormalizedWorkspacePath = "other.json" },
		func(b *JestInvocationBindingV1) { b.ExcludedFlagState = JestExcludedFlagsPresent },
		func(b *JestInvocationBindingV1) {
			b.JasmineEnvironmentFact, _ = NewEnvironmentPresenceFact(b.JasmineEnvironmentFact.Execution, JestJasmineEnvironment, true)
		},
	}
	for i, mutate := range mutations {
		changed := base
		mutate(&changed)
		digest, err := changed.ProducerBindingDigest()
		if err != nil || digest == baseDigest {
			t.Fatalf("mutation %d digest=%q base=%q err=%v", i, digest, baseDigest, err)
		}
	}
}

func TestJestProducerInvocationUnionIsClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	jest := qualifyJest(t, jestRequest(root, []string{"jest", "--json", "--outputFile=out.json"}), &recordingJestPresenceObserver{})
	pytest := qualifiedPytestBinding(t, root, environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"})
	valid := ProducerInvocationBinding{Kind: ProducerInvocationJest, JestInvocation: &jest}
	if err := valid.Validate(); err != nil || valid.AdapterID() != JestJSONAdapterID || valid.OutputBinding() != jest.OutputFile {
		t.Fatalf("valid jest union rejected: %#v err=%v", valid, err)
	}
	invalid := []ProducerInvocationBinding{
		{Kind: ProducerInvocationJest},
		{Kind: ProducerInvocationPytest, JestInvocation: &jest},
		{Kind: ProducerInvocationJest, PytestInvocation: &pytest, JestInvocation: &jest},
	}
	for i, candidate := range invalid {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid union %d accepted: %#v", i, candidate)
		}
	}
}

func TestJestCandidateArgvRejectsAtTokensWithoutRuntimeVersionAttestation(t *testing.T) {
	qualified := [][]string{
		{"jest", "--json", "--outputFile=out.json"},
	}
	for _, argv := range qualified {
		if !JestCandidateArgv(argv) {
			t.Fatalf("candidate rejected: %q", argv)
		}
	}
	for _, argv := range [][]string{
		{"node_modules/.bin/jest", "@acme", "--json", "--outputFile", "out.json"},
		{"jest", "tests", "@args.txt", "--json", "--outputFile=out.json"},
		{"npx", "jest", "--json", "--outputFile=out.json"},
		{"jest", "--outputFile=out.json"},
		{"jest", "--json", "--outputFile=out.json", "--bail"},
	} {
		if JestCandidateArgv(argv) {
			t.Fatalf("candidate accepted: %q", argv)
		}
	}
}
