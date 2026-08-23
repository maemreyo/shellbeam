package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type controlClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr bytes.Buffer

	cmdMu   sync.Mutex
	mu      sync.Mutex
	events  []ControlEvent
	readErr error
	closed  bool

	results chan ControlEvent
	done    chan struct{}
}

func (f *nativeFixture) attachControl(ctx context.Context, session, flags string) (*controlClient, error) {
	c, err := f.startControl(ctx, session, flags)
	if err != nil {
		return nil, err
	}
	if err := c.waitReady(ctx, f); err != nil {
		_ = c.close()
		return nil, err
	}
	return c, nil
}

// startControl launches the raw Control Mode client but deliberately does not
// wait for the attach command to complete. H0 P5 uses this split to race a
// deterministic producer against an attach whose privacy flags are already in
// the attach-session command line.
func (f *nativeFixture) startControl(ctx context.Context, session, flags string) (*controlClient, error) {
	return f.startControlWithEnvironment(ctx, session, flags, nil)
}

func (f *nativeFixture) startControlWithEnvironment(ctx context.Context, session, flags string, environment map[string]string) (*controlClient, error) {
	if session == "" {
		session = f.Session
	}
	args := []string{"-S", f.SocketPath, "-f", "/dev/null", "-C", "attach-session", "-E"}
	if flags != "" {
		args = append(args, "-f", flags)
	}
	args = append(args, "-t", session)
	cmd := exec.CommandContext(ctx, f.Tmux, args...)
	if len(environment) != 0 {
		cmd.Env = environmentWithOverrides(os.Environ(), environment)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &controlClient{
		cmd:     cmd,
		stdin:   stdin,
		results: make(chan ControlEvent, 16),
		done:    make(chan struct{}),
	}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go c.readLoop(stdout)
	return c, nil
}

func (c *controlClient) waitReady(ctx context.Context, f *nativeFixture) error {
	startup, err := c.waitCommandResult(ctx)
	if err != nil {
		return fmt.Errorf("control attach startup: %w; stderr=%s", err, strings.TrimSpace(c.stderr.String()))
	}
	if startup.Kind == EventCommandError {
		return fmt.Errorf("control attach failed: %s; stderr=%s", startup.Data, strings.TrimSpace(c.stderr.String()))
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return errors.New("control client has no process identity")
	}
	if _, err := f.waitClientByPID(ctx, c.cmd.Process.Pid); err != nil {
		return fmt.Errorf("control client identity: %w; stderr=%s", err, strings.TrimSpace(c.stderr.String()))
	}
	return nil
}

func (c *controlClient) readLoop(r io.Reader) {
	defer close(c.done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxControlLineBytes)
	var block *commandBlock
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if len(line) >= maxControlLineBytes {
			c.setReadErr(fmt.Errorf("control line too long at line %d", lineNo))
			return
		}
		if block != nil {
			if strings.HasPrefix(line, "%begin ") {
				c.setReadErr(fmt.Errorf("nested %%begin at line %d", lineNo))
				return
			}
			if strings.HasPrefix(line, "%end ") || strings.HasPrefix(line, "%error ") {
				if err := c.finishCommandBlock(lineNo, line, block); err != nil {
					c.setReadErr(err)
					return
				}
				block = nil
				continue
			}
			if strings.HasPrefix(line, "%") {
				c.setReadErr(fmt.Errorf("notification inside command block at line %d: %s", lineNo, line))
				return
			}
			block.lines = append(block.lines, line)
			continue
		}

		if strings.HasPrefix(line, "%begin ") {
			number, err := parseBegin(line)
			if err != nil {
				c.setReadErr(fmt.Errorf("line %d: %w", lineNo, err))
				return
			}
			block = &commandBlock{number: number}
			continue
		}
		if !strings.HasPrefix(line, "%") {
			c.setReadErr(fmt.Errorf("unexpected control text outside command block at line %d: %q", lineNo, line))
			return
		}
		event, err := parseNotification(line)
		if err != nil {
			c.setReadErr(fmt.Errorf("line %d: %w", lineNo, err))
			return
		}
		c.appendEvent(event)
	}
	if err := scanner.Err(); err != nil {
		c.setReadErr(err)
		return
	}
	if block != nil {
		c.setReadErr(fmt.Errorf("EOF mid-block for command %d", block.number))
	}
}

func (c *controlClient) finishCommandBlock(lineNo int, line string, block *commandBlock) error {
	number, err := parseBlockTerminator(line)
	if err != nil {
		return fmt.Errorf("line %d: %w", lineNo, err)
	}
	if number != block.number {
		return fmt.Errorf("command number mismatch at line %d: begin=%d end=%d", lineNo, block.number, number)
	}
	kind := EventCommandEnd
	if strings.HasPrefix(line, "%error ") {
		kind = EventCommandError
	}
	event := ControlEvent{Kind: kind, CommandNumber: block.number, Data: strings.Join(block.lines, "\n")}
	c.appendEvent(event)
	select {
	case c.results <- event:
		return nil
	case <-c.done:
		return errors.New("control client closed while delivering command result")
	default:
		return errors.New("control command result queue overflow")
	}
}

func (c *controlClient) setReadErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readErr == nil {
		c.readErr = err
	}
}

func (c *controlClient) appendEvent(event ControlEvent) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

func (c *controlClient) waitCommandResult(ctx context.Context) (ControlEvent, error) {
	select {
	case result := <-c.results:
		return result, nil
	case <-c.done:
		c.mu.Lock()
		err := c.readErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("control client exited")
		}
		return ControlEvent{}, err
	case <-ctx.Done():
		return ControlEvent{}, ctx.Err()
	}
}

