package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestReserveContextExecConcurrentExactReplayHasOneWinnerAndChangedRequestConflicts(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	want := validContextExecState(t, "ctxexec_store_01")
	var created, failed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, won, result := r.ReserveContextExec(context.Background(), want)
			if result.Err != nil || stored.RequestFingerprint != want.RequestFingerprint {
				failed.Add(1)
				return
			}
			if won {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 || created.Load() != 1 {
		t.Fatalf("failed=%d created=%d", failed.Load(), created.Load())
	}

	conflict := want.Clone()
	conflict.Request.Argv[1] = "run"
	fp, err := conflict.Request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	conflict.RequestFingerprint = fp
	if _, _, got := r.ReserveContextExec(context.Background(), conflict); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("changed request result=%#v", got)
	}
	stored, found, err := r.LookupContextExec(context.Background(), want.Request.ContextExecID)
	if err != nil || !found || stored.RequestFingerprint != want.RequestFingerprint || stored.Lifecycle != contextexec.LifecycleReserved {
		t.Fatalf("stored=%#v found=%v err=%v", stored, found, err)
	}
}

func TestContextExecHelperGenerationIsOneShotPrivateAndChildSpawnRequiresV6Reservation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	want := validContextExecState(t, "ctxexec_helper_01")
	stored, created, result := r.ReserveContextExec(context.Background(), want)
	if result.Err != nil || !created {
		t.Fatalf("reserve stored=%#v created=%v result=%#v", stored, created, result)
	}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_01", Generation: "helper_gen_01", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	stored, result = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper})
	if result.Err != nil || stored.Lifecycle != contextexec.LifecycleHelperRequested {
		t.Fatalf("helper requested stored=%#v result=%#v", stored, result)
	}

	claimMaterial := "never-persist-this-capability"
	sum := sha256.Sum256([]byte(claimMaterial))
	verifier := hex.EncodeToString(sum[:])
	stored, result = bindContextExecHelper(t, r, want.Request.ContextExecID, helper, verifier)
	if result.Err != nil || stored.Lifecycle != contextexec.LifecycleHelperAuthenticated || stored.Helper == nil || stored.Helper.Generation != helper.Generation {
		t.Fatalf("authenticated stored=%#v result=%#v", stored, result)
	}
	if replay, got := bindContextExecHelper(t, r, want.Request.ContextExecID, helper, verifier); got.Err != nil || replay.Lifecycle != contextexec.LifecycleHelperAuthenticated {
		t.Fatalf("auth replay=%#v result=%#v", replay, got)
	}
	other := helper
	other.Generation = "helper_gen_02"
	if _, got := bindContextExecHelper(t, r, want.Request.ContextExecID, other, verifier); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("second generation result=%#v", got)
	}

	raw, err := os.ReadFile(r.contextExecPath(want.Request.ContextExecID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), claimMaterial) {
		t.Fatal("raw claim capability persisted")
	}
	publicWire := string(mustContextExecJSON(t, stored))
	if strings.Contains(publicWire, verifier) || strings.Contains(publicWire, "claim_verifier") {
		t.Fatalf("lookup/public state exposed verifier: %s", publicWire)
	}

	child := validContextChildReservation(t, stored, "context_child_op_01", "context_child_session_01")
	reserveChild := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildReserved, ChildOperationID: child.OperationID, ChildSessionID: child.SessionID}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveChild); got.Err == nil {
		t.Fatal("child_reserved accepted before v6 reservation")
	}
	if _, created, got := r.ReserveOperation(context.Background(), child); got.Err != nil || !created {
		t.Fatalf("child reservation created=%v result=%#v", created, got)
	}
	stored, result = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveChild)
	if result.Err != nil || stored.Lifecycle != contextexec.LifecycleChildReserved || stored.ExecutionAuthorized {
		t.Fatalf("child reserve stored=%#v result=%#v", stored, result)
	}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned}); got.Err == nil {
		t.Fatal("child_spawned accepted before execute authorization")
	}
	authorize := reserveChild
	authorize.ExecutionAuthorized = true
	stored, result = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, authorize)
	if result.Err != nil || !stored.ExecutionAuthorized {
		t.Fatalf("authorize stored=%#v result=%#v", stored, result)
	}
	spawn := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned}
	stored, result = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, spawn)
	if result.Err != nil || stored.Lifecycle != contextexec.LifecycleChildSpawned || stored.ChildOperationID != child.OperationID || stored.ChildSessionID != child.SessionID {
		t.Fatalf("spawn stored=%#v result=%#v", stored, result)
	}
	if replay, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, spawn); got.Err != nil || replay.Lifecycle != contextexec.LifecycleChildSpawned {
		t.Fatalf("spawn replay=%#v result=%#v", replay, got)
	}
}

