# ShellBeam Execution Telemetry and Reproduction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Do not use subagents for this repository session. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement A4 / E23 Execution Performance & Resource Telemetry and E24 Reproduction Capsules so ShellBeam can expose bounded empirical execution history and immutable exactly-once reproduction provenance without changing terminal truth, adding command-admission tax, or claiming hermetic reproducibility.

**Architecture:** Reuse the durable E21/E22 substrate already present on `ai/execution-observation`. Telemetry is a deterministic derived record scheduled only after a terminal receipt is durable; the first implementation derives wall/input/output/outcome facts from existing immutable reservation, receipt, and terminal-session metadata and advertises resource metrics as unavailable unless a process-tree observer can prove stronger semantics. Reproduction is an explicit exactly-once materialization whose single durable create record contains the immutable capsule, so lost-response retry cannot select a richer later cut. Read-only inspection is added as closed action branches of the existing one-tool `local_shell` surface.

**Tech Stack:** Go 1.26.x; standard library crypto/encoding/runtime/time packages; existing `github.com/oklog/ulid/v2`; existing file-backed store, E21 observation journal, E22 structured-result store, IPC v2 and MCP v2 adapters. Add no direct dependency.

## Global Constraints

- Design authority is `docs/superpowers/specs/2026-08-14-execution-telemetry-reproduction-design.md` plus `docs/superpowers/specs/2026-08-14-agent-execution-observation-roadmap-design.md`.
- Preserve one MCP tool, `local_shell`; add action/query branches only.
- Terminal receipt, raw output, retry identity, evidence validity, and process ownership remain authoritative. Telemetry/repro failure must never rewrite them.
- A terminal execution produces at most one logical telemetry sample for one telemetry derivation contract. Retry/recovery upserts/replays the same derivation identity and cannot double-count a sample.
- `ExecutionFingerprint` is the initial execution-semantics identity. Human command/project labels never merge changed execution semantics.
- Telemetry collection is after durable terminal publication and non-blocking with respect to command admission/spawn/output polling. Historical aggregation runs only on explicit inspection.
- No E23 resource metric is advertised stronger than the currently proven process-tree observer. The initial core advertises CPU, RSS, I/O, and process-count resource metrics as `unavailable`; zero is never substituted for unavailable.
- Resource enforcement remains experimental and is not implemented by this plan.
- Repro creation is explicit and exactly-once under caller-provided `repro_create_id`; one accepted create freezes one immutable consistency cut.
- A repro capsule is provenance, not a tarball, source snapshot, execution replay, environment bootstrap, or `reproducible=true` claim.
- Repro records never copy raw environment values, stdin bytes/hashes, source file contents, checkpoint payloads/private identities, credentials, private keys, or raw network payloads.
- Command capture is conservative: the capsule always binds exact operation/receipt/execution fingerprints. Bounded argv metadata may be copied only when it passes the explicit safe-argument policy; otherwise command-detail completeness becomes `partial` and raw argv is omitted.
- Dynamic reference resolution is separate from immutable creation descriptors. Compaction/purge cannot mutate the historical descriptor.
- Telemetry retention is hard bounded and cohort-neutral: age/count/per-key eviction is chronological and does not preferentially retain successes or discard failures/timeouts/outliers.
- Keep `.codegraph` unchanged. Do not create/switch worktrees or branches. Do not push, open a PR, merge, or clean/reset unrelated state.
- Every implementation task follows RED -> minimal GREEN -> focused regression/race gate -> `devctl` affected gate -> `git diff --check` -> scoped local commit.
- The exact full-suite baseline at `c0624d8` exhibited one host-load `git_status_timeout` in `internal/adapter/git/TestDeltaTrackedModification`; the same focused test passed 5/5 immediately afterward. Do not mix an unrelated Git-timeout change into A4. Distinguish any future full-suite occurrence from A4 regressions using focused reruns.

---

## File Structure

### Shared/core contracts

- Create `internal/core/receipt/digest.go` and `digest_test.go` — deterministic validated receipt identity for derived records.
- Create `internal/core/telemetry/record.go` and `record_test.go` — E23 sample identity, metric quality, compatibility key, validation, privacy/bounds.
- Create `internal/core/telemetry/summary.go` and `summary_test.go` — deterministic percentile/outcome summary contract.
- Create `internal/core/repro/capsule.go` and `capsule_test.go` — immutable E24 create request, capsule, capture matrix, reference descriptors, validation.
- Modify `internal/core/capability/catalog.go` and tests — telemetry/repro features, advertised hard limits, explicit resource-observation support.
- Modify `internal/core/failure/failure.go` and tests — stable E23/E24 failure codes.
- Modify `internal/core/observation/event.go` and tests — `telemetry_changed` and `repro_recorded` event kinds.

### Durable store

- Create `internal/adapter/store/telemetry.go`, `telemetry_private.go`, `telemetry_test.go` — unique sample files, bounded scans, compatibility selection, chronological retention, E21 observation publication.
- Create `internal/adapter/store/repro.go`, `repro_private.go`, `repro_test.go` — one atomic durable create record per `repro_create_id`, exact replay/conflict behavior, bounded lookup/retention, E21 observation publication.
- Modify `internal/adapter/store/repository.go` — dedicated mutexes, directories, A4 default limits.
- Modify `internal/adapter/store/execution_observation.go` — reconcile telemetry/repro prepared observations.

### Application layer

