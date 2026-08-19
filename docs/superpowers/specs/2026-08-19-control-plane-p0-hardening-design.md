# Control-Plane P0 Hardening Design

## Status

Approved for implementation on 2026-08-19. Scope is limited to three P0 control-plane failures discovered through live ShellBeam use.

## Goal

Make ShellBeam's agent-facing control plane fail compactly, detect proven MCP/daemon binary skew, and preserve all known recoverable failure types across daemon → IPC → bridge → MCP without collapsing them to `internal`.

## Scope

This design implements exactly:

1. **P0-D — Recoverable-error typing invariant**
2. **P0-B — Compact validation failures / no full-schema amplification**
3. **P0-C — Runtime/version skew detection**

P1+ verification semantics, automatic capability composition, interactive handoff, macOS containment, and unrelated ergonomics are out of scope.

## Global invariants

- No store schema bump.
- No ShellBeam protocol major-version bump.
- No automatic daemon or tunnel restart.
- Canonical strict API schemas remain strict and remain the contract source of truth.
- MCP v1 behavior remains compatible unless a security/correctness boundary already rejects the request.
- Public failures never contain a full JSON Schema, raw validator dump, private path, command payload, secret, or unbounded implementation error text.
- `internal` is reserved for an unknown error or an actual ShellBeam invariant violation; a known operational failure code must survive public boundaries as itself.
- Failure details are bounded and whitelist-filtered by `internal/core/failure`.
- Proven runtime mismatch fails closed. Missing/insufficient identity does not create a false mismatch.

---

## P0-D — Recoverable-error typing invariant

### Problem

The daemon already emits typed `failure.Failure` values and IPC error envelopes already carry `details`, but the v2 IPC client projects only `code`, `message`, and `retryable` into `bridge.Response`. `bridge.Handler` then reconstructs a failure from only the code string. MCP tool errors likewise omit details.

This loses recovery facts such as `workspace_id`, `reason`, or future revision information. Worse, an operational failure that arrives through an unrecognized/raw path may be collapsed to `internal` even when ShellBeam has a stable public code for it.

### Design

`bridge.Response` gains `Details map[string]string`.

For v2:

```
daemon typed failure
  → IPC Error{code,message,retryable,details}
  → ipc.Client copies details into bridge.Response
  → bridge re-projects failure.New(Code(code), details, nil)
  → failure.Public filters to the code's safe allowlist
  → MCP structured error includes safe details
```

Unknown codes continue to become:

```json
{"code":"internal","message":"internal error","retryable":false}
```

with no details.

The boundary invariant is tested over representative failures and by a table that iterates public operational codes where practical. The test proves that a code registered in `publicSpecs` cannot become `internal` merely because it crossed IPC/bridge/MCP.

### Non-goal

This work does not mechanically relabel every arbitrary `fmt.Errorf` in the repository. Raw errors that represent true unknown failures may still become `internal`. Operational paths introduced or modified by this P0 must create typed failures at their source.

---

## P0-B — Compact validation failures

### Problem

The MCP tool currently advertises the canonical strict `mcp-input-v2.json`. External harnesses may validate tool arguments against that schema before a request reaches ShellBeam. On failure, the harness can include the entire large schema in its diagnostic. A one-field mistake therefore amplifies into tens of kilobytes of context pollution.

ShellBeam cannot fix that amplification in `call()` because `call()` is never reached.

### Design: dual-schema boundary

Keep the canonical strict schema unchanged:

```
api/schema/mcp-input-v2.json
```

It remains authoritative for contract tests, IPC parity, documentation, and internal strictness.

Expose a separate **transport-admission MCP schema** for the modern `local_shell` tool. It has a new schema identity and only guarantees that arguments are an object. It may enumerate known property names for discoverability, but it does not put type/enum/required/cross-field constraints on those properties and allows additional properties. Therefore normal semantic mistakes cannot be rejected by the external MCP schema validator.

ShellBeam then enforces the real contract itself:

```
transport object
  → bounded action/field preflight
  → strict jsonstrict.Decode
  → per-action semantic validation
  → compact failure.InvalidInput
```

The modern tool must return a bounded error like:

```json
{
  "schema_version": 2,
  "ok": false,
  "action": "start",
  "error": {
    "code": "invalid_input",
    "message": "invalid input",
    "retryable": false,
    "details": {
      "field": "wat",
      "reason": "unknown_field"
    }
  }
}
```

For malformed JSON where no safe field can be identified, use `reason=invalid_json`. For wrong values/types where exact classification is not safely available, use bounded `reason=invalid_value`; raw decoder/validator text is not public output.

### Required proof

- The transport schema accepts an object that the canonical strict schema rejects.
- The same malformed semantic request reaches ShellBeam through an official MCP session and returns compact `invalid_input`.
- The public text/structured result contains no `$defs`, `oneOf`, `Schema:`, canonical schema body, raw command value, or unbounded validator text.
- Valid requests and MCP v1 behavior remain unchanged.

---

## P0-C — Runtime/version skew detection

### Problem

`inspect.server` currently describes capabilities but not the actual running binary. A source tree or MCP process may be newer than the daemon while protocol/schema surfaces remain compatible. The harness can then silently believe a feature is running when the daemon is stale.

### Runtime identity

Add a safe optional runtime identity to the server catalog:

```text
schema_version = 1
version
revision
vcs_modified
binary_sha256
daemon_incarnation
daemon_started_at
```

Identity capture rules:

- Capture once at process startup.
- Prefer Go embedded build settings (`vcs.revision`, `vcs.modified`) with linker buildinfo as fallback.
- Hash the running executable once at startup; do not re-read it after an on-disk binary replacement.
- Never expose executable path or build host path.
- `daemon_incarnation` and `daemon_started_at` describe the running daemon instance, not MCP.

### Comparison

The MCP process captures its own build identity. Before a modern tool call is forwarded, compare MCP and daemon identity when enough information exists to prove a mismatch.

Comparison priority:

1. If both binary SHA-256 values are available, unequal digests prove mismatch.
2. Else, if both non-placeholder revisions are available, unequal revisions prove mismatch.
3. If identity is incomplete/unknown, do not manufacture a mismatch.

A proven mismatch returns:

```json
{
  "code": "runtime_version_mismatch",
  "message": "ShellBeam MCP and daemon builds do not match",
  "retryable": false,
  "details": {
    "mcp_revision": "...",
    "daemon_revision": "...",
    "reason": "binary_identity_mismatch",
    "recovery": "restart_daemon"
  }
}
```

No request that depends on daemon semantics is forwarded after a proven mismatch. ShellBeam does not restart anything automatically.

`inspect.server` exposes daemon identity when builds match or comparison is unavailable. The mismatch path itself is typed and compact.

### Compatibility

Runtime identity is optional in the v2 catalog so older daemon fixtures remain decodable. A missing identity means compatibility mode, not failure. This avoids breaking existing tests/older daemons while preventing silent operation when skew is actually provable.

---

## Acceptance criteria

1. A modern malformed `local_shell` object is not rejected by the exposed transport schema; ShellBeam returns `invalid_input` with bounded safe details.
2. Canonical `MCPInputV2` remains strict and rejects the same malformed request.
3. Known typed failure details survive daemon/IPC/bridge/MCP; unknown codes still become detail-free `internal`.
4. `inspect.server` can expose safe runtime identity without paths/secrets.
5. Proven MCP/daemon binary or revision mismatch returns `runtime_version_mismatch` before forwarding a normal operation.
6. Missing runtime identity does not falsely block compatibility.
7. No auto-restart and no store/protocol-major migration.
8. Focused, race, build, schema, and whole-repository tests pass.
