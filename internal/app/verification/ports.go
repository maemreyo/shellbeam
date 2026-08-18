package verification

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type PolicyLoadState string

const (
	PolicyLoadAbsent      PolicyLoadState = "absent"
	PolicyLoadValid       PolicyLoadState = "valid"
	PolicyLoadInvalid     PolicyLoadState = "invalid"
	PolicyLoadUnsupported PolicyLoadState = "unsupported"
)

type PolicyLoadResult struct {
	State     PolicyLoadState      `json:"state"`
	Proposal  *core.PolicyProposal `json:"proposal,omitempty"`
	RawDigest string               `json:"raw_digest,omitempty"`
	Code      string               `json:"code,omitempty"`
}

type PolicyLoader interface {
	Load(context.Context, workspace.Workspace) PolicyLoadResult
}
