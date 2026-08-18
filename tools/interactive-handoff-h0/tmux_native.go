package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type nativeFixture struct {
	Tmux       string
	Root       string
	SocketPath string
	Session    string
}

type serverIdentity struct {
	PID        int
	SocketPath string
	Version    string
}

type clientFacts struct {
	Name     string
	TTY      string
	PID      int
	ReadOnly bool
	Flags    string
	Width    int
	Height   int
}

type humanClient struct {
	cmd  *exec.Cmd
	pty  *os.File
	done chan struct{}

	mu      sync.Mutex
	output  bytes.Buffer
	readErr error
	closed  bool
}

func newNativeFixture(ctx context.Context, tmuxPath, root string) (*nativeFixture, error) {
	return newNativeFixtureWithCommand(ctx, tmuxPath, root, "exec /bin/sh")
}

func newNativeFixtureWithCommand(ctx context.Context, tmuxPath, root, command string) (*nativeFixture, error) {
	if !filepath.IsAbs(tmuxPath) {
		return nil, errors.New("tmux path must be absolute")
	}
	if root == "" || !filepath.IsAbs(root) {
		return nil, errors.New("fixture root must be absolute")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	f := &nativeFixture{
		Tmux:       tmuxPath,
		Root:       root,
		SocketPath: filepath.Join(root, "tmux.sock"),
		Session:    "h0-a",
	}
	args := []string{"-S", f.SocketPath, "-f", "/dev/null", "new-session", "-d", "-s", f.Session, command}
	out, err := exec.CommandContext(ctx, f.Tmux, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("start private tmux: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// ShellBeam H0 control keys must remain bindings even when they follow
	// fast human/pasted input. tmux defaults assume-paste-time to 1ms, which
	// deliberately bypasses key bindings for sufficiently fast input. The
	// private server owns this policy, so disable the heuristic explicitly.
	if _, err := f.tmux(ctx, "set-option", "-g", "assume-paste-time", "0"); err != nil {
		_ = f.close(context.Background())
		return nil, fmt.Errorf("disable assume-paste-time: %w", err)
	}
	return f, nil
}

func (f *nativeFixture) tmux(ctx context.Context, args ...string) ([]byte, error) {
	full := make([]string, 0, len(args)+4)
	full = append(full, "-S", f.SocketPath, "-f", "/dev/null")
	full = append(full, args...)
	out, err := exec.CommandContext(ctx, f.Tmux, full...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (f *nativeFixture) close(ctx context.Context) error {
	if f == nil {
		return nil
	}
	_, err := f.tmux(ctx, "kill-server")
	if err != nil && !strings.Contains(err.Error(), "no server running") && !strings.Contains(err.Error(), "No such file or directory") && !strings.Contains(err.Error(), "connection refused") {
		return err
	}
	return os.RemoveAll(f.Root)
}

func (f *nativeFixture) serverIdentity(ctx context.Context) (serverIdentity, error) {
	out, err := f.tmux(ctx, "display-message", "-p", "#{pid}|#{socket_path}|#{version}")
	if err != nil {
		return serverIdentity{}, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 3 {
		return serverIdentity{}, fmt.Errorf("unexpected server identity %q", out)
	}
	pid, err := strconv.Atoi(parts[0])
	if err != nil || pid <= 0 {
		return serverIdentity{}, fmt.Errorf("invalid pid %q", parts[0])
	}
	return serverIdentity{PID: pid, SocketPath: parts[1], Version: parts[2]}, nil
}

func (f *nativeFixture) attachHuman(ctx context.Context, readOnly bool) (*humanClient, error) {
	return f.attachHumanWithOptions(ctx, humanAttachOptions{Session: f.Session, ReadOnly: readOnly, PreserveEnvironment: true})
}

type humanAttachOptions struct {
	Session             string
	ReadOnly            bool
	PreserveEnvironment bool
	Environment         map[string]string
}

func (f *nativeFixture) attachHumanWithOptions(ctx context.Context, options humanAttachOptions) (*humanClient, error) {
	flags := "ignore-size"
	if options.ReadOnly {
		flags = "read-only,ignore-size"
	}
	session := options.Session
	if session == "" {
		session = f.Session
	}
	args := []string{"-S", f.SocketPath, "-f", "/dev/null", "attach-session"}
	if options.PreserveEnvironment {
		args = append(args, "-E")
	}
	args = append(args, "-f", flags, "-t", session)
	cmd := exec.CommandContext(ctx, f.Tmux, args...)
	overrides := map[string]string{"TERM": "xterm-256color"}
	for key, value := range options.Environment {
		overrides[key] = value
	}
	cmd.Env = environmentWithOverrides(os.Environ(), overrides)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		return nil, err
	}
	h := &humanClient{cmd: cmd, pty: ptmx, done: make(chan struct{})}
	go h.readLoop()
	return h, nil
}

func environmentWithOverride(env []string, key, value string) []string {
	return environmentWithOverrides(env, map[string]string{key: value})
}

func environmentWithOverrides(env []string, overrides map[string]string) []string {
	out := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, overridden := overrides[key]; overridden {
			continue
		}
		out = append(out, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, key+"="+overrides[key])
	}
	return out
}

func (h *humanClient) PID() int {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func (h *humanClient) readLoop() {
	defer close(h.done)
	buf := make([]byte, 32<<10)
	for {
		n, err := h.pty.Read(buf)
		if n > 0 {
			h.mu.Lock()
			h.output.Write(buf[:n])
			h.mu.Unlock()
		}
		if err != nil {
			h.mu.Lock()
			if !h.closed {
				h.readErr = err
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *humanClient) writeBytes(data []byte) error {
	_, err := h.pty.Write(data)
	return err
}

func (h *humanClient) writeString(s string) error { return h.writeBytes([]byte(s)) }

func (h *humanClient) setSize(rows, cols uint16) error {
	if rows == 0 || cols == 0 {
		return errors.New("human PTY size must be non-zero")
	}
	return pty.Setsize(h.pty, &pty.Winsize{Rows: rows, Cols: cols})
}

func (h *humanClient) outputString() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.output.String()
}

func (h *humanClient) waitContains(ctx context.Context, needle string) error {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		if strings.Contains(h.outputString(), needle) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.done:
			if strings.Contains(h.outputString(), needle) {
				return nil
			}
			h.mu.Lock()
			err := h.readErr
			h.mu.Unlock()
			if err == nil {
				err = errors.New("human client exited")
			}
			return err
		case <-ticker.C:
		}
	}
}

func (h *humanClient) close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()
	_ = h.pty.Close()
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-h.done:
		case <-time.After(100 * time.Millisecond):
			_ = h.cmd.Process.Kill()
			<-h.done
		}
		_ = h.cmd.Wait()
	}
	return nil
}

func (f *nativeFixture) clients(ctx context.Context) ([]clientFacts, error) {
	out, err := f.tmux(ctx, "list-clients", "-F", "#{client_name}|#{client_tty}|#{client_pid}|#{client_readonly}|#{client_flags}|#{client_width}|#{client_height}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	facts := make([]clientFacts, 0, len(lines))
	for _, line := range lines {
		fact, err := parseClientFactsLine(line)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, nil
}

func parseClientFactsLine(line string) (clientFacts, error) {
	parts := strings.SplitN(line, "|", 7)
	if len(parts) != 7 {
		return clientFacts{}, fmt.Errorf("invalid client facts %q", line)
	}
	pid, err := strconv.Atoi(parts[2])
	if err != nil || pid <= 0 {
		return clientFacts{}, fmt.Errorf("invalid client pid %q", parts[2])
	}
	controlMode := hasClientFlag(parts[4], "control-mode")
	width, err := parseClientDimension(parts[5], "width", controlMode)
	if err != nil {
		return clientFacts{}, err
	}
	height, err := parseClientDimension(parts[6], "height", controlMode)
	if err != nil {
		return clientFacts{}, err
	}
	return clientFacts{Name: parts[0], TTY: parts[1], PID: pid, ReadOnly: parts[3] == "1", Flags: parts[4], Width: width, Height: height}, nil
}

func parseClientDimension(raw, name string, allowUnavailable bool) (int, error) {
	if raw == "" && allowUnavailable {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid client %s %q", name, raw)
	}
	return value, nil
}

func hasClientFlag(flags, want string) bool {
	for _, flag := range strings.Split(flags, ",") {
		if flag == want {
			return true
		}
	}
	return false
}

func (f *nativeFixture) waitClientByPID(ctx context.Context, pid int) (clientFacts, error) {
	return f.waitClient(ctx, func(c clientFacts) bool { return c.PID == pid })
}

func (f *nativeFixture) clientByName(ctx context.Context, name string) (clientFacts, error) {
	clients, err := f.clients(ctx)
	if err != nil {
		return clientFacts{}, err
	}
	return selectExactClient(clients, name)
}

func selectExactClient(clients []clientFacts, name string) (clientFacts, error) {
	if name == "" {
		return clientFacts{}, errors.New("empty exact client name")
	}
	var match *clientFacts
	for i := range clients {
		if clients[i].Name != name {
			continue
		}
		if match != nil {
			return clientFacts{}, fmt.Errorf("ambiguous client name %q", name)
		}
		copy := clients[i]
		match = &copy
	}
	if match == nil {
		return clientFacts{}, fmt.Errorf("client %q not found", name)
	}
	return *match, nil
}

func (f *nativeFixture) waitClientReadOnly(ctx context.Context, name string, want bool) (clientFacts, error) {
	return f.waitClient(ctx, func(c clientFacts) bool { return c.Name == name && c.ReadOnly == want })
}

func (f *nativeFixture) waitClient(ctx context.Context, predicate func(clientFacts) bool) (clientFacts, error) {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		clients, err := f.clients(ctx)
		if err == nil {
			for _, c := range clients {
				if predicate(c) {
					return c, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return clientFacts{}, fmt.Errorf("wait client: %w (last tmux error: %v)", ctx.Err(), err)
			}
			return clientFacts{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (f *nativeFixture) setClientReadOnly(ctx context.Context, name string, readOnly bool) error {
	if name == "" {
		return errors.New("empty client name")
	}
	current, err := f.clientByName(ctx, name)
	if err != nil {
		return err
	}
	if current.ReadOnly == readOnly {
		return nil
	}
	// tmux 3.6a acknowledges refresh-client -f !read-only for a normal
	// terminal client without changing the flag. switch-client -c ... -r is
	// the documented exact-client toggle for terminal clients. -E preserves
	// the delegated session environment while changing presentation state.
	_, err = f.tmux(ctx, "switch-client", "-E", "-c", name, "-r")
	return err
}

func (f *nativeFixture) bindSameClientFence(ctx context.Context, key string) error {
	// H0-only measurement primitive: the key is consumed by tmux on the
	// human client's own input stream, then switch-client toggles that
	// current client read-only. -E keeps attachment/control presentation
	// from refreshing the delegated session environment.
	_, err := f.tmux(ctx, "bind-key", "-n", key, "switch-client", "-E", "-r")
	return err
}
