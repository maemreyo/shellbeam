package receipt

import (
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestStructuredResultSeparatesOperationChildAndOutput(t *testing.T) {
	exitOne := 1
	rec := Receipt{
		SchemaVersion:        2,
		OperationID:          "op",
		SessionID:            "session",
		RequestFingerprint:   "request",
		ExecutionFingerprint: "execution",
		DaemonIncarnation:    "daemon",
		State:                session.Failed,
		Outcome:              session.Failure,
		OutputBytes:          9,
		OutputComplete:       true,
		Spawn:                SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:                 ExitEvidence{Reaped: true, Code: &exitOne},
	}

	got, err := NewResult(ResultInput{
		OperationID: "op",
		SessionID:   "session",
		State:       session.Failed,
		Outcome:     session.Failure,
		Preview:     "failure",
		RawBytes:    9,
		Cursor:      0,
		NextCursor:  7,
		Truncated:   true,
		Receipt:     &rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Operation.State != OperationTerminal || got.Child == nil || got.Child.State != ChildExited || got.Child.Outcome != session.Failure {
		t.Fatalf("result state mapping=%#v", got)
	}
	if got.Child.ExitCode == nil || *got.Child.ExitCode != 1 || got.Child.TimedOut {
		t.Fatalf("child evidence=%#v", got.Child)
	}
	if got.Output.RawBytes != 9 || got.Output.ReturnedBytes != 7 || got.Output.NextCursor != 7 || !got.Output.Truncated || !got.Output.OutputComplete {
		t.Fatalf("output=%#v", got.Output)
	}
}

func TestStructuredResultMapsTerminalFailureKindsWithoutInventingExitEvidence(t *testing.T) {
	tests := []struct {
		name      string
		state     session.State
		outcome   session.Outcome
		spawn     SpawnEvidence
		exit      ExitEvidence
		wantState ChildState
		wantTimed bool
	}{
		{name: "spawn failure", state: session.Failed, outcome: session.Failure, spawn: SpawnEvidence{Attempted: true}, wantState: ChildSpawnFailed},
		{name: "timeout", state: session.TimedOut, outcome: session.Timeout, spawn: SpawnEvidence{Attempted: true, Succeeded: true}, exit: ExitEvidence{Reaped: true}, wantState: ChildExited, wantTimed: true},
		{name: "kill", state: session.Killed, outcome: session.KilledOutcome, spawn: SpawnEvidence{Attempted: true, Succeeded: true}, exit: ExitEvidence{Reaped: true}, wantState: ChildExited},
		{name: "abandoned", state: session.Abandoned, outcome: session.Ambiguous, wantState: ChildUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := Receipt{SchemaVersion: 2, OperationID: "op", SessionID: "s", RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "d", State: tt.state, Outcome: tt.outcome, Spawn: tt.spawn, Exit: tt.exit}
			got, err := NewResult(ResultInput{OperationID: "op", SessionID: "s", State: tt.state, Outcome: tt.outcome, Receipt: &rec})
			if err != nil {
				t.Fatal(err)
			}
			if got.Child == nil || got.Child.State != tt.wantState || got.Child.Outcome != tt.outcome || got.Child.TimedOut != tt.wantTimed {
				t.Fatalf("child=%#v", got.Child)
			}
			if tt.wantState != ChildExited && got.Child.ExitCode != nil {
				t.Fatalf("invented exit code=%#v", got.Child.ExitCode)
			}
		})
	}
}

func TestStructuredResultRejectsCursorBeyondRawOutput(t *testing.T) {
	_, err := NewResult(ResultInput{OperationID: "op", SessionID: "s", State: session.Running, RawBytes: 3, Cursor: 0, NextCursor: 4})
	if err == nil {
		t.Fatal("accepted next_cursor beyond raw output")
	}
}

func TestDelegatedV5StructuredResultProjectsAuthorityCaptureAndProviderExit(t *testing.T) {
	zero := 0
	rec := Receipt{
		SchemaVersion: 5, OperationID: "op-v5-result", SessionID: "session-v5-result",
		RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon",
		State: session.Completed, Outcome: session.Success, OutputBytes: 4, OutputComplete: true,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Code: &zero},
		SessionMode: "delegated_interactive", AuthorityEpoch: 3,
		EvidenceAuthority:        EvidenceAuthoritySessionLifecycleOnly,
		InputAuthorityProvenance: InputAuthorityAgentOnly,
		CaptureQuality:           CaptureComplete,
	}
	got, err := NewResult(ResultInput{OperationID: rec.OperationID, SessionID: rec.SessionID, State: rec.State, Outcome: rec.Outcome, Preview: "done", RawBytes: 4, NextCursor: 4, Receipt: &rec})
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionMode != "delegated_interactive" || got.AuthorityEpoch != 3 || got.EvidenceAuthority != EvidenceAuthoritySessionLifecycleOnly || got.InputAuthorityProvenance != InputAuthorityAgentOnly {
		t.Fatalf("authority projection=%#v", got)
	}
	if got.Output.CaptureQuality != CaptureComplete || len(got.Output.CaptureReasons) != 0 || !got.Output.OutputComplete {
		t.Fatalf("output=%#v", got.Output)
	}
	if got.Child == nil || got.Child.State != ChildExited || got.Child.ExitCode == nil || *got.Child.ExitCode != 0 {
		t.Fatalf("child=%#v", got.Child)
	}
	if rec.Exit.Reaped {
		t.Fatal("fixture accidentally claims daemon reap")
	}
}

func TestLegacyStructuredResultOmitsDelegatedProjectionFields(t *testing.T) {
	exit := 0
	rec := Receipt{SchemaVersion: 2, OperationID: "op-legacy-result", SessionID: "session-legacy-result", RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon", State: session.Completed, Outcome: session.Success, OutputComplete: true, Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Reaped: true, Code: &exit}}
	got, err := NewResult(ResultInput{OperationID: rec.OperationID, SessionID: rec.SessionID, State: rec.State, Outcome: rec.Outcome, Receipt: &rec})
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionMode != "" || got.AuthorityEpoch != 0 || got.EvidenceAuthority != "" || got.InputAuthorityProvenance != "" || got.Output.CaptureQuality != "" || len(got.Output.CaptureReasons) != 0 {
		t.Fatalf("legacy leaked delegated fields=%#v", got)
	}
}

func TestDelegatedLiveResultProjectsAuthorityContractWithoutTerminalReceipt(t *testing.T) {
	got, err := NewResult(ResultInput{
		OperationID: "op-live-delegated", SessionID: "session-live-delegated", State: session.Running,
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionMode != delegated.ModeDelegatedInteractive || got.AuthorityEpoch != 7 || got.EvidenceAuthority != EvidenceAuthoritySessionLifecycleOnly || got.InputAuthorityProvenance != InputAuthorityAgentOnly {
		t.Fatalf("delegated live metadata=%#v", got)
	}
	if got.Receipt != nil || got.Output.CaptureQuality != "" || len(got.Output.CaptureReasons) != 0 {
		t.Fatalf("live result invented terminal capture truth: %#v", got)
	}
}

func TestOrdinaryLiveResultDoesNotAcquireDelegatedAuthorityMetadata(t *testing.T) {
	got, err := NewResult(ResultInput{OperationID: "op-live", SessionID: "session-live", State: session.Running})
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionMode != "" || got.AuthorityEpoch != 0 || got.EvidenceAuthority != "" || got.InputAuthorityProvenance != "" {
		t.Fatalf("ordinary live metadata=%#v", got)
	}
}

func TestDelegatedV5StructuredResultProjectsPrivateOmissionWithoutPretendingComplete(t *testing.T) {
	zero := 0
	rec := Receipt{
		SchemaVersion: 5, OperationID: "op-v5-private-result", SessionID: "session-v5-private-result",
		RequestFingerprint: "request", ExecutionFingerprint: "execution", DaemonIncarnation: "daemon",
		State: session.Completed, Outcome: session.Success, OutputBytes: 4, OutputComplete: false,
		Spawn: SpawnEvidence{Attempted: true, Succeeded: true}, Exit: ExitEvidence{Code: &zero},
		SessionMode: delegated.ModeDelegatedInteractive, AuthorityEpoch: 3,
		EvidenceAuthority: EvidenceAuthoritySessionLifecycleOnly, InputAuthorityProvenance: InputAuthorityHumanWriteGranted,
		CaptureQuality: CapturePartial, CaptureReasons: []CaptureReason{CaptureReasonPrivateIntervalsOmitted},
	}
	got, err := NewResult(ResultInput{OperationID: rec.OperationID, SessionID: rec.SessionID, State: rec.State, Outcome: rec.Outcome, RawBytes: 4, NextCursor: 4, Receipt: &rec})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output.OutputComplete || got.Output.CaptureQuality != CapturePartial || len(got.Output.CaptureReasons) != 1 || got.Output.CaptureReasons[0] != CaptureReasonPrivateIntervalsOmitted {
		t.Fatalf("output=%#v", got.Output)
	}
	if got.EvidenceAuthority != EvidenceAuthoritySessionLifecycleOnly {
		t.Fatalf("evidence authority=%q", got.EvidenceAuthority)
	}
}
