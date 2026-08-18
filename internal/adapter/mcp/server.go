// Package mcp exposes ShellBeam's single stateless MCP tool using the official SDK.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/api/schema"
	bridge "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	mcpgo "github.com/modelcontextprotocol/go-sdk/mcp"
)

const mediaDisclosure = " read_media reads the original selected local image file bytes and sends those bytes to the connected MCP client/model; encoded files may include embedded metadata such as EXIF, GPS, ICC profiles, comments, application metadata, or trailing bytes."

const projectOnboardingInstructions = "For repository onboarding, inspect.project before relying on project-specific capabilities. Do not auto-trust discovered repository commands and do not automatically write .shellbeam/project.toml. When a shared manifest would be useful, audit bounded repository evidence, propose the focused change, obtain normal user approval before writing it, validate it, then review the exact current discovery_fingerprint. While that reviewed fingerprint is current, avoid repeated onboarding prompts. review_due requests re-review and does not block ordinary execution."

const (
	Instructions  = "ShellBeam runs commands as the local OS user with full authority. Use it only for intended local execution. For start, create one operation_id and reuse it for every retry; if the outcome is unknown, never create another. Poll with session_id and cursor. For write, use next_input_offset; acceptance means queued, while the terminal receipt proves delivery. For kill, create one kill_id and reuse it. Never infer command success from MCP success; require a terminal receipt and spawn/exit evidence. " + projectOnboardingInstructions
	ExtensionName = "io.github.maemreyo.shellbeam"
	modernMCP     = "2026-07-28"
)

const catalogRefreshInterval = 5 * time.Second

func ToolDefinition() *mcpgo.Tool { return toolDefinition(schema.MCPInputV1, schema.MCPOutputV1) }

func ToolDefinitionV2() *mcpgo.Tool { return toolDefinition(schema.MCPInputV2, schema.MCPOutputV2) }

func toolDefinitionV2(catalog capability.Catalog) *mcpgo.Tool {
	if !mediaCatalogAvailable(catalog) {
		return ToolDefinitionV2()
	}
	destructive, open := true, true
	return &mcpgo.Tool{
		Name: "local_shell", Title: "ShellBeam — Local Shell",
		Description: "Run and control commands with the full authority of the local OS user. Transport success is not command success; inspect the terminal receipt." + mediaDisclosure,
		InputSchema: composeMediaInputSchema(), OutputSchema: composeMediaOutputSchema(),
		Annotations: &mcpgo.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &open, IdempotentHint: false},
	}
}

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

type dynamicToolState struct {
	mu sync.RWMutex
	v2 *mcpgo.Tool
}

func newDynamicToolState(tool *mcpgo.Tool) *dynamicToolState { return &dynamicToolState{v2: tool} }

func (s *dynamicToolState) current() *mcpgo.Tool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.v2
}

func (s *dynamicToolState) update(next *mcpgo.Tool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reflect.DeepEqual(s.v2, next) {
		return false
	}
	s.v2 = next
	return true
}

func New(handler *bridge.Handler, catalog capability.Catalog) *mcpgo.Server {
	catalog = mediaCatalogForHandler(catalog, handler)
	caps := &mcpgo.ServerCapabilities{}
	caps.AddExtension(ExtensionName, map[string]any{"schema_version": 1, "catalog": catalog})
	server := mcpgo.NewServer(
		&mcpgo.Implementation{Name: "shellbeam", Title: "ShellBeam", Version: "v2"},
		&mcpgo.ServerOptions{Instructions: Instructions, Capabilities: caps},
	)
	v1 := ToolDefinition()
	state := newDynamicToolState(toolDefinitionV2(catalog))
	var toolHandler mcpgo.ToolHandler
	var refresh func(context.Context, bool) error
	var refreshMu sync.Mutex
	lastRefresh := time.Now()
	toolHandler = func(ctx context.Context, req *mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if err := refresh(ctx, callForcesCatalogRefresh(req)); err != nil {
			version := protocolGeneration(req.ProtocolVersion())
			if errors.Is(err, failure.InvalidDaemonResponse) {
				return versionedToolError(version, "", "invalid_daemon_response", "ShellBeam MCP/daemon tool schema mismatch; reconnect or restart MCP using the same ShellBeam build as the daemon", false), nil
			}
			return versionedToolError(version, "", "daemon_unavailable", "daemon capability refresh failed", true), nil
		}
		return call(ctx, handler, req)
	}
	refresh = func(ctx context.Context, force bool) error {
		if handler == nil || !handler.CatalogRefreshEnabled() {
			return nil
		}
		refreshMu.Lock()
		defer refreshMu.Unlock()
		now := time.Now()
		if !force && now.Sub(lastRefresh) < catalogRefreshInterval {
			return nil
		}
		if _, err := handler.RefreshEffectiveCatalog(ctx); err != nil {
			return err
		}
		lastRefresh = now
		next := toolDefinitionV2(handler.EffectiveCatalog())
		if state.update(next) {
			// Replacing the registered tool is the SDK-supported way to emit
			// notifications/tools/list_changed. The versioned list middleware
			// still projects the modern schema only to modern clients.
			server.AddTool(v1, toolHandler)
		}
		return nil
	}
	server.AddTool(v1, toolHandler)
	server.AddReceivingMiddleware(versionedToolList(v1, func(ctx context.Context) (*mcpgo.Tool, error) {
		if err := refresh(ctx, true); err != nil {
			return nil, err
		}
		return state.current(), nil
	}))
	return server
}

func callForcesCatalogRefresh(req *mcpgo.CallToolRequest) bool {
	if req == nil {
		return false
	}
	var hint struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(req.Params.Arguments, &hint) != nil {
		return false
	}
	return hint.Action == "inspect.server" || hint.Action == "read_media"
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

func versionedToolList(v1 *mcpgo.Tool, currentV2 func(context.Context) (*mcpgo.Tool, error)) mcpgo.Middleware {
	return func(next mcpgo.MethodHandler) mcpgo.MethodHandler {
		return func(ctx context.Context, method string, request mcpgo.Request) (mcpgo.Result, error) {
			var v2 *mcpgo.Tool
			if method == "tools/list" {
				var err error
				v2, err = currentV2(ctx)
				if err != nil {
					return nil, err
				}
			}
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