- Create `internal/app/telemetry/ports.go`, `service.go`, `service_test.go`, `worker.go`, `worker_test.go`, `inspect.go`, `inspect_test.go` — derive once after terminal, inspect compatible history, deterministic summaries.
- Create `internal/app/repro/ports.go`, `service.go`, `service_test.go`, `inspect.go`, `inspect_test.go`, `privacy.go`, `privacy_test.go` — materialize an immutable current cut and resolve current reference availability separately.
- Modify `internal/app/daemon/types.go`, `service.go` and focused tests — schedule telemetry after durable terminal publication; no aggregation/admission work.

### Public transport/composition

- Modify `internal/app/bridge/client_port.go`, bridge tests — typed telemetry/repro request/response carriage.
- Modify `internal/adapter/ipc/protocol_v2.go`, `server_unix.go`, `client_unix.go` plus focused tests — closed `inspect.telemetry`, `repro.create`, and `inspect.repro` branches.
- Modify `internal/adapter/mcp/input.go`, `call.go` plus focused tests — same three branches through the existing `local_shell` tool.
- Modify `api/schema/ipc-v2.json`, `api/schema/mcp-input-v2.json`, `api/schema/mcp-output-v2.json` plus schema tests — closed wire contract.
- Modify `cmd/shellbeam/command_daemon.go` and `execution_observation_test.go` — compose stores/services/workers and prove restart/no-tax behavior.

---

### Task 1: Freeze E23/E24 core contracts

**Files:**
- Create: `internal/core/receipt/digest.go`
- Create: `internal/core/receipt/digest_test.go`
- Create: `internal/core/telemetry/record.go`
- Create: `internal/core/telemetry/record_test.go`
- Create: `internal/core/telemetry/summary.go`
- Create: `internal/core/telemetry/summary_test.go`
- Create: `internal/core/repro/capsule.go`
- Create: `internal/core/repro/capsule_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`
- Modify: `internal/core/observation/event.go`
- Modify: `internal/core/observation/event_test.go`

**Interfaces:**
- Consumes: existing `receipt.Receipt`, `operation.ID`, `workspace.RepositoryID`, `workspace.WorkspaceID`, `session.Outcome`.
- Produces:
  - `receipt.Digest(Receipt) (string, error)`.
  - `telemetry.DerivationKey(receiptDigest string, producer Producer, schemaVersion int, configDigest string) (string, error)`.
  - `telemetry.CompatibilityKey(PerformanceRecord) (string, error)`.
  - `telemetry.PerformanceRecord.Validate() error`.
  - `telemetry.Summarize([]PerformanceRecord) (Summary, error)`.
  - `repro.CreateRequest.Fingerprint() (string, error)`.
  - `repro.Capsule.Validate() error`.
  - feature flags `execution_telemetry`, `reproduction_capsules` and resource-support fields that default to unavailable.

- [ ] **Step 1: Write receipt-digest and telemetry identity RED tests**

```go
func TestDigestAndDerivationIdentityAreStable(t *testing.T) {
    rec := validTerminalReceipt()
    a, err := receipt.Digest(rec)
    if err != nil { t.Fatal(err) }
    b, err := receipt.Digest(rec)
    if err != nil || a != b || len(a) != 64 { t.Fatalf("digest %q %q err=%v", a, b, err) }

    producer := telemetry.Producer{ProducerID: "shellbeam.telemetry", ProducerVersion: 1, CapabilityVersion: 1}
    first, err := telemetry.DerivationKey(a, producer, 1, strings.Repeat("b", 64))
    if err != nil { t.Fatal(err) }
    second, _ := telemetry.DerivationKey(a, producer, 1, strings.Repeat("b", 64))
    if first != second { t.Fatalf("unstable derivation key") }
}
```

Also assert that changing the receipt, producer version, or config digest changes the key; invalid/non-terminal receipts cannot produce a digest used as a terminal sample.

- [ ] **Step 2: Write telemetry record/compatibility/metric-quality RED tests**

```go
func TestPerformanceRecordKeepsExecutionSemanticsInCompatibilityKey(t *testing.T) {
    first := validPerformanceRecord()
    second := first
    second.DerivationKey = strings.Repeat("c", 64)
    second.CommandSemanticsFingerprint = strings.Repeat("d", 64)
    a, _ := telemetry.CompatibilityKey(first)
    b, _ := telemetry.CompatibilityKey(second)
    if a == b { t.Fatal("changed execution semantics merged") }
}

func TestUnavailableResourceMetricCannotPretendZeroIsObserved(t *testing.T) {
    record := validPerformanceRecord()
    zero := int64(0)
    record.Resources.MaxRSSBytes = telemetry.IntMetric{Quality: telemetry.MetricUnavailable, Value: &zero}
    if record.Validate() == nil { t.Fatal("unavailable metric accepted a value") }
}
```

The valid record contains non-negative wall/output/input counters, terminal outcome, stable platform/architecture fields, `Lifecycle=terminal`, and explicit completeness.

- [ ] **Step 3: Write deterministic summary RED tests**

```go
func TestSummaryIsDeterministicAndRetainsFailureTimeoutCohorts(t *testing.T) {
    records := []telemetry.PerformanceRecord{
        sample(100, session.Success),
        sample(200, session.Failure),
        sample(300, session.Timeout),
        sample(400, session.Success),
    }
    got, err := telemetry.Summarize(records)
    if err != nil { t.Fatal(err) }
    if got.Samples != 4 || got.OutcomeCounts.Failure != 1 || got.OutcomeCounts.Timeout != 1 { t.Fatalf("%#v", got) }
    if got.WallMS.P50 != 200 || got.WallMS.P95 != 400 { t.Fatalf("percentiles=%#v", got.WallMS) }
}
```

