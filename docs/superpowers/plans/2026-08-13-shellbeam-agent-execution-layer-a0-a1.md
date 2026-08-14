# ShellBeam Agent Execution Layer A0/A1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Execute with one primary agent; do not delegate unless the user explicitly authorizes delegation. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver the A0 correctness/compatibility foundation and the A1 workspace/onboarding ergonomics required for ShellBeam to become a deterministic Agent Execution Layer without turning it into an orchestrator.

**Architecture:** Preserve one Go module, one shipped binary, one local daemon, and one MCP tool. Introduce protocol-v2 contracts beside v1, then add consumer-owned application ports for capability discovery, workspace/activity provenance, Git identity advisories, and project-manifest inspection. All new context is observational and non-blocking; command execution remains available when context collection is unavailable.

**Tech Stack:** Go 1.26.5; MCP Go SDK v1.7.0 with MCP `2026-07-28`; JSON Schema draft 2020-12; `github.com/pelletier/go-toml/v2` v2.4.3; existing atomic JSON store, Unix IPC, process-group, PTY, and `devctl` infrastructure. Add no database, vector store, second daemon, frontend, or new network service in A0/A1.

**Design sources:**

- `docs/superpowers/specs/2026-08-13-agent-execution-layer-design.md`
- `docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md`
- `docs/superpowers/specs/2026-08-13-project-capability-onboarding-design.md`
- `docs/superpowers/specs/2026-08-13-shellbeam-v1-design.md`

## Global Constraints

- Keep the modular-monolith import direction in `AGENTS.md`: core imports standard library/core only; app owns its ports; adapters implement ports without importing sibling adapters; `cmd/shellbeam` remains the composition root.
- Split `cmd/shellbeam/command.go` before adding more command families. Do not grow another command router beyond the documented line budget.
- Production files warn above 350 physical lines and fail above 500; test files warn above 600 and fail above 800; functions require review above 60 and fail above 80.
- Avoid generic packages/files named `utils`, `helpers`, `common`, `shared`, `base`, `misc`, or generic `models`.
- Preserve V1 idempotency, process ownership, drain/reap ordering, output quotas, durable terminal receipts, and no-PID-from-disk signaling rules.
- Keep `local_shell` as one closed union. Do not add MCP tools for workspace, project, or evidence inspection.
- Ordinary command admission must not run network, `ssh`, or `gh` probes. Warm cached workspace observation runs no subprocess; cold observation has a strict bounded Git budget.
- Workspace hints, Git-state events, identity mismatches, concurrent-use facts, and manifest review status are advisories. They never change child outcome or block ordinary Git commands.
- `forget` is metadata-only. Material worktree removal is explicit, reports dirty state, and requires `--force` only when Git itself needs force. Never auto-prune.
- Repository manifests are untrusted declarative metadata. Inspecting or validating one never executes a declared command.
- Use test-first slices. Run focused tests, then affected/dirty tests. Reuse Go's build cache; do not use `go build -a`, routine `go clean`, or a fresh full suite after every edit.
- Every local commit in this linked worktree must execute the tracked `.githooks/pre-commit` by invoking Git as `git -c core.hooksPath=.githooks commit ...`. The hook runs the selective staged `devctl commit-gate`; do not set repository-common `core.hooksPath` or install into shared `.git/hooks` because that would affect sibling/primary worktrees.
- Every cached test/build/evidence claim is bound to its exact source fingerprint. Unknown, cached, stale, truncated, or unavailable facts are labeled honestly.
- No remote push, PR creation, service installation, account switching, or repository mutation beyond the named task without explicit user authorization.

## Execution Contract

This plan was prepared in:

```text
repository: /Users/trung.ngo/Documents/zaob-dev/shellbeam
worktree:   /Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees/design_agent-execution-layer
branch:     docs/agent-execution-layer-design
base:       origin/main
base SHA:   d00408827963d4da610f68b5245756137b4694d4
```

Before implementation, either continue in this worktree after committing the documentation baseline, or create a new implementation worktree from that commit. Never switch the user's primary checkout as a side effect.

Set an explicit comparison base once per execution session:

```bash
export SHELLBEAM_A01_BASE="${SHELLBEAM_BASE_REF:-origin/main}"
git status --short --branch
git merge-base --is-ancestor "$SHELLBEAM_A01_BASE" HEAD
go mod verify
```

Expected: correct worktree/branch, no unexpected changes, base resolves, and modules verify. If the tree contains unrelated user edits, preserve them and stop before overlapping files.

## Program Boundary

This plan implements these accepted enhancement groups:

| Group | A0/A1 delivery |
| --- | --- |
| E01 capability discovery | Server inspect fallback plus MCP `2026-07-28` discovery metadata |
| E02 structured result | Closed v2 envelope with operation, child, output, source, advisories, receipt |
| E03 typed failures | Stable public codes, retryability, safe details |
| E04 direct argv | Exact argv execution alongside compatible shell mode |
| E07 workspace provenance | Stable repository/workspace IDs and fast snapshots |
| E08 activity continuity | Reusable caller-declared activity records and dirty baseline |
| E09 worktree lifecycle | List, inspect, attach, create, rename, forget, remove |
| E12 project capability manifest | Strict inspection, status, review provenance, onboarding guidance |
| E13 Git identity | Repository/workspace profiles and advisory preflight |
| E17 transient Git state | Merge/rebase/cherry-pick/revert/bisect/conflict/detached/upstream facts |
| E20 compatibility | Dual-stack IPC/schema negotiation and honest feature absence |

Deferred deliberately:

- A2: output virtualization/search selectors, expected artifacts, evidence reuse, environment fingerprint, mutation scopes, and richer process inspection.
- B1: named persistent sessions and long-running runtime lifecycle.
- B2: optional Semble-style semantic context provider and cooperative provider integration.
- C: remote orchestration, distributed scheduling, and policy enforcement.

## Target Package Map

```text
internal/core/failure       stable public failure taxonomy
internal/core/capability    protocol/features/limits catalog
internal/core/workspace     repository/workspace identity and snapshots
internal/core/activity      activity/baseline facts
internal/core/project       manifest/status/review facts
internal/core/gitidentity   profile and match facts

internal/app/daemon         execution admission and inspect coordination
internal/app/workspace      registry/lifecycle service and consumer ports
internal/app/activity       activity service and consumer ports
internal/app/project        manifest inspection/onboarding service
internal/app/gitidentity    shallow/deep advisory preflight

internal/adapter/ipc        v1/v2 wire translation
internal/adapter/mcp        MCP discovery and one-tool v2 union
internal/adapter/process    shell and direct-argv process spawning
internal/adapter/store      durable v2 operations/workspace/activity metadata
internal/adapter/git        bounded Git observation and worktree operations
internal/adapter/project    filesystem/TOML manifest loader
```

