# ShellBeam A2.6 Advisory Mutation Scopes Design

Status: approved design; self-reviewed against the live A2.5 branch before repository commit

Scope: A2.6 / E10 advisory mutation scopes only; no locking, permission, scheduling, or provider-indexing semantics

## 1. Purpose

ShellBeam already exposes durable execution receipts, activity/workspace identity, bounded workspace observations, output views, structured results, evidence, environment/toolchain fingerprints, and current-host process inspection. The remaining A2 source-awareness/cooperative-concurrency gap is a cheap way for independent reasoning agents or sessions to declare which repository-relative regions they currently intend to read or mutate and to discover likely overlap before they waste work.

A2.6 adds short-lived durable advisory mutation scopes. A scope is a caller declaration, not an ownership grant. ShellBeam records the declaration exactly once, expires it automatically by TTL semantics, and derives bounded overlap advisories on explicit scope operations or inspection.

A2.6 must improve multi-agent coordination without turning ShellBeam into a scheduler, lock manager, permission system, or workflow engine. In particular, no active scope may prevent `start`, editing, Git operations, worktree removal, or any other action the current local user is otherwise allowed to perform.

## 2. Goals

A2.6 SHALL:

- let a caller declare a short-lived `read` or `mutate` scope for one activity and one workspace;
- use a deliberately small deterministic repository-relative selector language;
- make set/release mutations retry-safe under lost responses;
- preserve an unexpired declaration across bridge or daemon restart without treating it as process authority;
- expire declarations lazily without background watchers or timers;
- return bounded overlap advisories for active scopes in the same workspace;
- preserve activity identity as correlation only, never ownership;
- expose active scopes and advisories through explicit inspection and modern activity inspection;
- publish a bounded Event Journal change signal for successful durable set/release mutations without making the journal replay authority;
- keep scope persistence separate from operation/session control state and from the activity operation-history record;
- perform no Git, shell, subprocess, network, provider, or workspace-refresh work merely to evaluate overlap;
- preserve the single `local_shell` MCP tool;
- impose zero scope-store or overlap work on ordinary command admission when A2.6 is unused.

## 3. Non-goals

A2.6 does NOT:

- lock files, directories, repositories, worktrees, branches, or sessions;
- deny, delay, queue, serialize, cancel, or automatically retry commands;
- grant or revoke permissions;
- claim that a caller owns a path because it declared a scope;
- infer a scope by parsing shell commands, argv, diffs, ASTs, LSP results, process names, or conversation text;
- auto-stash, auto-revert, auto-reset, auto-switch branches, or auto-remove worktrees;
- require a scope before source mutation;
- make `git switch`, `git stash`, `git reset`, `git rebase`, `git prune`, worktree removal, or arbitrary shell execution fail because of an overlap;
- create a distributed lock or multi-machine coordination service;
- identify or authenticate an "agent" beyond existing activity/workspace correlation;
- infer semantic dependencies or affected-code regions;
- add a general glob engine, regex selector language, query DSL, vector database, or semantic index;
- start background cleanup, polling, filesystem watchers, or periodic conflict scans;
- add a second MCP tool;
- rewrite operation receipts, evidence, telemetry, or reproduction records.

## 4. Authority model

A mutation scope is authoritative only for the narrow statement:

> a caller successfully committed this declaration under this mutation identity at this time.

It is not authoritative for:

- who currently edits a file;
- whether another process can edit a file;
- whether the caller will actually touch every declared path;
- whether an observed change was caused by the declaring activity;
- whether an undeclared region is safe to mutate;
- source correctness, execution correctness, or evidence validity.

Overlap advisories are deterministic mechanical projections of active declarations. They are not persisted as independent truth and may be recomputed from current active scope records.

Expired scopes are inactive by definition even if lazy cleanup of their stored file has not yet completed. Cleanup success therefore never controls the truth of whether a scope is active.

