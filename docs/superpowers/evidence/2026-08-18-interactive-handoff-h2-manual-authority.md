# H2 Manual Human Authority — Darwin Completion Evidence

**Status:** H2 manual human↔agent authority transfer is complete for the approved Darwin/macOS experimental lane, subject to the final commit-gate for the exact staged tree containing this evidence.

```text
H3_ALLOWED=true
H4_ALLOWED=true
SECRET_HANDOFF_AVAILABLE=false
H2_ALLOWED_LINUX=false
```

H3 and H4 are independent successor slices. `H3_ALLOWED=true` does not imply H4 exists, and `H4_ALLOWED=true` does not imply H3 exists. The H2 tree itself still supports only `privacy=standard` + `completion.kind=manual_ready`.

## 1. Frozen prerequisite identity

- Master design spec commit: `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`
- H1 implementation HEAD: `62f9a364b3c319d776b0b480b7c86184b09eaa22`
- H1 attestation commit: `1fc7929fa8cf2ed15db9ea8a5400189a00002819`
- H1 evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md`
- H1 evidence SHA-256: `3cfa6ac8eba758a092f6f84517878cea2e05c76030cf0904fdbf597e12493ac1`
- H1 gate consumed by H2: `H2_ALLOWED=true`
- H2 prerequisite binding commit: `1b72507`
- H2 prerequisite evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-authority-binding.md`
- Last committed H2 implementation checkpoint before final acceptance: `ac8bc858023c5a9223a874e72e368f5cc6cd38c8` (`feat: recover manual human handoffs`)

The H2 implementation lineage after the H1 implementation checkpoint is:

```text
1fc7929 docs: attest delegated interactive core
1b72507 docs: bind h2 human authority prerequisites
4625eb1 feat: define interactive handoff state
fee44af docs: bind h2 handoff persistence semantics
8e5d002 feat: persist human handoff authority
05fcbfc feat: fence human delegated ingress
3701b95 docs: bind h2 handoff orchestration seams
0d3b573 docs: harden h2 handoff fence seams
d612b4b feat: arbitrate human agent handoff
1481952 docs: bind local h2 attach transport
88beab9 docs: fix detached h2 resume semantics
11e1a66 feat: add local interactive handoff attach
17eb778 feat: expose manual interactive handoff
ac8bc85 feat: recover manual human handoffs
```

## 2. Exact H0 provider binding

The implementation remains bound to the H0-qualified Darwin provider. No fallback provider or user tmux server is accepted.

```text
platform                  = darwin
provider_id               = tmux_control_mode
provider_version          = 1
control_adapter           = raw_control_mode
qualified_tmux            = /opt/homebrew/Cellar/tmux/3.6a/bin/tmux
tmux_version              = tmux 3.6a
tmux_sha256               = 70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef
observation_topology      = per_session_observer
H0_gate_sha256            = f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1
```

Darwin H0 P0–P15 are PASS. The H2-critical gates remain:

| Probe | Result | H2-bound fact |
|---|---|---|
| P2 | PASS | exact terminal client identity can be switched read-only/writable; missing or ambiguous identity rejects |
| P3 | PASS | acknowledged read-only transition fences all later markers on that same human-client stream; 1,000/1,000 measured; no application-quiescence claim |
| P7 | PASS | attach/switch/Control Mode reconnect with `-E` preserves delegated session environment |
| P8 | PASS | writable human client reaches shell-independent OOB control while foreground child owns pane stdin |
| P9 | PASS | read-only client can detach to local ShellBeam control; no pane-byte proxy is introduced |

Linux H0 remains `NOT_RUN`; therefore Linux H2 capability remains unavailable.

## 3. Qualified authority/control mechanisms

Exact human-client read-only/writable transition and fence are based on the qualified tmux client identity, not a pane/PID/TTY guess:

```text
tmux -S <private-socket> switch-client -E -c <exact-client-name> -r
input_fence_mechanism = tmux_same_client_switch-client_-E_-r_assume-paste-time_0
```

