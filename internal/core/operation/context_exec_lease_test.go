package operation

import (
	"strings"
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
)

func TestContextExecLeaseValidatesExactSessionEpochAndRequestBinding(t *testing.T) {
	valid := ContextExecLease{
		SessionID:          SessionID("session_context_01"),
		AuthorityEpoch:     delegated.AuthorityEpoch(4),
		ContextExecID:      "ctxexec_lease_01",
		RequestFingerprint: strings.Repeat("a", 64),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid lease: %v", err)
	}
	cases := map[string]func(*ContextExecLease){
		"session":      func(v *ContextExecLease) { v.SessionID = "" },
		"epoch":        func(v *ContextExecLease) { v.AuthorityEpoch = 0 },
		"context exec": func(v *ContextExecLease) { v.ContextExecID = "" },
		"fingerprint":  func(v *ContextExecLease) { v.RequestFingerprint = "bad" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := valid
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid lease accepted: %#v", got)
			}
		})
	}
}
