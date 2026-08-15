package outputview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

const OutputCursorPrefix = "outcur_v1_"

type CursorState struct {
	FrozenCutBytes int64  `json:"frozen_cut_bytes"`
	Offset         int64  `json:"offset"`
	Line           int    `json:"line,omitempty"`
	Progress       int    `json:"progress,omitempty"`
	WithinLine     bool   `json:"within_line,omitempty"`
	Phase          string `json:"phase"`
	Boundary       int64  `json:"boundary,omitempty"`
}

type outputCursorPayload struct {
	SchemaVersion       int         `json:"schema_version"`
	StateRootEpoch      string      `json:"state_root_epoch"`
	KeyGeneration       string      `json:"key_generation"`
	SessionID           string      `json:"session_id"`
	SelectorFingerprint string      `json:"selector_fingerprint"`
	State               CursorState `json:"state"`
}

type CursorCodec struct{ key observation.CursorKeyMaterial }

func NewCursorCodec(key observation.CursorKeyMaterial) (*CursorCodec, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return &CursorCodec{key: key}, nil
}

func (c *CursorCodec) Encode(sessionID string, selector Selector, state CursorState) (string, error) {
	if sessionID == "" || !validCursorState(state) {
		return "", fmt.Errorf("invalid output cursor input")
	}
	fingerprint, err := selector.Fingerprint()
	if err != nil {
		return "", err
	}
	payload := outputCursorPayload{SchemaVersion: 1, StateRootEpoch: c.key.StateRootEpoch, KeyGeneration: c.key.Generation, SessionID: sessionID, SelectorFingerprint: fingerprint, State: state}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(encoded)
	token := OutputCursorPrefix + body + "." + base64.RawURLEncoding.EncodeToString(c.sign([]byte(body)))
	if len(token) > MaxContinuationBytes {
		return "", fmt.Errorf("output cursor exceeds limit")
	}
	return token, nil
}

func (c *CursorCodec) Decode(token, sessionID string, selector Selector) (CursorState, error) {
	if len(token) <= len(OutputCursorPrefix) || len(token) > MaxContinuationBytes || !strings.HasPrefix(token, OutputCursorPrefix) {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "format")
	}
	parts := strings.Split(strings.TrimPrefix(token, OutputCursorPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "format")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payloadBytes) > 1536 {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "payload")
	}
	var payload outputCursorPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.SchemaVersion != 1 || payload.SessionID == "" || payload.SelectorFingerprint == "" || !validCursorState(payload.State) {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "payload")
	}
	if payload.StateRootEpoch != c.key.StateRootEpoch || payload.KeyGeneration != c.key.Generation {
		return CursorState{}, cursorFailure(failure.OutputContinuationExpired, "epoch")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, c.sign([]byte(parts[0]))) {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "integrity")
	}
	fingerprint, err := selector.Fingerprint()
	if err != nil || payload.SessionID != sessionID || payload.SelectorFingerprint != fingerprint {
		return CursorState{}, cursorFailure(failure.OutputContinuationInvalid, "binding")
	}
	return payload.State, nil
}

func (c *CursorCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key.Secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func validCursorState(state CursorState) bool {
	return state.FrozenCutBytes >= 0 && state.Offset >= 0 && state.Offset <= state.FrozenCutBytes && state.Line >= 0 && state.Progress >= 0 && state.Boundary >= 0 && state.Boundary <= state.FrozenCutBytes && state.Phase != ""
}

func cursorFailure(code failure.Code, reason string) error {
	return failure.New(code, map[string]string{"reason": reason}, nil)
}
