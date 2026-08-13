# ShellBeam Project Readiness and Typed Commands Design

## Status

Approved companion design for E25 Project Readiness and E28 Typed Parameterized Project Commands. This spec extends the [Agent Execution Observation Roadmap](./2026-08-14-agent-execution-observation-roadmap-design.md) and the existing [Project Capability Manifest and Agent Onboarding Design](./2026-08-13-project-capability-onboarding-design.md).

The feature set is intentionally declarative and deterministic. It must not grow into an installation/bootstrap system, task runner, or workflow language.

## 1. Decision

ShellBeam extends the project manifest with deterministic requirement declarations and a restricted typed-parameter contract for direct-argv project commands.

Project Readiness answers only whether declared prerequisites are observed as available/compatible under the current host/source view. It never installs, repairs, or starts anything.

Typed Project Commands let an agent bind validated scalar/project identifiers into canonical argv tokens. They execute exactly one command invocation and never imply dependency execution, conditionals, loops, matrices, dynamic shell interpolation, or hidden environment mutation.

## 2. Goals

- Let an attaching agent know early whether declared toolchains/executables/environment presence are available.
- Reuse manifest/toolchain/environment provenance already defined by the Agent Execution Layer.
- Prevent repeated failures caused by obvious missing/incompatible prerequisites when the project has already declared them.
- Preserve readiness as advisory observation rather than an execution gate.
- Let project manifests describe safe parameterized canonical direct-argv commands without requiring the agent to assemble shell strings.
- Bind exact manifest digest, parameter values, and resolved argv into operation/receipt provenance.
- Keep command binding small enough to audit mechanically and fuzz thoroughly.
- Preserve manifest v1 fixed-command behavior unchanged.

## 3. Non-goals

S3 does not add package/toolchain installation, dependency/service startup, environment repair, automatic bootstrap, external-service health probing, automatic command/profile selection, implicit dependency execution, shell templating, workflow expressions, secret-value capture, or dynamic command generation from prose.

## 4. Manifest versioning

Requirements and typed parameter definitions require Project Manifest schema v2.

- v2 readers accept valid v1 fixed commands unchanged.
- v1 readers encountering v2 report `unsupported` and preserve bytes.
- a newer daemon does not rewrite/upgrade a v1 manifest automatically.
- onboarding upgrades only when the capability has concrete benefit, source evidence is strong, and the active request permits repository edits.
- unknown extensions/newer versions are preserved rather than rewritten.

The manifest digest remains part of command/evidence provenance.

## 5. Requirements schema

Manifest v2 may declare a deliberately small requirements surface. Conceptually:

```toml
[requirements.toolchains.go]
version_source = "go.mod"

[requirements.executables.git]
required = true

[requirements.executables.docker]
required = false

[requirements.environment]
required_presence = ["DATABASE_URL"]
optional_presence = ["AWS_PROFILE"]
```

The exact TOML shape may be normalized during implementation planning, but it must preserve the semantics below and remain closed/versioned. Repository data cannot declare installation/fix/start commands as part of readiness.

## 6. Requirement kinds

Version 1 readiness supports:

- **Toolchain requirement:** references an existing manifest toolchain entry and its authoritative version/version-source semantics.
- **Executable presence:** checks whether a named executable can be resolved under the bounded effective execution environment.
- **Environment presence:** checks only presence/non-empty presence of declared variable names; values and hashes are forbidden.

No generic network/database/cloud service requirement exists in MVP.

## 7. Readiness result model

A readiness record binds repository/workspace, manifest digest, schema version, compatible environment/toolchain fingerprints, capture time, cache quality/age, and individual checks.

Top-level readiness states are exactly:

```text
ready
not_ready
partial
unavailable
```

`healthy/unhealthy` are avoided because they imply broader runtime/service claims.

## 8. Check status semantics

Per-check status is kind-specific but closed. Core concepts include:

```text
available
missing
compatible
incompatible
present
present_nonempty
absent
unknown
unavailable
```

