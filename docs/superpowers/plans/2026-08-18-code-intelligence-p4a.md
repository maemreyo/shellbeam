# ShellBeam P4-A Read-Only Code Intelligence Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Evolve the existing Go/gopls `inspect.code` path into a read-only Machine Truth provider that exposes a bounded fast-workspace correlation cut plus exact query/target source provenance, projects only reverse/dependent semantic facts into the landed P1 affected-relation vocabulary, and publishes bounded provider lifecycle/process/resource facts for later P2/P6-A consumers.

**Architecture:** Preserve the existing exact `SourceRef + byte range` contract and reuse the `DisplaySourceLocation` ergonomics already landed on `origin/main`; P4-A adds one explicit query-start **fast workspace correlation cut**, not a whole-source content identity. Exact source-byte authority remains on retained `SourceRef` bytes and bounded non-retaining `SourceBinder.CompareCurrent` rechecks. Cross-file provider targets are revalidated locally after the single provider response, with no additional gopls query. Semantic affected projection is pure post-processing over that already-produced `codeintel.Result` and admits only reverse/dependent V1 edges. `codeintel.ProviderManager` remains the code-intelligence-specific pool/queue/cooldown owner. Shared provider work is limited to neutral fact types plus cheap process-exit resource conversion; long-lived gopls does not inherit the command sampler's 250ms process-tree polling.

**Tech Stack:** Go 1.26.x, existing `go.lsp.dev` LSP client, gopls, `workspace.FastSnapshot.Generation`, existing provider manager/source retention, P1 `verification.AffectedSurface` contracts, existing IPC/MCP v2, JSON Schema 2020-12, PR #12 process resource evidence (`receipt.ResourceEvidence`).

**Spec:** `docs/superpowers/specs/2026-08-18-p4a-p6a-sequencing-amendment-design.md`

**Planning baseline observed on 2026-08-18:** `origin/main=4d033c71272a41f0a782f034d59e65c651a6ed72`. This baseline already contains `source.DisplaySourceLocation` on resolved code locations and command-exit CPU/RSS/process-count resource observation. P1 verification semantics are **not** yet landed on this baseline, so Task 0 must bind the actual completed-P1 execution commit before any P4-A production edit.

## Global Constraints

- P4-A execution starts only after P1 Stage A + Stage B are implemented and their completion checkpoint has a terminal PASS on the exact implementation branch.
- P4-A V1 is Go/gopls only.
- P4-A is source-read-only: no rename, `workspace/applyEdit`, code-action execution, semantic refactor, source write, DAP action, or automatic verification execution.
- Existing `codeintel.SourceRef` remains canonical exact source identity. Old SourceRefs are never rebound to current path bytes.
- Existing `source.DisplaySourceLocation` is the canonical model-facing path/line/range/preview projection for resolved locations. P4-A must not introduce a parallel `SourcePresentation` location object.
- `source_generation` is a separate **fast workspace correlation cut**. It is never inserted into SourceRef identity, never used to rewrite old SourceRefs, and never treated as a content digest.
- The P4-A `SourceCut` is the fresh `workspace.FastSnapshot.Generation` observed **before** provider execution. Its comparison semantics are one-way: `G_start != G_current` proves fast-workspace divergence; `G_start == G_current` MUST NOT prove that source bytes are unchanged. Exact byte freshness comes only from exact retained `SourceRef` bytes plus bounded non-retaining `SourceBinder.CompareCurrent`, or from a future explicitly requested `ExactSourceSnapshot` contract.
- Semantic projection may use only the already-returned `codeintel.Result`; it cannot call `ProviderPool.Query`, start gopls, enumerate extra symbols, recursively follow references, or synthesize missing facts.
- Cross-file exact repository/workspace target SourceRefs are locally revalidated once, under existing result/query bounds, before `SourceCorrelation` is finalized. Both selected-source and returned-target rechecks use the same non-retaining exact comparison primitive: they read/compare/discard current bytes without `Retain`, never mint a SourceRef, never consume SourceStore budget, never perform a second provider query, and never replace the returned target SourceRef.
- P4-A V1 affected projection admits only reverse/dependent semantic facts (`referenced_by`, `called_by`). Definition, callee, type-definition and import-target navigation remain `inspect.code` facts and are not projected into P1 `AffectedSurface` V1.
- Projected affected edges use `path -> path` subjects so P1 `MatchPaths` can consume them. Opaque query/target SourceRef IDs remain exact deep-inspection handles, not path selectors, generation substitutes, or `RelationID` inputs; stable exact source-pair provenance binds semantic relation identity.
- A semantic relation is emitted only for an exact mechanically derived target record whose `SourceCorrelation=current`, an exact query-source ref, and a valid fast-workspace source cut. Mixed/changed/unknown/advisory/provider-reported records remain code facts but do not receive a P1 affected relation.
- P4-A semantic domains are conservative. V1 never claims complete semantic coverage and does not use semantic-analysis absence as mechanically complete proof of non-applicability.
- `internal/app/codeintel.ProviderManager` retains pool size, queueing, per-provider in-flight limits, idle TTL, compatibility selection, cooldown and restart policy. No shared package gains those controls.
- P6-A must not import or depend on `internal/app/codeintel.ProviderManager`, `ProviderRequest`, `ProviderResponse`, or codeintel query/session policy.
- Provider-neutral runtime types contain facts only. They provide no `Query`, `Start`, `Acquire`, `Pool`, `Queue`, retry policy, worker count, or provider-specific protocol state.
- Reuse canonical `receipt.ResourceEvidence`; do not create a second CPU/RSS/I/O/process-count metric ontology.
- Long-lived gopls V1 records CPU/RSS at process exit from `os.ProcessState`. It leaves `process_count_peak` unavailable rather than running the command resource sampler's 250ms process-tree scan for the provider lifetime.
- PID is an address, not identity. Exact provider process correlation requires existing `process.Identity`/executable identity observation; missing identity remains partial/unavailable.
- A codeintel-specific bounded **latest-state projection** may hold the latest runtime fact per `provider_runtime_id` so P2 can consume current state and tests can verify terminal cleanup. It is not a durable history, process authority store, telemetry store, scheduler, or generic provider manager.
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
P4A_EXECUTION_BASE=<exact clean commit containing completed P1>
P4A_PLAN_SHA=<commit containing this plan>
P1_COMPLETION_EVIDENCE=docs/superpowers/evidence/2026-08-18-verification-semantics-p1-results.md
P1_FINAL_HEAD=<exact P1 completion head; must equal P4A_EXECUTION_BASE>
P1_SOURCE_FINGERPRINT=<source_fingerprint from the accepted checkpoint receipt>
P1_CHECKPOINT_RECEIPT=<durable receipt reference/copy recorded by P1 evidence, or Task-0 rerun receipt>
P1_CHECKPOINT_SELECTION=<literal selection from checkpoint receipt>
P1_CHECKPOINT_STATUS=passed
P1_CHECKPOINT_EXIT=0
P1_CHECKPOINT_PROOF_SOURCE=durable_p1_evidence|task0_single_rerun
```

`P1_CHECKPOINT_*` is prerequisite authority, not benchmark metadata. `devctl check`, `devctl test`, traceability, or later P4-A tests cannot substitute for a terminal successful P1 checkpoint.

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

- [ ] **Step 1: Hard-bind the real completed-P1 prerequisite and terminal checkpoint proof**

From a clean P4-A implementation worktree:

```bash
set -euo pipefail
test -z "$(git status --porcelain)"
P4A_EXECUTION_BASE="$(git rev-parse HEAD)"
P4A_PLAN_SHA="$(git log -n1 --format=%H -- docs/superpowers/plans/2026-08-18-code-intelligence-p4a.md)"
P1_COMPLETION_EVIDENCE="docs/superpowers/evidence/2026-08-18-verification-semantics-p1-results.md"

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

The **preferred** prerequisite proof is the committed P1 completion artifact. Accept it only if `P1_COMPLETION_EVIDENCE` contains a durable copy/record of the exact Task-10 checkpoint receipt and exposes enough literal data to verify all of:

```text
P1_FINAL_HEAD == P4A_EXECUTION_BASE
checkpoint.schema_version == 1
checkpoint.command == verify
checkpoint.source_fingerprint == P1_SOURCE_FINGERPRINT
checkpoint.status == passed
checkpoint.exit_code == 0
checkpoint.selection == full
checkpoint.started_at and checkpoint.finished_at are present
checkpoint receipt/reference is recorded
```

The durable checkpoint object is the normal `tools/devctl.Evidence` JSON emitted by `devctl verify --checkpoint --json`; do not create a second checkpoint schema. Current `devctl` does **not** persist argv or a first-class `checkpoint` verification mode in this receipt. V1 therefore proves the required terminal **full-verification checkpoint** by the semantics actually persisted: `command == verify` and `selection == full`. A `verify --dirty` receipt with `selection=affected` is insufficient even if it passed. `base` is retained literally as command provenance but is not interpreted as the verified HEAD. `P1_FINAL_HEAD` is the VCS identity that binds the receipt to the exact completed P1 tree. If a future receipt adds an authoritative verification-mode field, a later reviewed plan may bind that field directly; P4-A V1 must not infer literal argv that was not recorded.

When the durable artifact passes those checks, extract its literal values into `P1_FINAL_HEAD`, `P1_SOURCE_FINGERPRINT`, `P1_CHECKPOINT_RECEIPT`, `P1_CHECKPOINT_SELECTION`, `P1_CHECKPOINT_STATUS`, and `P1_CHECKPOINT_EXIT` **before** the next command. If those values cannot be extracted mechanically, treat durable evidence as unverifiable and take the single-rerun path below; do not guess them from prose.

Recompute only the **current source fingerprint** without using a test run as checkpoint authority:

```bash
P1_EXPLAIN_JSON="$(go run ./tools/devctl explain --base "$P4A_EXECUTION_BASE" --json)"
P1_CURRENT_SOURCE_FINGERPRINT="$(printf '%s' "$P1_EXPLAIN_JSON" | python3 -c 'import json,sys; print(json.load(sys.stdin)["source_fingerprint"])')"
test "$P1_CURRENT_SOURCE_FINGERPRINT" = "$P1_SOURCE_FINGERPRINT"
```

If the committed P1 completion artifact is absent, malformed, missing any field above, names another final HEAD, names another source fingerprint, or otherwise cannot be mechanically verified, **rerun the P1 completion checkpoint exactly once** on the still-clean `P4A_EXECUTION_BASE`:

```bash
set -euo pipefail
test "$(git rev-parse HEAD)" = "$P4A_EXECUTION_BASE"
test -z "$(git status --porcelain)"
P1_CHECKPOINT_JSON="$(go run ./tools/devctl verify --checkpoint --base "$P4A_EXECUTION_BASE" --json)"
printf '%s\n' "$P1_CHECKPOINT_JSON" > .build/p4a-p1-checkpoint-rerun.json
python3 - <<'PY_CHECKPOINT'
import json, pathlib
p = pathlib.Path('.build/p4a-p1-checkpoint-rerun.json')
r = json.loads(p.read_text())
assert r['schema_version'] == 1
assert r['command'] == 'verify'
assert r['status'] == 'passed'
assert r['exit_code'] == 0
assert r['source_fingerprint']
assert r['selection'] == 'full'
assert r['started_at'] and r['finished_at']
PY_CHECKPOINT
P1_FINAL_HEAD="$P4A_EXECUTION_BASE"
P1_SOURCE_FINGERPRINT="$(python3 -c 'import json; print(json.load(open(".build/p4a-p1-checkpoint-rerun.json"))["source_fingerprint"])')"
P1_CHECKPOINT_STATUS=passed
P1_CHECKPOINT_EXIT=0
P1_CHECKPOINT_SELECTION="$(python3 -c 'import json; print(json.load(open(".build/p4a-p1-checkpoint-rerun.json"))["selection"])')"
P1_CHECKPOINT_RECEIPT=.build/p4a-p1-checkpoint-rerun.json
P1_CHECKPOINT_PROOF_SOURCE=task0_single_rerun
```

A failed/interrupted rerun closes P4-A with `NOT_RUN: p1_checkpoint_unproven`; there is no blind second retry. If durable evidence validates, set `P1_CHECKPOINT_PROOF_SOURCE=durable_p1_evidence` and do **not** rerun the checkpoint. In both cases, freeze the exact accepted checkpoint fields plus `P1_FINAL_HEAD` into the Task-0 baseline evidence before any P4-A dirty change exists.

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
  --path p/target.go --line 3 --column 7
```

and terminates/reaps only the daemon process it started. It must not `pkill gopls`, `killall`, or delete unrelated runtime/state dirs.

- [ ] **Step 5: Capture the before-state baseline**

Use an isolated Go fixture committed in its own temporary git repo. Keep the dependency origin and dependent in different files so the before/after benchmark exercises the exact cross-file path used by Task 7:

`p/target.go`:

```go
package p

func Target(v int) int { return v + 1 }
```

`p/caller.go`:

```go
package p

func Caller() int { return Target(41) }
```

Record:

```text
execution_base_sha
plan_sha
p1_completion_evidence
p1_final_head
p1_source_fingerprint
p1_checkpoint_receipt
p1_checkpoint_selection
p1_checkpoint_status
p1_checkpoint_exit
p1_checkpoint_proof_source
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

### Task 1: Add one fast-workspace `SourceCut`, exact query-source identity, and bounded cross-file target revalidation

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
- Modify: `internal/app/codeintel/result_normalization.go`
- Modify: `internal/app/codeintel/result_normalization_test.go` if split tests exist on the landed execution base; otherwise keep focused cases in `service_test.go`
- Modify: `internal/adapter/codeintel/sourcefs/binder.go`
- Modify: `internal/adapter/codeintel/sourcefs/binder_test.go`
- Modify: `cmd/shellbeam/code_intelligence.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/command_daemon_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/a1_inspect_test.go`

**Interfaces:**

Do **not** add a second path/line presentation type. Existing resolved locations already carry exact retained source identity plus the PR #12 display projection:

```go
type ResolvedSourceLocation struct {
    SourceRefID string                        `json:"source_ref_id"`
    StartByte   int64                         `json:"start_byte"`
    EndByte     int64                         `json:"end_byte"`
    Display     *source.DisplaySourceLocation `json:"display,omitempty"`
}
```

Add one result-level **fast workspace correlation cut**, and keep the positioned query's exact source identity separate:

```go
package codeintel

import (
    "time"
    workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type SourceCut struct {
    RepositoryID   workspace.RepositoryID       `json:"repository_id,omitempty"`
    WorkspaceID    workspace.WorkspaceID        `json:"workspace_id,omitempty"`
    Generation     string                       `json:"generation,omitempty"`
    Quality        workspace.ObservationQuality `json:"quality"`
    ObservedAt     time.Time                    `json:"observed_at"`
    DiagnosticCode string                       `json:"diagnostic_code,omitempty"`
}

type Result struct {
    Status           ResultStatus      `json:"status"`
    Query            Query             `json:"query"`
    SourceCut        SourceCut         `json:"source_cut,omitzero"`
    QuerySourceRefID SourceRefID       `json:"query_source_ref_id,omitempty"`
    Selection        SelectionMetadata `json:"selection,omitzero"`
    Provider         ProviderMetadata  `json:"provider,omitzero"`
    Records          []Record          `json:"records,omitempty"`
}
```

`QuerySourceRefID` is **not** part of `SourceCut`. For a positioned query, it is the exact selected source ref used to form the provider request. It is omitted when no single exact query source exists. It grants no mutation/process authority and is validated only with existing `ParseSourceRefID` rules.

Generation validation reuses workspace identity syntax. If the landed P1 execution base has not already exported an equivalent helper, Task 1 adds exactly:

```go
func ValidateGeneration(value string) error {
    if !validGeneration(value) {
        return fmt.Errorf("invalid workspace generation")
    }
    return nil
}
```

to `internal/core/workspace/snapshot.go`; `SourceCut.Validate` and P1/P4-A consumers call this shared validator rather than each reimplementing the `gen_` grammar. If Task 0 discovers P1 already landed the same semantic helper under another exact name, the plan must be amended at Task 0 instead of adding an alias.

