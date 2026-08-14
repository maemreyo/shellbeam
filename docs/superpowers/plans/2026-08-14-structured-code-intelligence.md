# ShellBeam Structured Code Intelligence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add E29 model-oriented semantic diagnostics/navigation through the existing `local_shell` surface, using bounded SourceRefs and a lazy persistent gopls/LSP provider without turning ShellBeam into an IDE, editor, or semantic index engine.

**Architecture:** Build provider-neutral `codeintel` core/app contracts on top of the completed Lazy Workspace Freshness plan. `inspect.code` obtains model-facing activity/workspace selection from `WorkspaceDeltaSampler`, binds exact saved-file `SourceRef`s, lets an adapter-owned provider synchronize a possibly wider semantic scope, then independently checks selection completeness and per-record source correlation. The first provider is gopls over stdio LSP; provider lifecycle/sync state is bounded and opaque, and ordinary shell execution never starts or queries it.

**Tech Stack:** Go 1.26.5; `go.lsp.dev/protocol v1.0.1`; `go.lsp.dev/jsonrpc2 v1.0.1`; gopls over stdio LSP; existing ULID dependency for opaque local SourceRef IDs; existing ShellBeam IPC/MCP v2 closed schemas; no gopls CLI production dependency.

## Global Constraints

- Prerequisite: `docs/superpowers/plans/2026-08-14-lazy-workspace-freshness.md` completion gate is green on the execution branch. Do not implement E29 against the old eager workspace-observation path.
- Approved E29 design authority: `docs/superpowers/specs/2026-08-14-structured-code-intelligence-design.md` plus the shared observation/location contract in `docs/superpowers/specs/2026-08-14-agent-execution-observation-roadmap-design.md` at or after `07d1664796d9b65136fcae5dd3150258c6a89b5c`.
- Work only in the current isolated ShellBeam worktree/branch. Preserve unrelated dirty work; no reset/stash/clean/rebase/primary-checkout mutation. Use one primary agent unless the user explicitly authorizes subagents.
- TDD is mandatory. Each task starts with a focused failing test, implements the smallest coherent slice, then runs focused/dirty gates before commit.
- Keep one MCP tool, `local_shell`. E29 adds an `inspect.code` action/query branch to the existing closed transport; do not add a second MCP tool.
- ShellBeam remains query-only for E29. Do not expose/apply LSP code actions, rename/refactor edits, AST mutation, patch APIs, arbitrary ast-grep rule/rewrite execution, or code generation.
- E29 v1 is disk-bound. Unsaved editor overlays are out of scope. Only valid UTF-8 saved source can become a canonical `ResolvedSourceLocation`.
- `source_ref_id` is a server-issued opaque identity handle, not authorization. It is state-root/source-view scoped, immutable once issued, may expire, must never be rebound, and expired IDs must never be recycled for another source.
- Never add a public deterministic per-file source hash solely for location identity. Existing predecessor namespace-level source digests keep their separate evidence semantics.
- `SourceLocation` is a closed union. `ResolvedSourceLocation` has an exact SourceRef + zero-based half-open UTF-8 byte range. `ProviderReportedLocation` is a distinct lower-authority branch with no invented canonical byte range. Canonical-position operations reject it with `location_not_resolved`.
- Model input coordinates are 1-based lines + 1-based Unicode-scalar columns. The model never converts UTF-8/UTF-16/provider positions.
- Workspace selection, provider synchronization scope, exact returned-SourceRef correlation, and exact build/test/release evidence are separate authority dimensions. Do not collapse them into one `freshness` boolean.
- `ProviderSyncState` is adapter-owned, opaque, bounded, and compatible only within provider incarnation/executable/config/build identity + declared coverage. Core must not standardize `map[path]LastSourceRef` or maintain a whole-repository source mirror.
- Provider restart/config/build incompatibility discards the old sync token. Budget overflow downgrades completeness/status; it never returns empty-as-complete.
- gopls provider uses LSP. Do not use the gopls CLI as the production adapter. Keep gopls server-side file watching off; ShellBeam drives bounded synchronization from its explicit sampling/source contracts.
- ast-grep structural support and SCIP consumption are separate optional follow-up plans. Direct Tree-sitter integration is deferred and is not an E29 v1 fallback.
- Keep `cmd/shellbeam` the sole composition root; app owns ports/use-cases, adapters own process/LSP/OS effects, core owns pure contracts. No catch-all packages.
- Production files target 150–300 lines, require review >350, hard cap 500; test files review >600, hard cap 800; functions review >60, hard cap 80; interfaces normally 1–5 methods, hard cap 8.
- Every query is bounded by wall time, selected files/source bytes, response bytes, record count, concurrent requests, provider instances, source-ref retention, and restart/cooldown policy.
- Ordinary `local_shell start` with E29 supported-but-unused performs zero language-server/indexing/query work and adds no provider subprocess.
- Use focused tests first, then `go run ./tools/devctl test --dirty --base origin/main`, `go run ./tools/devctl check`, and targeted native/race gates. Do not treat `[no tests to run]` as proof of required behavior.
- Commit only task scope, inspect staged names/stat/check, and never use `--no-verify`.

---

## File Structure

### Core

- Create `internal/core/codeintel/source.go` — SourceRef/origin/resolution quality and closed SourceLocation union.
- Create `internal/core/codeintel/position.go` — byte/display coordinate validation helpers independent of LSP.
- Create `internal/core/codeintel/query.go` — closed provider-neutral query vocabulary.
- Create `internal/core/codeintel/result.go` — bounded result/selection/provider status and diagnostic/navigation records.
- Create `internal/core/codeintel/provider.go` — common provider compatibility/coverage metadata only; no sync implementation map.

### Application

- Create `internal/app/codeintel/ports.go` — consumer-owned workspace/activity/source/provider ports.
- Create `internal/app/codeintel/source_store.go` — bounded in-memory opaque SourceRef store with expiry tombstones.
- Create `internal/app/codeintel/provider_manager.go` — lazy provider lifecycle, capacity, compatibility, cooldown, opportunistic idle eviction.
- Create `internal/app/codeintel/service.go` — inspect orchestration and two-barrier authority handling.
- Keep these files focused; split query normalization/provider result normalization if any file approaches 350 lines.

