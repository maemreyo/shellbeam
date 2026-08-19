package delegatedtmux

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	core "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
)

const humanClientStateSchemaVersion = 1

type humanClientState struct {
	SchemaVersion      int       `json:"schema_version"`
	ProviderRef        string    `json:"provider_ref"`
	ClientRef          string    `json:"client_ref"`
	SessionID          string    `json:"session_id"`
	ProviderGeneration string    `json:"provider_generation"`
	ClientName         string    `json:"client_name"`
	TTY                string    `json:"tty"`
	PID                int       `json:"pid"`
	CreatedAt          time.Time `json:"created_at"`
}

func (s humanClientState) validate() error {
	if s.SchemaVersion != humanClientStateSchemaVersion || !safeOpaque(s.ProviderRef, 128) || !safeOpaque(s.ClientRef, 128) || !safeOpaque(s.SessionID, 128) || !safeOpaque(s.ProviderGeneration, 128) || !safeProviderPrivateString(s.ClientName, 512) || !safeProviderPrivateString(s.TTY, 512) || s.PID <= 0 || s.CreatedAt.IsZero() {
		return fmt.Errorf("invalid delegated human client state")
	}
	return nil
}

type tmuxClientFacts struct {
	Name, TTY string
	PID       int
	ReadOnly  bool
	Flags     string
}

func (p *Provider) AttachHuman(ctx context.Context, ref core.ProviderRef, spec app.HumanAttachSpec) (app.HumanAttachResult, error) {
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return app.HumanAttachResult{}, err
	}
	if spec.Stdin == nil || spec.Stdout == nil || spec.Stderr == nil {
		return app.HumanAttachResult{}, failure.New(failure.InvalidInput, map[string]string{"field": "human_attach_streams"}, nil)
	}
	if err := ctx.Err(); err != nil {
		return app.HumanAttachResult{}, err
	}
	cmd := exec.Command(p.config.TmuxPath, "-S", state.SocketPath, "-f", "/dev/null", "attach-session", "-E", "-f", "read-only,ignore-size", "-t", state.TmuxSession)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	cmd.Env = attachEnvironment(spec.Environment)
	if err := cmd.Start(); err != nil {
		return app.HumanAttachResult{}, failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "attach_start"}, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	facts, err := p.waitHumanClientByPID(ctx, state, cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		<-done
		return app.HumanAttachResult{}, err
	}
	if !facts.ReadOnly {
		_ = cmd.Process.Kill()
		<-done
		return app.HumanAttachResult{}, failure.New(failure.HumanClientNotProven, map[string]string{"reason": "attach_not_read_only"}, nil)
	}
	clientState, err := newHumanClientState(ref, state, facts)
	if err != nil {
		_ = cmd.Process.Kill()
		<-done
		return app.HumanAttachResult{}, err
	}
	if err := p.saveHumanClientState(state, clientState); err != nil {
		_ = cmd.Process.Kill()
		<-done
		return app.HumanAttachResult{}, failure.New(failure.HumanControlUnreachable, map[string]string{"reason": "client_state_write"}, err)
	}
	return app.HumanAttachResult{ClientRef: app.ProviderClientRef{Ref: clientState.ClientRef}, ObservedOwner: core.OwnerNone, Done: done}, nil
}

func (p *Provider) InspectHumanClient(ctx context.Context, ref core.ProviderRef, client app.ProviderClientRef) (app.HumanClientObservation, error) {
	state, err := p.humanProviderState(ctx, ref)
	if err != nil {
		return app.HumanClientObservation{}, err
	}
	clientState, err := p.loadHumanClientState(state, client)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return app.HumanClientObservation{}, failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_state_missing"}, err)
		}
		return app.HumanClientObservation{}, failure.New(failure.HumanClientNotProven, map[string]string{"reason": "client_state_invalid"}, err)
	}
	facts, err := p.exactHumanClient(ctx, state, clientState)
	if err != nil {
		return app.HumanClientObservation{}, err
	}
	owner := core.OwnerHuman
	if facts.ReadOnly {
		owner = core.OwnerNone
	}
	return app.HumanClientObservation{ClientRef: client, Present: true, ReadOnly: facts.ReadOnly, ObservedOwner: owner, ProviderGeneration: state.ProviderGeneration}, nil
}

