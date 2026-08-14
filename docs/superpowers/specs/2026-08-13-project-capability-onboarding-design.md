# ShellBeam Project Capability Manifest and Agent Onboarding Design

**Status:** Approved target design; A0/A1 implementation planned
**Date:** 2026-08-13
**Scope:** Optional project capability discovery for the Agent Execution Layer
**Parent design:** [ShellBeam Agent Execution Layer](./2026-08-13-agent-execution-layer-design.md)
**Implementation plan:** [Agent Execution Layer A0/A1](../plans/2026-08-13-shellbeam-agent-execution-layer-a0-a1.md)

## 1. Decision

ShellBeam supports an optional, repository-controlled capability manifest at:

```text
.shellbeam/project.toml
```

The file is a small, schema-versioned description of executable project capabilities: toolchains, canonical commands, verification profiles, relevant environment names, and expected outputs. It is not prompt documentation, a policy engine, or a replacement for `README.md`, `CONTRIBUTING.md`, or `AGENTS.md`.

The governing distinction is:

> Human and agent guidance stays in documentation; mechanically invocable project capabilities go in the manifest.

An AI coding agent must not wait for the user to know this file exists. During project onboarding, the agent checks manifest status, audits the repository when the file is absent or needs review, and creates or updates a minimal manifest when the evidence is strong and repository writes are authorized.

ShellBeam itself remains deterministic and contains no model or project reasoning. It validates, fingerprints, exposes, and records use of the manifest. The agent performs discovery and judgment through ordinary repository inspection.

Missing, invalid, or review-due manifests never block a shell command.

## 2. Goals

- Let a newly connected agent discover the cheapest correct format, test, build, and verification paths without repeatedly reconstructing them from prose.
- Prefer affected tests and incremental builds during coding while preserving explicit checkpoint and release gates.
- Make onboarding automatic, low-friction, evidence-based, and reusable across conversations and worktrees.
- Keep command execution explicit; reading a repository manifest never executes its commands.
- Bind verification evidence to the exact manifest revision that defined the command.
- Preserve user-authored manifest changes and avoid noisy generated churn.
- Keep local paths, account names, credentials, and machine-specific decisions out of the repository file.

## 3. Non-goals

The manifest will not:

- contain long-form instructions, coding style prose, task context, or model prompts;
- replace repository documentation or higher-priority user, organization, or agent instructions;
- grant permission to run a command or weaken the normal authority boundary;
- automatically execute commands when read, validated, or discovered;
- encode Git credentials, SSH host aliases, tokens, private environment values, or absolute user paths;
- describe every command available in the repository;
- become a general workflow language, CI engine, package manager, or dependency graph for arbitrary automation;
- guarantee that a repository-provided command is safe merely because it validates;
- require every repository to commit a manifest.

## 4. Trust and ownership

`.shellbeam/project.toml` is repository-controlled, untrusted input. A checked-out branch may change it just as it may change a build script.

ShellBeam therefore:

- parses it without interpolation, includes, templates, or network access;
- validates it against a closed schema with bounded sizes;
- never expands environment variables while parsing;
- never executes a declared command during load or validation;
- records the manifest content digest in any evidence derived from it;
- reports invalid or unsupported fields without falling back to guessed semantics;
- treats branch changes as potentially changing the active manifest.

The active user request and platform authority still decide whether a command may run. Repository instructions cannot use the manifest to self-authorize external effects, destructive actions, releases, or pushes.

## 5. File and schema contract

### 5.1 Location and discovery

The path is relative to the registered workspace root. ShellBeam does not search parent directories above that root and does not follow a manifest symlink outside the workspace.

For a Git worktree, the active file belongs to the checked-out source view. Different branches may therefore expose different manifest revisions while retaining the same stable repository and workspace identities.

The initial format is TOML because it is human-reviewable, diff-friendly, and suitable for small declarative data. `schema_version` is required and is an integer. Unsupported major versions are reported as `unsupported`, not partially interpreted.

