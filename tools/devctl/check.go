package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var forbiddenNames = map[string]bool{"utils": true, "helpers": true, "common": true, "shared": true, "base": true, "misc": true, "models": true}

func forbiddenPath(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if forbiddenNames[part] {
			return true
		}
	}
	return false
}

func checkFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	lines := 0
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines++
	}
	if err := s.Err(); err != nil {
		return err
	}
	limit := 500
	if strings.HasSuffix(path, "_test.go") {
		limit = 800
	}
	if lines > limit {
		return fmt.Errorf("%s has %d lines; hard cap %d", path, lines, limit)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}
	for _, d := range parsed.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		n := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		if n > 80 {
			return fmt.Errorf("%s function %s has %d lines; hard cap 80", path, fn.Name.Name, n)
		}
	}
	if err := checkImports(filepath.ToSlash(path), parsed); err != nil {
		return err
	}
	return nil
}

func checkImports(path string, file *ast.File) error {
	if strings.HasSuffix(path, "_test.go") {
		return nil
	}
	for _, spec := range file.Imports {
		imp := strings.Trim(spec.Path.Value, `"`)
		if strings.Contains(path, "/core/") && strings.Contains(imp, "/internal/app/") {
			return fmt.Errorf("core imports app: %s", path)
		}
		if strings.Contains(path, "/core/") && strings.Contains(imp, "/internal/adapter/") {
			return fmt.Errorf("core imports adapter: %s", path)
		}
		if strings.Contains(path, "/app/") && strings.Contains(imp, "/internal/adapter/") {
			return fmt.Errorf("app imports adapter: %s", path)
		}
		if i := strings.Index(path, "/adapter/"); i >= 0 && strings.Contains(imp, "/internal/adapter/") {
			from := strings.Split(path[i+len("/adapter/"):], "/")[0]
			parts := strings.Split(imp, "/internal/adapter/")
			if len(parts) == 2 {
				to := strings.Split(parts[1], "/")[0]
				if from != to {
					return fmt.Errorf("adapter %s imports sibling %s", from, to)
				}
			}
		}
	}
	return nil
}

func checkRepository(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && isIgnoredPath(filepath.ToSlash(rel)) {
			return filepath.SkipDir
		}
		if forbiddenPath(rel) {
			return fmt.Errorf("forbidden package path %s", rel)
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") {
			return checkFile(path)
		}
		return nil
	})
}
