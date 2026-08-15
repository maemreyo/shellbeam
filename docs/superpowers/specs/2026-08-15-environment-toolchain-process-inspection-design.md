# ShellBeam A2.5 Environment/Toolchain Fingerprint and Host Process Inspection Design

Status: approved design; self-reviewed against the live A2.4 branch before repository commit

Scope: A2.5 environment/toolchain fingerprint production + bounded host process inspection only

## 1. Purpose

ShellBeam already has durable execution receipts, workspace provenance, bounded retained output, structured results, and A2.4 mechanical evidence. The missing A2.5 capability is a bounded, lazy, secret-safe observation layer for:

1. reproducibility-relevant environment/toolchain identity; and
2. explicit host process inspection for ShellBeam-owned sessions or caller-selected local PIDs.

A2.5 must reduce follow-up commands such as `go version`, `node --version`, `python --version`, `ps`, and best-effort port inspection without adding mandatory work to ordinary compatible `local_shell` start/poll paths.

A2.5 is observational. It does not become a command scheduler, environment manager, process supervisor, package manager, or second execution authority.

## 2. Goals

A2.5 SHALL:

- expose versioned environment and toolchain fingerprints from normalized secret-safe facts;
- expose explicit capture quality, capture time, and compatibility version domains;
- probe supported toolchains lazily and under hard budgets;
- cache probe results without making cache presence an authority claim;
- never persist raw environment values or secret hashes;
- observe PATH only through a canonical digest and bounded metadata;
- allow manifest-selected environment-variable names for presence comparison only;
- expose bounded process-tree inspection for ShellBeam-owned sessions and explicit local PIDs;
- preserve PID/process identity honesty under exit, reuse, permission, and race conditions;
- keep optional listening-port observation best-effort and non-authoritative;
- integrate with A2.4 evidence as an optional trusted producer without mutating existing evidence records;
- remain inside the existing single `local_shell` public tool;
- preserve ordinary-command no-tax behavior.

## 3. Non-goals

A2.5 does NOT:

- dump or persist the effective environment;
- hash arbitrary environment values;
- inspect secret values from `/proc`, shell state, keychains, credential stores, or process environments;
- execute shell startup files merely to learn environment state;
- parse arbitrary command output heuristically to guess tool versions;
- install, upgrade, pin, or activate toolchains;
- create containers, virtual machines, sandboxes, or reproducible environments;
- supervise arbitrary host processes;
- kill, signal, pause, resume, or restart processes;
- infer ownership from process names;
- treat listening ports as execution correctness evidence;
- make environment/process observation mandatory for command admission;
- retroactively rewrite durable receipts or evidence;
- add a second MCP tool;
- introduce background polling/watchers solely for A2.5.

## 4. Authority model

A2.5 facts are mechanical observations.

They may enrich evidence, telemetry, reproduction, readiness, and diagnostics, but they never replace:

- durable execution receipts as child-execution truth;
- workspace/source authority;
- immutable A2.4 evidence records;
- ShellBeam session/process lifecycle authority.

Environment/toolchain observations have explicit capture quality and age. A compatible fingerprint means normalized facts matched under the same declared semantics; it does not prove hermetic reproducibility.

Process inspection reports what the operating system could establish at observation time. It must not turn absence of evidence into a stronger liveness/death claim.

## 5. Public capability surface

A2.5 stays within `local_shell`.

The public inspection surface SHALL expose two independent optional sub-capabilities:

```text
inspect.environment
inspect.process
```

Capability discovery SHALL advertise:

- feature presence;
- schema/fingerprint versions;
- hard record/byte/depth/count/time limits;
- supported built-in toolchain probes;
- whether best-effort port observation is supported on the current platform.

Older clients continue to work without requesting either capability.

## 6. Environment snapshot contract

A canonical environment snapshot is a bounded record conceptually containing:

```text
EnvironmentSnapshot
  schema_version
  snapshot_id
  captured_at
  quality
  environment_fingerprint
  fingerprint_version

  platform
    os
    architecture

  execution
    mode
    shell_identity?
    executable_identity?

  path
    digest
    entry_count

  variable_presence[]
    name
    present

  toolchain_manager?
    kind
    identity

  toolchains[]
    kind
    requested_identity
    observed_identity?
    version?
    quality
    diagnostic_code?
```

