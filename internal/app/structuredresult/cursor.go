package structuredresult

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	observation "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const (
	ResultCursorPrefix   = "rescur_v1_"
	MaxResultCursorBytes = 2048
)

type resultCursorPayload struct {
	SchemaVersion     int    `json:"schema_version"`
	StateRootEpoch    string `json:"state_root_epoch"`
	KeyGeneration     string `json:"key_generation"`
	OperationID       string `json:"operation_id"`
	DerivationKey     string `json:"derivation_key"`
	FilterFingerprint string `json:"filter_fingerprint"`
	MatchOffset       int    `json:"match_offset"`
}

type ResultCursorCodec struct{ key observation.CursorKeyMaterial }

func NewResultCursorCodec(key observation.CursorKeyMaterial) (*ResultCursorCodec, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return &ResultCursorCodec{key: key}, nil
}

func (c *ResultCursorCodec) Encode(operationID, derivationKey string, filter RecordFilter, offset int) (string, error) {
	if _, err := operation.ParseID(operationID); err != nil || !validDerivationKey(derivationKey) || offset < 0 {
		return "", fmt.Errorf("invalid structured cursor input")
	}
	fingerprint, err := filterFingerprint(filter)
	if err != nil {
		return "", err
	}
	payload := resultCursorPayload{SchemaVersion: 1, StateRootEpoch: c.key.StateRootEpoch, KeyGeneration: c.key.Generation, OperationID: operationID, DerivationKey: derivationKey, FilterFingerprint: fingerprint, MatchOffset: offset}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(encoded)
	sig := c.sign([]byte(body))
	token := ResultCursorPrefix + body + "." + base64.RawURLEncoding.EncodeToString(sig)
	if len(token) > MaxResultCursorBytes {
		return "", fmt.Errorf("structured cursor exceeds limit")
	}
	return token, nil
}

func (c *ResultCursorCodec) Decode(token, operationID, derivationKey string, filter RecordFilter) (int, error) {
	if len(token) <= len(ResultCursorPrefix) || len(token) > MaxResultCursorBytes || !strings.HasPrefix(token, ResultCursorPrefix) {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	parts := strings.Split(strings.TrimPrefix(token, ResultCursorPrefix), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(payloadBytes) > 1024 {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	var payload resultCursorPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.SchemaVersion != 1 || payload.MatchOffset < 0 {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	if payload.StateRootEpoch != c.key.StateRootEpoch || payload.KeyGeneration != c.key.Generation {
		return 0, resultCursorFailure("structured_continuation_expired")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, c.sign([]byte(parts[0]))) {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	fingerprint, err := filterFingerprint(filter)
	if err != nil {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	if payload.OperationID != operationID || payload.DerivationKey != derivationKey || payload.FilterFingerprint != fingerprint {
		return 0, resultCursorFailure("structured_continuation_invalid")
	}
	return payload.MatchOffset, nil
}

func (c *ResultCursorCodec) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key.Secret)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}
func filterFingerprint(filter RecordFilter) (string, error) {
	if err := filter.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(filter)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func validDerivationKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}
func resultCursorFailure(reason string) error {
	return failure.New(failure.InvalidInput, map[string]string{"reason": reason}, nil)
}
