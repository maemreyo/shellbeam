package lsp

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type DiagnosticLimits struct {
	MaxURIs              int
	MaxDiagnosticsPerURI int
	MaxMessageBytes      int
}

type ClientOptions struct {
	DiagnosticLimits DiagnosticLimits
	Configuration    []protocol.LSPAny
	WorkspaceFolders []protocol.WorkspaceFolder
}

type NormalizedDiagnostic struct {
	Range    protocol.Range
	Severity protocol.DiagnosticSeverity
	Code     string
	Source   string
	Message  string
}

type DiagnosticNotification struct {
	URI         uri.URI
	Version     int32
	HasVersion  bool
	Sequence    uint64
	Diagnostics []NormalizedDiagnostic
	Truncated   bool
}

type CapabilityState struct {
	PositionEncoding protocol.PositionEncodingKind
	TextDocumentSync protocol.TextDocumentSyncKind
	Diagnostics      bool
	Definition       bool
	References       bool
	TypeDefinition   bool
	Symbols          bool
	WorkspaceFolders bool
	ServerName       string
	ServerVersion    string
}

type Client struct {
	protocol.UnimplementedClient

	mu               sync.Mutex
	limits           DiagnosticLimits
	configuration    []protocol.LSPAny
	workspaceFolders []protocol.WorkspaceFolder
	diagnostics      map[uri.URI]DiagnosticNotification
	sequence         uint64
	changed          chan struct{}
}

func NewClient(options ClientOptions) (*Client, error) {
	if err := options.DiagnosticLimits.Validate(); err != nil {
		return nil, err
	}
	configuration := make([]protocol.LSPAny, len(options.Configuration))
	for i, value := range options.Configuration {
		configuration[i] = cloneLSPAny(value)
	}
	return &Client{
		limits:           options.DiagnosticLimits,
		configuration:    configuration,
		workspaceFolders: append([]protocol.WorkspaceFolder(nil), options.WorkspaceFolders...),
		diagnostics:      make(map[uri.URI]DiagnosticNotification),
		changed:          make(chan struct{}),
	}, nil
}

func (l DiagnosticLimits) Validate() error {
	if l.MaxURIs < 1 || l.MaxURIs > 1024 ||
		l.MaxDiagnosticsPerURI < 1 || l.MaxDiagnosticsPerURI > 4096 ||
		l.MaxMessageBytes < 1 || l.MaxMessageBytes > 64*1024 {
		return fmt.Errorf("invalid diagnostic limits")
	}
	return nil
}

func (c *Client) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	if params == nil || params.URI == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sequence++
	notification := DiagnosticNotification{URI: params.URI, Sequence: c.sequence}
	if version, ok := params.Version.Get(); ok {
		notification.Version = version
		notification.HasVersion = true
	}
	limit := min(len(params.Diagnostics), c.limits.MaxDiagnosticsPerURI)
	notification.Diagnostics = make([]NormalizedDiagnostic, 0, limit)
	for i := 0; i < limit; i++ {
		diagnostic, truncated := normalizeDiagnostic(params.Diagnostics[i], c.limits.MaxMessageBytes)
		notification.Diagnostics = append(notification.Diagnostics, diagnostic)
		notification.Truncated = notification.Truncated || truncated
	}
	if len(params.Diagnostics) > limit {
		notification.Truncated = true
	}
	if _, exists := c.diagnostics[params.URI]; !exists && len(c.diagnostics) >= c.limits.MaxURIs {
		c.evictOldestDiagnosticLocked()
	}
	c.diagnostics[params.URI] = notification
	c.signalLocked()
	return nil
}

func (c *Client) Configuration(_ context.Context, params *protocol.ConfigurationParams) ([]protocol.LSPAny, error) {
	if params == nil {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]protocol.LSPAny, len(params.Items))
	for i := range out {
		if i < len(c.configuration) {
			out[i] = cloneLSPAny(c.configuration[i])
		} else {
			out[i] = protocol.LSPAny([]byte("null"))
		}
	}
	return out, nil
}

func (c *Client) WorkspaceFolders(context.Context) ([]protocol.WorkspaceFolder, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]protocol.WorkspaceFolder(nil), c.workspaceFolders...), nil
}

func (*Client) RegisterCapability(context.Context, *protocol.RegistrationParams) error { return nil }
func (*Client) UnregisterCapability(context.Context, *protocol.UnregistrationParams) error {
	return nil
}
func (*Client) WorkDoneProgressCreate(context.Context, *protocol.WorkDoneProgressCreateParams) error {
	return nil
}

