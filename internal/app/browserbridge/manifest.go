package browserbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const HostName = "com.shellbeam.browser_bridge"

// ManifestDir returns the per-user Firefox native messaging host directory.
//
// Installation is per user and deliberately separate from shellbeam install:
// installing the daemon must never silently grant a browser extension a
// channel to machine facts.
func ManifestDir(goos, home string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Mozilla", "NativeMessagingHosts"), nil
	case "linux":
		return filepath.Join(home, ".mozilla", "native-messaging-hosts"), nil
	default:
		return "", fmt.Errorf("unsupported platform for native messaging manifest")
	}
}

func RenderManifest(hostPath, extensionID string) ([]byte, error) {
	if !filepath.IsAbs(hostPath) {
		return nil, fmt.Errorf("host path must be absolute")
	}
	id := strings.TrimSpace(extensionID)
	if id == "" || id != extensionID || strings.ContainsAny(id, "*, \t") {
		return nil, fmt.Errorf("exactly one literal extension id is required")
	}
	return json.MarshalIndent(map[string]any{
		"name":               HostName,
		"description":        "ShellBeam Browser Bridge (read-only machine facts)",
		"path":               hostPath,
		"type":               "stdio",
		"allowed_extensions": []string{id},
	}, "", "  ")
}

func InstallManifest(goos, home, hostPath, extensionID string) (string, error) {
	raw, err := RenderManifest(hostPath, extensionID)
	if err != nil {
		return "", err
	}
	dir, err := ManifestDir(goos, home)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, HostName+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func RemoveManifest(goos, home string) (string, error) {
	dir, err := ManifestDir(goos, home)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, HostName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