Initial resource limits are implementation constants advertised during capability negotiation:

- maximum file size: 64 KiB;
- maximum commands: 64;
- maximum verification profiles: 16;
- maximum steps per profile: 64;
- maximum expected outputs: 64;
- maximum relevant environment names: 64;
- maximum string length: 1,024 UTF-8 bytes unless a narrower field limit applies.

Limits may be revised through a protocol version without silently accepting a meaning-changing schema.

### 5.2 Version 1 shape

Version 1 supports these closed top-level tables:

- `project`
- `toolchains`
- `commands`
- `verification`
- `environment`
- `outputs`

Unknown fields fail validation so misspellings do not become ignored configuration. Future optional sections require a schema-version or negotiated extension.

A representative manifest is:

```toml
schema_version = 1

[project]
name = "shellbeam"

[toolchains.go]
version_source = "go.mod"

[commands.format_check]
argv = ["make", "fmt-check"]
cwd = "."
kind = "inspect"
cost = "fast"

[commands.test_affected]
argv = ["make", "test-dirty"]
cwd = "."
kind = "test"
cost = "medium"
source_scope = "affected"

[commands.build_incremental]
argv = ["make", "build"]
cwd = "."
kind = "build"
cost = "medium"

[[commands.build_incremental.expected_outputs]]
path = ".build/shellbeam"
kind = "file"
digest = "sha256"

[commands.test_full]
argv = ["make", "test"]
cwd = "."
kind = "test"
cost = "expensive"
source_scope = "full"

[verification.profiles.coding]
steps = ["format_check", "test_affected", "build_incremental"]

[verification.profiles.checkpoint]
steps = ["format_check", "test_affected", "build_incremental"]

[verification.profiles.release]
steps = ["format_check", "test_full", "build_incremental"]

[environment]
relevant_presence = ["CGO_ENABLED", "GOFLAGS"]

[[outputs]]
path = ".build"
kind = "directory"
role = "build-cache"
```

The example is illustrative, not a mandatory command vocabulary. Onboarding uses the repository's existing canonical wrappers instead of inventing equivalent commands.

### 5.3 Project table

`project.name` is an optional bounded display label. It is not repository identity. ShellBeam's stable `repository_id` remains local and is derived from Git-native anchors as defined by the workspace design.

The manifest does not store `workspace_id`, branch, remote URL, account profile, or an absolute root path.

### 5.4 Toolchain table

Each toolchain entry has a stable lowercase key and may declare one of:

- `version`: a repository-required version or constraint already established by project evidence;
- `version_source`: a repository-relative file from which the toolchain version is authoritatively derived;
- `manager`: a bounded identifier such as `mise`, `asdf`, `volta`, or `go` when that manager is already configured by the repository.

Onboarding should prefer `version_source` over duplicating a literal version that would become stale. A toolchain entry does not install software.

### 5.5 Command table

Each command has a stable ID matching `^[a-z][a-z0-9_-]{0,63}$` and exactly one execution form:

- `argv`: a non-empty array executed directly without a shell; or
- `shell`: a non-empty command string executed through the ShellBeam shell mode.

`argv` is preferred whenever the canonical repository entry point does not require shell syntax.

Supported metadata is deliberately small:

- `cwd`: repository-relative working directory, default `.`;
- `kind`: `format`, `inspect`, `test`, `build`, `generate`, or `release`;
- `cost`: `fast`, `medium`, or `expensive`;
- `source_scope`: `none`, `affected`, or `full`;
- `mutates_source`: optional boolean; absence means unknown;
- `external_effect`: optional boolean; absence means unknown;
- `timeout_ms`: optional bounded recommendation, not an authorization grant;
- `expected_outputs`: zero or more output observations;
- `depends_on`: zero or more command IDs used only for graph validation and presentation.