### Adapters

- Create `internal/adapter/codeintel/sourcefs/binder.go` — no-follow saved-file reader and UTF-8 path/source binding.
- Create `internal/adapter/codeintel/lsp/transport.go` — subprocess stdio + LSP-framed JSON-RPC lifetime.
- Create `internal/adapter/codeintel/lsp/client.go` — LSP client notifications/config handling, bounded diagnostic buffer, position encoding.
- Create `internal/adapter/codeintel/gopls/provider.go` — Go-specific synchronization/query/semantic-context rules.
- Create focused tests beneath the same adapter family. `internal/adapter/codeintel/*` subpackages may cooperate without violating the sibling-adapter import rule because they remain under the `codeintel` adapter family.

### Public transport/composition

- Modify `internal/core/capability/catalog.go` — add `code_intelligence` surface availability.
- Modify `internal/adapter/ipc/protocol_v2.go`, IPC client/server tests — add `inspect.code`.
- Modify `internal/app/bridge/client_port.go` — bridge method for code inspect.
- Modify `internal/adapter/mcp/input.go`, `call.go`, schemas/tests — expose the closed branch through existing `local_shell`.
- Modify `api/schema/ipc-v2.json`, `mcp-input-v2.json`, `mcp-output-v2.json` — closed query/result schemas.
- Modify `cmd/shellbeam/command_daemon.go` — create SourceRef store, provider manager/factory, and codeintel service; wire it into `daemonActions`.

---

### Task 1: Freeze the provider-neutral E29 core contract

**Files:**
- Create: `internal/core/codeintel/source.go`
- Create: `internal/core/codeintel/source_test.go`
- Create: `internal/core/codeintel/position.go`
- Create: `internal/core/codeintel/position_test.go`
- Create: `internal/core/codeintel/query.go`
- Create: `internal/core/codeintel/query_test.go`
- Create: `internal/core/codeintel/result.go`
- Create: `internal/core/codeintel/result_test.go`
- Create: `internal/core/codeintel/provider.go`
- Create: `internal/core/codeintel/provider_test.go`

**Interfaces:**
- Produces all public/core E29 types used by later tasks.
- No dependency on LSP, Git adapters, gopls, or filesystem APIs.

- [ ] **Step 1: Write RED SourceRef/SourceLocation tests**

Freeze these shapes:

```go
type SourceRefID string

type SourceOrigin string
const (
    SourceRepository SourceOrigin = "repository"
    SourceWorkspace  SourceOrigin = "workspace"
    SourceDependency SourceOrigin = "dependency"
    SourceToolchain  SourceOrigin = "toolchain"
    SourceGenerated  SourceOrigin = "generated"
    SourceExternal   SourceOrigin = "external"
)

type SourceRef struct {
    ID                SourceRefID              `json:"source_ref_id"`
    Origin            SourceOrigin             `json:"origin"`
    RepositoryID      workspace.RepositoryID   `json:"repository_id,omitempty"`
    WorkspaceID       workspace.WorkspaceID    `json:"workspace_id,omitempty"`
    LogicalPath       string                   `json:"logical_path,omitempty"`
    DisplayIdentity   string                   `json:"display_identity,omitempty"`
    ResolutionQuality ResolutionQuality        `json:"resolution_quality"`
    TextEncoding      string                   `json:"text_encoding"`
}

type ByteRange struct { Start, End int64 }

type SourceLocation struct {
    Kind             LocationKind                 `json:"kind"`
    Resolved         *ResolvedSourceLocation      `json:"resolved,omitempty"`
    ProviderReported *ProviderReportedLocation    `json:"provider_reported,omitempty"`
}
```

`Validate()` must enforce exactly one union member, `utf-8` for resolved E29 v1 sources, non-negative half-open byte ranges, safe bounded logical/display identity, and no canonical byte range on provider-reported locations.

Run:

```bash
go test ./internal/core/codeintel -run 'Source|Location' -count=1
```

Expected: FAIL because package/types do not exist.

- [ ] **Step 2: Implement SourceRef/location validators**

Do not add raw content hashes to `SourceRef`. `SourceRefID` syntax must be bounded and opaque, e.g. `src_<ULID>`; parsing validates form only and never derives source content.

- [ ] **Step 3: Write RED coordinate tests**

Use UTF-8 fixtures covering ASCII, Vietnamese, emoji, combining marks, CRLF, and end-of-file. Freeze model addressing:

```go
func DisplayPositionToByteOffset(src []byte, line, column int) (int64, error)
func ByteOffsetToDisplayPosition(src []byte, offset int64) (line, column int, err error)
```

Line/column are 1-based; column counts Unicode scalar values, not bytes, UTF-16 code units, or grapheme clusters. Reject invalid UTF-8.

- [ ] **Step 4: Implement query/result/provider metadata types**

Promoted query kinds:

```go
diagnostics
symbols
definition
references
import_declarations
resolved_import_targets
type_definition
type_summary
callers
callees
```

Scopes:

```go
file
changed_files
workspace
```

A query contains only model concepts:

```go
type Query struct {
    Kind     QueryKind `json:"kind"`
    Scope    Scope     `json:"scope,omitempty"`
    Path     string    `json:"path,omitempty"`
    Line     int       `json:"line,omitempty"`
    Column   int       `json:"column,omitempty"`
    Provider string    `json:"provider,omitempty"`
}
```

Result metadata must keep dimensions separate:

```go
type SelectionMetadata struct {
    Basis        workspace.SelectionBasis        `json:"selection_basis,omitempty"`
    Freshness    workspace.SampleFreshness       `json:"sample_freshness,omitempty"`
    Completeness workspace.SelectionCompleteness `json:"selection_completeness,omitempty"`
    Fallback     string                          `json:"fallback_available,omitempty"`
    ManagedOverlap bool                          `json:"managed_overlap,omitempty"`
}

type ProviderMetadata struct {
    ProviderID              string       `json:"provider_id"`
    Incarnation             string       `json:"provider_incarnation"`
    ExecutableVersion       string       `json:"provider_version,omitempty"`
    ConfigFingerprint       string       `json:"config_fingerprint,omitempty"`
    BuildFingerprint        string       `json:"build_fingerprint,omitempty"`
    BuildQuality            string       `json:"build_quality,omitempty"`
    Coverage                SyncCoverage `json:"sync_coverage"`
    SemanticScopeQuality    string       `json:"semantic_scope_quality,omitempty"`
}
```

No adapter-private sync token appears in core/public JSON.

- [ ] **Step 5: Add bounded record validation**

Define diagnostic, symbol, location-target, import, and type-summary records with maximum field lengths. `Result.Validate(limits)` rejects excess records/bytes, unknown query/status branches, and a result payload incompatible with the query kind. `mechanical` metadata must not imply exhaustive callers/callees.

Run:

```bash
go test ./internal/core/codeintel -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/codeintel/source.go internal/core/codeintel/source_test.go \
  internal/core/codeintel/position.go internal/core/codeintel/position_test.go \
  internal/core/codeintel/query.go internal/core/codeintel/query_test.go \
  internal/core/codeintel/result.go internal/core/codeintel/result_test.go \
  internal/core/codeintel/provider.go internal/core/codeintel/provider_test.go
git diff --cached --check
git commit -m "feat: define structured code intelligence contracts"
```

---

### Task 2: Build bounded SourceRef retention and exact saved-file binding

**Files:**
- Create: `internal/app/codeintel/source_store.go`
- Create: `internal/app/codeintel/source_store_test.go`
- Create: `internal/app/codeintel/ports.go`
- Create: `internal/adapter/codeintel/sourcefs/binder.go`
- Create: `internal/adapter/codeintel/sourcefs/binder_test.go`

**Interfaces:**
- Consumes: core SourceRef/location/position contract.
- Produces:

```go
type BoundSource struct {
    Ref   core.SourceRef
    Bytes []byte // private application value; never serialized by default
}

type SourceBinder interface {
    Bind(context.Context, workspace.Workspace, string) (BoundSource, error)
    Resolve(core.SourceRefID) (BoundSource, SourceRefState)
}
```

- [ ] **Step 1: Write RED retention tests**

Construct a store with deterministic clock/ID generator and prove:

- first bind returns opaque `src_...` ID;
- same ID never rebinds to different bytes;
- retention expiry returns `source_ref_expired` while tombstone retained;
- purged/unknown ID returns `source_ref_unavailable`;
- expired ID is never recycled;
- entry count, retained bytes, and tombstones are bounded;
- eviction cannot mutate a still-returned SourceRef value.

- [ ] **Step 2: Implement the in-memory store**

Use existing `github.com/oklog/ulid/v2` for IDs. Keep a mutex, bounded entry/order structures, byte accounting, expiry timestamps, and bounded expired tombstones. No persistence is required for E29 v1; daemon restart invalidates old refs and old IDs are never issued again by that incarnation’s generator.

- [ ] **Step 3: Write RED sourcefs tests**

Temp-workspace cases:

- valid UTF-8 regular file;
- Vietnamese/emoji coordinates;
- file too large for source-byte envelope;
- `..`/absolute path rejection;
- NUL/control path rejection;
- symlink inside repo does not silently follow an external target;
- FIFO/device/special file rejected;
- source replaced between Lstat/open/read verification yields retryable source-changed/unavailable rather than a falsely exact ref.

- [ ] **Step 4: Implement saved-file binding**

The sourcefs adapter resolves repository-relative paths under the bound workspace, uses `Lstat`/no-follow policy for v1, opens regular files only, reads within the configured byte cap, validates UTF-8, and hands exact bytes to the SourceRef store.

Do not expose host absolute paths in `SourceRef.DisplayIdentity`. Repository/workspace source uses safe logical relative paths. External/dependency locations returned only by a provider remain `ProviderReportedLocation` unless a future explicit safe resolver obtains exact bytes.

- [ ] **Step 5: Run focused/race tests**

```bash
go test -race ./internal/app/codeintel ./internal/adapter/codeintel/sourcefs ./internal/core/codeintel -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/codeintel/ports.go internal/app/codeintel/source_store.go internal/app/codeintel/source_store_test.go \
  internal/adapter/codeintel/sourcefs
git diff --cached --check
git commit -m "feat: bind immutable source references"
```

---

### Task 3: Add `inspect.code` to the existing IPC/MCP surface

**Files:**
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/a1_inspect_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/a1_inspect_test.go`

**Interfaces:**
- Produces public action `inspect.code` with required `workspace_id`, optional `activity_id`, and required closed `code_query`.
- The one MCP tool remains `local_shell`.

- [ ] **Step 1: Write RED closed-schema tests**

Example accepted input:

```json
{
  "action": "inspect.code",
  "workspace_id": "ws_01K00000000000000000000000",
  "activity_id": "ZMR-111-validator",
  "code_query": {
    "kind": "diagnostics",
    "scope": "changed_files"
  }
}
```

Reject:

- missing workspace ID/query;
- both unknown query fields and raw LSP fields such as `uri`, `document_version`, `jsonrpc_id`;
- file-position queries missing path/line/column;
- non-position query carrying position fields when disallowed;
- unknown provider/query kind/scope.

Run:

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp -run 'Code|Inspect' -count=1
```

Expected: FAIL.

- [ ] **Step 2: Extend the Go IPC contract**

Add to `RequestV2`:

```go
CodeQuery *codeintel.Query `json:"code_query,omitempty"`
```

and to `ResponseV2`:

```go
Code *codeintel.Result `json:"code,omitempty"`
```

`actionFieldsV2("inspect.code")` permits only `workspace_id`, `activity_id`, and `code_query`. `validateRequestV2` validates all three. Add the action to supported-v2 actions, not deferred actions.

