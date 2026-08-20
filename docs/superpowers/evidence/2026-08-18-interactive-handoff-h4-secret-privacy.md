# H4 Secret Handoff Privacy, Shell Readiness, and Native Acceptance

**Status:** H4 is complete for the H0-qualified Darwin/macOS lane. Linux privacy remains `NOT_RUN`, unadvertised, and fail-closed. H3 terminal presentation remains an independent optional composition layer.

```text
H4_COMPLETE=true
H5_DESIGN_ALLOWED=true
CONTEXT_EXEC_AVAILABLE=false
H4_ALLOWED_DARWIN=true
H4_ALLOWED_LINUX=false
H4_PRIVACY_TOPOLOGY=per_session_observer
H4_PRIVATE_FROM_FIRST_BYTE=true
H4_HISTORY_REPLAY_ALLOWED=false
```

## 1. Frozen prerequisites and implementation lineage

- Master design spec: `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md`
- Frozen spec commit: `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`
- H0 machine evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- H0 machine evidence SHA-256: `f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1`
- H0 Markdown evidence SHA-256: `a3d8dc9c518572660436cbdc7b604debd054e616a7834080066ef6eaa24f3a28`
- H2 authority evidence SHA-256: `6787b0ba1835ceaba43c05202226619c039c9738e3fd35d5494e00203dfbea7d`
- H3 presentation evidence SHA-256: `408af4c2726c60da40c65861e68e8443689a5225f03f5b7c40af5521a3639aaf`
- H4 privacy-binding evidence SHA-256: `800cea4338d96781e4aa22785cf046e59e93c55b69a7814a40b5446614a0746c`
- Pre-final-H4 HEAD: `39b98bf569fde67179a2c132b9afa74dec2ead2d` (`feat: expose secret handoff capability`)

H4 implementation commits before this final acceptance commit:

```text
248634a  docs: bind h4 secret privacy topology
c123a8c  feat: define shell readiness privacy proofs
23e0750  feat: isolate delegated private output
1ad7dd4  feat: report private output omission honestly
9ba8e74  feat: detect delegated shell runtime
14f17c1  feat: add ephemeral shell readiness adapters
e1b8273  feat: automate secret handoff readiness
39b98bf  feat: expose secret handoff capability
```

H2 records `H4_ALLOWED=true`. H0 qualifies only Darwin for the selected privacy topology; H4 does not infer Linux support from cross-builds or unit tests.

## 2. Exact provider and shell identities

The final native acceptance remains bound to the H0-qualified production provider:

```text
platform                  = darwin/arm64
provider_id               = tmux_control_mode
provider_version          = 1
observation_topology      = per_session_observer
qualified_tmux            = /opt/homebrew/bin/tmux
tmux_version              = tmux 3.6a
tmux_sha256               = 70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef
private_attach_flag       = no-output
history_replay_allowed    = false
```

Qualified shell matrix used by the final H4 run:

```text
fish = fish, version 4.7.1
zsh  = zsh 5.9 (arm64-apple-darwin25.0)
bash = GNU bash, version 3.2.57(1)-release (arm64-apple-darwin25)
```

Current-shell detection is pane/provider-backed. `$SHELL` is not authority. Unknown or changed foreground shell identity degrades to manual behavior; it does not receive guessed Bash syntax.

## 3. Required authority/privacy ordering

Native and orchestration tests prove the following ordering:

```text
reserve handoff + rotate authority epoch
-> fence agent ingress
-> prepare exact current-shell readiness watcher when requested
-> arm per-session observer private from attach
-> prove current private observer/provider identity
-> persist private_intervals_omitted capture truth
-> attach human read-only
-> prove exact human client
-> enable human write
```

Automatic reclaim is separately ordered:

```text
qualified shell boundary event
-> fence human ingress
-> force human client read-only
-> re-prove current provider/agent ownership
-> persist AGENT_OWNED while capture remains private
-> prove forward-only privacy release for the private epoch
-> release observer no-output without history replay
-> persist public capture
```

`TransferBoundary` and `PrivacyReleaseProof` remain distinct. Manual `ready` can return authority to the agent while capture stays private; manual ready alone never releases secret capture.

Abort during a private interaction fences the human before watcher cleanup. Resume rotates authority, discards stale attachment state, re-prepares readiness, and monotonic-rebinds the still-private observer to the newer epoch without publicizing or replaying prior bytes.