## 5. Public surface and one-tool contract

A2.6 remains inside the existing `local_shell` tool and adds three modern actions:

```text
mutation_scope.set
mutation_scope.release
inspect.mutation_scopes
```

Modern `inspect.activity` is also extended with bounded A2.6 facts when the capability is available.

No second MCP tool, prompt, resource, or transport session is introduced.

### 5.1 `mutation_scope.set`

Conceptual request:

```json
{
  "action": "mutation_scope.set",
  "mutation_id": "scope-set-01",
  "scope_id": "auth-edit",
  "activity_id": "activity-auth-refactor",
  "workspace_id": "ws_...",
  "mode": "mutate",
  "paths": ["src/auth/**", "tests/auth/**"],
  "ttl_ms": 900000
}
```

`ttl_ms` is optional. Omitted TTL uses the v1 default of 900000 ms (15 minutes).

A successful response returns:

- the committed active scope record;
- whether this mutation created or replaced an active declaration;
- the stable mutation receipt identity/result;
- bounded current overlap advisories involving that scope;
- explicit truncation markers when advisory details hit limits.

The action never waits for another scope to disappear.

### 5.2 `mutation_scope.release`

Conceptual request:

```json
{
  "action": "mutation_scope.release",
  "mutation_id": "scope-release-01",
  "scope_id": "auth-edit"
}
```

Release is advisory-state cleanup, not an unlock operation. If the target is already absent or expired, a new valid release mutation succeeds with `already_absent=true` and records that result exactly once. This avoids caller choreography around inspect-before-release.

### 5.3 `inspect.mutation_scopes`

Conceptual request:

```json
{
  "action": "inspect.mutation_scopes",
  "workspace_id": "ws_...",
  "activity_id": "activity-auth-refactor"
}
```

`workspace_id` is required. `activity_id` is an optional filter. The response contains only currently active scopes, deterministic overlap advisories, explicit counts, limits/truncation, and safe diagnostics.

Inspection is read-only. It may lazily attempt cleanup of expired records, but cleanup failure does not make an expired scope active and does not fail an otherwise valid inspection unless storage safety itself cannot be established.

### 5.4 `inspect.activity`

When A2.6 is available, modern activity inspection SHALL include:

```text
active_mutation_scopes[]
mutation_scope_advisories[]
mutation_scopes_truncated
mutation_scope_advisories_truncated
```

This work is paid only because `inspect.activity` was explicitly requested. Legacy activity projections omit A2.6-only fields.

## 6. Scope identity and durable mutation identity

A2.6 deliberately separates logical scope identity from retry identity.

### 6.1 `scope_id`

`scope_id` names the logical declaration. It is a stable opaque caller-selected identifier bounded by the same safe local identifier conventions used elsewhere in ShellBeam: non-empty, no path separators/control/whitespace, maximum 128 bytes.

The first successful set binds a `scope_id` to one `activity_id` and one `workspace_id`. A later set using the same `scope_id` may replace mode, selectors, and TTL only when the activity/workspace binding is unchanged. Attempting to move a scope ID to another activity or workspace fails with a stable conflict instead of silently changing correlation.

That binding survives release and TTL expiry as a compact scope-identity tombstone and does not expire automatically in A2.6. Reusing the same `scope_id` for the same activity/workspace is allowed; rebinding it elsewhere requires a separate future explicit purge lifecycle rather than implicit expiry. This keeps old mutation retries unambiguous.

`mutation_scope.set` requires a syntactically valid `activity_id` but does not require an activity operation-history record to exist yet. Activity is correlation rather than ownership, and requiring a prior command merely to create that correlation would add protocol choreography. `workspace_id`, however, must resolve to an existing registered workspace through bounded local state; the set action does not run Git or inspect the workspace filesystem to establish that registration.

### 6.2 `mutation_id`

Every set or release carries a caller-stable `mutation_id`. It is the exactly-once identity for that state transition.

