# ShellBeam Agent Execution Layer Design

**Status:** Approved target design; A0/A1 implementation planned
**Date:** 2026-08-13
**Scope:** Phase A foundation for agent-oriented execution, followed by incremental Phase B capabilities
**Depends on:** [ShellBeam V1 Design](./2026-08-13-shellbeam-v1-design.md)
**Companion designs:** [Workspace, Worktree, and Git Identity](./2026-08-13-workspace-worktree-git-identity-design.md) and [Project Capability Manifest and Agent Onboarding](./2026-08-13-project-capability-onboarding-design.md)
**Implementation plan:** [Agent Execution Layer A0/A1](../plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md)

## 1. Decision

ShellBeam evolves from a durable local shell bridge into a thin, deterministic Agent Execution Layer.

It continues to expose exactly one MCP tool, `local_shell`, and retains the V1 authority model: commands run as the local OS user with the user's authority. The layer adds structured context, continuity, evidence, and efficient output access around command execution. It does not become another reasoning agent.

The target is the middle architecture:

- richer than a generic shell with extra metadata;
- much smaller and more deterministic than a coding orchestrator;
- useful to ChatGPT, Codex, and other agents without owning their plans.

The optimization goal is:

> Help the agent run fewer commands, recover with fewer guesses, and reuse valid evidence.

All twenty accepted enhancements are in this target design. They are delivered in dependency-ordered slices rather than one protocol jump.

## 2. Architectural position

```text
                    ChatGPT / Codex / agent
                              |
                         local_shell
                              |
          +-------------------+-------------------+
          |                   |                   |
      Execution           Context             Evidence
          |                   |                   |
    argv / shell          workspace          exact source
    stateless starts      activity           command result
    named sessions        Git state          artifacts
    process groups        dirty baseline     freshness
    durable receipts      identity           environment
          |                   |                   |
          +-------------------+-------------------+
                              |
                         local daemon
```

There is no LLM, semantic summarizer, hidden policy engine, or shell-intent parser in the daemon.

## 3. Goals

- Make every command's execution, child outcome, source context, and retained output mechanically understandable.
- Carry task continuity across conversations without conversation ownership or workspace locks.
- Make worktrees and relative paths first-class while preserving absolute-path escape hatches.
- Distinguish inherited dirty state from changes observed after an activity baseline without claiming file ownership.
- Reuse current test, build, and artifact evidence instead of rerunning broad verification.
- Support long-running and interactive processes through explicit application state handles.
- Reduce model tokens consumed by noisy terminal output.
- Surface Git identity and concurrent-mutation risk as actionable, non-blocking advisories.
- Preserve compatibility, bounded latency, local privacy, and exactness labels.

## 4. Non-goals

ShellBeam will not:

- plan coding work, choose a fix, summarize code semantically, or decide whether evidence is sufficient for a user's goal;
- own a task, conversation, branch, workspace, or file;
- block ordinary Git operations, including switch, stash, reset, rebase, clean, or prune;
- infer command intent by parsing arbitrary shell strings;
- automatically rerun an ambiguous command or stale verification;
- turn mutation scopes into locks or permissions;
- make every command stateful;
- treat repository manifests as trusted authority;
- perform network, SSH, credential-helper, or GitHub CLI probes in the ordinary execution hot path;
- replace semantic code-search providers or embed a vector database in the core daemon;
- claim that an observed file delta was caused by one activity when concurrent actors are possible.

## 5. Contracts retained from V1

This design extends rather than replaces the V1 correctness contract.

The following remain normative:

- `operation_id` is the durable idempotency key for a start.
- A lost response is retried with the same ID; a new ID must not be invented for an ambiguous result.
- `session_id` and output cursors are explicit application handles.
- Start, write, and kill retry effects are deduplicated.
- Process groups, drain/reap ordering, output quotas, and durable terminal receipts remain authoritative.
- MCP/tool success is not child success.
- A non-zero exit, timeout, kill, failed spawn, or ambiguity is a typed terminal receipt, not a fabricated transport failure.
- After ownership is lost, ShellBeam never signals a PID or PGID reconstructed only from disk.
- Closed JSON Schema and real-payload conformance tests protect every input and output branch.
- Unknown exactness is reported as unavailable; truncated or cached data is never labeled exact.

New capabilities must preserve these invariants under retries, daemon restart, schema negotiation, and partial feature availability.

## 6. Identity and correlation model

### 6.1 Distinct identities

ShellBeam uses separate IDs for separate jobs:

| Identity | Meaning | Ownership semantics |
| --- | --- | --- |
| `repository_id` | Stable local Git repository identity | No ownership |
| `workspace_id` | Reusable registered directory/worktree | No ownership |
| `activity_id` | Caller-declared task/correlation identity | No ownership |
| `operation_id` | Exactly-once start intent | Idempotency only |
| `session_id` | One admitted process/session generation | Process handle |
| `session_name` | Optional human-stable alias for a persistent session | No implicit process creation |
| `evidence_id` | Immutable verification/artifact record | Historical fact |