- [ ] **Step 3: Extend daemon action and bridge envelopes without colliding with error `Code`**

Add an IPC-side optional action interface, parallel to `ProjectActions`/`WorkspaceActions`/`ActivityActions`:

```go
type CodeActions interface {
    InspectCode(context.Context, string, string, codeintel.Query) (codeintel.Result, error)
}
```

`daemonActions` implements that method by calling the code-intelligence service. Keep `bridge.DaemonClient` as the existing generic `Forward` port; extend only its request/response envelopes:

```go
type Request struct {
    // existing fields...
    CodeQuery *codeintel.Query
}

type Response struct {
    // existing fields...
    CodeResult *codeintel.Result
    Code       string // existing public error code; DO NOT rename/reuse
}
```

`requestV2FromBridge` maps `inspect.code` workspace/activity/query fields. `forwardV2` maps `ResponseV2.Code` into `bridge.Response.CodeResult` while preserving `bridge.Response.Code` exclusively for errors. No provider protocol details leak into bridge types beyond `codeintel.Query/Result`.

- [ ] **Step 4: Extend MCP input/call mapping and checked-in schemas**

The MCP adapter maps `action=inspect.code` to the same IPC action. Keep output bounded and closed. Add `FeatureCodeIntelligence` to capability catalog; feature availability means the ShellBeam query surface is compiled/wired, not that a language provider is currently installed. Provider readiness lives inside E29 result/status.

- [ ] **Step 5: Run transport/contract tests**

```bash
go test ./api/schema ./internal/core/capability ./internal/adapter/ipc ./internal/app/bridge ./internal/adapter/mcp -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/core/capability/catalog.go internal/core/capability/catalog_test.go \
  internal/adapter/ipc/protocol_v2.go internal/adapter/ipc/protocol_v2_test.go \
  internal/adapter/ipc/server_unix.go internal/adapter/ipc/client_unix.go \
  internal/app/bridge/client_port.go internal/adapter/mcp/input.go internal/adapter/mcp/call.go \
  internal/adapter/mcp/a1_inspect_test.go api/schema/ipc-v2.json api/schema/mcp-input-v2.json \
  api/schema/mcp-output-v2.json api/schema/a1_inspect_test.go
git diff --cached --check
git commit -m "feat: expose code intelligence inspection"
```

---

### Task 4: Implement E29 service selection and the two authority barriers

**Files:**
- Create: `internal/app/codeintel/service.go`
- Create: `internal/app/codeintel/service_test.go`
- Extend: `internal/app/codeintel/ports.go`

**Interfaces:**
- Consumes Lazy plan outputs:

```go
type WorkspaceLookup interface {
    Inspect(context.Context, string) (workspace.Workspace, error)
}

type WorkspaceSampler interface {
    Sample(context.Context, workspace.WorkspaceID, workspace.DeltaLimits) workspace.DeltaSample
}

type ActivitySelector interface {
    CompareWorkspace(context.Context, string, workspace.DeltaSample) (activity.Comparison, error)
}

type CoherenceSource interface {
    CaptureBarrier() workspace.CoherenceBarrier
}
```

- Consumes `SourceBinder` from Task 2 and `ProviderPool` from Task 5.
- Produces:

```go
type InspectRequest struct {
    WorkspaceID string
    ActivityID  string
    Query       core.Query
}
func (s *Service) Inspect(context.Context, InspectRequest) (core.Result, error)
```

- [ ] **Step 1: Write RED changed-files selection tests with fake provider**

Cases:

1. no activity -> model selection is workspace dirty;
2. valid activity baseline -> only `ObservedSinceBaseline` paths are model-facing;
3. inherited dirty `go.mod` is not model-facing activity change but the provider still receives the full workspace sample for hidden synchronization;
4. diverged activity baseline returns best available diagnostics with `SelectionDiverged` and `fallback_available=workspace_dirty`, never silently changes basis;
5. no changed paths may still call provider synchronization with the current sample so dirty->clean/provider state can reconcile;
6. budget overflow returns partial status, not empty+complete.

- [ ] **Step 2: Write RED two-barrier tests**

Use fake coherence/source/provider components:

- selection sample binds `foo.go` SourceRef A;
- provider returns exact diagnostic correlated to A;
- unrelated managed shell changes epoch and creates `bar.go` during query;
- result keeps `foo.go` source correlation exact but sets selection completeness `potentially_stale`.

A second case replaces `foo.go` during query; that record must become `source_changed_during_query`/stale or be retried once within budget. Never relabel it current.

- [ ] **Step 3: Implement selection orchestration**

For `scope=changed_files`:

```text
fresh WorkspaceDeltaSampler sample
-> choose model-facing paths using activity baseline or workspace dirty basis
-> bind exact SourceRefs for selected existing regular files
-> acquire provider
-> give provider full generic sample + selected bound sources + query
-> provider synchronizes its own wider scope and queries
-> capture ending coherence barrier
-> normalize selection completeness independently from records
-> validate each returned record against bound/provider correlation
```

For `scope=file`, bind the explicit path directly; Git changed-file selection may be unavailable and must not block file diagnostics/navigation in a registered non-Git workspace.

- [ ] **Step 4: Normalize provider-reported external locations honestly**

Provider results that target safe repository-bound exact sources may become `ResolvedSourceLocation`. Dependency/toolchain/external URIs for which ShellBeam does not have exact retained bytes become sanitized `ProviderReportedLocation` with origin/provenance and no byte range. Never expose raw home/module-cache absolute paths by default.

- [ ] **Step 5: Enforce result/input budgets before returning**

Bound selected file count, selected source bytes, result records, response bytes, and total service wall time. On overflow return stable partial/budget status and the records already proven within the contract only when completeness is labeled partial.

- [ ] **Step 6: Run focused tests**

```bash
go test -race ./internal/app/codeintel ./internal/core/codeintel -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/app/codeintel/service.go internal/app/codeintel/service_test.go internal/app/codeintel/ports.go
git diff --cached --check
git commit -m "feat: orchestrate bounded code intelligence queries"
```

