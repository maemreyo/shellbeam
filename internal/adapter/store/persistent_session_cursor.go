package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

const (
	persistentSessionCursorPrefix   = "pscur_v1_"
	maxPersistentSessionCursorBytes = 2048
)

type persistentBindingFilter struct {
	SessionName    string `json:"session_name,omitempty"`
	ActivityID     string `json:"activity_id,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
	State          string `json:"state,omitempty"`
	PersistentOnly bool   `json:"persistent_only"`
}

type persistentCursorPosition struct {
	CreatedAt time.Time `json:"created_at"`
	SessionID string    `json:"session_id"`
}

type persistentCursorPayload struct {
	SchemaVersion     int                      `json:"schema_version"`
	StateRootEpoch    string                   `json:"state_root_epoch"`
	KeyGeneration     string                   `json:"key_generation"`
	FilterFingerprint string                   `json:"filter_fingerprint"`
	Cut               persistentCursorPosition `json:"cut"`
	After             persistentCursorPosition `json:"after"`
}

type persistentSessionCursorCodec struct {
	key observation.CursorKeyMaterial
}

func newPersistentSessionCursorCodec(key observation.CursorKeyMaterial) (*persistentSessionCursorCodec, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return &persistentSessionCursorCodec{key: key}, nil
}

func (c *persistentSessionCursorCodec) Encode(filter persistentBindingFilter, cut, after persistentCursorPosition) (string, error) {
	if err := validatePersistentCursorPosition(cut); err != nil {
		return "", err
	}
	if err := validatePersistentCursorPosition(after); err != nil {
		return "", err
	}
	if comparePersistentPosition(after, cut) > 0 {
		return "", fmt.Errorf("persistent session cursor after cut")
	}
	fingerprint, err := persistentBindingFilterFingerprint(filter)
	if err != nil {
		return "", err
	}
	payload := persistentCursorPayload{
		SchemaVersion: 1, StateRootEpoch: c.key.StateRootEpoch, KeyGeneration: c.key.Generation,
		FilterFingerprint: fingerprint, Cut: cut, After: after,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	token := persistentSessionCursorPrefix + body + "." + base64.RawURLEncoding.EncodeToString(c.sign([]byte(body)))
	if len(token) > maxPersistentSessionCursorBytes {
		return "", fmt.Errorf("persistent session cursor exceeds limit")
	}
	return token, nil
}

func (c *persistentSessionCursorCodec) Decode(token string, filter persistentBindingFilter) (persistentCursorPosition, persistentCursorPosition, error) {
	fail := func(reason string) (persistentCursorPosition, persistentCursorPosition, error) {
		return persistentCursorPosition{}, persistentCursorPosition{}, failure.New(failure.InvalidInput, map[string]string{"reason": reason}, nil)
	}
	if len(token) <= len(persistentSessionCursorPrefix) || len(token) > maxPersistentSessionCursorBytes || !strings.HasPrefix(token, persistentSessionCursorPrefix) {
		return fail("persistent_session_continuation_invalid")
	}
	parts := strings.Split(strings.TrimPrefix(token, persistentSessionCursorPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fail("persistent_session_continuation_invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) > 1536 || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return fail("persistent_session_continuation_invalid")
	}
	var payload persistentCursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.SchemaVersion != 1 || validatePersistentCursorPosition(payload.Cut) != nil || validatePersistentCursorPosition(payload.After) != nil || comparePersistentPosition(payload.After, payload.Cut) > 0 {
		return fail("persistent_session_continuation_invalid")
	}
	if payload.StateRootEpoch != c.key.StateRootEpoch || payload.KeyGeneration != c.key.Generation {
		return fail("persistent_session_continuation_expired")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != parts[1] || !hmac.Equal(sig, c.sign([]byte(parts[0]))) {
		return fail("persistent_session_continuation_invalid")
	}
	fingerprint, err := persistentBindingFilterFingerprint(filter)
	if err != nil || fingerprint != payload.FilterFingerprint {
		return fail("persistent_session_continuation_invalid")
	}
	return payload.Cut, payload.After, nil
}

func (c *persistentSessionCursorCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key.Secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func persistentBindingFilterFingerprint(filter persistentBindingFilter) (string, error) {
	raw, err := json.Marshal(filter)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validatePersistentCursorPosition(value persistentCursorPosition) error {
	if value.CreatedAt.IsZero() {
		return fmt.Errorf("persistent session cursor timestamp missing")
	}
	if _, err := operation.ParseSessionID(value.SessionID); err != nil {
		return err
	}
	return nil
}

func comparePersistentPosition(a, b persistentCursorPosition) int {
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return 1
	}
	return strings.Compare(a.SessionID, b.SessionID)
}

func persistentPosition(binding persistent.Binding) persistentCursorPosition {
	return persistentCursorPosition{CreatedAt: binding.CreatedAt.UTC(), SessionID: binding.SessionID}
}