`quality` is one of:

```text
complete | partial | unavailable
```

The record contains no raw environment-variable values and no raw PATH list.

A snapshot may be partial because one or more optional probes are unavailable. A partial snapshot is still usable for the dimensions it actually observed.

## 7. Environment fingerprint

### 7.1 Canonical inputs

`environment_fingerprint` SHALL be a deterministic hash over a versioned canonical representation of secret-safe facts including only dimensions owned by the environment domain:

- OS;
- architecture;
- execution mode;
- effective shell or direct-exec identity;
- effective PATH digest;
- PATH entry count;
- selected environment-variable presence bitmap;
- declared toolchain-manager identity when available.

It SHALL NOT include:

- timestamp;
- cache age;
- diagnostic messages;
- raw environment values;
- hashes of arbitrary environment values;
- raw PATH entries;
- transient probe error text;
- process state.

### 7.2 PATH handling

A2.5 may inspect the effective PATH supplied to the child process, but public/persisted state contains only:

- a versioned digest over the canonical effective PATH representation;
- entry count;
- observation quality.

The raw PATH string/list is not persisted as A2.5 state.

### 7.3 Environment-variable handling

Only variable names selected by a trusted built-in policy or validated project manifest may participate.

For project-scoped observation, the existing validated manifest field `[environment].relevant_presence` / `Manifest.RelevantEnvironment` is the selection contract. A2.5 does not introduce a parallel manifest field. Manifest-v2 readiness requirements remain readiness policy unless a name is also selected by the environment fingerprint contract.

For each selected name A2.5 records presence only.

Examples:

```text
CI=true/false            -> NOT stored
CI present=true/false    -> allowed
TOKEN hash=...           -> forbidden
TOKEN present=true/false -> allowed when the name is explicitly selected
```

Unknown values are never hashed. This rule applies even when a value appears non-secret because low-entropy secrets may be recoverable from hashes.

## 8. Toolchain fingerprint and probes

### 8.1 Separate compatibility domain

Toolchains have a fingerprint version independent from the environment fingerprint version.

`toolchain_fingerprint` SHALL be deterministic over normalized successfully observed toolchain facts.

Environment and toolchain fingerprints remain separable so consumers can require one dimension without silently requiring the other.

### 8.2 Built-in probes

Initial supported built-in probe families may include:

- Go;
- Node.js;
- Python;
- Java;
- Rust.

A repository manifest may select additional declared toolchain identities only through the existing validated bounded `Manifest.Toolchains` contract (`version`, `version_source`, and optional `manager`). A2.5 does not add a parallel toolchain declaration schema.

### 8.3 Probe behavior

A probe:

- executes locally;
- is lazy;
- runs outside ordinary command admission;
- has an explicit timeout;
- has bounded output;
- uses a tool-specific parser;
- cannot execute arbitrary repository shell text merely to determine a version;
- does not fail the inspected command/session when unavailable.

Probe failure produces an unavailable/partial observation rather than a fabricated version.

A parser must recognize an explicitly supported version-output grammar. A2.5 must not heuristically scrape arbitrary output.

### 8.4 Cache

Toolchain probe results may be cached.

A cache entry includes at least:

```text
probe_identity
normalization_version
observed_identity
captured_at
quality
```

Cache use is explicit through the environment inspection request:

```text
freshness = cached | refresh
```

`cached` may reuse a compatible bounded entry. `refresh` attempts new probes within normal budgets.

Cache eviction does not alter receipts, evidence, idempotency, or historical immutable records.

A2.5 must not run refresh probes automatically on every command start.

## 9. Host process inspection

### 9.1 Supported targets

Process inspection accepts one of:

1. a ShellBeam session/process handle already known to the daemon; or
2. an explicit numeric local PID permitted by current-user OS authority.

It SHALL NOT select processes by executable/process name.

### 9.2 Result model

A bounded process observation conceptually contains:

```text
ProcessObservation
  schema_version
  observed_at
  quality
  target

  root
    pid
    parent_pid?
    process_identity?
    shellbeam_relation
    state
    start_time?
    executable_identity?
    argv_view?

  descendants[]
    pid
    parent_pid
    process_identity?
    state
    executable_identity?
    argv_view?

  ports[]?
    pid
    protocol
    local_endpoint_class
    port
    quality

  truncated
  diagnostic_codes[]
```