`SourceCut` semantics are deliberately weaker than exact source identity:

```text
SourceCut.Generation = workspace.FastSnapshot.Generation
SourceCut != ExactSourceSnapshot
SourceCut does not hash current file bytes

G_start != G_current
  -> definite fast-workspace divergence

G_start == G_current
  -> no conclusion about source-byte equality
  -> MUST NOT promote stale evidence/relations to current

exact byte freshness
  -> retained SourceRef bytes + bounded non-retaining current comparison
  -> or a future explicit ExactSourceSnapshot contract
```

Validation:

```text
zero SourceCut is allowed only for pre-P4-A internal fixtures during migration tests;
non-zero SourceCut requires observed_at and valid quality;
quality unavailable -> generation empty + diagnostic_code required;
quality fresh|cached|stale -> repository/workspace/generation present and valid;
SourceCut never contains path/line/symbol bytes or SourceRef IDs;
non-empty QuerySourceRefID must be a valid SourceRefID.
```

App port stays narrow:

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

`Service` stores `snapshots WorkspaceSnapshotSource`; production construction requires it non-nil. Existing codeintel runtime constructors gain the same dependency. `Service.Inspect` calls `ObserveFresh(ctx, workspace.Root)` **once, after workspace validation and before the provider query**, converts it into `SourceCut`, then performs the existing source selection/binding and exactly one `ProviderPool.Query`.

For positioned queries, after binding the query path and before calling the provider, freeze the selected exact source ID into `QuerySourceRefID`. Do not reconstruct it later from provider output.

#### Non-retaining exact source comparison

Current `sourcefs.Binder.Bind` is not a read-only freshness probe: it calls `SourceStore.Retain`, and `Retain` mints a new opaque `src_<ULID>` even when bytes are unchanged. Therefore Task 1 SHALL NOT use `Bind` for either the existing selected-source recheck or the new cross-file target recheck. Observation must not mutate source-retention state.

Extend the existing app port rather than creating a second binder service:

```go
type SourceComparison string

const (
    SourceSame        SourceComparison = "same"
    SourceChanged     SourceComparison = "changed"
    SourceUnavailable SourceComparison = "unavailable"
)

type SourceBinder interface {
    Bind(context.Context, workspace.Workspace, string) (BoundSource, error)
    Resolve(core.SourceRefID) (BoundSource, SourceRefState)
    CompareCurrent(context.Context, workspace.Workspace, BoundSource) (SourceComparison, error)
}
```

`sourcefs.Binder.CompareCurrent` reuses the same safe relative-path, `openat`/no-follow, size, UTF-8, file-identity and TOCTOU checks used by `Bind`, but its terminal dataflow is exactly:

```text
open current exact source
  -> bounded read
  -> stable-path/identity verification
  -> compare with expected BoundSource.Bytes
  -> discard current bytes
  -> ZERO SourceRetention.Retain calls
  -> ZERO new SourceRef IDs
  -> ZERO SourceStore eviction pressure
```

`SourceSame` means the safely re-read bytes exactly equal the expected retained bytes. A detected path/file transition or byte mismatch returns `SourceChanged`; missing/unreadable/unsupported observations return `SourceUnavailable`. Existing typed errors remain available as diagnostic causes, but callers branch on the closed comparison result rather than treating an arbitrary read failure as proof of change.

Task 1 replaces current `recheckSelected`'s `binder.Bind(...)` call with this primitive as well. This is required even without cross-file projection: otherwise every ordinary selected-source freshness check would continue allocating duplicate retained SourceRefs.

#### Stable exact derivation fingerprint for location pairs

Opaque SourceRef allocation identity remains useful for deep inspection but is not semantic identity. For an exact positioned query plus an exact resolved location target, Task 1 computes a relation-scoped source-pair fingerprint before the pure P4-A2 projector runs:

```go
type LocationTarget struct {
    Name                       string         `json:"name,omitempty"`
    Relationship               string         `json:"relationship"`
    Location                   SourceLocation `json:"location"`
    ExactSourcePairFingerprint string         `json:"exact_source_pair_fingerprint,omitempty"`
}
```

Fingerprint construction itself is bounded work. Current production allows an 8 MiB exact source file, 128 result records, and 8 MiB of selected-source bytes; repeating an 8 MiB query endpoint in raw-byte canonical JSON for many target pairs would otherwise create request-scale work far larger than the response budget. P4-A therefore extends `ServiceLimits` with one request-local source-pair fingerprint work budget:

```go
type ServiceLimits struct {
    // existing fields remain unchanged
    Delta                         workspace.DeltaLimits
    Result                        core.ResultLimits
    MaxSelectedSources            int
    MaxSelectedSourceBytes        int64
    MaxSourcePairFingerprintBytes int64
    MaxDuration                   time.Duration
}

type sourcePairFingerprintBudget struct {
    remaining int64
}

func (b *sourcePairFingerprintBudget) TryCharge(queryBytes, targetBytes int) bool
```

V1 production composition sets `MaxSourcePairFingerprintBytes: 16 << 20`: exactly twice the existing 8 MiB `MaxSelectedSourceBytes` and enough for one pair containing two maximum-size 8 MiB endpoints. `NewService` validates the field in `1..64<<20`; tests may use smaller limits. This is a **per-inspect request** budget created fresh before pair construction; it is not global state and does not survive a request.

For every previously unseen exact source pair, compute `charge = int64(len(query.Bytes)) + int64(len(target.Bytes))` with overflow-safe arithmetic and call `TryCharge` **before** `json.Marshal` or SHA-256. Repeated records for the same exact request-local `(query SourceRefID, target SourceRefID, query path, target path)` may reuse the already-computed pair fingerprint without another charge; those opaque IDs are cache keys only and never semantic identity. A failed charge performs zero marshal/hash work for that pair. Because the only unbounded-size JSON fields are the two charged `[]byte` fields, Go's base64 encoding and the subsequent single SHA-256 pass are a constant-factor expansion of source bytes already admitted by this budget; path/fixed-field sizes remain bounded by existing source/path contracts.

Budget exhaustion is fail-conservative and observable:

```text
pair charge would exceed remaining budget
  -> ExactSourcePairFingerprint = ""
  -> preserve literal SourceCorrelation (it may still be current)
  -> result.Status = partial
  -> diagnostic_code = source_pair_fingerprint_budget_exceeded
  -> P4-A2 omits that record from AffectedRelation projection
  -> semantic_dependents domain coverage = partial because otherwise-eligible pairs were omitted by the request-local work budget
```

The service must not fall back to SourceRef ULIDs, fast generation, path-only provenance, or an unbudgeted alternate hash when the budget is exhausted.

Canonical fingerprint input and encoding are closed for P4-A V1:

```go
type exactSourcePairFingerprintInput struct {
    SchemaVersion int                    `json:"schema_version"` // exactly 1
    RepositoryID  workspace.RepositoryID `json:"repository_id"`
    WorkspaceID   workspace.WorkspaceID  `json:"workspace_id"`
    QueryPath     string                 `json:"query_path"`
    QueryBytes    []byte                 `json:"query_bytes"`
    TargetPath    string                 `json:"target_path"`
    TargetBytes   []byte                 `json:"target_bytes"`
}

func exactSourcePairFingerprint(exactSourcePairFingerprintInput) (string, error)
```

The helper is called only after the request-local work budget successfully charges that exact pair. It validates repository/workspace IDs and both already-normalized safe repository-relative logical paths, then calls `encoding/json.Marshal` on **that exact closed struct** (no maps, no `omitempty`, no extra fields). Go JSON's deterministic `[]byte` base64 representation is therefore part of schema version 1. Hash the resulting JSON bytes with SHA-256 and return `csp_` + 64 lowercase hex characters. Any field-set/order/encoding change requires a reviewed schema-version bump; executors may not substitute a different canonicalization.