Core packages contain facts and invariants, not filesystem/Git/process calls. Application services own their narrow ports. Adapter packages do not import one another.

---

### Task 0: Land the reviewed design baseline

**Files:**

- Add: `docs/superpowers/specs/2026-08-13-agent-execution-layer-design.md`
- Add: `docs/superpowers/specs/2026-08-13-project-capability-onboarding-design.md`
- Add: `docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md`
- Add: `docs/superpowers/plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md`

- [x] **Step 1: Review the worktree and document set**

```bash
git status --short --branch
git diff --check
for f in \
  docs/superpowers/specs/2026-08-13-agent-execution-layer-design.md \
  docs/superpowers/specs/2026-08-13-project-capability-onboarding-design.md \
  docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md \
  docs/superpowers/plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md; do
  test -s "$f"
done
```

Expected: exactly the intended documentation files are new or modified; no whitespace errors; every file is non-empty.

- [x] **Step 2: Confirm links and plan structure**

```bash
if rg -n 'T[B]D|T[O]DO|F[I]XME|<replace[-]me>' \
  docs/superpowers/specs/2026-08-13-agent-execution-layer-design.md \
  docs/superpowers/specs/2026-08-13-project-capability-onboarding-design.md \
  docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md \
  docs/superpowers/plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md; then
  exit 1
fi
awk '/^```/{n++} END{exit n%2}' docs/superpowers/plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md
test -f docs/superpowers/specs/2026-08-13-shellbeam-v1-design.md
```

Expected: placeholder search returns no unresolved design instruction; code-fence count is even; the V1 dependency exists.

- [x] **Step 3: Commit only the documentation baseline**

```bash
git add \
  docs/superpowers/specs/2026-08-13-agent-execution-layer-design.md \
  docs/superpowers/specs/2026-08-13-project-capability-onboarding-design.md \
  docs/superpowers/specs/2026-08-13-workspace-worktree-git-identity-design.md \
  docs/superpowers/plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md
git diff --cached --check
git commit -m "docs: design agent execution layer"
```

Do this commit only when the user authorizes implementation/commit work. The current document-transfer session itself does not commit or push.

---

### Task 1: Make dirty tests genuinely selective and builds incremental

**Files:**

- Modify: `tools/devctl/main.go`
- Modify: `tools/devctl/test.go`
- Modify: `tools/devctl/evidence.go`
- Modify: `tools/devctl/main_test.go`
- Add: `tools/devctl/impact.go`
- Add: `tools/devctl/impact_test.go`
- Add: `tools/devctl/build.go`
- Add: `tools/devctl/build_test.go`
- Modify: `dev/test-impact.toml`
- Modify: `Makefile`
- Modify: `AGENTS.md`

**Interfaces:**

- `devctl test --dirty --base <ref> --json` selects suites from changed paths and records why each suite was selected.
- `devctl build --dirty --base <ref> --json` reuses Go caches and writes operation-local output under `.build/workspaces/<source-digest>/<build-id>/` before atomic publication.
- A global-path match selects all packages; no relevant change selects no tests and reports `selection=empty`, rather than silently running the full suite.

- [x] **Step 1: Write failing impact-selection tests**

Table-test at least:

```go
tests := []struct {
	name    string
	changed []string
	want    []string
}{
	{"docs only", []string{"docs/guide.md"}, []string{"contract:markdown"}},
	{"schema", []string{"api/schema/mcp-input-v2.json"}, []string{"./api/schema", "./tests/contract"}},
	{"package", []string{"internal/core/workspace/id.go"}, []string{"./internal/core/workspace"}},
	{"global", []string{"go.mod"}, []string{"./..."}},
	{"none", nil, nil},
}
```

Also cover rename pairs, deleted files, malformed TOML, overlapping mappings, deterministic order, and base-ref failure.

- [x] **Step 2: Prove the current bug RED**

```bash
go test ./tools/devctl -run 'TestSelectImpact|TestDirtyTestDoesNotSelectAllOnNoChange' -count=1
```

Expected: failure because current `devctl` always calls `listPackages()` and ignores the actual dirty selection.

- [x] **Step 3: Implement impact parsing and evidence**

Parse `dev/test-impact.toml` once. Extend evidence with:

```go
type SelectionReason struct {
	Suite   string   `json:"suite"`
	Paths   []string `json:"paths"`
	Mapping string   `json:"mapping"`
}
```

Use NUL-safe Git changed-path output. Keep package fallback deterministic and explicit. Never convert an empty selection into all packages unless `--full` was explicitly supplied.

- [x] **Step 4: Add incremental build publication**

Build to a unique staging directory keyed by source digest and build ID, then atomically publish `.build/shellbeam` only after success. Do not clear `GOCACHE`, use `-a`, or share an in-progress output path.

Add Make targets:

```make
test-dirty:
	go run ./tools/devctl test --dirty --base $${SHELLBEAM_BASE_REF:-origin/main}

build-dirty:
	go run ./tools/devctl build --dirty --base $${SHELLBEAM_BASE_REF:-origin/main}
```

- [x] **Step 5: Verify focused and dirty paths**

```bash
go test ./tools/devctl -run 'TestSelectImpact|TestDirty|TestBuildPublication' -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
go run ./tools/devctl build --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
```

Expected: selected suites/reasons match changed paths; a second build reuses Go cache; no fresh full suite is run.

- [x] **Step 6: Commit**

```bash
git add tools/devctl dev/test-impact.toml Makefile AGENTS.md
git commit -m "dev: select affected tests and incremental builds"
```

---

### Task 2: Introduce typed failures and the capability catalog

**Files:**

- Add: `internal/core/failure/failure.go`
- Add: `internal/core/failure/failure_test.go`
- Add: `internal/core/capability/catalog.go`
- Add: `internal/core/capability/catalog_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `internal/adapter/ipc/protocol.go`
- Modify: `internal/adapter/ipc/protocol_test.go`

**Interfaces:**