---

### Task 5: Add bounded provider lifecycle and opaque sync-state compatibility

**Files:**
- Create: `internal/app/codeintel/provider_manager.go`
- Create: `internal/app/codeintel/provider_manager_test.go`
- Extend: `internal/app/codeintel/ports.go`

**Interfaces:**
- Produces provider contract:

```go
type ProviderRequest struct {
    Workspace       workspace.Workspace
    Sample          workspace.DeltaSample
    SelectedSources []BoundSource
    Query           core.Query
}

type ProviderResponse struct {
    Metadata    core.ProviderMetadata
    Diagnostics []ProviderDiagnostic
    Symbols     []ProviderSymbol
    Locations   []ProviderLocation
    TypeSummary string
}

type Provider interface {
    Metadata() core.ProviderMetadata
    Query(context.Context, ProviderRequest) (ProviderResponse, error)
    Close() error
}

type ProviderFactory interface {
    Start(context.Context, workspace.Workspace, ProviderStartOptions) (Provider, error)
}
```

`Provider` owns its private synchronization token internally. The app manager never requires a per-path sync map.

- [ ] **Step 1: Write RED lifecycle/capacity tests**

Prove:

- first explicit request starts one provider lazily;
- ordinary shell has no dependency on/side effect from this manager;
- compatible warm request reuses provider;
- provider incarnation/executable/config/build fingerprint change discards incompatible provider/sync state and starts a new incarnation;
- provider crash marks instance failed and a subsequent request follows bounded restart policy;
- repeated failures enter cooldown rather than infinite restart;
- max provider instances and max in-flight requests return `provider_busy`/evict idle safely;
- active request is never evicted;
- idle eviction is opportunistic on acquire/release or explicit shutdown, not a required always-running background watcher;
- provider metadata declares coverage `exact_for_known_paths|provider_managed|partial|unknown`.

- [ ] **Step 2: Implement key/compatibility model**

Provider key includes workspace + provider ID + executable identity + provider config fingerprint + build/environment fingerprint. A changed key is incompatible; do not attempt clever state migration.

Keep adapter-private sync state inside the provider object. The common manager stores only provider instance metadata, lifecycle timestamps, failure/cooldown counters, and capacity accounting.

- [ ] **Step 3: Implement bounded admission**

Use per-manager/per-provider semaphores and a **zero-or-explicit-small bounded wait queue**. If capacity is exhausted, return immediately or after the configured bounded queue deadline; never create an unbounded goroutine/request backlog.

- [ ] **Step 4: Implement daemon shutdown close path**

Expose:

```go
func (m *ProviderManager) Close() error
```

Composition root/daemon shutdown calls it after code-intelligence request admission stops. Provider shutdown errors are observation/provider errors and never mutate command receipts.

- [ ] **Step 5: Run race tests**

```bash
go test -race ./internal/app/codeintel -run 'Provider|Capacity|Cooldown' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/app/codeintel/provider_manager.go internal/app/codeintel/provider_manager_test.go internal/app/codeintel/ports.go
git diff --cached --check
git commit -m "feat: manage bounded semantic providers"
```

---

### Task 6: Pin LSP libraries and build the generic stdio LSP transport

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/adapter/codeintel/lsp/transport.go`
- Create: `internal/adapter/codeintel/lsp/transport_test.go`
- Create: `internal/adapter/codeintel/lsp/client.go`
- Create: `internal/adapter/codeintel/lsp/client_test.go`
- Create: `internal/adapter/codeintel/lsp/testserver_test.go`

**Interfaces:**
- Consumes app provider adapter types, but contains no gopls-specific semantic widening.
- Produces `lsp.Session` wrapping typed `protocol.Server`, negotiated capabilities/position encoding, notification state, and process/JSON-RPC lifetime.

- [ ] **Step 1: Add exact dependencies and verify module integrity**

```bash
go get go.lsp.dev/protocol@v1.0.1 go.lsp.dev/jsonrpc2@v1.0.1
go mod tidy
go mod verify
```

Expected: exit 0. Inspect `go.mod`/`go.sum`; do not accept an unrelated toolchain downgrade or unexpected dependency replacement.

- [ ] **Step 2: Write an in-memory RED LSP test before spawning gopls**

Use `jsonrpc2.NewChannelStreamPair` to run a tiny typed test server/client. The ShellBeam client embeds `protocol.UnimplementedClient` and overrides only required callbacks such as `PublishDiagnostics`, `Configuration`, workspace-folder/config registration responses, and progress/log handling.

The production connection shape is:

```go
stream := jsonrpc2.NewStream(rwc)
rpcCtx, conn, server := protocol.NewClient(ctx, client, stream)
conn.Go(rpcCtx, protocol.Handlers(protocol.ClientHandler(client, jsonrpc2.MethodNotFoundHandler)))
```

The pinned API returns a connection and typed server dispatcher but the JSON-RPC read loop still needs `Conn.Go`; use the typed `ClientHandler` plus standard LSP middleware and `jsonrpc2.MethodNotFoundHandler` fallback as above. The test must prove notifications and server-initiated client calls are dispatched, not merely outgoing calls.

- [ ] **Step 3: Implement stdio subprocess transport**

Create a process with separate stdin/stdout used as one `io.ReadWriteCloser` adapter for LSP framing; stderr is captured into a bounded diagnostic ring, never mixed into protocol stdout. Requirements:

- no shell command interpolation; executable/argv are explicit;
- provider process gets workspace cwd and an explicit environment snapshot chosen by the gopls factory;
- close cancels RPC, sends LSP `shutdown`/`exit` when healthy within budget, then terminates/reaps according to provider ownership policy;
- JSON-RPC framing uses `jsonrpc2.NewStream`/LSP Content-Length framing;
- connection/process errors are normalized and bounded.

- [ ] **Step 4: Implement negotiated capability state**

Store the server’s chosen position encoding (default UTF-16 when omitted), text-document sync capability, diagnostic/navigation capability presence, server info/version, and workspace-folder support. Do not expose the full LSP capability tree to the model.

- [ ] **Step 5: Implement bounded push-diagnostic buffer**

`PublishDiagnostics` stores only bounded normalized notification data keyed by URI plus LSP document version/correlation metadata. It does **not** assert ShellBeam source authority; the gopls adapter later associates notifications with its exact open-document SourceRefs.

Use condition/channel notification rather than polling/sleep loops; a query can wait within a deadline for a newer correlated diagnostic notification.

- [ ] **Step 6: Run generic LSP tests/race**

```bash
go test -race ./internal/adapter/codeintel/lsp -count=1
go mod verify
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/adapter/codeintel/lsp/transport.go internal/adapter/codeintel/lsp/transport_test.go \
  internal/adapter/codeintel/lsp/client.go internal/adapter/codeintel/lsp/client_test.go \
  internal/adapter/codeintel/lsp/testserver_test.go
