# Resource enforcement native acceptance

Resource Enforcement V1 is not considered Linux-ready from cross-compilation or unit tests alone. The hard acceptance gate is `scripts/test-resource-enforcement-native.sh` on a Linux cgroup-v2 host.

The script provisions a dedicated empty cgroup child for the current test user when `SHELLBEAM_RESOURCE_CGROUP_ROOT` is not already supplied, enables the `memory` and `pids` controllers for its children, proves the unprivileged user can create/configure/kill/remove a leaf, exports the root, and always attempts cleanup. Provisioning failure is a test failure. A non-Linux host reports `NOT_RUN` with a nonzero exit code; it is never counted as Linux evidence.

The native integration lane covers:

- shell, argv, and PTY children observing a `job-*` cgroup in their first `/proc/self/cgroup` read;
- a descendant process exceeding `memory.max`, with `memory.oom.group=1`, producing typed `resource_limit/memory` truth while preserving the root child's literal SIGKILL evidence;
- a descendant fork storm incrementing `pids.events:max`, followed by terminal whole-job teardown;
- an ordinary no-limit child creating no operation cgroup even while it is still running;
- startup reconciliation of a stale populated `job-*` cgroup and its live descendant;
- 100 bounded operations with zero residual `job-*` directories or populated delegated root;
- CPU-time and persistent-session hard-limit requests failing before spawn.

Evidence is written under `.build/resource-enforcement-native/`: bounded environment facts, JSONL Go test output, and a terminal `summary.json` with `PASS`, `FAIL`, or `NOT_RUN`. Raw configured cgroup paths are intentionally not copied into the summary.

For local macOS development, run the ordinary resource unit tests and Linux cross-compile checks. Those are regression/compile evidence only; they do not satisfy this native gate.
