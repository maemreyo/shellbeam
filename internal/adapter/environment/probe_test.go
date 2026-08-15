package environment

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type fakeCommandRunner struct {
	calls     [][]string
	maxBytes  []int
	remaining []time.Duration
	result    CommandResult
	err       error
	block     bool
}

func (f *fakeCommandRunner) Run(ctx context.Context, argv []string, maxBytes int) (CommandResult, error) {
	f.calls = append(f.calls, append([]string(nil), argv...))
	f.maxBytes = append(f.maxBytes, maxBytes)
	if deadline, ok := ctx.Deadline(); ok {
		f.remaining = append(f.remaining, time.Until(deadline))
	}
	if f.block {
		<-ctx.Done()
		return CommandResult{}, ctx.Err()
	}
	return f.result, f.err
}

func TestProberUsesOnlyFixedRegistryAndBounds(t *testing.T) {
	tests := []struct {
		kind   string
		argv   []string
		output string
		want   string
	}{
		{"go", []string{"go", "env", "GOVERSION"}, "go1.26.5\n", "go1.26.5"},
		{"node", []string{"node", "--version"}, "v22.14.0\n", "v22.14.0"},
		{"python", []string{"python3", "--version"}, "Python 3.13.5\n", "3.13.5"},
		{"java", []string{"java", "-version"}, "openjdk version \"21.0.7\" 2025-04-15\n", "21.0.7"},
		{"rust", []string{"rustc", "--version"}, "rustc 1.88.0 (6b00bc388 2025-06-23)\n", "1.88.0"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			runner := &fakeCommandRunner{result: CommandResult{Output: []byte(tt.output), Executable: "/fixed/" + tt.argv[0]}}
			got := NewProber(runner).Probe(context.Background(), tt.kind, "host", project.Toolchain{})
			if got.Quality != core.ProbeComplete || got.Version != tt.want || got.ObservedIdentity != "/fixed/"+tt.argv[0] || got.DiagnosticCode != "" {
				t.Fatalf("observation=%#v", got)
			}
			if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], tt.argv) || runner.maxBytes[0] != MaxProbeOutputBytes {
				t.Fatalf("runner calls=%v max=%v", runner.calls, runner.maxBytes)
			}
			if len(runner.remaining) != 1 || runner.remaining[0] <= time.Second || runner.remaining[0] > ProbeTimeout {
				t.Fatalf("probe deadline remaining=%v", runner.remaining)
			}
		})
	}
}

func TestProberClassifiesTimeoutUnavailableUnsupportedAndParseFailure(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		runner := &fakeCommandRunner{block: true}
		got := NewProber(runner).Probe(context.Background(), "go", "host", project.Toolchain{})
		if got.Quality != core.ProbeUnavailable || got.DiagnosticCode != string(failure.ToolchainProbeTimeout) {
			t.Fatalf("timeout=%#v", got)
		}
	})
	t.Run("missing", func(t *testing.T) {
		runner := &fakeCommandRunner{err: errors.New("private executable lookup /Users/alice/bin/go token=secret")}
		got := NewProber(runner).Probe(context.Background(), "go", "host", project.Toolchain{})
		if got.Quality != core.ProbeUnavailable || got.DiagnosticCode != string(failure.ToolchainProbeUnavailable) {
			t.Fatalf("missing=%#v", got)
		}
		if strings.Contains(got.DiagnosticCode, "alice") || strings.Contains(got.DiagnosticCode, "secret") {
			t.Fatalf("error leaked into observation: %#v", got)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		runner := &fakeCommandRunner{}
		got := NewProber(runner).Probe(context.Background(), "ruby", "host", project.Toolchain{})
		if got.Quality != core.ProbeUnavailable || got.DiagnosticCode != string(failure.ToolchainProbeUnsupported) || len(runner.calls) != 0 {
			t.Fatalf("unsupported=%#v calls=%v", got, runner.calls)
		}
	})
	t.Run("parse", func(t *testing.T) {
		runner := &fakeCommandRunner{result: CommandResult{Output: []byte("secret malformed output"), Executable: "/usr/bin/go"}}
		got := NewProber(runner).Probe(context.Background(), "go", "host", project.Toolchain{})
		if got.Quality != core.ProbeUnavailable || got.DiagnosticCode != string(failure.ToolchainProbeUnavailable) || got.Version != "" || strings.Contains(got.DiagnosticCode, "secret") {
			t.Fatalf("parse failure=%#v", got)
		}
	})
}

func TestLimitedBufferStoresAtMostConfiguredBytes(t *testing.T) {
	buffer := newLimitedBuffer(4)
	if n, err := buffer.Write([]byte("123456789")); err != nil || n != 9 {
		t.Fatalf("write n=%d err=%v", n, err)
	}
	if got := string(buffer.Bytes()); got != "1234" {
		t.Fatalf("buffer=%q", got)
	}
}
