# ShellBeam Multi-Language Structured Results Design

Date: 2026-08-19
Status: design freeze for review; implementation planning is not yet authorized
Scope: extend E22 Structured Results from terminal raw-output adapters to provenance-safe artifact-backed producer adapters, with `pytest-junit-xml@v1` as the first qualified provider and a bounded path to Vitest/Jest/ESLint later

## 1. Decision

ShellBeam SHALL extend Structured Results with a closed, versioned input union that supports both terminal raw-output authority and immutable captured artifact authority.

The first artifact-backed adapter SHALL be:

```text
pytest-junit-xml@v1
```

It SHALL consume pytest's built-in JUnit XML output without requiring an extra pytest plugin.

ShellBeam SHALL NOT inject `--junitxml`, install `pytest-json-report`, infer pytest from shell prose, or treat a mutable workspace pathname as parser authority.

The architecture is:

```text
qualified execution intent
        │
        │ exact resolved argv + structured adapter contract
        ▼
ArtifactCaptureIntent
        │ frozen + durable before spawn
        │ baseline/collision qualification
        ▼
pytest child execution
        │
        ▼
child reaped + output drained
        │
        ▼
terminal Phase A
securely pin exact artifact file object
        │
        ├────────────► terminal receipt publication continues
        │
        ▼
terminal Phase B
bounded immutable materialization
        │
        ▼
private ArtifactBlob
        │
        ▼
ArtifactBlobRef
        │
        ▼
pytest-junit-xml@v1
        │
        ▼
E22 test_case / test_suite records
        │
        ▼
existing P1 evidence/sufficiency plane
```

Terminal receipt truth remains independent of structured-result projection truth.

## 2. Why this extension exists

E22 currently supports machine-readable Go providers by binding exact terminal output bytes through `RawOutputRef`.

That model does not safely cover producers whose native machine-readable result is written to a file. A mutable path such as `junit.xml` is not immutable source authority. Parsing it after a terminal receipt becomes durable can associate the wrong bytes with the operation because:

- a stale artifact from a previous run may already exist;
- another process may replace the pathname after pytest exits;
- the same underlying file object may be modified during capture;
- the current structured worker is scheduled only after durable terminal receipt publication;
- session retention and structured-result retention are independent lifecycles.

The missing primitive is therefore not an XML parser. It is provenance-safe immutable artifact acquisition.

## 3. Retained E22 and P1 contracts

This design composes with, and does not weaken, existing contracts.

The following remain authoritative:

- MCP/tool transport success is not child execution success.
- Terminal child outcome comes from durable receipt/spawn/reap/exit/signal evidence.
- `operation_id` remains exactly-once execution-start intent.
- Structured results are deterministic projections of qualified machine-readable producer facts.
- Mechanical authority is never created by prose/message heuristics.
- Unknown or unavailable information cannot be converted into negative evidence.
- P1 evidence sufficiency remains policy-driven and fail-conservative.
- Structured-result parsing SHALL NOT mutate execution semantics.
- Structured-result detail SHALL NOT overwrite terminal receipt truth.

## 4. Delivery scope and order

The provider family SHALL be delivered in this order:

```text
1. pytest-junit-xml
2. Vitest / Jest qualified native-machine contracts
3. ESLint qualified machine contract
4. TypeScript compiler only after a separate qualification decision
```

Pyright, TypeScript LSP, and the code-intelligence roadmap are not prerequisites for this work.

This design fully specifies the artifact-input foundation and `pytest-junit-xml@v1`.

Vitest, Jest, ESLint, and TypeScript compiler formats are intentionally not guessed in this document. Each later adapter SHALL either reuse a qualified existing `StructuredInputRef` kind or obtain an explicit design amendment.

## 5. Non-goals

V1 SHALL NOT:

- inject pytest command-line flags;
- delete or truncate pre-existing result files;
- discover or emulate effective pytest configuration files;
- auto-install pytest plugins;
- parse console output to reconstruct pytest outcomes;
- implement a generic JUnit XML adapter;
- infer exact pytest version from Python version or executable pathname;
- attest against hostile arbitrary same-user writers on ordinary workspace paths;
- introduce daemon-side test planning or retry strategy;
- recompute child success from normalized testcase records;
- create a Python/JS/TS-specific evidence ontology parallel to P1.

## 6. Structured input closed union

Structured input SHALL become a closed tagged union.

Conceptually:

```go
type StructuredInputKind string

const (
    StructuredInputRawOutput    StructuredInputKind = "raw_output"
    StructuredInputArtifactBlob StructuredInputKind = "artifact_blob"
)

type StructuredInputRef struct {
    Kind         StructuredInputKind
    RawOutput    *RawOutputRef
    ArtifactBlob *ArtifactBlobRef
}
```

Validation SHALL require:

```text
closed kind
exactly one branch
branch matches kind
branch validates independently
```

A Go interface with arbitrary implementations is not the persistence/wire contract.

## 7. Structured-result schema evolution

Current structured-result schema v1 embeds `RawOutputRef` directly in:

```text
Derivation.SourceAuthorityRefs
Record.SourceRef
Adapter.Parse(...)
```

The artifact-input extension changes that identity shape. New writes SHALL use a new structured-result schema version rather than silently reinterpret persisted v1 bytes.

Historical v1 raw-output derivations/records SHALL remain readable.

At the read/migration boundary, a v1 raw-output source may be normalized conceptually to:

```text
StructuredInputRef {
  kind = raw_output
  raw_output = historical RawOutputRef
}
```

Historical persisted records SHALL NOT be rewritten merely to adopt the union.

## 8. Raw-output behavior remains unchanged

`go-test-json` and `go-vet-json` SHALL retain `RawOutputRef` semantics.

The addition of `artifact_blob` SHALL NOT change:

- terminal output range identity;
- raw-output digest semantics;
- existing Go adapter selection rules;
- existing Go adapter mechanical authority;
- historical raw-output derivation identity.

Any schema migration must preserve these facts explicitly.

## 9. ArtifactCaptureIntent

Artifact capture authority SHALL begin before child spawn.

Conceptually:

```text
ArtifactCaptureIntent
  schema_version
  operation_id
  session_id
  repository_id
  workspace_id

  adapter_id
  declared_path_token
  normalized_workspace_path
  expected_kind = regular_file
  max_blob_bytes

  producer_binding_digest
  baseline
```

For `pytest-junit-xml@v1`, `producer_binding_digest` SHALL be the canonical digest of the complete `PytestInvocationBindingV1` qualification authority defined in Section 46. It SHALL NOT digest only resolved argv tokens or omit environment-absence/addopts/argument-file authority facts.

For pytest V1 there SHALL be exactly one capture intent.

The generic protocol ceiling SHALL remain bounded by `MaxSourceAuthorityRefs`; current ceiling is 8.

## 10. Capture intent is durable operation authority

A capture intent SHALL NOT exist only in daemon memory.

Before spawn, ShellBeam SHALL durably bind the frozen capture intent to the operation reservation, or to an equivalent immutable operation-authority record with the same replay semantics.

The durable binding SHALL be fingerprinted and replay-protected.

Changing any identity-significant capture input under the same operation identity SHALL cause metadata/request conflict rather than rebinding an existing operation.

Identity-significant inputs include at least:

- adapter identity;
- normalized artifact path;
- expected kind;
- producer/invocation/dialect binding digest.

The durable intent is required for post-crash validation of a committed private blob.

Replay SHALL resolve the already-durable operation/capture intent before any new baseline observation or path binding. A replay of an admitted operation MUST NOT re-read the workspace artifact path to create a different baseline, capture path, dialect binding, or byte budget. Stored frozen capture authority wins; incompatible replay metadata is a conflict.

## 11. Pre-spawn freshness baseline

For an ordinary workspace pathname, pytest mechanical artifact attribution in V1 requires:

```text
baseline = absent
```

If the path already exists before spawn:

```text
capture_result = preexisting_unqualified
```

