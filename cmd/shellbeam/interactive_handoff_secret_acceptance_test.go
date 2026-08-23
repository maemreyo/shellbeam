//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	handoff "github.com/maemreyo/shellbeam/internal/core/interactivehandoff"
	"github.com/maemreyo/shellbeam/internal/core/observation"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestInteractiveHandoffSecretNativeDaemonRestartKeepsGapAndReplacementPrivate(t *testing.T) {
	setup := startH4NativePrivateRestart(t)
	secretDuringGap := h4NativeCanary(t, "gap")
	gapVariants := b1SecretVariants([]byte(secretDuringGap))
	setup.first.hardKill(t)
	if _, err := setup.attach.master.Write([]byte(secretDuringGap + "\n")); err != nil {
		t.Fatal(err)
	}
	waitH4NativePTYContains(t, setup.attach.master, "H4-HUMAN:"+secretDuringGap)

	second := startB1NativeDaemon(t, setup.binary, setup.stateDir, setup.runtimeDir)
	defer second.hardKill(t)
	recovered := waitH4NativeHandoffStatus(t, second.client, setup.handoffID, handoff.StatusHumanOwned)
	if recovered.PrivacyState != handoff.PrivacyPrivate || recovered.CaptureState != handoff.CapturePrivate || recovered.HumanIngress != handoff.IngressWritable {
		t.Fatalf("recovered private handoff=%#v", recovered)
	}
	poll := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "poll", SessionID: setup.sessionID, MaxOutputBytes: 8192})
	events := callB1Native(t, second.client, ipcadapter.RequestV2{Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: "h4-task9-native-private-restart"}, MaxEvents: 64})
	h4AssertNativeNoSecret(t, "post-restart public IPC/event journal", []any{recovered, poll, events}, gapVariants)
	assertB1TreeNoSecret(t, setup.stateDir, "H4 private restart state", gapVariants, "")
	logBytes, _ := os.ReadFile(second.logPath)
	assertB1NoSecretVariants(t, "H4 daemon log", logBytes, gapVariants)

	secretAfterRestart := h4NativeCanary(t, "after")
	afterVariants := b1SecretVariants([]byte(secretAfterRestart))
	if _, err := setup.attach.master.Write([]byte(secretAfterRestart + "\n")); err != nil {
		t.Fatal(err)
	}
	waitH4NativePTYContains(t, setup.attach.master, "H4-HUMAN:"+secretAfterRestart)
	poll = callB1Native(t, second.client, ipcadapter.RequestV2{Action: "poll", SessionID: setup.sessionID, MaxOutputBytes: 8192})
	events = callB1Native(t, second.client, ipcadapter.RequestV2{Action: "inspect.events", Target: &observation.Target{Kind: observation.TargetOperation, OperationID: "h4-task9-native-private-restart"}, MaxEvents: 64})
	h4AssertNativeNoSecret(t, "replacement private observer/event journal", []any{poll, events}, afterVariants)
	assertB1TreeNoSecret(t, setup.stateDir, "H4 replacement-private state", afterVariants, "")
	logBytes, _ = os.ReadFile(second.logPath)
	assertB1NoSecretVariants(t, "H4 replacement daemon log", logBytes, afterVariants)

	truthPath := filepath.Join(setup.stateDir, "delegated-sessions", "capture", setup.sessionID+".json")
	truthBytes, err := os.ReadFile(truthPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(truthBytes, []byte(`"capture_quality":"partial"`)) || !bytes.Contains(truthBytes, []byte(`"private_intervals_omitted"`)) {
		t.Fatalf("durable private capture truth=%s", truthBytes)
	}
}

type h4NativeRestartSetup struct {
	binary, stateDir, runtimeDir, sessionID, handoffID string
	first                                              *b1NativeDaemon
	attach                                             *h4NativeAttach
}

