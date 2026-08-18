# ShellBeam Hermetic Boundary Design

**Status:** Approved architecture; provider qualification required before implementation

**Execution base:** `37983a50f3775001873d9fe7cffc0d8e2675dc44`

## Decision to approve

Define a versioned **Hermetic Boundary provider contract** whose purpose is narrow: enable authoritative dependency/evidence scope for explicit verification executions by **enforcing** closed input channels. ShellBeam core owns identity, request validation, provenance, durable execution truth, bounded public metadata, and authority promotion. A qualified private provider owns the platform sandbox/container mechanics.

This is **not** a general container platform, remote runner, package manager, workflow language, or default execution mode.

## Key authority rule

E27 observation remains advisory by default. A trace never becomes authoritative merely because it looks complete.

`authority=authoritative` is legal only for an execution where the negotiated `hermetic_boundary` contract was established **before first child instruction** and remained valid for the complete owned tree. There is no `probably_authoritative` state.

## Alternatives considered

### A. Build OS-specific sandboxing directly into ShellBeam core — rejected

This would embed platform policy/mechanics into the canonical execution state machine, create a large maintenance/security surface, and tempt cross-platform semantic normalization that the roadmap explicitly forbids.

### B. Private qualified Hermetic Boundary provider behind one local_shell — **recommended**

Core exposes one capability/request contract. Providers may differ by platform but must prove the same versioned boundary semantics before advertising support.

Pros: reuse-first, replaceable provider, explicit platform capability, failure isolation, keeps ShellBeam focused on authority/evidence.

Cons: first platform may be Linux-only; provider qualification is mandatory.

### C. Build a full OCI/container orchestration subsystem — rejected

ShellBeam would duplicate container lifecycle/network/image tooling rather than provide the trustworthy execution envelope around an existing qualified provider.

## V1 scope: verification-only ephemeral execution

V1 deliberately does **not** run arbitrary mutating agent commands against the host workspace.

- non-persistent;
- non-PTY;
- closed stdin;
- network disabled;
- explicit bounded repo-relative input scope;
- host workspace never mutated;
- provider creates an ephemeral private execution view;
- all writes are to ephemeral/private scratch/output and are discarded after result capture;
- no arbitrary home-directory mounts;
- no secret environment values;
- no automatic dependency download.

This is enough for build/test/check style evidence while keeping the first authority boundary reviewable.

## Conceptual request

```json
{
  "hermetic": {
    "version": 1,
    "mode": "required",
    "repo_inputs": ["go.mod", "go.sum", "cmd/**", "internal/**", "tests/**"],
    "network": "off",
    "environment": "fixed_allowlist",
    "stdin": "closed",
    "writes": "ephemeral_discard"
  }
}
```

The hermetic contract is part of the operation request fingerprint. If the provider cannot establish it within the startup budget, the child **does not spawn**.

## What V1 must enclose

The provider may advertise `hermetic_boundary_v1` only if it enforces all of:

1. **Filesystem namespace constrained.** Only the captured declared repo inputs, fixed provider/toolchain roots, and documented ephemeral scratch/output are reachable.
2. **Undeclared filesystem access denied.** Host workspace/home/runtime paths are absent, not merely unobserved.
3. **Network disabled.** No declared-network mode in V1.
4. **Environment fixed.** No inherited ambient environment; exact public non-secret names/values come from a fixed provider policy/request allowlist.
5. **Toolchain/provider identity fixed.** Executables and runtime libraries come from the qualified provider/toolchain identity, not host `PATH` drift.
6. **Time/randomness policy explicit.** V1 may declare clock/randomness as `ambient_nondeterministic` inputs; this prevents false deterministic-replay claims while still closing undeclared filesystem/network/env channels.
7. **Complete owned child tree enclosed.** Descendants cannot escape the boundary or open broader host authority.
8. **Boundary established pre-exec.** No late-attach authoritative mode.
9. **Provider-control failure safe.** Kernel/runtime enforcement either survives provider control-plane loss for the child lifetime, or loss forces termination and the run cannot publish authoritative hermetic evidence.

## Immutable input capture cut

A live read-only bind of the mutable host worktree is insufficient: another process could modify a file between two reads.

V1 therefore captures the explicit bounded repo input set at one source-generation consistency cut into a provider-private immutable execution view before spawn. Rules mirror existing safety boundaries where applicable:

- normalized repo-relative paths only;
- `..` escape rejected;
- `.git` and ShellBeam state/runtime excluded;
- symlinks never become an escape to undeclared host content;
- special files unsupported;
- deterministic path/count/file/total-byte/work budgets;
- incomplete requested capture fails rather than truncates and claims hermeticity.

V1 does not accept arbitrary external/home paths. External dependency/toolchain bytes must be supplied by the qualified provider identity, not ad-hoc host mounts.