The child SHALL still be allowed to execute unless another independent execution contract rejects it.

ShellBeam SHALL NOT automatically delete, truncate, rename, or replace the pre-existing file merely to make structured capture possible.

Ordinary-path baseline qualification SHALL use the same workspace-root descriptor authority, descriptor-relative traversal, component no-follow policy, final-target containment policy, and normalized path binding that terminal Phase A later uses.

A pathname-stat baseline such as `Lstat(workspace/path)` followed later by descriptor-relative Phase A resolution is not qualified V1 authority.

The baseline operation SHALL prove absence under that descriptor authority without following or rebinding path components. The exact normalized path bound by this baseline SHALL be the same immutable path binding consumed by collision registration, Phase A, blob provenance, and recovery.

If the platform implementation cannot provide the qualified descriptor-relative/no-follow traversal contract for the ordinary workspace path:

```text
path_unqualified / unavailable
→ no qualified baseline
→ no mechanical artifact attribution
```

A separately qualified operation-private/hermetic/provider-controlled result channel may define a stronger baseline contract in a future version.

## 12. Why the baseline is mandatory

Without a pre-spawn baseline:

```text
run N
  pytest writes junit.xml

run N+1
  junit.xml still exists
  pytest fails before producing a report

post-terminal parser opens junit.xml
```

The bytes from run N could be falsely attributed to run N+1.

XML validity, xunit2 shape, digest correctness, and parser success cannot repair this provenance error.

## 13. Managed path producer association

ShellBeam SHALL maintain path-scoped managed-producer association for active artifact capture intents.

Collision identity is at least:

```text
(repository/workspace, normalized artifact path)
```

Overlapping ShellBeam-managed producer intents for the same normalized path SHALL yield:

```text
managed_path_collision
```

and SHALL NOT grant mechanical artifact attribution.

`CoherenceBarrier.ActiveManagedShellOperations` is not filesystem authorship proof and SHALL NOT substitute for path-scoped producer association.

## 14. Limits of ordinary-path authorship

V1 can mechanically establish that:

- this ShellBeam operation declared the artifact path;
- the ordinary workspace path was absent at the pre-spawn baseline;
- no overlapping ShellBeam-managed producer intent claimed the same path;
- this exact file object was pinned at the terminal acquisition cut;
- exact stable bytes were durably materialized.

V1 cannot generically prove that an arbitrary hostile same-user external process did not create or modify the path between producer execution and terminal acquisition.

Requirements needing exclusive causal producer proof require a stronger result channel such as:

- operation-private result path;
- hermetic output boundary;
- provider-controlled immutable result channel.

## 15. Terminal capture ordering

Artifact source acquisition SHALL occur:

```text
child reaped
→ output drained
→ artifact Phase A acquisition
→ managed-shell authority release
→ process-resource release
→ durable receipt publication
→ artifact Phase B may continue asynchronously
```

The current post-receipt `scheduleStructuredTerminal()` pathname-open model is insufficient for artifact-backed authority.

Phase A SHALL happen before the pathname can be reused under a new managed execution boundary.

## 16. Phase A — ArtifactSourceHandle

Phase A SHALL securely acquire the exact source file object.

Conceptually:

```text
workspace root authority
→ descriptor-relative traversal
→ no-follow containment
→ open exact final object
→ fstat opened object
→ validate regular file + size bound
→ freeze source-handle metadata
```

It SHALL NOT use:

```text
stat(path)
→ later open(path)
```

as identity proof.

## 17. ArtifactSourceHandle lifecycle

`ArtifactSourceHandle` is:

```text
ephemeral
process-local
single-owner
not serializable
not public
not ArtifactBlobRef
```

It belongs to the artifact-capture subsystem, not to the child process-resource bundle.

`releaseProcessResources()` SHALL NOT close artifact-capture handles.

Phase B SHALL consume and close an acquired source handle exactly once.

## 18. Reservation-before-open

ShellBeam SHALL reserve bounded capture resources before opening an artifact source FD.

Before secure-open, acquisition must have capacity for:

- capture/acquisition concurrency;
- a pinned-handle slot;
- a materialization queue slot or equivalent ownership slot;
- eligibility to reserve blob-store bytes after source size is known.

ShellBeam SHALL NOT open an unbounded set of FDs and then wait for worker capacity.

Every queued materialization job holding an FD SHALL count under the same process-wide pinned-handle limit.

## 19. Terminal acquisition deadline

Filesystem syscalls are not generically cancellable merely because a Go context deadline expires.

The observable V1 guarantee is therefore:

```text
terminal receipt publication
MUST NOT wait beyond MaxTerminalAcquireDuration
for artifact qualification
```

An implementation may use bounded acquisition helpers.

If the qualification deadline expires:

```text
capture = unavailable
receipt publication continues
managed-shell authority releases
```

A late helper result SHALL:

- close any acquired FD;
- release reservations;
- never resurrect capture authority after the qualification deadline.

Process-wide acquisition concurrency SHALL bound helpers that remain stuck in kernel/VFS calls.

## 20. Phase B — immutable materialization

Phase B SHALL consume the pinned source handle only.

It SHALL NEVER reopen the workspace pathname.

Conceptually:

```text
pinned source FD
→ pre-read fstat
→ bounded stream
→ SHA-256 + exact byte count
→ private staged content
→ post-read fstat
→ source-stability validation
→ durable atomic private blob commit
```

Only after successful durable commit may an `ArtifactBlobRef` be minted.

## 21. Source mutation during materialization

Pathname replacement after Phase A acquisition cannot rebind the pinned source object.

If the underlying pinned object itself changes during materialization, ShellBeam SHALL fail closed.

At minimum the source-stability contract SHALL compare platform-qualified object identity and stable size/metadata facts before and after read. Mere equal mtime is not sufficient proof.

If stable-source proof fails:

```text
changed_during_capture
→ discard staged bytes
→ no ArtifactBlobRef
→ no mechanical pytest derivation
```

Torn-read digests SHALL NOT become structured input authority.

## 22. Capture result taxonomy

`ArtifactBlobRef` SHALL mean successful immutable materialization only.

Capture failure state belongs to a separate result object.

V1 capture results include:

```text
captured
missing
preexisting_unqualified
path_unqualified
kind_mismatch
changed_during_capture
managed_path_collision
budget_exceeded
unavailable
```

Invariant:

```text
capture_result != captured
→ ArtifactBlobRef MUST NOT exist
```

## 23. ArtifactBlobRef

Conceptually:

```text
ArtifactBlobRef
  schema_version
  blob_id

  operation_id
  session_id
  repository_id
  workspace_id

  declared_path
  normalized_workspace_path

  sha256
  size

  terminal_cut
  observation_cut
```

Every identity-bearing `ArtifactBlobRef` field SHALL be durably reconstructable byte-for-byte from ShellBeam-owned private blob authority. Private blob metadata SHALL persist the canonical `ArtifactBlobRef` identity payload itself, or a closed canonical payload sufficient to reconstruct it exactly. V1 chooses the stronger default: persist the canonical identity payload and validate it before mint/recovery.

`terminal_cut` has closed V1 meaning: it binds the exact validated durable terminal authority through the canonical terminal-receipt digest and its receipt-schema identity. It is persisted, not recomputed from current session state after recovery.

`observation_cut` has closed V1 meaning: it is a versioned deterministic digest of a canonical persisted `ArtifactCaptureObservationCutV1` payload containing at least the frozen capture-intent digest, qualified baseline/path-binding authority digest, qualified source-object observation scheme/digest, Phase-A observed size, and final source-stability observation/result required to justify the committed bytes. Platform-specific source facts may appear only through an explicitly versioned observation scheme.

Neither cut may contain a freshly generated timestamp, random identifier, current daemon incarnation, or recovery-time observation. Recovery SHALL reuse the exact persisted cut values; it MUST NOT regenerate them.

