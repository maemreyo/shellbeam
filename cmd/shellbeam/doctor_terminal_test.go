package main

import (
	"strings"
	"testing"

	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func TestDoctorTerminalPresentationCheckReportsAvailabilityAndFreshnessWithoutSensitiveState(t *testing.T) {
	check := doctorTerminalPresentationCheck(terminalPresentationDiagnostics{
		Providers: []terminalProviderDiagnostic{
			{ProviderID: "ghostty", Available: true},
		},
	})
	if check.ID != "terminal_presentation" || check.Status != control.Pass {
		t.Fatalf("check=%+v", check)
	}
	for _, want := range []string{
		"providers=ghostty:available",
		"active=5s",
		"recent=2m0s",
		"bridge_affinity=request_bound",
		"single_running=5s",
	} {
		if !strings.Contains(check.Hint, want) {
			t.Fatalf("hint %q missing %q", check.Hint, want)
		}
	}
	for _, forbidden := range []string{
		"/Applications",
		"com.mitchellh.ghostty",
		"pid=",
		"frontmost",
		"usage_history",
	} {
		if strings.Contains(check.Hint, forbidden) || strings.Contains(check.Message, forbidden) {
			t.Fatalf("doctor leaked %q in %+v", forbidden, check)
		}
	}
}

func TestDoctorTerminalPresentationCheckExplainsUnavailableProviderWithoutFailingDoctor(t *testing.T) {
	check := doctorTerminalPresentationCheck(terminalPresentationDiagnostics{
		Providers: []terminalProviderDiagnostic{
			{ProviderID: "ghostty", FailureReason: terminalProviderNotRunning},
		},
	})
	if check.Status != control.Warn {
		t.Fatalf("status=%q want warn: %+v", check.Status, check)
	}
	if !strings.Contains(check.Hint, "ghostty:unavailable(reason=not_running)") {
		t.Fatalf("hint=%q", check.Hint)
	}
}

func TestDoctorTerminalPresentationCheckExplainsUnsupportedPlatform(t *testing.T) {
	check := doctorTerminalPresentationCheck(terminalPresentationDiagnostics{
		FailureReason: terminalProviderPlatformUnsupported,
	})
	if check.Status != control.Warn || !strings.Contains(check.Hint, "reason=platform_unsupported") {
		t.Fatalf("check=%+v", check)
	}
}
