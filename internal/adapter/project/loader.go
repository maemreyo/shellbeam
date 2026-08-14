package project

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/project"
)

type Loader struct{}

func NewLoader() *Loader { return &Loader{} }

func (l *Loader) Load(ctx context.Context, root string) core.LoadResult {
	if err := ctx.Err(); err != nil {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodePathEscape}
	}
	manifestPath := filepath.Join(root, ".shellbeam", "project.toml")
	info, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return core.LoadResult{State: core.LoadAbsent}
	}
	if err != nil {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, resolveErr := filepath.EvalSymlinks(manifestPath)
		if resolveErr != nil || !withinRoot(root, target) {
			return core.LoadResult{State: core.LoadInvalid, Code: core.CodePathEscape}
		}
		manifestPath = target
		info, err = os.Stat(manifestPath)
		if err != nil {
			return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
		}
	}
	if !info.Mode().IsRegular() {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	if info.Size() > core.MaxManifestBytes {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeTooLarge}
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, core.MaxManifestBytes+1))
	if err != nil {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeSchemaError}
	}
	if len(data) > core.MaxManifestBytes {
		return core.LoadResult{State: core.LoadInvalid, Code: core.CodeTooLarge}
	}
	parsed, err := core.Parse(data)
	if err != nil {
		return core.LoadResult{State: core.LoadInvalid, ManifestDigest: core.RawDigest(data), Code: core.ErrorCode(err)}
	}
	digest := core.RawDigest(data)
	return core.LoadResult{State: core.LoadValid, Parsed: &parsed, ManifestDigest: digest, DiscoveryFingerprint: digest}
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
