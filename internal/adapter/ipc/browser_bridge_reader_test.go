package ipc

import (
	"context"
	"os"
	"testing"

	verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

func TestBrowserBridgeReaderSurfacesTransportFailure(t *testing.T) {
	reader := NewBrowserBridgeReader("/nonexistent/shellbeam-browser-bridge-test.sock")
	if _, _, err := reader.Activity(context.Background(), "wt"); err == nil {
		t.Fatal("expected transport error against missing socket")
	}
}

func TestBrowserBridgeReaderVerificationSendsCheckpointPhaseOverIPC(t *testing.T) {
	actions := &verificationActionsFixture{}
	runtimeDir, err := os.MkdirTemp("/tmp", "shellbeam-browser-bridge-verification-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeDir) })

	server, err := Listen(runtimeDir, actions)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve()

	reader := NewBrowserBridgeReader(server.SocketPath())
	got, ok, err := reader.Verification(context.Background(), "ws_01K00000000000000000000000", "activity-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got == nil {
		t.Fatalf("verification result ok=%v got=%#v", ok, got)
	}
	if actions.inspectReq.Phase != verificationcore.PhaseCheckpoint {
		t.Fatalf("phase sent over IPC=%q want %q", actions.inspectReq.Phase, verificationcore.PhaseCheckpoint)
	}
	if got.Phase != verificationcore.PhaseCheckpoint {
		t.Fatalf("response phase=%q want %q", got.Phase, verificationcore.PhaseCheckpoint)
	}
}
