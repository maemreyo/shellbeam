package decisionprotocol

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type CanonicalLedgerStore interface {
	AppendRecord(context.Context, core.RecordKind, any) (core.CanonicalRecordEnvelope, error)
	LoadRecord(context.Context, core.RecordSeq) (core.CanonicalRecordEnvelope, bool, error)
	ListEpisodeRecords(context.Context, core.EpisodeID, core.RecordSeq) ([]core.CanonicalRecordEnvelope, error)
	CurrentHighWater(context.Context) (core.RecordSeq, error)
}

type PolicyStore interface {
	PutPolicySnapshot(context.Context, core.PolicySnapshot) (bool, error)
	LoadPolicySnapshot(context.Context, string, string) (core.PolicySnapshot, bool, error)
	ActivatePolicyCAS(context.Context, core.PolicyActivationCommit) (core.PolicyActivationWriteResult, error)
	CurrentEffectivePolicy(context.Context, string, core.EpisodeKind) (core.PolicySnapshot, core.PolicyActivation, bool, error)
}

type ActivationGenerationSource interface {
	CurrentActivationGeneration(context.Context, string) (string, error)
}

type EpisodeMutationStore interface {
	CreateEpisode(context.Context, core.Episode) (core.CanonicalRecordEnvelope, bool, error)
	CreateCandidate(context.Context, core.Candidate) (core.CanonicalRecordEnvelope, bool, error)
	ReviseCandidateCAS(context.Context, core.CandidateID, core.Candidate) (core.CanonicalRecordEnvelope, error)
	FindEpisode(context.Context, core.EpisodeID) (core.Episode, bool, error)
	FindCandidate(context.Context, core.CandidateID) (core.Candidate, bool, error)
}

type WorkspaceInspector interface {
	Inspect(context.Context, string) (workspace.Workspace, error)
}

type SourceSnapshotter interface {
	ObserveFresh(context.Context, string) workspace.FastSnapshot
}

type EpisodeDependencies struct {
	Mutations  EpisodeMutationStore
	Ledger     CanonicalLedgerStore
	Workspaces WorkspaceInspector
	Snapshots  SourceSnapshotter
}