```go
type Failure struct {
	Code      Code
	Message   string
	Retryable bool
	Details   map[string]string
	Cause     error
}

type Catalog struct {
	ProtocolVersion       int
	ReceiptSchemaVersions []int
	ManifestVersions      []int
	Features              map[Feature]Availability
	Limits                Limits
}
```

Public codes include at least `invalid_input`, `feature_unavailable`, `operation_conflict`, `operation_metadata_conflict`, `workspace_not_found`, `workspace_address_escape`, `manifest_invalid`, `manifest_review_required`, `identity_observation_failed`, and `internal`.

- [x] **Step 1: Write failing taxonomy tests**

Test stable code serialization, `errors.Is`/`errors.As`, safe public messages, retryability, detail-key allowlists, and fallback mapping of unknown internal errors to `internal` without leaking raw paths or secrets.

- [x] **Step 2: Run RED**

```bash
go test ./internal/core/failure ./internal/core/capability ./internal/adapter/ipc -run 'TestFailure|TestCatalog|TestErrorEnvelope' -count=1
```

- [x] **Step 3: Implement core facts and boundary mapping**

Keep raw causes local for logs. IPC/MCP responses expose stable code, retryability, bounded safe details, and a human message. Replace existing public use of `err.Error()` as an error code.

Catalog defaults are compiled from actual support. Do not advertise A2/B features as available; represent them as unavailable or omit them according to the negotiated schema.

- [x] **Step 4: Verify and commit**

```bash
go test ./internal/core/failure ./internal/core/capability ./internal/app/daemon ./internal/app/bridge ./internal/adapter/ipc -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add internal/core/failure internal/core/capability internal/app/daemon internal/app/bridge internal/adapter/ipc
git commit -m "feat: add typed failures and capabilities"
```

---

### Task 3: Add dual-stack IPC v2 and server inspection

**Files:**

- Add: `api/schema/ipc-v2.json`
- Modify: `api/schema/embed.go`
- Modify: `api/schema/embed_test.go`
- Add: `internal/adapter/ipc/protocol_v2.go`
- Add: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/app/daemon/actions.go`
- Modify: `internal/app/daemon/service.go`

**Interfaces:**

- V1 request/response bytes remain accepted unchanged.
- V2 uses an explicit discriminator and closed request/response unions.
- `inspect server` is non-spawning and returns protocol versions, schema versions, features, and limits.
- Version/feature negotiation fails locally with `feature_unavailable`; it never downgrades a request silently.

- [x] **Step 1: Add failing compatibility fixtures**

Persist representative v1 start/poll/write/kill fixtures and assert byte-compatible decoding. Add v2 fixtures for `inspect server`, unknown versions, unknown actions, extra properties, and unsupported features.

- [x] **Step 2: Run RED**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/app/daemon -run 'TestIPCV2|TestV1Compatibility|TestInspectServer' -count=1
```

- [x] **Step 3: Implement translation at the adapter boundary**

Do not spread wire structs through app/core packages. Decode into v1 or v2 adapter structs, translate to application requests, and translate application results back to the requested wire version.

Use JSON Schema 2020-12 closure correctly: union roots use `unevaluatedProperties: false`; branch objects use `additionalProperties: false`. Validate real successful and failed payloads with the production schema library.

- [x] **Step 4: Verify and commit**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/app/daemon -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add api/schema internal/adapter/ipc internal/app/daemon
git commit -m "feat: add ipc v2 negotiation"
```

---

### Task 4: Persist v2 operation bindings and return structured execution results

**Files:**

- Add: `api/schema/operation-v2.json`
- Add: `api/schema/receipt-v2.json`
- Modify: `api/schema/embed.go`
- Modify: `api/schema/embed_test.go`
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/execution.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/core/receipt/receipt.go`
- Add: `internal/core/receipt/result.go`
- Add: `internal/core/receipt/result_test.go`
- Modify: `internal/adapter/store/reservation.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/reconcile.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`

**Interfaces:**

- Existing v1 on-disk operations remain readable.
- V2 stores `request_fingerprint`, `execution_fingerprint`, and `observation_binding_fingerprint` separately.
- The v2 result envelope distinguishes daemon operation state from child outcome and output completeness.
- Optional unsupported objects are absent, not fabricated with zero values.

- [x] **Step 1: Write failing migration and result tests**

Cover:

- reading v1 reservations/receipts after upgrading;
- writing and replaying v2 bindings;
- same operation plus changed request -> `operation_conflict` and no spawn;
- same request plus changed observation metadata -> `operation_metadata_conflict` and no reassignment;
- changed response controls -> replay succeeds;
- transport success plus child exit 1 remains `child.outcome=failure`;
- timeout, kill, failed spawn, abandoned, and ambiguous terminal states;
- exact output byte counts, cursor, truncation, and `output_complete`.

- [x] **Step 2: Run RED**

```bash
go test ./api/schema ./internal/core/operation ./internal/core/receipt ./internal/adapter/store ./internal/app/daemon -run 'TestV2|TestFingerprint|TestStructuredResult|TestReadsV1' -count=1
```

- [x] **Step 3: Implement append-compatible persistence**

Use explicit persisted schema/version fields and translators. Do not rewrite all historical state during daemon startup. New fields are written atomically for new v2 admissions; old records map to honest legacy/unknown values.

Make response-only controls explicit and exclude them from all durable fingerprints. Redact secret-bearing environment/command diagnostics before persistence.

- [x] **Step 4: Verify crash/retry invariants**

```bash
go test ./api/schema ./internal/core/operation ./internal/core/receipt ./internal/adapter/store ./internal/app/daemon -count=1
go test ./internal/adapter/store -run 'Test(Terminal|Publish|Reserve|Abandon|V2Reservation|ReadsV1)' -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
```

- [x] **Step 5: Commit**

```bash
git add api/schema internal/core/operation internal/core/receipt internal/adapter/store internal/app/daemon internal/adapter/ipc/protocol_v2.go
git commit -m "feat: persist structured operation results"
```

---

### Task 5: Expose MCP `2026-07-28` discovery and the v2 one-tool contract

**Files:**

- Modify: `api/schema/mcp-input-v1.json`
- Modify: `api/schema/mcp-output-v1.json`
- Add: `api/schema/mcp-input-v2.json`
- Add: `api/schema/mcp-output-v2.json`
- Add: `api/schema/mcp_v2_test.go`
- Modify: `api/schema/embed.go`
- Add: `internal/adapter/mcp/call.go`
- Add: `internal/adapter/mcp/discovery_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/server.go`
- Modify: `internal/adapter/mcp/server_test.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/ipc/ipc_integration_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler_test.go`

