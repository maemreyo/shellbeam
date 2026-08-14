package gopls

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestStartProviderInitializesBoundedDiskWorkspaceCapabilities(t *testing.T) {
	session := newFakeSession()
	factory := newTestFactory(t, session)
	ws := testWorkspace(t.TempDir())
	options, err := factory.Resolve(t.Context(), ws, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := factory.Start(t.Context(), ws, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = provider.Close() }()

	if session.initializeCalls != 1 || session.initializedCalls != 1 {
		t.Fatalf("initialize=%d initialized=%d", session.initializeCalls, session.initializedCalls)
	}
	params := session.initializeParams
	if params == nil || params.RootURI == nil || *params.RootURI != uri.File(ws.Root) {
		t.Fatalf("root uri=%v", params)
	}
	folders, ok := params.WorkspaceFolders.Get()
	if !ok || len(folders) != 1 || folders[0].URI != uri.File(ws.Root) {
		t.Fatalf("workspace folders=%#v", params.WorkspaceFolders)
	}
	if params.Capabilities.Workspace == nil || params.Capabilities.Workspace.Configuration == nil || !*params.Capabilities.Workspace.Configuration {
		t.Fatalf("workspace capabilities=%+v", params.Capabilities.Workspace)
	}
	if params.Capabilities.Workspace.DidChangeWatchedFiles != nil {
		t.Fatal("client unexpectedly opted into server-side file watching")
	}
	encodings := params.Capabilities.General.PositionEncodings
	if len(encodings) != 2 || encodings[0] != protocol.PositionEncodingKindUTF8 || encodings[1] != protocol.PositionEncodingKindUTF16 {
		t.Fatalf("position encodings=%v", encodings)
	}

	metadata := provider.Metadata()
	if metadata.ProviderID != core.ProviderGoSemantic || metadata.ExecutableVersion != "v0.test" ||
		metadata.ConfigFingerprint != options.ConfigFingerprint || metadata.BuildFingerprint != options.BuildFingerprint ||
		metadata.Incarnation == "" {
		t.Fatalf("metadata=%+v", metadata)
	}
}

func TestProviderCloseOwnsSessionAndMetadataIsStable(t *testing.T) {
	session := newFakeSession()
	factory := newTestFactory(t, session)
	ws := testWorkspace(t.TempDir())
	options, err := factory.Resolve(t.Context(), ws, core.Query{Kind: core.QueryDiagnostics, Scope: core.ScopeWorkspace, Provider: core.ProviderGoSemantic})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := factory.Start(t.Context(), ws, options)
	if err != nil {
		t.Fatal(err)
	}
	before := provider.Metadata()
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if session.closeCalls != 1 {
		t.Fatalf("close calls=%d", session.closeCalls)
	}
	if after := provider.Metadata(); after != before {
		t.Fatalf("metadata changed after close: before=%+v after=%+v", before, after)
	}
}

type fakeSession struct {
	mu sync.Mutex

	initializeResult *protocol.InitializeResult
	initializeParams *protocol.InitializeParams
	initializeCalls  int
	initializedCalls int
	closeCalls       int

	openCalls    []*protocol.DidOpenTextDocumentParams
	changeCalls  []*protocol.DidChangeTextDocumentParams
	closeDocs    []*protocol.DidCloseTextDocumentParams
	watchedCalls []*protocol.DidChangeWatchedFilesParams

	capabilities lspadapter.CapabilityState
	diagnostics  map[uri.URI][]lspadapter.DiagnosticNotification
}

func newFakeSession() *fakeSession {
	return &fakeSession{
		initializeResult: &protocol.InitializeResult{
			Capabilities: protocol.ServerCapabilities{
				PositionEncoding: protocol.PositionEncodingKindUTF8,
				TextDocumentSync: protocol.TextDocumentSyncKindFull,
			},
			ServerInfo: protocol.ServerInfo{Name: "gopls", Version: protocol.NewOptional("v0.test")},
		},
		capabilities: lspadapter.CapabilityState{
			PositionEncoding: protocol.PositionEncodingKindUTF8,
			TextDocumentSync: protocol.TextDocumentSyncKindFull,
			Diagnostics:      true,
		},
		diagnostics: make(map[uri.URI][]lspadapter.DiagnosticNotification),
	}
}

func (s *fakeSession) Initialize(_ context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializeCalls++
	s.initializeParams = params
	return s.initializeResult, nil
}

func (s *fakeSession) Initialized(context.Context, *protocol.InitializedParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializedCalls++
	return nil
}

func (s *fakeSession) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openCalls = append(s.openCalls, params)
	return nil
}

