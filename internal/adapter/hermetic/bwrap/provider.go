package bwrap

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/oklog/ulid/v2"
)

const (
	sandboxRoot    = "/work"
	sandboxInput   = "/work/input"
	sandboxScratch = "/work/scratch"
	sandboxShell   = "/bin/sh"
	fixedPath      = "/usr/bin:/bin"
	fixedHome      = "/homeless"
	fixedLang      = "C.UTF-8"
)

type Config struct {
	BubblewrapPath     string
	ToolchainRoot      string
	RuntimeRoot        string
	ProviderIdentity   core.ProviderIdentity
	ToolchainIdentity  core.ToolchainIdentity
	SecurityPolicyPath string
}

type qualificationOps interface {
	Qualify(context.Context, Config) (core.ProviderIdentity, core.ToolchainIdentity, error)
	ToolchainExecutable(toolchainRoot, sandboxPath string) bool
}

type Provider struct {
	config        Config
	ops           qualificationOps
	provider      core.ProviderIdentity
	toolchain     core.ToolchainIdentity
	newBoundaryID func() string
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
	return newWithOps(ctx, cfg, defaultQualificationOps())
}

func newWithOps(ctx context.Context, cfg Config, ops qualificationOps) (*Provider, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if ops == nil {
		return nil, fmt.Errorf("hermetic provider qualification unavailable")
	}
	providerID, toolchainID, err := ops.Qualify(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := providerID.Validate(); err != nil {
		return nil, err
	}
	if err := toolchainID.Validate(); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(providerID, cfg.ProviderIdentity) || toolchainID != cfg.ToolchainIdentity {
		return nil, fmt.Errorf("hermetic provider identity drift")
	}
	return &Provider{
		config:        cfg,
		ops:           ops,
		provider:      providerID,
		toolchain:     toolchainID,
		newBoundaryID: func() string { return "hb_" + ulid.Make().String() },
	}, nil
}

func (p *Provider) Prepare(ctx context.Context, req app.PrepareExecutionRequest) (_ app.PreparedExecution, retErr error) {
	if err := p.validatePrepare(ctx, req); err != nil {
		return app.PreparedExecution{}, err
	}
	target, err := p.targetArgv(req.Target)
	if err != nil {
		return app.PreparedExecution{}, err
	}
	sandboxCWD, err := sandboxWorkingDirectory(req.LogicalCWD)
	if err != nil {
		return app.PreparedExecution{}, err
	}
	if err := validateCapturedWorkingDirectory(req.Capture.PrivateRoot, req.LogicalCWD); err != nil {
		return app.PreparedExecution{}, err
	}
	manifestDigest, err := req.Capture.Manifest.Digest()
	if err != nil {
		return app.PreparedExecution{}, err
	}
	contentDigest, err := req.Capture.Manifest.ContentDigest()
	if err != nil {
		return app.PreparedExecution{}, err
	}
	boundaryID := p.newBoundaryID()
	if !validBoundaryID(boundaryID) {
		return app.PreparedExecution{}, fmt.Errorf("invalid hermetic boundary identity")
	}
	stateRoot := filepath.Join(p.config.RuntimeRoot, boundaryID)
	scratchRoot := filepath.Join(stateRoot, "scratch")
	if err := os.Mkdir(stateRoot, 0o700); err != nil {
		return app.PreparedExecution{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = removePrivateState(stateRoot)
		}
	}()
	if err := os.Mkdir(scratchRoot, 0o700); err != nil {
		return app.PreparedExecution{}, err
	}
	argv := p.providerArgv(req.Capture.PrivateRoot, scratchRoot, sandboxCWD, target)
	prepared := app.PreparedExecution{
		BoundaryID:            boundaryID,
		Provider:              p.provider,
		Toolchain:             p.toolchain,
		CaptureManifestSHA256: manifestDigest,
		CaptureContentSHA256:  contentDigest,
		Command: app.ProviderCommand{
			Executable:     p.config.BubblewrapPath,
			Argv:           argv,
			Dir:            "/",
			Env:            []string{},
			StdinMode:      operation.StdinModeClosed,
			ResourceLimits: req.Target.ResourceLimits.Clone(),
			StatusFD:       3,
		},
		PrivateStateRoot: stateRoot,
		ScratchRoot:      scratchRoot,
	}
	if err := prepared.ValidatePrivate(); err != nil {
		return app.PreparedExecution{}, err
	}
	cleanup = false
	return prepared, nil
}

func (p *Provider) Sweep(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || !cleanAbsolute(p.config.RuntimeRoot) {
		return fmt.Errorf("invalid hermetic runtime root")
	}
	info, err := os.Lstat(p.config.RuntimeRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid hermetic runtime root")
	}
	entries, err := os.ReadDir(p.config.RuntimeRoot)
	if err != nil {
		return fmt.Errorf("hermetic boundary sweep unavailable")
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validBoundaryID(entry.Name()) {
			continue
		}
		owned := filepath.Join(p.config.RuntimeRoot, entry.Name())
		ownedInfo, err := os.Lstat(owned)
		if err != nil || !ownedInfo.IsDir() || ownedInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe hermetic boundary residue")
		}
		if err := removePrivateState(owned); err != nil {
			return fmt.Errorf("hermetic boundary sweep failed")
		}
	}
	return nil
}

