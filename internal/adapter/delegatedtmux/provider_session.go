package delegatedtmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

func (p *Provider) Create(ctx context.Context, req app.CreateRequest) (app.CreateResult, error) {
	if err := p.validateCreate(ctx, req); err != nil {
		return app.CreateResult{}, err
	}
	state, err := p.state.load(req.ProviderRef.Ref)
	if err == nil {
		return p.resumeExistingCreate(ctx, req, state)
	}
	if !os.IsNotExist(err) {
		return app.CreateResult{}, delegatedUnavailable("private_state_read", err)
	}
	return p.createFresh(ctx, req)
}

func (p *Provider) validateCreate(ctx context.Context, req app.CreateRequest) error {
	if err := p.Probe(ctx); err != nil {
		return err
	}
	if err := validateCreateRequest(req); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "delegated_create"}, err)
	}
	return p.validateProviderRef(req.ProviderRef, req.SessionID)
}

func (p *Provider) resumeExistingCreate(ctx context.Context, req app.CreateRequest, state privateState) (app.CreateResult, error) {
	obs, err := p.Reattach(ctx, req.ProviderRef, req.Output)
	if err != nil {
		return app.CreateResult{}, err
	}
	if !state.StartReleased {
		if err := p.releaseAndPersist(ctx, &state); err != nil {
			return app.CreateResult{}, err
		}
		obs, err = p.Inspect(ctx, req.ProviderRef)
	}
	return app.CreateResult{ProviderRef: req.ProviderRef, Observation: obs}, err
}

func (p *Provider) createFresh(ctx context.Context, req app.CreateRequest) (app.CreateResult, error) {
	socketPath, err := p.availableSocket(req)
	if err != nil {
		return app.CreateResult{}, err
	}
	bootstrap, actual := providerSessionNames(req.ProviderRef.Ref)
	if err := p.startBootstrap(ctx, socketPath, bootstrap); err != nil {
		return app.CreateResult{}, err
	}
	cleanupServer := true
	gatePath := filepath.Join(filepath.Dir(socketPath), "start.fifo")
	defer func() {
		if cleanupServer {
			_ = p.killServer(context.Background(), socketPath)
			_ = os.Remove(gatePath)
		}
	}()
	control, err := p.startControl(ctx, socketPath, bootstrap)
	if err != nil {
		return app.CreateResult{}, err
	}
	controlOwned := true
	defer func() {
		if controlOwned {
			_ = control.close()
		}
	}()
	state, err := p.configureFreshSession(ctx, req, control, socketPath, bootstrap, actual, gatePath)
	if err != nil {
		return app.CreateResult{}, err
	}
	if err := p.state.save(state); err != nil {
		return app.CreateResult{}, delegatedUnavailable("private_state_write", err)
	}
	p.mu.Lock()
	p.controls[req.ProviderRef.Ref] = control
	p.mu.Unlock()
	controlOwned = false
	if err := p.releaseAndPersist(ctx, &state); err != nil {
		return app.CreateResult{}, err
	}
	cleanupServer = false
	obs, err := p.Inspect(ctx, req.ProviderRef)
	if err != nil {
		return app.CreateResult{}, err
	}
	return app.CreateResult{ProviderRef: req.ProviderRef, Observation: obs}, nil
}

func (p *Provider) availableSocket(req app.CreateRequest) (string, error) {
	socketPath, err := ensureRuntimeSocket(p.config.RuntimeBase, req.ProviderRef.Ref)
	if err != nil {
		return "", delegatedUnavailable("runtime_socket", err)
	}
	if _, err := os.Lstat(socketPath); err == nil {
		return "", failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": req.SessionID, "provider_id": ProviderID, "reason": "socket_without_private_state"}, nil)
	} else if !os.IsNotExist(err) {
		return "", delegatedUnavailable("socket_stat", err)
	}
	return socketPath, nil
}

func providerSessionNames(ref string) (string, string) {
	suffix := strings.TrimPrefix(ref, "dtmux_")
	return "boot_" + suffix[:16], "sb_" + suffix[:24]
}