Use one documented nearest-rank integer percentile method; no prediction/regression boolean exists.

- [ ] **Step 4: Write repro immutability/capture/privacy-shape RED tests**

```go
func TestCreateFingerprintBindsTargetAndPolicy(t *testing.T) {
    request := repro.CreateRequest{CreateID: "repro-create-1", OperationID: "op-1", Policy: repro.CapturePolicy{DependentDerivations: repro.CaptureCurrent}}
    first, err := request.Fingerprint()
    if err != nil { t.Fatal(err) }
    changed := request
    changed.OperationID = "op-2"
    second, _ := changed.Fingerprint()
    if first == second { t.Fatal("target operation not bound") }
}

func TestCapsuleHasNoRawEnvironmentOrInputContentFields(t *testing.T) {
    capsuleType := reflect.TypeOf(repro.Capsule{})
    for _, forbidden := range []string{"EnvironmentValues", "Stdin", "SourceContents", "CheckpointContents"} {
        if _, ok := capsuleType.FieldByName(forbidden); ok { t.Fatalf("forbidden field %s", forbidden) }
    }
}
```

Validate independent capture states, immutable creation descriptor fields, bounded results list, safe IDs/text, exact receipt/execution fingerprints, and a separate `ResolutionState` type that is not persisted inside the immutable descriptor.

- [ ] **Step 5: Write capability/failure/event RED tests**

Assert baseline catalog reports A4 unavailable, `WithExecutionTelemetry(...)` and `WithReproductionCapsules(...)` advertise exact limits, resource observation defaults each metric to `unavailable`, public failure serialization exposes only allowed safe details, and `telemetry_changed` / `repro_recorded` validate as E21 event kinds.

- [ ] **Step 6: Run Task 1 RED gate**

Run:

```bash
go test \
  ./internal/core/receipt \
  ./internal/core/telemetry \
  ./internal/core/repro \
  ./internal/core/capability \
  ./internal/core/failure \
  ./internal/core/observation \
  -count=1
```

Expected: FAIL because the new A4 packages/contracts do not exist yet.

- [ ] **Step 7: Implement the minimal core contracts**

Receipt digest:

```go
func Digest(rec Receipt) (string, error) {
    if err := rec.Validate(); err != nil || !rec.State.Terminal() {
        return "", fmt.Errorf("invalid terminal receipt")
    }
    data, err := json.Marshal(rec)
    if err != nil { return "", err }
    sum := sha256.Sum256(data)
    return hex.EncodeToString(sum[:]), nil
}
```

Telemetry identity must hash a closed canonical identity struct; compatibility identity includes repository/non-repository scope, command semantics, optional command-definition/parameter scope, toolchain/environment schema-aware fingerprints, platform, architecture, and telemetry schema version. Do not include `project_command_id` as grouping authority.

Core resource metrics use:

```go
type MetricQuality string
const (
    MetricExact MetricQuality = "exact"
    MetricPlatformReported MetricQuality = "platform_reported"
    MetricSampled MetricQuality = "sampled"
    MetricUnavailable MetricQuality = "unavailable"
)

type IntMetric struct {
    Quality MetricQuality `json:"quality"`
    Value   *int64        `json:"value,omitempty"`
}
```

`MetricUnavailable` requires `Value == nil`; all observed values are non-negative.

Repro v1 uses a closed current-cut policy:

```go
type DependentDerivationPolicy string
const CaptureCurrent DependentDerivationPolicy = "current"

type CapturePolicy struct {
    DependentDerivations DependentDerivationPolicy `json:"dependent_derivations"`
}
```

This freezes current pending/terminal/absent states without unbounded waiting. It does not silently imply future terminalization.

- [ ] **Step 8: Run focused GREEN and race**

```bash
go test \
  ./internal/core/receipt \
  ./internal/core/telemetry \
  ./internal/core/repro \
  ./internal/core/capability \
  ./internal/core/failure \
  ./internal/core/observation \
  -count=1

go test -race ./internal/core/telemetry ./internal/core/repro -count=1
```

Expected: PASS.

- [ ] **Step 9: Run affected gate and diff checks**

```bash
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git status --short -- .codegraph
```

Expected: affected gate PASS; diff check PASS; `.codegraph` blank.

- [ ] **Step 10: Commit Task 1**

```bash
git add \
  internal/core/receipt/digest.go internal/core/receipt/digest_test.go \
  internal/core/telemetry internal/core/repro \
  internal/core/capability/catalog.go internal/core/capability/catalog_test.go \
  internal/core/failure/failure.go internal/core/failure/failure_test.go \
  internal/core/observation/event.go internal/core/observation/event_test.go
git diff --cached --check
git commit -m "feat: define execution telemetry and repro contracts"
```

---

### Task 2: Persist unique bounded telemetry samples

**Files:**
- Create: `internal/adapter/store/telemetry.go`
- Create: `internal/adapter/store/telemetry_private.go`
- Create: `internal/adapter/store/telemetry_test.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/execution_observation.go`
- Modify: `internal/adapter/store/observation_test.go`

**Interfaces:**
- Consumes: `telemetry.PerformanceRecord`, `telemetry.CompatibilityKey`, E21 prepare/commit/abort observation primitives.
- Produces:
  - `PutPerformanceRecord(context.Context, telemetry.PerformanceRecord) error`.
  - `GetPerformanceRecord(context.Context, derivationKey string) (telemetry.PerformanceRecord, error)`.
  - `FindPerformanceByOperation(context.Context, operationID string) (telemetry.PerformanceRecord, bool, error)`.
  - `ListCompatiblePerformance(context.Context, compatibilityKey string, limit int) ([]telemetry.PerformanceRecord, error)`.

