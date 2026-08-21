package main

import (
	"context"
	"errors"
	"testing"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	decisionapp "github.com/maemreyo/shellbeam/internal/app/decisionprotocol"
	decisioncore "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestProjectDecisionProtocolErrorPublishesStableSafeFailures(t *testing.T) {
	existing := failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": "ws_01K00000000000000000000000"}, errors.New("private"))
	if got := projectDecisionProtocolError(existing); got != existing {
		t.Fatalf("existing failure changed: %v", got)
	}
	tests := []struct {
		name   string
		err    error
		code   failure.Code
		reason string
	}{
		{"episode", decisionapp.ErrEpisodeNotFound, failure.DecisionEpisodeNotFound, ""},
		{"candidate", decisionapp.ErrCandidateNotFound, failure.DecisionCandidateNotFound, ""},
		{"experiment", decisionapp.ErrExperimentNotFound, failure.DecisionExperimentNotFound, ""},
		{"reason", decisioncore.NewReasonError(decisioncore.ReasonProjectionConflict, "private episode-secret"), failure.DecisionProtocolRejected, string(decisioncore.ReasonProjectionConflict)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			public := failure.Public(projectDecisionProtocolError(tc.err))
			if public.Code != tc.code {
				t.Fatalf("public=%#v", public)
			}
			if tc.reason == "" {
				if len(public.Details) != 0 {
					t.Fatalf("details=%#v", public.Details)
				}
			} else if len(public.Details) != 1 || public.Details["reason"] != tc.reason {
				t.Fatalf("details=%#v", public.Details)
			}
		})
	}
	public := failure.Public(projectDecisionProtocolError(errors.New("secret entity episode-secret")))
	if public.Code != failure.Internal || len(public.Details) != 0 {
		t.Fatalf("internal projection=%#v", public)
	}
}

type decisionFailureOperations struct {
	decisionProtocolOperations
	projectErr error
	sealErr    error
}

func (f decisionFailureOperations) Project(context.Context, decisioncore.EpisodeID, decisioncore.CandidateID) (decisioncore.DecisionProjection, error) {
	return decisioncore.DecisionProjection{}, f.projectErr
}

func (f decisionFailureOperations) SealExperiment(context.Context, decisioncore.ExperimentID, string) (decisioncore.ExperimentSeal, decisioncore.DecisionProjection, error) {
	return decisioncore.ExperimentSeal{}, decisioncore.DecisionProjection{}, f.sealErr
}

func TestDecisionProtocolRuntimeProjectsSemanticFailuresOnce(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		req        ipcadapter.DecisionRequestV1
		projectErr error
		sealErr    error
		wantCode   failure.Code
		wantReason string
	}{
		{name: "episode_not_found", action: "decision.inspect", req: ipcadapter.DecisionRequestV1{EpisodeID: "episode-missing"}, projectErr: decisionapp.ErrEpisodeNotFound, wantCode: failure.DecisionEpisodeNotFound},
		{name: "candidate_not_found", action: "decision.inspect", req: ipcadapter.DecisionRequestV1{EpisodeID: "episode", CandidateID: "candidate-missing"}, projectErr: decisionapp.ErrCandidateNotFound, wantCode: failure.DecisionCandidateNotFound},
		{name: "experiment_not_found", action: "decision.experiment.seal", req: ipcadapter.DecisionRequestV1{ExperimentID: "experiment-missing", ActorRef: "actor"}, sealErr: decisionapp.ErrExperimentNotFound, wantCode: failure.DecisionExperimentNotFound},
		{name: "stale_projection_reason", action: "decision.inspect", req: ipcadapter.DecisionRequestV1{EpisodeID: "episode"}, projectErr: decisioncore.NewReasonError(decisioncore.ReasonProjectionConflict, "private stale episode-id"), wantCode: failure.DecisionProtocolRejected, wantReason: string(decisioncore.ReasonProjectionConflict)},
		{name: "invalid_transition_reason", action: "decision.inspect", req: ipcadapter.DecisionRequestV1{EpisodeID: "episode"}, projectErr: decisioncore.NewReasonError(decisioncore.ReasonEpisodeTerminalConflict, "private terminal episode-id"), wantCode: failure.DecisionProtocolRejected, wantReason: string(decisioncore.ReasonEpisodeTerminalConflict)},
		{name: "internal_stays_internal", action: "decision.inspect", req: ipcadapter.DecisionRequestV1{EpisodeID: "episode"}, projectErr: errors.New("private entity episode-secret"), wantCode: failure.Internal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &decisionProtocolRuntime{service: decisionFailureOperations{projectErr: tc.projectErr, sealErr: tc.sealErr}}
			_, err := runtime.DecisionProtocol(context.Background(), tc.action, "", tc.req)
			if err == nil {
				t.Fatal("expected projected error")
			}
			public := failure.Public(err)
			if public.Code != tc.wantCode {
				t.Fatalf("public=%#v", public)
			}
			if tc.wantReason == "" {
				if len(public.Details) != 0 {
					t.Fatalf("unexpected details=%#v", public.Details)
				}
			} else if len(public.Details) != 1 || public.Details["reason"] != tc.wantReason {
				t.Fatalf("reason details=%#v", public.Details)
			}
		})
	}
}
