package structuredresult

import (
	"strings"
	"testing"
)

func TestDerivationKeyIsStableAndLifecycleIsClosed(t *testing.T) {
	ref := RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 120, SHA256: strings.Repeat("a", 64)}
	producer := Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	first, err := DerivationKey([]RawOutputRef{ref}, producer, 1, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	second, err := DerivationKey([]RawOutputRef{ref}, producer, 1, strings.Repeat("b", 64))
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("keys first=%q second=%q err=%v", first, second, err)
	}
	changed, err := DerivationKey([]RawOutputRef{ref}, Producer{AdapterID: "go-test-json", AdapterVersion: 2, CapabilityVersion: 1}, 1, strings.Repeat("b", 64))
	if err != nil || changed == first {
		t.Fatalf("adapter version did not change derivation key")
	}
	pending := Derivation{SchemaVersion: SchemaVersionV1, DerivationKey: first, SourceAuthorityRefs: []StructuredInputRef{RawInputRef(ref)}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("b", 64), Lifecycle: LifecyclePending, Completeness: CompletenessUnavailable}
	if err := pending.Validate(); err != nil {
		t.Fatal(err)
	}
	terminal := pending
	terminal.Lifecycle = LifecycleTerminal
	terminal.ParseOutcome = ParseComplete
	terminal.Completeness = CompletenessComplete
	if err := terminal.Validate(); err != nil {
		t.Fatal(err)
	}
	terminal.ParseOutcome = "unknown"
	if err := terminal.Validate(); err == nil {
		t.Fatal("unknown parse outcome accepted")
	}
}

func TestDerivationRejectsUnboundedSourceRefsAndUnsafeProducerText(t *testing.T) {
	ref := RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 1, SHA256: strings.Repeat("a", 64)}
	refs := make([]RawOutputRef, MaxSourceAuthorityRefs+1)
	for i := range refs {
		refs[i] = ref
	}
	if _, err := DerivationKey(refs, Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}, 1, strings.Repeat("b", 64)); err == nil {
		t.Fatal("unbounded source refs accepted")
	}
	if err := (Producer{AdapterID: "bad\nproducer", AdapterVersion: 1, CapabilityVersion: 1}).Validate(); err == nil {
		t.Fatal("control-bearing producer id accepted")
	}
}

