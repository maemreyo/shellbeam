package observation

import (
	"encoding/hex"
	"fmt"
	"strings"
)

type CursorKeyMaterial struct {
	StateRootEpoch string
	Generation     string
	Secret         []byte
}

func (k CursorKeyMaterial) Validate() error {
	if !validTypedHex(k.StateRootEpoch, "epoch_", 16) || !validTypedHex(k.Generation, "key_", 16) || len(k.Secret) != 32 {
		return fmt.Errorf("invalid event cursor key material")
	}
	return nil
}

func validTypedHex(value, prefix string, bytes int) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	raw := strings.TrimPrefix(value, prefix)
	if len(raw) != bytes*2 {
		return false
	}
	decoded, err := hex.DecodeString(raw)
	return err == nil && len(decoded) == bytes
}

type ProjectionState struct {
	SchemaVersion          int       `json:"schema_version"`
	MaterializedThroughSeq ChangeSeq `json:"materialized_through_seq"`
	CompactedThroughSeq    ChangeSeq `json:"compacted_through_seq"`
}

func (s ProjectionState) Validate() error {
	if s.SchemaVersion != SchemaVersion || s.CompactedThroughSeq > s.MaterializedThroughSeq {
		return fmt.Errorf("invalid event projection state")
	}
	return nil
}
