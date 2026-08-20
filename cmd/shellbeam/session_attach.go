package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

const (
	h2LocalWarning = "Model-visible output remains public; do not enter secrets here. Secret handoff is unavailable until the privacy capability is present."
	h4LocalWarning = "Secret/private handoff active: model-visible capture is paused for this private interval; ShellBeam does not persist human input."
)

type handoffLocalCaller interface {
	CallHandoffLocal(context.Context, ipcadapter.HandoffLocalRequest) (ipcadapter.HandoffLocalResponse, error)
}
type sessionHumanProvider interface {
	Identity() delegated.ProviderIdentity
	AttachHuman(context.Context, delegated.ProviderRef, delegatedapp.HumanAttachSpec) (delegatedapp.HumanAttachResult, error)
	WaitWritableHumanControl(context.Context, delegated.ProviderRef, delegatedapp.ProviderClientRef, delegatedapp.HumanControlSpec) (handoff.HumanControlKind, error)
}
type sessionAttachDeps struct {
	caller   handoffLocalCaller
	provider sessionHumanProvider
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	environ  []string
	newID    func() string
}

func runSessionAttach(ctx context.Context, handoffID string, stdin io.Reader, stdout, stderr io.Writer) error {
	_, paths, err := loadCommon("session attach", nil)
	if err != nil {
		return err
	}
	runtime, err := newQualifiedDelegatedProvider(paths.StateDir, paths.RuntimeDir)
	if err != nil {
		return err
	}
	provider, ok := runtime.(sessionHumanProvider)
	if !ok {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "local_handoff"}, nil)
	}
	deps := sessionAttachDeps{caller: ipcadapter.NewClient(paths.Socket), provider: provider, stdin: stdin, stdout: stdout, stderr: stderr, environ: os.Environ(), newID: newLocalHandoffID}
	return runSessionAttachWith(ctx, handoffID, deps)
}

func runSessionAttachWith(ctx context.Context, handoffID string, deps sessionAttachDeps) error {
	if deps.caller == nil || deps.provider == nil || deps.stdin == nil || deps.stdout == nil || deps.stderr == nil || deps.newID == nil || !validSessionHandoffID(handoffID) {
		return failure.New(failure.InvalidInput, map[string]string{"field": "session_attach"}, nil)
	}
	return runSessionAttachCycle(ctx, handoffID, deps)
}

func runSessionAttachCycle(ctx context.Context, handoffID string, deps sessionAttachDeps) error {
	bootResp, err := callLocalExact(ctx, deps.caller, ipcadapter.HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: localRequestID("bootstrap", deps.newID()), Action: ipcadapter.HandoffLocalBootstrap, HandoffID: handoffID})
	if err != nil {
		return err
	}
	if bootResp.Bootstrap == nil {
		return failure.New(failure.InvalidDaemonResponse, nil, nil)
	}
	boot := *bootResp.Bootstrap
	if err := validateSessionAttachBootstrap(handoffID, boot, deps.provider.Identity()); err != nil {
		return err
	}
	fmt.Fprintln(deps.stderr, sessionAttachPrivacyWarning(boot.State))
	attach, err := deps.provider.AttachHuman(ctx, boot.ProviderRef, delegatedapp.HumanAttachSpec{Stdin: deps.stdin, Stdout: deps.stdout, Stderr: deps.stderr, Environment: localAttachEnvironment(deps.environ)})
	if err != nil {
		return err
	}
	bindResp, err := callLocalExact(ctx, deps.caller, ipcadapter.HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: localRequestID("bind", deps.newID()), Action: ipcadapter.HandoffLocalBind, HandoffID: handoffID, ClientRef: attach.ClientRef.Ref})
	if err != nil {
		return err
	}
	if bindResp.State == nil {
		return failure.New(failure.InvalidDaemonResponse, nil, nil)
	}
	state := *bindResp.State
	fmt.Fprintln(deps.stderr, "Controls: F10 status, F11 abort, F12 ready")
	for {
		spec := delegatedapp.HumanControlSpec{HandoffID: handoffID, AuthorityEpoch: state.AuthorityEpoch}
		kind, err := deps.provider.WaitWritableHumanControl(ctx, boot.ProviderRef, attach.ClientRef, spec)
		if err != nil {
			return err
		}
		controlResp, err := sendLocalControl(ctx, deps, state, kind)
		if err != nil {
			return err
		}
		if controlResp.Control == nil {
			return failure.New(failure.InvalidDaemonResponse, nil, nil)
		}
		state = controlResp.Control.State
		if kind == handoff.HumanControlStatus {
			fmt.Fprintf(deps.stderr, "Handoff status: %s\n", handoff.ProjectStatus(state))
			continue
		}
		fmt.Fprintln(deps.stderr, "Press F12 to detach from the read-only session.")
		if err := waitLocalAttachDone(ctx, attach.Done); err != nil {
			return err
		}
		if kind == handoff.HumanControlReady {
			return nil
		}
		if kind == handoff.HumanControlAbort {
			return runDetachedLocalControls(ctx, handoffID, state, deps)
		}
	}
}

