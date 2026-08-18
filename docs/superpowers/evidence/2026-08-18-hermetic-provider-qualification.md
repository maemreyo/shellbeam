# Hermetic Boundary V1 — Provider Qualification A0

**Status:** `PASS_FOR_TASKS_1_PLUS` — Provider Qualification Gate passed on native Linux.

Tasks 1+ may implement only the frozen provider/topology/qualification rules below;
this does **not** make Hermetic Boundary V1 production-ready before Tasks 1–8 pass.

**ShellBeam base:** `9f658555d6b37a65da8b323ef4d7b1c963f157c7`

## Candidate decision

The selected V1 provider primitive is **bubblewrap v0.11.2**, exact upstream
commit `1b80120ef26a28e065e67f89bfef873f13bdd317`, built in non-setuid mode.

The freeze is intentionally stronger than a package/version string: production
qualification must also bind the exact provider executable bytes, its dynamic
runtime manifest, the platform security-policy prerequisite (for example the
Ubuntu AppArmor profile when that restriction is active), and a content-addressed
toolchain root. Identity drift fails qualification instead of silently changing
hermetic semantics.

Primary provenance:

- upstream: https://github.com/containers/bubblewrap
- release/tag: https://github.com/containers/bubblewrap/releases/tag/v0.11.2
- source pin: https://github.com/containers/bubblewrap/commit/1b80120ef26a28e065e67f89bfef873f13bdd317
- license: LGPL-2.0-or-later in upstream source headers

Bubblewrap is treated as a low-level sandbox primitive, not as a security policy by
itself. ShellBeam must freeze and validate the exact invocation topology. Upstream
itself describes bubblewrap as a toolkit whose security boundary depends on the
arguments selected by its caller.

## Candidate comparison

| Candidate | Result | Reason |
|---|---|---|
| bubblewrap v0.11.2 | **selected / qualified** | Small namespace/mount primitive; explicit `--ro-bind`, user/PID/network namespaces, `--clearenv`, `--die-with-parent`, nested-userns disable; maintained upstream; exact pin possible. |
| NsJail 3.6 | rejected for V1 | Capable and maintained, but adds a materially larger supervisor/config/seccomp/cgroup dependency surface (including Kafel/protobuf) than V1 needs. |
| systemd-nspawn | rejected for V1 | Strong container tooling, but brings image/container lifecycle and privilege/helper semantics beyond the narrow provider contract. |
| Landlock alone | rejected as standalone | Useful unprivileged kernel defense, but does not provide the full mount/PID/toolchain/private-root topology; using it alone would make ShellBeam build substantially more sandbox-manager code. |

Comparator sources:

- https://github.com/google/nsjail
- https://www.freedesktop.org/software/systemd/man/latest/systemd-nspawn.html
- https://docs.kernel.org/userspace-api/landlock.html

## Frozen candidate topology under test

The A0 topology intentionally does **not** bind live host `/usr`, `/etc`, home,
workspace, `/proc`, `/sys`, or `/run` into the sandbox.

Before first child instruction, qualification materializes two private trees:

1. an immutable explicit repo-input capture; and
2. a **content-addressed toolchain root** containing only the declared executable
   profile, its runtime libraries, and frozen minimal `/etc` files.

Ordinary execution then invokes bubblewrap with:

- explicit user namespace plus all other relevant namespaces unshared;
- PID and network namespace isolation;
- `--die-with-parent`;
- `--disable-userns` + `--assert-userns-disabled` inside the sandbox;
- the content-addressed toolchain tree read-only at `/`;
- declared captured inputs read-only at `/work/input`;
- one provider-private writable `/work/scratch`;
- private `/dev` and tmpfs `/tmp`;
- no `/proc`, `/sys`, `/run`, host home, live host workspace, or live host toolchain mount;
- `--clearenv` followed by an exact public allowlist;
- stdin closed;
- no network sharing.

The bubblewrap executable itself is exact-version pinned. Its binary SHA-256,
dynamic-runtime manifest SHA-256, kernel identity, and toolchain-manifest SHA-256
are recorded as provider/runtime identity evidence. A future production adapter
must fail closed on identity drift rather than silently accepting host library or
toolchain changes.