func (c *controlClient) command(ctx context.Context, command string) (ControlEvent, error) {
	if strings.ContainsAny(command, "\r\n") || strings.TrimSpace(command) == "" {
		return ControlEvent{}, errors.New("control command must be one non-empty line")
	}
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	if _, err := io.WriteString(c.stdin, command+"\n"); err != nil {
		return ControlEvent{}, err
	}
	result, err := c.waitCommandResult(ctx)
	if err != nil {
		return ControlEvent{}, err
	}
	if result.Kind == EventCommandError {
		return result, fmt.Errorf("tmux control command %q: %s", command, result.Data)
	}
	return result, nil
}

func (c *controlClient) eventsSnapshot() []ControlEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ControlEvent(nil), c.events...)
}

func (c *controlClient) clearEvents() {
	c.mu.Lock()
	c.events = nil
	c.mu.Unlock()
}

func (c *controlClient) setPaneOutput(ctx context.Context, paneID string, enabled bool) error {
	state := "off"
	if enabled {
		state = "on"
	}
	// Control Mode accepts a tmux command language line, not an argv vector.
	// pane:state must be quoted because pane IDs begin with '%' and the text
	// parser otherwise rejects the argument on tmux 3.6a.
	arg := strconv.Quote(paneID + ":" + state)
	_, err := c.command(ctx, "refresh-client -A "+arg)
	return err
}

func (c *controlClient) paneOutputContains(paneID, marker string) bool {
	for _, event := range c.eventsSnapshot() {
		if event.Kind == EventPaneOutput && event.PaneID == paneID && strings.Contains(event.Data, marker) {
			return true
		}
	}
	return false
}

func (c *controlClient) anyPaneOutputContains(marker string) bool {
	for _, event := range c.eventsSnapshot() {
		if event.Kind == EventPaneOutput && strings.Contains(event.Data, marker) {
			return true
		}
	}
	return false
}

func (c *controlClient) waitPaneOutput(ctx context.Context, paneID, marker string) error {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		if c.paneOutputContains(paneID, marker) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			if c.paneOutputContains(paneID, marker) {
				return nil
			}
			c.mu.Lock()
			err := c.readErr
			c.mu.Unlock()
			if err == nil {
				err = errors.New("control client exited")
			}
			return err
		case <-ticker.C:
		}
	}
}

func (c *controlClient) paneIDsObserved() []string {
	seen := map[string]struct{}{}
	for _, event := range c.eventsSnapshot() {
		if event.Kind == EventPaneOutput && event.PaneID != "" {
			seen[event.PaneID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *controlClient) publicPaneOutput(privatePanes map[string]bool) string {
	var b strings.Builder
	for _, event := range c.eventsSnapshot() {
		if event.Kind != EventPaneOutput || privatePanes[event.PaneID] {
			continue
		}
		b.WriteString(event.Data)
	}
	return b.String()
}

func (c *controlClient) close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.stdin.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-c.done:
		case <-time.After(250 * time.Millisecond):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
		_ = c.cmd.Wait()
	}
	return nil
}

func (f *nativeFixture) createSession(ctx context.Context, name, command string) error {
	_, err := f.tmux(ctx, "new-session", "-d", "-s", name, command)
	return err
}

func (f *nativeFixture) splitPane(ctx context.Context, session, command string) (string, error) {
	out, err := f.tmux(ctx, "split-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, command)
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(string(out))
	if !strings.HasPrefix(paneID, "%") {
		return "", fmt.Errorf("invalid pane id %q", paneID)
	}
	return paneID, nil
}

func (f *nativeFixture) paneIDs(ctx context.Context, session string) ([]string, error) {
	out, err := f.tmux(ctx, "list-panes", "-t", session, "-F", "#{pane_id}")
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "%") {
			return nil, fmt.Errorf("invalid pane id %q", line)
		}
		ids = append(ids, line)
	}
	return ids, nil
}

func (f *nativeFixture) paneForSession(ctx context.Context, session string) (string, error) {
	ids, err := f.paneIDs(ctx, session)
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("session %q has %d panes, want 1", session, len(ids))
	}
	return ids[0], nil
}

func (f *nativeFixture) paneSize(ctx context.Context, paneID string) (int, int, error) {
	out, err := f.tmux(ctx, "display-message", "-p", "-t", paneID, "#{pane_width}|#{pane_height}")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "|")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected pane size %q", out)
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

func (f *nativeFixture) resizeWindowManual(ctx context.Context, session string, width, height int) error {
	if width <= 0 || height <= 0 {
		return errors.New("window size must be positive")
	}
	_, err := f.tmux(ctx, "resize-window", "-t", session, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	return err
}

func (f *nativeFixture) respawnPane(ctx context.Context, paneID, command string) error {
	_, err := f.tmux(ctx, "respawn-pane", "-k", "-t", paneID, command)
	return err
}

func (f *nativeFixture) emitMarker(ctx context.Context, paneID, marker string) error {
	if strings.ContainsAny(marker, "\r\n") {
		return errors.New("marker must be one line")
	}
	if _, err := f.tmux(ctx, "send-keys", "-l", "-t", paneID, marker); err != nil {
		return err
	}
	_, err := f.tmux(ctx, "send-keys", "-t", paneID, "Enter")
	return err
}

func (f *nativeFixture) setSessionEnvironment(ctx context.Context, session, key, value string) error {
	_, err := f.tmux(ctx, "set-environment", "-t", session, key, value)
	return err
}

func (f *nativeFixture) sessionEnvironment(ctx context.Context, session, key string) (string, error) {
	out, err := f.tmux(ctx, "show-environment", "-t", session, key)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(out))
	prefix := key + "="
	if !strings.HasPrefix(line, prefix) {
		return "", fmt.Errorf("unexpected environment line %q", line)
	}
	return strings.TrimPrefix(line, prefix), nil
}
