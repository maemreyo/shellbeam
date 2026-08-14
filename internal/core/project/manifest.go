// Package project defines the strict repository project capability manifest.
package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

const (
	SchemaVersion                 = 1
	MaxManifestBytes              = 64 << 10
	MaxCommands                   = 64
	MaxVerificationProfiles       = 16
	MaxStepsPerProfile            = 64
	MaxExpectedOutputs            = 64
	MaxRelevantEnvironment        = 64
	MaxStringBytes                = 1024
	MaxArgvItems                  = 128
	MaxTimeoutMS            int64 = 86_400_000
)

const (
	CodeTooLarge        = "project_manifest_too_large"
	CodeParseError      = "project_manifest_parse_error"
	CodeSchemaError     = "project_manifest_schema_error"
	CodeUnsupported     = "project_manifest_unsupported"
	CodePathEscape      = "project_manifest_path_escape"
	CodeUnknownCommand  = "project_manifest_unknown_command"
	CodeDependencyCycle = "project_manifest_dependency_cycle"
	CodeLimitExceeded   = "project_manifest_limit_exceeded"
)

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func HasCode(err error, code string) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

func projectError(code, message string) error {
	return &Error{Code: code, Err: errors.New(message)}
}

type Manifest struct {
	SchemaVersion        int                            `json:"schema_version"`
	Project              Project                        `json:"project,omitempty"`
	Toolchains           map[string]Toolchain           `json:"toolchains,omitempty"`
	Commands             map[string]Command             `json:"commands,omitempty"`
	VerificationProfiles map[string]VerificationProfile `json:"verification_profiles,omitempty"`
	RelevantEnvironment  []string                       `json:"relevant_environment,omitempty"`
	Outputs              []Output                       `json:"outputs,omitempty"`
}

type Project struct {
	Name string `json:"name,omitempty"`
}

type Toolchain struct {
	Version       string `json:"version,omitempty"`
	VersionSource string `json:"version_source,omitempty"`
	Manager       string `json:"manager,omitempty"`
}

type Command struct {
	Argv            []string `json:"argv,omitempty"`
	Shell           string   `json:"shell,omitempty"`
	CWD             string   `json:"cwd"`
	Kind            string   `json:"kind,omitempty"`
	Cost            string   `json:"cost,omitempty"`
	SourceScope     string   `json:"source_scope,omitempty"`
	MutatesSource   *bool    `json:"mutates_source,omitempty"`
	ExternalEffect  *bool    `json:"external_effect,omitempty"`
	TimeoutMS       int64    `json:"timeout_ms,omitempty"`
	ExpectedOutputs []Output `json:"expected_outputs,omitempty"`
	DependsOn       []string `json:"depends_on,omitempty"`
}

type VerificationProfile struct {
	Steps []string `json:"steps"`
}

type Output struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Digest   string `json:"digest,omitempty"`
	Role     string `json:"role,omitempty"`
	Required bool   `json:"required"`
}

type Parsed struct {
	Manifest    Manifest
	Fingerprint string
}

type rawManifest struct {
	SchemaVersion int                     `toml:"schema_version"`
	Project       rawProject              `toml:"project"`
	Toolchains    map[string]rawToolchain `toml:"toolchains"`
	Commands      map[string]rawCommand   `toml:"commands"`
	Verification  rawVerification         `toml:"verification"`
	Environment   rawEnvironment          `toml:"environment"`
	Outputs       []rawOutput             `toml:"outputs"`
}
type rawProject struct {
	Name string `toml:"name"`
}
type rawToolchain struct {
	Version       string `toml:"version"`
	VersionSource string `toml:"version_source"`
	Manager       string `toml:"manager"`
}
type rawCommand struct {
	Argv            []string    `toml:"argv"`
	Shell           string      `toml:"shell"`
	CWD             string      `toml:"cwd"`
	Kind            string      `toml:"kind"`
	Cost            string      `toml:"cost"`
	SourceScope     string      `toml:"source_scope"`
	MutatesSource   *bool       `toml:"mutates_source"`
	ExternalEffect  *bool       `toml:"external_effect"`
	TimeoutMS       int64       `toml:"timeout_ms"`
	ExpectedOutputs []rawOutput `toml:"expected_outputs"`
	DependsOn       []string    `toml:"depends_on"`
}
type rawVerification struct {
	Profiles map[string]rawVerificationProfile `toml:"profiles"`
}
type rawVerificationProfile struct {
	Steps []string `toml:"steps"`
}
type rawEnvironment struct {
	RelevantPresence []string `toml:"relevant_presence"`
}
type rawOutput struct {
	Path     string `toml:"path"`
	Kind     string `toml:"kind"`
	Digest   string `toml:"digest"`
	Role     string `toml:"role"`
	Required *bool  `toml:"required"`
}

var (
	idPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
)

func Parse(data []byte) (Parsed, error) {
	if len(data) > MaxManifestBytes {
		return Parsed{}, projectError(CodeTooLarge, "manifest exceeds size limit")
	}
	if !utf8.Valid(data) {
		return Parsed{}, projectError(CodeParseError, "manifest is not valid UTF-8")
	}
	var header struct {
		SchemaVersion *int `toml:"schema_version"`
	}
	if err := toml.Unmarshal(data, &header); err != nil {
		return Parsed{}, &Error{Code: CodeParseError, Err: err}
	}
	if header.SchemaVersion == nil {
		return Parsed{}, projectError(CodeSchemaError, "manifest schema_version is required")
	}
	if *header.SchemaVersion != SchemaVersion {
		return Parsed{}, projectError(CodeUnsupported, "unsupported manifest schema version")
	}
	var raw rawManifest
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Parsed{}, &Error{Code: CodeSchemaError, Err: err}
	}
	manifest, err := validateRaw(raw)
	if err != nil {
		return Parsed{}, err
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return Parsed{}, &Error{Code: CodeSchemaError, Err: err}
	}
	sum := sha256.Sum256(canonical)
	return Parsed{Manifest: manifest, Fingerprint: hex.EncodeToString(sum[:])}, nil
}

func RawDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}

func DiagnosticMessage(err error) string {
	if err == nil {
		return ""
	}
	var target *Error
	if errors.As(err, &target) {
		return fmt.Sprintf("%s", target.Code)
	}
	return CodeSchemaError
}
