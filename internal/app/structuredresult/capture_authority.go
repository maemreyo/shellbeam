package structuredresult

import (
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const CaptureAuthorityRecordSchemaV1 = 1

type CaptureAuthorityState string

const (
	CaptureAuthorityPrepared             CaptureAuthorityState = "prepared"
	CaptureAuthorityManagedPathCollision CaptureAuthorityState = "managed_path_collision"
	CaptureAuthorityAbandoned            CaptureAuthorityState = "abandoned"
)

type CaptureAuthorityRecord struct {
	SchemaVersion           int                   `json:"schema_version"`
	Authority               CaptureAuthority      `json:"authority"`
	StructuredCaptureDigest string                `json:"structured_capture_digest"`
	State                   CaptureAuthorityState `json:"state"`
}

func NewCaptureAuthorityRecord(authority CaptureAuthority) (CaptureAuthorityRecord, error) {
	if err := authority.Validate(); err != nil {
		return CaptureAuthorityRecord{}, err
	}
	digest, err := authority.StructuredCaptureDigest()
	if err != nil {
		return CaptureAuthorityRecord{}, err
	}
	return CaptureAuthorityRecord{
		SchemaVersion: CaptureAuthorityRecordSchemaV1,
		Authority:     authority, StructuredCaptureDigest: digest, State: CaptureAuthorityPrepared,
	}, nil
}

func (r CaptureAuthorityRecord) Validate() error {
	if r.SchemaVersion != CaptureAuthorityRecordSchemaV1 || r.Authority.Validate() != nil || !validStructuredAuthorityDigest(r.StructuredCaptureDigest) {
		return fmt.Errorf("invalid capture authority record")
	}
	want, err := r.Authority.StructuredCaptureDigest()
	if err != nil || want != r.StructuredCaptureDigest {
		return fmt.Errorf("capture authority digest mismatch")
	}
	switch r.State {
	case CaptureAuthorityPrepared, CaptureAuthorityManagedPathCollision, CaptureAuthorityAbandoned:
		return nil
	default:
		return fmt.Errorf("invalid capture authority state")
	}
}

func (r CaptureAuthorityRecord) AllowsMechanicalCapture() bool {
	return r.Validate() == nil && r.State == CaptureAuthorityPrepared
}

const (
	ArtifactCaptureIntentSchemaV1 = 1
	CaptureAuthoritySchemaV1      = 1
	CaptureBaselineSchemaV1       = 1
	CaptureExpectedRegularFile    = "regular_file"
	CaptureBaselineAbsent         = "absent"
	DefaultMaxArtifactBlobBytes   = int64(16 << 20)
	MaxArtifactBlobBytes          = int64(64 << 20)
)

type CaptureBaselineIdentity struct {
	SchemaVersion   int    `json:"schema_version,omitempty"`
	State           string `json:"state"`
	AuthorityDigest string `json:"authority_digest"`
}

type ArtifactCaptureIntent struct {
	SchemaVersion           int                     `json:"schema_version"`
	OperationID             string                  `json:"operation_id"`
	SessionID               string                  `json:"session_id"`
	RepositoryID            string                  `json:"repository_id"`
	WorkspaceID             string                  `json:"workspace_id"`
	AdapterID               string                  `json:"adapter_id"`
	DeclaredPathToken       string                  `json:"declared_path_token"`
	NormalizedWorkspacePath string                  `json:"normalized_workspace_path"`
	ExpectedKind            string                  `json:"expected_kind"`
	MaxBlobBytes            int64                   `json:"max_blob_bytes"`
	ProducerBindingDigest   string                  `json:"producer_binding_digest"`
	Baseline                CaptureBaselineIdentity `json:"baseline"`
}

type CaptureAuthority struct {
	SchemaVersion    int                        `json:"schema_version"`
	PytestInvocation *PytestInvocationBindingV1 `json:"pytest_invocation,omitempty"`
	Intent           ArtifactCaptureIntent      `json:"intent"`
}

func (b CaptureBaselineIdentity) Validate() error {
	version := b.SchemaVersion
	if version == 0 {
		version = CaptureBaselineSchemaV1
	}
	if version != CaptureBaselineSchemaV1 || b.State != CaptureBaselineAbsent || !validStructuredAuthorityDigest(b.AuthorityDigest) {
		return fmt.Errorf("invalid capture baseline identity")
	}
	return nil
}

func (i ArtifactCaptureIntent) Validate() error {
	if i.SchemaVersion != ArtifactCaptureIntentSchemaV1 {
		return fmt.Errorf("invalid artifact capture intent schema")
	}
	if _, err := operation.ParseID(i.OperationID); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(i.SessionID); err != nil {
		return err
	}
	if _, err := workspace.ParseRepositoryID(i.RepositoryID); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(i.WorkspaceID); err != nil {
		return err
	}
	if !operation.ValidStructuredAdapterID(i.AdapterID) || !boundedAuthorityText(i.DeclaredPathToken, maxPytestTokenBytes) || !validNormalizedCapturePath(i.NormalizedWorkspacePath) ||
		i.ExpectedKind != CaptureExpectedRegularFile || i.MaxBlobBytes < 1 || i.MaxBlobBytes > MaxArtifactBlobBytes || !validStructuredAuthorityDigest(i.ProducerBindingDigest) || i.Baseline.Validate() != nil {
		return fmt.Errorf("invalid artifact capture intent")
	}
	return nil
}

func (i ArtifactCaptureIntent) Digest() (string, error) {
	if err := i.Validate(); err != nil {
		return "", err
	}
	canonical := i
	if canonical.Baseline.SchemaVersion == 0 {
		canonical.Baseline.SchemaVersion = CaptureBaselineSchemaV1
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return digestStructuredAuthority(encoded), nil
}

func (a CaptureAuthority) Validate() error {
	if a.SchemaVersion != CaptureAuthoritySchemaV1 || a.PytestInvocation == nil || a.Intent.Validate() != nil || !a.PytestInvocation.QualifiedV1() {
		return fmt.Errorf("invalid capture authority")
	}
	bindingDigest, err := a.PytestInvocation.ProducerBindingDigest()
	if err != nil || bindingDigest != a.Intent.ProducerBindingDigest || a.Intent.AdapterID != PytestJUnitAdapterID ||
		a.Intent.DeclaredPathToken != a.PytestInvocation.JUnitOutput.DeclaredPathToken || a.Intent.NormalizedWorkspacePath != a.PytestInvocation.JUnitOutput.NormalizedWorkspacePath {
		return fmt.Errorf("capture authority binding mismatch")
	}
	return nil
}

func (a CaptureAuthority) StructuredCaptureDigest() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	return a.Intent.Digest()
}
