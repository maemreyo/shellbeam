# ShellBeam A2.4 Evidence Ledger and Expected Outputs Design

Date: 2026-08-15
Status: approved for execution by continuation mandate
Scope: A2.4 evidence ledger + expected-output/artifact observation only

## 1. Purpose

ShellBeam already has durable terminal receipts, workspace provenance, Event Journal event kinds for evidence/artifacts, project-manifest expected outputs, and structured result records. The missing A2.4 capability is one coherent mechanical evidence model that binds those existing authorities together without inventing a parallel execution truth store.

A2.4 answers bounded questions such as:

- Did this declared verification command mechanically succeed?
- Were its required declared outputs present and, when requested, completely digested?
- What source/workspace observation was the evidence bound to?
- Is that evidence still current according to facts ShellBeam can actually prove?

It does not judge whether a test suite is semantically comprehensive.

## 2. Authority classes

Authority remains layered and explicit:

1. The canonical terminal `receipt.Receipt` is the only authority for child spawn/exit/outcome/output-completeness truth.
2. A frozen evidence contract is authority for what verification/artifact observations the caller requested or a typed manifest declared.
3. Filesystem artifact observations are mechanical observations of declared repo-relative paths at a bounded terminal cut.
4. Workspace/source validity is derived from explicit workspace observations. A fast generation is not an exact source-content digest.
5. E21 `evidence_recorded`, `evidence_validity_changed`, and `artifact_observed` events are bounded change-feed notifications only. They are not evidence authority.
6. Structured code intelligence and structured output records may be referenced as derived facts but never turn a child into `pass` evidence.

A derived evidence failure never rewrites canonical receipt outcome/exit code.

## 3. Evidence contracts

### 3.1 Caller-declared contract

Protocol-v2 raw starts may declare an optional `evidence` object:

```json
{
  "verification_kind": "test",
  "source_scope": "full",
  "expected_outputs": [
    {"path":"dist/app","kind":"file","digest":"sha256","required":true}
  ]
}
```

The contract is observation metadata, not execution semantics. It therefore participates in the operation observation-binding fingerprint, not the execution fingerprint.

Caller evidence contracts require a workspace ID when expected outputs are non-empty because output paths are repository-relative. They are rejected for protocol v1.

`verification_kind` is one of:

- `format`
- `test`
- `build`
- `generate`
- `release`
- `artifact`

`artifact` is used when artifact observation is the only declared verification contract.

`source_scope` is optional and one of `none|affected|full`. `affected` is only an assertion of declared scope until a versioned selector proves an exact affected set; A2.4 must not infer affected files from arbitrary shell text.

Expected output declarations reuse the project-manifest output contract:

- path: normalized repository-relative path;
- kind: `file|directory|symlink`;
- digest: empty/`none|sha256|tree-sha256`;
- role must be empty for per-command/caller outputs;
- required defaults to the already-normalized boolean in the public contract;
- maximum 64 outputs;
- existing project path/string bounds apply.

The project package exposes a canonical validation/copy function instead of duplicating these rules in IPC/MCP code.

### 3.2 Typed project-command contract

Typed project commands already read a validated manifest exactly once during bind. A2.4 extends frozen `project.CommandBinding` so the terminal path never rereads the current manifest for evidence metadata.

Introduce `CommandBinding` schema v2 with frozen:

- `Kind`
- `SourceScope`
- `ExpectedOutputs`

New binders always write v2. Validation continues to accept persisted schema-v1 bindings with their historical field set. Schema-v1 bindings never gain inferred expected outputs after admission; evidence inspection reports the contract as legacy/insufficient where needed.

Receipt schema v3 and operation reservation schema v3 remain unchanged. Their nested binding validator accepts project binding v1/v2.

Lost-response retry remains retry-first: once the operation is admitted, current manifest/provider state is never reread merely to obtain evidence metadata.

### 3.3 Durable terminal binding

For raw starts, the normalized caller evidence contract is durably copied into the operation reservation and terminal receipt. For typed starts, the frozen project-command binding is the durable contract source.

The terminal worker receives only durable receipt authority; it never depends on the mutable request object or current manifest.

## 4. Evidence record

