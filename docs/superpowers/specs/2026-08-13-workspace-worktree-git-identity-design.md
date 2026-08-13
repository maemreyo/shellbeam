# ShellBeam Workspace, Worktree, and Git Identity Design

**Status:** Approved target design; A0/A1 implementation planned
**Date:** 2026-08-13
**Scope:** Phase A developer experience and provenance
**Companion designs:** [Agent Execution Layer](./2026-08-13-agent-execution-layer-design.md) and [Project Capability Manifest and Agent Onboarding](./2026-08-13-project-capability-onboarding-design.md)
**Implementation plan:** [Agent Execution Layer A0/A1](../plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md)

## 1. Decision

ShellBeam treats Git worktrees as a first-class developer workflow, but its safety model is advisory-first. It makes the active repository, worktree, branch, source generation, and Git account obvious without policing normal Git use.

The governing principle is:

> Make the intended workspace obvious; do not force it.

A workspace is reusable across conversations and agents. A conversation may use multiple workspaces, and multiple conversations may use the same workspace. ShellBeam does not establish an exclusive ownership relationship between a conversation and a worktree.

Callers may attach an optional soft `workspace_hint` to `local_shell.start`. The hint communicates intent, not ownership: it helps detect that task A is accidentally executing in task B's worktree while preserving unrestricted movement among workspaces.

ShellBeam continues to expose one MCP tool, `local_shell`. Workspace operations are optional CLI commands invoked through that tool; no repository-specific MCP tool is added.

## 2. Goals

- Make multi-branch work fast and understandable without repeated setup.
- Preserve dirty work and allow worktrees to be reused.
- Record which source state produced a command, test, build, or release result.
- Present ordinary workspace transitions as context events and reserve advisories for actionable mismatch or ambiguity.
- Reduce the chance of pushing company code with a personal SSH or GitHub account.
- Avoid extra confirmation turns and nonessential tool calls.
- Keep credentials outside ShellBeam state.

## 3. Non-goals

ShellBeam will not:

- Enforce one conversation per workspace.
- Lock a worktree against other conversations or agents.
- Block `git switch`, `stash`, `reset`, `clean`, `prune`, rebase, or ordinary Git commands.
- Parse arbitrary shell commands to determine whether they are safe.
- Run SSH, GitHub CLI, or network probes in ordinary command admission.
- Automatically switch the active `gh` account.
- Edit `~/.ssh/config`, create SSH keys, read private-key contents, or store tokens.
- Replace Git as the worktree authority.
- Promise filesystem isolation beyond what Git worktrees provide.

## 4. Terminology and identity

### 4.1 Repository

A repository has a stable local opaque `repository_id`. Its local anchor is the canonical Git common directory returned by Git. Remote URLs, owners, hostnames, and normalized remote identity are mutable observations, never components of repository identity.

Changing `origin` from an upstream repository to a personal fork therefore creates a context event or advisory; it does not create a new repository record. If the common directory moves, ShellBeam correlates the new observation with registered Git-native worktree anchors. Ambiguous correlation never silently merges two repository records.

### 4.2 Workspace

A workspace is a registered view of an existing directory, usually a Git worktree. It has:

- an opaque stable internal `workspace_id`, such as `ws_01K2...`;
- a mutable human-readable label;
- repository identity;
- canonical worktree root;
- a Git-native worktree anchor derived through Git, such as its private Git directory for a linked worktree;
- creation and last-seen timestamps;
- optional expected Git identity profile;
- advisory settings.

The human label is the normal user-facing identity. The opaque ID and Git-native anchor allow a supported `git worktree move` or repaired path relocation to preserve historical identity when Git can establish continuity. The filesystem path remains a mutable observation.

### 4.3 Fast generation

A fast generation is a bounded observation for ordinary shell provenance. It is not release-grade proof and contains at least:

- current branch or detached-HEAD state;
- HEAD commit;
- upstream when configured;
- dirty flag;
- Git-aware status facts and a bounded cached dirty-state digest;
- capture time.

Changing branches, commits, or dirty contents creates a new generation while retaining the same workspace identity.

The fast digest uses NUL-safe Git output and cached path metadata. It never follows symlinks and never stores source contents in receipts.

Every fast snapshot declares its quality as `fresh`, `cached`, or `unavailable`, including `observed_at` and cache age. It must not be described as exact.

### 4.4 Exact source snapshot

`devctl` test, build, indexing, and release operations use a separate exact source snapshot with two digests:

- `source_content_digest`: content-addressed identity of the effective source namespace used by the operation;
- `vcs_state_digest`: Git provenance needed to explain how that namespace relates to HEAD and the index.

The snapshot records branch, detached state, HEAD, source-view policy, and both digests. Branch names, remotes, workspace ID, and conversation state are provenance metadata and are excluded from `source_content_digest`, allowing equivalent source states to reuse compatible caches.

The canonical `vcs_state_digest` input includes:

- HEAD commit and branch/detached state;
- index entries, stages, modes, object IDs, and intent-to-add state;
- tracked working-tree status, including modification, deletion, mode change, and rename facts;
- untracked/ignored classification;
- submodule gitlink and checked-out state.

The canonical `source_content_digest` input includes:

- every effective tracked working-tree source path and its type, mode, and complete content;
- all non-ignored untracked paths by default, unless an explicit source-input policy excludes them;
- symlink link text and mode, without following the target;
- submodule effective commit and availability state; a relevant dirty submodule is recursively content-digested or makes exact quality unavailable;
- explicit inclusion policy for generated or ignored inputs that materially affect the requested build or test.

The default source view is `worktree` for local test/build/index operations and `commit_tree` for official clean release construction. Paths and records are encoded unambiguously and sorted by raw path bytes. Relevant regular-file contents are streamed completely into the hash, including large files. Unsupported special file types use explicit typed records and make exact quality unavailable when policy says they are relevant. If enumeration, reading, or the configured work budget cannot complete, quality is `unavailable`; ShellBeam never truncates inputs while claiming an exact result. Ignored files are excluded by default, but a repository policy may opt specific paths in.

Git-aware enumeration is authoritative. Raw recursive filesystem walking is not used to decide tracked, untracked, ignored, rename, intent-to-add, or submodule semantics.

### 4.5 Soft workspace affinity

The `start` branch of the closed `local_shell` input schema adds one optional closed object:

```json
{
  "workspace_hint": {
    "workspace_id": "ws_01K2...",
    "branch": "PI-756",
    "git_profile": "work"
  }
}
```

The object uses `additionalProperties: false` and `minProperties: 1`. `workspace_id` follows the bounded ShellBeam ID format; `branch` and `git_profile` are bounded, NUL/control-free UTF-8 comparison hints and are never passed to Git as commands. Semantics are advisory:

- the caller may change or omit the hint on any later command;
- it creates no conversation ownership, lease, or lock;
- a mismatch creates a cause-based advisory and execution continues;
- it is excluded from the `operation_id` intent fingerprint;
- the first accepted start records the hint as advisory context, not execution intent;
- a retry with a different hint does not mutate the bound process or durable execution facts, but may receive response-local advisories for that retry.

## 5. Advisory modes

Workspace observation has three modes:

| Mode | Behavior |
| --- | --- |
| `off` | Do not collect Git workspace metadata. |
| `observe` | Record provenance without user-facing advisories. |
| `warn` | Record provenance and return compact advisories. This is the default. |

There is no normal coding-mode `require` or exclusive lock. A mismatch never changes a successfully executed command into a ShellBeam failure.

Release verification is different: ShellBeam may execute a release command from any state, but its evidence aggregator must refuse to label the result a verified release PASS when the required clean-source or identity evidence is missing. This prevents a false claim without blocking local work.

