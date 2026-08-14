package gopls

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.lsp.dev/protocol"

	lspadapter "github.com/maemreyo/shellbeam/internal/adapter/codeintel/lsp"
	appcodeintel "github.com/maemreyo/shellbeam/internal/app/codeintel"
	core "github.com/maemreyo/shellbeam/internal/core/codeintel"
	workspacecore "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type Config struct {
	Executable        string
	Environment       []string
	Configuration     []protocol.LSPAny
	ConfigFingerprint string
	BuildFingerprint  string
	BuildQuality      string
	SyncLimits        SyncLimits
	DiagnosticWait    time.Duration
	StderrBytes       int
	ShutdownTimeout   time.Duration
}

type Factory struct {
	config Config
	deps   factoryDeps
}

type factoryDeps struct {
	lookPath           func(string) (string, error)
	executableIdentity func(string) (string, error)
	isGoWorkspace      func(string) bool
	startSession       func(context.Context, sessionStart) (semanticSession, error)
}

type sessionStart struct {
	Executable      string
	Dir             string
	Env             []string
	ClientOptions   lspadapter.ClientOptions
	StderrBytes     int
	ShutdownTimeout time.Duration
}

func DefaultConfig() Config {
	environment := append([]string(nil), os.Environ()...)
	configuration := []protocol.LSPAny{protocol.LSPAny([]byte("{}"))}
	return Config{
		Executable:        "gopls",
		Environment:       environment,
		Configuration:     configuration,
		ConfigFingerprint: fingerprintConfiguration(configuration),
		BuildFingerprint:  fingerprintBuildEnvironment(environment),
		BuildQuality:      "observed_environment",
		SyncLimits: SyncLimits{
			MaxOpenDocuments:   128,
			MaxOpenSourceBytes: 8 << 20,
		},
		DiagnosticWait:  250 * time.Millisecond,
		StderrBytes:     64 << 10,
		ShutdownTimeout: 2 * time.Second,
	}
}

func NewFactory(config Config) (*Factory, error) {
	return newFactory(config, factoryDeps{})
}

func newFactory(config Config, deps factoryDeps) (*Factory, error) {
	config = normalizeConfig(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if deps.lookPath == nil {
		deps.lookPath = exec.LookPath
	}
	if deps.executableIdentity == nil {
		deps.executableIdentity = statExecutableIdentity
	}
	if deps.isGoWorkspace == nil {
		deps.isGoWorkspace = hasGoWorkspaceMarker
	}
	if deps.startSession == nil {
		deps.startSession = startLSPSession
	}
	return &Factory{config: config, deps: deps}, nil
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.Executable == "" {
		config.Executable = defaults.Executable
	}
	if config.Environment == nil {
		config.Environment = defaults.Environment
	}
	if config.Configuration == nil {
		config.Configuration = defaults.Configuration
	}
	if config.ConfigFingerprint == "" {
		config.ConfigFingerprint = fingerprintConfiguration(config.Configuration)
	}
	if config.BuildFingerprint == "" {
		config.BuildFingerprint = fingerprintBuildEnvironment(config.Environment)
	}
	if config.BuildQuality == "" {
		config.BuildQuality = defaults.BuildQuality
	}
	if config.SyncLimits == (SyncLimits{}) {
		config.SyncLimits = defaults.SyncLimits
	}
	if config.DiagnosticWait == 0 {
		config.DiagnosticWait = defaults.DiagnosticWait
	}
	if config.StderrBytes == 0 {
		config.StderrBytes = defaults.StderrBytes
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = defaults.ShutdownTimeout
	}
	return config
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Executable) == "" || len(c.Executable) > 4096 ||
		c.ConfigFingerprint == "" || len(c.ConfigFingerprint) > core.MaxProviderTextBytes ||
		c.BuildFingerprint == "" || len(c.BuildFingerprint) > core.MaxProviderTextBytes ||
		c.BuildQuality == "" || len(c.BuildQuality) > core.MaxProviderTextBytes ||
		c.DiagnosticWait <= 0 || c.DiagnosticWait > 5*time.Second ||
		c.StderrBytes < 1 || c.StderrBytes > 1<<20 ||
		c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 10*time.Second {
		return fmt.Errorf("invalid gopls config")
	}
	if err := c.SyncLimits.Validate(); err != nil {
		return err
	}
	return nil
}