Add `internal/core/evidence` with a versioned immutable `Record` containing bounded mechanical facts:

- schema version;
- evidence ID;
- operation/session/activity/workspace IDs;
- verification kind;
- source scope;
- command authority:
  - request/execution/observation fingerprints;
  - typed project command ID, binding digest and manifest digest when present;
- receipt reference/digest and canonical terminal result summary;
- base evidence result;
- source binding facts actually proven;
- artifact observations;
- exact-source/environment/toolchain fingerprints only when supplied by a trusted compatible producer;
- completion time.

The evidence ID is deterministic from non-secret canonical authority metadata such as receipt digest + evidence-contract digest + schema version. It must never be a public deterministic hash of arbitrary environment secrets or artifact contents alone.

### 4.1 Base result

`result` is one of:

- `pass`
- `fail`
- `incomplete`
- `ambiguous`

Mechanical derivation:

- `pass`: authoritative terminal receipt is successful and every required artifact condition is satisfied.
- `fail`: authoritative terminal receipt is a known failure/timeout/kill, or a required artifact is missing/kind-mismatched/digest-mismatched.
- `incomplete`: required artifact observation could not be completed, output/source authority needed by the declared contract is unavailable, or terminal authority is incomplete.
- `ambiguous`: the canonical receipt itself is ambiguous/abandoned or contradictory persisted evidence authority is detected.

Optional artifact failure is recorded but does not convert an otherwise passing base result to fail.

A language diagnostic, AST/LSP fact, structured test record, or current manifest contents cannot create `pass` evidence.

### 4.2 Source binding and validity

A2.4 is honest about the source facts currently available.

Stored source binding may contain:

- workspace/repository ID;
- pre/post workspace generation and observation quality from terminal receipt provenance;
- observed-change flag;
- exact source-content/VCS digest only if a trusted producer supplies an `ExactSourceSnapshot`.

Current validity dimensions are returned separately from immutable base result:

- `source_match`: `exact|fast|mismatch|unknown`
- `freshness`: `current|stale|unknown`
- `artifact_match`: `current|changed|missing|not_required|unknown`
- `policy_match`: `current|changed|unknown`

A2.4 does **not** synthesize exact source snapshots. If only workspace generation is known, the strongest source match is `fast`. If no compatible current observation is available, it is `unknown`.

`exact-current` is therefore impossible until all exact dimensions are actually available. A2.5 may later supply environment/toolchain/exact source facts without changing A2.4 receipt authority.

`source_scope=affected` does not narrow freshness automatically. Until a versioned affected-set selector proves an exact subset binding, freshness conservatively applies to the full effective source observation.

## 5. Artifact observer

Add a bounded artifact observer under the evidence application boundary. It runs only after a durable terminal receipt and only when the frozen contract declares expected outputs.

### 5.1 Path authority

Artifact paths are resolved relative to the registered workspace root bound by workspace ID. Observation never trusts child CWD as repository authority for expected-output paths.

For every path:

- use `Lstat` semantics for the declared path;
- never follow a symlink merely to satisfy a file/directory declaration;
- symlink observation records bounded link text and does not traverse an escaping target;
- intermediate path traversal is checked so repo-relative declaration cannot escape the workspace through symlinks;
- runtime observation is described as observation-time path confinement, not child filesystem confinement.

### 5.2 Bounded metadata

Each observation records only bounded metadata:

- normalized path;
- declared/observed kind;
- existence;
- required flag;
- size where meaningful;
- mtime as context only;
- requested digest mode;
- completed digest only when fully proven;
- observation quality/status;
- bounded symlink text where applicable;
- observed time.

No arbitrary artifact contents are stored or returned.

### 5.3 Digests

- `sha256` on a file is streamed to completion under explicit per-file/total work ceilings. A partial hash is never emitted as identity.
- `tree-sha256` on a directory uses deterministic lexical relative-path order, includes entry kind/path and complete requested file identities, never follows symlinks, and is emitted only if the full tree fits bounded entry/byte/work ceilings.
- If a required complete digest cannot be obtained within limits or because of I/O mutation/race, observation is `unavailable` and evidence becomes `incomplete`.
- A requested digest incompatible with declared kind is rejected at contract validation rather than guessed at observation time.

