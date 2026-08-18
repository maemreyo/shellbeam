package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	bwrapadapter "github.com/maemreyo/shellbeam/internal/adapter/hermetic/bwrap"
	captureadapter "github.com/maemreyo/shellbeam/internal/adapter/hermetic/localfs"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/jsonstrict"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const hermeticProviderManifestMaxBytes = 32 << 10

type hermeticPrivateStarter interface {
	StartPrivateHermetic(context.Context, hermeticapp.ProviderCommand, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error)
}

type hermeticRuntimeFactory func(
	context.Context,
	string,
	string,
	hermeticapp.WorkspaceSource,
	hermeticPrivateStarter,
) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error)

type hermeticProviderManifest struct {
	SchemaVersion      int                            `json:"schema_version"`
	BubblewrapPath     string                         `json:"bubblewrap_path"`
	ToolchainRoot      string                         `json:"toolchain_root"`
	Provider           hermeticcore.ProviderIdentity  `json:"provider"`
	Toolchain          hermeticcore.ToolchainIdentity `json:"toolchain"`
	SecurityPolicyPath string                         `json:"security_policy_path,omitempty"`
}

func composeHermeticBoundary(
	ctx context.Context,
	enabled bool,
	stateDir, runtimeDir string,
	workspace hermeticapp.WorkspaceSource,
	owner daemonapp.ProcessOwner,
	catalog capability.Catalog,
	factory hermeticRuntimeFactory,
) (daemonapp.HermeticRuntime, capability.Catalog) {
	if !enabled || workspace == nil || owner == nil {
		return nil, catalog
	}
	starter, ok := owner.(hermeticPrivateStarter)
	if !ok || starter == nil {
		return nil, catalog
	}
	if factory == nil {
		factory = newQualifiedHermeticRuntime
	}
	runtime, support, err := factory(ctx, stateDir, runtimeDir, workspace, starter)
	if err != nil || runtime == nil || support == nil || !support.ValidV1() {
		return nil, catalog
	}
	advertised := catalog.WithHermeticBoundary(*support)
	if advertised.Features[capability.FeatureHermeticBoundaryV1] != capability.Available || advertised.HermeticBoundary == nil {
		return nil, catalog
	}
	return runtime, advertised
}

func newQualifiedHermeticRuntime(
	ctx context.Context,
	stateDir, runtimeDir string,
	workspace hermeticapp.WorkspaceSource,
	starter hermeticPrivateStarter,
) (daemonapp.HermeticRuntime, *capability.HermeticBoundarySupport, error) {
	manifest, err := loadHermeticProviderManifest(stateDir)
	if err != nil {
		return nil, nil, err
	}
	captureRoot, boundaryRoot, err := prepareHermeticRuntimeRoots(runtimeDir)
	if err != nil {
		return nil, nil, err
	}
	capture := hermeticapp.NewCaptureService(workspace, captureadapter.New(captureRoot), hermeticcore.DefaultCaptureLimits())
	provider, err := bwrapadapter.New(ctx, bwrapadapter.Config{
		BubblewrapPath: manifest.BubblewrapPath, ToolchainRoot: manifest.ToolchainRoot, RuntimeRoot: boundaryRoot,
		ProviderIdentity: manifest.Provider, ToolchainIdentity: manifest.Toolchain, SecurityPolicyPath: manifest.SecurityPolicyPath,
	})
	if err != nil {
		return nil, nil, err
	}
	runtime, err := bwrapadapter.NewRuntime(ctx, capture, provider, starter)
	if err != nil {
		return nil, nil, err
	}
	support := qualifiedHermeticSupport()
	return runtime, support, nil
}

func qualifiedHermeticSupport() *capability.HermeticBoundarySupport {
	return &capability.HermeticBoundarySupport{
		Version: 1, Maturity: "experimental", Provider: hermeticcore.ProviderBubblewrap, ProviderVersion: hermeticcore.BubblewrapVersionV1,
		Scope: "verification_only_ephemeral", Filesystem: "immutable_capture", Network: "off", Environment: "fixed_allowlist",
		Stdin: "closed", Writes: "ephemeral_discard", TimeRandomness: "ambient_nondeterministic", ChildTree: "enclosed",
		Placement: "pre_exec", PTY: "unsupported", PersistentSessions: "unsupported", Authority: "proven_input_scope",
	}
}

func loadHermeticProviderManifest(stateDir string) (hermeticProviderManifest, error) {
	path := filepath.Join(stateDir, "hermetic-v1", "provider.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > hermeticProviderManifestMaxBytes {
		return hermeticProviderManifest{}, fmt.Errorf("hermetic provider manifest unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > hermeticProviderManifestMaxBytes {
		return hermeticProviderManifest{}, fmt.Errorf("hermetic provider manifest unavailable")
	}
	var manifest hermeticProviderManifest
	if err := jsonstrict.Decode(data, &manifest); err != nil {
		return hermeticProviderManifest{}, fmt.Errorf("invalid hermetic provider manifest")
	}
	if err := manifest.Validate(); err != nil {
		return hermeticProviderManifest{}, err
	}
	return manifest, nil
}

func (m hermeticProviderManifest) Validate() error {
	if m.SchemaVersion != 1 || !cleanAbsoluteHermeticPath(m.BubblewrapPath) || !cleanAbsoluteHermeticPath(m.ToolchainRoot) {
		return fmt.Errorf("invalid hermetic provider manifest")
	}
	if err := m.Provider.Validate(); err != nil {
		return err
	}
	if err := m.Toolchain.Validate(); err != nil {
		return err
	}
	if (m.Provider.SecurityPolicyID == "") != (m.SecurityPolicyPath == "") {
		return fmt.Errorf("invalid hermetic security policy manifest")
	}
	if m.SecurityPolicyPath != "" && !cleanAbsoluteHermeticPath(m.SecurityPolicyPath) {
		return fmt.Errorf("invalid hermetic security policy path")
	}
	return nil
}

func prepareHermeticRuntimeRoots(runtimeDir string) (string, string, error) {
	if !cleanAbsoluteHermeticPath(runtimeDir) {
		return "", "", fmt.Errorf("invalid hermetic runtime directory")
	}
	root := filepath.Join(runtimeDir, "hermetic-v1")
	for _, path := range []string{root, filepath.Join(root, "captures"), filepath.Join(root, "boundaries")} {
		if err := ensureHermeticPrivateDirectory(path); err != nil {
			return "", "", err
		}
	}
	return filepath.Join(root, "captures"), filepath.Join(root, "boundaries"), nil
}

func ensureHermeticPrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("hermetic private directory unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("unsafe hermetic private directory")
	}
	return nil
}

func cleanAbsoluteHermeticPath(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}