ShellBeam re-observes the exact client after mutation. The fence is an `IngressFenceProof`; it is not a shell-quiescence proof, `TransferBoundary`, or `PrivacyReleaseProof`.

Writable-state HumanControl is OOB on the ShellBeam-owned private tmux server:

```text
bind-key -n F10 wait-for -S <generation-bound-status-channel>
bind-key -n F11 wait-for -S <generation-bound-abort-channel>
bind-key -n F12 wait-for -S <generation-bound-ready-channel>
tmux -S <private-socket> -f /dev/null wait-for <private-channel>
```

Read-only/fenced local control uses a detach binding and the same-user ShellBeam local control surface. No HumanControl text is injected into pane stdin.

Human attach is presentation only. Qualified attach/switch/reconnect paths use tmux `-E`; the human terminal environment is not synchronized into the delegated session.

## 4. Native manual lifecycle acceptance

`tests/integration/interactive_handoff_manual_test.go` is a Darwin-only native test using the real qualified `delegatedtmux.Provider`, a real private tmux server, real PTYs, the production daemon handoff coordinator, durable store, public request projection, private bootstrap/bind semantics, H0 HumanControl, and terminal v5 receipt truth.

The test proves this sequence:

1. A delegated interactive shell starts agent-owned at authority epoch `1`.
2. `handoff.request` with `privacy=standard` and `manual_ready` durably rotates desired authority before human admission; public state returns `HUMAN_CONNECTING`, both ingress lanes fenced, and safe attach argv:
   `shellbeam session attach --handoff-id handoff-task9-manual-one`.
3. Private bootstrap returns the exact provider ref. A real PTY human client is attached read-only to that exact delegated shell and then bound through the daemon.
4. The first human-owned state is epoch `2`: agent ingress fenced, exact human ingress writable. A normal agent write is rejected with `session_control_not_owned` before provider mutation.
5. Human raw bytes `HUMAN_CANARY_TASK9` reach the same delegated shell. The shell reports its original `SHELLBEAM_H2_SENTINEL=session_value` even though the attach process has conflicting `SHELLBEAM_H2_SENTINEL=attach_value`; attach therefore does not overwrite the delegated environment.
6. Human bytes do not advance the agent input ledger: `NextInputOffset` remains `0`.
7. F12 produces OOB `Ready`; the daemon establishes the human-attested transfer boundary, fences the exact human client, and returns agent ownership at epoch `3`. The old human client is still present but read-only.
8. Duplicate Ready with the same control identity replays the same durable outcome without repeating authority mutation.
9. Agent input is accepted again at epoch `3`, and the same shell emits `H2_LINE:session_value:AGENT_AFTER_READY_TASK9`.
10. A second handoff rotates authority again and attaches a second exact client. The provider does not claim reusable-client semantics, so the test requires a distinct opaque client while re-proving the first client remains read-only.
11. A stale Ready carrying the prior handoff epoch is rejected with `stale_control_generation` and cannot complete the newer handoff.
12. F11 produces OOB Abort. Abort fences/revokes further human authority and reaches `ABORTED` without killing the delegated session; `Poll` still reports the session running.
13. Duplicate Abort is idempotent.
14. Only an explicit later `terminate` control ends the delegated shell.
15. The terminal receipt durably reports `input_authority_provenance=human_write_authority_granted` and counts only agent-input bytes in `input_accepted_bytes` / `input_delivered_bytes`.