Digest output is evidence metadata for the explicitly declared artifact. It is not reused as a public identifier for unknown secret/environment bytes.

### 5.4 Stable statuses

Artifact status is one of:

- `current`
- `missing`
- `kind_mismatch`
- `digest_mismatch`
- `unavailable`

Observation quality distinguishes complete metadata/digest from unavailable facts. Required-vs-optional remains explicit.

## 6. Terminal scheduling and exactly-once persistence

Add an `EvidenceWorker` alongside current structured/telemetry workers.

Rules:

1. scheduling happens only after `PublishTerminal` has durably succeeded;
2. scheduling itself is bounded/non-blocking with respect to artifact scanning;
3. worker backpressure/failure never rewrites the terminal receipt;
4. no evidence/artifact filesystem work occurs before spawn or on ordinary start/poll;
5. worker derives a deterministic evidence ID and persists by create-once/CAS semantics;
6. duplicate scheduling for the same terminal cut is idempotent;
7. conflicting bytes under the same evidence identity fail closed and are surfaced as ambiguous/internal provenance failure, never overwrite canonical evidence.

If no explicit verification/evidence contract exists, no evidence record or artifact scan is scheduled. A bare arbitrary shell command with no declared intent/output contract does not become verification evidence merely because it exited 0.

Typed project commands with a frozen verification kind and/or expected outputs are eligible. Raw starts are eligible when a caller evidence contract is present or a declared intent kind maps mechanically to `format|test|build|generate|release`.

## 7. Event Journal integration

After durable state transitions:

- each completed expected-output observation may schedule bounded `artifact_observed` event metadata containing IDs/statuses only;
- durable first creation of an evidence record schedules `evidence_recorded`;
- an explicitly observed change from the last persisted validity observation schedules `evidence_validity_changed`.

Events carry bounded references/status metadata, never raw artifact contents or environment values.

Event scheduling failure does not invalidate already-durable evidence authority; existing observation obligations preserve retry semantics.

## 8. Inspection and lazy invalidation

Expose `inspect.evidence` through the existing single `local_shell` tool.

Input supports bounded filters:

- `operation_id`
- `workspace_id`
- optional verification kind/result/validity filters;
- bounded `max_records`;
- opaque continuation for pagination.

No full-state scan is allowed. Persistence maintains bounded indexes needed for operation/workspace lookup.

Inspection returns:

- explicit status `available|never_run|unavailable`;
- immutable evidence records;
- current validity observations;
- opaque continuation when more matching records exist.

“Never run” is represented by absence/status, never a fabricated failed record.

### 8.1 Current validity work

Current validity is evaluated only on explicit inspection (or an already-occurring managed workspace observation), never in a background watcher.

For source freshness:

- use existing workspace registry/coherence observation APIs;
- compare exact snapshot only when both sides provide compatible exact facts;
- otherwise compare compatible fast generation when available;
- otherwise return unknown.

For artifact freshness:

- do **not** rescan all artifacts by default merely because evidence is inspected;
- when the evidence source is already known stale/mismatch, artifact validity may remain `unknown` unless caller explicitly requests bounded artifact revalidation;
- optional `revalidate_artifacts=true` performs the same bounded observer against frozen declarations and records a validity observation, never changes immutable base evidence.

A changed current validity is stored separately from the immutable evidence record and may emit `evidence_validity_changed`.

No automatic rerun is triggered for stale evidence.

## 9. Persistence model

Reuse the current per-user state repository.

Recommended bounded layout:

```text
/evidence/records/<evidence_id>.json
/evidence/by-operation/<operation_id>.json
/evidence/by-workspace/<workspace_id>/<sequence>.json
/evidence/validity/<evidence_id>.json
```

Exact physical layout may adapt to existing repository helpers, but requirements are:

- no scan of all sessions to inspect evidence;
- immutable record bytes after first successful persist;
- bounded operation/workspace indexes;
- explicit retention/compaction semantics;
- compaction never silently converts missing authority into `pass`;
- validity observations are separate derived state and cannot rewrite base record result.

