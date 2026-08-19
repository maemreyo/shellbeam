# Control-Plane P0 Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent validation-schema amplification, detect proven MCP/daemon build skew, and preserve recoverable typed failures end-to-end.

**Architecture:** First repair the typed failure carrier so safe details survive IPC → bridge → MCP. Then replace the modern MCP tool's externally enforced strict schema with a transport-admission schema while keeping ShellBeam's canonical strict schema and internal validators authoritative. Finally add startup-captured runtime identity and a fail-closed mismatch gate before daemon-dependent MCP calls.

**Tech Stack:** Go 1.26, official MCP Go SDK v1.7.0, existing strict JSON boundary, IPC v2, capability catalog, Go `runtime/debug`, SHA-256.

**Spec:** `docs/superpowers/specs/2026-08-19-control-plane-p0-hardening-design.md`

## Global Constraints

- P0-B/P0-C/P0-D only.
- No store schema bump.
- No protocol major-version bump.
- No automatic daemon/tunnel restart.
- Canonical strict MCP/IPC schemas remain strict contract sources.
- `internal` is only for unknown/invariant failures.
- No full schema, raw validator dump, private path, command payload, or secret in public failures.
- Runtime mismatch must be proven; incomplete identity remains compatible.
- Every production behavior follows RED → GREEN → refactor.

---

### Task 1: Preserve typed recoverable failure details end-to-end (P0-D)

**Files:**
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/mcp/call.go`
- Test: `internal/adapter/ipc/bridge_checkpoint_trace_regression_test.go` or focused new v2 client test
- Test: `internal/app/bridge/handler_test.go`
- Test: `internal/adapter/mcp/server_test.go` / focused failure test

**Interfaces:**
- Produces: `bridge.Response.Details map[string]string`.
- Consumes: existing `failure.Failure.Details` and `failure.New/Public` whitelist projection.
- Produces: MCP v2 `error.details` for safe known failures.

- [x] Add a failing IPC-client test where a v2 error envelope contains `details={"workspace_id":"...","reason":"root_missing"}` and assert `bridge.Response.Details` preserves them.
- [x] Run the focused test and observe RED because `bridge.Response` has no details carrier.
- [x] Add `Details` to `bridge.Response` and copy v2 IPC error details; run the IPC test GREEN.
- [x] Add a failing bridge test proving known `workspace_root_missing` preserves canonical code/message/retryable plus safe details, while an unknown/private code becomes detail-free `internal`.
- [x] Change bridge normalization to reconstruct through `failure.New(failure.Code(response.Code), response.Details, nil)` and `failure.Public`; run bridge tests GREEN.
- [x] Add a failing MCP v2 test proving a typed daemon failure exposes safe `error.details` and no raw message.
- [x] Extend `versionedToolError/toolErrorV2` with optional safe details and thread bridge details through `call`; keep v1 shape unchanged; run MCP tests GREEN.
- [x] Add a boundary-invariant table for representative registered operational codes proving none becomes `internal` merely on projection; unknown code remains `internal`.
- [x] Run `go test ./internal/core/failure ./internal/app/bridge ./internal/adapter/ipc ./internal/adapter/mcp -count=1`.
- [x] Commit `fix: preserve typed control-plane failures`.

### Task 2: Replace schema-amplifying MCP admission with compact ShellBeam validation (P0-B)

**Files:**
- Create: `api/schema/mcp-tool-input-v2.json`
- Modify: `api/schema/embed.go`
- Modify: `internal/adapter/mcp/server.go`
- Create/Modify: `internal/adapter/mcp/validation_error.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/input.go` only for preflight ordering/helper reuse
- Test: `api/schema/embed_test.go`
- Test: `internal/adapter/mcp/discovery_test.go`
- Test: focused `internal/adapter/mcp/validation_error_test.go`

**Interfaces:**
- Produces: embedded schema name `MCPToolInputV2` with `$id=https://shellbeam.dev/schema/mcp-tool-input-v2.json`.
- Canonical `MCPInputV2` remains unchanged and strict.
- Produces: compact `failure.InvalidInput` projection with `field`/`reason` details.

