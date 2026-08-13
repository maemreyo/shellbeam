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
		Command   string `json:"command"`
		CWD       string `json:"cwd"`
		TTY       bool   `json:"tty"`
		TimeoutMS int64  `json:"timeout_ms"`
	}{1, i.Command, i.CWD, i.TTY, i.TimeoutMS})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
