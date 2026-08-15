package project

import (
	"strconv"
	"strings"
)

func validateRawV2(raw rawManifestV2) (Manifest, error) {
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
	requirements, err := validateRequirements(raw.Requirements, toolchains)
	if err != nil {
		return Manifest{}, err
	}
	commands, err := validateCommandsV2(raw.Commands)
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
		SchemaVersion: ManifestSchemaV2, Project: Project{Name: raw.Project.Name}, Toolchains: toolchains,
		Requirements: requirements, Commands: commands, VerificationProfiles: profiles,
		RelevantEnvironment: environment, Outputs: outputs,
	}, nil
}

func validateRequirements(raw rawRequirements, toolchains map[string]Toolchain) (Requirements, error) {
	if len(raw.Toolchains) > MaxRequirements || len(raw.Executables) > MaxRequirements ||
		len(raw.Environment.RequiredPresence)+len(raw.Environment.OptionalPresence) > MaxRequirements {
		return Requirements{}, projectError(CodeLimitExceeded, "requirement collection limit exceeded")
	}
	toolchainReqs := make(map[string]ToolchainRequirement, len(raw.Toolchains))
	for id, value := range raw.Toolchains {
		if !idPattern.MatchString(id) {
			return Requirements{}, projectError(CodeSchemaError, "invalid toolchain requirement id")
		}
		if _, ok := toolchains[id]; !ok {
			return Requirements{}, projectError(CodeSchemaError, "toolchain requirement references unknown toolchain")
		}
		toolchainReqs[id] = ToolchainRequirement{Required: defaultRequired(value.Required)}
	}
	executables := make(map[string]ExecutableRequirement, len(raw.Executables))
	for id, value := range raw.Executables {
		if !idPattern.MatchString(id) {
			return Requirements{}, projectError(CodeSchemaError, "invalid executable requirement id")
		}
		executables[id] = ExecutableRequirement{Required: defaultRequired(value.Required)}
	}
	environment, err := validateRequirementEnvironment(raw.Environment)
	if err != nil {
		return Requirements{}, err
	}
	return Requirements{Toolchains: toolchainReqs, Executables: executables, Environment: environment}, nil
}

func validateRequirementEnvironment(raw rawRequirementEnvironment) (EnvironmentRequirements, error) {
	seen := make(map[string]struct{}, len(raw.RequiredPresence)+len(raw.OptionalPresence))
	validate := func(values []string) ([]string, error) {
		out := append([]string(nil), values...)
		for _, name := range out {
			if _, duplicate := seen[name]; !envPattern.MatchString(name) || !bounded(name) || duplicate {
				return nil, projectError(CodeSchemaError, "invalid or duplicate environment requirement")
			}
			seen[name] = struct{}{}
		}
		return out, nil
	}
	required, err := validate(raw.RequiredPresence)
	if err != nil {
		return EnvironmentRequirements{}, err
	}
	optional, err := validate(raw.OptionalPresence)
	if err != nil {
		return EnvironmentRequirements{}, err
	}
	return EnvironmentRequirements{RequiredPresence: required, OptionalPresence: optional}, nil
}

func validateCommandsV2(raw map[string]rawCommandV2) (map[string]Command, error) {
	out := make(map[string]Command, len(raw))
	for id, value := range raw {
		base := rawCommand{
			Argv: value.Argv, Shell: value.Shell, CWD: value.CWD, Kind: value.Kind, Cost: value.Cost,
			SourceScope: value.SourceScope, MutatesSource: value.MutatesSource, ExternalEffect: value.ExternalEffect,
			TimeoutMS: value.TimeoutMS, ExpectedOutputs: value.ExpectedOutputs, DependsOn: value.DependsOn,
		}
		command, err := validateCommand(id, base)
		if err != nil {
			return nil, err
		}
		params, err := validateParameterDefinitions(value.Params)
		if err != nil {
			return nil, err
		}
		if len(params) > 0 && command.Shell != "" {
			return nil, projectError(CodeSchemaError, "parameterized shell commands are unsupported")
		}
		if err := validateCommandPlaceholders(command.Argv, params); err != nil {
			return nil, err
		}
		command.Params = params
		out[id] = command
	}
	return out, nil
}

func validateParameterDefinitions(raw map[string]rawParameterDefinition) (map[string]ParameterDefinition, error) {
	if len(raw) > MaxCommandParams {
		return nil, projectError(CodeLimitExceeded, "command parameter limit exceeded")
	}
	out := make(map[string]ParameterDefinition, len(raw))
	for id, value := range raw {
		if !idPattern.MatchString(id) {
			return nil, projectError(CodeSchemaError, "invalid parameter id")
		}
		definition, err := validateParameterDefinition(value)
		if err != nil {
			return nil, err
		}
		out[id] = definition
	}
	return out, nil
}

func validateParameterDefinition(raw rawParameterDefinition) (ParameterDefinition, error) {
	kind := ParameterKind(raw.Kind)
	if !validParameterKind(kind) {
		return ParameterDefinition{}, projectError(CodeSchemaError, "invalid parameter kind")
	}
	hasDefault := raw.Default != nil
	required := !hasDefault
	if raw.Required != nil {
		required = *raw.Required
	}
	if required == hasDefault {
		return ParameterDefinition{}, projectError(CodeSchemaError, "parameter required/default contract is invalid")
	}
	definition := ParameterDefinition{
		Kind: kind, Required: required, Enum: append([]string(nil), raw.Enum...),
		Min: cloneInt64(raw.Min), Max: cloneInt64(raw.Max), Exists: PathExistence(raw.Exists),
		Provider: raw.Provider, AllowLeadingDash: raw.AllowLeadingDash,
	}
	if hasDefault {
		definition.Default = *raw.Default
	}
	if err := validateParameterKindFields(&definition); err != nil {
		return ParameterDefinition{}, err
	}
	return definition, nil
}

