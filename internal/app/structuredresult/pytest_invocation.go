package structuredresult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	environment "github.com/maemreyo/shellbeam/internal/core/environment"
)

const (
	PytestInvocationSchemaV1             = 1
	EnvironmentPresenceAuthoritySchemaV1 = 1
	PytestProducerDirect                 = "pytest"
	PytestProducerPythonModule           = "python-m-pytest"
	PytestArgumentFileNone               = "none"
	PytestAddoptsEnvironment             = "PYTEST_ADDOPTS"
	PytestJUnitAdapterID                 = "pytest-junit-xml"
	maxPytestArgvItems                   = 256
	maxPytestTokenBytes                  = 4096
)

var environmentPresenceNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type EnvironmentPresenceFact struct {
	Name                   string                       `json:"name"`
	Present                bool                         `json:"present"`
	AuthoritySchemaVersion int                          `json:"authority_schema_version"`
	Execution              environment.ExecutionContext `json:"execution"`
	AuthorityDigest        string                       `json:"authority_digest"`
}

type EnvironmentPresenceObserver interface {
	ObserveEnvironmentPresence(context.Context, environment.ExecutionContext, string) (EnvironmentPresenceFact, error)
}

type JUnitOutputBinding struct {
	DeclaredPathToken       string `json:"declared_path_token"`
	NormalizedWorkspacePath string `json:"normalized_workspace_path"`
}

type PytestInvocationBindingV1 struct {
	SchemaVersion                int                     `json:"schema_version"`
	ProducerForm                 string                  `json:"producer_form"`
	JUnitOutput                  JUnitOutputBinding      `json:"junit_output"`
	JUnitFamilyOverride          string                  `json:"junit_family_override"`
	ConfigAddoptsOverride        string                  `json:"config_addopts_override"`
	ArgumentFileState            string                  `json:"argument_file_state"`
	PytestAddoptsEnvironmentFact EnvironmentPresenceFact `json:"pytest_addopts_environment_fact"`
}

type PytestInvocationRequest struct {
	Argv          []string
	ResolvedCWD   string
	WorkspaceRoot string
	Execution     environment.ExecutionContext
}

func NewEnvironmentPresenceFact(execution environment.ExecutionContext, name string, present bool) (EnvironmentPresenceFact, error) {
	if err := validatePresenceExecution(execution); err != nil {
		return EnvironmentPresenceFact{}, err
	}
	if !environmentPresenceNamePattern.MatchString(name) {
		return EnvironmentPresenceFact{}, fmt.Errorf("invalid environment presence name")
	}
	canonical := struct {
		Version   int                          `json:"version"`
		Execution environment.ExecutionContext `json:"execution"`
		Name      string                       `json:"name"`
		Present   bool                         `json:"present"`
	}{Version: EnvironmentPresenceAuthoritySchemaV1, Execution: execution, Name: name, Present: present}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return EnvironmentPresenceFact{}, err
	}
	return EnvironmentPresenceFact{
		Name: name, Present: present, AuthoritySchemaVersion: EnvironmentPresenceAuthoritySchemaV1,
		Execution: execution, AuthorityDigest: digestStructuredAuthority(encoded),
	}, nil
}

func (f EnvironmentPresenceFact) Validate() error {
	if f.AuthoritySchemaVersion != EnvironmentPresenceAuthoritySchemaV1 || !environmentPresenceNamePattern.MatchString(f.Name) || !validStructuredAuthorityDigest(f.AuthorityDigest) {
		return fmt.Errorf("invalid environment presence authority")
	}
	if err := validatePresenceExecution(f.Execution); err != nil {
		return err
	}
	want, err := NewEnvironmentPresenceFact(f.Execution, f.Name, f.Present)
	if err != nil || want.AuthorityDigest != f.AuthorityDigest {
		return fmt.Errorf("environment presence authority digest mismatch")
	}
	return nil
}

