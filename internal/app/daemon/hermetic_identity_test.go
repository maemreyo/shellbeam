package daemon

import (
	"context"
	"testing"

	hermeticcore "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

func daemonHermeticRequestForTest(inputs ...string) *hermeticcore.Request {
	return &hermeticcore.Request{
		Version: hermeticcore.RequestVersionV1, Mode: hermeticcore.ModeRequired, RepoInputs: inputs,
		Network: hermeticcore.NetworkOff, Environment: hermeticcore.EnvironmentFixedAllowlist,
		Stdin: hermeticcore.StdinClosed, Writes: hermeticcore.WritesEphemeralDiscard,
	}
}

func TestHermeticRawStartBindsAndClonesDaemonOperationIdentity(t *testing.T) {
	svc := &Service{}
	req := StartRequest{ProtocolVersion: 2, OperationID: "hermetic-raw", Command: "true", CWD: "/tmp", Hermetic: daemonHermeticRequestForTest("go.mod", "internal/**")}
	intent, err := svc.resolveStartIntent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Hermetic == nil || len(intent.Hermetic.RepoInputs) != 2 {
		t.Fatalf("resolved hermetic=%#v", intent.Hermetic)
	}
	withBoundary, err := intent.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	plainReq := req
	plainReq.Hermetic = nil
	plain, err := svc.resolveStartIntent(context.Background(), plainReq)
	if err != nil {
		t.Fatal(err)
	}
	withoutBoundary, err := plain.RequestFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if withBoundary == withoutBoundary {
		t.Fatal("daemon raw identity folded hermetic request into ordinary execution")
	}
	req.Hermetic.RepoInputs[0] = "changed"
	if intent.Hermetic.RepoInputs[0] == "changed" {
		t.Fatal("daemon raw identity aliased caller hermetic request")
	}
}

func TestHermeticTypedStartBindsAndClonesDaemonOperationIdentity(t *testing.T) {
	req := StartRequest{
		ProtocolVersion: 2, OperationID: "hermetic-typed", WorkspaceID: "ws_01K00000000000000000000000",
		ProjectCommandID: "test", Hermetic: daemonHermeticRequestForTest("go.mod", "internal/**"),
	}
	intent, withBoundary, err := typedRequestIntent(req)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Hermetic == nil || len(intent.Hermetic.RepoInputs) != 2 {
		t.Fatalf("typed hermetic=%#v", intent.Hermetic)
	}
	plainReq := req
	plainReq.Hermetic = nil
	_, withoutBoundary, err := typedRequestIntent(plainReq)
	if err != nil {
		t.Fatal(err)
	}
	if withBoundary == withoutBoundary {
		t.Fatal("daemon typed identity folded hermetic request into ordinary execution")
	}
	req.Hermetic.RepoInputs[0] = "changed"
	if intent.Hermetic.RepoInputs[0] == "changed" {
		t.Fatal("daemon typed identity aliased caller hermetic request")
	}
}
