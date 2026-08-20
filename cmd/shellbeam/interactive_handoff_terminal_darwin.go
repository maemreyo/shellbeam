//go:build darwin

package main

import (
	"context"
	"os"
	"time"

	terminaladapter "github.com/maemreyo/shellbeam/internal/adapter/terminalpresentation"
	control "github.com/maemreyo/shellbeam/internal/app/control"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	terminalcore "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const (
	terminalLSAppInfoPath        = "/usr/bin/lsappinfo"
	terminalProviderProbeTimeout = time.Second
)

type darwinTerminalPresentationConfig struct {
	LSAppInfoPath  string
	Providers      []terminalcore.TerminalIdentity
	CommandTimeout time.Duration
	Executable     func() (string, error)
	Now            func() time.Time
}

func composeDarwinTerminalPresentationRuntime(ctx context.Context, catalog capability.Catalog, store terminalapp.TerminalLaunchStore, config darwinTerminalPresentationConfig) terminalPresentationRuntime {
	degraded := terminalPresentationRuntime{Catalog: catalog}
	if catalog.Features[capability.FeatureInteractiveHandoff] != capability.Available || catalog.InteractiveHandoff == nil {
		return degraded
	}
	if config.Executable == nil || config.Now == nil || len(config.Providers) == 0 {
		return degraded
	}
	probe, err := terminaladapter.NewRunningSource(terminaladapter.RunningConfig{
		QueryPath:      config.LSAppInfoPath,
		Providers:      config.Providers,
		CommandTimeout: config.CommandTimeout,
	})
	if err != nil {
		return degraded
	}
	running, err := probe.Running(ctx)
	if err != nil || len(running) == 0 {
		return degraded
	}
	activity, err := terminaladapter.NewDarwinActivitySource(terminaladapter.DarwinConfig{
		LSAppInfoPath:  config.LSAppInfoPath,
		Providers:      running,
		CommandTimeout: config.CommandTimeout,
		Now:            config.Now,
	})
	if err != nil {
		return degraded
	}
	runningSource, err := terminaladapter.NewRunningSource(terminaladapter.RunningConfig{
		QueryPath:      config.LSAppInfoPath,
		Providers:      running,
		CommandTimeout: config.CommandTimeout,
	})
	if err != nil {
		return degraded
	}
	executable, err := config.Executable()
	if err != nil {
		return degraded
	}
	launcher := terminaladapter.NewLauncher(string(terminalcore.PlatformDarwin), terminaladapter.ExecLaunchRunner{})
	runtime, err := buildTerminalPresentationRuntime(terminalPresentationRuntimeInput{
		Catalog: catalog, Providers: running, Store: store, Activity: activity, Running: runningSource,
		Launcher: launcher, Executable: executable, Now: config.Now,
	})
	if err != nil {
		return degraded
	}
	return runtime
}

func composeHostTerminalPresentationRuntime(ctx context.Context, catalog capability.Catalog, store terminalapp.TerminalLaunchStore) terminalPresentationRuntime {
	return composeDarwinTerminalPresentationRuntime(ctx, catalog, store, darwinTerminalPresentationConfig{
		LSAppInfoPath:  terminalLSAppInfoPath,
		Providers:      terminaladapter.QualifiedIdentities(),
		CommandTimeout: terminalProviderProbeTimeout,
		Executable:     os.Executable,
		Now:            func() time.Time { return time.Now().UTC() },
	})
}

func doctorHostTerminalPresentationCheck(ctx context.Context) control.Check {
	return doctorDarwinTerminalPresentationCheck(ctx, terminalLSAppInfoPath, terminaladapter.QualifiedIdentities(), terminalProviderProbeTimeout)
}

func doctorDarwinTerminalPresentationCheck(ctx context.Context, queryPath string, providers []terminalcore.TerminalIdentity, timeout time.Duration) control.Check {
	diagnostics := terminalPresentationDiagnostics{Providers: make([]terminalProviderDiagnostic, 0, len(providers))}
	if len(providers) == 0 {
		diagnostics.FailureReason = terminalProviderProbeFailed
		return doctorTerminalPresentationCheck(diagnostics)
	}
	source, err := terminaladapter.NewRunningSource(terminaladapter.RunningConfig{
		QueryPath: queryPath, Providers: providers, CommandTimeout: timeout,
	})
	if err != nil {
		for _, provider := range providers {
			diagnostics.Providers = append(diagnostics.Providers, terminalProviderDiagnostic{ProviderID: provider.ProviderID, FailureReason: terminalProviderProbeFailed})
		}
		return doctorTerminalPresentationCheck(diagnostics)
	}
	running, err := source.Running(ctx)
	if err != nil {
		for _, provider := range providers {
			diagnostics.Providers = append(diagnostics.Providers, terminalProviderDiagnostic{ProviderID: provider.ProviderID, FailureReason: terminalProviderProbeFailed})
		}
		return doctorTerminalPresentationCheck(diagnostics)
	}
	runningSet := make(map[string]struct{}, len(running))
	for _, provider := range running {
		runningSet[provider.StableKey()] = struct{}{}
	}
	for _, provider := range providers {
		_, available := runningSet[provider.StableKey()]
		diagnostic := terminalProviderDiagnostic{ProviderID: provider.ProviderID, Available: available}
		if !available {
			diagnostic.FailureReason = terminalProviderNotRunning
		}
		diagnostics.Providers = append(diagnostics.Providers, diagnostic)
	}
	return doctorTerminalPresentationCheck(diagnostics)
}
