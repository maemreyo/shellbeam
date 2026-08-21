package main

import (
	"context"
	"fmt"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	decisionapp "github.com/maemreyo/shellbeam/internal/app/decisionprotocol"
	verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	coreevidence "github.com/maemreyo/shellbeam/internal/core/evidence"
	observationcore "github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type decisionWorkspaceList interface {
	List(context.Context) ([]workspacecore.Workspace, error)
}

type decisionWorkspaceRuntime interface {
	decisionWorkspaceList
	decisionapp.WorkspaceInspector
}

type decisionProtocolOperations interface {
	PutPolicySnapshot(context.Context, decisionapp.PutPolicySnapshotRequest) (decisioncore.PolicySnapshot, error)
	ActivatePolicy(context.Context, decisionapp.ActivatePolicyRequest) (decisioncore.PolicyActivation, error)
	CreateEpisode(context.Context, decisionapp.CreateEpisodeRequest) (decisioncore.DecisionProjection, error)
	Inspect(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProjection, error)
	Project(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProjection, error)
	Evaluate(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProtocolEvaluation, error)
	CloseUnresolved(context.Context, decisionapp.CloseUnresolvedRequest) (decisioncore.DecisionClosure, error)
	CreateCandidateInput(context.Context, decisionapp.CreateCandidateInputRequest) (decisioncore.DecisionProjection, error)
	ReviseCandidateInput(context.Context, decisionapp.ReviseCandidateInputRequest) (decisioncore.DecisionProjection, error)
	DefineExperimentInput(context.Context, decisionapp.DefineExperimentInputRequest) (decisioncore.DecisionProjection, error)
	BindPredictionInput(context.Context, decisionapp.BindPredictionInputRequest) (decisioncore.DecisionProjection, error)
	SealExperiment(context.Context, decisioncore.ExperimentID, string) (decisioncore.ExperimentSeal, decisioncore.DecisionProjection, error)
	CloseExperiment(context.Context, decisioncore.ExperimentID, string) (decisioncore.DecisionProjection, error)
	AbortExperiment(context.Context, decisioncore.ExperimentID, decisioncore.AbortPhase, string, string) (decisioncore.DecisionProjection, error)
	RecordAssessment(context.Context, decisionapp.RecordAssessmentRequest) (decisioncore.VerifierAssessment, error)
	ProposeSelection(context.Context, decisionapp.ProposeSelectionRequest) (decisioncore.SelectionProposal, error)
	CreateOverride(context.Context, decisionapp.CreateOverrideRequest) (decisioncore.DecisionOverride, error)
	CommitSelection(context.Context, decisionapp.CommitSelectionRequest) (decisioncore.SelectionCommit, error)
	MaterializeAuthority(context.Context, decisionapp.MaterializeAuthorityRequest) (decisionapp.MaterializeAuthorityResult, error)
}

type decisionExperimentStore struct {
	canonical  *storeadapter.DecisionProtocolStore
	repository *storeadapter.Repository
}

func (s decisionExperimentStore) DefineExperiment(ctx context.Context, value decisioncore.Experiment) (decisioncore.CanonicalRecordEnvelope, bool, error) {
	return s.canonical.DefineExperiment(ctx, value)
}
func (s decisionExperimentStore) FindExperiment(ctx context.Context, id decisioncore.ExperimentID) (decisioncore.Experiment, bool, error) {
	return s.canonical.FindExperiment(ctx, id)
}
func (s decisionExperimentStore) BindPrediction(ctx context.Context, value decisioncore.PredictionBinding) (decisioncore.CanonicalRecordEnvelope, bool, error) {
	return s.canonical.BindPrediction(ctx, value)
}
func (s decisionExperimentStore) SealExperimentCAS(ctx context.Context, value decisioncore.ExperimentSeal) (decisioncore.CanonicalRecordEnvelope, bool, error) {
	return s.canonical.SealExperimentCAS(ctx, value)
}
func (s decisionExperimentStore) MaterializeExperimentObservationCAS(ctx context.Context, value decisioncore.ExperimentObservationBinding) (decisioncore.ExperimentObservationBinding, bool, error) {
	return s.repository.MaterializeExperimentObservationCAS(ctx, value)
}
func (s decisionExperimentStore) CloseExperimentCAS(ctx context.Context, value decisioncore.ExperimentClosure) (decisioncore.CanonicalRecordEnvelope, bool, error) {
	return s.canonical.CloseExperimentCAS(ctx, value)
}
func (s decisionExperimentStore) AbortExperimentCAS(ctx context.Context, value decisioncore.ExperimentAbort) (decisioncore.CanonicalRecordEnvelope, bool, error) {
	return s.canonical.AbortExperimentCAS(ctx, value)
}

