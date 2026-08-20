# Vitest/Jest Structured Results — V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver `jest-json@v1` and `vitest-json@v1` on the deployed artifact-capture foundation, plus the three shared primitives they require that pytest V1 does not have — a failure-first record budget, traversal-observed entry counts, and a generalized producer-binding union in durable capture authority.

**Architecture:** Both adapters bind an explicitly requested producer output file, capture it through the existing `ArtifactCaptureIntent` → Phase A → Phase B → `ArtifactBlobRef` pipeline, and parse only the retained private blob with a strict per-profile JSON decoder. Record persistence changes from pytest's document-order cap to failure-first selection so the non-pass set is provably complete. Durable capture authority becomes a closed producer-binding union so a third producer needs no further refactor.

**Tech Stack:** Go 1.26.6; `github.com/go-json-experiment/json` via `internal/core/jsonstrict`; existing localfs descriptor capture, file-backed atomic store, environment presence observer, IPC/MCP v2 schemas, P1 verification semantics. No new module dependency. No reporter package, no runner installed on the execution path.

**Spec:** `docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md`

## Global Constraints

- Execution base is local `main` at the commit that merged pytest V1 (`39de442`). A clean `go test ./... -count=1` and `devctl verify --checkpoint` must pass before Task 1.
- TDD is mandatory: focused RED → minimal GREEN → focused regression → structural/dirty gate → commit. Never write production code first and backfill tests.
- Commit with `git -c core.hooksPath=.githooks commit ...` per `AGENTS.md`, staging only the intended scope.
- Keep one MCP tool, `local_shell`. No JavaScript/TypeScript-specific top-level tool, no second evidence ontology.
- Raw output and durable terminal receipts remain canonical execution truth. Structured records never overwrite child outcome, exit evidence, receipt identity, or P1 gate truth.
- `go-test-json`, `go-vet-json`, and `pytest-junit-xml` behavior, selection, identity, and persisted bytes must not change. Every task that touches shared code proves this with a focused regression over those three adapters.
- Both new adapters use `StructuredInputRef.artifact_blob`. Neither may use `raw_output`. No new input kind.
- ShellBeam never appends `--json`, `--reporter`, `--outputFile`, `run`, or `--run`, never removes an excluded flag, never installs a package, never edits a config file, and never redirects output to a private path.
- Core `TestStatus` stays exactly `pass|fail|skip|error`. Neither adapter ever produces `error` in V1.
- Neither parser reads `message`, `failureMessages`, `retryReasons`, `retryMessages`, or `failureDetails` for any semantic purpose, and no mechanical fact is derived from their content, presence, or length.
- Neither adapter reads the producer's `success` field for any purpose.
- Only executed producer versions become qualified profiles: Jest `v29`/`v30`, Vitest `v3`/`v4`. Any other observed key set is `unsupported` and fails closed.
- Record budget is failure-first (spec §33). Document-order truncation is forbidden. `Completeness=partial` + `CompletenessReason=pass_records_elided` and `ParseOutcome=budget_exceeded` stay distinct states; `zero_match` is another closed reason, not a new completeness enum value.
- Do not bump the shared `core.SchemaVersion` from 2 to 3. Derivation schema v3 and record/record-set schema v3 are introduced independently so existing Go/pytest persisted bytes remain v2 byte-for-byte.
- Selection is by set membership; emission is in original document order. Ordinals are never renumbered and records are never re-sorted.
- Production files target 150–300 lines, review above 350, hard cap 500; test files review above 600, hard cap 800; functions review above 60, hard cap 80; interfaces fail above eight methods.
- Broad gates use `go run ./tools/devctl check` and `go run ./tools/devctl test --dirty --base main`, with one deliberate final `go test ./... -count=1` plus `devctl verify --checkpoint --base main --json`.
- No push and no PR unless explicitly requested.
- Fixture manifests SHALL record the producer version emitted by the producer's own `--version` flag, not the version in the installed package's `package.json`. Verified Jest 30.4.2 reports `30.4.1` from `--version` while packages are `30.4.2`; Jest 29.7.0 reports consistently. A manifest pinned to the package version would label profiles incorrectly.

### Spec correction this plan carries

The spec claims in §4 and §64 that this work needs **zero** capture-layer change. That claim is wrong, and Task 3 exists because of it.

`CaptureAuthority` carries `PytestInvocation *PytestInvocationBindingV1` as a persisted JSON member, `PreSpawnCaptureRequest.Invocation` is typed as `PytestInvocationRequest`, and `buildCaptureAuthority` hardcodes `AdapterID: PytestJUnitAdapterID`. Durable capture authority is therefore pytest-shaped and cannot express a second producer.

Task 3 generalizes it additively and amends the spec text in the same commit. The corrected claim is: capture *mechanics* — baseline, collision, Phase A, Phase B, blob identity, retention, recovery — need no change; capture *authority typing* does.

## File Structure

### Shared core primitives

- Create `internal/core/structuredresult/observed_entries.go` + tests — `ObservedEntryCounts` traversal fact and closed `CompletenessReason` values.
- Modify `internal/core/structuredresult/derivation.go` + tests — terminal reason/count metadata, derivation-only schema v3, identity stability.
- Modify `internal/app/structuredresult/{ports.go,service.go,worker.go,inspect.go}` + tests — carry terminal metadata from parser to persistence and inspection.
- Modify `internal/adapter/store/structured_legacy.go`, structured transition tests — explicit v1/v2/v3 derivation decoding without rewriting old bytes.
- Modify `internal/core/verification/evidence.go`, `internal/adapter/verification/evidence_source.go` + tests — carry reason/counts across the existing P1 structured-evidence bridge without changing sufficiency semantics.
- Modify `internal/core/capability/catalog.go` + tests — advertise structured schema versions `[1,2,3]` once v3 read/write support exists.
- Create `internal/core/structuredresult/record_budget.go` + tests — failure-first selection, document-order emission, closed outcome/completeness mapping.
- Create `internal/core/structuredresult/failure_excerpt.go` + tests — bounded excerpt type, ANSI/control stripping, path classification; Task 2 owns record/record-set schema v3 for the new persisted member.

### Generalized capture authority

- Modify `internal/app/structuredresult/capture_authority.go`, `capture.go`, `capture_prepare_helpers.go` + tests — closed `ProducerInvocationBinding` union, adapter-driven intent construction.
- Modify `internal/adapter/store/structured_capture_authority.go` + tests — persisted union read/write with pytest v1 compatibility.
- Modify `docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md` — amend §4/§64.

### Jest adapter

- Create `internal/app/structuredresult/jest_invocation.go` + tests — producer form, `--json`/`--outputFile` binding, excluded-flag resolver, `JEST_JASMINE` presence authority, canonical digest.
- Create `internal/adapter/structured/jestjson/{types.go,profile.go,parser.go}` + tests + fuzz — profiles `v29`/`v30`, strict decode, status/disposition mapping.
- Modify `internal/app/structuredresult/selection.go`, `internal/app/daemon/admission.go`, `project_command.go`, `cmd/shellbeam/execution_structured_capture.go`, `execution_observation.go` + tests.
- Modify `internal/core/capability/catalog.go`, `internal/app/structuredresult/inspect.go`, `api/schema/ipc-v2.json`, `api/schema/mcp-output-v2.json` + tests.
- Create `scripts/generate-jest-json-fixtures.sh`, `scripts/test-jest-structured-results.sh`, `tests/fixtures/jest-json/manifest.json` + frozen fixtures for `29.7.0` and `30.4.2`.
- Create `cmd/shellbeam/structured_jest_test.go`.

### Vitest adapter (gated)

- Create `internal/app/structuredresult/vitest_invocation.go` + tests — producer form, run-mode proof, reporter binding, output-file binding.
- Create `internal/adapter/structured/vitestjson/{types.go,profile.go,parser.go}` + tests + fuzz — profiles `v3`/`v4`.
- Modify the same selection/admission/composition/capability/inspect surfaces.
- Create `scripts/generate-vitest-json-fixtures.sh`, `scripts/test-vitest-structured-results.sh`, `tests/fixtures/vitest-json/manifest.json` + frozen fixtures for `3.2.7` and `4.1.11`.
- Create `cmd/shellbeam/structured_vitest_test.go`.

---

### Task 0: Close terminal metadata plumbing before record-budget work

