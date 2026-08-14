package lsp

import (
	"context"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestClientDispatchesDiagnosticsAndServerInitiatedConfiguration(t *testing.T) {
	client, err := NewClient(ClientOptions{
		DiagnosticLimits: DiagnosticLimits{MaxURIs: 4, MaxDiagnosticsPerURI: 4, MaxMessageBytes: 64},
		Configuration:    []protocol.LSPAny{protocol.LSPAny([]byte(`"configured"`))},
		WorkspaceFolders: []protocol.WorkspaceFolder{{URI: uri.URI("file:///workspace"), Name: "workspace"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	clientStream, serverStream := jsonrpc2.NewChannelStreamPair(4)
	_, clientConn, _ := protocol.NewClient(ctx, client, clientStream)
	defer func() { _ = clientConn.Close() }()
	_, serverConn, callback := protocol.NewServer(ctx, protocol.UnimplementedServer{}, serverStream)
	defer func() { _ = serverConn.Close() }()

	section := "gopls"
	configuration, err := callback.Configuration(ctx, &protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{{Section: &section}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration) != 1 || string(configuration[0]) != `"configured"` {
		t.Fatalf("configuration=%#v", configuration)
	}
	folders, err := callback.WorkspaceFolders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].Name != "workspace" {
		t.Fatalf("folders=%#v", folders)
	}

	testURI := uri.URI("file:///workspace/main.go")
	if err := callback.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:     testURI,
		Version: protocol.NewOptional[int32](7),
		Diagnostics: []protocol.Diagnostic{{
			Range:    protocol.Range{Start: protocol.Position{Line: 1, Character: 2}, End: protocol.Position{Line: 1, Character: 4}},
			Severity: protocol.DiagnosticSeverityWarning,
			Code:     protocol.String("compiler"),
			Source:   protocol.NewOptional("gopls"),
			Message:  protocol.String("undefined: Thing"),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	notification, err := client.WaitDiagnostics(ctx, testURI, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !notification.HasVersion || notification.Version != 7 || notification.Sequence == 0 {
		t.Fatalf("notification=%+v", notification)
	}
	if len(notification.Diagnostics) != 1 || notification.Diagnostics[0].Message != "undefined: Thing" {
		t.Fatalf("diagnostics=%+v", notification.Diagnostics)
	}
}

func TestClientDiagnosticBufferIsBoundedAndWaitable(t *testing.T) {
	client, err := NewClient(ClientOptions{
		DiagnosticLimits: DiagnosticLimits{MaxURIs: 1, MaxDiagnosticsPerURI: 2, MaxMessageBytes: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstURI := uri.URI("file:///workspace/first.go")
	secondURI := uri.URI("file:///workspace/second.go")
	params := &protocol.PublishDiagnosticsParams{
		URI: firstURI,
		Diagnostics: []protocol.Diagnostic{
			{Message: protocol.String("0123456789")},
			{Message: protocol.String("abcdef")},
			{Message: protocol.String("discarded")},
		},
	}
	if err := client.PublishDiagnostics(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	first, ok := client.LatestDiagnostics(firstURI)
	if !ok || len(first.Diagnostics) != 2 || !first.Truncated {
		t.Fatalf("first=%+v ok=%v", first, ok)
	}
	if first.Diagnostics[0].Message != "01234" || first.Diagnostics[1].Message != "abcde" {
		t.Fatalf("messages=%+v", first.Diagnostics)
	}

	waited := make(chan DiagnosticNotification, 1)
	errCh := make(chan error, 1)
	go func(after uint64) {
		notification, waitErr := client.WaitDiagnostics(t.Context(), secondURI, after)
		if waitErr != nil {
			errCh <- waitErr
			return
		}
		waited <- notification
	}(first.Sequence)
	if err := client.PublishDiagnostics(t.Context(), &protocol.PublishDiagnosticsParams{URI: secondURI}); err != nil {
		t.Fatal(err)
	}
	select {
	case notification := <-waited:
		if notification.URI != secondURI || notification.Sequence <= first.Sequence {
			t.Fatalf("notification=%+v", notification)
		}
	case waitErr := <-errCh:
		t.Fatal(waitErr)
	case <-time.After(time.Second):
		t.Fatal("diagnostic waiter was not notified")
	}
	if _, ok := client.LatestDiagnostics(firstURI); ok {
		t.Fatal("oldest diagnostic URI was not evicted")
	}
}

func TestNegotiateCapabilitiesKeepsOnlyBoundedProviderFacts(t *testing.T) {
	supported := true
	result := &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:       protocol.TextDocumentSyncKindIncremental,
			DefinitionProvider:     protocol.Boolean(true),
			ReferencesProvider:     protocol.Boolean(true),
			TypeDefinitionProvider: protocol.Boolean(true),
			DocumentSymbolProvider: protocol.Boolean(true),
			DiagnosticProvider:     &protocol.DiagnosticOptions{},
			Workspace: &protocol.WorkspaceOptions{WorkspaceFolders: &protocol.WorkspaceFoldersServerCapabilities{
				Supported: &supported,
			}},
		},
		ServerInfo: protocol.ServerInfo{Name: "gopls", Version: protocol.NewOptional("v0.test")},
	}
	state := NegotiateCapabilities(result)
	if state.PositionEncoding != protocol.PositionEncodingKindUTF16 {
		t.Fatalf("position encoding=%q", state.PositionEncoding)
	}
	if state.TextDocumentSync != protocol.TextDocumentSyncKindIncremental {
		t.Fatalf("sync=%v", state.TextDocumentSync)
	}
	if !state.Diagnostics || !state.Definition || !state.References || !state.TypeDefinition || !state.Symbols || !state.WorkspaceFolders {
		t.Fatalf("state=%+v", state)
	}
	if state.ServerName != "gopls" || state.ServerVersion != "v0.test" {
		t.Fatalf("server=%q version=%q", state.ServerName, state.ServerVersion)
	}
}