## 4. Deterministic secret-canary surface scan

The final tests generate runtime canaries. **No actual canary value, encoding, hash, or derived length is recorded in this tracked evidence.** The test identity and PASS result are recorded instead.

A private canary is demonstrably real because the human PTY sees the delegated shell echo derived from the human input. The same generated value and common encoded/hash variants are required absent from model-visible/durable surfaces.

| Surface | Result | Proof source |
|---|---|---|
| human PTY | PASS: canary visible to human | `tests/integration/interactive_handoff_secret_test.go`; native restart acceptance |
| MCP result/public projection | PASS: no private value/derived material | H4 MCP projection regression + public state contract |
| IPC result/public projection | PASS | H4 IPC projection regression + native restart scan |
| canonical/public output | PASS | real secret integration and post-restart Poll scans |
| v5 receipt/result | PASS | privacy-only terminal lane is partial and contains no canary |
| Event Journal | PASS | native hard-restart `inspect.events` canary scan |
| handoff/delegated metadata | PASS | canonical/public state-tree scans |
| telemetry | PASS | state/public surface scan; no private transcript source exists |
| repro/evidence | PASS | state/public surface scan; delegated receipt remains lifecycle-only |
| durable state files | PASS | recursive generated-canary variant scan |
| daemon logs/errors | PASS | native restart daemon-log variant scan |
| provider reconnect/resync | PASS | canary typed while daemon is down remains human-visible but absent after replacement observer recovery |
| notifier argv/environment | PASS | notifier uses `/usr/bin/env -i`; native secret-env canary test verifies exclusion |

Repeated real secret integration was run with `-count=5` and passed. The hard daemon-restart lane additionally injects generated private input while the daemon is actually down, then injects a second generated private value after restart; both remain private.

## 5. Observer replacement, terminal proof, and no-replay fixes proven by Task 9

Task-9 native acceptance found and fixed two provider races that prior unit simulations did not expose:

1. tmux may publish `pane_dead=1` before `pane_dead_status`; terminal proof now performs only a bounded post-known-exit settle and requires the exact status rather than inventing an exit code;
2. daemon `Provider.Wait` may begin on the public observer before H4 swaps in the private observer. Wait now re-resolves the current exact observer across replacement, and observer generations share one cumulative public-output byte counter so replacement cannot create a false transport gap.

Native private terminal/observer-swap regressions passed repeatedly (`-count=20`). No `capture-pane` or history reconstruction is introduced.

A completed privacy record may be followed by another secret handoff on the same delegated session only for a distinct handoff ID and strictly newer authority epoch. Same/stale handoff identities remain rejected. This enables repeated privacy cycles without weakening replay protection.

## 6. Multi-session and resource matrix

A native three-session lane ran 100 full privacy cycles:

```text
A = private delegated session
B = noisy public delegated session
C = noisy public delegated session
cycles = 100
per-cycle A = arm private -> observer detach/reconnect -> private write -> release -> public write
```

Result:

```text
A private marker leakage = 0
A public-after-release    = observed
B public markers          = 100/100
C public markers          = 100/100
observer reconnect        = private-before-receive
history replay            = none
native test duration      = 36.174s
```

This complements H0 P14/P15, which already qualified the same `per_session_observer` topology across 128 privacy cycles and old/new overlap fault points.

## 7. Shell-readiness matrix

Fresh Task-9 shell tests passed on fish 4.7.1, zsh 5.9, and Bash 3.2.57.

For each supported shell the suite verifies:

- exported + non-empty requirement => satisfied;
- unset, empty, and non-exported values => not satisfied;
- only export/name metadata is inspected; secret values/hashes/lengths are not serialized;
- existing user prompt hooks remain intact;
- installation prompt is skipped and the next safe prompt is evaluated;
- watcher self-removes on notification;
- abort/cancellation removes the watcher/socket;
- nested/replaced shell identity requires reprobe and degrades manual on mismatch;
- no `.zshrc`, `.bashrc`, or `config.fish` mutation;
- notifier runs with `/usr/bin/env -i` and does not inherit the watched secret;
- 100 install/remove cycles leave no helper/hook creep.

Fresh shell-focused Task-9 run:

