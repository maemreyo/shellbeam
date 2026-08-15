package evidence

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var evidenceAuthorityIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (c Contract) Validate() error {
	_, err := c.Normalize()
	return err
}

func (c Contract) Normalize() (Contract, error) {
	if !validVerificationKind(c.VerificationKind) {
		return Contract{}, fmt.Errorf("invalid verification kind")
	}
	if c.SourceScope != "" && c.SourceScope != SourceScopeNone && c.SourceScope != SourceScopeAffected && c.SourceScope != SourceScopeFull {
		return Contract{}, fmt.Errorf("invalid source scope")
	}
	if len(c.ExpectedOutputs) > MaxExpectedOutputs {
		return Contract{}, fmt.Errorf("too many expected outputs")
	}
	if c.VerificationKind == VerificationArtifact && len(c.ExpectedOutputs) == 0 {
		return Contract{}, fmt.Errorf("artifact verification requires expected outputs")
	}
	outputs, err := project.ValidateExpectedOutputs(c.ExpectedOutputs)
	if err != nil {
		return Contract{}, err
	}
	out := c
	out.ExpectedOutputs = outputs
	return out, nil
}

func (r Record) Validate() error {
	if r.SchemaVersion != SchemaVersion || !validPrefixedDigest(r.EvidenceID, "ev_") || !validVerificationKind(r.VerificationKind) || !validDigest(r.ContractDigest) || !validDigest(r.ReceiptDigest) || r.CompletedAt.IsZero() {
		return fmt.Errorf("invalid evidence record")
	}
	if !evidenceAuthorityIDPattern.MatchString(r.OperationID) || !evidenceAuthorityIDPattern.MatchString(r.SessionID) {
		return fmt.Errorf("invalid evidence operation/session id")
	}
	if r.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
			return fmt.Errorf("invalid evidence workspace id: %w", err)
		}
	}
	switch r.Result {
	case ResultPass, ResultFail, ResultIncomplete, ResultAmbiguous:
	default:
		return fmt.Errorf("invalid evidence result")
	}
	if err := validateSource(r.Source); err != nil {
		return err
	}
	for _, artifact := range r.Artifacts {
		if artifact.Path == "" {
			return fmt.Errorf("invalid artifact observation")
		}
		switch artifact.Status {
		case ArtifactCurrent, ArtifactMissing, ArtifactKindMismatch, ArtifactDigestMismatch, ArtifactUnavailable:
		default:
			return fmt.Errorf("invalid artifact status")
		}
		switch artifact.Quality {
		case ObservationComplete, ObservationUnavailable:
		default:
			return fmt.Errorf("invalid artifact quality")
		}
	}
	return nil
}

func validVerificationKind(kind VerificationKind) bool {
	switch kind {
	case VerificationFormat, VerificationTest, VerificationBuild, VerificationGenerate, VerificationRelease, VerificationArtifact:
		return true
	default:
		return false
	}
}

func validateSource(source SourceBinding) error {
	switch source.ObservationQuality {
	case SourceQualityUnknown:
		if source.SourceContentDigest != "" || source.VCSStateDigest != "" {
			return fmt.Errorf("unknown source claims exact digest")
		}
	case SourceQualityFast:
		if source.SourceContentDigest != "" || source.VCSStateDigest != "" {
			return fmt.Errorf("fast source claims exact digest")
		}
	case SourceQualityExact:
		if !validDigest(source.SourceContentDigest) || !validDigest(source.VCSStateDigest) {
			return fmt.Errorf("exact source digest missing")
		}
	default:
		return fmt.Errorf("invalid source quality")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validPrefixedDigest(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && validDigest(strings.TrimPrefix(value, prefix))
}

func validGeneration(value string) bool { return validPrefixedDigest(value, "gen_") }

func (v ValidityObservation) Validate() error {
	if v.SchemaVersion != ValiditySchemaVersion || !validPrefixedDigest(v.EvidenceID, "ev_") || v.ObservedAt.IsZero() {
		return fmt.Errorf("invalid evidence validity observation")
	}
	if !validSourceMatch(v.Validity.SourceMatch) || !validFreshness(v.Validity.Freshness) || !validArtifactMatch(v.Validity.ArtifactMatch) || !validPolicyMatch(v.Validity.PolicyMatch) {
		return fmt.Errorf("invalid evidence validity dimensions")
	}
	if err := validateCurrentSource(v.CurrentSource); err != nil {
		return err
	}
	if len(v.Artifacts) > MaxExpectedOutputs {
		return fmt.Errorf("too many validity artifacts")
	}
	for _, artifact := range v.Artifacts {
		if artifact.Path == "" || artifact.ObservedAt.IsZero() {
			return fmt.Errorf("invalid validity artifact")
		}
		switch artifact.Status {
		case ArtifactCurrent, ArtifactMissing, ArtifactKindMismatch, ArtifactDigestMismatch, ArtifactUnavailable:
		default:
			return fmt.Errorf("invalid validity artifact status")
		}
		switch artifact.Quality {
		case ObservationComplete, ObservationUnavailable:
		default:
			return fmt.Errorf("invalid validity artifact quality")
		}
	}
	return nil
}

func validateCurrentSource(source CurrentSource) error {
	switch source.Quality {
	case SourceQualityUnknown:
		if source.SourceContentDigest != "" || source.VCSStateDigest != "" {
			return fmt.Errorf("unknown current source claims exact digest")
		}
	case SourceQualityFast:
		if source.WorkspaceID == "" || !validGeneration(source.Generation) || source.SourceContentDigest != "" || source.VCSStateDigest != "" {
			return fmt.Errorf("invalid fast current source")
		}
	case SourceQualityExact:
		if source.WorkspaceID == "" || !validDigest(source.SourceContentDigest) || !validDigest(source.VCSStateDigest) {
			return fmt.Errorf("invalid exact current source")
		}
	default:
		return fmt.Errorf("invalid current source quality")
	}
	return nil
}

func validSourceMatch(value SourceMatch) bool {
	return value == SourceMatchExact || value == SourceMatchFast || value == SourceMatchMismatch || value == SourceMatchUnknown
}
func validFreshness(value Freshness) bool {
	return value == FreshnessCurrent || value == FreshnessStale || value == FreshnessUnknown
}
func validArtifactMatch(value ArtifactMatch) bool {
	return value == ArtifactMatchCurrent || value == ArtifactMatchChanged || value == ArtifactMatchMissing || value == ArtifactMatchNotRequired || value == ArtifactMatchUnknown
}
func validPolicyMatch(value PolicyMatch) bool {
	return value == PolicyMatchCurrent || value == PolicyMatchChanged || value == PolicyMatchUnknown
}
