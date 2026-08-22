package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestStoreCanonicalizesDeterministicNoChildFailureFromOnlySafeSources(t *testing.T) {
	for _, attempted := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare_failure", true: "spawn_failure"}[attempted], func(t *testing.T) {
			r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
			want := validContextExecV2State(t, "ctxexec_no_child")
			if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
				t.Fatal(got.Err)
			}
			helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_no_child", Generation: "helper_no_child", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
			if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); got.Err != nil {
				t.Fatal(got.Err)
			}
			final := contextexec.ContextBinding{SessionID: want.Expectation.SessionID, AuthorityEpoch: want.Expectation.AuthorityEpoch, ShellIdentity: want.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: want.Expectation.CWDObserved, PrivacyState: want.Expectation.PrivacyState}
			state, got := r.BindHelperGeneration(context.Background(), want.Request.ContextExecID, helper, final, time.Now().UTC(), strings.Repeat("a", 64))
			if got.Err != nil {
				t.Fatal(got.Err)
			}
			result := contextexec.Result{SchemaVersion: contextexec.SchemaVersion, ContextExecID: want.Request.ContextExecID, RequestFingerprint: want.RequestFingerprint, Lifecycle: contextexec.LifecycleCanonicalized, Context: final, Helper: &helper, FailureCode: "context_exec_unavailable", EvidenceQuality: contextexec.EvidenceQualityUnproven}
			if attempted {
				child := validContextChildReservationV2(t, state, "context_no_child_op", "context_no_child_session", "/usr/bin/go")
				if _, created, got := r.ReserveOperation(context.Background(), child); got.Err != nil || !created {
					t.Fatal(got.Err)
				}
				reserve := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildReserved, ChildOperationID: child.OperationID, ChildSessionID: child.SessionID}
				if state, got = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserve); got.Err != nil {
					t.Fatal(got.Err)
				}
				reserve.ExecutionAuthorized = true
				if state, got = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserve); got.Err != nil {
					t.Fatal(got.Err)
				}
				result.Executable = contextexec.ExecutableIdentity{Requested: want.Request.Argv[0], ResolvedPath: "/usr/bin/go"}
				result.Spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "context_exec_unavailable"}
			}
			stored, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleCanonicalized, Result: &result})
			if got.Err != nil {
				t.Fatalf("canonical no-child transition: %v", got.Err)
			}
			if stored.Lifecycle != contextexec.LifecycleCanonicalized || stored.Result == nil || stored.Result.EvidenceAuthority != "" {
				t.Fatalf("stored=%#v", stored)
			}
		})
	}
}