func (p *Provider) Discard(ctx context.Context, prepared app.PreparedExecution) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || !validBoundaryID(prepared.BoundaryID) {
		return fmt.Errorf("invalid hermetic boundary identity")
	}
	expectedRoot := filepath.Join(p.config.RuntimeRoot, prepared.BoundaryID)
	expectedScratch := filepath.Join(expectedRoot, "scratch")
	if prepared.PrivateStateRoot != expectedRoot || prepared.ScratchRoot != expectedScratch || prepared.Provider != p.provider || prepared.Toolchain != p.toolchain {
		return fmt.Errorf("hermetic private execution ownership mismatch")
	}
	return removePrivateState(expectedRoot)
}

func (p *Provider) validatePrepare(ctx context.Context, req app.PrepareExecutionRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.ops == nil || p.newBoundaryID == nil {
		return fmt.Errorf("hermetic provider unavailable")
	}
	providerID, toolchainID, err := p.ops.Qualify(ctx, p.config)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(providerID, p.provider) || toolchainID != p.toolchain {
		return fmt.Errorf("hermetic provider identity drift")
	}
	canonical, err := req.Request.Canonical()
	if err != nil {
		return err
	}
	if err := req.Capture.Manifest.Validate(); err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical.RepoInputs, req.Capture.Manifest.Selectors) {
		return fmt.Errorf("hermetic capture request binding mismatch")
	}
	if !cleanAbsolute(req.Capture.PrivateRoot) {
		return fmt.Errorf("invalid hermetic capture root")
	}
	target := req.Target
	if target.TTY || target.StdinMode != operation.StdinModeClosed || len(target.EnvironmentAdditions) != 0 {
		return fmt.Errorf("hermetic v1 target requires non-tty, closed stdin, fixed environment")
	}
	if target.ResourceLimits != nil {
		if err := target.ResourceLimits.Validate(); err != nil {
			return err
		}
	}
	switch target.Mode {
	case operation.ExecutionModeShell:
		if target.Command == "" || len(target.Argv) != 0 {
			return fmt.Errorf("invalid hermetic shell target")
		}
	case operation.ExecutionModeArgv:
		if len(target.Argv) == 0 || target.Argv[0] == "" || target.Command != "" {
			return fmt.Errorf("invalid hermetic argv target")
		}
	default:
		return fmt.Errorf("invalid hermetic target execution mode")
	}
	return nil
}

