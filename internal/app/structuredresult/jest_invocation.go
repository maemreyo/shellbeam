package structuredresult

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

const (
	JestInvocationSchemaV1       = 1
	JestJSONAdapterID            = "jest-json"
	JestJasmineEnvironment       = "JEST_JASMINE"
	JestProducerDirect           = "jest"
	ArgumentFileStateNotExpanded = "producer_does_not_expand"
	JestExcludedFlagsAbsent      = "evaluated_absent"
	JestExcludedFlagsPresent     = "evaluated_present"
	JestV1ReleaseEvidence        = "jest-v1:29.7.0,30.4.1"
	maxJestArgvItems             = 256
	maxJestTokenBytes            = 4096
)

type JestInvocationBindingV1 struct {
	SchemaVersion          int                     `json:"schema_version"`
	ProducerForm           string                  `json:"producer_form"`
	JSONFlag               string                  `json:"json_flag"`
	OutputFile             CaptureOutputBinding    `json:"output_file"`
	ExcludedFlagState      string                  `json:"excluded_flag_state"`
	JasmineEnvironmentFact EnvironmentPresenceFact `json:"jasmine_environment_fact"`
	ArgumentFileState      string                  `json:"argument_file_state"`
	ArgumentFileEvidence   string                  `json:"argument_file_evidence"`
	ZeroMatchEmitsArtifact bool                    `json:"zero_match_emits_artifact"`
}

type JestInvocationRequest struct {
	Argv          []string
	ResolvedCWD   string
	WorkspaceRoot string
	Execution     environment.ExecutionContext
}

func (b JestInvocationBindingV1) Validate() error {
	if b.SchemaVersion != JestInvocationSchemaV1 || b.ProducerForm != JestProducerDirect || b.JSONFlag != "--json" || b.OutputFile.Validate() != nil {
		return fmt.Errorf("invalid jest invocation binding")
	}
	if b.ExcludedFlagState != JestExcludedFlagsAbsent && b.ExcludedFlagState != JestExcludedFlagsPresent {
		return fmt.Errorf("invalid jest excluded flag state")
	}
	if b.JasmineEnvironmentFact.Validate() != nil || b.JasmineEnvironmentFact.Name != JestJasmineEnvironment {
		return fmt.Errorf("invalid jest jasmine environment fact")
	}
	if b.ArgumentFileState != ArgumentFileStateNotExpanded || b.ArgumentFileEvidence != JestV1ReleaseEvidence || !b.ZeroMatchEmitsArtifact {
		return fmt.Errorf("invalid jest release behavior facts")
	}
	return nil
}

func (b JestInvocationBindingV1) QualifiedV1() bool {
	return b.Validate() == nil && b.ExcludedFlagState == JestExcludedFlagsAbsent && !b.JasmineEnvironmentFact.Present
}

func (b JestInvocationBindingV1) ProducerBindingDigest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return digestStructuredAuthority(encoded), nil
}

func QualifyJestInvocation(ctx context.Context, req JestInvocationRequest, observer EnvironmentPresenceObserver) (JestInvocationBindingV1, bool, error) {
	if err := ctx.Err(); err != nil {
		return JestInvocationBindingV1{}, false, err
	}
	if err := validateJestInvocationRequest(req); err != nil {
		return JestInvocationBindingV1{}, false, err
	}
	if req.Execution.Mode != "argv" {
		return JestInvocationBindingV1{}, false, nil
	}
	_, args, ok := jestProducer(req.Argv)
	if !ok {
		return JestInvocationBindingV1{}, false, nil
	}
	if jestContainsAtToken(args) {
		return JestInvocationBindingV1{}, false, nil
	}
	resolved, ok := resolveJestAuthorityArgs(args)
	if !ok || !resolved.json || resolved.outputFile == "" || resolved.excluded {
		return JestInvocationBindingV1{}, false, nil
	}
	output, ok := bindJUnitOutputPath(resolved.outputFile, req.ResolvedCWD, req.WorkspaceRoot)
	if !ok {
		return JestInvocationBindingV1{}, false, nil
	}
	if observer == nil {
		return JestInvocationBindingV1{}, false, fmt.Errorf("jest environment presence observer unavailable")
	}
	fact, err := observer.ObserveEnvironmentPresence(ctx, req.Execution, JestJasmineEnvironment)
	if err != nil {
		return JestInvocationBindingV1{}, false, err
	}
	if err := fact.Validate(); err != nil || fact.Name != JestJasmineEnvironment || fact.Execution != req.Execution {
		if err == nil {
			err = fmt.Errorf("jest environment presence authority mismatch")
		}
		return JestInvocationBindingV1{}, false, err
	}
	binding := JestInvocationBindingV1{
		SchemaVersion: JestInvocationSchemaV1, ProducerForm: JestProducerDirect, JSONFlag: "--json", OutputFile: output,
		ExcludedFlagState: JestExcludedFlagsAbsent, JasmineEnvironmentFact: fact,
		ArgumentFileState: ArgumentFileStateNotExpanded, ArgumentFileEvidence: JestV1ReleaseEvidence,
		ZeroMatchEmitsArtifact: true,
	}
	return binding, binding.QualifiedV1(), nil
}