The durable mutation contract is:

- same `mutation_id` + same canonical request fingerprint => replay the original mutation result without reapplying the mutation;
- same `mutation_id` + different canonical request fingerprint => `mutation_metadata_conflict`;
- a retry of an older set mutation after a newer successful replacement returns the older receipt but MUST NOT roll back or overwrite the current scope;
- a retry of a set after a lost response MUST NOT recompute `expires_at` or extend TTL;
- a retry of release MUST NOT publish a second release transition or Event Journal change obligation.

A2.6 mutation receipts may compact to small tombstones containing the mutation ID, request fingerprint, result class, scope ID, committed time, and committed expiry where applicable. They do not expire automatically in A2.6. Explicit future purge remains a separate user-authorized lifecycle concern.

## 7. Canonical scope record

The v1 active record is conceptually:

```text
MutationScopeV1
  schema_version = 1
  scope_id
  activity_id
  workspace_id
  mode = read | mutate
  paths[]
  declared_at
  expires_at
  revision_id
```

`revision_id` is the `mutation_id` of the latest successful set that produced the active record. A numeric revision counter is intentionally unnecessary.

Canonicalization before persistence/fingerprinting:

1. validate all IDs;
2. validate and canonicalize selectors;
3. reject duplicate selectors after canonicalization;
4. sort selectors bytewise;
5. apply/default and validate TTL;
6. compute `declared_at` once at durable mutation preparation;
7. compute `expires_at = declared_at + effective_ttl` once;
8. persist the canonical record and mutation result under the existing crash-safe state-root rules.

No absolute filesystem path, raw command text, source content, environment value, credential, process ID, or conversation text belongs in this record.

## 8. Selector language v1

A2.6 intentionally does not implement arbitrary glob semantics.

A selector is one of exactly three forms:

```text
**              whole workspace
path/to/item    exact repository-relative path
path/to/dir/**  repository-relative subtree rooted at path/to/dir
```

### 8.1 Normalization and validation

Selectors SHALL:

- use `/` as the separator;
- be UTF-8 and at most 256 bytes;
- be repository-relative;
- reject leading `/`;
- reject backslash separators;
- reject NUL/control characters;
- reject empty path segments;
- reject `.` and `..` segments;
- reject `*`, `?`, `[`, `]`, `{`, `}` and other glob syntax except the exact whole-workspace token `**` or one terminal `/**` suffix;
- reject suffix text after `/**`;
- canonicalize no filesystem symlinks and perform no filesystem stat;
- be compared case-sensitively because workspace source identity is byte-oriented and ShellBeam must not invent filesystem-specific equivalence.

`path/to/dir/**` includes the subtree root `path/to/dir` itself plus descendants.

The selector is an advisory repository-relative declaration, not a proof that the path exists.

### 8.2 Deterministic overlap

Define each selector as a region:

- `**` covers every repository-relative path;
- exact `p` covers only `p`;
- subtree `p/**` covers `p` and every path beginning `p/`.

Two selectors overlap iff their regions intersect.

Therefore:

- `**` overlaps every selector;
- exact `a` overlaps exact `b` only when `a == b`;
- exact `a` overlaps subtree `b/**` when `a == b` or `a` begins `b/`;
- subtree `a/**` overlaps subtree `b/**` when `a == b`, `a` is an ancestor of `b`, or `b` is an ancestor of `a`.

No filesystem walk, Git query, AST query, LSP query, or provider call is permitted for overlap calculation.

## 9. Conflict/advisory semantics

Only active scopes in the same `workspace_id` participate in overlap advisories.

The v1 matrix is:

| Scope A | Scope B | Overlapping selectors | Result |
| --- | --- | --- | --- |
| read | read | yes | quiet |
| read | mutate | yes | advisory |
| mutate | read | yes | advisory |
| mutate | mutate | yes | advisory |
| any | any | no | quiet |

