package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

type ProcessConfig struct {
	Executable      string
	Args            []string
	Dir             string
	Env             []string
	StderrBytes     int
	ShutdownTimeout time.Duration
}

type Session struct {
	Server protocol.Server
	Client *Client

	mu           sync.Mutex
	capabilities CapabilityState
	conn         jsonrpc2.Conn
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	waitCh       chan error
	stderr       *boundedRing
	closeOnce    sync.Once
	closeErr     error
	timeout      time.Duration
}

func StartProcess(ctx context.Context, config ProcessConfig, client *Client) (*Session, error) {
	if client == nil {
		return nil, fmt.Errorf("nil LSP client")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cmd := exec.Command(config.Executable, config.Args...)
	cmd.Dir = config.Dir
	cmd.Env = append([]string(nil), config.Env...)
	stderr := newBoundedRing(config.StderrBytes)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open LSP stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("open LSP stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start LSP process: %w", err)
	}

	lifetimeCtx, cancel := context.WithCancel(context.Background())
	stream := jsonrpc2.NewStream(&processReadWriteCloser{reader: stdout, writer: stdin})
	_, conn, server := protocol.NewClient(lifetimeCtx, client, stream)
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
		close(waitCh)
	}()
	return &Session{
		Server: server, Client: client, conn: conn, cmd: cmd, cancel: cancel,
		waitCh: waitCh, stderr: stderr, timeout: config.ShutdownTimeout,
	}, nil
}

func (c ProcessConfig) Validate() error {
	if c.Executable == "" || c.Dir == "" || !filepath.IsAbs(c.Dir) || c.Env == nil ||
		c.StderrBytes < 1 || c.StderrBytes > 1024*1024 ||
		c.ShutdownTimeout <= 0 || c.ShutdownTimeout > 10*time.Second {
		return fmt.Errorf("invalid LSP process config")
	}
	return nil
}

func (s *Session) SetInitializeResult(result *protocol.InitializeResult) CapabilityState {
	state := NegotiateCapabilities(result)
	s.mu.Lock()
	s.capabilities = state
	s.mu.Unlock()
	return state
}

func (s *Session) Capabilities() CapabilityState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capabilities
}

func (s *Session) Stderr() string {
	if s == nil || s.stderr == nil {
		return ""
	}
	return s.stderr.String()
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = s.close() })
	return s.closeErr
}

func (s *Session) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	var errs []error
	if s.Server != nil {
		if err := s.Server.Shutdown(ctx); err != nil && ctx.Err() == nil {
			errs = append(errs, fmt.Errorf("LSP shutdown: %w", err))
		}
		if ctx.Err() == nil {
			if err := s.Server.Exit(ctx); err != nil {
				errs = append(errs, fmt.Errorf("LSP exit: %w", err))
			}
		}
	}
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close LSP RPC: %w", err))
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.waitForProcess(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Session) waitForProcess(ctx context.Context) error {
	select {
	case err := <-s.waitCh:
		if err != nil {
			return fmt.Errorf("LSP process exit: %w", err)
		}
		return nil
	case <-ctx.Done():
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		select {
		case <-s.waitCh:
		case <-time.After(time.Second):
			return fmt.Errorf("LSP process reap timeout: %w", ctx.Err())
		}
		return fmt.Errorf("LSP process shutdown timeout: %w", ctx.Err())
	}
}

type processReadWriteCloser struct {
	reader io.ReadCloser
	writer io.WriteCloser
}

func (s *processReadWriteCloser) Read(p []byte) (int, error)  { return s.reader.Read(p) }
func (s *processReadWriteCloser) Write(p []byte) (int, error) { return s.writer.Write(p) }
func (s *processReadWriteCloser) Close() error {
	return errors.Join(s.writer.Close(), s.reader.Close())
}

type boundedRing struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func newBoundedRing(limit int) *boundedRing {
	return &boundedRing{limit: limit, buf: make([]byte, 0, limit)}
}

func (r *boundedRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	written := len(p)
	if len(p) >= r.limit {
		r.buf = append(r.buf[:0], p[len(p)-r.limit:]...)
		return written, nil
	}
	overflow := len(r.buf) + len(p) - r.limit
	if overflow > 0 {
		copy(r.buf, r.buf[overflow:])
		r.buf = r.buf[:len(r.buf)-overflow]
	}
	r.buf = append(r.buf, p...)
	return written, nil
}

func (r *boundedRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(append([]byte(nil), r.buf...))
}

var _ io.ReadWriteCloser = (*processReadWriteCloser)(nil)
var _ io.Writer = (*boundedRing)(nil)
