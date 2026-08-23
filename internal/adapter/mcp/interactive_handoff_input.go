package mcp

import (
	"fmt"

	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

func validateHandoffInputV2(v input) error {
	switch v.Action {
	case "handoff.request":
		if v.HandoffCompletion == nil {
			return fmt.Errorf("handoff.request requires completion")
		}
		return (handoff.Request{HandoffID: v.HandoffID, SessionID: v.SessionID, Reason: handoff.Reason(v.Reason), Privacy: v.HandoffPrivacy, Completion: *v.HandoffCompletion}).Validate()
	case "handoff.wait":
		if err := handoff.ValidateHandoffID(v.HandoffID); err != nil {
			return err
		}
		if v.YieldMS < 0 || v.YieldMS > handoffapp.MaxWait.Milliseconds() {
			return fmt.Errorf("invalid handoff wait")
		}
		return nil
	case "handoff.abort", "inspect.handoff":
		return handoff.ValidateHandoffID(v.HandoffID)
	default:
		return fmt.Errorf("invalid handoff action")
	}
}

func isPublicHandoffAction(action string) bool {
	switch action {
	case "handoff.request", "handoff.wait", "handoff.abort", "inspect.handoff":
		return true
	default:
		return false
	}
}