type jestResolvedAuthority struct {
	json       bool
	outputFile string
	excluded   bool
}

func resolveJestAuthorityArgs(args []string) (jestResolvedAuthority, bool) {
	var out jestResolvedAuthority
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		switch {
		case arg == "--json":
			out.json = true
		case arg == "--outputFile":
			if i+1 >= len(args) {
				return jestResolvedAuthority{}, false
			}
			i++
			out.outputFile = args[i]
		case strings.HasPrefix(arg, "--outputFile="):
			out.outputFile = strings.TrimPrefix(arg, "--outputFile=")
		case jestExcludedFlag(arg):
			out.excluded = true
		default:
			arity := jestKnownOptionArity(arg)
			if arity < 0 {
				return jestResolvedAuthority{}, false
			}
			if arity == 1 {
				if i+1 >= len(args) {
					return jestResolvedAuthority{}, false
				}
				i++
			}
		}
	}
	return out, true
}

func jestExcludedFlag(arg string) bool {
	for _, flag := range []string{
		"--listTests", "--collectTests", "--watch", "--watchAll", "--bail", "-b", "--onlyFailures", "-o", "--shard", "--testResultsProcessor",
	} {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func jestKnownOptionArity(arg string) int {
	if arg == "" || arg == "-" || arg[0] != '-' {
		return 0
	}
	if strings.Contains(arg, "=") {
		name := strings.SplitN(arg, "=", 2)[0]
		if jestValueOption(name) || jestFlagOption(name) {
			return 0
		}
		return -1
	}
	if jestValueOption(arg) {
		return 1
	}
	if jestFlagOption(arg) {
		return 0
	}
	return -1
}

func jestValueOption(arg string) bool {
	switch arg {
	case "--testNamePattern", "-t", "--testPathPattern", "--testPathPatterns", "--rootDir", "--config", "-c", "--maxWorkers", "-w", "--seed", "--testTimeout", "--coverageDirectory", "--coverageProvider":
		return true
	default:
		return false
	}
}

func jestFlagOption(arg string) bool {
	switch arg {
	case "--runInBand", "-i", "--randomize", "--showSeed", "--ci", "--colors", "--coverage", "--detectOpenHandles", "--forceExit", "--logHeapUsage", "--silent", "--verbose", "--passWithNoTests", "--runTestsByPath", "--findRelatedTests", "--expand", "-e", "--noStackTrace":
		return true
	default:
		return false
	}
}

func jestProducer(argv []string) (string, []string, bool) {
	if len(argv) == 0 || filepath.Base(filepath.Clean(argv[0])) != "jest" {
		return "", nil, false
	}
	return JestProducerDirect, argv[1:], true
}

func JestCandidateArgv(argv []string) bool {
	_, args, ok := jestProducer(argv)
	if !ok || jestContainsAtToken(args) {
		return false
	}
	resolved, ok := resolveJestAuthorityArgs(args)
	return ok && resolved.json && resolved.outputFile != "" && !resolved.excluded
}

func jestContainsAtToken(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "@") {
			return true
		}
	}
	return false
}

func validateJestInvocationRequest(req JestInvocationRequest) error {
	if len(req.Argv) == 0 || len(req.Argv) > maxJestArgvItems || !filepath.IsAbs(req.ResolvedCWD) || filepath.Clean(req.ResolvedCWD) != req.ResolvedCWD ||
		!filepath.IsAbs(req.WorkspaceRoot) || filepath.Clean(req.WorkspaceRoot) != req.WorkspaceRoot {
		return fmt.Errorf("invalid jest invocation request")
	}
	if err := validatePresenceExecution(req.Execution); err != nil {
		return err
	}
	for _, arg := range req.Argv {
		if !boundedAuthorityTextAllowEmpty(arg, maxJestTokenBytes) {
			return fmt.Errorf("invalid jest argv token")
		}
	}
	return nil
}