- [ ] Add a failing schema test: transport schema must accept object `{action:"start", command:7, wat:true}` while canonical `MCPInputV2` rejects it.
- [ ] Create a minimal object-rooted transport schema with no semantic type/enum/required restrictions and `additionalProperties:true`; embed it; run schema test GREEN.
- [ ] Change only modern tool discovery to advertise `MCPToolInputV2`; update discovery ID assertions; keep output schema unchanged.
- [ ] Add a failing official MCP-session test sending an unknown top-level field. Assert `code=invalid_input`, `details.field=wat`, `details.reason=unknown_field`, bounded text, and absence of `$defs`, `oneOf`, `Schema:`, or raw schema body.
- [ ] Implement a bounded validation classifier that preflights action/field names before strict decode and never publishes raw decoder text; run test GREEN.
- [ ] Add failing tests for invalid JSON / wrong typed value / missing required semantic value and assert bounded reasons (`invalid_json`, `invalid_value`, `missing_field` or a narrower safe reason) without echoing user payload.
- [ ] Implement minimal classification for those cases; use safe fallback `invalid_value` when exact field cannot be established without parsing raw implementation error text.
- [ ] Re-run canonical schema tests proving strict contracts were not weakened.
- [ ] Run `go test ./api/schema ./internal/core/jsonstrict ./internal/adapter/mcp -count=1`.
- [ ] Commit `fix: bound mcp validation failures`.

### Task 3: Publish runtime identity and fail closed on proven version skew (P0-C)

**Files:**
- Modify: `internal/buildinfo/info.go`
- Test: `internal/buildinfo/info_test.go`
- Modify: `internal/core/capability/catalog.go`
- Test: `internal/core/capability/catalog_test.go`
- Modify: `internal/core/failure/failure.go`
- Test: `internal/core/failure/failure_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/service.go`
- Modify: daemon composition under `cmd/shellbeam/command_daemon.go` / `command_daemon_composition.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `internal/adapter/mcp/server.go`
- Test: `internal/adapter/mcp/discovery_test.go`
- Test: `internal/app/bridge/media_test.go` or focused mismatch test
- Test: `cmd/shellbeam` runtime acceptance

**Interfaces:**
- Produces: safe `capability.RuntimeIdentity` (schema v1) embedded in optional `Catalog.Runtime`.
- Produces: `failure.RuntimeVersionMismatch` with allowed details `mcp_revision`, `daemon_revision`, `reason`, `recovery`.
- Produces: startup identity capture function in `internal/buildinfo` whose dependencies are injectable/testable.

- [ ] Add failing buildinfo tests for startup identity normalization: embedded VCS revision/modified state, linker fallback, and deterministic executable SHA-256 from injected bytes/reader.
- [ ] Implement minimal safe build identity capture; no executable path in returned/public type; run buildinfo tests GREEN.
- [ ] Add failing catalog/schema tests for optional runtime identity and clone/validation behavior.
- [ ] Add `RuntimeIdentity` to capability catalog and strict IPC/MCP output schemas; run schema/catalog tests GREEN.
- [ ] Add failing daemon test proving `InspectServer` returns runtime identity including daemon incarnation/start time supplied at service construction.
- [ ] Wire daemon startup-captured runtime identity into service options/catalog; run daemon test GREEN.
- [ ] Add failing failure-spec test for `runtime_version_mismatch` safe details.
- [ ] Register the new public failure code/spec; run failure tests GREEN.
- [ ] Add failing MCP tests: equal identities forward normally; unequal SHA/revision returns `runtime_version_mismatch` before forwarding the requested daemon operation; unknown identity remains compatible.
- [ ] Implement local MCP identity capture and comparison. Prefer SHA when both sides have one, then revision; do not auto-restart; run MCP tests GREEN.
- [ ] Add a real daemon/MCP acceptance proving `inspect.server` publishes no executable path and a synthetic/proven skew cannot silently forward an operation.
- [ ] Run `go test ./internal/buildinfo ./internal/core/capability ./internal/core/failure ./internal/app/daemon ./internal/app/bridge ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -count=1`.
- [ ] Commit `feat: detect shellbeam runtime skew`.

### Task 4: P0 integration and release-grade verification

**Files:**
- Test-only changes if a cross-boundary acceptance fixture is required.
- Update this plan with exact verification evidence after execution.

**Interfaces:**
- Exercises canonical strict schema, permissive MCP transport schema, IPC error envelope, bridge projection, MCP error result, and runtime identity handshake as one control plane.

- [ ] Run a modern official MCP-session malformed call and assert the complete returned text/structured payload is bounded and contains no schema dump markers.
- [ ] Run representative known operational failures through the live in-process IPC→bridge→MCP stack and assert no `internal` collapse.
- [ ] Run runtime identity match/mismatch compatibility matrix.
- [ ] Run `gofmt` on changed Go files and `git diff --check`.
- [ ] Run focused race: `go test -race ./internal/core/failure ./internal/app/bridge ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/daemon ./cmd/shellbeam -count=1 -timeout 15m`.
- [ ] Run build: `go build ./...`.
- [ ] Run whole repository: `go test ./... -count=1 -timeout 15m`.
- [ ] Inspect final diff against this spec; verify no P1+ work, no auto-restart, no canonical schema weakening, no unsafe public details.
- [ ] Commit any final test/docs-only verification updates if needed.