func (b JUnitOutputBinding) Validate() error {
	if !boundedAuthorityText(b.DeclaredPathToken, maxPytestTokenBytes) || !validNormalizedCapturePath(b.NormalizedWorkspacePath) {
		return fmt.Errorf("invalid junit output binding")
	}
	return nil
}

func (b PytestInvocationBindingV1) Validate() error {
	if b.SchemaVersion != PytestInvocationSchemaV1 || b.JUnitOutput.Validate() != nil || b.PytestAddoptsEnvironmentFact.Validate() != nil || b.PytestAddoptsEnvironmentFact.Name != PytestAddoptsEnvironment {
		return fmt.Errorf("invalid pytest invocation binding")
	}
	switch b.ProducerForm {
	case PytestProducerDirect, PytestProducerPythonModule:
	default:
		return fmt.Errorf("invalid pytest producer form")
	}
	if !boundedAuthorityText(b.JUnitFamilyOverride, 1024) || !strings.HasPrefix(b.JUnitFamilyOverride, "junit_family=") ||
		!boundedAuthorityText(b.ConfigAddoptsOverride, 1024) || !strings.HasPrefix(b.ConfigAddoptsOverride, "addopts=") ||
		!boundedAuthorityText(b.ArgumentFileState, 64) {
		return fmt.Errorf("invalid pytest invocation authority fields")
	}
	return nil
}

func (b PytestInvocationBindingV1) QualifiedV1() bool {
	return b.Validate() == nil && b.JUnitFamilyOverride == "junit_family=xunit2" && b.ConfigAddoptsOverride == "addopts=" && b.ArgumentFileState == PytestArgumentFileNone && !b.PytestAddoptsEnvironmentFact.Present
}

func (b PytestInvocationBindingV1) ProducerBindingDigest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return digestStructuredAuthority(encoded), nil
}

func QualifyPytestInvocation(ctx context.Context, req PytestInvocationRequest, observer EnvironmentPresenceObserver) (PytestInvocationBindingV1, bool, error) {
	if err := ctx.Err(); err != nil {
		return PytestInvocationBindingV1{}, false, err
	}
	if err := validatePytestInvocationRequest(req); err != nil {
		return PytestInvocationBindingV1{}, false, err
	}
	if req.Execution.Mode != "argv" {
		return PytestInvocationBindingV1{}, false, nil
	}
	producer, args, ok := pytestProducer(req.Argv)
	if !ok {
		return PytestInvocationBindingV1{}, false, nil
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "@") {
			return PytestInvocationBindingV1{}, false, nil
		}
	}
	resolved, ok := resolvePytestAuthorityArgs(args)
	if !ok || resolved.junitPath == "" || resolved.junitFamily != "junit_family=xunit2" || resolved.addopts != "addopts=" {
		return PytestInvocationBindingV1{}, false, nil
	}
	output, ok := bindJUnitOutputPath(resolved.junitPath, req.ResolvedCWD, req.WorkspaceRoot)
	if !ok {
		return PytestInvocationBindingV1{}, false, nil
	}
	if observer == nil {
		return PytestInvocationBindingV1{}, false, fmt.Errorf("pytest environment presence observer unavailable")
	}
	fact, err := observer.ObserveEnvironmentPresence(ctx, req.Execution, PytestAddoptsEnvironment)
	if err != nil {
		return PytestInvocationBindingV1{}, false, err
	}
	if err := fact.Validate(); err != nil || fact.Name != PytestAddoptsEnvironment || fact.Execution != req.Execution {
		if err == nil {
			err = fmt.Errorf("pytest environment presence authority mismatch")
		}
		return PytestInvocationBindingV1{}, false, err
	}
	binding := PytestInvocationBindingV1{
		SchemaVersion: PytestInvocationSchemaV1, ProducerForm: producer, JUnitOutput: output,
		JUnitFamilyOverride: resolved.junitFamily, ConfigAddoptsOverride: resolved.addopts,
		ArgumentFileState: PytestArgumentFileNone, PytestAddoptsEnvironmentFact: fact,
	}
	return binding, binding.QualifiedV1(), nil
}

