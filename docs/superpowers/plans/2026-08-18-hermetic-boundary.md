# Hermetic Boundary V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a verification-only hermetic execution boundary that can produce authoritative `proven_input_scope` only when a qualified provider enforces all declared V1 dependency channels before first child instruction.

**Architecture:** ShellBeam core owns request identity, immutable source-cut binding, durable child truth, authority promotion, and bounded public metadata. A private replaceable provider owns filesystem/network/environment/toolchain enclosure and ephemeral writable state. Implementation is gated by provider qualification; no provider is selected merely because it exists.

**Tech Stack:** Go 1.26.5 ShellBeam core plus a separately qualified Linux sandbox/container primitive; exact provider/version is intentionally frozen only after Task 0.

## Global Constraints

- Verification-only, non-persistent, non-PTY, closed stdin.
- Network off; no V1 network allowlist.
- Fixed environment/toolchain; no inherited secret environment values.
- Explicit bounded repo-relative immutable input capture; host workspace never mutates.
- E27 remains advisory unless the hermetic boundary itself proves all claimed channels.
- No `probably_authoritative` state.
- No OCI image builder/orchestrator, remote runner, package manager, or workflow language.
- No production implementation before Provider Qualification Gate PASS.

---

### Task 0: Provider Qualification Gate

**Files:**
- Create: `docs/superpowers/evidence/2026-08-18-hermetic-provider-qualification.md`
- Create: `.build/hermetic-a0/*` ignored raw measurement artifacts

- [ ] Evaluate maintained Linux candidates against provenance, exact pinning, public security boundary, no implicit download/network, filesystem/network/env/toolchain enclosure primitives, complete descendant containment, crash behavior, resource footprint, cleanup/storage convergence, and licensing.
- [ ] Run native spikes outside production source for undeclared file escape, network/DNS, inherited env, descendant escape, host mutation, provider crash, 20-command pressure, 60s idle, and repeated lifecycle cleanup.
- [ ] Record PASS/FAIL and freeze exactly one provider/topology/version or record `NOT_RUN/FAIL`.
- [ ] **Hard stop:** if Task 0 is not PASS, do not implement Tasks 1+.

### Task 1: Freeze hermetic request/capability/proven-scope contracts

Create focused core types for request, provider identity, boundary result, and `proven_input_scope`; bind the request to operation identity while preserving all old fingerprints when absent. Add strict MCP/IPC schema support and a separate experimental feature catalog.

### Task 2: Implement bounded immutable repo-input capture

Reuse workspace/source-generation authority. Capture only normalized repo-relative requested selectors at one consistency cut into provider-private storage; reject escape, `.git`, ShellBeam state, special files, symlink escape, submodule/external path, and budget truncation. No live mutable bind mount may satisfy V1.

### Task 3: Implement the qualified provider adapter behind a private port

Translate the frozen boundary request into the selected provider's exact filesystem/network/env/toolchain/child-tree primitives. No provider-specific mechanics enter ordinary model-facing workflow. Provider setup must complete before exec or prevent child spawn.

### Task 4: Bind hermetic execution to durable operation truth

Add pre-exec boundary identity/result state, preserve literal child receipt evidence, and publish authoritative hermetic completion only after provider continuity/completion is proven. Boundary loss after spawn can invalidate hermetic authority but cannot rewrite literal child exit.

### Task 5: Promote only proven scope into evidence validity

Allow `proven_input_scope` to narrow evidence validity only for successful boundary-v1 executions. E27 negative observations do not narrow further in V1; non-hermetic executions retain current conservative behavior.

### Task 6: Privacy, cleanup, and crash recovery

Keep provider paths/raw capture metadata private, enforce no secret env, discard ephemeral writes, sweep crash residue, and prove host workspace remains byte-for-byte unchanged outside normal ShellBeam state.

### Task 7: Native Linux adversarial matrix

Run all 13 design escape/continuity cases plus resource/storage gates on the selected provider. Cross-build is insufficient. Any channel that cannot be proven moves the platform/provider to unsupported rather than weakening semantics.

### Task 8: Full verification and anti-goal audit

Run focused race, full `go test ./...`, hardening/security, checkpoint verify, build, provider-native evidence, and anti-goal scan. Only then may `hermetic_boundary_v1` and authoritative `proven_input_scope` be advertised.
