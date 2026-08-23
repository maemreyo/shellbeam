package delegatedtmux

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
)

const (
	maxControlLineBytes     = 64 << 10
	maxPreTargetOutputBytes = 256 << 10
)

type eventKind string

const (
	eventCommandEnd     eventKind = "command_end"
	eventCommandError   eventKind = "command_error"
	eventPaneOutput     eventKind = "pane_output"
	eventMessage        eventKind = "message"
	eventClientDetached eventKind = "client_detached"
	eventExit           eventKind = "exit"
	eventUnknown        eventKind = "unknown"
)

type controlEvent struct {
	Kind          eventKind
	CommandNumber int
	PaneID        string
	Data          string
	Raw           string
}

type commandBlock struct {
	number int
	lines  []string
}

func parseControl(r io.Reader) ([]controlEvent, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024), maxControlLineBytes)
	var events []controlEvent
	var block *commandBlock
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		event, next, err := parseControlLine(strings.TrimSuffix(scanner.Text(), "\r"), block, lineNo)
		if err != nil {
			return nil, err
		}
		block = next
		if event != nil {
			events = append(events, *event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if block != nil {
		return nil, fmt.Errorf("EOF mid-block for command %d", block.number)
	}
	return events, nil
}

func parseControlLine(line string, block *commandBlock, lineNo int) (*controlEvent, *commandBlock, error) {
	if len(line) >= maxControlLineBytes {
		return nil, block, fmt.Errorf("control line too long at line %d", lineNo)
	}
	if block != nil {
		if strings.HasPrefix(line, "%begin ") {
			return nil, block, fmt.Errorf("nested %%begin at line %d", lineNo)
		}
		if strings.HasPrefix(line, "%end ") || strings.HasPrefix(line, "%error ") {
			number, err := parseBlockTerminator(line)
			if err != nil {
				return nil, block, fmt.Errorf("line %d: %w", lineNo, err)
			}
			if number != block.number {
				return nil, block, fmt.Errorf("command number mismatch at line %d", lineNo)
			}
			kind := eventCommandEnd
			if strings.HasPrefix(line, "%error ") {
				kind = eventCommandError
			}
			return &controlEvent{Kind: kind, CommandNumber: number, Data: strings.Join(block.lines, "\n")}, nil, nil
		}
		if strings.HasPrefix(line, "%") {
			return nil, block, fmt.Errorf("notification inside command block at line %d", lineNo)
		}
		block.lines = append(block.lines, line)
		return nil, block, nil
	}
	if strings.HasPrefix(line, "%begin ") {
		number, err := parseBegin(line)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		return nil, &commandBlock{number: number}, nil
	}
	if !strings.HasPrefix(line, "%") {
		return nil, nil, fmt.Errorf("unexpected control text outside command block at line %d", lineNo)
	}
	event, err := parseNotification(line)
	if err != nil {
		return nil, nil, fmt.Errorf("line %d: %w", lineNo, err)
	}
	return &event, nil, nil
}

func parseBegin(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || fields[0] != "%begin" {
		return 0, fmt.Errorf("malformed %%begin %q", line)
	}
	if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed %%begin timestamp")
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("malformed %%begin command number")
	}
	if _, err := strconv.ParseUint(fields[3], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed %%begin flags")
	}
	return n, nil
}

func parseBlockTerminator(line string) (int, error) {
	fields := strings.Fields(line)
	if len(fields) != 4 || (fields[0] != "%end" && fields[0] != "%error") {
		return 0, fmt.Errorf("malformed block terminator %q", line)
	}
	if _, err := strconv.ParseInt(fields[1], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed block timestamp")
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("malformed command number")
	}
	if _, err := strconv.ParseUint(fields[3], 10, 64); err != nil {
		return 0, fmt.Errorf("malformed block flags")
	}
	return n, nil
}

