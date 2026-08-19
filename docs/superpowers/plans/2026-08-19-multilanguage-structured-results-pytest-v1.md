# Multi-language Structured Results — Pytest V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the shared immutable artifact-input foundation and the first zero-runtime-dependency provider, `pytest-junit-xml@v1`, without weakening E22 raw-output identity, terminal receipt truth, P1 sufficiency, or filesystem provenance.

**Architecture:** Structured-result schema v2 replaces raw-only input identity with a closed `StructuredInputRef` union while preserving read compatibility for historical schema-v1 Go derivations. Pytest qualification freezes one canonical invocation authority before spawn, captures an explicitly requested xunit2 JUnit artifact through descriptor-relative baseline/terminal pinning, materializes it into ShellBeam-owned immutable blob storage, and parses only that blob with a bounded producer-specific XML adapter. Blob references and retirement share the structured-store serialization domain; ordinary session retention is independent; recovery never reopens the workspace result path.

**Tech Stack:** Go 1.26.6; stdlib `encoding/xml`, `crypto/sha256`, `encoding/json`, `io`; existing `golang.org/x/sys/unix`; existing file-backed atomic store, environment presence observer, IPC/MCP v2 schemas, P1 verification semantics; pytest built-in JUnit XML only; no new ShellBeam runtime dependency and no pytest plugin dependency.

**Spec:** `docs/superpowers/specs/2026-08-19-multilanguage-structured-results-design.md`

## Global Constraints

- Execution base is local `main` commit `27207d94b097040b571081c8c49d9c09487460c5`; clean baseline `go test ./... -count=1` passed before this plan was written.
- Implement only the shared artifact-input foundation plus `pytest-junit-xml@v1`. Vitest/Jest and ESLint receive separate follow-on plans after this foundation is proven. TypeScript compiler remains deferred pending qualification.
- TDD is mandatory: focused RED -> minimal GREEN -> focused regression -> structural/dirty gate -> commit. Never create production code first and backfill tests.
- Keep one MCP tool, `local_shell`. Do not add Python/JS/TS-specific top-level tools or a parallel evidence ontology.
- Raw output and durable terminal receipts remain canonical execution truth. Structured records never overwrite child outcome, exit evidence, receipt identity, or P1 gate truth.
- New structured-result writes use schema v2. Historical schema-v1 raw-output derivations/records remain readable without rewriting their persisted bytes or derivation identity. For an all-raw source set, v2 derivation identity SHALL deliberately reuse the legacy v1 canonical key envelope so the same raw refs + producer/schema/config keep the exact same derivation key across persistence-schema migration. Any artifact/mixed input uses the v2 tagged-union key envelope.
- `StructuredInputRef` is a closed tagged union: `raw_output | artifact_blob`; exactly one matching branch is valid.
- `go-test-json` and `go-vet-json` retain existing selection and native mechanical mappings. The schema migration must not add pre-spawn or parser tax to ordinary/raw Go execution.
- `pytest-junit-xml@v1` uses pytest built-in JUnit XML only. ShellBeam never installs plugins, appends `--junitxml`, appends `-o junit_family=xunit2`, appends `-o addopts=`, edits config, deletes stale artifacts, or reconstructs pytest semantics from console/prose.
- Pytest V1 producer forms are exact resolved `pytest ...` and `python -m pytest ...`; wrappers/shell strings are not auto-qualified.
- Qualification requires explicit built-in JUnit output, explicit effective `junit_family=xunit2`, effective empty config `addopts`, mechanically proven `PYTEST_ADDOPTS` absence, no argument-file source, expansion-free output path, and one normalized path resolved from frozen `ResolvedCWD`.
- The canonical `PytestInvocationBindingV1` payload is durable identity authority. `ArtifactCaptureIntent.ProducerBindingDigest` commits the entire payload, including environment-absence/addopts/argfile facts. Replay/recovery reuses it and never re-observes current environment to recreate qualification.
- Ordinary-path baseline and Phase A use the same pinned descriptor authority and no-follow traversal. A pathname-stat baseline is never V1 authority.
- For the implementation mechanism, hold one descriptor to the already-qualified parent directory across child execution. Baseline proves the final name absent with `fstatat(..., AT_SYMLINK_NOFOLLOW)`; Phase A opens that final name with `openat(..., O_NOFOLLOW)` against the same parent object.
- Managed same-path overlap invalidates *all* overlapping capture claims for that normalized path. A newcomer is not the only unqualified operation; the prior active claimant must also lose mechanical attribution.
- Terminal Phase A runs after child reap/output drain and before managed-shell authority release / durable receipt publication. Full blob copy remains asynchronous after receipt publication.
- Phase A acquisition wait is bounded. The implementation SHALL use `MaxTerminalArtifactAcquireDuration = 250ms`; late acquisition results close their FD and cannot resurrect authority.
- V1 defaults: `DefaultMaxArtifactBlobBytes = 16 MiB`, protocol ceiling `64 MiB`; `DefaultMaxArtifactBlobStoreBytes = min(256 MiB, available state authority after ControlReserve)`; `MaxActiveArtifactPathAuthoritiesGlobal = 4`; `MaxPinnedArtifactHandlesGlobal = 4`; acquisition concurrency `4`; materialization workers `2`; materialization queue depth `4` with the pinned-handle semaphore counting queued + active handles.
- Pytest V1 has exactly one artifact capture intent. Generic capture-intent protocol ceiling remains `8`.
- Parser bounds: max artifact bytes `16 MiB` by default; max persisted structured records `1024`; XML depth `32`; XML element count `8192`; per text/attribute field `64 KiB`; adapter parse duration remains bounded by structured worker limits.
- Blob ID is operation-scoped storage identity, not content identity. No cross-run blob dedupe/refcount in V1.
- Blob materialization uses same-filesystem private staging, data+metadata fsync, atomic directory rename, parent fsync, then and only then mint `ArtifactBlobRef`.
- `ArtifactBlobRef` identity payload, `terminal_cut`, and `observation_cut` are persisted canonically and reused byte-for-byte on recovery.
- A committed-but-unbound blob owns a structured recovery claim independent of session/operation bulk retention. Blob reference acquisition and retirement use the same `structuredMu`/durable reference domain.
- Never implicitly evict referenced live blob bytes under pressure. Reject new capture with typed budget/unavailable state instead.
- Production files target 150–300 lines, review above 350, hard cap 500; test files review above 600, hard cap 800; functions review above 60, hard cap 80.
- Broad gates use `go run ./tools/devctl check`, `go run ./tools/devctl test --dirty --base main`, targeted `go test -race`, and one deliberate final `go test ./... -count=1` + `go run ./tools/devctl verify --checkpoint --base main --json`.
- No push/PR in this plan unless explicitly requested.

---

## File Structure

### Core schema and compatibility