func (p *Provider) configureFreshSession(ctx context.Context, req app.CreateRequest, control *controlClient, socketPath, bootstrap, actual, gatePath string) (privateState, error) {
	generation, err := newGeneration()
	if err != nil {
		return privateState{}, err
	}
	if err := p.configureServer(ctx, control, generation); err != nil {
		return privateState{}, err
	}
	environment, err := workloadEnvironment(req.Spec)
	if err != nil {
		return privateState{}, err
	}
	if err := p.installWorkloadEnvironment(ctx, control, environment); err != nil {
		return privateState{}, err
	}
	if err := makePrivateFIFO(gatePath); err != nil {
		return privateState{}, err
	}
	wrapper, err := workloadWrapper(req.Spec, gatePath)
	if err != nil {
		return privateState{}, err
	}
	if err := p.controlCommand(ctx, control, "new-session", "-d", "-s", actual, "-c", req.Spec.CWD, wrapper); err != nil {
		return privateState{}, err
	}
	facts, err := p.queryFacts(ctx, control, actual)
	if err != nil {
		return privateState{}, err
	}
	if err := p.clearWorkloadEnvironment(ctx, control, environment); err != nil {
		return privateState{}, err
	}
	if err := p.controlCommand(ctx, control, "switch-client", "-E", "-t", actual); err != nil {
		return privateState{}, err
	}
	if err := control.setTarget(facts.PaneID, req.Output); err != nil {
		return privateState{}, err
	}
	_ = p.controlCommand(ctx, control, "kill-session", "-t", bootstrap)
	state := privateState{SchemaVersion: privateStateSchemaVersion, Ref: req.ProviderRef.Ref, SessionID: req.SessionID, SocketPath: socketPath, TmuxSession: actual, SessionInternalID: facts.SessionInternalID, WindowID: facts.WindowID, PaneID: facts.PaneID, ProviderGeneration: generation, StartGatePath: gatePath, ServerPID: facts.ServerPID, PanePID: facts.PanePID, TmuxVersion: p.config.ExpectedVersion, TmuxSHA256: p.config.ExpectedSHA256, CreatedAt: req.ProviderRef.CreatedAt, UpdatedAt: req.ProviderRef.CreatedAt}
	return state, nil
}

func (p *Provider) Reattach(ctx context.Context, ref core.ProviderRef, sink app.OutputSink) (app.Observation, error) {
	if err := p.Probe(ctx); err != nil {
		return app.Observation{}, err
	}
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return app.Observation{}, err
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		return app.Observation{}, failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": ref.SessionID, "provider_id": ProviderID, "reason": "private_state_missing"}, err)
	}
	_, privacyActive, err := p.activePrivacy(ref, state)
	if err != nil {
		return app.Observation{}, err
	}
	p.mu.Lock()
	existing := p.controls[ref.Ref]
	p.mu.Unlock()
	if existing != nil && privacyActive && !existing.isPrivateObservation() {
		if err := p.replaceWithPrivateObserver(ctx, ref, state, existing, sink); err != nil {
			return app.Observation{}, err
		}
		p.mu.Lock()
		existing = p.controls[ref.Ref]
		p.mu.Unlock()
	}
	if existing != nil {
		facts, err := p.queryFacts(ctx, existing, state.TmuxSession)
		if err != nil {
			return app.Observation{}, providerLost(ref, "inspect_existing", err)
		}
		if err := p.verifyFacts(ctx, existing, state, facts); err != nil {
			return app.Observation{}, err
		}
		if err := existing.setTarget(state.PaneID, sink); err != nil {
			return app.Observation{}, err
		}
		return p.observationFromFacts(existing, state, facts), nil
	}
	var control *controlClient
	if privacyActive {
		control, err = p.startPrivateControl(ctx, state.SocketPath, state.TmuxSession)
	} else {
		control, err = p.startControl(ctx, state.SocketPath, state.TmuxSession)
	}
	if err != nil {
		return app.Observation{}, providerLost(ref, "control_reattach", err)
	}
	facts, err := p.queryFacts(ctx, control, state.TmuxSession)
	if err != nil {
		_ = control.close()
		return app.Observation{}, providerLost(ref, "inspect_reattach", err)
	}
	if err := p.verifyFacts(ctx, control, state, facts); err != nil {
		_ = control.close()
		return app.Observation{}, err
	}
	if err := control.setTarget(state.PaneID, sink); err != nil {
		_ = control.close()
		return app.Observation{}, err
	}
	p.mu.Lock()
	p.controls[ref.Ref] = control
	p.mu.Unlock()
	if !state.StartReleased {
		if err := p.releaseAndPersist(ctx, &state); err != nil {
			return app.Observation{}, err
		}
	}
	return p.observationFromFacts(control, state, facts), nil
}

