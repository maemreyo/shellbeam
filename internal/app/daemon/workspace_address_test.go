package daemon_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type fakeAddressResolver struct {
	calls       atomic.Int32
	cwd         string
	logicalCWD  string
	workspaceID workspace.WorkspaceID
	err         error
}

func (r *fakeAddressResolver) resolved() (workspace.ResolvedAddress, error) {
	r.calls.Add(1)
	if r.err != nil {
		return workspace.ResolvedAddress{}, r.err
	}
	workspaceID := r.workspaceID
	if workspaceID == "" {
		workspaceID = workspace.WorkspaceID("ws_01K00000000000000000000000")
	}
	logicalCWD := r.logicalCWD
	if logicalCWD == "" {
		logicalCWD = "src"
	}
	return workspace.ResolvedAddress{WorkspaceID: workspaceID, LogicalCWD: logicalCWD, CWD: r.cwd}, nil
}

func (r *fakeAddressResolver) ResolveAddress(context.Context, workspace.Address) (workspace.ResolvedAddress, error) {
	return r.resolved()
}

func (r *fakeAddressResolver) ResolveAdmissionAddress(context.Context, workspace.Address) (workspace.ResolvedAddress, error) {
	return r.resolved()
}

func TestCWDOnlyAdmissionBindsDurableWorkspaceAndReplayDoesNotResolveAgain(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	workspaceID := workspace.WorkspaceID("ws_01K00000000000000000000007")
	resolver := &fakeAddressResolver{workspaceID: workspaceID, logicalCWD: "src", cwd: "/bound/repo/src"}
	svc := app.NewServiceWithWorkspaceResolver(st, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "op-cwd-auto-bind", Command: "true", CWD: "/caller/repo/src", YieldMS: 100}

	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	terminal := waitForTerminal(t, svc, first.SessionID)
	if resolver.calls.Load() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("resolver_calls=%d starts=%d", resolver.calls.Load(), owner.starts.Load())
	}
	stored, err := st.LoadOperation(context.Background(), operation.ID(req.OperationID))
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkspaceID != string(workspaceID) || stored.LogicalCWD != "src" || stored.CWD != "/bound/repo/src" {
		t.Fatalf("stored=%#v", stored)
	}
	if terminal.WorkspaceID != string(workspaceID) {
		t.Fatalf("terminal=%#v", terminal)
	}

	resolver.err = errors.New("resolver must not run during replay")
	replay, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("replay re-resolved cwd binding: %v", err)
	}
	if replay.SessionID != first.SessionID || resolver.calls.Load() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("replay=%#v resolver_calls=%d starts=%d", replay, resolver.calls.Load(), owner.starts.Load())
	}
}

func TestAgentExecutionA1RetryAfterMoveUsesDurableBindingBeforeWorkspaceResolution(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	resolver := &fakeAddressResolver{cwd: "/bound/original/src"}
	svc := app.NewServiceWithWorkspaceResolver(st, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "op-address-retry", WorkspaceID: "ws_01K00000000000000000000000", Command: "true", CWD: "src", YieldMS: 100}

	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, svc, first.SessionID)

	resolver.err = errors.New("worktree moved or removed")
	replay, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("retry re-resolved workspace: %v", err)
	}
	if replay.SessionID != first.SessionID || resolver.calls.Load() != 1 || owner.starts.Load() != 1 {
		t.Fatalf("first=%#v replay=%#v resolver_calls=%d starts=%d", first, replay, resolver.calls.Load(), owner.starts.Load())
	}
	stored, err := st.LoadOperation(context.Background(), "op-address-retry")
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorkspaceID != req.WorkspaceID || stored.LogicalCWD != "src" || stored.CWD != "/bound/original/src" {
		t.Fatalf("stored=%#v", stored)
	}
	result, err := replay.StructuredResult()
	if err != nil {
		t.Fatal(err)
	}
	if result.Operation.WorkspaceID != req.WorkspaceID {
		t.Fatalf("result operation=%#v", result.Operation)
	}
}

