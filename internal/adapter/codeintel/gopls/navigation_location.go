package gopls

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const navigationMaxObservedSourceBytes = 64 << 10

type normalizedNavigationLocation struct {
	Location    core.SourceLocation
	Observation *appcodeintel.LocationObservation
}

func normalizeNavigationLocation(workspace workspacecore.Workspace, documents []synchronizedDocument, documentURI uri.URI, value protocol.Range, encoding protocol.PositionEncodingKind) (normalizedNavigationLocation, error) {
	for _, document := range documents {
		if document.URI != documentURI {
			continue
		}
		byteRange, err := lspadapter.RangeToByteRange(document.Bytes, value, encoding)
		if err != nil {
			return normalizedNavigationLocation{}, err
		}
		return normalizedNavigationLocation{Location: core.SourceLocation{
			Kind: core.LocationResolved,
			Resolved: &core.ResolvedSourceLocation{
				SourceRefID: string(document.SourceRef), StartByte: byteRange.Start, EndByte: byteRange.End,
			},
		}}, nil
	}
	if logicalPath, ok := repositoryLogicalPath(workspace.Root, documentURI); ok {
		return normalizeObservedRepositoryLocation(workspace, logicalPath, documentURI, value, encoding)
	}
	return normalizedNavigationLocation{Location: providerReportedExternalLocation(documentURI, value)}, nil
}

func normalizeObservedRepositoryLocation(workspace workspacecore.Workspace, logicalPath string, documentURI uri.URI, value protocol.Range, encoding protocol.PositionEncodingKind) (normalizedNavigationLocation, error) {
	absolute := documentURI.FsPath()
	bytes, ok := readRegularUTF8Bounded(absolute, navigationMaxObservedSourceBytes)
	if !ok {
		return normalizedNavigationLocation{Location: core.SourceLocation{
			Kind: core.LocationProviderReported,
			ProviderReported: &core.ProviderReportedLocation{
				Origin: core.OriginRepository, SanitizedLogicalPath: logicalPath,
				NormalizationQuality: core.NormalizationUnavailable,
			},
		}}, nil
	}
	byteRange, err := lspadapter.RangeToByteRange(bytes, value, encoding)
	if err != nil {
		return normalizedNavigationLocation{}, err
	}
	startLine, startColumn, err := core.ByteOffsetToDisplayPosition(bytes, byteRange.Start)
	if err != nil {
		return normalizedNavigationLocation{}, err
	}
	endLine, endColumn, err := core.ByteOffsetToDisplayPosition(bytes, byteRange.End)
	if err != nil {
		return normalizedNavigationLocation{}, err
	}
	location := core.SourceLocation{
		Kind: core.LocationProviderReported,
		ProviderReported: &core.ProviderReportedLocation{
			Origin: core.OriginRepository, SanitizedLogicalPath: logicalPath,
			Line: startLine, Column: startColumn, EndLine: endLine, EndColumn: endColumn,
			NormalizationQuality: core.NormalizationExact,
		},
	}
	return normalizedNavigationLocation{
		Location:    location,
		Observation: &appcodeintel.LocationObservation{LogicalPath: logicalPath, Bytes: append([]byte(nil), bytes...)},
	}, nil
}

func providerReportedExternalLocation(documentURI uri.URI, value protocol.Range) core.SourceLocation {
	origin, logicalPath := classifyExternalURI(documentURI)
	return core.SourceLocation{
		Kind: core.LocationProviderReported,
		ProviderReported: &core.ProviderReportedLocation{
			Origin: origin, SanitizedLogicalPath: logicalPath,
			Line: int(value.Start.Line) + 1, Column: int(value.Start.Character) + 1,
			EndLine: int(value.End.Line) + 1, EndColumn: int(value.End.Character) + 1,
			NormalizationQuality: core.NormalizationPartial,
		},
	}
}

func providerReportedURIOnly(workspace workspacecore.Workspace, documentURI uri.URI) core.SourceLocation {
	if logicalPath, ok := repositoryLogicalPath(workspace.Root, documentURI); ok {
		return core.SourceLocation{
			Kind: core.LocationProviderReported,
			ProviderReported: &core.ProviderReportedLocation{
				Origin: core.OriginRepository, SanitizedLogicalPath: logicalPath,
				NormalizationQuality: core.NormalizationUnavailable,
			},
		}
	}
	origin, logicalPath := classifyExternalURI(documentURI)
	return core.SourceLocation{
		Kind: core.LocationProviderReported,
		ProviderReported: &core.ProviderReportedLocation{
			Origin: origin, SanitizedLogicalPath: logicalPath,
			NormalizationQuality: core.NormalizationUnavailable,
		},
	}
}

func repositoryLogicalPath(root string, documentURI uri.URI) (string, bool) {
	if !strings.HasPrefix(string(documentURI), "file:") {
		return "", false
	}
	path := filepath.Clean(documentURI.FsPath())
	root = filepath.Clean(root)
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	logicalPath := filepath.ToSlash(relative)
	if strings.HasPrefix(logicalPath, "/") || strings.Contains(logicalPath, "../") {
		return "", false
	}
	return logicalPath, true
}

func classifyExternalURI(documentURI uri.URI) (core.Origin, string) {
	if !strings.HasPrefix(string(documentURI), "file:") {
		return core.OriginExternal, "provider-location"
	}
	path := filepath.ToSlash(filepath.Clean(documentURI.FsPath()))
	if index := strings.Index(path, "/pkg/mod/"); index >= 0 {
		logical := strings.TrimPrefix(path[index+1:], "/")
		if strings.Contains(logical, "golang.org/toolchain@") {
			return core.OriginToolchain, logical
		}
		return core.OriginDependency, logical
	}
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "provider-location"
	}
	return core.OriginExternal, base
}

