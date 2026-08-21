package decisionprotocol

import (
	"context"
	"errors"
	"testing"
)

func TestDecisionEntityLookupErrorsAreTyped(t *testing.T) {
	svc, _, _ := selectionService(t, task7Policy())
	if _, err := svc.Project(context.Background(), "episode-missing", ""); !errors.Is(err, ErrEpisodeNotFound) {
		t.Fatalf("episode lookup err=%v", err)
	}
	if _, err := svc.Project(context.Background(), "ep-selection", "candidate-missing"); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("candidate lookup err=%v", err)
	}
	experimentSvc, _, _, _, _, _ := task4ExperimentService(t)
	if _, _, err := experimentSvc.SealExperiment(context.Background(), "experiment-missing", "actor"); !errors.Is(err, ErrExperimentNotFound) {
		t.Fatalf("experiment lookup err=%v", err)
	}
}