func parseNotification(line string) (controlEvent, error) {
	switch {
	case strings.HasPrefix(line, "%output "):
		rest := strings.TrimPrefix(line, "%output ")
		space := strings.IndexByte(rest, ' ')
		if space <= 0 {
			return controlEvent{}, fmt.Errorf("malformed %%output")
		}
		pane := rest[:space]
		if len(pane) < 2 || pane[0] != '%' {
			return controlEvent{}, fmt.Errorf("malformed pane id")
		}
		decoded, err := decodeControlOutput(rest[space+1:])
		if err != nil {
			return controlEvent{}, err
		}
		return controlEvent{Kind: eventPaneOutput, PaneID: pane, Data: decoded}, nil
	case strings.HasPrefix(line, "%message"):
		return controlEvent{Kind: eventMessage, Data: notificationData(line, "%message")}, nil
	case strings.HasPrefix(line, "%client-detached"):
		return controlEvent{Kind: eventClientDetached, Data: notificationData(line, "%client-detached")}, nil
	case strings.HasPrefix(line, "%exit"):
		return controlEvent{Kind: eventExit, Data: notificationData(line, "%exit")}, nil
	case strings.HasPrefix(line, "%end ") || strings.HasPrefix(line, "%error "):
		return controlEvent{}, fmt.Errorf("block terminator without %%begin")
	default:
		return controlEvent{Kind: eventUnknown, Raw: line}, nil
	}
}

func notificationData(line, prefix string) string {
	return strings.TrimPrefix(strings.TrimPrefix(line, prefix), " ")
}
func decodeControlOutput(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+3 >= len(s) || !isOctal(s[i+1]) || !isOctal(s[i+2]) || !isOctal(s[i+3]) {
			return "", fmt.Errorf("invalid control output escape at byte %d", i)
		}
		v := int(s[i+1]-'0')*64 + int(s[i+2]-'0')*8 + int(s[i+3]-'0')
		if v > 255 {
			return "", fmt.Errorf("invalid control output escape at byte %d", i)
		}
		b.WriteByte(byte(v))
		i += 4
	}
	return b.String(), nil
}
func isOctal(b byte) bool { return b >= '0' && b <= '7' }

func quoteTmuxArg(v string) (string, error) {
	if strings.IndexByte(v, 0) >= 0 {
		return "", errors.New("tmux argument contains NUL")
	}
	var b strings.Builder
	b.Grow(len(v) + 2)
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch c {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '$':
			b.WriteString("\\$")
		case '\n', '\r', '\t':
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "\\%03o", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}

type controlClient struct {
	cmd                *exec.Cmd
	stdin              io.WriteCloser
	stderr             bytes.Buffer
	results            chan controlEvent
	done               chan struct{}
	cmdMu              sync.Mutex
	mu                 sync.Mutex
	readErr            error
	closed             bool
	paneID             string
	sink               app.OutputSink
	outputBytes        atomic.Int64
	sharedOutputBytes  *atomic.Int64
	pending            []controlEvent
	pendingBytes       int
	privateObservation bool
}

func newControlClient(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.Reader) *controlClient {
	c := &controlClient{cmd: cmd, stdin: stdin, results: make(chan controlEvent, 64), done: make(chan struct{})}
	if cmd != nil {
		cmd.Stderr = &c.stderr
	}
	go c.readLoop(stdout)
	return c
}

func (c *controlClient) outputCounter() *atomic.Int64 {
	if c.sharedOutputBytes != nil {
		return c.sharedOutputBytes
	}
	return &c.outputBytes
}

func (c *controlClient) shareOutputCounter(old *controlClient) {
	if old != nil {
		c.sharedOutputBytes = old.outputCounter()
	}
}

func (c *controlClient) outputByteCount() int64 { return c.outputCounter().Load() }
func (c *controlClient) addOutputBytes(n int64) { c.outputCounter().Add(n) }

func (c *controlClient) targetSnapshot() (string, app.OutputSink) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paneID, c.sink
}

