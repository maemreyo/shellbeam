package mcp

import (
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestResourceLimitsMCPV2ForwardAndV1Reject(t *testing.T) {
	raw := []byte(`{"action":"start","operation_id":"resource-mcp","command":"true","cwd":"/tmp","limits":{"memory_bytes":67108864,"processes":8}}`)
	in, err := decodeInput(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateForVersion(2, in, raw); err != nil {
		t.Fatalf("modern protocol rejected limits: %v", err)
	}
	req := requestFromInput(2, in, raw)
	if req.Start.ResourceLimits == nil || req.Start.ResourceLimits.MemoryBytes != 64<<20 || req.Start.ResourceLimits.Processes != 8 {
		t.Fatalf("forwarded limits=%#v", req.Start.ResourceLimits)
	}
	if err := validateForVersion(1, in, raw); err == nil {
		t.Fatal("legacy protocol accepted resource limits")
	}
}

func TestResourceLimitCloneIsTransportSafe(t *testing.T) {
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	in := input{Action: "start", OperationID: "resource-mcp", Command: "true", CWD: "/tmp", ResourceLimits: limits}
	req := requestFromInput(2, in, []byte(`{"action":"start","operation_id":"resource-mcp","command":"true","cwd":"/tmp","limits":{"memory_bytes":67108864}}`))
	req.Start.ResourceLimits.MemoryBytes = 1
	if limits.MemoryBytes == 1 {
		t.Fatal("MCP request aliased decoded resource limits")
	}
}