Conversations are not persisted as an authority concept.

### 6.2 Activity identity

`activity_id` is an optional bounded caller-generated ID, for example `ZMR-111-validator`. The first accepted operation lazily creates the local activity record. No separate create call is required.

An activity may span:

- multiple conversations;
- multiple agents;
- multiple operations and sessions;
- multiple workspaces when the task genuinely crosses repositories.

A workspace may host multiple activities concurrently. ShellBeam correlates facts; it does not allocate exclusive access.

Activity records contain bounded references to workspaces, operations, baselines, evidence, live sessions, mutation declarations, and active advisories. Old operation detail may be compacted according to retention, while immutable receipt/evidence identities remain honest.

### 6.3 Explicit state and modern MCP

ShellBeam session and activity IDs are application-level explicit state handles, not hidden MCP transport sessions. Every stateful call carries its handle. This remains compatible with stateless MCP transports and allows a bridge request to land on any bridge instance that can route to the same local daemon.

## 7. Capability discovery and version negotiation

An agent must be able to learn supported features and limits before relying on them.

ShellBeam advertises at least:

```text
shellbeam_protocol_version
receipt_schema_versions
project_manifest_schema_versions
features:
  workspace_addressing
  workspace_provenance
  activities
  argv_mode
  output_views
  named_sessions
  process_inspection
  evidence_ledger
  expected_outputs
  environment_fingerprint
  mutation_scopes
  project_manifest
limits:
  command_bytes
  response_bytes
  session_output_bytes
  runtime_ms
  live_sessions
  activity_history
```

For MCP `2026-07-28`, ShellBeam uses standard server discovery and negotiated extension mechanisms where supported. For legacy hosts that still initialize a connection, equivalent bounded data is returned through the legacy capability path. A closed, non-spawning `local_shell` inspect branch provides a protocol-independent fallback.

The tool catalog remains stable and cacheable: one tool with a versioned closed union. ShellBeam does not make the agent learn features by submitting invalid trial commands.

Capability absence is explicit. An older daemon may still execute a basic command while returning `feature_unavailable` for unsupported observation fields; the bridge must not invent them.

## 8. One-tool action surface

The target `local_shell` union contains these action families:

- `start`: admit a stateless command or explicit persistent session;
- `poll`: wait for new output or terminal state;
- `write`: deduplicated stdin/PTY input;
- `kill`: deduplicated process-group signal;
- `read_output`: retrieve retained output through bounded selectors without rerunning;
- `inspect`: read server, workspace, activity, session, project, or evidence facts without spawning a child.

These are branches of one closed tool, not separate repository tools. Operational CLI equivalents may exist for humans and compatibility, but agents should not have to spawn `shellbeam` recursively for facts the active bridge already owns.

All union branches use JSON Schema 2020-12 correctly. A root union is closed with semantics such as `unevaluatedProperties: false`; it is not closed by placing `additionalProperties: false` at a root that declares no root properties. Contract tests validate realistic payloads through the production schema library.

## 9. Start input contract

### 9.1 Workspace-addressed cwd

A start uses exactly one address form:

```json
{
  "workspace_id": "ws_01K2...",
  "cwd": "src/utils"
}
```

or:

```json
{
  "cwd": "/absolute/path/to/repo/src/utils"
}
```

With `workspace_id`, `cwd` is relative and defaults to `.`. Resolution is anchored beneath the registered workspace root. `..`, NULs, and symlink traversal that escapes the root are rejected as an invalid address. A caller that intentionally needs another location uses an absolute cwd without workspace addressing.

This is address integrity, not Git policing. It does not prevent switching branches or using another worktree.

The optional `workspace_hint` from the workspace design remains a separate soft expectation. An explicit address chooses where to run; a hint describes expected context and can produce a non-blocking mismatch advisory.

On first admission, ShellBeam persists both the logical address and resolved absolute cwd. A retry replays the bound operation even if the worktree was subsequently moved.

### 9.2 Execution modes

Exactly one execution form is accepted:

```json
{ "argv": ["git", "status", "--porcelain=v2", "-z"] }
```

or the compatible shell form:

```json
{ "command": "make test-dirty && make build" }
```

`argv` directly executes the selected program and is preferred for ordinary tool invocation. It avoids quoting, interpolation, globbing, pipeline, and shell-option ambiguity.

`command` retains V1 semantics through the configured shell for pipelines, redirection, compound expressions, and interactive shell behavior. The receipt records `execution_mode`, effective executable or shell, and a secret-safe execution fingerprint.

ShellBeam never rewrites a shell string into argv or joins argv into a shell string.

### 9.3 Declared intent

The caller may attach a closed intent object:

```json
{
  "intent": {
    "kind": "test",
    "mutates_source": false,
    "external_effect": false
  }
}
```

`kind` is one of `inspect`, `format`, `edit`, `test`, `build`, `generate`, `git-push`, `release`, `long-running`, or `other`.

Intent is declared metadata, never a safety fact inferred from command text. It may select provenance depth, identity preflight, expected-output checks, activity baselines, and evidence classification. It never grants permission and never makes a command safe.

An omitted field means unknown, not false. The universal tool retains conservative MCP annotations.

### 9.4 Observation declarations

A start may also carry bounded, optional observation metadata:

- `activity_id`;
- `project_command_id` from a validated manifest;
- `expected_outputs`;
- an activity mutation-scope refresh;
- `session_name` and explicit persistence intent;
- output-view preferences;
- relevant evidence/profile identifiers.

None causes hidden command execution. Expected outputs and source deltas are observed after the child terminal state; they do not transform a successful child exit into a runtime/tool failure. They do affect whether a verification evidence record is complete.

### 9.5 Idempotency and binding

ShellBeam separates three durable hashes:

- `request_fingerprint`: caller-stable execution intent such as mode, exact command/argv, logical workspace-relative address or exact absolute cwd, TTY, timeout, and any future explicit environment overlay;
- `execution_fingerprint`: the first admission's resolved absolute cwd, effective shell/executable, and other execution facts actually bound to the child;
- `observation_binding_fingerprint`: activity, project command, declared intent, expected outputs, and mutation metadata bound to the first accepted operation.

Response controls such as yield, output selector, and response byte limit belong to neither fingerprint.

The same `operation_id` with a different request fingerprint returns `operation_conflict` and never spawns again. A retry with the same logical workspace address replays the first bound execution even if that worktree was subsequently moved; ShellBeam does not re-resolve the path before replay. A retry with conflicting bound observation metadata returns `operation_metadata_conflict`, identifies the original bounded metadata, and never reassigns the existing operation. Soft response-only hints may vary without changing any fingerprint.

## 10. Structured execution result

Every successful start/poll response uses a stable envelope that separates tool operation state from child outcome:

```json
{
  "schema_version": 2,
  "ok": true,
  "action": "poll",
  "operation": {
    "operation_id": "op_...",
    "activity_id": "ZMR-111-validator",
    "workspace_id": "ws_...",
    "session_id": "ses_...",
    "state": "terminal"
  },
  "child": {
    "state": "exited",
    "outcome": "failure",
    "exit_code": 1,
    "signal": null,
    "timed_out": false,
    "duration_ms": 1824
  },
  "output": {
    "canonical_stream": "combined",
    "preview": "...",
    "raw_bytes": 8421,
    "returned_bytes": 4096,
    "truncated": true,
    "binary": false,
    "next_cursor": 8421,
    "output_ref": "out_..."
  },
  "source": {
    "pre_generation": "gen_...",
    "post_generation": "gen_...",
    "observed_change": false,
    "quality": "fresh"
  },
  "context_delta": [],
  "change_summary": null,
  "artifacts": [],
  "evidence": [],
  "advisories": [],
  "receipt": {}
}
```

The exact schema remains a closed discriminated union. Optional objects are absent when unsupported or not requested; they are not filled with fabricated null facts.

`operation.state=terminal` means ShellBeam durably finalized the operation. `child.outcome=failure` and `exit_code=1` mean the command did not succeed. Tool transport success can never erase that distinction.

### 10.1 Output channels

The V1 append-only raw combined byte stream remains canonical for cursor and replay semantics.

- PTY output is inherently one combined stream.
- Pipe-mode implementations may additionally retain source-channel tags and expose stdout/stderr counters or previews.
- If channel tags are unavailable, ShellBeam returns `channel_views=unavailable`; it does not guess.
- Observed read order is not presented as a stronger total ordering guarantee than the OS capture provides.

## 11. Output virtualization

### 11.1 Raw and rendered views

ShellBeam retains the quota-bounded raw canonical bytes under the existing local output policy. Model-facing output defaults to a deterministic rendered view:

- ANSI control sequences stripped;
- invalid UTF-8 replaced only in the rendered view;
- repetitive carriage-return/progress frames collapsed mechanically;
- binary output detected and summarized by byte counts and type hints;
- first and last useful chunks preferred over an arbitrary prefix;
- explicit truncation markers and raw byte offsets retained.

Raw bytes remain accessible through bounded output reads while retention permits. Rendering never changes cursor accounting or receipt evidence.

### 11.2 Read selectors

`read_output` accepts exactly one bounded selector:

- raw byte range;
- tail by lines or bytes;
- line range;
- first-plus-last preview;
- literal search;
- RE2-compatible regular-expression search with match and work limits.

Selectors operate on retained output and never rerun the command. Results report raw byte ranges, rendered line ranges where available, match truncation, and retention quality.

