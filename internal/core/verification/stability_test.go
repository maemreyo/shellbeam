package verification

import (
	"strings"
	"testing"
	"time"

	evidence "github.com/maemreyo/shellbeam/internal/core/evidence"
)

func compatibilityCandidate() EvidenceCandidate {
	candidate := validEvidenceCandidate()
	candidate.ProviderClass = ProviderProjectCommand
	candidate.ProviderClassKnown = true
	candidate.ProjectCommandID = "test_package"
	candidate.SourceGeneration = "gen_" + strings.Repeat("1", 64)
	candidate.SourceContentDigest = strings.Repeat("2", 64)
	candidate.ProjectBindingDigest = strings.Repeat("3", 64)
	candidate.EnvironmentFingerprint = strings.Repeat("4", 64)
	candidate.EnvironmentFingerprintVersion = 1
	candidate.ToolchainFingerprint = strings.Repeat("5", 64)
	candidate.ToolchainFingerprintVersion = 1
	candidate.Authority = AuthorityMechanical
	candidate.AuthorityKnown = true
	return candidate
}

func TestCompatibilityKeyIncludesMechanicalVerificationSemantics(t *testing.T) {
	base := compatibilityCandidate()
	key, ok := CompatibilityKey(base)
	if !ok || key == "" {
		t.Fatalf("base compatibility key unavailable: key=%q ok=%v", key, ok)
	}
	variants := []EvidenceCandidate{
		func() EvidenceCandidate { v := base; v.SourceGeneration = "gen_" + strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.SourceContentDigest = strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.ProjectBindingDigest = strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.SemanticContractDigest = strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.EnvironmentFingerprint = strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.EnvironmentFingerprintVersion = 2; return v }(),
		func() EvidenceCandidate { v := base; v.ToolchainFingerprint = strings.Repeat("6", 64); return v }(),
		func() EvidenceCandidate { v := base; v.ToolchainFingerprintVersion = 2; return v }(),
		func() EvidenceCandidate { v := base; v.ProjectCommandID = "test_other"; return v }(),
	}
	for i, variant := range variants {
		got, ok := CompatibilityKey(variant)
		if !ok {
			t.Fatalf("variant %d unexpectedly unavailable", i)
		}
		if got == key {
			t.Fatalf("variant %d collided with base compatibility key", i)
		}
	}
}

func TestCompatibilityKeyExcludesDisplayOrderResultAndRerunReason(t *testing.T) {
	base := compatibilityCandidate()
	key, ok := CompatibilityKey(base)
	if !ok {
		t.Fatal("base compatibility unavailable")
	}
	variant := base
	variant.EvidenceID = "ev_" + strings.Repeat("9", 64)
	variant.OperationID = "different-operation"
	variant.SessionID = "different-session"
	variant.ActivityID = "different-activity"
	variant.CompletedAt = time.Unix(999, 0).UTC()
	variant.Result = CandidateFail
	variant.Attempt = &evidence.VerificationAttemptIntent{RerunOfEvidenceID: base.EvidenceID, RerunReason: evidence.RerunDiagnoseFlake}
	got, ok := CompatibilityKey(variant)
	if !ok || got != key {
		t.Fatalf("display/result/rerun metadata changed compatibility: got=%q want=%q ok=%v", got, key, ok)
	}
	variant.Attempt.RerunReason = evidence.RerunFlakeQualification
	got, ok = CompatibilityKey(variant)
	if !ok || got != key {
		t.Fatalf("flake qualification reason changed compatibility: got=%q want=%q ok=%v", got, key, ok)
	}
}

func TestCompatibilityKeyFailsClosedWhenRequiredFactsAreUnavailable(t *testing.T) {
	base := compatibilityCandidate()
	cases := map[string]func(*EvidenceCandidate){
		"provider": func(v *EvidenceCandidate) {
			v.ProviderClassKnown = false
			v.ProviderClass = ""
			v.AuthorityKnown = false
			v.Authority = ""
		},
		"generation":        func(v *EvidenceCandidate) { v.SourceGeneration = "" },
		"semantic contract": func(v *EvidenceCandidate) { v.SemanticContractDigest = "" },
		"typed command":     func(v *EvidenceCandidate) { v.ProjectCommandID = "" },
		"typed binding":     func(v *EvidenceCandidate) { v.ProjectBindingDigest = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if key, ok := CompatibilityKey(candidate); ok || key != "" {
				t.Fatalf("unsafe compatibility key produced: key=%q ok=%v", key, ok)
			}
		})
	}
}

func TestFlakeProtocolValidationUsesClosedV1RunBounds(t *testing.T) {
	for _, tc := range []struct {
		runs        int
		minPasses   int
		maxFailures int
		ok          bool
	}{{1, 1, 0, false}, {2, 1, 1, true}, {3, 1, 0, true}, {10, 10, 0, true}, {11, 1, 10, false}} {
		p := validPolicy()
		req := &p.Rules[0].Evidence[0]
		req.Stability = StabilityFlakeProtocol
		req.Flake = &FlakeProtocol{Runs: tc.runs, MinPasses: tc.minPasses, MaxFailures: tc.maxFailures}
		err := p.Validate()
		if (err == nil) != tc.ok {
			t.Fatalf("runs=%d validation err=%v want_ok=%v", tc.runs, err, tc.ok)
		}
	}
}
