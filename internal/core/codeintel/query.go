package codeintel

import "fmt"

type QueryKind string
type Scope string

const ProviderGoSemantic = "go_semantic"

const (
	QueryDiagnostics           QueryKind = "diagnostics"
	QuerySymbols               QueryKind = "symbols"
	QueryDefinition            QueryKind = "definition"
	QueryReferences            QueryKind = "references"
	QueryImportDeclarations    QueryKind = "import_declarations"
	QueryResolvedImportTargets QueryKind = "resolved_import_targets"
	QueryTypeDefinition        QueryKind = "type_definition"
	QueryTypeSummary           QueryKind = "type_summary"
	QueryCallers               QueryKind = "callers"
	QueryCallees               QueryKind = "callees"

	ScopeFile         Scope = "file"
	ScopeChangedFiles Scope = "changed_files"
	ScopeWorkspace    Scope = "workspace"
)

type Query struct {
	Kind     QueryKind `json:"kind"`
	Scope    Scope     `json:"scope,omitempty"`
	Path     string    `json:"path,omitempty"`
	Line     int       `json:"line,omitempty"`
	Column   int       `json:"column,omitempty"`
	Provider string    `json:"provider,omitempty"`
}

func (k QueryKind) Validate() error {
	switch k {
	case QueryDiagnostics, QuerySymbols, QueryDefinition, QueryReferences,
		QueryImportDeclarations, QueryResolvedImportTargets, QueryTypeDefinition,
		QueryTypeSummary, QueryCallers, QueryCallees:
		return nil
	default:
		return fmt.Errorf("invalid code query kind %q", k)
	}
}

func (s Scope) Validate() error {
	switch s {
	case ScopeFile, ScopeChangedFiles, ScopeWorkspace:
		return nil
	default:
		return fmt.Errorf("invalid code query scope %q", s)
	}
}

func (q Query) Validate() error {
	if err := q.Kind.Validate(); err != nil {
		return err
	}
	if q.Provider != "" && (q.Provider != ProviderGoSemantic || !safeBoundedText(q.Provider, MaxProviderTextBytes)) {
		return fmt.Errorf("invalid code query provider")
	}
	if isPositionQuery(q.Kind) {
		return q.validatePositionQuery()
	}
	return q.validateScopedQuery()
}

func (q Query) validatePositionQuery() error {
	if q.Scope != "" || !safeLogicalPath(q.Path) || q.Line < 1 || q.Column < 1 {
		return fmt.Errorf("invalid positioned code query")
	}
	return nil
}

func (q Query) validateScopedQuery() error {
	if q.Line != 0 || q.Column != 0 {
		return fmt.Errorf("position not allowed for scoped code query")
	}
	if err := q.Scope.Validate(); err != nil {
		return err
	}
	if q.Kind == QueryImportDeclarations || q.Kind == QueryResolvedImportTargets {
		if q.Scope != ScopeFile || !safeLogicalPath(q.Path) {
			return fmt.Errorf("import query requires file scope")
		}
		return nil
	}
	if q.Scope == ScopeFile {
		if !safeLogicalPath(q.Path) {
			return fmt.Errorf("file query requires safe path")
		}
		return nil
	}
	if q.Path != "" {
		return fmt.Errorf("non-file query cannot carry path")
	}
	return nil
}

func isPositionQuery(kind QueryKind) bool {
	switch kind {
	case QueryDefinition, QueryReferences, QueryTypeDefinition, QueryTypeSummary, QueryCallers, QueryCallees:
		return true
	default:
		return false
	}
}
