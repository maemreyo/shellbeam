package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	checkpointcore "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

type countingCheckpointProvider struct {
	mu      sync.Mutex
	capture int
	restore int
	inspect int
	sweep   int
}

func (*countingCheckpointProvider) Identity() checkpointcore.ProviderIdentity {
	return checkpointcore.ProviderIdentity{ID: "localfs", Version: 1}
}

func (*countingCheckpointProvider) ConflictDetection() checkpointcore.ConflictDetection {
	return checkpointcore.ConflictDetection{
		RegularFile: checkpointcore.ConflictBestEffort, Symlink: checkpointcore.ConflictBestEffort,
		AbsentToFile: checkpointcore.ConflictBestEffort, DirectoryTree: checkpointcore.ConflictUnsupported,
	}
}

func (p *countingCheckpointProvider) Capture(context.Context, checkpointapp.CaptureRequest) (checkpointapp.CaptureResult, error) {
	p.mu.Lock()
	p.capture++
	p.mu.Unlock()
	return checkpointapp.CaptureResult{}, errors.New("unexpected capture")
}
func (p *countingCheckpointProvider) Restore(context.Context, checkpointapp.ProviderRestoreRequest) (checkpointapp.ProviderRestoreResult, error) {
	p.mu.Lock()
	p.restore++
	p.mu.Unlock()
	return checkpointapp.ProviderRestoreResult{}, errors.New("unexpected restore")
}
func (p *countingCheckpointProvider) Inspect(context.Context, string) (checkpointapp.ProviderCheckpointStatus, error) {
	p.mu.Lock()
	p.inspect++
	p.mu.Unlock()
	return checkpointapp.ProviderCheckpointStatus{}, errors.New("unexpected inspect")
}
func (p *countingCheckpointProvider) Sweep(context.Context, checkpointapp.SweepRequest) (checkpointapp.SweepResult, error) {
	p.mu.Lock()
	p.sweep++
	p.mu.Unlock()
	return checkpointapp.SweepResult{}, errors.New("unexpected sweep")
}
func (p *countingCheckpointProvider) counts() (int, int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.capture, p.restore, p.inspect, p.sweep
}

func TestCheckpointDaemonDisabledDoesNotComposeProviderOrPrivateState(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	factoryCalls := 0
	factory := func(string, string) (checkpointapp.Provider, error) {
		factoryCalls++
		return &countingCheckpointProvider{}, nil
	}
	client, cancel, done := runCheckpointTestDaemon(t, stateDir, runtimeDir, false, factory)
	defer stopCheckpointTestDaemon(t, cancel, done)
	server := inspectCheckpointServer(t, client)
	if server.Features[capability.FeatureSafetyCheckpoints] != capability.Unavailable || server.SafetyCheckpoints != nil {
		t.Fatalf("disabled checkpoint capability=%#v", server)
	}
	if factoryCalls != 0 {
		t.Fatalf("disabled checkpoint provider factory calls=%d", factoryCalls)
	}
	assertCheckpointStateAbsent(t, stateDir)
}

func TestCheckpointDaemonEnabledHealthyAdvertisesExactLocalFSV1(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	provider := &countingCheckpointProvider{}
	client, cancel, done := runCheckpointTestDaemon(t, stateDir, runtimeDir, true, func(string, string) (checkpointapp.Provider, error) { return provider, nil })
	defer stopCheckpointTestDaemon(t, cancel, done)
	server := inspectCheckpointServer(t, client)
	if server.Features[capability.FeatureSafetyCheckpoints] != capability.Available || server.SafetyCheckpoints == nil {
		t.Fatalf("enabled checkpoint capability=%#v", server)
	}
	if server.SafetyCheckpoints.Provider != (checkpointcore.ProviderIdentity{ID: "localfs", Version: 1}) ||
		server.SafetyCheckpoints.ConflictDetection.DirectoryTree != checkpointcore.ConflictUnsupported ||
		server.Limits.CheckpointCreateSelectors != checkpointcore.MaxCreateSelectors ||
		server.Limits.CheckpointRestorePaths != checkpointcore.MaxRestorePaths {
		t.Fatalf("checkpoint support/limits=%#v limits=%#v", server.SafetyCheckpoints, server.Limits)
	}
}