`unknown` means ShellBeam identifies the requirement but cannot determine the comparison result. `unavailable` means the bounded observation itself cannot be performed/supported. An executable with unknown version is never mislabeled incompatible.

## 9. Deterministic top-level fold

- `ready`: every required check is satisfied and none is unknown/unavailable.
- `not_ready`: at least one required check is mechanically known missing/incompatible/absent.
- `partial`: none is known unsatisfied, but at least one required check is unknown/unavailable.
- `unavailable`: the manifest/readiness evaluation cannot establish a usable check set within the contract/budget.

Optional failures are exposed but do not make the project `not_ready`. This fold is versioned and contains no model reasoning.

## 10. Toolchain observation

Readiness reuses the existing toolchain/environment fingerprint rules: prefer authoritative repository `version_source`, use cached/bounded host probes outside warm admission, do not parse arbitrary human output as exact, and treat unsupported constraints/probe failures honestly. Manifest inspection cannot execute repository code merely to determine readiness.

## 11. Executable presence observation

Executable checks use the effective execution environment/path model already bound to ShellBeam execution. Presence does not imply version compatibility unless a separate toolchain check establishes it. Readiness never mutates PATH, installs shims, activates a version manager, or edits shell startup files.

## 12. Environment-presence observation

Persisted/public result records only `present`, `present_nonempty`, `absent`, or `unavailable`. Raw values are never stored, and hashing unknown values is forbidden. Repository configuration cannot opt a secret value into capture.

## 13. External-service boundary

Core readiness does not probe databases, Docker daemons, HTTP APIs, cloud accounts, clusters, or similar services. A project may define an explicit canonical `inspect` command; the reasoning agent chooses whether to run it. Readiness itself never auto-runs repository code.

## 14. Readiness caching

Readiness cache keys bind repository/workspace applicability, manifest digest, schema version, and compatible environment/toolchain fingerprints. Cache quality/age/TTL are explicit because host state can change independently of Git. Eviction has no effect on receipts, evidence, or idempotency.

## 15. Readiness is never an execution gate

An ordinary valid `local_shell start` may run even when readiness is `not_ready`, `partial`, `unavailable`, absent, or unsupported. This preserves the ability to run diagnostic or repair commands intentionally.

## 16. Typed parameterized project commands

Manifest v2 may parameterize direct-argv project commands using exact whole-token placeholders:

```toml
[commands.test_package]
argv = ["go", "test", "{package}"]

[commands.test_package.params.package]
kind = "repo_package"
required = true
```

The binder validates all parameters before spawn, resolves the exact argv, freezes the operation binding, and then passes the invocation to the existing direct-argv runtime. Parameterized shell-form commands are rejected.

## 17. Supported MVP parameter kinds

MVP supports exactly:

```text
string
enum
integer
repo_path
repo_package
```

Boolean token-shape semantics are deferred. No additional kind silently falls back to `string` when its validator/provider is unavailable.

## 18. Parameter-name and definition rules

Parameter IDs use the manifest's bounded stable identifier style. Each parameter definition declares only fields meaningful to its kind; unknown fields fail validation. Every argv placeholder must map to exactly one declared parameter. Caller-supplied undeclared params and duplicate/undefined placeholders fail before spawn.

## 19. Whole-token substitution only

In MVP a placeholder occupies an entire argv token:

```text
allowed:   "{package}"
rejected:  "./{package}/..."
rejected:  "--flag={value}"
rejected:  "prefix-{name}"
```

Each bound parameter produces exactly one argv token and never shell syntax. If a canonical executable expects a flag value, the fixed flag is its own token, for example `argv = ["go", "test", "-run", "{test_name}", "{package}"]`.

## 20. Parameter-kind semantics

### 20.1 `string`

A bounded UTF-8/NUL-free scalar producing one argv token. It has no path/package semantics.

### 20.2 `enum`

The value exactly matches one member of a bounded manifest-declared set and binds to that canonical member.

