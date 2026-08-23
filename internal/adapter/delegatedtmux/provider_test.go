package delegatedtmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProviderIdentityAndDeterministicRefArePure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	p, err := New(Config{Root: root, TmuxPath: "/bin/false", QualifiedPath: "/bin/false", ExpectedVersion: "tmux test", ExpectedSHA256: strings.Repeat("0", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Identity(); got.ID != ProviderID || got.Version != ProviderVersion {
		t.Fatalf("identity=%#v", got)
	}
	at := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	a, err := p.ProviderRefForSession("session_01", at)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.ProviderRefForSession("session_01", at)
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a.Ref == "" || a.SessionID != "session_01" {
		t.Fatalf("refs a=%#v b=%#v", a, b)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("pure ref derivation touched root: %v", err)
	}
}

func TestProbeRejectsUnqualifiedBinaryHash(t *testing.T) {
	dir := t.TempDir()
	tmux := filepath.Join(dir, "tmux")
	if err := os.WriteFile(tmux, []byte("#!/bin/sh\necho 'tmux test'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Root: filepath.Join(dir, "state"), TmuxPath: tmux, QualifiedPath: tmux, ExpectedVersion: "tmux test", ExpectedSHA256: "0000000000000000000000000000000000000000000000000000000000000000", AllowCurrentPlatformForTest: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Probe(t.Context()); err == nil {
		t.Fatal("wrong tmux hash accepted")
	}
}