If output was compacted, the immutable receipt remains and `output_unavailable` is explicit.

## 12. Workspace and Git context

The approved workspace/worktree design remains authoritative for stable IDs, fast generations, exact snapshots, soft affinity, advisories, naming, worktree reuse, removal confirmation, and multi-account Git safety.

### 12.1 Transient Git state

Fast workspace snapshots add bounded local facts for:

- merge in progress;
- rebase in progress;
- cherry-pick in progress;
- revert in progress;
- bisect in progress;
- unresolved index stages and conflict count;
- detached HEAD;
- ahead/behind relative to the already configured local upstream.

Ahead/behind never fetches. Missing or stale upstream refs are labeled with observation quality. Transient-state detection uses Git-native facts and never blocks execution.

### 12.2 Pre-existing dirty baseline

For an activity, ShellBeam captures a bounded Git-aware baseline before its first admitted operation in a workspace, or through an explicit inspect/start-activity request. The baseline records modified, deleted, renamed, unmerged, and untracked path facts with quality.

Later snapshots separate:

- `inherited_dirty`: paths already dirty at the observed baseline;
- `observed_since_baseline`: paths whose observed state differs from the baseline;
- `resolved_since_baseline`: inherited dirty paths no longer observed dirty;
- `baseline_diverged`: branch/reset/rebase or unavailable evidence prevents a meaningful path comparison.

The design deliberately avoids `activity_introduced_dirty`. ShellBeam observes time-ordered facts but cannot prove causation when the user, another agent, hooks, generators, or background tools may edit the same workspace.

### 12.3 Mechanical change summary

After a declared mutating operation, ShellBeam may return a bounded mechanical summary:

```text
modified=3 added=2 deleted=0 renamed=1 untracked=1
paths=[...]
paths_truncated=false
quality=fresh
```

It is based on pre/post Git-aware observations. It is not a semantic diff and not proof that the child caused every change. Detailed review remains an explicit Git diff.

## 13. Soft mutation coordination

An activity may advertise short-lived repository-relative mutation scopes:

```json
{
  "mode": "mutate",
  "paths": ["src/auth/**", "tests/auth/**"],
  "ttl_ms": 900000
}
```

Scopes are advisory declarations:

- `read` scopes do not conflict with reads;
- disjoint mutate scopes remain quiet;
- overlapping active mutate scopes in the same workspace produce one cause-deduplicated advisory;
- actual observed changes outside a declared scope may produce a post-execution advisory;
- expired scopes disappear automatically;
- no scope prevents execution, editing, branch changes, or worktree removal.

Glob overlap uses a documented deterministic matcher and bounded pattern count. ShellBeam does not parse commands to infer undeclared scopes and never calls the mechanism a lock.

## 14. Evidence ledger

### 14.1 Evidence record

Test, build, format-check, typecheck, generation, and artifact observations may produce immutable evidence records:

```text
evidence_id
activity_id
workspace_id
verification_kind
project_command_id or command_fingerprint
receipt_id
result
source_content_digest
vcs_state_digest
input_scope_digest and selector fingerprint when narrower than the full source view
manifest_digest
environment_fingerprint
toolchain_fingerprint
artifact_observations
completed_at
```

`result=pass` is mechanical: the declared verification command reached an authoritative successful terminal receipt and all required artifact/evidence conditions were satisfied. ShellBeam does not judge whether the repository's chosen test command is semantically comprehensive.

### 14.2 Validity dimensions

Evidence exposes independent dimensions rather than one vague “fresh” flag:

- `result`: `pass`, `fail`, `incomplete`, or `ambiguous`;
- `source_match`: `exact`, `fast`, `mismatch`, or `unknown`;
- `freshness`: `current`, `stale`, or `unknown`;
- `artifact_match`: `current`, `changed`, `missing`, or `not_required`;
- `policy_match`: `current`, `changed`, or `unknown`.

“Never run” is an absence state returned by inspection, not a fabricated evidence record.

An exact-current claim requires matching:

- exact source content digest and relevant VCS context;
- canonical command fingerprint;
- manifest and verification-policy digest when used;
- bounded environment/toolchain fingerprint;
- expected artifact facts;
- implementation/schema semantics that affect interpretation.

The conservative default input scope is the complete effective source namespace. Narrower evidence remains current across an unrelated source change only when a versioned affected-input selector can prove the exact command input scope and its own configuration fingerprint is unchanged. Without that proof, any source-content change makes the evidence stale; ShellBeam never guesses irrelevance from path names alone.

If any required dimension is unavailable, ShellBeam reports that fact. The reasoning agent decides whether fast or partial evidence is sufficient for the user's task.

### 14.3 Efficient verification

Evidence inspection lets an agent prefer:

1. current exact evidence already available;
2. affected tests and incremental builds for changed scopes;
3. broader checkpoint verification when required;
4. full clean test/build only at an explicit gate or when global triggers invalidate narrower evidence.