The canonical private identity payload and both cut payloads/digests SHALL be committed as part of private blob metadata before an `ArtifactBlobRef` is minted.

`BlobID` is storage identity.

`SHA256 + Size` is byte-content identity.

Operation/path/cut facts remain provenance.

Equal bytes from two runs SHALL NOT collapse their provenance identities.

## 24. Blob identity

V1 SHALL NOT deduplicate blobs across executions.

`BlobID` SHALL be deterministic and operation-scoped.

Conceptually:

```text
abl_ + H(
  "artifact_blob_v1"
  + operation_id
  + session_id
  + adapter_id
  + normalized artifact path
)
```

Content SHA-256 SHALL NOT be the `BlobID`.

Consequences:

```text
BlobID equality
  ≠ content equality proof

SHA256 equality
  ≠ same producer/run/provenance
```

## 25. Blob storage layout

Artifact blobs SHALL live under private structured-result state, not under session output retention.

Conceptually:

```text
state/
  structured-results/
    artifact-blobs/
      abl_<id>/
        metadata.json
        content
```

Directories SHALL remain private (`0700`) and files private (`0600`) subject to existing platform/store conventions.

Blob content is not a public arbitrary file-serving API.

Adapters consume it through a bounded internal resolver.

## 26. Atomic directory object

Bytes and metadata SHALL commit as one logical blob object.

V1 materialization SHALL stage on the same filesystem:

```text
.artifact-stage-...
  content
  metadata.json
```

Durable commit sequence SHALL include:

```text
stream pinned FD → staged content
SHA-256 + exact count
post-read source validation
fsync content
write + fsync metadata
fsync staging directory
atomic create/rename staging directory → abl_<id>
fsync artifact-blobs parent
```

The final destination SHALL be create-only/idempotent under deterministic identity; an existing conflicting blob object is not overwritten.

## 27. Private blob metadata

Private blob metadata SHALL include enough information to validate the committed object without reopening the workspace pathname.

At minimum:

- blob schema version;
- `BlobID`;
- frozen capture-intent digest;
- operation/session/repository/workspace identity;
- adapter identity;
- declared/normalized artifact path;
- content SHA-256;
- exact size;
- canonical `ArtifactBlobRef` identity payload;
- canonical/versioned terminal-cut authority payload or its closed reconstructable representation;
- canonical/versioned observation-cut payload or its closed reconstructable representation;
- durable commit state/version.

Platform-private source-object metadata may additionally be retained for audit but SHALL NOT become portable semantic identity unless explicitly versioned.

## 28. Ambiguous blob commit

If store acknowledgement around final rename/fsync is ambiguous, recovery MAY inspect the deterministic private blob destination.

It SHALL NOT reopen the workspace artifact path.

Recovery logic:

```text
lookup deterministic BlobID
→ validate private metadata
→ validate private content size/digest

exact match
  → committed

conflict/corruption
  → unavailable
```

This is safe because the private blob store is ShellBeam-owned immutable state.

## 29. Crash boundaries

Crash semantics SHALL distinguish two boundaries.

Before durable private blob commit:

```text
Phase A succeeded
→ daemon crashes
→ ephemeral source handle lost
→ workspace pathname MUST NOT be reopened
→ capture unavailable
```

After durable private blob commit:

```text
blob commit succeeded
→ daemon crashes before derivation completes
→ immutable private authority still exists
→ structured recovery MAY continue from the private blob
```

The durable blob commit is therefore the crash-recoverability boundary for artifact bytes.

## 30. Capture intent and crash recovery

Post-commit recovery SHALL require matching durable pre-spawn authority.

A committed blob may be attached/recovered only when:

- private blob metadata validates;
- its capture-intent digest matches the frozen durable operation capture intent;
- the operation identity/session binding is eligible;
- the terminal operation/receipt facts are compatible with recovery;
- the blob has not been compacted/withdrawn.

Private blob presence alone SHALL NOT authorize a new derivation.

A recovery-eligible committed-but-unbound capture SHALL also own a minimum durable structured recovery claim whose lifetime is independent of ordinary operation/session bulk retention. The claim SHALL retain enough canonical authority to validate the frozen capture intent, expected `BlobID`, operation/session/workspace provenance, and terminal authority cut without requiring the bulk operation record or workspace artifact pathname.

The minimum claim SHALL be durable no later than the point at which the private blob commit is considered recovery-eligible. A design that permits `durable blob commit → crash` to be recoverable SHALL therefore order the recovery claim before, or in the same serialized durability protocol as, the final recovery-eligible blob commit.

Ordinary session retention MAY remove terminal bulk history, but SHALL NOT destroy the last structured recovery-authority claim while the committed blob is still eligible for recovery. The claim may leave live authority only through a durable transition that is one of:

```text
bound_to_detailed_derivation
explicitly_abandoned_or_retired
orphan_gc_eligible
```

Binding a recovery claim to a detailed derivation SHALL follow the atomic reference-acquisition rule in Section 34. Orphan collection SHALL treat an eligible recovery claim as a live owner even when the original reservation/session has already been bulk-collected.

## 31. Blob byte ceiling

Blob capture stores real bytes and therefore has an independent per-blob bound.

V1 SHALL define:

```text
MaxArtifactBlobBytes <= 64 MiB
```

The configured/default value may be lower but SHALL NOT exceed the V1 protocol ceiling without a versioned contract change.

After Phase A `fstat`:

```text
source size > MaxArtifactBlobBytes
→ budget_exceeded
→ close FD
→ no Phase B materialization
```

## 32. Global blob storage authority

Blob storage SHALL be a bounded sub-authority of existing state-root storage limits.

It SHALL NOT bypass `MaxTotalState`, `ControlReserve`, or equivalent state-root safety contracts.

Conceptually:

```text
BlobBudgetLease
  reserve(expected_source_size + bounded metadata overhead)
      ↓
  Phase B
      ↓
  durable commit
      ↓
  convert reservation to retained state charge
```

Failure releases the reservation.

Disk exhaustion SHALL NOT be used as flow control.

## 33. No implicit eviction of live blob authority

When blob-store/state budget is full:

```text
DO NOT silently evict referenced live blob bytes
```

Instead:

```text
reject new capture
→ budget_exceeded / unavailable
```

Withdrawal of mechanical source bytes SHALL require an explicit lifecycle transition such as structured-detail compaction.

## 34. Capture-owned blob, reference-aware retention

Blob ownership SHALL be defined by capture, not by exactly one derivation.

This distinction is mandatory because derivation identity includes source refs plus producer/schema/config facts. The same immutable `ArtifactBlobRef` may therefore legitimately participate in more than one derivation identity.

V1 retention semantics are:

```text
one successful capture
→ one operation-scoped blob

zero or more derivations
→ may reference that blob
```

A detailed derivation holds a retention reference to each blob it requires.

Compacting one derivation removes only that derivation's retention reference.

Blob bytes MAY be retired only when:

```text
no non-compacted detailed derivation references the blob
AND
no recovery-eligible committed-but-unbound capture requires it
```

No cross-run shared ownership/refcount is introduced because V1 does not deduplicate blobs across executions.

Blob reference acquisition and blob retirement SHALL share one serialization/atomic authority domain. A detailed derivation that depends on a blob MUST NOT become durably visible until all required retained-blob references for that derivation have been durably acquired under that authority.

Retirement SHALL atomically:

```text
prove no live detailed derivation reference
AND
prove no recovery-eligible committed-but-unbound claim
AND
establish the retirement barrier / withdraw retained blob authority
```

Once the retirement barrier wins, no new detailed derivation reference may attach to that blob. A recovery path that converts a committed-but-unbound claim into a detailed derivation SHALL acquire the detailed reference under the same authority before releasing the recovery claim.

The implementation MAY realize this domain with a structured-store mutex, a durable reference index, or a transaction-like store primitive; the implementation plan SHALL choose the mechanism, but the ordering above is normative.

