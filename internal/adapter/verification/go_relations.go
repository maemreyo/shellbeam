package verification

import (
	"bufio"
	"context"
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/verification"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
	"github.com/maemreyo/shellbeam/internal/core/workspace"
)

type GoRelationLimits struct {
	MaxChangedPaths int
	MaxFiles        int
	MaxBytes        int64
	MaxRelations    int
	Timeout         time.Duration
}

func DefaultGoRelationLimits() GoRelationLimits {
	return GoRelationLimits{MaxChangedPaths: 256, MaxFiles: 2048, MaxBytes: 16 << 20, MaxRelations: 512, Timeout: 5 * time.Second}
}

type GoRelationProvider struct{ limits GoRelationLimits }

func NewGoRelationProvider(l GoRelationLimits) *GoRelationProvider {
	return &GoRelationProvider{limits: l}
}

type importEdge struct{ from, to string }

type goScanResult struct {
	imports     map[string]map[string]bool
	coverage    core.Coverage
	diagnostics []string
}

type goScanState struct {
	ctx         context.Context
	root        string
	nested      map[string]bool
	limits      GoRelationLimits
	deadline    time.Time
	files       int
	bytesRead   int64
	imports     map[string]map[string]bool
	coverage    core.Coverage
	diagnostics []string
}

var errGoRelationBound = errors.New("go relation bound reached")

func (p *GoRelationProvider) Derive(ctx context.Context, ws workspace.Workspace, generation string, changed []string) app.RelationResult {
	if !validGoRelationLimits(p.limits) {
		return app.RelationResult{Diagnostics: []string{"go_relation_invalid_limits"}}
	}
	deadline := time.Now().Add(p.limits.Timeout)
	module, err := readModulePath(filepath.Join(ws.Root, "go.mod"))
	if errors.Is(err, os.ErrNotExist) {
		return app.RelationResult{}
	}
	coverage := core.CoverageComplete
	diagnostics := []string{}
	if err != nil || module == "" {
		coverage = core.CoveragePartial
		diagnostics = append(diagnostics, "go_module_mapping_unavailable")
	}
	nested, nestedDiags := discoverNestedModules(ctx, ws.Root, deadline)
	if len(nestedDiags) > 0 {
		coverage = core.CoveragePartial
		diagnostics = append(diagnostics, nestedDiags...)
	}
	scan := scanGoImports(ctx, ws.Root, nested, p.limits, deadline)
	coverage = weakerCoverage(coverage, scan.coverage)
	diagnostics = append(diagnostics, scan.diagnostics...)
	graphModule := module
	if graphModule == "" {
		graphModule = "invalid.local/module"
	}
	reverse := buildReverseImports(graphModule, scan.imports, nested)
	changedDirs, changedLimited := selectChangedGoDirs(changed, nested, p.limits.MaxChangedPaths)
	if changedLimited {
		coverage = core.CoveragePartial
		diagnostics = append(diagnostics, "changed_path_limit_exceeded")
	}
	edges, relationLimited := reverseImportClosure(reverse, changedDirs, p.limits.MaxRelations)
	if relationLimited {
		coverage = core.CoveragePartial
		diagnostics = append(diagnostics, "go_relation_limit_exceeded")
	}
	return projectGoRelations(graphModule, generation, coverage, edges, diagnostics)
}

func validGoRelationLimits(l GoRelationLimits) bool {
	return l.MaxChangedPaths > 0 && l.MaxFiles > 0 && l.MaxBytes > 0 && l.MaxRelations > 0 && l.Timeout > 0
}

func discoverNestedModules(ctx context.Context, root string, deadline time.Time) (map[string]bool, []string) {
	nested := map[string]bool{}
	diagnostics := []string{}
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			diagnostics = append(diagnostics, "go_walk_error")
			return nil
		}
		if relationBudgetExpired(ctx, deadline) {
			return errGoRelationBound
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" && rel != "go.mod" {
			nested[path.Dir(rel)] = true
			diagnostics = append(diagnostics, "nested_go_module")
		}
		return nil
	})
	if errors.Is(err, errGoRelationBound) {
		diagnostics = append(diagnostics, "go_relation_work_bound_exceeded")
	} else if err != nil {
		diagnostics = append(diagnostics, "go_walk_error")
	}
	return nested, uniqueSorted(diagnostics)
}