```text
adapter/shellintegration = PASS (2.178s)
app/shellintegration     = PASS (1.013s)
```

## 8. Hard daemon restart inside a private interval

`cmd/shellbeam/interactive_handoff_secret_acceptance_test.go` uses the production native binary, real qualified tmux provider, and real `shellbeam session attach` on a PTY.

The test:

1. starts an H4 delegated session and requests `privacy=secret`;
2. attaches a real local human client and proves `HUMAN_OWNED + PrivacyPrivate + CapturePrivate`;
3. hard-kills the daemon while the human client remains writable/private;
4. types a generated private canary **while the daemon is down** and proves the human sees it;
5. restarts the daemon on the same state/runtime directories;
6. proves recovered state remains `HUMAN_OWNED + PrivacyPrivate + CapturePrivate`;
7. proves Poll, public handoff state, Event Journal, state files, and daemon log do not contain the generated value or tested variants;
8. types a second generated private value after restart and proves the replacement observer remains private-before-receive;
9. verifies durable capture truth remains privacy-partial.

No public/history reconstruction is used. Provider generation/identity mismatch remains fail-closed.

`session attach` itself now validates the H4 superset state rather than rejecting secret bootstrap with the old H2-only validator. Standard handoff retains the public-output warning; secret/private handoff displays the private-capture warning only after bootstrap proves private state.

## 9. H2/H3/H4 composition

Both required composition lanes passed. The H3 negative-control lane was also run with the real H0 tmux provider present, so it did not pass merely by skipping native setup:

```text
H2 + H3, H4 absent  = PASS (secret remains feature_unavailable)
H2 + H4, H3 absent  = PASS
H2 + H3 + H4        = PASS
full integration with H0 tmux = PASS (21.161s)
```

A final negative-control run exposed and fixed a capability-policy mismatch: the daemon coordinator previously enabled H4 from the delegated runtime's privacy interface even when the public capability catalog intentionally advertised `Secret=false`. H4 enablement is now catalog-driven; privacy/readiness implementations are prerequisites, not feature switches. A daemon regression proves an unadvertised H4 request returns `feature_unavailable` before privacy-provider mutation.

H3 is independently present on this branch, so the combined lane was run rather than recorded `NOT_RUN_COUNTERPART_ABSENT`. Terminal presentation does not become privacy authority, and privacy capability does not assume a terminal presenter exists.

During Task 7 final verification, native acceptance host-UI isolation was strengthened so test binaries cannot launch a real Ghostty window. The H4 native acceptance therefore exercises provider/privacy semantics without external GUI side effects.

## 10. Output and evidence truth

Privacy-only normal terminal completion is proven as:

```text
output_complete = false
capture_quality = partial
capture_reasons = [private_intervals_omitted]
evidence_authority = session_lifecycle_only
```

If transport/provider loss occurs later, capture truth promotes monotonically to `incomplete`; prior `private_intervals_omitted` is retained in canonical ordering together with the later reason(s).

The native canary lane originally exposed a false `provider_lost` terminal caused by stale observer/terminal-state races. After the provider fixes above, privacy-only terminal completion is truly partial rather than incorrectly incomplete.

Delegated v5 receipts remain `session_lifecycle_only`. Evidence worker and direct evidence service both refuse ordinary mechanical evidence derivation from that authority even when capture itself would otherwise be complete. Private omission is not execution failure and does not grant verification authority.

## 11. Fresh H4 completion gates

Module integrity:

```text
go mod verify
PASS: all modules verified
```

Exact aggregate Task-9 command was rerun clean after all fixes:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test \
  ./internal/core/shellintegration \
  ./internal/app/shellintegration \
  ./internal/adapter/shellintegration \
  ./internal/adapter/delegatedtmux \
  ./internal/app/interactivehandoff \
  ./internal/app/daemon \
  ./api/schema \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  ./cmd/shellbeam -count=1
