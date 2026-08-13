# Direct dependencies

- `github.com/modelcontextprotocol/go-sdk` v1.7.0: official MCP server/stdio implementation.
- `github.com/creack/pty` v1.1.24: native PTY creation.
- `github.com/oklog/ulid/v2` v2.1.1: time-sortable session/incarnation IDs.
- `github.com/pelletier/go-toml/v2` v2.3.1: strict configuration decoding.
- `golang.org/x/sys` v0.41.0: peer credential APIs; version selected by the MCP SDK module graph.

The Go standard library provides CLI, HTTP-over-UDS, processes, persistence, crypto, JSON, logging and service invocation. No telemetry dependency is included.

