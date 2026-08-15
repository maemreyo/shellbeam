package evidence

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/observation"
)

const EvidenceCursorPrefix = "evcur_v1_"
const MaxCursorBytes = core.MaxCursorBytes

type CursorState struct {
	IndexGeneration uint64 `json:"index_generation"`
	AfterSequence   uint64 `json:"after_sequence"`
}
type evidenceCursorPayload struct {
	SchemaVersion     int         `json:"schema_version"`
	StateRootEpoch    string      `json:"state_root_epoch"`
	KeyGeneration     string      `json:"key_generation"`
	FilterFingerprint string      `json:"filter_fingerprint"`
	State             CursorState `json:"state"`
}
type CursorCodec struct{ key observation.CursorKeyMaterial }

func NewCursorCodec(key observation.CursorKeyMaterial) (*CursorCodec, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return &CursorCodec{key: key}, nil
}
func (c *CursorCodec) Encode(filter InspectFilter, state CursorState) (string, error) {
	if state.AfterSequence > state.IndexGeneration {
		return "", fmt.Errorf("invalid evidence cursor state")
	}
	fp, err := filter.fingerprint()
	if err != nil {
		return "", err
	}
	payload := evidenceCursorPayload{SchemaVersion: 1, StateRootEpoch: c.key.StateRootEpoch, KeyGeneration: c.key.Generation, FilterFingerprint: fp, State: state}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	token := EvidenceCursorPrefix + body + "." + base64.RawURLEncoding.EncodeToString(c.sign([]byte(body)))
	if len(token) > MaxCursorBytes {
		return "", fmt.Errorf("evidence cursor exceeds limit")
	}
	return token, nil
}
func (c *CursorCodec) Decode(token string, filter InspectFilter) (CursorState, error) {
	fail := func(code failure.Code, reason string) (CursorState, error) {
		return CursorState{}, failure.New(code, map[string]string{"reason": reason}, nil)
	}
	if len(token) <= len(EvidenceCursorPrefix) || len(token) > MaxCursorBytes || !strings.HasPrefix(token, EvidenceCursorPrefix) {
		return fail(failure.EvidenceCursorInvalid, "format")
	}
	parts := strings.Split(strings.TrimPrefix(token, EvidenceCursorPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fail(failure.EvidenceCursorInvalid, "format")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(raw) > 1536 {
		return fail(failure.EvidenceCursorInvalid, "payload")
	}
	var payload evidenceCursorPayload
	if json.Unmarshal(raw, &payload) != nil || payload.SchemaVersion != 1 || payload.State.AfterSequence > payload.State.IndexGeneration {
		return fail(failure.EvidenceCursorInvalid, "payload")
	}
	if payload.StateRootEpoch != c.key.StateRootEpoch || payload.KeyGeneration != c.key.Generation {
		return fail(failure.EvidenceCursorExpired, "epoch")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, c.sign([]byte(parts[0]))) {
		return fail(failure.EvidenceCursorInvalid, "integrity")
	}
	fp, err := filter.fingerprint()
	if err != nil || fp != payload.FilterFingerprint {
		return fail(failure.EvidenceCursorInvalid, "binding")
	}
	return payload.State, nil
}
func (c *CursorCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key.Secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
