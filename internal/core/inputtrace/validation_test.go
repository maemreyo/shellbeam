package inputtrace

import (
	"strings"
	"testing"
)

func TestE27CoverageCannotClaimCompleteOwnedTreeWithoutPreExecContract(t *testing.T) {
	matrix := darwinPartialMatrix()
	matrix.FilesystemReads = CoverageCompleteForOwnedTree
	binding := InstrumentationBinding{
		SchemaVersion: SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: ModeBestEffort, Status: BindingActive,
		Provider: ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin",
		InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: EffectEnvironmentAffecting,
		PreExecCoverageEstablished: false, Coverage: matrix,
	}
	if err := binding.Validate(); err == nil {
		t.Fatal("complete_for_owned_tree accepted without pre-exec coverage")
	}
}

func TestE27TruncationCannotClaimCompleteOutcome(t *testing.T) {
	record := validRecordForValidation()
	record.Truncated = true
	record.Outcome = OutcomeComplete
	if err := record.Validate(); err == nil {
		t.Fatal("truncated trace claimed complete")
	}
	record.Outcome = OutcomePartial
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestE27ExternalAndSystemResourcesAreBoundedAndRedacted(t *testing.T) {
	for _, good := range []Resource{
		{ObservationClass: ClassFilesystemReads, PathClass: PathWorkspaceExternalRedacted, Identity: "external-1"},
		{ObservationClass: ClassLoadedLibraries, PathClass: PathSystemClassified, Identity: "system-library"},
	} {
		if err := good.Validate(); err != nil {
			t.Fatalf("valid resource rejected: %v", err)
		}
	}
	for _, bad := range []Resource{
		{ObservationClass: ClassFilesystemReads, PathClass: PathWorkspaceExternalRedacted, Identity: "/Users/alice/secret.txt"},
		{ObservationClass: ClassFilesystemReads, PathClass: PathRepoRelative, Identity: "../secret"},
		{ObservationClass: ClassFilesystemReads, PathClass: PathRepoRelative, Identity: "/absolute"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("unsafe resource accepted: %#v", bad)
		}
	}
}

func TestE27RequestAndInstrumentationFingerprintsAreDeterministic(t *testing.T) {
	if got, err := (Request{}).Fingerprint(); err != nil || got != "" {
		t.Fatalf("off request fingerprint=%q err=%v", got, err)
	}
	a := Request{Mode: ModeBestEffort}
	fa, err := a.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	fb, err := a.Fingerprint()
	if err != nil || fa != fb || len(fa) != 64 {
		t.Fatalf("fingerprints %q %q err=%v", fa, fb, err)
	}
	b := InstrumentationBinding{SchemaVersion: SchemaVersion, TraceID: "trace_01K00000000000000000000000", Mode: ModeBestEffort, Status: BindingActive, Provider: ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin", InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: EffectEnvironmentAffecting, Coverage: darwinPartialMatrix()}
	da, err := b.Digest()
	if err != nil || len(da) != 64 {
		t.Fatalf("digest=%q err=%v", da, err)
	}
	b.InstrumentationFingerprint = strings.Repeat("b", 64)
	db, err := b.Digest()
	if err != nil || da == db {
		t.Fatalf("binding digest did not change")
	}
}

func validRecordForValidation() Record {
	return Record{SchemaVersion: SchemaVersion, DerivationKey: strings.Repeat("b", 64), TraceID: "trace_01K00000000000000000000000", OperationID: "op", SessionID: "session", ReceiptDigest: strings.Repeat("c", 64), Mode: ModeBestEffort, Provider: ProviderIdentity{ID: "dyld-interpose", Version: 1, CapabilityVersion: 1}, Platform: "darwin", InstrumentationFingerprint: strings.Repeat("a", 64), InstrumentationEffect: EffectEnvironmentAffecting, Authority: AuthorityAdvisory, ScopeKind: ScopeObservedInput, MayHaveUnobservedDependencies: true, Coverage: darwinPartialMatrix(), Outcome: OutcomePartial}
}