V1 does not require the public operation index to expose multiple simultaneously active derivations for one operation. The retention rule is deliberately stronger: blob retirement MUST NOT encode an assumption that source authority can only ever have one derivation identity.

## 35. Session retention remains independent

Terminal/session lifetime and structured artifact lifetime are independent.

```text
terminal session collected
→ MUST NOT automatically delete a still-referenced ArtifactBlob
```

Conversely:

```text
ArtifactBlob retained
→ does not keep the whole terminal session alive
```

The dependency is one-way through structured-result source authority.

## 36. Derivation compaction and blob retirement

Structured-detail compaction SHALL own release of derivation-held blob references.

Conceptually:

```text
CompactDerivationDetail
  1. durably compact summary
  2. transition derivation → compacted
  3. remove detailed records
  4. release derivation's blob retention references
  5. retire blob bytes only if no live/recovery references remain
```

Session retention SHALL NOT perform steps 4 or 5.

## 37. ArtifactBlobRef after compaction

An `ArtifactBlobRef` is minted only after a real immutable blob durably exists.

Retention may later withdraw bytes. The ref SHALL never rebind to different bytes.

Resolver state is closed:

```text
retained
compacted
unavailable
```

Semantics:

```text
retained
  → exact bytes resolvable
  → parser may consume

compacted
  → historical identity retained
  → bytes intentionally withdrawn
  → parser MUST NOT run

unavailable
  → missing/corrupt/unqualified state
  → fail closed
```

## 38. Blob retirement and tombstones

Blob retirement SHALL atomically withdraw the active blob object before destructive byte removal.

A bounded recovery-aware retirement flow SHALL preserve the distinction between deliberate compaction and corruption.

Conceptually:

```text
artifact-blobs/abl_X/
  → atomic rename to private retirement staging
  → durably establish compacted tombstone
  → remove retired byte directory
```

A crash between these steps SHALL be recoverable from private retirement staging.

Tombstone:

```text
artifact-blob-tombstones/
  abl_X.json

  BlobID
  SHA256
  Size
  state = compacted
```

Tombstones SHALL NOT contain content bytes and SHALL NOT be accepted as parser input.

## 39. Orphan semantics

Two orphan classes are distinct.

### 39.1 Staging orphan

Crash before atomic blob commit may leave:

```text
.artifact-stage-*
```

Such state was never authority and may be removed by startup/sweeper cleanup.

### 39.2 Committed-but-unbound blob

Crash may occur after durable private blob commit and before derivation/index persistence.

Such a blob SHALL NOT be deleted immediately.

Recovery SHALL first attempt:

```text
terminal operation authority
+ frozen durable capture intent
+ deterministic BlobID
+ valid committed private blob
→ recover/continue structured derivation
```

Only a committed blob that cannot be traced to eligible durable capture authority and has no live derivation reference becomes orphan-GC eligible.

## 40. Startup ordering

Startup/recovery ordering SHALL be:

```text
validate artifact blob store
→ reconcile retirement staging/tombstones
→ structured capture/blob recovery
→ structured derivation recovery
→ bounded orphan collection
```

Orphan GC SHALL NOT race ahead of recoverable structured authority.

## 41. Adapter input API

Artifact-backed adapters SHALL consume a `StructuredInputRef` through a bounded resolver.

The parser SHALL NEVER receive:

- a mutable workspace pathname as authority;
- an `ArtifactSourceHandle`;
- a raw unvalidated FD;
- an `ArtifactObservation` as substitute authority.

The artifact resolver SHALL validate retained blob state and bounded reads before exposing bytes.

## 42. ArtifactObservation remains independent

`ArtifactObservation` remains useful evidence metadata for expected outputs.

It does not retain immutable bytes and SHALL NOT substitute for `ArtifactBlobRef`.

An artifact may have both:

```text
ArtifactObservation
and
ArtifactBlobRef
```

with different purposes and authority.

## 43. Pytest adapter identity

The first artifact-backed adapter SHALL be named:

```text
pytest-junit-xml@v1
```

It SHALL NOT be named generic `junit-xml`.

JUnit XML is an interchange family with producer-specific dialect and semantic behavior. The adapter identity must state the producer contract ShellBeam qualifies.

## 44. Pytest dependency contract

Pytest V1 SHALL use pytest's built-in JUnit XML support.

No additional pytest plugin is required.

ShellBeam SHALL NOT:

- install `pytest-json-report`;
- require `pytest-json-report`;
- auto-add JUnit flags;
- rewrite pytest configuration.

## 45. Selection precedence

Structured adapter source precedence remains:

```text
validated project command
> explicit caller adapter
> exact direct-argv safe rule
```

A project command supplies trusted resolved argv/binding. Project-command category alone SHALL NOT grant pytest producer identity.

All pytest selection/qualification paths SHALL converge on one immutable invocation binding.

## 46. Unified PytestInvocationBinding

Gate 1 producer qualification, Gate 2 JUnit-output qualification, and Gate 3 dialect qualification SHALL use one argv resolver.

Conceptually:

```text
PytestInvocationBindingV1
  producer_form
  junit_output
  junit_family_override
  config_addopts_override
  argument_file_state
  pytest_addopts_environment_fact
```

`pytest_addopts_environment_fact` SHALL be a canonical bounded authority fact, not a transient lookup result. For qualified V1 it records at least:

```text
name = PYTEST_ADDOPTS
present = false
authority_schema_version
authority_digest
```

The authority digest SHALL bind the exact mechanically observed absence fact used at pre-spawn qualification time. It SHALL be deterministic and replayable from durable operation/capture authority without re-observing the current process environment.

`config_addopts_override` SHALL encode the effective explicit empty override that neutralizes config `addopts`. `argument_file_state` SHALL encode the closed V1 state `none`; any argument-file source is unqualified rather than represented as `none`.

Independent parsers for selection, capture path, dialect, environment qualification, and addopts/argument-file state are forbidden because they can disagree about the effective invocation.

The canonical `PytestInvocationBindingV1` payload SHALL be the complete pre-spawn pytest qualification authority for producer/invocation/dialect selection. Its canonical digest is the `producer_binding_digest` stored by `ArtifactCaptureIntent`. The digest therefore commits all identity-bearing fields above, including `PYTEST_ADDOPTS` absence authority, the effective empty config-addopts override, and argument-file rejection state.

Crash recovery SHALL validate and reuse this persisted canonical invocation authority. It MUST NOT recreate `PYTEST_ADDOPTS` absence, addopts neutrality, or argument-file state from the recovery-time environment or filesystem.

## 47. Pytest producer qualification

V1 auto-qualifies only exact direct execution forms represented by the frozen resolved execution contract:

```text
pytest ...
python -m pytest ...
```

Wrappers are not auto-qualified:

```text
poetry run pytest
uv run pytest
tox
nox
make test
bash -c ...
python script_that_calls_pytest.py
```

A validated project command may still execute pytest, but producer qualification examines its final resolved argv rather than trusting a `kind=test` label.

## 48. Pytest JUnit output binding

V1 supports documented built-in JUnit output forms:

```text
--junitxml PATH
--junitxml=PATH
--junit-xml PATH
--junit-xml=PATH
```

If the option occurs multiple times, the resolver SHALL bind the effective final value according to the qualified pytest option contract.

V1 qualifies only an expansion-free JUnit path token. The supplied token MUST be invariant under the qualified platform's pytest-equivalent user/environment expansion rules. In particular, a token requiring `~` expansion or environment-variable expansion is outside V1 qualification.

A relative JUnit output path SHALL resolve against the frozen execution `ResolvedCWD`, not against workspace root by assumption. The resolver SHALL then prove workspace containment and produce one exact normalized workspace-relative path. That single normalized binding is consumed by baseline qualification, path collision authority, Phase A, blob identity/provenance, and recovery.

An absolute output path MAY qualify only if normalization proves it lies inside the same frozen workspace authority and can be represented by the same normalized workspace-relative binding. Otherwise the artifact path is unqualified.

