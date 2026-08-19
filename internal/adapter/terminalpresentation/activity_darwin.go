//go:build darwin

package terminalpresentation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const maxActivityEventBytes = 64 << 10

type DarwinConfig struct {
	LSAppInfoPath  string
	Providers      []core.TerminalIdentity
	CommandTimeout time.Duration
	Now            func() time.Time
}

type DarwinActivitySource struct {
	path           string
	providers      map[string]core.TerminalIdentity
	commandTimeout time.Duration
	now            func() time.Time
}

func NewDarwinActivitySource(config DarwinConfig) (*DarwinActivitySource, error) {
	if !validQueryConfig(config.LSAppInfoPath, config.CommandTimeout) || config.Now == nil || len(config.Providers) == 0 {
		return nil, errors.New("invalid Darwin activity source configuration")
	}
	providers, err := validateProviders(config.Providers)
	if err != nil {
		return nil, err
	}
	byBundle := make(map[string]core.TerminalIdentity, len(providers))
	for _, identity := range providers {
		byBundle[identity.BundleID] = identity
	}
	return &DarwinActivitySource{path: config.LSAppInfoPath, providers: byBundle, commandTimeout: config.CommandTimeout, now: config.Now}, nil
}

func (s *DarwinActivitySource) Current(ctx context.Context) (app.ForegroundObservation, error) {
	if s == nil {
		return app.ForegroundObservation{}, errors.New("nil Darwin activity source")
	}
	frontOutput, err := runBoundedCommand(ctx, s.commandTimeout, s.path, "front")
	if err != nil {
		return app.ForegroundObservation{}, err
	}
	front := strings.TrimSpace(string(frontOutput))
	if front == "" || strings.Contains(front, "\n") || !strings.HasPrefix(front, "ASN:") {
		return app.ForegroundObservation{}, errors.New("unexpected front application shape")
	}
	infoOutput, err := runBoundedCommand(ctx, s.commandTimeout, s.path, "info", "-only", "bundleid", front)
	if err != nil {
		return app.ForegroundObservation{}, err
	}
	bundleID, ok := parseBundleID(string(infoOutput))
	if !ok {
		return app.ForegroundObservation{}, errors.New("front application bundle identity unavailable")
	}
	return s.observation(bundleID)
}

func (s *DarwinActivitySource) Run(ctx context.Context, emit func(app.ForegroundObservation) error) error {
	if s == nil || emit == nil {
		return errors.New("invalid Darwin activity stream")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	reader, writer := io.Pipe()
	cmd := exec.CommandContext(ctx, s.path, "listen", "+becameFrontmost", "forever")
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return fmt.Errorf("start LaunchServices activity listener: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = writer.Close()
		waitCh <- err
		close(waitCh)
	}()

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), maxActivityEventBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		bundleID, ok := parseBundleID(line)
		if !ok || !strings.Contains(line, "kLSNotifyBecameFrontmost") {
			return stopActivityListener(cmd, reader, waitCh, errors.New("unexpected LaunchServices activity event shape"))
		}
		observation, err := s.observation(bundleID)
		if err != nil {
			return stopActivityListener(cmd, reader, waitCh, err)
		}
		if err := emit(observation); err != nil {
			return stopActivityListener(cmd, reader, waitCh, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return stopActivityListener(cmd, reader, waitCh, fmt.Errorf("read LaunchServices activity stream: %w", err))
	}
	_ = reader.Close()
	waitErr := <-waitCh
	if ctx.Err() != nil {
		return nil
	}
	if waitErr != nil {
		return fmt.Errorf("LaunchServices activity listener exited: %w", waitErr)
	}
	return nil
}

func (s *DarwinActivitySource) observation(bundleID string) (app.ForegroundObservation, error) {
	observedAt := s.now()
	if observedAt.IsZero() {
		return app.ForegroundObservation{}, errors.New("Darwin activity clock returned zero time")
	}
	result := app.ForegroundObservation{ObservedAt: observedAt, Quality: core.QualityNative}
	if identity, ok := s.providers[bundleID]; ok {
		copy := identity
		result.Identity = &copy
	}
	return result, nil
}

func parseBundleID(value string) (string, bool) {
	const marker = `"CFBundleIdentifier"="`
	index := strings.Index(value, marker)
	if index < 0 {
		return "", false
	}
	rest := value[index+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end <= 0 {
		return "", false
	}
	bundleID := rest[:end]
	if strings.ContainsAny(bundleID, "\r\n\t ") {
		return "", false
	}
	return bundleID, true
}

func stopActivityListener(cmd *exec.Cmd, reader *io.PipeReader, waitCh <-chan error, cause error) error {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = reader.Close()
	<-waitCh
	return cause
}
