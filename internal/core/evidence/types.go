package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	environment "github.com/maemreyo/shellbeam/internal/core/environment"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/project"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

const (
	SchemaVersion            = 1
	ArtifactSchemaVersion    = 1
	ValiditySchemaVersion    = 1
	MaxInspectRecords        = 128
	MaxExpectedOutputs       = project.MaxExpectedOutputs
	MaxArtifactMetadataBytes = 32 << 10
	MaxArtifactDigestBytes   = int64(64 << 20)
	MaxTreeEntries           = 4096
	MaxCursorBytes           = 2048
	MaxInspectScanRecords    = 512
	MaxRevalidateRecords     = 4
)

type VerificationKind string
type SourceScope string
type Result string
type ArtifactStatus string
type ObservationQuality string
type SourceQuality string
type SourceMatch string
type Freshness string
type ArtifactMatch string
type PolicyMatch string

const (
	VerificationFormat   VerificationKind = "format"
	VerificationTest     VerificationKind = "test"
	VerificationBuild    VerificationKind = "build"
	VerificationGenerate VerificationKind = "generate"
	VerificationRelease  VerificationKind = "release"
	VerificationArtifact VerificationKind = "artifact"

	SourceScopeNone     SourceScope = "none"
	SourceScopeAffected SourceScope = "affected"
	SourceScopeFull     SourceScope = "full"

	ResultPass       Result = "pass"
	ResultFail       Result = "fail"
	ResultIncomplete Result = "incomplete"
	ResultAmbiguous  Result = "ambiguous"

	ArtifactCurrent        ArtifactStatus = "current"
	ArtifactMissing        ArtifactStatus = "missing"
	ArtifactKindMismatch   ArtifactStatus = "kind_mismatch"
	ArtifactDigestMismatch ArtifactStatus = "digest_mismatch"
	ArtifactUnavailable    ArtifactStatus = "unavailable"

	ObservationComplete    ObservationQuality = "complete"
	ObservationUnavailable ObservationQuality = "unavailable"

	SourceQualityExact   SourceQuality = "exact"
	SourceQualityFast    SourceQuality = "fast"
	SourceQualityUnknown SourceQuality = "unknown"

	SourceMatchExact    SourceMatch = "exact"
	SourceMatchFast     SourceMatch = "fast"
	SourceMatchMismatch SourceMatch = "mismatch"
	SourceMatchUnknown  SourceMatch = "unknown"

	FreshnessCurrent Freshness = "current"
	FreshnessStale   Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"

	ArtifactMatchCurrent     ArtifactMatch = "current"
	ArtifactMatchChanged     ArtifactMatch = "changed"
	ArtifactMatchMissing     ArtifactMatch = "missing"
	ArtifactMatchNotRequired ArtifactMatch = "not_required"
	ArtifactMatchUnknown     ArtifactMatch = "unknown"

	PolicyMatchCurrent PolicyMatch = "current"
	PolicyMatchChanged PolicyMatch = "changed"
	PolicyMatchUnknown PolicyMatch = "unknown"
)

type Contract struct {
	VerificationKind VerificationKind `json:"verification_kind"`
	SourceScope      SourceScope      `json:"source_scope,omitempty"`
	ExpectedOutputs  []project.Output `json:"expected_outputs,omitempty"`
}

type TerminalResult struct {
	Authoritative bool            `json:"authoritative"`
	Outcome       session.Outcome `json:"outcome"`
}

type ArtifactObservation struct {
	SchemaVersion int                `json:"schema_version,omitempty"`
	Path          string             `json:"path"`
	DeclaredKind  string             `json:"declared_kind,omitempty"`
	ObservedKind  string             `json:"observed_kind,omitempty"`
	Required      bool               `json:"required"`
	Exists        bool               `json:"exists"`
	Size          int64              `json:"size,omitempty"`
	MTime         time.Time          `json:"mtime,omitempty"`
	DigestMode    string             `json:"digest_mode,omitempty"`
	Digest        string             `json:"digest,omitempty"`
	LinkText      string             `json:"link_text,omitempty"`
	Status        ArtifactStatus     `json:"status"`
	Quality       ObservationQuality `json:"quality"`
	ObservedAt    time.Time          `json:"observed_at,omitempty"`
}

type SourceBinding struct {
	RepositoryID        string        `json:"repository_id,omitempty"`
	WorkspaceID         string        `json:"workspace_id,omitempty"`
	PreGeneration       string        `json:"pre_generation,omitempty"`
	PostGeneration      string        `json:"post_generation,omitempty"`
	ObservedChange      bool          `json:"observed_change,omitempty"`
	ObservationQuality  SourceQuality `json:"observation_quality"`
	SourceContentDigest string        `json:"source_content_digest,omitempty"`
	VCSStateDigest      string        `json:"vcs_state_digest,omitempty"`
}