The resulting JUnit output binding SHALL be the single authority used by:

- structured selection;
- `ArtifactCaptureIntent`;
- pre-spawn baseline;
- path collision domain;
- terminal Phase A secure open;
- `ArtifactBlobRef` path provenance.

## 49. Explicit adapter mismatch

If the caller explicitly requests:

```text
structured_adapter = pytest-junit-xml
```

but the execution contract does not prove a supported pytest producer/invocation/dialect shape, ShellBeam SHALL return a typed structured-adapter contract error rather than guessing from console logs.

Capture failures discovered from filesystem state, such as pre-existing artifact or post-execution materialization failure, SHALL NOT rewrite child execution semantics.

## 50. Pytest dialect qualification

V1 qualifies only xunit2.

Current pytest default xunit2 is not runtime dialect authority.

A discovered config file saying `junit_family=xunit2` is not V1 authority.

An XML document merely lacking xunit1-only fields is not xunit2 authority.

V1 dialect authority requires an effective explicit override in the qualified pytest invocation:

```text
-o junit_family=xunit2
--override-ini junit_family=xunit2
--override-ini=junit_family=xunit2
```

Because pytest can prepend configuration `addopts` and `PYTEST_ADDOPTS` before ordinary command-line arguments, `ResolvedArgv` alone is not sufficient authority unless those built-in argument sources are neutralized or proven absent. Strict V1 therefore additionally requires all of the following:

```text
PYTEST_ADDOPTS
  mechanically proven absent from the frozen execution environment authority

config addopts
  explicitly neutralized by the caller/project command with an effective
  -o addopts=
  or
  --override-ini addopts=
  or
  --override-ini=addopts=

@argument-file expansion
  unsupported in V1 qualification
```

ShellBeam SHALL NOT inject the `addopts=` override. Presence of `PYTEST_ADDOPTS`, absence of an effective empty `addopts` override, or an argument-file source that pytest would expand makes producer/invocation/dialect qualification unavailable in V1.

`PytestInvocationBinding` SHALL be produced by one option-aware resolver that honors pytest option termination and option arity. It SHALL NOT independently scan token strings after `--`, consume option values as new options, or reconstruct an expanded argument file. Any qualified pytest argument source beginning as a supported parser argument-file source is rejected rather than expanded by ShellBeam.

Repeated supported overrides use the resolver's qualified effective last-value semantics.

Examples:

```text
-o junit_family=xunit2
-o junit_family=legacy
→ NOT qualified
```

```text
-o junit_family=legacy
-o junit_family=xunit2
→ qualified
```

## 51. Config discovery is deferred

V1 SHALL NOT implement effective pytest config discovery. The explicit empty `addopts` override above is the strict V1 mechanism for neutralizing config-provided `addopts`; it is not config discovery.

Doing so would otherwise require a separate subsystem for:

- rootdir/config-file discovery;
- `pytest.toml` / `pytest.ini` / `pyproject.toml` / `tox.ini` / `setup.cfg`;
- `-c`;
- config `addopts`;
- `PYTEST_ADDOPTS`;
- precedence/version differences;
- plugin mutation of arguments/configuration.

Explicit `-o/--override-ini` is the bounded V1 dialect authority.

## 52. Plugin mutation limitation

`pytest-junit-xml@v1` assumes the standard qualified pytest built-in JUnit/config semantics.

A plugin that deliberately mutates `junit_family`, replaces the built-in JUnit producer, or intercepts its semantics is outside the V1 producer contract.

Mechanical parsing is not cryptographic attestation against adversarial plugin behavior.

## 53. Producer version qualification

`pytest-junit-xml@v1` is a versioned semantic adapter contract.

Runtime mechanical authority SHALL NOT require exact pytest distribution version attestation in V1.

Exact pytest version cannot be mechanically derived from:

- resolved argv alone;
- Python version;
- executable basename/path;
- `python -m pytest` syntax;
- absence of incompatible XML fields.

V1 SHALL NOT spawn `pytest --version` as hidden pre-execution tax solely for structured derivation.

V1 SHALL NOT inspect Python package metadata solely to invent producer version identity.

## 54. Release qualification across pytest versions

Although exact runtime pytest version is not operation-attested, adapter releases SHALL be tested against a bounded pytest compatibility matrix.

The qualification matrix SHALL cover at least:

- the minimum line intentionally supported by the adapter release;
- current supported line(s);
- the latest line qualified at release time;
- xunit2 output for all mechanically claimed semantic cases.

Unknown/future producer behavior outside the tested structural vocabulary SHALL degrade to partial/unsupported/unavailable rather than best-effort interpretation.

## 55. Five independent pytest qualification gates

Mechanical pytest artifact derivation requires independent qualification of:

```text
1. producer
2. invocation
3. dialect
4. immutable artifact
5. semantic shape
```

No gate is inferred from another.

Conceptually:

```text
ProducerQualified
AND InvocationQualified
AND DialectQualified
AND ArtifactQualified
AND SemanticShapeQualified
```

## 56. Qualification result is not a boolean

Qualification SHALL preserve per-axis state for debugging/provenance.

Conceptually:

```text
PytestQualification
  producer
  invocation
  dialect
  artifact
  semantic
```

Closed axis states include:

```text
qualified
qualified_complete
qualified_partial
unavailable
unsupported
contradictory
not_evaluated
```

Only semantically meaningful subsets apply to each axis.

Typed diagnostics SHALL identify the failed axis.

## 57. Semantic-shape qualification

Even when producer/invocation/dialect/artifact qualification passes, XML must be inside the tested `pytest-junit-xml@v1` structural vocabulary.

The parser SHALL enforce bounded structural facts including:

- accepted root/suite structure;
- bounded nesting;
- bounded suite/testcase counts;
- bounded strings/attributes;
- valid finite numeric counters/durations;
- recognized testcase outcome element cardinality;
- bounded pytest skipped-type vocabulary;
- absence of contradictory testcase state.

Malformed XML, unsafe depth, or loss of trustworthy parse boundaries may make the derivation unavailable/partial.

An unsupported extension isolated to one testcase need not discard independent mechanically proven records when the parser can preserve boundaries safely.

## 58. Semantic gate completeness

Semantic-shape state SHALL distinguish:

```text
qualified_complete
qualified_partial
unsupported
contradictory
unavailable
```

`qualified_partial` means:

- independent valid records remain mechanical;
- derivation parse outcome/completeness must record partial coverage;
- missing distinctions remain unavailable rather than negative evidence.

## 59. Core TestStatus remains coarse

Universal `TestStatus` remains closed:

```text
pass
fail
skip
error
```

Pytest-specific outcomes SHALL NOT be added merely for one producer.

In particular, universal core status SHALL NOT gain:

```text
xfail
xpass
setup_error
teardown_error
```

## 60. ProducerTestDisposition

Producer-specific distinctions SHALL use an additive versioned metadata envelope.

Conceptually:

```text
ProducerTestDisposition
  namespace
  vocabulary_version
  code
```

Core validates the envelope, version/bounds, and safe text shape.

Core SHALL NOT understand pytest disposition codes in order to make generic verification judgments.

`pytest-junit-xml@v1` owns the closed V1 disposition vocabulary:

```text
pytest:skip
pytest:xfail
```

## 61. Exact pytest testcase mapping

V1 mechanical mapping is:

```text
ordinary testcase with no failure/error/skipped
  → status = pass
  → disposition absent

<failure>
  → status = fail
  → disposition absent

<skipped type="pytest.skip">
  → status = skip
  → disposition = pytest:skip

<skipped type="pytest.xfail">
  → status = skip
  → disposition = pytest:xfail

<error>
  → status = error
  → exact setup/teardown phase unavailable
```

Unknown but structurally valid skipped subtype may retain coarse `skip` while producer disposition is unavailable and semantic coverage is partial, subject to parser qualification rules.

