# Automatic Workspace Binding and Reconciliation Design

## Goal

Make protocol-v2 `cwd`-only execution inside a Git worktree automatically acquire durable ShellBeam repository/workspace identity, while preserving strict explicit `workspace_id` semantics and turning stale registered worktrees into typed, recoverable failures.

## Scope

This change is P0 only. It does not change verification sufficiency, capability composition, interactive sessions, macOS containment, activity aggregation, or generic error compactness.

## Current Problem

ShellBeam can discover Git provenance for an unregistered directory during an explicit fresh observation, but ordinary command admission intentionally uses registry-only `Bind`/`ObserveCached`. A v2 start with only an absolute `cwd` therefore records `workspace_unregistered`, so Evidence, Repro, Input Trace, Environment, Code Intelligence, and other workspace-aware features lose durable identity. Separately, a registered worktree whose root/gitdir has disappeared can surface opaque downstream `internal` errors.

## Design

### 1. Admission-time cwd binding

Add a workspace application operation dedicated to admission-time address resolution. For a v2 start without `workspace_id`:

1. Validate the absolute cwd.
2. Check registered workspaces first and choose the most-specific containing workspace without invoking Git.
3. Reconcile that registered workspace root from its durable GitDir before use.
4. If no registered workspace covers cwd, inspect Git exactly once at cwd and call the existing attach semantics to persist repository/workspace identity. This writes only ShellBeam registry records; it never creates, removes, moves, or prunes Git worktrees.
5. Return a `ResolvedAddress` containing the durable workspace ID plus a workspace-relative logical cwd and the canonical execution cwd.
6. If cwd is outside Git, preserve ordinary unregistered execution exactly as today.

Repeated cwd-only starts after lazy registration must resolve from the registry and avoid Git discovery tax, except for the existing cheap reconciliation required to prove a registered root is still usable.

### 2. Explicit workspace IDs stay strict

A request carrying `workspace_id` must never silently rebind to a different workspace. It resolves only that durable identity. Operation replay continues to use the stored admitted binding and must not re-resolve a moved/deleted workspace.

### 3. Stale workspace semantics

A registered workspace is stale when its durable GitDir cannot resolve to a live worktree and its recorded root is absent or no longer represents that Git worktree.

Expose typed public failures:

- `workspace_stale` with `workspace_id` and reason `gitdir_unresolved` or `root_mismatch`.
- `workspace_root_missing` with `workspace_id` when the registered root no longer exists.

Explicit-ID admission fails before reservation/spawn with the typed error. Cwd-only admission may ignore stale records that merely overlap the path, inspect the live cwd, and attach/reuse the Git identity actually present there.

No automatic `git worktree prune`, deletion of filesystem content, or destructive Git operation is allowed. Registry deletion is not required for admission correctness; stale records may remain inspectable until explicit housekeeping.

### 4. Provenance outcome

After successful cwd-only lazy binding, normal daemon capture must see the newly registered workspace and receipt provenance must include repository/workspace binding rather than `workspace_unregistered`. Existing post-execution conservative invalidation semantics are unchanged by this P0.

## Invariants

- No new protocol version or store schema.
- Explicit workspace identity is never changed implicitly.
- No spawn/reservation after workspace resolution failure.
- No destructive Git operation from automatic binding/reconciliation.
- Retry/idempotency remains operation-fingerprint driven and does not re-resolve already admitted operations.
- Outside-Git cwd remains valid ordinary shell behavior.
- Existing manual `workspace attach/create/remove/forget` semantics remain valid.

## Acceptance

1. First v2 cwd-only start inside an unregistered Git worktree persists repository/workspace identity and returns a receipt with non-empty workspace binding.
2. A second cwd-only start in the same worktree resolves from registry without another Git discovery/attach.
3. Nested cwd resolves to the same workspace with correct logical cwd.
4. Outside-Git cwd runs successfully and remains unregistered.
5. Explicit stale workspace fails before spawn with a typed stale/root-missing failure.
6. Cwd-only admission in a live worktree can recover even when a stale registry record exists for an old worktree path/identity.
7. Existing replay-after-move test remains green and resolver is not called on replay.
8. Full focused workspace/daemon/IPC/MCP tests and repository-wide tests pass.
