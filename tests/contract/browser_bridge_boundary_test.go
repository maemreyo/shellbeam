package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func browserBridgeRepoPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(verificationRepositoryRoot(t), rel)
}

var browserBridgeProductionFiles = []string{
	"internal/core/browserbridge/protocol.go",
	"internal/core/browserbridge/bound.go",
	"internal/app/browserbridge/ports.go",
	"internal/app/browserbridge/facts_activity.go",
	"internal/app/browserbridge/facts_verification.go",
	"internal/app/browserbridge/facts_structured.go",
	"internal/app/browserbridge/host.go",
	"internal/app/browserbridge/framing.go",
	"internal/app/browserbridge/manifest.go",
	"internal/adapter/ipc/browser_bridge_reader.go",
	"cmd/shellbeam-browser-host/main.go",
}

func TestBrowserBridgeSurfaceForbidsJudgmentFields(t *testing.T) {
	forbidden := []string{
		"mechanical_blockers",
		"should_continue",
		"needs_attention",
		"is_stuck",
		"task_complete",
		"work_complete",
		"safe_to_finish",
	}
	for _, rel := range browserBridgeProductionFiles {
		raw, err := os.ReadFile(browserBridgeRepoPath(t, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		lower := strings.ToLower(string(raw))
		for _, key := range forbidden {
			if strings.Contains(lower, key) {
				t.Fatalf("%s contains judgment field %q", rel, key)
			}
		}
	}
}

func TestBrowserBridgeDoesNotReuseTheGenericPassthrough(t *testing.T) {
	for _, rel := range browserBridgeProductionFiles {
		raw, err := os.ReadFile(browserBridgeRepoPath(t, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(raw), `internal/app/bridge"`) {
			t.Fatalf("%s imports the generic bridge handler", rel)
		}
	}
}

func TestBrowserBridgeActionsAreLimitedToTheDeclaredReads(t *testing.T) {
	allowed := map[string]bool{
		"inspect.activity":     true,
		"inspect.sessions":     true,
		"inspect.events":       true,
		"inspect.verification": true,
		"inspect.structured":   true,
	}
	rel := "internal/adapter/ipc/browser_bridge_reader.go"
	raw, err := os.ReadFile(browserBridgeRepoPath(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	seen := map[string]bool{}
	for _, fragment := range strings.Split(string(raw), `Action: "`)[1:] {
		end := strings.Index(fragment, `"`)
		if end < 0 {
			t.Fatalf("%s contains an unterminated action literal", rel)
		}
		action := fragment[:end]
		if !allowed[action] {
			t.Fatalf("%s uses undeclared daemon action %q", rel, action)
		}
		seen[action] = true
	}
	for action := range allowed {
		if !seen[action] {
			t.Fatalf("%s does not pin declared daemon action %q", rel, action)
		}
	}
	if len(seen) != len(allowed) {
		t.Fatalf("declared action set drifted: seen=%v", seen)
	}
}