**Files:**
- Create: `internal/core/structuredresult/observed_entries.go`
- Create: `internal/core/structuredresult/observed_entries_test.go`
- Modify: `internal/core/structuredresult/derivation.go`, `derivation_test.go`
- Modify: `internal/app/structuredresult/ports.go`, `service.go`, `service_test.go`, `worker.go`, `worker_test.go`, `inspect.go`, `inspect_test.go`
- Modify: `internal/adapter/store/structured_legacy.go`, `structured_legacy_test.go`, `structured_results_private.go`, transition/replay tests as needed
- Modify: `internal/core/verification/evidence.go`, `evidence_test.go`
- Modify: `internal/adapter/verification/evidence_source.go`, `evidence_source_test.go`
- Modify: `internal/core/capability/catalog.go`, `catalog_test.go`
- Modify: `cmd/shellbeam/execution_observation_test.go`
- Modify: `api/schema/ipc-v2.json`, `api/schema/mcp-output-v2.json`, `api/schema/structured_inspect_test.go`
- Modify: `internal/adapter/ipc/structured_inspect_test.go`, `internal/adapter/mcp/structured_inspect_test.go`

**Interfaces:**

```go
type CompletenessReason string

const (
    CompletenessReasonPassRecordsElided CompletenessReason = "pass_records_elided"
    CompletenessReasonZeroMatch         CompletenessReason = "zero_match"
)

type ObservedEntryCounts struct {
    Namespace         string `json:"namespace"`
    VocabularyVersion int    `json:"vocabulary_version"`
    Files             int    `json:"files"`
    Entries           int    `json:"entries"`
    Pass              int    `json:"pass"`
    Fail              int    `json:"fail"`
    Skip              int    `json:"skip"`
    Error             int    `json:"error"`
}
```

`Derivation` gains `CompletenessReason CompletenessReason` and `ObservedEntries *ObservedEntryCounts` as terminal-only, non-identity metadata. `ParseResult`, `inspect.structured`, and `StructuredEvidenceDetail` carry the same fields additively. No P1 sufficiency rule consumes these values in this task; this task only preserves the existing bridge and makes the facts available to future explicit bounded requirements.

Derivation persistence gains an explicit schema-v3 branch while records remain on the current schema. Do **not** change the shared `core.SchemaVersion = 2` constant. Existing v1/v2 derivations decode exactly as before and are never rewritten solely to normalize versions. Existing adapters that return no reason/count metadata still persist schema-v2 derivations.

- [ ] **Step 1: Write RED core validation tests**

Cover: closed reason values; reason only on terminal partial derivations; `pass_records_elided` and `zero_match` require `ParsePartial` and `CompletenessPartial` before compaction, while `CompletenessCompacted` preserves the terminal reason afterward; observed counts only on terminal derivations; vocabulary version 1; safe bounded namespace; every count non-negative and <= `maxObservedEntries=65536`; `Pass+Fail+Skip+Error == Entries`; and **no** `Files <= Entries` invariant. Include a valid fixture with `Files=2, Entries=1` to pin the Jest module-error case.

Run: `go test ./internal/core/structuredresult -run 'ObservedEntr|CompletenessReason|DerivationV3' -count=1`
Expected: FAIL because the types/fields do not exist.

- [ ] **Step 2: Implement core types and derivation-only schema v3**

Add a dedicated derivation-v3 constant. `Derivation.Validate` accepts v1/v2/v3, rejects v3-only fields on v1/v2, and keeps derivation identity unchanged. Do not alter `Record.Validate` or the shared record schema constant in this task.

- [ ] **Step 3: Write RED store compatibility/transition tests**

Pin literal v1 and v2 derivation JSON bytes, then add v3 fixtures. Assert v1/v2 read unchanged, v2 processing -> v3 terminal is allowed only when v3 terminal metadata is present/valid, unknown schema fails closed, and replay/compaction preserves reason/count metadata. Assert old v2 bytes are not rewritten by read/replay.

- [ ] **Step 4: Implement store v3 decoding and transition rules**

Decode v1, v2, and v3 explicitly with strict unknown-field rejection. Keep record-set decoding unchanged in this task.

- [ ] **Step 5: Write RED app plumbing tests**

Add `CompletenessReason` and `ObservedEntries` to `ParseResult`. Prove worker -> service -> repository persistence, `inspect.structured` exposure, and defensive copying. When output truncation downgrades a parser `complete` result to partial, reason remains empty unless the parser supplied a valid reason for the resulting partial state.

- [ ] **Step 6: Implement app plumbing**

Use one terminal metadata-bearing completion path; preserve compatibility wrappers for callers that only provide coverage. Existing adapters must continue to persist schema-v2 derivations.

- [ ] **Step 7: Write RED P1 bridge and capability tests**

`StructuredEvidenceDetail` receives `ParseOutcome`, `CompletenessReason`, and `ObservedEntries` additively and excludes them from compatibility identity exactly like the existing detail envelope. `EvidenceSource.bindStructuredDetail` copies them without changing the literal evidence result or sufficiency outcome. Capability advertises `[1,2,3]`. The closed IPC/MCP `structured_inspection` schemas add `completeness_reason` and `observed_entries` additively; both transports prove those fields survive serialization without widening `additionalProperties`.

- [ ] **Step 8: Implement bridge/capability and run focused regression**

```bash
go test ./internal/core/structuredresult ./internal/app/structuredresult ./internal/adapter/store ./internal/core/verification ./internal/adapter/verification ./internal/core/capability ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -run 'Structured|ObservedEntr|CompletenessReason|Derivation|Evidence|Capability' -count=1
go run ./tools/devctl test --dirty --base main --json
```

- [ ] **Step 9: Commit**

```bash
git add internal/core/structuredresult internal/app/structuredresult internal/adapter/store internal/core/verification internal/adapter/verification internal/core/capability internal/adapter/ipc internal/adapter/mcp api/schema cmd/shellbeam docs/superpowers/plans/2026-08-20-vitest-jest-structured-results-v1.md docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: persist structured terminal metadata"
```

---

### Task 1: Failure-first record budget

**Files:**
- Create: `internal/core/structuredresult/record_budget.go`
- Create: `internal/core/structuredresult/record_budget_test.go`

**Interfaces:**
- Produces `SelectRecordsFailureFirst` as a pure function over `[]Record`, usable by any adapter and by no other layer.
- Reuses the Task-0 `CompletenessReason` vocabulary; it does not invent a second budget-reason type.

- [ ] **Step 1: Write RED budget-selection tests**

Use these exact public shapes:

```go
type RecordBudget struct { MaxRecords int }

type BudgetSelection struct {
    Records            []Record
    Outcome            ParseOutcome
    Completeness       Completeness
    CompletenessReason CompletenessReason
}

func SelectRecordsFailureFirst([]Record, RecordBudget) (BudgetSelection, error)
```

Mandatory set is every non-`test_case` record plus every `test_case` whose status is not `pass`. Optional set is `test_case` records with status `pass`.

Cover, at minimum:

```text
everything fits                          → complete, reason ""
one pass elided                          → partial, reason pass_records_elided
all pass elided, every fail retained     → partial, reason pass_records_elided
mandatory set alone exceeds cap          → budget_exceeded, partial, reason ""
suite records are never elided
diagnostic/artifact_result never elided
emission order equals input order        (NOT mandatory-first)
selection is stable for identical input
MaxRecords <= 0                          → error
```

The order assertion is load-bearing: build an input where a `pass` at index 0 is elided and a `fail` at index 5 is kept, then assert the surviving slice is still ascending by original index.

Run: `go test ./internal/core/structuredresult -run 'RecordBudget|FailureFirst' -count=1`
Expected: FAIL because the selector does not exist.

- [ ] **Step 2: Implement the selector**

Mark selected indices in one pass, then emit in ascending index order. Do not sort, renumber, or mutate input records. When the mandatory set exceeds the cap, truncate mandatory records in document order and return `ParseBudgetExceeded` with `CompletenessPartial` and an empty reason — a caller must be able to see the failures that did fit without confusing this with a complete failure set.

- [ ] **Step 3: Prove no existing-adapter regression and commit**

```bash
go test ./internal/core/structuredresult ./internal/app/structuredresult ./internal/adapter/structured/gojson ./internal/adapter/structured/pytestjunit -count=1
go run ./tools/devctl check
git add internal/core/structuredresult
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: select structured records failure first"
```

---

### Task 2: Bounded failure excerpts, specified and gated

**Files:**
- Create: `internal/core/structuredresult/failure_excerpt.go`
- Create: `internal/core/structuredresult/failure_excerpt_test.go`
- Modify: `internal/core/structuredresult/record.go`
- Modify: `internal/core/structuredresult/record_test.go`

