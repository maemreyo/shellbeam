package mcp

import (
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func observationInspectionSuccessV2(action string, out bridge.Response, body map[string]any) (string, *mcpgo.CallToolResult) {
	if action == "inspect.environment" {
		if out.Environment == nil {
			return "", toolErrorV2(action, "invalid_daemon_response", "environment inspection missing", false)
		}
		body["environment"] = out.Environment
		return "inspect.environment: " + string(out.Environment.Quality), nil
	}
	if out.Process == nil {
		return "", toolErrorV2(action, "invalid_daemon_response", "process inspection missing", false)
	}
	body["process"] = out.Process
	return "inspect.process: " + string(out.Process.Quality), nil
}