- [ ] **Step 1: Write unique-idempotent store RED test**

Create one record, replay exactly, then attempt the same derivation key with different data. Exact replay succeeds without a second file/event; conflicting identity returns `telemetry_record_conflict`.

- [ ] **Step 2: Write hard-bound/cohort-neutral retention RED tests**

Open a repository with small A4 test limits. Insert mixed success/failure/timeout records. Prove per-key and global eviction removes oldest records by `CapturedAt` regardless of outcome, distinct compatibility keys never exceed global/per-repository ceilings, and file/metadata bytes stay below the configured derived-data ceiling.

- [ ] **Step 3: Write compatibility scan and E21 observation RED tests**

A changed `ExecutionFingerprint` must be excluded from the old compatibility bucket even when `ProjectCommandID` is unchanged. A newly durable sample emits one `telemetry_changed` obligation/event correlated to operation/session/workspace; replay emits none.

- [ ] **Step 4: Run RED**

```bash
go test ./internal/adapter/store -run 'Telemetry|Observation' -count=1
```

Expected: FAIL for missing telemetry store APIs.

- [ ] **Step 5: Implement bounded sample files and scans**

Use deterministic file names under `derived/telemetry/samples/<derivation_key>.json`. The sample directory itself is the rebuildable index authority: bounded inspection scans at most the advertised maximum retained sample files, validates every record, and sorts by `(CapturedAt, DerivationKey)` for deterministic retention/summary order. Do not add a second crash-sensitive mutable index merely for convenience.

Before a first durable create, prepare:

```go
observation.PrepareRequest{
    Kind: observation.EventTelemetryChanged,
    Correlation: correlationForOperation(record.OperationID),
    SubjectRef: "telemetry:" + record.DerivationKey,
    Summary: "execution telemetry changed",
}
```

Commit/abort the obligation using the same durability reconciliation pattern as structured results.

- [ ] **Step 6: Implement hard limits**

Add explicit A4 fields to `store.Limits` with safe defaults when zero in ordinary tests. Enforce:
- total telemetry records;
- metadata bytes;
- distinct compatibility keys;
- per-repository distinct keys;
- records per compatibility key;
- maximum age when retention is invoked.

Eviction order is chronological only. No outcome-based pruning.

- [ ] **Step 7: Extend E21 recovery**

`reconcilableObservationKind` and `observationSubjectPresent` recognize `telemetry_changed` by validating the deterministic telemetry file. Prepared obligation reconciliation never invents a sample.

- [ ] **Step 8: Run focused GREEN/race**

```bash
go test ./internal/adapter/store -run 'Telemetry|Observation' -count=1
go test -race ./internal/adapter/store -run 'Telemetry' -count=1
```

Expected: PASS.

- [ ] **Step 9: Run affected gate and commit**

```bash
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/adapter/store/repository.go internal/adapter/store/execution_observation.go internal/adapter/store/observation_test.go internal/adapter/store/telemetry*.go
git diff --cached --check
git commit -m "feat: persist bounded execution telemetry"
```

---

### Task 3: Derive telemetry asynchronously after durable terminal truth

**Files:**
- Create: `internal/app/telemetry/ports.go`
- Create: `internal/app/telemetry/service.go`
- Create: `internal/app/telemetry/service_test.go`
- Create: `internal/app/telemetry/worker.go`
- Create: `internal/app/telemetry/worker_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/service.go`
- Create/modify focused daemon telemetry-worker tests.

**Interfaces:**
- Consumes: terminal receipt, operation reservation, terminal session `UpdatedAt`, Task 2 telemetry repository.
- Produces:
  - `telemetryapp.Worker.ScheduleTerminal(context.Context, receipt.Receipt) error`.
  - daemon `TelemetryWorker` port with the same bounded non-blocking contract.

- [ ] **Step 1: Write derivation RED tests**

For one stored reservation at `CreatedAt=T0`, terminal session metadata at `UpdatedAt=T1`, and terminal receipt, derive one record with:

```text
wall_ms = max(0, T1-T0)
output_bytes = receipt.OutputBytes
input_accepted_bytes = receipt.InputAcceptedBytes
input_delivered_bytes = receipt.InputDeliveredBytes
terminal_outcome = receipt.Outcome
timed_out = receipt.Outcome == timeout
command_semantics_fingerprint = receipt.ExecutionFingerprint
captured_at = T1
```

The record's resource fields are explicit unavailable metrics and completeness reflects that resource observation is unavailable without weakening the exact existing counters.

- [ ] **Step 2: Write deterministic retry/crash RED tests**

Scheduling the same receipt twice produces the same receipt digest/derivation key and one persisted sample. If a sample was persisted before worker acknowledgement, a new worker/restart discovers/replays the same record without double count.

- [ ] **Step 3: Write daemon no-authority-rewrite RED tests**

A telemetry queue-full/store-error after terminal publication must leave the stored receipt unchanged and successful child outcome successful. The scheduler must be called only after `LoadReceipt` can read the durable terminal receipt.

- [ ] **Step 4: Run RED**

```bash
go test ./internal/app/telemetry ./internal/app/daemon -run 'Telemetry' -count=1
```

Expected: FAIL for missing worker/service wiring.

- [ ] **Step 5: Implement service + bounded worker**

Follow E22's worker pattern but keep an independent telemetry queue/lifecycle. Queue admission is O(1), parser/aggregation work is absent, and the worker reloads reservation/session/receipt from durable store before deriving. Use a fixed versioned producer/config digest.

