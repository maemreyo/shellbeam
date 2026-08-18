# ShellBeam Resource Enforcement Design

**Status:** Approved design; implementation authorized after plan review

**Execution base:** `37983a50f3775001873d9fe7cffc0d8e2675dc44`

## Decision to approve

Add an experimental **hard resource-enforcement** capability that is strictly separate from E23 observation. The first implementation is Linux-only and supports only limits that ShellBeam can prove at the complete owned process-tree boundary. macOS remains explicitly unsupported for hard tree enforcement until a public native primitive/provider passes its own qualification gate.

No request is silently downgraded from hard to sampled/best-effort enforcement.

## Why this shape

Existing E23 already freezes the public truth contract:

- absence of a limit keeps ordinary observe-only behavior;
- unsupported limits fail `resource_limit_unsupported` before spawn;
- enforcement and observation are separate;
- exit/signal evidence remains literal OS truth;
- ShellBeam must not synthesize exit 137 from a resource event.

The design should therefore prefer **narrow hard support** over broad approximate support.

## Alternatives considered

### A. Hard-only, platform capability-gated — **recommended**

Linux uses a delegated cgroup v2 subtree and race-free spawn into the cgroup. macOS advertises no hard tree limits initially.

Pros: honest semantics, inherited child containment, kernel-backed memory/process limits, no sampling race in the enforcement boundary.

Cons: feature is unavailable on macOS initially; Linux requires a writable/delegated cgroup v2 environment.

### B. Hard where available + sampled watchdog mode

Expose a second explicitly named `sampled_watchdog` quality for platforms lacking hard primitives.

Pros: more useful on macOS.

Cons: monitoring interval becomes correctness-relevant; burst overshoot is unavoidable; tree enumeration can race forks/exits; substantially larger contract surface.

**Decision:** defer. If added later, it must be a separate advertised enforcement quality and an explicit caller opt-in. It must never satisfy a `hard` request.

### C. Cross-platform sampler presented as hard enforcement — rejected

This violates the existing no-overclaim contract and creates false safety on the machine class where protection matters most.

## V1 public request

Keep the already-frozen conceptual shape and initially enable only the hard-supported fields:

```json
{
  "limits": {
    "memory_bytes": 4294967296,
    "processes": 256
  }
}
```

`cpu_time_ms` remains schema-reserved/experimental but returns `resource_limit_unsupported` for hard V1 until ShellBeam has a whole-owned-tree primitive matching the documented semantics. POSIX `RLIMIT_CPU` is not sufficient because it is per-process.

No `best_effort=true` or implicit downgrade flag exists in V1.

## Capability discovery

Conceptually:

```json
{
  "resource_enforcement": {
    "version": 1,
    "maturity": "experimental",
    "scope": "owned_process_tree",
    "placement": "pre_exec_atomic",
    "memory_bytes": "hard",
    "processes": "hard",
    "cpu_time_ms": "unsupported",
    "persistent_sessions": "unsupported",
    "provider": "linux_cgroup_v2"
  }
}
```

macOS V1 reports the same capability family with the three hard limits `unsupported`, not zeros and not sampled claims.


## Approved implementation refinement: dedicated delegated cgroup root

The Linux V1 provider does **not** silently reorganize the daemon process cgroup and does not assume `/sys/fs/cgroup` is writable. Hard enforcement is enabled only when the daemon is launched with an explicit provider-private configuration identifying a **dedicated delegated cgroup v2 root**. The initial environment binding is `SHELLBEAM_RESOURCE_CGROUP_ROOT`; it is daemon/operator configuration, not a per-command model-facing field.

**Native-gate correction (2026-08-18):** the first Ubuntu native run proved that an unprivileged daemon outside the delegated subtree cannot satisfy the kernel containment rule for atomic placement into a child `job-*` cgroup. Therefore the delegated root has one reserved direct child named `manager`. The operator/service manager must place the ShellBeam daemon directly in `manager` **before ShellBeam starts provider qualification**. `job-*` and transient `probe-*` cgroups are siblings of `manager`. ShellBeam itself never moves/reparents the daemon, never kills/removes `manager`, and never treats `manager` as an operation resource domain.

Qualification requires that the configured path:

- is absolute, resolves inside the cgroup v2 mount, and is not a symlink escape;
- is process-empty itself and dedicated to ShellBeam resource domains;
- contains the reserved direct `manager` cgroup, with the current daemon PID directly present in `manager/cgroup.procs`, and no child cgroups beneath `manager`;
- contains no unrelated sibling cgroups; at qualification entry only `manager` and stale/active `job-*` siblings are valid, while `probe-*` exists only transiently when created by the current qualification attempt;
- exposes `memory` and `pids` controllers to children;
- permits create/configure/kill/remove of an empty probe child;
- exposes `cgroup.kill`, `cgroup.events`, `memory.events`, and `pids.events`.

If the configuration is absent or qualification fails, `resource_enforcement` is unavailable and a request containing `limits` fails closed before child spawn. Ordinary requests with no limits remain unchanged and perform zero cgroup work.

