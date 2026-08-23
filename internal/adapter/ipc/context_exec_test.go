//go:build linux || darwin

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"

	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func TestContextExecIPCV2DecodeAcceptsExactArgvOnlyRequest(t *testing.T) {
	raw := `{"ipc_version":2,"kind":"request","request_id":"ctxexec-ipc","action":"context.exec","context_exec_id":"ctxexec_public_01","session_id":"session_public_01","authority_epoch":4,"argv":["go","test","./..."],"timeout_ms":30000,"max_output_bytes":1048576}`
	got, err := decodeRequestV2(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("valid context.exec rejected: %v", err)
	}
	if got.Action != "context.exec" || got.SessionID != "session_public_01" || got.AuthorityEpoch != 4 || len(got.Argv) != 3 || got.TimeoutMS != 30000 || got.MaxOutputBytes != 1048576 {
		t.Fatalf("decoded request=%#v", got)
	}
}

func TestContextExecIPCV2DecodeRejectsCrossActionFields(t *testing.T) {
	for _, field := range []string{`"command":"echo nope"`, `"cwd":"/tmp"`, `"tty":true`, `"persistent":true`, `"stdin_mode":"closed"`, `"session_mode":"delegated_interactive"`} {
		raw := `{"ipc_version":2,"kind":"request","request_id":"ctxexec-ipc","action":"context.exec","context_exec_id":"ctxexec_public_01","session_id":"session_public_01","authority_epoch":4,"argv":["go"],"timeout_ms":30000,"max_output_bytes":1048576,` + field + `}`
		if _, err := decodeRequestV2(strings.NewReader(raw)); !errors.Is(err, failure.InvalidInput) {
			t.Fatalf("forbidden field accepted: %s err=%v", field, err)
		}
	}
}

type contextExecDispatchActions struct {
	fakeActions
	calls int
	got   contextcore.Request
	state operation.ContextExecState
}

func (a *contextExecDispatchActions) ExecuteContext(_ context.Context, req contextcore.Request) (operation.ContextExecState, error) {
	a.calls++
	a.got = req.Clone()
	return a.state.Clone(), nil
}