func composeDecisionProtocolRuntime(repository *storeadapter.Repository, workspaces decisionWorkspaceRuntime, snapshots decisionapp.SourceSnapshotter, structured decisionapp.StructuredSource, candidates verificationapp.EvidenceCandidateSource) (*decisionProtocolRuntime, error) {
	if repository == nil || workspaces == nil || snapshots == nil || structured == nil || candidates == nil {
		return nil, fmt.Errorf("decision protocol composition dependencies unavailable")
	}
	store := storeadapter.NewDecisionProtocolStore(repository)
	experiments := decisionExperimentStore{canonical: store, repository: repository}
	resolver := decisionapp.NewAuthorityResolverRegistry(nil, decisionapp.NewExplicitCallerAuthorityProvider(nil))
	service, err := decisionapp.NewRuntimeService(store, decisionActivationGenerationSource{workspaces: workspaces, snapshots: snapshots}, decisionapp.EpisodeDependencies{
		Mutations: store, Experiments: experiments, Ledger: store, Workspaces: workspaces, Snapshots: snapshots,
		Receipts: decisionReceiptSource{store: repository}, Structured: structured, Verification: decisionVerificationSource{store: repository, candidates: candidates},
		Assessments: store, Selections: store, Authorities: store, AuthorityResolver: resolver,
	})
	if err != nil {
		return nil, err
	}
	return &decisionProtocolRuntime{service: service, workspaces: workspaces}, nil
}

func bindDecisionProtocolRuntime(repository *storeadapter.Repository, actions *daemonActions, workspaces decisionWorkspaceRuntime, snapshots decisionapp.SourceSnapshotter, structured decisionapp.StructuredSource, candidates verificationapp.EvidenceCandidateSource) error {
	if actions == nil {
		return fmt.Errorf("decision protocol actions unavailable")
	}
	runtime, err := composeDecisionProtocolRuntime(repository, workspaces, snapshots, structured, candidates)
	if err != nil {
		return err
	}
	actions.decision = runtime
	return nil
}

type decisionActivationGenerationSource struct {
	workspaces decisionWorkspaceList
	snapshots  decisionapp.SourceSnapshotter
}

func (s decisionActivationGenerationSource) CurrentActivationGeneration(ctx context.Context, repositoryID string) (string, error) {
	if s.workspaces == nil || s.snapshots == nil || repositoryID == "" {
		return "", fmt.Errorf("decision activation generation source unavailable")
	}
	workspaces, err := s.workspaces.List(ctx)
	if err != nil {
		return "", err
	}
	var match *workspacecore.Workspace
	for i := range workspaces {
		workspace := workspaces[i]
		if string(workspace.RepositoryID) != repositoryID {
			continue
		}
		if err := workspace.Validate(); err != nil {
			return "", fmt.Errorf("invalid decision activation workspace: %w", err)
		}
		if match != nil {
			return "", fmt.Errorf("decision activation repository workspace is ambiguous")
		}
		copy := workspace
		match = &copy
	}
	if match == nil {
		return "", fmt.Errorf("decision activation repository workspace unavailable")
	}
	snapshot := s.snapshots.ObserveFresh(ctx, match.Root)
	if snapshot.DiagnosticCode == "observation_budget_exceeded" {
		snapshot = s.snapshots.ObserveFresh(ctx, match.Root)
	}
	if snapshot.Quality != workspacecore.QualityFresh || snapshot.Generation == "" {
		return "", fmt.Errorf("fresh decision activation generation unavailable")
	}
	if err := snapshot.Validate(); err != nil {
		return "", fmt.Errorf("invalid decision activation snapshot: %w", err)
	}
	if snapshot.RepositoryID != match.RepositoryID || snapshot.WorkspaceID != match.ID {
		return "", fmt.Errorf("decision activation snapshot workspace mismatch")
	}
	return snapshot.Generation, nil
}

