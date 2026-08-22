package contextexec

import (
	"testing"
	"time"
)

func validShellContinuityExpectation() ShellContinuityExpectation {
	return ShellContinuityExpectation{
		SessionID:                "session_ctx",
		AuthorityEpoch:           3,
		ProviderGeneration:       "gen_ctx",
		ShellRuntimeIdentity:     "zsh:runtime_ctx",
		PaneShellPID:             4242,
		PaneShellProcessIdentity: "proc_shell_ctx",
		PaneTTY:                  "/dev/ttys042",
		HelperExecutableIdentity: "/opt/shellbeam/bin/shellbeam",
	}
}

func validShellContinuityProof(e ShellContinuityExpectation) ShellContinuityProof {
	return ShellContinuityProof{
		SessionID:                e.SessionID,
		AuthorityEpoch:           e.AuthorityEpoch,
		ProviderGeneration:       e.ProviderGeneration,
		ShellRuntimeIdentity:     e.ShellRuntimeIdentity,
		PaneShellPID:             e.PaneShellPID,
		PaneShellProcessIdentity: e.PaneShellProcessIdentity,
		PaneTTY:                  e.PaneTTY,
		HelperPID:                4343,
		HelperExecutableIdentity: e.HelperExecutableIdentity,
		ForegroundProven:         true,
		ObservedAt:               time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
	}
}

func TestShellContinuityExpectationValidate(t *testing.T) {
	valid := validShellContinuityExpectation()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid expectation rejected: %v", err)
	}

	tests := map[string]func(*ShellContinuityExpectation){
		"session":          func(v *ShellContinuityExpectation) { v.SessionID = "" },
		"epoch":            func(v *ShellContinuityExpectation) { v.AuthorityEpoch = 0 },
		"generation":       func(v *ShellContinuityExpectation) { v.ProviderGeneration = "" },
		"shell_runtime":    func(v *ShellContinuityExpectation) { v.ShellRuntimeIdentity = "" },
		"pane_pid":         func(v *ShellContinuityExpectation) { v.PaneShellPID = 1 },
		"process_identity": func(v *ShellContinuityExpectation) { v.PaneShellProcessIdentity = "" },
		"pane_tty":         func(v *ShellContinuityExpectation) { v.PaneTTY = "ttys042" },
		"helper_path":      func(v *ShellContinuityExpectation) { v.HelperExecutableIdentity = "shellbeam" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			got := valid
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid expectation accepted: %#v", got)
			}
		})
	}
}

func TestShellContinuityProofValidate(t *testing.T) {
	expectation := validShellContinuityExpectation()
	valid := validShellContinuityProof(expectation)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid proof rejected: %v", err)
	}

	tests := map[string]func(*ShellContinuityProof){
		"helper_pid":  func(v *ShellContinuityProof) { v.HelperPID = 0 },
		"foreground":  func(v *ShellContinuityProof) { v.ForegroundProven = false },
		"observed_at": func(v *ShellContinuityProof) { v.ObservedAt = time.Time{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			got := valid
			mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid proof accepted: %#v", got)
			}
		})
	}
}

func TestShellContinuityProofValidateForExpectation(t *testing.T) {
	expectation := validShellContinuityExpectation()
	proof := validShellContinuityProof(expectation)
	if err := proof.ValidateFor(expectation); err != nil {
		t.Fatalf("matching proof rejected: %v", err)
	}

	tests := map[string]func(*ShellContinuityProof){
		"session":          func(v *ShellContinuityProof) { v.SessionID = "session_other" },
		"epoch":            func(v *ShellContinuityProof) { v.AuthorityEpoch++ },
		"generation":       func(v *ShellContinuityProof) { v.ProviderGeneration = "gen_other" },
		"shell_runtime":    func(v *ShellContinuityProof) { v.ShellRuntimeIdentity = "bash:runtime_ctx" },
		"pane_pid":         func(v *ShellContinuityProof) { v.PaneShellPID++ },
		"process_identity": func(v *ShellContinuityProof) { v.PaneShellProcessIdentity = "proc_shell_other" },
		"pane_tty":         func(v *ShellContinuityProof) { v.PaneTTY = "/dev/ttys043" },
		"helper_path":      func(v *ShellContinuityProof) { v.HelperExecutableIdentity = "/opt/shellbeam/bin/shellbeam-other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			got := proof
			mutate(&got)
			if err := got.ValidateFor(expectation); err == nil {
				t.Fatalf("mismatched proof accepted: %#v", got)
			}
		})
	}
}
