package project

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	app "github.com/maemreyo/shellbeam/internal/app/project"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const (
	goPackageProviderID      = "go-repo-package"
	goPackageProviderVersion = 1
)

type GoPackageValidator struct{}

func NewGoPackageValidator() *GoPackageValidator { return &GoPackageValidator{} }

func (v *GoPackageValidator) ValidatePackage(ctx context.Context, record workspace.Workspace, provider, value string) (app.ParameterValidation, error) {
	if err := ctx.Err(); err != nil {
		return app.ParameterValidation{}, err
	}
	if provider != "go" {
		return app.ParameterValidation{}, fmt.Errorf("repo package provider unavailable")
	}
	normalized, base, err := normalizeGoPackage(value)
	if err != nil {
		return app.ParameterValidation{}, err
	}
	full := filepath.Join(record.Root, filepath.FromSlash(base))
	resolvedRoot, resolvedTarget, info, err := observeContainedPath(record.Root, full)
	if err != nil {
		return app.ParameterValidation{}, err
	}
	if err := ensureContained(resolvedRoot, resolvedTarget); err != nil {
		return app.ParameterValidation{}, err
	}
	if !info.IsDir() {
		return app.ParameterValidation{}, fmt.Errorf("repo package base is not a directory")
	}
	return app.ParameterValidation{Value: normalized, ProviderID: goPackageProviderID, ProviderVersion: goPackageProviderVersion}, nil
}

func normalizeGoPackage(value string) (normalized string, base string, err error) {
	if !validPackageScalar(value) || strings.HasPrefix(value, "-") || strings.Contains(value, "\\") || filepath.IsAbs(value) || strings.HasPrefix(value, "/") {
		return "", "", fmt.Errorf("invalid go repo package")
	}
	if value == "." {
		return ".", ".", nil
	}
	if !strings.HasPrefix(value, "./") {
		return "", "", fmt.Errorf("go repo package must be dot-relative")
	}
	hasEllipsis := strings.HasSuffix(value, "/...")
	withoutEllipsis := value
	if hasEllipsis {
		withoutEllipsis = strings.TrimSuffix(value, "/...")
	}
	if strings.Contains(withoutEllipsis, "...") {
		return "", "", fmt.Errorf("invalid go package ellipsis")
	}
	rawBase := strings.TrimPrefix(withoutEllipsis, "./")
	for _, segment := range strings.Split(rawBase, "/") {
		if segment == ".." {
			return "", "", fmt.Errorf("go repo package contains parent traversal")
		}
	}
	clean := path.Clean(rawBase)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." && !hasEllipsis && value != "./." {
		return "", "", fmt.Errorf("go repo package escapes repository")
	}
	if hasEllipsis {
		if clean == "." {
			return "./...", ".", nil
		}
		return "./" + clean + "/...", clean, nil
	}
	if clean == "." {
		return ".", ".", nil
	}
	return "./" + clean, clean, nil
}

func validPackageScalar(value string) bool {
	if value == "" || len(value) > core.MaxStringBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
