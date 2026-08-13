package operation

import "time"

type Reservation struct {
	SchemaVersion                 int       `json:"schema_version"`
	OperationID                   ID        `json:"operation_id"`
	SessionID                     SessionID `json:"session_id"`
	Fingerprint                   string    `json:"fingerprint,omitempty"`
	RequestFingerprint            string    `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint          string    `json:"execution_fingerprint,omitempty"`
	ObservationBindingFingerprint string    `json:"observation_binding_fingerprint,omitempty"`
	Command                       string    `json:"command"`
	CWD                           string    `json:"cwd"`
	TTY                           bool      `json:"tty"`
	TimeoutMS                     int64     `json:"timeout_ms"`
	Shell                         string    `json:"shell"`
	DaemonIncarnation             string    `json:"daemon_incarnation"`
	ControlReservationBytes       int64     `json:"control_reservation_bytes"`
	CreatedAt                     time.Time `json:"created_at"`
}

func (r Reservation) EffectiveRequestFingerprint() string {
	if r.RequestFingerprint != "" {
		return r.RequestFingerprint
	}
	return r.Fingerprint
}
