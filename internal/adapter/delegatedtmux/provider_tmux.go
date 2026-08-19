package delegatedtmux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (p *Provider) startBootstrap(ctx context.Context, socket, session string) error {
	out, err := p.runTmux(ctx, socket, "new-session", "-d", "-s", session, "exec /bin/sleep 86400")
	if err != nil {
		return delegatedUnavailable("bootstrap", fmt.Errorf("%w: %s", err, strings.TrimSpace(out)))
	}
	return nil
}
func (p *Provider) runTmux(ctx context.Context, socket string, args ...string) (string, error) {
	full := append([]string{"-S", socket, "-f", "/dev/null"}, args...)
	cmd := exec.CommandContext(ctx, p.config.TmuxPath, full...)
	cmd.Env = helperEnvironment(p.config.TmuxPath)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func (p *Provider) killServer(ctx context.Context, socket string) error {
	out, err := p.runTmux(ctx, socket, "kill-server")
	if err != nil && isServerGoneText(out) {
		return nil
	}
	return err
}
func isServerGoneText(text string) bool {
	return strings.Contains(text, "no server running") || strings.Contains(text, "connection refused") || strings.Contains(text, "No such file")
}

func (p *Provider) startControl(ctx context.Context, socket, target string) (*controlClient, error) {
	cmd := exec.Command(p.config.TmuxPath, "-S", socket, "-f", "/dev/null", "-C", "attach-session", "-E", "-f", "ignore-size", "-t", target)
	cmd.Env = helperEnvironment(p.config.TmuxPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := newControlClient(cmd, stdin, stdout)
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	startup, err := c.waitResult(ctx)
	if err != nil {
		_ = c.close()
		return nil, err
	}
	if startup.Kind == eventCommandError {
		_ = c.close()
		return nil, fmt.Errorf("control attach failed: %s", startup.Data)
	}
	return c, nil
}
func (p *Provider) configureServer(ctx context.Context, c *controlClient, generation string) error {
	for _, args := range [][]string{{"set-option", "-g", "assume-paste-time", "0"}, {"set-option", "-g", "exit-empty", "off"}, {"set-option", "-g", "default-shell", "/bin/sh"}, {"set-window-option", "-g", "remain-on-exit", "on"}, {"set-environment", "-g", "SHELLBEAM_PROVIDER_GENERATION", generation}} {
		if err := p.controlCommand(ctx, c, args...); err != nil {
			return err
		}
	}
	return nil
}
func (p *Provider) controlCommand(ctx context.Context, c *controlClient, args ...string) error {
	line, err := tmuxCommandLine(args...)
	if err != nil {
		return err
	}
	_, err = c.command(ctx, line)
	return err
}
func tmuxCommandLine(args ...string) (string, error) {
	if len(args) == 0 || !safeCommandName(args[0]) {
		return "", fmt.Errorf("invalid tmux command")
	}
	out := []string{args[0]}
	for _, arg := range args[1:] {
		q, err := quoteTmuxArg(arg)
		if err != nil {
			return "", err
		}
		out = append(out, q)
	}
	return strings.Join(out, " "), nil
}
func safeCommandName(v string) bool {
	if v == "" {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func workloadEnvironment(spec operation.ExecutionSpec) (map[string]string, error) {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok && validEnvKey(key) && !strings.ContainsRune(value, 0) {
			values[key] = value
		}
	}
	for _, entry := range spec.EnvironmentAdditions {
		if !validEnvKey(entry.Key) || entry.Value == "" || strings.ContainsRune(entry.Value, 0) {
			return nil, fmt.Errorf("invalid delegated workload environment")
		}
		values[entry.Key] = entry.Value
	}
	delete(values, "SHELLBEAM_PROVIDER_GENERATION")
	return values, nil
}
func validEnvKey(v string) bool {
	if v == "" || !((v[0] >= 'A' && v[0] <= 'Z') || (v[0] >= 'a' && v[0] <= 'z') || v[0] == '_') {
		return false
	}
	for i := 1; i < len(v); i++ {
		b := v[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' {
			continue
		}
		return false
	}
	return true
}
func (p *Provider) installWorkloadEnvironment(ctx context.Context, c *controlClient, env map[string]string) error {
	keys := sortedKeys(env)
	for _, key := range keys {
		if err := p.controlCommand(ctx, c, "set-environment", "-g", key, env[key]); err != nil {
			return err
		}
	}
	return nil
}
func (p *Provider) clearWorkloadEnvironment(ctx context.Context, c *controlClient, env map[string]string) error {
	for _, key := range sortedKeys(env) {
		if err := p.controlCommand(ctx, c, "set-environment", "-gu", key); err != nil {
			return err
		}
	}
	return nil
}
func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func workloadWrapper(spec operation.ExecutionSpec, gate string) (string, error) {
	if err := validateExecutionSpec(spec); err != nil {
		return "", err
	}
	gateQ := shellQuote(gate)
	var target string
	switch spec.Mode {
	case operation.ExecutionModeShell:
		target = "exec -a " + shellQuote(spec.Shell) + " " + shellQuote(spec.Executable) + " -lc " + shellQuote(spec.Command)
	case operation.ExecutionModeArgv:
		parts := []string{"exec", "-a", shellQuote(spec.Argv[0]), shellQuote(spec.Executable)}
		for _, arg := range spec.Argv[1:] {
			parts = append(parts, shellQuote(arg))
		}
		target = strings.Join(parts, " ")
	}
	return "IFS= read -r _sb_gate < " + gateQ + "; " + target, nil
}
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
func validateExecutionSpec(spec operation.ExecutionSpec) error {
	if spec.TTY || !filepath.IsAbs(spec.CWD) || !filepath.IsAbs(spec.Executable) {
		return fmt.Errorf("invalid delegated execution spec")
	}
	switch spec.Mode {
	case operation.ExecutionModeShell:
		if spec.Shell == "" || spec.Executable != spec.Shell || spec.Command == "" || len(spec.Argv) != 0 {
			return fmt.Errorf("invalid delegated shell spec")
		}
	case operation.ExecutionModeArgv:
		if len(spec.Argv) == 0 || spec.Argv[0] == "" || spec.Command != "" || spec.Shell != "" {
			return fmt.Errorf("invalid delegated argv spec")
		}
	default:
		return fmt.Errorf("invalid delegated execution mode")
	}
	return nil
}
func validateCreateRequest(req app.CreateRequest) error {
	if !safeOpaque(req.SessionID, 128) || !safeOpaque(req.OperationID, 128) || req.Output == nil {
		return fmt.Errorf("invalid delegated create request")
	}
	return validateExecutionSpec(req.Spec)
}

func (p *Provider) queryFacts(ctx context.Context, c *controlClient, target string) (tmuxFacts, error) {
	line, err := tmuxCommandLine("display-message", "-p", "-t", target, "#{session_id}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_dead}|#{pane_dead_status}|#{socket_path}|#{pid}|#{version}")
	if err != nil {
		return tmuxFacts{}, err
	}
	result, err := c.command(ctx, line)
	if err != nil {
		return tmuxFacts{}, err
	}
	parts := strings.Split(result.Data, "|")
	if len(parts) != 9 {
		return tmuxFacts{}, fmt.Errorf("unexpected tmux facts")
	}
	panePID, err := strconv.Atoi(parts[3])
	if err != nil || panePID <= 0 {
		return tmuxFacts{}, fmt.Errorf("invalid pane pid")
	}
	serverPID, err := strconv.Atoi(parts[7])
	if err != nil || serverPID <= 0 {
		return tmuxFacts{}, fmt.Errorf("invalid server pid")
	}
	terminal := parts[4] == "1"
	var code *int
	if terminal && parts[5] != "" {
		v, err := strconv.Atoi(parts[5])
		if err != nil {
			return tmuxFacts{}, fmt.Errorf("invalid pane exit status")
		}
		code = &v
	}
	return tmuxFacts{SessionInternalID: parts[0], WindowID: parts[1], PaneID: parts[2], PanePID: panePID, ServerPID: serverPID, Terminal: terminal, ExitCode: code, SocketPath: parts[6], Version: "tmux " + strings.TrimPrefix(parts[8], "tmux ")}, nil
}
func (p *Provider) queryGeneration(ctx context.Context, c *controlClient) (string, error) {
	line, _ := tmuxCommandLine("show-environment", "-g", "SHELLBEAM_PROVIDER_GENERATION")
	result, err := c.command(ctx, line)
	if err != nil {
		return "", err
	}
	prefix := "SHELLBEAM_PROVIDER_GENERATION="
	if !strings.HasPrefix(result.Data, prefix) {
		return "", fmt.Errorf("provider generation missing")
	}
	return strings.TrimPrefix(result.Data, prefix), nil
}
func (p *Provider) verifyFacts(ctx context.Context, c *controlClient, state privateState, facts tmuxFacts) error {
	generation, err := p.queryGeneration(ctx, c)
	if err != nil {
		return err
	}
	if generation != state.ProviderGeneration || facts.SessionInternalID != state.SessionInternalID || facts.WindowID != state.WindowID || facts.PaneID != state.PaneID || facts.PanePID != state.PanePID || facts.ServerPID != state.ServerPID || filepath.Clean(facts.SocketPath) != filepath.Clean(state.SocketPath) || facts.Version != state.TmuxVersion || state.TmuxSHA256 != p.config.ExpectedSHA256 {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": state.SessionID, "provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion)}, fmt.Errorf("provider identity mismatch"))
	}
	return nil
}
func (p *Provider) observationFromFacts(c *controlClient, state privateState, facts tmuxFacts) app.Observation {
	owner := core.OwnerAgent
	if facts.Terminal {
		owner = core.OwnerNone
	}
	return app.Observation{Provider: p.Identity(), ProviderCurrent: true, ProviderGeneration: state.ProviderGeneration, Owner: owner, Terminal: facts.Terminal, ExitCode: facts.ExitCode, PanePID: facts.PanePID, OutputBytes: c.outputBytes.Load()}
}
func (p *Provider) currentControl(ctx context.Context, ref core.ProviderRef) (*controlClient, privateState, app.Observation, error) {
	state, err := p.state.load(ref.Ref)
	if err != nil {
		return nil, state, app.Observation{}, providerLost(ref, "private_state", err)
	}
	p.mu.Lock()
	c := p.controls[ref.Ref]
	p.mu.Unlock()
	if c == nil {
		return nil, state, app.Observation{}, providerLost(ref, "observer_missing", nil)
	}
	obs, err := p.Inspect(ctx, ref)
	return c, state, obs, err
}
func (p *Provider) validateProviderRef(ref core.ProviderRef, sessionID string) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if ref.ProviderID != ProviderID || ref.ProviderVersion != ProviderVersion || ref.SessionID != sessionID {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": sessionID, "provider_id": ref.ProviderID, "provider_version": fmt.Sprint(ref.ProviderVersion), "expected_provider_id": ProviderID, "expected_provider_version": fmt.Sprint(ProviderVersion)}, nil)
	}
	expected, err := p.ProviderRefForSession(sessionID, ref.CreatedAt)
	if err != nil {
		return err
	}
	if expected.Ref != ref.Ref {
		return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": sessionID, "provider_id": ref.ProviderID}, fmt.Errorf("provider ref mismatch"))
	}
	return nil
}
func providerLost(ref core.ProviderRef, reason string, cause error) error {
	return failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": ref.SessionID, "provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion), "reason": reason}, cause)
}
func newGeneration() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "gen_" + hex.EncodeToString(raw[:]), nil
}
func (p *Provider) releaseAndPersist(ctx context.Context, state *privateState) error {
	if state.StartReleased {
		return nil
	}
	if err := releasePrivateFIFO(ctx, state.StartGatePath); err != nil {
		return failure.New(failure.DelegatedReconcileBlocked, map[string]string{"session_id": state.SessionID, "provider_id": ProviderID, "reason": "start_gate"}, err)
	}
	state.StartReleased = true
	state.UpdatedAt = time.Now().UTC()
	if err := p.state.save(*state); err != nil {
		return delegatedUnavailable("private_state_release", err)
	}
	return nil
}
