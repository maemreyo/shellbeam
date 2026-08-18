# Experimental hard resource enforcement

ShellBeam resource enforcement V1 is an **experimental Linux-only** execution boundary. It is disabled by default. macOS/Darwin does not advertise hard resource enforcement in V1.

## Operator configuration

Set `SHELLBEAM_RESOURCE_CGROUP_ROOT` to an absolute, clean path naming a dedicated cgroup-v2 directory delegated to the ShellBeam user. The configured root must itself contain no processes and must expose and enable the `memory` and `pids` controllers for child cgroups.

The operator/service manager must create one reserved direct child named `manager` and launch the ShellBeam daemon directly inside that cgroup before resource-provider qualification. The daemon PID must appear in `manager/cgroup.procs`; `manager` must not contain child cgroups. `job-*` and transient `probe-*` cgroups are siblings of `manager`. The provider never moves the daemon into `manager`, never kills/removes `manager`, and rejects unrelated sibling cgroups.

This topology is required for unprivileged atomic placement: the daemon and each target `job-*` leaf remain inside the same delegated containment boundary. At daemon startup ShellBeam verifies the root and reserved manager, checks the required control/event files, cleans only stale owned `job-*` siblings, and proves that a child can be born atomically into a fresh job cgroup. Qualification errors leave `resource_enforcement` unavailable; they are never converted into a sampled or best-effort hard-limit claim. Ordinary daemon operation remains available without resource enforcement.

A qualified V1 provider supports hard whole-owned-tree `memory_bytes` and `processes` limits for non-persistent protocol-v2 starts. `cpu_time_ms` and persistent-session hard limits are unsupported in V1. Requested unsupported limits fail before child spawn.

## Runtime semantics

Every bounded operation receives one fresh `job-*` child cgroup. ShellBeam configures `memory.max`/`memory.oom.group=1` and/or `pids.max`, then uses the kernel cgroup-FD spawn primitive so the child is born in the job cgroup; there is no post-spawn attach fallback. A local `pids.events:max` increment is terminal for that operation and causes the owned job cgroup to be killed. Memory classification comes from operation-local `memory.events` deltas.

Terminal receipts preserve the child's literal exit code or signal. A proven hard-limit breach is reported separately as a resource failure; ShellBeam does not manufacture exit code 137. Explicit user kill and timeout lifecycle decisions take precedence over resource classification when they were already armed.

After termination ShellBeam reads final resource events, kills the owned cgroup, proves it is unpopulated, and removes it. Internal cgroup paths are not part of public failure details.

When no hard limits are requested, child startup does not create, read, or poll per-operation cgroups. Such children simply inherit the daemon's operator-provided `manager` cgroup. Startup qualification itself occurs only when `SHELLBEAM_RESOURCE_CGROUP_ROOT` is configured.

Linux hard support is not considered release-ready merely because this configuration exists. The native Linux acceptance lane must also pass the repository's resource-enforcement test gate.
