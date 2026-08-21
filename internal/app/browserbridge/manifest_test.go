package browserbridge

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRenderManifestPinsExactlyOneExtensionID(t *testing.T) {
	raw, err := RenderManifest("/usr/local/bin/shellbeam-browser-host", "attention-router@shellbeam.local")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var manifest struct {
		Name              string   `json:"name"`
		Type              string   `json:"type"`
		Path              string   `json:"path"`
		AllowedExtensions []string `json:"allowed_extensions"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if manifest.Name != HostName || manifest.Type != "stdio" {
		t.Fatalf("manifest identity = %+v", manifest)
	}
	if len(manifest.AllowedExtensions) != 1 || manifest.AllowedExtensions[0] != "attention-router@shellbeam.local" {
		t.Fatalf("allowed_extensions = %v", manifest.AllowedExtensions)
	}
}

func TestRenderManifestRejectsMissingOrWildcardExtensionID(t *testing.T) {
	for _, id := range []string{"", "*", "  ", "a@b, c@d"} {
		if _, err := RenderManifest("/usr/local/bin/shellbeam-browser-host", id); err == nil {
			t.Fatalf("extension id %q accepted", id)
		}
	}
}

func TestRenderManifestRequiresAbsoluteHostPath(t *testing.T) {
	if _, err := RenderManifest("shellbeam-browser-host", "a@b"); err == nil {
		t.Fatal("relative host path accepted")
	}
}

func TestManifestDirIsPerUserAndPlatformSpecific(t *testing.T) {
	darwin, err := ManifestDir("darwin", "/Users/u")
	if err != nil {
		t.Fatalf("darwin: %v", err)
	}
	if darwin != "/Users/u/Library/Application Support/Mozilla/NativeMessagingHosts" {
		t.Fatalf("darwin dir = %q", darwin)
	}
	linux, err := ManifestDir("linux", "/home/u")
	if err != nil {
		t.Fatalf("linux: %v", err)
	}
	if linux != "/home/u/.mozilla/native-messaging-hosts" {
		t.Fatalf("linux dir = %q", linux)
	}
	if _, err := ManifestDir("plan9", "/home/u"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestInstallThenRemoveManifestRoundTrips(t *testing.T) {
	home := t.TempDir()
	path, err := InstallManifest("linux", home, "/usr/local/bin/shellbeam-browser-host", "a@b")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if _, err := RemoveManifest("linux", home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("manifest survived removal")
	}
}
