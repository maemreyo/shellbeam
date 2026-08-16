package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (r CreateRequest) Fingerprint() (string, error) {
	normalized, err := r.Normalize()
	if err != nil {
		return "", err
	}
	payload := struct {
		SchemaVersion int      `json:"schema_version"`
		Kind          string   `json:"kind"`
		CreateID      string   `json:"checkpoint_create_id"`
		WorkspaceID   string   `json:"workspace_id"`
		ActivityID    string   `json:"activity_id,omitempty"`
		Paths         []string `json:"paths"`
	}{SchemaVersion, "checkpoint_create", normalized.CreateID, normalized.WorkspaceID, normalized.ActivityID, normalized.Paths}
	return digestJSON(payload)
}

func (r RestoreRequest) Fingerprint() (string, error) {
	normalized, err := r.Normalize()
	if err != nil {
		return "", err
	}
	payload := struct {
		SchemaVersion int      `json:"schema_version"`
		Kind          string   `json:"kind"`
		RestoreID     string   `json:"restore_id"`
		CheckpointID  string   `json:"checkpoint_id"`
		Paths         []string `json:"paths"`
	}{SchemaVersion, "checkpoint_restore", normalized.RestoreID, normalized.CheckpointID, normalized.Paths}
	return digestJSON(payload)
}

func digestJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
