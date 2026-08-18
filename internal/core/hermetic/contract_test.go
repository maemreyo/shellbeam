package hermetic

import (
	"strings"
	"testing"
)

func validRequest() Request {
	return Request{
		Version:     RequestVersionV1,
		Mode:        ModeRequired,
		RepoInputs:  []string{"go.mod", "cmd/**", "internal/**"},
		Network:     NetworkOff,
		Environment: EnvironmentFixedAllowlist,
		Stdin:       StdinClosed,
		Writes:      WritesEphemeralDiscard,
	}
}

func TestRequestClonePreservesOmissionAndDeepCopiesInputs(t *testing.T) {
	var omitted *Request
	if got := omitted.Clone(); got != nil {
		t.Fatalf("nil hermetic request cloned as %#v", got)
	}
	original := validRequest()
	clone := original.Clone()
	if clone == nil || len(clone.RepoInputs) != len(original.RepoInputs) {
		t.Fatalf("clone=%#v original=%#v", clone, original)
	}
	clone.RepoInputs[0] = "changed"
	if original.RepoInputs[0] == "changed" {
		t.Fatal("hermetic request clone aliased repo inputs")
	}
}

func TestRequestV1ValidationIsClosedAndBounded(t *testing.T) {
	if err := validRequest().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []Request{
		{},
		func() Request { r := validRequest(); r.Version = 2; return r }(),
		func() Request { r := validRequest(); r.Mode = "optional"; return r }(),
		func() Request { r := validRequest(); r.RepoInputs = nil; return r }(),
		func() Request { r := validRequest(); r.RepoInputs = []string{"../secret"}; return r }(),
		func() Request { r := validRequest(); r.RepoInputs = []string{"/etc/passwd"}; return r }(),
		func() Request { r := validRequest(); r.RepoInputs = []string{"cmd\\**"}; return r }(),
		func() Request { r := validRequest(); r.RepoInputs = []string{"go.mod", "go.mod"}; return r }(),
		func() Request {
			r := validRequest()
			r.RepoInputs = []string{strings.Repeat("a", MaxRepoInputSelectorBytes+1)}
			return r
		}(),
		func() Request { r := validRequest(); r.Network = "allow"; return r }(),
		func() Request { r := validRequest(); r.Environment = "inherit"; return r }(),
		func() Request { r := validRequest(); r.Stdin = "inherit"; return r }(),
		func() Request { r := validRequest(); r.Writes = "host"; return r }(),
	}
	for i, r := range cases {
		if err := r.Validate(); err == nil {
			t.Fatalf("case %d accepted: %#v", i, r)
		}
	}
}

func TestRequestFingerprintTreatsRepoInputsAsASetAndPreservesOmission(t *testing.T) {
	base := strings.Repeat("a", 64)
	if got, err := BindFingerprint("request", base, nil); err != nil || got != base {
		t.Fatalf("omitted binding got=%q err=%v", got, err)
	}
	first := validRequest()
	second := validRequest()
	second.RepoInputs = []string{"internal/**", "go.mod", "cmd/**"}
	got, err := BindFingerprint("request", base, &first)
	if err != nil {
		t.Fatal(err)
	}
	want, err := BindFingerprint("request", base, &second)
	if err != nil {
		t.Fatal(err)
	}
	if got == base || got != want {
		t.Fatalf("set binding got=%s want=%s base=%s", got, want, base)
	}
	changed := validRequest()
	changed.RepoInputs = []string{"go.mod", "cmd/**"}
	other, err := BindFingerprint("request", base, &changed)
	if err != nil {
		t.Fatal(err)
	}
	if other == got {
		t.Fatal("different declared repo scope collided")
	}
}

func TestProviderBoundaryAndProvenScopeContracts(t *testing.T) {
	provider := ProviderIdentity{
		Provider:              ProviderBubblewrap,
		Version:               BubblewrapVersionV1,
		BinarySHA256:          strings.Repeat("a", 64),
		RuntimeManifestSHA256: strings.Repeat("b", 64),
		SecurityPolicyID:      "apparmor:bwrap-userns-restrict",
		SecurityPolicySHA256:  strings.Repeat("c", 64),
	}
	toolchain := ToolchainIdentity{ID: "toolchain_go_1_26_6", ManifestSHA256: strings.Repeat("d", 64)}
	if err := provider.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := toolchain.Validate(); err != nil {
		t.Fatal(err)
	}
	result := BoundaryResult{
		SchemaVersion:      BoundaryResultSchemaV1,
		BoundaryID:         "hb_01K00000000000000000000000",
		Provider:           provider,
		Toolchain:          toolchain,
		EstablishedPreExec: true,
		Continuity:         ContinuityComplete,
	}
	if err := result.Validate(); err != nil || !result.Authoritative() {
		t.Fatalf("result=%#v err=%v authoritative=%v", result, err, result.Authoritative())
	}
	lost := result
	lost.Continuity = ContinuityLost
	if err := lost.Validate(); err != nil || lost.Authoritative() {
		t.Fatalf("lost result err=%v authoritative=%v", err, lost.Authoritative())
	}
	scope := ProvenInputScope{
		SchemaVersion:         ProvenInputScopeSchemaV1,
		RepoInputs:            []string{"cmd/**", "go.mod"},
		CaptureManifestSHA256: strings.Repeat("e", 64),
		CaptureContentSHA256:  strings.Repeat("f", 64),
		Provider:              provider,
		Toolchain:             toolchain,
		Environment:           EnvironmentFixedAllowlist,
		Stdin:                 StdinClosed,
		Network:               NetworkOff,
		AmbientInputs:         []AmbientInputClass{AmbientClock, AmbientRandomness},
	}
	if err := scope.Validate(); err != nil {
		t.Fatal(err)
	}
	noncanonicalScope := scope
	noncanonicalScope.RepoInputs = []string{"go.mod", "cmd/**"}
	if err := noncanonicalScope.Validate(); err == nil {
		t.Fatal("noncanonical proven input scope accepted")
	}
}

func TestRequestRejectsUnboundedGlobAndGitMetadataSelectors(t *testing.T) {
	badSelectors := []string{
		"*.go", "internal/*", "internal/??", "internal/[ab].go", "{a,b}", "internal/**/nested",
		".git", ".git/**", "nested/.git/config", "nested/.git/**",
	}
	for _, selector := range badSelectors {
		req := validRequest()
		req.RepoInputs = []string{selector}
		if err := req.Validate(); err == nil {
			t.Fatalf("accepted unsafe selector %q", selector)
		}
	}
	for _, selector := range []string{"go.mod", "internal/**", ".github/**", "dir with spaces/file.txt"} {
		req := validRequest()
		req.RepoInputs = []string{selector}
		if err := req.Validate(); err != nil {
			t.Fatalf("rejected valid selector %q: %v", selector, err)
		}
	}
}
