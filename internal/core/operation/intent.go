package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
)

type Intent struct {
	Command        string `json:"command"`
	CWD            string `json:"cwd"`
	TTY            bool   `json:"tty"`
	TimeoutMS      int64  `json:"timeout_ms"`
	YieldMS        int64  `json:"-"`
	MaxOutputBytes int    `json:"-"`
}

func (i Intent) Fingerprint() (string, error) {
	return i.fingerprint(1, "request", "")
}

func (i Intent) RequestFingerprint() (string, error) {
	return i.fingerprint(2, "request", "")
}

func (i Intent) ExecutionFingerprint(shell string) (string, error) {
	if shell == "" {
		return "", fmt.Errorf("shell is empty")
	}
	return i.fingerprint(2, "execution", shell)
}

func (i Intent) fingerprint(version int, kind, shell string) (string, error) {
	if i.Command == "" {
		return "", fmt.Errorf("command is empty")
	}
	if !filepath.IsAbs(i.CWD) {
		return "", fmt.Errorf("cwd must be absolute")
	}
	if i.TimeoutMS < 0 {
		return "", fmt.Errorf("timeout must be non-negative")
	}
	b, err := json.Marshal(struct {
		Version   int    `json:"version"`
		Kind      string `json:"kind,omitempty"`
		Command   string `json:"command"`
		CWD       string `json:"cwd"`
		TTY       bool   `json:"tty"`
		TimeoutMS int64  `json:"timeout_ms"`
		Shell     string `json:"shell,omitempty"`
	}{version, kind, i.Command, i.CWD, i.TTY, i.TimeoutMS, shell})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

type ObservationBinding struct {
	ActivityID string `json:"activity_id,omitempty"`
}

func (b ObservationBinding) Fingerprint() (string, error) {
	if b.ActivityID == "" {
		return "", nil
	}
	data, err := json.Marshal(struct {
		Version    int    `json:"version"`
		ActivityID string `json:"activity_id,omitempty"`
	}{Version: 1, ActivityID: b.ActivityID})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
