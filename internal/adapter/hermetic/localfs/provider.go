//go:build linux || darwin

package localfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
	"github.com/oklog/ulid/v2"
)

type Provider struct {
	privateRoot  string
	newCaptureID func() string
	afterCopy    func(int, string) error
}

func New(privateRoot string) *Provider {
	return &Provider{
		privateRoot: filepath.Clean(privateRoot),
		newCaptureID: func() string {
			return "hcap_" + ulid.Make().String()
		},
	}
}

func (p *Provider) Capture(ctx context.Context, req app.ProviderCaptureRequest) (_ app.CapturedView, retErr error) {
	if err := validateProviderRequest(ctx, p, req); err != nil {
		return app.CapturedView{}, err
	}
	selectors, err := canonicalSelectors(req.Selectors)
	if err != nil {
		return app.CapturedView{}, err
	}
	selected, err := selectSourceFiles(ctx, req.Root, selectors, req.Limits)
	if err != nil {
		return app.CapturedView{}, err
	}
	baseline, err := baselineSourceFiles(ctx, req.Root, selected, req.Limits)
	if err != nil {
		return app.CapturedView{}, err
	}
	captureID := p.newCaptureID()
	layout, err := createPrivateLayout(p.privateRoot, captureID)
	if err != nil {
		return app.CapturedView{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = discardCaptureDir(layout.captureDir)
		}
	}()
	manifest, err := p.copySelected(ctx, layout.root, req, selectors, selected, baseline)
	if err != nil {
		return app.CapturedView{}, err
	}
	if err := verifySourceUnchanged(ctx, req.Root, selectors, req.Limits, selected, manifest); err != nil {
		return app.CapturedView{}, err
	}
	if err := freezePrivateTree(layout.root); err != nil {
		return app.CapturedView{}, err
	}
	cleanup = false
	return app.CapturedView{CaptureID: captureID, PrivateRoot: layout.root, Manifest: manifest}, nil
}

func (p *Provider) copySelected(ctx context.Context, privateRoot string, req app.ProviderCaptureRequest, selectors []string, selected []selectedFile, baseline []sourceIdentity) (core.CaptureManifest, error) {
	reader, err := openSourceRoot(req.Root)
	if err != nil {
		return core.CaptureManifest{}, err
	}
	defer reader.Close()
	entries := make([]core.CaptureEntry, 0, len(selected))
	total := int64(0)
	for i, source := range selected {
		if err := ctx.Err(); err != nil {
			return core.CaptureManifest{}, err
		}
		data, executable, digest, err := reader.ReadRegular(source.Path, req.Limits.MaxFileBytes)
		if err != nil {
			return core.CaptureManifest{}, err
		}
		if i >= len(baseline) || !baseline[i].matches(source.Path, int64(len(data)), digest, executable) {
			return core.CaptureManifest{}, fmt.Errorf("hermetic source changed before copy")
		}
		total += int64(len(data))
		if total > req.Limits.MaxTotalBytes {
			return core.CaptureManifest{}, fmt.Errorf("hermetic capture byte budget exceeded")
		}
		if err := writePrivateFile(privateRoot, source.Path, data, executable); err != nil {
			return core.CaptureManifest{}, err
		}
		entries = append(entries, core.CaptureEntry{Path: source.Path, Size: int64(len(data)), SHA256: digest, Executable: executable})
		if p.afterCopy != nil {
			if err := p.afterCopy(i+1, source.Path); err != nil {
				return core.CaptureManifest{}, err
			}
		}
	}
	manifest := core.CaptureManifest{
		SchemaVersion: core.CaptureManifestSchemaVersion, WorkspaceID: req.WorkspaceID,
		SourceGeneration: req.SourceGeneration, Selectors: append([]string(nil), selectors...), Entries: entries, TotalBytes: total,
	}
	return manifest.Canonical()
}

func (p *Provider) Discard(ctx context.Context, view app.CapturedView) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || !validCaptureID(view.CaptureID) {
		return fmt.Errorf("invalid hermetic capture identity")
	}
	wantRoot := filepath.Join(p.privateRoot, view.CaptureID, "root")
	if filepath.Clean(view.PrivateRoot) != wantRoot || !filepath.IsAbs(wantRoot) {
		return fmt.Errorf("hermetic capture ownership mismatch")
	}
	return discardCaptureDir(filepath.Dir(wantRoot))
}

func validateProviderRequest(ctx context.Context, p *Provider, req app.ProviderCaptureRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.newCaptureID == nil || !filepath.IsAbs(p.privateRoot) || filepath.Clean(p.privateRoot) != p.privateRoot {
		return fmt.Errorf("invalid hermetic private root")
	}
	if err := req.Limits.Validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(req.Root) || filepath.Clean(req.Root) != req.Root {
		return fmt.Errorf("invalid hermetic source root")
	}
	if err := validateRootDirectories(req.Root, p.privateRoot); err != nil {
		return err
	}
	if _, err := workspacecore.ParseWorkspaceID(string(req.WorkspaceID)); err != nil {
		return fmt.Errorf("invalid hermetic workspace binding")
	}
	if _, err := workspacecore.ParseRepositoryID(string(req.RepositoryID)); err != nil {
		return fmt.Errorf("invalid hermetic repository binding")
	}
	if err := core.ValidateSourceGeneration(req.SourceGeneration); err != nil {
		return err
	}
	return nil
}

func validateRootDirectories(sourceRoot, privateRoot string) error {
	sourceInfo, err := os.Lstat(sourceRoot)
	if err != nil || !sourceInfo.IsDir() || sourceInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic source root")
	}
	privateInfo, err := os.Lstat(privateRoot)
	if err != nil || !privateInfo.IsDir() || privateInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic private root")
	}
	sourceResolved, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return fmt.Errorf("invalid hermetic source root")
	}
	privateResolved, err := filepath.EvalSymlinks(privateRoot)
	if err != nil {
		return fmt.Errorf("invalid hermetic private root")
	}
	privateInsideSource, err := isPathWithin(sourceResolved, privateResolved)
	if err != nil {
		return err
	}
	sourceInsidePrivate, err := isPathWithin(privateResolved, sourceResolved)
	if err != nil {
		return err
	}
	if privateInsideSource || sourceInsidePrivate {
		return fmt.Errorf("hermetic private root overlaps source workspace")
	}
	return nil
}

func isPathWithin(root, candidate string) (bool, error) {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}

func validCaptureID(value string) bool {
	if len(value) != 31 || len(value) < 6 || value[:5] != "hcap_" {
		return false
	}
	for _, r := range value[5:] {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

var _ app.CaptureProvider = (*Provider)(nil)
