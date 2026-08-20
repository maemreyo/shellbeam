package bridge

import (
	"context"
	"reflect"
	"testing"

	dp "github.com/maemreyo/shellbeam/internal/core/decisionprotocol"
)

var decisionProtocolActionContract = []string{
	"decision.policy.snapshot",
	"decision.policy.activate",
	"decision.create",
	"decision.inspect",
	"decision.evaluate",
	"decision.close_unresolved",
	"decision.candidate.create",
	"decision.candidate.revise",
	"decision.experiment.define",
	"decision.prediction.bind",
	"decision.experiment.seal",
	"decision.experiment.close",
	"decision.experiment.abort",
	"decision.assessment.record",
	"decision.selection.propose",
	"decision.override.create",
	"decision.selection.commit",
	"decision.authority.materialize",
}

func TestDecisionProtocolActionClassifierIsExact(t *testing.T) {
	for _, action := range decisionProtocolActionContract {
		if !isDecisionProtocolAction(action) {
			t.Fatalf("decision action not classified: %s", action)
		}
	}
	for _, action := range []string{"start", "poll", "decision", "decision.create.extra", "verification.policy.activate"} {
		if isDecisionProtocolAction(action) {
			t.Fatalf("non-decision action classified: %s", action)
		}
	}
}

type decisionBridgeClient struct {
	last   Request
	starts int
}

func (c *decisionBridgeClient) Forward(_ context.Context, req Request) (Response, error) {
	c.last = req
	if req.Action == "start" {
		c.starts++
	}
	projection := dp.DecisionProjection{EpisodeID: dp.EpisodeID("episode-transport")}
	return Response{Decision: &DecisionResponse{Projection: &projection}}, nil
}

func TestDecisionProtocolBridgeForwardsTypedPayloadWithoutExecution(t *testing.T) {
	client := &decisionBridgeClient{}
	h := New(client)
	request := &DecisionRequest{EpisodeID: "episode-transport", CandidateID: "candidate-transport"}
	out, err := h.Handle(context.Background(), Request{ProtocolVersion: 2, Action: "decision.evaluate", Decision: request})
	if err != nil {
		t.Fatal(err)
	}
	if client.starts != 0 || client.last.Action != "decision.evaluate" || !reflect.DeepEqual(client.last.Decision, request) {
		t.Fatalf("starts=%d last=%#v", client.starts, client.last)
	}
	if out.Decision == nil || out.Decision.Projection == nil || out.Decision.Projection.EpisodeID != "episode-transport" {
		t.Fatalf("out=%#v", out)
	}
}