## 6. Runtime data flow

For a command whose `cwd` belongs to a registered or discoverable Git worktree:

1. Resolve the canonical worktree and repository identity.
2. Read the latest bounded workspace snapshot and schedule a refresh when stale.
3. Resolve any soft `workspace_hint` and evaluate context transitions and advisories against the current facts.
4. Start the command using existing durable operation semantics.
5. Return context events and advisories in the same `local_shell` response; never add a workspace-only round trip.
6. At terminal finalization, read or refresh a lightweight post-execution snapshot when workspace observation is enabled.
7. Bind the receipt to the pre/post snapshots, including their quality, and record whether an observed change occurred.

Workspace observations are excluded from the ordinary `operation_id` intent fingerprint. A transport retry after an edit must still replay the already-bound operation instead of producing `operation_conflict`.

Test, build, indexing, and release subsystems key cache/evidence to the appropriate exact snapshot digest rather than the fast generation. This does not change shell-operation idempotency.

### 6.1 Latency budget

Workspace assistance must not turn every shell command into a synchronous repository scan. The observer therefore:

- caches immutable repository identity and recent workspace snapshots;
- uses Git's stable plumbing/porcelain output instead of walking `.git` directly;
- incrementally rehashes only dirty paths whose safe file metadata changed;
- performs no network, SSH, or GitHub CLI access during ordinary command admission;
- applies a small configurable synchronous budget and degrades to `cached` or `unavailable` when that budget is exceeded;
- refreshes stale observations in the background when safe.

Exceeding the observation budget never delays process spawn indefinitely. The initial target is no more than 25 ms p95 warm overhead and 150 ms p95 cold overhead on the reference repositories; measured evidence may tune these values. Exact dirty-state computation belongs inside `devctl` test/build/release execution, where its cost is part of the requested operation rather than an extra MCP round trip.

The enforceable work-budget invariants are:

- warm admission with a valid cache performs zero network, zero `ssh`, zero `gh`, and zero subprocesses;
- cold or stale admission performs at most two bounded local Git subprocesses and obeys the configured wall-clock budget;
- no ordinary admission runs `ssh -G`, `gh auth status`, credential helpers, hooks, or user-configured external probes;
- budget exhaustion returns cached or unavailable provenance and starts the requested process normally;
- deeper identity inspection occurs only in an explicit external-effect/identity preflight or explicit verification command.

## 7. Context and advisory UX

ShellBeam separates routine workflow facts from actionable risk signals.

`context_events` include transitions such as workspace, branch, HEAD, remote, and dirty-generation changes. They are neutral context, not warnings. They use a transition fingerprint and are emitted once for the observed transition.

To avoid turning normal editing into chatter, dirty-generation change is receipt-only by default. It enters the response event array only when it invalidates a reused context/index/test claim or when verbose workspace reporting was requested. Workspace, branch, HEAD, and remote transitions may be summarized once because they materially change task orientation.

`advisories` include soft-affinity mismatch, Git profile mismatch, effective-identity ambiguity, credential overrides, and observation failure. They are:

- advisory and non-blocking;
- returned with the command response;
- deduplicated by `workspace_id + advisory_code + cause_fingerprint`;
- shown again only when a fact that contributes to the cause fingerprint changes;
- bounded in count and length;
- redacted so they never expose tokens or private-key contents.

Generation changes unrelated to an identity advisory do not cause that advisory to repeat. For example, editing `foo.go` cannot re-emit an unchanged `git_profile_mismatch`.

Cause and transition fingerprints are hashes over normalized safe facts. They do not embed raw emails, absolute paths, tokens, key material, or secret environment values.

Because ShellBeam uses closed output schemas, successful response branches add optional bounded `context_events` and `advisories` arrays. An event has a stable code, concise neutral message, workspace ID, observed generation, and transition fingerprint. An advisory has a stable code, severity, concise actionable message, workspace ID, and cause fingerprint. Receipts add an optional versioned `workspace_provenance` object. Older receipts remain readable, and an absent object means provenance was not captured rather than that the workspace was clean.

