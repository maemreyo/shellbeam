package codeintel

import "testing"

func TestQueryValidateAcceptsPromotedModelVocabulary(t *testing.T) {
	valid := []Query{
		{Kind: QueryDiagnostics, Scope: ScopeChangedFiles},
		{Kind: QueryDiagnostics, Scope: ScopeWorkspace},
		{Kind: QueryDiagnostics, Scope: ScopeFile, Path: "internal/app/service.go"},
		{Kind: QuerySymbols, Scope: ScopeFile, Path: "internal/app/service.go"},
		{Kind: QuerySymbols, Scope: ScopeWorkspace},
		{Kind: QueryDefinition, Path: "internal/app/service.go", Line: 12, Column: 7},
		{Kind: QueryReferences, Path: "internal/app/service.go", Line: 12, Column: 7},
		{Kind: QueryImportDeclarations, Scope: ScopeFile, Path: "internal/app/service.go"},
		{Kind: QueryResolvedImportTargets, Scope: ScopeFile, Path: "internal/app/service.go"},
		{Kind: QueryTypeDefinition, Path: "internal/app/service.go", Line: 12, Column: 7},
		{Kind: QueryTypeSummary, Path: "internal/app/service.go", Line: 12, Column: 7},
		{Kind: QueryCallers, Path: "internal/app/service.go", Line: 12, Column: 7},
		{Kind: QueryCallees, Path: "internal/app/service.go", Line: 12, Column: 7, Provider: "go_semantic"},
	}
	for i, query := range valid {
		if err := query.Validate(); err != nil {
			t.Fatalf("valid[%d] rejected: %#v: %v", i, query, err)
		}
	}
}

func TestQueryValidateRejectsProtocolLeakageShapes(t *testing.T) {
	invalid := []Query{
		{},
		{Kind: "hover"},
		{Kind: QueryDiagnostics},
		{Kind: QueryDiagnostics, Scope: "repository"},
		{Kind: QueryDiagnostics, Scope: ScopeFile},
		{Kind: QueryDiagnostics, Scope: ScopeWorkspace, Path: "main.go"},
		{Kind: QueryDefinition, Path: "main.go"},
		{Kind: QueryDefinition, Path: "main.go", Line: 1},
		{Kind: QueryDefinition, Scope: ScopeFile, Path: "main.go", Line: 1, Column: 1},
		{Kind: QueryImportDeclarations, Scope: ScopeFile, Path: "main.go", Line: 1, Column: 1},
		{Kind: QuerySymbols, Scope: ScopeWorkspace, Line: 1, Column: 1},
		{Kind: QueryDiagnostics, Scope: ScopeFile, Path: "../escape.go"},
		{Kind: QueryDiagnostics, Scope: ScopeFile, Path: "/private/main.go"},
		{Kind: QueryDiagnostics, Scope: ScopeWorkspace, Provider: "bad\nprovider"},
	}
	for i, query := range invalid {
		if err := query.Validate(); err == nil {
			t.Fatalf("invalid[%d] accepted: %#v", i, query)
		}
	}
}

func TestQueryKindsAreClosedAndClassified(t *testing.T) {
	for _, kind := range []QueryKind{
		QueryDiagnostics,
		QuerySymbols,
		QueryDefinition,
		QueryReferences,
		QueryImportDeclarations,
		QueryResolvedImportTargets,
		QueryTypeDefinition,
		QueryTypeSummary,
		QueryCallers,
		QueryCallees,
	} {
		if err := kind.Validate(); err != nil {
			t.Fatalf("promoted kind %q rejected: %v", kind, err)
		}
	}
	if err := QueryKind("raw_lsp").Validate(); err == nil {
		t.Fatal("raw provider protocol kind accepted")
	}
}