**Interfaces:**
- Produces `FailureExcerpt` plus `NormalizeFailureExcerpt`, a pure function with no filesystem access.
- Adds `TestCase.FailureExcerpt *FailureExcerpt`, populated by no adapter until the Step 5 exposure check passes.

- [ ] **Step 1: Write RED normalization tests**

```go
const MaxFailureExcerptBytes = 2048

type FailureExcerpt struct {
    Namespace         string `json:"namespace"`
    VocabularyVersion int    `json:"vocabulary_version"`
    Text              string `json:"text"`
    Truncated         bool   `json:"truncated"`
    Redacted          bool   `json:"redacted"`
}

func NormalizeFailureExcerpt(raw, namespace, workspaceRoot string) (FailureExcerpt, bool)
```

Cover:

```text
CSI/OSC escape sequences stripped, surrounding text preserved
C0/C1 control characters removed, newline retained
invalid UTF-8                        → not ok
over-length input                    → truncated on a rune boundary, Truncated true
absolute path inside workspace root  → rewritten workspace-relative
absolute path outside workspace root → redacted marker, Redacted true
system path                          → classified, not persisted verbatim
no path present                      → Redacted false
empty or whitespace-only result      → not ok
```

Reuse the `internal/core/inputtrace` `PathClass` constants for the classification vocabulary rather than defining a second one.

The escape-sequence parser SHALL be a small, hand-rolled function in `failure_excerpt.go` (no new dependency). Coverage: CSI sequences (`ESC [` ... `<final byte 0x40-0x7E>`), OSC sequences (`ESC ]` ... terminated by `BEL`/`ST`), and incomplete/malformed sequences (drop the partial sequence, preserve surrounding text). Fuzz in Task 7 Step 7 with `go test -fuzz=FuzzNormalizeFailureExcerpt` over 30s.

- [ ] **Step 2: Implement the normalizer**

Return `ok=false` rather than a partially normalized value. Anything the normalizer cannot fully classify is omitted; omission makes the record partial and never makes its status unavailable.

A second test in Task 1 Step 1 SHALL exercise the budget at scale: build an input of 10000 records (cap 8192) with 50 fails at random positions and assert the selection is stable, every fail is retained, and emit-order equals input order. This is the regression for the load-bearing "set membership selection + document-order emission" property under realistic size.

- [ ] **Step 3: Attach to TestCase with strict validation and record schema v3**

`Record.Validate` rejects an excerpt whose text exceeds the cap, contains an escape sequence, contains a control character other than newline, or contains an absolute path in any class. Prove the record digest/ID is unchanged by the presence of an excerpt.

Persisting the new member uses record/record-set schema v3. Add explicit v1/v2/v3 record decoding tests, prove existing v2 Go/pytest record bytes are not rewritten, and keep the shared `core.SchemaVersion=2` constant unchanged; use a dedicated record-v3 constant for the new shape.

- [ ] **Step 4: Prove no existing-adapter regression**

```bash
go test ./internal/core/structuredresult ./internal/adapter/structured/pytestjunit ./internal/adapter/structured/gojson -count=1
```

- [ ] **Step 5: Resolve the retention-containment gate before any adapter populates the field**

Spec §34 makes the excerpt conditional on retention containment: the excerpt's lifetime must be bounded by the same retention policy that bounds the raw output it summarizes. The original draft phrased the dependency as a redaction-parity check against raw output views, which is empirically wrong (`traceOutputRedactor` scrubs only internal trace-protocol artifacts; the IPC/MCP `inspect.structured` response is a passthrough; raw output is served through `receipt.VisibleOutput` which is UTF-8 boundary clipping only — neither side has user-secret redaction). A redaction-parity check would either fail trivially or pass trivially and would not catch the real exposure gap.

The real gap: raw output is deleted by session retention after `TerminalRetention` (168h default), while a structured record with an excerpt outlives that window because records live until explicit compaction. An excerpt therefore outlives the raw output it derives from, becoming the only surviving copy of that failure text in daemon storage.

Establish it mechanically. Add a test in `internal/core/structuredresult` (or in the existing record-retention authority) that proves a per-record marker can be honored by an explicit compaction sweep no later than the raw output's `TerminalRetention`. Without that marker the field stays unpopulated; with it the field ships. Record the result in this plan:

```text
gate PASSED  → per-record retention marker honored by compaction sweep;
               adapters populate the field in Tasks 5 and 10
gate FAILED  → field stays unpopulated; Tasks 5 and 10 skip it;
               record the failing control here and stop — do not
               work around it
```

This is a real gate that can fail. The pytest V1 producer V1 also stores blob bytes that live past raw-output retention with unnormalized failure text — that is a separate at-rest hazard and out of scope for this work, but should be opened as a follow-up issue if retention-containment here succeeds.

- [ ] **Step 6: Commit**

```bash
go run ./tools/devctl check
git add internal/core/structuredresult internal/adapter/ipc internal/adapter/mcp docs/superpowers/plans/2026-08-20-vitest-jest-structured-results-v1.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: bound structured failure excerpts"
```

---

### Task 3: Generalize durable capture authority to a producer-binding union

**Files:**
- Modify: `internal/app/structuredresult/capture_authority.go`
- Modify: `internal/app/structuredresult/capture_authority_test.go`
- Modify: `internal/app/structuredresult/capture.go`
- Modify: `internal/app/structuredresult/capture_prepare_helpers.go`
- Modify: `internal/app/structuredresult/capture_test.go`
- Modify: `internal/adapter/store/structured_capture_authority.go`
- Modify: `internal/adapter/store/structured_capture_authority_test.go`
- Modify: `docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md`

**Interfaces:**
- Produces a closed `ProducerInvocationBinding` union in durable capture authority, with `pytest_invocation` preserved byte-for-byte for existing persisted records.
- Produces an adapter-driven `buildCaptureAuthority` so no producer identity is hardcoded in the preparer.

- [ ] **Step 1: Write RED persisted-compatibility tests**

Write a literal on-disk capture-authority record in today's exact shape, with `pytest_invocation` populated and no other binding member. Reopen the store and assert: the record decodes, validates, yields the identical `StructuredCaptureDigest`, and its bytes are not rewritten.

This must pass before and after the refactor. `readPrivateJSON` uses `DisallowUnknownFields`, so this test is the only thing standing between a schema change and an unreadable authority record.

Run: `go test ./internal/adapter/store -run 'CaptureAuthority' -count=1`
Expected: PASS today. It is a pinning test, written first deliberately.

- [ ] **Step 2: Write RED union tests**

```go
type ProducerInvocationBinding struct {
    Kind    ProducerInvocationKind     `json:"kind"`
    Pytest  *PytestInvocationBindingV1 `json:"pytest_invocation,omitempty"`
    Jest    *JestInvocationBindingV1   `json:"jest_invocation,omitempty"`
    Vitest  *VitestInvocationBindingV1 `json:"vitest_invocation,omitempty"`
}
```

Validation mirrors `StructuredInputRef`: closed kind, exactly one branch, branch matches kind, branch validates independently.

Assert that a persisted record with `kind` absent and `pytest_invocation` present is read as `kind=pytest` — the legacy shape must normalize in memory without a byte rewrite. Assert two branches set, zero branches set, and kind/branch mismatch all fail closed.

- [ ] **Step 3: Generalize the request and the authority builder**

Replace `PreSpawnCaptureRequest.Invocation PytestInvocationRequest` with a producer-agnostic carrier:

```go
type PreSpawnCaptureRequest struct {
    OperationID   operation.ID
    SessionID     operation.SessionID
    RepositoryID  string
    WorkspaceID   string
    WorkspaceRoot string
    MaxBlobBytes  int64
    Producer      ProducerCaptureRequest
}

type ProducerCaptureRequest interface {
    AdapterID() string
    Qualify(context.Context, EnvironmentPresenceObserver) (ProducerInvocationBinding, bool, error)
    OutputBinding() CaptureOutputBinding   // declared token + normalized workspace path
}
```

`buildCaptureAuthority` then takes the binding's `AdapterID()` and `OutputBinding()` instead of hardcoding `PytestJUnitAdapterID` and `binding.JUnitOutput`. The interface stays at three methods, well under the eight-method cap.

Move pytest to an implementation of `ProducerCaptureRequest` in the same change so no intermediate commit leaves the package uncompilable.

- [ ] **Step 4: Prove pytest identity is bit-stable across the refactor**

The strongest available regression: for a fixed pytest invocation, assert `ProducerBindingDigest()` and `StructuredCaptureDigest` are identical to values captured before this task. Hard-code the expected digests as literals in the test so a future refactor cannot silently drift them.

