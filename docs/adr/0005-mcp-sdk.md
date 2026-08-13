# ADR 0005: Official MCP SDK and stdio bridge

ShellBeam pins `github.com/modelcontextprotocol/go-sdk` v1.7.0 and uses its stdio transport. Exactly one destructive, open-world, non-idempotent tool is registered. Checked-in draft 2020-12 schemas are supplied directly and ShellBeam performs closed-union validation before forwarding.

The SDK's module graph selects `golang.org/x/sys` v0.41.0, replacing the planning-time v0.35.0 pin. ShellBeam accepts that minimum selected version and uses it directly for peer credentials; it does not force a lower incompatible module graph.

SDK types remain inside `internal/adapter/mcp`. The application bridge knows only a consumer-owned daemon-client port. The bridge stores no process/session authority, so restarting it cannot terminate a command.
