package project

const (
	MaxRequirements  = 64
	MaxCommandParams = 32
	MaxEnumValues    = 64
)

type Requirements struct {
	Toolchains  map[string]ToolchainRequirement  `json:"toolchains,omitempty"`
	Executables map[string]ExecutableRequirement `json:"executables,omitempty"`
	Environment EnvironmentRequirements          `json:"environment,omitempty"`
}

type ToolchainRequirement struct {
	Required bool `json:"required"`
}

type ExecutableRequirement struct {
	Required bool `json:"required"`
}

type EnvironmentRequirements struct {
	RequiredPresence []string `json:"required_presence,omitempty"`
	OptionalPresence []string `json:"optional_presence,omitempty"`
}

type ParameterKind string

const (
	ParameterString      ParameterKind = "string"
	ParameterEnum        ParameterKind = "enum"
	ParameterInteger     ParameterKind = "integer"
	ParameterRepoPath    ParameterKind = "repo_path"
	ParameterRepoPackage ParameterKind = "repo_package"
)

type PathExistence string

const (
	PathExistsAny       PathExistence = "any"
	PathExistsFile      PathExistence = "file"
	PathExistsDirectory PathExistence = "directory"
)

type ParameterDefinition struct {
	Kind             ParameterKind `json:"kind"`
	Required         bool          `json:"required"`
	Default          string        `json:"default,omitempty"`
	Enum             []string      `json:"enum,omitempty"`
	Min              *int64        `json:"min,omitempty"`
	Max              *int64        `json:"max,omitempty"`
	Exists           PathExistence `json:"exists,omitempty"`
	Provider         string        `json:"provider,omitempty"`
	AllowLeadingDash bool          `json:"allow_leading_dash,omitempty"`
}

func SupportedManifestSchemaVersion(version int) bool {
	return version == ManifestSchemaV1 || version == ManifestSchemaV2
}

type rawManifestV2 struct {
	SchemaVersion int                     `toml:"schema_version"`
	Project       rawProject              `toml:"project"`
	Toolchains    map[string]rawToolchain `toml:"toolchains"`
	Requirements  rawRequirements         `toml:"requirements"`
	Commands      map[string]rawCommandV2 `toml:"commands"`
	Verification  rawVerification         `toml:"verification"`
	Environment   rawEnvironment          `toml:"environment"`
	Outputs       []rawOutput             `toml:"outputs"`
}

type rawRequirements struct {
	Toolchains  map[string]rawRequirement `toml:"toolchains"`
	Executables map[string]rawRequirement `toml:"executables"`
	Environment rawRequirementEnvironment `toml:"environment"`
}

type rawRequirement struct {
	Required *bool `toml:"required"`
}

type rawRequirementEnvironment struct {
	RequiredPresence []string `toml:"required_presence"`
	OptionalPresence []string `toml:"optional_presence"`
}

type rawCommandV2 struct {
	Argv            []string                          `toml:"argv"`
	Shell           string                            `toml:"shell"`
	CWD             string                            `toml:"cwd"`
	Kind            string                            `toml:"kind"`
	Cost            string                            `toml:"cost"`
	SourceScope     string                            `toml:"source_scope"`
	MutatesSource   *bool                             `toml:"mutates_source"`
	ExternalEffect  *bool                             `toml:"external_effect"`
	TimeoutMS       int64                             `toml:"timeout_ms"`
	ExpectedOutputs []rawOutput                       `toml:"expected_outputs"`
	DependsOn       []string                          `toml:"depends_on"`
	Params          map[string]rawParameterDefinition `toml:"params"`
}

type rawParameterDefinition struct {
	Kind             string   `toml:"kind"`
	Required         *bool    `toml:"required"`
	Default          *string  `toml:"default"`
	Enum             []string `toml:"enum"`
	Min              *int64   `toml:"min"`
	Max              *int64   `toml:"max"`
	Exists           string   `toml:"exists"`
	Provider         string   `toml:"provider"`
	AllowLeadingDash bool     `toml:"allow_leading_dash"`
}
