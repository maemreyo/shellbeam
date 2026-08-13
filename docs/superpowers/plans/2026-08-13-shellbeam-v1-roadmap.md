# ShellBeam V1 Implementation Roadmap

> **Execution rule:** Complete and self-review every plan in this roadmap before writing production code. Implement on local branch `ai/v1-implementation` with one primary agent, test-first, and no GitHub connector.

**Source of truth:** `docs/superpowers/specs/2026-08-13-shellbeam-v1-design.md`

**Deliverable:** A source ZIP that a macOS or Linux user can unpack, build, test, install as a per-user service, and connect to OpenAI Secure MCP Tunnel. Credentialed tunnel/ChatGPT checks are packaged as explicit user-run acceptance steps and are never reported as locally proven unless actually executed.

## Ordered checkpoints

| CP | Plan | Produces | Gate |
|---:|---|---|---|
| 1 | `2026-08-13-shellbeam-foundation-contracts.md` | Module, schemas, pure contracts, config, architecture gates, `devctl` | Foundation contracts green; no runtime |
| 2 | `2026-08-13-shellbeam-durable-store.md` | Secure file store, reservation-before-spawn, budgets, retention/tombstones | Persistence/fault tests green |
| 3 | `2026-08-13-shellbeam-process-runtime.md` | Process groups, pipes/PTY, capture, stdin, timeout, kill, finalization | Native runtime/race/fault tests green |
| 4 | `2026-08-13-shellbeam-daemon-ipc.md` | Daemon ownership, UDS IPC, peer UID, startup abandonment | Native daemon/IPC/security tests green |
| 5 | `2026-08-13-shellbeam-mcp-bridge.md` | Stateless stdio MCP bridge and one `local_shell` tool | Schema/SDK/Inspector-local tests green |
| 6 | `2026-08-13-shellbeam-service-cli-doctor.md` | CLI composition, launchd/systemd-user install, status/doctor | Native service contract and CLI E2E green |
| 7 | `2026-08-13-shellbeam-hardening-release-package.md` | Race/fuzz/fault/security gates, docs, release builds, ZIP | Exact-source release evidence and package checks green |

## Cross-checkpoint dependency direction

```text
cmd/shellbeam
  -> internal/app/daemon + internal/app/bridge
  -> internal/adapter/{mcp,ipc,process,store,service}

internal/app/*       -> internal/core/* + consumer-owned ports
internal/adapter/*   -> internal/app/* + internal/core/*
internal/core/*      -> standard library only
```

Adapters may depend inward; core never imports SDK, filesystem, PTY, Unix-socket, service-manager, or CLI packages. Sibling adapters communicate only through consumer-owned ports in `internal/app`.

## Design coverage map

| Design section | Owning proof |
|---|---|
| 4 architecture/trust boundaries | CP1 barriers; CP4 UDS/UID; CP5 stateless bridge; CP6 service |
| 5 MCP contract | CP1 checked-in schemas; CP5 SDK registration, metadata, closed input/output |
| 6 retry semantics | CP1 fingerprint/state; CP2 reservation; CP3 input/kill; CP4 lost-response integration |
| 7 state/result contract | CP1 pure transitions; CP2 durable terminal; CP3 finalization; CP4 abandonment |
| 8 output/quotas | CP2 budgets/cursor/storage; CP3 capture/drain/termination |
| 9 persistence/receipts | CP2 atomic store/tombstone; CP4 startup reconciliation |
| 10 process lifecycle | CP3 native runtime; CP4 daemon shutdown |
| 11 binary/service ownership | CP6 CLI, systemd-user, launchd |
| 12 configuration | CP1 typed defaults; CP6 loading/validation |
| 13 security model | CP2 secure paths; CP4 peer UID; CP6 redaction; CP7 adversarial gate |
| 14 stable failures | CP1 error schema; CP2–6 boundary mapping; CP7 acceptance trace |
| 15 testing strategy | Every checkpoint focused/dirty/race/fault; CP7 full campaign |
| 16 V1 acceptance criteria | CP7 machine-checked acceptance matrix |
| 19 development policy | CP1 baseline policy and every checkpoint gate |
| 20 technology baseline | CP1 toolchain/config dependency; CP3/5 pinned runtime dependencies |
| 21 repository/import barriers | CP1 architecture gate; corrected package ownership in CP2–6 |
| 22 size/clean code | CP1 size/waiver gate; enforced at every checkpoint |
| 23 developer commands | CP1 `devctl`; CP7 release/package subcommands |
| 24 incremental build | CP1 evidence/cache policy; CP7 only builds required tuples |
| 25 affected selection | CP1 selector; every checkpoint updates `test-impact.toml` |
| 26 test profiles | Focused RED/GREEN per task; checkpoint gates; CP7 release/nightly |
| 27 quality/security/operability | CP1 gates; CP6 diagnostics; CP7 hardening/security |
| 28 CI/release | CP1 checkpoint workflow definitions; CP7 nightly/release evidence |
| 29 AI workflow | One primary agent, task capsules, exact-source evidence |
| 30 Definition of Done | Each completion gate plus CP7 final claim rules |