It deliberately excludes `QuerySourceRefID`, target `SourceRefID`, timestamps, provider incarnation and fast workspace generation. Two independently allocated SourceRefs over identical exact bytes therefore produce the same pair fingerprint; a byte change in either endpoint changes it. The fingerprint is **not** a public per-file content hash, cannot resolve source bytes, and must never be described as `source_content_digest` or `ExactSourceSnapshot`. It exists only as stable provenance for one semantic source-pair derivation, preserving the E29 rule against inventing deterministic per-file hashes merely to identify locations.

Populate it only when both query and target are exact repository/workspace BoundSources for the current workspace and their correlation is `current`. Missing/changed/unknown endpoints leave it empty and therefore ineligible for P4-A2 relation projection.

#### Bounded cross-file target revalidation

Current main can promote a cross-file gopls target into an exact returned SourceRef, but `correlationForLocation` currently recognizes only selected input refs. Task 1 fixes that **inside codeintel result truth**, before P4-A2 projection.

After `promoteObservedLocations(...)` returns and before record normalization:

```text
already-bounded provider response
  -> collect unique resolved SourceRef IDs from returned repository/workspace locations
  -> at most ResultLimits.MaxRecords unique target refs
  -> no provider query
  -> no recursive navigation
  -> all work remains inside existing query context / MaxDuration
```

For each candidate target ref:

```text
retained, state = binder.Resolve(target_ref)

state != current
or retained ref is not exact, lacks safe logical path,
or repository/workspace does not match current workspace
  -> target correlation = unknown

comparison, err = binder.CompareCurrent(workspace, retained)

comparison == SourceChanged
  -> source_changed_during_query

comparison == SourceSame
  -> current

comparison == SourceUnavailable
or other comparison/observation failure
  -> unknown
```

`SourceRefID` equality is **not** the comparison: SourceRef IDs are retention identities. `CompareCurrent` compares exact retained/current bytes without allocating another identity. Preserve the original returned target SourceRef and byte range in the record; revalidation only supplies correlation truth and, when both endpoints are exact/current, the stable source-pair fingerprint.

Do not duplicate existing selected-source work. Build `selectedIDs` first; returned target IDs already present in `selectedIDs` continue to use the existing selected-source recheck. Only exact returned IDs outside that set enter the cross-file/local target recheck. Freeze the dataflow explicitly:

```go
type SourceCorrelations map[core.SourceRefID]core.SourceCorrelation

type TargetCorrelation = SourceCorrelations

func (s *Service) recheckSelected(
    ctx context.Context,
    workspace workspace.Workspace,
    selected []BoundSource,
) (SourceCorrelations, bool /* anyNonCurrent */)

func (s *Service) recheckReturnedTargets(
    ctx context.Context,
    workspace workspace.Workspace,
    response ProviderResponse,
    selectedIDs map[core.SourceRefID]struct{},
) (SourceCorrelations, bool /* boundedOrUnknown */)

func correlationForLocation(
    location core.SourceLocation,
    selectedIDs map[core.SourceRefID]struct{},
    selectedCorrelations SourceCorrelations,
    returnedTargets SourceCorrelations,
) core.SourceCorrelation
```

Both selected and returned-target rechecks preserve the full closed comparison state:

```text
CompareCurrent == SourceSame
  -> CorrelationCurrent

CompareCurrent == SourceChanged
  -> CorrelationSourceChangedDuringQuery

CompareCurrent == SourceUnavailable
or comparison cannot complete
  -> CorrelationUnknown
  -> result degrades to partial
```

A selected SourceRef is **never** considered current merely because it is present in `selectedIDs`; `selectedIDs` is membership/deduplication only. `correlationForLocation` first returns the exact `selectedCorrelations` value for selected refs, then consults `returnedTargets` for non-selected exact results; absent entries remain `CorrelationUnknown`. `normalizeRecords` receives both frozen correlation maps and never binds files itself.

The service caller must stop using the old `len(changedRefs) != 0` rule. The replacement is literal:

```go
selectedCorrelations, selectedNonCurrent := s.recheckSelected(queryCtx, workspace, selected)
if barriersDiffer(before, after) || selectedNonCurrent {
    degradeSelectionForBarrier(&selection)
}
```

`selectedNonCurrent` is false only when every selected source was rechecked `CorrelationCurrent`; it is true for any `CorrelationSourceChangedDuringQuery`, `CorrelationUnknown`, incomplete/bounded recheck, or comparison error. Presence of current entries in the map alone never degrades the result.

If either selected or target collection/recheck hits the existing result bound, query deadline, or cannot finish a candidate, affected candidates become `CorrelationUnknown` and the code result degrades to `partial`; it never upgrades unknown to current. No freshness recheck path is allowed to call `Bind` merely to compare bytes. Exact source-pair fingerprints may be populated only when **both** query/selected and target correlations are `CorrelationCurrent`; selected `unknown|changed` therefore prevents a fingerprint and prevents P4-A2 relation projection.

- [ ] **Step 1: Write failing core/source-cut and one-way generation tests**

Required tests:

```text
TestSourceCutKeepsFastGenerationSeparateFromSourceRefAndExactSnapshot
TestFastGenerationEqualityDoesNotProveSourceByteEquality
TestSourceCutDifferentGenerationProvesFastWorkspaceDivergenceOnly
TestResultQuerySourceRefValidatesSeparatelyFromSourceCut
```

The equality test uses two exact source byte states that intentionally share the same `FastSnapshot` facts/generation and proves no API/helper interprets generation equality as byte freshness.

Also test unavailable cut, bad generation, missing diagnostic, missing IDs, zero timestamp and unsafe diagnostic text.

- [ ] **Step 2: Write failing service/correlation tests before implementation**

Required tests:

```text
TestInspectCodeCapturesOneFreshFastWorkspaceCutBeforeProviderQuery
TestInspectCodeSourceCutUnavailableDoesNotInventGeneration
TestInspectCodePositionedQueryFreezesExactQuerySourceRefBeforeProviderQuery
TestInspectCodeSelectedSourceChangedDuringQueryKeepsStartCutAndChangedCorrelation
TestInspectCodeCrossFileExactTargetRecheckMarksCurrentWhenRetainedBytesMatch
TestInspectCodeCrossFileTargetByteChangeMarksSourceChangedDuringQueryEvenWhenFastGenerationEqual
TestInspectCodeCrossFileTargetRecheckFailureRemainsUnknownAndPartial
TestInspectCodeCrossFileTargetRecheckIsBoundedByResultLimit
TestInspectCodeCrossFileRecheckDoesNotIncreaseProviderQueryCount
TestInspectCodeSelectedCompareCurrentSameIsCurrentAndDoesNotDegradeSelection
TestInspectCodeSelectedCompareCurrentChangedIsSourceChangedDuringQueryAndDegradesSelection
TestInspectCodeSelectedCompareCurrentUnavailableIsUnknownPartialAndProducesNoSourcePairFingerprint
TestInspectCodeSelectedRecheckDoesNotRetainDuplicateSourceRefs
TestInspectCodeCrossFileRecheckDoesNotRetainDuplicateSourceRefs
TestSourcePairFingerprintUsesExactClosedV1CanonicalJSON
TestSourcePairFingerprintWorkChargeUsesQueryPlusTargetBytesAndRejectsOverflow
TestSourcePairFingerprintBudgetExhaustionSkipsMarshalHashAndDegradesPartial
TestSourcePairFingerprintRepeatedExactPairUsesRequestLocalCacheWithoutRecharge
TestSourcePairFingerprintStableAcrossOpaqueSourceRefReallocation
TestSourcePairFingerprintChangesWhenEitherExactEndpointBytesChange
TestInspectCodeOldSourceRefStillResolvesRetainedBytesAfterWorkspaceChanges
TestResolvedRepositoryLocationsUseExistingDisplaySourceLocation
TestProviderReportedLocationStillDoesNotInventResolvedDisplayOrSourceRef
```

Use a two-file fake provider fixture (`target.go`, `caller.go`). The provider returns an exact resolved target SourceRef outside the selected query-source set. Prove local revalidation changes only `SourceCorrelation`; the record retains the original target SourceRef and one caller request still causes exactly one provider `Query`.

- [ ] **Step 3: Run RED**