**Interfaces:**

- Exactly one tool remains named `local_shell`.
- Its v2 union initially exposes `start`, `poll`, `write`, `kill`, and `inspect.server`.
- Standard MCP server discovery advertises extension key `io.github.maemreyo.shellbeam` with the capability catalog.
- Legacy initialized clients retain the existing v1 tool behavior and can use the closed non-spawning inspect fallback.

- [x] **Step 1: Add failing SDK-level tests**

Using the pinned MCP Go SDK in memory, initialize both a current and legacy-compatible client. Assert:

- negotiated MCP version;
- one stable tool name;
- discovery metadata and feature limits;
- realistic payload validation for every v1/v2 branch;
- no spawn for `inspect.server`;
- unsupported v2 feature returns `feature_unavailable` rather than trial-and-error schema failure.

- [x] **Step 2: Run RED**

```bash
go test ./api/schema ./internal/adapter/mcp ./internal/app/bridge -run 'TestDiscovery|TestMCPV2|TestInspectServer|TestLegacy' -count=1
```

- [x] **Step 3: Implement against the current pinned SDK**

Use the SDK's `ServerOptions.Capabilities`, discovery support, and initialize result. Do not upgrade the SDK merely for this task: v1.7.0 already supports MCP `2026-07-28`. Keep extension payload bounded and versioned.

Validate the tool schema with `google/jsonschema-go` using real payloads. A union root must use `unevaluatedProperties: false`; retain the regression test that prevents the previous root-closure bug.

- [x] **Step 4: Run the A0 checkpoint**

```bash
go test ./api/schema ./internal/adapter/mcp ./internal/app/bridge ./internal/adapter/ipc ./internal/app/daemon -count=1
go vet ./api/schema ./internal/adapter/mcp ./internal/app/bridge ./internal/adapter/ipc ./internal/app/daemon
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
go run ./tools/devctl check --base "$SHELLBEAM_A01_BASE" --json
git diff --check
```

Expected: v1 compatibility, typed failures, v2 result, and discovery all pass. This is the A0 gate; do not start workspace features while it is red.

- [x] **Step 5: Commit**

```bash
git add api/schema internal/adapter/mcp internal/app/bridge
git commit -m "feat: expose agent execution protocol v2"
```

---

### Task 6: Add stable repository/workspace identity and lifecycle CLI

**Files:**

- Add: `internal/core/workspace/id.go`
- Add: `internal/core/workspace/id_test.go`
- Add: `internal/core/workspace/types.go`
- Add: `internal/app/workspace/service.go`
- Add: `internal/app/workspace/service_test.go`
- Add: `internal/app/workspace/ports.go`
- Add: `internal/adapter/store/workspaces.go`
- Add: `internal/adapter/store/workspaces_test.go`
- Add: `internal/adapter/git/repository.go`
- Add: `internal/adapter/git/worktree.go`
- Add: `internal/adapter/git/worktree_test.go`
- Split: `cmd/shellbeam/command.go`
- Add: `cmd/shellbeam/command_workspace.go`
- Add: `cmd/shellbeam/command_workspace_test.go`
- Modify: `cmd/shellbeam/main.go`

**Interfaces:**

```text
shellbeam workspace list [--json]
shellbeam workspace inspect <label-or-id> [--json]
shellbeam workspace attach <path> [--label <label>] [--json]
shellbeam workspace create <repo> [--ref <ref>] [--path <path>] [--label <label>] [--json]
shellbeam workspace rename <label-or-id> <new-label> [--json]
shellbeam workspace forget <label-or-id> [--json]
shellbeam workspace remove <label-or-id> [--force] [--json]
```

`repository_id` represents stable local repository continuity. `workspace_id` represents one reusable directory/worktree and is never a conversation lock.

- [x] **Step 1: Split the composition/CLI router before adding commands**

Move daemon, service, MCP, and workspace command parsing into focused files while leaving construction in `cmd/shellbeam`. Preserve every existing CLI test and line budget.

```bash
go test ./cmd/shellbeam -count=1
go run ./tools/devctl check --base "$SHELLBEAM_A01_BASE" --json
```

- [x] **Step 2: Write failing identity and lifecycle tests**

Cover bare/non-bare repositories, linked worktrees, symlinked input path normalization, reattach/reuse, moved worktree continuity where provable, label collision suffixing, unconventional labels, explicit path, default sibling template, and path already occupied by the intended worktree.

Removal tests must prove:

- `forget` only removes registry metadata;
- clean explicit remove deletes the named Git worktree and forgets metadata;
- dirty remove reports one actionable advisory and requires `--force`;
- removal never runs `git worktree prune`;
- no command deletes a plain non-worktree directory.

- [x] **Step 3: Run RED**

```bash
go test ./internal/core/workspace ./internal/app/workspace ./internal/adapter/git ./internal/adapter/store ./cmd/shellbeam -run 'TestWorkspace|TestWorktree|TestForget|TestRemove' -count=1
```

- [x] **Step 4: Implement registry and Git lifecycle adapter**

Persist registry metadata with the existing atomic-store pattern. Use Git porcelain output, argument arrays, `--` path separation, bounded output, and no shell interpolation. Naming `<ISSUE-KEY>_<short-purpose>` is a low-priority suggestion only; never reject an existing or unconventional name.

An explicit `workspace remove` command is itself the user's removal intent. Do not add a second interactive confirmation layer. Return dirty/unusual state clearly and let `--force` express the exceptional removal.

- [x] **Step 5: Verify and commit**

```bash
go test ./internal/core/workspace ./internal/app/workspace ./internal/adapter/git ./internal/adapter/store ./cmd/shellbeam -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add cmd/shellbeam internal/core/workspace internal/app/workspace internal/adapter/git internal/adapter/store
git commit -m "feat: add reusable workspace lifecycle"
```

---

### Task 7: Capture fast Git snapshots, transient state, and bounded cache quality

**Files:**

