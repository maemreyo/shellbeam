package decisionprotocol

import (
	"context"

	structuredapp "github.com/maemreyo/shellbeam/internal/app/structuredresult"
	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
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

type ReceiptSource interface {
	FindReceiptByOperation(context.Context, operation.ID) (receipt.Receipt, bool, error)
}

type StructuredSource interface {
	InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error)
}

type VerificationSource interface {
	AcquireVerificationObservationCut(context.Context, operation.ID) (VerificationObservationCut, error)
	QualifiedEvidenceForOperation(context.Context, operation.ID, VerificationObservationCut) (QualifiedEvidenceSet, error)
}

type AssessmentStore interface {
	RecordAssessment(context.Context, core.VerifierAssessment) (core.CanonicalRecordEnvelope, bool, error)
}

type QualifyVerifierContextRequest struct {
	EpisodeID            core.EpisodeID
	ActorRef             string
	DeclaredContextClass core.ContextClass
	DeclaredProviderID   string
}

type VerifierContextQualifier interface {
	QualifyVerifierContext(context.Context, QualifyVerifierContextRequest) (core.ContextQualificationResult, error)
}

type EpisodeDependencies struct {
	Mutations         EpisodeMutationStore
	Experiments       ExperimentMutationStore
	Ledger            CanonicalLedgerStore
	Workspaces        WorkspaceInspector
	Snapshots         SourceSnapshotter
	Receipts          ReceiptSource
	Structured        StructuredSource
	Verification      VerificationSource
	Assessments       AssessmentStore
	VerifierQualifier VerifierContextQualifier
}

type ExperimentMutationStore interface {
	DefineExperiment(context.Context, core.Experiment) (core.CanonicalRecordEnvelope, bool, error)
	FindExperiment(context.Context, core.ExperimentID) (core.Experiment, bool, error)
	BindPrediction(context.Context, core.PredictionBinding) (core.CanonicalRecordEnvelope, bool, error)
	SealExperimentCAS(context.Context, core.ExperimentSeal) (core.CanonicalRecordEnvelope, bool, error)
	MaterializeExperimentObservationCAS(context.Context, core.ExperimentObservationBinding) (core.ExperimentObservationBinding, bool, error)
	CloseExperimentCAS(context.Context, core.ExperimentClosure) (core.CanonicalRecordEnvelope, bool, error)
	AbortExperimentCAS(context.Context, core.ExperimentAbort) (core.CanonicalRecordEnvelope, bool, error)
}