The native acceptance was run three times on the final pre-evidence code:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./tests/integration -run '^TestInteractiveHandoffManualNativeAcceptance$' -count=3
PASS (7.594s)
```

## 5. OOB, privacy, environment, and metadata boundaries

The native test proves F11/F12 control sequences are absent from pane output after the handoff/abort cycle. Existing H0 native tests prove the foreground child does not receive the OOB control key.

The local CLI warning contract remains exact and is covered by `cmd/shellbeam/session_attach_test.go`:

```text
Model-visible output remains public; do not enter secrets here. Secret handoff is unavailable until the privacy capability is present.
```

The test deliberately does **not** assert that the terminal transcript hides the human canary. H2 standard handoff capture is public, so human-typed text may legitimately appear in terminal output.

Instead, the acceptance requires the canary to be absent from ShellBeam control/authority metadata:

- observation obligations / Event Journal metadata;
- `interactive-handoffs/` durable request/state/control/WAL files;
- delegated input-authority provenance metadata;
- the agent input ledger and terminal receipt input-byte accounting.

No provider/client/tmux private identifier is projected into the public handoff state beyond the safe local attach argv.

H2 policy rejects unsupported privacy/readiness before human writability:

```text
privacy=secret                         -> feature_unavailable(secret_handoff)
completion=environment_exported_nonempty -> feature_unavailable(automatic_handoff_completion)
```

Future vocabulary remains structurally representable in v2 schema where required for forward compatibility, but runtime `Request.ValidateH2()` and `State.ValidateH2()` remain the authority gate.

## 6. Receipt provenance blocker found by final native acceptance

Task 9 found one correctness blocker that earlier unit/fault matrices did not expose: delegated terminal receipt construction hardcoded `input_authority_provenance=agent_only` even after durable human writable authority had been granted.

The fix moves terminal provenance selection behind a dedicated daemon helper. For schema-v5 delegated terminal receipts:

- exact durable `agent_only` remains `agent_only`;
- exact durable `human_write_authority_granted` remains promoted;
- if the H2-capable provenance store cannot prove the value at terminalization, receipt truth fails conservatively to `human_write_authority_granted` rather than making the stronger false claim that only the agent could have supplied input;
- non-H2 stores continue to report `agent_only`.

Focused daemon tests lock that policy, and the real native test now observes `human_write_authority_granted` at terminal.

## 7. Restart, crash, client-loss, expiry, and Event Journal matrix

Task 8 checkpoint `ac8bc858023c5a9223a874e72e368f5cc6cd38c8` completed fail-closed recovery before final H2 acceptance.

Covered recovery facts include:

- `AGENT_FENCING` resumes exact agent-ingress fence proof after restart;
- `HUMAN_CONNECTING` with no client remains safe pending with both ingress lanes fenced;
- a durable read-only exact client remains safe pending;
- a writable exact client is promoted to `HUMAN_OWNED` only when durable `human_write_authority_granted` provenance is present;
- exact human-owned state survives restart when client/generation/provenance all re-prove;
- exact client disappearance while human-owned does **not** infer completion or agent ownership; it returns to fenced human-connect/reattach semantics;
- ambiguous client observation/proof loss blocks startup instead of fabricating a fence;
- `HUMAN_FENCING` resumes exact fence/read-only/reclaim ordering;
- provider generation/identity mismatch blocks reclaim;
- H1 startup may reattach transport continuity for desired `human|none` without granting agent authority; H2 then reconciles authority;
- provider/control observer loss never becomes a PID/name-based authority guess;
- expiry first durably revokes desired authority with a fresh epoch, then proves fences; expiry never implicitly kills the delegated session;
- one central expiry scheduler is used; there is no resident goroutine/ticker per handoff;
- lifecycle Event Journal events are metadata-only and exactly-once across normal replay, same-process WAL retry, and restart WAL settlement;
- handoff WAL transactions are settled before prepared Event Journal obligations during startup recovery.

Native daemon-crash acceptance proves a real delegated tmux session survives daemon restart through H1 reattach followed by H2 reconciliation while old agent writes remain fenced until authority is re-proven.

Task 8 pre-commit and hook commit-gates passed all affected suites. Task 8 commit is `ac8bc858023c5a9223a874e72e368f5cc6cd38c8`.

## 8. Fresh final H2 verification

Fresh normal lane:

```text
go mod verify
PASS: all modules verified

