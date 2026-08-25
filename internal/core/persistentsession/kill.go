package persistentsession

import (
	"fmt"
	"time"
)

const KillRecordSchemaVersion = 1

type KillRecord struct {
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	KillID        string    `json:"kill_id"`
	Signal        string    `json:"signal"`
	Attempted     bool      `json:"attempted"`
	Succeeded     bool      `json:"succeeded"`
	Needed        bool      `json:"needed"`
	Complete      bool      `json:"complete"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func validKillID(v string) bool {
	if len(v) < 1 || len(v) > 128 || !isKillIDAlphaNum(v[0]) {
		return false
	}
	for i := 1; i < len(v); i++ {
		if !isKillIDAlphaNum(v[i]) && v[i] != '_' && v[i] != '-' {
			return false
		}
	}
	return true
}

func isKillIDAlphaNum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func (r KillRecord) Validate() error {
	if r.SchemaVersion != KillRecordSchemaVersion || !validOpaque(r.SessionID) || !validKillID(r.KillID) {
		return fmt.Errorf("invalid persistent kill record")
	}
	if r.Signal != "INT" && r.Signal != "TERM" && r.Signal != "KILL" {
		return fmt.Errorf("invalid persistent kill signal")
	}
	if r.Succeeded && !r.Attempted || !r.Complete && (r.Attempted || r.Succeeded) {
		return fmt.Errorf("invalid persistent kill attempt")
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) {
		return fmt.Errorf("invalid persistent kill timestamps")
	}
	return nil
}