type decisionProtocolRuntime struct {
	service        decisionProtocolOperations
	workspaces     decisionWorkspaceList
	trustedPeerUID func(context.Context) (uint32, bool)
}
type decisionObservationStore interface {
	FindOperation(context.Context, operation.ID) (operation.Reservation, bool, error)
	LoadReceipt(context.Context, operation.SessionID) (receipt.Receipt, error)
	ObservationHighWatermark(context.Context) (observationcore.ChangeSeq, error)
}

type decisionReceiptSource struct {
	store decisionObservationStore
}

func (s decisionReceiptSource) FindReceiptByOperation(ctx context.Context, id operation.ID) (receipt.Receipt, bool, error) {
	if s.store == nil {
		return receipt.Receipt{}, false, fmt.Errorf("decision receipt source unavailable")
	}
	reservation, found, err := s.store.FindOperation(ctx, id)
	if err != nil || !found {
		return receipt.Receipt{}, found, err
	}
	record, err := s.store.LoadReceipt(ctx, reservation.SessionID)
	if err != nil {
		return receipt.Receipt{}, false, err
	}
	if record.OperationID != string(id) || record.SessionID != string(reservation.SessionID) {
		return receipt.Receipt{}, false, fmt.Errorf("decision receipt authority mismatch")
	}
	return record, true, nil
}

type decisionVerificationSource struct {
	store      decisionObservationStore
	candidates verificationapp.EvidenceCandidateSource
}

func (s decisionVerificationSource) AcquireVerificationObservationCut(ctx context.Context, id operation.ID) (decisionapp.VerificationObservationCut, error) {
	if s.store == nil || s.candidates == nil {
		return decisionapp.VerificationObservationCut{}, fmt.Errorf("decision verification source unavailable")
	}
	if _, found, err := s.store.FindOperation(ctx, id); err != nil {
		return decisionapp.VerificationObservationCut{}, err
	} else if !found {
		return decisionapp.VerificationObservationCut{}, fmt.Errorf("decision verification operation unavailable")
	}
	high, err := s.store.ObservationHighWatermark(ctx)
	if err != nil {
		return decisionapp.VerificationObservationCut{}, err
	}
	return decisionapp.VerificationObservationCut{EvidenceIndexGeneration: uint64(high)}, nil
}

func (s decisionVerificationSource) QualifiedEvidenceForOperation(ctx context.Context, id operation.ID, cut decisionapp.VerificationObservationCut) (decisionapp.QualifiedEvidenceSet, error) {
	reservation, found, err := s.store.FindOperation(ctx, id)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("decision verification operation unavailable")
		}
		return decisionapp.QualifiedEvidenceSet{}, err
	}
	if err := s.requireEvidenceCut(ctx, cut); err != nil {
		return decisionapp.QualifiedEvidenceSet{}, err
	}
	result, err := s.candidates.Candidates(ctx, verificationapp.CandidateQuery{WorkspaceID: reservation.WorkspaceID, ActivityID: reservation.ActivityID, MaxRecords: coreevidence.MaxInspectRecords})
	if err != nil {
		return decisionapp.QualifiedEvidenceSet{}, err
	}
	if err := s.requireEvidenceCut(ctx, cut); err != nil {
		return decisionapp.QualifiedEvidenceSet{}, err
	}
	filtered := make([]verificationcore.EvidenceCandidate, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		if candidate.OperationID == string(id) {
			filtered = append(filtered, candidate)
		}
	}
	return decisionapp.QualifiedEvidenceSet{Cut: cut, Candidates: filtered, Coverage: result.Coverage}, nil
}

func (s decisionVerificationSource) requireEvidenceCut(ctx context.Context, cut decisionapp.VerificationObservationCut) error {
	high, err := s.store.ObservationHighWatermark(ctx)
	if err != nil {
		return err
	}
	if uint64(high) != cut.EvidenceIndexGeneration {
		return decisioncore.NewReasonError(decisioncore.ReasonObservationNotSettled, "verification evidence index advanced during observation materialization")
	}
	return nil
}

func (r *decisionProtocolRuntime) DecisionProtocol(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	if r == nil || r.service == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("decision protocol runtime unavailable")
	}
	switch action {
	case "decision.policy.snapshot", "decision.policy.activate", "decision.create":
		return r.dispatchDecisionContextAction(ctx, action, req)
	case "decision.inspect", "decision.evaluate", "decision.close_unresolved":
		return r.dispatchDecisionEpisodeAction(ctx, action, req)
	case "decision.candidate.create", "decision.candidate.revise":
		return r.dispatchDecisionCandidateAction(ctx, action, req)
	case "decision.experiment.define", "decision.prediction.bind", "decision.experiment.seal", "decision.experiment.close", "decision.experiment.abort":
		return r.dispatchDecisionExperimentAction(ctx, action, req)
	case "decision.assessment.record", "decision.selection.propose", "decision.override.create", "decision.selection.commit", "decision.authority.materialize":
		return r.dispatchDecisionTerminalAction(ctx, action, req)
	default:
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("unknown decision protocol action %q", action)
	}
}