- Add: `internal/core/workspace/snapshot.go`
- Add: `internal/core/workspace/snapshot_test.go`
- Add: `internal/app/workspace/observer.go`
- Add: `internal/app/workspace/observer_test.go`
- Add: `internal/adapter/git/snapshot.go`
- Add: `internal/adapter/git/snapshot_test.go`
- Add: `internal/adapter/git/cache.go`
- Add: `internal/adapter/git/cache_test.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `internal/app/daemon/service.go`

**Interfaces:**

- Fast generation and exact source snapshot are distinct types and fields.
- Snapshot covers repository/workspace IDs, HEAD/ref, dirty summary, merge/rebase/cherry-pick/revert/bisect, conflicts, detached HEAD, and local ahead/behind with quality.
- Quality is one of `fresh`, `cached`, `stale`, or `unavailable`, with observation timestamp and cause-safe diagnostics.
- Context observation failure never changes command admission or child outcome.

- [x] **Step 1: Write failing fixture tests**

Create bounded temporary repositories for clean, modified, untracked, renamed, conflicted, detached, merge, rebase, cherry-pick, revert, bisect, missing upstream, and stale local upstream cases. Assert no fetch/network behavior.

Add budget tests using a fake runner/clock:

- warm hit: zero subprocesses;
- cold/stale: at most two local Git subprocesses;
- timeout/malformed output: cached or unavailable snapshot, command still admitted;
- concurrent identical observations: singleflight/coalesced work without unbounded waiters.

- [x] **Step 2: Run RED**

```bash
go test ./internal/core/workspace ./internal/app/workspace ./internal/adapter/git -run 'TestSnapshot|TestTransient|TestObservationBudget|TestCache' -count=1
```

- [x] **Step 3: Implement observation behind a consumer-owned port**

The daemon depends on the app-level observer interface, not the Git adapter. Use a short TTL and fact-based invalidation. Record pre/post generation in receipts without claiming which exact bytes the child read.

Never execute `ssh`, `gh`, `git fetch`, hooks, or user shell startup for ordinary observation.

- [x] **Step 4: Verify and commit**

```bash
go test ./internal/core/workspace ./internal/app/workspace ./internal/adapter/git ./internal/app/daemon ./internal/core/receipt -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add internal/core/workspace internal/app/workspace internal/adapter/git internal/app/daemon internal/core/receipt
git commit -m "feat: observe bounded workspace provenance"
```

---

### Task 8: Add reusable activities and observational dirty baselines

**Files:**

- Add: `internal/core/activity/activity.go`
- Add: `internal/core/activity/activity_test.go`
- Add: `internal/core/activity/baseline.go`
- Add: `internal/core/activity/baseline_test.go`
- Add: `internal/app/activity/service.go`
- Add: `internal/app/activity/service_test.go`
- Add: `internal/app/activity/ports.go`
- Add: `internal/adapter/store/activities.go`
- Add: `internal/adapter/store/activities_test.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/core/receipt/result.go`

**Interfaces:**

- `activity_id` is optional, bounded, caller-generated, and lazily created on first admitted operation.
- One activity may span conversations/workspaces; one workspace may host multiple activities.
- Dirty comparisons return `inherited_dirty`, `observed_since_baseline`, `resolved_since_baseline`, or `baseline_diverged`.
- No field claims that an activity caused or owns a file change.

- [x] **Step 1: Write failing state and comparison tests**

Cover activity reuse, concurrent activities, multiple workspaces, baseline capture, inherited dirty paths, later path change, resolved dirty path, rename/unmerged/untracked facts, branch/reset/rebase divergence, compaction, and corrupted-record isolation.

- [x] **Step 2: Run RED**

```bash
go test ./internal/core/activity ./internal/app/activity ./internal/adapter/store ./internal/app/daemon -run 'TestActivity|TestBaseline|TestObservedSince|TestDiverged' -count=1
```

- [x] **Step 3: Implement bounded references and lazy creation**

Persist bounded references to operations, workspaces, baselines, and advisories. Reuse the atomic store and retention rules. Do not introduce a create-activity round trip, conversation ID, exclusive lease, or workspace ownership check.

- [x] **Step 4: Verify and commit**

```bash
go test ./internal/core/activity ./internal/app/activity ./internal/adapter/store ./internal/app/daemon ./internal/core/receipt -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add internal/core/activity internal/app/activity internal/adapter/store internal/app/daemon internal/core/receipt
git commit -m "feat: track reusable execution activities"
```

---

### Task 9: Address starts by workspace and emit compact non-blocking context

**Files:**

- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/intent_test.go`
- Add: `internal/core/workspace/address.go`
- Add: `internal/core/workspace/address_test.go`
- Add: `internal/core/workspace/advisory.go`
- Add: `internal/core/workspace/advisory_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/workspace/service.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`

**Interfaces:**

- Start accepts exactly one address form: absolute `cwd`, or `workspace_id` plus a relative `cwd` defaulting to `.`.
- Workspace-relative resolution rejects NUL, `..`, and symlink traversal outside the registered root.
- `workspace_hint` is a response-only soft expectation and never part of operation identity.
- Context events and advisories return in the original start/poll response; no mandatory extra inspect call is introduced.
- Retry lookup occurs before current workspace resolution and replays the first bound absolute cwd.

- [x] **Step 1: Write failing address and retry tests**

Cover valid nested cwd, omitted relative cwd, unknown workspace, absolute cwd plus workspace conflict, `..`, NUL, escaping symlink, internal symlink, moved worktree before first admission, and moved/removed worktree after first admission.

Critical retry fixture:

1. admit operation with logical `workspace_id` + relative cwd;
2. persist its resolved cwd and execution binding;
3. move the worktree;
4. retry the same operation/request fingerprint;
5. return/replay the original durable operation without re-resolving or spawning.

- [x] **Step 2: Write advisory behavior tests**

Matching, mismatching, changed, and omitted hints all execute. Branch switch, stash, reset, rebase, dirty changes, and concurrent activity are neutral context or compact advisories. Repeated same-cause advisories are deduplicated; a changed cause fingerprint may re-emit.

- [x] **Step 3: Run RED**

```bash
go test ./internal/core/workspace ./internal/core/operation ./internal/app/workspace ./internal/app/daemon ./internal/adapter/ipc ./internal/adapter/mcp -run 'TestAddress|TestRetryAfterMove|TestWorkspaceHint|TestAdvisory' -count=1
```

- [x] **Step 4: Implement lookup-before-resolution admission**

Admission order is normative:

1. validate bounded request syntax;
2. look up `operation_id`;
3. if present, compare the caller-stable request fingerprint and replay/conflict without workspace resolution;
4. if absent, resolve the logical address safely;
5. persist logical address, resolved execution binding, and observation binding atomically before spawn.

Advisory observation is fail-open. Its error is recorded safely but never replaces the child/operation result.

- [x] **Step 5: Verify and commit**