- Create `internal/core/structuredresult/input_ref.go` + tests — schema-v2 `StructuredInputRef`, `ArtifactBlobRef`, closed terminal/observation cuts.
- Create `internal/core/structuredresult/producer_metadata.go` + tests — producer disposition envelope, semantic coverage, artifact entry identity/address, suite aggregate fields.
- Modify `internal/core/structuredresult/derivation.go`, `record.go` + tests — schema-v2 source union and metadata while retaining explicit schema-v1 validation/key path.
- Create `internal/adapter/store/structured_legacy.go` + tests — strict v1 JSON decode/normalization without persisted rewrite.

### Qualification and pre-spawn capture authority

- Create `internal/app/structuredresult/pytest_invocation.go` + tests — exact option-aware pytest resolver and canonical binding digest.
- Create `internal/app/structuredresult/capture_authority.go` + tests — capture intent/qualification orchestration ports.
- Modify `internal/core/operation/persistence.go`, `intent.go`, store reservation validation/tests — durable `StructuredCaptureDigest` replay binding without importing provider-specific core types into operation.
- Create/modify structured store capture-authority files under `internal/adapter/store/` — durable canonical binding + intent record before spawn.

### Descriptor-safe capture/materialization

- Create `internal/adapter/localfs/artifact_capture_unix.go`, `artifact_capture_test.go`, `artifact_capture_race_test.go`; add unsupported-platform stub — pinned parent-dir baseline/Phase-A open.
- Create `internal/app/structuredresult/capture.go`, `capture_test.go` — managed path claims, resource reservations, acquisition timeout, late-result discard.
- Create `internal/adapter/store/structured_artifact_blob.go`, `structured_artifact_blob_test.go`, `structured_artifact_blob_fault_test.go` — blob staging/commit/resolve/budget/recovery claims/reference index/tombstones.
- Modify `internal/adapter/store/repository.go`, state accounting/retention/reconcile files and tests — private dirs, bounded accounting, startup ordering, session-retention independence.

### Worker/parser/public integration

- Modify `internal/app/structuredresult/ports.go`, `input.go`, `worker.go`, `service.go`, `inspect.go` + tests — union reader, raw compatibility, artifact scheduling, semantics coverage inspection.
- Create `internal/adapter/structured/pytestjunit/parser.go`, `types.go`, tests and frozen fixtures — bounded xunit2 producer-specific parser.
- Modify daemon admission/finalization/composition + tests — qualification before spawn, Phase A before receipt, Phase B after durable receipt.
- Modify capability/IPC/MCP schemas/tests — advertise adapter/capture limits and allow schema-v1/v2 structured records without a second tool.
- Add `scripts/generate-pytest-junit-fixtures.sh` and `tests/fixtures/pytest-junit/manifest.json` — reproducible release-qualification fixture provenance for pytest `8.4.2` and `9.1.1`; committed tests consume frozen XML and never require network/pytest at runtime.

---

### Task 1: Define structured-result schema v2 and producer metadata

**Files:**
- Create: `internal/core/structuredresult/input_ref.go`
- Create: `internal/core/structuredresult/input_ref_test.go`
- Create: `internal/core/structuredresult/producer_metadata.go`
- Create: `internal/core/structuredresult/producer_metadata_test.go`
- Modify: `internal/core/structuredresult/derivation.go`
- Modify: `internal/core/structuredresult/derivation_test.go`
- Modify: `internal/core/structuredresult/record.go`
- Modify: `internal/core/structuredresult/record_test.go`

**Interfaces:**
- Produces additive schema-v2 `StructuredInputRef`, `ArtifactBlobRef`, cut types and producer metadata consumed by later tasks without breaking the existing raw-only public structs in this commit.
- Keep existing `DerivationKey([]RawOutputRef, ...)` as the exact legacy/raw API and algorithm. Add `DerivationKeyForInputs([]StructuredInputRef, ...)`: all-raw refs delegate to `DerivationKey` and therefore produce the exact legacy key; any artifact/mixed refs use the v2 tagged-union envelope. Persistence schema version alone never changes raw derivation identity.

- [x] **Step 1: Write RED closed-union and blob-ref identity tests**

Use exact public shapes:

```go
const (
    SchemaVersionV1 = 1
    SchemaVersion   = 2
)

type StructuredInputRef struct {
    Kind         StructuredInputKind `json:"kind"`
    RawOutput    *RawOutputRef       `json:"raw_output,omitempty"`
    ArtifactBlob *ArtifactBlobRef    `json:"artifact_blob,omitempty"`
}

type ArtifactBlobRef struct {
    SchemaVersion int               `json:"schema_version"`
    BlobID string                   `json:"blob_id"`
    OperationID, SessionID string
    RepositoryID, WorkspaceID string
    DeclaredPath, NormalizedWorkspacePath string
    SHA256 string
    Size int64
    TerminalCut TerminalCutV1       `json:"terminal_cut"`
    ObservationCut ObservationCutV1 `json:"observation_cut"`
}
```

Tests reject unknown kind, zero/two branches, invalid digest/size/path/cut, and prove equal content does not imply equal BlobID/provenance.

Run: `go test ./internal/core/structuredresult -run 'StructuredInput|ArtifactBlob|Cut' -count=1`
Expected: FAIL because v2 input types do not exist.

- [x] **Step 2: Implement the minimal v2 union/cut validators**

`TerminalCutV1` binds receipt schema version + 64-hex canonical receipt digest. `ObservationCutV1` binds schema version `1` + 64-hex canonical observation payload digest. No timestamp/random/current-daemon fields are accepted as identity.

- [x] **Step 3: Write RED tests for producer metadata and artifact entry identity**

```go
type ProducerTestDisposition struct { Namespace string; VocabularyVersion int; Code string }
type ProducerSemanticsCoverage struct { Namespace string; VocabularyVersion int; Format, Family string; MechanicallyObservable, Unavailable []string }
type ArtifactTestEntryRef struct { ArtifactBlobID string; SuiteOrdinal, TestcaseOrdinal int }
type ProducerTestAddress struct { Namespace string; VocabularyVersion int; SuiteName, Classname, Name string }
type TestSuiteAggregate struct { Tests, Failures, Errors, Skipped int }
```

Core validates bounded envelopes only; it does not switch on `pytest:xfail` to decide verification truth.

- [x] **Step 4: Add v2 key/metadata contracts without breaking current raw structs**

Add `DerivationKeyForInputs`, producer disposition/coverage/address/entry-ref/aggregate validators and deterministic artifact `RecordID` helper, but leave existing `Derivation.SourceAuthorityRefs []RawOutputRef` and `Record.SourceRef RawOutputRef` unchanged until Task 2 migrates their callers atomically.

Add an explicit regression proving `DerivationKeyForInputs(v2 raw union) == DerivationKey(raw refs)` for the same producer/schema/config.

Keep `TestStatus` exactly `pass|fail|skip|error`.

- [x] **Step 5: Run focused regression and commit**