ShellBeam never automatically reruns stale evidence. It explains why the evidence is stale so the agent can choose the smallest useful next command.

## 15. Project capability manifest and onboarding

The optional `.shellbeam/project.toml` contract is defined in the companion [Project Capability Manifest and Agent Onboarding Design](./2026-08-13-project-capability-onboarding-design.md).

The important runtime rules are:

- a newly attached agent inspects manifest status automatically;
- when absent or review-due, the agent audits high-signal repository automation and creates/updates a minimal file when useful, supported by evidence, and authorized;
- the manifest contains machine-readable capabilities, never copied prompt prose;
- reading or validating the file executes nothing;
- absent, invalid, or review-due status never blocks coding;
- command and verification evidence is bound to the exact manifest digest;
- equivalent conversations/worktrees reuse current onboarding results.

ShellBeam provides status, validation, fingerprints, and exposure. The agent performs discovery and judgment.

## 16. Environment fingerprint

Receipts may include a bounded, versioned environment fingerprint sufficient for reproducibility comparison without dumping the environment:

- OS and architecture;
- effective shell or direct-exec mode;
- hash and entry count for effective `PATH`, not the raw path list by default;
- selected Go, Node, Python, Java, Rust, or repository-declared toolchain versions;
- relevant environment-variable presence bitmap;
- local toolchain-manager identity when declared;
- capture quality and timestamp.

Tool version probes are cached, lazy, locally executed, and outside the warm command-admission critical path. Probe failure never blocks a command. A version is omitted or marked unavailable rather than parsed from arbitrary output heuristically.

Secret values are never stored. Hashing an unknown secret value is also disallowed because low-entropy values may be recoverable. Repository manifests may select variable names for presence comparison but cannot opt their values into receipts.

The fingerprint version declares normalization rules so two records are compared only under compatible semantics.

## 17. Declared outputs and artifacts

A caller or validated project command may declare repository-relative expected outputs. Terminal observation records, within explicit budgets:

- normalized path and kind;
- existence;
- size where meaningful;
- modification time as context, not content identity;
- complete SHA-256 for a file when requested;
- complete deterministic tree digest for a directory when requested and affordable;
- symlink link text without following an escaping target;
- quality and observation time.

An exact digest is streamed to completion or marked unavailable. A missing required artifact does not rewrite the child's exit code, but the associated verification evidence becomes `fail`; inability to complete the required observation makes it `incomplete`. A missing optional artifact remains an explicit observation without failing the evidence record.

Artifact observations remove routine `ls`, `stat`, and `sha256sum` follow-up calls while keeping the raw command result honest.

## 18. Default-stateless and explicit persistent sessions

### 18.1 Default behavior

Every ordinary start remains self-contained:

- cwd is explicit;
- execution mode is explicit;
- stdin behavior is explicit;
- no previous shell's `cd`, exports, aliases, or job table are inherited;
- completion ends the child session.

ShellBeam does not hide a long-lived shell behind successive `local_shell` calls.

### 18.2 Named persistent sessions

Long-running servers, watch mode, REPLs, debuggers, and database shells may use an explicit `session_name`. The name is scoped to activity and workspace when those are present.

A retry with the original `operation_id` replays the existing named session. A different operation targeting a live name returns `session_name_conflict` with the existing session handle and never aliases, kills, or replaces the live process, even when the launch fingerprint matches. Reuse after a terminal generation creates a new `session_id` under the name's bounded history and still requires a new `operation_id`.

Persistent means discoverable and intentionally retained; it does not mean every process is a shell. Stateful shell behavior exists only when the explicitly launched child is itself a shell or REPL.

### 18.3 Daemon restart

Bridge/tunnel disconnect continuity remains available through explicit handles. Daemon-restart continuity requires the per-session supervisor architecture from the V1.1 roadmap:

- supervisor owns the child process group, I/O, ledger, and final evidence;
- daemon reattaches only through a high-entropy capability handshake;
- absence or failed proof produces `abandoned/ambiguous`;
- no stale PID/PGID is trusted.

Non-TTY supervisor ownership should land before the more complex PTY reattachment path. Named sessions may be useful before full restart survival, but capability discovery must state the actual durability level.

## 19. Process introspection

Session inspection returns bounded local facts:

- session and optional name;
- root process ID when currently owned;
- process-group identity;
- owned child-process observations;
- running, stopped, finalizing, or terminal state;
- start time and uptime;
- termination cause and exit evidence;
- recent rendered output reference;
- supervisor/restart-survival quality.

Listening ports associated with the owned process group are an optional, platform-specific capability. Port observation is best-effort, bounded, and never in the spawn hot path. Unsupported platforms return `ports=unavailable` rather than using `ps | grep` or claiming ownership from names.

Inspection never expands the set of processes ShellBeam may signal.

