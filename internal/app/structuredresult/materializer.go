package structuredresult

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	core "github.com/maemreyo/shellbeam/internal/core/structuredresult"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	ArtifactRecoveryClaimSchemaV1         = 1
	ArtifactCaptureObservationCutSchemaV1 = 1
	ArtifactSourceStabilityStable         = "stable"
	ArtifactBlobMetadataOverhead          = int64(64 << 10)
)

var ErrArtifactChangedDuringCapture = errors.New("artifact_changed_during_capture")

type ArtifactCaptureObservationCutV1 struct {
	SchemaVersion              int    `json:"schema_version"`
	CaptureIntentDigest        string `json:"capture_intent_digest"`
	BaselineAuthorityDigest    string `json:"baseline_authority_digest"`
	SourceObservationScheme    string `json:"source_observation_scheme"`
	PhaseASourceIdentityDigest string `json:"phase_a_source_identity_digest"`
	PhaseASize                 int64  `json:"phase_a_size"`
	FinalSourceIdentityDigest  string `json:"final_source_identity_digest"`
	FinalSize                  int64  `json:"final_size"`
	StabilityResult            string `json:"stability_result"`
}

func (o ArtifactCaptureObservationCutV1) Validate() error {
	if o.SchemaVersion != ArtifactCaptureObservationCutSchemaV1 || !validStructuredAuthorityDigest(o.CaptureIntentDigest) ||
		!validStructuredAuthorityDigest(o.BaselineAuthorityDigest) || o.SourceObservationScheme != ArtifactSourceIdentityUnixV1 ||
		!validStructuredAuthorityDigest(o.PhaseASourceIdentityDigest) || !validStructuredAuthorityDigest(o.FinalSourceIdentityDigest) ||
		o.PhaseASize < 0 || o.FinalSize < 0 || o.PhaseASize != o.FinalSize || o.StabilityResult != ArtifactSourceStabilityStable {
		return fmt.Errorf("invalid artifact capture observation cut")
	}
	return nil
}