- [ ] **Step 6: Schedule from both terminal paths**

In normal wait finalization and spawn-failure finalization:

```go
s.publishUntilDurable(rec)
s.scheduleStructuredTerminal(rec, l.reservation.StructuredAdapter)
s.scheduleTelemetryTerminal(rec)
```

The ordering is deliberate: durable receipt first; derived workers second. Neither worker may change `rec` or terminal metadata.

- [ ] **Step 7: Run focused GREEN/race**

```bash
go test ./internal/app/telemetry ./internal/app/daemon -run 'Telemetry' -count=1
go test -race ./internal/app/telemetry ./internal/app/daemon -run 'Telemetry' -count=1
```

Expected: PASS.

- [ ] **Step 8: Gate and commit**

```bash
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/app/telemetry internal/app/daemon/types.go internal/app/daemon/service.go internal/app/daemon/*telemetry*test.go
git diff --cached --check
git commit -m "feat: derive telemetry after terminal receipts"
```

---

### Task 4: Expose deterministic bounded telemetry history inspection

**Files:**
- Create: `internal/app/telemetry/inspect.go`
- Create: `internal/app/telemetry/inspect_test.go`
- Modify: `internal/app/telemetry/ports.go`

**Interfaces:**
- Consumes: Task 2 operation lookup/compatible scan and Task 1 summary.
- Produces:

```go
type InspectRequest struct {
    OperationID string `json:"operation_id"`
    MaxSamples  int    `json:"max_samples"`
}

type InspectResult struct {
    SchemaVersion    int                       `json:"schema_version"`
    Status           Status                    `json:"status"`
    OperationID      string                    `json:"operation_id"`
    CompatibilityKey string                    `json:"compatibility_key,omitempty"`
    Latest           *telemetry.PerformanceRecord `json:"latest,omitempty"`
    Summary          *telemetry.Summary        `json:"summary,omitempty"`
    SamplesReturned  int                       `json:"samples_returned"`
    SamplesAvailable int                       `json:"samples_available"`
}
```

- [ ] **Step 1: Write inspection RED tests**

Cover not-found/unavailable, one sample, bounded compatible history, same human command label with changed semantics separated, platform/toolchain/environment incompatibility separated, and no regression/prediction label.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/telemetry -run 'Inspect|Summary' -count=1
```

- [ ] **Step 3: Implement read-only inspection**

Resolve the target operation's sample, derive its compatibility key, request at most `MaxInspectSamples`, summarize only exact compatible records, and report available/returned counts. No worker scheduling, retention, or command execution occurs during inspection.

- [ ] **Step 4: Run GREEN/race and commit**

```bash
go test ./internal/app/telemetry -count=1
go test -race ./internal/app/telemetry -count=1
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/app/telemetry
git diff --cached --check
git commit -m "feat: inspect execution telemetry history"
```

---

### Task 5: Add telemetry capability/composition without ordinary-path aggregation tax

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/execution_observation_test.go`
- Modify: `cmd/shellbeam/daemon_test.go`
- Modify: `internal/core/capability/catalog.go` if composition requires final advertised values.

**Interfaces:**
- Consumes: telemetry worker/service/store and Task 4 inspector.
- Produces: shared daemon A4 runtime, `InspectTelemetry` action method for transport in Task 8.

- [ ] **Step 1: Write daemon composition/no-tax RED tests**

Prove ordinary `start` performs zero telemetry history scans/percentile aggregation before spawn and during poll. One terminal execution schedules exactly one bounded sample derivation after durable receipt. `inspect.server`, workspace/activity/project/event/structured/code inspection do not trigger telemetry aggregation.

- [ ] **Step 2: Write restart RED test**

Start daemon, produce a terminal sample, restart on the same state directory, inspect in-process telemetry service, and prove one sample remains one sample.

- [ ] **Step 3: Run RED**

```bash
go test ./cmd/shellbeam -run 'Telemetry|NoTax' -count=1
```

- [ ] **Step 4: Compose one shared telemetry runtime**

Reuse the same `storeadapter.Repository`; do not construct a second execution/observation store. Start one bounded telemetry worker with daemon lifetime and close admission/reap it during daemon shutdown. Worker close failure is diagnostic only and cannot rewrite terminal receipts.

- [ ] **Step 5: Run GREEN/race/benchmark evidence**

```bash
go test ./cmd/shellbeam -run 'Telemetry|NoTax' -count=1
go test -race ./internal/app/telemetry ./internal/app/daemon ./cmd/shellbeam -run 'Telemetry|NoTax' -count=1
go test ./internal/app/daemon -run '^$' -bench 'Workspace|Start' -benchmem -count=3
```

Benchmark observations are evidence only; do not invent a p95 promise.

- [ ] **Step 6: Gate and commit**

```bash
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add cmd/shellbeam/command_daemon.go cmd/shellbeam/*telemetry*test.go cmd/shellbeam/execution_observation_test.go cmd/shellbeam/daemon_test.go internal/core/capability/catalog.go
git diff --cached --check
git commit -m "feat: wire execution telemetry runtime"
```

---

### Task 6: Persist exactly-once immutable repro create records

**Files:**
- Create: `internal/adapter/store/repro.go`
- Create: `internal/adapter/store/repro_private.go`
- Create: `internal/adapter/store/repro_test.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/execution_observation.go`

