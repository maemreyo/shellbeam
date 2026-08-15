# Evidence Ledger and Expected Outputs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans task-by-task. This session intentionally uses one primary agent and does not dispatch subagents.

**Goal:** Implement A2.4 durable mechanical verification evidence and bounded expected-output observation through the existing `local_shell` tool, preserving canonical receipt authority and ordinary-start no-tax behavior.

**Architecture:** Freeze evidence declarations at admission, derive artifact/evidence records only after a durable terminal receipt, persist immutable evidence plus bounded indexes, lazily derive current validity on explicit inspection, and publish only bounded E21 change-feed events. Typed project commands use a backward-compatible project binding schema v2; raw v2 starts use an observation-bound evidence contract.

**Tech Stack:** Go 1.26.x, standard library filesystem/hash primitives, existing project/workspace/receipt/store/observation/IPC/MCP architecture, JSON Schema 2020-12.

## Global constraints

- Exactly one MCP tool: `local_shell`.
- Receipt outcome is never rewritten by evidence/artifact derivation.
- No current manifest/provider read on admitted retry.
- No artifact scan/hash before durable terminal receipt.
- No background evidence validity watcher.
- No exact source/environment/toolchain claim without a trusted producer.
- No artifact contents persisted or returned.
- No network/SSH/gh access in ordinary start path.
- `.codegraph` untouched; no push/PR/merge.
- Production hard cap 500 lines/file, function hard cap 80, interface hard cap 8 methods.

---

### Task 1: Evidence contracts, result/validity model, and capability shape

**Files:**
- Create: `internal/core/evidence/types.go`
- Create: `internal/core/evidence/validation.go`
- Create: `internal/core/evidence/types_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`

**Deliverables:**
- Versioned evidence/contract/artifact/validity types.
- Mechanical result derivation helper independent from adapters.
- Limits for expected outputs, returned records, artifact metadata/digest/tree work, pagination cursor.
- `WithEvidence(...)` capability helper but features remain unpromoted in daemon catalog until Task 7.

- [x] RED: validation/result tests for pass/fail/incomplete/ambiguous and optional-vs-required artifacts.
- [x] RED: validity tests prove fast source is never exact.
- [x] RED: capability baseline unavailable and helper requires positive limits.
- [x] GREEN: minimum contracts + validators + derivation.
- [x] Focused/race/devctl/diff gates.
- [x] Commit `feat: define evidence and artifact contracts`.

### Task 2: Freeze caller and typed evidence bindings at admission

