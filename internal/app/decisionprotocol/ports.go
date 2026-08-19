package decisionprotocol

import (
	"context"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
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
