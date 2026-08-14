package gopls

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/oklog/ulid/v2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type semanticSession interface {
	Initialize(context.Context, *protocol.InitializeParams) (*protocol.InitializeResult, error)
	Initialized(context.Context, *protocol.InitializedParams) error
	DidOpen(context.Context, *protocol.DidOpenTextDocumentParams) error
	DidChange(context.Context, *protocol.DidChangeTextDocumentParams) error
	DidClose(context.Context, *protocol.DidCloseTextDocumentParams) error
	DidChangeWatchedFiles(context.Context, *protocol.DidChangeWatchedFilesParams) error
	LatestDiagnostics(uri.URI) (lspadapter.DiagnosticNotification, bool)
	WaitDiagnostics(context.Context, uri.URI, uint64) (lspadapter.DiagnosticNotification, error)
	Capabilities() lspadapter.CapabilityState
	SetCapabilities(lspadapter.CapabilityState)
	Close() error
}

type Provider struct {
	mu         sync.Mutex
	workspace  workspacecore.Workspace
	session    semanticSession
	metadata   core.ProviderMetadata
	syncer     *documentSync
	collector  *diagnosticCollector
	navigation *navigationService
	closed     bool
}

func startProvider(ctx context.Context, workspace workspacecore.Workspace, options appcodeintel.ProviderStartOptions, config Config, session semanticSession) (*Provider, error) {
	params := initializeParams(workspace)
	result, err := session.Initialize(ctx, params)
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("initialize gopls: %w", err)
	}
	capabilities := lspadapter.NegotiateCapabilities(result)
	session.SetCapabilities(capabilities)
	if capabilities.TextDocumentSync == protocol.TextDocumentSyncKindNone {
		_ = session.Close()
		return nil, fmt.Errorf("gopls does not support text document synchronization")
	}
	if !supportedPositionEncoding(capabilities.PositionEncoding) {
		_ = session.Close()
		return nil, fmt.Errorf("unsupported gopls position encoding %q", capabilities.PositionEncoding)
	}
	if err := session.Initialized(ctx, &protocol.InitializedParams{}); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("initialize gopls session: %w", err)
	}
	syncer, err := newDocumentSync(session, config.SyncLimits)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	metadata := core.ProviderMetadata{
		ProviderID:           core.ProviderGoSemantic,
		Incarnation:          "gopls_" + ulid.Make().String(),
		ExecutableVersion:    capabilities.ServerVersion,
		ConfigFingerprint:    options.ConfigFingerprint,
		BuildFingerprint:     options.BuildFingerprint,
		BuildQuality:         config.BuildQuality,
		Coverage:             core.SyncExactForKnownPaths,
		SemanticScopeQuality: "workspace_root",
	}
	if err := metadata.Validate(); err != nil {
		_ = session.Close()
		return nil, err
	}
	provider := &Provider{
		workspace: workspace,
		session:   session,
		metadata:  metadata,
		syncer:    syncer,
		collector: newDiagnosticCollector(session, capabilities.PositionEncoding, config.DiagnosticWait),
	}
	if navigationSession, ok := session.(navigationSession); ok {
		provider.navigation = newNavigationService(navigationSession, navigationCapabilitiesFromServer(result.Capabilities), capabilities.PositionEncoding)
	}
	return provider, nil
}

func initializeParams(workspace workspacecore.Workspace) *protocol.InitializeParams {
	root := workspaceURI(workspace)
	configuration, workspaceFolders := true, true
	processID := int32(os.Getpid())
	return &protocol.InitializeParams{
		ProcessID: &processID,
		ClientInfo: protocol.ClientInfo{
			Name:    "shellbeam",
			Version: protocol.NewOptional("e29"),
		},
		RootURI: &root,
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: root, Name: workspace.Label}}),
		},
		Capabilities: protocol.ClientCapabilities{
			Workspace: &protocol.WorkspaceClientCapabilities{
				Configuration:    &configuration,
				WorkspaceFolders: &workspaceFolders,
			},
			General: &protocol.GeneralClientCapabilities{
				PositionEncodings: []protocol.PositionEncodingKind{
					protocol.PositionEncodingKindUTF8,
					protocol.PositionEncodingKindUTF16,
				},
			},
		},
	}
}