## Proven input scope

The first authoritative schema is intentionally conservative:

```text
proven_input_scope = declared immutable repo input set
                   + fixed provider/toolchain identity
                   + fixed environment policy
                   + stdin=closed
                   + network=off
                   + declared ambient time/randomness classes
```

E27 may still record actual observed reads inside the boundary, but V1 **does not use negative trace observations to narrow** the authoritative declared scope. A future separately reviewed version may allow authoritative actual-read narrowing only if trace completeness is itself enforced/proven under the boundary.

This preserves the roadmap rule: observation can broaden suspicion; only enforcement can justify narrowing.

## Evidence validity interaction

Only evidence produced under a valid completed `hermetic_boundary_v1` may use its `proven_input_scope` to establish that changes outside the declared immutable scope cannot affect that execution.

Hermetic evidence is not automatically deterministic or reproducible because time/randomness may be declared ambient. Reproduction and dependency authority remain separate claims.

## Provider Qualification Gate

No provider is accepted because a sandbox/container package merely exists. Before implementation freeze, the candidate must pass:

- maintenance/provenance;
- exact version pinning and upgrade stability;
- documented public security boundary;
- no implicit downloads/network on ordinary use;
- resource footprint and startup/warm latency;
- provider/browser/runtime crash isolation as applicable;
- cleanup/storage convergence;
- licensing;
- native platform proof for every advertised enclosure claim.

Initial recommendation: qualify a Linux provider first. **macOS remains unsupported** until a public, supported provider can prove the same V1 enclosure semantics; do not base authoritative evidence on deprecated/private sandbox behavior.

## Core/provider split

Core owns:

- capability negotiation and maturity;
- request fingerprint / exactly-once identity;
- workspace/source-generation binding;
- public `hermetic_boundary_id` and provider/version identity;
- durable terminal receipt;
- authority promotion to `proven_input_scope` only after provider completion proof;
- privacy/bounded status presentation;
- retention metadata and cleanup status.

Provider owns:

- immutable input materialization;
- filesystem/network/env/toolchain enclosure;
- ephemeral writable layer/scratch;
- complete child-tree containment;
- platform-native cleanup;
- private low-level manifests needed to prove completion.

Provider-specific mechanics do not become mandatory model-facing choreography.

## Failure semantics

Before spawn:

- provider absent/unsupported/budget failure => typed `hermetic_boundary_*` failure and zero child spawn.

After spawn:

- enclosure loss or unprovable continuity => execution may still have literal process truth, but **no authoritative hermetic evidence** is published;
- if provider semantics require termination on control/enclosure loss, preserve literal exit/signal evidence and separately report boundary failure;
- provider metadata failure never rewrites an already durable child exit.

## Privacy

- no arbitrary environment values/secrets in V1;
- captured source bytes stay provider-private except ordinary child output already governed by ShellBeam output policy;
- public metadata uses repo-relative path identities and bounded provider/toolchain IDs;
- no raw host absolute paths or provider-private sandbox paths in ordinary output;
- no deterministic identity for arbitrary excluded sensitive host bytes because they are not accepted into V1 scope.

## Resource / storage gate

A candidate provider must demonstrate:

- cold and warm startup cost;
- whole owned process-tree CPU/RSS/process count;
- immutable-input materialization bytes and latency at representative scopes;
- 1 and 2 concurrent sessions (screening only; V1 may still ship concurrency=1);
- 20+ command/evidence workload output pressure;
- 60s+ idle and repeated open/run/close cycles;
- normal/crash cleanup convergence;
- no cache/image download during ordinary hermetic execution;
- no host workspace mutation.

## Validation matrix

Native tests deliberately attempt to escape every claimed channel:

1. undeclared repo file read denied;
2. `..`, symlink, `/tmp`, home and ShellBeam state escape denied unless explicitly provider-internal;
3. network connect/DNS denied;
4. inherited secret env absent;
5. host `PATH` executable injection impossible;
6. child/grandchild cannot escape namespace/policy;
7. concurrent host modification after capture cannot alter sandbox input bytes;
8. provider kill/enclosure-loss path never publishes authoritative scope;
9. requested capture truncation/budget overflow prevents spawn;
10. writes do not mutate host worktree;
11. evidence only narrows against the declared proven scope after successful boundary completion;
12. E27 absence alone still cannot narrow a non-hermetic execution;
13. repeated native runs converge storage/processes after normal and crash exits.

## Explicit non-goals

- default execution mode;
- interactive shell/PTY in V1;
- persistent sessions in V1;
- arbitrary network allowlists in V1;
- arbitrary external/home mounts;
- secret env injection;
- applying sandbox writes back to host workspace;
- remote execution;
- OCI image builder/orchestrator;
- deterministic time/randomness in V1;
- using E27 negative observation as authority by itself.
