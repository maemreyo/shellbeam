package hermetic

import (
	"reflect"
	"strings"
	"testing"
)

func TestBoundaryBindingValidatesCanonicalPublicAuthorityIdentity(t *testing.T) {
	binding := boundaryBindingFixture()
	if err := binding.Validate(); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	changed := binding
	changed.Request.RepoInputs = []string{"internal/**", "go.mod"}
	if err := changed.Validate(); err == nil {
		t.Fatal("noncanonical durable request accepted")
	}
	changed = binding
	changed.CaptureManifestSHA256 = ""
	if err := changed.Validate(); err == nil {
		t.Fatal("binding without capture digest accepted")
	}
}

func TestBoundaryResultMustMatchFrozenBindingBeforeAuthority(t *testing.T) {
	binding := boundaryBindingFixture()
	result := BoundaryResult{
		SchemaVersion:      BoundaryResultSchemaV1,
		BoundaryID:         binding.BoundaryID,
		Provider:           binding.Provider,
		Toolchain:          binding.Toolchain,
		EstablishedPreExec: true,
		Continuity:         ContinuityComplete,
	}
	if err := ValidateBoundaryCompletion(binding, result); err != nil {
		t.Fatalf("matching completion rejected: %v", err)
	}
	if !result.Authoritative() {
		t.Fatal("complete established result not authoritative")
	}
	mismatch := result
	mismatch.BoundaryID = "hb_01K00000000000000000000099"
	if err := ValidateBoundaryCompletion(binding, mismatch); err == nil {
		t.Fatal("result for a different frozen boundary accepted")
	}
	lost := result
	lost.Continuity = ContinuityLost
	if err := ValidateBoundaryCompletion(binding, lost); err != nil {
		t.Fatalf("well-formed lost completion rejected: %v", err)
	}
	if lost.Authoritative() {
		t.Fatal("lost boundary remained authoritative")
	}
}

func boundaryBindingFixture() BoundaryBinding {
	return BoundaryBinding{
		SchemaVersion:         BoundaryBindingSchemaV1,
		BoundaryID:            "hb_01K00000000000000000000000",
		Request:               Request{Version: 1, Mode: ModeRequired, RepoInputs: []string{"go.mod", "internal/**"}, Network: NetworkOff, Environment: EnvironmentFixedAllowlist, Stdin: StdinClosed, Writes: WritesEphemeralDiscard},
		CaptureManifestSHA256: repeatDigest('d'),
		CaptureContentSHA256:  repeatDigest('e'),
		Provider:              ProviderIdentity{Provider: ProviderBubblewrap, Version: BubblewrapVersionV1, BinarySHA256: repeatDigest('a'), RuntimeManifestSHA256: repeatDigest('b')},
		Toolchain:             ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: repeatDigest('c')},
	}
}

func repeatDigest(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}

func TestProvenInputScopeRequiresAuthoritativeMatchingCompletion(t *testing.T) {
	binding := boundaryBindingFixture()
	binding.CaptureContentSHA256 = strings.Repeat("e", 64)
	result := BoundaryResult{SchemaVersion: BoundaryResultSchemaV1, BoundaryID: binding.BoundaryID, Provider: binding.Provider, Toolchain: binding.Toolchain, EstablishedPreExec: true, Continuity: ContinuityComplete}
	scope, ok, err := ProvenInputScopeFromCompletion(binding, result)
	if err != nil || !ok {
		t.Fatalf("scope=%#v ok=%v err=%v", scope, ok, err)
	}
	if scope.CaptureManifestSHA256 != binding.CaptureManifestSHA256 || scope.CaptureContentSHA256 != binding.CaptureContentSHA256 || !reflect.DeepEqual(scope.RepoInputs, binding.Request.RepoInputs) {
		t.Fatalf("scope lost capture authority: %#v", scope)
	}
	if scope.Environment != EnvironmentFixedAllowlist || scope.Stdin != StdinClosed || scope.Network != NetworkOff || !reflect.DeepEqual(scope.AmbientInputs, []AmbientInputClass{AmbientClock, AmbientRandomness}) {
		t.Fatalf("scope boundary semantics=%#v", scope)
	}
	lost := result
	lost.Continuity = ContinuityLost
	if _, ok, err := ProvenInputScopeFromCompletion(binding, lost); err != nil || ok {
		t.Fatalf("lost completion promoted ok=%v err=%v", ok, err)
	}
	mismatch := result
	mismatch.BoundaryID = "hb_01K00000000000000000000099"
	if _, ok, err := ProvenInputScopeFromCompletion(binding, mismatch); err == nil || ok {
		t.Fatalf("mismatched completion promoted ok=%v err=%v", ok, err)
	}
}
