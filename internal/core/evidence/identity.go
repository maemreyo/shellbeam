package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func EvidenceID(receiptDigest, contractDigest string) (string, error) {
	if !validDigest(receiptDigest) || !validDigest(contractDigest) {
		return "", fmt.Errorf("invalid evidence authority digest")
	}
	encoded, err := json.Marshal(struct {
		SchemaVersion  int    `json:"schema_version"`
		ReceiptDigest  string `json:"receipt_digest"`
		ContractDigest string `json:"contract_digest"`
	}{SchemaVersion: SchemaVersion, ReceiptDigest: receiptDigest, ContractDigest: contractDigest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "ev_" + hex.EncodeToString(sum[:]), nil
}
