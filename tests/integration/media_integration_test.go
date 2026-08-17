//go:build linux || darwin

package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	mediaBinaryOnce sync.Once
	mediaBinaryPath string
	mediaBinaryErr  error
)

func TestMediaRealDaemonMCPPrivacySentinelNeverPersists(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	runtimeDir, err := os.MkdirTemp("/tmp", "sb-rm-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })
	canonicalToken := "CANON_" + randomMediaToken(t)
	payloadToken := "PAYLOAD_" + randomMediaToken(t) + "_GPS_21.0285_105.8542"
	canonicalRoot := filepath.Join(root, canonicalToken)
	aliasRoot := filepath.Join(root, "selected")
	if err := os.MkdirAll(canonicalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonicalRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	imageBytes := privacyPNG(t, []byte(payloadToken))
	if err := os.WriteFile(filepath.Join(canonicalRoot, "probe.png"), imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	daemonLog := filepath.Join(root, "daemon.log")
	daemon := startMediaDaemon(t, stateDir, runtimeDir, daemonLog)
	if !daemon.ready(t) {
		t.Fatalf("daemon not ready: %s", daemon.log(t))
	}
	mcpLog := filepath.Join(root, "mcp.log")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.Command(mediaTestBinary(t), "mcp", "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh")
	log, err := os.OpenFile(mcpLog, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = log
	client := mcpgo.NewClient(&mcpgo.Implementation{Name: "rich-media-privacy-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcpgo.CommandTransport{Command: cmd}, nil)
	if err != nil {
		_ = log.Close()
		t.Fatalf("connect MCP: %v", err)
	}

	result, err := session.CallTool(ctx, &mcpgo.CallToolParams{
		Name:      "local_shell",
		Arguments: json.RawMessage(`{"action":"read_media","cwd":` + mustJSON(t, aliasRoot) + `,"path":"probe.png"}`),
	})
	if err != nil {
		_ = session.Close()
		_ = log.Close()
		t.Fatalf("read_media: %v", err)
	}
	if result.IsError {
		_ = session.Close()
		_ = log.Close()
		t.Fatalf("read_media returned error: %#v", result)
	}
	var gotImage *mcpgo.ImageContent
	for _, content := range result.Content {
		if imageContent, ok := content.(*mcpgo.ImageContent); ok {
			if gotImage != nil {
				t.Fatalf("multiple ImageContent values: %#v", result.Content)
			}
			gotImage = imageContent
		}
	}
	if gotImage == nil || gotImage.MIMEType != "image/png" || !bytes.Equal(gotImage.Data, imageBytes) {
		t.Fatalf("native image did not preserve original encoded bytes: %#v", gotImage)
	}
	structured, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{payloadToken, canonicalToken, canonicalRoot} {
		if bytes.Contains(structured, []byte(forbidden)) {
			t.Fatalf("structured content leaked %q: %s", forbidden, structured)
		}
	}
	if !bytes.Contains(structured, []byte(aliasRoot)) {
		t.Fatalf("structured content lost exact caller display address: %s", structured)
	}

	if err := session.Close(); err != nil {
		t.Fatalf("close MCP: %v", err)
	}
	_ = log.Close()
	daemon.stop(t)

	for _, path := range []string{stateDir, daemonLog, mcpLog} {
		assertTreeOmits(t, path, payloadToken, canonicalToken, canonicalRoot)
	}
}

func TestMediaOrdinaryExecutionLeavesReaderAndAdmissionUntouched(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store, err := storeadapter.Open(stateRoot, storeadapter.Limits{MaxSessions: 4, MaxSessionOutput: 1 << 20, MaxTotalState: 1 << 24, ControlReserve: 1 << 10})
	if err != nil {
		t.Fatal(err)
	}
	reader := &blockingIntegrationMediaReader{entered: make(chan struct{}, media.MaxConcurrentReads), release: make(chan struct{})}
	svc := daemonapp.NewService(store, processadapter.Owner{}, daemonapp.Options{
		Incarnation: "media-hot-path", Shell: "/bin/sh", MaxQueuedInputBytes: 1024,
		TerminationGrace: 50 * time.Millisecond, MediaReader: reader,
	})
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })

	view, err := svc.Start(context.Background(), daemonapp.StartRequest{ProtocolVersion: 2, OperationID: "media-no-tax", Command: "cat", CWD: t.TempDir(), StdinMode: operation.StdinModeStream, YieldMS: 5, MaxOutputBytes: 256})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Write(context.Background(), daemonapp.WriteRequest{SessionID: view.SessionID, InputOffset: 0, Chars: "ordinary\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Poll(context.Background(), daemonapp.PollRequest{SessionID: view.SessionID, Cursor: 0, YieldMS: 20, MaxOutputBytes: 256}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.InspectServer(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveProcessSession(context.Background(), view.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Kill(context.Background(), daemonapp.KillRequest{SessionID: view.SessionID, KillID: "media-no-tax-kill", Signal: "TERM"}); err != nil {
		t.Fatal(err)
	}
	if got := reader.calls.Load(); got != 0 {
		t.Fatalf("ordinary shell path entered media reader %d times", got)
	}

	done := make(chan error, media.MaxConcurrentReads)
	for i := 0; i < media.MaxConcurrentReads; i++ {
		go func(i int) {
			_, err := svc.ReadMedia(context.Background(), daemonapp.MediaRequest{CWD: t.TempDir(), Path: "probe.png"})
			done <- err
		}(i)
	}
	for i := 0; i < media.MaxConcurrentReads; i++ {
		select {
		case <-reader.entered:
		case <-time.After(time.Second):
			t.Fatal("ordinary execution consumed a media admission slot")
		}
	}
	if _, err := svc.ReadMedia(context.Background(), daemonapp.MediaRequest{CWD: t.TempDir(), Path: "probe.png"}); !errors.Is(err, failure.CapacityExceeded) {
		t.Fatalf("expected only the two deliberate reads to fill media admission, got %v", err)
	}
	close(reader.release)
	for i := 0; i < media.MaxConcurrentReads; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if got := reader.calls.Load(); got != media.MaxConcurrentReads {
		t.Fatalf("media reader calls=%d want=%d", got, media.MaxConcurrentReads)
	}
}

type blockingIntegrationMediaReader struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (r *blockingIntegrationMediaReader) Read(ctx context.Context, _ string, _ media.LogicalPath, _ media.Limits) (media.File, error) {
	r.calls.Add(1)
	r.entered <- struct{}{}
	select {
	case <-r.release:
		return media.File{MIMEType: "image/png", Format: "png", Width: 1, Height: 1, Data: privacyPNGBytes()}, nil
	case <-ctx.Done():
		return media.File{}, ctx.Err()
	}
}

type mediaDaemonProcess struct {
	cmd     *exec.Cmd
	client  *ipcadapter.Client
	logPath string
	waited  bool
}

func startMediaDaemon(t *testing.T, stateDir, runtimeDir, logPath string) *mediaDaemonProcess {
	t.Helper()
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(mediaTestBinary(t), "daemon", "--state-dir", stateDir, "--runtime-dir", runtimeDir, "--shell", "/bin/sh")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		t.Fatal(err)
	}
	_ = log.Close()
	p := &mediaDaemonProcess{cmd: cmd, client: ipcadapter.NewClient(filepath.Join(runtimeDir, "daemon.sock")), logPath: logPath}
	t.Cleanup(func() { p.stop(t) })
	return p
}

func (p *mediaDaemonProcess) ready(t *testing.T) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			return false
		}
		response, err := p.client.CallV2(context.Background(), ipcadapter.RequestV2{IPVersion: 2, Kind: "request", RequestID: "media-ready", Action: "inspect.server"})
		if err == nil && response.OK {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func (p *mediaDaemonProcess) stop(t *testing.T) {
	t.Helper()
	if p == nil || p.waited || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
		t.Error("media daemon did not stop")
	}
	p.waited = true
}

func (p *mediaDaemonProcess) log(t *testing.T) string {
	t.Helper()
	data, _ := os.ReadFile(p.logPath)
	return string(data)
}

func mediaTestBinary(t *testing.T) string {
	t.Helper()
	mediaBinaryOnce.Do(func() {
		root, err := filepath.Abs("../..")
		if err != nil {
			mediaBinaryErr = err
			return
		}
		dir, err := os.MkdirTemp("/tmp", "shellbeam-rich-media-integration-")
		if err != nil {
			mediaBinaryErr = err
			return
		}
		mediaBinaryPath = filepath.Join(dir, "shellbeam")
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", mediaBinaryPath, "./cmd/shellbeam")
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			mediaBinaryErr = errors.New(err.Error() + ": " + string(output))
		}
	})
	if mediaBinaryErr != nil {
		t.Fatalf("build shellbeam: %v", mediaBinaryErr)
	}
	return mediaBinaryPath
}

func privacyPNG(t *testing.T, trailing []byte) []byte {
	t.Helper()
	data := privacyPNGBytes()
	return append(data, trailing...)
}

func privacyPNGBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

func randomMediaToken(t *testing.T) string {
	t.Helper()
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(data[:])
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertTreeOmits(t *testing.T, root string, forbidden ...string) {
	t.Helper()
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	check := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if value != "" && bytes.Contains(data, []byte(value)) {
				t.Fatalf("private media sentinel leaked into %s: %q", path, value)
			}
		}
	}
	if !info.IsDir() {
		check(root)
		return
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			check(path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