git diff --cached --check
git commit -m "feat: add typed lsp provider transport"
```

---

### Task 7: Implement the Go/gopls provider, synchronization, and diagnostics

**Files:**
- Create: `internal/adapter/codeintel/gopls/provider.go`
- Create: `internal/adapter/codeintel/gopls/provider_test.go`
- Create: `internal/adapter/codeintel/gopls/sync.go`
- Create: `internal/adapter/codeintel/gopls/sync_test.go`
- Create: `internal/adapter/codeintel/gopls/diagnostics.go`
- Create: `internal/adapter/codeintel/gopls/diagnostics_test.go`
- Create: `internal/adapter/codeintel/gopls/factory.go`
- Create: `internal/adapter/codeintel/gopls/factory_test.go`

**Interfaces:**
- Implements Task 5 `Provider`/`ProviderFactory`.
- Uses Task 6 `lsp.Session` and Task 2 exact bound sources.

- [ ] **Step 1: Write RED factory/readiness tests**

The built-in provider selection is deterministic:

- explicit provider `go_semantic` selects gopls when executable validation succeeds;
- otherwise a registered Go workspace with one unambiguous available `gopls` uses it as the built-in default;
- missing executable returns provider unavailable without installing anything;
- provider executable identity/version/config/build environment are captured in compatibility metadata;
- no provider is started merely by ordinary shell or server-info capability discovery.

Use `exec.LookPath`/explicit executable validation, not `gopls version` on every query. Version may be observed once at provider startup under the explicit E29 budget.

- [ ] **Step 2: Write RED initialization tests against a fake typed LSP server**

Assert initialize params include only intended client capabilities, disk workspace folder/root, supported position encodings, and **no server-side watcher opt-in**. Validate initialized response capability parsing and one `initialized` notification.

- [ ] **Step 3: Implement bounded document synchronization state inside the adapter**

The adapter-private state may retain only bounded known/open document entries:

```go
type openDocument struct {
    URI       protocol.DocumentURI
    Version   int32
    SourceRef codeintel.SourceRefID
}
```

This map is **private to the gopls adapter**, capped by configured open-doc count/source bytes, and is not a core ProviderSyncState requirement.

Synchronization rules:

- newly selected exact source -> `textDocument/didOpen` with exact UTF-8 text;
- same open path with new SourceRef -> monotonic document version + `didChange` according to negotiated sync kind;
- previously open path now deleted/no longer safely bindable -> `didClose` plus watched-file/delete notification when supported;
- dirty -> clean still sends B -> A because the adapter compares its last synchronized SourceRef/private state to the newly bound current representation, not current dirty-set membership;
- generic delete+add is sufficient for rename; do not require rename inference;
- bounded eviction closes a document before discarding its private sync entry.

- [ ] **Step 4: Add semantic-context invalidation rules owned by the Go adapter**

Recognize Go provider context triggers from generic workspace transitions/provider state, including `go.mod`, `go.work`, provider build flags/environment fingerprint, and configured gopls workspace-file semantics. A trigger may send watched-file notifications/reconfigure or discard/restart provider state when the contract requires it.

Do not add these names/rules to `WorkspaceDeltaSampler`.

Clean branch switch/reset/rebase with unknown broad source transitions may widen/reload provider scope. A HEAD-only commit/stage transition with unchanged source/context does not force reload solely because HEAD changed.

- [ ] **Step 5: Write RED push-diagnostics correlation tests**

Cases:

1. didOpen SourceRef A -> diagnostics version/correlation A -> exact current record;
2. source becomes B before old notification arrives -> old A notification is stale, never current for B;
3. unrelated workspace epoch change -> exact A record stays exact, selection completeness may downgrade separately;
4. diagnostic wait budget expires -> partial/starting status, not fake empty success;
5. gopls sends diagnostics for a non-selected semantic file -> adapter may use them for provider state but model result respects requested scope/record budget.

- [ ] **Step 6: Implement diagnostics query using push notifications**

Do not depend on gopls pull diagnostics for v1. After synchronization, wait within the query deadline for diagnostics correlated to the synchronized document versions/source refs. If provider/workspace-wide diagnostics arrive later, expose readiness/partial status rather than blocking the shell or spinning.

Normalize severity/code/message and provider provenance, but leave location conversion to the shared adapter/service normalization. No source snippets by default.

- [ ] **Step 7: Run provider unit/race tests with fake LSP**

```bash
go test -race ./internal/adapter/codeintel/gopls ./internal/adapter/codeintel/lsp -count=1
```

Expected: PASS without requiring a real installed gopls for unit tests.

- [ ] **Step 8: Commit**

```bash
git add internal/adapter/codeintel/gopls/provider.go internal/adapter/codeintel/gopls/provider_test.go \
  internal/adapter/codeintel/gopls/sync.go internal/adapter/codeintel/gopls/sync_test.go \
  internal/adapter/codeintel/gopls/diagnostics.go internal/adapter/codeintel/gopls/diagnostics_test.go \
  internal/adapter/codeintel/gopls/factory.go internal/adapter/codeintel/gopls/factory_test.go
