package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type environmentCanonical struct {
	Version          int                `json:"version"`
	Platform         Platform           `json:"platform"`
	Execution        ExecutionContext   `json:"execution"`
	PathDigest       string             `json:"path_digest"`
	PathEntryCount   int                `json:"path_entry_count"`
	VariablePresence []VariablePresence `json:"variable_presence,omitempty"`
	ManagerKind      string             `json:"manager_kind,omitempty"`
	ManagerIdentity  string             `json:"manager_identity,omitempty"`
}

type toolchainCanonical struct {
	Version      int                        `json:"version"`
	Observations []toolchainCanonicalRecord `json:"observations"`
}

type toolchainCanonicalRecord struct {
	Kind              string       `json:"kind"`
	RequestedIdentity string       `json:"requested_identity"`
	ObservedIdentity  string       `json:"observed_identity,omitempty"`
	Version           string       `json:"version,omitempty"`
	Quality           ProbeQuality `json:"quality"`
}

func PathFingerprint(raw string) PathObservation {
	entries := []string{}
	if raw != "" {
		entries = strings.Split(raw, ":")
	}
	encoded, _ := json.Marshal(struct {
		Version int      `json:"version"`
		Entries []string `json:"entries"`
	}{Version: FingerprintVersion, Entries: entries})
	return PathObservation{
		Digest:     digestBytes(encoded),
		EntryCount: len(entries),
		Quality:    QualityComplete,
	}
}

func EnvironmentFingerprint(input FingerprintInput) (string, error) {
	if err := validateFingerprintInput(input); err != nil {
		return "", err
	}
	presence := append([]VariablePresence(nil), input.VariablePresence...)
	sort.Slice(presence, func(i, j int) bool { return presence[i].Name < presence[j].Name })
	canonical := environmentCanonical{
		Version:          FingerprintVersion,
		Platform:         input.Platform,
		Execution:        input.Execution,
		PathDigest:       input.Path.Digest,
		PathEntryCount:   input.Path.EntryCount,
		VariablePresence: presence,
	}
	if input.ToolchainManager != nil {
		canonical.ManagerKind = input.ToolchainManager.Kind
		canonical.ManagerIdentity = input.ToolchainManager.Identity
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func ToolchainFingerprint(observations []ToolchainObservation) (string, error) {
	if len(observations) > MaxToolchainObservations {
		return "", fmt.Errorf("too many toolchain observations")
	}
	records := make([]toolchainCanonicalRecord, 0, len(observations))
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		if err := validateToolchainObservation(observation); err != nil {
			return "", err
		}
		if _, ok := seen[observation.Kind]; ok {
			return "", fmt.Errorf("duplicate toolchain kind")
		}
		seen[observation.Kind] = struct{}{}
		records = append(records, toolchainCanonicalRecord{
			Kind:              observation.Kind,
			RequestedIdentity: observation.RequestedIdentity,
			ObservedIdentity:  observation.ObservedIdentity,
			Version:           observation.Version,
			Quality:           observation.Quality,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Kind < records[j].Kind })
	encoded, err := json.Marshal(toolchainCanonical{Version: ToolchainFingerprintVersion, Observations: records})
	if err != nil {
		return "", err
	}
	return digestBytes(encoded), nil
}

func SnapshotID(capturedAt time.Time, environmentFingerprint, toolchainFingerprint string) string {
	encoded, _ := json.Marshal(struct {
		Version                int    `json:"version"`
		CapturedAt             string `json:"captured_at"`
		EnvironmentFingerprint string `json:"environment_fingerprint"`
		ToolchainFingerprint   string `json:"toolchain_fingerprint,omitempty"`
	}{
		Version:                SnapshotSchemaVersion,
		CapturedAt:             capturedAt.UTC().Format(time.RFC3339Nano),
		EnvironmentFingerprint: environmentFingerprint,
		ToolchainFingerprint:   toolchainFingerprint,
	})
	return "env_" + digestBytes(encoded)
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
