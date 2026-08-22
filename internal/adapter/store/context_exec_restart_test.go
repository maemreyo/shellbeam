package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	contextexec "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestContextExecRecoveryCandidatesSurviveRestartAndExcludeTerminalStates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r := openRecoveryRepository(t, root)
	active := validContextExecState(t, "ctxexec_recover_active")
	if _, created, got := r.ReserveContextExec(context.Background(), active); got.Err != nil || !created {
		t.Fatalf("active reserve=%v %#v", created, got)
	}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_recover", Generation: "helper_gen_recover", RequestFingerprint: active.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	if _, got := r.AdvanceContextExec(context.Background(), active.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); got.Err != nil {
		t.Fatal(got.Err)
	}

	terminal := validContextExecState(t, "ctxexec_recover_terminal")
	if _, created, got := r.ReserveContextExec(context.Background(), terminal); got.Err != nil || !created {
		t.Fatal(got.Err)
	}
	if _, got := r.AdvanceContextExec(context.Background(), terminal.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &contextexec.HelperBinding{OpaqueLaunchID: "launch_terminal", Generation: "helper_gen_terminal", RequestFingerprint: terminal.RequestFingerprint, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}}); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, got := r.AdvanceContextExec(context.Background(), terminal.Request.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleAmbiguous}); got.Err != nil {
		t.Fatal(got.Err)
	}

	r = openRecoveryRepository(t, root)
	found, ok, err := r.LookupContextExec(context.Background(), active.Request.ContextExecID)
	if err != nil || !ok || found.Lifecycle != contextexec.LifecycleHelperRequested {
		t.Fatalf("found=%#v ok=%v err=%v", found, ok, err)
	}
	candidates, err := r.ListContextExecRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Request.ContextExecID != active.Request.ContextExecID || candidates[0].Lifecycle != contextexec.LifecycleHelperRequested {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func TestContextExecRecoveryCandidatesIncludeCanonicalizedStateWhileExactLeaseRemains(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	r, binding, _, _ := h2HandoffFixture(t, root, "canonical-repair")
	req := contextexec.Request{
		ContextExecID: "ctxexec_canonical_repair", SessionID: binding.SessionID, AuthorityEpoch: binding.AuthorityEpoch,
		Argv: []string{"go", "test", "./..."}, TimeoutMS: 1000, MaxOutputBytes: 4096,
	}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := r.now().UTC()
	state := operation.ContextExecState{
		SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fp,
		Expectation: contextexec.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "gen_repair", ShellIdentity: "fish:runtime_repair", CWDObserved: "/tmp/project", PrivacyState: "standard"},
		Lifecycle:   contextexec.LifecycleReserved, CreatedAt: at, UpdatedAt: at,
	}
	if _, created, result := r.ReserveContextExec(context.Background(), state); result.Err != nil || !created {
		t.Fatalf("reserve created=%v result=%#v", created, result)
	}
	lease, created, result := r.AcquireContextExecLease(context.Background(), operation.SessionID(req.SessionID), req.AuthorityEpoch, req.ContextExecID, fp)
	if result.Err != nil || !created {
		t.Fatalf("lease=%#v created=%v result=%#v", lease, created, result)
	}
	helper := contextexec.HelperBinding{OpaqueLaunchID: "launch_repair", Generation: "helper_repair", RequestFingerprint: fp, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	if _, result := r.AdvanceContextExec(context.Background(), req.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleHelperRequested, Helper: &helper}); result.Err != nil {
		t.Fatal(result.Err)
	}
	boundary := r.now().UTC()
	final := contextexec.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: state.Expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: state.Expectation.CWDObserved, PrivacyState: "standard"}
	if _, result := r.BindHelperGeneration(context.Background(), req.ContextExecID, helper, final, boundary, strings.Repeat("a", 64)); result.Err != nil {
		t.Fatal(result.Err)
	}
	failureResult := contextexec.Result{SchemaVersion: contextexec.SchemaVersion, ContextExecID: req.ContextExecID, RequestFingerprint: fp, Lifecycle: contextexec.LifecycleCanonicalized, Context: final, Helper: &helper, EvidenceQuality: contextexec.EvidenceQualityUnproven, FailureCode: "context_exec_unavailable"}
	if _, result := r.AdvanceContextExec(context.Background(), req.ContextExecID, operation.ContextExecTransition{Lifecycle: contextexec.LifecycleCanonicalized, Result: &failureResult}); result.Err != nil {
		t.Fatal(result.Err)
	}

	r = openRecoveryRepository(t, root)
	foundLease, ok, err := r.FindContextExecLease(context.Background(), operation.SessionID(req.SessionID), req.AuthorityEpoch)
	if err != nil || !ok || foundLease != lease {
		t.Fatalf("lease=%#v ok=%v err=%v", foundLease, ok, err)
	}
	candidates, err := r.ListContextExecRecoveryCandidates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Request.ContextExecID != req.ContextExecID || candidates[0].Lifecycle != contextexec.LifecycleCanonicalized {
		t.Fatalf("candidates=%#v", candidates)
	}
}