```bash
go test ./internal/app/structuredresult ./internal/adapter/store ./internal/core/operation -run 'Capture|Pytest|Authority|Reservation' -count=1
go test ./internal/app/structuredresult ./internal/adapter/store -race -run 'Capture|Authority' -count=1
```

- [ ] **Step 5: Record the downgrade hazard and amend the spec**

A record written with `jest_invocation` is unreadable by a binary that predates this task, because unknown members are rejected. That is acceptable for a single local daemon but must be written down, not discovered.

Amend the spec in this commit:

- §4 — move capture *authority typing* out of the reuse list, keeping capture *mechanics* in it;
- §62 — replace invariant 2 with the accurate statement;
- §64 — remove the "no capture-layer change" absolute and cite this task.

- [ ] **Step 6: Commit**

```bash
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base main
git add internal/app/structuredresult internal/adapter/store docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "refactor: generalize capture authority producer binding"
```

---

### Task 4: Qualify the exact Jest invocation

**Files:**
- Create: `internal/app/structuredresult/jest_invocation.go`
- Create: `internal/app/structuredresult/jest_invocation_test.go`
- Modify: `internal/app/structuredresult/capture_authority.go`

**Interfaces:**
- Produces `JestInvocationBindingV1`, `JestInvocationRequest`, `QualifyJestInvocation`, `JestCandidateArgv`, and a canonical `ProducerBindingDigest`.
- Reuses `EnvironmentPresenceFact` unchanged for `JEST_JASMINE`.

- [ ] **Step 1: Write RED table tests for producer and flag resolution**

```go
const (
    JestInvocationSchemaV1         = 1
    JestJSONAdapterID              = "jest-json"
    JestJasmineEnvironment         = "JEST_JASMINE"
    ArgumentFileStateNotExpanded   = "producer_does_not_expand"
)

type JestInvocationBindingV1 struct {
    SchemaVersion          int                     `json:"schema_version"`
    ProducerForm           string                  `json:"producer_form"`
    JSONFlag               string                  `json:"json_flag"`
    OutputFile             CaptureOutputBinding    `json:"output_file"`
    ExcludedFlagState      string                  `json:"excluded_flag_state"`
    JasmineEnvironmentFact EnvironmentPresenceFact `json:"jasmine_environment_fact"`
    ArgumentFileState      string                  `json:"argument_file_state"`
    ArgumentFileEvidence   string                  `json:"argument_file_evidence"`
    ZeroMatchEmitsArtifact bool                    `json:"zero_match_emits_artifact"`
}
```

Table coverage:

```text
qualified:   jest --json --outputFile=out.json
             jest --json --outputFile out.json
             node_modules/.bin/jest --json --outputFile=out.json
             relative output path resolved from frozen ResolvedCWD
             in-workspace absolute output path
             jest @acme                            (scoped-package filter)

unqualified: missing --json
             missing --outputFile
             --outputFile without --json
             npm test / npx jest / yarn jest / pnpm jest / bun jest
             node ./node_modules/jest/bin/jest.js  (entry script path is
                                                   an implementation detail
                                                   of the distribution)
             shell string form
             --listTests, --collectTests, --watch, --watchAll,
             --bail, -b, --onlyFailures, -o, --shard,
             --testResultsProcessor
             output path requiring ~ or environment expansion
             output path outside workspace containment
```

**No `@token` shape rule.** Verified empirically that `jest @acme` correctly selects scoped-package tests and is a qualified, working invocation. Treating the leading `@` as disqualifying would reject real monorepo scoped-package filters. The non-expansion guarantee is recorded in the binding as `argument_file_state = producer_does_not_expand` (with `argument_file_evidence` carrying the producer version that established the fact) and is enforced per-version by the release qualification matrix, not by runtime shape detection. See spec §20.1.

`--listTests` deserves its own named test with a comment: with that flag the payload becomes a JSON array of path strings, and `--outputFile` is honored even without `--json`. Silently accepting it would parse a different schema entirely.

- [ ] **Step 2: Write RED environment-authority tests**

`JEST_JASMINE` absent → qualified; present → unqualified. The fact stores only presence, never a value, and its digest is deterministic and replayable from durable authority.

Add an explicit negative test asserting that the agent-detection variables (`CLAUDECODE`, `AI_AGENT`, `CURSOR_AGENT`, and the rest of the set) are **not** consulted and do **not** affect qualification. Spec §25 establishes they change only stderr, and ShellBeam runs under them routinely — a future change that treats them as disqualifying would break every real run.

- [ ] **Step 3: Implement one option-aware resolver**

Honor `--` termination and option arity. Do not scan tokens after `--`, do not consume an option value as a new option, and do not expand an argument file. `excluded_flag_state` records that the closed exclusion set was evaluated and found absent, so the fact is committed into the digest rather than re-derived at recovery.

- [ ] **Step 4: Prove digest sensitivity**

Same argv with a different `JEST_JASMINE` fact, a different normalized output path, or a different excluded-flag state must produce a different `ProducerBindingDigest`.

- [ ] **Step 5: Commit**

```bash
go test ./internal/app/structuredresult -run 'Jest' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: qualify jest structured invocation"
```

---

### Task 4.5: Generate Jest real-document fixtures before pinning parser assumptions

**Files:**
- Create: `scripts/generate-jest-real-doc-fixtures.sh`
- Create: `tests/fixtures/jest-json/real-doc-fixtures/{manifest.json,jest-29.7.0/,jest-30.4.2/}`

**Interfaces:**
- Produces at least one real `--json --outputFile=<path>` document per qualified Jest version (29.7.0, 30.4.2) by running the actual producer in a throwaway install, with `JEST_JASMINE` unset.
- Records the producer version string from `jest --version` output (NOT from `package.json`) — verified Jest 30.4.2 reports `30.4.1` while packages are `30.4.2`, so a manifest pinned to package.json would mislabel the profile.

> This task is sequenced BEFORE Task 5 for the same reason Task 1 is sequenced before any parser: the structural profile discriminator (§30 of the spec) and the zero-match emission behavior (§22.5) are facts about real producer output, not about presumed output. Pinning a parser against assumed key sets is the same defect as pinning a parser against a document-order cap: the parser passes its fixtures and fails on real repositories.

- [ ] **Step 1: Write the manifest shell**

Generate one minimal pass-only document per version:

```bash
# install version, run --version, capture producer-reported version
INSTALLED="$(npx jest --version | awk '{print $1}')"
# run a minimal pass-only suite to /tmp/<INSTALLED>.json
# freeze bytes; record producer version, exact argv, SHA-256 in manifest
```

The manifest format mirrors `tests/fixtures/pytest-junit/manifest.json`: producer version, exact generator command, normalization note, SHA-256.

- [ ] **Step 2: Generate the zero-match emission document per version**

For each version, run an invocation that filters out every test and capture what happens at the declared output path. Record:

```text
Jest 30.4.2   argv: jest --json --outputFile=/tmp/empty.json --testNamePattern=__none__
              file present after invocation?  NO
              exit code: 1
Jest 29.7.0   (same)
              file present after invocation?  NO
              exit code: 1
```

If a version emits the file, the binding's `zero_match_emits_artifact` is set to `true` and the parser-level `Completeness=partial`, `CompletenessReason=zero_match` detection (Task 5) is required. If not, `false`.

- [ ] **Step 3: Generate the `@file` non-expansion document per version**

For each version, run an invocation whose argv contains `@args.txt` (file containing `--bail`) and verify the producer does not expand the file:

```text
Jest 30.4.2   argv: jest --json --outputFile=/tmp/expand.json @args.txt
              args.txt content: --bail
              producer behavior: 0 matches, no file emitted at declared path
              argfile expansion observed?  NO
```

Record the result. If a version DOES expand, the binding's `argument_file_state` flips to `producer_expands` (a new closed value) and the plan returns to amend spec §20.1.

- [ ] **Step 4: Verify the observed key set before any parser code is written**

For each version, dump the JSON document and verify the assertion-key count and the presence of `failing`/`startAt` match the spec §30 table. If they do not, stop and amend spec §30 rather than letting the parser commit to an assumed profile.

- [ ] **Step 5: Commit real-doc fixtures**

```bash
./scripts/generate-jest-real-doc-fixtures.sh
go test ./tests/fixtures/jest-json -count=1 || true
go run ./tools/devctl check
git add scripts/generate-jest-real-doc-fixtures.sh tests/fixtures/jest-json
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: freeze jest real-document fixtures"
```

---

### Task 5: Implement the `jest-json@v1` strict parser

