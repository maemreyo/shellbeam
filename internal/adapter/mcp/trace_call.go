package mcp

import (
	"fmt"

	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	inputtraceapp "github.com/maemreyo/shellbeam/internal/app/inputtrace"
	trace "github.com/maemreyo/shellbeam/internal/core/inputtrace"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func startInputTraceStatus(mode trace.Mode, result inputtraceapp.InspectResult) map[string]any {
	return map[string]any{"requested_mode": mode, "status": result.Status, "trace_id": result.TraceID}
}

func inputTraceSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.InputTrace == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "input trace inspection missing", false)
	}
	body["input_trace"] = out.InputTrace
	return fmt.Sprintf("inspect.trace: %s", out.InputTrace.Status), nil
}
