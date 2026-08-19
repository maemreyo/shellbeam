package structuredresult

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	ArtifactBlobSchemaVersion     = 1
	TerminalCutSchemaVersion      = 1
	ObservationCutSchemaVersion   = 1
	structuredInputEnvelopeV2     = 2
	maxStructuredArtifactPathSize = 4096
)

type StructuredInputKind string

const (
	StructuredInputRawOutput    StructuredInputKind = "raw_output"
	StructuredInputArtifactBlob StructuredInputKind = "artifact_blob"
)

type StructuredInputRef struct {
	Kind         StructuredInputKind `json:"kind"`
	RawOutput    *RawOutputRef       `json:"raw_output,omitempty"`
	ArtifactBlob *ArtifactBlobRef    `json:"artifact_blob,omitempty"`
}

type TerminalCutV1 struct {
	SchemaVersion        int    `json:"schema_version"`
	ReceiptSchemaVersion int    `json:"receipt_schema_version"`
	ReceiptDigest        string `json:"receipt_digest"`
}

type ObservationCutV1 struct {
	SchemaVersion int    `json:"schema_version"`
	Digest        string `json:"digest"`
}

type ArtifactBlobRef struct {
	SchemaVersion           int              `json:"schema_version"`
	BlobID                  string           `json:"blob_id"`
	OperationID             string           `json:"operation_id"`
	SessionID               string           `json:"session_id"`
	RepositoryID            string           `json:"repository_id"`
	WorkspaceID             string           `json:"workspace_id"`
	DeclaredPath            string           `json:"declared_path"`
	NormalizedWorkspacePath string           `json:"normalized_workspace_path"`
	SHA256                  string           `json:"sha256"`
	Size                    int64            `json:"size"`
	TerminalCut             TerminalCutV1    `json:"terminal_cut"`
	ObservationCut          ObservationCutV1 `json:"observation_cut"`
}

func (r StructuredInputRef) Validate() error {
	branches := 0
	if r.RawOutput != nil {
		branches++
	}
	if r.ArtifactBlob != nil {
		branches++
	}
	if branches != 1 {
		return fmt.Errorf("structured input requires exactly one branch")
	}
	switch r.Kind {
	case StructuredInputRawOutput:
		if r.RawOutput == nil || r.ArtifactBlob != nil {
			return fmt.Errorf("structured input kind/branch mismatch")
		}
		return r.RawOutput.Validate()
	case StructuredInputArtifactBlob:
		if r.ArtifactBlob == nil || r.RawOutput != nil {
			return fmt.Errorf("structured input kind/branch mismatch")
		}
		return r.ArtifactBlob.Validate()
	default:
		return fmt.Errorf("invalid structured input kind")
	}
}

func (r ArtifactBlobRef) Validate() error {
	if r.SchemaVersion != ArtifactBlobSchemaVersion || !validArtifactBlobID(r.BlobID) {
		return fmt.Errorf("invalid artifact blob identity")
	}
	if _, err := operation.ParseID(r.OperationID); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(r.SessionID); err != nil {
		return err
	}
	if _, err := workspace.ParseRepositoryID(r.RepositoryID); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return err
	}
	if !safeStructuredText(r.DeclaredPath, maxStructuredArtifactPathSize) || !validNormalizedWorkspacePath(r.NormalizedWorkspacePath) {
		return fmt.Errorf("invalid artifact blob path")
	}
	if !validDigest(r.SHA256) || r.Size < 0 || r.TerminalCut.Validate() != nil || r.ObservationCut.Validate() != nil {
		return fmt.Errorf("invalid artifact blob content authority")
	}
	return nil
}

func (c TerminalCutV1) Validate() error {
	if c.SchemaVersion != TerminalCutSchemaVersion || c.ReceiptSchemaVersion < 1 || !validDigest(c.ReceiptDigest) {
		return fmt.Errorf("invalid terminal cut")
	}
	return nil
}

func (c ObservationCutV1) Validate() error {
	if c.SchemaVersion != ObservationCutSchemaVersion || !validDigest(c.Digest) {
		return fmt.Errorf("invalid observation cut")
	}
	return nil
}

func DerivationKeyForInputs(refs []StructuredInputRef, producer Producer, schemaVersion int, configDigest string) (string, error) {
	if len(refs) == 0 || len(refs) > MaxSourceAuthorityRefs {
		return "", fmt.Errorf("invalid source authority refs")
	}
	rawRefs := make([]RawOutputRef, 0, len(refs))
	allRaw := true
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return "", err
		}
		if ref.Kind != StructuredInputRawOutput {
			allRaw = false
			continue
		}
		rawRefs = append(rawRefs, *ref.RawOutput)
	}
	if allRaw {
		return DerivationKey(rawRefs, producer, schemaVersion, configDigest)
	}
	if err := producer.Validate(); err != nil {
		return "", err
	}
	if schemaVersion < 1 || !validDigest(configDigest) {
		return "", fmt.Errorf("invalid derivation identity")
	}
	encoded, err := json.Marshal(struct {
		Version  int                  `json:"version"`
		Refs     []StructuredInputRef `json:"refs"`
		Producer Producer             `json:"producer"`
		Schema   int                  `json:"schema"`
		Config   string               `json:"config"`
	}{structuredInputEnvelopeV2, refs, producer, schemaVersion, configDigest})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validArtifactBlobID(value string) bool {
	return strings.HasPrefix(value, "abl_") && validDigest(strings.TrimPrefix(value, "abl_"))
}

func validNormalizedWorkspacePath(value string) bool {
	if !safeStructuredText(value, maxStructuredArtifactPathSize) || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