```bash
go test ./internal/core/structuredresult -count=1
go run ./tools/devctl check
git add internal/core/structuredresult
git diff --cached --check
git commit -m "feat: define artifact structured input contracts"
```

---

### Task 2: Preserve schema-v1 raw results and migrate current Go adapters to v2 writes

**Files:**
- Create: `internal/adapter/store/structured_legacy.go`
- Create: `internal/adapter/store/structured_legacy_test.go`
- Modify: `internal/adapter/store/structured_results.go`
- Modify: `internal/adapter/store/structured_results_private.go`
- Modify: `internal/adapter/store/structured_records.go`
- Modify: `internal/app/structuredresult/ports.go`
- Modify: `internal/app/structuredresult/input.go`
- Modify: `internal/app/structuredresult/worker.go`
- Modify: `internal/app/structuredresult/service.go`
- Modify: `internal/adapter/structured/gojson/test.go`
- Modify: `internal/adapter/structured/gojson/vet.go`
- Test corresponding existing package tests.

**Interfaces:**
- Produces strict `decodeStructuredDerivation` / `decodeStructuredRecordSet` read boundaries that normalize v1 raw refs in memory without changing historical derivation keys.
- Atomically migrates core `Derivation.SourceAuthorityRefs` and `Record.SourceRef` to `StructuredInputRef` together with `Adapter.Parse`/`Reader`; raw binder returns `StructuredInputRef{Kind: raw_output}` so no intermediate commit breaks dependent packages.

- [ ] **Step 1: Write RED persisted-v1 compatibility fixtures**

Write literal schema-v1 JSON using the old `RawOutputRef` shape and old derivation-key algorithm. Reopen store and assert `inspect.structured` returns the historical derivation key/records, source normalized to raw input, with no file rewrite (`stat`/bytes unchanged).

Run: `go test ./internal/adapter/store -run 'StructuredLegacy|StructuredV1' -count=1`
Expected: FAIL until legacy decode exists.

- [ ] **Step 2: Implement strict legacy decode and atomically migrate the current in-memory structs to v2**

Schema `1` decodes through store-local legacy structs and validates with the existing raw `DerivationKey`; schema `2` decodes into `Derivation.SourceAuthorityRefs []StructuredInputRef` / `Record.SourceRef StructuredInputRef` and validates with `DerivationKeyForInputs`. Unknown schema fails closed. `PutDerivation`/`PutRecords` create only v2 for new writes. If a v2 raw replay resolves an already persisted v1 derivation with the same legacy key, treat it as the same immutable derivation identity; never overwrite the v1 bytes merely to upgrade schema. Add optional terminal-only `Derivation.SemanticsCoverage`; it is not derivation identity.

- [ ] **Step 3: Generalize reader/adapter APIs and keep Go adapters raw-only in the same compile-safe change**

```go
type Adapter interface {
    ID() string
    Version() int
    Parse(context.Context, core.StructuredInputRef, Reader, Limits) (ParseResult, error)
}

type Reader interface {
    ReadInputRange(context.Context, core.StructuredInputRef, int64, int) ([]byte, error)
    DescribeInput(context.Context, core.StructuredInputRef) (InputContext, error)
}
```

Go adapters reject non-raw input. Raw binder/input reader preserves exact terminal range/hash semantics.

- [ ] **Step 4: Prove no Go behavior regression**

```bash
go test ./internal/core/structuredresult ./internal/app/structuredresult ./internal/adapter/structured/gojson ./internal/adapter/store -count=1
go test ./internal/app/structuredresult ./internal/adapter/structured/gojson -race -count=1
go run ./tools/devctl check
```

- [ ] **Step 5: Commit**

```bash
git add internal/core/structuredresult internal/app/structuredresult internal/adapter/structured/gojson internal/adapter/store
git diff --cached --check
git commit -m "feat: migrate structured results to input v2"
```

---

### Task 3: Qualify exact pytest invocation authority and bind it into replay identity

**Files:**
- Create: `internal/app/structuredresult/pytest_invocation.go`
- Create: `internal/app/structuredresult/pytest_invocation_test.go`
- Create: `internal/app/structuredresult/capture_authority.go`
- Create: `internal/app/structuredresult/capture_authority_test.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/intent_test.go`
- Modify: `internal/adapter/store/reservation.go`
- Modify: reservation/replay tests.

**Interfaces:**
- Produces canonical `PytestInvocationBindingV1`, `JUnitOutputBinding`, `ArtifactCaptureIntent`, `CaptureAuthority`, and `ProducerBindingDigest`.
- Operation layer persists only `StructuredCaptureDigest` in reservation/observation fingerprint; provider-specific canonical authority stays in structured state, avoiding an operation↔structuredresult import cycle.

- [ ] **Step 1: Write RED table tests for option-aware pytest resolution**

Cover exact producer forms, all four JUnit options, repeated final-value behavior, `--` termination, option arity, wrapper negatives, any `@...` token negative, `-o/--override-ini` forms, effective empty `addopts`, xunit2/legacy ordering, relative path from frozen `ResolvedCWD`, in-workspace absolute path, `~`/env-expansion negatives.

- [ ] **Step 2: Add bounded `PYTEST_ADDOPTS` presence authority input**

The qualifier consumes a small pre-spawn host-presence port returning only `Name`, `Present`, authority schema, frozen execution context and deterministic digest. The digest is `H(schema + execution_context + name + present)` under a versioned canonical envelope. Test absent -> qualified and present -> unqualified. The canonical binding includes this exact fact; no environment value is stored.

- [ ] **Step 3: Implement canonical digest and replay-stable capture intent**

```go
type PytestInvocationBindingV1 struct {
    SchemaVersion int
    ProducerForm string
    JUnitOutput JUnitOutputBinding
    JUnitFamilyOverride string
    ConfigAddoptsOverride string
    ArgumentFileState string
    PytestAddoptsEnvironmentFact EnvironmentPresenceFact
}
```

`ProducerBindingDigest()` hashes canonical JSON. `ArtifactCaptureIntent` includes adapter/path/limits/digest/baseline identity. Same argv with a different environment-absence/addopts/argfile fact must produce a different digest.

- [ ] **Step 4: Extend operation observation replay identity only with the capture digest**

Add bounded `StructuredCaptureDigest string` to `Reservation` and `ObservationBinding`; bump only the modern observation-binding envelope version. Legacy fingerprints remain byte-for-byte unchanged when the field is empty. Replay of an already admitted operation uses stored capture authority and MUST NOT re-observe `PYTEST_ADDOPTS`.

- [ ] **Step 5: Run focused tests and commit**

```bash
go test ./internal/app/structuredresult ./internal/core/operation ./internal/adapter/store -run 'Pytest|Capture|Structured|ObservationBinding|Reservation' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult internal/core/operation internal/adapter/store
git diff --cached --check
git commit -m "feat: qualify pytest structured invocation"
```

