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
- same-version unknown fields fail closed schema validation as `invalid`; preservation does **not** mean acceptance.
- a newer unsupported major or negotiated extension that the reader does not understand is `unsupported`, not partially interpreted.
- onboarding/writers preserve invalid/unsupported unknown bytes rather than stripping fields during an attempted rewrite.
- a newer daemon does not rewrite/upgrade a v1 manifest automatically.
- onboarding upgrades only when the capability has concrete benefit, source evidence is strong, and the active request permits repository edits.

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

A bounded UTF-8 argv token. It is not shell-interpolated. Because argv integrity does not imply CLI semantic safety, textual positional parameters default to rejecting a leading `-` unless the manifest explicitly sets a lexical policy allowing it.

### 20.2 `enum`

The value exactly matches one member of a bounded manifest-declared set and binds to that canonical member. An enum may intentionally contain option-like tokens because the allowed vocabulary is explicit.

### 20.3 `integer`

A bounded integer validated numerically and serialized as canonical base-10.

### 20.4 `repo_path`

A repository-relative token validated under the binder's **binding-time observation contract**: normalization, `..` rejection, declared existence rule, and observed symlink/workspace-root relationship are checked before spawn. The resulting provenance records source generation/path-observation quality.

This is address integrity at binding time, not filesystem confinement. A concurrent actor may change the path after validation, and the child executable ultimately decides how to interpret argv and what filesystem paths it opens. Runtime confinement requires a separate sandbox/FD-based provider contract and is not claimed by E28.

Path-like positional parameters reject leading `-` by default so a caller cannot accidentally turn a positional path into a CLI option. A manifest may explicitly opt into an option-like lexical policy when that is genuinely the canonical command contract.

### 20.5 `repo_package`

A provider-backed package/module identifier with language/toolchain-specific validation. Capability discovery reports providers. If the provider is unavailable, binding returns `parameter_validation_unavailable`; it never downgrades to raw string. Package-like values reject leading `-` unless the provider/manifest contract explicitly allows it.

The provider validates syntax/address membership; it does not execute arbitrary package managers/build systems on the hot binding path.

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

## 23. Start request, retry-first lookup, and first-admission binding

Conceptual first call remains:

```json
{
  "action": "start",
  "operation_id": "op_...",
  "workspace_id": "ws_...",
  "project_command_id": "test_package",
  "params": {"package": "./internal/runtime"}
}
```

The idempotency order is normative and matches the predecessor operation contract:

```text
receive request
  ↓
compute caller-stable typed_request_fingerprint
  ↓
lookup operation_id BEFORE workspace/manifest/provider reads
  ├─ exists
  │    compare caller-stable fingerprint
  │    replay frozen operation/binding or return operation_conflict
  │
  └─ absent
       durably reserve operation intent/fingerprint
       resolve logical workspace/cwd
       load/validate exact manifest
       resolve project command
       validate params/provider/lexical policies
       bind argv/cwd
       freeze execution + observation binding
       admit/spawn
```

`typed_request_fingerprint` contains only caller-stable intent: logical workspace address or exact absolute address where applicable, project command ID, canonical caller parameters/default semantics, TTY, timeout, and explicit execution options. It does **not** contain current manifest digest, provider-derived validation results, provider version, resolved cwd, or resolved argv.

Those resolved facts belong to the frozen first-admission execution/observation binding. Therefore a lost-response retry still replays even if the worktree moved, manifest was deleted/changed, or a package validator/provider was upgraded/unavailable after admission.

No process spawns before first-admission binding succeeds. Existing-operation replay does not need binding to succeed again.

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

## 25. Retry and manifest/provider-change semantics

The first accepted operation freezes the exact manifest digest, command definition, validator/provider identity where relevant, resolved cwd/argv, parameter binding, and observation metadata.

A retry with the same `operation_id` performs caller-intent fingerprint validation from persisted data and replays the original operation **without re-reading current workspace/manifest/provider state**. A conflicting caller fingerprint returns `operation_conflict` and never spawns again.

A manifest/source/provider upgrade after admission cannot mutate old resolved argv, evidence provenance, telemetry, code-intelligence associations, or repro records. A new invocation under new semantics requires a new `operation_id`.

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