The manifest never stores a secret-bearing environment value. Version 1 does not allow arbitrary per-command environment maps. A later extension may support references to locally configured environment profiles, but repository data must never contain the secret values.

`depends_on` must form a directed acyclic graph. ShellBeam exposes the graph; it does not automatically execute the graph merely because a command is requested.

### 5.6 Verification profiles

A verification profile is an ordered list of command IDs. Recommended conventional IDs are:

- `coding`: cheapest useful feedback during an edit loop;
- `checkpoint`: broader evidence before handoff, commit, or pull request;
- `release`: complete release gate.

The names are conventional, not hard-coded authority levels. A repository may add other bounded profile names.

Profiles solve a specific efficiency problem:

- full test and full clean build do not run by default during the inner loop;
- affected tests and incremental builds are preferred when available;
- broader profiles remain explicit gates rather than disappearing from quality policy.

An agent selects a profile based on the task and current evidence. ShellBeam does not infer the correct profile from arbitrary shell text.

### 5.7 Environment table

`environment.relevant_presence` lists environment variable names whose presence can affect reproducibility. ShellBeam records only presence by default, never values.

Repository data cannot opt a secret value into receipts. Any future non-secret value capture requires a separate local allowlist and explicit field classification. Known secret-shaped names remain denied regardless of repository configuration.

### 5.8 Outputs

Top-level `outputs` describe stable project locations such as build caches, generated code roots, or coverage directories. Command-level `expected_outputs` describe artifacts expected from one execution.

Every path is repository-relative, normalized, NUL-free, and cannot escape the workspace root through `..` or a followed symlink. Supported output kinds are `file`, `directory`, and `symlink`. Supported digest modes are `none`, `sha256` for a file, and `tree-sha256` for an explicitly requested directory tree. Command-level outputs are required by default; `required = false` marks an observation as optional.

An exact digest is all-or-unavailable: a budget overrun never produces a truncated hash labeled exact.

## 6. Manifest status model

ShellBeam exposes one of these deterministic states for the active source view:

| State | Meaning |
| --- | --- |
| `absent` | No manifest exists in the active workspace source view. |
| `valid` | The file parses, validates, and matches the last recorded discovery fingerprint. |
| `review_due` | The file remains valid, but relevant repository capability inputs changed after its last review. |
| `invalid` | TOML or schema validation failed. |
| `unsupported` | The schema version or required extension is not understood. |
| `conflicted` | The worktree contains an unresolved merge stage for the manifest. |
| `unavailable` | ShellBeam could not read or evaluate status within its bounded budget. |

`review_due` means “audit recommended,” not “wrong.” It must not be called stale proof unless a referenced command or path is demonstrably invalid.

A local onboarding decision may also record `not_needed` for an absent manifest. That decision is keyed to the repository capability-input fingerprint and expires automatically when those inputs change. It is local metadata and is never written into the repository as a placeholder file.

## 7. Onboarding triggers

The agent runs or resumes capability onboarding when any of these is true:

- first attachment to a repository with `absent` status and no current `not_needed` decision;
- manifest status is `review_due`, `invalid`, `unsupported`, or `conflicted`;
- the user explicitly requests onboarding, developer workflow setup, or manifest refresh;
- a branch or source change introduces new canonical automation or invalidates referenced commands;
- capability negotiation reports a newer supported schema whose adoption has a concrete benefit.

The agent does not repeat onboarding on every command, every conversation, or every worktree. Repository-level discovery results are reusable when the relevant source fingerprint matches. A new worktree on a branch with equivalent capability inputs reuses the result.

Onboarding is interruptible. The requested coding operation may continue with existing repository conventions if manifest work is nonessential. ShellBeam never inserts a confirmation gate into the command path.

## 8. Agent onboarding workflow

An onboarding-capable AI agent follows this deterministic outer workflow:

1. Inspect ShellBeam capabilities and active `manifest_status`.
2. If status is `valid`, load only the capabilities relevant to the current task.
3. If onboarding is due, inventory high-signal project files using bounded repository search.
4. Identify existing canonical command entry points and toolchain sources.
5. Reconcile those commands with CI and developer documentation.
6. Decide whether a manifest provides enough value to justify a repository file.
7. Synthesize the smallest supported candidate without invented commands or copied prose.
8. Validate the candidate with ShellBeam's schema validator.
9. Use only read-only or otherwise already-authorized cheap probes when a probe materially increases confidence.
10. Create or update the file when write authority and confidence rules permit; otherwise present a candidate or defer the ambiguous fields.
11. Record the discovery-input fingerprint and result in local ShellBeam state.
12. Use manifest command IDs and verification profiles for subsequent evidence correlation.

This workflow belongs in the agent-facing onboarding contract advertised by ShellBeam. It is not a daemon-side reasoning loop.

## 9. Discovery evidence and precedence

The agent audits repository evidence in this order:

1. Existing project-owned automation surfaces: `Makefile`, `Taskfile`, `justfile`, package scripts, checked-in project CLIs, and equivalent task definitions.
2. CI and release workflows that invoke the canonical checks.
3. Toolchain and package manifests such as `go.mod`, `package.json`, lockfiles, version files, and language configuration.
4. Test, build, formatter, linter, code-generation, and coverage configuration.
5. `AGENTS.md`, `CONTRIBUTING.md`, and focused developer documentation as corroborating evidence.
6. README examples only when no stronger executable source exists.

The agent prefers the highest existing wrapper used consistently by developers and CI. If CI runs `make test`, the candidate should normally reference `make test`, not reverse-engineer and duplicate its recipe.

The agent must not:

- invent a `test_affected` command merely because affected testing is desirable;
- translate prose into a shell command without executable corroboration;
- copy a full CI pipeline into the manifest;
- assume `--help`, `--dry-run`, or a task named `help` is free of side effects;
- execute repository code solely to make onboarding appear complete;
- silently choose between conflicting canonical commands when the choice affects correctness.

When evidence conflicts, the agent omits the uncertain field and records a compact reason. It asks the user only when the ambiguity materially blocks the active task or would establish a consequential team convention.

## 10. When a manifest is useful

The agent should create a manifest when at least one durable benefit exists, for example:

- the repository has multiple toolchains or non-obvious version sources;
- canonical commands are hidden behind project wrappers;
- affected tests or incremental builds materially reduce feedback time;
- full test or clean build is expensive and belongs only at broader gates;
- verification order matters;
- generated outputs or release artifacts need mechanical observation;
- multiple worktrees or conversations would otherwise repeat the same discovery;
- CI and local developer commands need an explicit, machine-readable mapping.

A manifest is usually unnecessary for a trivial repository with one obvious toolchain, canonical default commands, no expensive gate, and no reusable output contract. In that case the agent records a fingerprinted local `not_needed` decision and continues without creating repository noise.

## 11. Write and update policy

### 11.1 Authority

Onboarding respects the active request:

- For a change/build/onboarding task that authorizes repository edits, the agent may create a high-confidence new manifest without an extra confirmation turn. It reports the file as part of the task's source delta.
- For review, explanation, or diagnosis-only work, the agent does not modify the repository. It may return a proposed manifest or findings.
- The agent never commits, pushes, releases, or changes Git identity merely because it created the file.
- If repository policy explicitly forbids new configuration files, the agent obeys that policy and records `not_needed` or proposes another location.

### 11.2 Confidence

Automatic creation requires all referenced commands to have strong repository evidence and the candidate to validate. Optional uncertain sections are omitted rather than guessed.

Automatic update requires a focused diff. The agent preserves comments, ordering where practical, and user-authored entries. It never regenerates the entire file blindly. An unsupported schema version or unknown extension is preserved byte-for-byte and is never auto-rewritten.

If an existing entry conflicts with newly observed evidence, the agent reports the discrepancy. It changes the entry automatically only when the canonical source is unambiguous and the active task authorizes repository edits.

