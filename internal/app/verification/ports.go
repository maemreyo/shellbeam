package verification

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/project"
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

type WorkspaceLookup interface {
	ListWorkspaces(context.Context) ([]workspace.Workspace, error)
}

type PolicyLoader interface {
	Load(context.Context, workspace.Workspace) PolicyLoadResult
}

type PolicyAuthorityStore interface {
	PutPolicySnapshot(context.Context, core.PolicySnapshot) (bool, error)
	FindActivation(context.Context, workspace.RepositoryID, string) (core.PolicyActivation, bool, error)
	ActivatePolicyCAS(context.Context, core.PolicyActivationCommit) (core.ActivationWriteResult, error)
	CurrentActivation(context.Context, workspace.RepositoryID) (core.PolicyActivation, bool, error)
	LoadPolicySnapshot(context.Context, workspace.RepositoryID, string) (core.PolicySnapshot, bool, error)
}

type WaiverAuthorityStore interface {
	FindWaiver(context.Context, workspace.RepositoryID, string) (core.VerificationWaiver, bool, error)
	PutWaiver(context.Context, core.VerificationWaiverIntent) (core.WaiverWriteResult, error)
	FindWaiverRevocation(context.Context, workspace.RepositoryID, string) (core.WaiverRevocation, bool, error)
	PutWaiverRevocation(context.Context, core.WaiverRevocationIntent) (core.RevocationWriteResult, error)
	ListWaivers(context.Context, workspace.RepositoryID) ([]core.VerificationWaiver, []core.WaiverRevocation, error)
}

type SourceSnapshotter interface {
	ObserveFresh(context.Context, string) workspace.FastSnapshot
}

type ProjectInspector interface {
	Inspect(context.Context, string) (project.Inspection, error)
}

type ProjectCommandResolver interface {
	Resolve(context.Context, string, string, map[string]string) (project.CommandBinding, error)
}
