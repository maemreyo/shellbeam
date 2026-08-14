// Package mcp exposes ShellBeam's single stateless MCP tool using the official SDK.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/maemreyo/shellbeam/api/schema"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

const projectOnboardingInstructions = "For repository onboarding, inspect.project before relying on project-specific capabilities. Do not auto-trust discovered repository commands and do not automatically write .shellbeam/project.toml. When a shared manifest would be useful, audit bounded repository evidence, propose the focused change, obtain normal user approval before writing it, validate it, then review the exact current discovery_fingerprint. While that reviewed fingerprint is current, avoid repeated onboarding prompts. review_due requests re-review and does not block ordinary execution."

const (
	Instructions  = "ShellBeam runs commands as the local OS user with full authority. Use it only for intended local execution. For start, create one operation_id and reuse it for every retry; if the outcome is unknown, never create another. Poll with session_id and cursor. For write, use next_input_offset; acceptance means queued, while the terminal receipt proves delivery. For kill, create one kill_id and reuse it. Never infer command success from MCP success; require a terminal receipt and spawn/exit evidence. " + projectOnboardingInstructions
	ExtensionName = "io.github.maemreyo.shellbeam"
	modernMCP     = "2026-07-28"
)

func ToolDefinition() *mcpgo.Tool { return toolDefinition(schema.MCPInputV1, schema.MCPOutputV1) }

func ToolDefinitionV2() *mcpgo.Tool { return toolDefinition(schema.MCPInputV2, schema.MCPOutputV2) }

func toolDefinition(inputName, outputName schema.Name) *mcpgo.Tool {
	destructive, open := true, true
	input, _ := schema.Load(inputName)
	output, _ := schema.Load(outputName)
	return &mcpgo.Tool{
		Name: "local_shell", Title: "ShellBeam — Local Shell",
		Description: "Run and control commands with the full authority of the local OS user. Transport success is not command success; inspect the terminal receipt.",
		InputSchema: json.RawMessage(input), OutputSchema: json.RawMessage(output),
		Annotations: &mcpgo.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &open, IdempotentHint: false},
	}
}

func New(handler *bridge.Handler, catalog capability.Catalog) *mcpgo.Server {
	caps := &mcpgo.ServerCapabilities{}
	caps.AddExtension(ExtensionName, map[string]any{"schema_version": 1, "catalog": catalog})
	server := mcpgo.NewServer(
		&mcpgo.Implementation{Name: "shellbeam", Title: "ShellBeam", Version: "v2"},
		&mcpgo.ServerOptions{Instructions: Instructions, Capabilities: caps},
	)
	v1, v2 := ToolDefinition(), ToolDefinitionV2()
	server.AddTool(v1, func(ctx context.Context, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return call(ctx, handler, req)
	})
	server.AddReceivingMiddleware(versionedToolList(v1, v2))
	return server
}

func Run(ctx context.Context, handler *bridge.Handler) error {
	catalog, err := inspectCatalog(ctx, handler)
	if err != nil {
		return err
	}
	return New(handler, catalog).Run(ctx, &mcpgo.StdioTransport{})
}

func inspectCatalog(ctx context.Context, handler *bridge.Handler) (capability.Catalog, error) {
	out, err := handler.Handle(ctx, bridge.Request{ProtocolVersion: 2, Action: "inspect.server"})
	if err != nil {
		return capability.Catalog{}, fmt.Errorf("inspect server capabilities: %w", err)
	}
	if out.Code != "" {
		return capability.Catalog{}, fmt.Errorf("inspect server capabilities: %s", out.Code)
	}
	if out.Server == nil {
		return capability.Catalog{}, fmt.Errorf("inspect server capabilities: missing catalog")
	}
	return *out.Server, nil
}

func versionedToolList(v1, v2 *mcpgo.Tool) mcpgo.Middleware {
	return func(next mcpgo.MethodHandler) mcpgo.MethodHandler {
		return func(ctx context.Context, method string, request mcpgo.Request) (mcpgo.Result, error) {
			result, err := next(ctx, method, request)
			if err != nil || method != "tools/list" {
				return result, err
			}
			list, ok := result.(*mcpgo.ListToolsResult)
			req, requestOK := request.(*mcpgo.ListToolsRequest)
			if !ok || !requestOK || len(list.Tools) != 1 || list.Tools[0].Name != "local_shell" {
				return result, nil
			}
			copyResult := *list
			copyResult.Tools = []*mcpgo.Tool{v1}
			if req.ProtocolVersion() >= modernMCP {
				copyResult.Tools[0] = v2
			}
			return &copyResult, nil
		}
	}
}