func (p *Provider) Write(ctx context.Context, ref core.ProviderRef, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	control, state, obs, err := p.currentControl(ctx, ref)
	if err != nil {
		return err
	}
	if !obs.ProviderCurrent || obs.Owner != core.OwnerAgent {
		return failure.New(failure.SessionControlNotOwned, map[string]string{"session_id": ref.SessionID, "owner": string(obs.Owner), "required_owner": string(core.OwnerAgent)}, nil)
	}
	for start := 0; start < len(data); start += 1024 {
		end := start + 1024
		if end > len(data) {
			end = len(data)
		}
		args := []string{"send-keys", "-H", "-t", state.PaneID}
		for _, b := range data[start:end] {
			args = append(args, fmt.Sprintf("%02x", b))
		}
		if err := p.controlCommand(ctx, control, args...); err != nil {
			return providerLost(ref, "write", err)
		}
	}
	return nil
}

func (p *Provider) Signal(ctx context.Context, ref core.ProviderRef, name string) error {
	_, state, obs, err := p.currentControl(ctx, ref)
	if err != nil {
		return err
	}
	if !obs.ProviderCurrent || obs.Owner != core.OwnerAgent {
		return failure.New(failure.SessionControlNotOwned, map[string]string{"session_id": ref.SessionID, "owner": string(obs.Owner), "required_owner": string(core.OwnerAgent)}, nil)
	}
	if err := signalProcessGroup(state.PanePID, name); err != nil {
		return providerLost(ref, "signal", err)
	}
	return nil
}

func (p *Provider) Inspect(ctx context.Context, ref core.ProviderRef) (app.Observation, error) {
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return app.Observation{}, err
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		return app.Observation{}, providerLost(ref, "private_state", err)
	}
	p.mu.Lock()
	control := p.controls[ref.Ref]
	p.mu.Unlock()
	if control == nil {
		return app.Observation{}, providerLost(ref, "observer_missing", nil)
	}
	facts, err := p.queryFacts(ctx, control, state.TmuxSession)
	if err != nil {
		return app.Observation{}, providerLost(ref, "inspect", err)
	}
	if err := p.verifyFacts(ctx, control, state, facts); err != nil {
		return app.Observation{}, err
	}
	return p.observationFromFacts(control, state, facts), nil
}

func (p *Provider) Wait(ctx context.Context, ref core.ProviderRef) (app.Observation, error) {
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return app.Observation{}, err
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		return app.Observation{}, providerLost(ref, "private_state", err)
	}
	control, facts, err := p.currentWaitFacts(ctx, ref, state, "wait_inspect")
	if err != nil {
		return app.Observation{}, err
	}
	if facts.Terminal {
		if facts.ExitCode != nil {
			return p.observationFromFacts(control, state, facts), nil
		}
		return p.inspectTerminalAfterExitRace(ctx, ref, state)
	}

	watcher, err := newProcessExitWatcher(state.PanePID)
	if err != nil {
		if errors.Is(err, errProcessAlreadyGone) {
			return p.inspectTerminalAfterExitRace(ctx, ref, state)
		}
		return app.Observation{}, providerLost(ref, "wait_register", err)
	}
	defer watcher.Close()
	// Registration must be followed by a fresh proof through the currently
	// installed observer. Privacy may replace the observer while this Wait is
	// blocked, but it never replaces the provider/pane process identity.
	control, facts, err = p.currentWaitFacts(ctx, ref, state, "wait_post_register_inspect")
	if err != nil {
		return app.Observation{}, err
	}
	if facts.Terminal {
		if facts.ExitCode != nil {
			return p.observationFromFacts(control, state, facts), nil
		}
		return p.inspectTerminalAfterExitRace(ctx, ref, state)
	}
	if err := watcher.Wait(ctx); err != nil {
		return app.Observation{}, providerLost(ref, "wait_process_exit", err)
	}
	return p.inspectTerminalAfterExitRace(ctx, ref, state)
}

