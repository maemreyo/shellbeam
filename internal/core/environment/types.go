// Package environment defines versioned, secret-safe environment and toolchain observation contracts.
package environment

import (
	"time"

	"github.com/maemreyo/shellbeam/internal/core/project"
)

const (
	SnapshotSchemaVersion       = 1
	FingerprintVersion          = 1
	ToolchainFingerprintVersion = 1
	MaxRelevantVariables        = project.MaxRelevantEnvironment
	MaxToolchainProbes          = 5
	MaxToolchainObservations    = 16
)

type Quality string
type ProbeQuality string
type Freshness string

const (
	QualityComplete    Quality = "complete"
	QualityPartial     Quality = "partial"
	QualityUnavailable Quality = "unavailable"

	ProbeComplete    ProbeQuality = "complete"
	ProbeUnavailable ProbeQuality = "unavailable"

	FreshnessCached  Freshness = "cached"
	FreshnessRefresh Freshness = "refresh"
)

type Platform struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type ExecutionContext struct {
	Mode     string `json:"mode"`
	Identity string `json:"identity"`
}

type PathObservation struct {
	Digest     string  `json:"digest,omitempty"`
	EntryCount int     `json:"entry_count"`
	Quality    Quality `json:"quality"`
}

type VariablePresence struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
}

type ToolchainManager struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
}

type ToolchainObservation struct {
	Kind              string       `json:"kind"`
	RequestedIdentity string       `json:"requested_identity"`
	ObservedIdentity  string       `json:"observed_identity,omitempty"`
	Version           string       `json:"version,omitempty"`
	Quality           ProbeQuality `json:"quality"`
	DiagnosticCode    string       `json:"diagnostic_code,omitempty"`
}

type FingerprintInput struct {
	Platform         Platform
	Execution        ExecutionContext
	Path             PathObservation
	VariablePresence []VariablePresence
	ToolchainManager *ToolchainManager
}

type Snapshot struct {
	SchemaVersion               int                    `json:"schema_version"`
	SnapshotID                  string                 `json:"snapshot_id"`
	CapturedAt                  time.Time              `json:"captured_at"`
	Quality                     Quality                `json:"quality"`
	EnvironmentFingerprint      string                 `json:"environment_fingerprint,omitempty"`
	FingerprintVersion          int                    `json:"fingerprint_version"`
	ToolchainFingerprint        string                 `json:"toolchain_fingerprint,omitempty"`
	ToolchainFingerprintVersion int                    `json:"toolchain_fingerprint_version,omitempty"`
	Platform                    Platform               `json:"platform"`
	Execution                   ExecutionContext       `json:"execution"`
	Path                        PathObservation        `json:"path"`
	VariablePresence            []VariablePresence     `json:"variable_presence,omitempty"`
	ToolchainManager            *ToolchainManager      `json:"toolchain_manager,omitempty"`
	Toolchains                  []ToolchainObservation `json:"toolchains,omitempty"`
}

type Binding struct {
	SnapshotID                    string    `json:"snapshot_id"`
	EnvironmentFingerprint        string    `json:"environment_fingerprint"`
	EnvironmentFingerprintVersion int       `json:"environment_fingerprint_version"`
	ToolchainFingerprint          string    `json:"toolchain_fingerprint,omitempty"`
	ToolchainFingerprintVersion   int       `json:"toolchain_fingerprint_version,omitempty"`
	CapturedAt                    time.Time `json:"captured_at"`
}

func (s Snapshot) Binding() Binding {
	return Binding{
		SnapshotID:                    s.SnapshotID,
		EnvironmentFingerprint:        s.EnvironmentFingerprint,
		EnvironmentFingerprintVersion: s.FingerprintVersion,
		ToolchainFingerprint:          s.ToolchainFingerprint,
		ToolchainFingerprintVersion:   s.ToolchainFingerprintVersion,
		CapturedAt:                    s.CapturedAt,
	}
}

func (b Binding) CompatibleWith(other Binding) bool {
	return b.EnvironmentFingerprintVersion > 0 &&
		b.EnvironmentFingerprintVersion == other.EnvironmentFingerprintVersion &&
		b.ToolchainFingerprintVersion == other.ToolchainFingerprintVersion
}