func (f *Factory) Resolve(_ context.Context, workspace workspacecore.Workspace, query core.Query) (appcodeintel.ProviderStartOptions, error) {
	if err := workspace.Validate(); err != nil {
		return appcodeintel.ProviderStartOptions{}, err
	}
	if query.Provider != "" && query.Provider != core.ProviderGoSemantic {
		return appcodeintel.ProviderStartOptions{}, fmt.Errorf("unsupported code provider %q", query.Provider)
	}
	if query.Provider == "" && !f.deps.isGoWorkspace(workspace.Root) {
		return appcodeintel.ProviderStartOptions{}, fmt.Errorf("workspace has no unambiguous Go semantic root")
	}
	path, err := f.deps.lookPath(f.config.Executable)
	if err != nil {
		return appcodeintel.ProviderStartOptions{}, fmt.Errorf("gopls executable unavailable: %w", err)
	}
	identity, err := f.deps.executableIdentity(path)
	if err != nil {
		return appcodeintel.ProviderStartOptions{}, fmt.Errorf("gopls executable identity: %w", err)
	}
	return appcodeintel.ProviderStartOptions{
		ProviderID:         core.ProviderGoSemantic,
		ExecutableIdentity: identity,
		ConfigFingerprint:  f.config.ConfigFingerprint,
		BuildFingerprint:   f.config.BuildFingerprint,
	}, nil
}

func (f *Factory) Start(ctx context.Context, workspace workspacecore.Workspace, options appcodeintel.ProviderStartOptions) (appcodeintel.Provider, error) {
	path, err := f.deps.lookPath(f.config.Executable)
	if err != nil {
		return nil, fmt.Errorf("gopls executable unavailable: %w", err)
	}
	identity, err := f.deps.executableIdentity(path)
	if err != nil {
		return nil, fmt.Errorf("gopls executable identity: %w", err)
	}
	if options.ProviderID != core.ProviderGoSemantic || options.ExecutableIdentity != identity ||
		options.ConfigFingerprint != f.config.ConfigFingerprint || options.BuildFingerprint != f.config.BuildFingerprint {
		return nil, fmt.Errorf("gopls start options no longer compatible")
	}
	session, err := f.deps.startSession(ctx, sessionStart{
		Executable: path,
		Dir:        workspace.Root,
		Env:        append([]string(nil), f.config.Environment...),
		ClientOptions: lspadapter.ClientOptions{
			DiagnosticLimits: lspadapter.DiagnosticLimits{MaxURIs: 512, MaxDiagnosticsPerURI: 512, MaxMessageBytes: 16 << 10},
			Configuration:    f.config.Configuration,
			WorkspaceFolders: []protocol.WorkspaceFolder{{URI: workspaceURI(workspace), Name: workspace.Label}},
		},
		StderrBytes:     f.config.StderrBytes,
		ShutdownTimeout: f.config.ShutdownTimeout,
	})
	if err != nil {
		return nil, err
	}
	return startProvider(ctx, workspace, options, f.config, session)
}

func startLSPSession(ctx context.Context, start sessionStart) (semanticSession, error) {
	client, err := lspadapter.NewClient(start.ClientOptions)
	if err != nil {
		return nil, err
	}
	session, err := lspadapter.StartProcess(ctx, lspadapter.ProcessConfig{
		Executable:      start.Executable,
		Dir:             start.Dir,
		Env:             start.Env,
		StderrBytes:     start.StderrBytes,
		ShutdownTimeout: start.ShutdownTimeout,
	}, client)
	if err != nil {
		return nil, err
	}
	return newLSPSemanticSession(session), nil
}

func statExecutableIdentity(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("gopls executable is not a regular file")
	}
	raw := fmt.Sprintf("%s\x00%d\x00%d\x00%d", absolute, info.Size(), info.ModTime().UnixNano(), uint32(info.Mode()))
	return fmt.Sprintf("gopls_exec_%x", sha256.Sum256([]byte(raw))), nil
}

func hasGoWorkspaceMarker(root string) bool {
	for _, name := range []string{"go.work", "go.mod"} {
		if info, err := os.Stat(filepath.Join(root, name)); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func fingerprintConfiguration(values []protocol.LSPAny) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = h.Write(value)
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("gopls_cfg_%x", h.Sum(nil))
}

func fingerprintBuildEnvironment(environment []string) string {
	allowed := map[string]struct{}{
		"GOOS": {}, "GOARCH": {}, "GOFLAGS": {}, "GOWORK": {}, "GOPATH": {}, "GOMODCACHE": {},
		"GOPROXY": {}, "GONOSUMDB": {}, "GOPRIVATE": {}, "GOTOOLCHAIN": {}, "GOENV": {},
		"CGO_ENABLED": {}, "CC": {}, "CXX": {},
	}
	selected := make([]string, 0, len(allowed))
	for _, value := range environment {
		key, _, ok := strings.Cut(value, "=")
		if _, wanted := allowed[key]; ok && wanted {
			selected = append(selected, value)
		}
	}
	sort.Strings(selected)
	h := sha256.New()
	for _, value := range selected {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("go_build_%x", h.Sum(nil))
}