func TestContextExecReserveFaultBoundariesRetryWithoutDuplicateReservation(t *testing.T) {
	points := []string{"create.create_temp", "create.write", "create.file_sync", "create.close", "create.link", "create.open_dir", "create.dir_sync"}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			r := openRecoveryRepository(t, root)
			want := validContextExecState(t, "ctxexec_fault_01")
			r.writer = failAtomicWriter(point)
			_, created, first := r.ReserveContextExec(context.Background(), want)
			if first.Err == nil || created {
				t.Fatalf("first created=%v result=%#v", created, first)
			}
			_, statErr := os.Stat(r.contextExecPath(want.Request.ContextExecID))
			committed := statErr == nil
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal(statErr)
			}
			r = openRecoveryRepository(t, root)
			stored, created, retry := r.ReserveContextExec(context.Background(), want)
			if retry.Err != nil || stored.RequestFingerprint != want.RequestFingerprint {
				t.Fatalf("retry stored=%#v created=%v result=%#v", stored, created, retry)
			}
			if created == committed {
				t.Fatalf("created=%v committed-before-retry=%v", created, committed)
			}
		})
	}
}

func validContextExecState(t *testing.T, id string) operation.ContextExecState {
	t.Helper()
	req := contextexec.Request{ContextExecID: id, SessionID: "parent_session_01", AuthorityEpoch: 4, Argv: []string{"go", "test", "./..."}, TimeoutMS: 30_000, MaxOutputBytes: 1 << 20}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 2, 30, 0, 0, time.UTC)
	return operation.ContextExecState{SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_store_01", ShellIdentity: "fish:runtime_01", CWDObserved: "/tmp/project", PrivacyState: "standard"}, Lifecycle: contextexec.LifecycleReserved, CreatedAt: at, UpdatedAt: at}
}

func bindContextExecHelper(t *testing.T, r *Repository, id string, helper contextexec.HelperBinding, verifier string) (operation.ContextExecState, app.StoreResult) {
	t.Helper()
	state, found, err := r.LookupContextExec(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("lookup before helper bind found=%v err=%v", found, err)
	}
	final := contextexec.ContextBinding{SessionID: state.Expectation.SessionID, AuthorityEpoch: state.Expectation.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: state.Expectation.CWDObserved, PrivacyState: state.Expectation.PrivacyState}
	boundary := time.Date(2026, 8, 21, 2, 31, 0, 0, time.UTC)
	return r.BindHelperGeneration(context.Background(), id, helper, final, boundary, verifier)
}

func validContextChildReservation(t *testing.T, state operation.ContextExecState, opID operation.ID, sessionID operation.SessionID) operation.Reservation {
	t.Helper()
	binding := &operation.ContextExecBinding{ContextExecID: state.Request.ContextExecID, ParentSessionID: operation.SessionID(state.Request.SessionID), AuthorityEpoch: state.Request.AuthorityEpoch, RequestFingerprint: state.RequestFingerprint}
	execFP, err := binding.ExecutionFingerprint(state.Expectation.CWDObserved, "/usr/bin/go")
	if err != nil {
		t.Fatal(err)
	}
	return operation.Reservation{SchemaVersion: operation.ContextExecReservationSchemaVersion, OperationID: opID, SessionID: sessionID, RequestFingerprint: state.RequestFingerprint, ExecutionFingerprint: execFP, ExecutionMode: operation.ExecutionModeArgv, Executable: "/usr/bin/go", Argv: append([]string(nil), state.Request.Argv...), CWD: state.Expectation.CWDObserved, TimeoutMS: state.Request.TimeoutMS, DaemonIncarnation: "daemon", ContextExec: binding}
}

func mustContextExecJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReserveContextExecExactRequestReplayReturnsFrozenFirstContext(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	want := validContextExecState(t, "ctxexec_context_replay")
	stored, created, got := r.ReserveContextExec(context.Background(), want)
	if got.Err != nil || !created {
		t.Fatalf("first reserve stored=%#v created=%v result=%#v", stored, created, got)
	}

	replay := want.Clone()
	replay.Expectation.ShellIdentity = "zsh:runtime_after_response_loss"
	replay.Expectation.CWDObserved = "/tmp/changed-after-reserve"
	stored, created, got = r.ReserveContextExec(context.Background(), replay)
	if got.Err != nil || created {
		t.Fatalf("exact public request replay rejected after context drift: stored=%#v created=%v result=%#v", stored, created, got)
	}
	if stored.Expectation != want.Expectation {
		t.Fatalf("replay replaced frozen expectation: got=%#v want=%#v", stored.Expectation, want.Expectation)
	}
}

