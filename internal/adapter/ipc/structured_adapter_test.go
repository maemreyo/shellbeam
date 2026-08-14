//go:build linux || darwin

package ipc

import (
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
	"testing"
)

func TestStructuredAdapterBridgeV2RequestPreservesMetadata(t *testing.T) {
	req := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: daemon.StartRequest{OperationID: "op", Argv: []string{"go", "test", "-json", "./..."}, CWD: "/tmp", StructuredAdapter: "go-test-json"}})
	if req.StructuredAdapter != "go-test-json" {
		t.Fatalf("request=%#v", req)
	}
	if err := validateRequestV2(req); err != nil {
		t.Fatal(err)
	}
}
