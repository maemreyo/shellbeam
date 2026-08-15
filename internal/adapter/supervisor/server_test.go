package supervisor

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestServerAuthenticatesThenRoutesBoundedControlRequests(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-server", "generation-server")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "cat", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(runtime, capability)
	if err != nil {
		t.Fatal(err)
	}
	client, serverConn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.ServeConn(ctx, serverConn) }()
	reader := bufio.NewReader(client)

	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := Proof(capability, layoutSessionID(layout), "generation-server", challenge)
	if err != nil {
		t.Fatal(err)
	}
	handshake := Request{ProtocolVersion: ProtocolVersion, Kind: KindHandshake, SessionID: layoutSessionID(layout), GenerationID: "generation-server", Handshake: &HandshakeRequest{Challenge: challenge, Proof: proof}}
	if response := roundTripRequest(t, client, reader, handshake); !response.OK || !response.Authenticated || response.Error != nil {
		t.Fatalf("handshake=%#v", response)
	}
	status := roundTripRequest(t, client, reader, Request{ProtocolVersion: ProtocolVersion, Kind: KindStatus, SessionID: layoutSessionID(layout), GenerationID: "generation-server"})
	if !status.OK || status.Status == nil || status.Status.PID != 4242 || status.Status.State != "running" {
		t.Fatalf("status=%#v", status)
	}
	write := roundTripRequest(t, client, reader, Request{ProtocolVersion: ProtocolVersion, Kind: KindWrite, SessionID: layoutSessionID(layout), GenerationID: "generation-server", Write: &WriteRequest{InputOffset: 0, Chars: "x"}})
	if !write.OK || write.Write == nil || write.Write.NextOffset != 1 || owner.handle.WriteCount() != 1 {
		t.Fatalf("write=%#v count=%d", write, owner.handle.WriteCount())
	}
	if err := owner.Emit([]byte("out")); err != nil {
		t.Fatal(err)
	}
	output := roundTripRequest(t, client, reader, Request{ProtocolVersion: ProtocolVersion, Kind: KindOutput, SessionID: layoutSessionID(layout), GenerationID: "generation-server", Output: &OutputRequest{Offset: 0, MaxBytes: 16}})
	if !output.OK || output.Output == nil || string(output.Output.Data) != "out" || output.Output.NextOffset != 3 || output.Output.Extent != 3 {
		t.Fatalf("output=%#v", output)
	}
	signal := Request{ProtocolVersion: ProtocolVersion, Kind: KindSignal, SessionID: layoutSessionID(layout), GenerationID: "generation-server", Signal: &SignalRequest{KillID: "kill-server", Signal: "TERM"}}
	first := roundTripRequest(t, client, reader, signal)
	second := roundTripRequest(t, client, reader, signal)
	if !first.OK || !second.OK || owner.handle.SignalCount("TERM") != 1 {
		t.Fatalf("signals first=%#v second=%#v count=%d", first, second, owner.handle.SignalCount("TERM"))
	}
	owner.handle.FinishSignal("terminated")
	if _, err := runtime.WaitTerminal(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server connection did not exit")
	}
}

func TestServerRejectsWrongProofAndPostAuthIdentityMismatch(t *testing.T) {
	layout, capability := runtimePrivateState(t, "persistent-session-auth", "generation-auth")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(runtime, capability)
	if err != nil {
		t.Fatal(err)
	}

	badClient, badServer := net.Pipe()
	go func() { _ = server.ServeConn(context.Background(), badServer) }()
	badReader := bufio.NewReader(badClient)
	challenge, _ := NewChallenge()
	bad := Request{ProtocolVersion: ProtocolVersion, Kind: KindHandshake, SessionID: layoutSessionID(layout), GenerationID: "generation-auth", Handshake: &HandshakeRequest{Challenge: challenge, Proof: "invalid-proof"}}
	response := roundTripRequest(t, badClient, badReader, bad)
	if response.Error == nil || response.Error.Code != string(failure.SupervisorAuthFailed) || response.OK {
		t.Fatalf("bad auth response=%#v", response)
	}
	_ = badClient.Close()

	client, conn := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.ServeConn(ctx, conn) }()
	reader := bufio.NewReader(client)
	challenge, _ = NewChallenge()
	proof, _ := Proof(capability, layoutSessionID(layout), "generation-auth", challenge)
	good := Request{ProtocolVersion: ProtocolVersion, Kind: KindHandshake, SessionID: layoutSessionID(layout), GenerationID: "generation-auth", Handshake: &HandshakeRequest{Challenge: challenge, Proof: proof}}
	if response := roundTripRequest(t, client, reader, good); !response.OK {
		t.Fatalf("good handshake=%#v", response)
	}
	mismatch := Request{ProtocolVersion: ProtocolVersion, Kind: KindStatus, SessionID: layoutSessionID(layout), GenerationID: "generation-other"}
	response = roundTripRequest(t, client, reader, mismatch)
	if response.Error == nil || response.Error.Code != string(failure.SupervisorStateConflict) || response.OK {
		t.Fatalf("identity mismatch response=%#v", response)
	}
	_ = client.Close()
	owner.handle.FinishCode(0)
	_, _ = runtime.WaitTerminal(context.Background())
}

func roundTripRequest(t *testing.T, conn net.Conn, reader *bufio.Reader, request Request) Response {
	t.Helper()
	if err := EncodeRequest(conn, request); err != nil {
		t.Fatal(err)
	}
	response, err := DecodeResponse(reader)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func layoutSessionID(layout Layout) string {
	return filepathBaseForTest(layout.SessionDir)
}

func filepathBaseForTest(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