### 20.3 `integer`

A bounded integer validated numerically and serialized as canonical base-10.

### 20.4 `repo_path`

A repository-relative normalized path that cannot escape the bound workspace through `..` or followed symlink. The parameter schema declares whether existence is required rather than leaving the binder to guess.

### 20.5 `repo_package`

A provider-backed repository package/module identifier with language/toolchain-specific validation. Capability discovery reports providers. If the provider is unavailable, binding returns `parameter_validation_unavailable`; it never downgrades to raw string.

## 21. Defaults and optional parameters

MVP avoids dynamic argv shape:

- a placeholder param is required unless a literal validated scalar default is supplied;
- a default still creates the same one argv token;
- optional params cannot conditionally add/remove neighboring tokens;
- no flag-presence expansion exists;
- no computed/default expression exists.

Projects needing structurally different invocations define separate command IDs.

## 22. Shell-form boundary

Fixed shell commands remain valid under manifest v2. Parameterized shell commands are invalid. ShellBeam does not define quoting/interpolation rules for repository-controlled command templates.

## 23. Start request and binding

Conceptual call:

```json
{
  "action": "start",
  "operation_id": "op_...",
  "workspace_id": "ws_...",
  "project_command_id": "test_package",
  "params": {
    "package": "./internal/runtime"
  }
}
```

Binding order is deterministic:

1. resolve the originally bound workspace/repository source view;
2. load/validate exact active manifest version/digest;
3. resolve command by ID;
4. reject shell-form parameterization;
5. validate supplied parameter names/count/types;
6. bind whole argv tokens and command cwd;
7. freeze command/manifest/params/resolved-argv metadata;
8. compute fingerprints under existing idempotency rules;
9. admit/spawn through ordinary direct-argv semantics.

No process spawns before binding succeeds.

## 24. Receipt and provenance binding

Operation/receipt metadata records a bounded binding such as:

```json
{
  "project_command": {
    "id": "test_package",
    "manifest_digest": "...",
    "params": {
      "package": "./internal/runtime"
    },
    "resolved_argv": ["go", "test", "./internal/runtime"]
  }
}
```

The binding can be referenced by evidence, telemetry, and reproduction capsules. This parameter system is not intended for secret values.

## 25. Retry and manifest-change semantics

The first accepted operation freezes the binding. A retry with the same `operation_id` replays the original operation and never re-resolves a newer manifest/workspace into a different invocation. Conflicting retry metadata/params returns the existing operation-conflict semantics and never spawns again.

A manifest change after admission cannot mutate old resolved argv, evidence provenance, telemetry, or repro records. A new invocation under the new manifest requires a new `operation_id`.

## 26. Command graph boundary

Existing `depends_on` remains validation/presentation metadata. Calling a command never auto-executes dependencies. S3 has no execution-graph, step, matrix, retry-policy, or on-failure field. The agent may inspect the graph and explicitly issue calls.

## 27. Readiness and typed-command interaction

Readiness may mechanically expose which declared prerequisites are associated with project capabilities, but it never auto-selects, blocks, or runs a command. ShellBeam exposes facts/bindings; the reasoning agent chooses the strategy.

## 28. Capability discovery

Capability discovery exposes at least:

```text
project_readiness:
  version: 1
  requirement_kinds: [toolchain, executable, environment_presence]
typed_project_commands:
  version: 1
  manifest_version: 2
  parameter_kinds: [string, enum, integer, repo_path, repo_package]
  package_providers: [...]
```

Unsupported validators/kinds are explicit; clients do not learn support by invalid trial requests.

## 29. Stable failure/status additions

Readiness facts reserve `project_readiness_unavailable`, `project_requirement_invalid`, `toolchain_missing`, `toolchain_version_unknown`, and `toolchain_incompatible` where typed codes are needed.

Binding failures include:

- `project_command_not_found`;
- `project_command_not_parameterized`;
- `parameter_unknown`;
- `parameter_missing`;
- `parameter_invalid`;
- `parameter_kind_unsupported`;
- `parameter_validation_unavailable`;
- `project_command_binding_conflict`.

Most missing/incompatible prerequisite observations belong inside a successful readiness result rather than `ok=false`. Binding failures are request/tool failures because no valid command can be admitted.

## 30. Performance budget

Warm ordinary `start` that does not use a project command performs no readiness refresh. Project-command binding uses cached validated manifest state and bounded deterministic validation. Toolchain/executable probes are lazy/cached outside warm spawn. Package validators cannot execute arbitrary package managers/build systems on the spawn hot path; expensive validation must use an explicit bounded cache/inspection phase or report unavailable.

## 31. Security/privacy requirements

- Readiness never persists/exposes environment values.
- Parameter values obey ordinary command/receipt size/privacy rules; no secret parameter type exists.
- `repo_path` rejects workspace/symlink escape under the workspace contract.
- Project data cannot request installation or privilege-changing behavior through readiness metadata.
- Typed binding never invokes a shell to expand parameters.

## 32. Validation strategy

### 32.1 Manifest v1/v2 tests

- valid v1 fixed commands remain valid under v2 reader;
- v1 reader rejects/preserves v2 as unsupported;
- unknown field/version handling;
- parameterized shell rejection;
- undefined/duplicate placeholder rejection;
- invalid requirement syntax rejection;
- no automatic rewrite on read/inspect.

### 32.2 Readiness tests

- compatible/missing/incompatible/unknown/unavailable toolchain states;
- required/optional executable presence;
- environment presence/nonempty/absent without value exposure;
- deterministic top-level fold;
- cache quality/age/TTL;
- no project code/external service auto-execution;
- `not_ready` does not block arbitrary start;
- probe budget failure downgrades quality honestly.

### 32.3 Parameter-binder tests

- valid/invalid boundaries for every MVP kind;
- unknown/missing params rejected before spawn;
- whole-token substitution only;
- integer canonicalization and enum exactness;
- repo-path escape/symlink cases;
- package-provider unavailable/unsupported;
- no fallback to string;
- exact resolved-argv goldens;
- retry after manifest change reuses original binding;
- conflicting retry never respawns;
- fuzz/property tests for schema, binder, and path normalization.

### 32.4 Integration tests

A parameterized project command executes through ordinary direct-argv runtime and produces receipt/evidence/telemetry/repro provenance containing exact manifest digest and resolved binding.

## 33. Acceptance criteria

S3 is complete only when:

1. Readiness identifies compatible, missing, incompatible, unknown, and unavailable prerequisites mechanically without running project code.
2. Environment checks never expose or hash values.
3. `not_ready`/`partial`/`unavailable` never blocks an otherwise valid arbitrary start.
4. Typed argv binding is deterministic and occurs entirely before spawn.
5. Unknown/missing/invalid params fail before process creation.
6. Parameterized shell commands are schema-rejected.
7. MVP supports only the declared five parameter kinds and never silently downgrades unsupported providers/kinds.
8. `repo_path` cannot escape the workspace.
9. Retry never rebinds an existing operation after manifest/source change.
10. Receipt/evidence can reference exact manifest digest, command ID, params, and resolved argv.
11. `depends_on` cannot trigger implicit execution.
12. Existing v1 fixed commands continue unchanged.
13. No install/fix/bootstrap/start-service/workflow semantics exist in daemon readiness/binding logic.

## 34. Reference agent flow

```text
inspect manifest + readiness
  Go compatible
  required environment present
  optional tool missing
inspect test_package parameter schema
start project_command_id=test_package, package=./internal/runtime
ShellBeam validates and freezes ["go", "test", "./internal/runtime"]
ordinary execution runtime publishes authoritative receipt
receipt binds manifest + params + resolved argv
```

The agent decides what command to run and why. ShellBeam makes canonical command invocation mechanical rather than dependent on fragile shell-string assembly.
