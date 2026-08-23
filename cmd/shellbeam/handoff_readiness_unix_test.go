//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	shelladapter "github.com/maemreyo/shellbeam/internal/adapter/shellintegration"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	shellcore "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type handoffReadinessProvider struct {
	countingDelegatedProvider
	mu          sync.Mutex
	obs         delegatedapp.Observation
	inspectSeq  []delegatedapp.Observation
	inspectCall int
	writes      []string
}

func (p *handoffReadinessProvider) Inspect(context.Context, delegated.ProviderRef) (delegatedapp.Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inspectCall++
	if len(p.inspectSeq) != 0 {
		i := p.inspectCall - 1
		if i >= len(p.inspectSeq) {
			i = len(p.inspectSeq) - 1
		}
		return p.inspectSeq[i], nil
	}
	return p.obs, nil
}

func (p *handoffReadinessProvider) Write(_ context.Context, _ delegated.ProviderRef, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writes = append(p.writes, string(data))
	return nil
}

func (p *handoffReadinessProvider) writeSnapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.writes...)
}

func task7ReadinessProvider(command string) (*handoffReadinessProvider, delegated.ProviderRef) {
	now := time.Now().UTC()
	ref := delegated.ProviderRef{SchemaVersion: delegated.ProviderRefSchemaVersion, SessionID: "session_task7_readiness", ProviderID: "tmux_control_mode", ProviderVersion: 1, Ref: "provider_task7_readiness", CreatedAt: now, UpdatedAt: now}
	p := &handoffReadinessProvider{}
	p.obs = delegatedapp.Observation{Provider: p.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_task7", Owner: delegated.OwnerAgent, PanePID: 1234, CurrentCommand: command}
	return p, ref
}

func shortTask7RuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sb-t7-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Clean(dir)
}

func task7ReadinessRequest(ref delegated.ProviderRef, id string) handoffapp.ReadinessRequest {
	return handoffapp.ReadinessRequest{
		HandoffID: id, SessionID: ref.SessionID, AuthorityEpoch: 2, ProviderRef: ref, ProviderGeneration: "gen_task7",
		Requirement: shellcore.Requirement{Kind: shellcore.RequirementEnvironmentExportedNonempty, Name: "CONTROL_PLANE_API_KEY"},
	}
}