## 62. XPASS is not mechanically reconstructable in V1

Non-strict XPASS collapses to a normal passed testcase in pytest JUnit XML.

Therefore:

```text
XML says pass
→ mechanical core status = pass
→ MUST NOT claim pytest:xpass
```

Strict XPASS collapses to a failure representation without a dedicated mechanically typed XPASS marker.

Therefore:

```text
XML says failure
→ mechanical core status = fail
→ MUST NOT claim pytest:xpass from message text
```

V1 SHALL NOT recover XPASS by:

- message grep;
- marker reconstruction;
- test name convention;
- config inference;
- failure prose parsing.

## 63. XFAIL execution state is unavailable

`pytest:xfail` SHALL NOT imply that the test body executed.

Pytest supports xfail modes in which execution may not occur.

Therefore:

```text
producer_disposition = pytest:xfail
```

is not evidence for an `executed=true` fact.

V1 exposes no universal execution-state claim for this case.

## 64. Setup/teardown phase is unavailable

A pytest JUnit `<error>` maps mechanically to:

```text
TestStatus = error
```

The exact phase is unavailable in V1.

ShellBeam SHALL NOT parse generated message text such as setup/teardown prose to promote phase into mechanical truth.

## 65. ProducerSemanticsCoverage

Producer semantic fidelity SHALL be explicit at qualification/capability level.

Conceptually:

```text
ProducerSemanticsCoverage
  namespace = pytest
  vocabulary_version = 1
  format = junit_xml
  family = xunit2

  mechanically_observable:
    core:test_status_pass
    core:test_status_fail
    core:test_status_skip
    core:test_status_error
    pytest:skip
    pytest:xfail

  unavailable:
    pytest:xpass_exact
    pytest:error_phase
    pytest:xfail_execution_state
```

Coverage is not an authority downgrade.

Mechanically observable facts remain mechanical even when another semantic dimension is unavailable.

## 66. Coverage and P1 sufficiency

P1 SHALL distinguish insufficient semantic coverage from parser failure.

Conceptually:

```text
obligation requires exact XPASS detection
pytest-junit-xml@v1 lacks pytest:xpass_exact
→ provider semantic coverage insufficient
→ obligation not satisfied
```

Likewise:

```text
obligation requires setup-vs-teardown error distinction
→ pytest-junit-xml@v1 insufficient
```

V1 SHALL NOT interpret missing producer distinction as negative evidence.

Any policy requirement that consumes these dimensions must itself be an explicit bounded/versioned P1 requirement contract; free-form coverage strings do not automatically gain policy semantics.

## 67. Artifact testcase entry identity

One XML `<testcase>` element SHALL normalize to one mechanical E22 testcase observation.

Artifact-entry identity is structural and derivation-scoped.

Conceptually:

```text
ArtifactTestEntryRef
  artifact_blob_id
  suite_ordinal
  testcase_ordinal
```

A deterministic record identity may be derived from:

```text
H(
  derivation_key
  + "testcase"
  + suite_ordinal
  + testcase_ordinal
)
```

The exact closed record-ID schema SHALL be versioned with structured-result schema evolution.

## 68. Artifact entry identity is not logical pytest identity

Artifact entry identity guarantees:

```text
same immutable XML bytes
+ same derivation
→ same structural record identity
```

It does not guarantee:

```text
different run
→ same logical pytest item identity
```

V1 does not claim cross-run logical pytest item identity from JUnit XML.

## 69. ProducerTestAddress

Mechanically reported presentation/correlation fields may be represented separately.

Conceptually:

```text
ProducerTestAddress
  namespace = pytest
  vocabulary_version = 1
  suite_name
  classname
  name
```

This address is not:

- a globally unique identity;
- a pytest nodeid;
- a dedup key;
- P1 evidence identity by itself;
- a cross-run stability key.

## 70. Duplicate testcase semantics

Two XML testcase elements with identical `classname + name` SHALL remain two distinct E22 records.

ShellBeam SHALL NOT:

- deduplicate them;
- merge statuses;
- select the "worst" status;
- sum/replace duration to synthesize one item;
- infer setup/call/teardown phase from duplication;
- infer one logical pytest item identity.

A pytest item that creates a call failure and a teardown error may therefore produce distinct `fail` and `error` observations with the same producer-reported address.

## 71. Testcase record count semantics

For pytest JUnit V1:

```text
E22 test_case record count
= normalized XML testcase entry count
```

It SHALL NOT be described as logical pytest item count.

Pytest JUnit serialization can emit multiple testcase elements for one logical item under some failure combinations.

## 72. Suite aggregates are producer aggregates

Qualified pytest `<testsuite>` aggregate fields SHALL be treated as producer-reported suite aggregate facts.

ShellBeam SHALL NOT recompute suite `tests/failures/errors/skipped` by counting normalized testcase records.

A suite-level coarse E22 status may be deterministically normalized from qualified producer aggregate counters, not from child-record reaggregation.

V1 precedence for a mechanically valid suite aggregate is:

```text
errors > 0
  → error
else failures > 0
  → fail
else tests > 0 AND skipped == tests
  → skip
else
  → pass
```

This suite observation does not replace terminal child outcome.

## 73. Terminal receipt remains execution truth

Terminal receipt/evidence remains authoritative for child execution outcome.

Structured test cases and suite aggregates are detailed mechanical observations.

Therefore:

```text
many normalized pass records
MUST NOT override a failing/non-zero child receipt
```

and:

```text
child success
MUST NOT rewrite individual structured failure/error observations
```

These are independent truth dimensions.

## 74. Duration normalization

Test/suite duration is optional mechanical metadata.

When a qualified non-negative finite JUnit duration is present, V1 SHALL deterministically normalize seconds to whole milliseconds without using locale-dependent parsing.

Sub-millisecond remainder SHALL be truncated toward zero.

Invalid, negative, non-finite, or overflow duration SHALL make that duration unavailable or the affected record partial according to structural qualification; it SHALL NOT be guessed.

## 75. Derivation authority

`pytest-junit-xml@v1` records may be mechanical only when the input artifact and semantic fields satisfy the independent qualification contract.

Mechanical authority comes from:

- qualified pytest producer/invocation/dialect;
- immutable captured bytes;
- mechanically typed XML structure/attributes;
- deterministic normalization.

Mechanical authority does not come from:

- prose messages;
- test naming conventions;
- console output guesses;
- default pytest configuration assumptions;
- mutable artifact pathname after capture.

## 76. Derivation partiality

An artifact-backed derivation may be partial for independent reasons, including:

- unsupported local semantic extension;
- producer semantic coverage gap;
- bounded record limit reached after a safe boundary;
- a mechanically valid coarse fact with unavailable producer-specific distinction.

Partiality SHALL be explicit in derivation parse outcome/completeness and qualification diagnostics.

Mechanical facts already proven do not become advisory merely because another fact is unavailable.

## 77. Capture failure and parser execution

The parser SHALL run only when:

```text
CaptureResult == captured
AND ArtifactBlobRef resolves retained bytes
AND producer/invocation/dialect qualification is sufficient
```

A syntactically valid JUnit document does not repair:

- preexisting-unqualified artifact;
- managed path collision;
- changed-during-capture source;
- compacted blob;
- corrupt/private blob mismatch;
- missing durable capture intent authority.

## 78. Structured derivation identity

Artifact-backed derivation identity SHALL include the full `StructuredInputRef`, producer identity, derivation schema version, and derivation config digest.

For an artifact input, full source identity therefore includes:

- operation-scoped blob provenance;
- immutable content identity;
- path/capture provenance encoded by the versioned ref.

Equal content SHA across runs does not collapse derivation provenance.

## 79. Record/source retention invariant

While detailed records for a derivation remain available:

```text
all artifact blobs required to substantiate those detailed records
MUST remain retained/resolvable
```

After derivation compaction:

```text
blob bytes MAY be retired according to reference-aware retention
```

