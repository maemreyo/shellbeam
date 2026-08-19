package mcp

import (
	"encoding/json"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

func runtimeBuildMismatch(local capability.RuntimeIdentity, daemon *capability.RuntimeIdentity) (string, bool) {
	if daemon == nil || local.Validate() != nil || daemon.Validate() != nil {
		return "", false
	}
	if local.BinarySHA256 != "" && daemon.BinarySHA256 != "" {
		if local.BinarySHA256 != daemon.BinarySHA256 {
			return "binary_identity_mismatch", true
		}
		return "", false
	}
	if local.Revision != "" && daemon.Revision != "" && local.Revision != daemon.Revision {
		return "revision_mismatch", true
	}
	return "", false
}

func runtimeVersionMismatchToolError(action string, local capability.RuntimeIdentity, daemon *capability.RuntimeIdentity, reason string) *mcpgo.CallToolResult {
	details := map[string]string{"reason": reason, "recovery": "restart_daemon"}
	if local.Revision != "" {
		details["mcp_revision"] = local.Revision
	}
	if daemon != nil && daemon.Revision != "" {
		details["daemon_revision"] = daemon.Revision
	}
	public := failure.Public(failure.New(failure.RuntimeVersionMismatch, details, nil))
	return versionedToolErrorDetails(2, action, string(public.Code), public.Message, public.Retryable, public.Details)
}

func toolActionHint(req *mcpgo.CallToolRequest) string {
	if req == nil {
		return ""
	}
	var hint struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(req.Params.Arguments, &hint)
	return hint.Action
}
