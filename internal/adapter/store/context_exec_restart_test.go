package store

import (
	"context"
	"path/filepath"
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
