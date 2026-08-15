package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	ReadinessSchemaVersion = 1
	MaxReadinessChecks     = MaxRequirements * 3
)

type ReadinessState string

const (
	ReadinessReady       ReadinessState = "ready"
	ReadinessNotReady    ReadinessState = "not_ready"
	ReadinessPartial     ReadinessState = "partial"
	ReadinessUnavailable ReadinessState = "unavailable"
)

type RequirementKind string

const (
	RequirementToolchain           RequirementKind = "toolchain"
	RequirementExecutable          RequirementKind = "executable"
	RequirementEnvironmentPresence RequirementKind = "environment_presence"
)

type CheckStatus string

const (
	CheckAvailable       CheckStatus = "available"
	CheckMissing         CheckStatus = "missing"
	CheckCompatible      CheckStatus = "compatible"
	CheckIncompatible    CheckStatus = "incompatible"
	CheckPresent         CheckStatus = "present"
	CheckPresentNonEmpty CheckStatus = "present_nonempty"
	CheckAbsent          CheckStatus = "absent"
	CheckUnknown         CheckStatus = "unknown"
	CheckUnavailable     CheckStatus = "unavailable"
)

type CacheQuality string

const (
	CacheFresh  CacheQuality = "fresh"
	CacheCached CacheQuality = "cached"
)

type ReadinessCheck struct {
	ID              string          `json:"id"`
	Kind            RequirementKind `json:"kind"`
	Required        bool            `json:"required"`
	Status          CheckStatus     `json:"status"`
	Code            string          `json:"code,omitempty"`
	ProviderID      string          `json:"provider_id,omitempty"`
	ProviderVersion int             `json:"provider_version,omitempty"`
}

type Readiness struct {
	SchemaVersion          int              `json:"schema_version"`
	State                  ReadinessState   `json:"state"`
	RepositoryID           string           `json:"repository_id"`
	WorkspaceID            string           `json:"workspace_id"`
	ManifestDigest         string           `json:"manifest_digest"`
	ManifestSchemaVersion  int              `json:"manifest_schema_version"`
	EnvironmentFingerprint string           `json:"environment_fingerprint,omitempty"`
	ToolchainFingerprint   string           `json:"toolchain_fingerprint,omitempty"`
	CapturedAt             time.Time        `json:"captured_at"`
	CacheQuality           CacheQuality     `json:"cache_quality"`
	CacheAgeMS             int64            `json:"cache_age_ms"`
	Checks                 []ReadinessCheck `json:"checks"`
}

func FoldReadiness(checks []ReadinessCheck) ReadinessState {
	if len(checks) == 0 {
		return ReadinessUnavailable
	}
	partial := false
	for _, check := range checks {
		if !check.Required {
			continue
		}
		switch check.Status {
		case CheckMissing, CheckIncompatible, CheckAbsent:
			return ReadinessNotReady
		case CheckUnknown, CheckUnavailable:
			partial = true
		}
	}
	if partial {
		return ReadinessPartial
	}
	return ReadinessReady
}

func (c ReadinessCheck) Validate() error {
	if !validRequirementID(c.Kind, c.ID) || !validCheckStatus(c.Kind, c.Status) {
		return fmt.Errorf("invalid readiness check")
	}
	if !boundedOptional(c.Code) {
		return fmt.Errorf("invalid readiness code")
	}
	if c.ProviderID == "" {
		if c.ProviderVersion != 0 {
			return fmt.Errorf("provider version without provider")
		}
		return nil
	}
	if !idPattern.MatchString(c.ProviderID) || c.ProviderVersion < 1 {
		return fmt.Errorf("invalid readiness provider")
	}
	return nil
}

func (r Readiness) Validate() error {
	if r.SchemaVersion != ReadinessSchemaVersion || !validReadinessState(r.State) {
		return fmt.Errorf("invalid readiness schema or state")
	}
	if _, err := workspace.ParseRepositoryID(r.RepositoryID); err != nil {
		return fmt.Errorf("invalid readiness repository: %w", err)
	}
	if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return fmt.Errorf("invalid readiness workspace: %w", err)
	}
	if !validFingerprint(r.ManifestDigest) || !SupportedManifestSchemaVersion(r.ManifestSchemaVersion) {
		return fmt.Errorf("invalid readiness manifest binding")
	}
	if r.EnvironmentFingerprint != "" && !validFingerprint(r.EnvironmentFingerprint) {
		return fmt.Errorf("invalid environment fingerprint")
	}
	if r.ToolchainFingerprint != "" && !validFingerprint(r.ToolchainFingerprint) {
		return fmt.Errorf("invalid toolchain fingerprint")
	}
	if r.CapturedAt.IsZero() || r.CacheAgeMS < 0 || r.CacheQuality != CacheFresh && r.CacheQuality != CacheCached {
		return fmt.Errorf("invalid readiness capture metadata")
	}
	if r.CacheQuality == CacheFresh && r.CacheAgeMS != 0 {
		return fmt.Errorf("fresh readiness cannot have cache age")
	}
	if err := validateReadinessChecks(r.Checks); err != nil {
		return err
	}
	if r.State != FoldReadiness(r.Checks) {
		return fmt.Errorf("readiness state does not match checks")
	}
	return nil
}

func ReadinessChecksFingerprint(checks []ReadinessCheck) (string, error) {
	if err := validateReadinessChecks(checks); err != nil {
		return "", err
	}
	normalized := append([]ReadinessCheck(nil), checks...)
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Kind != normalized[j].Kind {
			return normalized[i].Kind < normalized[j].Kind
		}
		if normalized[i].ID != normalized[j].ID {
			return normalized[i].ID < normalized[j].ID
		}
		return readinessCheckSortKey(normalized[i]) < readinessCheckSortKey(normalized[j])
	})
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validateReadinessChecks(checks []ReadinessCheck) error {
	if len(checks) > MaxReadinessChecks {
		return fmt.Errorf("readiness check limit exceeded")
	}
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		if err := check.Validate(); err != nil {
			return err
		}
		key := string(check.Kind) + "\x00" + check.ID
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate readiness check")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validRequirementID(kind RequirementKind, id string) bool {
	switch kind {
	case RequirementToolchain, RequirementExecutable:
		return idPattern.MatchString(id)
	case RequirementEnvironmentPresence:
		return envPattern.MatchString(id) && bounded(id)
	default:
		return false
	}
}

func validCheckStatus(kind RequirementKind, status CheckStatus) bool {
	switch kind {
	case RequirementToolchain:
		return status == CheckAvailable || status == CheckMissing || status == CheckCompatible || status == CheckIncompatible || status == CheckUnknown || status == CheckUnavailable
	case RequirementExecutable:
		return status == CheckAvailable || status == CheckMissing || status == CheckUnknown || status == CheckUnavailable
	case RequirementEnvironmentPresence:
		return status == CheckPresent || status == CheckPresentNonEmpty || status == CheckAbsent || status == CheckUnknown || status == CheckUnavailable
	default:
		return false
	}
}

func validReadinessState(state ReadinessState) bool {
	return state == ReadinessReady || state == ReadinessNotReady || state == ReadinessPartial || state == ReadinessUnavailable
}

func readinessCheckSortKey(check ReadinessCheck) string {
	return fmt.Sprintf("%t\x00%s\x00%s\x00%s\x00%d", check.Required, check.Status, check.Code, check.ProviderID, check.ProviderVersion)
}
