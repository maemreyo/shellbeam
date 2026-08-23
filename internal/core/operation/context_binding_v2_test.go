package operation

import (
	"testing"
	"time"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestContextExecStateV2SeparatesExpectationFromAuthenticatedPromptContext(t *testing.T) {
	req := contextexec.Request{ContextExecID: "ctxexec_v2_state", SessionID: "parent_session_01", AuthorityEpoch: 4, Argv: []string{"go", "test"}, TimeoutMS: 1000, MaxOutputBytes: 1024}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	expectation := contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_v2", ShellIdentity: "fish:runtime_v2", CWDObserved: "/tmp/project", PrivacyState: "standard"}
	reserved := ContextExecState{SchemaVersion: ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: expectation, Lifecycle: contextexec.LifecycleReserved, CreatedAt: at, UpdatedAt: at}
	if err := reserved.Validate(); err != nil {
		t.Fatalf("reserved: %v", err)
	}
	final := contextexec.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: expectation.CWDObserved, PrivacyState: expectation.PrivacyState}
	bad := reserved
	bad.Context = &final
	if err := bad.Validate(); err == nil {
		t.Fatal("reserved state pre-claimed prompt context")
	}

	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_v2", Generation: "helper_v2", RequestFingerprint: fp, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	requested := reserved
	requested.Lifecycle, requested.Helper = contextexec.LifecycleHelperRequested, &helper
	if err := requested.Validate(); err != nil {
		t.Fatalf("helper requested: %v", err)
	}
	authenticated := requested
	authenticated.Lifecycle, authenticated.Context, authenticated.BoundaryObservedAt = contextexec.LifecycleHelperAuthenticated, &final, at.Add(time.Second)
	if err := authenticated.Validate(); err != nil {
		t.Fatalf("authenticated: %v", err)
	}
	if authenticated.ExecutionAuthorized {
		t.Fatal("helper authentication authorized child execution")
	}

	childReserved := authenticated
	childReserved.Lifecycle = contextexec.LifecycleChildReserved
	childReserved.ChildOperationID, childReserved.ChildSessionID = "context_v2_child_op", "context_v2_child_session"
	if err := childReserved.Validate(); err != nil {
		t.Fatalf("child reserved: %v", err)
	}
	childReserved.ExecutionAuthorized = true
	if err := childReserved.Validate(); err != nil {
		t.Fatalf("authorized child reserved: %v", err)
	}
	spawned := childReserved
	spawned.Lifecycle = contextexec.LifecycleChildSpawned
	if err := spawned.Validate(); err != nil {
		t.Fatalf("spawned: %v", err)
	}
	spawned.ExecutionAuthorized = false
	if err := spawned.Validate(); err == nil {
		t.Fatal("child_spawned accepted without execute authorization")
	}
}

func TestContextExecLifecycleRequiresChildReservedBeforeSpawn(t *testing.T) {
	if contextexec.LifecycleHelperAuthenticated.CanAdvanceTo(contextexec.LifecycleChildSpawned) {
		t.Fatal("helper_authenticated advanced directly to child_spawned")
	}
	if !contextexec.LifecycleHelperAuthenticated.CanAdvanceTo(contextexec.LifecycleChildReserved) {
		t.Fatal("helper_authenticated cannot reserve child")
	}
	if !contextexec.LifecycleChildReserved.CanAdvanceTo(contextexec.LifecycleChildSpawned) {
		t.Fatal("child_reserved cannot advance to child_spawned")
	}
}

func TestCanonicalizedNoChildFailureStateAllowsPrepareOrFailedSpawnTruth(t *testing.T) {
	for _, spawnAttempted := range []bool{false, true} {
		state := authenticatedContextExecV2State(t)
		if spawnAttempted {
			state.Lifecycle = contextexec.LifecycleChildReserved
			state.ChildOperationID = "context_child_op_failure"
			state.ChildSessionID = "context_child_session_failure"
			state.ExecutionAuthorized = true
		}
		result := contextexec.Result{
			SchemaVersion: contextexec.SchemaVersion, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint,
			Lifecycle: contextexec.LifecycleCanonicalized, Context: *state.Context, Helper: state.Helper,
			FailureCode: "context_exec_unavailable", EvidenceQuality: contextexec.EvidenceQualityUnproven,
		}
		if spawnAttempted {
			result.Executable = contextexec.ExecutableIdentity{Requested: state.Request.Argv[0], ResolvedPath: "/usr/bin/go"}
			result.Spawn = receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "context_exec_unavailable"}
		}
		state.Lifecycle = contextexec.LifecycleCanonicalized
		state.Result = &result
		state.UpdatedAt = state.UpdatedAt.Add(time.Second)
		if err := state.Validate(); err != nil {
			t.Fatalf("spawnAttempted=%v err=%v state=%#v", spawnAttempted, err, state)
		}
	}
}

func authenticatedContextExecV2State(t *testing.T) ContextExecState {
	t.Helper()
	req := contextexec.Request{ContextExecID: "ctxexec_v2_failure", SessionID: "parent_session_failure", AuthorityEpoch: 4, Argv: []string{"go", "test"}, TimeoutMS: 1000, MaxOutputBytes: 1024}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	expectation := contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_failure", ShellIdentity: "fish:runtime_failure", CWDObserved: "/tmp/project", PrivacyState: "standard"}
	final := contextexec.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: expectation.CWDObserved, PrivacyState: expectation.PrivacyState}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_failure", Generation: "helper_failure", RequestFingerprint: fp, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	state := ContextExecState{SchemaVersion: ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp, Expectation: expectation, Context: &final, BoundaryObservedAt: at.Add(time.Second), Lifecycle: contextexec.LifecycleHelperAuthenticated, Helper: &helper, CreatedAt: at, UpdatedAt: at.Add(time.Second)}
	if err := state.Validate(); err != nil {
		t.Fatal(err)
	}
	return state
}
