package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type provenanceStoreFake struct {
	value string
	err   error
}

func (f provenanceStoreFake) LoadInputAuthorityProvenance(context.Context, operation.SessionID) (string, error) {
	return f.value, f.err
}

func TestDelegatedTerminalInputAuthorityProvenanceNeverOverstatesAgentOnly(t *testing.T) {
	cases := []struct {
		name  string
		store provenanceStoreFake
		want  string
	}{
		{"agent", provenanceStoreFake{value: receipt.InputAuthorityAgentOnly}, receipt.InputAuthorityAgentOnly},
		{"human", provenanceStoreFake{value: receipt.InputAuthorityHumanWriteGranted}, receipt.InputAuthorityHumanWriteGranted},
		{"read failure", provenanceStoreFake{err: errors.New("unavailable")}, receipt.InputAuthorityHumanWriteGranted},
		{"future value", provenanceStoreFake{value: "future"}, receipt.InputAuthorityHumanWriteGranted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveTerminalInputAuthorityProvenance(t.Context(), tc.store, operation.SessionID("session-provenance"))
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}
