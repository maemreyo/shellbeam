package contextexec

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

func TestProtocolFramesAreClosedBoundedAndTyped(t *testing.T) {
	hello := HelloFrame{ProtocolVersion: ProtocolVersion, Kind: KindHello, OpaqueLaunchID: "launch_01"}
	var wire bytes.Buffer
	if err := writeFrame(&wire, hello); err != nil {
		t.Fatal(err)
	}
	got, err := readHelloFrame(&wire)
	if err != nil || got != hello {
		t.Fatalf("hello=%#v err=%v", got, err)
	}

	malformed := []string{
		`{"protocol_version":2,"kind":"hello","opaque_launch_id":"launch_01"}` + "\n",
		`{"protocol_version":1,"kind":"future","opaque_launch_id":"launch_01"}` + "\n",
		`{"protocol_version":1,"kind":"hello","opaque_launch_id":"launch_01","extra":true}` + "\n",
		`{"protocol_version":1,"kind":"hello","opaque_launch_id":"bad id"}` + "\n",
	}
	for _, raw := range malformed {
		if _, err := readHelloFrame(strings.NewReader(raw)); err == nil {
			t.Fatalf("accepted malformed %s", raw)
		}
	}

	tooLarge := strings.Repeat("x", MaxFrameBytes+1) + "\n"
	if _, err := readHelloFrame(strings.NewReader(tooLarge)); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestRequestAndOutputFramesPreserveSeparateAuthoritativeStreams(t *testing.T) {
	req := core.Request{ContextExecID: "ctxexec_01", SessionID: "session_01", AuthorityEpoch: 4, Argv: []string{"printf", "ok"}, TimeoutMS: 1000, MaxOutputBytes: 1024}
	fp, err := req.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	helper := core.HelperBinding{OpaqueLaunchID: "launch_01", Generation: "generation_01", RequestFingerprint: fp, ExecutablePath: "/opt/shellbeam/bin/shellbeam"}
	frame := RequestFrame{ProtocolVersion: ProtocolVersion, Kind: KindRequest, Request: req, Context: core.ContextBinding{SessionID: req.SessionID, AuthorityEpoch: req.AuthorityEpoch, ShellIdentity: "fish:runtime_01", BoundaryQuality: "shell_prompt", CWDObserved: "/tmp/project", PrivacyState: "standard"}, Helper: helper}
	var wire bytes.Buffer
	if err := writeFrame(&wire, frame); err != nil {
		t.Fatal(err)
	}
	got, err := readRequestFrame(&wire)
	if err != nil || got.Request.ContextExecID != req.ContextExecID || got.Helper.Generation != helper.Generation {
		t.Fatalf("request=%#v err=%v", got, err)
	}

	for _, stream := range []OutputStream{StreamStdout, StreamStderr} {
		out := OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: stream, Offset: 0, Data: []byte("owned")}
		var buf bytes.Buffer
		if err := writeFrame(&buf, out); err != nil {
			t.Fatal(err)
		}
		decoded, err := readOutputFrame(&buf)
		if err != nil || decoded.Stream != stream || string(decoded.Data) != "owned" {
			t.Fatalf("output=%#v err=%v", decoded, err)
		}
	}
	bad := OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: "pane", Data: []byte("mixed")}
	if err := bad.Validate(); err == nil {
		t.Fatal("mixed pane stream accepted as authoritative")
	}
}

func TestServerReceivesOnlyContiguousBoundChildFramesAndTerminalIdentity(t *testing.T) {
	expectation := validClaimExpectation(t)
	state := validBoundState(t, expectation)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	server := &Server{Expectation: expectation}
	done := make(chan struct {
		result ReceivedResult
		err    error
	}, 1)
	go func() {
		result, err := server.ReceiveResult(context.Background(), left, state)
		done <- struct {
			result ReceivedResult
			err    error
		}{result, err}
	}()
	client := Client{Conn: right, OpaqueLaunchID: expectation.Identity.OpaqueLaunchID}
	if err := client.SendOutput(OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: StreamStdout, Offset: 0, Data: []byte("out")}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendOutput(OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: StreamStderr, Offset: 0, Data: []byte("err")}); err != nil {
		t.Fatal(err)
	}
	result := validTerminalResultForState(t, state, 3, 3, true)
	if err := client.SendTerminal(TerminalFrame{ProtocolVersion: ProtocolVersion, Kind: KindTerminal, Result: result}); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if string(got.result.Stdout) != "out" || string(got.result.Stderr) != "err" || got.result.Terminal.ContextExecID != state.Request.ContextExecID {
		t.Fatalf("result=%#v", got.result)
	}
}

func TestServerRejectsOutputGapAndTerminalCountForgery(t *testing.T) {
	expectation := validClaimExpectation(t)
	state := validBoundState(t, expectation)
	for name, send := range map[string]func(*Client) error{
		"gap": func(c *Client) error {
			return c.SendOutput(OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: StreamStdout, Offset: 1, Data: []byte("x")})
		},
		"count": func(c *Client) error {
			if err := c.SendOutput(OutputFrame{ProtocolVersion: ProtocolVersion, Kind: KindOutput, Stream: StreamStdout, Offset: 0, Data: []byte("x")}); err != nil {
				return err
			}
			r := validTerminalResultForState(t, state, 2, 0, true)
			return c.SendTerminal(TerminalFrame{ProtocolVersion: ProtocolVersion, Kind: KindTerminal, Result: r})
		},
	} {
		t.Run(name, func(t *testing.T) {
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			server := &Server{Expectation: expectation}
			done := make(chan error, 1)
			go func() { _, err := server.ReceiveResult(context.Background(), left, state); done <- err }()
			client := &Client{Conn: right}
			if err := send(client); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err == nil {
				t.Fatal("forged output accepted")
			}
		})
	}
}

func validTerminalResultForState(t *testing.T, state operation.ContextExecState, stdout, stderr int64, complete bool) core.Result {
	t.Helper()
	zero := 0
	quality := core.EvidenceQualityComplete
	if !complete {
		quality = core.EvidenceQualityIncomplete
	}
	return core.Result{SchemaVersion: core.SchemaVersion, ContextExecID: state.Request.ContextExecID, RequestFingerprint: state.RequestFingerprint, Lifecycle: core.LifecycleCanonicalized, Context: state.Context, Helper: state.Helper, Executable: core.ExecutableIdentity{Requested: state.Request.Argv[0], ResolvedPath: "/usr/bin/printf"}, Spawn: receipt.SpawnEvidence{Attempted: true, Succeeded: true}, Exit: receipt.ExitEvidence{Reaped: true, Code: &zero}, Output: core.OutputEvidence{StdoutBytes: stdout, StderrBytes: stderr, OutputComplete: complete, Truncated: !complete, Attribution: core.OutputAttributionHelperOwnedChildPipes}, EvidenceQuality: quality, EvidenceAuthority: core.EvidenceAuthorityContextExecChildOwnedV1}
}
