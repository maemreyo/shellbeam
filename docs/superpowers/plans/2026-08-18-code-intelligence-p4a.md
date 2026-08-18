# ShellBeam P4-A Read-Only Code Intelligence Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the existing Go/gopls `inspect.code` path into a read-only Machine Truth provider that exposes exact model-friendly source cuts, projects semantic facts into the landed P1 affected-relation vocabulary, and publishes bounded provider lifecycle/process/resource facts for later P2/P6-A consumers.

**Architecture:** Preserve the existing exact `SourceRef + byte range` contract and reuse the `DisplaySourceLocation` ergonomics already landed on `origin/main`; P4-A adds one explicit query-start workspace source cut rather than inventing a second location model. Semantic relation projection is pure post-processing over an already-produced `codeintel.Result`, so one user query remains one provider query. `codeintel.ProviderManager` remains the code-intelligence-specific pool/queue/cooldown owner. Shared provider work is limited to neutral fact types plus cheap process-exit resource conversion; long-lived gopls does not inherit the command sampler's 250ms process-tree polling.

**Tech Stack:** Go 1.26.x, existing `go.lsp.dev` LSP client, gopls, `workspace.FastSnapshot.Generation`, existing provider manager/source retention, P1 `verification.AffectedSurface` contracts, existing IPC/MCP v2, JSON Schema 2020-12, PR #12 process resource evidence (`receipt.ResourceEvidence`).

**Spec:** `docs/superpowers/specs/2026-08-18-p4a-p6a-sequencing-amendment-design.md`

**Planning baseline observed on 2026-08-18:** `origin/main=4d033c71272a41f0a782f034d59e65c651a6ed72`. This baseline already contains `source.DisplaySourceLocation` on resolved code locations and command-exit CPU/RSS/process-count resource observation. P1 verification semantics are **not** yet landed on this baseline, so Task 0 must bind the actual completed-P1 execution commit before any P4-A production edit.

## Global Constraints

- P4-A execution starts only after P1 Stage A + Stage B are implemented and their completion checkpoint has a terminal PASS on the exact implementation branch.
- P4-A V1 is Go/gopls only.
- P4-A is source-read-only: no rename, `workspace/applyEdit`, code-action execution, semantic refactor, source write, DAP action, or automatic verification execution.
- Existing `codeintel.SourceRef` remains canonical exact source identity. Old SourceRefs are never rebound to current path bytes.
- Existing `source.DisplaySourceLocation` is the canonical model-facing path/line/range/preview projection for resolved locations. P4-A must not introduce a parallel `SourcePresentation` location object.
- `source_generation` is a separate workspace cut. It is never inserted into SourceRef identity and never used to rewrite old SourceRefs.
- The P4-A source cut is the fresh workspace generation observed **before** provider execution. It is not a claim that the workspace remained unchanged after that cut. Existing `SourceCorrelation`/barrier/recheck semantics preserve changes observed during the query; future consumers compare the cut to current generation for staleness.
- Semantic projection may use only the already-returned `codeintel.Result`; it cannot call `ProviderPool.Query`, start gopls, enumerate extra symbols, recursively follow references, or synthesize missing facts.
- Semantic relations preserve source generation, provider identity/provenance, derivation authority and non-exhaustive coverage. Partial/unknown input never becomes stronger.
- A semantic relation is emitted only for a record whose `SourceCorrelation=current` and for a result with a valid source generation cut. Mixed/changed/unknown records remain code facts but do not receive a falsely exact P1 relation.
- P4-A semantic domains are conservative. V1 does not use semantic-analysis absence as mechanically complete proof of non-applicability.
- `internal/app/codeintel.ProviderManager` retains pool size, queueing, per-provider in-flight limits, idle TTL, compatibility selection, cooldown and restart policy. No shared package gains those controls.
- P6-A must not import or depend on `internal/app/codeintel.ProviderManager`, `ProviderRequest`, `ProviderResponse`, or codeintel query/session policy.
- Provider-neutral runtime types contain facts only. They provide no `Query`, `Start`, `Acquire`, `Pool`, `Queue`, retry policy, worker count, or provider-specific protocol state.
- Reuse canonical `receipt.ResourceEvidence`; do not create a second CPU/RSS/I/O/process-count metric ontology.
- Long-lived gopls V1 records CPU/RSS at process exit from `os.ProcessState`. It leaves `process_count_peak` unavailable rather than running the command resource sampler's 250ms process-tree scan for the provider lifetime.
- PID is an address, not identity. Exact provider process correlation requires existing `process.Identity`/executable identity observation; missing identity remains partial/unavailable.
- A codeintel-specific bounded **latest-state projection** may hold the latest runtime fact per provider incarnation so P2 can consume current state and tests can verify terminal cleanup. It is not a durable history, process authority store, telemetry store, scheduler, or generic provider manager.
- `inspect.code` remains caller-triggered and bounded by existing `ResultLimits` (production response limit currently 1 MiB).
- No P4-A output may contain `task_complete`, `work_complete`, `safe_to_finish`, or equivalent task-completion truth.
- All execution/verification commands after Task 0 use the immutable `P4A_EXECUTION_BASE`, never a moving `origin/main` as evidence identity.

---

### Task 0: Bind the completed-P1 execution base and freeze the real `inspect.code` baseline

**Files:**
- Create: `tools/benchmark-codeintel-p4a/main.go`
- Create: `tools/benchmark-codeintel-p4a/main_test.go`
- Create: `scripts/benchmark-codeintel-p4a.sh`
- Create: `docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-baseline.md`

**Interfaces:**

Every later task consumes these immutable execution facts:

```text
P4A_EXECUTION_BASE=<exact commit containing completed P1>
P4A_PLAN_SHA=<commit containing this plan>
P1_SOURCE_FINGERPRINT=<P1 completion source fingerprint>
```

The benchmark tool uses the existing IPC v2 client directly; it does not add a product CLI command:

```go
package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "time"

    ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
    codeintel "github.com/maemreyo/shellbeam/internal/core/codeintel"
)

type measurement struct {
    SchemaVersion             int    `json:"schema_version"`
    Scenario                  string `json:"scenario"`
    WallMS                    int64  `json:"wall_ms"`
    ResponseBytes             int    `json:"response_bytes"`
    RecordCount               int    `json:"record_count"`
    DisplayLocationCount      int    `json:"display_location_count"`
    SourceGenerationPresent   bool   `json:"source_generation_present"`
    SemanticRelationCount     int    `json:"semantic_relation_count"`
    ProviderRuntimePresent    bool   `json:"provider_runtime_present"`
    ProviderIncarnation       string `json:"provider_incarnation,omitempty"`
    MeasurementQuality        string `json:"measurement_quality"`
}

func call(ctx context.Context, socket, workspaceID, scenario string, query codeintel.Query) (ipc.ResponseV2, int64, error) {
    started := time.Now()
    response, err := ipc.NewClient(socket).CallV2(ctx, ipc.RequestV2{
        IPVersion: 2, Kind: "request", RequestID: "p4a-bench-" + scenario,
        Action: "inspect.code", WorkspaceID: workspaceID, CodeQuery: &query,
    })
    return response, time.Since(started).Milliseconds(), err
}
```

The tool marshals `response.Code` back to JSON and counts additive P4-A fields generically, so the same executable source works before/after the feature without inventing a compatibility API.

The shell harness outputs one JSON row per scenario:

```json
{
  "schema_version": 1,
  "scenario": "definition|references|callers|callees|diagnostics",
  "wall_ms": 0,
  "response_bytes": 0,
  "record_count": 0,
  "display_location_count": 0,
  "source_generation_present": false,
  "semantic_relation_count": 0,
  "provider_runtime_present": false,
  "provider_incarnation": "",
  "measurement_quality": "observed|not_run"
}
```

- [ ] **Step 1: Hard-bind the real completed-P1 prerequisite**

From a clean P4-A implementation worktree:

```bash
set -euo pipefail
test -z "$(git status --porcelain)"
P4A_EXECUTION_BASE="$(git rev-parse HEAD)"
P4A_PLAN_SHA="$(git log -n1 --format=%H -- docs/superpowers/plans/2026-08-18-code-intelligence-p4a.md)"

test -f internal/core/verification/relation.go
test -f internal/app/verification/affected.go
test -f internal/adapter/verification/go_relations.go
rg -n 'type AffectedSurface struct' internal/core/verification
rg -n 'type AffectedRelation struct' internal/core/verification
rg -n 'func RelationID\(' internal/core/verification
rg -n 'type RelationProvider interface' internal/app/verification
python3 scripts/check-verification-p1-plan-traceability.py
go run ./tools/devctl check
```

Also prove the PR #12 primitives P4-A now relies on are present on the execution base:

```bash
rg -n 'type DisplaySourceLocation struct' internal/core/source/location.go
rg -n 'type ResourceEvidence struct' internal/core/receipt/receipt.go
rg -n 'func .*Resource.*ProcessState|platformMaxRSSBytes' internal/adapter/process/resource_observation_*.go
```