## Local Linux screening — not the hard PASS gate

A privileged Docker Desktop Linux VM was used only as a kernel screening harness,
not as the selected runtime/provider. Linux kernel observed: `6.12.76-linuxkit`
(aarch64).

Exact locally built provider evidence:

- bubblewrap: `0.11.2`
- upstream commit: `1b80120ef26a28e065e67f89bfef873f13bdd317`
- local aarch64 binary SHA-256: `197c2587972c0f66ecfa4afb1bb101d7a524e80c6797a0f02740241cacc9137b`
- non-setuid mode: `0755`

The corrected fixed-toolchain campaign completed **26/26** checks with no failure.
Representative measurements:

- content-addressed A0 toolchain tree: ~21.6 MB;
- cold sandbox start: ~8.7 ms;
- warm average over 50 starts: ~3.6 ms;
- 60-second idle: stable at 3 processes / ~4.0 MiB RSS;
- 20-command output-pressure campaign: PASS;
- two concurrent sessions: PASS; separate screening measured roughly 8 processes /
  11.1 MiB RSS total;
- 100 repeated lifecycles: no provider process residue;
- provider SIGKILL with `--die-with-parent` + PID namespace: exact marked descendant
  converged to zero;
- outer-network-disabled screening: ordinary pre-provisioned sandbox start still
  succeeded and DNS remained unavailable.

The adversarial matrix also passed declared-read, undeclared-read denial, absolute
symlink escape denial, `..` escape denial, live home/runtime absence, read-only
captured input, private scratch, concurrent host mutation isolation, exact env,
secret-env absence, host-PATH injection denial, closed stdin, direct network denial,
DNS denial, descendant escape denial, host-source non-mutation, idle cleanup and
final process convergence.

These measurements are screening only because the outer Docker harness required
privileged nesting to permit namespace creation. They cannot authorize Tasks 1+.

## Native Linux hard gate

Tracked test: `scripts/test-hermetic-provider-a0.sh`

Tracked workflow: `.github/workflows/hermetic-provider-a0.yml`

The native job must run as the ordinary unprivileged GitHub runner user and must:

1. clone only the official upstream repository during **qualification-time**;
2. build exact tag `v0.11.2` and verify exact commit
   `1b80120ef26a28e065e67f89bfef873f13bdd317`;
3. verify the provider binary is not setuid/setgid;
4. prove ordinary pre-provisioned starts make no AF_INET/AF_INET6 syscalls from the
   provider path (`strace` gate);
5. pass filesystem/network/env/toolchain/descendant/crash/mutation attacks;
6. measure cold/warm, 1-session idle and 2-session footprint;
7. pass 20-command pressure, 100 lifecycle convergence, 60-second idle and
   provider-private storage cleanup;
8. end with `hermetic_provider_a0 verdict=PASS reason=native_linux_provider_qualified exit=0`.

Qualification-time source retrieval/build is deliberately separate from ordinary
hermetic execution. **Ordinary execution is not permitted to download a provider,
toolchain, dependency, or source input.**

## Decision rule

- Native gate PASS + review of bounded artifacts => freeze bubblewrap v0.11.2 and
  the topology above for Tasks 1+.
- Any failed enclosure/continuity/resource/cleanup assertion => `FAIL`; do not
  implement Tasks 1+.
- Native gate unavailable => `NOT_RUN`; do not implement Tasks 1+.

### Native run 1 finding — targeted LSM prerequisite discovered

PR #11 native run `32100496656` built the exact upstream provider successfully,
but the first sandbox smoke failed on the ordinary Ubuntu runner with
`bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`. This is a hard
`FAIL`, not provider evidence.

The failure matches Ubuntu/AppArmor unprivileged-user-namespace restrictions:
bubblewrap needs temporary namespace capabilities during setup (including
`CAP_NET_ADMIN` for loopback), while the sandbox child must not retain them. The
next qualification attempt therefore uses the targeted distro-provided
`bwrap-userns-restrict` profile bound to `/usr/bin/bwrap` and installs the exact
A0 binary at that path. It does **not** disable AppArmor, change the global
user-namespace sysctl, or run ordinary sandbox commands as root. The AppArmor
package/profile version and SHA-256 are evidence inputs and must be requalified
on drift.

