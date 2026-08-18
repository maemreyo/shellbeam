package bwrap

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	hermeticapp "github.com/maemreyo/shellbeam/internal/app/hermetic"
	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

type runtimeCaptureFake struct {
	view       hermeticapp.CapturedView
	captureErr error
	discards   int
}

func (f *runtimeCaptureFake) Capture(context.Context, string, core.Request) (hermeticapp.CapturedView, error) {
	return f.view, f.captureErr
}
func (f *runtimeCaptureFake) Discard(context.Context, hermeticapp.CapturedView) error {
	f.discards++
	return nil
}

type runtimeProviderFake struct {
	prepared     hermeticapp.PreparedExecution
	prepareErr   error
	discardCalls int
}

func (f *runtimeProviderFake) Prepare(context.Context, hermeticapp.PrepareExecutionRequest) (hermeticapp.PreparedExecution, error) {
	return f.prepared, f.prepareErr
}
func (f *runtimeProviderFake) Discard(context.Context, hermeticapp.PreparedExecution) error {
	f.discardCalls++
	return nil
}

type runtimeStarterFake struct {
	handle daemonapp.ProcessHandle
	spawn  receipt.SpawnEvidence
	status string
	err    error
	calls  int
}

func (f *runtimeStarterFake) StartPrivateHermetic(context.Context, hermeticapp.ProviderCommand, daemonapp.OutputSink) (daemonapp.ProcessHandle, receipt.SpawnEvidence, io.ReadCloser, error) {
	f.calls++
	if f.err != nil {
		return nil, f.spawn, nil, f.err
	}
	return f.handle, f.spawn, io.NopCloser(strings.NewReader(f.status)), nil
}

type runtimeHandleFake struct {
	exit    receipt.ExitEvidence
	signals []string
	waits   int
}

func (h *runtimeHandleFake) Write([]byte) error { return errors.New("stdin_closed") }
func (h *runtimeHandleFake) CloseStdin() error  { return nil }
func (h *runtimeHandleFake) Signal(name string) receipt.SignalEvidence {
	h.signals = append(h.signals, name)
	return receipt.SignalEvidence{Requested: name, Attempted: true, Succeeded: true}
}
func (h *runtimeHandleFake) Wait(context.Context) receipt.ExitEvidence { h.waits++; return h.exit }
func (h *runtimeHandleFake) Close() error                              { return nil }

type runtimeSink struct{}

func (runtimeSink) Append(context.Context, []byte) error { return nil }
func (runtimeSink) CaptureFailed(error)                  {}