func runDetachedLocalControls(ctx context.Context, handoffID string, state handoff.State, deps sessionAttachDeps) error {
	for {
		fmt.Fprint(deps.stderr, "Local control [resume|terminate|status]> ")
		line, err := readLocalControlLine(deps.stdin)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var kind handoff.HumanControlKind
		switch strings.TrimSpace(line) {
		case "resume":
			kind = handoff.HumanControlResume
		case "terminate":
			kind = handoff.HumanControlTerminate
		case "status":
			kind = handoff.HumanControlStatus
		default:
			fmt.Fprintln(deps.stderr, "Expected resume, terminate, or status.")
			continue
		}
		resp, err := sendLocalControl(ctx, deps, state, kind)
		if err != nil {
			return err
		}
		if resp.Control == nil {
			return failure.New(failure.InvalidDaemonResponse, nil, nil)
		}
		state = resp.Control.State
		switch kind {
		case handoff.HumanControlStatus:
			fmt.Fprintf(deps.stderr, "Handoff status: %s\n", handoff.ProjectStatus(state))
		case handoff.HumanControlTerminate:
			return nil
		case handoff.HumanControlResume:
			return runSessionAttachCycle(ctx, handoffID, deps)
		}
	}
}

func sessionAttachPrivacyWarning(state handoff.State) string {
	if state.PrivacyState == handoff.PrivacyPrivate && state.CaptureState == handoff.CapturePrivate {
		return h4LocalWarning
	}
	return h2LocalWarning
}

func validateSessionAttachBootstrap(handoffID string, boot handoffapp.LocalBootstrap, identity delegated.ProviderIdentity) error {
	state := boot.State
	if boot.HandoffID != handoffID || state.HandoffID != handoffID {
		return failure.New(failure.InvalidDaemonResponse, map[string]string{"reason": "handoff_identity_mismatch"}, nil)
	}
	if err := state.ValidateH4(); err != nil {
		return failure.New(failure.InvalidDaemonResponse, map[string]string{"reason": "invalid_handoff_state"}, err)
	}
	providerOwnerOK := state.ProviderOwner == delegated.OwnerAgent || state.ProviderOwner == delegated.OwnerNone
	if state.Phase != handoff.PhaseHumanConnecting || state.DesiredOwner != delegated.OwnerHuman || !providerOwnerOK || state.AgentIngress != handoff.IngressFenced || state.HumanIngress != handoff.IngressFenced || state.HumanClient != nil {
		return failure.New(failure.HandoffNotPending, map[string]string{"handoff_id": handoffID, "phase": string(state.Phase)}, nil)
	}
	if err := boot.ProviderRef.Validate(); err != nil || boot.ProviderRef.SessionID != state.SessionID {
		return failure.New(failure.InvalidDaemonResponse, map[string]string{"reason": "invalid_provider_ref"}, err)
	}
	if err := identity.Validate(); err != nil {
		return failure.New(failure.FeatureUnavailable, map[string]string{"feature": "local_handoff_provider"}, err)
	}
	refIdentity := delegated.ProviderIdentity{ID: boot.ProviderRef.ProviderID, Version: boot.ProviderRef.ProviderVersion}
	if refIdentity != identity {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"provider_id": refIdentity.ID, "expected_provider_id": identity.ID}, nil)
	}
	return nil
}

func sendLocalControl(ctx context.Context, deps sessionAttachDeps, state handoff.State, kind handoff.HumanControlKind) (ipcadapter.HandoffLocalResponse, error) {
	id := deps.newID()
	req := ipcadapter.HandoffLocalRequest{LocalVersion: 1, Kind: "request", RequestID: localRequestID("control", id), Action: ipcadapter.HandoffLocalControl, HandoffID: state.HandoffID, AuthorityEpoch: state.AuthorityEpoch, ControlID: "hc_" + string(kind) + "_" + id, ControlKind: kind}
	return callLocalExact(ctx, deps.caller, req)
}

func callLocalExact(ctx context.Context, caller handoffLocalCaller, req ipcadapter.HandoffLocalRequest) (ipcadapter.HandoffLocalResponse, error) {
	resp, err := caller.CallHandoffLocal(ctx, req)
	if err == nil {
		return resp, nil
	}
	var typed *failure.Failure
	if errors.As(err, &typed) {
		return resp, err
	}
	return caller.CallHandoffLocal(ctx, req)
}

func waitLocalAttachDone(ctx context.Context, done <-chan error) error {
	if done == nil {
		return failure.New(failure.InvalidDaemonResponse, nil, nil)
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func readLocalControlLine(r io.Reader) (string, error) {
	var b strings.Builder
	var one [1]byte
	for b.Len() < 64 {
		n, err := r.Read(one[:])
		if n > 0 {
			if one[0] == '\n' {
				return b.String(), nil
			}
			if one[0] != '\r' {
				b.WriteByte(one[0])
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && b.Len() > 0 {
				return b.String(), nil
			}
			return "", err
		}
	}
	return "", failure.New(failure.InvalidInput, map[string]string{"field": "local_control"}, nil)
}
func localRequestID(kind, id string) string { return "hl_" + kind + "_" + id }
func newLocalHandoffID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func localAttachEnvironment(env []string) []string {
	allowed := map[string]bool{"HOME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true, "PATH": true, "TERM": true, "TMPDIR": true}
	out := make([]string, 0, 7)
	hasTerm := false
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !allowed[key] {
			continue
		}
		if key == "TERM" {
			hasTerm = true
		}
		out = append(out, entry)
	}
	if !hasTerm {
		out = append(out, "TERM=xterm-256color")
	}
	return out
}