### Native run 2 — PASS and provider freeze

PR #11 native run **`32100753535`**, job **`95600634793`**, on exact branch
HEAD `fe273f1f30b32bedf922815cbea85a67121c149d` passed the complete A0 job.
The matching standard checkpoint run **`32100753539`** also passed both Ubuntu
and macOS verification jobs before this evidence-only freeze commit.

Native Ubuntu identity/evidence:

- runner: `Linux 6.17.0-1022-azure x86_64`;
- upstream source tag: `v0.11.2`;
- upstream source commit: `1b80120ef26a28e065e67f89bfef873f13bdd317`;
- exact x86_64 A0 provider binary SHA-256:
  `cc9208e457f442d7e2202c37e3530f892386549e59d98c0922f87115ac787889`;
- provider mode: `0755`, non-setuid/non-setgid;
- provider dynamic-runtime manifest SHA-256:
  `f4ef35ff50800fb0581bc9d2efd743197dc781cb5f12e0bdacde63b55d637750`;
- AppArmor unprivileged-userns restriction: `1`;
- AppArmor package/profile package:
  `4.0.1really4.0.1-0ubuntu0.24.04.7`;
- loaded targeted `bwrap-userns-restrict` profile SHA-256:
  `11d39094f044f0cda0febb3ad517b830301da6b2ce929664af09ee9e4dd264f9`;
- A0 content-addressed toolchain manifest SHA-256:
  `d6286677d79e6964f101aeea073a2b71d1af93193dc3d93022ad380006830cf6`;
- A0 toolchain snapshot bytes: `22,483,680`.

The toolchain hash above proves the qualification mechanism on this runner; it is
**not** a universal shipping toolchain hash. Production V1 must use an explicitly
qualified content-addressed toolchain identity and fail closed if those bytes
change. The same rule applies to the bubblewrap dynamic-runtime manifest and any
active platform security profile.

Native adversarial/resource result: **29 PASS / 0 FAIL**. In particular:

- ordinary provider path under `strace` opened no `AF_INET`/`AF_INET6` socket;
  only local `AF_UNIX` and route `AF_NETLINK` setup traffic was observed;
- declared input read succeeded while undeclared path, symlink and `..` escapes
  were denied;
- `/proc`, `/sys`, `/run`, host home and host PATH injection were absent;
- environment was exactly the fixed allowlist; inherited secret env was absent;
- stdin was closed; network connect and DNS were denied;
- descendant escape was denied and provider SIGKILL converged the exact marked
  descendant to zero;
- immutable capture was unaffected by concurrent host mutation and sandbox writes
  did not mutate host source;
- 20-command output pressure and 100 repeated lifecycles passed with no provider
  process residue;
- cold start: `7,048 µs`; warm average over 50 starts: `5,515 µs`;
- two concurrent sandboxes: `8` processes / `14,236 KiB` RSS total;
- 60-second idle remained stable at `3` processes / `4,940 KiB` RSS at t=1,30,59s;
- provider-private fixture before cleanup: `22,483,791` bytes; after cleanup only
  bounded A0 evidence remained (`12,517` bytes);
- terminal marker:
  `hermetic_provider_a0 verdict=PASS reason=native_linux_provider_qualified exit=0`.

Uploaded bounded evidence artifact: ID **`9311480848`**, artifact SHA-256
`e86d0eff16509a9cb3e8b3bc6f41bd71cf00503c87470a6ce2d01acb30e86424`.

### Frozen Task0 decision

**PASS.** Tasks 1+ are authorized to target **bubblewrap v0.11.2** only through
the frozen topology and a fail-closed private provider qualification step. This
PASS does not authorize:

- fallback to setuid bubblewrap;
- disabling AppArmor or the global unprivileged-userns restriction;
- live host `/usr`, `/etc`, workspace, home, `/proc`, `/sys` or `/run` mounts;
- ambient environment, network, stdin, or host PATH;
- runtime provider/toolchain/source downloads;
- authority publication if provider/runtime/toolchain/security-policy identity or
  continuity cannot be proven.

macOS remains unsupported for authoritative Hermetic Boundary V1.
