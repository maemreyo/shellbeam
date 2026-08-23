package gopls

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

func TestNavigationDefinitionReferencesAndTypeDefinitionUseExactSelectedSource(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBB", "package p\nvar X = 1\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	location := protocol.Location{URI: doc.URI, Range: protocol.Range{
		Start: protocol.Position{Line: 1, Character: 4}, End: protocol.Position{Line: 1, Character: 5},
	}}
	fake := &navigationFakeSession{
		fakeSession: newFakeSession(),
		definition:  protocol.LocationSlice{location},
		references:  []protocol.Location{location},
		typeDef:     protocol.LocationSlice{location},
	}
	nav := newNavigationService(fake, navigationCapabilities{Definition: true, References: true, TypeDefinition: true}, protocol.PositionEncodingKindUTF8)
	for _, query := range []core.Query{
		{Kind: core.QueryDefinition, Path: "main.go", Line: 2, Column: 5},
		{Kind: core.QueryReferences, Path: "main.go", Line: 2, Column: 5},
		{Kind: core.QueryTypeDefinition, Path: "main.go", Line: 2, Column: 5},
	} {
		response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{Workspace: ws, Query: query, SelectedSources: []appcodeintel.BoundSource{source}}, []synchronizedDocument{doc})
		if err != nil {
			t.Fatalf("%s: %v", query.Kind, err)
		}
		if response.Status != core.StatusReady || len(response.Locations) != 1 {
			t.Fatalf("%s response=%+v", query.Kind, response)
		}
		resolved := response.Locations[0].Location.Resolved
		if resolved == nil || resolved.SourceRefID != string(source.Ref.ID) || resolved.StartByte != int64(len("package p\nvar ")) || resolved.EndByte != int64(len("package p\nvar X")) {
			t.Fatalf("%s location=%+v", query.Kind, response.Locations[0].Location)
		}
		if resolved.Display == nil || resolved.Display.Path != "main.go" || resolved.Display.Line != 2 || resolved.Display.Column != 5 || resolved.Display.EndLine != 2 || resolved.Display.EndColumn != 6 || resolved.Display.Preview != "var X = 1" {
			t.Fatalf("%s display=%+v", query.Kind, resolved.Display)
		}
	}
	if fake.lastPosition != (protocol.Position{Line: 1, Character: 4}) {
		t.Fatalf("query position=%+v", fake.lastPosition)
	}
}

func TestNavigationDocumentAndWorkspaceSymbolsAreProviderDerived(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	text := "package p\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBC", text)
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	fake := &navigationFakeSession{
		fakeSession: newFakeSession(),
		documentSymbols: protocol.DocumentSymbolSlice{{
			Name: "Hello", Kind: protocol.SymbolKindFunction,
			Range:          protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 1, Character: 15}},
			SelectionRange: protocol.Range{Start: protocol.Position{Line: 1, Character: 5}, End: protocol.Position{Line: 1, Character: 10}},
		}},
		workspaceSymbols: protocol.SymbolInformationSlice{{
			BaseSymbolInformation: protocol.BaseSymbolInformation{Name: "Hello", Kind: protocol.SymbolKindFunction},
			Location:              protocol.Location{URI: doc.URI, Range: protocol.Range{Start: protocol.Position{Line: 1, Character: 5}, End: protocol.Position{Line: 1, Character: 10}}},
		}},
	}
	nav := newNavigationService(fake, navigationCapabilities{DocumentSymbols: true, WorkspaceSymbols: true}, protocol.PositionEncodingKindUTF8)

	fileResponse, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QuerySymbols, Scope: core.ScopeFile, Path: "main.go"}, SelectedSources: []appcodeintel.BoundSource{source},
	}, []synchronizedDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(fileResponse.Symbols) != 1 || fileResponse.Symbols[0].Name != "Hello" || fileResponse.Symbols[0].Location.Resolved == nil {
		t.Fatalf("file symbols=%+v", fileResponse.Symbols)
	}

	workspaceResponse, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QuerySymbols, Scope: core.ScopeWorkspace},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaceResponse.Symbols) != 1 || workspaceResponse.Symbols[0].Location.Kind != core.LocationProviderReported {
		t.Fatalf("workspace symbols=%+v", workspaceResponse.Symbols)
	}
	reported := workspaceResponse.Symbols[0].Location.ProviderReported
	if reported == nil || reported.Origin != core.OriginRepository || reported.SanitizedLogicalPath != "main.go" || reported.NormalizationQuality != core.NormalizationExact {
		t.Fatalf("workspace symbol location=%+v", workspaceResponse.Symbols[0].Location)
	}
	if workspaceResponse.Symbols[0].Observation == nil || string(workspaceResponse.Symbols[0].Observation.Bytes) != text {
		t.Fatalf("workspace symbol observation=%+v", workspaceResponse.Symbols[0].Observation)
	}
}