func TestContextExecIPCV2DispatchesExactRequestAndProjectsNoPrivateState(t *testing.T) {
	req := contextcore.Request{ContextExecID: "ctxexec_public_01", SessionID: "session_public_01", AuthorityEpoch: 4, Argv: []string{"go", "test"}, TimeoutMS: 30000, MaxOutputBytes: 1048576}
	fingerprint, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 22, 8, 0, 0, 0, time.UTC)
	state := operation.ContextExecState{
		SchemaVersion:      operation.ContextExecStateSchemaVersion,
		Request:            req,
		RequestFingerprint: fingerprint,
		Expectation: contextcore.ContextExpectation{
			SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch,
			ProviderGeneration: "private_provider_generation", ShellIdentity: "zsh:private_shell_identity",
			CWDObserved: "/private/context/path", PrivacyState: "standard",
		},
		Lifecycle: contextcore.LifecycleHelperRequested,
		Helper: &contextcore.HelperBinding{
			OpaqueLaunchID: "private_launch_id", Generation: "private_helper_generation",
			RequestFingerprint: fingerprint, ExecutablePath: "/private/helper/shellbeam",
		},
		CreatedAt: at, UpdatedAt: at,
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}
	actions := &contextExecDispatchActions{state: state}
	server := &Server{actions: actions}
	resp := ResponseV2{IPVersion: 2, Kind: "response", RequestID: "ctxexec-dispatch", Action: "context.exec"}
	err = server.dispatchV2(t.Context(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "ctxexec-dispatch", Action: "context.exec",
		ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch,
		Argv: append([]string(nil), req.Argv...), TimeoutMS: req.TimeoutMS, MaxOutputBytes: int(req.MaxOutputBytes),
	}, &resp)
	if err != nil {
		t.Fatal(err)
	}
	if actions.calls != 1 || actions.got.ContextExecID != req.ContextExecID || actions.got.SessionID != req.SessionID || actions.got.AuthorityEpoch != req.AuthorityEpoch || strings.Join(actions.got.Argv, "\x00") != strings.Join(req.Argv, "\x00") {
		t.Fatalf("calls=%d request=%#v", actions.calls, actions.got)
	}
	wire, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(wire)
	if !strings.Contains(text, `"context_exec"`) || !strings.Contains(text, req.ContextExecID) || !strings.Contains(text, `"lifecycle":"helper_requested"`) {
		t.Fatalf("missing public context projection: %s", text)
	}
	for _, forbidden := range []string{"private_provider_generation", "private_shell_identity", "/private/context/path", "private_launch_id", "private_helper_generation", "/private/helper/shellbeam", fingerprint, `"request_fingerprint":`, `"expectation":`, `"helper":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("context.exec response leaked %q: %s", forbidden, text)
		}
	}
}

type canonicalContextExecActions struct {
	*contextExecDispatchActions
	pollCalls int
	view      app.View
}

func (a *canonicalContextExecActions) Poll(_ context.Context, req app.PollRequest) (app.View, error) {
	a.pollCalls++
	if req.SessionID != string(a.state.ChildSessionID) || req.Cursor != 0 || req.MaxOutputBytes != int(a.state.Request.MaxOutputBytes) {
		return app.View{}, fmt.Errorf("unexpected context output poll: %#v", req)
	}
	return a.view, nil
}

func TestContextExecIPCV2CanonicalReplayReturnsBoundedChildOutputWithoutRawReceipt(t *testing.T) {
	req := contextcore.Request{ContextExecID: "ctxexec_public_output", SessionID: "session_public_output", AuthorityEpoch: 4, Argv: []string{"printf", "ok\\n"}, TimeoutMS: 30000, MaxOutputBytes: 1048576}
	fingerprint, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 22, 8, 30, 0, 0, time.UTC)
	expectation := contextcore.ContextExpectation{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ProviderGeneration: "private_generation_output", ShellIdentity: "zsh:private_runtime_output", CWDObserved: "/private/output/cwd", PrivacyState: "standard"}
	finalContext := contextcore.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: expectation.ShellIdentity, BoundaryQuality: "shell_prompt", CWDObserved: expectation.CWDObserved, PrivacyState: "standard"}
	helper := contextcore.HelperBinding{OpaqueLaunchID: "private_launch_output", Generation: "private_helper_output", RequestFingerprint: fingerprint, ExecutablePath: "/private/helper/shellbeam"}
	childOp, childSession, err := operation.DeriveContextChildIDs(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	result := contextcore.Result{
		SchemaVersion: contextcore.SchemaVersion, ContextExecID: req.ContextExecID, RequestFingerprint: fingerprint,
		Lifecycle: contextcore.LifecycleCanonicalized, Context: finalContext, Helper: &helper,
		Executable: contextcore.ExecutableIdentity{Requested: req.Argv[0], ResolvedPath: "/usr/bin/printf"},
		Spawn:      receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero},
		Output:          contextcore.OutputEvidence{StdoutBytes: 3, StderrBytes: 0, OutputComplete: true, Attribution: contextcore.OutputAttributionHelperOwnedChildPipes},
		EvidenceQuality: contextcore.EvidenceQualityComplete, EvidenceAuthority: contextcore.EvidenceAuthorityContextExecChildOwnedV1,
	}
	state := operation.ContextExecState{
		SchemaVersion: operation.ContextExecStateSchemaVersion, Request: req, RequestFingerprint: fingerprint,
		Expectation: expectation, Context: &finalContext, BoundaryObservedAt: at.Add(time.Second), Lifecycle: contextcore.LifecycleCanonicalized,
		Helper: &helper, ChildOperationID: childOp, ChildSessionID: childSession, ExecutionAuthorized: true, Result: &result,
		CreatedAt: at, UpdatedAt: at.Add(2 * time.Second),
	}
	if err := state.Validate(); err != nil {
		t.Fatalf("fixture invalid: %v", err)
	}
	base := &contextExecDispatchActions{state: state}
	actions := &canonicalContextExecActions{contextExecDispatchActions: base, view: app.View{
		OperationID: string(childOp), SessionID: string(childSession), State: session.Completed, Outcome: session.Success,
		Output: "ok\n", Cursor: 0, NextCursor: 3, RawOutputBytes: 3,
	}}
	server := &Server{actions: actions}
	resp := ResponseV2{IPVersion: 2, Kind: "response", RequestID: "ctxexec-output", Action: "context.exec"}
	err = server.dispatchV2(t.Context(), RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "ctxexec-output", Action: "context.exec",
		ContextExecID: req.ContextExecID, SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch,
		Argv: append([]string(nil), req.Argv...), TimeoutMS: req.TimeoutMS, MaxOutputBytes: int(req.MaxOutputBytes),
	}, &resp)
	if err != nil {
		t.Fatal(err)
	}
	if actions.pollCalls != 1 || resp.ContextExec == nil || resp.ContextExec.Output == nil {
		t.Fatalf("polls=%d response=%#v", actions.pollCalls, resp.ContextExec)
	}
	wire, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(wire)
	for _, required := range []string{`"preview":"ok\n"`, `"raw_bytes":3`, `"returned_bytes":3`, `"output_complete":true`, `"evidence_authority":"context_exec_child_owned_v1"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing %s in %s", required, text)
		}
	}
	for _, forbidden := range []string{"private_generation_output", "private_runtime_output", "/private/output/cwd", "private_launch_output", "private_helper_output", "/private/helper/shellbeam", fingerprint, `"receipt":`, `"request_fingerprint":`, `"execution_fingerprint":`, `"cwd":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("canonical public output leaked %q: %s", forbidden, text)
		}
	}
}