**Files:**
- Create: `internal/adapter/structured/jestjson/types.go`
- Create: `internal/adapter/structured/jestjson/profile.go`
- Create: `internal/adapter/structured/jestjson/parser.go`
- Create: `internal/adapter/structured/jestjson/parser_test.go`
- Create: `internal/adapter/structured/jestjson/parser_limits_test.go`
- Create: `internal/adapter/structured/jestjson/parser_fuzz_test.go`

**Interfaces:**
- Produces `jestjson.Adapter{}` with `ID() == "jest-json"`, version `1`, artifact-only input.
- Emits `ParseResult` with `SemanticsCoverage`, `ObservedEntries`, and `CompletenessReason`, and applies `SelectRecordsFailureFirst` before returning.

- [ ] **Step 1: Write RED profile-discrimination tests**

Two closed decode structs. `v30` declares the 13 assertion members; `v29` declares 11 (no `failing`, no `startAt`). Try `v30` first, then `v29`; first clean strict decode wins.

Members the adapter does not consume — `snapshot`, `openHandles`, `coverageMap`, `failureDetails`, `retryReasons` — SHALL be declared as raw-message fields so their internal shape is not pinned, while still satisfying `RejectUnknownMembers` at the level the profile does pin.

```text
v30 document                         → profile v30
v29 document                         → profile v29
v30 document with one unknown member → unsupported, fails closed
duplicate JSON member                → fails closed
--listTests array-of-strings payload → fails closed, not "empty run"
trailing bytes after the document    → fails closed
```

Run: `go test ./internal/adapter/structured/jestjson -count=1`
Expected: FAIL because the package does not exist.

- [ ] **Step 2: Write RED status and disposition tests**

```text
passed,  failing=false              → pass,  no disposition
passed,  failing=true               → pass + jest:failing_expected   (v30)
failed,  failing=true               → fail + jest:failing_unexpected (v30)
failed,  failing=false              → fail
pending                             → skip + jest:pending
todo                                → skip + jest:todo
skipped | disabled | focused        → unsupported, fails closed
v29 document with any failing claim  → coverage marks both codes unavailable
```

File-level: `failed → fail`, `passed → pass`, `skipped → skip`, `focused → pass + jest:suite_focused`. Add a fixture with one pass plus one `it.skip` and assert the file reports `focused`, not `passed` — that is the trap spec §46 names.

`invocations` maps to `jest:invocations` from the integer only. Assert a `passed` record with `invocations == 3` stays `pass` and is never relabeled or described as flaky.

- [ ] **Step 3: Write RED suite-error and budget tests**

```text
beforeAll throws     → per-assertion fail entries, no error status
afterAll throws      → file fail, assertions keep real statuses
module-level throw   → file fail, assertionResults empty
```

Assert no record anywhere carries `TestStatus = error`, and that the heuristic "failed file with no failed assertion" is **not** used to synthesize one.

Budget: build a document with more entries than the cap where failures sit past the cap position, and assert every failure is persisted, passes are elided, completeness is `partial` with reason `pass_records_elided`, and `ObservedEntryCounts.Entries` still equals the full traversed count.

- [ ] **Step 4: Implement the parser**

Bounds: `maxJestJSONRecords = 8192`, observed-entry ceiling `65536`, per string field `64 KiB`, input bytes inherited from the resolver, plus the context deadline. Reject on the observed-entry ceiling before normalizing anything.

Never read `success`. Never recompute a suite status from records. Never emit `TestSuiteAggregate`.

- [ ] **Step 5: Implement identity, address, and duration**

`ArtifactTestEntryRef` from blob ID plus file and assertion ordinals; `RecordID = H(derivation_key + "testcase" + ordinals)`. `ProducerTestAddress` carries the `PathClass`-classified file path, joined ancestor titles, and title. Never treat `fullName` as identity. Durations truncate to whole milliseconds toward zero; the exec-error branch's equal `startTime`/`endTime` are not persisted as facts.

- [ ] **Step 6: Emit coverage, and populate excerpts only if Task 2 gate passed**

Coverage exactly as spec §53, with `family` set from the observed profile and both `failing` codes moved to `unavailable` under `v29`.

- [ ] **Step 7: Run parser, limits, fuzz and commit**

```bash
go test ./internal/adapter/structured/jestjson ./internal/core/structuredresult -count=1
go test ./internal/adapter/structured/jestjson -race -count=1
go test ./internal/adapter/structured/jestjson -run '^$' -fuzz=FuzzJestJSON -fuzztime=30s
go run ./tools/devctl check
git add internal/adapter/structured/jestjson
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: parse jest json structured results"
```

---

### Task 6: Wire Jest into selection, admission, composition, capability and inspect

**Files:**
- Modify: `internal/app/structuredresult/selection.go`, `selection_test.go`
- Modify: `internal/app/daemon/admission.go`, `project_command.go`
- Modify: `internal/app/daemon/structured_pytest_admission_test.go` (rename to cover both producers) or add `structured_js_admission_test.go`
- Modify: `cmd/shellbeam/execution_structured_capture.go`, `execution_structured_capture_test.go`
- Modify: `cmd/shellbeam/execution_observation.go`
- Modify: `internal/core/capability/catalog.go`, `catalog_test.go`
- Modify: `internal/app/structuredresult/inspect.go`, `inspect_test.go`
- Modify: `internal/adapter/ipc/structured_inspect_test.go`, `internal/adapter/mcp/structured_inspect_test.go`, `internal/adapter/mcp/discovery_test.go`
- Modify: `api/schema/ipc-v2.json`, `api/schema/mcp-output-v2.json`

**Interfaces:**
- `SelectAdapterWithPytest` becomes a producer-agnostic `SelectAdapterWithCapture` taking the qualified binding set; the pytest entry point is preserved for existing callers until its last caller moves.
- Capability advertises `jest-json` and the observed-profile vocabulary additively.

- [ ] **Step 1: Write RED selection and admission tests**

Auto-selection only on a fully qualified binding. Explicit `structured_adapter=jest-json` with any missing element returns a typed precondition error **before spawn**. Assert an argv that would qualify for neither producer selects nothing, and that no argv can qualify for two producers at once.

Extend the `admission.go` precondition-message switch with a `jest-json` case describing the required shape, matching the existing pytest wording style.

- [ ] **Step 2: Generalize the capture-runtime dispatch**

`PrepareStructuredCapture` currently branches on `req.StructuredAdapter == PytestJUnitAdapterID` and `PytestCandidateArgv`. Replace with a small ordered producer table so a third producer is a table entry, not another branch. Compose a `JEST_JASMINE` presence observer alongside the existing `PYTEST_ADDOPTS` one.

- [ ] **Step 3: Register the adapter and advertise it**

Add `jestjson.Adapter{}` to the worker adapter slice. Advertise adapter ID `jest-json`; advertise structured schema versions `[1,2,3]`; add nothing to the MCP tool surface.

- [ ] **Step 4: Extend inspection additively**

`inspect.structured` reports observed profile, semantics coverage, observed entry counts, budget completeness reason, and retained/compacted/unavailable source state. It exposes no blob bytes and no workspace path beyond bounded logical provenance.

- [ ] **Step 5: Schema and model-truth tests**

Assert v1 clients still decode additive output; assert `task_complete`, `work_complete`, and `safe_to_finish` remain absent from structured transport; assert the raw terminal receipt is unchanged by any of this.

- [ ] **Step 6: Run integration gates and commit**

```bash
go test ./internal/app/structuredresult ./internal/app/daemon ./internal/core/capability ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'Structured|Jest|Pytest|Capability' -count=1
go test ./internal/app/daemon ./internal/app/structuredresult ./internal/adapter/ipc ./internal/adapter/mcp -race -run 'Structured|Jest' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult internal/app/daemon internal/core/capability internal/adapter/ipc internal/adapter/mcp api/schema cmd/shellbeam
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: expose jest structured results"
```

---

### Task 7: Freeze Jest fixtures and run end-to-end acceptance

**Files:**
- Create: `scripts/generate-jest-json-fixtures.sh`
- Create: `scripts/test-jest-structured-results.sh`
- Create: `tests/fixtures/jest-json/manifest.json`
- Create: frozen fixtures under `tests/fixtures/jest-json/jest-29.7.0/` and `jest-30.4.2/`
- Create: `cmd/shellbeam/structured_jest_test.go`

**Interfaces:**
- Manifest records producer version, exact generating invocation, and SHA-256 per fixture. Committed tests consume frozen bytes and need no network, no Node, and no installed Jest.
- The release script creates throwaway installs in the scratchpad for deliberate qualification only.

