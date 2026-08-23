//go:build !darwin

package main

import (
	"context"

	control "github.com/maemreyo/shellbeam/internal/app/control"
	terminalapp "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func composeHostTerminalPresentationRuntime(_ context.Context, catalog capability.Catalog, _ terminalapp.TerminalLaunchStore) terminalPresentationRuntime {
	return terminalPresentationRuntime{Catalog: catalog}
}

func doctorHostTerminalPresentationCheck(context.Context) control.Check {
	return doctorTerminalPresentationCheck(terminalPresentationDiagnostics{FailureReason: terminalProviderPlatformUnsupported})
}
