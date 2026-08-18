package delegatedtmux

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivateStateRoundTripIsPrivateAndClosed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	store := privateStateStore{root: root}
	now := time.Date(2026, 8, 18, 18, 0, 0, 0, time.UTC)
	want := privateState{SchemaVersion: privateStateSchemaVersion, Ref: "dtmux_abc", SessionID: "session_01", SocketPath: "/tmp/private.sock", TmuxSession: "sb_abc", SessionInternalID: "$1", WindowID: "@1", PaneID: "%1", ProviderGeneration: "gen_abc", StartGatePath: "/tmp/start.fifo", StartReleased: true, ServerPID: 123, PanePID: 456, TmuxVersion: "tmux 3.6a", TmuxSHA256: qualifiedTmuxSHA256, CreatedAt: now, UpdatedAt: now}
	if err := store.save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path(want.Ref))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode=%#o", info.Mode().Perm())
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if rootInfo.Mode().Perm() != 0o700 {
		t.Fatalf("root mode=%#o", rootInfo.Mode().Perm())
	}
	got, err := store.load(want.Ref)
	if err != nil || got != want {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if _, err := store.load("../escape"); err == nil {
		t.Fatal("state traversal accepted")
	}
}
