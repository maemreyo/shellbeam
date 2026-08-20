//go:build darwin || linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	shelladapter "github.com/maemreyo/shellbeam/internal/adapter/shellintegration"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

const maxHandoffShellScriptBytes = 64 << 10

type delegatedHandoffReadiness struct {
	provider   daemonapp.DelegatedRuntime
	runtimeDir string
	executable string
}

func composeDelegatedHandoffReadiness(provider daemonapp.DelegatedRuntime, runtimeDir string) handoffapp.ReadinessPreparer {
	if provider == nil {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil
	}
	return newDelegatedHandoffReadiness(provider, runtimeDir, filepath.Clean(executable))
}

func newDelegatedHandoffReadiness(provider daemonapp.DelegatedRuntime, runtimeDir, executable string) *delegatedHandoffReadiness {
	return &delegatedHandoffReadiness{provider: provider, runtimeDir: filepath.Clean(runtimeDir), executable: filepath.Clean(executable)}
}

func (r *delegatedHandoffReadiness) Prepare(ctx context.Context, req handoffapp.ReadinessRequest) (handoffapp.PreparedReadiness, error) {
	if r == nil || r.provider == nil {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("provider_unconfigured", nil)
	}
	if err := req.Validate(); err != nil {
		return handoffapp.PreparedReadiness{}, err
	}
	obs, err := r.provider.Inspect(ctx, req.ProviderRef)
	if err != nil {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("provider_inspect_failed", err)
	}
	identity := r.provider.Identity()
	if obs.Provider != identity || !obs.ProviderCurrent || obs.ProviderGeneration != req.ProviderGeneration || obs.Terminal || obs.PanePID <= 1 || obs.CurrentCommand == "" {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("provider_facts_unproven", nil)
	}
	facts := shellapp.ProviderProcessFacts{
		SessionID: req.SessionID, ProviderID: identity.ID, ProviderVersion: identity.Version,
		ProviderGeneration: obs.ProviderGeneration, PanePID: obs.PanePID, CurrentCommand: obs.CurrentCommand,
	}
	shellObs, err := shelladapter.NewUnixProbe().Probe(ctx, shellapp.ProbeRequest{Facts: facts})
	if err != nil {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("shell_probe_failed", err)
	}
	if !shellObs.AdapterEligible() {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("current_shell_unknown", nil)
	}
	port := &delegatedShellCommandPort{
		provider: r.provider, ref: req.ProviderRef, providerGeneration: req.ProviderGeneration,
		currentCommand: obs.CurrentCommand,
	}
	deps := shelladapter.Dependencies{Executable: r.executable, RuntimeDir: r.runtimeDir, Command: port}
	adapter, err := shellAdapterFor(shellObs.Identity.Family, deps)
	if err != nil {
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("adapter_unavailable", err)
	}
	watchReq := shellapp.WatchRequest{HandoffID: req.HandoffID, AuthorityEpoch: req.AuthorityEpoch, Shell: shellObs.Identity, Requirement: req.Requirement}
	watcher, err := adapter.Install(ctx, watchReq)
	if err != nil {
		if failure.Public(err).Code == failure.ShellIdentityChanged {
			return handoffapp.PreparedReadiness{}, failure.Normalize(err)
		}
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("adapter_install_failed", err)
	}
	prepared := handoffapp.PreparedReadiness{Shell: shellObs.Identity, Watcher: watcher}
	if err := prepared.Validate(); err != nil {
		_ = watcher.Close()
		return handoffapp.PreparedReadiness{}, shellReadinessUnavailable("prepared_readiness_invalid", err)
	}
	return prepared, nil
}

func shellAdapterFor(family shellcore.ShellFamily, deps shelladapter.Dependencies) (shellapp.Adapter, error) {
	switch family {
	case shellcore.ShellFish:
		return shelladapter.NewFishAdapter(deps)
	case shellcore.ShellZsh:
		return shelladapter.NewZshAdapter(deps)
	case shellcore.ShellBash:
		return shelladapter.NewBashAdapter(deps)
	default:
		return nil, fmt.Errorf("unsupported current shell")
	}
}

type delegatedShellCommandPort struct {
	provider           daemonapp.DelegatedRuntime
	ref                delegated.ProviderRef
	providerGeneration string
	currentCommand     string
}

func (p *delegatedShellCommandPort) WriteShell(ctx context.Context, script string) error {
	if p == nil || p.provider == nil || script == "" || len(script) > maxHandoffShellScriptBytes || strings.IndexByte(script, 0) >= 0 {
		return shellReadinessUnavailable("generated_script_invalid", nil)
	}
	obs, err := p.provider.Inspect(ctx, p.ref)
	if err != nil {
		return failure.New(failure.ShellIntegrationLost, map[string]string{"reason": "provider_reinspection_failed"}, err)
	}
	if !obs.ProviderCurrent || obs.ProviderGeneration != p.providerGeneration || obs.Terminal {
		return failure.New(failure.ShellIntegrationLost, map[string]string{"reason": "provider_identity_changed"}, nil)
	}
	if obs.CurrentCommand != p.currentCommand {
		return failure.New(failure.ShellIdentityChanged, map[string]string{"reason": "current_shell_changed"}, nil)
	}
	if err := p.provider.Write(ctx, p.ref, []byte(script+"\n")); err != nil {
		return failure.New(failure.ShellIntegrationLost, map[string]string{"reason": "generated_hook_write_failed"}, err)
	}
	return nil
}

func shellReadinessUnavailable(reason string, cause error) error {
	return failure.New(failure.ShellIntegrationUnavailable, map[string]string{"reason": reason}, cause)
}

var _ handoffapp.ReadinessPreparer = (*delegatedHandoffReadiness)(nil)
var _ shelladapter.CommandPort = (*delegatedShellCommandPort)(nil)
var _ delegatedapp.Provider = (daemonapp.DelegatedRuntime)(nil)
