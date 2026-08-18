package verification

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

type CandidateFreshness string

const (
	CandidateCurrent CandidateFreshness = "current"
	CandidateStale   CandidateFreshness = "stale"
	CandidateUnknown CandidateFreshness = "unknown"
)

type CandidateResult string

const (
	CandidatePass       CandidateResult = "pass"
	CandidateFail       CandidateResult = "fail"
	CandidateIncomplete CandidateResult = "incomplete"
	CandidateAmbiguous  CandidateResult = "ambiguous"
)

var candidateProjectCommandIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type EvidenceCandidate struct {
	EvidenceID                    string                              `json:"evidence_id"`
	VerificationKind              evidence.VerificationKind           `json:"verification_kind"`
	ProviderClass                 ProviderClass                       `json:"provider_class,omitempty"`
	ProviderClassKnown            bool                                `json:"provider_class_known"`
	ProjectCommandID              string                              `json:"project_command_id,omitempty"`
	OperationID                   string                              `json:"operation_id"`
	SessionID                     string                              `json:"session_id"`
	ActivityID                    string                              `json:"activity_id,omitempty"`
	WorkspaceID                   string                              `json:"workspace_id"`
	SourceGeneration              string                              `json:"source_generation,omitempty"`
	SourceContentDigest           string                              `json:"source_content_digest,omitempty"`
	ProjectBindingDigest          string                              `json:"project_binding_digest,omitempty"`
	ManifestDigest                string                              `json:"manifest_digest,omitempty"`
	ContractDigest                string                              `json:"contract_digest"`
	EnvironmentFingerprint        string                              `json:"environment_fingerprint,omitempty"`
	EnvironmentFingerprintVersion int                                 `json:"environment_fingerprint_version,omitempty"`
	ToolchainFingerprint          string                              `json:"toolchain_fingerprint,omitempty"`
	ToolchainFingerprintVersion   int                                 `json:"toolchain_fingerprint_version,omitempty"`
	Authority                     DerivationAuthority                 `json:"authority,omitempty"`
	AuthorityKnown                bool                                `json:"authority_known"`
	Freshness                     CandidateFreshness                  `json:"freshness"`
	Result                        CandidateResult                     `json:"result"`
	Attempt                       *evidence.VerificationAttemptIntent `json:"verification_attempt,omitempty"`
	SemanticContractDigest        string                              `json:"semantic_contract_digest"`
	CompletedAt                   time.Time                           `json:"completed_at"`
}

func (f CandidateFreshness) Validate() error {
	switch f {
	case CandidateCurrent, CandidateStale, CandidateUnknown:
		return nil
	default:
		return fmt.Errorf("invalid candidate freshness %q", f)
	}
}

func (r CandidateResult) Validate() error {
	switch r {
	case CandidatePass, CandidateFail, CandidateIncomplete, CandidateAmbiguous:
		return nil
	default:
		return fmt.Errorf("invalid candidate result %q", r)
	}
}

func (c EvidenceCandidate) Validate() error {
	if !isDerivedID(c.EvidenceID, "ev_") || c.OperationID == "" || c.SessionID == "" || c.WorkspaceID == "" || c.CompletedAt.IsZero() {
		return fmt.Errorf("invalid candidate identity")
	}
	if !validEvidenceVerificationKind(c.VerificationKind) || !candidateDigest(c.ContractDigest) || !candidateDigest(c.SemanticContractDigest) {
		return fmt.Errorf("invalid candidate evidence contract")
	}
	if c.Freshness.Validate() != nil || c.Result.Validate() != nil {
		return fmt.Errorf("invalid candidate status")
	}
	if c.SourceGeneration != "" && !isDerivedID(c.SourceGeneration, "gen_") {
		return fmt.Errorf("invalid candidate source generation")
	}
	for _, digest := range []string{c.SourceContentDigest, c.ProjectBindingDigest, c.ManifestDigest, c.EnvironmentFingerprint, c.ToolchainFingerprint} {
		if digest != "" && !candidateDigest(digest) {
			return fmt.Errorf("invalid candidate digest")
		}
	}
	if c.Attempt != nil && c.Attempt.Validate() != nil {
		return fmt.Errorf("invalid candidate attempt")
	}
	if err := c.validateProviderAuthority(); err != nil {
		return err
	}
	if (c.EnvironmentFingerprint == "") != (c.EnvironmentFingerprintVersion == 0) || (c.ToolchainFingerprint == "") != (c.ToolchainFingerprintVersion == 0) {
		return fmt.Errorf("candidate fingerprint/version mismatch")
	}
	return nil
}

func (c EvidenceCandidate) validateProviderAuthority() error {
	if !c.ProviderClassKnown {
		if c.ProviderClass != "" || c.AuthorityKnown || c.Authority != "" {
			return fmt.Errorf("unknown candidate provider claims authority")
		}
		return nil
	}
	if c.ProviderClass != ProviderProjectCommand || !c.AuthorityKnown || c.Authority != AuthorityMechanical || !candidateProjectCommandIDPattern.MatchString(c.ProjectCommandID) || !candidateDigest(c.ProjectBindingDigest) {
		return fmt.Errorf("invalid typed project-command candidate authority")
	}
	return nil
}

func validEvidenceVerificationKind(kind evidence.VerificationKind) bool {
	switch kind {
	case evidence.VerificationFormat, evidence.VerificationTest, evidence.VerificationBuild, evidence.VerificationGenerate, evidence.VerificationRelease, evidence.VerificationArtifact:
		return true
	default:
		return false
	}
}

func candidateDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
