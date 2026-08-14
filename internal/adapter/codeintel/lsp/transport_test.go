package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
)

func TestProcessTransportUsesTypedLSPAndSeparatesBoundedStderr(t *testing.T) {
	client, err := NewClient(ClientOptions{DiagnosticLimits: DiagnosticLimits{
		MaxURIs: 2, MaxDiagnosticsPerURI: 2, MaxMessageBytes: 64,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	env := append([]string(nil), os.Environ()...)
	env = append(env, "SHELLBEAM_LSP_HELPER=1", "SHELLBEAM_LSP_HELPER_WORKSPACE=expected")
	session, err := StartProcess(ctx, ProcessConfig{
		Executable:      os.Args[0],
		Args:            []string{"-test.run=TestLSPHelperProcess"},
		Dir:             t.TempDir(),
		Env:             env,
		StderrBytes:     64,
		ShutdownTimeout: 2 * time.Second,
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Server.Initialize(ctx, &protocol.InitializeParams{})
	if err != nil {
		_ = session.Close()
		t.Fatal(err)
	}
	state := session.SetInitializeResult(result)
	if state.PositionEncoding != protocol.PositionEncodingKindUTF8 || state.TextDocumentSync != protocol.TextDocumentSyncKindFull {
		_ = session.Close()
		t.Fatalf("state=%+v", state)
	}
	if err := session.Server.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		_ = session.Close()
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	stderr := session.Stderr()
	if len(stderr) > 64 {
		t.Fatalf("stderr exceeded bound: %d", len(stderr))
	}
	if !strings.Contains(stderr, "helper-marker") {
		t.Fatalf("stderr tail=%q", stderr)
	}
}

func TestProcessTransportKeepsPipesOpenForLateExitOutput(t *testing.T) {
	client, err := NewClient(ClientOptions{DiagnosticLimits: DiagnosticLimits{
		MaxURIs: 2, MaxDiagnosticsPerURI: 2, MaxMessageBytes: 64,
	}})
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "exit.marker")
	env := append([]string(nil), os.Environ()...)
	env = append(env,
		"SHELLBEAM_LSP_HELPER=1",
		"SHELLBEAM_LSP_HELPER_WORKSPACE=expected",
		"SHELLBEAM_LSP_HELPER_EXIT_MARKER="+marker,
	)
	session, err := StartProcess(t.Context(), ProcessConfig{
		Executable: os.Args[0], Args: []string{"-test.run=TestLSPHelperProcess"},
		Dir: t.TempDir(), Env: env, StderrBytes: 64, ShutdownTimeout: 2 * time.Second,
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Server.Initialize(t.Context(), &protocol.InitializeParams{}); err != nil {
		t.Fatal(err)
	}
	if err := session.Server.Initialized(t.Context(), &protocol.InitializedParams{}); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "exit-finished" {
		t.Fatalf("late exit output did not finish before transport close: data=%q err=%v", data, err)
	}
}

func TestProcessTransportRejectsInvalidProcessConfig(t *testing.T) {
	client, err := NewClient(ClientOptions{DiagnosticLimits: DiagnosticLimits{
		MaxURIs: 1, MaxDiagnosticsPerURI: 1, MaxMessageBytes: 16,
	}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []ProcessConfig{
		{},
		{Executable: os.Args[0], Dir: "relative", StderrBytes: 64, ShutdownTimeout: time.Second},
		{Executable: os.Args[0], Dir: t.TempDir(), StderrBytes: 0, ShutdownTimeout: time.Second},
		{Executable: os.Args[0], Dir: t.TempDir(), StderrBytes: 64, ShutdownTimeout: 0},
	}
	for i, config := range cases {
		if _, err := StartProcess(context.Background(), config, client); err == nil {
			t.Fatalf("case %d unexpectedly accepted", i)
		}
	}
}