```bash
go test ./internal/core/workspace ./internal/core/operation ./internal/app/workspace ./internal/app/daemon ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add api/schema internal/core/workspace internal/core/operation internal/app/workspace internal/app/daemon internal/adapter/ipc internal/adapter/mcp
git commit -m "feat: address commands by reusable workspace"
```

---

### Task 10: Add direct argv execution, declared intent, and three fingerprints

**Files:**

- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/intent_test.go`
- Modify: `internal/core/operation/execution.go`
- Modify: `internal/adapter/process/shell.go`
- Add: `internal/adapter/process/argv.go`
- Add: `internal/adapter/process/argv_test.go`
- Modify: `internal/app/daemon/process_port.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`

**Interfaces:**

Exactly one execution form is accepted:

```json
{"argv":["git","status","--porcelain=v2","-z"]}
```

or:

```json
{"command":"make test-dirty && make build"}
```

Intent is a closed optional object with `kind`, nullable/optional `mutates_source`, and nullable/optional `external_effect`. Omitted means unknown, not false.

- [x] **Step 1: Write failing process and schema tests**

Cover exact argv boundaries containing spaces, quotes, wildcard characters, empty argument, Unicode, and dash-prefixed values; missing/empty argv; command+argv conflict; PTY and pipe modes; cancellation; process groups; environment inheritance; failed executable lookup; and direct-argv receipt fields.

Test intent enum/unknown fields and prove intent never grants authority or changes execution semantics.

- [x] **Step 2: Run RED**

```bash
go test ./internal/core/operation ./internal/adapter/process ./internal/app/daemon ./api/schema -run 'TestArgv|TestExecutionMode|TestIntent|TestFingerprints' -count=1
```

- [x] **Step 3: Implement without shell conversion**

Use `exec.CommandContext`/existing process-group ownership with `argv[0]` and `argv[1:]`. Never join argv into a shell command or parse a shell command into argv. Preserve the existing configured-shell path unchanged.

Compute:

- request fingerprint from caller-stable execution intent and logical address;
- execution fingerprint from first resolved cwd and actual shell/executable binding;
- observation binding fingerprint from activity/project/intent/expected-observation metadata.

Exclude yield, cursor, output selectors, response byte limits, and `workspace_hint`.

- [x] **Step 4: Verify and commit**

```bash
go test ./internal/core/operation ./internal/adapter/process ./internal/app/daemon ./internal/core/receipt ./api/schema -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add api/schema internal/core/operation internal/adapter/process internal/app/daemon internal/core/receipt
git commit -m "feat: support direct argv execution"
```

---

### Task 11: Add multi-account Git identity profiles and advisory preflight

**Files:**

- Add: `internal/core/gitidentity/profile.go`
- Add: `internal/core/gitidentity/profile_test.go`
- Add: `internal/core/gitidentity/snapshot.go`
- Add: `internal/core/gitidentity/snapshot_test.go`
- Add: `internal/app/gitidentity/service.go`
- Add: `internal/app/gitidentity/service_test.go`
- Add: `internal/app/gitidentity/ports.go`
- Add: `internal/adapter/git/identity.go`
- Add: `internal/adapter/git/identity_test.go`
- Modify: `api/schema/config-v1.json`
- Modify: `internal/config/config.go`
- Modify: `internal/config/load.go`
- Modify: `internal/config/config_test.go`
- Modify: `cmd/shellbeam/command_workspace.go`
- Modify: `cmd/shellbeam/command_workspace_test.go`
- Modify: `internal/core/workspace/advisory.go`

**Interfaces:**

```text
shellbeam workspace preflight [<label-or-id>] --effect push|pr|tag|release|publish|verify [--deep] [--json]
```

Profiles hold expectations only: SSH host aliases, remote owner/URL patterns, commit emails, signing-key fingerprints, GitHub host/user. They never contain token values, private keys, or passphrases.

- [x] **Step 1: Write failing profile-resolution tests**

Resolution order is workspace override, repository binding, unique remote-rule match, then unknown. Cover ambiguous matches and per-worktree config. Test commit, transport, and GitHub CLI identities independently.

Fixtures include company remote with personal email, personal SSH alias, wrong stored `gh` account, token override, host/repository/config-dir overrides, author/committer/transport runtime overrides, generic `github.com`, and unknown profile.

- [x] **Step 2: Prove shallow hot-path safety**

With fake runners and an SSH config containing `Match exec`, assert ordinary admission invokes neither `ssh` nor `gh`. It may inspect safe environment-presence classes and cached local Git configuration in the target cwd.

- [x] **Step 3: Run RED**

```bash
go test ./api/schema ./internal/core/gitidentity ./internal/app/gitidentity ./internal/adapter/git ./internal/config ./cmd/shellbeam -run 'TestIdentity|TestProfile|TestPreflight|TestNoSSHOrGHOnAdmission' -count=1
```

- [x] **Step 4: Implement shallow and explicit deep observation**

Shallow preflight is local, bounded, and non-networking. `--deep` may run bounded `ssh -G <alias>` and `gh auth status --active --hostname <host>` because the caller explicitly requested it. Record safe classifications/fingerprints only.

When a token override is present, report the stored user separately and effective user as unknown unless independently proven. Never call `gh auth switch`. A completed mismatch report exits successfully; observation/invocation failure is a separate typed failure.

Cause-deduplicate mismatch advisories so ordinary file edits do not repeat account warnings.

- [x] **Step 5: Add redaction and timeout tests**

Seed fake tokens, private-key paths, emails, and hostile command output. Assert no token/private-key contents reach logs, receipts, JSON responses, or advisories. Deep probe timeout degrades to an advisory and never affects unrelated execution.

- [x] **Step 6: Verify and commit**

```bash
go test ./api/schema ./internal/core/gitidentity ./internal/app/gitidentity ./internal/adapter/git ./internal/config ./cmd/shellbeam ./internal/observability -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add api/schema/config-v1.json internal/core/gitidentity internal/app/gitidentity internal/adapter/git internal/config cmd/shellbeam internal/core/workspace
git commit -m "feat: advise on repository git identity"
```

---

### Task 12: Parse and inspect a strict project capability manifest

**Files:**

- Add: `api/schema/project-manifest-v1.json`
- Modify: `api/schema/embed.go`
- Modify: `api/schema/embed_test.go`
- Add: `internal/core/project/manifest.go`
- Add: `internal/core/project/manifest_test.go`
- Add: `internal/core/project/status.go`
- Add: `internal/core/project/status_test.go`
- Add: `internal/app/project/service.go`
- Add: `internal/app/project/service_test.go`
- Add: `internal/app/project/ports.go`
- Add: `internal/adapter/project/loader.go`
- Add: `internal/adapter/project/loader_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/server.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`

**Interfaces:**

- Discover only `<workspace-root>/.shellbeam/project.toml` in v1; no upward search outside the registered workspace.
- Maximum file size is 64 KiB before parsing.
- Two-stage strict decode rejects unknown fields, duplicate command/profile IDs, invalid relative paths, cycles/invalid references, and commands outside bounded limits.
- `inspect project` returns `absent`, `valid`, `invalid`, or `review_due`, plus schema version, discovery/review fingerprints, confidence/provenance, and declared capabilities.
- Inspection/validation never executes manifest commands.

- [ ] **Step 1: Write failing parser/status tests**

Cover absent file, minimal valid file, complete sample, unknown table/key, oversized file, invalid UTF-8, absolute/escaping path, duplicate IDs, bad profile reference, unsupported schema version, stable canonical fingerprint, review-fingerprint drift, and parser fuzzing.

- [ ] **Step 2: Prove non-execution**

Use a manifest declaring a command that would create a sentinel file. Call loader, validator, status, IPC inspect, and MCP inspect; assert the sentinel never exists and the process runner port is never invoked.

- [ ] **Step 3: Run RED**

```bash
go test ./api/schema ./internal/core/project ./internal/app/project ./internal/adapter/project ./internal/adapter/ipc ./internal/adapter/mcp -run 'TestManifest|TestProjectStatus|TestInspectionDoesNotExecute' -count=1
```

- [ ] **Step 4: Implement deterministic inspection**

Use the already pinned TOML v2 dependency. Decode syntax into a raw closed representation, then validate semantic references into immutable core facts. Normalize repository-relative paths without touching their targets. Never interpolate environment variables or shell syntax during inspection.

Return command IDs and exact argv/shell declarations as data. Actual execution remains the ordinary `start` path and requires the caller to choose a command.

- [ ] **Step 5: Verify and commit**

```bash
go test ./api/schema ./internal/core/project ./internal/app/project ./internal/adapter/project ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add api/schema internal/core/project internal/app/project internal/adapter/project internal/adapter/ipc internal/adapter/mcp
git commit -m "feat: inspect project capability manifests"
```

---

### Task 13: Add review provenance and the agent onboarding workflow

**Files:**

- Add: `internal/core/project/review.go`
- Add: `internal/core/project/review_test.go`
- Modify: `internal/app/project/service.go`
- Modify: `internal/app/project/service_test.go`
- Modify: `internal/adapter/store/repository.go`
- Add: `internal/adapter/store/project_reviews.go`
- Add: `internal/adapter/store/project_reviews_test.go`
- Add: `cmd/shellbeam/command_project.go`
- Add: `cmd/shellbeam/command_project_test.go`
- Modify: `cmd/shellbeam/main.go`
- Modify: `internal/adapter/mcp/server.go`
- Modify: `docs/testing/tunnel-e2e.md`

**Interfaces:**

```text
shellbeam project inspect [--workspace <label-or-id>] [--json]
shellbeam project validate [--workspace <label-or-id>] [--json]
shellbeam project review [--workspace <label-or-id>] --fingerprint <fingerprint> [--json]
```

Onboarding is an agent workflow, not hidden daemon automation:

1. inspect server capabilities;
2. inspect/attach the workspace;
3. inspect project manifest status;
4. if absent, review bounded repository evidence to decide whether a manifest would add value;
5. draft `.shellbeam/project.toml` only when useful;
6. show the diff and obtain normal user approval before writing shared repository policy;
7. validate it;
8. explicitly record review provenance for the exact fingerprint.

- [ ] **Step 1: Write failing review-state tests**

Cover exact fingerprint review, changed manifest, changed discovery inputs, unsupported version, missing reviewer metadata, review of invalid/absent content, and concurrent atomic review writes. `review_due` is informational and never prevents command execution.

- [ ] **Step 2: Write failing onboarding-instruction tests**

Assert MCP server instructions tell capable agents to inspect manifest status during repository onboarding, assess whether a manifest is warranted, never auto-trust discovered commands, request approval before adding/editing the shared file, and avoid repeated prompts while the reviewed fingerprint is current.

Do not encode language/ecosystem guesses in the daemon. The reasoning agent examines existing `AGENTS.md`, README, build files, CI, scripts, and package metadata using bounded reads.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/project ./internal/app/project ./internal/adapter/store ./cmd/shellbeam ./internal/adapter/mcp -run 'TestReview|TestOnboarding|TestProjectCommand' -count=1
```

