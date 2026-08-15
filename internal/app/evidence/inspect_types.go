package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var projectCommandFilterPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type InspectFilter struct {
	EvidenceID          string                `json:"evidence_id,omitempty"`
	OperationID         string                `json:"operation_id,omitempty"`
	WorkspaceID         string                `json:"workspace_id,omitempty"`
	ProjectCommandID    string                `json:"project_command_id,omitempty"`
	ActivityID          string                `json:"activity_id,omitempty"`
	VerificationKind    core.VerificationKind `json:"verification_kind,omitempty"`
	Result              core.Result           `json:"result,omitempty"`
	RevalidateArtifacts bool                  `json:"revalidate_artifacts,omitempty"`
}

func (f InspectFilter) Validate() error {
	if f.EvidenceID != "" && (!regexp.MustCompile(`^ev_[0-9a-f]{64}$`).MatchString(f.EvidenceID)) {
		return fmt.Errorf("invalid evidence id")
	}
	if f.OperationID != "" {
		if _, err := operation.ParseID(f.OperationID); err != nil {
			return err
		}
	}
	if f.WorkspaceID != "" {
		if _, err := workspace.ParseWorkspaceID(f.WorkspaceID); err != nil {
			return err
		}
	}
	if f.ActivityID != "" {
		if _, err := activity.ParseID(f.ActivityID); err != nil {
			return err
		}
	}
	if f.ProjectCommandID != "" && !projectCommandFilterPattern.MatchString(f.ProjectCommandID) {
		return fmt.Errorf("invalid project command id")
	}
	if f.VerificationKind != "" {
		switch f.VerificationKind {
		case core.VerificationFormat, core.VerificationTest, core.VerificationBuild, core.VerificationGenerate, core.VerificationRelease, core.VerificationArtifact:
		default:
			return fmt.Errorf("invalid verification kind")
		}
	}
	if f.Result != "" {
		switch f.Result {
		case core.ResultPass, core.ResultFail, core.ResultIncomplete, core.ResultAmbiguous:
		default:
			return fmt.Errorf("invalid evidence result")
		}
	}
	return nil
}

func (f InspectFilter) fingerprint() (string, error) {
	if err := f.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
