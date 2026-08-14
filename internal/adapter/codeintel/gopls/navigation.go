package gopls

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

const (
	navigationMaxResults   = 128
	navigationMaxTextBytes = 16 << 10
	navigationMaxRoots     = 8
)

type navigationCapabilities struct {
	Definition       bool
	References       bool
	TypeDefinition   bool
	DocumentSymbols  bool
	WorkspaceSymbols bool
	Hover            bool
	CallHierarchy    bool
}

type navigationSession interface {
	Definition(context.Context, *protocol.DefinitionParams) (protocol.DefinitionResult, error)
	TypeDefinition(context.Context, *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error)
	References(context.Context, *protocol.ReferenceParams) ([]protocol.Location, error)
	DocumentSymbol(context.Context, *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error)
	Symbols(context.Context, *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error)
	Hover(context.Context, *protocol.HoverParams) (*protocol.Hover, error)
	PrepareCallHierarchy(context.Context, *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error)
	IncomingCalls(context.Context, *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error)
	OutgoingCalls(context.Context, *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error)
}

type navigationService struct {
	session      navigationSession
	capabilities navigationCapabilities
	encoding     protocol.PositionEncodingKind
}

func newNavigationService(session navigationSession, capabilities navigationCapabilities, encoding protocol.PositionEncodingKind) *navigationService {
	return &navigationService{session: session, capabilities: capabilities, encoding: lspadapter.EffectivePositionEncoding(encoding)}
}

func navigationCapabilitiesFromServer(capabilities protocol.ServerCapabilities) navigationCapabilities {
	return navigationCapabilities{
		Definition:       providerCapabilityEnabled(capabilities.DefinitionProvider),
		References:       providerCapabilityEnabled(capabilities.ReferencesProvider),
		TypeDefinition:   providerCapabilityEnabled(capabilities.TypeDefinitionProvider),
		DocumentSymbols:  providerCapabilityEnabled(capabilities.DocumentSymbolProvider),
		WorkspaceSymbols: providerCapabilityEnabled(capabilities.WorkspaceSymbolProvider),
		Hover:            providerCapabilityEnabled(capabilities.HoverProvider),
		CallHierarchy:    providerCapabilityEnabled(capabilities.CallHierarchyProvider),
	}
}

