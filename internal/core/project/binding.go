package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	BindingSchemaV1      = 1
	BindingSchemaVersion = 2
)

const (
	BindingSourceCaller        = "caller"
	BindingSourceDefault       = "default"
	PathObservationExactAtBind = "exact_at_bind"
)

type ParameterBinding struct {
	ID              string        `json:"id"`
	Kind            ParameterKind `json:"kind"`
	Value           string        `json:"value"`
	Source          string        `json:"source"`
	ProviderID      string        `json:"provider_id,omitempty"`
	ProviderVersion int           `json:"provider_version,omitempty"`
}

type CommandBinding struct {
	SchemaVersion          int                `json:"schema_version"`
	ManifestDigest         string             `json:"manifest_digest"`
	ManifestSchemaVersion  int                `json:"manifest_schema_version"`
	CommandID              string             `json:"command_id"`
	ParameterFingerprint   string             `json:"parameter_fingerprint"`
	Parameters             []ParameterBinding `json:"parameters"`
	ResolvedArgv           []string           `json:"resolved_argv"`
	LogicalCWD             string             `json:"logical_cwd"`
	ResolvedCWD            string             `json:"resolved_cwd"`
	SourceGeneration       string             `json:"source_generation,omitempty"`
	PathObservationQuality string             `json:"path_observation_quality,omitempty"`
	Kind                   string             `json:"kind,omitempty"`
	SourceScope            string             `json:"source_scope,omitempty"`
	ExpectedOutputs        []Output           `json:"expected_outputs,omitempty"`
}

func (p ParameterBinding) Validate() error {
	if !idPattern.MatchString(p.ID) || !validParameterKind(p.Kind) || !bounded(p.Value) {
		return fmt.Errorf("invalid parameter binding")
	}
	if p.Source != BindingSourceCaller && p.Source != BindingSourceDefault {
		return fmt.Errorf("invalid parameter binding source")
	}
	if p.ProviderVersion < 0 || (p.ProviderID == "") != (p.ProviderVersion == 0) {
		return fmt.Errorf("invalid parameter provider")
	}
	if p.ProviderID != "" && !idPattern.MatchString(p.ProviderID) {
		return fmt.Errorf("invalid parameter provider")
	}
	if p.Kind != ParameterRepoPackage && p.ProviderID != "" {
		return fmt.Errorf("provider on unsupported parameter kind")
	}
	return nil
}

func ParameterFingerprint(parameters []ParameterBinding) (string, error) {
	ordered := append([]ParameterBinding(nil), parameters...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	for i, parameter := range ordered {
		if err := parameter.Validate(); err != nil {
			return "", err
		}
		if i > 0 && ordered[i-1].ID == parameter.ID {
			return "", fmt.Errorf("duplicate parameter binding")
		}
	}
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (b CommandBinding) Validate() error {
	if (b.SchemaVersion != BindingSchemaV1 && b.SchemaVersion != BindingSchemaVersion) || !validDigestHex(b.ManifestDigest) || b.ManifestSchemaVersion != ManifestSchemaV2 {
		return fmt.Errorf("invalid command binding identity")
	}
	if b.SchemaVersion == BindingSchemaV1 && (b.Kind != "" || b.SourceScope != "" || len(b.ExpectedOutputs) != 0) {
		return fmt.Errorf("legacy command binding contains v2 metadata")
	}
	if b.SchemaVersion == BindingSchemaVersion {
		if !oneOfOptional(b.Kind, "format", "inspect", "test", "build", "generate", "release") || !oneOfOptional(b.SourceScope, "none", "affected", "full") {
			return fmt.Errorf("invalid command binding evidence metadata")
		}
		normalized, err := ValidateExpectedOutputs(b.ExpectedOutputs)
		if err != nil || !sameOutputs(normalized, b.ExpectedOutputs) {
			return fmt.Errorf("invalid command binding expected outputs")
		}
	}
	if !idPattern.MatchString(b.CommandID) || !validDigestHex(b.ParameterFingerprint) {
		return fmt.Errorf("invalid command binding identity")
	}
	if len(b.ResolvedArgv) < 1 || len(b.ResolvedArgv) > MaxArgvItems {
		return fmt.Errorf("invalid resolved argv")
	}
	for _, token := range b.ResolvedArgv {
		if !bounded(token) {
			return fmt.Errorf("invalid resolved argv token")
		}
	}
	normalizedCWD, err := normalizeRelative(b.LogicalCWD, false)
	if err != nil || normalizedCWD != b.LogicalCWD || !filepath.IsAbs(b.ResolvedCWD) {
		return fmt.Errorf("invalid command binding cwd")
	}
	for i, parameter := range b.Parameters {
		if err := parameter.Validate(); err != nil {
			return err
		}
		if i > 0 && b.Parameters[i-1].ID >= parameter.ID {
			return fmt.Errorf("parameters are not uniquely sorted")
		}
	}
	fingerprint, err := ParameterFingerprint(b.Parameters)
	if err != nil || fingerprint != b.ParameterFingerprint {
		return fmt.Errorf("parameter fingerprint mismatch")
	}
	if b.SourceGeneration != "" && !validGenerationValue(b.SourceGeneration) {
		return fmt.Errorf("invalid source generation")
	}
	if b.PathObservationQuality != "" && b.PathObservationQuality != PathObservationExactAtBind {
		return fmt.Errorf("invalid path observation quality")
	}
	return nil
}

func sameOutputs(a, b []Output) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b CommandBinding) Digest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validDigestHex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validGenerationValue(value string) bool {
	return strings.HasPrefix(value, "gen_") && validDigestHex(strings.TrimPrefix(value, "gen_"))
}