## 20. Typed failures and retryability

The existing closed error model is extended rather than replaced.

Three categories remain distinct:

1. Tool/contract failure: invalid workspace, invalid request, protocol mismatch, capacity, persistence, or unavailable feature. Returned as `ok=false` with stable code, hint, and `retryable`.
2. Terminal execution failure: direct-exec spawn failure, timeout, output limit, input delivery failure, or lost ownership. Returned as `ok=true` with a typed receipt failure reason.
3. Child program failure: a started child exits non-zero or by signal. Returned as `ok=true`, authoritative child outcome, and exit evidence.

Stable additions include:

- `invalid_workspace`
- `workspace_path_escape`
- `activity_not_found`
- `operation_metadata_conflict`
- `session_name_conflict`
- `feature_unavailable`
- `output_selector_invalid`
- `evidence_not_found`
- `project_manifest_invalid`
- `expected_output_invalid`
- `observation_budget_exceeded`

Direct argv execution can normalize spawn errors such as executable not found or permission denied. In shell mode, a shell-reported “command not found” may only be child exit 127 plus shell output; ShellBeam must not pretend it observed a direct-exec spawn error.

Retryability describes the failed ShellBeam action, not whether repeating an externally effectful command is safe. Ambiguous execution is never automatically retryable with a new operation ID.

## 21. Activity handoff and inspection

`local_shell.inspect` with `target=activity` returns bounded structured facts:

- activity ID and display label;
- associated workspaces and current branch/HEAD facts;
- inherited dirty baseline and observed-since-baseline delta;
- transient Git state;
- latest operations and terminal outcomes;
- current/stale/never-run evidence summary;
- named/live sessions and recent output references;
- expected artifact observations;
- active mutation scopes;
- current context events and advisories;
- project manifest status and digest;
- exactness, cache age, truncation, and unavailable markers.

The response contains facts only. ShellBeam does not write a narrative handoff, decide next steps, or preserve hidden conversation memory. A new reasoning agent reconstructs the task from these explicit facts plus repository/user context.

## 22. Data model, retention, and privacy

Private local state adds versioned records similar to:

```text
activities/<activity-id>/index.json
workspaces/<workspace-id>/...
evidence/<evidence-id>.json
projects/<repository-id>/discovery.json
sessions/<session-id>/...
```

State writers use the existing atomic publication and durability rules. Immutable evidence and terminal receipts are content-addressed or tombstoned so retention cannot make an old operation rerunnable.

Retention is tiered:

- live/finalizing process control state is never selected;
- raw output may compact before receipt/evidence summaries;
- activity operation lists may compact to bounded summaries;
- exact source digests and manifest/environment fingerprints remain while referenced evidence remains;
- explicit purge remains user-authorized and reports the loss of idempotency/evidence history.

Receipts and diagnostics exclude source contents, raw environment values, tokens, private keys, and raw Git credentials. Absolute paths are returned only when needed for local operation/debugging and are not placed into cause fingerprints.

## 23. Performance and concurrency

Agent assistance must not slow every shell command.

The warm admission path retains the workspace design budget:

- zero network, SSH, `gh`, credential-helper, and arbitrary tool probes;
- zero subprocesses with valid fast caches;
- bounded local Git work only when cold/stale;
- process spawn proceeds with cached or unavailable optional context after budget exhaustion.

Exact source snapshots, artifact hashes, toolchain probes, evidence evaluation, and index updates are requested work, background work, or terminal finalization work with explicit budgets. They do not masquerade as free hot-path metadata.

Concurrency rules:

- one daemon writer coordinates a local state root;
- operation and input/kill ledgers remain atomic;
- output, hashing, Git observation, evidence evaluation, and indexing use separate bounded worker pools;
- expensive exact snapshots deduplicate by source-view key;
- mutation scopes advise but never serialize user work;
- a stateless MCP bridge can scale independently because all application state handles are explicit.

Multi-machine routing, hosted gateways, and distributed workspace state remain separate future systems; they are not smuggled into local activity identity.

## 24. Compatibility and migration

Each capability has explicit protocol, IPC, receipt, and local-state versions.

- Old clients may continue using the four V1 action branches.
- New optional input fields are accepted only after bridge/daemon capability negotiation.
- Unsupported observation data is omitted with a capability/status marker, never guessed.
- Terminal V1 receipts remain readable and immutable.
- Derived indexes and environment/toolchain caches may rebuild; idempotency tombstones and authoritative receipts may not be discarded during migration.
- Schema migrations are crash-safe, resumable, and covered by fixture tests.
- Tool list caching changes only when the actual closed schema/version changes.

MCP protocol revision support is an adapter boundary. The local daemon semantics do not depend on transport-level sessions or a particular host's initialization behavior.

## 25. Enhancement coverage and verdicts