func (p *Provider) humanProviderState(ctx context.Context, ref core.ProviderRef) (privateState, error) {
	if err := p.Probe(ctx); err != nil {
		return privateState{}, err
	}
	if err := p.validateProviderRef(ref, ref.SessionID); err != nil {
		return privateState{}, err
	}
	state, err := p.state.load(ref.Ref)
	if err != nil {
		return privateState{}, providerLost(ref, "private_state", err)
	}
	if err := p.verifyExternalProviderState(ctx, state); err != nil {
		return privateState{}, err
	}
	return state, nil
}

func (p *Provider) verifyExternalProviderState(ctx context.Context, state privateState) error {
	facts, err := p.externalTmux(ctx, state, "display-message", "-p", "-t", state.TmuxSession, "#{session_id}|#{window_id}|#{pane_id}|#{pane_pid}|#{socket_path}|#{pid}|#{version}")
	if err != nil {
		return failure.New(failure.DelegatedProviderLost, map[string]string{"session_id": state.SessionID, "provider_id": ProviderID, "reason": "human_provider_facts"}, err)
	}
	parts := strings.Split(strings.TrimSpace(facts), "|")
	if len(parts) != 7 {
		return providerIdentityMismatch(state, "human_provider_facts_shape")
	}
	panePID, paneErr := strconv.Atoi(parts[3])
	serverPID, serverErr := strconv.Atoi(parts[5])
	if paneErr != nil || serverErr != nil || panePID != state.PanePID || serverPID != state.ServerPID || parts[0] != state.SessionInternalID || parts[1] != state.WindowID || parts[2] != state.PaneID || filepath.Clean(parts[4]) != filepath.Clean(state.SocketPath) || "tmux "+strings.TrimPrefix(parts[6], "tmux ") != state.TmuxVersion {
		return providerIdentityMismatch(state, "human_provider_identity")
	}
	generation, err := p.externalTmux(ctx, state, "show-environment", "-g", "SHELLBEAM_PROVIDER_GENERATION")
	if err != nil || strings.TrimSpace(generation) != "SHELLBEAM_PROVIDER_GENERATION="+state.ProviderGeneration {
		return providerIdentityMismatch(state, "human_provider_generation")
	}
	return nil
}

func (p *Provider) externalTmux(ctx context.Context, state privateState, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	full := []string{"-S", state.SocketPath, "-f", "/dev/null"}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, p.config.TmuxPath, full...)
	cmd.Env = helperEnvironment(p.config.TmuxPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (p *Provider) listHumanClients(ctx context.Context, state privateState) ([]tmuxClientFacts, error) {
	out, err := p.externalTmux(ctx, state, "list-clients", "-F", "#{client_name}|#{client_tty}|#{client_pid}|#{client_readonly}|#{client_flags}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(out)
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	facts := make([]tmuxClientFacts, 0, len(lines))
	for _, line := range lines {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			return nil, fmt.Errorf("invalid tmux client facts")
		}
		pid, err := strconv.Atoi(parts[2])
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("invalid tmux client pid")
		}
		facts = append(facts, tmuxClientFacts{Name: parts[0], TTY: parts[1], PID: pid, ReadOnly: parts[3] == "1", Flags: parts[4]})
	}
	return facts, nil
}

