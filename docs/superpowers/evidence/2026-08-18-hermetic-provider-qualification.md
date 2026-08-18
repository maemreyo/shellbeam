# Hermetic Boundary V1 — Provider Qualification A0

**Status:** `PENDING_NATIVE` — no production implementation is authorized yet.

**ShellBeam base:** `9f658555d6b37a65da8b323ef4d7b1c963f157c7`

## Candidate decision

The preferred A0 candidate is **bubblewrap v0.11.2**, exact upstream commit
`1b80120ef26a28e065e67f89bfef873f13bdd317`, built in non-setuid mode.
This is not yet a PASS until the native Ubuntu qualification job succeeds.

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
| bubblewrap v0.11.2 | **candidate** | Small namespace/mount primitive; explicit `--ro-bind`, user/PID/network namespaces, `--clearenv`, `--die-with-parent`, nested-userns disable; maintained upstream; exact pin possible. |
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

Until a fresh native run is attached below, the decision remains `PENDING_NATIVE`.