func (c *controlClient) isPrivateObservation() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.privateObservation
}

func (c *controlClient) setPrivateObservation(value bool) {
	c.mu.Lock()
	c.privateObservation = value
	c.mu.Unlock()
}

func (c *controlClient) setTarget(pane string, sink app.OutputSink) error {
	c.mu.Lock()
	if pane == "" {
		c.mu.Unlock()
		return errors.New("empty control target pane")
	}
	c.paneID = pane
	c.sink = sink
	pending := append([]controlEvent(nil), c.pending...)
	c.pending = nil
	c.pendingBytes = 0
	c.mu.Unlock()
	for _, event := range pending {
		if event.PaneID == pane && sink != nil {
			data := []byte(event.Data)
			if err := sink.Append(data); err != nil {
				c.setReadErr(fmt.Errorf("output sink: %w", err))
				return err
			}
			c.addOutputBytes(int64(len(data)))
		}
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
		event, next, err := parseControlLine(strings.TrimSuffix(scanner.Text(), "\r"), block, lineNo)
		if err != nil {
			c.setReadErr(err)
			return
		}
		block = next
		if event == nil {
			continue
		}
		if event.Kind == eventCommandEnd || event.Kind == eventCommandError {
			select {
			case c.results <- *event:
			case <-c.done:
				return
			default:
				c.setReadErr(errors.New("control command result queue overflow"))
				return
			}
			continue
		}
		if event.Kind == eventPaneOutput {
			c.deliverOutput(*event)
		}
	}
	if err := scanner.Err(); err != nil {
		c.setReadErr(err)
		return
	}
	if block != nil {
		c.setReadErr(fmt.Errorf("EOF mid-block for command %d", block.number))
	}
}
func (c *controlClient) deliverOutput(event controlEvent) {
	c.mu.Lock()
	pane, sink := c.paneID, c.sink
	if pane == "" {
		c.pendingBytes += len(event.Data)
		if c.pendingBytes > maxPreTargetOutputBytes {
			c.mu.Unlock()
			c.setReadErr(errors.New("pre-target output buffer exceeded"))
			return
		}
		c.pending = append(c.pending, event)
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if event.PaneID != pane || sink == nil {
		return
	}
	data := []byte(event.Data)
	if len(data) == 0 {
		return
	}
	if err := sink.Append(data); err != nil {
		c.setReadErr(fmt.Errorf("output sink: %w", err))
		return
	}
	c.addOutputBytes(int64(len(data)))
}
func (c *controlClient) setReadErr(err error) {
	c.mu.Lock()
	if c.readErr == nil {
		c.readErr = err
	}
	c.mu.Unlock()
}
func (c *controlClient) waitResult(ctx context.Context) (controlEvent, error) {
	select {
	case r := <-c.results:
		return r, nil
	case <-c.done:
		c.mu.Lock()
		err := c.readErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("control client exited")
		}
		return controlEvent{}, err
	case <-ctx.Done():
		return controlEvent{}, ctx.Err()
	}
}
func (c *controlClient) command(ctx context.Context, command string) (controlEvent, error) {
	if command == "" || strings.ContainsAny(command, "\r\n") {
		return controlEvent{}, errors.New("control command must be one non-empty line")
	}
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	if _, err := io.WriteString(c.stdin, command+"\n"); err != nil {
		return controlEvent{}, err
	}
	result, err := c.waitResult(ctx)
	if err != nil {
		return controlEvent{}, err
	}
	if result.Kind == eventCommandError {
		return result, fmt.Errorf("tmux control command failed: %s", result.Data)
	}
	return result, nil
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
		waited := make(chan error, 1)
		go func() { waited <- c.cmd.Wait() }()
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
		select {
		case err := <-waited:
			return err
		case <-timer.C:
			_ = c.cmd.Process.Kill()
			return <-waited
		}
	}
	return nil
}
