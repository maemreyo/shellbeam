package project

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	app "github.com/maemreyo/shellbeam/internal/app/project"
	core "github.com/maemreyo/shellbeam/internal/core/project"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type RepoPathValidator struct{}

func NewRepoPathValidator() *RepoPathValidator { return &RepoPathValidator{} }

func (v *RepoPathValidator) ValidatePath(ctx context.Context, record workspace.Workspace, definition core.ParameterDefinition, value string) (app.ParameterValidation, error) {
	if err := ctx.Err(); err != nil {
		return app.ParameterValidation{}, err
	}
	normalized, err := normalizeRepoRelative(value, definition.AllowLeadingDash)
	if err != nil {
		return app.ParameterValidation{}, err
	}
	full := filepath.Join(record.Root, filepath.FromSlash(normalized))
	resolvedRoot, resolvedTarget, info, err := observeContainedPath(record.Root, full)
	if err != nil {
		return app.ParameterValidation{}, err
	}
	if err := ensureContained(resolvedRoot, resolvedTarget); err != nil {
		return app.ParameterValidation{}, err
	}
	if err := validatePathExistence(definition.Exists, info); err != nil {
		return app.ParameterValidation{}, err
	}
	return app.ParameterValidation{Value: normalized, ObservationQuality: core.PathObservationExactAtBind}, nil
}

func normalizeRepoRelative(value string, allowLeadingDash bool) (string, error) {
	if value == "" || len(value) > core.MaxStringBytes || !utf8.ValidString(value) || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", fmt.Errorf("repo path must be repository-relative")
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return "", fmt.Errorf("repo path contains control characters")
		}
	}
	clean := path.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, "../") || !allowLeadingDash && strings.HasPrefix(clean, "-") {
		return "", fmt.Errorf("repo path escapes or is option-shaped")
	}
	return clean, nil
}

func observeContainedPath(root, target string) (string, string, os.FileInfo, error) {
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(filepath.Clean(target))
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(filepath.Clean(target))
	if err != nil {
		return "", "", nil, fmt.Errorf("stat repo path: %w", err)
	}
	return resolvedRoot, resolvedTarget, info, nil
}

func ensureContained(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolved repo path escapes workspace")
	}
	return nil
}

func validatePathExistence(rule core.PathExistence, info os.FileInfo) error {
	switch rule {
	case "", core.PathExistsAny:
		return nil
	case core.PathExistsFile:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("repo path is not a file")
		}
	case core.PathExistsDirectory:
		if !info.IsDir() {
			return fmt.Errorf("repo path is not a directory")
		}
	default:
		return fmt.Errorf("unsupported path existence rule")
	}
	return nil
}
