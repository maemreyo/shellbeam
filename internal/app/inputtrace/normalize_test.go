package inputtrace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/inputtrace"
)

func TestE27InputTraceRedactNormalizesRepoSystemExternalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	secretPath := filepath.Join(external, "LOWENTROPY-secret.txt")
	if err := os.WriteFile(secretPath, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secretPath, filepath.Join(root, "escape-link")); err != nil {
		t.Fatal(err)
	}
	resources := []ProviderResource{
		{ObservationClass: core.ClassFilesystemReads, Path: filepath.Join(root, "inside.txt")},
		{ObservationClass: core.ClassFilesystemReads, Path: "inside.txt"},
		{ObservationClass: core.ClassFilesystemMetadataQueries, Path: secretPath},
		{ObservationClass: core.ClassFilesystemReads, Path: filepath.Join(root, "escape-link")},
		{ObservationClass: core.ClassExecutedBinaries, Path: "/usr/bin/env"},
	}
	got, summary := NormalizeResources(NormalizationContext{WorkspaceRoot: root, ExecutionCWD: root}, resources)
	if !summary.Truncated && len(got) != 4 {
		t.Fatalf("resources=%#v summary=%#v", got, summary)
	}
	seen := map[string]bool{}
	for _, item := range got {
		seen[string(item.PathClass)+":"+item.Identity] = true
	}
	if !seen["repo_relative:inside.txt"] || !seen["system_classified:usr"] || !seen["workspace_external_redacted:external-1"] {
		t.Fatalf("normalized=%#v", got)
	}
	encoded, _ := json.Marshal(got)
	for _, forbidden := range []string{external, "LOWENTROPY", secretPath, "escape-link"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public normalization leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestE27InputTraceExternalOrdinalsAreRecordLocalNotDictionaryIdentity(t *testing.T) {
	first, _ := NormalizeResources(NormalizationContext{}, []ProviderResource{{ObservationClass: core.ClassFilesystemReads, Path: "/Users/alice/private-a"}})
	second, _ := NormalizeResources(NormalizationContext{}, []ProviderResource{{ObservationClass: core.ClassFilesystemReads, Path: "/Users/alice/private-b"}})
	if len(first) != 1 || len(second) != 1 || first[0].Identity != "external-1" || second[0].Identity != "external-1" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestE27InputTraceNormalizeBoundsReturnedResourcesAndDowngrades(t *testing.T) {
	root := t.TempDir()
	resources := make([]ProviderResource, 0, core.MaxPublicResources+1)
	for i := 0; i < core.MaxPublicResources+1; i++ {
		name := filepath.Join(root, "f-"+strings.Repeat("x", 4)+"-"+fmtInt(i))
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
		resources = append(resources, ProviderResource{ObservationClass: core.ClassFilesystemReads, Path: name})
	}
	got, summary := NormalizeResources(NormalizationContext{WorkspaceRoot: root, ExecutionCWD: root}, resources)
	if len(got) != core.MaxPublicResources || !summary.Truncated || summary.Observed != core.MaxPublicResources+1 || summary.Returned != core.MaxPublicResources {
		t.Fatalf("len=%d summary=%#v", len(got), summary)
	}
}

func fmtInt(v int) string { return strconv.Itoa(v) }
