# Decision Protocol V1 — Multi-Workspace Routing Amendment

Date: 2026-08-21
Status: approved design; repository amendment authority
Target branch: `fix/decision-protocol-multi-workspace`
Historical base supplied at approval: `origin/main` observed as `3939ddb1c465405d1c1def04cc103b83d2655d6c` before connector failure
Repository base observed while materializing this amendment: `origin/main` at `3939ddbde8766fb3c6c5e8ce886d2aec2621503c`

## 1. Problem

Decision Protocol V1 currently resolves repository context by requiring the daemon to have exactly one registered workspace. In a long-lived daemon with multiple registered workspaces, Decision actions that require repository context fail before dispatch.

The current transport contract also rejects caller-supplied Decision `workspace_id`, while the runtime has no alternative trusted per-request workspace binding. Therefore the original assumption that Decision workspace identity can always be server-derived is incomplete in the current global MCP/daemon architecture.

The MCP process working directory is not an authoritative workspace binding and MUST NOT be used for Decision routing.

## 2. Amendment

For Decision Protocol actions only, allow **top-level** `workspace_id` in the outer MCP/IPC request envelope as a **workspace address selector**.

This field is not a Decision semantic field and is not authoritative repository identity. The nested `decision` payload remains unchanged.

`repository_id` remains server-derived and MUST NOT become caller-writable. A nested `decision.workspace_id` or `decision.repository_id` remains invalid input.

## 3. Authoritative resolution algorithm

For every Decision action that needs workspace/repository context:

1. If outer `workspace_id` is present:
   - validate/parse it as a workspace ID;
   - inspect the daemon's registered workspace state for that ID;
   - if no registered workspace exists, return the stable workspace-not-found failure;
   - use the inspected workspace record as the authoritative workspace;
   - derive `repository_id` from that inspected workspace server-side.
2. If outer `workspace_id` is absent:
   - list registered workspaces;
   - exactly one workspace: retain existing singleton fallback for backward compatibility;
   - zero workspaces: fail closed with `decision_context_unavailable`;
   - more than one workspace: fail closed with `decision_context_unavailable`.
3. Never infer a Decision workspace from:
   - MCP process cwd;
   - daemon "last used" workspace;
   - mutable implicit session history;
   - an unverified caller repository identifier.

The selector applies to every Decision action whose dispatch requires workspace/repository context. Semantic lookups remain keyed by their canonical Decision identities; this amendment does not turn the selector into a second semantic ownership field.

## 4. Security and authority invariants

The routing selector grants no repository authority by itself. The server MUST:

- resolve the selector against the registered workspace registry;
- derive repository ownership from the resolved workspace;
- continue to derive trusted authority actor identity from authenticated IPC peer context;
- reject caller-forged server-owned repository/workspace fields inside Decision semantic payloads.

`workspace_id` in the outer envelope is analogous to an address used to select a registered server-owned object. The authoritative repository context is the resolved workspace record, not the caller string.

## 5. Stable failure taxonomy

Transport/runtime failures introduced or clarified by this amendment:

### `decision_context_unavailable`

Returned when:

- no registered workspace exists and no explicit resolvable selector is supplied; or
- registered workspace state is ambiguous because more than one workspace exists and no explicit selector is supplied.

### Existing workspace-not-found failure

Returned when an explicit selector references a workspace that is not registered.

### Semantic lookup failures

Decision semantic lookup failures MUST no longer collapse to generic `internal`:

- `decision_episode_not_found`
- `decision_candidate_not_found`
- `decision_experiment_not_found`

### Decision domain rejections

Decision domain rejections represented by core `ReasonError` MUST be projected to the stable public failure:

- `decision_protocol_rejected`
  - safe detail: `reason=<stable Decision ReasonCode>`

The projection MUST preserve the core `ReasonCode`, including stale policy/projection and invalid protocol-transition/conflict cases. No arbitrary internal error text or entity IDs need to be exposed.