func (p *Provider) Metadata() core.ProviderMetadata {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.metadata
}

func (p *Provider) Query(ctx context.Context, request appcodeintel.ProviderRequest) (appcodeintel.ProviderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return appcodeintel.ProviderResponse{}, fmt.Errorf("gopls provider closed")
	}
	if request.Workspace.ID != p.workspace.ID || request.Workspace.Root != p.workspace.Root {
		return appcodeintel.ProviderResponse{}, fmt.Errorf("gopls workspace mismatch")
	}
	views := make([]boundSourceView, 0, len(request.SelectedSources))
	for _, source := range request.SelectedSources {
		views = append(views, boundSourceView{Source: source})
	}
	documents, err := p.syncer.Synchronize(ctx, request.Workspace, views, request.Sample)
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	var response appcodeintel.ProviderResponse
	if request.Query.Kind == core.QueryDiagnostics {
		response = p.collector.Collect(ctx, documents)
	} else {
		if p.navigation == nil {
			return appcodeintel.ProviderResponse{}, &appcodeintel.Error{Code: appcodeintel.CodeQueryUnsupported, Cause: fmt.Errorf("navigation session unavailable")}
		}
		response, err = p.navigation.Query(ctx, request, documents)
		if err != nil {
			return appcodeintel.ProviderResponse{}, err
		}
	}
	response.Metadata = p.metadata
	if p.syncer.CoveragePartial() {
		response.Metadata.Coverage = core.SyncPartial
	}
	return response, nil
}

func (p *Provider) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	return p.session.Close()
}

func workspaceURI(workspace workspacecore.Workspace) uri.URI {
	return uri.File(workspace.Root)
}

func supportedPositionEncoding(encoding protocol.PositionEncodingKind) bool {
	switch encoding {
	case protocol.PositionEncodingKindUTF8, protocol.PositionEncodingKindUTF16, protocol.PositionEncodingKindUTF32:
		return true
	default:
		return false
	}
}

type lspSemanticSession struct {
	session *lspadapter.Session
	mu      sync.Mutex
	state   lspadapter.CapabilityState
}

func newLSPSemanticSession(session *lspadapter.Session) *lspSemanticSession {
	return &lspSemanticSession{session: session}
}

func (s *lspSemanticSession) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return s.session.Server.Initialize(ctx, params)
}

func (s *lspSemanticSession) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	return s.session.Server.Initialized(ctx, params)
}

func (s *lspSemanticSession) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	return s.session.Server.DidOpen(ctx, params)
}

func (s *lspSemanticSession) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	return s.session.Server.DidChange(ctx, params)
}

func (s *lspSemanticSession) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	return s.session.Server.DidClose(ctx, params)
}

func (s *lspSemanticSession) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	return s.session.Server.DidChangeWatchedFiles(ctx, params)
}

func (s *lspSemanticSession) LatestDiagnostics(documentURI uri.URI) (lspadapter.DiagnosticNotification, bool) {
	return s.session.Client.LatestDiagnostics(documentURI)
}

func (s *lspSemanticSession) WaitDiagnostics(ctx context.Context, documentURI uri.URI, after uint64) (lspadapter.DiagnosticNotification, error) {
	return s.session.Client.WaitDiagnostics(ctx, documentURI, after)
}

func (s *lspSemanticSession) Capabilities() lspadapter.CapabilityState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *lspSemanticSession) SetCapabilities(state lspadapter.CapabilityState) {
	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
}

func (s *lspSemanticSession) Close() error { return s.session.Close() }

var _ appcodeintel.Provider = (*Provider)(nil)
var _ appcodeintel.ProviderFactory = (*Factory)(nil)
var _ appcodeintel.ProviderOptionsResolver = (*Factory)(nil)