func TestRuntimeRequiresPreExecProofAndPublishesCompleteBoundaryWithoutRewritingExit(t *testing.T) {
	capture, provider, prepared := runtimeFixture(t)
	zero := 0
	inner := &runtimeHandleFake{exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	starter := &runtimeStarterFake{handle: inner, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, status: "{\"child-pid\":4242}\n{\"exit-code\":0}\n"}
	runtime := newRuntime(capture, provider, starter, time.Second)
	got, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got.BoundaryID != prepared.BoundaryID {
		t.Fatalf("prepared=%#v", got)
	}
	h, spawn, err := runtime.Start(context.Background(), got, runtimeSink{})
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if exit.Code == nil || *exit.Code != 0 || exit.Signal != "" {
		t.Fatalf("literal exit rewritten: %#v", exit)
	}
	result := h.(interface{ HermeticBoundaryResult() core.BoundaryResult }).HermeticBoundaryResult()
	if !result.Authoritative() || result.BoundaryID != prepared.BoundaryID {
		t.Fatalf("boundary result=%#v", result)
	}
}

func TestRuntimeStatusLossAfterPreExecLosesContinuityButPreservesLiteralSignal(t *testing.T) {
	capture, provider, _ := runtimeFixture(t)
	inner := &runtimeHandleFake{exit: receipt.ExitEvidence{Reaped: true, Signal: "killed"}}
	starter := &runtimeStarterFake{handle: inner, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, status: "{\"child-pid\":4242}\n"}
	runtime := newRuntime(capture, provider, starter, time.Second)
	prepared, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	h, spawn, err := runtime.Start(context.Background(), prepared, runtimeSink{})
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if exit.Signal != "killed" || exit.Code != nil {
		t.Fatalf("literal signal rewritten: %#v", exit)
	}
	result := h.(interface{ HermeticBoundaryResult() core.BoundaryResult }).HermeticBoundaryResult()
	if !result.EstablishedPreExec || result.Continuity != core.ContinuityLost || result.Authoritative() {
		t.Fatalf("boundary result=%#v", result)
	}
}

func TestRuntimeRefusesSpawnWhenStatusEndsBeforePreExecProof(t *testing.T) {
	capture, provider, _ := runtimeFixture(t)
	zero := 0
	inner := &runtimeHandleFake{exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	starter := &runtimeStarterFake{handle: inner, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, status: "{\"exit-code\":0}\n"}
	runtime := newRuntime(capture, provider, starter, 100*time.Millisecond)
	prepared, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	h, spawn, err := runtime.Start(context.Background(), prepared, runtimeSink{})
	if err == nil || h != nil || !spawn.Attempted || spawn.Succeeded || spawn.ErrorCode != "provider_spawn_failed" {
		t.Fatalf("handle=%T spawn=%#v err=%v", h, spawn, err)
	}
	if inner.waits != 1 {
		t.Fatalf("failed provider not reaped waits=%d", inner.waits)
	}
}

func TestRuntimePrepareAndDiscardOwnCaptureAndProviderStateTogether(t *testing.T) {
	capture, provider, prepared := runtimeFixture(t)
	runtime := newRuntime(capture, provider, &runtimeStarterFake{}, time.Second)
	got, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	if got.BoundaryID != prepared.BoundaryID {
		t.Fatalf("prepared=%#v", got)
	}
	if err := runtime.Discard(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if provider.discardCalls != 1 || capture.discards != 1 {
		t.Fatalf("provider=%d capture=%d", provider.discardCalls, capture.discards)
	}
	if err := runtime.Discard(context.Background(), got); err == nil {
		t.Fatal("double discard accepted")
	}
}

func runtimeFixture(t *testing.T) (*runtimeCaptureFake, *runtimeProviderFake, hermeticapp.PreparedExecution) {
	t.Helper()
	ctx := validRuntimeCapture(t)
	providerID := core.ProviderIdentity{Provider: core.ProviderBubblewrap, Version: core.BubblewrapVersionV1, BinarySHA256: hex64('a'), RuntimeManifestSHA256: hex64('b')}
	toolchain := core.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: hex64('c')}
	prepared := hermeticapp.PreparedExecution{BoundaryID: "hb_01K00000000000000000000077", Provider: providerID, Toolchain: toolchain, CaptureManifestSHA256: mustManifestDigest(t, ctx.Manifest), Command: hermeticapp.ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--json-status-fd", "3", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}, PrivateStateRoot: "/private/hb_01K00000000000000000000077", ScratchRoot: "/private/hb_01K00000000000000000000077/scratch"}
	return &runtimeCaptureFake{view: ctx}, &runtimeProviderFake{prepared: prepared}, prepared
}
func validRuntimeCapture(t *testing.T) hermeticapp.CapturedView {
	t.Helper()
	manifest := core.CaptureManifest{SchemaVersion: core.CaptureManifestSchemaVersion, WorkspaceID: "ws_01K00000000000000000000000", SourceGeneration: "gen_" + hex64('d'), Selectors: []string{"go.mod"}, Entries: []core.CaptureEntry{{Path: "go.mod", Size: 1, SHA256: hex64('e')}}, TotalBytes: 1}
	manifest, err := manifest.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return hermeticapp.CapturedView{CaptureID: "hcap_01K00000000000000000000077", PrivateRoot: "/private/hcap_01K00000000000000000000077", Manifest: manifest}
}
func mustManifestDigest(t *testing.T, manifest core.CaptureManifest) string {
	t.Helper()
	d, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func daemonPrepareFixture() daemonapp.HermeticPrepareRequest {
	return daemonapp.HermeticPrepareRequest{WorkspaceID: "ws_01K00000000000000000000000", LogicalCWD: ".", Request: core.Request{Version: 1, Mode: core.ModeRequired, RepoInputs: []string{"go.mod"}, Network: core.NetworkOff, Environment: core.EnvironmentFixedAllowlist, Stdin: core.StdinClosed, Writes: core.WritesEphemeralDiscard}, Target: operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Command: "true", CWD: "/repo", StdinMode: operation.StdinModeClosed}}
}

func TestRuntimeRejectsMalformedOrOversizedStatusBeforePreExecProof(t *testing.T) {
	cases := map[string]string{
		"malformed":   "not-json\n",
		"invalid pid": "{\"child-pid\":0}\n",
		"oversized":   "{\"unknown\":\"" + strings.Repeat("x", maxProviderStatusLineBytes) + "\"}\n",
	}
	for name, statusText := range cases {
		t.Run(name, func(t *testing.T) {
			capture, provider, _ := runtimeFixture(t)
			zero := 0
			inner := &runtimeHandleFake{exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
			starter := &runtimeStarterFake{handle: inner, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, status: statusText}
			runtime := newRuntime(capture, provider, starter, 100*time.Millisecond)
			prepared, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
			if err != nil {
				t.Fatal(err)
			}
			h, spawn, err := runtime.Start(context.Background(), prepared, runtimeSink{})
			if err == nil || h != nil || spawn.Succeeded || spawn.ErrorCode != "provider_spawn_failed" {
				t.Fatalf("unsafe status accepted handle=%T spawn=%#v err=%v", h, spawn, err)
			}
		})
	}
}

func TestRuntimeIgnoresUnknownStatusMembersButRequiresConsistentKnownFacts(t *testing.T) {
	capture, provider, _ := runtimeFixture(t)
	zero := 0
	inner := &runtimeHandleFake{exit: receipt.ExitEvidence{Reaped: true, Code: &zero}}
	starter := &runtimeStarterFake{handle: inner, spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, status: "{\"future\":true,\"child-pid\":4242}\n{\"future2\":{},\"exit-code\":0}\n"}
	runtime := newRuntime(capture, provider, starter, time.Second)
	prepared, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	h, spawn, err := runtime.Start(context.Background(), prepared, runtimeSink{})
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	_ = h.Wait(context.Background())
	if !h.(interface{ HermeticBoundaryResult() core.BoundaryResult }).HermeticBoundaryResult().Authoritative() {
		t.Fatal("forward-compatible unknown status members broke complete proof")
	}
}

func TestRuntimeOwnedPreparedStateIsDeepClonedAgainstCallerMutation(t *testing.T) {
	capture, provider, _ := runtimeFixture(t)
	runtime := newRuntime(capture, provider, &runtimeStarterFake{}, time.Second)
	prepared, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	original := clonePreparedExecution(prepared)
	prepared.Command.Argv[0] = "/forged"
	if err := runtime.Discard(context.Background(), prepared); err == nil {
		t.Fatal("mutated prepared ownership accepted")
	}
	if err := runtime.Discard(context.Background(), original); err != nil {
		t.Fatalf("original prepared state lost after caller mutation: %v", err)
	}
}

func TestRuntimeAndRealPrivateRunnerProvePreExecWithoutAmbientEnvironment(t *testing.T) {
	t.Setenv("SHELLBEAM_RUNTIME_SECRET", "must-not-cross")
	capture, provider, prepared := runtimeFixture(t)
	prepared.Command = hermeticapp.ProviderCommand{
		Executable: "/bin/sh",
		Argv: []string{"/bin/sh", "-c", `
if [ -n "${SHELLBEAM_RUNTIME_SECRET+x}" ]; then exit 71; fi
printf '{"child-pid":4242}\n' >&3
printf '{"exit-code":0}\n' >&3
printf runtime-clean
`},
		Dir: t.TempDir(), Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3,
	}
	provider.prepared = prepared
	runtime := newRuntime(capture, provider, processadapter.Owner{}, time.Second)
	got, err := runtime.Prepare(context.Background(), daemonPrepareFixture())
	if err != nil {
		t.Fatal(err)
	}
	sink := &capturingRuntimeSink{}
	h, spawn, err := runtime.Start(context.Background(), got, sink)
	if err != nil || !spawn.Succeeded {
		t.Fatalf("spawn=%#v err=%v", spawn, err)
	}
	exit := h.Wait(context.Background())
	if exit.Code == nil || *exit.Code != 0 || exit.Signal != "" {
		t.Fatalf("literal exit=%#v", exit)
	}
	result := h.(interface{ HermeticBoundaryResult() core.BoundaryResult }).HermeticBoundaryResult()
	if !result.Authoritative() {
		t.Fatalf("boundary=%#v", result)
	}
	if sink.String() != "runtime-clean" {
		t.Fatalf("output=%q", sink.String())
	}
}

type capturingRuntimeSink struct {
	mu sync.Mutex
	b  []byte
}

func (s *capturingRuntimeSink) Append(_ context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b = append(s.b, data...)
	return nil
}
func (s *capturingRuntimeSink) CaptureFailed(error) {}
func (s *capturingRuntimeSink) String() string      { s.mu.Lock(); defer s.mu.Unlock(); return string(s.b) }
