package verification

import (
	"context"

	"github.com/maemreyo/shellbeam/internal/core/activity"
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

type WorkspaceInspector interface {
	Inspect(context.Context, string) (workspace.Workspace, error)
}

type WorkspaceSampler interface {
	Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample
}

type ActivitySelector interface {
	CompareWorkspace(context.Context, string, workspace.DeltaSample) (activity.Comparison, error)
}

type RelationResult struct {
	Domains     []core.AffectedDomain
	Relations   []core.AffectedRelation
	Diagnostics []string
}

type RelationProvider interface {
	Derive(context.Context, workspace.Workspace, string, []string) RelationResult
}

type EffectivePolicyStore interface {
	CurrentActivation(context.Context, workspace.RepositoryID) (core.PolicyActivation, bool, error)
	LoadPolicySnapshot(context.Context, workspace.RepositoryID, string) (core.PolicySnapshot, bool, error)
}

type AffectedDeriver interface {
	Derive(context.Context, AffectedRequest) (AffectedResult, error)
}

type ObligationDeriver interface {
	Derive(context.Context, ObligationRequest) (ObligationResult, error)
}

type ActiveWaiverReader interface {
	ActiveWaivers(context.Context, WaiverScope) ([]core.VerificationWaiver, error)
}

type StarterPolicyPreviewer interface {
	Preview(context.Context, string, string, *project.Manifest) (PolicyPreview, error)
}

type EvidenceCandidateSource interface {
	Candidates(context.Context, CandidateQuery) (CandidateResultSet, error)
}

type CandidateQuery struct {
	WorkspaceID       string
	ActivityID        string
	ProjectCommandIDs []string
	MaxRecords        int
}

type CandidateResultSet struct {
	Candidates  []core.EvidenceCandidate
	Coverage    core.Coverage
	Diagnostics []string
}
