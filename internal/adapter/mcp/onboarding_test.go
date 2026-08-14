package mcp

import (
	"strings"
	"testing"
)

func TestOnboardingInstructionsRequireReviewWorkflow(t *testing.T) {
	text := strings.ToLower(projectOnboardingInstructions)
	required := []string{
		"inspect.project",
		"do not auto-trust",
		"do not automatically write",
		"user approval",
		"validate",
		"exact current discovery_fingerprint",
		"review_due",
		"does not block ordinary execution",
	}
	for _, phrase := range required {
		if !strings.Contains(text, phrase) {
			t.Errorf("instructions missing %q: %s", phrase, projectOnboardingInstructions)
		}
	}
}
