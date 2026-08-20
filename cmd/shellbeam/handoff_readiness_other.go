//go:build !darwin && !linux

package main

import (
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	handoffapp "github.com/maemreyo/shellbeam/internal/app/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func composeDelegatedHandoffReadiness(daemonapp.DelegatedRuntime, string) handoffapp.ReadinessPreparer {
	return nil
}

func supportedHandoffShellIntegrations() []capability.ShellIntegrationSupport { return nil }