Activity IDs do not suppress overlap. An activity is correlation, not ownership; two concurrent callers may legitimately share one activity.

One unordered scope pair produces at most one advisory regardless of how many selector pairs overlap.

Conceptual advisory:

```text
MutationScopeAdvisoryV1
  code = mutation_scope_overlap
  workspace_id
  scope_ids[2]              sorted
  activity_ids[1..2]        bounded, deduplicated
  modes[2]
  conflict_kind             read_mutate | mutate_mutate
  overlap_examples[]        bounded selector-pair examples
  cause_fingerprint
```

`cause_fingerprint` is deterministic over the advisory code, workspace ID, the two current `revision_id` values, modes, and canonical selector sets. It contains no absolute paths and exists for caller-side/context deduplication, not authority.

Advisories are sorted deterministically by `(workspace_id, scope_id_a, scope_id_b)` before response truncation.

A repeated inspect of unchanged scope state therefore yields the same ordered advisories and cause fingerprints without persisting an advisory ledger.

## 10. TTL and expiration

A2.6 v1 uses:

- default TTL: 900000 ms (15 minutes);
- minimum explicit TTL: 1000 ms;
- maximum TTL: 1800000 ms (30 minutes).

TTL is part of the canonical set request fingerprint after defaulting.

Expiration semantics:

- a scope is active iff its record validates and `now < expires_at`;
- `now >= expires_at` means inactive immediately for all reads/conflict calculations;
- lazy cleanup may remove the expired active-record file after determining inactivity;
- cleanup failure is a diagnostic/storage-maintenance issue and does not resurrect the scope;
- daemon restart reloads only validated unexpired scopes as active;
- bridge restart has no special lifecycle effect;
- no background timer, watcher, goroutine, cron-style sweep, or polling loop exists solely for A2.6;
- expiration itself does not emit an Event Journal event because there is no timer-driven authoritative state transition.

Because scopes are advisory, persisted UTC timestamps are sufficient for v1. A record whose `expires_at <= declared_at` or whose declared duration exceeds the v1 maximum is invalid/corrupt and never treated as active.

## 11. Storage and crash semantics

A2.6 state is stored separately from:

- operation/session process-control records;
- activity operation-history records;
- evidence records;
- Event Journal materialized events.

Conceptually the state root contains dedicated versioned scope records and mutation receipts/tombstones. Exact on-disk naming remains an adapter detail, but all paths remain under the verified private ShellBeam state root and inherit existing no-symlink/ownership/mode/atomic-publication requirements.

### 11.1 Set ordering

A successful new set mutation follows:

```text
validate/default/canonicalize
        ↓
load prior mutation receipt if any
        ↓
load current active scope state + bounded capacity view
        ↓
prepare canonical replacement and mutation receipt
        ↓
commit scope mutation + durable observation obligation
        ↓
return committed result + derived advisories
```

If persistence is known to have failed before publication, no new scope is claimed.

If durability is ambiguous after a possible publication boundary, return the existing stable persistence-ambiguity category. Exact retry with the same `mutation_id` must reconcile/load before deciding and MUST NOT blindly apply a second lease.

### 11.2 Release ordering

Release uses the same mutation-receipt discipline. If a currently active record exists, successful publication removes/tombstones that active declaration and records the release result. If the scope is absent/expired, the durable result records `already_absent=true` without inventing an unlock effect.

### 11.3 Capacity

Capacity checks count only validated unexpired scopes. Expired records do not consume active-scope capacity even if lazy deletion has not succeeded yet.

Capacity failure is all-or-nothing: no scope replacement and no successful mutation result are published.

## 12. Fixed v1 limits

Capability discovery SHALL advertise at least these hard A2.6 limits:

```text
mutation_scope_schema_versions = [1]
mutation_scope_max_active_per_activity = 16
mutation_scope_max_active_per_workspace = 64
mutation_scope_max_paths_per_scope = 16
mutation_scope_max_selector_bytes = 256
mutation_scope_default_ttl_ms = 900000
mutation_scope_max_ttl_ms = 1800000
mutation_scope_inspect_scopes = 64
mutation_scope_inspect_advisories = 32
mutation_scope_advisory_overlap_examples = 4
```

The minimum explicit TTL of 1000 ms is also a closed validation rule even if not separately exposed as a tunable limit.

Implementations may use lower operator-configured storage ceilings only by reporting the effective hard limits truthfully through capability discovery. They must never silently accept more than the public hard contract and truncate authoritative mutation input.

## 13. Capability/version contract

`FeatureMutationScopes` remains the feature key.

Baseline catalogs continue to report it unavailable until a real A2.6 service/store is composed.

When available, the modern catalog SHALL expose:

- mutation-scope schema versions;
- effective active/count/path/selector/TTL/inspection/advisory limits;
- no claim of locking, enforcement, remote coordination, or automatic scope inference.

Legacy capability projections omit A2.6-only version/limit additions while preserving the pre-existing feature compatibility behavior.

Capability discovery must not probe the filesystem, Git, network, or providers to decide whether A2.6 is available. Availability is a composition/configuration fact.

## 14. Event Journal integration

A2.6 adds one closed E21 event kind:

```text
mutation_scope_changed
```

A newly committed set or release mutation creates exactly one durable observation obligation coupled to the same state-root visibility boundary as the authoritative scope mutation.

The event is intentionally summary-light:

```text
kind = mutation_scope_changed
subject_ref = scope_id
correlation = activity_id/workspace_id when available
summary = stable safe transition class: set | released
```

It does not embed selector lists, absolute paths, command text, or caller prose.

Rules:

- exact retry of a committed mutation does not allocate a second `change_seq` or event obligation;
- an older mutation retry after a newer scope revision does not rewrite current state and does not emit another event;
- a release that records `already_absent=true` creates its durable mutation receipt but no `mutation_scope_changed` obligation because active scope state did not change;
- lazy TTL expiration emits no event;
- Event Journal projection failure does not roll back a committed scope mutation; the existing observation-obligation recovery contract applies;
- the journal event is an accelerator telling consumers to inspect current state, not replay authority for reconstructing leases.

## 15. Activity and workspace inspection integration

A2.6 is workspace-scoped and activity-correlated.

`inspect.mutation_scopes(workspace_id, activity_id?)` is the direct bounded source for current active declarations.

Modern `inspect.activity(activity_id)` SHALL aggregate active scopes for the activity's currently known workspace references when A2.6 is available. It must remain bounded by A2.6 inspect limits and report truncation rather than silently omitting unknown amounts.

No activity inspection may infer that another activity is an agent owner. It reports declaration facts and overlap advisories only.

A2.6 does not require activity records to embed scope arrays. This prevents every TTL refresh from rewriting the activity history record and keeps operation-history compaction independent of short-lived advisory leases.

## 16. Outside-declared-scope observations

The earlier Agent Execution Layer design permits a post-execution advisory when observed changes lie outside a declared mutate scope. A2.6 preserves that possibility without adding hidden observation tax.

V1 rule:

- A2.6 SHALL expose a pure bounded evaluator that can compare an already-available repository-relative changed-path observation against a declared mutate scope.
- It MAY surface a `mutation_scope_observed_outside` advisory only when a suitable workspace delta/change-path observation already exists because that observation was explicitly requested or produced by an existing execution-observation path.
- It MUST NOT run `git status`, `git diff`, filesystem crawling, exact source hashing, or another workspace sample solely to produce this advisory.
- It MUST NOT claim that the declaring activity caused the outside-scope change. The wording is observational: the workspace had an observed changed path outside the declaration.
- absence of an outside-scope advisory never proves that all actual mutations stayed within scope.

This feature is therefore useful when facts are already available but remains outside ordinary admission/finalization tax.

