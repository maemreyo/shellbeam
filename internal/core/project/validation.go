package project

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateRaw(raw rawManifest) (Manifest, error) {
	if len(raw.Commands) > MaxCommands || len(raw.Verification.Profiles) > MaxVerificationProfiles ||
		len(raw.Environment.RelevantPresence) > MaxRelevantEnvironment || len(raw.Outputs) > MaxExpectedOutputs {
		return Manifest{}, projectError(CodeLimitExceeded, "manifest collection limit exceeded")
	}
	if !boundedOptional(raw.Project.Name) {
		return Manifest{}, projectError(CodeSchemaError, "invalid project name")
	}
	toolchains, err := validateToolchains(raw.Toolchains)
	if err != nil {
		return Manifest{}, err
	}
	commands, err := validateCommands(raw.Commands)
	if err != nil {
		return Manifest{}, err
	}
	profiles, err := validateVerificationProfiles(raw.Verification.Profiles, commands)
	if err != nil {
		return Manifest{}, err
	}
	environment, err := validateRelevantEnvironment(raw.Environment.RelevantPresence)
	if err != nil {
		return Manifest{}, err
	}
	outputs, err := validateOutputs(raw.Outputs)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateCommandGraph(commands); err != nil {
		return Manifest{}, err
	}
	return Manifest{
		SchemaVersion: ManifestSchemaV1, Project: Project{Name: raw.Project.Name}, Toolchains: toolchains,
		Commands: commands, VerificationProfiles: profiles, RelevantEnvironment: environment, Outputs: outputs,
	}, nil
}

func validateToolchains(raw map[string]rawToolchain) (map[string]Toolchain, error) {
	out := make(map[string]Toolchain, len(raw))
	for id, value := range raw {
		if !idPattern.MatchString(id) || !boundedOptional(value.Version) || !boundedOptional(value.Manager) {
			return nil, projectError(CodeSchemaError, "invalid toolchain declaration")
		}
		versionSource, err := normalizeRelative(value.VersionSource, true)
		if err != nil {
			return nil, err
		}
		if countNonEmpty(value.Version, versionSource, value.Manager) != 1 {
			return nil, projectError(CodeSchemaError, "toolchain requires exactly one version source")
		}
		out[id] = Toolchain{Version: value.Version, VersionSource: versionSource, Manager: value.Manager}
	}
	return out, nil
}

func validateCommands(raw map[string]rawCommand) (map[string]Command, error) {
	out := make(map[string]Command, len(raw))
	for id, value := range raw {
		command, err := validateCommand(id, value)
		if err != nil {
			return nil, err
		}
		out[id] = command
	}
	return out, nil
}

func validateVerificationProfiles(raw map[string]rawVerificationProfile, commands map[string]Command) (map[string]VerificationProfile, error) {
	out := make(map[string]VerificationProfile, len(raw))
	for id, value := range raw {
		if !idPattern.MatchString(id) || len(value.Steps) > MaxStepsPerProfile {
			return nil, projectError(CodeLimitExceeded, "invalid verification profile")
		}
		steps := append([]string(nil), value.Steps...)
		for _, commandID := range steps {
			if _, ok := commands[commandID]; !ok {
				return nil, projectError(CodeUnknownCommand, "verification profile references unknown command")
			}
		}
		out[id] = VerificationProfile{Steps: steps}
	}
	return out, nil
}

func validateRelevantEnvironment(raw []string) ([]string, error) {
	out := append([]string(nil), raw...)
	for _, name := range out {
		if !envPattern.MatchString(name) || !bounded(name) {
			return nil, projectError(CodeSchemaError, "invalid relevant environment name")
		}
	}
	return out, nil
}

func validateOutputs(raw []rawOutput) ([]Output, error) {
	out := make([]Output, 0, len(raw))
	for _, value := range raw {
		converted, err := validateOutput(value, true)
		if err != nil {
			return nil, err
		}
		out = append(out, converted)
	}
	return out, nil
}