- [ ] **Step 4: Implement explicit review provenance**

Persist manifest fingerprint, discovery fingerprint, schema version, reviewed timestamp, tool version, and non-secret reviewer/source class. Do not store conversation content. A review record never changes the manifest and cannot make an invalid manifest valid.

CLI validate exits nonzero for invocation/validation failure; inspect reports status as data. Review requires the caller-provided current fingerprint to prevent acknowledging a file that changed between inspection and review.

- [ ] **Step 5: Document a manual onboarding acceptance flow**

Extend `docs/testing/tunnel-e2e.md` with a bounded scenario for absent -> assessed -> proposed -> approved/written -> valid -> reviewed -> review_due after source change. Include a negative case proving no file is created merely by inspect.

- [ ] **Step 6: Verify and commit**

```bash
go test ./internal/core/project ./internal/app/project ./internal/adapter/store ./cmd/shellbeam ./internal/adapter/mcp -count=1
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
git diff --check
git add internal/core/project internal/app/project internal/adapter/store cmd/shellbeam internal/adapter/mcp docs/testing/tunnel-e2e.md
git commit -m "feat: guide project capability onboarding"
```

---

### Task 14: Complete inspect surfaces and the A1 acceptance checkpoint

**Files:**

- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/server.go`
- Modify: `internal/adapter/mcp/server_test.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `cmd/shellbeam/doctor.go`
- Modify: `cmd/shellbeam/daemon_test.go`
- Modify: `docs/testing/tunnel-e2e.md`

**Interfaces:**

The one-tool `inspect` family now supports bounded reads for `server`, `workspace`, `activity`, and `project`. It does not yet expose A2 evidence/output selectors or B1 persistent-session management.