## 17. Error and diagnostic contract

A2.6 uses typed stable failures consistent with the existing failure boundary. Required stable cases include:

```text
mutation_scope_invalid
mutation_scope_binding_conflict
mutation_metadata_conflict
mutation_scope_capacity_exceeded
persistence_unavailable
persistence_ambiguous
feature_unavailable
invalid_workspace
```

Validation details may identify safe field names/reasons but never echo arbitrary absolute paths, environment values, command text, credentials, source contents, or raw OS errors.

Release of an absent/expired scope is a successful exactly-once no-op result with `already_absent=true`, not a not-found failure.

Retryability describes the ShellBeam mutation action only. No A2.6 error means that rerunning an externally effectful shell command is safe.

## 18. Privacy and security

A2.6 is not a security boundary.

Persisted/public scope data is limited to caller-declared safe correlation IDs, repository-relative selectors, mode, and bounded timestamps/TTL metadata.

A2.6 SHALL NOT persist or expose:

- absolute workspace paths in scope/advisory cause fingerprints;
- raw command or argv values;
- stdin or output contents;
- source-file contents;
- raw environment values or hashes of environment values;
- credentials, tokens, keys, Git auth material, or arbitrary process environment;
- remote host information;
- hidden conversation/agent memory.

Repository-relative selectors are intentionally visible to other local ShellBeam consumers because coordination is the feature's purpose. They remain bounded and local-state private under existing state-root ownership/mode rules.

No scope increases the authority of a caller beyond the current local OS user authority ShellBeam already has.

## 19. Performance and no-tax requirements

### 19.1 Ordinary command path

With A2.6 compiled and available but unused, ordinary compatible `start -> poll -> terminal` SHALL perform:

- zero mutation-scope store reads;
- zero mutation-scope store writes;
- zero overlap calculations;
- zero scope TTL refreshes;
- zero scope Event Journal obligations;
- zero Git subprocesses for A2.6;
- zero filesystem walks for A2.6;
- zero provider/network calls for A2.6;
- zero extra synchronous durability barrier for A2.6.

An active scope elsewhere in the workspace MUST NOT change these rules. Scope existence never becomes a command-admission preflight.

### 19.2 Explicit set/release/inspect work

Explicit A2.6 actions may pay for bounded private-state reads/writes and in-memory selector comparisons only.

V1 uses straightforward bounded pairwise comparison rather than a new index/trie because limits are small and deterministic:

- at most 64 active scopes per workspace;
- at most 16 selectors per scope;
- at most 32 returned advisories;
- at most 4 overlap examples per advisory.

A more complex index is justified only by measured need and must not become a mandatory background subsystem.

## 20. Concurrency semantics

The local daemon remains the single writer for the state root. A2.6 must still be correct under concurrent client calls.

Required concurrency behavior:

- two concurrent new set mutations for different scope IDs may both succeed if final active capacity permits;
- capacity accounting is atomic across concurrent sets;
- concurrent set mutations for the same scope ID serialize to a deterministic latest committed record while preserving each mutation receipt;
- exact duplicate mutation IDs never apply twice;
- same mutation ID with conflicting request bytes never wins a race nondeterministically;
- release racing set is ordered by durable commit order; retry receipts replay their own original result without reordering committed state;
- inspection observes one valid committed cut or explicitly reports storage unavailability; it never fabricates a half-written scope;
- no scope operation obtains or stores process-signal authority.

## 21. Compatibility and migration

A2.6 adds new modern transport branches and capability fields without changing the semantics of existing `start`, `poll`, `write`, `kill`, or prior inspect actions.

Requirements:

- older clients can ignore the unavailable/unknown feature via existing version negotiation;
- modern closed schemas reject cross-action fields and unknown fields;
- legacy projections omit A2.6-only response fields;
- existing activity schema remains readable because scopes are stored separately;
- scope/mutation records are versioned and strict-decoded with unknown-field/trailing-data rejection;
- corrupt scope records are never treated as active;
- corrupt one-scope state must be isolated/surfaced according to existing private-state corruption policy rather than triggering destructive repair;
- no migration may reinterpret a previous operation/session PID as scope ownership or vice versa.

## 22. Proposed code boundaries

The implementation SHALL follow existing ShellBeam layering without creating a monolithic activity feature file.

Conceptual boundaries:

```text
internal/core/mutationscope
  types.go            canonical records / advisories / limits
  selector.go         normalization + pure overlap
  validation.go       record/request validation

internal/app/mutationscope
  service.go          set/release/inspect orchestration
  ports.go            narrow store + journal obligation ports
  evaluator.go        deterministic advisory fold

internal/adapter/store
  mutation_scopes.go  private durable scope + mutation receipt storage

internal/core/capability
  mutation_scopes.go  feature/version/limit composition

internal/app/daemon
  mutation_scope.go   action-facing composition port only

internal/adapter/ipc
internal/adapter/mcp
api/schema
cmd/shellbeam
  closed transport/schema/runtime wiring
```

Exact file splitting may follow repository structural gates, but responsibilities SHALL remain separated: selector semantics in core, mutation orchestration in app, durability in store, and transport mapping outside core/app.

## 23. Testing strategy

### 23.1 Core selector tests

Prove:

- exact/exact equality and disjointness;
- exact/subtree parent/child overlap in both argument orders;
- subtree/subtree equal/ancestor/disjoint cases;
- `**` overlaps every valid selector;
- `path/**` includes `path` itself;
- deterministic canonical sorting/dedup rejection;
- rejection of absolute, traversal, backslash, malformed wildcard, control, oversized, empty-segment, and invalid UTF-8 selectors;
- no filesystem access is needed for normalization/overlap.

### 23.2 Advisory tests

Table-test the complete mode matrix and prove:

- read/read is quiet;
- read/mutate and mutate/read advise;
- mutate/mutate advises;
- disjoint regions are quiet;
- one scope pair yields one cause-deduplicated advisory;
- overlap examples and total advisories truncate at exact limits;
- ordering and cause fingerprints are stable across input order.

### 23.3 Exactly-once mutation tests

Prove:

- lost set response + same mutation ID does not extend TTL;
- same mutation ID/different request conflicts;
- old set retry after newer replacement does not roll state back;
- concurrent duplicate set commits one effect;
- concurrent distinct updates serialize without corrupting receipts;
- release retry is one effect;
- absent/expired release is stable successful no-op;
- persistence-before-publication failure leaves no claimed active mutation;
- ambiguous publication forces reconcile-before-retry.

### 23.4 TTL/restart tests

With an injected clock, prove:

- just-before expiry active;
- at expiry inactive;
- expired state does not consume capacity;
- lazy cleanup failure does not resurrect scope;
- daemon/service reconstruction preserves valid unexpired scope state;
- no timer/background goroutine is necessary for expiration correctness.

### 23.5 Store/security tests

Prove:

- strict schema/unknown/trailing rejection;
- atomic replacement and concurrent writer behavior;
- private path ownership/mode/symlink protections reuse existing store guarantees;
- corrupt record isolation;
- compact mutation tombstone round trip;
- no absolute paths/commands/env/source contents appear in stored scope records or mutation receipts.

### 23.6 Event Journal tests

Prove:

- successful new set publishes one `mutation_scope_changed` obligation/event;
- successful release of an active scope publishes one;
- exact retries publish none additional;
- absent/expired release records one durable mutation result without publishing `mutation_scope_changed` or inventing an unlock;
- lazy expiry publishes none;
- journal materialization failure does not invalidate committed scope truth and is recoverable through existing obligation semantics.

### 23.7 IPC/MCP/schema tests

Prove:

- all three new branches are closed and reject cross-action/unknown fields;
- limits and ID/TTL/mode/selector bounds are represented accurately;
- structured output matches schema exactly;
- legacy generations omit/reject A2.6-only fields as required;
- `inspect.activity` modern projection carries bounded A2.6 fields;
- MCP still registers exactly one tool.

### 23.8 No-tax and non-blocking acceptance

On a real daemon with A2.6 available:

1. create overlapping mutate scopes and confirm an advisory is returned;
2. run ordinary source-mutating shell/Git commands anyway and prove they are admitted/executed normally;
3. run `git switch`, stash/reset-style representative operations in an isolated acceptance repository and prove scopes do not enforce or serialize them;
4. instrument scope store/evaluator and prove ordinary start/poll with no A2.6 action performs zero scope reads/writes/evaluations;
5. prove no Git/process/provider subprocess is spawned by set/release/inspect itself;
6. restart daemon and prove only unexpired durable declarations remain active;
7. verify one-tool MCP surface and capability limits.

## 24. Completion criteria

A2.6 is complete only when fresh verification on the exact final source tree proves all of the following:

1. `FeatureMutationScopes` is available only when the real service/store is composed.
2. Scope schema v1 and all effective hard limits are discoverable without trial calls.
3. `scope_id` is durably bound to one activity/workspace across active, released, and expired states while a new mutation may replace mode/paths/TTL without moving that binding.
4. `mutation_id` retries are exactly once; lost responses never extend TTL.
5. An older mutation retry cannot roll back a newer scope revision.
6. Selector v1 supports only `**`, exact repo-relative paths, and terminal `/**` subtrees.
7. Selector overlap is deterministic and needs no filesystem/Git/provider work.
8. Read/read remains quiet; read/mutate and mutate/mutate overlap produce bounded advisories.
9. Advisories never block or delay command/Git/worktree behavior.
10. Active scopes survive daemon restart only as durable advisory declarations, never process authority.
11. Expiration is lazy, watcher-free, and inactive truth is independent of cleanup success.
12. Capacity counts only unexpired scopes and is race-safe.
13. Release of absent/expired state is an exactly-once successful no-op.
14. Scope state remains separate from activity operation history and operation/session control records.
15. Modern direct/activity inspection returns bounded active scopes/advisories with explicit truncation.
16. `mutation_scope_changed` integrates with E21 exactly once for committed active-state changes; exact retries, absent releases, and TTL expiry create no duplicate or timer event.
17. Outside-scope observation can reuse already-available changed-path facts but never triggers hidden Git/workspace sampling or overclaims causation.
18. Storage/transport output excludes absolute paths, command/stdin/output/source/env/credential data not allowed by this design.
19. A2.6 adds no locks, permissions, scheduling, command inference, provider indexing, remote coordination, watcher, or second MCP tool.
20. Ordinary compatible start/poll/terminal pays zero mutation-scope lookup/evaluation/durability tax even while unrelated scopes are active.
21. Core/store/app/IPC/MCP/daemon tests, relevant race suites, schema gates, privacy scans, `devctl check`, dirty/global selection, staged diff checks, commit gate, and final exact source-fingerprint proof all pass as required by the implementation plan.

## 25. Boundaries after A2.6

A2.6 intentionally leaves these for separate designs:

- persistent named-session ownership/supervision beyond existing runtime contracts (B1);
- semantic provider/index integration keyed by exact source digest (remaining B2 provider work);
- evidence invalidation optimization based on measured provider usage;
- remote/distributed mutation coordination;
- filesystem or Git locking;
- automatic scope inference from commands, diffs, AST/LSP, or agent plans;
- semantic dependency tracing;
- automatic evidence reruns;
- containers/hermetic workspaces;
- daemon-side planning/reasoning.

A2.6's job is narrower: give reasoning agents cheap, durable, TTL-bounded declarations and deterministic overlap warnings while preserving the user's ability to keep working without protocol friction.