func startH4NativePrivateRestart(t *testing.T) h4NativeRestartSetup {
	t.Helper()
	tmuxPath := requireDelegatedNativeTmux(t)
	t.Setenv("PATH", filepath.Dir(tmuxPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	binary := buildB1NativeBinary(t)
	stateDir, runtimeDir := b1NativeDirs(t)
	home := h4NativeAttachHome(t, stateDir, runtimeDir)
	var providerState delegatedNativeProviderState
	t.Cleanup(func() { killDelegatedNativeProvider(providerState) })
	first := startB1NativeDaemon(t, binary, stateDir, runtimeDir)
	inspect := callB1Native(t, first.client, ipcadapter.RequestV2{Action: "inspect.server"})
	if inspect.Server == nil || inspect.Server.InteractiveHandoff == nil || !inspect.Server.InteractiveHandoff.Secret || inspect.Server.InteractiveHandoff.Privacy == nil {
		first.hardKill(t)
		t.Fatalf("H4 production capability unavailable: %#v", inspect.Server)
	}
	started := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "start", OperationID: "h4-task9-native-private-restart", CWD: "/tmp",
		Command:     "stty -echo; printf 'H4-RESTART-READY\\n'; while IFS= read -r line; do printf 'H4-HUMAN:%s\\n' \"$line\"; done",
		SessionMode: delegated.ModeDelegatedInteractive, StdinMode: operation.StdinModeStream,
		TimeoutMode: operation.TimeoutModeUnlimited, YieldMS: 50, MaxOutputBytes: 8192,
	})
	if started.Result == nil || started.Result.Operation.SessionID == "" {
		first.hardKill(t)
		t.Fatalf("start=%#v", started)
	}
	sessionID := started.Result.Operation.SessionID
	waitB1NativeOutputContains(t, first.client, sessionID, "H4-RESTART-READY")
	_, providerState = loadDelegatedNativeProviderIdentity(t, stateDir, sessionID)
	completion := handoff.Completion{Kind: handoff.CompletionManualReady}
	requested := callB1NativeDaemon(t, first, ipcadapter.RequestV2{
		Action: "handoff.request", HandoffID: "handoff-h4-native-private-restart", SessionID: sessionID,
		Reason: string(handoff.ReasonCredentialRequired), HandoffPrivacy: handoff.PrivacySecret, HandoffCompletion: &completion,
	})
	if requested.Handoff == nil || requested.Handoff.PrivacyState != handoff.PrivacyPrivate || requested.Handoff.CaptureState != handoff.CapturePrivate || requested.Handoff.HumanIngress != handoff.IngressFenced {
		first.hardKill(t)
		t.Fatalf("secret request=%#v", requested.Handoff)
	}
	attach := startH4NativeAttach(t, binary, home, tmuxPath, requested.Handoff.HandoffID)
	pre := waitH4NativeHandoffStatus(t, first.client, requested.Handoff.HandoffID, handoff.StatusHumanOwned)
	if pre.PrivacyState != handoff.PrivacyPrivate || pre.CaptureState != handoff.CapturePrivate || pre.HumanIngress != handoff.IngressWritable {
		first.hardKill(t)
		t.Fatalf("pre-restart human private=%#v", pre)
	}
	return h4NativeRestartSetup{binary: binary, stateDir: stateDir, runtimeDir: runtimeDir, sessionID: sessionID, handoffID: requested.Handoff.HandoffID, first: first, attach: attach}
}

type h4NativeAttach struct {
	cmd    *exec.Cmd
	master *os.File
	done   chan error
}

func startH4NativeAttach(t *testing.T, binary, home, tmuxPath, handoffID string) *h4NativeAttach {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "session", "attach", "--handoff-id", handoffID)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.Env = h4NativeEnv(os.Environ(), map[string]string{
		"HOME": home, "PATH": filepath.Dir(tmuxPath) + ":/usr/bin:/bin:/usr/sbin:/sbin", "TERM": "xterm-256color", "SHELL": "/bin/sh",
	})
	if err := cmd.Start(); err != nil {
		_ = master.Close()
		_ = slave.Close()
		t.Fatal(err)
	}
	_ = slave.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	attach := &h4NativeAttach{cmd: cmd, master: master, done: done}
	t.Cleanup(func() {
		_ = master.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	})
	waitH4NativePTYContains(t, master, h4LocalWarning)
	return attach
}

func h4NativeAttachHome(t *testing.T, stateDir, runtimeDir string) string {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "Library", "Application Support", "ShellBeam", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("schema_version = 1\nstate_dir = %q\nruntime_dir = %q\nshell = %q\n", stateDir, runtimeDir, "/bin/sh")
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func waitH4NativeHandoffStatus(t *testing.T, client *ipcadapter.Client, handoffID string, want handoff.DerivedStatus) handoff.PublicState {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: fmt.Sprintf("h4-inspect-%d", time.Now().UnixNano()), Action: "inspect.handoff", HandoffID: handoffID})
		if err == nil && resp.OK && resp.Handoff != nil && resp.Handoff.Status == want {
			return *resp.Handoff
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("handoff %s did not reach %s", handoffID, want)
	return handoff.PublicState{}
}

func waitH4NativePTYContains(t *testing.T, f *os.File, needle string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var out strings.Builder
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 || fds[0].Revents&unix.POLLIN == 0 {
			continue
		}
		read, err := f.Read(buf)
		if read > 0 {
			out.Write(buf[:read])
			if strings.Contains(out.String(), needle) {
				return out.String()
			}
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("attach PTY missing %q: %q", needle, out.String())
	return ""
}

func h4NativeCanary(t *testing.T, label string) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return "SHELLBEAM_H4_" + label + "_" + hex.EncodeToString(raw[:])
}

func h4NativeEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, replaced := overrides[key]; replaced {
				continue
			}
		}
		out = append(out, entry)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}

func h4AssertNativeNoSecret(t *testing.T, surface string, value any, variants [][]byte) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	assertB1NoSecretVariants(t, surface, raw, variants)
}