**Interfaces:**
- Consumes: `repro.CreateRequest`, `repro.Capsule`, E21 observation primitives.
- Produces:
  - `CreateRepro(context.Context, requestFingerprint string, capsule repro.Capsule) (repro.Capsule, bool, error)` where `bool` means first durable creation.
  - `GetReproByCreateID(context.Context, createID string) (repro.Capsule, bool, error)`.
  - `GetRepro(context.Context, reproID string) (repro.Capsule, bool, error)`.

- [ ] **Step 1: Write lost-response/idempotency RED tests**

One atomic file under `derived/repro/creates/<safe-create-id>.json` contains `{schema_version, request_fingerprint, capsule}`. Same create ID + same fingerprint replays the exact original capsule bytes/capture cut. Same create ID + different fingerprint returns `operation_metadata_conflict`/typed repro conflict and never writes another capsule.

- [ ] **Step 2: Write concurrent create RED test**

Run two goroutines with the same create ID/fingerprint but different candidate `repro_id`/cuts. Exactly one durable file wins; both callers resolve to the winning stored capsule. This proves first durable acceptance rather than in-memory timing decides identity.

- [ ] **Step 3: Write quota/retention RED tests**

Hard-cap capsule count/metadata bytes/age. Retention removes whole old create records; it cannot partially rewrite a retained capsule descriptor. `GetRepro` scans only the bounded retained create set.

- [ ] **Step 4: Write E21 repro event RED test**

First durable create emits one `repro_recorded` event; exact retry emits none. Recovery commits a prepared obligation only when the matching immutable create record exists.

- [ ] **Step 5: Run RED**

```bash
go test ./internal/adapter/store -run 'Repro|Observation' -count=1
```

- [ ] **Step 6: Implement atomic create-record storage**

Do not use separate mutable request and capsule files. The single atomic create record is the exactly-once authority and contains the frozen capsule. On `os.ErrExist`, load and compare the stored request fingerprint, then return the stored capsule.

- [ ] **Step 7: Run GREEN/race and commit**

```bash
go test ./internal/adapter/store -run 'Repro|Observation' -count=1
go test -race ./internal/adapter/store -run 'Repro' -count=1
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/adapter/store/repository.go internal/adapter/store/execution_observation.go internal/adapter/store/repro*.go
git diff --cached --check
git commit -m "feat: persist exactly-once repro capsules"
```

---

### Task 7: Materialize one explicit current-cut repro capsule with privacy-safe descriptors

**Files:**
- Create: `internal/app/repro/ports.go`
- Create: `internal/app/repro/service.go`
- Create: `internal/app/repro/service_test.go`
- Create: `internal/app/repro/privacy.go`
- Create: `internal/app/repro/privacy_test.go`
- Create: `internal/app/repro/inspect.go`
- Create: `internal/app/repro/inspect_test.go`

**Interfaces:**
- Consumes: operation reservation, terminal receipt/session, workspace provenance, structured derivation lookup/summary, telemetry operation lookup, Task 6 repro store.
- Produces:

```go
func (s *Service) Create(ctx context.Context, request repro.CreateRequest) (repro.Capsule, error)
func (s *Service) Inspect(ctx context.Context, reproID string) (InspectResult, error)
```

- [ ] **Step 1: Write materialization cut RED test**

Create with structured derivation pending and telemetry absent. The frozen capsule records exactly those states. Complete both derivations afterward; retry same `repro_create_id` must return byte-equivalent original capsule. A new create ID may capture the richer terminal/current state and a different `capture_cut_digest`.

- [ ] **Step 2: Write source/environment/input capture RED tests**

Use receipt workspace provenance when present. Exact source/VCS digest stays exact; unavailable/partial provenance remains partial/unavailable. No environment/toolchain fingerprint is invented when the current runtime has none. Store only accepted/delivered byte counts and input completeness; no stdin identity/content.

- [ ] **Step 3: Write command privacy RED tests**

Fixtures include:

```text
--token=super-secret
password=hunter2
-----BEGIN PRIVATE KEY-----
/Users/alice/.ssh/id_work
```

Serialized capsule must not contain those strings. When argv cannot be copied safely, retain execution mode/executable/`ExecutionFingerprint`, omit raw argv, and mark command details partial. Safe bounded argv such as `go test ./...` may be retained exactly.

- [ ] **Step 4: Write immutable-descriptor/dynamic-resolution RED test**

After a referenced structured record compacts or telemetry detail is purged, `Inspect` must keep the exact creation descriptor unchanged and report current `resolution_state=compacted|purged|unavailable` separately. Never regenerate a replacement producer result and call it the original.

- [ ] **Step 5: Run RED**

```bash
go test ./internal/app/repro -count=1
```

- [ ] **Step 6: Implement cut collection + digest**

Load the authoritative operation/receipt first. Build a closed capture-cut input from exact current lifecycles/identities, sort reference descriptors deterministically, hash the canonical bounded cut, then attempt Task 6 atomic create. If another caller won, return the winner rather than recomputing/replacing it.

Use `ulid.Make()` only for candidate `repro_id`; the durable create record decides which ID wins.

- [ ] **Step 7: Implement inspection**

Inspection is read-only. It returns immutable capsule metadata plus current resolution state for each creation descriptor. It never executes, refreshes providers, waits for derivations, or mutates retention.

- [ ] **Step 8: Run GREEN/race/privacy gate and commit**

```bash
go test ./internal/app/repro -count=1
go test -race ./internal/app/repro -count=1
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/app/repro
git diff --cached --check
git commit -m "feat: materialize immutable repro cuts"
```

---

### Task 8: Add closed IPC/MCP actions through the one `local_shell` tool

