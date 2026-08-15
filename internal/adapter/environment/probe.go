package environment

import (
	"context"
	"errors"
	"os/exec"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

const (
	ProbeTimeout        = 2 * time.Second
	MaxProbeOutputBytes = 512
)

type CommandResult struct {
	Output     []byte
	Executable string
}

type CommandRunner interface {
	Run(context.Context, []string, int) (CommandResult, error)
}

type Prober struct {
	runner CommandRunner
}

type probeSpec struct {
	argv []string
}

var probeRegistry = map[string]probeSpec{
	"go":     {argv: []string{"go", "env", "GOVERSION"}},
	"node":   {argv: []string{"node", "--version"}},
	"python": {argv: []string{"python3", "--version"}},
	"java":   {argv: []string{"java", "-version"}},
	"rust":   {argv: []string{"rustc", "--version"}},
}

func NewProber(runner CommandRunner) *Prober {
	if runner == nil {
		runner = hostCommandRunner{}
	}
	return &Prober{runner: runner}
}

func NewHostProber() *Prober {
	return NewProber(hostCommandRunner{})
}

func (p *Prober) Probe(ctx context.Context, kind, requestedIdentity string, _ project.Toolchain) core.ToolchainObservation {
	if requestedIdentity == "" {
		requestedIdentity = "host"
	}
	base := core.ToolchainObservation{Kind: kind, RequestedIdentity: requestedIdentity}
	spec, ok := probeRegistry[kind]
	if !ok {
		base.Quality = core.ProbeUnavailable
		base.DiagnosticCode = string(failure.ToolchainProbeUnsupported)
		return base
	}
	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	result, err := p.runner.Run(probeCtx, append([]string(nil), spec.argv...), MaxProbeOutputBytes)
	if err != nil {
		base.Quality = core.ProbeUnavailable
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			base.DiagnosticCode = string(failure.ToolchainProbeTimeout)
		} else {
			base.DiagnosticCode = string(failure.ToolchainProbeUnavailable)
		}
		return base
	}
	version, err := parseProbeVersion(kind, result.Output)
	if err != nil || result.Executable == "" {
		base.Quality = core.ProbeUnavailable
		base.DiagnosticCode = string(failure.ToolchainProbeUnavailable)
		return base
	}
	base.ObservedIdentity = result.Executable
	base.Version = version
	base.Quality = core.ProbeComplete
	return base
}

type hostCommandRunner struct{}

func (hostCommandRunner) Run(ctx context.Context, argv []string, maxBytes int) (CommandResult, error) {
	if len(argv) == 0 || maxBytes < 1 {
		return CommandResult{}, exec.ErrNotFound
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return CommandResult{}, err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	output := newLimitedBuffer(maxBytes)
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Run(); err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Output: output.Bytes(), Executable: path}, nil
}

type limitedBuffer struct {
	limit int
	data  []byte
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	remaining := b.limit - len(b.data)
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.data = append(b.data, p[:remaining]...)
	}
	return original, nil
}

func (b *limitedBuffer) Bytes() []byte {
	return append([]byte(nil), b.data...)
}