func TestNavigationTypeSummaryUsesBoundedHoverText(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBD", "package p\nvar X = 1\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	fake := &navigationFakeSession{fakeSession: newFakeSession(), hover: &protocol.Hover{Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: strings.Repeat("x", navigationMaxTextBytes+100)}}}
	nav := newNavigationService(fake, navigationCapabilities{Hover: true}, protocol.PositionEncodingKindUTF8)
	response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QueryTypeSummary, Path: "main.go", Line: 2, Column: 5}, SelectedSources: []appcodeintel.BoundSource{source},
	}, []synchronizedDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != core.StatusPartial || len(response.TypeSummary) != navigationMaxTextBytes {
		t.Fatalf("response status=%q len=%d", response.Status, len(response.TypeSummary))
	}
}

func TestNavigationTypeSummarySanitizesMultilineHoverText(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBZ", "package p\nvar X = 1\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	fake := &navigationFakeSession{fakeSession: newFakeSession(), hover: &protocol.Hover{Contents: &protocol.MarkupContent{
		Kind: protocol.MarkupKindMarkdown, Value: "```go\nvar X int\n```\n\nA bounded summary.",
	}}}
	nav := newNavigationService(fake, navigationCapabilities{Hover: true}, protocol.PositionEncodingKindUTF8)
	response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QueryTypeSummary, Path: "main.go", Line: 2, Column: 5}, SelectedSources: []appcodeintel.BoundSource{source},
	}, []synchronizedDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if response.TypeSummary != "```go var X int ``` A bounded summary." {
		t.Fatalf("type summary=%q", response.TypeSummary)
	}
}

func TestNavigationCallHierarchyIsProviderReportedNeverExhaustive(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBE", "package p\nfunc A() {}\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	item := protocol.CallHierarchyItem{
		Name: "A", Kind: protocol.SymbolKindFunction, URI: doc.URI,
		Range:          protocol.Range{Start: protocol.Position{Line: 1}, End: protocol.Position{Line: 1, Character: 11}},
		SelectionRange: protocol.Range{Start: protocol.Position{Line: 1, Character: 5}, End: protocol.Position{Line: 1, Character: 6}},
	}
	fake := &navigationFakeSession{
		fakeSession: newFakeSession(), prepare: []protocol.CallHierarchyItem{item},
		incoming: []protocol.CallHierarchyIncomingCall{{From: item}},
		outgoing: []protocol.CallHierarchyOutgoingCall{{To: item}},
	}
	nav := newNavigationService(fake, navigationCapabilities{CallHierarchy: true}, protocol.PositionEncodingKindUTF8)
	for _, kind := range []core.QueryKind{core.QueryCallers, core.QueryCallees} {
		response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
			Workspace: ws, Query: core.Query{Kind: kind, Path: "main.go", Line: 2, Column: 6}, SelectedSources: []appcodeintel.BoundSource{source},
		}, []synchronizedDocument{doc})
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Locations) != 1 || response.Locations[0].Completeness != core.CompletenessProviderReported {
			t.Fatalf("%s locations=%+v", kind, response.Locations)
		}
		if response.Locations[0].Completeness == core.CompletenessExhaustive {
			t.Fatalf("%s incorrectly exhaustive", kind)
		}
	}
}

func TestNavigationUnsupportedCapabilityAndImportsFailExplicitly(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBF", "package p\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	nav := newNavigationService(&navigationFakeSession{fakeSession: newFakeSession()}, navigationCapabilities{}, protocol.PositionEncodingKindUTF8)
	queries := []core.Query{
		{Kind: core.QueryDefinition, Path: "main.go", Line: 1, Column: 1},
		{Kind: core.QueryResolvedImportTargets, Scope: core.ScopeFile, Path: "main.go"},
		{Kind: core.QueryImportDeclarations, Scope: core.ScopeFile, Path: "main.go"},
	}
	for _, query := range queries {
		_, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{Workspace: ws, Query: query, SelectedSources: []appcodeintel.BoundSource{source}}, []synchronizedDocument{doc})
		if appcodeintel.ErrorCode(err) != appcodeintel.CodeQueryUnsupported || appcodeintel.Retryable(err) {
			t.Fatalf("%s err=%v code=%q retryable=%v", query.Kind, err, appcodeintel.ErrorCode(err), appcodeintel.Retryable(err))
		}
	}
}