type pytestResolvedAuthority struct {
	junitPath   string
	junitFamily string
	addopts     string
}

func resolvePytestAuthorityArgs(args []string) (pytestResolvedAuthority, bool) {
	var out pytestResolvedAuthority
	terminated := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if terminated {
			continue
		}
		if arg == "--" {
			terminated = true
			continue
		}
		switch {
		case arg == "--junitxml" || arg == "--junit-xml":
			if i+1 >= len(args) {
				return pytestResolvedAuthority{}, false
			}
			i++
			out.junitPath = args[i]
		case strings.HasPrefix(arg, "--junitxml="):
			out.junitPath = strings.TrimPrefix(arg, "--junitxml=")
		case strings.HasPrefix(arg, "--junit-xml="):
			out.junitPath = strings.TrimPrefix(arg, "--junit-xml=")
		case arg == "-o" || arg == "--override-ini":
			if i+1 >= len(args) {
				return pytestResolvedAuthority{}, false
			}
			i++
			applyPytestOverride(&out, args[i])
		case strings.HasPrefix(arg, "--override-ini="):
			applyPytestOverride(&out, strings.TrimPrefix(arg, "--override-ini="))
		default:
			arity := pytestKnownOptionArity(arg)
			if arity < 0 {
				return pytestResolvedAuthority{}, false
			}
			if arity == 1 {
				if i+1 >= len(args) {
					return pytestResolvedAuthority{}, false
				}
				i++
			}
		}
	}
	return out, true
}

func applyPytestOverride(out *pytestResolvedAuthority, value string) {
	switch {
	case strings.HasPrefix(value, "junit_family="):
		out.junitFamily = value
	case strings.HasPrefix(value, "addopts="):
		out.addopts = value
	}
}

// pytestKnownOptionArity is deliberately bounded to standard pytest options.
// Unknown option spellings fail qualification rather than letting a plugin or
// future parser extension make an option value look like ShellBeam authority.
func pytestKnownOptionArity(arg string) int {
	if arg == "" || arg[0] != '-' || arg == "-" {
		return 0
	}
	if strings.Contains(arg, "=") {
		return 0
	}
	if strings.HasPrefix(arg, "-k") && arg != "-k" || strings.HasPrefix(arg, "-m") && arg != "-m" || strings.HasPrefix(arg, "-r") && arg != "-r" {
		return 0
	}
	valueOptions := map[string]struct{}{
		"-k": {}, "-m": {}, "-r": {}, "--maxfail": {}, "--confcutdir": {}, "--ignore": {}, "--ignore-glob": {}, "--deselect": {},
		"--import-mode": {}, "--doctest-glob": {}, "--basetemp": {}, "--durations": {}, "--durations-min": {}, "--rootdir": {},
		"--tb": {}, "--capture": {}, "--color": {}, "--code-highlight": {}, "--pastebin": {}, "--assert": {}, "--verbosity": {},
		"--pdbcls": {}, "--junit-prefix": {}, "--log-level": {}, "--log-format": {}, "--log-date-format": {}, "--log-cli-level": {},
		"--log-cli-format": {}, "--log-cli-date-format": {}, "--log-file": {}, "--log-file-mode": {}, "--log-file-level": {}, "--log-file-format": {},
		"--log-file-date-format": {}, "--lfnf": {}, "--cache-show": {},
	}
	if _, ok := valueOptions[arg]; ok {
		return 1
	}
	flags := map[string]struct{}{
		"-x": {}, "--exitfirst": {}, "--fixtures": {}, "--funcargs": {}, "--fixtures-per-test": {}, "--pdb": {}, "--trace": {}, "--full-trace": {},
		"--no-header": {}, "--no-summary": {}, "-q": {}, "--quiet": {}, "-v": {}, "--verbose": {}, "-s": {}, "--lf": {}, "--last-failed": {},
		"--ff": {}, "--failed-first": {}, "--nf": {}, "--new-first": {}, "--cache-clear": {}, "--sw": {}, "--stepwise": {}, "--sw-skip": {},
		"--stepwise-skip": {}, "--collect-only": {}, "--co": {}, "--pyargs": {}, "--doctest-modules": {}, "--doctest-continue-on-failure": {},
		"--continue-on-collection-errors": {}, "--collect-in-virtualenv": {}, "--noconftest": {}, "--keep-duplicates": {}, "--showlocals": {}, "-l": {},
		"--disable-warnings": {}, "--disable-pytest-warnings": {}, "--strict-config": {}, "--strict-markers": {}, "--strict": {}, "--runxfail": {},
		"--setup-only": {}, "--setup-show": {}, "--setup-plan": {}, "--log-cli": {},
	}
	if _, ok := flags[arg]; ok {
		return 0
	}
	return -1
}