func validateParameterKindFields(value *ParameterDefinition) error {
	switch value.Kind {
	case ParameterString:
		if len(value.Enum) != 0 || value.Min != nil || value.Max != nil || value.Exists != "" || value.Provider != "" {
			return projectError(CodeSchemaError, "string parameter has incompatible fields")
		}
		return validateOptionalDefaultToken(value, false)
	case ParameterEnum:
		if value.Min != nil || value.Max != nil || value.Exists != "" || value.Provider != "" || value.AllowLeadingDash {
			return projectError(CodeSchemaError, "enum parameter has incompatible fields")
		}
		return validateEnumParameter(value)
	case ParameterInteger:
		if len(value.Enum) != 0 || value.Exists != "" || value.Provider != "" || value.AllowLeadingDash {
			return projectError(CodeSchemaError, "integer parameter has incompatible fields")
		}
		return validateIntegerParameter(value)
	case ParameterRepoPath:
		if len(value.Enum) != 0 || value.Min != nil || value.Max != nil || value.Provider != "" {
			return projectError(CodeSchemaError, "repo_path parameter has incompatible fields")
		}
		return validateRepoPathParameter(value)
	case ParameterRepoPackage:
		if len(value.Enum) != 0 || value.Min != nil || value.Max != nil || value.Exists != "" || !idPattern.MatchString(value.Provider) {
			return projectError(CodeSchemaError, "repo_package parameter has incompatible fields")
		}
		return validateOptionalDefaultToken(value, false)
	default:
		return projectError(CodeSchemaError, "invalid parameter kind")
	}
}

func validateEnumParameter(value *ParameterDefinition) error {
	if len(value.Enum) < 1 || len(value.Enum) > MaxEnumValues {
		return projectError(CodeSchemaError, "enum parameter requires bounded values")
	}
	seen := make(map[string]struct{}, len(value.Enum))
	for _, item := range value.Enum {
		if !bounded(item) {
			return projectError(CodeSchemaError, "invalid enum value")
		}
		if _, ok := seen[item]; ok {
			return projectError(CodeSchemaError, "duplicate enum value")
		}
		seen[item] = struct{}{}
	}
	if !value.Required {
		if _, ok := seen[value.Default]; !ok {
			return projectError(CodeSchemaError, "enum default is not declared")
		}
	}
	return nil
}

func validateIntegerParameter(value *ParameterDefinition) error {
	if value.Min != nil && value.Max != nil && *value.Min > *value.Max {
		return projectError(CodeSchemaError, "integer parameter bounds are inverted")
	}
	if value.Required {
		return nil
	}
	parsed, err := strconv.ParseInt(value.Default, 10, 64)
	if err != nil || value.Min != nil && parsed < *value.Min || value.Max != nil && parsed > *value.Max {
		return projectError(CodeSchemaError, "integer parameter default is invalid")
	}
	value.Default = strconv.FormatInt(parsed, 10)
	return nil
}

func validateRepoPathParameter(value *ParameterDefinition) error {
	if value.Exists == "" {
		value.Exists = PathExistsAny
	}
	if value.Exists != PathExistsAny && value.Exists != PathExistsFile && value.Exists != PathExistsDirectory {
		return projectError(CodeSchemaError, "invalid repo_path existence rule")
	}
	if value.Required {
		return nil
	}
	normalized, err := normalizeRelative(value.Default, false)
	if err != nil || !value.AllowLeadingDash && strings.HasPrefix(normalized, "-") {
		return projectError(CodeSchemaError, "repo_path default is invalid")
	}
	value.Default = normalized
	return nil
}

func validateOptionalDefaultToken(value *ParameterDefinition, allowEmpty bool) error {
	if value.Required {
		return nil
	}
	if (!allowEmpty && !bounded(value.Default)) || !value.AllowLeadingDash && strings.HasPrefix(value.Default, "-") {
		return projectError(CodeSchemaError, "parameter default is invalid")
	}
	return nil
}

func validateCommandPlaceholders(argv []string, params map[string]ParameterDefinition) error {
	seen := make(map[string]int, len(params))
	for _, token := range argv {
		if !strings.ContainsAny(token, "{}") {
			continue
		}
		if len(token) < 3 || token[0] != '{' || token[len(token)-1] != '}' || strings.Count(token, "{") != 1 || strings.Count(token, "}") != 1 {
			return projectError(CodeSchemaError, "parameter placeholder must occupy a whole argv token")
		}
		id := token[1 : len(token)-1]
		if !idPattern.MatchString(id) {
			return projectError(CodeSchemaError, "invalid parameter placeholder")
		}
		if _, ok := params[id]; !ok {
			return projectError(CodeSchemaError, "undefined parameter placeholder")
		}
		seen[id]++
		if seen[id] > 1 {
			return projectError(CodeSchemaError, "duplicate parameter placeholder")
		}
	}
	for id := range params {
		if seen[id] != 1 {
			return projectError(CodeSchemaError, "parameter declaration is unused")
		}
	}
	return nil
}

func validParameterKind(kind ParameterKind) bool {
	switch kind {
	case ParameterString, ParameterEnum, ParameterInteger, ParameterRepoPath, ParameterRepoPackage:
		return true
	default:
		return false
	}
}

func defaultRequired(value *bool) bool {
	return value == nil || *value
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