git diff --cached --check
git commit -m "feat: synchronize go semantics with gopls"
```

---

### Task 8: Implement gopls navigation and provider-neutral location normalization

**Files:**
- Create: `internal/adapter/codeintel/gopls/navigation.go`
- Create: `internal/adapter/codeintel/gopls/navigation_test.go`
- Create: `internal/adapter/codeintel/lsp/position.go`
- Create: `internal/adapter/codeintel/lsp/position_test.go`
- Modify: `internal/app/codeintel/service.go`
- Modify: `internal/app/codeintel/service_test.go`

**Interfaces:**
- Extends Task 7 provider support for E29B query kinds.

- [ ] **Step 1: Write RED position-encoding tests**

Given exact UTF-8 source with emoji/Vietnamese/combining marks, convert canonical byte offsets to/from negotiated LSP UTF-8/UTF-16/UTF-32 positions. UTF-16 default applies only when the server omitted position encoding. Invalid boundary/mid-codepoint positions fail closed.

Run:

```bash
go test ./internal/adapter/codeintel/lsp -run Position -count=1
```

Expected: FAIL.

- [ ] **Step 2: Implement LSP position conversion around exact bytes**

Do not reuse the model display column as LSP character units. Always convert:

```text
model line/scalar column
-> exact SourceRef byte offset
-> negotiated LSP position
```

and provider results:

```text
provider URI/range + encoding
-> exact retained bytes when resolvable
-> zero-based half-open UTF-8 ByteRange
```

- [ ] **Step 3: Write RED navigation tests against fake typed server**

Cover `definition`, `references`, document/workspace `symbols`, `type_definition`, `type_summary` (hover/signature-like bounded provider text), and optional callers/callees. Test unsupported capability returns `code_intelligence_query_unsupported`, not grep approximation.

For imports, keep the public split:

- `import_declarations`: provider/structural declarations for the document;
- `resolved_import_targets`: semantic resolved targets only where gopls/provider can report them reliably; unavailable remains explicit rather than inventing a generic module model.

- [ ] **Step 4: Normalize repository vs external locations**

If a returned URI maps to a safely bound workspace regular UTF-8 source, issue/reuse an exact SourceRef and return `ResolvedSourceLocation`. If it points to Go module cache, toolchain, generated external root, or another unbound semantic root and exact bytes are not safely retained, return `ProviderReportedLocation` with sanitized `origin/display_identity` and provider range summary.

A provider-reported location cannot be fed back into a canonical-position query until explicitly resolved; return `location_not_resolved`.

- [ ] **Step 5: Bound and normalize call hierarchy**

Return provider-supported callers/callees with `completeness=provider_reported|partial`; empty results never claim no runtime caller exists.

- [ ] **Step 6: Run focused tests**

```bash
go test ./internal/adapter/codeintel/lsp ./internal/adapter/codeintel/gopls ./internal/app/codeintel -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/adapter/codeintel/lsp/position.go internal/adapter/codeintel/lsp/position_test.go \
  internal/adapter/codeintel/gopls/navigation.go internal/adapter/codeintel/gopls/navigation_test.go \
  internal/app/codeintel/service.go internal/app/codeintel/service_test.go
git diff --cached --check
git commit -m "feat: expose semantic navigation facts"
```

---

### Task 9: Wire E29 into the daemon and prove no ordinary-shell provider tax

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/daemon_test.go`
- Modify: `internal/adapter/ipc/server_unix.go` if final action composition needs the concrete code service.
- Modify: `internal/app/daemon/shutdown.go` only if provider-manager shutdown belongs in the existing daemon lifecycle; otherwise keep provider close in composition-root defer.

**Interfaces:**
- Consumes completed codeintel service/provider manager and shared Lazy tracker/sampler/activity service.
- Produces live `inspect.code` daemon action.

- [ ] **Step 1: Write RED composition test**

Build the daemon with fake provider factory and assert:

- server capability advertises `code_intelligence`;
- no provider factory `Start` occurs at daemon startup/server-info/ordinary `local_shell start`/workspace/activity/project inspect;
- first `inspect.code` invokes provider acquisition exactly once;
- second compatible query reuses the same provider;
- provider unavailable returns bounded E29 status while the daemon remains healthy.

- [ ] **Step 2: Wire one shared source of workspace truth**

Composition root creates and reuses:

```text
workspace registry/service
WorkspaceCoherenceTracker
WorkspaceDeltaSampler
activity service backed by same sampler
SourceRef store/sourcefs binder
ProviderManager(gopls factory)
codeintel Service
```

Do not instantiate a second independent tracker/sampler for E29.

- [ ] **Step 3: Wire `daemonActions.InspectCode`**

Forward to the codeintel service only. No LSP/provider logic belongs in `cmd/shellbeam` or IPC server.

- [ ] **Step 4: Wire bounded provider shutdown**

On clean daemon shutdown, stop admitting E29 queries, close provider manager within a bounded deadline, and then finish daemon exit. A provider close failure is logged/returned as provider shutdown detail but does not rewrite already durable shell receipts.

- [ ] **Step 5: Run daemon/transport tests**

```bash
go test -race ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/codeintel -run 'Code|Daemon|Provider' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/shellbeam/command_daemon.go cmd/shellbeam/daemon_test.go internal/adapter/ipc/server_unix.go internal/app/daemon/shutdown.go
git diff --cached --check
git commit -m "feat: wire structured code intelligence"
```

Stage only files actually changed; omit unchanged optional paths from `git add`.

---

### Task 10: Native gopls acceptance, resource bounds, and final checkpoint

**Files:**
- Create: `tests/integration/codeintel_gopls_native_test.go`
- Create: `tests/integration/testdata/codeintel_go/` small deterministic Go fixture workspace.
- Modify: `dev/test-impact.toml` if needed for `internal/**/codeintel/**` mapping.
- Add no structural/SCIP implementation in this checkpoint.

