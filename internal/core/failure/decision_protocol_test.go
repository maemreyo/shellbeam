package failure

import (
	"errors"
	"testing"
)

func TestDecisionProtocolFailureCodesAreStableAndSafe(t *testing.T) {
	tests := []struct {
		code    Code
		want    string
		details map[string]string
	}{
		{DecisionEpisodeNotFound, "decision_episode_not_found", map[string]string{"episode_id": "episode-secret"}},
		{DecisionCandidateNotFound, "decision_candidate_not_found", map[string]string{"candidate_id": "candidate-secret"}},
		{DecisionExperimentNotFound, "decision_experiment_not_found", map[string]string{"experiment_id": "experiment-secret"}},
		{DecisionProtocolRejected, "decision_protocol_rejected", map[string]string{"reason": "PROJECTION_CONFLICT", "episode_id": "episode-secret"}},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			public := Public(New(tc.code, tc.details, errors.New("private entity episode-secret")))
			if public.Code != tc.code || string(public.Code) != tc.want || public.Message == "" {
				t.Fatalf("public=%#v", public)
			}
			if tc.code == DecisionProtocolRejected {
				if len(public.Details) != 1 || public.Details["reason"] != "PROJECTION_CONFLICT" {
					t.Fatalf("rejection details=%#v", public.Details)
				}
			} else if len(public.Details) != 0 {
				t.Fatalf("lookup details leaked=%#v", public.Details)
			}
		})
	}
}
