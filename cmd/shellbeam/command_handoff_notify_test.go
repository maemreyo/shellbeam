package main

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	shelladapter "github.com/maemreyo/shellbeam/internal/adapter/shellintegration"
)

func TestHiddenHandoffNotifierSendsClosedMetadataWithoutEnvironment(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sb-hn-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "notify.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	secret := "task6-secret-canary-7H2K"
	t.Setenv("CONTROL_PLANE_API_KEY", secret)
	got := make(chan shelladapter.Notification, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		var msg shelladapter.Notification
		err = json.NewDecoder(bufio.NewReader(conn)).Decode(&msg)
		if err != nil {
			errCh <- err
			return
		}
		got <- msg
	}()
	code := run([]string{"__handoff_notify", "--socket", socket, "--handoff-id", "handoff-task6", "--epoch", "4", "--event-id", "evt_task6", "--shell-runtime-id", "runtime-task6", "--event", "prompt_boundary", "--satisfied", "true"}, os.Stdout, os.Stderr)
	if code != 0 {
		t.Fatalf("notify exit=%d", code)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case msg := <-got:
		encoded, _ := json.Marshal(msg)
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("secret leaked: %s", encoded)
		}
		if msg.HandoffID != "handoff-task6" || msg.AuthorityEpoch != 4 || !msg.Satisfied || msg.Event != shelladapter.NotificationPromptBoundary {
			t.Fatalf("msg=%#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("notifier did not connect")
	}
}

func TestHiddenNotifierIsAbsentFromPublicUsageAndRejectsExtraFields(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(errOut.String(), "handoff_notify") {
		t.Fatalf("hidden command leaked in usage: %s", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"__handoff_notify", "--socket", "/tmp/n.sock", "--handoff-id", "h", "--epoch", "1", "--event-id", "e", "--shell-runtime-id", "r", "--event", "prompt_boundary", "--satisfied", "false", "extra"}, &out, &errOut); code == 0 {
		t.Fatal("notifier accepted trailing argument")
	}
}