func TestWorkspaceStateFailuresPreserveTypedRecoveryDetailsBeforeReservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   error
		code   failure.Code
		reason string
	}{
		{name: "stale", kind: workspaceapp.ErrWorkspaceStale, code: failure.WorkspaceStale, reason: "root_mismatch"},
		{name: "root_missing", kind: workspaceapp.ErrWorkspaceRootMissing, code: failure.WorkspaceRootMissing, reason: "root_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
			if err != nil {
				t.Fatal(err)
			}
			owner := &fakeOwner{}
			workspaceID := workspace.WorkspaceID("ws_01K00000000000000000000000")
			resolver := &fakeAddressResolver{err: &workspaceapp.WorkspaceStateError{Kind: tc.kind, WorkspaceID: workspaceID, Reason: tc.reason}}
			svc := app.NewServiceWithWorkspaceResolver(st, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
			_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-address-state-" + tc.name, WorkspaceID: string(workspaceID), Command: "true", CWD: "."})
			if !errors.Is(err, tc.code) {
				t.Fatalf("err=%v", err)
			}
			var typed *failure.Failure
			if !errors.As(err, &typed) || typed.Details["workspace_id"] != string(workspaceID) || typed.Details["reason"] != tc.reason {
				t.Fatalf("typed=%#v err=%v", typed, err)
			}
			if owner.starts.Load() != 0 {
				t.Fatalf("starts=%d", owner.starts.Load())
			}
			if _, loadErr := st.LoadOperation(context.Background(), operation.ID("op-address-state-"+tc.name)); loadErr == nil {
				t.Fatal("operation reserved after workspace state resolution failure")
			}
		})
	}
}

func TestWorkspaceAddressResolutionFailureDoesNotReserveOrSpawn(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	owner := &fakeOwner{}
	resolver := &fakeAddressResolver{err: failure.New(failure.WorkspaceNotFound, map[string]string{"workspace_id": "ws_01K00000000000000000000000"}, nil)}
	svc := app.NewServiceWithWorkspaceResolver(st, owner, resolver, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	_, err = svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-address-missing", WorkspaceID: "ws_01K00000000000000000000000", Command: "true", CWD: "."})
	if !errors.Is(err, failure.WorkspaceNotFound) {
		t.Fatalf("err=%v", err)
	}
	if owner.starts.Load() != 0 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
	if _, err := st.LoadOperation(context.Background(), "op-address-missing"); err == nil {
		t.Fatal("operation reserved after resolution failure")
	}
}

func TestWorkspaceHintAdvisoryIsNonBlockingDeduplicatedAndNotFingerprintBound(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	pre := daemonSnapshot(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", workspace.QualityFresh)
	post := pre
	retry := pre
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{pre, post, retry, retry}}
	owner := &fakeOwner{}
	svc := app.NewServiceWithWorkspaceObserver(st, owner, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	req := app.StartRequest{ProtocolVersion: 2, OperationID: "op-hint", Command: "true", CWD: "/repo", WorkspaceHint: &workspace.Hint{Branch: "topic"}}
	first, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Advisories) != 1 || first.Advisories[0].Code != "workspace_hint_mismatch" {
		t.Fatalf("first=%#v", first.Advisories)
	}
	waitForTerminal(t, svc, first.SessionID)
	same, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(same.Advisories) != 0 {
		t.Fatalf("same cause re-emitted=%#v", same.Advisories)
	}
	req.WorkspaceHint = &workspace.Hint{Branch: "develop"}
	changed, err := svc.Start(context.Background(), req)
	if err != nil {
		t.Fatalf("hint changed operation fingerprint: %v", err)
	}
	if len(changed.Advisories) != 1 || changed.Advisories[0].CauseFingerprint == first.Advisories[0].CauseFingerprint {
		t.Fatalf("changed=%#v first=%#v", changed.Advisories, first.Advisories)
	}
	if owner.starts.Load() != 1 {
		t.Fatalf("starts=%d", owner.starts.Load())
	}
}

func TestContextEventEmitsOnceForBranchTransitionAcrossStarts(t *testing.T) {
	st, err := storeadapter.Open(filepath.Join(t.TempDir(), "state"), storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1000, MaxTotalState: 1 << 20, ControlReserve: 100})
	if err != nil {
		t.Fatal(err)
	}
	main := daemonSnapshot(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", workspace.QualityFresh)
	topic := main
	topic.Ref = "refs/heads/topic"
	topic, err = workspace.WithGeneration(topic)
	if err != nil {
		t.Fatal(err)
	}
	observer := &sequenceWorkspaceObserver{snapshots: []workspace.FastSnapshot{main, topic}}
	svc := app.NewServiceWithWorkspaceObserver(st, &fakeOwner{}, observer, app.Options{Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100})
	first, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-context-a", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ContextEvents) != 0 {
		t.Fatalf("first events=%#v", first.ContextEvents)
	}
	waitForTerminal(t, svc, first.SessionID)
	second, err := svc.Start(context.Background(), app.StartRequest{ProtocolVersion: 2, OperationID: "op-context-b", Command: "true", CWD: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ContextEvents) != 1 || second.ContextEvents[0].Code != "branch_changed" {
		t.Fatalf("events=%#v", second.ContextEvents)
	}
	waitForTerminal(t, svc, second.SessionID)
}