Adding `workspace_hint`, response arrays, and receipt provenance requires an explicit compatible MCP/IPC/receipt schema-version increment. Bridge and daemon negotiate the capability; an older peer executes without workspace assistance and reports that the advisory feature is unavailable rather than silently inventing provenance.

Representative context-event codes:

- `workspace_changed`
- `branch_changed`
- `head_rewound`
- `dirty_generation_changed`
- `remote_changed`

Representative advisory codes:

- `workspace_hint_mismatch`
- `git_profile_unknown`
- `git_profile_mismatch`
- `commit_identity_mismatch`
- `ssh_identity_ambiguous`
- `gh_account_mismatch`
- `credential_env_override`

Examples:

```text
Context: workspace PI-756_criteria-validation changed branch main -> PI-756.
```

```text
Advisory: expected Git profile work, but remote/commit/gh observations match personal; execution continued.
```

`workspace_label_nonstandard` appears only as a suggestion in `workspace create`, `rename`, or `inspect`, never in ordinary `local_shell` execution. Concurrent workspace use is receipt/inspect-only by default and creates no ordinary event or advisory.

## 8. Worktree lifecycle

ShellBeam provides optional CLI conveniences while Git remains authoritative:

```text
shellbeam workspace list
shellbeam workspace inspect <label-or-id>
shellbeam workspace attach --path <existing-path> [--label <label>]
shellbeam workspace create --repo <path> --branch <branch> [--base <ref>] [--label <label>] [--path <path>]
shellbeam workspace rename <label-or-id> <new-label>
shellbeam workspace forget <label-or-id>
shellbeam workspace remove <label-or-id> [--force]
```

`attach` is preferred when a matching worktree already exists. `create` first checks `git worktree list --porcelain -z`; if the branch already has a worktree, it reuses and returns that location instead of creating a duplicate by accident. The user can still choose another valid Git arrangement explicitly.

Path resolution order for `create` is:

1. explicit `--path`;
2. repository-level path template;
3. global path template;
4. default sibling template: `<repo-parent>/<repo-name>-worktrees/<label>`.

Templates support only documented fields such as repository name, label, issue key, and branch slug. ShellBeam derives the label and path without prompting when the result is unambiguous and available. If the path is occupied by the intended existing Git worktree, it reuses it. If an unrelated path occupies the candidate, ShellBeam chooses a deterministic short suffix and reports the resolved path; it does not interrupt the normal flow merely to choose a folder name.

`forget` only removes ShellBeam registry metadata. It does not touch the filesystem or Git.

`remove` performs a material deletion. Confirmation rules optimize for flow:

- An explicit user instruction to remove a named worktree is already confirmation; do not ask again.
- If an agent independently proposes removal, request one confirmation.
- A dirty or unusual worktree produces a clear warning and requires explicit force intent, but no multi-stage confirmation ceremony.
- Report whether removal succeeded and what metadata was forgotten.

ShellBeam does not automatically run `git worktree prune`. When requested, it may show `--dry-run` output before the actual prune without treating the dry run as a separate approval if the user already authorized pruning.

## 9. Naming convention

Labels use a soft convention:

```text
<ISSUE-KEY>_<short-purpose>
```

Examples:

```text
ZMR-111_workspace-provenance
PI-756_criteria-validation
```

Without an issue key:

```text
<kind>_<short-purpose>
```

Suggested kinds are `feature`, `fix`, `review`, `spike`, `hotfix`, and `release`.

Normalization suggestions:

- preserve the recognizable issue key;
- use lowercase kebab-case for the purpose;
- keep the label concise;
- add a short numeric suffix only on collision.

This is a recommendation, not a validation barrier. Existing or unconventional names remain usable. At most, ShellBeam emits one low-priority suggestion.