| ID | Accepted enhancement | Verdict |
| --- | --- | --- |
| E01 | Activity identity | Add as independent correlation; never ownership. |
| E02 | Workspace-addressed cwd | Add relative addressing plus absolute-path fallback. |
| E03 | Declared intent envelope | Add as untrusted metadata; never parse shell. |
| E04 | Structured LLM result | Extend V1 envelope; separate operation and child outcome. |
| E05 | Output virtualization | Extend retained raw output with bounded deterministic views. |
| E06 | Explicit persistent sessions | Extend durable sessions with names and explicit state only. |
| E07 | Argv and shell modes | Add direct argv; preserve compatible shell command mode. |
| E08 | Git transient state | Extend fast workspace snapshot; local-only, no fetch. |
| E09 | Dirty baseline | Add inherited versus observed-since-baseline; no causal claim. |
| E10 | Soft mutation coordination | Add TTL advisory scopes; no locks or permission semantics. |
| E11 | Evidence ledger | Add immutable evidence and multidimensional validity. |
| E12 | Project capability manifest | Add optional manifest plus automatic agent onboarding companion design. |
| E13 | Capability discovery | Add negotiated feature/limit discovery with legacy fallback. |
| E14 | Environment fingerprint | Add bounded, lazy, secret-safe fingerprint. |
| E15 | Declared artifacts | Add terminal observation bound to evidence. |
| E16 | Mechanical source summary | Extend pre/post provenance; bounded and non-semantic. |
| E17 | Typed errors/retryability | Extend existing typed contract; preserve category boundaries. |
| E18 | Process-group introspection | Extend session inspection; ports optional and best-effort. |
| E19 | Token-conscious output | Add deterministic rendered view; retain raw cursor truth. |
| E20 | Activity handoff | Add structured inspect facts; no AI-written memory. |

Nothing in the accepted list requires a second MCP tool or reasoning inside the daemon.

## 26. Delivery roadmap

All capabilities are target scope, but implementation is staged to make Phase A strengthen later Phase B work.

### A0: correctness and compatibility foundation

- preserve all V1 idempotency, durability, output, and process-lifecycle gates;
- protocol/IPC/receipt version framework;
- MCP `2026-07-28` adapter plus supported legacy adapter;
- capability and limit discovery;
- production-schema validation with realistic payload regression tests;
- stable structured envelope and extended typed failures.

### A1: execution ergonomics and onboarding

- activity ID and lazy activity record;
- workspace-addressed cwd;
- direct argv mode;
- declared intent and observation binding;
- Git transient-state snapshot;
- project manifest M0/M1 and automatic agent onboarding;
- activity/workspace inspect foundation.

This slice makes implementation of later features faster because the agent can address workspaces reliably, invoke canonical cheap commands, and understand the active capability set.

### A2: source awareness and reduced command count

- pre-existing dirty baseline;
- pre/post mechanical change summary;
- output rendered defaults and `read_output` selectors;
- expected-output observation;
- environment/toolchain fingerprint;
- exact evidence ledger integrated with affected tests and incremental builds;
- structured activity handoff.

### B1: persistent runtime

- named persistent non-TTY sessions;
- per-session supervisor and daemon reattachment;
- process-tree inspection;
- named PTY/REPL sessions after non-TTY ownership is proven;
- optional platform port observation.

### B2: cooperative concurrency and provider integration

- TTL mutation scopes and overlap advisories;
- evidence invalidation optimization based on measured usage;
- external semantic index/provider capability integration keyed by exact source digest;
- no core vector database or mandatory indexing subsystem.

### C: explicitly deferred expansion

- remote multi-machine execution;
- hosted/public gateway and fleet scheduling;
- containers or security profiles that change current-user authority;
- Windows/ConPTY runtime;
- additional planning agents or daemon-side reasoning.

These require separate evidence and designs. They are not prerequisites for a high-end local coding substrate.

## 27. Validation strategy

### 27.1 Contract tests

- Every closed input/output union branch with real payloads and unknown-field rejection.
- Correct root closure for JSON Schema composition.
- Capability negotiation across new bridge/new daemon, new/old, old/new, and unsupported-version pairs.
- Execution and observation fingerprints, including non-affecting response controls.
- Stable operation/child/error category separation.
- Receipt and local-state migration fixtures.

### 27.2 Activity and workspace tests

- Activity reuse across conversations and multiple workspaces.
- Multiple activities in one workspace without ownership failures.
- Workspace-relative path resolution, relocation replay, path and symlink escape rejection.
- Merge/rebase/cherry-pick/revert/bisect/conflict/ahead-behind states.
- Inherited dirty, observed-since-baseline, resolved, and baseline-diverged cases.
- Mutation-scope overlap, disjointness, TTL expiry, deduplication, and non-blocking behavior.
- Worktree switch/stash/reset/prune operations remain executable.

### 27.3 Execution and output tests