## Frozen dependency set

- Go directive `1.26.0`, toolchain `go1.26.5`.
- `github.com/modelcontextprotocol/go-sdk` `v1.7.0`.
- `github.com/pelletier/go-toml/v2` `v2.3.1`.
- `github.com/creack/pty` `v1.1.24`.
- `github.com/oklog/ulid/v2` `v2.1.1`.
- `golang.org/x/sys` `v0.41.0` (the minimum selected by official MCP Go SDK v1.7.0's module graph; also used directly for peer credentials).
- Test-only `github.com/google/go-cmp` `v0.7.0`.

No other direct dependency may be added without an ADR that states why the standard library and existing dependencies are insufficient, which package imports it, and how it is removed or upgraded.

## Global implementation protocol

For every behavior:

1. Add one focused test that fails for the intended reason.
2. Run only that test and record RED output.
3. Add the minimum production behavior.
4. Re-run the focused test and record GREEN output.
5. Refactor without changing behavior.
6. Run `go run ./tools/devctl test --dirty --base main --json`.
7. Run `go run ./tools/devctl verify --checkpoint --base main --json` at the checkpoint boundary.
8. Commit only after evidence matches the current source fingerprint.

Do not implement a later checkpoint early. If a test needs a future adapter, use a small consumer-owned fake.

## Commit sequence

The documentation baseline is one commit on `main`. Implementation commits occur on `ai/v1-implementation` in the exact task order inside each plan. Commit messages are specified by each task; amend only the current task commit before proceeding. Do not push or open a pull request.

## Stop conditions

Stop execution and revise the relevant plan before code if any of these occurs:

- A public MCP, IPC, config, receipt, or persistence schema must change.
- Reservation-before-spawn cannot be proven under a tested fault.
- A platform API cannot support same-UID peer authentication.
- A process might be signaled using an identity reconstructed only from disk.
- Accepted stdin can be lost while a receipt still reports success.
- Terminal state can become visible before reap, drain, input accounting, and durable receipt publication.
- Disk/control reservations cannot bound state and preserve receipt headroom.
- A new direct dependency appears necessary.

## Platform evidence matrix

| Boundary | Linux build host | macOS build host | Packaged user-run |
|---|---:|---:|---:|
| Pure/core/store contracts | Required | Required in release CI | Included |
| Linux process/PTY/peer UID/systemd | Required | N/A | Included |
| macOS process/PTY/peer UID/launchd | Cross-build only on Linux | Required before public release | Included |
| Local stdio MCP/Inspector | Required when tools available | Required before public release | Included |
| Secure MCP Tunnel + ChatGPT credentialed path | Only with configured credentials | Only with configured credentials | Required acceptance checklist |

The final report labels every row `PASS`, `NOT RUN`, or `BLOCKED` with reason. Cross-compilation is never described as native runtime proof.

## Package contents

The final ZIP contains source, `go.mod`/`go.sum`, design, all seven plans, schemas, service templates, tests, `devctl`, README, security model, troubleshooting, user-run tunnel checklist, release evidence, dependency inventory, checksums, and license. It excludes `.git`, `.build/cache`, credentials, socket/state directories, raw command output, and locally installed service files.

## Roadmap completion gate

Planning is complete only when:

- every design acceptance criterion maps to at least one checkpoint task and test;
- every planned production file has an owning checkpoint;
- public/persisted versioned types are introduced before consumers;
- no task says “later”, “TBD”, “as needed”, or leaves an implementation choice open;
- local/no-remote execution works from the documentation baseline;
- credentialed and native-platform gaps are explicitly user-run rather than silently skipped.
