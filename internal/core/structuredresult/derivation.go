// Package structuredresult defines deterministic machine-readable execution projections.
package structuredresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

const (
	SchemaVersion          = 1
	MaxSourceAuthorityRefs = 8
)

type Lifecycle string
type ParseOutcome string
type Completeness string

const (
	LifecyclePending    Lifecycle = "pending"
	LifecycleProcessing Lifecycle = "processing"
	LifecycleTerminal   Lifecycle = "terminal"

	ParseComplete       ParseOutcome = "complete"
	ParsePartial        ParseOutcome = "partial"
	ParseMalformed      ParseOutcome = "malformed"
	ParseUnavailable    ParseOutcome = "unavailable"
	ParseBudgetExceeded ParseOutcome = "budget_exceeded"

	CompletenessComplete    Completeness = "complete"
	CompletenessPartial     Completeness = "partial"
	CompletenessUnavailable Completeness = "unavailable"
	CompletenessCompacted   Completeness = "compacted"
)

type RawOutputRef struct {
	SessionID string `json:"session_id"`
	StartByte int64  `json:"start_byte"`
	EndByte   int64  `json:"end_byte"`
	SHA256    string `json:"sha256"`
}

type Producer struct {
	AdapterID         string `json:"adapter_id"`
	AdapterVersion    int    `json:"adapter_version"`
	CapabilityVersion int    `json:"capability_version"`
}

type Derivation struct {
	SchemaVersion           int            `json:"schema_version"`
	DerivationKey           string         `json:"derivation_key"`
	SourceAuthorityRefs     []RawOutputRef `json:"source_authority_refs"`
	Producer                Producer       `json:"producer"`
	DerivationSchemaVersion int            `json:"derivation_schema_version"`
	DerivationConfigDigest  string         `json:"derivation_config_digest"`
	Lifecycle               Lifecycle      `json:"lifecycle"`
	ParseOutcome            ParseOutcome   `json:"parse_outcome,omitempty"`
	Completeness            Completeness   `json:"completeness"`
}

func DerivationKey(refs []RawOutputRef, producer Producer, schemaVersion int, configDigest string) (string, error) {
	if len(refs) == 0 || len(refs) > MaxSourceAuthorityRefs {
		return "", fmt.Errorf("invalid source authority refs")
	}
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return "", err
		}
	}
	if err := producer.Validate(); err != nil || schemaVersion < 1 || !validDigest(configDigest) {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("invalid derivation identity")
	}
	b, err := json.Marshal(struct {
		Refs     []RawOutputRef `json:"refs"`
		Producer Producer       `json:"producer"`
		Schema   int            `json:"schema"`
		Config   string         `json:"config"`
	}{refs, producer, schemaVersion, configDigest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (d Derivation) Validate() error {
	if d.SchemaVersion != SchemaVersion || !validDigest(d.DerivationKey) || len(d.SourceAuthorityRefs) == 0 || len(d.SourceAuthorityRefs) > MaxSourceAuthorityRefs || d.DerivationSchemaVersion < 1 || !validDigest(d.DerivationConfigDigest) || d.Producer.Validate() != nil || !validCompleteness(d.Completeness) {
		return fmt.Errorf("invalid structured derivation")
	}
	for _, ref := range d.SourceAuthorityRefs {
		if err := ref.Validate(); err != nil {
			return err
		}
	}
	switch d.Lifecycle {
	case LifecyclePending, LifecycleProcessing:
		if d.ParseOutcome != "" {
			return fmt.Errorf("parse outcome before terminal")
		}
	case LifecycleTerminal:
		if !validParseOutcome(d.ParseOutcome) {
			return fmt.Errorf("terminal parse outcome missing")
		}
	default:
		return fmt.Errorf("invalid derivation lifecycle")
	}
	return nil
}

func (r RawOutputRef) Validate() error {
	if strings.TrimSpace(r.SessionID) == "" || r.StartByte < 0 || r.EndByte <= r.StartByte || !validDigest(r.SHA256) {
		return fmt.Errorf("invalid raw output ref")
	}
	return nil
}
func (p Producer) Validate() error {
	if !safeStructuredText(p.AdapterID, 128) || p.AdapterVersion < 1 || p.CapabilityVersion < 1 {
		return fmt.Errorf("invalid producer")
	}
	return nil
}
func safeStructuredText(v string, max int) bool {
	if strings.TrimSpace(v) == "" || strings.TrimSpace(v) != v || len(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
func validParseOutcome(v ParseOutcome) bool {
	switch v {
	case ParseComplete, ParsePartial, ParseMalformed, ParseUnavailable, ParseBudgetExceeded:
		return true
	}
	return false
}
func validCompleteness(v Completeness) bool {
	switch v {
	case CompletenessComplete, CompletenessPartial, CompletenessUnavailable, CompletenessCompacted:
		return true
	}
	return false
}