func TestContextExecV6OperationReplayRejectsChangedExecutionIdentity(t *testing.T) {
	r := openRecoveryRepository(t, filepath.Join(t.TempDir(), "state"))
	state := validContextExecState(t, "ctxexec_v6_replay")
	child := validContextChildReservation(t, state, "context_v6_replay_op", "context_v6_replay_session")
	stored, created, got := r.ReserveOperation(context.Background(), child)
	if got.Err != nil || !created {
		t.Fatalf("first child reservation stored=%#v created=%v result=%#v", stored, created, got)
	}

	changed := child
	changed.Executable = "/usr/local/bin/go"
	changed.CWD = "/tmp/other-project"
	fp, err := changed.ContextExec.ExecutionFingerprint(changed.CWD, changed.Executable)
	if err != nil {
		t.Fatal(err)
	}
	changed.ExecutionFingerprint = fp
	if _, _, got := r.ReserveOperation(context.Background(), changed); !errors.Is(got.Err, failure.OperationConflict) {
		t.Fatalf("changed v6 execution identity accepted as replay: %#v", got)
	}
}

func TestContextExecTransitionFaultMatrixReopenRetryIsIdempotent(t *testing.T) {
	for _, point := range []string{"replace.rename", "replace.open_dir", "replace.dir_sync"} {
		t.Run(point, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "state")
			r := openRecoveryRepository(t, root)
			want := validContextExecState(t, "ctxexec_transition_fault")
			if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
				t.Fatalf("reserve created=%v result=%#v", created, got)
			}
			helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_fault", Generation: "helper_gen_fault", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
			transition := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}
			r.writer = failAtomicWriter(point)
			if _, first := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, transition); first.Err == nil {
				t.Fatal("fault did not interrupt helper-requested persistence")
			}
			r = openRecoveryRepository(t, root)
			stored, retry := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, transition)
			if retry.Err != nil || stored.Lifecycle != contextexec.LifecycleHelperRequested || stored.Helper == nil || *stored.Helper != helper {
				t.Fatalf("retry stored=%#v result=%#v", stored, retry)
			}
		})
	}
}

func TestContextExecAuthAndSpawnAckLossReplaySameGenerationAndChild(t *testing.T) {
	for _, stage := range []string{"auth", "spawn"} {
		for _, point := range []string{"replace.rename", "replace.open_dir", "replace.dir_sync"} {
			t.Run(stage+"/"+point, func(t *testing.T) {
				root := filepath.Join(t.TempDir(), "state")
				r := openRecoveryRepository(t, root)
				want := validContextExecState(t, "ctxexec_ack_fault_"+stage)
				if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
					t.Fatalf("reserve created=%v result=%#v", created, got)
				}
				helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_ack", Generation: "helper_gen_ack", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
				if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); got.Err != nil {
					t.Fatal(got.Err)
				}
				verifier := strings.Repeat("a", 64)
				if stage == "auth" {
					r.writer = failAtomicWriter(point)
					if _, first := bindContextExecHelper(t, r, want.Request.ContextExecID, helper, verifier); first.Err == nil {
						t.Fatal("fault did not interrupt auth persistence")
					}
					r = openRecoveryRepository(t, root)
					stored, retry := bindContextExecHelper(t, r, want.Request.ContextExecID, helper, verifier)
					if retry.Err != nil || stored.Lifecycle != contextexec.LifecycleHelperAuthenticated || stored.Helper == nil || stored.Helper.Generation != helper.Generation {
						t.Fatalf("auth retry stored=%#v result=%#v", stored, retry)
					}
					return
				}
				if _, got := bindContextExecHelper(t, r, want.Request.ContextExecID, helper, verifier); got.Err != nil {
					t.Fatal(got.Err)
				}
				state, found, err := r.LookupContextExec(context.Background(), want.Request.ContextExecID)
				if err != nil || !found {
					t.Fatalf("lookup found=%v err=%v", found, err)
				}
				child := validContextChildReservation(t, state, "context_ack_child_op", "context_ack_child_session")
				if _, created, got := r.ReserveOperation(context.Background(), child); got.Err != nil || !created {
					t.Fatalf("child reserve created=%v result=%#v", created, got)
				}
				reserveChild := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildReserved, ChildOperationID: child.OperationID, ChildSessionID: child.SessionID}
				if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveChild); got.Err != nil {
					t.Fatal(got.Err)
				}
				authorize := reserveChild
				authorize.ExecutionAuthorized = true
				if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, authorize); got.Err != nil {
					t.Fatal(got.Err)
				}
				transition := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned}
				r.writer = failAtomicWriter(point)
				if _, first := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, transition); first.Err == nil {
					t.Fatal("fault did not interrupt child-spawned persistence")
				}
				r = openRecoveryRepository(t, root)
				stored, retry := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, transition)
				if retry.Err != nil || stored.Lifecycle != contextexec.LifecycleChildSpawned || stored.ChildOperationID != child.OperationID || stored.ChildSessionID != child.SessionID {
					t.Fatalf("spawn retry stored=%#v result=%#v", stored, retry)
				}
			})
		}
	}
}