SHELLBEAM_H0_TMUX="$(command -v tmux)" go test \
  ./internal/core/interactivehandoff \
  ./internal/app/interactivehandoff \
  ./internal/app/delegatedsession \
  ./internal/adapter/delegatedtmux \
  ./internal/app/daemon \
  ./api/schema \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  ./cmd/shellbeam -count=1
PASS
  delegatedtmux  11.267s
  daemon        119.836s
  schema         21.008s
  ipc             8.046s
  mcp             1.452s
  cmd/shellbeam 295.649s
```

Fresh race lane:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race \
  ./internal/core/interactivehandoff \
  ./internal/app/interactivehandoff \
  ./internal/adapter/delegatedtmux \
  ./internal/app/daemon -count=1
PASS
  core/interactivehandoff       1.625s
  app/interactivehandoff        2.161s
  adapter/delegatedtmux         8.465s
  app/daemon                   88.435s
```

Static gate after the receipt-provenance fix/refactor:

```text
go run ./tools/devctl check
PASS
receipt: .build/receipts/20260819T135939.371144000Z-check.json
```

Fresh dirty impact gate, with base `33fe40999910a08410204993b9edb8f7e58698a5`:

```text
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
status=passed
exit_code=0
source_fingerprint=a39780d226c42d13f4d7d1b5f8bfd90ab794bb5b919bd4fa37b0fb150d79533c
started_at=2026-08-19T14:15:15.987351Z
finished_at=2026-08-19T14:22:04.196778Z
cmd/shellbeam=PASS (292.829s)
tests/integration=PASS (22.718s)
internal/app/daemon=PASS (90.777s)
```

`git diff --check` passed before this evidence file was written.

Anti-goal scan:

```text
rg -n 'preferred_terminal|capture-pane.*(loop|poll)|\.zshrc|\.bashrc|config\.fish' internal cmd api
# no matches
```

Future automatic-readiness vocabulary appears only in the v2 schema/forward-compatibility tests. Runtime rejection remains explicit in `internal/core/interactivehandoff/types.go` and `State.ValidateH2()`.

## 9. H2 completion-gate verdict

| # | Completion condition | Verdict |
|---|---|---|
| 1 | exact H1 delegated core/provider prerequisite bound | PASS |
| 2 | orthogonal canonical handoff state; projection grants no authority | PASS |
| 3 | accepted transfer intent rotates epoch before next-owner provider mutation | PASS |
| 4 | agent/human ingress never overlap under ShellBeam authority | PASS |
| 5 | exact H0 `FenceHumanIngress` used as ingress proof only | PASS |
| 6 | manual Ready creates human-attested boundary without secret-privacy fiction | PASS |
| 7 | exact human client attach is environment-preserving and only exact client becomes writable | PASS |
| 8 | writable and fenced/read-only HumanControl are pane-stdin independent | PASS |
| 9 | normal agent write/kill is fenced while human-owned | PASS |
| 10 | abort revokes/fences human authority without rollback or implicit kill | PASS |
| 11 | restart/client-loss/provider-loss recovery is fail-closed | PASS |
| 12 | wait/expiry are bounded; no resident per-handoff watcher | PASS |
| 13 | MCP remains one model tool; private local control is not a second model tool | PASS |
| 14 | no secret privacy, shell readiness, or automatic terminal launch advertised in H2; warning says output is public | PASS |
| 15 | possible human writable authority promotes durable terminal provenance without storing keystrokes | PASS |
| 16 | native/race/schema/privacy-metadata/devctl gates pass and successor gates are recorded | PASS |

Final successor gates for the Darwin experimental lane:

```text
H3_ALLOWED=true
H4_ALLOWED=true
SECRET_HANDOFF_AVAILABLE=false
H2_ALLOWED_LINUX=false
```

The final staged tree must still pass `git diff --cached --check` and `devctl commit-gate` before the H2 checkpoint commit is created. A post-commit clean-tree `devctl check` is also required.