func (o ArtifactCaptureObservationCutV1) Digest() (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

type BlobByteReservation struct {
	CaptureAuthorityID string `json:"capture_authority_id"`
	Bytes              int64  `json:"bytes"`
}

func (r BlobByteReservation) Validate() error {
	if !validStructuredAuthorityDigest(r.CaptureAuthorityID) || r.Bytes < 1 || r.Bytes > MaxArtifactBlobBytes+ArtifactBlobMetadataOverhead {
		return fmt.Errorf("invalid blob byte reservation")
	}
	return nil
}

type ArtifactRecoveryClaim struct {
	SchemaVersion      int                `json:"schema_version"`
	CaptureAuthorityID string             `json:"capture_authority_id"`
	BlobID             string             `json:"blob_id"`
	OperationID        string             `json:"operation_id"`
	SessionID          string             `json:"session_id"`
	RepositoryID       string             `json:"repository_id"`
	WorkspaceID        string             `json:"workspace_id"`
	AdapterID          string             `json:"adapter_id"`
	TerminalCut        core.TerminalCutV1 `json:"terminal_cut"`
}

func (c ArtifactRecoveryClaim) Validate() error {
	if c.SchemaVersion != ArtifactRecoveryClaimSchemaV1 || !validStructuredAuthorityDigest(c.CaptureAuthorityID) ||
		!validArtifactBlobIDForApp(c.BlobID) || !operation.ValidStructuredAdapterID(c.AdapterID) || c.TerminalCut.Validate() != nil {
		return fmt.Errorf("invalid artifact recovery claim")
	}
	intent := ArtifactCaptureIntent{
		SchemaVersion: ArtifactCaptureIntentSchemaV1,
		OperationID:   c.OperationID, SessionID: c.SessionID, RepositoryID: c.RepositoryID, WorkspaceID: c.WorkspaceID,
	}
	if _, err := operation.ParseID(intent.OperationID); err != nil {
		return err
	}
	if _, err := operation.ParseSessionID(c.SessionID); err != nil {
		return err
	}
	if _, err := workspace.ParseRepositoryID(c.RepositoryID); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(c.WorkspaceID); err != nil {
		return err
	}
	return nil
}

type ArtifactBlobCommitRequest struct {
	CaptureAuthorityID string
	Intent             ArtifactCaptureIntent
	BlobID             string
	TerminalCut        core.TerminalCutV1
	PreSourceIdentity  ArtifactSourceIdentity
	Source             ArtifactSourceHandle
	Reservation        BlobByteReservation
}

func (r ArtifactBlobCommitRequest) Validate() error {
	if r.Intent.Validate() != nil || !validStructuredAuthorityDigest(r.CaptureAuthorityID) || r.TerminalCut.Validate() != nil ||
		r.PreSourceIdentity.Validate() != nil || r.Source == nil || r.Reservation.Validate() != nil || r.Reservation.CaptureAuthorityID != r.CaptureAuthorityID {
		return fmt.Errorf("invalid artifact blob commit request")
	}
	digest, err := r.Intent.Digest()
	if err != nil || digest != r.CaptureAuthorityID {
		return fmt.Errorf("artifact blob capture authority mismatch")
	}
	blobID, err := ArtifactBlobID(r.Intent)
	if err != nil || blobID != r.BlobID {
		return fmt.Errorf("artifact blob identity mismatch")
	}
	wantBytes, err := artifactBlobReservedBytes(r.PreSourceIdentity.Size)
	if err != nil || wantBytes != r.Reservation.Bytes {
		return fmt.Errorf("artifact blob reservation mismatch")
	}
	return nil
}

type ArtifactBlobRepository interface {
	FindCaptureAuthority(context.Context, operation.ID) (CaptureAuthorityRecord, error)
	ReserveBlobBytes(context.Context, string, int64) (BlobByteReservation, error)
	ReleaseBlobReservation(context.Context, BlobByteReservation) error
	PutRecoveryClaim(context.Context, ArtifactRecoveryClaim) error
	GetRecoveryClaim(context.Context, string) (ArtifactRecoveryClaim, error)
	CommitArtifactBlob(context.Context, ArtifactBlobCommitRequest) (core.ArtifactBlobRef, error)
	ResolveArtifactBlob(context.Context, string) (core.ArtifactBlobRef, error)
}

type Materializer struct{ repository ArtifactBlobRepository }

func NewMaterializer(repository ArtifactBlobRepository) *Materializer {
	return &Materializer{repository: repository}
}

func (m *Materializer) Materialize(ctx context.Context, capture TerminalCaptureResult, rec receipt.Receipt) (core.ArtifactBlobRef, error) {
	if m == nil || m.repository == nil {
		_ = capture.Close()
		return core.ArtifactBlobRef{}, fmt.Errorf("artifact blob repository unavailable")
	}
	defer capture.Close()
	if err := ctx.Err(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	intent, pre, err := m.authorizeMaterialization(ctx, capture, rec)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	reserveBytes, err := artifactBlobReservedBytes(pre.Size)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	reservation, err := m.repository.ReserveBlobBytes(ctx, capture.CaptureAuthorityID, reserveBytes)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	defer m.repository.ReleaseBlobReservation(context.Background(), reservation)
	blobID, err := ArtifactBlobID(intent)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	terminalCut, err := TerminalCutForReceipt(rec)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	claim := ArtifactRecoveryClaim{
		SchemaVersion: ArtifactRecoveryClaimSchemaV1, CaptureAuthorityID: capture.CaptureAuthorityID, BlobID: blobID,
		OperationID: intent.OperationID, SessionID: intent.SessionID, RepositoryID: intent.RepositoryID, WorkspaceID: intent.WorkspaceID,
		AdapterID: intent.AdapterID, TerminalCut: terminalCut,
	}
	if err := claim.Validate(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if err := m.repository.PutRecoveryClaim(ctx, claim); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	request := ArtifactBlobCommitRequest{
		CaptureAuthorityID: capture.CaptureAuthorityID, Intent: intent, BlobID: blobID, TerminalCut: terminalCut,
		PreSourceIdentity: pre, Source: capture.Source(), Reservation: reservation,
	}
	if err := request.Validate(); err != nil {
		return core.ArtifactBlobRef{}, err
	}
	ref, err := m.repository.CommitArtifactBlob(ctx, request)
	if err != nil {
		return core.ArtifactBlobRef{}, err
	}
	if !artifactBlobRefMatchesMaterialization(ref, intent, blobID, terminalCut) {
		return core.ArtifactBlobRef{}, fmt.Errorf("committed artifact blob identity mismatch")
	}
	return ref, nil
}

func (m *Materializer) authorizeMaterialization(ctx context.Context, capture TerminalCaptureResult, rec receipt.Receipt) (ArtifactCaptureIntent, ArtifactSourceIdentity, error) {
	if capture.State != TerminalCaptureAcquired || capture.Source() == nil || capture.BlobBudgetCapability() == nil || capture.SourceIdentity.Validate() != nil {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, fmt.Errorf("invalid acquired terminal capture")
	}
	if err := rec.Validate(); err != nil || !rec.State.Terminal() {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, fmt.Errorf("invalid terminal receipt")
	}
	opID, err := operation.ParseID(rec.OperationID)
	if err != nil {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, err
	}
	record, err := m.repository.FindCaptureAuthority(ctx, opID)
	if err != nil {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, err
	}
	if err := record.Validate(); err != nil || record.State != CaptureAuthorityPrepared || record.StructuredCaptureDigest != capture.CaptureAuthorityID {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, fmt.Errorf("capture authority unavailable for materialization")
	}
	intent := record.Authority.Intent
	if intent.OperationID != rec.OperationID || intent.SessionID != rec.SessionID {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, fmt.Errorf("terminal receipt capture binding mismatch")
	}
	pre, err := capture.Source().StatIdentity()
	if err != nil {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, err
	}
	if pre.Validate() != nil || pre != capture.SourceIdentity {
		return ArtifactCaptureIntent{}, ArtifactSourceIdentity{}, ErrArtifactChangedDuringCapture
	}
	return intent, pre, nil
}

func artifactBlobRefMatchesMaterialization(ref core.ArtifactBlobRef, intent ArtifactCaptureIntent, blobID string, terminalCut core.TerminalCutV1) bool {
	return ref.Validate() == nil && ref.BlobID == blobID && ref.OperationID == intent.OperationID && ref.SessionID == intent.SessionID &&
		ref.RepositoryID == intent.RepositoryID && ref.WorkspaceID == intent.WorkspaceID && ref.DeclaredPath == intent.DeclaredPathToken &&
		ref.NormalizedWorkspacePath == intent.NormalizedWorkspacePath && ref.TerminalCut == terminalCut
}

func ArtifactBlobID(intent ArtifactCaptureIntent) (string, error) {
	if err := intent.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		Version                 string `json:"version"`
		OperationID             string `json:"operation_id"`
		SessionID               string `json:"session_id"`
		AdapterID               string `json:"adapter_id"`
		NormalizedWorkspacePath string `json:"normalized_workspace_path"`
	}{"artifact_blob_v1", intent.OperationID, intent.SessionID, intent.AdapterID, intent.NormalizedWorkspacePath})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "abl_" + hex.EncodeToString(sum[:]), nil
}

func TerminalCutForReceipt(rec receipt.Receipt) (core.TerminalCutV1, error) {
	digest, err := receipt.Digest(rec)
	if err != nil {
		return core.TerminalCutV1{}, err
	}
	cut := core.TerminalCutV1{SchemaVersion: core.TerminalCutSchemaVersion, ReceiptSchemaVersion: rec.SchemaVersion, ReceiptDigest: digest}
	return cut, cut.Validate()
}

func artifactBlobReservedBytes(size int64) (int64, error) {
	if size < 0 || size > MaxArtifactBlobBytes || size > math.MaxInt64-ArtifactBlobMetadataOverhead {
		return 0, fmt.Errorf("artifact blob size exceeds budget")
	}
	return size + ArtifactBlobMetadataOverhead, nil
}

func validArtifactBlobIDForApp(value string) bool {
	if len(value) != len("abl_")+64 || value[:4] != "abl_" {
		return false
	}
	_, err := hex.DecodeString(value[4:])
	return err == nil
}
