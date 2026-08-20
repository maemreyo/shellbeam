package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

type structuredSchemaHeader struct {
	SchemaVersion int `json:"schema_version"`
}

type legacyDerivationV1 struct {
	SchemaVersion           int                 `json:"schema_version"`
	DerivationKey           string              `json:"derivation_key"`
	SourceAuthorityRefs     []core.RawOutputRef `json:"source_authority_refs"`
	Producer                core.Producer       `json:"producer"`
	DerivationSchemaVersion int                 `json:"derivation_schema_version"`
	DerivationConfigDigest  string              `json:"derivation_config_digest"`
	Lifecycle               core.Lifecycle      `json:"lifecycle"`
	ParseOutcome            core.ParseOutcome   `json:"parse_outcome,omitempty"`
	Completeness            core.Completeness   `json:"completeness"`
}

type legacyRecordV1 struct {
	SchemaVersion    int                   `json:"schema_version"`
	RecordKind       core.RecordKind       `json:"record_kind"`
	Authority        core.Authority        `json:"authority"`
	DerivationMethod core.DerivationMethod `json:"derivation_method"`
	Producer         core.Producer         `json:"producer"`
	OperationID      string                `json:"operation_id"`
	SourceRef        core.RawOutputRef     `json:"source_ref"`
	Diagnostic       *core.Diagnostic      `json:"diagnostic,omitempty"`
	TestCase         *core.TestCase        `json:"test_case,omitempty"`
	TestSuite        *core.TestSuite       `json:"test_suite,omitempty"`
	ArtifactResult   *core.ArtifactResult  `json:"artifact_result,omitempty"`
}

type legacyRecordSetV1 struct {
	SchemaVersion int              `json:"schema_version"`
	DerivationKey string           `json:"derivation_key"`
	Records       []legacyRecordV1 `json:"records"`
}

func readStructuredDerivation(path string) (core.Derivation, error) {
	raw, err := readStructuredRaw(path, maxStructuredMetadataBytes)
	if err != nil {
		return core.Derivation{}, err
	}
	return decodeStructuredDerivation(raw)
}

func decodeStructuredDerivation(raw []byte) (core.Derivation, error) {
	var header structuredSchemaHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return core.Derivation{}, err
	}
	switch header.SchemaVersion {
	case core.SchemaVersionV1:
		var legacy legacyDerivationV1
		if err := decodeStructuredStrict(raw, &legacy); err != nil {
			return core.Derivation{}, err
		}
		return normalizeLegacyDerivation(legacy)
	case core.SchemaVersion, core.DerivationSchemaVersionV3:
		var current core.Derivation
		if err := decodeStructuredStrict(raw, &current); err != nil {
			return core.Derivation{}, err
		}
		return current, validateStructuredDerivation(current)
	default:
		return core.Derivation{}, fmt.Errorf("unsupported_structured_derivation_schema")
	}
}

func readStructuredRecordSet(path string, derivation core.Derivation) (structuredRecordSet, error) {
	raw, err := readStructuredRaw(path, maxStructuredRecordFileBytes)
	if err != nil {
		return structuredRecordSet{}, err
	}
	return decodeStructuredRecordSet(raw, derivation)
}

func decodeStructuredRecordSet(raw []byte, derivation core.Derivation) (structuredRecordSet, error) {
	var header structuredSchemaHeader
	if err := json.Unmarshal(raw, &header); err != nil {
		return structuredRecordSet{}, err
	}
	switch header.SchemaVersion {
	case core.SchemaVersionV1:
		var legacy legacyRecordSetV1
		if err := decodeStructuredStrict(raw, &legacy); err != nil {
			return structuredRecordSet{}, err
		}
		set := normalizeLegacyRecordSet(legacy)
		return set, validateStructuredRecordSet(set, derivation)
	case core.SchemaVersion:
		var current structuredRecordSet
		if err := decodeStructuredStrict(raw, &current); err != nil {
			return structuredRecordSet{}, err
		}
		return current, validateStructuredRecordSet(current, derivation)
	default:
		return structuredRecordSet{}, fmt.Errorf("unsupported_structured_record_schema")
	}
}

func normalizeLegacyDerivation(legacy legacyDerivationV1) (core.Derivation, error) {
	refs := make([]core.StructuredInputRef, len(legacy.SourceAuthorityRefs))
	for i, ref := range legacy.SourceAuthorityRefs {
		refs[i] = core.RawInputRef(ref)
	}
	derivation := core.Derivation{
		SchemaVersion: legacy.SchemaVersion, DerivationKey: legacy.DerivationKey, SourceAuthorityRefs: refs,
		Producer: legacy.Producer, DerivationSchemaVersion: legacy.DerivationSchemaVersion, DerivationConfigDigest: legacy.DerivationConfigDigest,
		Lifecycle: legacy.Lifecycle, ParseOutcome: legacy.ParseOutcome, Completeness: legacy.Completeness,
	}
	return derivation, validateStructuredDerivation(derivation)
}

func normalizeLegacyRecordSet(legacy legacyRecordSetV1) structuredRecordSet {
	records := make([]core.Record, len(legacy.Records))
	for i, record := range legacy.Records {
		records[i] = core.Record{
			SchemaVersion: record.SchemaVersion, RecordKind: record.RecordKind, Authority: record.Authority,
			DerivationMethod: record.DerivationMethod, Producer: record.Producer, OperationID: record.OperationID,
			SourceRef: core.RawInputRef(record.SourceRef), Diagnostic: record.Diagnostic, TestCase: record.TestCase,
			TestSuite: record.TestSuite, ArtifactResult: record.ArtifactResult,
		}
	}
	return structuredRecordSet{SchemaVersion: legacy.SchemaVersion, DerivationKey: legacy.DerivationKey, Records: records}
}

func readStructuredRaw(path string, maxBytes int64) ([]byte, error) {
	var raw json.RawMessage
	if err := readPrivateJSON(path, maxBytes, &raw); err != nil {
		return nil, err
	}
	return append([]byte(nil), raw...), nil
}

func decodeStructuredStrict(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing structured json")
	}
	return nil
}

func sameDerivationReplay(a, b core.Derivation) bool {
	return a.DerivationKey == b.DerivationKey && reflect.DeepEqual(a.SourceAuthorityRefs, b.SourceAuthorityRefs) && a.Producer == b.Producer &&
		a.DerivationSchemaVersion == b.DerivationSchemaVersion && a.DerivationConfigDigest == b.DerivationConfigDigest &&
		a.Lifecycle == b.Lifecycle && a.ParseOutcome == b.ParseOutcome && a.Completeness == b.Completeness &&
		a.CompletenessReason == b.CompletenessReason && reflect.DeepEqual(a.ObservedEntries, b.ObservedEntries) && reflect.DeepEqual(a.SemanticsCoverage, b.SemanticsCoverage)
}

func sameRecordSetReplay(current, next structuredRecordSet) bool {
	if reflect.DeepEqual(current, next) {
		return true
	}
	if current.SchemaVersion != core.SchemaVersionV1 || next.SchemaVersion != core.SchemaVersion || current.DerivationKey != next.DerivationKey || len(current.Records) != len(next.Records) {
		return false
	}
	for i := range current.Records {
		legacy := current.Records[i]
		modern := next.Records[i]
		if legacy.SchemaVersion != core.SchemaVersionV1 || modern.SchemaVersion != core.SchemaVersion {
			return false
		}
		legacy.SchemaVersion = core.SchemaVersion
		if !reflect.DeepEqual(legacy, modern) {
			return false
		}
	}
	return true
}