func (p *Provider) targetArgv(target operation.ExecutionSpec) ([]string, error) {
	switch target.Mode {
	case operation.ExecutionModeShell:
		if !p.ops.ToolchainExecutable(p.config.ToolchainRoot, sandboxShell) {
			return nil, fmt.Errorf("qualified hermetic shell unavailable")
		}
		return []string{sandboxShell, "-lc", target.Command}, nil
	case operation.ExecutionModeArgv:
		executable, err := p.resolveToolchainExecutable(target.Argv[0])
		if err != nil {
			return nil, err
		}
		out := append([]string{executable}, target.Argv[1:]...)
		for _, arg := range out {
			if arg == "" || strings.IndexByte(arg, 0) >= 0 {
				return nil, fmt.Errorf("invalid hermetic target argument")
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid hermetic target execution mode")
	}
}

func (p *Provider) resolveToolchainExecutable(name string) (string, error) {
	if strings.IndexByte(name, 0) >= 0 || name == "" {
		return "", fmt.Errorf("invalid hermetic target executable")
	}
	if strings.ContainsRune(name, '/') {
		if !filepath.IsAbs(name) || filepath.Clean(name) != name || !allowedSandboxExecutable(name) || !p.ops.ToolchainExecutable(p.config.ToolchainRoot, name) {
			return "", fmt.Errorf("hermetic target executable unavailable in qualified toolchain")
		}
		return name, nil
	}
	for _, dir := range []string{"/usr/bin", "/bin"} {
		candidate := dir + "/" + name
		if p.ops.ToolchainExecutable(p.config.ToolchainRoot, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("hermetic target executable unavailable in qualified toolchain")
}

func (p *Provider) providerArgv(captureRoot, scratchRoot, sandboxCWD string, target []string) []string {
	args := []string{
		p.config.BubblewrapPath,
		"--unshare-user",
		"--unshare-all",
		"--die-with-parent",
		"--disable-userns",
		"--assert-userns-disabled",
		"--json-status-fd", "3",
		"--ro-bind", p.config.ToolchainRoot, "/",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--ro-bind", captureRoot, sandboxInput,
		"--bind", scratchRoot, sandboxScratch,
		"--clearenv",
		"--setenv", "PATH", fixedPath,
		"--setenv", "HOME", fixedHome,
		"--setenv", "PWD", sandboxCWD,
		"--setenv", "LANG", fixedLang,
		"--chdir", sandboxCWD,
		"--",
	}
	return append(args, target...)
}

func sandboxWorkingDirectory(logical string) (string, error) {
	if logical == "" || strings.Contains(logical, "\\") || path.IsAbs(logical) || path.Clean(logical) != logical || logical == ".." || strings.HasPrefix(logical, "../") || strings.Contains(logical, "/../") {
		return "", fmt.Errorf("invalid hermetic logical cwd")
	}
	if logical == "." {
		return sandboxInput, nil
	}
	return path.Join(sandboxInput, logical), nil
}

func validateCapturedWorkingDirectory(root, logical string) error {
	if _, err := sandboxWorkingDirectory(logical); err != nil {
		return err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("invalid hermetic capture root")
	}
	candidate := resolvedRoot
	if logical != "." {
		candidate = filepath.Join(resolvedRoot, filepath.FromSlash(logical))
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("hermetic logical cwd is absent from captured input")
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("hermetic logical cwd escapes captured input")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("hermetic logical cwd is not a captured directory")
	}
	return nil
}

func validateConfig(cfg Config) error {
	if !cleanAbsolute(cfg.BubblewrapPath) || !cleanAbsolute(cfg.ToolchainRoot) || !cleanAbsolute(cfg.RuntimeRoot) {
		return fmt.Errorf("invalid hermetic provider paths")
	}
	if err := cfg.ProviderIdentity.Validate(); err != nil {
		return err
	}
	if err := cfg.ToolchainIdentity.Validate(); err != nil {
		return err
	}
	if cfg.ProviderIdentity.SecurityPolicyID != "" && !cleanAbsolute(cfg.SecurityPolicyPath) {
		return fmt.Errorf("hermetic security policy path required")
	}
	if cfg.ProviderIdentity.SecurityPolicyID == "" && cfg.SecurityPolicyPath != "" {
		return fmt.Errorf("unexpected hermetic security policy path")
	}
	return nil
}

func allowedSandboxExecutable(value string) bool {
	for _, root := range []string{"/usr/bin/", "/bin/"} {
		if strings.HasPrefix(value, root) && len(value) > len(root) && !strings.Contains(value[len(root):], "/") {
			return true
		}
	}
	return false
}

func cleanAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func validBoundaryID(value string) bool {
	if len(value) != 29 || !strings.HasPrefix(value, "hb_") {
		return false
	}
	for _, r := range value[3:] {
		if !strings.ContainsRune("0123456789ABCDEFGHJKMNPQRSTVWXYZ", r) {
			return false
		}
	}
	return true
}

func removePrivateState(root string) error {
	if root == "" {
		return nil
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

var _ app.ExecutionProvider = (*Provider)(nil)