func (s *fakeSession) DidChange(_ context.Context, params *protocol.DidChangeTextDocumentParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changeCalls = append(s.changeCalls, params)
	return nil
}

func (s *fakeSession) DidClose(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeDocs = append(s.closeDocs, params)
	return nil
}

func (s *fakeSession) DidChangeWatchedFiles(_ context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.watchedCalls = append(s.watchedCalls, params)
	return nil
}

func (s *fakeSession) Capabilities() lspadapter.CapabilityState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capabilities
}

func (s *fakeSession) SetCapabilities(state lspadapter.CapabilityState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.capabilities = state
}

func (s *fakeSession) LatestDiagnostics(documentURI uri.URI) (lspadapter.DiagnosticNotification, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.diagnostics[documentURI]
	if len(values) == 0 {
		return lspadapter.DiagnosticNotification{}, false
	}
	return values[len(values)-1], true
}

func (s *fakeSession) WaitDiagnostics(ctx context.Context, documentURI uri.URI, after uint64) (lspadapter.DiagnosticNotification, error) {
	for {
		s.mu.Lock()
		for _, notification := range s.diagnostics[documentURI] {
			if notification.Sequence > after {
				s.mu.Unlock()
				return notification, nil
			}
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return lspadapter.DiagnosticNotification{}, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *fakeSession) pushDiagnostics(documentURI uri.URI, notification lspadapter.DiagnosticNotification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diagnostics[documentURI] = append(s.diagnostics[documentURI], notification)
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

func newTestFactory(t *testing.T, session *fakeSession) *Factory {
	t.Helper()
	config := DefaultConfig()
	config.ConfigFingerprint = "cfg_test"
	config.BuildFingerprint = "build_test"
	factory, err := newFactory(config, factoryDeps{
		lookPath: func(name string) (string, error) {
			if name != "gopls" {
				return "", fmt.Errorf("unexpected executable %q", name)
			}
			return "/tool/gopls", nil
		},
		executableIdentity: func(path string) (string, error) { return "exec_identity", nil },
		isGoWorkspace:      func(string) bool { return true },
		startSession: func(context.Context, sessionStart) (semanticSession, error) {
			return session, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func testWorkspace(root string) workspacecore.Workspace {
	now := time.Now().UTC()
	return workspacecore.Workspace{
		SchemaVersion: workspacecore.SchemaVersion,
		ID:            workspacecore.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PM"),
		RepositoryID:  workspacecore.RepositoryID("repo_01KZZ8AJJYRPX53ZX04P2NB9PM"),
		Label:         "test",
		Root:          root,
		GitDir:        root + "/.git",
		CreatedAt:     now,
		LastSeenAt:    now,
	}
}

func boundSource(path, id, text string) appcodeintel.BoundSource {
	return appcodeintel.BoundSource{
		Ref: core.SourceRef{
			ID: core.SourceRefID(id), Origin: core.SourceWorkspace,
			RepositoryID: workspacecore.RepositoryID("repo_01KZZ8AJJYRPX53ZX04P2NB9PM"),
			WorkspaceID:  workspacecore.WorkspaceID("ws_01KZZ8AJJYRPX53ZX04P2NB9PM"),
			LogicalPath:  path, ResolutionQuality: core.ResolutionExact, TextEncoding: core.TextEncodingUTF8,
		},
		Bytes: []byte(text),
	}
}