## 6. Backward compatibility

Existing callers that omit `workspace_id` continue to work when exactly one workspace is registered.

Existing Decision semantic request DTOs remain wire-compatible. Only the outer Decision action field set changes to permit `workspace_id`.

Existing callers attempting to place `workspace_id` or `repository_id` inside `decision` remain rejected.

## 7. Required RED tests before production changes

### Runtime routing

- registered workspaces A and B + explicit selector A => repository A derived server-side;
- registered workspaces A and B + explicit selector B => repository B derived server-side;
- registered workspaces A and B + no selector => `decision_context_unavailable`;
- zero registered workspaces + no selector => `decision_context_unavailable`;
- explicit unknown workspace => stable workspace-not-found failure;
- singleton + no selector => existing behavior remains green.

### Transport

- Decision MCP outer `workspace_id` is accepted and forwarded;
- bridge/IPC preserves the outer workspace selector for Decision dispatch;
- nested Decision `workspace_id` remains rejected;
- nested Decision `repository_id` remains rejected;
- strict per-action field validation remains closed for unrelated fields.

### Semantic failures

- nonexistent episode => `decision_episode_not_found`, not `internal`;
- nonexistent candidate => `decision_candidate_not_found`, not `internal`;
- nonexistent experiment => `decision_experiment_not_found`, not `internal`;
- core `ReasonError` => `decision_protocol_rejected` with the original stable `ReasonCode` in safe detail `reason`.

## 8. Acceptance

The amendment is complete only when:

1. `decision.policy.snapshot` works in a daemon with 2+ registered workspaces when the correct outer `workspace_id` is supplied.
2. Repository identity is always derived server-side from the registered workspace.
3. Multi-workspace routing without a selector fails closed and deterministically.
4. Caller forgery of server-owned repository identity remains impossible.
5. Episode/candidate/experiment not-found failures are distinguishable.
6. Stale policy/projection and invalid transition/conflict failures preserve stable Decision reason codes.
7. Existing singleton Decision tests remain green.
8. Focused MCP, IPC, schema, runtime, failure, and Decision tests pass.
9. A realistic isolated multi-workspace daemon probe passes without stopping or mutating the user's production daemon.

## 9. Implementation boundaries

Expected touch points include:

- `cmd/shellbeam/decision_protocol.go`
- `cmd/shellbeam/decision_protocol_test.go`
- `internal/adapter/mcp/input_fields.go`
- `internal/adapter/mcp/request.go`
- `internal/adapter/mcp/decision_protocol_test.go`
- `internal/app/bridge/client_port.go` and Decision bridge tests as needed
- `internal/adapter/ipc/client_v2_request.go`
- `internal/adapter/ipc/protocol_v2_fields.go`
- `internal/adapter/ipc/decision_protocol_v2.go`
- `internal/adapter/ipc/decision_protocol_test.go`
- strict MCP/IPC schemas and generated/checker artifacts as required
- Decision application error typing
- `internal/core/failure` public projection

Do not modify Decision core state-machine semantics.

Do not use the root-repository worktree for tests or implementation.

Do not kill or restart the production daemon. The realistic probe MUST use an isolated daemon/state directory.

## 10. Traceability amendment

The historical Decision Protocol V1 specification remains valid except for the narrower claim that Decision `WorkspaceID` is always purely server-derived and therefore absent from caller-writable transport fields.

That claim is amended as follows:

> For Decision Protocol, `repository_id` is always server-derived. `workspace_id` MAY appear only at the outer transport envelope as an address selector used to select an already registered workspace. The selector is validated against server-owned workspace state and is not semantic Decision authority. Nested Decision repository/workspace identity remains server-owned and caller-forbidden.

The implementation plan and traceability checker MUST bind the new transport-routing tests and semantic failure tests to this amendment rather than weakening the original Decision state-machine requirements.