If any interface is absent, renamed, or semantically different, stop with `NOT_RUN: plan_binding_mismatch` and amend this plan against the landed P1/current-main code. Do not add compatibility shims simply to preserve this planning document.

Capture the P1 source fingerprint directly from the clean immutable execution base before any P4-A edit:

```bash
P1_TEST_JSON="$(go run ./tools/devctl test --base "$P4A_EXECUTION_BASE" --json)"
P1_SOURCE_FINGERPRINT="$(printf '%s' "$P1_TEST_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["source_fingerprint"])')"
test -n "$P1_SOURCE_FINGERPRINT"
```

The command must exit `0`; if the completed-P1 base no longer passes its own selected verification, P4-A is blocked. Store the exact fingerprint in baseline evidence before any P4-A dirty change exists.

- [ ] **Step 2: Write the benchmark tool test first**

`tools/benchmark-codeintel-p4a/main_test.go` tests the pure JSON counter helper with a pre-P4-A result and an additive post-P4-A fixture:

```go
func TestMeasureCodeResultCountsAdditiveP4AFacts(t *testing.T) {
    raw := []byte(`{
      "status":"ready",
      "query":{"kind":"definition","path":"p.go","line":3,"column":8},
      "provider":{"provider_id":"go_semantic","provider_incarnation":"gopls_x","sync_coverage":"exact_for_known_paths"},
      "source_cut":{"generation":"gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","quality":"fresh"},
      "provider_runtime":{"schema_version":1,"provider_family":"language_semantic","provider_id":"go_semantic","provider_incarnation":"gopls_x","lifecycle":"live","workspace_id":"ws_01K00000000000000000000000"},
      "semantic_projection":{"status":"available","surface":{"schema_version":1,"repository_id":"repo_01K00000000000000000000000","workspace_id":"ws_01K00000000000000000000000","source_generation":"gen_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","domains":[],"relations":[]}},
      "records":[{"kind":"location_target","authority":"mechanical","source_correlation":"current","location_target":{"relationship":"definition","location":{"kind":"resolved","resolved":{"source_ref_id":"src_x","start_byte":0,"end_byte":1,"display":{"path":"p.go","line":3,"column":1}}}}}]
    }`)
    got, err := measureRaw("definition", 12, raw)
    if err != nil { t.Fatal(err) }
    if !got.SourceGenerationPresent || !got.ProviderRuntimePresent || got.DisplayLocationCount != 1 {
        t.Fatalf("measurement=%+v", got)
    }
}
```

- [ ] **Step 3: Run RED, then implement the benchmark tool**

```bash
go test ./tools/benchmark-codeintel-p4a -count=1
```

Expected RED: helper/tool absent.

Implement flags:

```text
--socket        required Unix socket path
--workspace-id  required workspace ID
--scenario      required enum: definition|references|callers|callees|diagnostics
--path          required for positioned/file scenarios
--line          default 1
--column        default 1
```

Scenario-to-query mapping is closed in the tool; arbitrary query JSON is not accepted.

- [ ] **Step 4: Write the practical harness using only existing product commands**

`scripts/benchmark-codeintel-p4a.sh` expects:

```bash
: "${SHELLBEAM_BIN:?}"
: "${P4A_FIXTURE_ROOT:?}"
: "${P4A_STATE_DIR:?}"
: "${P4A_RUNTIME_DIR:?}"
```

It registers the fixture using the actual current CLI:

```bash
workspace_json="$($SHELLBEAM_BIN workspace attach "$P4A_FIXTURE_ROOT" \
  --state-dir "$P4A_STATE_DIR" --runtime-dir "$P4A_RUNTIME_DIR" --json)"
workspace_id="$(printf '%s' "$workspace_json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
```

It starts one daemon with the same state/runtime dirs, waits for `$P4A_RUNTIME_DIR/daemon.sock` plus `shellbeam doctor --require-ready` according to the repository's current readiness helper, runs the five benchmark queries via:

```bash
go run ./tools/benchmark-codeintel-p4a \
  --socket "$P4A_RUNTIME_DIR/daemon.sock" \
  --workspace-id "$workspace_id" \
  --scenario definition \
  --path p/p.go --line 3 --column 27
```

and terminates/reaps only the daemon process it started. It must not `pkill gopls`, `killall`, or delete unrelated runtime/state dirs.

- [ ] **Step 5: Capture the before-state baseline**

Use an isolated Go fixture committed in its own temporary git repo:

```go
package p

func Target(v int) int { return v + 1 }
func Caller() int       { return Target(41) }
```

Record:

```text
execution_base_sha
plan_sha
p1_source_fingerprint
origin/main observed during planning (informational only)
go version
gopls executable path + version, or provider_unavailable
fixture commit
workspace id
five benchmark JSON rows
provider/display behavior already present from PR #12
related live process identities before/after daemon shutdown
```

If gopls is absent, record `measurement_quality=not_run` and exact lookup error. Do not install gopls automatically.

- [ ] **Step 6: Run GREEN/gates and commit Task 0**

```bash
gofmt -w tools/benchmark-codeintel-p4a
go test ./tools/benchmark-codeintel-p4a -count=1
chmod +x scripts/benchmark-codeintel-p4a.sh
git diff --check
go test ./tests/contract -run Markdown -count=1
go run ./tools/devctl commit-gate --base "$P4A_EXECUTION_BASE" --json
git add tools/benchmark-codeintel-p4a scripts/benchmark-codeintel-p4a.sh docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-baseline.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: freeze p4a code intelligence baseline"
```

---

### Task 1: Add one exact query-start `SourceCut` and finish existing source-location ergonomics

**Files:**
- Create: `internal/core/codeintel/source_cut.go`
- Create: `internal/core/codeintel/source_cut_test.go`
- Modify: `internal/core/workspace/snapshot.go`
- Modify: `internal/core/workspace/snapshot_test.go`
- Modify: `internal/core/codeintel/result.go`
- Modify: `internal/core/codeintel/result_test.go`
- Modify: `internal/app/codeintel/ports.go`
- Modify: `internal/app/codeintel/service.go`
- Modify: `internal/app/codeintel/service_test.go`
- Modify: `cmd/shellbeam/code_intelligence.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/a1_inspect_test.go`

**Interfaces:**

Do **not** add a second path/line presentation type. Existing resolved locations already carry:

```go
type ResolvedSourceLocation struct {
    SourceRefID string                 `json:"source_ref_id"`
    StartByte   int64                  `json:"start_byte"`
    EndByte     int64                  `json:"end_byte"`
    Display     *source.DisplaySourceLocation `json:"display,omitempty"`
}
```

Add one result-level source cut:

```go
package codeintel

import (
    "time"
    workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SourceCut struct {
    RepositoryID  workspace.RepositoryID       `json:"repository_id,omitempty"`
    WorkspaceID   workspace.WorkspaceID        `json:"workspace_id,omitempty"`
    Generation    string                       `json:"generation,omitempty"`
    Quality       workspace.ObservationQuality `json:"quality"`
    ObservedAt    time.Time                    `json:"observed_at"`
    DiagnosticCode string                      `json:"diagnostic_code,omitempty"`
}

type Result struct {
    Status    ResultStatus      `json:"status"`
    Query     Query             `json:"query"`
    SourceCut SourceCut         `json:"source_cut,omitzero"`
    Selection SelectionMetadata `json:"selection,omitzero"`
    Provider  ProviderMetadata  `json:"provider,omitzero"`
    Records   []Record          `json:"records,omitempty"`
}
```

Generation validation reuses workspace identity semantics. If the landed P1 execution base has not already exported an equivalent helper, Task 1 adds exactly:

```go
func ValidateGeneration(value string) error {
    if !validGeneration(value) {
        return fmt.Errorf("invalid workspace generation")
    }
    return nil
}
```

to `internal/core/workspace/snapshot.go`; `SourceCut.Validate` and P1/P4-A consumers call this shared validator rather than each reimplementing the `gen_` grammar. If Task 0 discovers P1 already landed the same semantic helper under another exact name, the plan must be amended at Task 0 instead of adding an alias.

Validation:

```text
zero SourceCut is allowed only for pre-P4-A internal fixtures during migration tests;
non-zero SourceCut requires observed_at and valid quality;
quality unavailable -> generation empty + diagnostic_code required;
quality fresh|cached|stale -> repository/workspace/generation present and valid;
SourceCut never contains path/line/symbol bytes.
```

App port:

```go
type WorkspaceSnapshotSource interface {
    ObserveFresh(context.Context, string) workspace.FastSnapshot
}

func NewService(
    workspaces WorkspaceLookup,
    sampler WorkspaceSampler,
    activities ActivitySelector,
    binder SourceBinder,
    providers ProviderPool,
    coherence CoherenceSource,
    snapshots WorkspaceSnapshotSource,
    limits ServiceLimits,
) (*Service, error)
```