### 11.3 Flow

No manifest state adds a modal prompt to normal coding. Confirmation is reserved for an actual material choice, not for routine file creation supported by clear evidence.

## 12. Discovery fingerprint and review-due detection

ShellBeam stores local discovery provenance separately from the repository file. The record includes:

- `repository_id` and active source snapshot identity;
- manifest content digest and schema version;
- a sorted list of capability-input paths selected during onboarding;
- exact content digests for those bounded inputs when available;
- discovery timestamp and agent/protocol version;
- outcome: `created`, `updated`, `confirmed`, `proposed`, or `not_needed`;
- validation result.

The record excludes file contents, prompts, credentials, absolute paths when a repository-relative path is sufficient, and machine-specific account configuration.

Changes to selected capability inputs produce `review_due`. Typical inputs include task files, package scripts, CI workflows, toolchain version files, and the manifest itself. Ordinary source edits do not trigger manifest review.

If exact input hashing cannot complete, status quality is `unavailable` or `cached`; ShellBeam does not claim the manifest is current based on a truncated scan.

## 13. Runtime exposure

Capability negotiation advertises:

- support for project manifests;
- supported schema versions and extensions;
- parser and resource limits;
- manifest status support;
- validation support;
- whether discovery fingerprints and evidence binding are available.

Workspace and activity inspection return a bounded `project` object containing:

- status and status quality;
- schema version and manifest digest when readable;
- available command IDs and verification profile IDs;
- review reason codes;
- last onboarding outcome and time;
- no raw command text unless explicitly requested.

The agent can explicitly request the validated manifest data through the single `local_shell` contract or invoke `shellbeam project inspect --json` through normal execution. Ordinary command responses include only relevant command/profile IDs and manifest digest, not the entire manifest.

## 14. Integration with execution and evidence

When an agent starts a declared command, it may supply `project_command_id`. ShellBeam resolves the active validated manifest once and binds these facts to the operation:

- manifest content digest;
- command ID and canonical command fingerprint;
- resolved execution mode and repository-relative cwd;
- declared intent, cost, source scope, and expected outputs;
- relevant toolchain and environment fingerprint selectors.

The agent can inspect the final argv or shell command before choosing to start it, and the operation response/receipt records the exact resolved form. A command ID is not a hidden macro.

Evidence created from the execution is current only when all required dimensions still match:

- source content digest and relevant VCS context;
- command fingerprint;
- manifest digest and schema semantics;
- environment/toolchain fingerprint;
- expected artifact observations;
- verification policy/profile version where applicable.

Changing prose documentation alone does not invalidate evidence unless that file was explicitly selected as a capability input or command input.

## 15. Validation and typed failures

Manifest inspection uses stable result codes, including:

- `project_manifest_absent`
- `project_manifest_too_large`
- `project_manifest_parse_error`
- `project_manifest_schema_error`
- `project_manifest_unsupported`
- `project_manifest_conflicted`
- `project_manifest_path_escape`
- `project_manifest_unknown_command`
- `project_manifest_dependency_cycle`
- `project_manifest_limit_exceeded`
- `project_manifest_changed_during_resolve`

Each result includes a bounded location when available, concise message, actionable hint, and retryability. Validation errors never become child exit results and never prevent an unrelated explicitly supplied shell command from running.

If a manifest changes between resolution and operation admission, ShellBeam retries the read within a bounded budget or returns `project_manifest_changed_during_resolve`. It never combines fields from two revisions.

## 16. Privacy and portability

The committed manifest contains only repository-portable facts. The following remain in local ShellBeam configuration or state:

- SSH and Git account profiles;
- absolute workspace paths;
- local tool installation paths;
- secret environment values;
- per-user performance overrides;
- onboarding timestamps and local `not_needed` decisions;
- machine-specific cache locations when they differ from repository paths.

