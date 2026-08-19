package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"runtime/debug"
	"strings"
	"testing"
)

func TestCurrentUsesLinkerValues(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	t.Cleanup(func() { version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt })
	version, commit, builtAt = "v0.1.0-dev", "abc123", "2026-08-13T00:00:00Z"
	got := Current()
	if got.Version != version || got.Commit != commit || got.BuiltAt != builtAt {
		t.Fatalf("Current() = %#v", got)
	}
}

func TestCaptureProcessIdentityUsesEmbeddedVCSAndExecutableDigest(t *testing.T) {
	modified := true
	got := captureProcessIdentity(
		func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "86b0cb56cf7a57dd6ab1d0208bf08ffcb3acbbbf"},
					{Key: "vcs.modified", Value: "true"},
				},
			}, true
		},
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("binary-bytes")), nil },
		Info{Version: "dev", Commit: "unknown", BuiltAt: "unknown"},
	)
	wantDigest := sha256.Sum256([]byte("binary-bytes"))
	if got.Revision != "86b0cb56cf7a57dd6ab1d0208bf08ffcb3acbbbf" || got.VCSModified == nil || *got.VCSModified != modified || got.BinarySHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("identity=%#v", got)
	}
	if got.Version != "" {
		t.Fatalf("development version should stay unknown, got=%q", got.Version)
	}
}

func TestCaptureProcessIdentityFallsBackToLinkerMetadataAndToleratesMissingDigest(t *testing.T) {
	got := captureProcessIdentity(
		func() (*debug.BuildInfo, bool) { return nil, false },
		func() (io.ReadCloser, error) { return nil, errors.New("unavailable") },
		Info{Version: "v1.2.3", Commit: "abc1234", BuiltAt: "2026-08-19T00:00:00Z"},
	)
	if got.Version != "v1.2.3" || got.Revision != "abc1234" || got.BinarySHA256 != "" || got.VCSModified != nil {
		t.Fatalf("identity=%#v", got)
	}
}
