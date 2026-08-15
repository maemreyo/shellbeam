# Bounded Output Views Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement A2.3 bounded retained-output slicing/search through the existing `local_shell` tool without adding ordinary start/poll tax or a second truth store.

**Architecture:** Add `internal/app/outputview` as the selector/continuation/rendering application boundary. It reads canonical `sessions/<session>/output.log` only through a narrow store port, reuses state-root cursor key material for HMAC continuations, and is wired through daemon actions → IPC v2 → bridge → MCP v2. Views are ephemeral projections; receipt/raw cursor authority is unchanged.

**Tech Stack:** Go 1.26.x, standard library (`regexp` RE2, `crypto/hmac`, `encoding/json/base64`), existing store/IPC/MCP architecture and JSON Schema 2020-12.

## Global Constraints

- Exactly one MCP tool remains: `local_shell`.
- No persisted output index/view and no background output-view work.
- Explicit view work is bounded; ordinary start/poll performs zero output-view scans/searches.
- Canonical raw byte offsets and receipts remain authority.
- No public deterministic hash of arbitrary output contents.
- `FeatureOutputViews` is advertised only after full daemon/IPC/MCP wiring.
- Production files target 150–300 lines, review >350, hard cap 500; functions review >60, hard cap 80.
- Do not touch `.codegraph`; do not push/PR/merge.

---

### Task 1: Output-view contracts, limits, and capability shape

**Files:**
- Create: `internal/app/outputview/types.go`
- Create: `internal/app/outputview/validation_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`

**Interfaces:**
- Produces: `outputview.Request`, `Selector`, `Result`, `Match`, `RetentionState`, constants `MaxReturnBytes`, `MaxWorkBytes`, `MaxLines`, `MaxMatches`, `MaxPatternBytes`, `MaxContinuationBytes`.
- Produces: `capability.Catalog.WithOutputViews(...)` and output-view limit fields.

- [ ] **Step 1: Write RED validation tests** covering exactly-one selector contract, raw/tail/lines/preview/search bounds, invalid regex-independent shape, and capability remaining unavailable before `WithOutputViews`.
- [ ] **Step 2: Run** `go test ./internal/app/outputview ./internal/core/capability` and confirm missing package/types fail.
- [ ] **Step 3: Implement minimal closed contracts** with selector `Kind` values `raw_range|tail|lines|preview|search`; reject conflicting fields and over-limit requests.
- [ ] **Step 4: Add truthful capability fields**: `output_view_schema_versions`, and limits for return/work/lines/matches/pattern/continuation. `WithOutputViews` sets `FeatureOutputViews=available` only with positive limits.
- [ ] **Step 5: Run focused tests +** `go run ./tools/devctl check`, `git diff --check`, `.codegraph` status.
- [ ] **Step 6: Commit** `feat: define bounded output view contracts`.

### Task 2: Retained-output store metadata and bounded range reads

**Files:**
- Create: `internal/app/outputview/ports.go`
- Create: `internal/adapter/store/output_view_test.go`
- Modify: `internal/adapter/store/repository.go`

**Interfaces:**
- Produces store methods: `OutputExtent(ctx, sessionID) (outputview.Extent, error)` and existing bounded `ReadOutput` as the byte reader.
- `Extent` contains session identity, retained byte size, retention state, and terminal/live cut metadata without output-content hashes.

- [ ] **Step 1: Write RED store tests** for retained empty output, retained non-empty output, compacted output, unknown session, and read out-of-range.
- [ ] **Step 2: Run** `go test ./internal/adapter/store -run 'Output(View|Extent|Cursor)'` and confirm RED.
- [ ] **Step 3: Implement `OutputExtent`** from session metadata + `output.log` stat; distinguish `retained`, `compacted`, `unavailable`; never fabricate compacted bytes from receipt metadata.
- [ ] **Step 4: Keep range reads bounded** and normalize missing-file behavior using extent semantics rather than treating every zero cursor as empty output.
- [ ] **Step 5: Run package/race/devctl/diff gates.**
- [ ] **Step 6: Commit** `feat: expose retained output extents`.

### Task 3: Raw, tail, line, and deterministic preview selectors

**Files:**
- Create: `internal/app/outputview/service.go`
- Create: `internal/app/outputview/raw_test.go`
- Create: `internal/app/outputview/lines_test.go`
- Create: `internal/app/outputview/render.go`
- Create: `internal/app/outputview/render_test.go`

**Interfaces:**
- Produces: `type Store interface { OutputExtent(...); ReadOutput(...) }` and `Service.Read(ctx, Request) (Result, error)`.
- `Result` reports selector kind, retention state, frozen cut bytes, returned raw ranges, rendered text, partial/truncated flags, and optional continuation.

