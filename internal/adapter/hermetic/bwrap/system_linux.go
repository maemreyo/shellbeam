//go:build linux

package bwrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

type linuxQualificationOps struct{}

func defaultQualificationOps() qualificationOps { return linuxQualificationOps{} }

func (linuxQualificationOps) Qualify(ctx context.Context, cfg Config) (core.ProviderIdentity, core.ToolchainIdentity, error) {
	if err := ctx.Err(); err != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, err
	}
	if os.Geteuid() == 0 {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic provider requires unprivileged user")
	}
	if err := validateRuntimeRoot(cfg.RuntimeRoot); err != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, err
	}
	if err := validateBubblewrapBinary(cfg.BubblewrapPath); err != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, err
	}
	version, err := bubblewrapVersion(ctx, cfg.BubblewrapPath)
	if err != nil || version != "bubblewrap "+core.BubblewrapVersionV1 {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic provider version drift")
	}
	binaryDigest, err := fileSHA256(cfg.BubblewrapPath)
	if err != nil || binaryDigest != cfg.ProviderIdentity.BinarySHA256 {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic provider binary drift")
	}
	runtimePaths, err := bubblewrapRuntimePaths(ctx, cfg.BubblewrapPath)
	if err != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, err
	}
	runtimeDigest, err := runtimeManifestDigest(runtimePaths)
	if err != nil || runtimeDigest != cfg.ProviderIdentity.RuntimeManifestSHA256 {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic provider runtime drift")
	}
	if err := validateSecurityPolicy(cfg); err != nil {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, err
	}
	toolchainDigest, err := toolchainManifestDigest(cfg.ToolchainRoot)
	if err != nil || toolchainDigest != cfg.ToolchainIdentity.ManifestSHA256 {
		return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic toolchain identity drift")
	}
	return cfg.ProviderIdentity, cfg.ToolchainIdentity, nil
}

func (linuxQualificationOps) ToolchainExecutable(root, sandboxPath string) bool {
	return toolchainExecutable(root, sandboxPath)
}

func validateRuntimeRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic runtime root")
	}
	return nil
}

func validateBubblewrapBinary(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 == 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return fmt.Errorf("invalid hermetic bubblewrap binary")
	}
	return nil
}

func bubblewrapVersion(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func bubblewrapRuntimePaths(ctx context.Context, path string) ([]string, error) {
	ldd := "/usr/bin/ldd"
	if _, err := os.Stat(ldd); err != nil {
		ldd = "/bin/ldd"
	}
	cmd := exec.CommandContext(ctx, ldd, path)
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for _, field := range strings.Fields(string(out)) {
		if strings.HasPrefix(field, "/") {
			candidate := filepath.Clean(strings.TrimRight(field, ",;"))
			if filepath.IsAbs(candidate) {
				set[candidate] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("hermetic provider runtime manifest empty")
	}
	paths := make([]string, 0, len(set))
	for candidate := range set {
		paths = append(paths, candidate)
	}
	sort.Strings(paths)
	return paths, nil
}

func validateSecurityPolicy(cfg Config) error {
	id := cfg.ProviderIdentity
	if id.SecurityPolicyID == "" {
		return nil
	}
	info, err := os.Lstat(cfg.SecurityPolicyPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic security policy")
	}
	digest, err := fileSHA256(cfg.SecurityPolicyPath)
	if err != nil || digest != id.SecurityPolicySHA256 {
		return fmt.Errorf("hermetic security policy drift")
	}
	if cfg.BubblewrapPath != "/usr/bin/bwrap" {
		return fmt.Errorf("targeted hermetic security policy requires /usr/bin/bwrap")
	}
	return nil
}
