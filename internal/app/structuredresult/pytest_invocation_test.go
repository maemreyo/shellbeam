package structuredresult

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

type fakePytestPresenceObserver struct {
	present bool
	calls   int
}

func (f *fakePytestPresenceObserver) ObserveEnvironmentPresence(_ context.Context, execution environment.ExecutionContext, name string) (EnvironmentPresenceFact, error) {
	f.calls++
	return NewEnvironmentPresenceFact(execution, name, f.present)
}

func TestPytestInvocationQualifiesExactProducerFormsAndJUnitOptions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(root, "pkg")
	cases := []struct {
		name         string
		argv         []string
		producerForm string
		declared     string
		normalized   string
	}{
		{"pytest long legacy spelling split", []string{"pytest", "--junitxml", "reports/a.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, PytestProducerDirect, "reports/a.xml", "pkg/reports/a.xml"},
		{"pytest long legacy spelling equals", []string{"pytest", "--junitxml=reports/b.xml", "--override-ini", "junit_family=xunit2", "--override-ini", "addopts="}, PytestProducerDirect, "reports/b.xml", "pkg/reports/b.xml"},
		{"python module modern spelling split", []string{"python", "-m", "pytest", "--junit-xml", "reports/c.xml", "--override-ini=junit_family=xunit2", "--override-ini=addopts="}, PytestProducerPythonModule, "reports/c.xml", "pkg/reports/c.xml"},
		{"python module modern spelling equals", []string{"python", "-m", "pytest", "--junit-xml=reports/d.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, PytestProducerPythonModule, "reports/d.xml", "pkg/reports/d.xml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observer := &fakePytestPresenceObserver{}
			got, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{
				Argv: tc.argv, ResolvedCWD: cwd, WorkspaceRoot: root,
				Execution: environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"},
			}, observer)
			if err != nil || !qualified {
				t.Fatalf("qualified=%v err=%v binding=%#v", qualified, err, got)
			}
			if observer.calls != 1 {
				t.Fatalf("presence observations=%d", observer.calls)
			}
			if got.SchemaVersion != PytestInvocationSchemaV1 || got.ProducerForm != tc.producerForm || got.JUnitOutput.DeclaredPathToken != tc.declared || got.JUnitOutput.NormalizedWorkspacePath != tc.normalized {
				t.Fatalf("binding=%#v", got)
			}
			if got.JUnitFamilyOverride != "junit_family=xunit2" || got.ConfigAddoptsOverride != "addopts=" || got.ArgumentFileState != PytestArgumentFileNone {
				t.Fatalf("authority fields=%#v", got)
			}
			if got.PytestAddoptsEnvironmentFact.Name != PytestAddoptsEnvironment || got.PytestAddoptsEnvironmentFact.Present || got.PytestAddoptsEnvironmentFact.AuthorityDigest == "" {
				t.Fatalf("environment fact=%#v", got.PytestAddoptsEnvironmentFact)
			}
		})
	}
}

func TestPytestInvocationUsesEffectiveFinalValuesAndOptionTermination(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(root, "sub")
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	observer := &fakePytestPresenceObserver{}

	binding, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{
		Argv:        []string{"pytest", "--junitxml=first.xml", "--junit-xml", "second.xml", "-o", "junit_family=legacy", "-o", "junit_family=xunit2", "-o", "addopts=-q", "-o", "addopts=", "--", "--junitxml=ignored.xml", "-o", "junit_family=legacy"},
		ResolvedCWD: cwd, WorkspaceRoot: root, Execution: execution,
	}, observer)
	if err != nil || !qualified {
		t.Fatalf("qualified=%v err=%v binding=%#v", qualified, err, binding)
	}
	if binding.JUnitOutput.DeclaredPathToken != "second.xml" || binding.JUnitOutput.NormalizedWorkspacePath != "sub/second.xml" {
		t.Fatalf("junit output=%#v", binding.JUnitOutput)
	}

	_, qualified, err = QualifyPytestInvocation(context.Background(), PytestInvocationRequest{
		Argv:        []string{"pytest", "-o", "junit_family=xunit2", "-o", "addopts=", "--", "--junitxml=too-late.xml"},
		ResolvedCWD: cwd, WorkspaceRoot: root, Execution: execution,
	}, &fakePytestPresenceObserver{})
	if err != nil || qualified {
		t.Fatalf("post-terminator junit option qualified=%v err=%v", qualified, err)
	}
}

