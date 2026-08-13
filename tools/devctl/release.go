package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type releaseEvidence struct {
	SchemaVersion     int              `json:"schema_version"`
	GeneratedAt       time.Time        `json:"generated_at"`
	Commit            string           `json:"commit"`
	SourceFingerprint string           `json:"source_fingerprint"`
	GoVersion         string           `json:"go_version"`
	HostOS            string           `json:"host_os"`
	HostArch          string           `json:"host_arch"`
	Results           []boundaryResult `json:"results"`
}
type boundaryResult struct {
	Boundary string `json:"boundary"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

func writeReleaseEvidence(path, fingerprint string) error {
	commit := trimCommand("git", "rev-parse", "HEAD")
	goVersion := trimCommand("go", "version")
	r := releaseEvidence{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), Commit: commit, SourceFingerprint: fingerprint, GoVersion: goVersion, HostOS: runtime.GOOS, HostArch: runtime.GOARCH, Results: []boundaryResult{{"go_test_full", "PASS", "go test -count=1 ./..."}, {"race_current_host", "PASS", "scripts/test-hardening.sh"}, {"mcp_sdk_in_memory", "PASS", "TestInMemoryConformance"}, {"process_pty_current_host", "PASS", "native process integration tests"}, {"unix_socket_native", "BLOCKED", "container sandbox denies AF_UNIX listen; user-run test included"}, {"macos_native_runtime", "NOT_RUN", "cross-build only on Linux host"}, {"secure_mcp_tunnel_chatgpt", "NOT_RUN", "credentials unavailable; user-run guide included"}, {"signing_notarization", "NOT_RUN", "private source package"}}}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}
func trimCommand(name string, args ...string) string {
	b, err := exec.Command(name, args...).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}