## 10. Git identity profiles

Multiple-account expectations are repository-scoped by default because linked worktrees normally share remotes and common configuration. A workspace may override the expected profile. Whenever effective identity is computed, it is resolved in the target `cwd`; ordinary hot-path responses may use explicitly aged cached facts. This is necessary because `extensions.worktreeConfig`, includes, environment, and command-scoped configuration can make one worktree differ from the repository default.

An identity profile describes expectations, not credentials:

```toml
[git_profiles.work]
ssh_host_aliases = ["github-work"]
remote_owners = ["company-org"]
commit_emails = ["developer@company.example"]
gh_host = "github.com"
gh_user = "developer-company"

[git_profiles.personal]
ssh_host_aliases = ["github-personal"]
remote_owners = ["personal-owner"]
commit_emails = ["developer@personal.example"]
gh_host = "github.com"
gh_user = "personal-owner"
```

Profiles may additionally declare allowed signing-key fingerprints and repository URL patterns. They never contain tokens, SSH private-key material, or passphrases.

### 10.1 Three independent identities

ShellBeam evaluates these separately:

1. **Commit identity:** author, committer, and signing identity plus their observed source.
2. **Git transport identity:** remote URL/owner/host alias and transport-override class.
3. **GitHub CLI identity:** stored active account when observable, target host/repository overrides, and credential-override class.

A match in one category does not imply a match in the others.

### 10.2 Profile resolution

Expected profile resolution order is:

1. explicit workspace override;
2. explicit repository binding;
3. unique match from normalized remote owner/URL rules;
4. no profile, producing at most an advisory `git_profile_unknown`.

Ambiguous automatic matches do not choose silently.

### 10.3 SSH observation

Ordinary command admission observes only the remote URL, SSH host alias, safe cached facts, and presence classes for `GIT_SSH`, `GIT_SSH_COMMAND`, or runtime configuration overrides. It runs no `ssh` command.

An explicit deep identity preflight may run `ssh -G <host-alias>` under its own timeout and resource budget to inspect effective configuration. Although this opens no remote connection, OpenSSH configuration may contain `Match exec`, which executes a local shell command while configuration is evaluated. ShellBeam therefore never invokes `ssh -G` silently or in the ordinary hot path.

The deep result records only safe facts such as host alias, target hostname, identity-file path fingerprints, and `IdentitiesOnly` state. It does not read private-key contents. A generic `github.com` remote in a multi-account environment may produce `ssh_identity_ambiguous`, particularly when transport is overridden, several agent keys are loaded, or `IdentitiesOnly` cannot be established.

### 10.4 GitHub CLI observation

Ordinary command admission runs no `gh` process. An explicit identity preflight may inspect the stored active account using `gh auth status --active --hostname <host>`. It must never include tokens in logs, receipts, advisories, or diagnostic output.

`GH_TOKEN`, `GITHUB_TOKEN`, enterprise-token variants, `GH_HOST`, `GH_REPO`, or a non-default `GH_CONFIG_DIR` can override credentials, target, or stored configuration. ShellBeam records only presence, safe target metadata, and source class, never token values.

When a token override is present, a stored user from `gh auth status` is not claimed as the effective user. The snapshot reports the stored user separately, records the override class, and marks effective user `unknown` unless an authoritative non-secret observation proves it.

ShellBeam does not automatically execute `gh auth switch`, because it changes account state for the host and may disrupt another workspace or conversation. Profile-specific `GH_CONFIG_DIR` support is deferred until a real workflow experiment proves it improves isolation without adding excessive configuration burden.

### 10.5 Override sources

Identity snapshots represent source as well as value/match status. At minimum they distinguish:

```text
commit_identity_source:
  config | worktree_config | environment | runtime_config | unknown

transport_override:
  none | GIT_SSH | GIT_SSH_COMMAND | runtime_config | unknown

gh_target_source:
  repository | GH_HOST | GH_REPO | config_dir | unknown

gh_credential_source:
  stored | token_override | unknown
```

