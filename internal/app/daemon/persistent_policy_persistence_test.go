package daemon

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestPersistentV4ReservationFreezesResolvedExecutionPolicy(t *testing.T) {
	svc := &Service{options: Options{Incarnation: "daemon-policy", Shell: "/bin/sh"}}
	req := StartRequest{
		ProtocolVersion: 2,
		OperationID:     "persistent-policy-freeze",
		Command:         "sleep 10",
		CWD:             "/tmp",
		Persistent:      true,
		SessionName:     "policy-freeze",
		StdinMode:       operation.StdinModeClosed,
		TimeoutMS:       5000,
	}
	resolved, err := svc.resolveExecutionPolicy(req)
	if err != nil {
		t.Fatal(err)
	}
	intent := operation.Intent{
		Command:       req.Command,
		CWD:           req.CWD,
		Persistent:    true,
		SessionName:   req.SessionName,
		StdinMode:     req.StdinMode,
		TimeoutMS:     req.TimeoutMS,
		Resolved:      &resolved,
		TimeoutSource: timeoutSourceOf(resolved),
	}
	spec := operation.ExecutionSpec{
		Mode:       operation.ExecutionModeShell,
		Shell:      "/bin/sh",
		Executable: "/bin/sh",
		Command:    req.Command,
		CWD:        req.CWD,
		TimeoutMS:  resolved.TimeoutMS,
		StdinMode:  resolved.StdinMode,
	}
	got, err := svc.reservationForStart(req, operation.ID(req.OperationID), intent, spec)
	if err != nil {
		t.Fatal(err)
	}
	if got.StdinMode != operation.StdinModeClosed {
		t.Fatalf("reservation stdin_mode=%q want %q", got.StdinMode, operation.StdinModeClosed)
	}
	if got.TimeoutSource != timeoutSourceRequested {
		t.Fatalf("reservation timeout_source=%q want %q", got.TimeoutSource, timeoutSourceRequested)
	}
	if got.StdinModeSource != "requested" {
		t.Fatalf("reservation stdin_mode_source=%q want requested", got.StdinModeSource)
	}
}