`process_identity` is present only when the platform can provide enough stable facts to distinguish the observed process from a later PID reuse within the supported contract.

If identity cannot be established, A2.5 reports lower quality rather than pretending PID alone is stable identity.

### 9.3 Lifecycle honesty

For ShellBeam-owned sessions, existing durable session/process authority remains primary.

Host inspection may enrich current state but must not contradict durable lifecycle facts by guesswork.

For arbitrary PID inspection:

- failure to observe because of permissions is not `not_found`;
- an exited/reused process race must not be represented as the originally requested process;
- PID reuse must be detected when supported identity facts disagree;
- observation after target disappearance may return a typed transient/unknown result.

### 9.4 Bounds

Process-tree traversal is bounded by advertised hard limits for:

- descendant count;
- traversal depth;
- response bytes;
- observation time;
- optional port records.

On bound exhaustion, return the bounded partial result with `truncated=true`; do not continue unbounded enumeration.

### 9.5 Privacy

Process inspection SHALL NOT read:

- child environment contents;
- source file contents;
- open-file contents;
- socket payloads;
- key material;
- credentials.

Executable/argv publication must follow the existing bounded local privacy policy. Sensitive host-specific absolute paths are redacted/classified where the existing public contract requires it.

## 10. Optional port observation

Listening-port observation is optional and platform-dependent.

It is:

- best-effort;
- bounded;
- local only;
- attached to process inspection;
- never execution/evidence authority.

Failure or lack of platform support yields an unavailable/partial port section without failing process inspection as a whole.

A2.5 does not scan arbitrary remote hosts or networks.

## 11. A2.4 evidence integration

A2.5 acts as an optional trusted producer for future evidence derivation.

New evidence records may bind compatible values such as:

```text
environment_fingerprint
toolchain_fingerprint
```

only when those values come from a compatible A2.5 observation available to the derivation path.

A2.5 does not:

- mutate existing A2.4 evidence records;
- reread current environment during retry and change the admitted execution contract;
- claim exact-current evidence merely because environment/toolchain fingerprints exist;
- synthesize exact source authority.

Evidence validity remains multidimensional. Exact claims require every required dimension to have exact compatible authority.

## 12. Integration with later capabilities

A2.5 is the canonical producer for environment/toolchain compatibility facts consumed by later or existing companion designs such as:

- Project Readiness;
- Execution Telemetry;
- Reproduction Capsules;
- evidence validity.

Consumers must bind the fingerprint version as part of compatibility.

They must not merge records across incompatible normalization versions.

Process inspection remains a separate observation surface and is not implicitly added to every telemetry/evidence record.

## 13. Durability and restart semantics

Environment/toolchain cache entries may be persisted or rebuilt according to the existing derived-cache policy.

Derived cache loss is not authoritative data loss.

Immutable historical consumers that already persisted a fingerprint continue to retain that fingerprint even if the cache entry is evicted.

Process liveness is never persisted as a claim that survives daemon restart.

After restart:

- durable ShellBeam session/receipt identity is recovered by existing mechanisms;
- a fresh explicit process inspection is required for current host state;
- previously observed arbitrary PID liveness is not replayed as current truth.

## 14. Failure taxonomy

A2.5 introduces typed failures/diagnostics with stable categories such as:

```text
environment_observation_unavailable
toolchain_probe_unavailable
toolchain_probe_timeout
toolchain_probe_unsupported
process_not_found
process_access_denied
process_identity_changed
process_observation_incomplete
process_limit_exceeded
port_observation_unavailable
```

Observation-specific failures do not rewrite child exit codes.

A multi-dimension environment snapshot may succeed partially when one optional probe fails.

Invalid public requests still fail through normal typed validation.

## 15. Performance requirements

### 15.1 Ordinary compatible execution

With A2.5 compiled and available but unused, warm ordinary compatible `start`/`poll` MUST NOT:

- execute tool version probes;
- enumerate the host process table;
- inspect ports;
- walk PATH entries beyond work already necessary for normal execution;
- persist A2.5 snapshot/cache records solely because a command ran;
- block on A2.5 cache refresh.

No A2.5 watcher/background loop is required.

### 15.2 Explicit observation

Explicit environment/toolchain/process inspection is requested work and carries hard budgets.