func TestPytestInvocationHonorsOptionArityAndRejectsArgumentFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	request := PytestInvocationRequest{
		Argv:        []string{"pytest", "-k", "junitxml", "--junitxml=real.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
		ResolvedCWD: root, WorkspaceRoot: root,
		Execution: environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"},
	}
	binding, qualified, err := QualifyPytestInvocation(context.Background(), request, &fakePytestPresenceObserver{})
	if err != nil || !qualified || binding.JUnitOutput.DeclaredPathToken != "real.xml" {
		t.Fatalf("qualified=%v err=%v binding=%#v", qualified, err, binding)
	}

	for _, argv := range [][]string{
		{"pytest", "@args.txt", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
		{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts=", "--", "@tests.txt"},
	} {
		request.Argv = argv
		_, qualified, err = QualifyPytestInvocation(context.Background(), request, &fakePytestPresenceObserver{})
		if err != nil || qualified {
			t.Fatalf("argument file argv=%q qualified=%v err=%v", argv, qualified, err)
		}
	}
}

func TestPytestInvocationRejectsWrappersMissingAuthorityAndExpansionPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	validTail := []string{"--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts="}
	invalid := [][]string{
		append([]string{"poetry", "run", "pytest"}, validTail...),
		append([]string{"uv", "run", "pytest"}, validTail...),
		append([]string{"python", "script_that_calls_pytest.py"}, validTail...),
		append([]string{"bash", "-c", "pytest --junitxml=out.xml"}, validTail[1:]...),
		{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2"},
		{"pytest", "--junitxml=out.xml", "-o", "addopts="},
		{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts=-q"},
		{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts=", "-o", "junit_family=legacy"},
		{"pytest", "--junitxml=~/out.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
		{"pytest", "--junitxml=$REPORT_DIR/out.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
	}
	for _, argv := range invalid {
		observer := &fakePytestPresenceObserver{}
		_, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{Argv: argv, ResolvedCWD: root, WorkspaceRoot: root, Execution: execution}, observer)
		if err != nil || qualified {
			t.Fatalf("argv=%q qualified=%v err=%v", argv, qualified, err)
		}
	}
}

func TestPytestInvocationPathAuthorityUsesFrozenResolvedCWDAndWorkspaceContainment(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	cwd := filepath.Join(root, "nested")
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	base := []string{"pytest", "-o", "junit_family=xunit2", "-o", "addopts="}
	for _, tc := range []struct {
		name       string
		path       string
		qualified  bool
		normalized string
	}{
		{"relative", filepath.Join("reports", "junit.xml"), true, "nested/reports/junit.xml"},
		{"absolute inside", filepath.Join(root, "reports", "junit.xml"), true, "reports/junit.xml"},
		{"absolute outside", filepath.Join(filepath.Dir(root), "outside.xml"), false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := append(append([]string(nil), base...), "--junitxml="+tc.path)
			binding, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{Argv: argv, ResolvedCWD: cwd, WorkspaceRoot: root, Execution: execution}, &fakePytestPresenceObserver{})
			if err != nil || qualified != tc.qualified {
				t.Fatalf("qualified=%v want=%v err=%v binding=%#v", qualified, tc.qualified, err, binding)
			}
			if qualified && binding.JUnitOutput.NormalizedWorkspacePath != tc.normalized {
				t.Fatalf("normalized=%q want=%q", binding.JUnitOutput.NormalizedWorkspacePath, tc.normalized)
			}
		})
	}
}

func TestPytestAddoptsPresenceAuthorityIsSecretFreeAndQualificationCritical(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	execution := environment.ExecutionContext{Mode: "argv", Identity: "/usr/bin/pytest"}
	request := PytestInvocationRequest{Argv: []string{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts="}, ResolvedCWD: root, WorkspaceRoot: root, Execution: execution}
	absent := &fakePytestPresenceObserver{}
	binding, qualified, err := QualifyPytestInvocation(context.Background(), request, absent)
	if err != nil || !qualified || absent.calls != 1 {
		t.Fatalf("absent qualified=%v err=%v calls=%d", qualified, err, absent.calls)
	}
	fact := binding.PytestAddoptsEnvironmentFact
	if fact.Name != PytestAddoptsEnvironment || fact.Present || fact.Execution != execution || fact.AuthoritySchemaVersion != EnvironmentPresenceAuthoritySchemaV1 || len(fact.AuthorityDigest) != 64 {
		t.Fatalf("fact=%#v", fact)
	}
	encoded := strings.ToLower(fact.AuthorityDigest)
	if strings.Contains(encoded, "secret") {
		t.Fatal("authority fact unexpectedly contains environment value")
	}
	present := &fakePytestPresenceObserver{present: true}
	_, qualified, err = QualifyPytestInvocation(context.Background(), request, present)
	if err != nil || qualified || present.calls != 1 {
		t.Fatalf("present qualified=%v err=%v calls=%d", qualified, err, present.calls)
	}
}

func TestPytestInvocationRejectsNonArgvExecutionContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	observer := &fakePytestPresenceObserver{}
	_, qualified, err := QualifyPytestInvocation(context.Background(), PytestInvocationRequest{
		Argv:        []string{"pytest", "--junitxml=out.xml", "-o", "junit_family=xunit2", "-o", "addopts="},
		ResolvedCWD: root, WorkspaceRoot: root,
		Execution: environment.ExecutionContext{Mode: "shell", Identity: "/bin/sh"},
	}, observer)
	if err != nil || qualified {
		t.Fatalf("shell execution qualified=%v err=%v", qualified, err)
	}
	if observer.calls != 0 {
		t.Fatalf("shell execution observed environment %d times", observer.calls)
	}
}