Relevant Git environment classes include `GIT_AUTHOR_*`, `GIT_COMMITTER_*`, `EMAIL`, `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`, and `GIT_CONFIG_COUNT` with its indexed key/value pairs. ShellBeam stores override presence, key classifications, and other safe classifications in ordinary provenance. Raw runtime values are excluded or redacted; an explicit preflight may compute and compare effective non-secret author/committer facts without publishing them in shared evidence.

An inline `git -c ...`, prefixed environment assignment such as `GH_TOKEN=... gh ...`, or shell-startup export embedded inside an arbitrary command cannot be known reliably before execution without evaluating/parsing that shell. ShellBeam intentionally does not parse the command and does not claim to detect those cases. The snapshot covers the environment ShellBeam supplies to the child; explicit wrappers/preflights may provide stronger evidence when needed.

## 11. External-effect preflight

ShellBeam does not parse the shell language or block commands. It can nevertheless improve clarity at two low-cost boundaries:

- before an explicit ShellBeam workspace lifecycle command;
- after any command, by comparing observed pre/post state.

Agent guidelines call for a bounded explicit preflight before externally visible Git effects such as push, PR creation, tag creation, release publication, or other publishing when cached identity evidence is missing or invalid. A representative CLI is:

```text
shellbeam workspace preflight [<label-or-id>] --effect push|pr|tag|release|publish|verify [--deep]
```

The preflight evaluates remote owner, expected profile, effective author/committer/signing match, transport overrides, stored/effective `gh` evidence, target overrides, and credential override classes. `--deep` may perform the explicitly authorized bounded SSH/GitHub CLI observations described above. Results are cached by the facts that determine their cause, not by dirty source generation.

The preflight is advisory: a completed check exits successfully even when it reports mismatches, so it cannot become an accidental Git gate when composed in a shell. Invocation or inspection failure is reported separately. The externally visible command remains technically executable. The agent surfaces mismatches before the effect and does not request repeated confirmation while the cause fingerprint remains unchanged and the user has already authorized that exact account/remote combination.

ShellBeam does not infer an external effect by parsing arbitrary shell text. Without an explicit preflight, hint, or wrapper, it may only detect relevant mutations after execution; the design does not overclaim preventive coverage for that path.

## 12. Integration with developer tooling

### 12.1 Dirty and affected tests

Affected-test calculation is scoped to the exact source snapshot and configured merge base of the target worktree. Results record workspace ID as consumer metadata, base, HEAD, source-content digest, VCS-state digest, selected packages, and cache information.

Changing the exact source snapshot invalidates only affected-test selection metadata, not Go's safe content-addressed build cache.

### 12.2 Incremental builds

Collision-prone build outputs use operation-local staging beneath a namespace such as:

```text
.build/workspaces/<workspace-id>/<source-digest>/<build-id>/
```

A successful build publishes a named artifact through atomic replace or a content-addressed artifact reference. Two conversations building the same workspace and source digest therefore cannot clobber each other's in-progress output. Language/compiler caches that are already safely content-addressed remain shared across worktrees and operations.

### 12.3 Semantic code search

An optional Semble-style context provider uses the cache identity:

```text
repository namespace
+ source content digest
+ indexer version
+ indexer configuration fingerprint
```

`workspace_id` is consumer/reference metadata rather than mandatory cache identity. Two worktrees with the same source-content digest can reuse one compatible index even if their branch names or VCS-only state differ. Search results carry the source-content digest; when it differs from the consumer's current state, ShellBeam or the agent reports stale context and refreshes incrementally when practical.

The search provider remains an optional external CLI/provider, not a dependency of the core daemon.

### 12.4 Receipts and release evidence

