package hermetic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	CaptureManifestSchemaVersion       = 1
	MaxCapturePaths                    = 8192
	MaxCaptureFileBytes          int64 = 32 << 20
	MaxCaptureTotalBytes         int64 = 512 << 20
	MaxCaptureWalkEntries              = 32768
)

type CaptureLimits struct {
	MaxPaths       int
	MaxFileBytes   int64
	MaxTotalBytes  int64
	MaxWalkEntries int
}

func DefaultCaptureLimits() CaptureLimits {
	return CaptureLimits{
		MaxPaths: MaxCapturePaths, MaxFileBytes: MaxCaptureFileBytes,
		MaxTotalBytes: MaxCaptureTotalBytes, MaxWalkEntries: MaxCaptureWalkEntries,
	}
}

func (l CaptureLimits) Validate() error {
	if l.MaxPaths <= 0 || l.MaxPaths > MaxCapturePaths ||
		l.MaxFileBytes <= 0 || l.MaxFileBytes > MaxCaptureFileBytes ||
		l.MaxTotalBytes < l.MaxFileBytes || l.MaxTotalBytes > MaxCaptureTotalBytes ||
		l.MaxWalkEntries < l.MaxPaths || l.MaxWalkEntries > MaxCaptureWalkEntries {
		return fmt.Errorf("invalid hermetic capture limits")
	}
	return nil
}

type CaptureEntry struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
	Executable bool   `json:"executable"`
}

type CaptureManifest struct {
	SchemaVersion    int                       `json:"schema_version"`
	WorkspaceID      workspacecore.WorkspaceID `json:"workspace_id"`
	SourceGeneration string                    `json:"source_generation"`
	Selectors        []string                  `json:"selectors"`
	Entries          []CaptureEntry            `json:"entries"`
	TotalBytes       int64                     `json:"total_bytes"`
}

func (m CaptureManifest) Validate() error {
	if m.SchemaVersion != CaptureManifestSchemaVersion {
		return fmt.Errorf("invalid hermetic capture manifest version")
	}
	if _, err := workspacecore.ParseWorkspaceID(string(m.WorkspaceID)); err != nil {
		return err
	}
	if !validSourceGeneration(m.SourceGeneration) {
		return fmt.Errorf("invalid hermetic source generation")
	}
	selectors, err := normalizeRepoInputs(m.Selectors)
	if err != nil || !sameStrings(selectors, m.Selectors) {
		return fmt.Errorf("noncanonical hermetic capture selectors")
	}
	if len(m.Entries) > MaxCapturePaths || m.TotalBytes < 0 || m.TotalBytes > MaxCaptureTotalBytes {
		return fmt.Errorf("hermetic capture manifest exceeds bounds")
	}
	total := int64(0)
	last := ""
	for _, entry := range m.Entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		if last != "" && entry.Path <= last {
			return fmt.Errorf("noncanonical hermetic capture entries")
		}
		last = entry.Path
		total += entry.Size
		if total > MaxCaptureTotalBytes {
			return fmt.Errorf("hermetic capture byte budget exceeded")
		}
	}
	if total != m.TotalBytes {
		return fmt.Errorf("hermetic capture byte accounting mismatch")
	}
	return nil
}

func (e CaptureEntry) Validate() error {
	if err := validateCapturedPath(e.Path); err != nil {
		return err
	}
	if e.Size < 0 || e.Size > MaxCaptureFileBytes || !validSHA256(e.SHA256) {
		return fmt.Errorf("invalid hermetic capture entry")
	}
	return nil
}

func (m CaptureManifest) Canonical() (CaptureManifest, error) {
	out := m
	selectors, err := normalizeRepoInputs(m.Selectors)
	if err != nil {
		return CaptureManifest{}, err
	}
	out.Selectors = selectors
	out.Entries = append([]CaptureEntry(nil), m.Entries...)
	sort.Slice(out.Entries, func(i, j int) bool { return out.Entries[i].Path < out.Entries[j].Path })
	if err := out.Validate(); err != nil {
		return CaptureManifest{}, err
	}
	return out, nil
}

type captureContentDigestPayload struct {
	SchemaVersion int            `json:"schema_version"`
	Selectors     []string       `json:"selectors"`
	Entries       []CaptureEntry `json:"entries"`
	TotalBytes    int64          `json:"total_bytes"`
}

// ContentDigest binds only the declared immutable repo-input selection and its
// captured bytes/mode facts. Workspace identity/generation are intentionally
// excluded so a change outside the proven scope cannot invalidate this digest.
func (m CaptureManifest) ContentDigest() (string, error) {
	canonical, err := m.Canonical()
	if err != nil {
		return "", err
	}
	payload := captureContentDigestPayload{
		SchemaVersion: 1, Selectors: canonical.Selectors, Entries: canonical.Entries, TotalBytes: canonical.TotalBytes,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (m CaptureManifest) Digest() (string, error) {
	canonical, err := m.Canonical()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validateCapturedPath(value string) error {
	base, recursive, err := parseRepoInputSelector(value)
	if err != nil || recursive || base != value {
		return fmt.Errorf("invalid hermetic captured path")
	}
	return nil
}

func ValidateSourceGeneration(value string) error {
	if !strings.HasPrefix(value, "gen_") || !validSHA256(strings.TrimPrefix(value, "gen_")) {
		return fmt.Errorf("invalid hermetic source generation")
	}
	return nil
}

func validSourceGeneration(value string) bool { return ValidateSourceGeneration(value) == nil }

func sameStrings(a, b []string) bool {
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