func pytestProducer(argv []string) (string, []string, bool) {
	if len(argv) >= 1 && argv[0] == "pytest" {
		return PytestProducerDirect, argv[1:], true
	}
	if len(argv) >= 3 && argv[0] == "python" && argv[1] == "-m" && argv[2] == "pytest" {
		return PytestProducerPythonModule, argv[3:], true
	}
	return "", nil, false
}

func bindJUnitOutputPath(token, resolvedCWD, workspaceRoot string) (JUnitOutputBinding, bool) {
	if !boundedAuthorityText(token, maxPytestTokenBytes) || strings.HasPrefix(token, "~") || strings.ContainsAny(token, "$%") {
		return JUnitOutputBinding{}, false
	}
	candidate := token
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(resolvedCWD, candidate)
	}
	candidate = filepath.Clean(candidate)
	root := filepath.Clean(workspaceRoot)
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return JUnitOutputBinding{}, false
	}
	normalized := filepath.ToSlash(rel)
	binding := JUnitOutputBinding{DeclaredPathToken: token, NormalizedWorkspacePath: normalized}
	if binding.Validate() != nil {
		return JUnitOutputBinding{}, false
	}
	return binding, true
}

func validatePytestInvocationRequest(req PytestInvocationRequest) error {
	if len(req.Argv) == 0 || len(req.Argv) > maxPytestArgvItems || !filepath.IsAbs(req.ResolvedCWD) || filepath.Clean(req.ResolvedCWD) != req.ResolvedCWD ||
		!filepath.IsAbs(req.WorkspaceRoot) || filepath.Clean(req.WorkspaceRoot) != req.WorkspaceRoot {
		return fmt.Errorf("invalid pytest invocation request")
	}
	if err := validatePresenceExecution(req.Execution); err != nil {
		return err
	}
	for _, arg := range req.Argv {
		if !boundedAuthorityTextAllowEmpty(arg, maxPytestTokenBytes) {
			return fmt.Errorf("invalid pytest argv token")
		}
	}
	return nil
}

func validatePresenceExecution(execution environment.ExecutionContext) error {
	if execution.Mode != "argv" && execution.Mode != "shell" || !boundedAuthorityText(execution.Identity, maxPytestTokenBytes) {
		return fmt.Errorf("invalid environment presence execution")
	}
	return nil
}

func validNormalizedCapturePath(value string) bool {
	if !boundedAuthorityText(value, maxPytestTokenBytes) || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func boundedAuthorityText(value string, max int) bool {
	return value != "" && boundedAuthorityTextAllowEmpty(value, max)
}

func boundedAuthorityTextAllowEmpty(value string, max int) bool {
	if len(value) > max || strings.ContainsRune(value, 0) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validStructuredAuthorityDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func digestStructuredAuthority(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