func TestNavigationExternalDependencyLocationNeverFabricatesCanonicalSourceRef(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	source := boundSource("main.go", "src_01ARZ3NDEKTSV4RRFFQ69G5FBG", "package p\nvar X = 1\n")
	doc := synchronizedDocument{URI: uri.File(filepath.Join(root, "main.go")), Version: 1, SourceRef: source.Ref.ID, LogicalPath: source.Ref.LogicalPath, Bytes: source.Bytes}
	external := filepath.Join(t.TempDir(), "pkg", "mod", "example.com", "dep@v1.0.0", "dep.go")
	if err := os.MkdirAll(filepath.Dir(external), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("package dep\nvar X = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &navigationFakeSession{
		fakeSession: newFakeSession(),
		definition:  protocol.LocationSlice{{URI: uri.File(external), Range: protocol.Range{Start: protocol.Position{Line: 1, Character: 4}, End: protocol.Position{Line: 1, Character: 5}}}},
	}
	nav := newNavigationService(fake, navigationCapabilities{Definition: true}, protocol.PositionEncodingKindUTF8)
	response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QueryDefinition, Path: "main.go", Line: 2, Column: 5}, SelectedSources: []appcodeintel.BoundSource{source},
	}, []synchronizedDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Locations) != 1 || response.Locations[0].Location.Kind != core.LocationProviderReported || response.Locations[0].Location.Resolved != nil {
		t.Fatalf("location=%+v", response.Locations)
	}
	reported := response.Locations[0].Location.ProviderReported
	if reported == nil || reported.Origin != core.OriginDependency || strings.Contains(reported.SanitizedLogicalPath, root) || filepath.IsAbs(reported.SanitizedLogicalPath) {
		t.Fatalf("reported=%+v", reported)
	}
}

type navigationFakeSession struct {
	*fakeSession
	definition       protocol.DefinitionResult
	typeDef          protocol.DefinitionResult
	references       []protocol.Location
	documentSymbols  protocol.DocumentSymbolResult
	workspaceSymbols protocol.WorkspaceSymbolResult
	hover            *protocol.Hover
	prepare          []protocol.CallHierarchyItem
	incoming         []protocol.CallHierarchyIncomingCall
	outgoing         []protocol.CallHierarchyOutgoingCall
	lastPosition     protocol.Position
}

func (s *navigationFakeSession) Definition(_ context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	s.lastPosition = params.Position
	return s.definition, nil
}

func (s *navigationFakeSession) TypeDefinition(_ context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	s.lastPosition = params.Position
	return s.typeDef, nil
}

func (s *navigationFakeSession) References(_ context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	s.lastPosition = params.Position
	return s.references, nil
}

func (s *navigationFakeSession) DocumentSymbol(context.Context, *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	return s.documentSymbols, nil
}

func (s *navigationFakeSession) Symbols(context.Context, *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	return s.workspaceSymbols, nil
}

func (s *navigationFakeSession) Hover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	s.lastPosition = params.Position
	return s.hover, nil
}

func (s *navigationFakeSession) PrepareCallHierarchy(_ context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	s.lastPosition = params.Position
	return s.prepare, nil
}

func (s *navigationFakeSession) IncomingCalls(context.Context, *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	return s.incoming, nil
}

func (s *navigationFakeSession) OutgoingCalls(context.Context, *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	return s.outgoing, nil
}

func TestNavigationWorkspaceObservationIsBounded(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	large := strings.Repeat("x", 70<<10)
	if err := os.WriteFile(filepath.Join(root, "large.go"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	documentURI := uri.File(filepath.Join(root, "large.go"))
	fake := &navigationFakeSession{
		fakeSession: newFakeSession(),
		workspaceSymbols: protocol.SymbolInformationSlice{{
			BaseSymbolInformation: protocol.BaseSymbolInformation{Name: "Large", Kind: protocol.SymbolKindVariable},
			Location:              protocol.Location{URI: documentURI, Range: protocol.Range{Start: protocol.Position{}, End: protocol.Position{Character: 1}}},
		}},
	}
	nav := newNavigationService(fake, navigationCapabilities{WorkspaceSymbols: true}, protocol.PositionEncodingKindUTF8)
	response, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws, Query: core.Query{Kind: core.QuerySymbols, Scope: core.ScopeWorkspace},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Symbols) != 1 {
		t.Fatalf("symbols=%+v", response.Symbols)
	}
	symbol := response.Symbols[0]
	if symbol.Observation != nil || symbol.Location.ProviderReported == nil || symbol.Location.ProviderReported.NormalizationQuality != core.NormalizationUnavailable {
		t.Fatalf("oversized workspace observation escaped bound: %+v", symbol)
	}
}

func TestNavigationPositionQueryRequiresExactSynchronizedSource(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(root)
	nav := newNavigationService(&navigationFakeSession{fakeSession: newFakeSession()}, navigationCapabilities{Definition: true}, protocol.PositionEncodingKindUTF8)
	_, err := nav.Query(t.Context(), appcodeintel.ProviderRequest{
		Workspace: ws,
		Query:     core.Query{Kind: core.QueryDefinition, Path: "missing.go", Line: 1, Column: 1},
	}, nil)
	if appcodeintel.ErrorCode(err) != appcodeintel.CodeLocationNotResolved || appcodeintel.Retryable(err) {
		t.Fatalf("err=%v code=%q retryable=%v", err, appcodeintel.ErrorCode(err), appcodeintel.Retryable(err))
	}
}
