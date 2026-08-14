package observation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/observation"
)

const (
	EventCursorPrefix   = "evtcur_v1_"
	MaxEventCursorBytes = 2048
)

type cursorPayload struct {
	SchemaVersion     int            `json:"schema_version"`
	StateRootEpoch    string         `json:"state_root_epoch"`
	KeyGeneration     string         `json:"key_generation"`
	AfterSeq          core.ChangeSeq `json:"after_seq"`
	TargetFingerprint string         `json:"target_fingerprint"`
}

type CursorCodec struct {
	key core.CursorKeyMaterial
}

func NewCursorCodec(key core.CursorKeyMaterial) (*CursorCodec, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return &CursorCodec{key: key}, nil
}

func (c *CursorCodec) Encode(target core.Target, seq core.ChangeSeq) (string, error) {
	fingerprint, err := targetFingerprint(target)
	if err != nil {
		return "", err
	}
	payload := cursorPayload{SchemaVersion: 1, StateRootEpoch: c.key.StateRootEpoch, KeyGeneration: c.key.Generation, AfterSeq: seq, TargetFingerprint: fingerprint}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(encoded)
	sig := c.sign([]byte(body))
	token := EventCursorPrefix + body + "." + base64.RawURLEncoding.EncodeToString(sig)
	if len(token) > MaxEventCursorBytes {
		return "", fmt.Errorf("event cursor exceeds limit")
	}
	return token, nil
}

func (c *CursorCodec) Decode(token string, target core.Target) (core.ChangeSeq, error) {
	if len(token) <= len(EventCursorPrefix) || len(token) > MaxEventCursorBytes || !strings.HasPrefix(token, EventCursorPrefix) {
		return 0, cursorFailure(failure.EventCursorInvalid, "format")
	}
	parts := strings.Split(strings.TrimPrefix(token, EventCursorPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, cursorFailure(failure.EventCursorInvalid, "format")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payloadBytes) > 1024 {
		return 0, cursorFailure(failure.EventCursorInvalid, "payload")
	}
	var payload cursorPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.SchemaVersion != 1 || payload.TargetFingerprint == "" {
		return 0, cursorFailure(failure.EventCursorInvalid, "payload")
	}
	if payload.StateRootEpoch != c.key.StateRootEpoch || payload.KeyGeneration != c.key.Generation {
		return 0, cursorFailure(failure.EventCursorExpired, "epoch")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, c.sign([]byte(parts[0]))) {
		return 0, cursorFailure(failure.EventCursorInvalid, "integrity")
	}
	fingerprint, err := targetFingerprint(target)
	if err != nil || payload.TargetFingerprint != fingerprint {
		return 0, cursorFailure(failure.EventCursorInvalid, "target")
	}
	return payload.AfterSeq, nil
}

func (c *CursorCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key.Secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func targetFingerprint(target core.Target) (string, error) {
	if err := target.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func cursorFailure(code failure.Code, reason string) error {
	return failure.New(code, map[string]string{"reason": reason}, nil)
}
