# ShellBeam MCP Bridge Implementation Plan

> **Requires:** Checkpoints 1–4 green. Use official MCP Go SDK `v1.7.0`; no custom MCP transport.

**Goal:** Expose exactly one stdio MCP tool, `local_shell`, as a stateless adapter to the daemon Unix socket with truthful schemas, annotations, instructions, and structured results.

### Task 1: SDK pin and adapter boundary

**Files:** `go.mod`, `go.sum`, `internal/adapter/mcp/server.go`, `server_test.go`, `internal/app/bridge/handler.go`, `handler_test.go`, `docs/adr/0005-mcp-sdk.md`.

- [ ] Add `github.com/modelcontextprotocol/go-sdk@v1.7.0`; verify module graph and license inventory.
- [ ] Wrap SDK behind `mcp.Run(ctx, io.Reader, io.Writer, *bridge.Handler) error`; `bridge.Handler` depends only on its consumer-owned `DaemonClient` port, implemented by `internal/adapter/ipc.Client`. No SDK or IPC concrete type crosses into core/app.
- [ ] ADR records official SDK, stdio transport, one-tool constraint, and version pin.
- [ ] Commit `feat: establish official mcp adapter`.

### Task 2: Instructions, metadata, and tool registration

**Files:** `internal/adapter/mcp/metadata.go`, `metadata_test.go`, `api/schema/mcp-input-v1.json`, `api/schema/mcp-output-v1.json`.

- [ ] Assert initialization instructions begin with the exact approved self-contained guidance and first 512 characters remain self-contained.
- [ ] Register exactly `local_shell`; zero prompts/resources. Title/description state local-user authority and receipt requirement.
- [ ] Annotations are `readOnly=false`, `destructive=true`, `openWorld=true`, `idempotent=false`.
- [ ] Commit `feat: register truthful local shell tool`.

### Task 3: Closed input decoding and policy validation

**Files:** `internal/adapter/mcp/input.go`, `input_test.go`, `input_fuzz_test.go`.

- [ ] For every action, test required/default/boundary values, cross-action fields, unknown fields, invalid IDs, relative cwd, empty command/input, chars+EOF, signal enum, and configured upper bounds.
- [ ] Decode into the frozen closed union, apply response-only defaults without changing start fingerprint semantics, and map policy errors to stable `ok=false`/`isError=true` results.
- [ ] Commit `feat: validate local shell mcp input`.

### Task 4: Structured output and error mapping

**Files:** `internal/adapter/mcp/output.go`, `errors.go`, `output_test.go`, `tests/contract/testdata/mcp/*.json`.

- [ ] Golden-test start/poll/write/kill success, terminal failure/timeout/killed/ambiguous as `ok=true`, and validation/auth/capacity/persistence/IPC/protocol as `ok=false` with `isError=true`.
- [ ] `structuredContent` exactly matches output schema. Text content is a concise safe summary and never includes command, cwd, stdin, raw output, credentials, or arbitrary OS error.
- [ ] Unknown output fields and schema drift fail contract tests.
- [ ] Commit `feat: map daemon results to mcp output`.

### Task 5: Stateless bridge behavior

**Files:** `internal/app/bridge/handler.go`, `handler_test.go`, `tests/integration/bridge_integration_test.go`.

- [ ] Test daemon unavailable/reconnect, request cancellation, initial yield, long poll, response cap, and bridge restart during live daemon session.
- [ ] Each call uses the IPC client; bridge holds no session/process authority. Cancellation propagates only to IPC wait, never a daemon kill.
- [ ] A lost IPC response followed by exact retry preserves start/write/kill dedupe behavior end to end.
- [ ] Commit `feat: forward mcp actions statelessly`.

### Task 6: Local MCP conformance harness

**Files:** `tests/integration/mcp_conformance_test.go`, `scripts/test-mcp-local.sh`, `docs/testing/mcp-local.md`, `dev/test-impact.toml`.

- [ ] Add JSON-RPC stdio integration tests for initialize, tools/list, tools/call, malformed calls, cancellation, and clean EOF.
- [ ] Script builds a temporary binary, starts a temporary daemon/state root, runs the available official Inspector CLI in non-credentialed local mode, and cleans only its temp directory. If Inspector is absent, exit with documented code 3 and mark `NOT RUN`, not PASS.
- [ ] Run conformance, dirty/checkpoint, and race scopes; commit `test: prove mcp bridge checkpoint`.

## Completion gate

Checkpoint 5 requires exact schema/SDK conformance and local stdio end-to-end proof. It does not claim Secure MCP Tunnel or ChatGPT credentialed proof.