```

Final post-policy aggregate results:

```text
core/shellintegration        PASS (0.536s)
app/shellintegration         PASS (0.978s)
adapter/shellintegration     PASS (4.301s)
adapter/delegatedtmux        PASS (68.696s)
app/interactivehandoff       PASS (2.553s)
app/daemon                   PASS (163.123s)
api/schema                   PASS (28.803s)
adapter/ipc                  PASS (9.632s)
adapter/mcp                  PASS (1.903s)
cmd/shellbeam                PASS (360.747s)
```

An earlier aggregate invocation hit two unrelated E26 checkpoint native tests with transient `workspace_unavailable`. Task-9 changed no checkpoint code. Both failing E26 tests immediately passed in isolated rerun (`5.12s` and `0.88s`), full `cmd/shellbeam` then passed `256.070s`, and the exact aggregate command above subsequently passed clean. The final H4 decision is based on the clean rerun, not on assuming the transient failure away.

Fresh race lane:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race \
  ./internal/app/shellintegration \
  ./internal/adapter/shellintegration \
  ./internal/adapter/delegatedtmux \
  ./internal/app/interactivehandoff \
  ./internal/app/daemon -count=1

app/shellintegration         PASS (1.538s)
adapter/shellintegration     PASS (4.134s)
adapter/delegatedtmux        PASS (60.425s)
app/interactivehandoff       PASS (2.761s)
app/daemon                   PASS (88.380s)
```

Fresh structural gate:

```text
go run ./tools/devctl check
PASS
receipt = .build/receipts/20260820T163434.208000000Z-check.json
```

Fresh dirty affected-suite gate:

```text
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
PASS
base = 33fe40999910a08410204993b9edb8f7e58698a5
```

The affected selection passed schema, cmd, delegated tmux, IPC/MCP/shell/store/terminal adapters, bridge, daemon, delegated/evidence/handoff/shell/terminal apps, capability/core packages, contract, integration, and H0 tooling. Representative long lanes:

```text
app/daemon         PASS (70.830s)
tests/integration PASS (8.743s)
cmd/shellbeam      PASS (242.340s)
```

`git diff --check` is clean.

## 12. Anti-goal and privacy review

Exact plan scans were run:

```text
rg -n 'capture-pane|\.zshrc|\.bashrc|config\.fish|preferred_terminal' internal cmd api || true
rg -n 'env\s*$|printenv|set\s+-x.*VALUE' internal/adapter/shellintegration || true
```

Matches are forbidden-string assertions in tests; production H4 code contains no capture-pane/history reconstruction, dotfile mutation, secret-value print, or preferred-terminal dependency.

Additional production review found no secret value/hash/digest/length persistence in H4 private/capture metadata. Durable privacy state contains only closed identity/epoch/private-state facts; durable capture truth contains only quality/reason enums and timestamps. Notifier invocation remains:

```text
/usr/bin/env -i <shellbeam> __handoff_notify ...
```

The hidden notifier is not advertised in public CLI help and carries only bounded handoff/epoch/event/runtime identity plus boolean readiness result.

## 13. H4 completion gate

| Requirement | Result |
|---|---|
| H0 topology exact/current | PASS |
| private before secret human write | PASS |
| all model-visible/durable canary surfaces scanned | PASS |
| private A does not suppress public B/C | PASS, 100/100 Task-9 cycles plus H0 P14/P15 |
| reconnect/replacement private from first byte | PASS |
| no history/capture-pane replay | PASS |
| transfer boundary distinct from privacy release | PASS |
| manual ready does not release privacy | PASS |
| release only after human fence + current proof + forward-only boundary | PASS |
| privacy-only capture truth is partial | PASS |
| later failure promotes incomplete without erasing private reason | PASS |
| delegated receipt remains lifecycle-only evidence | PASS |
| H2+H4/no-H3 | PASS |
| H2+H3+H4 | PASS |
| fish/zsh/bash preserve hooks and leave no helper/dotfile residue | PASS |
| unknown/nested shell fails closed/manual | PASS |
| readiness publishes boolean/proof only | PASS |
| notifier excludes delegated secret env | PASS |
| private abort fences before cleanup | PASS |
| credential presence remains distinct from capability verification | PASS |
| context-exec remains unavailable | PASS |
| native canary/restart/resource/race/schema/devctl gates | PASS |

Final decision:

```text
H4_COMPLETE=true
H5_DESIGN_ALLOWED=true
CONTEXT_EXEC_AVAILABLE=false
H4_PLATFORM=darwin_only
H4_LINUX=NOT_RUN
```

H5 design may proceed. This does not authorize context-exec implementation by implication; `CONTEXT_EXEC_AVAILABLE=false` remains explicit until the independent H5 design/qualification path completes.