- [ ] **Step 1: Generate producer-realistic fixtures for both versions**

Per version: ordinary pass; ordinary failure; `test.skip`; `test.todo`; `describe.skip`; `beforeAll` throw; `beforeEach` throw; `afterAll` throw; module-level throw; `retryTimes` ending failed; `retryTimes` ending passed; `it.failing` failing; `it.failing` passing; a one-pass-plus-one-skip file for the `focused` trap; and a suite exceeding the record cap.

Exact invocation includes `--json --outputFile=<path>` with `JEST_JASMINE` unset. Freeze SHA-256 and version in the manifest.

- [ ] **Step 2: Prove fixture semantics against parser tests**

Run identical expectations across both version directories, except the `failing` dimension, which must be observable under `v30` and unavailable under `v29`. Assert no fixture yields `TestStatus = error`.

- [ ] **Step 3: Run the qualification negative matrix mechanically**

```text
JEST_JASMINE absent/present
agent-detection variables set        (must NOT change qualification)
--listTests / --collectTests / --watch / --bail / --shard / --onlyFailures
--testResultsProcessor
missing --json / missing --outputFile / --outputFile without --json
wrapper and shell-string forms
relative and absolute output paths, containment negatives
pre-existing artifact
symlinked component / final
managed same-path overlap
terminal acquire timeout / late result
blob byte and store budget
BigInt payload (producer writes nothing)
globalSetup throw (producer writes nothing)
zero-match emission (§22.5) — invocation that filters out every test;
                          record whether the producer emits a document
                          and what the parser's completeness state is
@file non-expansion (§20.1) — argv token @args.txt whose file contains
                             payload-shape-affecting flags; verify the
                             producer does not expand the file
```

No negative may mutate child truth or produce a mechanical blob-derived result.

- [ ] **Step 4: Measure document size with and without coverage**

Spec §35 defers the blob-ceiling decision to this measurement. Record, for a representative repository, the document size at `--coverage` on and off, and whether `DefaultMaxArtifactBlobBytes` is adequate. Write the number and the decision into this plan. If the default is inadequate, stop and amend the spec rather than raising a constant quietly.

- [ ] **Step 5: Run crash, retention and concurrency acceptance**

Committed blob plus daemon restart before derivation; recovery claim surviving session GC; ref-acquire versus retire race; compaction and tombstone; identical terminal/observation cuts after restart. Assert recovery never re-runs Jest and never reopens the workspace output path.

- [ ] **Step 6: Run the real daemon end-to-end path**

Disposable daemon and state root, temp git workspace, throwaway Jest install, exact qualified argv through public IPC. Poll the terminal receipt, wait for `inspect.structured`, and assert mechanical records plus independent receipt truth.

Include the two cases that matter most for trust:

```text
a run whose JSON says success:true while exit code is nonzero
  → receipt truth wins, no structured claim contradicts it

an unqualified invocation
  → child execution preserved, inspect.structured not_found
```

- [ ] **Step 7: Commit qualification evidence**

```bash
./scripts/test-jest-structured-results.sh
go test ./internal/adapter/structured/jestjson ./cmd/shellbeam -run 'Jest|Structured' -count=1
go run ./tools/devctl check
git add scripts/generate-jest-json-fixtures.sh scripts/test-jest-structured-results.sh tests/fixtures/jest-json cmd/shellbeam/structured_jest_test.go docs/superpowers/plans/2026-08-20-vitest-jest-structured-results-v1.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: qualify jest structured results"
```

---

### Task 8: Deploy Jest and answer the Vitest value-review gate

**Files:**
- Modify: `docs/superpowers/plans/2026-08-20-vitest-jest-structured-results-v1.md` — record the decision.
- Modify: `docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md` — record the §5 review outcome.

**Interfaces:**
- Produces a written decision authorizing or declining Tasks 9–12. No code.

- [ ] **Step 1: Verify and deploy `jest-json@v1`**

Full-repository verification, merged-HEAD build, deploy under the existing manual runtime model, and a live qualified Jest structured operation verified against runtime binary identity and daemon incarnation.

- [ ] **Step 2: Collect usage evidence over a real interval**

Spec §5 fixes the question:

```text
does jest-json@v1 change any P1 outcome that raw output plus exit
code did not already determine?
```

Collect, for qualified Jest operations: how many produced a derivation; how many were `complete` versus `partial` reason `pass_records_elided` versus `budget_exceeded`; how many had `invocations > 1`; how many had `jest:failing_*`; and in how many cases a P1 candidate or an agent decision actually consumed which-tests-failed rather than only whether-anything-failed.

That last count is the decisive one. If nothing consumes which-or-how-many, the adapter is telemetry rather than evidence, and the same will be true of Vitest with strictly less to offer.

- [ ] **Step 3: Decide and record**

```text
AUTHORIZED  → proceed to Task 9
DECLINED    → Tasks 9-12 are abandoned; the spec stands as a
              qualification record; close this plan at Task 13
              covering Jest only
```

Record the counts, the decision, the date, and the reasoning in both documents. A decision to decline is a successful outcome of this plan, not a failure of it.

- [ ] **Step 4: Commit the decision**

```bash
git add docs/superpowers/plans/2026-08-20-vitest-jest-structured-results-v1.md docs/superpowers/specs/2026-08-20-vitest-jest-structured-results-design.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "docs: record jest structured results value review"
```

---

### Task 9: Qualify the exact Vitest invocation

> Gated on Task 8 returning AUTHORIZED.

**Files:**
- Create: `internal/app/structuredresult/vitest_invocation.go`
- Create: `internal/app/structuredresult/vitest_invocation_test.go`
- Modify: `internal/app/structuredresult/capture_authority.go`

**Interfaces:**
- Produces `VitestInvocationBindingV1`, `QualifyVitestInvocation`, `VitestCandidateArgv`, and a canonical digest.
- Carries **no** environment-presence fact: spec §20 establishes Vitest has no argument-injection channel to prove absent.

- [ ] **Step 1: Write RED producer and run-mode tests**

```go
const VitestArgumentFileStateNotExpanded = "producer_does_not_expand"

type VitestInvocationBindingV1 struct {
    SchemaVersion         int                  `json:"schema_version"`
    ProducerForm          string               `json:"producer_form"`
    RunModeBinding        string               `json:"run_mode_binding"`
    JSONReporter          string               `json:"json_reporter"`
    OutputFile            CaptureOutputBinding `json:"output_file"`
    OutputFileForm        string               `json:"output_file_form"`
    ExcludedFlagState     string               `json:"excluded_flag_state"`
    ArgumentFileState     string               `json:"argument_file_state"`
    ArgumentFileEvidence  string               `json:"argument_file_evidence"`
    ZeroMatchEmitsArtifact bool                `json:"zero_match_emits_artifact"`
}
```

```text
qualified:   vitest run --reporter=json --outputFile.json=out.json
             vitest run --reporter=json --outputFile=out.json      (json sole reporter)
             vitest --run --reporter=json --outputFile.json=out.json
             node_modules/.bin/vitest run ...
             vitest run @acme                                     (scoped-package filter)

unqualified: no run subcommand and no --run          (watch mode)
             --watch / -w
             --reporter=json --reporter=junit --outputFile=out      (clobber risk)
             missing --reporter=json
             missing output-file binding
             npx / pnpm / yarn / npm test wrappers
             vite test
             --shard / --bail / --merge-reports
             expansion-required or non-contained output path
```

**No `@token` shape rule.** Verified empirically on Vitest 3.2.7 that `vitest run @acme --reporter=json --outputFile.json=...` correctly selects scoped-package tests (`numTotalTests: 1` for `packages/@acme/ui/ui.test.js`). Treating the leading `@` as disqualifying would reject real monorepo scoped-package filters. The non-expansion guarantee is recorded in the binding as `argument_file_state = producer_does_not_expand` (with `argument_file_evidence` carrying the producer version) and is enforced per-version by the release qualification matrix.

**Per-version emission pin (spec §22.5).** Vitest `3.2.7` emits a zero-result document when zero tests match; the binding SHALL record `zero_match_emits_artifact = true` and the parser SHALL set `Completeness=partial` with `CompletenessReason=zero_match` in that case. The fact is re-verified per qualified Vitest version by the release qualification matrix.

The multi-reporter case needs its own named test: with a plain string `--outputFile`, `getOutputFile` returns the same path for every reporter, and json and junit overwrite each other. `output_file_form` records which of the two accepted forms was used, so the digest distinguishes them.

- [ ] **Step 2: Assert no environment fact is recorded**

