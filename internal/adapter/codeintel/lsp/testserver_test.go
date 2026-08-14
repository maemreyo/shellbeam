package lsp

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

func TestLSPHelperProcess(t *testing.T) {
	if os.Getenv("SHELLBEAM_LSP_HELPER") != "1" {
		return
	}
	if os.Getenv("SHELLBEAM_LSP_HELPER_WORKSPACE") != "expected" {
		fmt.Fprintln(os.Stderr, "bad helper environment")
		return
	}
	fmt.Fprint(os.Stderr, strings.Repeat("x", 96)+"helper-marker")
	ctx, cancel := context.WithCancel(context.Background())
	server := &helperLSPServer{cancel: cancel}
	_, conn, _ := protocol.NewServer(ctx, server, jsonrpc2.NewStream(&helperStdio{}))
	<-conn.Done()
	os.Exit(0)
}

type helperLSPServer struct {
	protocol.UnimplementedServer
	cancel context.CancelFunc
}

func (s *helperLSPServer) Initialize(context.Context, *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			PositionEncoding: protocol.PositionEncodingKindUTF8,
			TextDocumentSync: protocol.TextDocumentSyncKindFull,
		},
		ServerInfo: protocol.ServerInfo{Name: "helper", Version: protocol.NewOptional("v1")},
	}, nil
}

func (*helperLSPServer) Initialized(context.Context, *protocol.InitializedParams) error { return nil }
func (*helperLSPServer) Shutdown(context.Context) error                                 { return nil }
func (s *helperLSPServer) Exit(context.Context) error {
	s.cancel()
	return nil
}

type helperStdio struct{}

func (*helperStdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (*helperStdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (*helperStdio) Close() error                { return nil }

var _ io.ReadWriteCloser = (*helperStdio)(nil)
