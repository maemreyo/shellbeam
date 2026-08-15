package project

import "testing"

func TestCanonicalCommandOutputValidationRejectsDigestKindMismatch(t *testing.T) {
	required := true
	for _, raw := range []rawOutput{
		{Path: "dist/file", Kind: "file", Digest: "tree-sha256", Required: &required},
		{Path: "dist/tree", Kind: "directory", Digest: "sha256", Required: &required},
		{Path: "dist/link", Kind: "symlink", Digest: "sha256", Required: &required},
	} {
		if _, err := validateOutput(raw, false); err == nil {
			t.Fatalf("canonical validator accepted incompatible output: %#v", raw)
		}
	}
}

func TestValidateExpectedOutputsUsesCanonicalNormalization(t *testing.T) {
	got, err := ValidateExpectedOutputs([]Output{{Path: "dist/../dist/report.json", Kind: "file", Digest: "sha256", Required: false}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "dist/report.json" || got[0].Required {
		t.Fatalf("normalized=%#v", got)
	}
}