---

### Task 4: Persist capture authority and establish descriptor-relative absent baseline

**Files:**
- Create: `internal/adapter/store/structured_capture_authority.go`
- Create: `internal/adapter/store/structured_capture_authority_test.go`
- Create: `internal/adapter/localfs/artifact_capture_unix.go`
- Create: `internal/adapter/localfs/artifact_capture_unsupported.go`
- Create: `internal/adapter/localfs/artifact_capture_test.go`
- Create: `internal/adapter/localfs/artifact_capture_race_test.go`
- Create: `internal/app/structuredresult/capture.go`
- Create: `internal/app/structuredresult/capture_test.go`
- Modify: `internal/adapter/store/structured_results.go`
- Modify: `internal/adapter/store/structured_results_private.go`

**Interfaces:**
- Produces durable `ReserveCaptureAuthority`/`FindCaptureAuthority` with create-only replay conflict semantics.
- Produces process-local `ArtifactPathAuthority` holding a pinned parent-dir FD and final basename from baseline through Phase A.

- [ ] **Step 1: Write RED durable authority/replay tests**

Persist canonical invocation binding + capture intent before spawn. Exact replay is idempotent; same operation with different producer binding/path/baseline is conflict. Reopen store and recover identical canonical bytes/digest without environment observation.

- [ ] **Step 2: Write RED descriptor baseline tests**

Baseline opens workspace root/parent using `open/openat + O_DIRECTORY|O_CLOEXEC|O_NOFOLLOW`, checks final name with `fstatat(..., AT_SYMLINK_NOFOLLOW)`, requires `ENOENT`, and holds the parent FD. Reject symlinked component, final pre-existing file, parent swap, outside path and unsupported platform.

- [ ] **Step 3: Implement finite path-authority capacity and managed collision registry**

Use global capacity `4`. Key active claims by `(workspace_id, normalized_workspace_path)`. When overlap occurs, durably mark every current claimant and the newcomer `managed_path_collision`; no claimant on that overlap may later produce an `ArtifactBlobRef`. Child execution remains allowed.

- [ ] **Step 4: Bind pre-spawn ordering**

Capture authority order is: resolve execution argv/CWD -> qualify pytest -> acquire path-authority slot -> descriptor baseline -> persist capture authority -> reserve operation with `StructuredCaptureDigest` -> register/confirm managed path claim -> spawn. On reservation/spawn failure, release parent FD/claim and durably abandon prepared capture authority.

- [ ] **Step 5: Focused race/security gate and commit**

```bash
go test ./internal/adapter/localfs ./internal/app/structuredresult ./internal/adapter/store -run 'Artifact|Capture|Baseline|Collision' -count=1
go test ./internal/adapter/localfs ./internal/app/structuredresult -race -run 'Artifact|Capture|Collision' -count=1
go run ./tools/devctl check
git add internal/adapter/localfs internal/app/structuredresult internal/adapter/store
git diff --cached --check
git commit -m "feat: bind structured artifact capture authority"
```

---
### Task 5: Acquire terminal artifact source before receipt publication without blocking terminal truth

**Files:**
- Create: `internal/app/structuredresult/capture_terminal.go`
- Create: `internal/app/structuredresult/capture_terminal_test.go`
- Modify: `internal/adapter/localfs/artifact_capture_unix.go`
- Modify: `internal/adapter/localfs/artifact_capture_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/structured_adapter.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/structured_worker_test.go`
- Modify: `internal/app/daemon/terminal_lifecycle_internal_test.go`

**Interfaces:**
- Produces `ArtifactSourceHandle` (process-local, non-serializable, single-owner) and `TerminalCaptureResult`.
- Adds daemon-side `StructuredCaptureTerminal` port separate from the post-receipt parser scheduler so terminal Phase A can run before managed-shell lease release.

- [ ] **Step 1: Write RED lifecycle/ordering tests around ordinary terminal completion**

Instrument a test capture owner and assert exact sequence for an artifact-backed reservation:

```text
child reaped + output drained
→ terminal capture acquire requested
→ terminal capture acquire resolved or deadline expires
→ managed shell lease ends
→ child resources released
→ durable terminal receipt published
→ post-receipt structured materialization scheduled
```

The existing raw-output Go adapter path remains `publish receipt → schedule raw structured worker`; it must not run Phase A.

- [ ] **Step 2: Write RED acquisition deadline and late-result tests**

Use a controllable localfs acquire hook. Assert finalizer waits no longer than `250ms`, terminal receipt still publishes, a helper that returns after deadline closes any acquired FD, and no late success can update the capture from unavailable to qualified.

- [ ] **Step 3: Implement `ArtifactSourceHandle` and exact source-object observation**

The handle owns parent FD + final file FD + frozen source identity/size + capture authority ID. It exposes only `Read`, `StatIdentity`, and `Close`; it has no JSON tags and cannot be stored. Final open uses held parent FD + `openat(O_RDONLY|O_CLOEXEC|O_NOFOLLOW|O_NONBLOCK)`, then `fstat`; require regular file and `size <= DefaultMaxArtifactBlobBytes`.

- [ ] **Step 4: Reserve finite resources before final open**

Acquire in this order: acquisition worker slot -> pinned-handle semaphore -> materialization queue slot -> blob-byte reservation capability from the store -> final `openat`. If any reservation fails, return typed capture unavailable/budget result without opening final artifact FD. A queued job with an FD still consumes the global pinned-handle slot.

- [ ] **Step 5: Implement daemon Phase-A ordering without changing receipt truth**

Add a small capture owner to daemon options. `finishTerminal` and terminal failure paths call it only when reservation has a structured capture digest/authority. Phase-A result is structured metadata; it must not alter `receipt.State`, `Outcome`, `Exit`, `Signal`, `OutputComplete`, or execution fingerprint.

- [ ] **Step 6: Run focused race/lifecycle tests and commit**

```bash
go test ./internal/adapter/localfs ./internal/app/structuredresult ./internal/app/daemon -run 'Artifact|Capture|Terminal|StructuredWorker' -count=1
go test ./internal/app/structuredresult ./internal/app/daemon -race -run 'Artifact|Capture|Terminal' -count=1
go run ./tools/devctl check
git add internal/adapter/localfs internal/app/structuredresult internal/app/daemon
git diff --cached --check
git commit -m "feat: pin structured artifacts at terminal"
```

---

### Task 6: Materialize immutable artifact blobs and durable recovery claims

**Files:**
- Create: `internal/adapter/store/structured_artifact_blob.go`
- Create: `internal/adapter/store/structured_artifact_blob_private.go`
- Create: `internal/adapter/store/structured_artifact_blob_test.go`
- Create: `internal/adapter/store/structured_artifact_blob_fault_test.go`
- Create: `internal/app/structuredresult/materializer.go`
- Create: `internal/app/structuredresult/materializer_test.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/adapter/store/structured_results_private.go`
- Modify: `internal/adapter/store/admission.go` or state-accounting file owning `MaxTotalState` reservations
- Modify focused store root/fault tests.