func scanGoImports(ctx context.Context, root string, nested map[string]bool, limits GoRelationLimits, deadline time.Time) goScanResult {
	state := &goScanState{ctx: ctx, root: root, nested: nested, limits: limits, deadline: deadline, imports: map[string]map[string]bool{}, coverage: core.CoverageComplete}
	err := filepath.WalkDir(root, state.visit)
	if errors.Is(err, errGoRelationBound) {
		state.coverage = core.CoveragePartial
		state.diagnostics = append(state.diagnostics, "go_relation_work_bound_exceeded")
	} else if err != nil {
		state.coverage = core.CoveragePartial
		state.diagnostics = append(state.diagnostics, "go_walk_error")
	}
	return goScanResult{imports: state.imports, coverage: state.coverage, diagnostics: uniqueSorted(state.diagnostics)}
}

func (s *goScanState) visit(full string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		s.degrade("go_walk_error")
		return nil
	}
	if relationBudgetExpired(s.ctx, s.deadline) {
		return errGoRelationBound
	}
	rel, err := filepath.Rel(s.root, full)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	if d.IsDir() {
		if d.Name() == ".git" || d.Name() == "vendor" || (rel != "." && underNested(rel, s.nested)) {
			return filepath.SkipDir
		}
		return nil
	}
	if d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(rel, ".go") || underNested(rel, s.nested) {
		return nil
	}
	if s.files >= s.limits.MaxFiles {
		return errGoRelationBound
	}
	info, err := d.Info()
	if err != nil {
		s.degrade("go_file_stat_failed")
		return nil
	}
	if s.bytesRead+info.Size() > s.limits.MaxBytes {
		return errGoRelationBound
	}
	return s.readGoFile(full, rel)
}

func (s *goScanState) readGoFile(full, rel string) error {
	data, err := os.ReadFile(full)
	if err != nil {
		s.degrade("go_file_read_failed")
		return nil
	}
	s.files++
	s.bytesRead += int64(len(data))
	parsed, err := parser.ParseFile(token.NewFileSet(), rel, data, parser.ImportsOnly)
	if err != nil {
		s.degrade("go_parse_failed:" + rel)
		return nil
	}
	dir := path.Dir(rel)
	if s.imports[dir] == nil {
		s.imports[dir] = map[string]bool{}
	}
	for _, imp := range parsed.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err == nil {
			s.imports[dir][value] = true
		}
	}
	return nil
}

func (s *goScanState) degrade(code string) {
	s.coverage = core.CoveragePartial
	s.diagnostics = append(s.diagnostics, code)
}

func buildReverseImports(module string, pkgImports map[string]map[string]bool, nested map[string]bool) map[string][]string {
	reverse := map[string][]string{}
	for importer, imports := range pkgImports {
		for imp := range imports {
			target, ok := rootModuleImportDir(module, imp)
			if !ok || underNested(target, nested) {
				continue
			}
			reverse[target] = append(reverse[target], importer)
		}
	}
	for target := range reverse {
		sort.Strings(reverse[target])
	}
	return reverse
}

func selectChangedGoDirs(changed []string, nested map[string]bool, max int) ([]string, bool) {
	dirs := []string{}
	limited := false
	for _, changedPath := range changed {
		if len(dirs) >= max {
			limited = true
			break
		}
		if !strings.HasSuffix(changedPath, ".go") {
			continue
		}
		dir := path.Dir(changedPath)
		if !underNested(dir, nested) {
			dirs = append(dirs, dir)
		}
	}
	return uniqueSorted(dirs), limited
}

