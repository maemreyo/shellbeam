//go:build darwin

package dyld

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE27PrivacyBindingNeverExposesPrivateCompilerSocketOrStatePaths(t *testing.T) {
	state := e27PrivateState(t)
	provider := New(state)
	prepared, err := provider.Prepare(context.Background(), e27PrepareRequest("privacy"))
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Abort()
	encoded, err := json.Marshal(prepared.Binding())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{state, providerRoot(state), "DYLD_INSERT_LIBRARIES", "SHELLBEAM_TRACE_SOCKET", ".dylib", ".sock", "raw.events"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("binding leaked %q: %s", forbidden, text)
		}
	}
	for _, entry := range prepared.EnvironmentAdditions() {
		if entry.Key == "SHELLBEAM_TRACE_SOCKET" {
			info, err := os.Lstat(entry.Value)
			if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
				t.Fatalf("socket=%#v err=%v", info, err)
			}
			parent := filepath.Dir(entry.Value)
			if parent != socketRoot() {
				t.Fatalf("socket parent=%q want=%q", parent, socketRoot())
			}
			if err := validatePrivateDir(parent); err != nil {
				t.Fatalf("unsafe socket root: %v", err)
			}
		}
	}
}
