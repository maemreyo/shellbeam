//go:build darwin

package main

import (
	"strings"
	"testing"
	"time"

	terminaladapter "github.com/maemreyo/shellbeam/internal/adapter/terminalpresentation"
	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func TestDoctorDarwinTerminalPresentationCheckReportsRunningAndNotRunning(t *testing.T) {
	providers := terminaladapter.QualifiedIdentities()
	available := doctorDarwinTerminalPresentationCheck(t.Context(), writeTerminalLSAppInfoFixture(t, true), providers, time.Second)
	if available.Status != control.Pass || !strings.Contains(available.Hint, "ghostty:available") {
		t.Fatalf("available check=%+v", available)
	}

	unavailable := doctorDarwinTerminalPresentationCheck(t.Context(), writeTerminalLSAppInfoFixture(t, false), providers, time.Second)
	if unavailable.Status != control.Warn || !strings.Contains(unavailable.Hint, "ghostty:unavailable(reason=not_running)") {
		t.Fatalf("unavailable check=%+v", unavailable)
	}
}