func providerCapabilityEnabled(value any) bool {
	if value == nil {
		return false
	}
	if boolean, ok := value.(protocol.Boolean); ok {
		return bool(boolean)
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() != reflect.Ptr || !reflected.IsNil()
}

func (n *navigationService) Query(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument) (appcodeintel.ProviderResponse, error) {
	switch request.Query.Kind {
	case core.QueryDefinition:
		if !n.capabilities.Definition {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		return n.definition(ctx, request, documents, false)
	case core.QueryReferences:
		if !n.capabilities.References {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		return n.references(ctx, request, documents)
	case core.QueryTypeDefinition:
		if !n.capabilities.TypeDefinition {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		return n.definition(ctx, request, documents, true)
	case core.QuerySymbols:
		return n.symbols(ctx, request, documents)
	case core.QueryTypeSummary:
		if !n.capabilities.Hover {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		return n.typeSummary(ctx, request, documents)
	case core.QueryCallers, core.QueryCallees:
		if !n.capabilities.CallHierarchy {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		return n.callHierarchy(ctx, request, documents)
	case core.QueryImportDeclarations, core.QueryResolvedImportTargets:
		return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
	default:
		return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
	}
}

func (n *navigationService) definition(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument, typeDefinition bool) (appcodeintel.ProviderResponse, error) {
	params, err := n.positionParams(request.Query, request.Workspace.Root, documents)
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	var result protocol.DefinitionResult
	if typeDefinition {
		result, err = n.session.TypeDefinition(ctx, &protocol.TypeDefinitionParams{TextDocumentPositionParams: params})
	} else {
		result, err = n.session.Definition(ctx, &protocol.DefinitionParams{TextDocumentPositionParams: params})
	}
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	locations := definitionLocations(result)
	response := appcodeintel.ProviderResponse{Status: core.StatusReady}
	for _, location := range locations {
		if len(response.Locations) >= navigationMaxResults {
			response.Status = core.StatusPartial
			break
		}
		normalized, normalizeErr := normalizeNavigationLocation(request.Workspace, documents, location.URI, location.Range, n.encoding)
		if normalizeErr != nil {
			response.Status = core.StatusPartial
			continue
		}
		response.Locations = append(response.Locations, appcodeintel.ProviderLocation{
			Location: normalized.Location, Observation: normalized.Observation,
			Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
		})
	}
	return response, nil
}

func (n *navigationService) references(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument) (appcodeintel.ProviderResponse, error) {
	params, err := n.positionParams(request.Query, request.Workspace.Root, documents)
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	locations, err := n.session.References(ctx, &protocol.ReferenceParams{
		TextDocumentPositionParams: params,
		Context:                    protocol.ReferenceContext{IncludeDeclaration: true},
	})
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	return n.normalizeLocations(request, documents, locations, "reference")
}

func (n *navigationService) symbols(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument) (appcodeintel.ProviderResponse, error) {
	if request.Query.Scope == core.ScopeFile {
		if !n.capabilities.DocumentSymbols {
			return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
		}
		document, err := queryDocument(request.Query, request.Workspace.Root, documents)
		if err != nil {
			return appcodeintel.ProviderResponse{}, err
		}
		result, err := n.session.DocumentSymbol(ctx, &protocol.DocumentSymbolParams{TextDocument: protocol.TextDocumentIdentifier{URI: document.URI}})
		if err != nil {
			return appcodeintel.ProviderResponse{}, err
		}
		return n.documentSymbols(request, documents, document.URI, result)
	}
	if !n.capabilities.WorkspaceSymbols {
		return appcodeintel.ProviderResponse{}, unsupportedQuery(request.Query.Kind)
	}
	result, err := n.session.Symbols(ctx, &protocol.WorkspaceSymbolParams{Query: ""})
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	return n.workspaceSymbols(request, documents, result)
}

func (n *navigationService) typeSummary(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument) (appcodeintel.ProviderResponse, error) {
	params, err := n.positionParams(request.Query, request.Workspace.Root, documents)
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	hover, err := n.session.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: params})
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	if hover == nil {
		return appcodeintel.ProviderResponse{Status: core.StatusReady}, nil
	}
	text := normalizeHoverText(hoverText(hover.Contents))
	if text == "" {
		return appcodeintel.ProviderResponse{Status: core.StatusReady}, nil
	}
	status := core.StatusReady
	if len(text) > navigationMaxTextBytes {
		text = boundNavigationText(text, navigationMaxTextBytes)
		status = core.StatusPartial
	}
	return appcodeintel.ProviderResponse{Status: status, TypeSummary: text}, nil
}

func (n *navigationService) callHierarchy(ctx context.Context, request appcodeintel.ProviderRequest, documents []synchronizedDocument) (appcodeintel.ProviderResponse, error) {
	params, err := n.positionParams(request.Query, request.Workspace.Root, documents)
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	roots, err := n.session.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{TextDocumentPositionParams: params})
	if err != nil {
		return appcodeintel.ProviderResponse{}, err
	}
	response := appcodeintel.ProviderResponse{Status: core.StatusReady}
	if len(roots) > navigationMaxRoots {
		roots = roots[:navigationMaxRoots]
		response.Status = core.StatusPartial
	}
	for _, root := range roots {
		if request.Query.Kind == core.QueryCallers {
			calls, callErr := n.session.IncomingCalls(ctx, &protocol.CallHierarchyIncomingCallsParams{Item: root})
			if callErr != nil {
				return appcodeintel.ProviderResponse{}, callErr
			}
			for _, call := range calls {
				if !n.appendCallLocation(&response, request, documents, call.From, "caller") {
					break
				}
			}
		} else {
			calls, callErr := n.session.OutgoingCalls(ctx, &protocol.CallHierarchyOutgoingCallsParams{Item: root})
			if callErr != nil {
				return appcodeintel.ProviderResponse{}, callErr
			}
			for _, call := range calls {
				if !n.appendCallLocation(&response, request, documents, call.To, "callee") {
					break
				}
			}
		}
	}
	return response, nil
}

func (n *navigationService) appendCallLocation(response *appcodeintel.ProviderResponse, request appcodeintel.ProviderRequest, documents []synchronizedDocument, item protocol.CallHierarchyItem, relationship string) bool {
	if len(response.Locations) >= navigationMaxResults {
		response.Status = core.StatusPartial
		return false
	}
	normalized, err := normalizeNavigationLocation(request.Workspace, documents, item.URI, item.SelectionRange, n.encoding)
	if err != nil {
		response.Status = core.StatusPartial
		return true
	}
	response.Locations = append(response.Locations, appcodeintel.ProviderLocation{
		Name: item.Name, Relationship: relationship, Location: normalized.Location, Observation: normalized.Observation,
		Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
	})
	return true
}

func (n *navigationService) normalizeLocations(request appcodeintel.ProviderRequest, documents []synchronizedDocument, locations []protocol.Location, relationship string) (appcodeintel.ProviderResponse, error) {
	response := appcodeintel.ProviderResponse{Status: core.StatusReady}
	for _, location := range locations {
		if len(response.Locations) >= navigationMaxResults {
			response.Status = core.StatusPartial
			break
		}
		normalized, err := normalizeNavigationLocation(request.Workspace, documents, location.URI, location.Range, n.encoding)
		if err != nil {
			response.Status = core.StatusPartial
			continue
		}
		response.Locations = append(response.Locations, appcodeintel.ProviderLocation{
			Relationship: relationship, Location: normalized.Location, Observation: normalized.Observation,
			Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
		})
	}
	return response, nil
}

func (n *navigationService) positionParams(query core.Query, workspaceRoot string, documents []synchronizedDocument) (protocol.TextDocumentPositionParams, error) {
	document, err := queryDocument(query, workspaceRoot, documents)
	if err != nil {
		return protocol.TextDocumentPositionParams{}, err
	}
	offset, err := core.DisplayPositionToByteOffset(document.Bytes, query.Line, query.Column)
	if err != nil {
		return protocol.TextDocumentPositionParams{}, &appcodeintel.Error{Code: appcodeintel.CodeLocationNotResolved, Cause: err}
	}
	position, err := lspadapter.ByteOffsetToPosition(document.Bytes, offset, n.encoding)
	if err != nil {
		return protocol.TextDocumentPositionParams{}, &appcodeintel.Error{Code: appcodeintel.CodeLocationNotResolved, Cause: err}
	}
	return protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: document.URI}, Position: position,
	}, nil
}

func queryDocument(query core.Query, workspaceRoot string, documents []synchronizedDocument) (synchronizedDocument, error) {
	if query.Path == "" {
		return synchronizedDocument{}, &appcodeintel.Error{Code: appcodeintel.CodeLocationNotResolved, Cause: fmt.Errorf("query path required")}
	}
	want := workspaceDocumentURI(workspaceRoot, query.Path)
	for _, document := range documents {
		if document.URI == want {
			return document, nil
		}
	}
	return synchronizedDocument{}, &appcodeintel.Error{Code: appcodeintel.CodeLocationNotResolved, Cause: fmt.Errorf("query source is not exactly synchronized")}
}

func definitionLocations(result protocol.DefinitionResult) []protocol.Location {
	switch value := result.(type) {
	case *protocol.Location:
		if value == nil {
			return nil
		}
		return []protocol.Location{*value}
	case protocol.LocationSlice:
		return append([]protocol.Location(nil), value...)
	case protocol.DefinitionLinkSlice:
		locations := make([]protocol.Location, 0, len(value))
		for _, link := range value {
			locations = append(locations, protocol.Location{URI: link.TargetURI, Range: link.TargetSelectionRange})
		}
		return locations
	default:
		return nil
	}
}

func unsupportedQuery(kind core.QueryKind) error {
	return &appcodeintel.Error{Code: appcodeintel.CodeQueryUnsupported, Retryable: false, Cause: fmt.Errorf("provider does not support %s", kind)}
}

func hoverText(contents protocol.HoverContents) string {
	switch value := contents.(type) {
	case protocol.String:
		return string(value)
	case *protocol.MarkupContent:
		if value != nil {
			return value.Value
		}
	case *protocol.MarkedStringWithLanguage:
		if value != nil {
			return value.Value
		}
	case protocol.MarkedStringSlice:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, markedStringText(item))
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func markedStringText(value protocol.MarkedString) string {
	switch item := value.(type) {
	case protocol.String:
		return string(item)
	case *protocol.MarkedStringWithLanguage:
		if item != nil {
			return item.Value
		}
	}
	return ""
}

func normalizeHoverText(value string) string {
	value = strings.ToValidUTF8(value, "")
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	return strings.Join(fields, " ")
}

func boundNavigationText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}

func (s *lspSemanticSession) Definition(ctx context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	return s.session.Server.Definition(ctx, params)
}
func (s *lspSemanticSession) TypeDefinition(ctx context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	return s.session.Server.TypeDefinition(ctx, params)
}
func (s *lspSemanticSession) References(ctx context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	return s.session.Server.References(ctx, params)
}
func (s *lspSemanticSession) DocumentSymbol(ctx context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	return s.session.Server.DocumentSymbol(ctx, params)
}
func (s *lspSemanticSession) Symbols(ctx context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	return s.session.Server.Symbols(ctx, params)
}
func (s *lspSemanticSession) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	return s.session.Server.Hover(ctx, params)
}
func (s *lspSemanticSession) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	return s.session.Server.PrepareCallHierarchy(ctx, params)
}
func (s *lspSemanticSession) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	return s.session.Server.IncomingCalls(ctx, params)
}
func (s *lspSemanticSession) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	return s.session.Server.OutgoingCalls(ctx, params)
}
