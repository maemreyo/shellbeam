package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestContextExecChildReservedMustPersistBeforeExecuteAuthorizationAndSpawn(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	want := validContextExecV2State(t, "ctxexec_v2_store")
	if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_v2", Generation: "helper_v2", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); got.Err != nil {
		t.Fatal(got.Err)
	}
	final := contextexec.ContextBinding{SessionID: want.Expectation.SessionID, AuthorityEpoch: want.Expectation.AuthorityEpoch, ShellIdentity: want.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: want.Expectation.CWDObserved, PrivacyState: want.Expectation.PrivacyState}
	boundary := time.Date(2026, 8, 21, 10, 5, 0, 0, time.UTC)
	stored, got := r.BindHelperGeneration(context.Background(), want.Request.ContextExecID, helper, final, boundary, strings.Repeat("a", 64))
	if got.Err != nil || stored.Context == nil || *stored.Context != final || stored.BoundaryObservedAt != boundary {
		t.Fatalf("bind stored=%#v result=%#v", stored, got)
	}
	child := validContextChildReservationV2(t, stored, "context_v2_child_op", "context_v2_child_session", "/usr/bin/go")
	if _, created, got := r.ReserveOperation(context.Background(), child); got.Err != nil || !created {
		t.Fatalf("child reserve created=%v result=%#v", created, got)
	}
	reserveTransition := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildReserved, ChildOperationID: child.OperationID, ChildSessionID: child.SessionID}
	stored, got = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveTransition)
	if got.Err != nil || stored.Lifecycle != contextexec.LifecycleChildReserved || stored.ExecutionAuthorized {
		t.Fatalf("child reserved=%#v result=%#v", stored, got)
	}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned}); got.Err == nil {
		t.Fatal("spawn accepted before execute authorization")
	}
	authorize := reserveTransition
	authorize.ExecutionAuthorized = true
	stored, got = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, authorize)
	if got.Err != nil || !stored.ExecutionAuthorized {
		t.Fatalf("authorize stored=%#v result=%#v", stored, got)
	}
	stored, got = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned})
	if got.Err != nil || stored.Lifecycle != contextexec.LifecycleChildSpawned || !stored.ExecutionAuthorized {
		t.Fatalf("spawn stored=%#v result=%#v", stored, got)
	}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveTransition); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("authorization regression result=%#v", got)
	}
}

func TestContextExecReserveReplayKeepsFirstExpectation(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	want := validContextExecV2State(t, "ctxexec_v2_replay")
	if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
		t.Fatal(got.Err)
	}
	replay := want.Clone()
	replay.Expectation.ProviderGeneration = "gen_after_drift"
	replay.Expectation.ShellIdentity = "zsh:runtime_after_drift"
	replay.Expectation.CWDObserved = "/tmp/after-drift"
	stored, created, got := r.ReserveContextExec(context.Background(), replay)
	if got.Err != nil || created {
		t.Fatalf("replay stored=%#v created=%v result=%#v", stored, created, got)
	}
	if stored.Expectation != want.Expectation {
		t.Fatalf("expectation replaced: got=%#v want=%#v", stored.Expectation, want.Expectation)
	}
}

func TestLegacyContextExecV1PresenceFailsClosedWithoutMutation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	want := validContextExecV2State(t, "ctxexec_legacy_block")
	legacyDir := filepath.Join(root, "context-exec", "v1")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(legacyDir, want.Request.ContextExecID+".json")
	original := []byte(`{"schema_version":1,"legacy":true}`)
	if err := os.WriteFile(legacyPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := r.LookupContextExec(context.Background(), want.Request.ContextExecID); err == nil || found || !errors.Is(err, failure.ContextExecAmbiguous) {
		t.Fatalf("lookup found=%v err=%v", found, err)
	}
	if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err == nil || created || !errors.Is(got.Err, failure.ContextExecAmbiguous) {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	after, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("legacy context exec bytes mutated")
	}
}

func validContextExecV2State(t *testing.T, id string) operation.ContextExecState {
	t.Helper()
	req := contextexec.Request{ContextExecID: id, SessionID: "parent_session_01", AuthorityEpoch: 4, Argv: []string{"go", "test", "./..."}, TimeoutMS: 30_000, MaxOutputBytes: 1 << 20}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	return operation.ContextExecState{SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_v2", ShellIdentity: "fish:runtime_v2", CWDObserved: "/tmp/project", PrivacyState: "standard"}, Lifecycle: contextexec.LifecycleReserved, CreatedAt: at, UpdatedAt: at}
}

func validContextChildReservationV2(t *testing.T, state operation.ContextExecState, opID operation.ID, sessionID operation.SessionID, executable string) operation.Reservation {
	t.Helper()
	binding := &operation.ContextExecBinding{ContextExecID: state.Request.ContextExecID, ParentSessionID: operation.SessionID(state.Request.SessionID), AuthorityEpoch: state.Request.AuthorityEpoch, RequestFingerprint: state.RequestFingerprint}
	execFP, err := binding.ExecutionFingerprint(state.Expectation.CWDObserved, executable)
	if err != nil {
		t.Fatal(err)
	}
	return operation.Reservation{SchemaVersion: operation.ContextExecReservationSchemaVersion, OperationID: opID, SessionID: sessionID, RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: execFP, ExecutionMode: operation.ExecutionModeArgv, Executable: executable, Argv: append([]string(nil), state.Request.Argv...), CWD: state.Expectation.CWDObserved, TimeoutMS: state.Request.TimeoutMS, DaemonIncarnation: "daemon", ContextExec: binding}
}
