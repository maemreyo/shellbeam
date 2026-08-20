package mcp

import (
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func decisionProtocolSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if out.Decision == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "decision protocol result missing", false)
	}
	body["decision"] = out.Decision
	return action, nil
}