`Service` stores `snapshots WorkspaceSnapshotSource`; production construction requires it non-nil. Tests that intentionally exercise constructor validation prove nil snapshots are rejected. Existing codeintel runtime constructors gain the same exact dependency:

```go
func composeCodeIntelligenceRuntime(
    workspaces appcodeintel.WorkspaceLookup,
    sampler appcodeintel.WorkspaceSampler,
    activities appcodeintel.ActivitySelector,
    coherence appcodeintel.CoherenceSource,
    snapshots appcodeintel.WorkspaceSnapshotSource,
    factory codeProviderFactory,
    resolver codeProviderResolver,
) (*codeIntelligenceRuntime, error)

func newCodeIntelligenceRuntime(
    workspaces appcodeintel.WorkspaceLookup,
    sampler appcodeintel.WorkspaceSampler,
    activities appcodeintel.ActivitySelector,
    coherence appcodeintel.CoherenceSource,
    snapshots appcodeintel.WorkspaceSnapshotSource,
) (*codeIntelligenceRuntime, error)
```

`newCodeIntelligenceRuntimeWithProvider(...)` receives `snapshots` in the same position immediately after `coherence` and forwards it to `appcodeintel.NewService`.

`Service.Inspect` calls `ObserveFresh(ctx, workspace.Root)` **once, after workspace validation and before the provider query**. It converts that snapshot into `SourceCut` and includes the cut even on early `StatusUnavailable` returns caused by unavailable changed-file selection. It does not take a second fresh snapshot at query end. Existing coherence barriers and selected-source rechecks continue to mark `source_changed_during_query`; the source cut remains the literal start cut and may become stale after the call.

- [ ] **Step 1: Write failing core/source-cut tests**

Required tests:

```go
func TestSourceCutKeepsGenerationSeparateFromSourceRef(t *testing.T) {
    cut := SourceCut{
        RepositoryID: "repo_01K00000000000000000000000",
        WorkspaceID: "ws_01K00000000000000000000000",
        Generation: "gen_" + strings.Repeat("a", 64),
        Quality: workspace.QualityFresh,
        ObservedAt: time.Unix(1, 0).UTC(),
    }
    if err := cut.Validate(); err != nil { t.Fatal(err) }
    encoded, _ := json.Marshal(cut)
    if bytes.Contains(encoded, []byte("source_ref")) { t.Fatalf("cut leaked SourceRef identity: %s", encoded) }
}
```

Also test unavailable cut, bad generation, missing diagnostic, missing IDs, zero timestamp and unsafe diagnostic text.

- [ ] **Step 2: Write failing service tests before implementation**

Required tests:

```text
TestInspectCodeCapturesOneFreshSourceCutBeforeProviderQuery
TestInspectCodeSourceCutUnavailableDoesNotInventGeneration
TestInspectCodeSourceChangedDuringQueryKeepsStartGenerationAndChangedCorrelation
TestInspectCodeOldSourceRefStillResolvesRetainedBytesAfterWorkspaceGenerationChanges
TestResolvedRepositoryLocationsUseExistingDisplaySourceLocation
TestProviderReportedLocationStillDoesNotInventResolvedDisplayOrSourceRef
```

The fake snapshot source counts `ObserveFresh` calls and returns G1. The fake provider mutates a selected source before returning. Assert exactly one fresh snapshot call, result cut G1, affected record correlation `source_changed_during_query`, and no SourceRef rebind.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/codeintel ./internal/app/codeintel ./cmd/shellbeam -run 'SourceCut|InspectCode.*Generation|DisplaySourceLocation' -count=1
```

Expected: compile/test failure because `SourceCut`/snapshot dependency do not exist.

- [ ] **Step 4: Implement source cut and daemon wiring**

Reuse the existing daemon `workspaceObserver`; do not create a second observer:

```go
codeRuntime, err := composeCodeIntelligenceRuntime(
    workspaceSvc,
    deltaSampler,
    activitySvc,
    coherence,
    workspaceObserver,
    providerFactory,
    providerResolver,
)
```

Update all codeintel runtime constructors consistently. Existing tests that inject fake providers must inject a deterministic fake snapshot source.

For resolved locations, preserve the PR #12 contract already required by Task 0: `DisplaySourceLocation` is generated from retained/synchronized bytes where exact. Task 1 tests that behavior but does not rewrite gopls display normalization. A missing PR #12 display primitive is a Task-0 `plan_binding_mismatch`, not an implementation branch inside Task 1.

- [ ] **Step 5: Extend closed wire schemas additively**

Add `$defs.code_source_cut` once in each applicable v2 schema:

```json
{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "repository_id":{"type":"string","pattern":"^repo_[0-9A-HJKMNP-TV-Z]{26}$"},
    "workspace_id":{"type":"string","pattern":"^ws_[0-9A-HJKMNP-TV-Z]{26}$"},
    "generation":{"type":"string","pattern":"^gen_[0-9a-f]{64}$"},
    "quality":{"enum":["fresh","cached","stale","unavailable"]},
    "observed_at":{"type":"string","format":"date-time"},
    "diagnostic_code":{"type":"string","minLength":1,"maxLength":128}
  },
  "required":["quality","observed_at"]
}
```

The schema cannot express every cross-field invariant; Go validation tests cover unavailable-vs-generation consistency. Add optional `source_cut` to the code result. Do not change/remove existing `resolved.display` schema.

- [ ] **Step 6: Run GREEN/race and commit**

```bash
gofmt -w internal/core/workspace/snapshot.go internal/core/workspace/snapshot_test.go internal/core/codeintel internal/app/codeintel cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go
go test ./internal/core/workspace ./internal/core/codeintel ./internal/app/codeintel ./internal/adapter/codeintel/gopls ./cmd/shellbeam ./api/schema -run 'Generation|SourceCut|InspectCode|DisplaySourceLocation|CodeIntelligence' -count=1
go test -race ./internal/app/codeintel ./internal/adapter/codeintel/gopls ./cmd/shellbeam -run 'SourceCut|CodeIntelligence' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/workspace/snapshot.go internal/core/workspace/snapshot_test.go internal/core/codeintel internal/app/codeintel cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go api/schema/ipc-v2.json api/schema/mcp-output-v2.json api/schema/a1_inspect_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: bind code intelligence to source generation"
```

---

### Task 2: Add a pure semantic relation projector into P1 `AffectedSurface`

**Files:**
- Modify: `internal/core/verification/relation.go`
- Modify: `internal/core/verification/relation_test.go`
- Create: `internal/core/codeintel/semantic_projection.go`
- Create: `internal/core/codeintel/semantic_projection_test.go`
- Create: `internal/adapter/verification/codeintel_relations.go`
- Create: `internal/adapter/verification/codeintel_relations_test.go`

**Interfaces:**

Extend only the P1 relation/domain vocabularies needed to label semantic-provider derivation:

```go
const (
    BasisSemanticProvider RelationBasis = "semantic_provider"
    DomainSemanticCode    AffectedDomainKind = "semantic_code"
)
```

Core codeintel owns only the model-facing wrapper around P1's exact surface type; the adapter owns derivation:

```go
package codeintel

import verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"

type SemanticProjectionStatus string
const (
    SemanticProjectionAvailable   SemanticProjectionStatus = "available"
    SemanticProjectionPartial     SemanticProjectionStatus = "partial"
    SemanticProjectionUnavailable SemanticProjectionStatus = "unavailable"
)

type SemanticProjection struct {
    Status      SemanticProjectionStatus      `json:"status"`
    Surface     *verificationcore.AffectedSurface `json:"surface,omitempty"`
    Diagnostics []string                      `json:"diagnostics,omitempty"`
}
```

The projector consumes a completed result and has **no provider/query interface**:

```go
package verification

import (
    codeintelcore "github.com/maemreyo/shellbeam/internal/core/codeintel"
    verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"
)

type CodeIntelProjectionInput struct {
    Result     codeintelcore.Result
    CapturedAt time.Time
}

func ProjectCodeIntelRelations(CodeIntelProjectionInput) (codeintelcore.SemanticProjection, error)
```

The output's `Surface` is the landed P1 `verificationcore.AffectedSurface`; domains, relations, subjects, provider refs, `DomainID` and `RelationID` are all P1 types. P4-A does not define a parallel relation ontology or a second projection-status enum.

Projection eligibility:

```text
SourceCut unavailable/invalid/no generation
  -> SemanticProjectionUnavailable, Surface=nil, diagnostic semantic_source_generation_unavailable

Result status unavailable|failed
  -> SemanticProjectionUnavailable; no P1 relation/domain fabricated

Result status stale
  -> SemanticProjectionPartial; surface may contain only current records, domain coverage unknown/partial

record SourceCorrelation != current
  -> retain code fact in inspect.code Records
  -> omit from semantic relations
  -> projection/domain diagnostics note skipped non-current facts