func reverseImportClosure(reverse map[string][]string, changedDirs []string, maxRelations int) ([]importEdge, bool) {
	edges := []importEdge{}
	seen := map[string]bool{}
	queue := append([]string(nil), changedDirs...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, importer := range reverse[current] {
			key := current + "\x00" + importer
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(edges) >= maxRelations {
				return edges, true
			}
			edges = append(edges, importEdge{from: current, to: importer})
			queue = append(queue, importer)
		}
	}
	return edges, false
}

func projectGoRelations(module, generation string, coverage core.Coverage, edges []importEdge, diagnostics []string) app.RelationResult {
	provider := &core.ProviderRef{ID: "go_static_import_graph", Version: 1}
	provenance := []string{"module:" + module}
	captured := time.Now().UTC()
	domain := core.AffectedDomain{Kind: core.DomainGoImportGraph, DerivationAuthority: core.AuthorityMechanical, Coverage: coverage, Provider: provider, SourceGeneration: generation, ProvenanceRefs: provenance, CapturedAt: captured}
	if generation == "" {
		domain.Coverage = core.CoverageUnknown
		domain.DomainID, _ = core.DomainIDWithoutGeneration(domain.Kind, provider, provenance)
	} else {
		domain.DomainID, _ = core.DomainID(domain.Kind, provider, generation, provenance)
	}
	out := app.RelationResult{Domains: []core.AffectedDomain{domain}}
	if generation != "" {
		out.Relations, diagnostics = projectImportEdges(module, generation, domain, edges, diagnostics)
	}
	sort.Slice(out.Relations, func(i, j int) bool { return out.Relations[i].RelationID < out.Relations[j].RelationID })
	out.Diagnostics = uniqueSorted(diagnostics)
	return out
}

func projectImportEdges(module, generation string, domain core.AffectedDomain, edges []importEdge, diagnostics []string) ([]core.AffectedRelation, []string) {
	out := make([]core.AffectedRelation, 0, len(edges))
	for _, edge := range edges {
		fromImport := moduleImportPath(module, edge.from)
		toImport := moduleImportPath(module, edge.to)
		refs := []string{"module:" + module, "target:" + fromImport, "importer:" + toImport}
		in := core.RelationIdentityInput{From: core.Subject{Kind: core.SubjectPackage, Value: fromImport}, To: core.Subject{Kind: core.SubjectPath, Value: edge.to}, Kind: "imported_by", Basis: core.BasisImportGraph, DerivationAuthority: core.AuthorityMechanical, Coverage: domain.Coverage, Provider: domain.Provider, SourceGeneration: generation, ProvenanceRefs: refs}
		id, err := core.RelationID(in)
		if err != nil {
			diagnostics = append(diagnostics, "go_relation_identity_failed")
			continue
		}
		out = append(out, core.AffectedRelation{RelationID: id, From: in.From, To: in.To, Kind: in.Kind, Basis: in.Basis, DerivationAuthority: in.DerivationAuthority, Coverage: in.Coverage, Provider: domain.Provider, SourceGeneration: generation, ProvenanceRefs: refs, CapturedAt: domain.CapturedAt})
	}
	return out, diagnostics
}

func readModulePath(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")), nil
		}
	}
	return "", scanner.Err()
}

func rootModuleImportDir(module, importPath string) (string, bool) {
	if module == "" || importPath == "" {
		return "", false
	}
	if importPath == module {
		return ".", true
	}
	prefix := module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	rel := path.Clean(strings.TrimPrefix(importPath, prefix))
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func moduleImportPath(module, dir string) string {
	if dir == "." {
		return module
	}
	return module + "/" + dir
}

func relationBudgetExpired(ctx context.Context, deadline time.Time) bool {
	return ctx.Err() != nil || time.Now().After(deadline)
}

func weakerCoverage(a, b core.Coverage) core.Coverage {
	if core.CoverageNoStrongerThan(a, b) {
		return a
	}
	return b
}

func underNested(rel string, nested map[string]bool) bool {
	for root := range nested {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func uniqueSorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for _, value := range out {
		if value == "" {
			continue
		}
		if n == 0 || out[n-1] != value {
			out[n] = value
			n++
		}
	}
	return out[:n]
}