func TestContextExecTerminalAndCanonicalAckLossReplayWithoutInventingSecondChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	want := validContextExecState(t, "ctxexec_terminal_ack")
	if _, created, got := r.ReserveContextExec(context.Background(), want); got.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, got)
	}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_terminal_ack", Generation: "helper_gen_terminal_ack", RequestFingerprint: want.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := bindContextExecHelper(t, r, want.Request.ContextExecID, helper, strings.Repeat("b", 64)); got.Err != nil {
		t.Fatal(got.Err)
	}
	state, found, err := r.LookupContextExec(context.Background(), want.Request.ContextExecID)
	if err != nil || !found {
		t.Fatalf("lookup found=%v err=%v", found, err)
	}
	child := validContextChildReservation(t, state, "context_terminal_ack_op", "context_terminal_ack_session")
	if _, created, got := r.ReserveOperation(context.Background(), child); got.Err != nil || !created {
		t.Fatalf("child reserve created=%v result=%#v", created, got)
	}
	reserveChild := operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildReserved, ChildOperationID: child.OperationID, ChildSessionID: child.SessionID}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, reserveChild); got.Err != nil {
		t.Fatal(got.Err)
	}
	authorize := reserveChild
	authorize.ExecutionAuthorized = true
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, authorize); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildSpawned}); got.Err != nil {
		t.Fatal(got.Err)
	}

	terminal := validStoredContextExecResult(state, helper, contextexec.LifecycleChildTerminal)
	r.writer = failAtomicWriter("replace.open_dir")
	if _, first := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildTerminal, Result: &terminal}); first.Err == nil {
		t.Fatal("terminal ack-loss fault not surfaced")
	}
	r = openRecoveryRepository(t, root)
	stored, retry := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleChildTerminal, Result: &terminal})
	if retry.Err != nil || stored.Lifecycle != contextexec.LifecycleChildTerminal || stored.Result == nil || !stored.Result.Exit.Reaped {
		t.Fatalf("terminal retry stored=%#v result=%#v", stored, retry)
	}

	canonical := validStoredContextExecResult(state, helper, contextexec.LifecycleCanonicalized)
	r.writer = failAtomicWriter("replace.dir_sync")
	if _, first := r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleCanonicalized, Result: &canonical}); first.Err == nil {
		t.Fatal("canonical ack-loss fault not surfaced")
	}
	r = openRecoveryRepository(t, root)
	stored, retry = r.AdvanceContextExec(context.Background(), want.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleCanonicalized, Result: &canonical})
	if retry.Err != nil || stored.Lifecycle != contextexec.LifecycleCanonicalized || stored.Result == nil || stored.Result.EvidenceAuthority != contextexec.EvidenceAuthorityContextExecChildOwnedV1 || stored.ChildOperationID != child.OperationID {
		t.Fatalf("canonical retry stored=%#v result=%#v", stored, retry)
	}
}

func validStoredContextExecResult(state operation.ContextExecState, helper contextexec.HelperBinding, lifecycle contextexec.Lifecycle) contextexec.Result {
	zero := 0
	result := contextexec.Result{
		SchemaVersion: contextexec.SchemaVersion, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint,
		Lifecycle: lifecycle, Context: *state.Context, Helper: &helper,
		Executable: contextexec.ExecutableIdentity{Requested: state.Request.Argv[0], ResolvedPath: "/usr/bin/go"},
		Spawn:      receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
		Output:          contextexec.OutputEvidence{StdoutBytes: 8, OutputComplete: true, Attribution: contextexec.OutputAttributionHelperOwnedChildPipes},
		EvidenceQuality: contextexec.EvidenceQualityComplete,
	}
	if lifecycle == contextexec.LifecycleCanonicalized {
		result.EvidenceAuthority = contextexec.EvidenceAuthorityContextExecChildOwnedV1
	}
	return result
}