- [ ] **Step 1: RED raw/tail/line tests** for byte accounting, UTF-8 boundary safety, empty output, byte/line out-of-range, bounded backwards tail work, and huge-line handling.
- [ ] **Step 2: RED rendering tests** for ANSI stripping, invalid UTF-8 replacement, CR progress collapse, binary-like summarization, and first+last omission marker.
- [ ] **Step 3: Implement minimal bounded readers** using chunked `ReadOutput`; never allocate beyond return/work caps.
- [ ] **Step 4: Implement deterministic renderer** only inside explicit output-view actions; do not modify ordinary `start/poll` rendering in this task.
- [ ] **Step 5: Run focused/package/race/devctl/diff gates.**
- [ ] **Step 6: Commit** `feat: read bounded output views`.

### Task 4: Opaque continuation and bounded literal/RE2 line search

**Files:**
- Create: `internal/app/outputview/cursor.go`
- Create: `internal/app/outputview/cursor_test.go`
- Create: `internal/app/outputview/search.go`
- Create: `internal/app/outputview/search_test.go`

**Interfaces:**
- Produces: `NewCursorCodec(observation.CursorKeyMaterial)`, signed token prefix `outcur_v1_`, encode/decode bound to session + selector fingerprint + frozen cut + scan byte + line number.
- `search` supports `literal|regex`, case sensitivity, max matches, bounded per-call work, line-oriented regex semantics.

- [ ] **Step 1: RED cursor tests** for tampering, wrong session/selector/cut, key generation expiry, token-size bound.
- [ ] **Step 2: RED search tests** for literal/regex, case sensitivity, malformed regex, match ranges/excerpts, bounded work continuation, oversized logical line, continuation resume without duplicate matches.
- [ ] **Step 3: Implement HMAC codec** following existing event/structured cursor-key pattern; do not expose output hashes.
- [ ] **Step 4: Implement line-oriented bounded search** with `regexp.Compile`, hard line/work/match/excerpt caps, frozen-cut continuation.
- [ ] **Step 5: Run focused/package/race/devctl/diff gates.**
- [ ] **Step 6: Commit** `feat: search retained output safely`.

### Task 5: Daemon, IPC v2, bridge, MCP v2, and schemas

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Add/modify focused IPC/MCP/schema tests.

**Interfaces:**
- Adds `OutputViewActions.ReadOutputView(context.Context, outputview.Request) (outputview.Result, error)`.
- Adds `RequestV2.Selector`, `RequestV2.Continuation`, `ResponseV2.OutputView` and matching bridge response.

- [ ] **Step 1: RED protocol/schema tests** proving `read_output` is accepted, unknown fields rejected, selector schema closed, malformed requests fail before daemon action, and response validates.
- [ ] **Step 2: RED IPC/MCP integration tests** proving action reaches outputview service and remains one `local_shell` tool.
- [ ] **Step 3: Wire daemon composition** with `store.EventCursorKey(ctx)` → `outputview.NewCursorCodec` → service; no worker/goroutine.
- [ ] **Step 4: Remove `read_output` from deferred lists**, add supported action validation/dispatch/bridge/MCP success mapping.
- [ ] **Step 5: Run adapter/schema/cmd tests, race, devctl/diff gates.**
- [ ] **Step 6: Commit** `feat: expose output views through local shell`.

### Task 6: Capability promotion, no-tax proof, and A2.3 checkpoint

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`
- Create: `cmd/shellbeam/output_views_test.go`
- Update spec/plan checkboxes/evidence only where exact behavior is proven.

**Interfaces:**
- Final daemon catalog calls `WithOutputViews(...)` with application hard limits.

- [ ] **Step 1: RED end-to-end acceptance**: run a real daemon child producing ANSI/UTF-8/multiple lines, then exercise raw/tail/lines/preview/literal/regex continuation through IPC v2 and MCP-facing schema.
- [ ] **Step 2: Add no-tax regression** with a counting output-view store/codec seam proving ordinary start/poll does not invoke output-view extent/read/search work.
- [ ] **Step 3: Promote capability** and assert advertised limits/schema version are exact.
- [ ] **Step 4: Run acceptance x3**, focused/full relevant `-race`, `go mod verify`, `go run ./tools/devctl test --dirty --base origin/main --json`, `go run ./tools/devctl check`, schema tests, `git diff --check`, anti-goal scan, `.codegraph` status.
- [ ] **Step 5: Capture exact HEAD/source fingerprint and fresh receipts.**
- [ ] **Step 6: Commit** `test: verify bounded output views`.

## Self-review

- Spec coverage: selectors, rendering, retention states, continuations, search, limits, schemas, capability promotion, and no-tax each map to Tasks 1–6.
- No persistent index/blob copy is introduced.
- Regex semantics are explicitly line-oriented, avoiding ambiguous cross-window matches.
- Continuation binds a frozen byte-count cut rather than an output-content hash.
- `read_output` remains independent from receipt child outcome/evidence authority.