A resolver returning `compacted` is an intentional historical state, not silent corruption.

## 80. Public inspect behavior

Structured inspection SHALL expose bounded normalized records and qualification/completeness facts.

It SHALL NOT expose arbitrary blob bytes or private filesystem paths as a generic download API.

Model-facing output may expose bounded provenance identifiers/digests sufficient to understand authority without turning private blob storage into a file server.

## 81. Failure/diagnostic taxonomy

V1 SHALL use typed diagnostics rather than one generic `qualified=false` bit.

The vocabulary must cover at least:

```text
pytest_producer_unqualified
pytest_junit_output_unqualified
pytest_junit_dialect_unqualified
pytest_semantic_shape_unsupported
pytest_semantic_shape_contradictory

artifact_preexisting_unqualified
artifact_path_unqualified
artifact_kind_mismatch
artifact_managed_path_collision
artifact_changed_during_capture
artifact_budget_exceeded
artifact_capture_unavailable
artifact_blob_compacted
artifact_blob_unavailable
artifact_blob_conflict
```

Exact code names may be normalized during implementation planning but SHALL remain closed/versioned and ontology-specific.

## 82. Explicit caller behavior

An explicit caller adapter request is a contract request, not a hint.

If pre-execution producer/invocation/dialect requirements are incompatible, ShellBeam SHALL fail the structured-adapter precondition rather than silently run an unrelated parser.

If the child is allowed to execute and artifact capture later fails, child receipt truth remains unchanged and structured result becomes unavailable/partial with typed capture diagnostics.

## 83. Direct argv auto-selection

Direct argv auto-selection SHALL be intentionally strict.

It may select `pytest-junit-xml` only from a supported exact pytest producer form that explicitly requests built-in JUnit output.

Auto-selection SHALL NOT append JUnit flags.

Dialect qualification remains an independent gate; selection alone does not grant mechanical parser authority.

## 84. Project-command integration

A project command may carry explicit structured-adapter intent and supplies an immutable resolved argv binding.

Qualification SHALL use that exact resolved argv.

Project-command classification such as `test` does not itself establish:

- pytest producer;
- JUnit output path;
- xunit2 dialect.

The capture intent derived from a project command SHALL be bound into the same operation reservation/replay identity as a raw direct start.

## 85. No generic JUnit fallback

If pytest qualification fails, ShellBeam SHALL NOT fall back to a generic JUnit parser and then claim equivalent mechanical semantics.

Generic JUnit may become a separately designed provider family later, with its own semantics coverage and qualification contract.

`pytest-junit-xml@v1` remains producer-specific.

## 86. Security boundaries

Artifact capture SHALL inherit ShellBeam's path-sensitive security posture:

- workspace-root descriptor authority for both baseline and Phase A;
- descriptor-relative traversal as a V1 qualification requirement for ordinary workspace artifact paths;
- no-follow semantics for every traversed path component and final target;
- fail-closed `path_unqualified / unavailable` when the platform cannot provide that ordinary-path traversal contract;
- regular-file requirement;
- private state permissions;
- create-only/idempotent private blob identity;
- bounded bytes/counts/depth/time/concurrency;
- no reopening workspace path after source acquisition;
- no implicit live-authority eviction.

Security failures SHALL fail closed without changing child outcome.

## 87. Resource limits V1

The implementation plan SHALL choose explicit defaults within these protocol bounds:

```text
MaxArtifactCaptureIntentsPerOperation <= 8
pytest-junit-xml capture intents       = exactly 1
MaxPinnedArtifactHandlesPerOperation  <= capture intent count
MaxPinnedArtifactHandlesGlobal        = finite
AcquisitionConcurrency                = finite
MaterializationQueueDepth             = finite and <= pinned-handle authority
MaxTerminalAcquireDuration            = finite
MaxArtifactBlobBytes                  <= 64 MiB
MaxArtifactBlobStoreBytes             = finite sub-budget of state authority
Max XML depth/count/string bytes       = finite
```

Protocol ceilings are not permission for implementation defaults to use the maximum.

## 88. Compatibility with current store limits

Blob retained-byte accounting SHALL integrate with the existing total state authority rather than introduce an independent unbounded counter.

Control reserve remains protected.

Blob-budget reservation must be concurrency-safe; an advisory `stateBytes` snapshot alone is not sufficient to prevent oversubscription by concurrent materializers.

## 89. Adapter release qualification matrix

The implementation SHALL include a provider qualification suite covering at least:

### Producer forms

```text
pytest
python -m pytest
wrapper negatives
shell-string negative
```

### JUnit output options and invocation authority

```text
--junitxml PATH
--junitxml=PATH
--junit-xml PATH
--junit-xml=PATH
repeated option last-value cases
relative JUnit path resolves from frozen ResolvedCWD
absolute in-workspace path normalizes to same workspace binding
path requiring ~ expansion → unqualified
path requiring environment-variable expansion → unqualified
PYTEST_ADDOPTS absent → eligible
PYTEST_ADDOPTS present → unqualified
config addopts explicitly neutralized with effective empty override → eligible
missing effective empty addopts override → unqualified
@argument-file source → unqualified
producer_binding_digest changes when any canonical invocation-authority fact changes
crash recovery reproduces identical PytestInvocationBindingV1 without environment re-observation
```

### Dialect

```text
explicit xunit2 qualified
explicit legacy after xunit2 → unqualified
explicit xunit2 after legacy → qualified
default-only → unqualified
config-only → unqualified
```

### Capture provenance

```text
baseline absent
pre-existing artifact
missing artifact
path replacement after pin
same-object mutation during copy
managed same-path collision
byte-budget exhaustion
acquisition timeout/late result
ambiguous private commit
crash before blob commit
crash after blob commit
```

### Test semantics

```text
pass
failure
regular skip
xfail
non-strict xpass collapse
strict xpass collapse
error
unknown skipped subtype
duplicate classname+name entries
call-failure + teardown-error multi-entry shape
suite aggregate counts
```

### Retention/recovery

```text
session collected while blob retained
multiple derivations reference one capture blob
one derivation compacted while another still detailed
last detailed reference compacted
ref-acquire vs retire race → serialized; no dangling detailed derivation
retirement barrier wins → new detailed reference cannot attach
blob tombstone resolution
staging orphan cleanup
committed-but-unbound recovery
recovery claim survives ordinary session/operation GC
recovery claim → detailed reference handoff is atomic
cut identity reconstructs byte-for-byte after crash recovery
terminal_cut is reused, not regenerated
observation_cut is reused, not regenerated
orphan GC after recovery pass
```

## 90. Test fixtures SHALL be producer-realistic

Pytest qualification fixtures SHALL be generated from real supported pytest lines where practical and then frozen as test fixtures with provenance.

Hand-authored XML may be used for parser boundary/error tests, but SHALL NOT be the sole evidence that a producer-specific semantic shape is real.

Fixture provenance SHALL identify the producer version used for release qualification without claiming that runtime operations attest that exact version.

## 91. P1 integration

Artifact-backed structured test observations SHALL feed existing P1 evidence/sufficiency through the existing structured-results/evidence bridge.

They SHALL NOT create a parallel Python-specific verification gate.

P1 consumes:

- coarse test status;
- mechanical authority;
- source/operation/environment/command compatibility;
- provider semantic coverage where an explicit requirement asks for it;
- existing terminal child truth independently.

## 92. Coverage is not a scalar confidence score

`ProducerSemanticsCoverage` SHALL remain explicit capability facts.

V1 SHALL NOT compress semantic fidelity into a confidence percentage or scalar risk value used to skip obligations.

P1 sufficiency remains requirement-specific and fail-conservative.

## 93. Terminal truth is never re-aggregated from records

Structured result code SHALL NOT compute child success by folding normalized test records.

Likewise, P1 SHALL NOT replace durable terminal outcome merely because structured records look internally consistent.

The relationship is additive:

```text
receipt
  = execution truth

structured records
  = detailed producer observations
```