func TestDelegatedHandoffReadinessUsesExactCurrentFishCommand(t *testing.T) {
	provider, ref := task7ReadinessProvider("fish")
	runtimeDir := shortTask7RuntimeDir(t)
	ackTask7ReadinessInstall(t, runtimeDir, provider.obs, ref, "handoff_task7_fish", 2)
	preparer := newDelegatedHandoffReadiness(provider, runtimeDir, "/bin/echo")
	prepared, err := preparer.Prepare(t.Context(), task7ReadinessRequest(ref, "handoff_task7_fish"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Shell.Family != shellcore.ShellFish {
		t.Fatalf("shell=%#v", prepared.Shell)
	}
	writes := provider.writeSnapshot()
	if len(writes) != 1 || !strings.Contains(writes[0], "function __shellbeam_handoff_") || strings.Contains(writes[0], "precmd_functions") || strings.Contains(writes[0], "PROMPT_COMMAND") {
		t.Fatalf("writes=%q", writes)
	}
	_ = prepared.Watcher.Close()
}

func TestDelegatedHandoffReadinessUsesExactCurrentNushellCommand(t *testing.T) {
	provider, ref := task7ReadinessProvider("nu")
	runtimeDir := shortTask7RuntimeDir(t)
	ackTask7ReadinessInstall(t, runtimeDir, provider.obs, ref, "handoff_task9_nushell", 2)
	preparer := newDelegatedHandoffReadiness(provider, runtimeDir, "/bin/echo")
	prepared, err := preparer.Prepare(t.Context(), task7ReadinessRequest(ref, "handoff_task9_nushell"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Shell.Family != shellcore.ShellNushell {
		t.Fatalf("shell=%#v", prepared.Shell)
	}
	writes := provider.writeSnapshot()
	if len(writes) != 1 || !strings.Contains(writes[0], "$env.config.hooks.pre_prompt") || strings.Contains(writes[0], "eval ") || strings.Contains(writes[0], "PROMPT_COMMAND") || strings.Contains(writes[0], "fish_prompt") {
		t.Fatalf("writes=%q", writes)
	}
	_ = prepared.Watcher.Close()
}

func ackTask7ReadinessInstall(t *testing.T, runtimeDir string, obs delegatedapp.Observation, ref delegated.ProviderRef, handoffID string, epoch delegated.AuthorityEpoch) {
	t.Helper()
	facts := shellapp.ProviderProcessFacts{
		SessionID: ref.SessionID, ProviderID: obs.Provider.ID, ProviderVersion: obs.Provider.Version,
		ProviderGeneration: obs.ProviderGeneration, PanePID: obs.PanePID, CurrentCommand: obs.CurrentCommand,
		PaneTTY: obs.PaneTTY, CWD: obs.CWD,
	}
	shellObs, err := shelladapter.NewUnixProbe().Probe(t.Context(), shellapp.ProbeRequest{Facts: facts})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			paths, _ := filepath.Glob(filepath.Join(runtimeDir, ".hn_*.sock"))
			if len(paths) == 1 {
				base := filepath.Base(paths[0])
				hex := strings.TrimSuffix(strings.TrimPrefix(base, ".hn_"), ".sock")
				_ = shelladapter.SendNotification(context.Background(), paths[0], shelladapter.Notification{
					HandoffID: handoffID, AuthorityEpoch: epoch, EventID: "evt_" + hex,
					ShellRuntimeID: shellObs.Identity.RuntimeID, Event: shelladapter.NotificationHookInstalled,
				})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
}

func TestDelegatedHandoffReadinessUnknownCurrentCommandFailsClosed(t *testing.T) {
	provider, ref := task7ReadinessProvider("python3")
	preparer := newDelegatedHandoffReadiness(provider, shortTask7RuntimeDir(t), "/bin/echo")
	_, err := preparer.Prepare(t.Context(), task7ReadinessRequest(ref, "handoff_task7_unknown"))
	if !errors.Is(err, failure.ShellIntegrationUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if len(provider.writeSnapshot()) != 0 {
		t.Fatalf("unknown shell received syntax: %q", provider.writeSnapshot())
	}
}

func TestDelegatedHandoffReadinessShellDriftBeforeInstallWritesNothing(t *testing.T) {
	provider, ref := task7ReadinessProvider("fish")
	first := provider.obs
	second := provider.obs
	second.CurrentCommand = "zsh"
	provider.inspectSeq = []delegatedapp.Observation{first, second}
	preparer := newDelegatedHandoffReadiness(provider, shortTask7RuntimeDir(t), "/bin/echo")
	_, err := preparer.Prepare(t.Context(), task7ReadinessRequest(ref, "handoff_task7_drift"))
	if !errors.Is(err, failure.ShellIdentityChanged) {
		t.Fatalf("err=%v", err)
	}
	if len(provider.writeSnapshot()) != 0 {
		t.Fatalf("drifted shell received syntax: %q", provider.writeSnapshot())
	}
}

var _ daemonapp.DelegatedRuntime = (*handoffReadinessProvider)(nil)

func TestDelegatedHandoffReadinessPanePIDDriftBeforeInstallWritesNothing(t *testing.T) {
	provider, ref := task7ReadinessProvider("fish")
	first := provider.obs
	second := provider.obs
	second.PanePID = first.PanePID + 1000
	provider.inspectSeq = []delegatedapp.Observation{first, second}
	preparer := newDelegatedHandoffReadiness(provider, shortTask7RuntimeDir(t), "/bin/echo")

	_, err := preparer.Prepare(t.Context(), task7ReadinessRequest(ref, "handoff_task5_pid_drift"))
	if !errors.Is(err, failure.ShellIdentityChanged) {
		t.Fatalf("err=%v", err)
	}
	if len(provider.writeSnapshot()) != 0 {
		t.Fatalf("changed shell process received syntax: %q", provider.writeSnapshot())
	}
}
