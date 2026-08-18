package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type ResourceLimitKind string

const (
	ResourceLimitMemory    ResourceLimitKind = "memory"
	ResourceLimitProcesses ResourceLimitKind = "processes"
	ResourceLimitCPUTime   ResourceLimitKind = "cpu_time"
)

// ResourceLimits is the caller-visible hard execution budget. A zero field is
// omitted; a non-nil all-zero value is invalid because an explicit empty
// contract must not be folded into saying nothing.
type ResourceLimits struct {
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	Processes   int   `json:"processes,omitempty"`
	CPUTimeMS   int64 `json:"cpu_time_ms,omitempty"`
}

func (l ResourceLimits) Empty() bool {
	return l.MemoryBytes == 0 && l.Processes == 0 && l.CPUTimeMS == 0
}

func (l ResourceLimits) Validate() error {
	if l.MemoryBytes < 0 {
		return fmt.Errorf("memory_bytes must be positive")
	}
	if l.Processes < 0 {
		return fmt.Errorf("processes must be positive")
	}
	if l.CPUTimeMS < 0 {
		return fmt.Errorf("cpu_time_ms must be positive")
	}
	if l.Empty() {
		return fmt.Errorf("resource limits must name at least one limit")
	}
	return nil
}

func (l *ResourceLimits) Clone() *ResourceLimits {
	if l == nil {
		return nil
	}
	copy := *l
	return &copy
}

func bindResourceFingerprint(kind, base string, limits *ResourceLimits) (string, error) {
	if limits == nil {
		return base, nil
	}
	if err := limits.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Version int            `json:"version"`
		Kind    string         `json:"kind"`
		Base    string         `json:"base_fingerprint"`
		Limits  ResourceLimits `json:"limits"`
	}{Version: 1, Kind: kind, Base: base, Limits: *limits})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