func (c *Client) LatestDiagnostics(documentURI uri.URI) (DiagnosticNotification, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	notification, ok := c.diagnostics[documentURI]
	if !ok {
		return DiagnosticNotification{}, false
	}
	return cloneNotification(notification), true
}

func (c *Client) WaitDiagnostics(ctx context.Context, documentURI uri.URI, after uint64) (DiagnosticNotification, error) {
	for {
		c.mu.Lock()
		if notification, ok := c.diagnostics[documentURI]; ok && notification.Sequence > after {
			result := cloneNotification(notification)
			c.mu.Unlock()
			return result, nil
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return DiagnosticNotification{}, ctx.Err()
		case <-changed:
		}
	}
}

func NegotiateCapabilities(result *protocol.InitializeResult) CapabilityState {
	state := CapabilityState{PositionEncoding: protocol.PositionEncodingKindUTF16}
	if result == nil {
		return state
	}
	capabilities := result.Capabilities
	if capabilities.PositionEncoding != "" {
		state.PositionEncoding = capabilities.PositionEncoding
	}
	state.TextDocumentSync = textDocumentSyncKind(capabilities.TextDocumentSync)
	state.Diagnostics = capabilities.DiagnosticProvider != nil
	state.Definition = capabilityEnabled(capabilities.DefinitionProvider)
	state.References = capabilityEnabled(capabilities.ReferencesProvider)
	state.TypeDefinition = capabilityEnabled(capabilities.TypeDefinitionProvider)
	state.Symbols = capabilityEnabled(capabilities.DocumentSymbolProvider)
	if capabilities.Workspace != nil && capabilities.Workspace.WorkspaceFolders != nil {
		supported := capabilities.Workspace.WorkspaceFolders.Supported
		state.WorkspaceFolders = supported != nil && *supported
	}
	state.ServerName = result.ServerInfo.Name
	if version, ok := result.ServerInfo.Version.Get(); ok {
		state.ServerVersion = version
	}
	return state
}

func textDocumentSyncKind(syncValue protocol.TextDocumentSync) protocol.TextDocumentSyncKind {
	switch value := syncValue.(type) {
	case protocol.TextDocumentSyncKind:
		return value
	case *protocol.TextDocumentSyncOptions:
		if value != nil && value.Change != nil {
			return *value.Change
		}
	}
	return protocol.TextDocumentSyncKindNone
}

func capabilityEnabled(value any) bool {
	if value == nil {
		return false
	}
	if boolean, ok := value.(protocol.Boolean); ok {
		return bool(boolean)
	}
	return true
}

func normalizeDiagnostic(input protocol.Diagnostic, maxMessageBytes int) (NormalizedDiagnostic, bool) {
	message, messageTruncated := boundedUTF8(diagnosticMessage(input.Message), maxMessageBytes)
	code, codeTruncated := boundedUTF8(diagnosticCode(input.Code), maxMessageBytes)
	source, _ := input.Source.Get()
	source, sourceTruncated := boundedUTF8(source, maxMessageBytes)
	return NormalizedDiagnostic{
		Range: input.Range, Severity: input.Severity, Code: code, Source: source, Message: message,
	}, messageTruncated || codeTruncated || sourceTruncated
}

func diagnosticMessage(value protocol.InlayHintTooltip) string {
	switch typed := value.(type) {
	case protocol.String:
		return string(typed)
	case *protocol.MarkupContent:
		if typed != nil {
			return typed.Value
		}
	}
	return ""
}

func diagnosticCode(value protocol.ProgressToken) string {
	switch typed := value.(type) {
	case protocol.String:
		return string(typed)
	case protocol.Integer:
		return strconv.FormatInt(int64(typed), 10)
	}
	return ""
}

func boundedUTF8(value string, maxBytes int) (string, bool) {
	if len(value) <= maxBytes {
		return value, false
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end], true
}

func (c *Client) evictOldestDiagnosticLocked() {
	var oldestURI uri.URI
	var oldestSequence uint64
	for documentURI, notification := range c.diagnostics {
		if oldestURI == "" || notification.Sequence < oldestSequence {
			oldestURI = documentURI
			oldestSequence = notification.Sequence
		}
	}
	if oldestURI != "" {
		delete(c.diagnostics, oldestURI)
	}
}

func (c *Client) signalLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func cloneNotification(input DiagnosticNotification) DiagnosticNotification {
	input.Diagnostics = append([]NormalizedDiagnostic(nil), input.Diagnostics...)
	return input
}

func cloneLSPAny(value protocol.LSPAny) protocol.LSPAny {
	return append(protocol.LSPAny(nil), value...)
}