**Files:**
- Modify: `internal/core/project/binding.go`
- Modify: `internal/core/project/binding_test.go`
- Modify: `internal/core/project/validation.go`
- Modify/add: project expected-output public validation tests
- Modify: `internal/app/project/binder.go`
- Modify: `internal/app/project/binder_test.go`
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/core/operation/*test.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `internal/core/receipt/*test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/bindings.go`
- Modify: `internal/app/daemon/project_command.go`
- Modify relevant daemon retry tests.

**Deliverables:**
- `project.CommandBinding` v2 freezes `Kind`, `SourceScope`, `ExpectedOutputs`; v1 remains readable.
- Public canonical `project.ValidateExpectedOutputs` for caller declarations.
- `operation.EvidenceBinding` included in `ObservationBinding` fingerprint.
- Raw v2 reservation/receipt persists normalized evidence binding.
- Typed receipt obtains evidence contract solely from frozen v2 project binding.
- Lost-response retry never rereads current manifest/provider.

- [x] RED: project binding v1 compatibility + v2 required frozen metadata/digest sensitivity.
- [x] RED: binder freezes command metadata and deep copies outputs.
- [x] RED: caller evidence changes observation fingerprint but not execution fingerprint.
- [x] RED: retry conflicting evidence metadata fails before spawn/current-state reads.
- [x] GREEN: schema-v2 project binding + normalized durable evidence binding.
- [x] Focused/race/devctl/diff gates.
- [x] Commit `feat: freeze evidence contracts at admission`.

### Task 3: Bounded filesystem artifact observer

**Files:**
- Create: `internal/app/evidence/observer.go`
- Create: `internal/app/evidence/observer_file_test.go`
- Create: `internal/app/evidence/observer_tree_test.go`
- Create: `internal/app/evidence/path.go`
- Create: `internal/app/evidence/path_test.go`

**Deliverables:**
- Workspace-root-bound artifact observer with no symlink escape.
- File metadata + optional complete SHA-256.
- Directory metadata + optional deterministic complete tree SHA-256.
- Symlink bounded link-text observation without target following.
- Explicit missing/kind-mismatch/unavailable semantics.
- Work ceilings enforced without partial digest identity.

- [x] RED: file/dir/symlink/current/missing/kind mismatch/optional required cases.
- [x] RED: file SHA-256 complete and mutation/work-limit unavailable cases.
- [x] RED: deterministic lexical tree digest, tree ceilings, escaping intermediate/final symlinks.
- [x] GREEN: minimal bounded observer.
- [x] Focused/race/devctl/diff gates.
- [x] Commit `feat: observe expected artifacts safely`.

### Task 4: Immutable evidence persistence, terminal worker, and E21 events

**Files:**
- Create: `internal/app/evidence/worker.go`
- Create: `internal/app/evidence/worker_test.go`
- Create: `internal/adapter/store/evidence.go`
- Create: `internal/adapter/store/evidence_test.go`
- Modify: `internal/app/daemon/types.go`
- Create: `internal/app/daemon/evidence_worker.go`
- Create: `internal/app/daemon/evidence_worker_test.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Add composition helper file if needed to keep structural limits.

**Deliverables:**
- Deterministic evidence ID from receipt digest + contract digest.
- Create-once immutable record persistence with operation/workspace indexes.
- Evidence worker queue bounded; scheduling after durable terminal only.
- Worker loads frozen contract from receipt, resolves workspace root, observes outputs, derives record, persists once.
- `artifact_observed` and `evidence_recorded` scheduled only after durable state.
- Duplicate scheduling idempotent; conflicting bytes fail closed.
- No worker schedule for unqualified bare command.

- [x] RED: store exact-once/restart/index tests.
- [x] RED: terminal scheduling durable-first/no-before-spawn/no-contract/no-tax/backpressure tests.
- [x] RED: receipt success + missing required artifact produces evidence fail while receipt stays success.
- [x] GREEN: persistence + worker + daemon composition.
- [x] Relevant race/devctl/diff gates.
- [x] Commit `feat: persist terminal evidence records`.

### Task 5: Bounded evidence inspection and lazy current validity

**Files:**
- Create: `internal/app/evidence/service.go`
- Create: `internal/app/evidence/service_test.go`
- Create: `internal/app/evidence/cursor.go`
- Create: `internal/app/evidence/cursor_test.go`
- Extend: `internal/adapter/store/evidence.go`
- Extend tests.

**Deliverables:**
- `InspectRequest/InspectResult` with bounded operation/workspace/kind/result filters.
- Signed opaque `evcur_v1` cursor bound to filters + index generation.
- `available|never_run|unavailable` status.
- Current source validity from compatible exact facts when present, otherwise fast generation/unknown only.
- Optional bounded artifact revalidation; immutable base record unchanged.
- Separate validity observation persistence and `evidence_validity_changed` event only when an explicit re-observation changes status.

- [x] RED: pagination/tamper/filter binding/never-run tests.
- [x] RED: fast-current/fast-mismatch/unknown and never-exact-with-fast-only tests.
- [x] RED: revalidation changes separate validity but immutable record bytes stay identical.
- [x] GREEN: bounded inspect/cursor/lazy validity.
- [x] Focused/race/devctl/diff gates.
- [x] Commit `feat: inspect persisted evidence validity`.

### Task 6: IPC/MCP/schema exposure for evidence declarations and inspection

**Files:**
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Add focused schema/IPC/MCP tests.

**Deliverables:**
- Raw `start.evidence` closed schema.
- `inspect.evidence` action in existing `local_shell` union.
- Typed results forwarded through daemon→IPC→bridge→MCP.
- Unknown/cross-action fields rejected.
- No second tool and no deferred branch.

- [x] RED: closed schema start evidence + inspect request/response.
- [x] RED: IPC/MCP forwarding and no-spawn inspection tests.
- [x] GREEN: transport/application wiring.
- [x] Package/race/devctl/diff gates.
- [x] Commit `feat: expose evidence through local shell`.

### Task 7: Capability promotion, real-daemon acceptance, no-tax, and A2.4 checkpoint

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Create: `cmd/shellbeam/evidence_ledger_test.go`
- Modify capability/schema tests as needed.
- Mark this plan complete only after evidence.

**Acceptance scenarios:**
- caller raw v2 build/test with expected file SHA-256;
- typed project command with frozen expected output and retry after manifest mutation;
- child success + required missing output => evidence fail, receipt still success;
- optional missing output => evidence can pass;
- directory tree digest and symlink observation;
- restart then inspect persisted evidence;
- fast source validity is fast/current or unknown, never exact;
- ordinary start/poll without evidence contract causes zero artifact/evidence worker filesystem work;
- Event Journal sees bounded artifact/evidence events after durable state;
- exactly one `local_shell` tool.

- [x] RED final capability/real-daemon/no-tax acceptance.
- [x] Promote `FeatureEvidenceLedger` + `FeatureExpectedOutputs` with exact hard limits.
- [x] Acceptance x3; relevant/full race; `go mod verify`.
- [x] `go run ./tools/devctl check` and `devctl test --dirty --base origin/main --json`.
- [x] Privacy/anti-goal scan; one-tool discovery; `git diff --check`; `.codegraph` clean.
- [x] Capture exact fingerprint/fresh receipts and post-commit verify exact checkpoint.
- [x] Commit `test: verify evidence ledger and expected outputs`.

## Self-review

- The plan never makes evidence a second receipt.
- New typed bindings version their expanded semantics instead of silently changing schema-v1 meaning.
- Admission freezes contracts; terminal/retry paths do not read current manifest/provider state.
- Artifact work is terminal-only and bounded.
- Current validity is explicit/lazy; exact claims require exact producers.
- E21 events are notifications after durable evidence state.
- A2.5 environment/toolchain fingerprint remains a separate producer slice.