func validateCommand(id string, raw rawCommand) (Command, error) {
	if !idPattern.MatchString(id) || len(raw.ExpectedOutputs) > MaxExpectedOutputs || len(raw.DependsOn) > MaxCommands {
		return Command{}, projectError(CodeLimitExceeded, "invalid command declaration")
	}
	hasArgv := raw.Argv != nil
	if hasArgv == (raw.Shell != "") || (hasArgv && (len(raw.Argv) == 0 || len(raw.Argv) > MaxArgvItems)) {
		return Command{}, projectError(CodeSchemaError, "command requires exactly one non-empty execution form")
	}
	if raw.Shell != "" && !bounded(raw.Shell) {
		return Command{}, projectError(CodeSchemaError, "shell command exceeds limit")
	}
	for _, arg := range raw.Argv {
		if !bounded(arg) {
			return Command{}, projectError(CodeSchemaError, "argv item exceeds limit")
		}
	}
	cwd, err := normalizeRelative(raw.CWD, false)
	if err != nil {
		return Command{}, err
	}
	if cwd == "" {
		cwd = "."
	}
	if !oneOfOptional(raw.Kind, "format", "inspect", "test", "build", "generate", "release") ||
		!oneOfOptional(raw.Cost, "fast", "medium", "expensive") ||
		!oneOfOptional(raw.SourceScope, "none", "affected", "full") ||
		raw.TimeoutMS < 0 || raw.TimeoutMS > MaxTimeoutMS {
		return Command{}, projectError(CodeSchemaError, "invalid command metadata")
	}
	out := Command{
		Argv: append([]string(nil), raw.Argv...), Shell: raw.Shell, CWD: cwd, Kind: raw.Kind, Cost: raw.Cost,
		SourceScope: raw.SourceScope, MutatesSource: raw.MutatesSource, ExternalEffect: raw.ExternalEffect,
		TimeoutMS: raw.TimeoutMS, DependsOn: append([]string(nil), raw.DependsOn...),
	}
	for _, dep := range out.DependsOn {
		if !idPattern.MatchString(dep) {
			return Command{}, projectError(CodeSchemaError, "invalid dependency id")
		}
	}
	for _, value := range raw.ExpectedOutputs {
		converted, err := validateOutput(value, false)
		if err != nil {
			return Command{}, err
		}
		out.ExpectedOutputs = append(out.ExpectedOutputs, converted)
	}
	return out, nil
}

func validateOutput(raw rawOutput, topLevel bool) (Output, error) {
	normalized, err := normalizeRelative(raw.Path, false)
	if err != nil {
		return Output{}, err
	}
	if normalized == "" || !oneOf(raw.Kind, "file", "directory", "symlink") ||
		!oneOfOptional(raw.Digest, "none", "sha256", "tree-sha256") || !boundedOptional(raw.Role) {
		return Output{}, projectError(CodeSchemaError, "invalid output declaration")
	}
	if !topLevel && raw.Role != "" {
		return Output{}, projectError(CodeSchemaError, "command output cannot declare role")
	}
	if (raw.Kind == "file" && raw.Digest == "tree-sha256") ||
		(raw.Kind == "directory" && raw.Digest == "sha256") ||
		(raw.Kind == "symlink" && raw.Digest != "" && raw.Digest != "none") {
		return Output{}, projectError(CodeSchemaError, "output digest incompatible with kind")
	}
	required := true
	if raw.Required != nil {
		required = *raw.Required
	}
	return Output{Path: normalized, Kind: raw.Kind, Digest: raw.Digest, Role: raw.Role, Required: required}, nil
}

func validateCommandGraph(commands map[string]Command) error {
	state := make(map[string]uint8, len(commands))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return projectError(CodeDependencyCycle, "command dependency cycle")
		case 2:
			return nil
		}
		state[id] = 1
		for _, dep := range commands[id].DependsOn {
			if _, ok := commands[dep]; !ok {
				return projectError(CodeUnknownCommand, "command depends on unknown command")
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range commands {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRelative(value string, optional bool) (string, error) {
	if value == "" && optional {
		return "", nil
	}
	if value == "" {
		return ".", nil
	}
	if !bounded(value) || strings.ContainsRune(value, 0) || strings.HasPrefix(value, "/") {
		return "", projectError(CodePathEscape, "path must be repository-relative")
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", projectError(CodePathEscape, "path escapes repository")
	}
	return clean, nil
}

func bounded(value string) bool {
	if value == "" || len(value) > MaxStringBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func boundedOptional(value string) bool { return value == "" || bounded(value) }
func countNonEmpty(values ...string) int {
	n := 0
	for _, value := range values {
		if value != "" {
			n++
		}
	}
	return n
}
func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
func oneOfOptional(value string, allowed ...string) bool {
	return value == "" || oneOf(value, allowed...)
}
