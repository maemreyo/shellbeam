package hermetic

import (
	"fmt"
	"regexp"
	"slices"
)

const (
	BoundaryResultSchemaV1   = 1
	ProvenInputScopeSchemaV1 = 1
)

type Continuity string

const (
	ContinuityComplete Continuity = "complete"
	ContinuityLost     Continuity = "lost"
)

var boundaryIDPattern = regexp.MustCompile(`^hb_[0-9A-HJKMNP-TV-Z]{26}$`)

type BoundaryResult struct {
	SchemaVersion      int               `json:"schema_version"`
	BoundaryID         string            `json:"boundary_id"`
	Provider           ProviderIdentity  `json:"provider"`
	Toolchain          ToolchainIdentity `json:"toolchain"`
	EstablishedPreExec bool              `json:"established_pre_exec"`
	Continuity         Continuity        `json:"continuity"`
}

func (r BoundaryResult) Validate() error {
	if r.SchemaVersion != BoundaryResultSchemaV1 || !boundaryIDPattern.MatchString(r.BoundaryID) {
		return fmt.Errorf("invalid hermetic boundary result identity")
	}
	if err := r.Provider.Validate(); err != nil {
		return err
	}
	if err := r.Toolchain.Validate(); err != nil {
		return err
	}
	if r.Continuity != ContinuityComplete && r.Continuity != ContinuityLost {
		return fmt.Errorf("invalid hermetic boundary continuity")
	}
	return nil
}

func (r BoundaryResult) Authoritative() bool {
	return r.Validate() == nil && r.EstablishedPreExec && r.Continuity == ContinuityComplete
}

type AmbientInputClass string

const (
	AmbientClock      AmbientInputClass = "clock"
	AmbientRandomness AmbientInputClass = "randomness"
)

type ProvenInputScope struct {
	SchemaVersion         int                 `json:"schema_version"`
	RepoInputs            []string            `json:"repo_inputs"`
	CaptureManifestSHA256 string              `json:"capture_manifest_sha256"`
	CaptureContentSHA256  string              `json:"capture_content_sha256"`
	Provider              ProviderIdentity    `json:"provider"`
	Toolchain             ToolchainIdentity   `json:"toolchain"`
	Environment           EnvironmentMode     `json:"environment"`
	Stdin                 StdinMode           `json:"stdin"`
	Network               NetworkMode         `json:"network"`
	AmbientInputs         []AmbientInputClass `json:"ambient_inputs"`
}

func (s ProvenInputScope) Validate() error {
	if s.SchemaVersion != ProvenInputScopeSchemaV1 || !validSHA256(s.CaptureManifestSHA256) || !validSHA256(s.CaptureContentSHA256) {
		return fmt.Errorf("invalid proven input scope identity")
	}
	canonicalInputs, err := normalizeRepoInputs(s.RepoInputs)
	if err != nil {
		return err
	}
	if !slices.Equal(canonicalInputs, s.RepoInputs) {
		return fmt.Errorf("noncanonical proven input scope")
	}
	if err := s.Provider.Validate(); err != nil {
		return err
	}
	if err := s.Toolchain.Validate(); err != nil {
		return err
	}
	if s.Environment != EnvironmentFixedAllowlist || s.Stdin != StdinClosed || s.Network != NetworkOff {
		return fmt.Errorf("invalid proven input scope boundary semantics")
	}
	if len(s.AmbientInputs) != 2 || s.AmbientInputs[0] != AmbientClock || s.AmbientInputs[1] != AmbientRandomness {
		return fmt.Errorf("invalid proven input scope ambient inputs")
	}
	return nil
}
