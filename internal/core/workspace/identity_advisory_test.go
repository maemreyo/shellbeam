package workspace

import (
	"encoding/json"
	"strings"
	"testing"

	gitidentity "github.com/maemreyo/shellbeam/internal/core/gitidentity"
)

func TestIdentityAdvisoryFingerprintTracksCauseNotFileStateOrSecrets(t *testing.T) {
	id := WorkspaceID("ws_01K00000000000000000000000")
	first := AdvisoryFromIdentityFinding(id, gitidentity.Finding{
		Code: "commit_identity_mismatch", Severity: "warning", Message: "first wording",
		Facts: map[string]string{"profile": "work", "resolution_source": "workspace", "raw_email": "secret@example.invalid"},
	})
	sameCause := AdvisoryFromIdentityFinding(id, gitidentity.Finding{
		Code: "commit_identity_mismatch", Severity: "warning", Message: "different wording after file edit",
		Facts: map[string]string{"profile": "work", "resolution_source": "workspace", "raw_email": "other-secret@example.invalid"},
	})
	if first.CauseFingerprint == "" || first.CauseFingerprint != sameCause.CauseFingerprint {
		t.Fatalf("same identity cause was not stable: first=%q same=%q", first.CauseFingerprint, sameCause.CauseFingerprint)
	}
	changed := AdvisoryFromIdentityFinding(id, gitidentity.Finding{
		Code: "commit_identity_mismatch", Severity: "warning", Message: "profile changed",
		Facts: map[string]string{"profile": "personal", "resolution_source": "workspace"},
	})
	if changed.CauseFingerprint == first.CauseFingerprint {
		t.Fatal("changed identity cause reused prior fingerprint")
	}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret@example.invalid") {
		t.Fatalf("identity advisory leaked secret fact: %s", data)
	}
}