Receipts may include digests and bounded labels but must not expose command environment values or private-key material.

## 17. Delivery slices

### M0: schema and deterministic inspection

- closed TOML schema and parser limits;
- status state machine;
- validation CLI/library;
- capability negotiation fields;
- manifest digest in workspace inspection.

### M1: agent onboarding contract

- initialization guidance that tells agents when onboarding is due;
- discovery-input inventory and local fingerprint records;
- high-confidence create/update workflow;
- `not_needed` reuse across equivalent worktrees;
- focused review-due reason codes.

### M2: execution and evidence binding

- `project_command_id` resolution;
- visible resolved argv/shell command;
- verification profile inspection;
- expected-output observation;
- evidence keys bound to manifest and toolchain fingerprints.

### M3: ecosystem refinements

- measured discovery adapters for additional build/task systems;
- migration tooling for future schema versions;
- editor or CLI presentation improvements;
- only those extensions justified by repository evidence and usage data.

## 18. Validation strategy

Tests must cover:

- valid, absent, malformed, oversized, unsupported, and conflicted manifests;
- unknown fields, path escapes, symlink escapes, duplicate IDs, missing references, and dependency cycles;
- argv/shell exclusivity and bounded command fields;
- no command execution during parse, status, discovery, or validation;
- branch changes selecting the correct manifest revision;
- review-due detection changing only for selected capability inputs;
- equivalent worktrees reusing current discovery results;
- local `not_needed` invalidation after relevant input change;
- preservation of existing comments and entries during focused updates;
- read-only tasks producing proposals without repository writes;
- exact manifest digest and command fingerprint in evidence;
- evidence becoming stale after command, manifest, source, toolchain, or expected-output changes;
- secret-shaped environment names never exposing values;
- missing/invalid manifests never blocking an explicitly supplied command.

## 19. Acceptance criteria

This design is complete when:

1. A new agent can determine manifest support and status without trial-and-error commands.
2. On first use of a nontrivial repository, the agent automatically audits capability evidence without waiting for the user to request a manifest by name.
3. A high-confidence manifest can be created during an authorized coding task without an unnecessary confirmation round trip.
4. A review-only task never modifies the repository.
5. The resulting file contains machine-readable capabilities and no copied prompt documentation.
6. A trivial repository can remain manifest-free through a reusable, fingerprinted local `not_needed` result.
7. Missing, invalid, unsupported, or review-due status never blocks normal command execution.
8. A declared affected-test or incremental-build path is preferred in the coding profile, while full verification remains available in broader profiles.
9. Evidence identifies the exact manifest revision, command, source, environment/toolchain, and required outputs that produced it.
10. Onboarding is reused across conversations and equivalent worktrees and reruns only when relevant evidence changes.
11. Repository files cannot cause ShellBeam to execute a command merely by being read or validated.
12. Local account, secret, and machine-specific data never enters the committed manifest.

## 20. Reference behavior

For an absent manifest in a repository with `go.mod`, a `Makefile`, and CI that invokes `make fmt-check`, `make test-dirty`, and `make build`:

1. ShellBeam reports `project.status=absent` during workspace inspection.
2. The agent recognizes onboarding is due and reads the small set of high-signal files.
3. It uses the existing Make targets, marks `make test` as an expensive full gate only if repository evidence supports that fact, and avoids inventing direct `go test` alternatives.
4. It creates a minimal `.shellbeam/project.toml` because repository edits are already authorized and the evidence is unambiguous.
5. ShellBeam validates the file and records the capability-input fingerprint locally.
6. The agent uses the coding verification profile for the edit loop and reserves full verification for the appropriate checkpoint.
7. A later conversation on an equivalent worktree loads the current manifest and evidence immediately.
8. If the Makefile or CI command mapping changes, status becomes `review_due`; coding continues while the agent performs a focused refresh.

This provides automatic onboarding without turning ShellBeam into a reasoning agent or the manifest into a prompt file.
