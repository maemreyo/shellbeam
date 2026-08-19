package ipc

import (
	"context"
	"fmt"
	"time"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type HandoffActions interface {
	RequestHandoffPublic(context.Context, handoff.Request) (handoff.PublicState, error)
	WaitHandoffPublic(context.Context, handoffapp.WaitRequest) (handoff.PublicState, bool, error)
	AbortHandoffPublic(context.Context, string) (handoff.PublicState, error)
	InspectHandoffPublic(context.Context, string) (handoff.PublicState, error)
}

func validateHandoffRequestV2(v RequestV2) error {
	switch v.Action {
	case "handoff.request":
		if v.HandoffCompletion == nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "completion"}, fmt.Errorf("handoff completion missing"))
		}
		return (handoff.Request{HandoffID: v.HandoffID, SessionID: v.SessionID, Reason: v.HandoffReason, Privacy: v.HandoffPrivacy, Completion: *v.HandoffCompletion}).Validate()
	case "handoff.wait":
		if err := handoff.ValidateHandoffID(v.HandoffID); err != nil {
			return err
		}
		if v.YieldMS < 0 || v.YieldMS > handoffapp.MaxWait.Milliseconds() {
			return failure.New(failure.InvalidInput, map[string]string{"field": "yield_time_ms"}, fmt.Errorf("invalid handoff wait"))
		}
		return nil
	case "handoff.abort", "inspect.handoff":
		return handoff.ValidateHandoffID(v.HandoffID)
	default:
		return failure.New(failure.InvalidInput, map[string]string{"field": "action"}, fmt.Errorf("invalid handoff action"))
	}
}

func (s *Server) handoffV2(ctx context.Context, req RequestV2, resp *ResponseV2) error {
	actions, ok := s.actions.(HandoffActions)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "interactive_handoff"}, nil)
	}
	var state handoff.PublicState
	var err error
	switch req.Action {
	case "handoff.request":
		state, err = actions.RequestHandoffPublic(ctx, handoff.Request{HandoffID: req.HandoffID, SessionID: req.SessionID, Reason: req.HandoffReason, Privacy: req.HandoffPrivacy, Completion: *req.HandoffCompletion})
	case "handoff.wait":
		state, resp.HandoffTimedOut, err = actions.WaitHandoffPublic(ctx, handoffapp.WaitRequest{HandoffID: req.HandoffID, Yield: time.Duration(req.YieldMS) * time.Millisecond})
	case "handoff.abort":
		state, err = actions.AbortHandoffPublic(ctx, req.HandoffID)
	case "inspect.handoff":
		state, err = actions.InspectHandoffPublic(ctx, req.HandoffID)
	}
	if err != nil {
		return err
	}
	resp.Handoff = &state
	return nil
}
