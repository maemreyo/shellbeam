package contextexec

import (
	"context"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestReconcileAppliesConservativeRecoveryMatrixWithoutHelperMutation(t *testing.T) {
	req := admissionRequest()
	requested := helperRequestedState(t, req)
	authenticated := helperAuthenticatedState(t, req)
	childReserved := authenticated.Clone()
	childReserved.Lifecycle = core.LifecycleChildReserved
	childReserved.ChildOperationID, childReserved.ChildSessionID, _ = operation.DeriveContextChildIDs(childReserved.RequestFingerprint)
	childReserved.UpdatedAt = childReserved.UpdatedAt.Add(1)
	if err := childReserved.Validate(); err != nil {
		t.Fatal(err)
	}
	authorized := childReserved.Clone()
	authorized.ExecutionAuthorized = true
	authorized.UpdatedAt = authorized.UpdatedAt.Add(1)
	if err := authorized.Validate(); err != nil {
		t.Fatal(err)
	}
	spawned := authorized.Clone()
	spawned.Lifecycle = core.LifecycleChildSpawned
	spawned.UpdatedAt = spawned.UpdatedAt.Add(1)
	if err := spawned.Validate(); err != nil {
		t.Fatal(err)
	}
	terminal := spawned.Clone()
	result := validChildTerminalResult(t, terminal)
	terminal.Lifecycle = core.LifecycleChildTerminal
	terminal.Result = &result
	terminal.UpdatedAt = terminal.UpdatedAt.Add(1)
	if err := terminal.Validate(); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name          string
		state         operation.ContextExecState
		want          RecoveryDisposition
		wantLifecycle core.Lifecycle
		wantAdvance   bool
		wantRelease   bool
	}{
		{"reserved", admissionReservedState(t, req), RecoveryResumeAdmission, core.LifecycleReserved, false, false},
		{"helper requested", requested, RecoveryAmbiguousDelivery, core.LifecycleAmbiguous, true, false},
		{"helper authenticated", authenticated, RecoveryReconnectSameGeneration, core.LifecycleHelperAuthenticated, false, false},
		{"child reserved before ack", childReserved, RecoveryPreparedClosed, core.LifecycleChildReserved, false, false},
		{"child reserved after ack", authorized, RecoverySpawnUnknown, core.LifecycleAmbiguous, true, false},
		{"child spawned", spawned, RecoveryHelperLost, core.LifecycleHelperLost, true, false},
		{"child terminal", terminal, RecoveryFinal, core.LifecycleCanonicalized, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &admissionStoreFake{state: tc.state.Clone(), found: true, recoveryCandidates: []operation.ContextExecState{tc.state.Clone()}}
			svc := NewService(Options{Store: store})
			got, err := svc.Reconcile(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Disposition != tc.want || got[0].State.Lifecycle != tc.wantLifecycle {
				t.Fatalf("decision=%#v", got)
			}
			if (store.advanceCalls != 0) != tc.wantAdvance {
				t.Fatalf("advance=%d", store.advanceCalls)
			}
			if (store.releaseCalls != 0) != tc.wantRelease {
				t.Fatalf("release=%d wantRelease=%v", store.releaseCalls, tc.wantRelease)
			}
		})
	}
}

func TestReconcileAuthorizedChildReservedNeverReturnsDuplicateExecutionAuthorization(t *testing.T) {
	req := admissionRequest()
	state := helperAuthenticatedState(t, req)
	state.Lifecycle = core.LifecycleChildReserved
	state.ChildOperationID, state.ChildSessionID, _ = operation.DeriveContextChildIDs(state.RequestFingerprint)
	state.ExecutionAuthorized = true
	state.UpdatedAt = state.UpdatedAt.Add(1)
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &admissionStoreFake{state: state, found: true, recoveryCandidates: []operation.ContextExecState{state}}
	svc := NewService(Options{Store: store})
	decisions, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Disposition != RecoverySpawnUnknown || decisions[0].State.Lifecycle != core.LifecycleAmbiguous {
		t.Fatalf("decisions=%#v", decisions)
	}
}

func validChildTerminalResult(t *testing.T, state operation.ContextExecState) core.Result {
	t.Helper()
	zero := 0
	return core.Result{
		SchemaVersion: core.SchemaVersion, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint,
		Lifecycle: core.LifecycleChildTerminal, Context: *state.Context, Helper: state.Helper,
		Executable: core.ExecutableIdentity{Requested: state.Request.Argv[0], ResolvedPath: "/usr/bin/go"},
		Spawn:      receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
		Output:          core.OutputEvidence{OutputComplete: true, Attribution: core.OutputAttributionHelperOwnedChildPipes},
		EvidenceQuality: core.EvidenceQualityComplete,
	}
}
