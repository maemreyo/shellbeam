package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func verificationRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestVerificationSurfaceForbidsCompletionTruthFields(t *testing.T) {
	root := verificationRepositoryRoot(t)
	paths := []string{
		"internal/core/verification", "internal/app/verification", "internal/app/bridge/verification.go",
		"internal/adapter/ipc/verification_protocol_v2.go", "internal/adapter/mcp/verification_input.go", "internal/adapter/mcp/verification_call.go", "cmd/shellbeam/verification.go",
		"api/schema/ipc-v2.json", "api/schema/mcp-input-v2.json", "api/schema/mcp-output-v2.json",
	}
	forbidden := []string{"task_complete", "work_complete", "safe_to_finish"}
	for _, rel := range paths {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		files := []string{rel}
		if info.IsDir() {
			files = nil
			err = filepath.WalkDir(filepath.Join(root, rel), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
					r, _ := filepath.Rel(root, path)
					files = append(files, r)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, file := range files {
			data, err := os.ReadFile(filepath.Join(root, file))
			if err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(string(data))
			for _, key := range forbidden {
				if strings.Contains(lower, key) {
					t.Fatalf("completion truth field %q leaked into %s", key, file)
				}
			}
		}
	}
}

func TestVerificationStageAOmitGateStatus(t *testing.T) {
	root := verificationRepositoryRoot(t)
	for _, rel := range []string{"internal/app/verification/inspect.go", "api/schema/ipc-v2.json", "api/schema/mcp-output-v2.json"} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"gate_status"`) {
			t.Fatalf("Stage A gate_status leaked into %s", rel)
		}
	}
}

func TestVerificationSchemasRemainClosedJSON(t *testing.T) {
	root := verificationRepositoryRoot(t)
	for _, rel := range []string{"api/schema/ipc-v2.json", "api/schema/mcp-input-v2.json", "api/schema/mcp-output-v2.json"} {
		var doc map[string]any
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}