**Interfaces:**
- Verifies E29A/E29B + Go/gopls on real macOS/Linux hosts.

- [ ] **Step 1: Add real-gopls test precondition without auto-install**

Native test locates `gopls` with `exec.LookPath`. If absent, it must report a clear test skip only in the optional developer lane; release/native E29 readiness lane must require the executable explicitly. Never download/install gopls in the test.

- [ ] **Step 2: Prove edit-loop diagnostics on a fixture**

Fixture starts valid, then test writes exact invalid saved source through normal filesystem setup and invokes `inspect.code diagnostics(changed_files)`. Assert an undefined symbol/type diagnostic arrives before any build/test command is run, with:

- activity/workspace selection basis as requested;
- selection freshness/completeness;
- exact SourceRef correlation for the returned source;
- `provider_id=gopls` and build/provider provenance;
- bounded message/record count.

Fix the source and assert dirty->clean/changed state synchronizes and old diagnostic does not remain falsely current.

- [ ] **Step 3: Prove navigation/Unicode/external behavior**

On real gopls:

- definition/references/symbols/type-definition/type-summary work for UTF-8 source containing non-ASCII text;
- canonical byte ranges round-trip correctly;
- dependency/toolchain target that is not safely retained is provider-reported rather than given a fabricated canonical range;
- unsupported/partial call hierarchy is labeled honestly.

- [ ] **Step 4: Prove semantic-context widening**

Use a small multi-package/module fixture. Change `go.mod`/`go.work` or the provider build fingerprint and assert provider sync/restart/widening occurs even when the model-facing activity selection contains only a source file. A pure `git commit` with unchanged current source must not force semantic reload solely because HEAD changed.

- [ ] **Step 5: Prove provider lifecycle bounds**

Native/stub stress cases:

- provider crash -> bounded restart;
- repeated crash -> cooldown;
- query cancellation/timeout releases in-flight slot;
- max concurrent queries -> `provider_busy`, no unbounded goroutines;
- source-ref entry/byte limits and provider result limits are enforced;
- provider restart/config/build change invalidates incompatible sync token;
- no active query is killed by idle eviction;
- daemon shutdown reaps gopls.

Run race tests for app/generic LSP components even if the real gopls native process lane itself is not under `-race`.

- [ ] **Step 6: Benchmark warm/cold explicit query and ordinary no-tax path**

Record separate measurements:

```text
ordinary shell, E29 enabled but unused
first/cold inspect.code
warm changed-files diagnostics
warm definition
provider restart recovery
```

The ordinary-shell comparison must preserve the roadmap <=5 ms p95 / <=10 ms p99 incremental enabled-but-unused gate. Do not impose an invented numeric gopls query p95 until measurement evidence is recorded; explicit query wall budgets still cap each call.

- [ ] **Step 7: Run exact final verification**

```bash
go mod verify
go test ./internal/core/codeintel ./internal/app/codeintel ./internal/adapter/codeintel/... \
  ./internal/core/workspace ./internal/app/workspace ./internal/app/activity \
  ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam ./tests/integration -count=1
go test -race ./internal/app/codeintel ./internal/adapter/codeintel/... ./internal/app/workspace ./internal/app/daemon -count=1
go run ./tools/devctl test --dirty --base origin/main
go run ./tools/devctl check
git diff --check origin/main...HEAD
```

Expected: all required commands exit 0. Native gopls readiness must be distinguished from a skipped optional lane; compile-only/non-native evidence does not count as native provider proof.

- [ ] **Step 8: Review anti-goals mechanically**

Search the final diff and reject implementation if it introduced any of:

```text
new MCP tool besides local_shell
watcher enabled by default
gopls CLI production calls
code-action/edit application
AST mutation/query DSL
core whole-repo path->SourceRef sync map
unbounded provider queue/index
raw absolute dependency paths in model-facing records
public deterministic per-file source hash
```

- [ ] **Step 9: Commit final E29 checkpoint**

Inspect staged scope/check/stat and commit:

```bash
git commit -m "test: verify structured code intelligence"
```

---

## Completion Gate

E29 Go semantic core is implementation-ready/complete only when the exact final tree proves:

1. `inspect.code` is one bounded branch of existing `local_shell`; the model never speaks LSP/JSON-RPC/document versions or provider sync protocol;
2. SourceRef IDs are opaque, immutable, never rebound/reused, bounded/expiring, and canonical locations use exact UTF-8 byte ranges only;
3. provider-reported external/unresolved locations never receive fabricated canonical byte ranges;
4. activity/workspace selection basis + sample freshness + selection completeness are explicit and independent from per-record SourceRef correlation/provider-scope completeness;
5. ProviderSyncState is adapter-owned/bounded and becomes incompatible on provider restart/executable/config/build identity changes;
6. dirty->clean/delete/branch-switch/semantic-context transitions synchronize correctly without equating current dirty paths to provider sync scope;
7. gopls diagnostics are source-correlated despite asynchronous push behavior; timeout/late notifications downgrade honestly rather than return empty-as-current;
8. definition/references/symbols/type/import/call-hierarchy facts retain provider/completeness limitations and do not become build/test evidence;
9. ordinary shell performs zero E29 provider work when E29 is unused;
10. gopls lifecycle, capacity, cancellation, crash cooldown, source-ref retention, and result/input budgets are bounded;
11. no watcher-by-default, editor/patch API, hook framework, shadow filesystem, AST engine, gopls CLI production adapter, ast-grep/SCIP hidden indexing, or code-action mutation was introduced;
12. focused, native gopls, race, dirty-test, module-integrity, and repository-check gates pass on the required host lanes.

## Explicit Follow-ups Outside This Plan

Do not fold these into E29 Go core implementation unless separately approved:

- ast-grep query-only structural provider;
- SCIP index consumption/provider;
- direct Tree-sitter integration;
- editor/unsaved-buffer overlays;
- source mutation/refactoring/code actions;
- conditional SourceRef-based patch/write API;
- automatic semantic index generation;
- server-side filesystem watching as a default coherence mechanism.
