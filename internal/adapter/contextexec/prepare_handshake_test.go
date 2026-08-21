package contextexec

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestPrepareHandshakeBumpsProtocolAndFramesAreClosed(t *testing.T) {
	if ProtocolVersion != 3 {
		t.Fatalf("protocol version=%d want=3 for prepare/execute handshake", ProtocolVersion)
	}
	prepared := PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: "/usr/bin/printf"}
	if err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}
	execute := ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: operation.ID("context_child_op_01"), ChildSessionID: operation.SessionID("context_child_session_01")}
	if err := execute.Validate(); err != nil {
		t.Fatal(err)
	}
	spawn := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}
	if err := spawn.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: "/usr/bin/printf", FailureCode: "context_exec_unavailable"}).Validate(); err == nil {
		t.Fatal("prepared frame accepted both executable and failure")
	}
	if err := (ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true}).Validate(); err == nil {
		t.Fatal("execute authorization accepted without child identity")
	}
	if err := (SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: false}}).Validate(); err == nil {
		t.Fatal("spawn frame accepted without attempted spawn")
	}
	if err := (SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: false}}).Validate(); err == nil {
		t.Fatal("failed spawn frame accepted without stable error code")
	}
	if err := (SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: execute.ChildOperationID, ChildSessionID: execute.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true, ErrorCode: "context_exec_unavailable"}}).Validate(); err == nil {
		t.Fatal("successful spawn frame accepted with failure code")
	}
}

type orderingPreparedLauncher struct {
	log      *[]string
	mu       *sync.Mutex
	prepared *orderingPreparedExecution
}

func (l *orderingPreparedLauncher) Qualified() bool { return true }
func (l *orderingPreparedLauncher) Prepare(ChildSpec) (PreparedExecution, error) {
	l.mu.Lock()
	*l.log = append(*l.log, "prepare")
	l.mu.Unlock()
	return l.prepared, nil
}

type orderingPreparedExecution struct {
	log        *[]string
	mu         *sync.Mutex
	authorized *bool
	startCalls int
	closeCalls int
}

func (*orderingPreparedExecution) ResolvedExecutable() string { return "/usr/bin/printf" }
func (p *orderingPreparedExecution) Start() (*ChildProcess, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !*p.authorized {
		return nil, errors.New("start before durable execute authorization")
	}
	p.startCalls++
	*p.log = append(*p.log, "start")
	zero := 0
	return &ChildProcess{
		ResolvedExecutable: "/usr/bin/printf",
		Stdout:             io.NopCloser(strings.NewReader("")),
		Stderr:             io.NopCloser(strings.NewReader("")),
		Wait:               func() (ChildExit, error) { return ChildExit{Reaped: true, Code: &zero}, nil },
		KillGroup:          func() error { return nil },
	}, nil
}
func (p *orderingPreparedExecution) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
	*p.log = append(*p.log, "close")
	return nil
}

type orderingExecutionProtocol struct {
	log        *[]string
	mu         *sync.Mutex
	authorized *bool
	ackErr     error
}

func (p *orderingExecutionProtocol) AuthorizePrepared(_ context.Context, frame PreparedFrame) (ExecuteFrame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.log = append(*p.log, "authorize")
	if p.ackErr != nil {
		return ExecuteFrame{}, p.ackErr
	}
	if frame.ResolvedExecutable != "/usr/bin/printf" || frame.FailureCode != "" {
		return ExecuteFrame{}, errors.New("unexpected prepared identity")
	}
	*p.authorized = true
	return ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}, nil
}
func (p *orderingExecutionProtocol) SendSpawn(frame SpawnFrame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.log = append(*p.log, "spawn")
	if !*p.authorized || !frame.Spawn.Attempted || !frame.Spawn.Succeeded {
		return errors.New("invalid spawn ordering")
	}
	return nil
}
func (*orderingExecutionProtocol) SendOutput(OutputFrame) error { return nil }

