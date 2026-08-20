package structuredresult

import (
	"strings"
	"testing"
)

func testArtifactBlobRef() ArtifactBlobRef {
	return ArtifactBlobRef{
		SchemaVersion:           ArtifactBlobSchemaVersion,
		BlobID:                  "abl_" + strings.Repeat("a", 64),
		OperationID:             "op-artifact",
		SessionID:               "session-artifact",
		RepositoryID:            "repo_01K00000000000000000000000",
		WorkspaceID:             "ws_01K00000000000000000000000",
		DeclaredPath:            "reports/junit.xml",
		NormalizedWorkspacePath: "reports/junit.xml",
		SHA256:                  strings.Repeat("b", 64),
		Size:                    128,
		TerminalCut:             TerminalCutV1{SchemaVersion: TerminalCutSchemaVersion, ReceiptSchemaVersion: 4, ReceiptDigest: strings.Repeat("c", 64)},
		ObservationCut:          ObservationCutV1{SchemaVersion: ObservationCutSchemaVersion, Digest: strings.Repeat("d", 64)},
	}
}

func TestStructuredInputRefIsClosedAndExactlyOneBranch(t *testing.T) {
	raw := RawOutputRef{SessionID: "session-1", StartByte: 0, EndByte: 3, SHA256: strings.Repeat("e", 64)}
	blob := testArtifactBlobRef()
	valid := []StructuredInputRef{
		{Kind: StructuredInputRawOutput, RawOutput: &raw},
		{Kind: StructuredInputArtifactBlob, ArtifactBlob: &blob},
	}
	for _, ref := range valid {
		if err := ref.Validate(); err != nil {
			t.Fatalf("valid ref %#v: %v", ref, err)
		}
	}
	invalid := []StructuredInputRef{
		{},
		{Kind: "future", RawOutput: &raw},
		{Kind: StructuredInputRawOutput},
		{Kind: StructuredInputArtifactBlob},
		{Kind: StructuredInputRawOutput, RawOutput: &raw, ArtifactBlob: &blob},
		{Kind: StructuredInputRawOutput, ArtifactBlob: &blob},
		{Kind: StructuredInputArtifactBlob, RawOutput: &raw},
	}
	for _, ref := range invalid {
		if err := ref.Validate(); err == nil {
			t.Fatalf("invalid ref accepted: %#v", ref)
		}
	}
}

func TestArtifactBlobRefRequiresCanonicalIdentityAndClosedCuts(t *testing.T) {
	valid := testArtifactBlobRef()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*ArtifactBlobRef){
		func(v *ArtifactBlobRef) { v.BlobID = strings.Repeat("a", 64) },
		func(v *ArtifactBlobRef) { v.RepositoryID = "repo-bad" },
		func(v *ArtifactBlobRef) { v.WorkspaceID = "ws-bad" },
		func(v *ArtifactBlobRef) { v.NormalizedWorkspacePath = "../junit.xml" },
		func(v *ArtifactBlobRef) { v.SHA256 = "bad" },
		func(v *ArtifactBlobRef) { v.Size = -1 },
		func(v *ArtifactBlobRef) { v.TerminalCut.ReceiptDigest = "bad" },
		func(v *ArtifactBlobRef) { v.ObservationCut.SchemaVersion = 2 },
	}
	for i, mutate := range mutations {
		got := valid
		mutate(&got)
		if err := got.Validate(); err == nil {
			t.Fatalf("mutation %d accepted: %#v", i, got)
		}
	}
}

func TestArtifactContentEqualityDoesNotCollapseStorageIdentity(t *testing.T) {
	a := testArtifactBlobRef()
	b := a
	b.BlobID = "abl_" + strings.Repeat("f", 64)
	b.OperationID = "op-other"
	b.SessionID = "session-other"
	if a.SHA256 != b.SHA256 || a.Size != b.Size || a.BlobID == b.BlobID {
		t.Fatalf("content/storage identity setup invalid: a=%#v b=%#v", a, b)
	}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := b.Validate(); err != nil {
		t.Fatal(err)
	}
}
