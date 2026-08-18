# Automatic Workspace Binding and Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically bind v2 cwd-only execution to durable workspace identity and return typed stale-workspace failures without destructive Git behavior.

**Architecture:** Extend the existing workspace application resolver rather than adding a parallel registry. `workspace.Service` owns admission-time cwd resolution/lazy attach and stale reconciliation; daemon admission consumes one resolver method and remains responsible for operation idempotency/reservation ordering. Existing observer/provenance code consumes the newly persisted identity naturally.

**Tech Stack:** Go 1.26, existing workspace/store/git adapters, daemon IPC/MCP tests.

**Spec:** `docs/superpowers/specs/2026-08-19-automatic-workspace-binding-reconciliation-design.md`

## Global Constraints

- P0 only; do not implement P1-P6 enhancements.
- No protocol or store schema bump.
- No `git worktree prune` or destructive Git operation.
- Explicit `workspace_id` never silently rebinds.
- Outside-Git cwd remains an ordinary unregistered shell.
- Every production behavior is TDD: failing test first, observed RED, minimal GREEN.

---

### Task 1: Admission-time cwd resolution and lazy durable attach

**Files:**
- Modify: `internal/app/workspace/address.go`
- Modify: `internal/app/workspace/service.go`
- Test: `internal/app/workspace/address_test.go`
- Test: `internal/app/workspace/service_test.go`

**Interfaces:**
- Produces: `(*Service).ResolveAdmissionAddress(context.Context, core.Address) (core.ResolvedAddress, error)`.
- Reuses: `(*Service).Attach(context.Context, path, label)` and existing registry/Git ports.

- [x] Write failing tests proving cwd-only resolution inside an unregistered Git worktree lazily persists one workspace/repository and returns its ID/logical cwd.
- [x] Run focused workspace tests and observe the expected missing-behavior failure.
- [x] Implement minimal admission resolver: registry-first match, otherwise Git inspect + attach, outside-Git passthrough.
- [x] Run focused tests to GREEN.
- [x] Add failing test proving second resolution reuses registry identity and does not call Git `Inspect` again for discovery.
- [x] Implement minimal registry fast path and run tests GREEN.

### Task 2: Typed stale workspace reconciliation

**Files:**
- Modify: `internal/app/workspace/address.go`
- Modify: `internal/app/workspace/service_helpers.go` if a focused helper is needed
- Modify: `internal/core/failure/failure.go`
- Test: `internal/app/workspace/address_test.go`
- Test: `internal/core/failure/failure_test.go` or closest existing public-projection test

**Interfaces:**
- Produces application sentinel errors `ErrWorkspaceStale` / `ErrWorkspaceRootMissing` carrying enough context for daemon normalization.
- Produces public failure codes `workspace_stale` and `workspace_root_missing` with `workspace_id` and `reason` as applicable.

- [x] Write failing explicit-ID tests for missing root and unresolved GitDir; assert no alternate workspace is selected.
- [x] Observe RED.
- [x] Implement minimal stale classification in registered-address reconciliation.
- [x] Write failing public failure projection tests.
- [x] Add failure codes/specs and normalization mapping; run tests GREEN.
- [x] Add cwd-only recovery test showing a live cwd can attach despite an unrelated/obsolete stale registry record; run GREEN.

### Task 3: Daemon admission uses automatic workspace binding

**Files:**
- Modify: `internal/app/daemon/workspace_resolver.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `cmd/shellbeam/command_daemon.go` only if constructor wiring requires no-op signature alignment
- Test: `internal/app/daemon/workspace_address_test.go`
- Test: `internal/app/daemon/workspace_lazy_test.go`

**Interfaces:**
- `WorkspaceResolver` keeps the existing explicit-address contract; cwd-only admission uses the optional `AdmissionWorkspaceResolver.ResolveAdmissionAddress(context.Context, workspace.Address)` capability so unrelated resolver test doubles/consumers are not widened.
- `resolveStartIntent` calls it for all protocol-v2 absolute-cwd starts, not only explicit workspace IDs.

- [x] Write failing daemon test: cwd-only request receives resolved durable workspace ID and logical/physical cwd before reservation.
- [x] Observe RED and confirm owner start count remains zero on resolution errors.
- [x] Wire resolver into v2 cwd-only admission and propagate resolved workspace ID into intent/reservation.
- [x] Run daemon focused tests GREEN.
- [x] Preserve and run replay-after-move test proving resolver is called only on first admission.

### Task 4: Native acceptance and regression verification

**Files:**
- Modify/Test: closest existing native daemon acceptance test under `cmd/shellbeam/` (prefer extending workspace/address acceptance rather than creating a broad new suite).

**Interfaces:**
- Exercises real store + real Git adapter + daemon service.

- [x] Write failing native acceptance: initialize temporary Git repo, start cwd-only v2 command, assert receipt binding is non-empty and workspace registry contains the same root.
- [x] Native acceptance passed immediately after lower-layer RED→GREEN implementation; no acceptance-specific production adjustment was required.
- [x] Make only production fixes required by the acceptance test; run GREEN.
- [x] Add acceptance for outside-Git cwd remaining unregistered and explicit stale ID failing before spawn.
- [x] Run: `go test ./internal/app/workspace ./internal/app/daemon ./internal/core/failure ./cmd/shellbeam`.
- [x] Run: `go test -race ./internal/app/workspace ./internal/app/daemon ./cmd/shellbeam`.
- [x] Run repository full suite: `go test ./...`.
- [x] Run `git diff --check` and inspect final diff for P0-only scope.


## Execution Evidence

- Lower-layer TDD observed RED before GREEN for lazy cwd binding, stale classification, daemon durable binding/replay, Input Trace propagation, Evidence expected-output binding, and stale `inspect.code` lookup.
- Native daemon acceptance proves cwd-only Git execution persists a workspace/repository binding and outside-Git cwd remains unregistered.
- Explicit stale workspace IDs fail before spawn with `workspace_stale` / `workspace_root_missing` and public-safe `workspace_id` + `reason`.
- A2.6 privacy acceptance was updated to the approved P0 contract: one bounded `git` discovery is allowed for an unregistered cwd; all unrelated guarded probes remain forbidden.
- Fresh verification: `go test ./... -count=1 -timeout 15m` PASS.
- Fresh race verification: `go test -race ./internal/app/workspace ./internal/app/daemon ./cmd/shellbeam -count=1 -timeout 15m` PASS.
- Fresh build and hygiene: `go build ./...` PASS; `git diff --check` PASS.