```bash
go test ./internal/core/workspace ./internal/core/codeintel ./internal/app/codeintel ./internal/adapter/codeintel/sourcefs ./cmd/shellbeam -run 'SourceCut|FastGeneration|QuerySourceRef|SourcePair|FingerprintBudget|CompareCurrent|CrossFile|SelectedRecheck|SelectedCompareCurrent|InspectCode.*Generation|DisplaySourceLocation' -count=1
```

Expected: new SourceCut/query-source/non-retaining tri-state correlation/source-pair contracts are missing.

- [ ] **Step 4: Implement source cut, query-source identity and bounded target recheck**

Reuse the existing daemon `workspaceObserver`; do not create a second observer. Extend the existing `SourceBinder` with `CompareCurrent`; do not create a source-ref-to-path resolver service. Implement the safe read/compare path in `sourcefs.Binder` without calling `Retain`. Add `MaxSourcePairFingerprintBytes` to `ServiceLimits`, set production composition in `cmd/shellbeam/code_intelligence.go` to `16 << 20`, and construct one `sourcePairFingerprintBudget` per inspect request. Keep target revalidation, request-local pair-cache/budget accounting, and source-pair fingerprint construction in `internal/app/codeintel`, not the gopls adapter and not the verification projector.

For resolved locations, preserve the PR #12 contract already required by Task 0: `DisplaySourceLocation` is generated from retained/synchronized bytes where exact. A missing PR #12 display primitive is a Task-0 `plan_binding_mismatch`, not an implementation branch inside Task 1.

- [ ] **Step 5: Extend closed wire schemas additively**

Add `$defs.code_source_cut` once in each applicable v2 schema with the existing repository/workspace/generation/quality/timestamp/diagnostic shape. Add optional `source_cut` and optional `query_source_ref_id` (`^src_[0-9A-HJKMNP-TV-Z]{26}$`) to the code result. Extend the existing location-target object with optional `exact_source_pair_fingerprint` using the closed `^csp_[0-9a-f]{64}$` form chosen by the core helper; this is relation-scoped derivation provenance, not a SourceRef or per-file source digest. Do not put `query_source_ref_id` inside `code_source_cut`, and do not change/remove existing `resolved.display` schema.

- [ ] **Step 6: Run GREEN/race and commit**

```bash
gofmt -w internal/core/workspace/snapshot.go internal/core/workspace/snapshot_test.go internal/core/codeintel internal/app/codeintel internal/adapter/codeintel/sourcefs cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go
go test ./internal/core/workspace ./internal/core/codeintel ./internal/app/codeintel ./internal/adapter/codeintel/sourcefs ./internal/adapter/codeintel/gopls ./cmd/shellbeam ./api/schema -run 'Generation|SourceCut|QuerySourceRef|SourcePair|FingerprintBudget|CompareCurrent|CrossFile|InspectCode|DisplaySourceLocation|CodeIntelligence' -count=1
go test -race ./internal/app/codeintel ./internal/adapter/codeintel/sourcefs ./internal/adapter/codeintel/gopls ./cmd/shellbeam -run 'SourceCut|SourcePair|FingerprintBudget|CompareCurrent|CrossFile|CodeIntelligence' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/workspace/snapshot.go internal/core/workspace/snapshot_test.go internal/core/codeintel internal/app/codeintel internal/adapter/codeintel/sourcefs cmd/shellbeam/code_intelligence.go cmd/shellbeam/command_daemon.go cmd/shellbeam/command_daemon_test.go api/schema/ipc-v2.json api/schema/mcp-output-v2.json api/schema/a1_inspect_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: bind code intelligence source correlation"
```

---
### Task 2: Project only reverse/dependent semantic edges into P1 `AffectedSurface`

**Files:**
- Modify: `internal/core/verification/relation.go`
- Modify: `internal/core/verification/relation_test.go`
- Create: `internal/core/codeintel/semantic_projection.go`
- Create: `internal/core/codeintel/semantic_projection_test.go`
- Create: `internal/adapter/verification/codeintel_relations.go`
- Create: `internal/adapter/verification/codeintel_relations_test.go`

**Interfaces:**

Extend only the P1 relation/domain vocabularies needed to label bounded semantic dependent derivation:

```go
const (
    BasisSemanticProvider RelationBasis = "semantic_provider"
    DomainSemanticDependents AffectedDomainKind = "semantic_dependents"
)
```

Core codeintel owns only the model-facing wrapper around P1's exact surface type:

```go
package codeintel

import verification "github.com/maemreyo/shellbeam/internal/core/verification"

type SemanticProjectionStatus string

const (
    SemanticProjectionAvailable   SemanticProjectionStatus = "available"
    SemanticProjectionPartial     SemanticProjectionStatus = "partial"
    SemanticProjectionUnavailable SemanticProjectionStatus = "unavailable"
)

type SemanticProjection struct {
    Status      SemanticProjectionStatus      `json:"status"`
    Surface     *verification.AffectedSurface `json:"surface,omitempty"`
    Diagnostics []string                      `json:"diagnostics,omitempty"`
}
```

The verification adapter owns derivation and consumes a completed result with no provider/query/binder interface:

```go
package verification

type CodeIntelProjectionInput struct {
    Result     codeintel.Result
    CapturedAt time.Time
}

func ProjectCodeIntelRelations(
    input CodeIntelProjectionInput,
) (codeintel.SemanticProjection, error)
```

Projection status is closed:

```text
query kind is not references|callers
  -> unavailable, Surface=nil, diagnostic `affected_projection_not_applicable`

invalid/unavailable SourceCut or missing QuerySourceRefID for a reverse-dependent query
  -> unavailable, no Surface fabricated

result status unavailable|failed
  -> unavailable

result status stale|partial, or correlation/recheck uncertainty
  -> partial; any retained domain remains non-complete

ready result with valid exact eligible facts
  -> available; semantic_dependents coverage still at most bounded
```

The output's `Surface` is the landed P1 `AffectedSurface`; domains, relations, subjects, provider refs, `DomainID` and `RelationID` remain P1 types. P4-A does not define a parallel affected ontology.

### V1 affected-edge boundary

`inspect.code` remains the richer navigation surface. P4-A2 projects only reverse/dependent relations that can widen an affected path set without reversing dependency direction:

```text
QueryReferences + relationship=reference
  -> relation_kind = referenced_by

QueryCallers + relationship=caller
  -> relation_kind = called_by
```

The following remain code/navigation facts only and MUST NOT enter P1 `AffectedSurface` V1:

```text
definition
type_definition
callee
resolved_import_target
import_declaration
symbol/type-summary navigation
```

Current gopls explicitly reports `QueryImportDeclarations` and `QueryResolvedImportTargets` unsupported; P4-A2 therefore defines no import affected mapping without a qualified producer. A future producer requires plan/spec review of relation direction before promotion.

The reverse relation is relative to the query anchor; it does **not** claim the query path is itself currently changed. P2 may later join these relations with an independently mechanical changed/affected seed. P4-A never turns an arbitrary navigation query into user-task affected truth by itself.

### Eligibility and subjects

A relation is eligible only when all are true:

```text
result SourceCut valid (fast-workspace correlation only)
result QuerySourceRefID is valid
query kind is references or callers
record kind is location_target
record relationship matches the exact reverse mapping above
record authority == mechanical
record SourceCorrelation == current
record target is resolved exact SourceRef
record target has DisplaySourceLocation with safe repository-relative path
record target has a valid ExactSourcePairFingerprint produced by Task 1
target display path != result.Query.Path (self-path navigation is not a widening edge)
```

Anything else remains visible in `inspect.code` but is omitted from the affected projection. Provider-reported/advisory targets are not downgraded into advisory affected edges in P4-A V1.

Subjects are path-addressable for P1 policy matching:

```text
from = path:<result.Query.Path>
to   = path:<target resolved.display.path>
```

No `source_ref` subject is used as the affected target and no `symbol` subject is invented.

Exact byte/provenance binding is separate from path selection and from opaque SourceRef allocation identity. P1 `RelationID` hashes `ProvenanceRefs`, so P4-A MUST NOT place `src_<ULID>` values in those refs. P4-A V1 freezes the provider derivation identity as well:

```go
type semanticProviderDerivationIdentity struct {
    SchemaVersion        int    `json:"schema_version"` // exactly 1
    ProviderID           string `json:"provider_id"`
    ExecutableVersion    string `json:"executable_version"`
    ConfigFingerprint    string `json:"config_fingerprint"`
    BuildFingerprint     string `json:"build_fingerprint"`
    BuildQuality         string `json:"build_quality"`
    SemanticScopeQuality string `json:"semantic_scope_quality"`
}

func semanticProviderDerivationFingerprint(codeintel.ProviderMetadata) (string, error)
```

The helper copies **exactly** those six ProviderMetadata fields plus schema version, validates them with the existing metadata contract, marshals that exact closed struct with `encoding/json.Marshal` (no maps/`omitempty`), hashes the JSON with SHA-256, and returns `cpd_` + 64 lowercase hex characters. `SemanticScopeQuality` is included because current gopls uses it to declare semantic scope compatibility (`workspace_root`). `Incarnation` is excluded because it is runtime allocation identity. `Coverage` is excluded because coverage is already an independent P1 `RelationID` dimension. Timestamps/runtime IDs/query counters are excluded. There are no executor-selected "admissible" provider fields in V1.

Relation provenance is therefore exactly:

```text
codeintel_exact_source_pair:<record.location_target.exact_source_pair_fingerprint>
codeintel_provider:<semanticProviderDerivationFingerprint(result.Provider)>
codeintel_query:<sha256(JSON-marshal of the validated closed Query struct)>
```

The opaque exact handles remain available separately for deep inspection through `result.QuerySourceRefID` and `record.target.resolved.source_ref_id`; they are **not** inputs to `RelationID`. The enclosing `inspect.code` result also preserves literal `ProviderMetadata.Incarnation` for runtime/deep inspection, but incarnation is not semantic relation identity.

Relation-ID stability is scoped to the contract P1 actually froze. **Holding every other P1 `RelationID` input constant, including `SourceGeneration`, authority, coverage, basis, provider ref, endpoints and kind**:

```text
same repository/workspace + same query/target paths + same exact endpoint bytes
+ same semantic query/provider derivation
+ newly allocated opaque SourceRef ULIDs only
  -> same source-pair/provider/query provenance
  -> stable RelationID

changed exact bytes at either endpoint
  -> changed source-pair provenance
  -> changed RelationID

changed SourceGeneration with endpoint bytes otherwise unchanged
  -> RelationID changes because SourceGeneration is itself a P1 identity dimension
```

P4-A guarantees stability against **opaque SourceRef allocation churn**, not across workspace-generation cuts. `SourceGeneration` remains the fast-workspace correlation cut and MUST NOT be interpreted as content equality. The pair fingerprint is relation-scoped provenance, not a substitute for `ExactSourceSnapshot.source_content_digest`.

Provider identity remains:

```go
verification.ProviderRef{ID: "codeintel/go_semantic", Version: 1}
```

Authority is `mechanical`; P4-A V1 never emits authoritative semantic edges. Coverage is conservative:

```text
ready + stable/current eligible records -> bounded at strongest
partial result/selection/provider sync -> partial
stale/unknown/recheck uncertainty -> unknown or partial
semantic_dependents domain is never complete in P4-A V1
zero matching relations may emit a non-complete semantic_dependents domain
```

Therefore relation/domain absence cannot mechanically prove policy non-applicability.

- [ ] **Step 1: Write RED affected-direction/provenance tests**

Required tests:

```text
TestCodeIntelProjectionRequiresValidFastSourceCutAndExactQuerySourceRef
TestCodeIntelReferenceProjectsPathToReferencedByPath
TestCodeIntelCallerProjectsPathToCalledByPath
TestCodeIntelCrossFileCurrentTargetSurvivesProjection
TestCodeIntelReferenceSelfPathIsNavigationOnlyNotAffectedEdge
TestCodeIntelDefinitionIsNavigationOnlyNotAffected
TestCodeIntelCalleeIsNavigationOnlyNotAffected
TestCodeIntelTypeDefinitionIsNavigationOnlyNotAffected
TestCodeIntelUnsupportedImportKindsHaveNoAffectedMapping
TestCodeIntelProviderReportedOrAdvisoryTargetIsNotAffected
TestCodeIntelChangedOrUnknownTargetIsNotAffected
TestCodeIntelSelectedUnknownProducesNoSemanticRelation
TestCodeIntelFingerprintBudgetExhaustionOmitsSemanticRelationAndDegradesCoverage
TestCodeIntelAffectedRelationProvenanceUsesStableExactSourcePairNotOpaqueSourceRefIDs
TestCodeIntelProviderDerivationIdentityUsesExactClosedFieldSetAndExcludesIncarnationCoverage
TestCodeIntelAffectedRelationIDStableAcrossOpaqueSourceRefReallocationWhenExactBytesAndGenerationUnchanged
TestCodeIntelAffectedRelationIDChangesWhenEitherExactEndpointBytesChangeEvenIfFastGenerationEqual
TestCodeIntelAffectedRelationIDChangesAcrossSourceGenerationEvenWhenExactEndpointsUnchanged
TestCodeIntelSemanticDependentsNeverClaimsCompleteCoverage
TestCodeIntelProjectionEmitsNonCompleteDomainWithZeroRelations
TestCodeIntelProjectorHasNoProviderPoolBinderOrQueryDependency
```

For the dependency test, compile the projector with only a `codeintel.Result`; source-scan the production projector for `ProviderPool`, `ProviderRequest`, `SourceBinder`, `.Bind(` and provider `Query(` calls.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/verification ./internal/core/codeintel ./internal/adapter/verification -run 'CodeIntel|SemanticDependents|ReferencedBy|CalledBy' -count=1
```

Expected: new domain/basis/projector mappings are missing.

- [ ] **Step 3: Implement deterministic reverse-dependent projector**

Construct the P1 path subjects from the already-validated query path and exact target display path. Build sorted/deduplicated provenance refs **only** from the closed source-pair/provider/query derivation fingerprints defined above; never include `QuerySourceRefID`, target `SourceRefID`, provider incarnation or runtime identity in `ProvenanceRefs`. Then call the P1 identity helper:

```go
relationID, err := verification.RelationID(verification.RelationIdentityInput{
    From:                verification.Subject{Kind: verification.SubjectPath, Value: result.Query.Path},
    To:                  verification.Subject{Kind: verification.SubjectPath, Value: targetPath},
    Kind:                relationKind,
    Basis:               verification.BasisSemanticProvider,
    DerivationAuthority: verification.AuthorityMechanical,
    Coverage:            coverage,
    Provider:            &providerRef,
    SourceGeneration:    result.SourceCut.Generation,
    ProvenanceRefs:      provenance,
})
```

Sort/deduplicate projected relations by `RelationID`. Never use timestamp, preview text or diagnostic prose in identity. Never derive a forward edge by reversing a returned result after the fact.

- [ ] **Step 4: Run GREEN/race and commit**

```bash
gofmt -w internal/core/verification/relation.go internal/core/verification/relation_test.go internal/core/codeintel/semantic_projection.go internal/core/codeintel/semantic_projection_test.go internal/adapter/verification/codeintel_relations.go internal/adapter/verification/codeintel_relations_test.go
go test ./internal/core/verification ./internal/core/codeintel ./internal/adapter/verification -run 'CodeIntel|SemanticProjection|SemanticDependents|Relation' -count=1
go test -race ./internal/adapter/verification -run 'CodeIntel' -count=1
go run ./tools/devctl test --dirty --base "$P4A_EXECUTION_BASE" --json
git add internal/core/verification/relation.go internal/core/verification/relation_test.go internal/core/codeintel/semantic_projection.go internal/core/codeintel/semantic_projection_test.go internal/adapter/verification/codeintel_relations.go internal/adapter/verification/codeintel_relations_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: project semantic dependent relations"
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

Replace the one-consumer `waitCh` authority with one cached terminal result plus a broadcast-only done channel:

```go
type processExit struct {
    err     error
    runtime ProcessRuntime
}

type Session struct {
    // existing protocol/process fields...
    exitMu    sync.RWMutex
    exit      processExit
    exitReady bool
    done      chan struct{}
}
```