func (r *decisionProtocolRuntime) dispatchDecisionContextAction(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	workspace, err := resolveDecisionWorkspace(ctx, r.workspaces)
	if err != nil {
		return ipcadapter.DecisionResponseV1{}, err
	}
	switch action {
	case "decision.policy.snapshot":
		value, err := r.service.PutPolicySnapshot(ctx, decisionapp.PutPolicySnapshotRequest{RepositoryID: string(workspace.RepositoryID), Content: req.Policy.Content})
		return decisionResponsePolicy(value), err
	case "decision.policy.activate":
		value, err := r.service.ActivatePolicy(ctx, decisionapp.ActivatePolicyRequest{RepositoryID: string(workspace.RepositoryID), ActivationID: req.ActivationID, PolicyDigest: req.PolicyDigest, ProposalGeneration: req.ProposalGeneration, ExpectedPreviousPolicyDigest: req.ExpectedPreviousPolicyDigest, ActorRef: req.ActorRef})
		return decisionResponseActivation(value), err
	case "decision.create":
		value, err := r.service.CreateEpisode(ctx, decisionapp.CreateEpisodeRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), Kind: req.EpisodeKind, RepositoryID: string(workspace.RepositoryID), WorkspaceID: string(workspace.ID), PredecessorEpisodeID: decisioncore.EpisodeID(req.PredecessorEpisodeID), ExpectedPolicyDigest: req.ExpectedPolicyDigest, ExpectedActivationRef: req.ExpectedActivationRef, ActorRef: req.ActorRef})
		return decisionResponseProjection(value), err
	}
	return ipcadapter.DecisionResponseV1{}, fmt.Errorf("unsupported contextual decision action")
}

func (r *decisionProtocolRuntime) dispatchDecisionEpisodeAction(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	episodeID, candidateID := decisioncore.EpisodeID(req.EpisodeID), decisioncore.CandidateID(req.CandidateID)
	switch action {
	case "decision.inspect":
		value, err := r.service.Project(ctx, episodeID, candidateID)
		return decisionResponseProjection(value), err
	case "decision.evaluate":
		value, err := r.service.Evaluate(ctx, episodeID, candidateID)
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Evaluation: &value}, nil
	case "decision.close_unresolved":
		value, err := r.service.CloseUnresolved(ctx, decisionapp.CloseUnresolvedRequest{EpisodeID: episodeID, ActorRef: req.ActorRef, ProjectionDigest: req.ExpectedProjectionDigest, Reason: req.Reason, UnresolvedDimensions: append([]string(nil), (*req.UnresolvedDimensions)...)})
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Closure: &value}, nil
	}
	return ipcadapter.DecisionResponseV1{}, fmt.Errorf("unsupported episode decision action")
}

func (r *decisionProtocolRuntime) dispatchDecisionCandidateAction(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	input := req.Candidate
	if input == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("candidate input missing")
	}
	base := decisionapp.CreateCandidateInputRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), CandidateID: decisioncore.CandidateID(input.CandidateID), SemanticClaim: input.SemanticClaim, CandidateKind: input.CandidateKind, ActorRef: req.ActorRef}
	if action == "decision.candidate.create" {
		value, err := r.service.CreateCandidateInput(ctx, base)
		return decisionResponseProjection(value), err
	}
	value, err := r.service.ReviseCandidateInput(ctx, decisionapp.ReviseCandidateInputRequest{EpisodeID: base.EpisodeID, ParentCandidateID: decisioncore.CandidateID(req.ParentCandidateID), CandidateID: base.CandidateID, SemanticClaim: base.SemanticClaim, CandidateKind: base.CandidateKind, ActorRef: base.ActorRef})
	return decisionResponseProjection(value), err
}

