package inputtrace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (r Request) Fingerprint() (string, error) {
	mode, err := NormalizeMode(r.Mode)
	if err != nil {
		return "", err
	}
	if mode == ModeOff {
		return "", nil
	}
	encoded, err := json.Marshal(struct {
		Version int  `json:"version"`
		Mode    Mode `json:"trace_mode"`
	}{Version: 1, Mode: mode})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (b InstrumentationBinding) Digest() (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