An explicit test that the binding carries no environment-presence member and that `VITEST_ADDOPTS`-style variables do not affect qualification. This pins spec §20 so a future contributor does not add a presence probe by analogy with pytest.

- [ ] **Step 3: Implement the resolver and prove digest sensitivity**

Same argv with a different output-file form, run-mode proof, or normalized path yields a different digest.

- [ ] **Step 4: Commit**

```bash
go test ./internal/app/structuredresult -run 'Vitest' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: qualify vitest structured invocation"
```

---

### Task 9.5: Generate Vitest real-document fixtures before pinning parser assumptions

**Files:**
- Create: `scripts/generate-vitest-real-doc-fixtures.sh`
- Create: `tests/fixtures/vitest-json/real-doc-fixtures/{manifest.json,vitest-3.2.7/,vitest-4.1.11/}`

> Same role as Task 4.5 for Jest. Sequenced BEFORE Task 10 so the v3/v4 discriminator and the zero-match emission behavior are verified against real producer output before any parser code is written.

- [ ] **Step 1: Write the manifest shell**

Generate one minimal pass-only document per version with `vitest run --reporter=json --outputFile.json=<path>`:

```bash
INSTALLED="$(npx vitest --version | awk '{print $2}')"
# run a minimal pass-only suite to /tmp/<INSTALLED>.json
# freeze bytes; record producer version (from --version, NOT package.json),
# exact argv, SHA-256 in manifest
```

- [ ] **Step 2: Generate the zero-match emission document per version**

Verified empirically on Vitest 3.2.7: an invocation that filters out every test emits a zero-result document at the declared path (`numTotalTests: 0`, `success: false`, `testResults: []`). Re-verify per version:

```text
Vitest 3.2.7    argv: vitest run --testNamePattern=__none__ --reporter=json
                          --outputFile.json=/tmp/empty.json
                file present after invocation?  YES
                numTotalTests: 0, success: false
                zero_match_emits_artifact: TRUE
                parser behavior: CompletenessPartial / reason=zero_match

Vitest 4.1.11   (same)
                file present after invocation?  [record]
                zero_match_emits_artifact:    [record]
```

- [ ] **Step 3: Generate the `@file` non-expansion document per version**

For each version, run `vitest run @args.txt --reporter=json --outputFile.json=<path>` where `args.txt` contains `--bail`. Verified on 3.2.7: 0 matches, document emitted. Re-verify per version.

- [ ] **Step 4: Verify the observed key set before any parser code is written**

For each version, dump the JSON document and verify the spec §30 table — `tags` in 4.x, no `tags` in 3.x, `benchmarks` only in 5.x. If a version's emitted key set disagrees with spec §30, stop and amend spec §30 rather than letting the parser commit to an assumed discriminator.

- [ ] **Step 5: Commit real-doc fixtures**

```bash
./scripts/generate-vitest-real-doc-fixtures.sh
go test ./tests/fixtures/vitest-json -count=1 || true
go run ./tools/devctl check
git add scripts/generate-vitest-real-doc-fixtures.sh tests/fixtures/vitest-json
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: freeze vitest real-document fixtures"
```

---

### Task 10: Implement the `vitest-json@v1` strict parser

> Gated on Task 8 returning AUTHORIZED.

**Files:**
- Create: `internal/adapter/structured/vitestjson/{types.go,profile.go,parser.go}`
- Create: `internal/adapter/structured/vitestjson/parser_test.go`
- Create: `internal/adapter/structured/vitestjson/parser_limits_test.go`
- Create: `internal/adapter/structured/vitestjson/parser_fuzz_test.go`

**Interfaces:**
- Produces `vitestjson.Adapter{}` with `ID() == "vitest-json"`, version `1`, artifact-only input.

- [ ] **Step 1: Write RED profile-discrimination tests**

`v4` declares `tags` on assertions; `v3` does not. Both declare `snapshot` and `coverageMap` as raw-message members. Try `v4` then `v3`.

```text
v4 document                          → profile v4
v3 document                          → profile v3
document carrying benchmarks (v5)    → unsupported, fails closed
document carrying an unknown member  → unsupported, fails closed
duplicate JSON member                → fails closed
```

The v5 case is deliberate: `5.0.0-rc.2` was never executed, so it is unqualified by spec §30 even though its shape is known.

- [ ] **Step 2: Write RED status tests**

```text
passed   → pass
failed   → fail
skipped  → skip + vitest:skipped
todo     → skip + vitest:todo
pending  → contradictory, record fails closed
disabled → unsupported, document fails closed
```

`pending` and `disabled` each need a comment explaining why they fail closed rather than mapping: `pending` at report time accompanies the producer's own internal-bug warning, and `disabled` is unreachable dead code whose appearance voids the profile assumption.

File-level union is only `failed | passed`. Add a fixture whose file contains only skipped tests and assert it reports `passed`, with a test comment recording that a `pass` suite record must not be read as "tests executed".

- [ ] **Step 3: Write RED anti-inference tests**

The most important tests in this task, because they pin what the adapter must refuse to do:

```text
passed with failureMessages non-empty   → pass, NO flake claim,
                                          coverage marks flake_state unavailable
only-sibling emitted as skipped         → skip, NO focus claim
success:true with a failing receipt     → receipt wins, nothing contradicts it
unhandled async error absent from JSON  → coverage marks
                                          unhandled_error_visibility unavailable
```

- [ ] **Step 3.5: Write RED zero-match parser detection**

Verified empirically on Vitest 3.2.7: an invocation that filters out every test emits a zero-result document at the declared path (`numTotalTests: 0`, `success: false`, `testResults: []`). Without a parser-level check the document decodes cleanly into the v3/v4 profile and the adapter would persist `ObservedEntryCounts.Entries=0` with `Completeness = complete`, and a P1 obligation over "did the run pass" would receive a vacuous affirmative.

The parser SHALL set `Completeness=partial` with the closed reason `zero_match`; this is distinct from `pass_records_elided`, while `budget_exceeded` remains a distinct parse outcome:

```text
zero_result_document with binding.ZeroMatchEmitsArtifact == true
  → Completeness = partial
  → CompletenessReason = zero_match
  → Records    = []  (no records persisted; no test cases were traversed)
  → ObservedEntryCounts.Entries = 0 (truthfully: zero tests selected)

zero_result_document with binding.ZeroMatchEmitsArtifact == false
  → unreachable in practice: if the producer doesn't emit on zero match,
    the document doesn't exist and Phase A returns unavailable
```

The closed `CompletenessReason` enum from Task 0 SHALL be used. RED test: feed the Task 9.5 zero-match fixture for Vitest 3.2.7 and assert `ParseOutcome=partial`, `Completeness=partial`, `CompletenessReason=zero_match`, NOT `complete`.

- [ ] **Step 4: Implement the parser**

`maxVitestJSONRecords = 8192` and the shared observed-entry ceiling. `duration` is absent for skipped and todo entries; `endTime` is a non-integer float and is not persisted as a fact. Apply the shared failure-first budget. Emit no `TestSuiteAggregate`.

- [ ] **Step 5: Emit coverage and commit**

Coverage exactly as spec §53, `family` from the observed profile.

```bash
go test ./internal/adapter/structured/vitestjson ./internal/core/structuredresult -count=1
go test ./internal/adapter/structured/vitestjson -race -count=1
go test ./internal/adapter/structured/vitestjson -run '^$' -fuzz=FuzzVitestJSON -fuzztime=30s
go run ./tools/devctl check
git add internal/adapter/structured/vitestjson
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: parse vitest json structured results"
```

---

### Task 11: Wire Vitest into the shared surfaces

> Gated on Task 8 returning AUTHORIZED.

**Files:**
- Modify: `internal/app/structuredresult/selection.go`, `selection_test.go`
- Modify: `internal/app/daemon/admission.go`, `project_command.go` + tests
- Modify: `cmd/shellbeam/execution_structured_capture.go`, `execution_observation.go` + tests
- Modify: `internal/core/capability/catalog.go` + tests
- Modify: `internal/adapter/ipc`/`mcp` structured tests, `api/schema/*.json`

**Interfaces:**
- Adds one entry to the producer table established in Task 6. If this task needs a new branch rather than a table entry, that table was wrong and Task 6 should be revisited.

- [ ] **Step 1: Write RED selection and admission tests**

Auto-selection only on a fully qualified binding; explicit mismatch returns a typed precondition error before spawn. Assert no argv qualifies for both Jest and Vitest.

- [ ] **Step 2: Register, advertise, wire**

