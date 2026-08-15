package environment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
)

func TestHostObserveCapturesSecretSafeBaseFacts(t *testing.T) {
	values := map[string]string{
		"PATH":   "/bin::/usr/bin:/bin",
		"TOKEN":  "secret",
		"EMPTY":  "",
		"NUMBER": "1234",
	}
	host := &Host{
		goos:   func() string { return "darwin" },
		goarch: func() string { return "arm64" },
		lookupEnv: func(name string) (string, bool) {
			value, ok := values[name]
			return value, ok
		},
	}
	got, err := host.Observe(context.Background(), core.ExecutionContext{Mode: "shell", Identity: "/opt/homebrew/bin/fish"}, []string{"TOKEN", "EMPTY", "TOKEN", "NUMBER"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Platform.OS != "darwin" || got.Platform.Architecture != "arm64" || got.Execution.Identity != "/opt/homebrew/bin/fish" {
		t.Fatalf("base facts=%#v", got)
	}
	if got.Path.EntryCount != 4 || got.Path.Quality != core.QualityComplete || got.Path.Digest == "" {
		t.Fatalf("path=%#v", got.Path)
	}
	wantPresence := []core.VariablePresence{{Name: "EMPTY", Present: true}, {Name: "NUMBER", Present: true}, {Name: "TOKEN", Present: true}}
	if len(got.VariablePresence) != len(wantPresence) {
		t.Fatalf("presence=%#v", got.VariablePresence)
	}
	for i := range wantPresence {
		if got.VariablePresence[i] != wantPresence[i] {
			t.Fatalf("presence[%d]=%#v want %#v", i, got.VariablePresence[i], wantPresence[i])
		}
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"secret", "1234", "/bin::/usr/bin:/bin"} {
		if strings.Contains(text, secret) {
			t.Fatalf("host observation leaked %q: %s", secret, text)
		}
		sum := sha256.Sum256([]byte(secret))
		if strings.Contains(text, hex.EncodeToString(sum[:])) {
			t.Fatalf("host observation leaked direct value hash for %q: %s", secret, text)
		}
	}
}

func TestHostObservePathCountingIsDeterministic(t *testing.T) {
	for name, path := range map[string]string{
		"empty":    "",
		"repeated": "/bin:/bin",
		"empties":  ":/bin:",
	} {
		t.Run(name, func(t *testing.T) {
			host := &Host{goos: func() string { return "linux" }, goarch: func() string { return "amd64" }, lookupEnv: func(key string) (string, bool) {
				if key == "PATH" {
					return path, true
				}
				return "", false
			}}
			got, err := host.Observe(context.Background(), core.ExecutionContext{Mode: "argv", Identity: "/usr/bin/env"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := core.PathFingerprint(path)
			if got.Path != want {
				t.Fatalf("path=%#v want %#v", got.Path, want)
			}
		})
	}
}
