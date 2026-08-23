package ipc

import (
	"context"
	"fmt"
	"time"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type HandoffActions interface {
	RequestHandoffPublic(context.Context, handoff.Request) (handoff.PublicState, error)
	WaitHandoffPublic(context.Context, handoffapp.WaitRequest) (handoff.PublicState, bool, error)
	AbortHandoffPublic(context.Context, string) (handoff.PublicState, error)
	InspectHandoffPublic(context.Context, string) (handoff.PublicState, error)
}

type HandoffPresentationActions interface {
	RequestHandoffPublicWithPresentation(context.Context, handoff.Request, *terminalpresentation.BridgeAffinityHint) (handoff.PublicState, error)
}

func validateHandoffRequestV2(v RequestV2) error {
	switch v.Action {
	case "handoff.request":
		if v.HandoffCompletion == nil {
			return failure.New(failure.InvalidInput, map[string]string{"field": "completion"}, fmt.Errorf("handoff completion missing"))
		}
		if v.TerminalAffinity != nil {
			if err := v.TerminalAffinity.Validate(); err != nil {
				return failure.New(failure.InvalidInput, map[string]string{"field": "terminal_affinity"}, err)
			}
		}
		return (handoff.Request{HandoffID: v.HandoffID, SessionID: v.SessionID, Reason: handoff.Reason(v.Reason), Privacy: v.HandoffPrivacy, Completion: *v.HandoffCompletion}).Validate()
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
		handoffReq := handoff.Request{HandoffID: req.HandoffID, SessionID: req.SessionID, Reason: handoff.Reason(req.Reason), Privacy: req.HandoffPrivacy, Completion: *req.HandoffCompletion}
		if presentation, ok := s.actions.(HandoffPresentationActions); ok {
			state, err = presentation.RequestHandoffPublicWithPresentation(ctx, handoffReq, req.TerminalAffinity)
		} else {
			state, err = actions.RequestHandoffPublic(ctx, handoffReq)
		}
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