Ordinary command receipts include fast best-effort workspace provenance without changing command success. Test/build receipts use the exact source snapshot and distinguish its content and VCS-state digests. Official release evidence requires an exact clean commit-tree view and matching required identity policy; otherwise execution results remain available but are labeled unverified.

## 13. Configuration ownership and privacy

- Workspace registry and profiles are local per-user state.
- Repository bindings may live in ShellBeam state; no repository file is modified merely by observation.
- Shared executable project capabilities may optionally live in `.shellbeam/project.toml` under the companion onboarding design; local profile names, account bindings, and machine paths never belong in that file.
- Raw commands, output, source contents, token values, and private-key contents are not part of workspace provenance.
- Raw absolute paths are local diagnostic data and are redacted from remotely shared or release evidence by default.
- Local receipt retention follows ShellBeam's existing bounded retention policy.

## 14. Failure handling

Workspace and identity observation is best effort. Failure to run Git, SSH configuration inspection, or `gh` inspection produces a bounded advisory and never prevents an unrelated shell command from running.

Registry corruption is isolated from process execution. ShellBeam may rebuild discoverable workspace metadata from Git and retain historical receipts by opaque IDs. It must not silently recreate destructive intent.

If provenance changes while a command runs, both pre- and post-generation are retained. ShellBeam reports that the workspace changed during execution rather than guessing which source state the process actually read at each instant.

## 15. Validation strategy

### 15.1 Workspace behavior

- Reuse one worktree from multiple conversations without denial.
- Use multiple workspaces from one conversation.
- Supply matching, mismatching, changed, and omitted soft workspace hints; verify execution always continues and hints never affect operation identity.
- Switch branch, reset HEAD, stash, change remotes, and modify files; verify command execution continues and context/advisory classification is correct.
- Retry one `operation_id` after the workspace changes; verify no duplicate spawn and no operation conflict.
- Move a Git worktree or rename a workspace label while preserving historical identity when Git continuity is provable.
- Attach a pre-existing worktree and avoid accidental duplicate creation.
- Create with explicit path, repository/global template, and default template; verify collision resolution does not add an unnecessary prompt.

### 15.2 Context and advisory quality

- Verify context events and advisories share the original response and add no workspace-only request.
- Verify editing files does not repeat an unchanged identity advisory.
- Verify cause-changing identity facts re-emit the appropriate advisory.
- Verify ordinary branch/dirty transitions are neutral context events.
- Verify naming suggestions occur only in lifecycle commands and concurrent use is inspect-only by default.
- Bound event/advisory count and message size.
- Verify advisory failures never change child exit status or receipt outcome.
- Verify schema/capability negotiation with an older peer preserves command execution and honestly reports unavailable workspace assistance.

### 15.3 Removal UX

- Explicit user removal proceeds without a second confirmation.
- Agent-initiated removal asks once.
- Dirty removal gives a concrete warning and honors explicit force intent.
- `forget` never deletes files.

### 15.4 Multi-account safety

- Company remote with personal commit email.
- Company remote through personal SSH alias.
- Correct SSH alias with wrong active `gh` user.
- Correct stored `gh` user overridden by `GH_TOKEN` presence.
- `GH_HOST`, `GH_REPO`, and `GH_CONFIG_DIR` target/configuration overrides.
- `GIT_AUTHOR_*`, `GIT_COMMITTER_*`, `EMAIL`, `GIT_SSH`, `GIT_SSH_COMMAND`, and runtime-config environment classes.
- Generic `github.com` remote with multiple identities loaded.
- Profile ambiguity and unknown profile.
- Repository default profile overridden by one worktree; verify effective identity is observed at target `cwd`.
- Verify `ssh -G` and `gh` are never invoked during ordinary admission, including with an SSH configuration containing `Match exec`.
- Verify token override makes effective `gh` user unknown unless separately proven.
- Verify no token or private-key material appears in logs, receipts, or advisories.

### 15.5 Cache and provenance

