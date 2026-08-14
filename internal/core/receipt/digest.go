package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Digest returns a deterministic identity for one validated terminal receipt.
// It is derived from the canonical JSON representation of the closed Receipt
// struct and is intended for immutable derived-record correlation only.
func Digest(rec Receipt) (string, error) {
	if err := rec.Validate(); err != nil {
		return "", err
	}
	if !rec.State.Terminal() {
		return "", fmt.Errorf("receipt is not terminal")
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