**Files:**
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Create: `internal/adapter/ipc/telemetry_repro_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/telemetry_repro_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create/modify: A4 schema tests.

**Interfaces:**
- Consumes: Task 4 telemetry inspector, Task 7 repro create/inspect service.
- Produces three v2 action branches:
  - `inspect.telemetry { operation_id, max_samples }`
  - `repro.create { repro_create_id, operation_id, capture_policy? }`
  - `inspect.repro { repro_id }`

- [ ] **Step 1: Write IPC closed-field RED tests**

Valid examples decode exactly. Cross-action fields, malformed IDs, zero/out-of-range `max_samples`, unknown capture policy, and attempts to attach `command`, `stdin`, environment, or arbitrary provider fields fail closed before action dispatch.

- [ ] **Step 2: Write MCP one-tool RED tests**

Call the existing `local_shell` tool for each action. Assert no second MCP tool is registered, no `start` call occurs for read-only inspections, and `repro.create` calls only the explicit repro service action.

- [ ] **Step 3: Write schema RED tests**

The checked-in JSON schemas accept the three valid shapes and reject unknown/cross-action fields. Output schemas bound sample/reference arrays and expose capture completeness without `reproducible` or root-cause/performance-regression booleans.

- [ ] **Step 4: Run RED**

```bash
go test ./internal/app/bridge ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'Telemetry|Repro' -count=1
```

- [ ] **Step 5: Implement typed bridge/IPC action interfaces**

Follow the existing optional `EventActions`, `StructuredActions`, and `CodeActions` pattern. `repro.create` is an explicit mutation action but it does not spawn a child. `inspect.telemetry` / `inspect.repro` are read-only.

- [ ] **Step 6: Implement MCP mapping and output summaries**

Use compact summaries such as `inspect.telemetry: N sample(s)`, `repro.create: <repro_id>`, and `inspect.repro: <repro_id>`. Structured content carries the full bounded typed result.

- [ ] **Step 7: Update capability discovery**

Advertise A4 feature versions/limits and explicit resource metric support. Legacy capability view strips A4-only additions just as it strips E21/E22/E29 additions.

- [ ] **Step 8: Run GREEN/race/schema gates and commit**

```bash
go test ./internal/app/bridge ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'Telemetry|Repro' -count=1
go test -race ./internal/adapter/ipc ./internal/adapter/mcp -run 'Telemetry|Repro' -count=1
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git add internal/app/bridge internal/adapter/ipc internal/adapter/mcp api/schema
git diff --cached --check
git commit -m "feat: expose telemetry and repro actions"
```

---

### Task 9: Compose A4 in the daemon and prove no hidden workflow/provider tax

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Create/modify: `cmd/shellbeam/telemetry_repro_test.go`
- Modify: `cmd/shellbeam/execution_observation_test.go`

**Interfaces:**
- Consumes: Task 5 telemetry runtime; Task 7 repro service; Task 8 action surfaces.
- Produces: daemon actions `InspectTelemetry`, `CreateRepro`, `InspectRepro` backed by the shared store.

- [ ] **Step 1: Write end-to-end daemon RED test**

Start a command, poll to terminal, wait for bounded telemetry derivation, inspect telemetry through IPC, create repro, inspect repro, and inspect events. Assert one telemetry sample and one `repro_recorded` event; lost-response-style create retry returns the same `repro_id`/cut.

- [ ] **Step 2: Write ordinary-path no-tax RED test**

With A4 compiled/advertised but no history/repro inspection requested:
- no telemetry aggregation scan occurs before/during start/poll;
- no repro materializer runs;
- no resource provider process/probe starts;
- no network/SSH/`gh` access is introduced;
- telemetry terminal queue admission happens only after durable terminal publication.

- [ ] **Step 3: Write failure isolation RED test**

Force telemetry persistence failure and repro-store failure independently. Command terminal receipt/child outcome/Event Journal continuity remain valid. A repro failure returns a typed A4 error only to the explicit caller.

- [ ] **Step 4: Run RED**

```bash
go test ./cmd/shellbeam -run 'Telemetry|Repro|A4' -count=1
```

- [ ] **Step 5: Implement daemon action composition**

Reuse one store, workspace service, event materializer, structured-result store and operation metadata. Do not duplicate workspace/coherence/provider stacks. Repro reads existing derived state; it never starts code-intelligence or structured parsers to enrich a cut.

- [ ] **Step 6: Run GREEN/race and gate**

```bash
go test ./cmd/shellbeam -run 'Telemetry|Repro|A4' -count=1
go test -race ./internal/app/telemetry ./internal/app/repro ./internal/app/daemon ./cmd/shellbeam -run 'Telemetry|Repro|A4' -count=1
go run ./tools/devctl test --dirty --base origin/main
git diff --check
git status --short -- .codegraph
```

- [ ] **Step 7: Commit**

```bash
git add cmd/shellbeam/command_daemon.go cmd/shellbeam/*telemetry* cmd/shellbeam/*repro* cmd/shellbeam/execution_observation_test.go
git diff --cached --check
git commit -m "feat: compose execution telemetry and repro"
```

---

### Task 10: Crash/restart, compaction, privacy, native bounds, and final A4 checkpoint

**Files:**
- Modify/create: `tests/integration/execution_telemetry_repro_test.go`
- Modify: relevant retention/compaction tests where A4 references existing E22 state.
- Modify: `cmd/shellbeam/execution_observation_test.go` if final acceptance belongs there.

**Interfaces:**
- Verifies all A4 contracts; introduces no new product surface.

- [ ] **Step 1: Add crash/restart acceptance**

Cover:
- sample durable before event acknowledgement -> restart -> one sample/event, not two;
- repro create durable response lost -> restart -> same create ID returns exact original capsule;
- old repro cut stays unchanged after structured/telemetry derivations become richer;
- prepared observation without canonical derived subject is aborted rather than materialized from guesswork.

- [ ] **Step 2: Add retention/compaction acceptance**

Compact E22 structured detail referenced by a retained repro capsule and evict old telemetry detail under a tiny test quota. Repro inspection preserves original descriptors and reports dynamic compacted/purged states honestly.

- [ ] **Step 3: Add privacy/adversarial acceptance**

Persist/serialize state containing known fixtures for token, private-key text, stdin content, raw environment value, external absolute path, and source content. Search `derived/telemetry` and `derived/repro` plus IPC/MCP output; forbidden values must be absent except data already explicitly allowed by canonical raw output storage, which is not copied into A4 records.

- [ ] **Step 4: Add resource-capability acceptance**

On macOS/Linux, `inspect.server` advertises the initial A4 resource fields as unavailable. Tests must reject zero-valued fabricated observations. Cross-build does not count as native resource proof.

- [ ] **Step 5: Run focused integration x3**

```bash
go test ./tests/integration -run 'Telemetry|Repro' -count=3 -v
```

Expected: PASS x3.

- [ ] **Step 6: Run required package/race gates**

```bash
go mod verify
go test \
  ./internal/core/receipt \
  ./internal/core/telemetry \
  ./internal/core/repro \
  ./internal/core/capability \
  ./internal/core/failure \
  ./internal/core/observation \
  ./internal/app/telemetry \
  ./internal/app/repro \
  ./internal/app/daemon \
  ./internal/adapter/store \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  ./cmd/shellbeam \
  ./api/schema \
  ./tests/integration \
  -count=1

go test -race \
  ./internal/app/telemetry \
  ./internal/app/repro \
  ./internal/app/daemon \
  ./internal/adapter/store \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  -count=1
```

- [ ] **Step 7: Run repository gates**

```bash
PATH="/Users/trung.ngo/go/bin:$PATH" go run ./tools/devctl test --dirty --base origin/main
PATH="/Users/trung.ngo/go/bin:$PATH" go run ./tools/devctl check
git diff --check origin/main...HEAD
git status --short -- .codegraph
```

If a full `go test ./... -count=1` reports the pre-existing `internal/adapter/git/TestDeltaTrackedModification` `git_status_timeout`, rerun that exact focused test 5x and report both results. Do not label A4 complete unless all A4-focused/package/race/devctl gates are fresh and green.

- [ ] **Step 8: Anti-goal scan**

Prove production A4 adds none of:
- command scheduler/profile selector/timeout tuner;
- root-cause or performance-regression recommendations;
- automatic rerun/replay;
- source/environment/stdin/checkpoint payload capture;
- resource enforcement;
- second MCP tool;
- repro-triggered structured/code-intelligence provider startup;
- unbounded telemetry/repro scans or retention.

- [ ] **Step 9: Commit final A4 acceptance**

```bash
git add tests/integration cmd/shellbeam internal api/schema
git diff --cached --check
git commit -m "test: verify execution telemetry and reproduction"
```

- [ ] **Step 10: Re-run exact final-tree verification**

Run Task 10 Steps 5-8 again on the final commit. Record exact HEAD, receipts emitted by `devctl`, test counts/results, `.codegraph` status, and any remaining roadmap items.

---

## Completion Gate

A4 / E23-E24 is complete only when the exact final tree proves all of the following:

1. One compatible terminal execution/telemetry derivation is counted at most once despite retry/crash recovery.
2. Historical grouping always binds execution semantics; a reused command label cannot merge changed execution definitions.
3. Sample/key/metadata/per-key retention is hard bounded and chronological/cohort-neutral.
4. Toolchain/environment/schema/platform incompatibility is kept separate; absent fingerprints remain unavailable rather than invented.
5. Resource metrics are explicit per-metric quality and the initial unsupported process-tree metrics are advertised/recorded as unavailable, never zero/exact.
6. Telemetry aggregation never runs on ordinary command admission/spawn/poll paths; terminal derivation is scheduled only after durable receipt publication.
7. `repro_create_id` is exactly-once and lost-response/concurrent retry resolves to one immutable winning capsule/cut.
8. A richer later observation can create a new cut under a new create ID but cannot mutate the old capsule.
9. Creation-time reference descriptors stay immutable while inspection reports current available/compacted/purged/unavailable state separately.
10. Repro remains useful after eligible detail compaction without reconstructing a different producer's output as the original.
11. A4 records and output exclude raw environment values, stdin bytes/hashes, source contents, credentials/private keys, checkpoint private identities/payloads, and raw network payloads.
12. Telemetry/repro failures do not damage terminal receipts, operation idempotency, process authority, evidence authority, or E21 continuity.
13. `local_shell` remains the only MCP public tool.
14. Resource enforcement remains outside the A4 core-complete claim.
15. All fresh focused/package/race/devctl/diff/privacy gates pass on the exact final HEAD, with any unrelated baseline Git timeout separately reproduced/reported rather than silently ignored.

## Explicit Roadmap Boundary After A4

After this plan is complete, the next core roadmap checkpoint is A5 / E25 Project Readiness + E28 Typed Parameterized Project Commands under `docs/superpowers/specs/2026-08-14-project-readiness-typed-commands-design.md`. E26/E27 remain experimental and do not block A3-A5 core completion.
