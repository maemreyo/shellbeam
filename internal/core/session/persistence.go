package session

import "time"

type Snapshot struct {
	SchemaVersion     int       `json:"schema_version"`
	OperationID       string    `json:"operation_id"`
	SessionID         string    `json:"session_id"`
	DaemonIncarnation string    `json:"daemon_incarnation"`
	State             State     `json:"state"`
	Outcome           Outcome   `json:"outcome"`
	OutputBytes       int64     `json:"output_bytes"`
	OutputAvailable   bool      `json:"output_available"`
	UpdatedAt         time.Time `json:"updated_at"`
}