func (p *Provider) currentWaitFacts(ctx context.Context, ref core.ProviderRef, state privateState, reason string) (*controlClient, tmuxFacts, error) {
	for attempt := 0; attempt < 3; attempt++ {
		p.mu.Lock()
		control := p.controls[ref.Ref]
		p.mu.Unlock()
		if control == nil {
			return nil, tmuxFacts{}, providerLost(ref, "observer_missing", nil)
		}
		facts, queryErr := p.queryFacts(ctx, control, state.TmuxSession)
		var verifyErr error
		if queryErr == nil {
			verifyErr = p.verifyFacts(ctx, control, state, facts)
		}
		if queryErr == nil && verifyErr == nil {
			return control, facts, nil
		}
		p.mu.Lock()
		current := p.controls[ref.Ref]
		p.mu.Unlock()
		if current != nil && current != control {
			continue
		}
		if queryErr != nil {
			return nil, tmuxFacts{}, providerLost(ref, reason, queryErr)
		}
		return nil, tmuxFacts{}, verifyErr
	}
	return nil, tmuxFacts{}, providerLost(ref, reason, errors.New("observer changed during proof"))
}

const (
	terminalProviderSettleBudget   = 100 * time.Millisecond
	terminalProviderSettleInterval = 5 * time.Millisecond
)

func (p *Provider) inspectTerminalAfterExitRace(ctx context.Context, ref core.ProviderRef, state privateState) (app.Observation, error) {
	deadline := time.Now().Add(terminalProviderSettleBudget)
	for {
		control, facts, err := p.currentWaitFacts(ctx, ref, state, "wait_terminal_inspect")
		if err != nil {
			return app.Observation{}, err
		}
		if facts.Terminal && facts.ExitCode != nil {
			return p.observationFromFacts(control, state, facts), nil
		}
		if !time.Now().Before(deadline) {
			return app.Observation{}, failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": ref.SessionID, "provider_id": ProviderID, "reason": "process_exit_without_terminal_provider_state"}, nil)
		}
		timer := time.NewTimer(terminalProviderSettleInterval)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return app.Observation{}, ctx.Err()
		}
	}
}

func (p *Provider) Detach(ctx context.Context, ref core.ProviderRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return err
	}
	if _, err := p.state.load(ref.Ref); err != nil {
		return providerLost(ref, "private_state", err)
	}
	p.mu.Lock()
	control := p.controls[ref.Ref]
	delete(p.controls, ref.Ref)
	p.mu.Unlock()
	if control != nil {
		return control.close()
	}
	return nil
}

func (p *Provider) Close(ctx context.Context, ref core.ProviderRef) error {
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return err
	}
	state, err := p.state.load(ref.Ref)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	p.mu.Lock()
	control := p.controls[ref.Ref]
	delete(p.controls, ref.Ref)
	p.mu.Unlock()
	if control != nil {
		_ = control.close()
	}
	if err == nil {
		if killErr := p.killServer(ctx, state.SocketPath); killErr != nil {
			return killErr
		}
		_ = os.RemoveAll(filepath.Dir(state.SocketPath))
	}
	if privacyErr := p.privacy.remove(ref.Ref); privacyErr != nil {
		return privacyErr
	}
	return p.state.remove(ref.Ref)
}

type tmuxFacts struct {
	SessionInternalID, WindowID, PaneID               string
	PanePID, ServerPID                                int
	Terminal                                          bool
	ExitCode                                          *int
	CurrentCommand, PaneTTY, CWD, SocketPath, Version string
}
