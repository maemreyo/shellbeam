package delegatedtmux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
)

type controlWaitResult struct {
	kind handoff.HumanControlKind
	err  error
}

func (p *Provider) ArmWritableHumanControl(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef, spec app.HumanControlSpec) error {
	if err := spec.Validate(); err != nil {
		return failure.New(failure.InvalidInput, map[string]string{"field": "human_control"}, err)
	}
	obs, err := p.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return err
	}
	if obs.ReadOnly || obs.ObservedOwner != core.OwnerHuman {
		return failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "client_not_writable"}, nil)
	}
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return err
	}
	bindings := []struct {
		key  string
		kind handoff.HumanControlKind
	}{{"F10", handoff.HumanControlStatus}, {"F11", handoff.HumanControlAbort}, {"F12", handoff.HumanControlReady}}
	for _, binding := range bindings {
		channel := humanControlChannel(state, client, spec, binding.kind)
		if _, err := p.externalTmux(ctx, state, "bind-key", "-n", binding.key, "wait-for", "-S", channel); err != nil {
			return failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "bind_writable_control"}, err)
		}
	}
	return nil
}

func (p *Provider) WaitWritableHumanControl(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef, spec app.HumanControlSpec) (handoff.HumanControlKind, error) {
	if err := spec.Validate(); err != nil {
		return "", failure.New(failure.InvalidInput, map[string]string{"field": "human_control"}, err)
	}
	obs, err := p.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return "", err
	}
	if obs.ReadOnly || obs.ObservedOwner != core.OwnerHuman {
		return "", failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "client_not_writable"}, nil)
	}
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return "", err
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	kinds := []handoff.HumanControlKind{handoff.HumanControlStatus, handoff.HumanControlAbort, handoff.HumanControlReady}
	results := make(chan controlWaitResult, len(kinds))
	for _, kind := range kinds {
		kind := kind
		channel := humanControlChannel(state, client, spec, kind)
		go func() { results <- controlWaitResult{kind: kind, err: p.waitForHumanControl(waitCtx, state, channel)} }()
	}
	var firstErr error
	for completed := 0; completed < len(kinds); completed++ {
		select {
		case result := <-results:
			if result.err == nil {
				cancel()
				drainControlWaiters(results, len(kinds)-completed-1)
				return result.kind, nil
			}
			if firstErr == nil && !errors.Is(result.err, context.Canceled) {
				firstErr = result.err
			}
		case <-ctx.Done():
			cancel()
			return "", ctx.Err()
		}
	}
	return "", failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "wait_for_control"}, firstErr)
}

func drainControlWaiters(results <-chan controlWaitResult, count int) {
	for i := 0; i < count; i++ {
		<-results
	}
}

func (p *Provider) PrepareReadOnlyLocalControl(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef) error {
	obs, err := p.InspectHumanClient(ctx, ref, client)
	if err != nil {
		return err
	}
	if !obs.ReadOnly {
		return failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "client_still_writable"}, nil)
	}
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return err
	}
	if _, err := p.externalTmux(ctx, state, "bind-key", "-n", "F12", "detach-client"); err != nil {
		return failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "bind_readonly_detach"}, err)
	}
	return nil
}

func (p *Provider) waitForHumanControl(ctx context.Context, state privateState, channel string) error {
	cmd := exec.CommandContext(ctx, p.config.TmuxPath, "-S", state.SocketPath, "-f", "/dev/null", "wait-for", channel)
	cmd.Env = helperEnvironment(p.config.TmuxPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("wait-for: %w: %s", err, string(out))
	}
	return nil
}

func humanControlChannel(state privateState, client app.ProviderClientRef, spec app.HumanControlSpec, kind handoff.HumanControlKind) string {
	logical := state.ProviderGeneration + "\x00" + client.Ref + "\x00" + spec.HandoffID + "\x00" + fmt.Sprint(spec.AuthorityEpoch) + "\x00" + string(kind)
	sum := sha256.Sum256([]byte(logical))
	return "sbhc_" + hex.EncodeToString(sum[:16])
}