The wait goroutine is the only writer:

```text
cmd.Wait()
  -> copy ProcessState
  -> derive ExitResourceEvidence once
  -> freeze processExit under exitMu
  -> set exitReady
  -> close(done) exactly once
```

`waitForProcess()` waits on `done` and then reads the cached `processExit`; `ProcessRuntime()` reads the same cached state and never receives from a consumable result channel. On timeout, existing kill fallback still waits on `done` before reading the cached result. Multiple/concurrent readers therefore observe one immutable terminal authority instead of racing to consume it. Before terminal completion, `ProcessRuntime()` may return the frozen started PID/StartedAt with `Reaped=false` but no terminal fields. `Session.Close()` retains its existing Shutdown/Exit/stdin-close/wait/kill-fallback semantics.

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
TestLSPSessionWaitAndProcessRuntimeReadSameCachedExitWithoutCompeting
TestLSPSessionConcurrentProcessRuntimeReadersSeeSameTerminalState
TestLSPSessionProviderPathDoesNotStartPeriodicProcessTreeSampler
TestLSPSessionCloseKeepsExistingShutdownExitKillFallback
```

`TestLSPSessionProviderPathDoesNotStartPeriodicProcessTreeSampler` injects a process/resource hook counter or uses the extracted helper boundary to prove no `resourceSampleInterval` ticker belongs to LSP session.

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
    Status           ResultStatus                     `json:"status"`
    Query            Query                            `json:"query"`
    SourceCut        SourceCut                        `json:"source_cut,omitzero"`
    QuerySourceRefID SourceRefID                      `json:"query_source_ref_id,omitempty"`
    Selection        SelectionMetadata                `json:"selection,omitzero"`
    Provider         ProviderMetadata                 `json:"provider,omitzero"`
    ProviderRuntime  *providerobservation.Observation `json:"provider_runtime,omitempty"`
    Records          []Record                         `json:"records,omitempty"`
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
TestProviderManagerIdleEvictionPreservesRuntimeIDThroughClosingAndTerminal
TestProviderManagerReplacementPreservesOldRuntimeIDUntilTerminalBeforeNewLive
TestProviderManagerCloseSnapshotsManagedProviderNotBareProvider
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

Current eviction helpers return bare `Provider` values and therefore discard manager-owned correlation. Change the internal close handoff to preserve the exact `managedProvider` (or an equivalent private close candidate containing both `Provider` and `runtimeID`):

```go
func (m *ProviderManager) collectExpiredIdleLocked(now time.Time) []*managedProvider
func (m *ProviderManager) evictIncompatibleIdleLocked(key providerKey) []*managedProvider
func (m *ProviderManager) evictOldestIdleLocked() *managedProvider

func (m *ProviderManager) closeManagedProviders(
    ctx context.Context,
    providers []*managedProvider,
) error
```

`ProviderManager.Close` likewise snapshots `[]*managedProvider`, not `[]Provider`, before dropping map membership. The private close candidate keeps `runtimeID`, provider metadata/incarnation and provider pointer long enough to publish `closing -> terminal|lost` with the same `prun_X`. It grants no new process authority.

Create one helper that performs close publication around the existing provider close call. Replace all direct `closeProviders`/`provider.Close()` eviction/replacement/manager-close paths with that helper; preserve queue/admission locking and perform provider `Close()` outside the manager mutex as today. Start-failure cleanup also keeps the reserved runtime ID until its literal terminal/lost fact is published.

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

### Task 7: Real-gopls cross-file acceptance, resource/leak benchmark, and P4-A completion evidence

**Files:**
- Create: `cmd/shellbeam/code_intelligence_p4a_acceptance_test.go`
- Modify: `tools/benchmark-codeintel-p4a/main.go`
- Modify: `tools/benchmark-codeintel-p4a/main_test.go`
- Modify: `scripts/benchmark-codeintel-p4a.sh`
- Create: `docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-results.md`

**Interfaces:**

P4-A completion evidence records independently:

```text
P4-A1 fast source cut + exact query/cross-file correlation
P4-A2 reverse/dependent semantic P1 relation projection
P4-A3 neutral runtime/resource fact contract
P4-A4 gopls lifecycle/runtime publication
real-gopls practical acceptance
provider cleanup semantics
provider tree quiescence
full checkpoint
```

- [ ] **Step 1: Write real-gopls two-file source/relation acceptance**

`TestP4ARealGoplsSourceRelationRuntimeAcceptance` creates an isolated git-backed temporary Go module with the dependency origin and dependent in different files:

`target.go`:

```go
package p

func Target(v int) int { return v + 1 }
```

`caller.go`:

```go
package p

func Caller() int { return Target(41) }
```

Resolve gopls through the production factory. If unavailable, record/skip as `NOT_RUN`; never install automatically.

Anchor the references and callers queries on `Target` in `target.go`. When gopls is available, assert:

```text
one inspect request -> one provider Query
result QuerySourceRefID is the exact selected target.go SourceRef
cross-file caller.go result is exact resolved SourceRef + byte range + DisplaySourceLocation(path=caller.go)
Task-1 local target recheck marks caller.go SourceCorrelation=current when retained/current bytes match
references projection emits target.go -> caller.go referenced_by
callers projection emits target.go -> caller.go called_by
affected relation subjects are path -> path
relation provenance contains the stable exact source-pair fingerprint and contains no opaque query/target SourceRef IDs
semantic relation SourceGeneration equals the fast SourceCut generation but is not treated as content digest
definition/callee/type-definition records remain inspect.code navigation facts and are absent from P1 affected relations
semantic dependent domain/relations never claim complete coverage
provider_runtime provider ID/incarnation equals result Provider metadata
warm queries keep same gopls incarnation/runtime ID
```

The acceptance must fail if a cross-file exact target falls back to `CorrelationUnknown` solely because it was not in the originally selected source set.

- [ ] **Step 2: Add source-mutation and fast-generation equality honesty acceptance**

Prove both dimensions independently:

```text
fast workspace cut:
  G_start != G_current -> divergence may be reported
  G_start == G_current -> never proves bytes current

exact target/query authority:
  old SourceRef retains old exact bytes
  non-retaining CompareCurrent compares current path bytes to retained bytes without minting SourceRefs
```

Include a fixture where target bytes change while the Git fast snapshot facts/generation intentionally remain equal. Assert the cross-file record becomes `source_changed_during_query` (or `unknown` if exact recheck cannot finish), never `current` because generations compare equal.

Also prove:

```text
old SourceRef still resolves retained old bytes
new query may produce a new SourceRef without rebinding the old one
old code result retains its original query/target SourceRef deep handles
unchanged exact endpoint bytes across newly allocated SourceRefs keep relation identity stable **only while SourceGeneration and every other P1 RelationID input are also unchanged**
changed SourceGeneration changes RelationID even when endpoint bytes and all other derivation inputs are unchanged
changed exact endpoint bytes produce a new source-pair fingerprint and relation identity even when fast generation is equal
changed/unknown record remains visible as code fact but is omitted from P1 affected projection
source-pair fingerprint work budget exhaustion leaves current code facts visible, marks result partial, emits source_pair_fingerprint_budget_exceeded, and omits only unproven semantic relations
```

- [ ] **Step 3: Add provider close/quiescence/resource acceptance**

Run at least five warm queries, capture exact gopls process identity and stable `provider_runtime_id`, then close the code runtime.

Assert:

```text
runtime projection shows same live runtime ID/incarnation during warm reuse
eviction/runtime close preserves that runtime ID through closing -> terminal|lost
waitForProcess and ProcessRuntime read the same cached processExit authority
root LSP child is reaped; any surviving captured exact descendant makes cleanup=incomplete
when all captured identities are gone gopls V1 still reports cleanup=unknown because exhaustive descendant closure is unproven
terminal CPU user/system + max RSS use canonical receipt resource evidence when platform reports them
read/write bytes remain unavailable unless a future canonical observer exists
process_count_peak remains unavailable for gopls V1
no 250ms provider-lifetime process-tree sampler was started
```

Do not wait production idle TTL; focused unit tests exercise eviction with short limits. Runtime close is the practical cleanup observation gate, not proof of exhaustive tree quiescence.

- [ ] **Step 4: Re-run the exact Task-0 practical benchmark**

Expected delta when gopls is available:

```text
display_location_count remains >= baseline
source_generation_present = true, interpreted only as fast-workspace cut
provider_runtime_present = true
references/callers cross-file scenarios have semantic_relation_count > 0 when exact current targets exist
definition/callee scenarios may have navigation records but MUST NOT gain affected semantic relations
same provider incarnation/runtime ID across warm scenarios
response remains <= existing ResultLimits.MaxResponseBytes
no source writes
no debugger/DAP child
```

There is **no universal latency target**. Record before/after wall time and response bytes. If source correlation/runtime observation materially regresses practical latency/resource use, diagnose the cause; do not weaken correctness tests to hit an invented P99.

- [ ] **Step 5: Run negative architecture/direction gates**

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

Affected projector must not admit forward/navigation-only mappings:

```bash
if rg -n 'semantic_(definition|callee|import_target)|QueryDefinition|QueryCallees|QueryResolvedImportTargets' internal/adapter/verification/codeintel_relations.go; then
  echo 'forward navigation leaked into P4-A affected projection' >&2
  exit 1