func (r *decisionProtocolRuntime) dispatchDecisionExperimentAction(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	switch action {
	case "decision.experiment.define":
		value, err := r.service.DefineExperimentInput(ctx, decisionapp.DefineExperimentInputRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), ExperimentID: decisioncore.ExperimentID(req.ExperimentID), ActorRef: req.ActorRef})
		return decisionResponseProjection(value), err
	case "decision.prediction.bind":
		return r.dispatchDecisionPrediction(ctx, req)
	case "decision.experiment.seal":
		seal, projection, err := r.service.SealExperiment(ctx, decisioncore.ExperimentID(req.ExperimentID), req.ActorRef)
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Seal: &seal, Projection: &projection}, nil
	case "decision.experiment.close":
		value, err := r.service.CloseExperiment(ctx, decisioncore.ExperimentID(req.ExperimentID), req.ActorRef)
		return decisionResponseProjection(value), err
	case "decision.experiment.abort":
		value, err := r.service.AbortExperiment(ctx, decisioncore.ExperimentID(req.ExperimentID), req.AbortPhase, req.Reason, req.ActorRef)
		return decisionResponseProjection(value), err
	}
	return ipcadapter.DecisionResponseV1{}, fmt.Errorf("unsupported experiment decision action")
}

func (r *decisionProtocolRuntime) dispatchDecisionPrediction(ctx context.Context, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	input := req.Prediction
	if input == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("prediction input missing")
	}
	value, err := r.service.BindPredictionInput(ctx, decisionapp.BindPredictionInputRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), ExperimentID: decisioncore.ExperimentID(req.ExperimentID), PredictionID: decisioncore.PredictionID(input.PredictionID), CandidateID: decisioncore.CandidateID(input.CandidateID), Role: input.Role, Predicate: input.Predicate})
	return decisionResponseProjection(value), err
}

func (r *decisionProtocolRuntime) dispatchDecisionTerminalAction(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	switch action {
	case "decision.assessment.record":
		return r.dispatchDecisionAssessment(ctx, req)
	case "decision.selection.propose":
		value, err := r.service.ProposeSelection(ctx, decisionapp.ProposeSelectionRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), CandidateID: decisioncore.CandidateID(req.CandidateID), ActorRef: req.ActorRef, Rationale: req.Reason})
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Proposal: &value}, nil
	case "decision.override.create":
		value, err := r.service.CreateOverride(ctx, decisionapp.CreateOverrideRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), CandidateID: decisioncore.CandidateID(req.CandidateID), ExpectedPolicyDigest: req.ExpectedPolicyDigest, ExpectedProjectionDigest: req.ExpectedProjectionDigest, BlockingRequirementDigest: req.BlockingRequirementDigest, AuthorityAttestationRef: req.AuthorityAttestationRef, Reason: req.Reason})
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Override: &value}, nil
	case "decision.selection.commit":
		value, err := r.service.CommitSelection(ctx, decisionapp.CommitSelectionRequest{EpisodeID: decisioncore.EpisodeID(req.EpisodeID), CandidateID: decisioncore.CandidateID(req.CandidateID), ActorRef: req.ActorRef, ExpectedPolicyDigest: req.ExpectedPolicyDigest, ExpectedProjectionDigest: req.ExpectedProjectionDigest, OverrideRef: req.OverrideRef, IdempotencyKey: req.IdempotencyKey})
		if err != nil {
			return ipcadapter.DecisionResponseV1{}, err
		}
		return ipcadapter.DecisionResponseV1{Selection: &value}, nil
	case "decision.authority.materialize":
		return r.dispatchDecisionAuthority(ctx, req)
	}
	return ipcadapter.DecisionResponseV1{}, fmt.Errorf("unsupported terminal decision action")
}

func (r *decisionProtocolRuntime) dispatchDecisionAssessment(ctx context.Context, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	input := req.Assessment
	if input == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("assessment input missing")
	}
	preferred := make([]decisioncore.CandidateID, len(input.PreferredCandidates))
	for i, id := range input.PreferredCandidates {
		preferred[i] = decisioncore.CandidateID(id)
	}
	rejected := make([]decisioncore.CandidateID, len(input.SemanticRejections))
	for i, id := range input.SemanticRejections {
		rejected[i] = decisioncore.CandidateID(id)
	}
	value, err := r.service.RecordAssessment(ctx, decisionapp.RecordAssessmentRequest{AssessmentID: input.AssessmentID, EpisodeID: decisioncore.EpisodeID(req.EpisodeID), ActorRef: req.ActorRef, DeclaredContextClass: input.DeclaredContextClass, DeclaredProviderIdentity: input.DeclaredProviderIdentity, PreferredCandidates: preferred, SemanticRejections: rejected, Rationale: input.Rationale})
	if err != nil {
		return ipcadapter.DecisionResponseV1{}, err
	}
	return ipcadapter.DecisionResponseV1{Assessment: &value}, nil
}

