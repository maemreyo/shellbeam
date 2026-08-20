package delegatedtmux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const (
	ProviderID           = "tmux_control_mode"
	ProviderVersion      = 1
	qualifiedTmuxPath    = "/opt/homebrew/Cellar/tmux/3.6a/bin/tmux"
	qualifiedTmuxVersion = "tmux 3.6a"
	qualifiedTmuxSHA256  = "70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef"
)

type Config struct {
	Root                        string
	RuntimeBase                 string
	TmuxPath                    string
	QualifiedPath               string
	ExpectedVersion             string
	ExpectedSHA256              string
	AllowCurrentPlatformForTest bool
}

type Provider struct {
	config   Config
	state    privateStateStore
	privacy  privacyStateStore
	mu       sync.Mutex
	controls map[string]*controlClient
}

var _ app.Provider = (*Provider)(nil)
var _ app.PrivacyProvider = (*Provider)(nil)

func DarwinQualifiedConfig(root, tmuxPath string) Config {
	return Config{Root: root, TmuxPath: tmuxPath, QualifiedPath: qualifiedTmuxPath, ExpectedVersion: qualifiedTmuxVersion, ExpectedSHA256: qualifiedTmuxSHA256}
}

func New(config Config) (*Provider, error) {
	if config.Root == "" || !filepath.IsAbs(config.Root) {
		return nil, fmt.Errorf("delegated tmux root must be absolute")
	}
	if config.TmuxPath == "" || !filepath.IsAbs(config.TmuxPath) {
		return nil, fmt.Errorf("delegated tmux path must be absolute")
	}
	if config.QualifiedPath == "" || !filepath.IsAbs(config.QualifiedPath) {
		return nil, fmt.Errorf("qualified tmux path must be absolute")
	}
	if config.ExpectedVersion == "" || !validDigest(config.ExpectedSHA256) {
		return nil, fmt.Errorf("invalid qualified tmux identity")
	}
	if config.RuntimeBase == "" {
		config.RuntimeBase = "/tmp"
	}
	if !filepath.IsAbs(config.RuntimeBase) {
		return nil, fmt.Errorf("delegated tmux runtime base must be absolute")
	}
	return &Provider{config: config, state: privateStateStore{root: config.Root}, privacy: privacyStateStore{root: config.Root}, controls: map[string]*controlClient{}}, nil
}

func (p *Provider) Identity() core.ProviderIdentity {
	return core.ProviderIdentity{ID: ProviderID, Version: ProviderVersion}
}

func (p *Provider) ProviderRefForSession(sessionID string, at time.Time) (core.ProviderRef, error) {
	if !safeOpaque(sessionID, 128) || at.IsZero() {
		return core.ProviderRef{}, fmt.Errorf("invalid delegated provider ref input")
	}
	sum := sha256.Sum256([]byte(ProviderID + "\x00" + fmt.Sprint(ProviderVersion) + "\x00" + sessionID))
	ref := "dtmux_" + hex.EncodeToString(sum[:16])
	out := core.ProviderRef{SchemaVersion: core.ProviderRefSchemaVersion, SessionID: sessionID, ProviderID: ProviderID, ProviderVersion: ProviderVersion, Ref: ref, CreatedAt: at.UTC(), UpdatedAt: at.UTC()}
	return out, out.Validate()
}

func (p *Provider) Probe(ctx context.Context) error {
	if p == nil {
		return delegatedUnavailable("provider_nil", nil)
	}
	if runtime.GOOS != "darwin" && !p.config.AllowCurrentPlatformForTest {
		return delegatedUnavailable("platform_unqualified", nil)
	}
	resolved, err := filepath.EvalSymlinks(p.config.TmuxPath)
	if err != nil {
		return delegatedUnavailable("tmux_path", err)
	}
	qualified, err := filepath.EvalSymlinks(p.config.QualifiedPath)
	if err != nil {
		return delegatedUnavailable("qualified_path", err)
	}
	if filepath.Clean(resolved) != filepath.Clean(qualified) {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion)}, fmt.Errorf("tmux path mismatch"))
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return delegatedUnavailable("tmux_read", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != p.config.ExpectedSHA256 {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion)}, fmt.Errorf("tmux digest mismatch"))
	}
	cmd := exec.CommandContext(ctx, p.config.TmuxPath, "-V")
	cmd.Env = helperEnvironment(p.config.TmuxPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return delegatedUnavailable("tmux_version_probe", err)
	}
	if strings.TrimSpace(string(out)) != p.config.ExpectedVersion {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion)}, fmt.Errorf("tmux version mismatch"))
	}
	return nil
}

func helperEnvironment(tmuxPath string) []string {
	values := map[string]string{"PATH": filepath.Dir(tmuxPath) + ":/usr/bin:/bin:/usr/sbin:/sbin", "TERM": "xterm-256color", "TMPDIR": os.TempDir()}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		values["HOME"] = home
	}
	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		if value := os.Getenv(key); value != "" && !strings.ContainsRune(value, 0) {
			values[key] = value
		}
	}
	keys := []string{"HOME", "LANG", "LC_ALL", "LC_CTYPE", "PATH", "TERM", "TMPDIR"}
	out := make([]string, 0, len(values))
	for _, key := range keys {
		if value, ok := values[key]; ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func validDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
func delegatedUnavailable(reason string, cause error) error {
	return failure.New(failure.DelegatedSessionUnavailable, map[string]string{"provider_id": ProviderID, "platform": runtime.GOOS, "reason": reason}, cause)
}