**Interfaces:**
- Produces `ArtifactBlobRepository` with create-only `CommitArtifactBlob`, `ResolveArtifactBlob`, `ReserveBlobBytes`, `ReleaseBlobReservation`, `PutRecoveryClaim`, `GetRecoveryClaim`.
- `Materializer.Materialize(TerminalCaptureResult, receipt.Receipt)` consumes and closes the source handle exactly once.

- [ ] **Step 1: Write RED private-layout and durability tests**

Expect:

```text
structured-results/artifact-blobs/abl_<id>/
  metadata.json   0600
  content         0600
structured-results/artifact-recovery/<capture-id>.json
```

Directories are 0700. A visible blob directory is accepted only when metadata+content validate exactly; staging directories are never authority.

- [ ] **Step 2: Write RED source-stability tests**

Materialization captures pre-read `fstat`, streams bytes with SHA-256/exact count, captures post-read `fstat`, and rejects any dev/inode/mode/size/mtime/ctime identity change as `artifact_changed_during_capture`. Path replacement after Phase A does not matter because bytes come from the pinned FD.

- [ ] **Step 3: Implement canonical blob identity/cuts/metadata**

`BlobID` deterministically hashes operation/session/adapter/normalized path and excludes content SHA. `terminal_cut` stores receipt schema version + canonical `receipt.Digest`. `observation_cut` hashes a canonical v1 payload containing capture-intent digest, baseline/path authority digest, source observation scheme+identity digest, Phase-A size and final stability result. Persist the exact canonical `ArtifactBlobRef` payload in metadata.

- [ ] **Step 4: Implement same-filesystem durable commit**

Create private staging directory, stream content, `fsync(content)`, write+`fsync(metadata.json)`, `fsync(staging dir)`, rename staging directory to final BlobID destination, `fsync(artifact-blobs parent)`. Only then return a retained `ArtifactBlobRef`.

Ambiguous rename acknowledgement resolves only from the private deterministic destination; validate metadata and content size/digest. Never reopen workspace path.

- [ ] **Step 5: Persist recovery claim before recovery-eligible commit**

The durable recovery claim commits frozen capture authority digest, expected BlobID, operation/session/workspace/repository identity and terminal cut. Order it before final blob visibility (or in the same serialized store protocol). Crash after committed blob but before derivation may therefore recover without operation/session bulk history.

- [ ] **Step 6: Bind blob bytes to total state authority**

`ReserveBlobBytes(expectedSize + boundedMetadataOverhead)` runs under concurrency-safe store authority, protects `ControlReserve`, and refuses oversubscription. Success converts reservation to retained charge at commit; all failure paths release the reservation. State exhaustion never serves as flow control.

- [ ] **Step 7: Fault-inject every durability boundary and commit**

Test content sync, metadata write/sync, staging-dir sync, rename, parent sync, recovery-claim write and ambiguous destination verification. Then:

```bash
go test ./internal/adapter/store ./internal/app/structuredresult -run 'ArtifactBlob|Materializ|RecoveryClaim|BlobBudget' -count=1
go test ./internal/adapter/store ./internal/app/structuredresult -race -run 'ArtifactBlob|Materializ|BlobBudget' -count=1
go run ./tools/devctl check
git add internal/adapter/store internal/app/structuredresult
git diff --cached --check
git commit -m "feat: persist immutable structured artifact blobs"
```

---

### Task 7: Make blob references, compaction, retention and crash recovery atomic

**Files:**
- Create: `internal/adapter/store/structured_artifact_refs.go`
- Create: `internal/adapter/store/structured_artifact_refs_test.go`
- Create: `internal/adapter/store/structured_artifact_recovery.go`
- Create: `internal/adapter/store/structured_artifact_recovery_test.go`
- Modify: `internal/adapter/store/structured_results.go`
- Modify: `internal/adapter/store/structured_records.go`
- Modify: `internal/adapter/store/structured_results_private.go`
- Modify: `internal/adapter/store/retention.go`
- Modify: `internal/adapter/store/reconcile.go`
- Modify: store reopen/root tests.

**Interfaces:**
- Produces `AcquireDerivationBlobRefs`, `CompactDerivationDetail`, `ResolveArtifactBlobState`, `RecoverStructuredArtifacts`, `CollectArtifactOrphans` under one `structuredMu` serialization domain.
- Defines durable blob resolution state `retained|compacted|unavailable` and compacted tombstones.

- [ ] **Step 1: Write RED ref-acquire vs retire race test**

Use barriers to force:

```text
T1 sees candidate for retirement
T2 tries to publish a new detailed derivation referencing blob
```

Exactly one authority wins. If retirement barrier wins, T2 cannot publish detail. If reference acquisition wins, T1 cannot retire bytes.

- [ ] **Step 2: Make detailed derivation visibility depend on durable blob refs**

For artifact inputs, `PutDerivation(pending)` validates retained blob and acquires all required derivation references before creating the derivation file. On create failure, roll back/refuse reference acquisition idempotently. Raw-only derivations need no blob refs.

- [ ] **Step 3: Extend compaction with explicit retirement transition**

Compaction first persists summary/derivation compacted state, removes detailed records, releases this derivation's refs, then under the same authority checks all remaining detailed refs + recovery claims. If none remain, atomically withdraw final blob dir and publish a small tombstone with BlobID/SHA/size/state=compacted. Session retention never performs this step.

- [ ] **Step 4: Prove ordinary session GC cannot destroy recovery authority**

Seed terminal session + committed-unbound blob + recovery claim, run terminal retention until operation/session bulk history is gone, reopen store, and recover/bind the blob. The claim survives until `bound_to_detailed_derivation`, explicit retire/abandon, or durable orphan-GC eligibility.

- [ ] **Step 5: Implement startup recovery ordering and orphan rules**

On store/runtime startup: validate blob store -> finish interrupted retirement/tombstones -> recover committed claims/blobs -> recover derivations -> collect bounded staging/orphans. Staging dirs can be deleted immediately; committed blobs with eligible claim/ref cannot be GC'd. Recovery never runs pytest and never opens workspace artifact path.

- [ ] **Step 6: Run focused store/race/retention tests and commit**

```bash
go test ./internal/adapter/store -run 'Structured|Artifact|Blob|Retention|Recovery|Orphan|Compaction' -count=1
go test ./internal/adapter/store -race -run 'Artifact|Blob|Retention|Recovery|Compaction' -count=1
go run ./tools/devctl check
git add internal/adapter/store
git diff --cached --check
git commit -m "feat: retain structured artifact authority"
```

---

### Task 8: Generalize the structured worker to immutable artifact inputs and recovery