func readRegularUTF8Bounded(path string, limit int) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 0 || info.Size() > int64(limit) {
		return nil, false
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	bytes, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(bytes) > limit || !utf8.Valid(bytes) {
		return nil, false
	}
	return bytes, true
}

func workspaceDocumentURI(root, logicalPath string) uri.URI {
	return uri.File(filepath.Join(root, filepath.FromSlash(logicalPath)))
}

func (n *navigationService) documentSymbols(request appcodeintel.ProviderRequest, documents []synchronizedDocument, documentURI uri.URI, result protocol.DocumentSymbolResult) (appcodeintel.ProviderResponse, error) {
	response := appcodeintel.ProviderResponse{Status: core.StatusReady}
	switch symbols := result.(type) {
	case protocol.DocumentSymbolSlice:
		for _, symbol := range symbols {
			if !n.appendDocumentSymbol(&response, request, documents, documentURI, symbol) {
				break
			}
		}
	case protocol.SymbolInformationSlice:
		for _, symbol := range symbols {
			if !n.appendSymbolInformation(&response, request, documents, symbol) {
				break
			}
		}
	}
	return response, nil
}

func (n *navigationService) appendDocumentSymbol(response *appcodeintel.ProviderResponse, request appcodeintel.ProviderRequest, documents []synchronizedDocument, documentURI uri.URI, symbol protocol.DocumentSymbol) bool {
	if len(response.Symbols) >= navigationMaxResults {
		response.Status = core.StatusPartial
		return false
	}
	normalized, err := normalizeNavigationLocation(request.Workspace, documents, documentURI, symbol.SelectionRange, n.encoding)
	if err != nil {
		response.Status = core.StatusPartial
		return true
	}
	response.Symbols = append(response.Symbols, appcodeintel.ProviderSymbol{
		Name: symbol.Name, Kind: symbolKindLabel(symbol.Kind), Location: normalized.Location, Observation: normalized.Observation,
		Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
	})
	for _, child := range symbol.Children {
		if !n.appendDocumentSymbol(response, request, documents, documentURI, child) {
			return false
		}
	}
	return true
}

func (n *navigationService) workspaceSymbols(request appcodeintel.ProviderRequest, documents []synchronizedDocument, result protocol.WorkspaceSymbolResult) (appcodeintel.ProviderResponse, error) {
	response := appcodeintel.ProviderResponse{Status: core.StatusReady}
	switch symbols := result.(type) {
	case protocol.SymbolInformationSlice:
		for _, symbol := range symbols {
			if !n.appendSymbolInformation(&response, request, documents, symbol) {
				break
			}
		}
	case protocol.WorkspaceSymbolSlice:
		for _, symbol := range symbols {
			if len(response.Symbols) >= navigationMaxResults {
				response.Status = core.StatusPartial
				break
			}
			providerSymbol := appcodeintel.ProviderSymbol{
				Name: symbol.Name, Kind: symbolKindLabel(symbol.Kind),
				Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
			}
			switch location := symbol.Location.(type) {
			case *protocol.Location:
				if location == nil {
					continue
				}
				normalized, err := normalizeNavigationLocation(request.Workspace, documents, location.URI, location.Range, n.encoding)
				if err != nil {
					response.Status = core.StatusPartial
					continue
				}
				providerSymbol.Location, providerSymbol.Observation = normalized.Location, normalized.Observation
			case *protocol.LocationUriOnly:
				if location == nil {
					continue
				}
				providerSymbol.Location = providerReportedURIOnly(request.Workspace, location.URI)
			default:
				response.Status = core.StatusPartial
				continue
			}
			response.Symbols = append(response.Symbols, providerSymbol)
		}
	}
	return response, nil
}

func (n *navigationService) appendSymbolInformation(response *appcodeintel.ProviderResponse, request appcodeintel.ProviderRequest, documents []synchronizedDocument, symbol protocol.SymbolInformation) bool {
	if len(response.Symbols) >= navigationMaxResults {
		response.Status = core.StatusPartial
		return false
	}
	normalized, err := normalizeNavigationLocation(request.Workspace, documents, symbol.Location.URI, symbol.Location.Range, n.encoding)
	if err != nil {
		response.Status = core.StatusPartial
		return true
	}
	response.Symbols = append(response.Symbols, appcodeintel.ProviderSymbol{
		Name: symbol.Name, Kind: symbolKindLabel(symbol.Kind), Location: normalized.Location, Observation: normalized.Observation,
		Authority: core.AuthorityMechanical, Completeness: core.CompletenessProviderReported,
	})
	return true
}

func symbolKindLabel(kind protocol.SymbolKind) string {
	switch kind {
	case protocol.SymbolKindFile:
		return "file"
	case protocol.SymbolKindModule:
		return "module"
	case protocol.SymbolKindNamespace:
		return "namespace"
	case protocol.SymbolKindPackage:
		return "package"
	case protocol.SymbolKindClass:
		return "class"
	case protocol.SymbolKindMethod:
		return "method"
	case protocol.SymbolKindProperty:
		return "property"
	case protocol.SymbolKindField:
		return "field"
	case protocol.SymbolKindConstructor:
		return "constructor"
	case protocol.SymbolKindEnum:
		return "enum"
	case protocol.SymbolKindInterface:
		return "interface"
	case protocol.SymbolKindFunction:
		return "function"
	case protocol.SymbolKindVariable:
		return "variable"
	case protocol.SymbolKindConstant:
		return "constant"
	case protocol.SymbolKindStruct:
		return "struct"
	case protocol.SymbolKindTypeParameter:
		return "type_parameter"
	default:
		return fmt.Sprintf("symbol_%d", kind)
	}
}
