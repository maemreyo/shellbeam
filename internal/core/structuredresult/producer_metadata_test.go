package structuredresult

import (
	"strings"
	"testing"
)

func TestProducerMetadataEnvelopesAreBoundedButProviderAgnostic(t *testing.T) {
	disposition := ProducerTestDisposition{Namespace: "pytest", VocabularyVersion: 1, Code: "xfail"}
	if err := disposition.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (ProducerTestDisposition{Namespace: "pytest", VocabularyVersion: 0, Code: "xfail"}).Validate(); err == nil {
		t.Fatal("zero vocabulary version accepted")
	}
	if err := (ProducerTestDisposition{Namespace: "pytest", VocabularyVersion: 1, Code: "bad\ncode"}).Validate(); err == nil {
		t.Fatal("control-bearing disposition accepted")
	}

	coverage := ProducerSemanticsCoverage{
		Namespace: "pytest", VocabularyVersion: 1, Format: "junit_xml", Family: "xunit2",
		MechanicallyObservable: []string{"core:test_status_error", "core:test_status_pass", "pytest:xfail"},
		Unavailable:            []string{"pytest:error_phase", "pytest:xpass_exact"},
	}
	if err := coverage.Validate(); err != nil {
		t.Fatal(err)
	}
	coverage.MechanicallyObservable = []string{"pytest:xfail", "pytest:xfail"}
	if err := coverage.Validate(); err == nil {
		t.Fatal("duplicate coverage claim accepted")
	}
}

func TestArtifactEntryRecordIDUsesStructuralOrdinalNotProducerAddress(t *testing.T) {
	entry := ArtifactTestEntryRef{ArtifactBlobID: "abl_" + strings.Repeat("a", 64), SuiteOrdinal: 2, TestcaseOrdinal: 3}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("b", 64)
	first, err := ArtifactTestRecordID(key, entry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ArtifactTestRecordID(key, entry)
	if err != nil || first != second || len(first) != 64 {
		t.Fatalf("record ids first=%q second=%q err=%v", first, second, err)
	}
	entry.TestcaseOrdinal++
	changed, err := ArtifactTestRecordID(key, entry)
	if err != nil || changed == first {
		t.Fatalf("ordinal did not change record id: changed=%q err=%v", changed, err)
	}

	address := ProducerTestAddress{Namespace: "pytest", VocabularyVersion: 1, SuiteName: "pytest", Classname: "pkg.test_mod", Name: "test_x"}
	if err := address.Validate(); err != nil {
		t.Fatal(err)
	}
	address.Name = "bad\nname"
	if err := address.Validate(); err == nil {
		t.Fatal("control-bearing producer address accepted")
	}
}

func TestSuiteAggregateAllowsProducerDoubleFailureAccounting(t *testing.T) {
	aggregate := TestSuiteAggregate{Tests: 1, Failures: 1, Errors: 1, Skipped: 0}
	if err := aggregate.Validate(); err != nil {
		t.Fatalf("producer aggregate with fail+error for one logical test rejected: %v", err)
	}
	aggregate.Tests = -1
	if err := aggregate.Validate(); err == nil {
		t.Fatal("negative suite count accepted")
	}
}