Probe timeout, traversal depth, record count, response bytes, and cache refresh behavior are advertised through capabilities.

## 16. Security requirements

A2.5 preserves monotonic privacy.

Forbidden persisted/public data includes:

- arbitrary raw environment values;
- hashes of arbitrary environment values;
- raw effective PATH;
- child process environments;
- secret manager/keychain contents;
- private-key material;
- socket payloads.

Tests SHALL include low-entropy sentinel secrets to prove they appear neither verbatim nor through prohibited direct hashing.

Toolchain probes execute fixed supported argv/probe adapters rather than arbitrary manifest shell fragments.

## 17. Package and responsibility boundaries

Implementation should preserve small independent units.

Recommended responsibility split:

```text
internal/core/environment/
    versioned snapshot/fingerprint contracts and normalization

internal/app/environment/
    capture orchestration, cache policy, toolchain probe coordination

internal/adapter/environment/
    OS/execution-context facts and built-in toolchain probes

internal/core/process/
    process observation contracts and identity semantics

internal/app/process/
    bounded process inspection orchestration

internal/adapter/process/
    platform process/port observation
```

The live repository already uses `internal/app/...` for application services and `internal/adapter/process` for current-daemon child ownership. A2.5 composes with those conventions: read-only process/port observation extends the existing process adapter boundary without replacing or broadening signal authority.

Project Readiness already contains bounded presence-only environment observation and a fixed bounded Go version probe. A2.5 should share or extract those mechanical host-probe primitives where semantics match, while keeping readiness-domain fingerprints distinct from A2.5 versioned fingerprints unless a consumer explicitly binds the A2.5 fingerprint version.

Environment and process observation SHALL remain separable modules with explicit interfaces.

Public IPC/MCP composition belongs in the existing one-tool transport path.

## 18. Acceptance criteria

A2.5 is complete only when all of the following are proven on fresh source bytes:

1. ordinary compatible `start`/`poll` executes no toolchain or process probes;
2. same normalized environment facts under the same fingerprint version produce the same fingerprint;
3. changed compatible environment facts change the fingerprint;
4. incompatible fingerprint versions are never compared as identical authority;
5. raw environment values do not appear in persisted state or public responses;
6. low-entropy secret values are not hashed into published/persisted identities;
7. raw PATH is not persisted by A2.5; digest/count semantics are deterministic;
8. built-in toolchain probes are lazy, bounded, and parsed only through supported adapters;
9. unavailable/timeout toolchain probes do not block command execution;
10. `cached` inspection can reuse compatible cache state without mandatory probes;
11. `refresh` performs bounded probe refresh without changing ordinary command admission;
12. process inspection accepts ShellBeam session identity or explicit PID, never name-based guessing;
13. process-tree traversal obeys hard count/depth/byte/time limits;
14. permission failure is distinguished from process absence;
15. PID reuse/identity change cannot be reported as the original observed process when identity evidence is available;
16. daemon restart does not replay stale process liveness as current truth;
17. optional port observation failure does not fail the base process observation;
18. A2.4 evidence integration can bind compatible environment/toolchain fingerprints for new records without mutating old evidence;
19. capability discovery advertises A2.5 features and exact hard limits;
20. one-tool `local_shell` architecture remains intact;
21. privacy/anti-goal scan proves no environment dump, secret hash, watcher, automatic rerun, package installation, or second MCP tool;
22. targeted tests, relevant race tests, full required race gate, module verification, `devctl check`, dirty affected/global test selection, staged diff checks, and commit gate all pass as required by the implementation plan;
23. real-daemon acceptance covers environment snapshot, toolchain cache/refresh, process inspection, restart behavior, no-tax behavior, and secret redaction;
24. final checkpoint records a fresh exact source fingerprint and verifies the same fingerprint post-commit.

## 19. Boundaries after A2.5

A2.5 intentionally does not implement:

- advisory mutation scopes (A2.6/B2);
- persistent named session ownership beyond existing runtime contracts (B1);
- containers/hermetic environment creation;
- automatic evidence reruns;
- semantic dependency tracing;
- remote execution/process inspection;
- package/toolchain installation;
- daemon-side reasoning.

A2.5 supplies bounded mechanical environment/toolchain/process facts so higher layers and the reasoning agent can make better decisions without paying mandatory hot-path cost.