record SourceCorrelation=current
  -> eligible for conservative relation mapping
```

Mapping V1:

```text
definition             -> semantic_definition
references             -> semantic_reference
callers                -> semantic_caller
callees                -> semantic_callee
resolved_import_targets-> semantic_import_target
```

Subjects remain inside P1 V1 vocabulary:

```text
from:
  path:<query.Path> for positioned/file queries

to:
  source_ref:<exact target SourceRef> when resolved
  otherwise path:<sanitized provider-reported logical path> only when the record remains current and path is safe
  otherwise omit relation
```

V1 intentionally does not add a `symbol` P1 subject kind. Symbol/name stays in codeintel record fields; P2 may join it later without changing P1 relation identity.

Authority:

```text
exact resolved target + mechanical record -> mechanical
provider-reported target or advisory record -> advisory
never authoritative
```

Coverage is conservative:

```text
ready + complete selection + usable records -> bounded
partial result/selection/provider sync -> partial
stale/unknown/provider-reported-only -> unknown or partial
call hierarchy is never complete in P4-A V1
zero matching relations still emits semantic_code domain only when a valid source cut exists;
that domain is bounded/partial/unknown, never complete
```

Therefore semantic-domain absence can never by itself prove policy non-applicability.

Provider identity:

```go
verification.ProviderRef{ID: "codeintel/go_semantic", Version: 1}
```

Provenance refs are bounded hashes, not raw paths/environment:

```text
codeintel_provider:<sha256(canonical ProviderMetadata)>
codeintel_query:<sha256(canonical Query)>
```

- [ ] **Step 1: Write RED relation tests**

Required tests:

```text
TestCodeIntelProjectionRequiresValidSourceGeneration
TestCodeIntelReferenceProjectionPreservesMechanicalAuthorityAndGeneration
TestCodeIntelCallHierarchyNeverClaimsCompleteCoverage
TestCodeIntelPartialResultCannotStrengthenCoverage
TestCodeIntelChangedDuringQueryRecordIsNotProjected
TestCodeIntelProviderReportedTargetNeverInventsSourceRef
TestCodeIntelProjectionRelationIDChangesWithProviderProvenance
TestCodeIntelProjectionEmitsNonCompleteDomainWithZeroRelations
TestCodeIntelProjectorHasNoProviderPoolDependency
```

For the dependency test, compile the function with only a `codeintel.Result`; additionally source-scan `codeintel_relations.go` for `ProviderPool`, `ProviderRequest`, `Query(` and fail if present.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/adapter/verification -run 'CodeIntel|SemanticCode' -count=1
```

Expected: new basis/domain/projector missing.

- [ ] **Step 3: Implement deterministic projector**

Use P1 identity helpers exactly:

```go
domainID, err := verificationcore.DomainID(
    verificationcore.DomainSemanticCode,
    &providerRef,
    result.SourceCut.Generation,
    provenance,
)

relationID, err := verificationcore.RelationID(verificationcore.RelationIdentityInput{
    From: from, To: to, Kind: relationKind,
    Basis: verificationcore.BasisSemanticProvider,
    DerivationAuthority: authority,
    Coverage: coverage,
    Provider: &providerRef,
    SourceGeneration: result.SourceCut.Generation,
    ProvenanceRefs: provenance,
})
```

Sort/deduplicate projected relations by `RelationID`. Never use timestamp, preview text or diagnostic prose in relation identity.

- [ ] **Step 4: Run GREEN/race and commit**

```bash
gofmt -w internal/core/verification/relation.go internal/core/verification/relation_test.go internal/core/codeintel/semantic_projection.go internal/core/codeintel/semantic_projection_test.go internal/adapter/verification/codeintel_relations.go internal/adapter/verification/codeintel_relations_test.go
go test ./internal/core/verification ./internal/core/codeintel ./internal/adapter/verification -run 'CodeIntel|SemanticProjection|SemanticCode|Relation' -count=1
go test -race ./internal/adapter/verification -run 'CodeIntel' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/verification/relation.go internal/core/verification/relation_test.go internal/core/codeintel/semantic_projection.go internal/core/codeintel/semantic_projection_test.go internal/adapter/verification/codeintel_relations.go internal/adapter/verification/codeintel_relations_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: project code intelligence affected relations"
```

---

### Task 3: Define provider-neutral runtime facts and extract cheap exit-resource conversion

**Files:**
- Create: `internal/core/providerobservation/types.go`
- Create: `internal/core/providerobservation/types_test.go`
- Modify: `internal/adapter/process/resource_observation_unix.go`
- Modify: `internal/adapter/process/resource_observation_test.go`

**Interfaces:**

Neutral fact package:

```go
package providerobservation

const SchemaVersion = 1

type Lifecycle string
const (
    LifecycleStarting Lifecycle = "starting"
    LifecycleLive     Lifecycle = "live"
    LifecycleClosing  Lifecycle = "closing"
    LifecycleTerminal Lifecycle = "terminal"
    LifecycleLost     Lifecycle = "lost"
)

type ProcessQuality string
const (
    ProcessExact       ProcessQuality = "exact"
    ProcessPartial     ProcessQuality = "partial"
    ProcessUnavailable ProcessQuality = "unavailable"
)

type CleanupState string
const (
    CleanupNotEvaluated CleanupState = "not_evaluated"
    CleanupComplete     CleanupState = "complete"
    CleanupIncomplete   CleanupState = "incomplete"
    CleanupUnknown      CleanupState = "unknown"
)

type RuntimeID string

func ParseRuntimeID(value string) (RuntimeID, error)

type ProcessCorrelation struct {
    PID                int               `json:"pid,omitempty"`
    Identity           *process.Identity `json:"process_identity,omitempty"`
    ExecutableIdentity string            `json:"executable_identity,omitempty"`
    Quality            ProcessQuality    `json:"quality"`
}

type Observation struct {
    SchemaVersion       int                    `json:"schema_version"`
    ProviderRuntimeID   RuntimeID              `json:"provider_runtime_id"`
    ProviderFamily      string                 `json:"provider_family"`
    ProviderID          string                 `json:"provider_id"`
    ProviderIncarnation string                 `json:"provider_incarnation,omitempty"`
    ExecutableVersion   string                 `json:"executable_version,omitempty"`
    WorkspaceID         workspace.WorkspaceID  `json:"workspace_id"`
    Lifecycle           Lifecycle              `json:"lifecycle"`
    Process             *ProcessCorrelation    `json:"process,omitempty"`
    StartedAt           time.Time              `json:"started_at,omitempty"`
    LastUsedAt          time.Time              `json:"last_used_at,omitempty"`
    TerminalAt          time.Time              `json:"terminal_at,omitempty"`
    TerminalResources   *receipt.ResourceEvidence `json:"terminal_resources,omitempty"`
    Cleanup             CleanupState           `json:"cleanup"`
    DiagnosticCodes     []string               `json:"diagnostic_codes,omitempty"`
}
```

Validation rules:

```text
provider_runtime_id required in every lifecycle state; `ParseRuntimeID` accepts exactly `prun_` + 26-character Crockford ULID and rejects whitespace/control/path separators
provider_family/provider_id/workspace/lifecycle required
incarnation required for live|closing|terminal|lost, optional only while starting
provider_runtime_id is manager-assigned before provider spawn and remains unchanged through starting -> live -> closing -> terminal|lost; provider incarnation never replaces it
ProcessExact -> PID + process.Identity + executable identity required
ProcessUnavailable -> no PID/identity fabricated
live -> terminal_at absent, terminal_resources absent, cleanup not_evaluated
terminal -> terminal_at required; cleanup complete|incomplete|unknown
lost -> terminal resource evidence optional and never fabricated
terminal resource metrics reuse receipt.ResourceEvidence.Validate()
max diagnostic codes 16, safe bounded values
```

`ParseRuntimeID` validates correlation syntax only; runtime IDs carry no authority. Add a reflection anti-policy test proving fields such as `Query`, `QueueDepth`, `PoolSize`, `RetryCount`, `MaxInFlight`, `WorkerCount`, `Cooldown` are absent.

Extract one cheap canonical helper from PR #12 process resource code:

```go
// ExitResourceEvidence derives exit-time CPU/RSS evidence without starting a
// periodic process-tree sampler. process_count_peak and I/O remain unavailable.
func ExitResourceEvidence(state *os.ProcessState) *receipt.ResourceEvidence
```

Refactor the existing command `resourceSampler.Finish` to use a private common helper:

```go
func buildExitResourceEvidence(state *os.ProcessState, processCountPeak *int64) *receipt.ResourceEvidence
```

Command executions pass the sampled peak. Long-lived provider use calls `ExitResourceEvidence(state)` with nil peak.

- [ ] **Step 1: Write RED neutral-fact tests**

Required tests:

```text
TestProviderObservationClosedLifecycleAndCleanupVocabulary
TestProviderRuntimeIDClosedSyntaxAndNoAuthorityMeaning
TestProviderObservationRequiresStableRuntimeIDAcrossLifecycle
TestProviderObservationExactProcessRequiresIdentityNotPIDAlone
TestProviderObservationLiveCannotClaimTerminalResources
TestProviderObservationTerminalReusesReceiptResourceEvidence
TestProviderObservationContainsFactsNotManagerPolicy
```

- [ ] **Step 2: Write RED process-resource extraction tests**

Tests prove:

```text
ExitResourceEvidence(nil) == nil
valid ProcessState -> CPU user/system platform_reported, RSS platform_reported when platform supplies it
ReadBytes/WriteBytes unavailable
ProcessCountPeak unavailable for cheap provider path
existing command resourceSampler.Finish still reports sampled process_count_peak
no new background goroutine/ticker is created by ExitResourceEvidence
```

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/providerobservation ./internal/adapter/process -run 'ProviderObservation|ExitResource|ResourceSampler' -count=1
```

Expected: new package/helper absent.

- [ ] **Step 4: Implement minimal facts/helper**

Do not create `internal/app/provider`, `ProviderManager`, scheduler, queue, or generic session package. Do not export the existing `resourceSampler` or its 250ms ticker for provider use.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/core/providerobservation internal/adapter/process/resource_observation_unix.go internal/adapter/process/resource_observation_test.go
go test ./internal/core/providerobservation ./internal/adapter/process -run 'ProviderObservation|ExitResource|ResourceObservation|ResourceSampler' -count=1
go test -race ./internal/core/providerobservation ./internal/adapter/process -run 'ProviderObservation|ExitResource|ResourceSampler' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/providerobservation internal/adapter/process/resource_observation_unix.go internal/adapter/process/resource_observation_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: define provider runtime observation facts"
```

---

### Task 4: Instrument the real gopls child with exact identity and terminal resource truth

**Files:**
- Modify: `internal/adapter/codeintel/lsp/transport.go`
- Modify: `internal/adapter/codeintel/lsp/transport_test.go`
- Modify: `internal/app/codeintel/ports.go`
- Modify: `internal/adapter/codeintel/gopls/factory.go`
- Modify: `internal/adapter/codeintel/gopls/factory_test.go`
- Modify: `internal/adapter/codeintel/gopls/provider.go`
- Modify: `internal/adapter/codeintel/gopls/provider_test.go`
- Modify: `cmd/shellbeam/code_intelligence.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`

**Interfaces:**

LSP process runtime is protocol-neutral process fact, not provider metadata:

```go
package lsp

type ProcessRuntime struct {
    PID               int
    StartedAt         time.Time
    TerminalAt        time.Time
    TerminalResources *receipt.ResourceEvidence
    Reaped            bool
    ExitCode          *int
    ExitSignal        string
}

func (s *Session) ProcessRuntime() ProcessRuntime
```

Change `Session.waitCh` from `chan error` to a closed internal result:

```go
type processExit struct {
    err       error
    runtime   ProcessRuntime
}
```

After `cmd.Wait()`, copy `cmd.ProcessState`, derive terminal resources using `processadapter.ExitResourceEvidence`, freeze terminal timestamp and exit status exactly once, then publish the immutable `ProcessRuntime`. `Session.Close()` retains its existing Shutdown/Exit/stdin-close/wait/kill-fallback semantics.

App process observer port:

```go
type ProviderProcessObserver interface {
    Observe(context.Context, int) (process.ProcessFact, error)
    Children(context.Context, []int) (map[int][]int, bool, error)
}
```

The interface intentionally matches the stateless capabilities already provided by `processadapter.HostInspector`; P4-A does not add another host-process implementation.

Extend gopls factory construction without breaking tests:

```go
func NewFactory(config Config) (*Factory, error) {
    return NewFactoryWithProcessObserver(config, nil)
}

func NewFactoryWithProcessObserver(
    config Config,
    observer appcodeintel.ProviderProcessObserver,
) (*Factory, error)
```

`Factory` stores the observer as a dependency; `Factory.Start` passes it into `startProvider(...)`. The production daemon creates one stateless `processadapter.NewHostInspector()` before composing code intelligence and passes that same inspector to the gopls factory and later process-inspection service. Injected test factories are not wrapped or replaced. No process ownership store is created.

The real `lspSemanticSession` implements an optional internal interface:

```go
type processBackedSemanticSession interface {
    semanticSession
    ProcessRuntime() lspadapter.ProcessRuntime
}
```

`gopls.Provider` stores the initial host-observed root process fact when available. It also owns a bounded one-shot cleanup snapshot used only at close time:

```go
type observedProviderProcess struct {
    PID      int
    Identity process.Identity
}

const (
    providerCleanupMaxDescendants = process.MaxDescendants
    providerCleanupMaxDepth       = process.MaxTraversalDepth
)

func snapshotProviderProcessTree(
    ctx context.Context,
    observer appcodeintel.ProviderProcessObserver,
    rootPID int,
) (facts []observedProviderProcess, complete bool)

func verifyProviderProcessTreeGone(
    ctx context.Context,
    observer appcodeintel.ProviderProcessObserver,
    facts []observedProviderProcess,
    snapshotComplete bool,
) providerobservation.CleanupState
```

`snapshotProviderProcessTree` runs **once immediately before provider/session close**, breadth-first to the existing process hard bounds. It calls `Children`/`Observe` only for that bounded close snapshot; it never starts a ticker. A child traversal/identity error or bound hit makes `complete=false`.

After LSP close/root reap, `verifyProviderProcessTreeGone` re-observes each exact PID identity once:

```text
ProcessNotFound -> original identity is gone
same PID but different exact process.Identity -> original identity is gone
ProcessIdentityChanged -> original identity is gone but observation remains diagnostic
same exact identity still live -> cleanup=incomplete
access denied / observation incomplete / initial snapshot incomplete -> cleanup=unknown
all exact captured identities proven gone + root reaped -> cleanup=unknown with diagnostic `provider_descendant_closure_unproven`; the one-shot pre-close snapshot cannot prove that no descendant was spawned after the snapshot
```

Root `cmd.Wait()` alone is never enough to mark tree cleanup complete. In fact, direct-exec gopls P4-A V1 has no exhaustive descendant-closure authority, so it never emits `CleanupComplete`: the close snapshot can prove a positive surviving exact identity (`CleanupIncomplete`) or support an honest `CleanupUnknown` when no captured identity survives. `CleanupComplete` remains in the provider-neutral ontology for future providers backed by stronger ownership/quiescence proof.

`gopls.Provider` implements:

```go
func (p *Provider) RuntimeObservation(ctx context.Context) providerobservation.Observation
```

Live observation:

```text
provider_family = language_semantic
provider_id = go_semantic
provider_incarnation = existing gopls metadata incarnation
workspace = exact provider workspace
lifecycle = live
process exact only when HostInspector returned exact Identity + executable identity
started_at = LSP ProcessRuntime.StartedAt
terminal_resources absent
cleanup = not_evaluated
```

After close/reap:

```text
lifecycle = terminal when child reaped and identity/lifecycle is known
terminal_at = frozen LSP runtime terminal time
terminal_resources = ExitResourceEvidence(ProcessState)
process_count_peak = unavailable
cleanup = incomplete when any captured exact root/descendant identity survives close;
cleanup = unknown when captured identities are gone or traversal/identity was incomplete, because P4-A V1 has no exhaustive descendant-closure authority;
P4-A gopls V1 never emits cleanup=complete
```

Unexpected loss without trustworthy terminal state becomes `lost`, not successful terminal cleanup.

- [ ] **Step 1: Write RED LSP runtime tests**

Required tests:

```text
TestLSPSessionExposesStartedChildPIDAndStartedAt
TestLSPSessionFreezesExitResourcesOnceAfterReap
TestLSPSessionProviderPathDoesNotStartPeriodicProcessTreeSampler
TestLSPSessionCloseKeepsExistingShutdownExitKillFallback
```

The third test injects a process/resource hook counter or uses the extracted helper boundary to prove no `resourceSampleInterval` ticker belongs to LSP session.

- [ ] **Step 2: Write RED gopls process/runtime tests**

Required tests:

```text
TestGoplsRuntimeObservationCorrelatesExactHostProcessIdentity
TestGoplsRuntimeObservationKeepsPIDPartialWhenIdentityUnavailable
TestGoplsLiveObservationDoesNotClaimTerminalResources
TestGoplsTerminalObservationUsesCanonicalExitResourceEvidence
TestGoplsCleanupDoesNotTreatRootReapAsTreeQuiescence
TestGoplsCleanupAllCapturedIdentitiesGoneStillReportsClosureUnproven
TestGoplsCleanupSurvivingExactDescendantIsIncomplete
TestGoplsCleanupIncompleteTraversalIsUnknown
TestGoplsRuntimeObservationKeepsProviderMetadataIncarnationStable
```

- [ ] **Step 3: Run RED**

```bash
go test ./internal/adapter/codeintel/lsp ./internal/adapter/codeintel/gopls ./cmd/shellbeam -run 'ProcessRuntime|RuntimeObservation|Gopls.*Process' -count=1
```

Expected: missing interfaces/runtime methods.

- [ ] **Step 4: Implement production wiring**

In daemon composition, create/reuse the host inspector before gopls factory construction. If `runDaemonWithProviders` receives a test factory, do not wrap/replace it; injected test factories retain authority over their fake provider behavior.

Do not move LSP protocol/session state into `providerobservation` or `ProviderManager`.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/adapter/codeintel/lsp internal/app/codeintel/ports.go internal/adapter/codeintel/gopls cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go
go test ./internal/adapter/codeintel/lsp ./internal/adapter/codeintel/gopls ./cmd/shellbeam -run 'ProcessRuntime|RuntimeObservation|CodeIntelligence|Gopls' -count=1
go test -race ./internal/adapter/codeintel/lsp ./internal/adapter/codeintel/gopls -run 'ProcessRuntime|RuntimeObservation' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/adapter/codeintel/lsp internal/app/codeintel/ports.go internal/adapter/codeintel/gopls cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: observe gopls process lifecycle"
```

---

### Task 5: Publish bounded provider lifecycle facts without turning `ProviderManager` into a generic framework

**Files:**
- Create: `internal/app/codeintel/runtime_projection.go`
- Create: `internal/app/codeintel/runtime_projection_test.go`
- Modify: `internal/app/codeintel/ports.go`
- Modify: `internal/app/codeintel/provider_manager.go`
- Modify: `internal/app/codeintel/provider_manager_policy.go`
- Modify: `internal/app/codeintel/provider_manager_test.go`
- Modify: `internal/app/codeintel/service.go`
- Modify: `internal/app/codeintel/service_test.go`
- Modify: `internal/core/codeintel/result.go`
- Modify: `internal/core/codeintel/result_test.go`

**Interfaces:**

Optional provider runtime interface:

```go
type RuntimeObservedProvider interface {
    Provider
    RuntimeObservation(context.Context) providerobservation.Observation
}

type ProviderRuntimeSink interface {
    Publish(providerobservation.Observation)
}

type ProviderResponse struct {
    Status      core.ResultStatus
    Metadata    core.ProviderMetadata
    Runtime     *providerobservation.Observation
    Diagnostics []ProviderDiagnostic
    Symbols     []ProviderSymbol
    Locations   []ProviderLocation
    TypeSummary string
}
```

The production codeintel runtime uses a **codeintel-specific latest-state projection**, not a generic provider store:

```go
const MaxProviderRuntimeFacts = 16

type RuntimeProjection struct {
    // keyed by ProviderRuntimeID; only latest fact is retained.
}

func NewRuntimeProjection(max int) (*RuntimeProjection, error)
func (p *RuntimeProjection) Publish(providerobservation.Observation)
func (p *RuntimeProjection) Get(runtimeID providerobservation.RuntimeID) (providerobservation.Observation, bool)
func (p *RuntimeProjection) List() []providerobservation.Observation
```

Rules:

```text
max <= 16; deterministic oldest-terminal eviction
one latest fact per provider_runtime_id, no time series
manager generates `prun_` + ULID at `managedProvider` reservation before `Factory.Start`; this ID is stable for the provider attempt
starting has runtime ID but may lack provider incarnation
after start, live/closing/terminal/lost keep the same runtime ID and also bind the real provider incarnation
replacement gets a new runtime ID even if compatibility key is identical
terminal/lost fact may remain until bounded eviction so cleanup can be inspected
projection carries no ProcessHandle, kill authority, stdin, query, queue, retry or scheduler API
projection is in-memory only; daemon restart loses it and consumers see unknown/unavailable rather than fake continuity
```

Preserve the existing constructor exactly for broad call-site compatibility and add one explicit constructor for runtime publication:

```go
func NewProviderManager(
    factory ProviderFactory,
    resolver ProviderOptionsResolver,
    limits ProviderManagerLimits,
) (*ProviderManager, error) {
    return NewProviderManagerWithRuntimeSink(factory, resolver, limits, nil)
}

func NewProviderManagerWithRuntimeSink(
    factory ProviderFactory,
    resolver ProviderOptionsResolver,
    limits ProviderManagerLimits,
    runtimeSink ProviderRuntimeSink,
) (*ProviderManager, error)
```

Do not change `ProviderManagerLimits` semantics. Add `runtimeID providerobservation.RuntimeID` to `managedProvider`; generate `"prun_" + ulid.Make().String()` at reservation, parse it through `providerobservation.ParseRuntimeID`, and freeze the parsed value before publication. Runtime ID is correlation only and grants no process authority.

After a provider query succeeds, `ProviderManager.Query` obtains the current fact from `RuntimeObservedProvider` when implemented, overlays the manager-owned `ProviderRuntimeID` and `LastUsedAt`, publishes it to the sink, and places a copy in `ProviderResponse.Runtime` **before** returning. The service copies only that response fact into `Result.ProviderRuntime`; it never performs a second provider query or lookup by PID. Providers without `RuntimeObservedProvider` leave `Runtime=nil` and do not get fabricated facts.

Lifecycle publication:

```text
reserved start          -> starting
provider start complete -> live observation from RuntimeObservedProvider
successful query reuse  -> live with LastUsedAt updated; same incarnation
incompatible replacement/idle eviction/daemon close
                        -> closing before Provider.Close
                        -> terminal/unknown after close according to provider observation
unexpected non-contract provider query failure
                        -> lost/closing/terminal literal sequence; never fake success
cooldown                -> remains manager policy, not copied into Observation
```

Close helpers must stop discarding `Provider.Close()` errors where lifecycle truth depends on them. A close error publishes cleanup incomplete/unknown and remains returned/aggregated according to existing manager call semantics.

Expose the provider fact used by the current query on `codeintel.Result`:

```go
type Result struct {
    Status          ResultStatus                     `json:"status"`
    Query           Query                            `json:"query"`
    SourceCut       SourceCut                        `json:"source_cut,omitzero"`
    Selection       SelectionMetadata                `json:"selection,omitzero"`
    Provider        ProviderMetadata                 `json:"provider,omitzero"`
    ProviderRuntime *providerobservation.Observation `json:"provider_runtime,omitempty"`
    Records         []Record                         `json:"records,omitempty"`
}
```

The service obtains `ProviderRuntime` only from `ProviderResponse.Runtime` returned by the single query. It does not ask the runtime projection to guess which provider handled the request, and it never issues a second provider query.

- [ ] **Step 1: Write RED projection tests**

Required tests:

```text
TestRuntimeProjectionKeepsOnlyLatestFactPerRuntimeID
TestRuntimeProjectionStartingAndLiveUseSameRuntimeID
TestRuntimeProjectionReplacementGetsNewRuntimeID
TestRuntimeProjectionRetainsTerminalFactUntilBoundedEviction
TestRuntimeProjectionDoesNotContainProcessAuthorityOrManagerPolicy
TestRuntimeProjectionRestartHasNoFakeContinuity
```

- [ ] **Step 2: Write RED manager lifecycle tests**

Required tests:

```text
TestProviderManagerPublishesStartingThenLiveWithStableRuntimeID
TestProviderManagerResponseCarriesTheRuntimeFactUsedByThatQuery
TestProviderManagerWarmReuseKeepsSameIncarnationRuntimeIDAndUpdatesLiveFact
TestProviderManagerIdleEvictionPublishesClosingThenTerminal
TestProviderManagerReplacementPublishesOldTerminalBeforeNewLive
TestProviderManagerUnexpectedFailurePublishesLostWithoutErasingProviderError
TestProviderManagerCloseErrorPublishesCleanupIncompleteAndReturnsError
TestProviderManagerRuntimeObservationDoesNotChangeCompatibilityKey
TestProviderManagerStillEnforcesOriginalPoolQueueCooldownLimits
```

Use test TTLs/counters; do not wait production five-minute idle TTL.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/app/codeintel ./internal/core/codeintel -run 'RuntimeProjection|ProviderManager.*Runtime|ProviderManager.*Lifecycle' -count=1
```

Expected: sink/projection/result field missing.

- [ ] **Step 4: Implement lifecycle publication without moving manager policy**

Create one helper in `ProviderManager` that performs close publication around the existing provider close call. Replace all direct `closeProviders`/`provider.Close()` eviction paths with that helper; preserve queue/admission locking and do provider `Close()` outside the manager mutex as today.

Do not import DAP/debug packages anywhere under `internal/app/codeintel`.

- [ ] **Step 5: Run GREEN/race and commit**

```bash
gofmt -w internal/app/codeintel internal/core/codeintel
go test ./internal/app/codeintel ./internal/core/codeintel -run 'RuntimeProjection|ProviderManager|RuntimeObservation|Result' -count=1
go test -race ./internal/app/codeintel -run 'RuntimeProjection|ProviderManager' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/app/codeintel internal/core/codeintel
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: publish bounded code provider runtime facts"
```

---

### Task 6: Join semantic projection at the composition boundary and expose closed P4-A output

**Files:**
- Modify: `internal/core/codeintel/semantic_projection.go`
- Modify: `internal/core/codeintel/semantic_projection_test.go`
- Modify: `internal/core/codeintel/result.go`
- Modify: `internal/core/codeintel/result_test.go`
- Modify: `cmd/shellbeam/code_intelligence.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/mcp/a1_inspect_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/a1_inspect_test.go`

**Interfaces:**

Task 2 already created the wrapper around P1's exact `AffectedSurface`; Task 6 does not redefine it and contains no derivation logic in core codeintel. It only adds the existing wrapper to the result:

```go
SemanticProjection *SemanticProjection `json:"semantic_projection,omitempty"`
```

The command/composition layer joins the pure adapter after codeintel service returns:

```go
func (a *daemonActions) InspectCode(ctx context.Context, workspaceID, activityID string, query codeintel.Query) (codeintel.Result, error) {
    result, err := a.code.Inspect(ctx, appcodeintel.InspectRequest{WorkspaceID: workspaceID, ActivityID: activityID, Query: query})
    if err != nil { return codeintel.Result{}, err }
    projection, err := verificationadapter.ProjectCodeIntelRelations(verificationadapter.CodeIntelProjectionInput{
        Result: result,
        CapturedAt: result.SourceCut.ObservedAt,
    })
    if err != nil { return codeintel.Result{}, err }
    result.SemanticProjection = toCodeIntelSemanticProjection(projection)
    return result, nil
}
```

This placement is deliberate:

```text
internal/app/codeintel does not depend on verification app/adapter packages
projector cannot call codeintel provider
composition layer may join two read-only subsystems
P2 later consumes the same P1 surface rather than a second schema
```

Wire schema:

```json
"code_semantic_projection": {
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "status":{"enum":["available","partial","unavailable"]},
    "surface":{"$ref":"#/$defs/verification_affected_surface"},
    "diagnostics":{"type":"array","maxItems":16,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":128}}
  },
  "required":["status"]
}
```

Task 0 must verify whether completed P1 already owns `$defs.verification_affected_surface`. If its exact name/shape differs, Task 0 amends this plan before execution. Under the expected landed P1 contract, Task 6 reuses that definition directly; it never creates a `codeintel_affected_surface` copy. If P1 intentionally kept the full surface internal and no shared full-surface `$defs` exists, that absence is a Task-0 binding mismatch requiring plan review rather than an implementation-time schema invention.

`provider_runtime` schema mirrors `providerobservation.Observation`. The current wire schemas already carry `$defs.telemetry_metric` + `$defs.telemetry_resources` with the same six metric names and exact `unavailable|exact|platform_reported|sampled` quality/value semantics as `receipt.ResourceEvidence`; Task 6 reuses `$defs.telemetry_resources` for `terminal_resources` rather than creating another resource schema. Add `provider_runtime_id` with pattern `^prun_[0-9A-HJKMNP-TV-Z]{26}$`.

- [ ] **Step 1: Write RED semantic-wrapper/schema tests**

Required tests:

```text
TestInspectCodeOutputIncludesSourceCutCurrentProviderRuntimeAndSemanticProjection
TestInspectCodeSemanticProjectionUsesP1AffectedSurfaceType
TestInspectCodeSemanticProjectionDoesNotIncreaseProviderQueryCount
TestInspectCodeUnavailableGenerationReportsUnavailableProjectionNotEmptyCompleteSurface
TestInspectCodeSchemaRejectsUnknownProviderRuntimeFields
TestInspectCodeSchemaRejectsUnknownSemanticProjectionFields
TestInspectCodeNeverEmitsTaskCompleteTruth
TestLegacyCapabilityProjectionDoesNotInventP4AFields
```

Use the existing counting provider from `cmd/shellbeam/command_daemon_test.go`: one `inspect.code` request must still cause exactly one provider `Query` call.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/codeintel ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'P4A|SemanticProjection|ProviderRuntime|SourceCut' -count=1
```

Expected: wrapper/wire fields missing.

- [ ] **Step 3: Implement join + closed schemas**

MCP summary remains one bounded status line; do not concatenate source previews, relation arrays or runtime observations into prose summary.

Forbidden production fields/actions in this task:

```text
task_complete
work_complete
safe_to_finish
recommended_fix
auto_run
rename
workspace_edit
code_action
debug.start
debug.attach
evaluate
setVariable
writeMemory
```

- [ ] **Step 4: Run GREEN/race and commit**

```bash
gofmt -w internal/core/codeintel cmd/shellbeam internal/adapter/ipc/protocol_v2_test.go internal/adapter/mcp
go test ./internal/core/codeintel ./internal/core/verification ./internal/core/providerobservation ./internal/adapter/verification ./internal/app/codeintel ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'P4A|InspectCode|SemanticProjection|ProviderRuntime|SourceCut|CodeIntelligence' -count=1
go test -race ./internal/app/codeintel ./cmd/shellbeam ./internal/adapter/ipc ./internal/adapter/mcp -run 'P4A|InspectCode|ProviderRuntime' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/codeintel cmd/shellbeam internal/adapter/ipc/protocol_v2_test.go internal/adapter/mcp api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: expose p4a machine truth code facts"
```

---

### Task 7: Real-gopls acceptance, resource/leak benchmark, and P4-A completion evidence

**Files:**
- Create: `cmd/shellbeam/code_intelligence_p4a_acceptance_test.go`
- Modify: `tools/benchmark-codeintel-p4a/main.go`
- Modify: `tools/benchmark-codeintel-p4a/main_test.go`
- Modify: `scripts/benchmark-codeintel-p4a.sh`
- Create: `docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-results.md`

**Interfaces:**

P4-A completion evidence records literal status independently:

```text
P4-A1 source cut/display ergonomics
P4-A2 semantic P1 relation projection
P4-A3 neutral runtime/resource fact contract
P4-A4 gopls lifecycle/runtime publication
real-gopls practical acceptance
provider cleanup/quiescence
full checkpoint
```

- [ ] **Step 1: Write the real-gopls acceptance test**

`TestP4ARealGoplsSourceRelationRuntimeAcceptance` creates an isolated git-backed temporary Go module:

```go
package p

func Target(v int) int { return v + 1 }
func Caller() int       { return Target(41) }
```

Resolve gopls through the same production factory path. If it is unavailable:

```go
if _, err := exec.LookPath("gopls"); err != nil {
    t.Skip("gopls unavailable: P4-A provider acceptance NOT_RUN")
}
```

When available, assert:

```text
resolved definition/reference/caller/callee records retain SourceRef + byte ranges
every repository resolved location with known logical path has DisplaySourceLocation
result SourceCut has exact workspace/repository IDs + generation observed before query
semantic projection source_generation equals result SourceCut generation
semantic relations use semantic_provider basis and never authoritative authority
call hierarchy domain/relations never claim complete coverage
provider_runtime provider ID/incarnation equals result Provider metadata
live provider process correlation is exact or explicitly partial/unavailable
warm queries keep same gopls incarnation
one inspect request remains one provider query
```

- [ ] **Step 2: Add source mutation honesty acceptance**

Fixture sequence:

```text
G1 query Target -> result/source cut G1
mutate p.go -> workspace generation G2
old G1 SourceRef still resolves retained G1 bytes
new query -> cut G2/new SourceRef as appropriate
no old SourceRef is rebound to G2 bytes
semantic projection from G1 remains bound to G1 and is stale relative to G2 rather than rewritten
```

Also force a changed-during-query fake/provider fixture and prove the changed record remains visible in code facts but is omitted from semantic relation projection.

- [ ] **Step 3: Add provider close/quiescence/resource acceptance**

Run at least five warm queries, capture exact gopls process identity, then close the code runtime.

Assert:

```text
runtime projection shows same live incarnation during warm reuse
close publishes closing then terminal/lost literal fact for that incarnation
root LSP child is reaped; any surviving captured exact descendant makes cleanup=incomplete; when all captured identities are gone gopls V1 still reports cleanup=unknown because exhaustive descendant closure is unproven
terminal CPU user/system + max RSS use canonical receipt resource evidence when platform reports them
read/write bytes remain unavailable unless a future canonical observer exists
process_count_peak remains unavailable for gopls V1
no 250ms provider-lifetime process-tree sampler was started
no captured root/descendant provider process survives runtime close under the same exact process identity in the no-observed-leak acceptance path; runtime fact remains cleanup=unknown with descendant-closure caveat
```

Do not wait production 5-minute idle TTL; unit tests cover idle eviction with short limits. Runtime close is the real deterministic cleanup gate.

- [ ] **Step 4: Re-run the exact Task-0 practical benchmark**

Expected semantic delta when gopls is available:

```text
display_location_count remains >= baseline (PR #12 already supplied much of this)
source_generation_present = true
provider_runtime_present = true
semantic_relation_count > 0 for reference/call scenarios that return eligible current records
same provider incarnation across warm scenarios
response remains <= existing ResultLimits.MaxResponseBytes
no source writes
no debugger/DAP child
```

There is **no universal latency target**. Record before/after wall time and response bytes. If source-cut or runtime observation materially regresses practical latency/resource use, diagnose the cause; do not weaken correctness tests just to hit an invented P99.

- [ ] **Step 5: Run negative architecture gates**

Provider-neutral package must contain no control policy:

```bash
if rg -n 'MaxInFlight|MaxQueueDepth|QueueWait|Cooldown|ProviderRequest|ProviderResponse|Query\(|WorkerCount|PoolSize|RetryCount' internal/core/providerobservation; then
  echo 'provider-neutral fact package absorbed control policy' >&2
  exit 1
fi
```

Codeintel manager must contain no DAP dependency:

```bash
if go list -deps ./internal/app/codeintel | rg '/(debug|dap)(/|$)'; then
  echo 'DAP dependency leaked into codeintel manager' >&2
  exit 1
fi
```

P4-A production query/action vocabulary must not gain mutation/debug actions:

```bash
if rg -n 'QueryRename|workspace/applyEdit|code_action_execute|semantic_refactor|debug\.start|debug\.attach|setVariable|writeMemory' \
  internal/core/codeintel internal/app/codeintel internal/adapter/codeintel cmd/shellbeam/code_intelligence.go; then
  echo 'P4-A mutation/debug surface leaked' >&2
  exit 1
fi
```

Completion-truth fields remain forbidden:

```bash
if rg -n 'json:"(task_complete|work_complete|safe_to_finish)' \
  internal/core/codeintel internal/core/providerobservation internal/adapter/verification; then
  echo 'task-completion truth leaked into P4-A' >&2
  exit 1
fi
```

- [ ] **Step 6: Run fresh targeted + repository verification**

```bash
set -euo pipefail
go test ./internal/core/codeintel ./internal/core/providerobservation ./internal/core/verification -count=1
go test ./internal/adapter/codeintel/... ./internal/app/codeintel ./internal/adapter/verification ./internal/adapter/process -count=1
go test ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'CodeIntelligence|InspectCode|P4A|SemanticProjection|ProviderRuntime|SourceCut' -count=1
go test -race ./internal/adapter/codeintel/... ./internal/app/codeintel ./internal/adapter/verification ./internal/adapter/process -count=1
go run ./tools/devctl check
go run ./tools/devctl test --base "$P4A_EXECUTION_BASE" --json
go run ./tools/devctl verify --checkpoint --base "$P4A_EXECUTION_BASE" --json
```

A completion claim requires the terminal checkpoint receipt. If the outer harness kills/times out the checkpoint, record `checkpoint=UNPROVEN` separately from targeted gates; do not convert infrastructure interruption into PASS or test failure.

- [ ] **Step 7: Write literal result evidence and commit**

`docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-results.md` contains:

```text
execution_base_sha
final_head
p1_source_fingerprint
Go/gopls identity
platform/architecture
baseline vs final benchmark JSON

P4-A1 source cut/display                  PASS | FAIL | NOT_RUN
P4-A2 semantic relation bridge            PASS | FAIL | NOT_RUN
P4-A3 neutral runtime/resource facts      PASS | FAIL | NOT_RUN
P4-A4 gopls lifecycle publication         PASS | FAIL | NOT_RUN
real-gopls practical acceptance           PASS | FAIL | NOT_RUN
provider cleanup semantics                PASS | FAIL | NOT_RUN
provider tree quiescence                   PROVEN | UNPROVEN | OBSERVED_LIVE
checkpoint                                PASS | FAIL | UNPROVEN
```

Commit only after fresh targeted/contract gates pass:

```bash
git diff --check
go run ./tools/devctl commit-gate --base "$P4A_EXECUTION_BASE" --json
git add cmd/shellbeam/code_intelligence_p4a_acceptance_test.go tools/benchmark-codeintel-p4a scripts/benchmark-codeintel-p4a.sh docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-results.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: qualify p4a code intelligence foundation"
```

---

## Spec Coverage Matrix

| Sequencing/spec contract | Plan owner | Concrete proof |
|---|---|---|
| P4-A follows completed P1 and precedes P2 | Task 0 | exact P1 execution-base binding |
| Go/gopls only | Global + Tasks 0/7 | real gopls acceptance; no extra provider |
| Reuse exact SourceRef | Task 1 | old SourceRef/G1 retention test |
| Model-facing path:line ergonomics | Task 1 | preserve existing `DisplaySourceLocation` instead of parallel model |
| Source generation separate from SourceRef | Task 1 | `SourceCut` + generation transition tests |
| Changed-during-query honesty | Tasks 1/2/7 | changed record retained but not semantically projected |
| Stable semantic affected relations | Task 2 | P1 `AffectedSurface` projector |
| authority × coverage × generation × provenance | Task 2 | projector mapping/identity tests |
| no universal semantic completeness | Task 2/7 | bounded/partial/unknown domains, call hierarchy never complete |
| no automatic semantic fan-out | Tasks 2/6 | projector has no ProviderPool + query-count acceptance |
| provider-neutral fact envelope only | Task 3 | reflection negative-policy test |
| reuse existing resource ontology | Task 3 | `receipt.ResourceEvidence` exit helper |
| avoid resource-heavy long-lived sampler | Tasks 3/4/7 | no provider 250ms process-tree ticker test |
| gopls process identity/lifecycle | Task 4 | exact/partial host process + one-shot descendant cleanup tests |
| stable starting→terminal provider runtime identity | Tasks 3/5 | `provider_runtime_id` lifecycle tests |
| bounded lifecycle publication | Task 5 | latest-state projection keyed by runtime ID + lifecycle transition tests |
| do not generalize `codeintel.ProviderManager` | Tasks 3/5/7 | neutral-package scan; manager limits unchanged |
| P6-A independent from codeintel manager | Task 7 | `go list -deps` DAP negative gate |
| bounded model-facing output | Task 6 | closed IPC/MCP schemas + existing Result limit |
| semantic output reuses P1 ontology | Task 6 | wrapper points to `verification.AffectedSurface` |
| no mutation-capable LSP actions | Global/Task 7 | production enum/source negative gate |
| no task-completion truth | Global/Tasks 6/7 | schema/field negative tests |
| practical resource/leak benchmark | Task 7 | baseline/results evidence + exact observed-process leak check; quiescence stays UNPROVEN without closure authority |
| P2/P6-A plans remain adaptive | Whole plan | no P2 or DAP implementation task here |

## Explicit Deferred Boundaries

This plan does **not** implement or freeze detailed implementation for:

```text
P6-A Delve qualification/debug-session core
P2 EngineeringStateView
P6-B debugger evidence integration
P3 Mutation Transaction
P4-B rename/workspace-edit/code-action/refactor providers
Browser full integration
additional language providers
Resource Governor concurrency selection
automatic verification execution
planner/memory/multi-agent autonomy
```

P6-A implementation planning begins only after Task 7 produces real P4-A provider/source/runtime acceptance and reviewers can inspect the contracts that actually landed.

## Plan Self-Review Gate

Before execution approval, all answers must be mechanically supportable from this plan:

1. Does P4-A create a second path/line presentation model? **No; it reuses `DisplaySourceLocation`.**
2. Can `SourceCut` rewrite/rebind an old SourceRef? **No.**
3. Does one `inspect.code` request still cause one provider query? **Yes; relation projection is post-processing only.**
4. Can a changed/unknown code record become an exact P1 relation? **No.**
5. Can semantic absence prove universal non-applicability? **No; P4-A semantic coverage is never complete V1.**
6. Does provider-neutral code own pool/queue/cooldown/session policy? **No.**
7. Does long-lived gopls run the command process-tree sampler every 250ms? **No.**
8. Are gopls CPU/RSS facts expressed in a second metric schema? **No; terminal facts reuse `receipt.ResourceEvidence`.**
9. Can PID, root reap, or a one-shot descendant snapshot prove exhaustive provider-tree cleanup? **No; gopls P4-A V1 reports `cleanup=unknown` unless it positively observes a surviving exact identity, which yields `incomplete`.**
10. Does a starting provider lose correlation identity when its real gopls incarnation appears? **No; manager-owned `provider_runtime_id` is stable across the lifecycle.**
11. Does codeintel manager become a dependency of future DAP? **No.**
12. Does P4-A add source mutation/debug actions or task-completion truth? **No.**
13. Does execution hard-bind the actually landed P1 contracts before production code? **Yes.**
14. Does provider absence auto-install gopls? **No; practical acceptance becomes NOT_RUN.**
15. Does the plan account for PR #12 already landing display/resource primitives? **Yes; it reuses rather than duplicates them.**