func TestRuntimePrepareWaitsForExecuteAckBeforeStart(t *testing.T) {
	cwd := t.TempDir()
	req := runtimeRequestFrame(t, cwd, []string{"printf", "ok"}, 1024, 1000)
	var mu sync.Mutex
	log := []string{}
	authorized := false
	prepared := &orderingPreparedExecution{log: &log, mu: &mu, authorized: &authorized}
	launcher := &orderingPreparedLauncher{log: &log, mu: &mu, prepared: prepared}
	peer := &orderingExecutionProtocol{log: &log, mu: &mu, authorized: &authorized}
	runtime := Runtime{Launcher: launcher, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}

	terminal, err := runtime.Execute(context.Background(), req, peer)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Result.ContextExecID != req.Request.ContextExecID {
		t.Fatalf("terminal=%#v", terminal)
	}
	mu.Lock()
	got := append([]string(nil), log...)
	mu.Unlock()
	if strings.Join(got, ",") != "prepare,authorize,start,close,spawn" {
		t.Fatalf("order=%v", got)
	}
	if prepared.startCalls != 1 || prepared.closeCalls != 1 {
		t.Fatalf("start=%d close=%d", prepared.startCalls, prepared.closeCalls)
	}
}

func TestRuntimeDroppedExecuteAckClosesPreparedObjectWithoutStart(t *testing.T) {
	cwd := t.TempDir()
	req := runtimeRequestFrame(t, cwd, []string{"printf", "ok"}, 1024, 1000)
	var mu sync.Mutex
	log := []string{}
	authorized := false
	prepared := &orderingPreparedExecution{log: &log, mu: &mu, authorized: &authorized}
	launcher := &orderingPreparedLauncher{log: &log, mu: &mu, prepared: prepared}
	peer := &orderingExecutionProtocol{log: &log, mu: &mu, authorized: &authorized, ackErr: io.EOF}
	runtime := Runtime{Launcher: launcher, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}

	if _, err := runtime.Execute(context.Background(), req, peer); err == nil {
		t.Fatal("dropped execute ack accepted")
	}
	if prepared.startCalls != 0 || prepared.closeCalls != 1 {
		t.Fatalf("start=%d close=%d", prepared.startCalls, prepared.closeCalls)
	}
}

func TestSpawnFailureFrameCarriesNoEvidenceAuthority(t *testing.T) {
	frame := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01", ResolvedExecutable: "/usr/bin/printf", Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "context_exec_unavailable"}}
	if err := frame.Validate(); err != nil {
		t.Fatal(err)
	}
	_ = core.EvidenceAuthorityContextExecChildOwnedV1 // authority promotion is intentionally not part of SpawnFrame.
}

type serverPreparedRecorder struct {
	log      *[]string
	mu       *sync.Mutex
	execute  ExecuteFrame
	prepared PreparedFrame
	spawned  SpawnFrame
}

func (r *serverPreparedRecorder) AuthorizePrepared(_ context.Context, state operation.ContextExecState, frame PreparedFrame) (operation.ContextExecState, ExecuteFrame, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.log = append(*r.log, "prepared_callback")
	r.prepared = frame
	if state.Lifecycle != core.LifecycleHelperAuthenticated {
		return operation.ContextExecState{}, ExecuteFrame{}, errors.New("prepared callback received wrong lifecycle")
	}
	next := state.Clone()
	next.Lifecycle = core.LifecycleChildReserved
	next.ChildOperationID = r.execute.ChildOperationID
	next.ChildSessionID = r.execute.ChildSessionID
	next.ExecutionAuthorized = true
	return next, r.execute, nil
}

func (r *serverPreparedRecorder) RecordSpawn(_ context.Context, state operation.ContextExecState, frame SpawnFrame) (operation.ContextExecState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.log = append(*r.log, "spawn_callback")
	r.spawned = frame
	if state.Lifecycle != core.LifecycleChildReserved || !state.ExecutionAuthorized {
		return operation.ContextExecState{}, errors.New("spawn callback before durable authorization")
	}
	next := state.Clone()
	next.Lifecycle = core.LifecycleChildSpawned
	return next, nil
}

