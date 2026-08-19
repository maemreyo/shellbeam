package terminalpresentation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const (
	maxLSAppInfoOutput = 32 << 10
	maxCommandTimeout  = 5 * time.Second
)

type RunningConfig struct {
	QueryPath      string
	Providers      []core.TerminalIdentity
	CommandTimeout time.Duration
}

type RunningSource struct {
	path           string
	providers      []core.TerminalIdentity
	commandTimeout time.Duration
}

func NewRunningSource(config RunningConfig) (*RunningSource, error) {
	if !validQueryConfig(config.QueryPath, config.CommandTimeout) || len(config.Providers) == 0 {
		return nil, errors.New("invalid running terminal source configuration")
	}
	providers, err := validateProviders(config.Providers)
	if err != nil {
		return nil, err
	}
	return &RunningSource{path: config.QueryPath, providers: providers, commandTimeout: config.CommandTimeout}, nil
}

func (s *RunningSource) Running(ctx context.Context) ([]core.TerminalIdentity, error) {
	if s == nil {
		return nil, errors.New("nil running terminal source")
	}
	result := make([]core.TerminalIdentity, 0, len(s.providers))
	for _, identity := range s.providers {
		output, err := runBoundedCommand(ctx, s.commandTimeout, s.path, "find", "bundleid="+identity.BundleID)
		if err != nil {
			return nil, fmt.Errorf("query running terminal: %w", err)
		}
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			continue
		}
		for _, line := range strings.Split(trimmed, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "ASN:") {
				return nil, errors.New("unexpected running terminal query shape")
			}
		}
		result = append(result, identity)
	}
	return result, nil
}

func validateProviders(values []core.TerminalIdentity) ([]core.TerminalIdentity, error) {
	result := make([]core.TerminalIdentity, len(values))
	seen := make(map[string]struct{}, len(values))
	for i, identity := range values {
		if err := identity.Validate(); err != nil {
			return nil, err
		}
		if identity.Platform != core.PlatformDarwin || identity.BundleID == "" {
			return nil, errors.New("running terminal provider is not Darwin-qualified")
		}
		if _, ok := seen[identity.BundleID]; ok {
			return nil, errors.New("duplicate running terminal bundle identity")
		}
		seen[identity.BundleID] = struct{}{}
		result[i] = identity
	}
	return result, nil
}

func validQueryConfig(path string, timeout time.Duration) bool {
	return filepath.IsAbs(path) && timeout > 0 && timeout <= maxCommandTimeout
}

type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = b.Buffer.Write(data)
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func runBoundedCommand(ctx context.Context, timeout time.Duration, path string, args ...string) ([]byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	buffer := &boundedBuffer{limit: maxLSAppInfoOutput}
	cmd := exec.CommandContext(probeCtx, path, args...)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("lsappinfo command failed: %w", err)
	}
	if buffer.truncated {
		return nil, errors.New("lsappinfo output exceeds bound")
	}
	return append([]byte(nil), buffer.Bytes()...), nil
}