- [ ] **Step 1: Write end-to-end contract tests**

Through a real Unix IPC server and in-memory MCP client, verify:

- current and legacy discovery;
- inspect branches never spawn;
- attach/reuse one worktree across multiple activity IDs;
- one activity crosses two registered workspaces;
- branch switch, stash, reset, and rebase continue to execute and return honest context;
- same operation retry after worktree move never respawns;
- direct argv preserves boundaries;
- company/personal identity mismatch is advisory and cause-deduplicated;
- manifest inspect/validate never executes declared commands;
- child exit failure remains distinct from tool failure;
- observation/cache/manifest corruption degrades without disabling basic v1 execution.

- [ ] **Step 2: Run focused acceptance tests**

```bash
go test ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./internal/app/daemon ./cmd/shellbeam ./tests/contract -run 'TestAgentExecutionA1|TestCompatibility|TestWorkspace|TestProject' -count=1
```

- [ ] **Step 3: Run architecture and affected verification**

```bash
while IFS= read -r -d '' file; do
  gofmt -w "$file"
done < <(git diff --name-only -z "$SHELLBEAM_A01_BASE"...HEAD -- '*.go')
go run ./tools/devctl check --base "$SHELLBEAM_A01_BASE" --json
go run ./tools/devctl test --dirty --base "$SHELLBEAM_A01_BASE" --json
go vet ./cmd/shellbeam ./internal/... ./api/schema ./tools/devctl
git diff --check
```

- [ ] **Step 4: Run one deliberate full checkpoint**

Only after all focused/dirty checks pass, run the full suite once for the integration boundary:

```bash
go test ./... -count=1
go vet ./...
go build -o .build/checkpoints/a1/shellbeam ./cmd/shellbeam
.build/checkpoints/a1/shellbeam doctor --json
```

This is the checkpoint exception to the no-full-suite edit-loop rule. Record source fingerprint, exact commands, elapsed time, selected packages, Go cache state, binary digest, and outcomes in the existing devctl evidence format.

- [ ] **Step 5: Run the manual tunnel acceptance flow**

Use the existing isolated `shellbeam` tunnel profile. Exercise capability discovery, workspace/activity/project inspect, direct argv, nonzero exit, retry, advisory behavior, and the manifest no-auto-write case. Inspect durable state/receipts independently; do not trust the model's self-report alone.

- [ ] **Step 6: Review security/privacy boundaries**

```bash
rg -n 'GH_TOKEN|GITHUB_TOKEN|PRIVATE KEY|IdentityFile|passphrase' .build docs internal cmd api --glob '!**/*_test.go'
go test ./internal/observability ./internal/core/gitidentity ./internal/adapter/git ./internal/adapter/project -run 'Test.*Redact|Test.*NoSecret|TestInspectionDoesNotExecute' -count=1
```

Expected: no fixture secret in generated evidence or responses; only intentional documentation/config key names appear in source search.

- [ ] **Step 7: Commit the A1 checkpoint**

```bash
git add api/schema internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/app/daemon cmd/shellbeam docs/testing/tunnel-e2e.md
git diff --cached --check
git commit -m "test: verify agent execution layer a1"
```

Do not push or open a PR unless the user explicitly requests it.

---

## Independent Self-Review Before Handoff

Review the implementation against these questions, with code/test evidence for every “yes”:

| Area | Required evidence |
| --- | --- |
| Compatibility | Existing v1 schemas, real payloads, persisted records, CLI, and MCP flow still pass |
| Exactly-once | Retry lookup precedes mutable workspace resolution; conflicts never spawn |
| Fingerprints | Request, execution, and observation bindings are separate and response controls are excluded |
| Workspace UX | Reuse is unrestricted; hints and names are soft; ordinary Git commands are never blocked |
| Destructive UX | `forget` is metadata-only; remove is explicit, bounded, and never auto-prunes |
| Multi-account safety | Commit, transport, and GitHub identities are evaluated independently in target cwd |
| Hot-path budget | Warm context uses zero subprocesses; cold context stays within local Git budget; no SSH/GitHub/network probes |
| Manifest trust | Inspection is strict and non-executing; shared-file writes require normal user approval; review is fingerprint-bound |
| Architecture | Ports live with consumers; adapters do not import siblings; composition stays in `cmd/shellbeam` |
| Performance | Dirty selection is real; builds reuse Go cache; full suite occurs only at checkpoint |
| Privacy | Tokens/private keys/raw secret-bearing diagnostics never enter receipts, logs, advisories, or release evidence |
| Claims | Cached/stale/unavailable/truncated facts are labeled and never promoted to exact |

If any row lacks evidence, keep the relevant task open rather than weakening the claim.

## Follow-On Plan Boundaries

Create separate executable plans only after A1 is stable:

1. **A2 output and evidence:** deterministic rendered output, bounded raw/tail/line/search selectors, expected outputs/artifacts, evidence ledger and freshness, exact environment/source snapshots, mechanical change summaries, mutation-scope advisories, richer process inspection.
2. **B1 persistent runtime:** explicit named sessions, attach/detach, daemon-restart recovery, leases/retention, long-running service ergonomics.
3. **B2 semantic context provider:** benchmark Semble and alternatives behind an optional provider interface; key indexes by repository namespace + source-content digest + provider/config version; keep indexing outside the core daemon and degrade cleanly when absent.

Do not pull B1/B2 features into A0/A1 merely because adjacent types exist. Their performance, privacy, and lifecycle contracts need separate evidence.

## Completion Gate

A0/A1 is complete only when:

- one MCP tool remains, current clients discover capabilities without invalid trial calls, and legacy clients still execute;
- typed result/failure envelopes preserve child outcome and V1 exactly-once guarantees;
- worktrees are reusable first-class workspaces with non-blocking context and pragmatic lifecycle commands;
- retry remains correct after workspace movement;
- direct argv and shell execution are both explicit and exact;
- activity dirty baselines report observation, never ownership/causation;
- account mismatches are visible before declared external effects without running silent `ssh`/`gh` probes;
- agents are instructed to assess and, with approval, create a strict project manifest when onboarding warrants it;
- affected tests/builds are source-bound and incremental;
- focused, dirty, architecture, security, one deliberate full checkpoint, and tunnel acceptance evidence all pass for the same source fingerprint.

At handoff, report exact branch/worktree, commits, source fingerprint, checks executed, skipped checks with reason, residual A2/B risks, and whether anything was pushed or installed.
