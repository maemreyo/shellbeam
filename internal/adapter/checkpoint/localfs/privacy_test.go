package localfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
)

func TestCapturePublicResultDoesNotExposeSecretContentHashSymlinkTextOrAbsoluteRoot(t *testing.T) {
	workspace := t.TempDir()
	secret := "pink-elephant-low-entropy-secret"
	if err := os.WriteFile(filepath.Join(workspace, "secret.txt"), []byte(secret), 0644); err != nil {
		t.Fatal(err)
	}
	linkText := filepath.Join(t.TempDir(), "private-target-secret")
	if err := os.Symlink(linkText, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	runtime := filepath.Join(t.TempDir(), "run")
	mustPrivateDir(t, state)
	mustPrivateDir(t, runtime)
	request := captureRequest(workspace, []string{"link", "secret.txt"})
	result, err := New(state, runtime).Capture(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(sum[:])
	publicCheckpoint := core.Checkpoint{
		SchemaVersion:     core.SchemaVersion,
		CheckpointID:      request.CheckpointID,
		CreateID:          "cp-create-1",
		Provider:          core.ProviderIdentity{ID: "localfs", Version: 1},
		WorkspaceID:       request.WorkspaceID,
		ActivityID:        request.ActivityID,
		SourceGeneration:  request.SourceGeneration,
		CreatedAt:         time.Unix(1, 0).UTC(),
		CapturedPathCount: result.CapturedPathCount,
		Excluded:          result.Excluded,
		Unsupported:       result.Unsupported,
		TotalBytes:        result.TotalBytes,
		CaptureQuality:    result.CaptureQuality,
		RetentionState:    core.RetentionAvailable,
		OpaqueEntryRefs:   result.OpaqueEntryRefs,
	}
	for name, value := range map[string]any{"capture_result": result, "checkpoint": publicCheckpoint} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{secret, hash, linkText, workspace, state, runtime} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s leaked %q: %s", name, forbidden, text)
			}
		}
	}
	for _, ref := range result.OpaqueEntryRefs {
		if ref == hash || strings.Contains(ref, hash) {
			t.Fatalf("opaque ref dictionary-comparable: %q", ref)
		}
	}
}
