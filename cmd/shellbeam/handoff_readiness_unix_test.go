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

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
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
	preparer := newDelegatedHandoffReadiness(provider, shortTask7RuntimeDir(t), "/bin/echo")
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