func TestServerExecutionExchangeAuthorizesBeforeAckAndRecordsSpawnBeforeTerminal(t *testing.T) {
	expectation := validClaimExpectation(t)
	state := validBoundState(t, expectation)
	var mu sync.Mutex
	log := []string{}
	execute := ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}
	recorder := &serverPreparedRecorder{log: &log, mu: &mu, execute: execute}
	server := &Server{Expectation: expectation, AuthorizePrepared: recorder.AuthorizePrepared, RecordSpawn: recorder.RecordSpawn}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()

	done := make(chan error, 1)
	go func() {
		_, _, err := server.ReceiveExecution(context.Background(), left, state)
		done <- err
	}()
	prepared := PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: "/usr/bin/printf"}
	if err := writeFrame(right, prepared); err != nil {
		t.Fatal(err)
	}
	ack, err := readExecuteFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	beforeSpawn := append([]string(nil), log...)
	mu.Unlock()
	if strings.Join(beforeSpawn, ",") != "prepared_callback" {
		t.Fatalf("execute ACK escaped before durable callback: %v", beforeSpawn)
	}
	spawn := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: ack.ChildOperationID, ChildSessionID: ack.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}
	if err := writeFrame(right, spawn); err != nil {
		t.Fatal(err)
	}
	terminal := validTerminalResultForState(t, state, 0, 0, true)
	if err := writeFrame(right, TerminalFrame{ProtocolVersion: ProtocolVersion, Kind: KindTerminal, Result: terminal}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), log...)
	mu.Unlock()
	if strings.Join(got, ",") != "prepared_callback,spawn_callback" {
		t.Fatalf("server callback order=%v", got)
	}
	if recorder.spawned.ResolvedExecutable != recorder.prepared.ResolvedExecutable {
		t.Fatalf("spawn executable=%q prepared=%q", recorder.spawned.ResolvedExecutable, recorder.prepared.ResolvedExecutable)
	}
}

func TestServerExecutionExchangeRejectsSpawnExecutableOrChildIdentityDrift(t *testing.T) {
	for _, mutate := range []func(*SpawnFrame){
		func(v *SpawnFrame) { v.ResolvedExecutable = "/usr/bin/other" },
		func(v *SpawnFrame) { v.ChildOperationID = "context_child_op_other" },
		func(v *SpawnFrame) { v.ChildSessionID = "context_child_session_other" },
	} {
		expectation := validClaimExpectation(t)
		state := validBoundState(t, expectation)
		execute := ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}
		var mu sync.Mutex
		log := []string{}
		recorder := &serverPreparedRecorder{log: &log, mu: &mu, execute: execute}
		server := &Server{Expectation: expectation, AuthorizePrepared: recorder.AuthorizePrepared, RecordSpawn: recorder.RecordSpawn}
		left, right := net.Pipe()
		done := make(chan error, 1)
		go func() { _, _, err := server.ReceiveExecution(context.Background(), left, state); done <- err }()
		prepared := PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: "/usr/bin/printf"}
		if err := writeFrame(right, prepared); err != nil {
			t.Fatal(err)
		}
		ack, err := readExecuteFrame(right)
		if err != nil {
			t.Fatal(err)
		}
		spawn := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: ack.ChildOperationID, ChildSessionID: ack.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}}
		mutate(&spawn)
		if err := writeFrame(right, spawn); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err == nil {
			t.Fatal("server accepted spawn identity drift")
		}
		left.Close()
		right.Close()
	}
}

func TestServerExecutionExchangePrepareFailureEndsWithoutChildOrTerminal(t *testing.T) {
	expectation := validClaimExpectation(t)
	state := validBoundState(t, expectation)
	var preparedCalls, spawnCalls int
	server := &Server{
		Expectation: expectation,
		AuthorizePrepared: func(_ context.Context, got operation.ContextExecState, frame PreparedFrame) (operation.ContextExecState, ExecuteFrame, error) {
			preparedCalls++
			if frame.FailureCode == "" || frame.ResolvedExecutable != "" {
				return operation.ContextExecState{}, ExecuteFrame{}, errors.New("prepare failure not preserved")
			}
			return got, ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: false}, nil
		},
		RecordSpawn: func(context.Context, operation.ContextExecState, SpawnFrame) (operation.ContextExecState, error) {
			spawnCalls++
			return operation.ContextExecState{}, errors.New("spawn recorder reached after prepare failure")
		},
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, _, err := server.ReceiveExecution(context.Background(), left, state); done <- err }()
	if err := writeFrame(right, PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, FailureCode: "context_exec_unavailable"}); err != nil {
		t.Fatal(err)
	}
	ack, err := readExecuteFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Authorized {
		t.Fatal("prepare failure was authorized")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if preparedCalls != 1 || spawnCalls != 0 {
		t.Fatalf("prepared=%d spawn=%d", preparedCalls, spawnCalls)
	}
}