func TestDerivationKeyForRawUnionPreservesLegacyIdentity(t *testing.T) {
	raw := RawOutputRef{SessionID: "session-raw-key", StartByte: 2, EndByte: 9, SHA256: strings.Repeat("a", 64)}
	producer := Producer{AdapterID: "go-test-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("b", 64)
	legacy, err := DerivationKey([]RawOutputRef{raw}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := DerivationKeyForInputs([]StructuredInputRef{{Kind: StructuredInputRawOutput, RawOutput: &raw}}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	if modern != legacy {
		t.Fatalf("raw union changed historical derivation identity: legacy=%s modern=%s", legacy, modern)
	}
}

func TestDerivationKeyForArtifactInputIncludesBlobProvenance(t *testing.T) {
	blob := testArtifactBlobRef()
	producer := Producer{AdapterID: "pytest-junit-xml", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("b", 64)
	first, err := DerivationKeyForInputs([]StructuredInputRef{{Kind: StructuredInputArtifactBlob, ArtifactBlob: &blob}}, producer, 2, config)
	if err != nil {
		t.Fatal(err)
	}
	blob.TerminalCut.ReceiptDigest = strings.Repeat("e", 64)
	changed, err := DerivationKeyForInputs([]StructuredInputRef{{Kind: StructuredInputArtifactBlob, ArtifactBlob: &blob}}, producer, 2, config)
	if err != nil || changed == first {
		t.Fatalf("artifact provenance did not change derivation identity: first=%s changed=%s err=%v", first, changed, err)
	}
}

func TestDerivationV3AcceptsTerminalObservedMetadataWithoutChangingIdentity(t *testing.T) {
	raw := RawOutputRef{SessionID: "session-v3-raw", StartByte: 0, EndByte: 7, SHA256: strings.Repeat("c", 64)}
	producer := Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("d", 64)
	rawKey, err := DerivationKeyForInputs([]StructuredInputRef{RawInputRef(raw)}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	counts := &ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 1, Fail: 1}
	rawDerivation := Derivation{
		SchemaVersion: DerivationSchemaVersionV3, DerivationKey: rawKey, SourceAuthorityRefs: []StructuredInputRef{RawInputRef(raw)},
		Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: LifecycleTerminal, ParseOutcome: ParsePartial, Completeness: CompletenessPartial,
		CompletenessReason: CompletenessReasonPassRecordsElided, ObservedEntries: counts,
	}
	if err := rawDerivation.Validate(); err != nil {
		t.Fatal(err)
	}
	withoutMetadata := rawDerivation
	withoutMetadata.SchemaVersion = SchemaVersion
	withoutMetadata.CompletenessReason = ""
	withoutMetadata.ObservedEntries = nil
	if err := withoutMetadata.Validate(); err != nil {
		t.Fatal(err)
	}
	if rawDerivation.DerivationKey != withoutMetadata.DerivationKey {
		t.Fatalf("terminal metadata changed raw derivation identity: with=%s without=%s", rawDerivation.DerivationKey, withoutMetadata.DerivationKey)
	}

	blob := testArtifactBlobRef()
	artifactKey, err := DerivationKeyForInputs([]StructuredInputRef{ArtifactInputRef(blob)}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	withArtifactMetadata := rawDerivation
	withArtifactMetadata.DerivationKey = artifactKey
	withArtifactMetadata.SourceAuthorityRefs = []StructuredInputRef{ArtifactInputRef(blob)}
	if err := withArtifactMetadata.Validate(); err != nil {
		t.Fatal(err)
	}
	withoutArtifactMetadata := withArtifactMetadata
	withoutArtifactMetadata.SchemaVersion = SchemaVersion
	withoutArtifactMetadata.CompletenessReason = ""
	withoutArtifactMetadata.ObservedEntries = nil
	if err := withoutArtifactMetadata.Validate(); err != nil {
		t.Fatal(err)
	}
	if withArtifactMetadata.DerivationKey != withoutArtifactMetadata.DerivationKey {
		t.Fatalf("terminal metadata changed artifact derivation identity: with=%s without=%s", withArtifactMetadata.DerivationKey, withoutArtifactMetadata.DerivationKey)
	}
}

func TestDerivationV3RejectsInvalidTerminalMetadataStates(t *testing.T) {
	ref := RawOutputRef{SessionID: "session-v3-invalid", StartByte: 0, EndByte: 2, SHA256: strings.Repeat("e", 64)}
	producer := Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("f", 64)
	key, err := DerivationKeyForInputs([]StructuredInputRef{RawInputRef(ref)}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	base := Derivation{
		SchemaVersion: DerivationSchemaVersionV3, DerivationKey: key, SourceAuthorityRefs: []StructuredInputRef{RawInputRef(ref)},
		Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: LifecycleTerminal, ParseOutcome: ParsePartial, Completeness: CompletenessPartial,
		ObservedEntries: &ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 1, Entries: 1, Fail: 1},
	}
	cases := []struct {
		name   string
		mutate func(*Derivation)
	}{
		{"unknown reason", func(v *Derivation) { v.CompletenessReason = "other" }},
		{"reason on complete", func(v *Derivation) {
			v.ParseOutcome = ParseComplete
			v.Completeness = CompletenessComplete
			v.CompletenessReason = CompletenessReasonPassRecordsElided
		}},
		{"reason with budget outcome", func(v *Derivation) {
			v.ParseOutcome = ParseBudgetExceeded
			v.CompletenessReason = CompletenessReasonPassRecordsElided
		}},
		{"zero match with entries", func(v *Derivation) { v.CompletenessReason = CompletenessReasonZeroMatch }},
		{"metadata before terminal", func(v *Derivation) {
			v.Lifecycle = LifecycleProcessing
			v.ParseOutcome = ""
			v.Completeness = CompletenessUnavailable
		}},
		{"v2 claims v3 metadata", func(v *Derivation) { v.SchemaVersion = SchemaVersion }},
		{"v3 without v3 metadata", func(v *Derivation) { v.ObservedEntries = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base
			tc.mutate(&got)
			if err := got.Validate(); err == nil {
				t.Fatalf("invalid v3 derivation accepted: %#v", got)
			}
		})
	}
}

func TestDerivationV3AcceptsZeroMatchReasonOnlyWithZeroObservedEntries(t *testing.T) {
	ref := RawOutputRef{SessionID: "session-v3-zero", StartByte: 0, EndByte: 1, SHA256: strings.Repeat("1", 64)}
	producer := Producer{AdapterID: "vitest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("2", 64)
	key, err := DerivationKeyForInputs([]StructuredInputRef{RawInputRef(ref)}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	d := Derivation{
		SchemaVersion: DerivationSchemaVersionV3, DerivationKey: key, SourceAuthorityRefs: []StructuredInputRef{RawInputRef(ref)},
		Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: LifecycleTerminal, ParseOutcome: ParsePartial, Completeness: CompletenessPartial,
		CompletenessReason: CompletenessReasonZeroMatch,
		ObservedEntries:    &ObservedEntryCounts{Namespace: "vitest", VocabularyVersion: 1},
	}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDerivationV3PreservesPartialReasonAcrossCompaction(t *testing.T) {
	ref := RawOutputRef{SessionID: "session-v3-compacted", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("3", 64)}
	producer := Producer{AdapterID: "jest-json", AdapterVersion: 1, CapabilityVersion: 1}
	config := strings.Repeat("4", 64)
	key, err := DerivationKeyForInputs([]StructuredInputRef{RawInputRef(ref)}, producer, 1, config)
	if err != nil {
		t.Fatal(err)
	}
	d := Derivation{
		SchemaVersion: DerivationSchemaVersionV3, DerivationKey: key, SourceAuthorityRefs: []StructuredInputRef{RawInputRef(ref)},
		Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: config,
		Lifecycle: LifecycleTerminal, ParseOutcome: ParsePartial, Completeness: CompletenessCompacted,
		CompletenessReason: CompletenessReasonPassRecordsElided,
		ObservedEntries:    &ObservedEntryCounts{Namespace: "jest", VocabularyVersion: 1, Files: 2, Entries: 2, Fail: 1, Pass: 1},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("compacted partial derivation lost terminal metadata contract: %v", err)
	}
}