func (p *Provider) waitHumanClientByPID(ctx context.Context, state privateState, pid int) (tmuxClientFacts, error) {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		clients, err := p.listHumanClients(ctx, state)
		if err == nil {
			for _, client := range clients {
				if client.PID == pid {
					return client, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return tmuxClientFacts{}, failure.New(failure.HumanClientNotProven, map[string]string{"reason": "attach_identity_timeout"}, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *Provider) exactHumanClient(ctx context.Context, state privateState, want humanClientState) (tmuxClientFacts, error) {
	clients, err := p.listHumanClients(ctx, state)
	if err != nil {
		return tmuxClientFacts{}, failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_list"}, err)
	}
	matches := 0
	var found tmuxClientFacts
	for _, client := range clients {
		if client.Name == want.ClientName {
			matches++
			found = client
		}
	}
	if matches == 0 {
		return tmuxClientFacts{}, failure.New(failure.HandoffClientLost, map[string]string{"reason": "client_missing"}, nil)
	}
	if matches != 1 || found.PID != want.PID || found.TTY != want.TTY {
		return tmuxClientFacts{}, failure.New(failure.HumanClientNotProven, map[string]string{"reason": "client_identity_mismatch"}, nil)
	}
	return found, nil
}

func newHumanClientState(ref core.ProviderRef, state privateState, facts tmuxClientFacts) (humanClientState, error) {
	logical := ref.Ref + "\x00" + state.ProviderGeneration + "\x00" + facts.Name + "\x00" + facts.TTY + "\x00" + strconv.Itoa(facts.PID)
	sum := sha256.Sum256([]byte(logical))
	clientRef := "hclient_" + hex.EncodeToString(sum[:16])
	out := humanClientState{SchemaVersion: humanClientStateSchemaVersion, ProviderRef: ref.Ref, ClientRef: clientRef, SessionID: ref.SessionID, ProviderGeneration: state.ProviderGeneration, ClientName: facts.Name, TTY: facts.TTY, PID: facts.PID, CreatedAt: time.Now().UTC()}
	return out, out.validate()
}

func (p *Provider) humanClientDir(state privateState) string {
	return filepath.Join(filepath.Dir(state.SocketPath), "human-clients")
}

func (p *Provider) humanClientPath(state privateState, client app.ProviderClientRef) string {
	if client.Validate() != nil {
		return ""
	}
	return filepath.Join(p.humanClientDir(state), client.Ref+".json")
}

func (p *Provider) saveHumanClientState(state privateState, client humanClientState) error {
	if err := client.validate(); err != nil || client.ProviderRef != state.Ref || client.SessionID != state.SessionID || client.ProviderGeneration != state.ProviderGeneration {
		return fmt.Errorf("human client/provider state mismatch")
	}
	dir := p.humanClientDir(state)
	if err := ensurePrivateDirectory(dir); err != nil {
		return err
	}
	return writePrivateHumanClientJSON(filepath.Join(dir, client.ClientRef+".json"), client)
}

func (p *Provider) loadHumanClientState(state privateState, client app.ProviderClientRef) (humanClientState, error) {
	var out humanClientState
	path := p.humanClientPath(state, client)
	if path == "" {
		return out, fmt.Errorf("invalid human client ref")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	if err := out.validate(); err != nil || out.ClientRef != client.Ref || out.ProviderRef != state.Ref || out.SessionID != state.SessionID || out.ProviderGeneration != state.ProviderGeneration {
		return out, fmt.Errorf("human client state mismatch")
	}
	return out, nil
}

func writePrivateHumanClientJSON(path string, value humanClientState) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".human-client-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func attachEnvironment(env []string) []string {
	if len(env) == 0 {
		return nil
	}
	out := append([]string(nil), env...)
	for _, entry := range out {
		if strings.HasPrefix(entry, "TERM=") {
			return out
		}
	}
	return append(out, "TERM=xterm-256color")
}

func providerIdentityMismatch(state privateState, reason string) error {
	return failure.New(failure.DelegatedProviderMismatch, map[string]string{"session_id": state.SessionID, "provider_id": ProviderID, "provider_version": fmt.Sprint(ProviderVersion)}, fmt.Errorf("%s", reason))
}

func safeProviderPrivateString(v string, max int) bool {
	if len(v) < 1 || len(v) > max {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] == 0 || v[i] < 0x20 || v[i] == 0x7f {
			return false
		}
	}
	return true
}