**Files:**
- Modify: `internal/app/structuredresult/ports.go`
- Modify: `internal/app/structuredresult/input.go`
- Modify: `internal/app/structuredresult/input_test.go`
- Modify: `internal/app/structuredresult/worker.go`
- Modify: `internal/app/structuredresult/worker_test.go`
- Create: `internal/app/structuredresult/artifact_reader.go`
- Create: `internal/app/structuredresult/artifact_reader_test.go`
- Modify: `internal/app/structuredresult/service.go`
- Modify: `cmd/shellbeam/execution_observation.go`
- Test composition/runtime files.

**Interfaces:**
- `Reader.ReadInputRange` resolves raw ranges through existing raw binder and artifact ranges only through retained private blob resolver.
- `Worker.ScheduleTerminal` remains raw-output entry point. Add `Worker.ScheduleArtifact` for a committed `ArtifactBlobRef` + frozen producer binding; startup recovery calls the same artifact scheduling path.

- [ ] **Step 1: Write RED union-reader tests**

Raw input reads are byte-for-byte unchanged. Artifact reads require `ArtifactBlobState=retained`, exact ref metadata match and bounded range; compacted returns typed compacted state, unavailable/corrupt fails closed. Reader never accepts pathname authority.

- [ ] **Step 2: Write RED worker identity tests**

Artifact worker derives v2 key from full `StructuredInputRef` + producer/schema/config. Restart/retry of the same committed blob produces the same key; changed terminal/observation cut changes input identity/key. Raw legacy key behavior remains tested separately.

- [ ] **Step 3: Split raw and artifact scheduling without duplicate parser orchestration**

Factor one `processInput` path after input authority exists. Raw terminal scheduler binds terminal output first; artifact scheduler receives already committed ref. Both use the same pending -> processing -> terminal derivation lifecycle and operation index.

- [ ] **Step 4: Make recovery idempotent**

Startup structured recovery resolves committed-unbound claims, validates private blob/ref and schedules artifact processing. If derivation already terminal, no parser rerun; if pending/processing, resume same key. Recovery never executes child command/pytest.

- [ ] **Step 5: Prove raw-path no-tax/no-regression and commit**

```bash
go test ./internal/app/structuredresult ./internal/adapter/structured/gojson ./internal/adapter/store ./cmd/shellbeam -run 'Structured|Artifact|GoTest|GoVet' -count=1
go test ./internal/app/structuredresult ./internal/adapter/structured/gojson -race -count=1
go run ./tools/devctl check
git add internal/app/structuredresult internal/adapter/structured/gojson internal/adapter/store cmd/shellbeam
git diff --cached --check
git commit -m "feat: process artifact structured inputs"
```

---
### Task 9: Implement the bounded `pytest-junit-xml@v1` xunit2 parser

**Files:**
- Create: `internal/adapter/structured/pytestjunit/types.go`
- Create: `internal/adapter/structured/pytestjunit/parser.go`
- Create: `internal/adapter/structured/pytestjunit/parser_test.go`
- Create: `internal/adapter/structured/pytestjunit/parser_limits_test.go`
- Create: `internal/adapter/structured/pytestjunit/parser_fuzz_test.go`
- Modify: `internal/app/structuredresult/ports.go`
- Modify: `internal/app/structuredresult/service.go`
- Modify: `internal/app/structuredresult/worker.go`
- Modify focused app/core tests.

**Interfaces:**
- Produces adapter `pytestjunit.Adapter{}` with `ID() == "pytest-junit-xml"`, version `1`, artifact-only input.
- Extends `ParseResult` with optional `SemanticsCoverage *core.ProducerSemanticsCoverage`; terminal derivation persists this bounded provider capability fact. Coverage is not part of derivation identity because changing semantics requires an adapter-version bump.

- [ ] **Step 1: Write RED parser tests from frozen producer-realistic XML fixtures**

Cover ordinary pass, ordinary failure, `pytest.skip`, `pytest.xfail`, non-strict XPASS collapse, strict XPASS collapse, `<error>`, unknown skipped subtype, duplicate classname+name testcase entries, call-failure + teardown-error multi-entry shape and suite aggregate counters. Tests assert no message/prose reconstruction.

- [ ] **Step 2: Implement a streaming bounded XML reader**

Use `encoding/xml.Decoder.Token()`; never `io.ReadAll` unbounded and never DOM a producer payload. Enforce depth `32`, elements `8192`, per attribute/text field `64 KiB`, input byte bound inherited from `StructuredInputRef` resolver, record bound `1024`, and context deadline. DTD/entity/external-resource behavior stays stdlib-safe; unexpected structural roots/nesting fail closed according to semantic-shape taxonomy.

- [ ] **Step 3: Implement exact testcase mapping**

```text
no outcome child                  → pass
<failure>                         → fail
<error>                           → error
<skipped type="pytest.skip">      → skip + pytest:skip
<skipped type="pytest.xfail">     → skip + pytest:xfail
other mechanically valid skipped → skip, producer disposition absent/partial diagnostic
```

Never infer XPASS from failure/pass message strings. Never infer setup/call/teardown phase. `pytest:xfail` never implies test body executed.

- [ ] **Step 4: Preserve structural entry identity and producer address**

Each XML testcase receives artifact blob ID + suite ordinal + testcase ordinal. `RecordID = H(derivation_key + "testcase" + ordinals)`. `classname + name` is stored only as `ProducerTestAddress`; duplicates remain distinct and maintain XML order.

- [ ] **Step 5: Map suite aggregates independently of testcase records**

Parse producer `tests/failures/errors/skipped/time`. Validate nonnegative bounded counters. Normalize suite status only from aggregate counters with precedence `error > fail > all-skipped > pass`. Do not recompute suite counters/status from child records and do not alter child terminal receipt truth.

- [ ] **Step 6: Emit static V1 semantics coverage and record-granular partiality**

Coverage mechanically observable: coarse pass/fail/skip/error + `pytest:skip` + `pytest:xfail`. Unavailable: `pytest:xpass_exact`, `pytest:error_phase`, `pytest:xfail_execution_state`. Locally unsupported independent testcase extension may keep valid records mechanical with derivation `partial`; malformed XML/ambiguous structural boundary makes the safe remainder unavailable/partial as specified, never generic-JUnit guess.

- [ ] **Step 7: Run parser/fuzz/structural tests and commit**

```bash
go test ./internal/adapter/structured/pytestjunit ./internal/app/structuredresult ./internal/core/structuredresult -count=1
go test ./internal/adapter/structured/pytestjunit -race -count=1
go test ./internal/adapter/structured/pytestjunit -run '^$' -fuzz=FuzzPytestJUnit -fuzztime=10s
go run ./tools/devctl check
git add internal/adapter/structured/pytestjunit internal/app/structuredresult internal/core/structuredresult
git diff --cached --check
git commit -m "feat: parse pytest junit structured results"
```

---