- Fast generation and the exact source snapshot are never conflated in schema, logs, or claims.
- Exact snapshot fixtures cover modified/untracked/ignored/deleted/renamed/intent-to-add paths, symlink text without target traversal, submodules, modes, large streaming files, and unsupported special files.
- Equivalent source namespaces with different branch/VCS-only metadata have equal source-content digests and distinct VCS-state digests.
- Dirty-test selection is tied to the correct exact source snapshot.
- Concurrent builds in the same workspace use isolated staging and atomic publication; safe shared compiler caches remain effective.
- Equal source-content digests across two worktrees reuse a compatible semantic index; changed digests are marked stale.
- Release aggregation rejects a false verified-PASS claim without preventing the underlying command from running.

### 15.6 Work-budget enforcement

- Warm cached admission runs zero subprocesses and no network/SSH/`gh` probes.
- Cold/stale admission never exceeds two local Git subprocesses or its wall-clock budget.
- Timeout and malformed Git output degrade to cached/unavailable provenance and still spawn the requested command.
- Latency benchmarks report p50/p95/p99 separately for cached, cold, dirty-large, and non-repository directories.

## 16. Delivery slices

1. **Observation foundation:** stable repository/worktree identity, fast generation, soft workspace hint, receipt fields, `off|observe|warn`, context events, cause-based advisories, and hard work budgets.
2. **Reusable registry and CLI:** list, inspect, attach, create with path templates, rename, and forget; soft naming suggestions.
3. **Exact provenance:** exact source-snapshot/content/VCS digest contracts and `devctl` integration for test/build/release evidence.
4. **Git identity profiles:** repository default/workspace override, environment-source classification, explicit deep preflight, and redaction tests.
5. **Material lifecycle:** remove UX with single-confirmation semantics and safe dirty advisories.
6. **Developer-tool integration:** operation-local build publication, affected-test provenance, and content-keyed optional semantic indexes.

Each slice is independently useful. Failure of workspace metadata must degrade to ordinary ShellBeam execution, not disable the local shell.

## 17. Acceptance criteria

The design is successful when:

- normal multi-branch work does not require extra confirmation turns;
- a worktree can be reused freely across conversations;
- a conversation can move among workspaces without re-registration;
- an optional soft hint makes accidental workspace mismatch visible without binding or blocking;
- Git state-changing commands remain executable;
- context events stay neutral while advisories are compact, cause-deduplicated, actionable, and non-blocking;
- ordinary receipts honestly identify fast provenance quality, while exact claims use the separate exact source snapshot and its typed digests;
- company repositories make personal SSH, commit, or `gh` identity mismatches obvious before externally visible operations;
- ShellBeam stores no GitHub token or SSH private-key contents;
- one MCP tool and one reasoning agent remain intact;
- warm workspace observation stays within its latency budget and observation failure cleanly degrades to ordinary execution.

## 18. Reference behavior

- Git worktree discovery and lifecycle follow the stable porcelain interface documented in [Git worktree](https://git-scm.com/docs/git-worktree.html).
- Git identity and transport override classification follows [Git environment variables](https://git-scm.com/docs/git) and runtime configuration follows [Git config](https://git-scm.com/docs/git-config).
- Multiple SSH accounts follow GitHub's documented separate-key, host-alias, and `IdentitiesOnly` pattern in [Managing multiple accounts](https://docs.github.com/en/account-and-profile/how-tos/account-management/managing-multiple-accounts).
- The explicit-only treatment of `ssh -G` follows OpenSSH configuration semantics, including local shell execution by [`Match exec`](https://man.openbsd.org/OpenBSD-current/man/ssh_config).
- GitHub CLI account observation follows [`gh auth status`](https://cli.github.com/manual/gh_auth_status); ShellBeam does not silently call [`gh auth switch`](https://cli.github.com/manual/gh_auth_switch).
- Environment override handling follows [GitHub CLI environment variables](https://cli.github.com/manual/gh_help_environment), with values always redacted.
