package contextexec

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestLifecycleIsClosedAndOnlyMovesForward(t *testing.T) {
	ordered := []Lifecycle{LifecycleReserved, LifecycleHelperRequested, LifecycleHelperAuthenticated, LifecycleChildSpawned, LifecycleChildTerminal, LifecycleCanonicalized}
	for i, value := range ordered {
		if err := value.Validate(); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if i+1 < len(ordered) && !value.CanAdvanceTo(ordered[i+1]) {
			t.Fatalf("%q cannot advance to %q", value, ordered[i+1])
		}
	}
	for _, terminal := range []Lifecycle{LifecycleCanonicalized, LifecycleHelperLost, LifecycleAmbiguous} {
		if !terminal.Terminal() {
			t.Fatalf("%q is not terminal", terminal)
		}
	}
	if LifecycleChildSpawned.CanAdvanceTo(LifecycleHelperAuthenticated) {
		t.Fatal("lifecycle moved backward")
	}
	if err := Lifecycle("future").Validate(); err == nil {
		t.Fatal("future lifecycle accepted")
	}
}

func validCanonicalResult() Result {
	zero := 0
	return Result{
		SchemaVersion:      SchemaVersion,
		ContextExecID:      "ctxexec_01",
		RequestFingerprint: strings.Repeat("a", 64),
		Lifecycle:          LifecycleCanonicalized,
		Context:            ContextBinding{SessionID: "session_01", AuthorityEpoch: 3, ShellIdentity: "fish:runtime_01", BoundaryQuality: "shell_prompt", CWDObserved: "/tmp/project", PrivacyState: "standard"},
		Helper:             &HelperBinding{OpaqueLaunchID: "launch_01", Generation: "helper_gen_01", RequestFingerprint: strings.Repeat("a", 64), ExecutablePath: "/opt/shellbeam/bin/shellbeam"},
		Executable:         ExecutableIdentity{Requested: "go", ResolvedPath: "/usr/local/bin/go"},
		Spawn:              receipt.SpawnEvidence{Attempted: true, Succeeded: true},
		Exit:               receipt.ExitEvidence{Reaped: true, Code: &zero},
		Output:             OutputEvidence{StdoutBytes: 10, StderrBytes: 2, OutputComplete: true, Attribution: OutputAttributionHelperOwnedChildPipes},
		EvidenceQuality:    EvidenceQualityComplete,
		EvidenceAuthority:  EvidenceAuthorityContextExecChildOwnedV1,
	}
}

func TestCanonicalResultRequiresLiteralHelperOwnedTerminalEvidence(t *testing.T) {
	result := validCanonicalResult()
	if err := result.Validate(); err != nil {
		t.Fatalf("valid result: %v", err)
	}
	cases := map[string]func(*Result){
		"helper":                  func(v *Result) { v.Helper = nil },
		"request fingerprint":     func(v *Result) { v.RequestFingerprint = "bad" },
		"helper request mismatch": func(v *Result) { v.Helper.RequestFingerprint = strings.Repeat("b", 64) },
		"requested executable":    func(v *Result) { v.Executable.Requested = "" },
		"resolved executable":     func(v *Result) { v.Executable.ResolvedPath = "relative/go" },
		"spawn":                   func(v *Result) { v.Spawn.Succeeded = false },
		"reap":                    func(v *Result) { v.Exit.Reaped = false },
		"output attribution":      func(v *Result) { v.Output.Attribution = "pane" },
		"output completeness":     func(v *Result) { v.Output.OutputComplete = false },
		"quality":                 func(v *Result) { v.EvidenceQuality = EvidenceQualityIncomplete },
		"authority":               func(v *Result) { v.EvidenceAuthority = "ordinary" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := validCanonicalResult()
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatalf("invalid canonical result accepted: %#v", bad)
			}
		})
	}
}

func TestAmbiguousOrHelperLostResultCannotClaimMechanicalAuthority(t *testing.T) {
	for _, state := range []Lifecycle{LifecycleAmbiguous, LifecycleHelperLost} {
		result := validCanonicalResult()
		result.Lifecycle = state
		result.EvidenceQuality = EvidenceQualityAmbiguous
		result.EvidenceAuthority = ""
		result.Output.OutputComplete = false
		result.Exit = receipt.ExitEvidence{}
		if err := result.Validate(); err != nil {
			t.Fatalf("%s safe degraded result rejected: %v", state, err)
		}
		result.EvidenceAuthority = EvidenceAuthorityContextExecChildOwnedV1
		if err := result.Validate(); err == nil {
			t.Fatalf("%s claimed mechanical authority", state)
		}
	}
}

func TestCanonicalResultMayKeepChildOwnedAuthorityWithIncompleteOutput(t *testing.T) {
	result := validCanonicalResult()
	result.Output.OutputComplete = false
	result.Output.Truncated = true
	result.EvidenceQuality = EvidenceQualityIncomplete
	if err := result.Validate(); err != nil {
		t.Fatalf("child-owned terminal evidence with explicit output truncation rejected: %v", err)
	}
}

func TestHelperLostBeforeChildSpawnDoesNotInventExecutableOrOutputEvidence(t *testing.T) {
	result := validCanonicalResult()
	result.Lifecycle = LifecycleHelperLost
	result.Executable = ExecutableIdentity{}
	result.Spawn = receipt.SpawnEvidence{Attempted: false, Succeeded: false}
	result.Exit = receipt.ExitEvidence{}
	result.Output = OutputEvidence{}
	result.EvidenceQuality = EvidenceQualityAmbiguous
	result.EvidenceAuthority = ""
	if err := result.Validate(); err != nil {
		t.Fatalf("early helper loss required invented child facts: %v", err)
	}
}
