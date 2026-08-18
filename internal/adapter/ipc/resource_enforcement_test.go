//go:build linux || darwin

package ipc

import (
	"testing"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemon "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestResourceLimitsIPCV2DecodeAndBridgeRoundTrip(t *testing.T) {
	raw := []byte(`{"ipc_version":2,"kind":"request","request_id":"resource","action":"start","operation_id":"resource-op","command":"true","cwd":"/tmp","limits":{"memory_bytes":67108864,"processes":8}}`)
	got, err := decodeRequestV2(bytesReaderV2(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.ResourceLimits == nil || got.ResourceLimits.MemoryBytes != 64<<20 || got.ResourceLimits.Processes != 8 {
		t.Fatalf("decoded limits=%#v", got.ResourceLimits)
	}
	limits := &operation.ResourceLimits{MemoryBytes: 96 << 20, Processes: 4}
	wire := requestV2FromBridge(bridge.Request{ProtocolVersion: 2, Action: "start", Start: daemon.StartRequest{OperationID: "resource-op", Command: "true", CWD: "/tmp", ResourceLimits: limits}})
	if wire.ResourceLimits == nil || wire.ResourceLimits.MemoryBytes != 96<<20 || wire.ResourceLimits.Processes != 4 {
		t.Fatalf("wire limits=%#v", wire.ResourceLimits)
	}
	wire.ResourceLimits.MemoryBytes = 1
	if limits.MemoryBytes == 1 {
		t.Fatal("IPC request aliased caller resource limits")
	}
}