Add `vitestjson.Adapter{}` to the worker slice and `vitest-json` to the advertised adapter IDs. Add the producer-table entry. No new MCP tool.

- [ ] **Step 3: Prove the other three adapters are untouched**

```bash
go test ./internal/app/structuredresult ./internal/app/daemon ./internal/core/capability ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'Structured|Vitest|Jest|Pytest|GoTest|GoVet|Capability' -count=1
go test ./internal/app/daemon ./internal/app/structuredresult -race -run 'Structured|Vitest' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult internal/app/daemon internal/core/capability internal/adapter/ipc internal/adapter/mcp api/schema cmd/shellbeam
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: expose vitest structured results"
```

---

### Task 12: Freeze Vitest fixtures and run end-to-end acceptance

> Gated on Task 8 returning AUTHORIZED.

**Files:**
- Create: `scripts/generate-vitest-json-fixtures.sh`, `scripts/test-vitest-structured-results.sh`
- Create: `tests/fixtures/vitest-json/manifest.json` + frozen fixtures for `3.2.7` and `4.1.11`
- Create: `cmd/shellbeam/structured_vitest_test.go`

- [ ] **Step 1: Generate producer-realistic fixtures for both versions**

Per version: pass; failure; `skip`; `todo`; `only` with siblings; `beforeEach` throw; collection throw; retry ending failed; retry ending passed; unhandled async error with `success:true`; a file of only skipped tests; and a suite exceeding the record cap.

- [ ] **Step 2: Prove fixture semantics and the anti-inference contract**

Same expectations across both directories. Assert the retry-ending-passed fixture yields `pass` with no flake claim, and the `only` fixture yields siblings as `skip` with no focus claim.

- [ ] **Step 3: Run the qualification negative matrix**

The Task 9 unqualified table, plus pre-existing artifact, symlinked component and final, managed same-path overlap, acquire timeout and late result, and blob budget negatives.

Include the config-redirection case explicitly: a config file whose reporter-level `outputFile` points elsewhere must yield capture-unavailable, and must never yield bytes from another path. This is the mechanical proof of the spec §19 fail-closed argument.

Also include the per-version release-qualification tests from spec §20.1 and §22.5:

```text
zero-match emission          invocation that filters out every test;
                             record whether the producer emits a document
                             and what the parser's completeness state is.
                             Vitest 3.2.7 emits and the parser SHALL set
                             CompletenessPartial / reason=zero_match.

@file non-expansion          argv token @args.txt whose file contains
                             payload-shape-affecting flags; verify the
                             producer does not expand the file. Both Vitest
                             3.2.7 and 4.1.11 verified [RUN 3.2.7].

scoped-package @filter       vitest run @acme ... — must qualify, not be
                             rejected by any shape rule.
```

- [ ] **Step 4: Crash, retention and concurrency acceptance**

As Task 7 Step 5, for Vitest.

- [ ] **Step 5: Real daemon end-to-end path**

As Task 7 Step 6, including the `success:true`-with-nonzero-exit case, which is a verified Vitest behavior rather than a hypothetical.

- [ ] **Step 6: Commit**

```bash
./scripts/test-vitest-structured-results.sh
go test ./internal/adapter/structured/vitestjson ./cmd/shellbeam -run 'Vitest|Structured' -count=1
go run ./tools/devctl check
git add scripts/generate-vitest-json-fixtures.sh scripts/test-vitest-structured-results.sh tests/fixtures/vitest-json cmd/shellbeam/structured_vitest_test.go
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: qualify vitest structured results"
```

---

### Task 13: Final verification, evidence freeze, merge and deploy

**Files:**
- Modify only plan/spec/evidence docs required to record terminal verification after implementation bytes are committed.
- Do not change production bytes after the final source checkpoint unless verification finds a defect; a defect returns to its owning task with RED first.

- [ ] **Step 1: Static architecture and compatibility audit**

Mechanically scan that the implementation contains: one MCP tool; no generic "JavaScript test JSON" adapter; no injected producer flag; no reporter or runner install on the execution path; no workspace-path reopen after Phase A; no session-retention blob deletion; no parser-based child-outcome rewrite; no read of any producer `success` field; no `TestStatus = error` emitted by either new adapter; no document-order record truncation; and no derivation of any fact from failure text.

- [ ] **Step 2: Focused package and race gates**

```bash
go test ./internal/core/structuredresult ./internal/core/operation ./internal/core/capability ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/localfs ./internal/adapter/store ./internal/adapter/structured/... ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -count=1
go test -race ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/store ./internal/adapter/structured/... ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl check
git diff --check
go run ./tools/devctl test --dirty --base main
```

- [ ] **Step 3: Release qualification and full repository verification**

```bash
./scripts/test-pytest-structured-results.sh
./scripts/test-jest-structured-results.sh
./scripts/test-vitest-structured-results.sh   # only if Task 8 authorized Vitest
go test ./... -count=1
go run ./tools/devctl verify --checkpoint --base main --json
```

Require `command=verify`, `selection=full`, `status=passed`, `exit_code=0`, plus the exact committed source fingerprint and final feature HEAD. Running the pytest script here is mandatory, not optional: Tasks 1–3 changed shared core and capture-authority code that pytest depends on.

- [ ] **Step 4: Concurrent-main audit before merge**

Fetch `origin/main`, compare local `main`, the feature merge-base, and changed-file overlaps. Rebase only if main has advanced; resolve semantic overlaps individually and rerun affected or full gates as required. Never merge stale verification evidence across a changed source fingerprint.

- [ ] **Step 5: Merge and deploy only after final checkpoint authority**

Fast-forward local main when possible, verify merged HEAD and tree, build the exact merged binary, deploy under the existing manual runtime model, and verify runtime binary identity, daemon incarnation, and one live qualified structured operation per delivered adapter. Do not push or open a PR unless separately requested.

- [ ] **Step 6: Cleanup only after live deploy proof**

Remove the implementation worktree and branch only when merged main is verified and deployed and no unmerged work remains. Preserve every fixture and evidence artifact.

---

## Spec Coverage Map

| Spec sections | Implementation owner |
|---|---|
| 1 decision, 5 delivery gate | Task 8 |
| 2 evidence basis, 30–31 profiles/matrix | Tasks 4.5, 5, 7, 9.5, 10, 12 |
| 3–4 retained contracts and reuse | Task 3 (amends §4), Task 13 audit |
| 6 non-goals | Task 13 audit |
| 7–10 artifact_blob decision, no new input kind | Task 3 |
| 11–12 adapter identity and payload divergence | Tasks 4, 5, 9, 10 |
| 13–14 six gates, unified binding | Tasks 3, 4, 9 |
| 15–20.1 Vitest producer/watch/reporter/output/redirection/env/argfile | Task 9, Task 9.5, Task 12 negatives |
| 21–26 Jest producer/binding/excluded flags/env/mutation | Task 4, Task 7 negatives |
| 22.5 zero-match emission per producer version | Task 4.5, Task 9.5, Task 10 Step 3.5 |
| 27–29 config deferred, no schema version, strict profile gate | Tasks 5, 10 |
| 32–33 parser bounds, failure-first budget | Task 1, Tasks 5 and 10 |
| 34 failure excerpts (retention-containment gate) | Task 2 Step 5 (gated) |
| 35 coverage payload ceiling | Task 7 Step 4 |
| 36 multi-project unqualified | Tasks 4, 9 |
| 37–40 Vitest status/focus/retry/success | Task 10 |
| 41–43 Jest status/failing/invocations | Task 5 |
| 44–45 hook phase and error status unavailable | Tasks 5, 10 |
| 46–47.2 suite mapping, aggregates unavailable, observed counts, terminal metadata | Task 0, Task 1, Tasks 5 and 10 |
| 48–52 address/identity/order/duration/record count | Tasks 5, 10 |
| 53–54 coverage declarations and P1 bridge/sufficiency boundary | Task 0, Tasks 5, 6, 10, 11 |
| 55–56 selection precedence and explicit mismatch | Tasks 6, 11 |
| 57–61 security/limits/capability/observability/no auto-repair | Tasks 6, 11, 13 |
| 62–66 invariants/deferred/sequencing/acceptance | Task 3 (amends §62), Task 13 |

## Execution Order After This Plan

```text
terminal metadata contract → failure-first core primitives → generalized capture authority
→ jest-json@v1 → verify → deploy
→ value-review gate
→ vitest-json@v1 (only if authorized) → verify → deploy
→ ESLint native-machine-contract design, then plan
→ TypeScript compiler only after explicit qualification
```

Two outcomes are acceptable at the gate. Building Vitest because it was already specified is not one of them.
