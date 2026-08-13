package receipt

import (
	"github.com/maemreyo/shellbeam/internal/core/session"
	"testing"
)

func TestSuccessRequiresCompleteEvidence(t *testing.T) {
	r := Receipt{SchemaVersion: 1, State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: true, Code: ptr(0)}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.InputAcceptedBytes = 1
	if err := r.Validate(); err == nil {
		t.Fatal("accepted input not delivered")
	}
}

func ptr(v int) *int { return &v }
