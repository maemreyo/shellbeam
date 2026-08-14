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
	pending := Derivation{SchemaVersion: 1, DerivationKey: first, SourceAuthorityRefs: []RawOutputRef{ref}, Producer: producer, DerivationSchemaVersion: 1, DerivationConfigDigest: strings.Repeat("b", 64), Lifecycle: LifecyclePending, Completeness: CompletenessUnavailable}
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