func TestCheckpointDaemonProviderFailureKeepsOrdinaryExecutionHealthy(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	client, cancel, done := runCheckpointTestDaemon(t, stateDir, runtimeDir, true, func(string, string) (checkpointapp.Provider, error) {
		return nil, errors.New("provider unhealthy")
	})
	defer stopCheckpointTestDaemon(t, cancel, done)
	server := inspectCheckpointServer(t, client)
	if server.Features[capability.FeatureSafetyCheckpoints] != capability.Unavailable || server.SafetyCheckpoints != nil {
		t.Fatalf("failed provider checkpoint capability=%#v", server)
	}
	result := callA1Terminal(t, client, ipcadapter.RequestV2{Action: "start", OperationID: "e26-provider-failure-start", CWD: t.TempDir(), Command: "true"})
	assertA1ChildSuccess(t, result)
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "e26-provider-failure-inspect", Action: "checkpoint_inspect",
		CheckpointID: "chk_01K00000000000000000000000",
	})
	if err != nil || response.OK || response.Error == nil || response.Error.Code != string(failure.FeatureUnavailable) {
		t.Fatalf("checkpoint unavailable response=%#v err=%v", response, err)
	}
}

func TestCheckpointEnabledButUnusedOrdinaryStartHasZeroProviderAndRepositoryTax(t *testing.T) {
	stateDir, runtimeDir := a1RuntimeDirs(t)
	provider := &countingCheckpointProvider{}
	client, cancel, done := runCheckpointTestDaemon(t, stateDir, runtimeDir, true, func(string, string) (checkpointapp.Provider, error) { return provider, nil })
	defer stopCheckpointTestDaemon(t, cancel, done)
	result := callA1Terminal(t, client, ipcadapter.RequestV2{Action: "start", OperationID: "e26-no-tax-start", CWD: t.TempDir(), Command: "printf no-tax"})
	assertA1ChildSuccess(t, result)
	if capture, restore, inspect, sweep := provider.counts(); capture != 0 || restore != 0 || inspect != 0 || sweep != 0 {
		t.Fatalf("ordinary start touched checkpoint provider capture=%d restore=%d inspect=%d sweep=%d", capture, restore, inspect, sweep)
	}
	assertCheckpointStateAbsent(t, stateDir)
}

func runCheckpointTestDaemon(t *testing.T, stateDir, runtimeDir string, enabled bool, factory checkpointProviderFactory) (*ipcadapter.Client, context.CancelFunc, <-chan error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	contents := "schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_checkpoints = false\n"
	if enabled {
		contents = "schema_version = 1\nmax_concurrent_sessions = 4\nexperimental_checkpoints = true\n"
	}
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	args := []string{"--config", configPath, "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh"}
	go func() { done <- runDaemonWithProviders(ctx, args, nil, nil, factory) }()
	waitForPath(t, filepath.Join(runtimeDir, "daemon.sock"))
	return ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock")), cancel, done
}

func stopCheckpointTestDaemon(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("daemon shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("checkpoint test daemon did not stop")
	}
}

func inspectCheckpointServer(t *testing.T, client *ipcadapter.Client) capability.Catalog {
	t.Helper()
	response, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "e26-server", Action: "inspect.server"})
	if err != nil || !response.OK || response.Server == nil {
		t.Fatalf("inspect.server response=%#v err=%v", response, err)
	}
	return *response.Server
}

func assertCheckpointStateAbsent(t *testing.T, stateDir string) {
	t.Helper()
	for _, path := range []string{filepath.Join(stateDir, "checkpoints"), filepath.Join(stateDir, "checkpoint-content")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("checkpoint state unexpectedly exists path=%s err=%v", path, err)
		}
	}
}