### Task 10: Wire pytest capture/adapter into daemon, capability, inspect and existing P1 evidence

**Files:**
- Modify: `internal/app/structuredresult/selection.go`
- Modify: `internal/app/structuredresult/selection_test.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/structured_adapter.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/structured_precondition_test.go`
- Modify: `internal/app/daemon/structured_worker_test.go`
- Modify: `cmd/shellbeam/execution_observation.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify/add structured runtime tests in `cmd/shellbeam/`
- Modify: `internal/core/capability/catalog.go` and tests
- Modify: `internal/app/structuredresult/inspect.go` and tests
- Modify: `internal/core/verification/evidence.go` and tests
- Modify: `internal/adapter/verification/evidence_source.go` and tests
- Modify/add a bounded structured-detail inspection port in `internal/adapter/verification/`
- Modify: `internal/adapter/ipc/structured_inspect_test.go`
- Create: `internal/adapter/mcp/structured_inspect_test.go`
- Modify: `internal/adapter/mcp/discovery_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-output-v2.json`

**Interfaces:**
- Advertises `pytest-junit-xml` and artifact capture limits through existing capability catalog.
- `inspect.structured` remains the same one-tool action and exposes bounded schema-v2 records, derivation semantics coverage and artifact source state while preserving schema-v1 decoding.

- [ ] **Step 1: Write RED selection/admission tests for pytest**

Auto-selection only when exact direct producer + explicit JUnit output + full qualified invocation authority are present. Explicit `structured_adapter=pytest-junit-xml` with missing JUnit/xunit2/addopts/env proof/argfile returns typed precondition/contract error before spawn. ShellBeam never appends flags.

- [ ] **Step 2: Wire pre-spawn qualification to actual host presence observation**

Compose a bounded presence observer for `PYTEST_ADDOPTS` that stores only presence authority, never the value. The capture-authority service receives frozen `ResolvedArgv`, `ResolvedCWD`, repository/workspace and exact execution context. Project-command and direct argv paths converge on the same qualifier/binding.

- [ ] **Step 3: Wire Phase A and Phase B into daemon composition**

`cmd/shellbeam` composes path authority/capture manager/materializer/blob store plus `pytestjunit.Adapter{}`. Artifact-backed terminal finalization executes Phase A before receipt publication; after durable receipt, materializer commits blob and schedules artifact worker. Raw Go structured scheduler remains unchanged.

- [ ] **Step 4: Extend capability and structured inspection additively**

Advertise adapter IDs `go-test-json`, `go-vet-json`, `pytest-junit-xml`; structured schema versions `[1,2]`; artifact input kind/limits; no second MCP tool. `inspect.structured` reports terminal derivation coverage and retained/compacted/unavailable source state without exposing blob bytes/path outside bounded logical provenance.

- [ ] **Step 5: Add the missing provider-neutral structured-detail → P1 read bridge without rewriting evidence truth**

Current P1 candidates are built from the immutable Evidence Ledger and terminal receipt; there is no direct structured-detail join yet. Add an optional read-time `StructuredEvidenceDetail` to `verification.EvidenceCandidate`, populated by the verification adapter from the operation's structured derivation/summary. It is bounded and provider-neutral:

```go
type StructuredEvidenceDetail struct {
    DerivationKey string
    Completeness structuredresult.Completeness
    MechanicalTestStatuses []structuredresult.TestStatus // unique, sorted, closed
    SemanticsCoverage *structuredresult.ProducerSemanticsCoverage
}
```

The join MUST NOT mutate the durable Evidence Record. `EvidenceCandidate.Result`, authority, freshness, source/environment/command compatibility and stability continue to derive exactly as before from Evidence Ledger/reservation authority. If structured detail is pending/compacted/unavailable, the enrichment is absent or explicitly incomplete; absence is never negative evidence.

Do NOT add a free-form policy rule that magically understands `pytest:xpass_exact`. This plan only exposes bounded semantic coverage to P1; a future policy requirement consuming a producer semantic dimension requires its own explicit core contract.

- [ ] **Step 6: Schema/transport/model-truth tests**

Assert v1 clients can still decode additive output, v2 schema accepts producer disposition/coverage/address/entry refs, raw terminal receipt remains unchanged, and forbidden completion claims such as `task_complete`, `work_complete`, and `safe_to_finish` are absent from structured transport. Existing typed IPC/MCP production pass-through remains unchanged unless a failing compatibility test proves an adapter-layer change is required; do not preemptively duplicate the DTO.

- [ ] **Step 7: Run integration gates and commit**

```bash
go test ./internal/app/structuredresult ./internal/app/daemon ./internal/core/capability ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./cmd/shellbeam -run 'Structured|Pytest|Capability' -count=1
go test ./internal/app/daemon ./internal/app/structuredresult ./internal/adapter/ipc ./internal/adapter/mcp -race -run 'Structured|Pytest' -count=1
go run ./tools/devctl check
git add internal/app/structuredresult internal/app/daemon internal/core/capability internal/core/verification internal/adapter/verification internal/adapter/ipc/structured_inspect_test.go internal/adapter/mcp/structured_inspect_test.go internal/adapter/mcp/discovery_test.go api/schema/ipc-v2.json api/schema/mcp-output-v2.json cmd/shellbeam
git diff --cached --check
git commit -m "feat: expose pytest structured results"
```

---

### Task 11: Freeze real pytest qualification fixtures and run end-to-end acceptance

**Files:**
- Create: `scripts/generate-pytest-junit-fixtures.sh`
- Create: `scripts/test-pytest-structured-results.sh`
- Create: `tests/fixtures/pytest-junit/manifest.json`
- Create: frozen XML fixtures under `tests/fixtures/pytest-junit/pytest-8.4.2/`
- Create: frozen XML fixtures under `tests/fixtures/pytest-junit/pytest-9.1.1/`
- Create/modify: `cmd/shellbeam/structured_pytest_test.go`
- Modify: `docs/superpowers/plans/2026-08-19-multilanguage-structured-results-pytest-v1.md` only to mark completed steps/evidence after the executable artifacts exist.

**Interfaces:**
- Fixture manifest records producer version, exact generator command, xunit2/addopts invocation and SHA-256 for every XML fixture. Normal unit/CI tests consume frozen bytes and require no installed pytest/network.
- Release-qualification script creates throwaway venvs in `/tmp`, installs exact pytest lines only for deliberate qualification testing, then exercises real daemon/public IPC behavior; it is not ordinary spawn tax or a ShellBeam runtime dependency.

- [ ] **Step 1: Generate producer-realistic fixtures for two qualified pytest lines**

For each of `8.4.2` and `9.1.1`, generate: ordinary pass/fail/skip/xfail/non-strict-xpass/strict-xpass/error plus call-fail+teardown-error duplicate-entry fixture. Exact invocation includes explicit JUnit path, `-o junit_family=xunit2`, `-o addopts=`, and empty/absent `PYTEST_ADDOPTS`. Freeze XML SHA-256 and pytest version in manifest.

- [ ] **Step 2: Prove fixture semantics against parser tests**

Run the same parser expectations for both version directories. Assert XPASS and setup/teardown distinctions remain unclaimed exactly as V1 coverage says.

- [ ] **Step 3: Run qualification negative matrix mechanically**

Cover:

```text
PYTEST_ADDOPTS absent/present
empty addopts override present/missing
@argument-file
relative JUnit path from ResolvedCWD
~ and env-expansion path negatives
default-only/legacy/xunit2 last-wins
wrapper/shell-string negatives
pre-existing artifact
symlinked component/final
managed same-path overlap
source replacement vs same-object mutation
terminal acquire timeout/late result
blob byte/store budget
```

No negative may mutate child truth or create a mechanical blob-derived result.

- [ ] **Step 4: Run crash/retention concurrency acceptance**

Mechanically exercise committed blob + daemon restart before derivation, recovery claim surviving session GC, ref-acquire vs retire race, compaction+tombstone, and exact terminal/observation cut identity after restart. Assert recovery never reruns pytest or reopens JUnit workspace path.

- [ ] **Step 5: Run real daemon end-to-end pytest path**

Start a disposable daemon/state root, attach a temp git workspace, execute exact qualified pytest direct argv through public IPC, poll terminal receipt, wait for `inspect.structured`, and assert mechanical testcase/suite records plus terminal receipt remain independent. Repeat one negative invocation and assert no structured derivation while child execution result is preserved.

- [ ] **Step 6: Commit qualification evidence**

```bash
./scripts/test-pytest-structured-results.sh
go test ./internal/adapter/structured/pytestjunit ./cmd/shellbeam -run 'Pytest|Structured' -count=1
go run ./tools/devctl check
git add scripts/generate-pytest-junit-fixtures.sh scripts/test-pytest-structured-results.sh tests/fixtures/pytest-junit cmd/shellbeam/structured_pytest_test.go docs/superpowers/plans/2026-08-19-multilanguage-structured-results-pytest-v1.md
git diff --cached --check
git commit -m "test: qualify pytest structured results"
```

---

### Task 12: Final verification, evidence freeze, merge/deploy gate

**Files:**
- Modify only plan/checklist/evidence docs required to record terminal verification after implementation bytes are committed.
- Do not change production bytes after the final source checkpoint unless verification finds a defect; a defect returns to its owning task with RED first.

**Interfaces:**
- Produces final exact implementation HEAD/source fingerprint/checkpoint receipt used by merge/deploy.

- [ ] **Step 1: Static architecture/compatibility audit**

Mechanically scan that the implementation contains one MCP tool, no generic JUnit fallback, no pytest flag/config injection, no workspace-path reopen after Phase A, no session-retention blob deletion, no parser-based child-outcome rewrite, and no hidden pytest version subprocess on ordinary structured start.

- [ ] **Step 2: Run focused package + race gates**

```bash
go test ./internal/core/structuredresult ./internal/core/operation ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/localfs ./internal/adapter/store ./internal/adapter/structured/gojson ./internal/adapter/structured/pytestjunit ./internal/adapter/ipc ./internal/adapter/mcp ./internal/core/capability ./api/schema ./cmd/shellbeam -count=1
go test -race ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/localfs ./internal/adapter/store ./internal/adapter/structured/pytestjunit ./internal/adapter/ipc ./internal/adapter/mcp -count=1
go run ./tools/devctl check
git diff --check
go run ./tools/devctl test --dirty --base main
```

- [ ] **Step 3: Run release qualification + full repository verification**

```bash
./scripts/test-pytest-structured-results.sh
go test ./... -count=1
go run ./tools/devctl verify --checkpoint --base main --json
```

Require `command=verify`, `selection=full`, `status=passed`, `exit_code=0`, exact committed source fingerprint and exact final feature HEAD.

- [ ] **Step 4: Concurrent-main audit before merge**

Fetch `origin/main`, compare local `main`, feature merge-base and exact changed-file overlaps. Rebase only if current main has advanced; resolve semantic overlaps individually and rerun affected/full gates as required. Never merge stale verification evidence across a changed source fingerprint.

- [ ] **Step 5: Merge/deploy only after final checkpoint authority**

Fast-forward local main when possible, verify merged HEAD/tree, build the exact merged binary, deploy using the existing manual runtime model, and verify runtime binary identity + daemon incarnation + a live qualified pytest structured operation. Do not push/create PR unless separately requested.

- [ ] **Step 6: Cleanup only after live deploy proof**

Remove implementation worktree/branch only when merged main is verified/deployed and no unmerged dirt remains. Preserve fixture/evidence artifacts committed by Task 11.

---

## Spec Coverage Map

| Spec sections | Implementation owner |
|---|---|
| 1–5 decision/scope/non-goals | Global constraints + Task 12 audit |
| 6–8 input union/schema migration/raw preservation | Tasks 1–2 |
| 9–14 capture intent/baseline/collision/authorship limits | Tasks 3–4, Task 11 negatives |
| 15–22 terminal ordering/source handle/materialization/capture taxonomy | Tasks 5–6 |
| 23–33 blob identity/storage/commit/crash/budgets | Task 6 |
| 34–40 ref retention/compaction/tombstones/orphans/startup | Task 7 |
| 41–42 adapter resolver vs ArtifactObservation | Tasks 2, 8 |
| 43–58 pytest identity/qualification/version/semantic shape | Tasks 3, 9, 11 |
| 59–76 TestStatus/disposition/coverage/identity/duplicates/suite/duration/authority | Tasks 1, 9 |
| 77–81 capture failure/derivation identity/retention/inspect/diagnostics | Tasks 6–10 |
| 82–88 caller/selection/project/security/resource/store limits | Tasks 3–6, 10 |
| 89–90 qualification matrix/producer-realistic fixtures | Task 11 |
| 91–93 P1/provider-neutral truth/terminal independence | Tasks 9–10, Task 12 audit |
| 94 Vitest/Jest | Deferred to follow-on plan after pytest foundation is deployed |
| 95 ESLint | Deferred to follow-on plan after Vitest/Jest qualification |
| 96 TypeScript compiler | Explicitly deferred pending qualification; no LSP prerequisite |
| 97–102 migration/cursor/compaction/recovery/audit/no auto-repair | Tasks 2, 7–12 |
| 103–107 invariants/deferred/sequencing/acceptance/final position | Global constraints + Task 12 |

## Execution Order After This Plan

```text
pytest shared artifact foundation + pytest-junit-xml@v1
→ final checkpoint / merge / deploy
→ write + execute Vitest/Jest native-machine-contract plan
→ write + execute ESLint native-machine-contract plan
→ TypeScript compiler only after explicit qualification design
```

No Pyright/TypeScript LSP or P4-A dependency blocks this sequence.