func TestServerExecutionExchangeExplicitSpawnFailureEndsWithoutTerminal(t *testing.T) {
	expectation := validClaimExpectation(t)
	state := validBoundState(t, expectation)
	var mu sync.Mutex
	log := []string{}
	execute := ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}
	recorder := &serverPreparedRecorder{log: &log, mu: &mu, execute: execute}
	server := &Server{Expectation: expectation, AuthorizePrepared: recorder.AuthorizePrepared, RecordSpawn: func(ctx context.Context, got operation.ContextExecState, frame SpawnFrame) (operation.ContextExecState, error) {
		if frame.Spawn.Succeeded || frame.Spawn.ErrorCode == "" {
			return operation.ContextExecState{}, errors.New("spawn failure truth missing")
		}
		return recorder.RecordSpawn(ctx, got, frame)
	}}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	done := make(chan error, 1)
	go func() { _, _, err := server.ReceiveExecution(context.Background(), left, state); done <- err }()
	prepared := PreparedFrame{ProtocolVersion: ProtocolVersion, Kind: KindPrepared, ResolvedExecutable: "/usr/bin/printf"}
	if err := writeFrame(right, prepared); err != nil {
		t.Fatal(err)
	}
	ack, err := readExecuteFrame(right)
	if err != nil {
		t.Fatal(err)
	}
	spawn := SpawnFrame{ProtocolVersion: ProtocolVersion, Kind: KindSpawn, ChildOperationID: ack.ChildOperationID, ChildSessionID: ack.ChildSessionID, ResolvedExecutable: prepared.ResolvedExecutable, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: false, ErrorCode: "context_exec_unavailable"}}
	if err := writeFrame(right, spawn); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if recorder.spawned.Spawn.Succeeded || recorder.spawned.Spawn.ErrorCode == "" {
		t.Fatalf("spawn=%#v", recorder.spawned)
	}
}

type startFailurePrepared struct{}

func (*startFailurePrepared) ResolvedExecutable() string { return "/usr/bin/printf" }
func (*startFailurePrepared) Close() error               { return nil }
func (*startFailurePrepared) Start() (*ChildProcess, error) {
	return nil, errors.New("synthetic start failure")
}

type startFailureLauncher struct{}

func (*startFailureLauncher) Qualified() bool { return true }
func (*startFailureLauncher) Prepare(ChildSpec) (PreparedExecution, error) {
	return &startFailurePrepared{}, nil
}

type startFailureProtocol struct{ spawn SpawnFrame }

func (*startFailureProtocol) AuthorizePrepared(_ context.Context, frame PreparedFrame) (ExecuteFrame, error) {
	if err := frame.Validate(); err != nil {
		return ExecuteFrame{}, err
	}
	return ExecuteFrame{ProtocolVersion: ProtocolVersion, Kind: KindExecute, Authorized: true, ChildOperationID: "context_child_op_01", ChildSessionID: "context_child_session_01"}, nil
}
func (p *startFailureProtocol) SendSpawn(frame SpawnFrame) error {
	p.spawn = frame
	return nil
}
func (*startFailureProtocol) SendOutput(OutputFrame) error { return nil }

func TestRuntimeStartFailureReportsExactPreparedExecutableAndStableErrorCode(t *testing.T) {
	cwd := t.TempDir()
	req := runtimeRequestFrame(t, cwd, []string{"printf", "ok"}, 1024, 1000)
	peer := &startFailureProtocol{}
	runtime := Runtime{Launcher: &startFailureLauncher{}, Environ: func() []string { return []string{"PATH=/usr/bin:/bin"} }, Getwd: func() (string, error) { return cwd, nil }}
	if _, err := runtime.Execute(context.Background(), req, peer); err == nil {
		t.Fatal("start failure accepted")
	}
	if peer.spawn.ResolvedExecutable != "/usr/bin/printf" {
		t.Fatalf("resolved executable=%q", peer.spawn.ResolvedExecutable)
	}
	if !peer.spawn.Spawn.Attempted || peer.spawn.Spawn.Succeeded || peer.spawn.Spawn.ErrorCode == "" {
		t.Fatalf("spawn=%#v", peer.spawn.Spawn)
	}
	if err := peer.spawn.Validate(); err != nil {
		t.Fatalf("failed spawn frame invalid: %v", err)
	}
}