fi
```

P4-A production query/action vocabulary must not gain mutation/debug actions:

```bash
if rg -n '"(rename|workspace_edit|code_action|debug\.start|debug\.attach|evaluate|setVariable|writeMemory)"' \
  internal/core/codeintel internal/app/codeintel cmd/shellbeam/code_intelligence.go \
  api/schema/ipc-v2.json api/schema/mcp-output-v2.json; then
  echo 'mutation/debug vocabulary leaked into P4-A production surface' >&2
  exit 1
fi
```

Completion-truth fields remain forbidden on the P4-A production surface:

```bash
if rg -n '(task_complete|work_complete|safe_to_finish)' \
  internal/core/codeintel internal/app/codeintel cmd/shellbeam/code_intelligence.go \
  api/schema/ipc-v2.json api/schema/mcp-output-v2.json; then
  echo 'task-completion truth leaked into P4-A production surface' >&2
  exit 1
fi
```


- [ ] **Step 6: Run fresh targeted + repository verification**

```bash
set -euo pipefail
go test ./internal/core/codeintel ./internal/core/providerobservation ./internal/core/verification -count=1
go test ./internal/adapter/codeintel/... ./internal/app/codeintel ./internal/adapter/verification ./internal/adapter/process -count=1
go test ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'CodeIntelligence|InspectCode|P4A|CrossFile|SemanticProjection|ProviderRuntime|SourceCut' -count=1
go test -race ./internal/adapter/codeintel/... ./internal/app/codeintel ./internal/adapter/verification ./internal/adapter/process -count=1
go run ./tools/devctl check
go run ./tools/devctl test --base "$P4A_EXECUTION_BASE" --json
go run ./tools/devctl verify --checkpoint --base "$P4A_EXECUTION_BASE" --json
```

A P4-A completion claim requires its own terminal checkpoint receipt. If the outer harness kills/times out that checkpoint, record `checkpoint=UNPROVEN` separately from targeted gates; do not convert infrastructure interruption into PASS or test failure.

- [ ] **Step 7: Write literal result evidence and commit**

`docs/superpowers/evidence/2026-08-18-code-intelligence-p4a-results.md` contains:

```text
execution_base_sha
final_head
p1_final_head
p1_source_fingerprint
p1_checkpoint_receipt/status/exit/proof_source
Go/gopls identity
platform/architecture
baseline vs final benchmark JSON

P4-A1 fast cut + exact correlation             PASS | FAIL | NOT_RUN
P4-A2 reverse semantic affected bridge          PASS | FAIL | NOT_RUN
P4-A3 neutral runtime/resource facts             PASS | FAIL | NOT_RUN
P4-A4 gopls lifecycle publication                PASS | FAIL | NOT_RUN
real-gopls cross-file practical acceptance       PASS | FAIL | NOT_RUN
provider cleanup semantics                       PASS | FAIL | NOT_RUN
provider tree quiescence                         PROVEN | UNPROVEN | OBSERVED_LIVE
checkpoint                                       PASS | FAIL | UNPROVEN
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
| P4-A follows completed P1 and precedes P2 | Task 0 | exact P1 final HEAD + durable/rerun terminal checkpoint PASS binding |
| Go/gopls only | Global + Tasks 0/7 | real gopls acceptance; no extra provider |
| Reuse exact SourceRef | Task 1 | old SourceRef/G1 retention test |
| Model-facing path:line ergonomics | Task 1 | preserve existing `DisplaySourceLocation` instead of parallel model |
| Fast workspace generation separate from exact source bytes | Task 1 | one-way `SourceCut` comparison + bounded non-retaining `CompareCurrent` tests |
| Cross-file/current changed-during-query honesty | Tasks 1/2/7 | bounded target recheck; changed/unknown record retained but not projected |
| True affected-edge direction | Task 2 | references/callers reverse dependents only; path→path P1 projection |
| authority × coverage × fast generation × exact provenance | Task 2 | path selectors + stable exact source-pair provenance; opaque SourceRefs excluded from RelationID |
| no universal semantic completeness | Task 2/7 | semantic dependents bounded/partial/unknown, never complete |
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

1. Does Task 0 require an actual terminal P1 checkpoint PASS bound to the exact P1 final HEAD/source fingerprint? **Yes; `devctl test` is not a substitute, and unverifiable durable evidence gets at most one checkpoint rerun.**
2. Does P4-A create a second path/line presentation model? **No; it reuses `DisplaySourceLocation`.**
3. Is `SourceCut.Generation` an exact source-content identity? **No; it is only a fast workspace correlation cut. Inequality proves divergence; equality proves nothing about bytes.**
4. Where does exact byte freshness come from? **Retained SourceRef bytes plus bounded non-retaining `SourceBinder.CompareCurrent`; a future `ExactSourceSnapshot` only when explicitly required.**
5. Can a cross-file exact target become `current` without being in the original selected source set? **Yes, but only after bounded local retained/current byte revalidation; no second provider query.**
6. Does local cross-file revalidation replace/rebind the returned target SourceRef? **No; it only labels correlation.**
7. Does one `inspect.code` request still cause one provider query? **Yes.**
8. Which navigation facts enter P1 affected projection V1? **Only exact mechanical reverse dependents: referenced-by and called-by.**
9. Can definition, callee, type-definition or import-target navigation widen P1 affected paths in V1? **No.**
10. Are affected endpoints usable by P1 `MatchPaths`? **Yes; projected subjects are path→path. Opaque query/target SourceRefs remain separate deep handles, while RelationID provenance uses the stable exact source-pair fingerprint.**
11. Can changed/unknown/advisory/provider-reported code facts become P1 affected relations? **No.**
12. Can semantic absence prove universal non-applicability? **No; P4-A semantic-dependent coverage is never complete V1.**
13. Does provider-neutral code own pool/queue/cooldown/session policy? **No.**
14. Does long-lived gopls run the command process-tree sampler every 250ms? **No.**
15. Are gopls CPU/RSS facts expressed in a second metric schema? **No; terminal facts reuse `receipt.ResourceEvidence`.**
16. Can PID alone become exact provider process identity? **No.**
17. Can `waitForProcess()` consume the only terminal result before `ProcessRuntime()` reads it? **No; one cached `processExit` is broadcast by a closed `done` channel.**
18. Can manager eviction discard `provider_runtime_id` before closing/terminal publication? **No; close handoff retains `managedProvider`/runtime ID.**
19. Does codeintel manager become a dependency of future DAP? **No.**
20. Does P4-A add source mutation/debug actions or task-completion truth? **No.**
21. Does execution hard-bind the actually landed P1 contracts before production code? **Yes.**
22. Does provider absence auto-install gopls? **No; practical acceptance becomes NOT_RUN.**
23. Does the plan account for PR #12 already landing display/resource primitives? **Yes; it reuses rather than duplicates them.**
