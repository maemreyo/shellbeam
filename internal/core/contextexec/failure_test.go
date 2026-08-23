package contextexec

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestContextExecFailureVocabularyIsStableAndPublicSafe(t *testing.T) {
	cases := map[failure.Code]string{
		failure.ContextExecUnavailable:      "context_exec_unavailable",
		failure.ContextExecStaleGeneration:  "context_exec_stale_generation",
		failure.ContextExecNotAgentOwned:    "context_exec_not_agent_owned",
		failure.ContextExecPrivacyBlocked:   "context_exec_privacy_blocked",
		failure.ContextExecBoundaryUnproven: "context_exec_boundary_unproven",
		failure.ContextHelperAuthFailed:     "context_helper_auth_failed",
		failure.ContextHelperLost:           "context_helper_lost",
		failure.ContextExecAmbiguous:        "context_exec_ambiguous",
	}
	for code, want := range cases {
		if string(code) != want {
			t.Fatalf("code=%q want=%q", code, want)
		}
		public := failure.Public(failure.New(code, map[string]string{
			"context_exec_id": "ctxexec_01",
			"session_id":      "session_01",
			"authority_epoch": "3",
			"reason":          "safe_reason",
			"secret":          "must_not_project",
		}, nil))
		if public.Code != code || public.Message == "" {
			t.Fatalf("public=%#v", public)
		}
		if _, ok := public.Details["secret"]; ok {
			t.Fatalf("unsafe detail projected for %s", code)
		}
	}
}