## Linux provider mechanics

### Qualification precondition

Before admitting a request with limits, the provider proves all requested mechanics:

1. unified cgroup v2 is active;
2. ShellBeam has a writable/delegated subtree whose process-empty root contains the reserved `manager` cgroup;
3. the current daemon PID is a direct member of `manager`, so the daemon and target `job-*` leaves share the delegated root as their containment boundary;
4. required controllers are enabled (`memory`, `pids`);
5. the provider can create an operation-owned leaf cgroup and prove writable `memory.max`, `memory.swap.max`, `memory.oom.group`, and `pids.max` controls;
6. `clone3(CLONE_INTO_CGROUP)` is available through Go's `syscall.SysProcAttr{UseCgroupFD:true,CgroupFD:...}` path;
7. event files needed for final classification are readable;
8. cleanup primitives are usable.

Any missing precondition returns `resource_limit_unsupported` **before child spawn**.

### Operation-owned resource domain

Each admitted non-persistent operation gets one random/opaque `job-*` cgroup directly under the delegated root, as a sibling of the reserved `manager` cgroup. The cgroup name is internal correctness machinery and is not model-facing. Ordinary no-limit children inherit `manager`; the provider performs no per-operation cgroup work for them.

Configure before spawn:

- `memory.max = memory_bytes` when requested;
- `memory.swap.max = 0` for memory-limited workloads so anonymous memory cannot escape the V1 RAM budget into per-job swap;
- `memory.oom.group = 1` for memory-limited workloads so an OOM event cannot intentionally leave a partially surviving job tree;
- `pids.max = processes` when requested.

Then open the cgroup directory and spawn the child directly into it using `UseCgroupFD/CgroupFD`. There is no post-spawn attach fallback for a hard request.

### Process-limit breach policy

`pids.max` rejects a fork/clone that would exceed the limit. V1 treats any increment of the operation's `pids.events:max` during the request as a resource-limit breach. Once observed, ShellBeam terminates the owned cgroup and publishes a resource-limit terminal cause even if the child could otherwise recover from `EAGAIN`. This makes the contract deterministic: **attempting to exceed the declared process budget is terminal**.

### Memory-limit breach policy

Finalization compares operation-local memory event counters from the pre-spawn baseline. A cgroup OOM / `memory.events` limit event caused by the operation produces:

```text
termination_cause = resource_limit
resource_limit_kind = memory
```

Literal signal/exit evidence is still reported separately. No synthetic exit code is inserted.

### Kill and cleanup

Terminalization order:

1. preserve literal child wait/signal evidence;
2. read final cgroup event/peak counters under a bounded finalization budget;
3. if the job tree remains populated, use the cgroup kill primitive or existing owned-tree termination path;
4. prove `cgroup.events:populated=0` or report cleanup-incomplete metadata;
5. remove the operation cgroup;
6. never let optional telemetry persistence delay terminal receipt publication indefinitely.

A leaked enforcement cgroup is a cleanup/recovery defect, not permission to rewrite an already-durable child receipt.

## Persistent sessions

**Unsupported in V1.** Persistent sessions have supervisor/restart/reconciliation lifetime semantics that require a separately reviewed resource-domain lifetime design. Returning unsupported is safer than binding a cgroup lifetime incorrectly across daemon/supervisor recovery.

## Failure taxonomy

Reuse/reserve:

- `resource_limit_unsupported` — requested hard semantics cannot be established pre-spawn;
- typed terminal cause `resource_limit` + `resource_limit_kind=memory|processes`;
- cleanup status is separate from child outcome and resource breach truth.

Do not add a second MCP tool.

## Privacy / storage / performance

- no raw cgroup paths are necessary in ordinary model-facing responses;
- ordinary starts with no `limits` perform zero cgroup provider work;
- capability probing is cached per daemon/platform generation and invalidated on provider failure/config changes;
- operation cgroup state is kernel/runtime state, not durable telemetry history;
- only bounded enforcement metadata joins receipts/telemetry.

## Native validation gate before implementation may be called complete

Linux native evidence must prove:

1. unsupported/delegation failure prevents spawn;
2. memory limit applies to descendants, not only the root PID;
3. process limit blocks descendant fork storms and ShellBeam terminates on first breach event;
4. child is born inside the operation cgroup (no pre-attach escape window);
5. memory breach reports typed cause while preserving literal signal/exit evidence;
6. no-limit start takes the old zero-provider-work path;
7. provider/daemon crash recovery leaves no live owned descendants/cgroup leak after reconciliation;
8. repeated 100+ bounded runs show no cgroup/storage creep;
9. native Linux tests pass; cross-build alone cannot claim support.

## Explicit non-goals

- CPU scheduling / command planning;
- automatic tuning based on telemetry;
- containerization;
- macOS sampled watchdog masquerading as hard enforcement;
- IO bandwidth limits in V1;
- persistent-session enforcement in V1;
- changing ordinary timeout semantics.