Warm ordinary `start` that does not use a project command performs no readiness refresh or typed-command work.

For a project-command first admission with valid caches, binding targets **p95 <= 10 ms incremental local work**, performs zero network access and zero subprocesses, and uses bounded deterministic manifest/parameter validation. This is a capability-specific bound inside the umbrella global admission ceiling, not an additive budget granted to every feature.

Package validators cannot execute arbitrary package managers/build systems on the spawn hot path. Expensive validation uses an explicit cached/inspection phase or returns `parameter_validation_unavailable`. Existing-operation retry bypasses current manifest/provider resolution entirely.

## 31. Security/privacy requirements

- Readiness never persists/exposes environment values or deterministic hashes of unknown secret values.
- Parameter values obey ordinary command/receipt size/privacy rules; no secret parameter type exists.
- `repo_path`/`repo_package` provide bind-time lexical/address validation only and never claim sandbox confinement.
- `argv` binding eliminates shell-template injection but does not by itself make arbitrary CLI tokens semantically safe; positional parameter kinds default to rejecting option-shaped leading `-` tokens unless explicitly allowed.
- Project data cannot request installation or privilege-changing behavior through readiness metadata.
- Typed binding never invokes a shell to expand parameters.

## 32. Validation strategy

### 32.1 Manifest v1/v2 tests

- valid v1 fixed commands remain accepted unchanged;
- same-version unknown field is `invalid` while bytes are preserved by writers;
- unsupported newer major/extension is `unsupported` and preserved, never partially interpreted;
- onboarding does not auto-upgrade solely because a newer daemon exists.

### 32.2 Readiness tests

- compatible/missing/incompatible/unknown toolchain states are distinct;
- environment presence does not leak/hash values;
- cached freshness/TTL quality is explicit;
- readiness never blocks an otherwise valid arbitrary start and never auto-runs repository bootstrap/service code.

### 32.3 Parameter-binder/idempotency tests

- existing `operation_id` replay succeeds after workspace move, manifest deletion/change, and validator/provider disappearance;
- conflicting caller-stable typed request fingerprint fails before any current provider/workspace read can spawn;
- first admission freezes exact manifest/provider/resolved argv facts;
- parameterized shell commands are rejected;
- whole-token substitution only; unknown/missing/duplicate params reject before spawn;
- leading-dash lexical policy distinguishes positional identifier/path from intentional option-like enum;
- `repo_path` verifies binding-time root/symlink observation but tests explicitly prove the contract does not claim post-bind confinement;
- provider-backed package validation never silently downgrades to raw string;
- fuzz/property tests cover parameter decoding, defaults, canonical fingerprint serialization, path normalization, Unicode/size limits, and option-shaped inputs.

### 32.4 Integration/performance tests

- receipt/evidence/repro/telemetry references freeze the original manifest/command binding;
- `depends_on` remains presentation-only and never auto-executes;
- cached binding meets the p95 local-work target without subprocess/network access;
- ordinary non-project start performs no readiness/binding work.

## 33. Acceptance criteria

S3 core is complete only when:

1. Readiness reports deterministic prerequisite status without project repair/installation/service startup.
2. Environment checks never expose or hash values.
3. `not_ready`/`partial`/`unavailable` never blocks an otherwise valid arbitrary start.
4. Existing-operation lookup/replay occurs before current workspace/manifest/provider reads.
5. Lost-response retry replays the frozen typed command after manifest/provider/source change.
6. First-admission typed argv binding is deterministic and all invalid params fail before process creation.
7. Parameterized shell commands are schema-rejected and unsupported providers never downgrade silently.
8. `repo_path` is explicitly bind-time address integrity, not runtime confinement.
9. Positional token lexical policy prevents accidental option injection by default while allowing explicit manifest opt-in.
10. Receipt/evidence can reference exact manifest digest, command ID, parameter binding fingerprint, validator identity, and resolved argv.
11. `depends_on` cannot trigger implicit execution.
12. Existing v1 fixed commands continue unchanged; preservation of unknown bytes never means schema acceptance.
13. Cached binding meets the explicit budget and non-project starts pay no S3 tax.
14. No install/fix/bootstrap/start-service/workflow semantics exist in daemon readiness/binding logic.

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
