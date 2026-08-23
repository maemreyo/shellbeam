package capability

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const RuntimeIdentitySchemaVersion = 1

type RuntimeIdentity struct {
	SchemaVersion     int    `json:"schema_version"`
	Version           string `json:"version,omitempty"`
	Revision          string `json:"revision,omitempty"`
	VCSModified       *bool  `json:"vcs_modified,omitempty"`
	BinarySHA256      string `json:"binary_sha256,omitempty"`
	DaemonIncarnation string `json:"daemon_incarnation,omitempty"`
	DaemonStartedAt   string `json:"daemon_started_at,omitempty"`
}

func (r RuntimeIdentity) Validate() error {
	if r.SchemaVersion != RuntimeIdentitySchemaVersion {
		return fmt.Errorf("invalid runtime identity schema version")
	}
	for name, value := range map[string]string{
		"version":            r.Version,
		"revision":           r.Revision,
		"daemon_incarnation": r.DaemonIncarnation,
	} {
		if len(value) > 128 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("invalid runtime identity %s", name)
		}
	}
	if r.BinarySHA256 != "" {
		if len(r.BinarySHA256) != 64 {
			return fmt.Errorf("invalid runtime binary sha256")
		}
		decoded, err := hex.DecodeString(r.BinarySHA256)
		if err != nil || len(decoded) != 32 || strings.ToLower(r.BinarySHA256) != r.BinarySHA256 {
			return fmt.Errorf("invalid runtime binary sha256")
		}
	}
	if r.DaemonStartedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, r.DaemonStartedAt); err != nil {
			return fmt.Errorf("invalid daemon start time")
		}
	}
	return nil
}

func (r RuntimeIdentity) Clone() RuntimeIdentity {
	out := r
	if r.VCSModified != nil {
		value := *r.VCSModified
		out.VCSModified = &value
	}
	return out
}
