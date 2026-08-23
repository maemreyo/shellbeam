package contextexec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (r Request) Fingerprint() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Version        int      `json:"version"`
		Kind           string   `json:"kind"`
		ContextExecID  string   `json:"context_exec_id"`
		SessionID      string   `json:"session_id"`
		AuthorityEpoch uint64   `json:"authority_epoch"`
		Argv           []string `json:"argv"`
		TimeoutMS      int64    `json:"timeout_ms"`
		MaxOutputBytes int64    `json:"max_output_bytes"`
	}{
		Version: SchemaVersion, Kind: "context_exec_request", ContextExecID: r.ContextExecID,
		SessionID: r.SessionID, AuthorityEpoch: uint64(r.AuthorityEpoch), Argv: r.Argv,
		TimeoutMS: r.TimeoutMS, MaxOutputBytes: r.MaxOutputBytes,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