## 10. Public contracts and capability discovery

Add capability schema/limits for at minimum:

- evidence record schema versions;
- artifact observation schema versions;
- max evidence records returned;
- max expected outputs;
- max artifact metadata bytes;
- max artifact digest bytes/work;
- max tree entries;
- max pagination cursor bytes.

Promote only after full implementation and acceptance:

- `FeatureEvidenceLedger`
- `FeatureExpectedOutputs`

Both remain unavailable until admission binding, terminal derivation, persistence, inspection, IPC/MCP schemas, and real-daemon acceptance are complete.

Exactly one MCP tool remains `local_shell`.

## 11. Privacy and security

A2.4 must preserve monotonic privacy:

- never store raw environment secret values;
- never publish deterministic hashes of arbitrary unknown secret values as privacy workaround;
- never store artifact file contents;
- never follow expected-output symlinks outside the registered workspace;
- never infer command semantics from arbitrary shell text;
- never infer source coverage from path names alone;
- never claim exact source/environment/toolchain identity when producer evidence is unavailable;
- bound error details, symlink text, artifact paths, metadata and all result sets.

## 12. Performance / no-tax invariants

With A2.4 compiled and available but unused, ordinary compatible start must perform:

- zero artifact `stat`/walk/hash work;
- zero evidence history scan;
- zero current manifest read beyond work already required for an explicitly requested typed project command;
- zero evidence validity refresh;
- zero network/SSH/gh access;
- no background evidence worker work for an operation without an evidence contract.

For an explicit evidence contract, admission cost is limited to validation/deep-copy/fingerprint of at most 64 small declarations. Artifact I/O runs terminal-only after durable receipt.

Typed-command warm binding p95 remains <= 10 ms; freezing already-loaded manifest metadata must not add provider/network subprocess work.

## 13. Acceptance

A2.4 is complete only when all are proven:

1. New typed project-command bindings freeze kind/source-scope/expected outputs; persisted v1 binding compatibility is retained.
2. Lost-response typed retry reuses the frozen v2 contract without reading current manifest/provider state.
3. Raw protocol-v2 evidence contracts participate in observation binding, survive durable reservation/receipt, and conflicting retry metadata fails before spawn.
4. Artifact observer proves file/directory/symlink behavior, missing/kind mismatch, optional-vs-required, file SHA-256, deterministic full tree SHA-256, symlink non-following, path escape rejection and bounded unavailable behavior.
5. Partial digest is never exposed as exact identity.
6. Evidence worker schedules only after durable terminal receipt, exactly once/idempotently; backpressure never rewrites child truth.
7. Evidence result derivation matches receipt/artifact rules and never uses language intelligence as test authority.
8. Immutable evidence + indexes survive daemon restart and bounded inspection does not scan all sessions.
9. `inspect.evidence` distinguishes available/never-run/unavailable and returns bounded pagination.
10. Current validity never claims exact when only fast/unknown source facts exist; no automatic rerun occurs.
11. E21 evidence/artifact event kinds are emitted only after durable corresponding state and carry bounded metadata.
12. IPC/MCP/schema wiring stays inside one `local_shell` tool with closed unions/unknown-field rejection.
13. Real daemon acceptance proves typed and caller expected outputs, successful artifact verification, required missing artifact failure without receipt rewrite, and stale/unknown source validity.
14. No-tax regression proves ordinary start/poll without evidence contract performs no artifact/evidence work.
15. Capability discovery promotes `evidence_ledger` and `expected_outputs` only at final checkpoint with truthful limits.
16. Relevant race, `go mod verify`, `devctl check`, dirty/full tests, privacy anti-goal scans, diff checks and `.codegraph` gates pass.

## 14. Boundaries after A2.4

A2.4 intentionally does not implement:

- environment/toolchain fingerprint production (A2.5);
- arbitrary host process inspection (A2.5);
- advisory mutation scopes (A2.6);
- persistent named runtime sessions (B1);
- experimental safety checkpoint/input tracing/resource enforcement.

Future slices may enrich evidence validity with new proven facts, but they must not mutate historical receipt/evidence authority or reinterpret an old `pass` as exact-current without compatible versioned observations.
