package failure

import (
	"errors"
	"testing"
)

func TestDecisionContextUnavailableIsStableAndDetailFree(t *testing.T) {
	public := Public(New(DecisionContextUnavailable, map[string]string{
		"workspace_id": "ws_01K00000000000000000000001",
		"reason":       "ambiguous",
	}, errors.New("private daemon state")))
	if public.Code != DecisionContextUnavailable {
		t.Fatalf("code=%q", public.Code)
	}
	if public.Message == "" {
		t.Fatal("missing stable public message")
	}
	if len(public.Details) != 0 {
		t.Fatalf("unexpected public details=%#v", public.Details)
	}
}
