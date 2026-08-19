package decisionprotocol

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

type RecordAssessmentRequest struct {
	AssessmentID             string
	EpisodeID                core.EpisodeID
	ActorRef                 string
	DeclaredContextClass     core.ContextClass
	DeclaredProviderIdentity string
	PreferredCandidates      []core.CandidateID
	SemanticRejections       []core.CandidateID
	Rationale                string
}

func (s *Service) RecordAssessment(ctx context.Context, req RecordAssessmentRequest) (core.VerifierAssessment, error) {
	if s == nil || s.mutations == nil || s.assessments == nil {
		return core.VerifierAssessment{}, fmt.Errorf("decision assessment dependencies unavailable")
	}
	if _, found, err := s.mutations.FindEpisode(ctx, req.EpisodeID); err != nil {
		return core.VerifierAssessment{}, err
	} else if !found {
		return core.VerifierAssessment{}, fmt.Errorf("assessment episode unavailable")
	}
	if err := s.requireAssessmentCandidates(ctx, req); err != nil {
		return core.VerifierAssessment{}, err
	}
	assessment := core.VerifierAssessment{
		AssessmentID: req.AssessmentID, EpisodeID: req.EpisodeID, ActorRef: req.ActorRef,
		DeclaredContextClass: req.DeclaredContextClass, DeclaredProviderIdentity: req.DeclaredProviderIdentity,
		PreferredCandidates: append([]core.CandidateID(nil), req.PreferredCandidates...), SemanticRejections: append([]core.CandidateID(nil), req.SemanticRejections...),
		Rationale: req.Rationale, CreatedAt: s.now().UTC(),
	}
	if s.qualifier != nil {
		result, err := s.qualifier.QualifyVerifierContext(ctx, QualifyVerifierContextRequest{EpisodeID: req.EpisodeID, ActorRef: req.ActorRef, DeclaredContextClass: req.DeclaredContextClass, DeclaredProviderID: req.DeclaredProviderIdentity})
		if err != nil {
			return core.VerifierAssessment{}, err
		}
		if err := result.Validate(); err != nil {
			return core.VerifierAssessment{}, err
		}
		if result.Status == core.ContextQualificationQualified {
			assessment.QualifiedContextClass = result.QualifiedContextClass
			copyQualification := *result.Qualification
			assessment.ContextQualification = &copyQualification
		}
	}
	if err := assessment.Validate(); err != nil {
		return core.VerifierAssessment{}, err
	}
	if _, _, err := s.assessments.RecordAssessment(ctx, assessment); err != nil {
		return core.VerifierAssessment{}, err
	}
	return assessment, nil
}

func (s *Service) requireAssessmentCandidates(ctx context.Context, req RecordAssessmentRequest) error {
	ids := append([]core.CandidateID(nil), req.PreferredCandidates...)
	ids = append(ids, req.SemanticRejections...)
	for _, id := range ids {
		candidate, found, err := s.mutations.FindCandidate(ctx, id)
		if err != nil {
			return err
		}
		if !found || candidate.EpisodeID != req.EpisodeID {
			return fmt.Errorf("assessment candidate unavailable in episode")
		}
	}
	return nil
}