func (r *decisionProtocolRuntime) dispatchDecisionAuthority(ctx context.Context, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	input := req.AuthorityRequest
	if input == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("authority input missing")
	}
	actorRef, err := r.trustedAuthorityActor(ctx)
	if err != nil {
		return ipcadapter.DecisionResponseV1{}, err
	}
	value, err := r.service.MaterializeAuthority(ctx, decisionapp.MaterializeAuthorityRequest{ActorRef: actorRef, RequiredAuthorityClass: input.RequiredAuthorityClass, RequiredScope: input.RequiredScope})
	if err != nil {
		return ipcadapter.DecisionResponseV1{}, err
	}
	return ipcadapter.DecisionResponseV1{Attestation: value.Attestation, AuthorityStatus: value.Status}, nil
}

func (r *decisionProtocolRuntime) trustedAuthorityActor(ctx context.Context) (string, error) {
	reader := ipcadapter.TrustedPeerUID
	if r != nil && r.trustedPeerUID != nil {
		reader = r.trustedPeerUID
	}
	uid, ok := reader(ctx)
	if !ok {
		return "", fmt.Errorf("trusted decision caller unavailable")
	}
	return trustedDecisionActorRef(uid), nil
}

func decisionResponsePolicy(value decisioncore.PolicySnapshot) ipcadapter.DecisionResponseV1 {
	return ipcadapter.DecisionResponseV1{Policy: &value}
}
func decisionResponseActivation(value decisioncore.PolicyActivation) ipcadapter.DecisionResponseV1 {
	return ipcadapter.DecisionResponseV1{Activation: &value}
}
func decisionResponseProjection(value decisioncore.DecisionProjection) ipcadapter.DecisionResponseV1 {
	return ipcadapter.DecisionResponseV1{Projection: &value}
}

func resolveDecisionWorkspace(ctx context.Context, workspaces decisionWorkspaceList) (workspacecore.Workspace, error) {
	if workspaces == nil {
		return workspacecore.Workspace{}, fmt.Errorf("decision workspace context unavailable")
	}
	values, err := workspaces.List(ctx)
	if err != nil {
		return workspacecore.Workspace{}, err
	}
	if len(values) != 1 {
		return workspacecore.Workspace{}, fmt.Errorf("decision workspace context requires exactly one registered workspace")
	}
	if err := values[0].Validate(); err != nil {
		return workspacecore.Workspace{}, fmt.Errorf("invalid decision workspace context: %w", err)
	}
	return values[0], nil
}

func decisionProtocolSupport() capability.DecisionProtocolSupport {
	return capability.DecisionProtocolSupport{SchemaVersion: 1, ProtocolVersion: 1, PredicateKinds: []string{"OPERATION_OUTCOME", "STRUCTURED_TEST_STATUS", "STRUCTURED_DIAGNOSTIC_PRESENCE", "VERIFICATION_RESULT"}, AuthorityProviders: []string{decisionapp.ExplicitCallerAuthorityProviderID + ".v1"}, OneExecutionPerExperiment: true}
}

func trustedDecisionActorRef(uid uint32) string {
	return decisionapp.ExplicitCallerActorRef(uid)
}

func (a *daemonActions) DecisionProtocol(ctx context.Context, action string, req ipcadapter.DecisionRequestV1) (ipcadapter.DecisionResponseV1, error) {
	if a == nil || a.decision == nil {
		return ipcadapter.DecisionResponseV1{}, fmt.Errorf("decision protocol runtime unavailable")
	}
	return a.decision.DecisionProtocol(ctx, action, req)
}

func (a *daemonActions) InspectServer(ctx context.Context) (daemonapp.ServerInfo, error) {
	if a == nil || a.Actions == nil {
		return daemonapp.ServerInfo{}, fmt.Errorf("daemon actions unavailable")
	}
	info, err := a.Actions.InspectServer(ctx)
	if err != nil {
		return daemonapp.ServerInfo{}, err
	}
	if a.decision != nil {
		info.Capabilities = info.Capabilities.WithDecisionProtocol(decisionProtocolSupport())
	}
	return info, nil
}