type CurrentSource struct {
	WorkspaceID         string        `json:"workspace_id,omitempty"`
	Generation          string        `json:"generation,omitempty"`
	Quality             SourceQuality `json:"quality"`
	SourceContentDigest string        `json:"source_content_digest,omitempty"`
	VCSStateDigest      string        `json:"vcs_state_digest,omitempty"`
}

type Validity struct {
	SourceMatch   SourceMatch   `json:"source_match"`
	Freshness     Freshness     `json:"freshness"`
	ArtifactMatch ArtifactMatch `json:"artifact_match"`
	PolicyMatch   PolicyMatch   `json:"policy_match"`
}

type ValidityObservation struct {
	SchemaVersion int                   `json:"schema_version"`
	EvidenceID    string                `json:"evidence_id"`
	Validity      Validity              `json:"validity"`
	CurrentSource CurrentSource         `json:"current_source"`
	Artifacts     []ArtifactObservation `json:"artifacts,omitempty"`
	ObservedAt    time.Time             `json:"observed_at"`
}

type CommandAuthority struct {
	RequestFingerprint     string `json:"request_fingerprint,omitempty"`
	ExecutionFingerprint   string `json:"execution_fingerprint,omitempty"`
	ObservationFingerprint string `json:"observation_fingerprint,omitempty"`
	ProjectCommandID       string `json:"project_command_id,omitempty"`
	ProjectBindingDigest   string `json:"project_binding_digest,omitempty"`
	ManifestDigest         string `json:"manifest_digest,omitempty"`
}

type Record struct {
	SchemaVersion      int                   `json:"schema_version"`
	EvidenceID         string                `json:"evidence_id"`
	OperationID        string                `json:"operation_id"`
	SessionID          string                `json:"session_id"`
	ActivityID         string                `json:"activity_id,omitempty"`
	WorkspaceID        string                `json:"workspace_id,omitempty"`
	VerificationKind   VerificationKind      `json:"verification_kind"`
	SourceScope        SourceScope           `json:"source_scope,omitempty"`
	ContractDigest     string                `json:"contract_digest"`
	Command            CommandAuthority      `json:"command"`
	ReceiptDigest      string                `json:"receipt_digest"`
	Terminal           TerminalResult        `json:"terminal"`
	Result             Result                `json:"result"`
	Source             SourceBinding         `json:"source"`
	Artifacts          []ArtifactObservation `json:"artifacts,omitempty"`
	CompletedAt        time.Time             `json:"completed_at"`
	EnvironmentBinding *environment.Binding  `json:"environment_binding,omitempty"`
}

func (c Contract) Digest() (string, error) {
	normalized, err := c.Normalize()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func DeriveResult(terminal TerminalResult, artifacts []ArtifactObservation) Result {
	if !terminal.Authoritative {
		return ResultIncomplete
	}
	if terminal.Outcome == session.Ambiguous {
		return ResultAmbiguous
	}
	if terminal.Outcome != session.Success {
		switch terminal.Outcome {
		case session.Failure, session.Timeout, session.KilledOutcome:
			return ResultFail
		default:
			return ResultIncomplete
		}
	}
	result := ResultPass
	for _, artifact := range artifacts {
		if !artifact.Required {
			continue
		}
		switch artifact.Status {
		case ArtifactCurrent:
		case ArtifactMissing, ArtifactKindMismatch, ArtifactDigestMismatch:
			return ResultFail
		case ArtifactUnavailable:
			result = ResultIncomplete
		default:
			result = ResultIncomplete
		}
	}
	return result
}

func DeriveSourceValidity(record SourceBinding, current CurrentSource) Validity {
	validity := Validity{SourceMatch: SourceMatchUnknown, Freshness: FreshnessUnknown, ArtifactMatch: ArtifactMatchUnknown, PolicyMatch: PolicyMatchUnknown}
	if record.WorkspaceID == "" || current.WorkspaceID == "" || record.WorkspaceID != current.WorkspaceID {
		return validity
	}
	if record.ObservationQuality == SourceQualityExact && current.Quality == SourceQualityExact && validDigest(record.SourceContentDigest) && validDigest(record.VCSStateDigest) && validDigest(current.SourceContentDigest) && validDigest(current.VCSStateDigest) {
		if record.SourceContentDigest == current.SourceContentDigest && record.VCSStateDigest == current.VCSStateDigest {
			validity.SourceMatch, validity.Freshness = SourceMatchExact, FreshnessCurrent
		} else {
			validity.SourceMatch, validity.Freshness = SourceMatchMismatch, FreshnessStale
		}
		return validity
	}
	if validGeneration(record.PostGeneration) && validGeneration(current.Generation) {
		if record.PostGeneration == current.Generation {
			validity.SourceMatch, validity.Freshness = SourceMatchFast, FreshnessCurrent
		} else {
			validity.SourceMatch, validity.Freshness = SourceMatchMismatch, FreshnessStale
		}
	}
	return validity
}