- Argv quoting/empty arguments and shell-mode compatibility.
- Named-session attach, live-name conflict, terminal-name reuse, and no implicit shell state.
- ANSI, binary, invalid UTF-8, carriage-return progress, large-line, and mixed-channel output.
- First/last, tail, line, byte, literal, and bounded-regex selectors without rerun.
- Raw cursor identity unchanged by rendered views.
- Output retention and explicit unavailable results.

### 27.4 Evidence tests

- Exact-current, fast-current, stale, unknown, incomplete, ambiguous, and never-run presentation.
- Invalidation by source, command, manifest, environment/toolchain, policy, and artifact changes.
- Unrelated changes not invalidating narrowly scoped evidence when the selector proves irrelevance.
- Dirty/affected test selection before full test in coding profiles.
- Exact hashing completes or becomes unavailable; never truncated-exact.
- Missing artifact does not alter child exit evidence but affects verification evidence.

### 27.5 Runtime and performance tests

- Supervisor ownership proof across daemon restart and ambiguous fallback.
- No signaling from stored PID/PGID alone.
- Process-child and optional port observation accuracy/quality.
- Warm/cold latency budgets and zero-probe hot-path invariants.
- Concurrent output, Git observation, hashing, evidence, and indexing worker limits.
- Crash injection during every authoritative state publication.

## 28. Acceptance criteria

The Agent Execution Layer is ready when:

1. A new agent can discover supported features and limits without invalid trial calls.
2. One `local_shell` remains the complete MCP tool surface.
3. Existing V1 clients retain durable exactly-once start/write/kill behavior.
4. An agent can run by workspace ID plus relative cwd or by explicit absolute cwd.
5. Argv and shell modes are exclusive, visible, and fingerprinted correctly.
6. Every terminal response unambiguously separates ShellBeam completion from child success.
7. Retained output can be searched or sliced without rerunning the command, while raw cursor truth remains unchanged.
8. Activity facts survive conversation changes without creating conversation or workspace ownership.
9. Transient Git operations and conflicts are visible without blocking Git commands.
10. Dirty reporting distinguishes inherited from observed-since-baseline and never overclaims causation.
11. Current exact evidence prevents unnecessary repeat verification, while stale/unknown reasons remain explicit.
12. The agent automatically performs project capability onboarding when due and creates a minimal manifest when useful and authorized.
13. Full tests and clean builds remain explicit gates rather than inner-loop defaults.
14. Expected artifacts and environment/toolchain differences are mechanically observable without secret leakage.
15. Named persistent sessions are explicit and process ownership remains safe across supported disconnect/restart levels.
16. Mutation scopes produce useful overlap warnings and never locks.
17. A new conversation can reconstruct workspace, source delta, sessions, evidence, and advisories from activity inspection.
18. Optional context work respects latency budgets and never fabricates exactness.
19. Multi-account Git preflight remains local, advisory-first, and free of SSH/`gh` probes in normal admission.
20. Every accepted enhancement E01-E20 has implementation evidence or is visibly capability-gated during staged rollout.

## 29. Reference agent flow

For an existing activity in a reusable worktree:

1. The agent discovers ShellBeam features and inspects the activity.
2. ShellBeam returns workspace, branch/HEAD, transient Git state, inherited dirty baseline, observed delta, live sessions, manifest status, and evidence freshness.
3. If onboarding is due, the agent performs a bounded capability audit and creates or refreshes a minimal manifest when authorized.
4. The agent selects the cheapest verification profile not already covered by exact-current evidence.
5. It starts an argv or shell operation using workspace-relative cwd, declared intent, expected outputs, and the same activity ID.
6. ShellBeam returns a compact rendered preview plus a durable output reference, child outcome, source delta, artifacts, evidence, and advisories.
7. If a failure detail was truncated, the agent reads the retained tail or matching lines instead of rerunning.
8. A later conversation calls activity inspection and continues from facts rather than reconstructing state with `pwd`, `git status`, `git log`, `ps`, repeated tests, and repeated build checks.

## 30. Standards references

- [Model Context Protocol 2026-07-28 release](https://blog.modelcontextprotocol.io/posts/2026-07-28/): stateless core, explicit request metadata, and `server/discover`.
- [SEP-2567: Sessionless MCP via Explicit State Handles](https://modelcontextprotocol.io/seps/2567-sessionless-mcp): explicit application handles instead of hidden transport session state.
- [SEP-2106: JSON Schema 2020-12 for tool schemas](https://modelcontextprotocol.io/seps/2106-json-schema-2020-12): composition-capable input/output schema contract.
- [MCP 2025-11-25 tool structured output](https://modelcontextprotocol.io/specification/2025-11-25/server/tools): structured content and output-schema compatibility for older hosts.

These references define transport/schema integration. ShellBeam's local durability, process ownership, workspace, activity, and evidence semantics remain application contracts specified here and in the V1/companion designs.
