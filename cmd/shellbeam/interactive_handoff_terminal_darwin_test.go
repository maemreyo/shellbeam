//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	terminaladapter "github.com/maemreyo/shellbeam/internal/adapter/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func TestComposeDarwinTerminalPresentationRuntimeAdvertisesOnlyRunningQualifiedProviders(t *testing.T) {
	base := capability.Baseline(capability.Limits{}).WithDelegatedInteractive(capability.DelegatedInteractiveSupport{ProviderID: "tmux_control_mode", ProviderVersion: 1, Platform: "darwin", MaxMutationRecords: 4096}).WithInteractiveHandoff(capability.InteractiveHandoffSupport{ManualStandard: true})
	query := writeTerminalLSAppInfoFixture(t, true)
	runtime := composeDarwinTerminalPresentationRuntime(t.Context(), base, terminalRuntimeStoreFake{}, darwinTerminalPresentationConfig{
		LSAppInfoPath: query, Providers: terminaladapter.QualifiedIdentities(), CommandTimeout: time.Second,
		Executable: func() (string, error) { return "/usr/local/bin/shellbeam", nil }, Now: time.Now,
	})
	if runtime.Catalog.InteractiveHandoff == nil || runtime.Catalog.InteractiveHandoff.TerminalPresentation == nil || runtime.PresenterFactory == nil || runtime.Start == nil {
		t.Fatalf("running qualified provider did not compose H3: %#v", runtime)
	}

	degraded := composeDarwinTerminalPresentationRuntime(t.Context(), base, terminalRuntimeStoreFake{}, darwinTerminalPresentationConfig{
		LSAppInfoPath: writeTerminalLSAppInfoFixture(t, false), Providers: terminaladapter.QualifiedIdentities(), CommandTimeout: time.Second,
		Executable: func() (string, error) { return "/usr/local/bin/shellbeam", nil }, Now: time.Now,
	})
	if degraded.Catalog.InteractiveHandoff == nil || !degraded.Catalog.InteractiveHandoff.ManualStandard || degraded.Catalog.InteractiveHandoff.TerminalPresentation != nil || degraded.PresenterFactory != nil {
		t.Fatalf("non-running provider should degrade to H2: %#v", degraded)
	}
}

func writeTerminalLSAppInfoFixture(t *testing.T, running bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lsappinfo")
	findOutput := ""
	if running {
		findOutput = "echo 'ASN:0x0-0x12345:'"
	}
	script := "#!/bin/sh\ncase \"$1\" in\n  find) " + findOutput + ";;\n  front) echo 'ASN:0x0-0x12345:';;\n  info) echo 'bundleID=\"com.mitchellh.ghostty\"';;\n  listen) exit 0;;\n  *) exit 2;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