## 94. Future Vitest/Jest adapters

Vitest/Jest SHALL be designed only after pytest artifact-input infrastructure is proven.

The next adapter SHALL prefer an existing native deterministic machine contract that can be qualified without ShellBeam mutating command semantics.

It may use:

- raw terminal machine output; or
- artifact blob input;

but SHALL NOT be forced into JUnit merely because pytest uses JUnit.

Adapter naming SHALL identify producer/native format contract rather than generic ecosystem labels.

## 95. Future ESLint adapter

ESLint follows Vitest/Jest in delivery order.

Its provider contract SHALL preserve:

- exact producer/invocation qualification;
- native deterministic machine output;
- location/diagnostic mechanical mapping;
- explicit semantic coverage;
- no prose extraction.

The existing artifact-input work may be reused only if ESLint's qualified contract actually produces an artifact rather than terminal raw output.

## 96. TypeScript compiler is deferred pending qualification

TypeScript compiler Structured Results SHALL NOT be implemented merely because `tsc` is available.

A separate qualification step must decide:

- which native output contract is deterministic enough;
- whether raw output or artifact input is authoritative;
- what diagnostics/status semantics are mechanically observable;
- which version/config facts must be bound.

No TypeScript LSP prerequisite is implied.

## 97. Migration and compatibility guardrails

The StructuredInputRef migration SHALL preserve:

- historical v1 raw-output readability;
- existing Go adapter behavior;
- existing terminal receipt semantics;
- existing result inspection/cursor safety;
- bounded record persistence;
- compaction behavior for raw-output derivations.

Artifact-specific state SHALL not retroactively change historical raw-output derivation identity.

## 98. Cursor and inspection identity

Result cursors remain bound to operation and derivation identity.

If structured-result schema evolution changes derivation identity representation, cursor schema/version migration SHALL be explicit.

A cursor SHALL never be accepted across a different derivation or compacted-source state merely because the normalized records have similar names/statuses.

## 99. Compaction is not parsing failure

`ArtifactBlobState=compacted` means bytes were intentionally retired after eligible structured-detail compaction.

It SHALL NOT be surfaced as:

```text
parser malformed
producer failed
capture changed_during_capture
```

These states have distinct provenance and diagnostics.

## 100. Recovery is not re-execution

Recovery from a durably committed private blob SHALL NOT rerun pytest.

It SHALL NOT reopen `junit.xml`.

It SHALL NOT alter terminal receipt.

Recovery continues only the deterministic structured derivation from already committed immutable authority.

## 101. Observability and audit

ShellBeam SHALL preserve enough structured metadata to explain:

- why the adapter was selected;
- which pytest producer form was qualified;
- which effective JUnit path was bound;
- how xunit2 was explicitly proven;
- pre-spawn baseline result;
- capture result/diagnostic;
- blob identity/content digest/size;
- semantic-shape completeness;
- producer semantics coverage;
- compaction/retention state.

These facts SHALL be bounded and machine-readable.

## 102. No hidden auto-repair

ShellBeam SHALL NOT silently make an unqualified invocation qualified by:

- adding `--junitxml`;
- adding `-o junit_family=xunit2`;
- deleting a stale artifact;
- changing CWD;
- modifying pytest config;
- installing a plugin;
- changing output path to a ShellBeam-private path.

A future opt-in execution-transform feature would require separate mutation/authority design.

## 103. Design invariants

The following are frozen V1 invariants:

1. Mutable pathname is never parser authority.
2. Successful immutable materialization is required for `ArtifactBlobRef`.
3. Pre-spawn ordinary-path baseline must be absent for mechanical attribution.
4. ShellBeam never deletes/truncates a stale artifact to qualify capture.
5. Terminal Phase A pins the exact file object before managed-shell release and before durable receipt publication.
6. Phase B never reopens the workspace pathname.
7. Same-object mutation during copy yields no blob authority.
8. Capture intent is durable and replay-protected before spawn.
9. Blob ID is operation-scoped storage identity; SHA-256 is byte identity.
10. No cross-run blob deduplication in V1.
11. Blob retention is capture-owned and reference-aware across derivations.
12. Session retention does not delete live structured artifact authority.
13. Live referenced blobs are never implicitly evicted for space.
14. Committed private blobs may recover derivation only against durable frozen capture authority.
15. `pytest-junit-xml@v1` requires explicit built-in JUnit output and explicit xunit2 override.
16. Pytest config discovery is deferred.
17. Exact pytest runtime version attestation is not required in V1.
18. Core `TestStatus` remains `pass|fail|skip|error`.
19. XPASS is not reconstructed from prose.
20. Error phase is unavailable in V1.
21. `pytest:xfail` does not imply body execution.
22. Duplicate XML testcase entries are preserved, never merged by `classname+name`.
23. Testcase record count is artifact-entry count, not logical pytest test count.
24. Producer suite aggregates are not recomputed from testcase records.
25. Terminal receipt remains child/suite execution truth.
26. Coverage gaps do not downgrade independent mechanical facts to advisory.
27. Generic JUnit fallback is forbidden.
28. Structured projection never mutates child execution semantics.

## 104. Deferred beyond V1

Explicitly deferred:

- pytest effective config discovery;
- exact runtime pytest distribution-version attestation;
- plugin provenance/attestation;
- hostile arbitrary same-user filesystem writer exclusion;
- operation-private pytest output injection;
- generic JUnit family adapter;
- native pytest XPASS recovery beyond JUnit capability;
- setup-vs-teardown phase recovery from prose;
- cross-run logical pytest test identity;
- cross-run content-addressed blob dedup/refcounting;
- arbitrary blob download/file-serving API;
- Vitest/Jest/ESLint concrete format contracts;
- TypeScript compiler adapter until qualification.

## 105. Implementation sequencing implications

The implementation plan SHALL preserve this dependency order:

```text
A. StructuredInputRef schema migration
B. durable ArtifactCaptureIntent admission/replay authority
C. path-scoped managed artifact claim + baseline
D. terminal Phase A source acquisition/resource bounds
E. private blob store + Phase B materialization/recovery
F. artifact resolver + reference-aware retention/compaction
G. pytest invocation/dialect qualification
H. pytest xunit2 parser + normalized semantics/identity
I. public inspect/schema/capability integration
J. P1 evidence integration + real-daemon qualification matrix
K. deploy pytest adapter
```

Vitest/Jest work starts only after pytest V1 acceptance.

## 106. Acceptance boundary

The design is successful only if implementation can prove all of the following without weakening existing E22/P1 authority:

```text
pytest command explicitly requests JUnit output + explicit xunit2
→ child executes unchanged
→ stale pre-existing result cannot be misattributed
→ exact terminal file object is pinned before release
→ immutable bytes are durably captured or capture fails closed
→ parser reads only retained private blob bytes
→ pytest typed semantics normalize mechanically and conservatively
→ duplicate testcase entries remain distinct
→ suite aggregates and child receipt retain independent truth
→ blob bytes survive session retention while needed
→ compaction can intentionally retire bytes without ref rebinding
→ crash recovery never reopens workspace result path
→ P1 consumes the evidence without a Python-specific parallel ontology
```

## 107. Final architectural position

ShellBeam SHALL treat producer artifacts as engineering truth only after provenance-safe capture converts a mutable execution-side pathname into immutable ShellBeam-owned source authority.

`pytest-junit-xml@v1` is the first consumer of that primitive, not a special-case exception to it.

The long-term extension path is therefore:

```text
execution truth
      ↓
qualified native machine contract
      ↓
RawOutputRef OR ArtifactBlobRef
      ↓
producer-specific deterministic adapter
      ↓
shared E22 structured records
      ↓
shared P1 evidence/sufficiency semantics
```

This architecture permits Python and later JavaScript/TypeScript ecosystems without moving producer quirks into the universal evidence ontology and without weakening ShellBeam's machine-truth boundary.
