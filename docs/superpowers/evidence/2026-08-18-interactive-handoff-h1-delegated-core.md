# H1 Delegated Interactive Core — Darwin Acceptance Evidence

H2_ALLOWED=true
H2_SCOPE=darwin_experimental
H2_ALLOWED_LINUX=false

## Attestation boundary

This report attests one immutable H1 implementation checkpoint. The report itself is committed afterward as a docs-only attestation so that the implementation commit hash and source fingerprint are not self-referential.

- Implementation checkpoint HEAD: `62f9a364b3c319d776b0b480b7c86184b09eaa22`
- Implementation checkpoint commit: `test: verify delegated interactive core`
- Branch: `design/human-agent-interactive-session-handoff`
- `devctl test --dirty --base 887c4b7240024bace5ce144624bc458f4b7742cd` source fingerprint: `9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f`
- Task 9 code-checkpoint commit-gate source fingerprint: `9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f`
- Postcommit `devctl check`: PASS — `.build/receipts/20260819T043732.157676000Z-check.json`
- Platform scope: **Darwin/macOS experimental only**. Linux remains unadvertised and fail-closed.

The implementation checkpoint was clean before this attestation was written. This report does not reinterpret Linux `NOT_RUN` as PASS and does not change production behavior.

## H0 qualification and provider binding

- H0 machine gate: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- H0 gate SHA-256: `f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1`
- H1 provider binding: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-provider-binding.md`
- Provider-binding SHA-256: `3af6deba3bed46c39f5ae2e358d255e64d711c00b75e300a8d40cc5cabaecbba`
- Darwin H0 gate: `H1_ALLOWED=true`
- Linux H0 gate: `H1_ALLOWED=false` (`NOT_RUN`)
- Cross-platform aggregate H0 gate remains `false`.

Qualified Darwin provider:

```text
provider_id              = tmux_control_mode
provider_version         = 1
control_adapter          = raw_control_mode
tmux                     = /opt/homebrew/Cellar/tmux/3.6a/bin/tmux
tmux_version             = tmux 3.6a
tmux_sha256              = 70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef
input_fence_mechanism    = tmux_same_client_switch-client_-E_-r_assume-paste-time_0
observation_topology     = per_session_observer
```

All Darwin H0 P0–P15 are PASS in the bound native report. H1 uses the qualified ShellBeam-owned raw Control Mode implementation; `gotmuxcc` is not the production control adapter.

## H1 public/runtime contract

The accepted H1 surface at the implementation checkpoint is:

```text
session_mode                   = delegated_interactive
reservation_schema             = 5
receipt_schema                 = 5
initial_authority_epoch        = 1
evidence_authority             = session_lifecycle_only
input_authority_provenance     = agent_only
capture_quality                = complete | partial | incomplete
capture_reasons                = private_intervals_omitted | provider_lost | transport_gap
max_mutation_records/session   = 4096
daemon_restart_continuity      = true
host_reboot_continuity         = false
platform                       = darwin
```

Modern v2 start/write/kill/result/capability surfaces carry delegated mode/epoch truth. Legacy v1 remains closed: it cannot start delegated mode, legacy catalog omits the delegated capability and receipt v5, and polling a delegated session through v1 projects a newer receipt as `null` rather than downgrading or leaking v5 metadata.

Delegated H1 policy is interactive and persistent-like: `stdin_mode=stream` and unbounded timeout. Unsupported finite/default timeout or closed stdin forms fail before provider work. Direct and B1 persistent behavior remain on their legacy paths.

## Receipt and evidence authority

Receipt v5 separates lifecycle truth from capture truth.

- Healthy H1 output is `capture_quality=complete`, `capture_reasons=[]`, `output_complete=true`.
- Provider loss is incomplete and never fabricates child exit evidence.
- A daemon restart creates a forward-only observer boundary. If lifecycle later proves exit 0, the session can remain `completed/success` while truthfully reporting `output_complete=false`, `capture_quality=incomplete`, `capture_reasons=[transport_gap]`.
- Schemas 1–4 retain their existing success-completeness requirement.
- Delegated receipts are `session_lifecycle_only`; they are not ordinary test/build verification evidence.
- `Reservation.EvidenceEligible()` is false for delegated schema 5.
- Explicit ordinary evidence contracts on delegated start are rejected before provider work.
- Daemon regressions prove delegated `test` and `build` intents can finish with a lifecycle receipt without scheduling an ordinary evidence record.

## Authority and mutation semantics

H1 begins agent-owned. Durable mutation admission obeys **idempotency before authority**:

1. look up durable logical mutation identity;
2. exact known retry replays its stored result even if the current epoch is newer;
3. an unseen mutation must match current epoch and owner;
4. reserve durably before provider delivery;
5. complete with durable provider outcome or `outcome_unknown` if delivery truth is ambiguous.

Write identity binds:

```text
session_id + authority_epoch + kind=write + input_offset + next_offset + fingerprint
```

`next_offset` is an internal durable write-span fact added before release so restart recovery can reconstruct the next agent input coordinate without guessing. It does not change the public meaning of `input_offset`.

Kill/signal uses a stable control ID, current epoch, and no write span. Human bytes do not exist in H1.

Restart recovery accepts only a unique contiguous prefix of completed successful write mutations. Reserved/delivered/outcome-unknown entries, gaps, conflicts, or unreadable records fence reconciliation instead of guessing continuation.

## Mutation taxonomy audit

Production H1 callsites map as follows:

| Kind | H1 status | Provider/lifecycle path | Policy |
|---|---|---|---|
| `write` | available | daemon `MutationWrite` → durable admission → provider `Write` | epoch + owner + idempotency-before-authority |
| `signal/kill` | available | daemon `MutationKill` → durable admission → provider `Signal` | stable kill ID + epoch + owner |
| `resize` | unavailable in H1 | no public/provider resize action lane | deferred; no bypass |
| `transfer` | unavailable in H1 | no H1 action | H2-only |
| `human_control` | unavailable in H1 | no H1 action | H2-only |
| `provider_authority` | no public mutation action in H1 | provider observation/reconciliation only | fresh provider proof; no caller bypass |

Provider lifecycle operations are separately bounded:

- `Create`: only after schema-5 reservation + durable provider ref/binding reserve.
- `Reattach`: same durable ref for lost-response retry and daemon restart.
- `Inspect`: fresh provider authority/current-generation proof for unseen control mutations.
- `Wait`: terminal observation through the qualified Darwin event-driven watcher.
- `Detach`: graceful daemon shutdown; preserves tmux object/private recovery state.
- `Close`: terminal cleanup; may destroy provider session only after terminal publication.

No production callsite gives write/kill authority outside the durable admission path.

Raw exact-checkpoint mutation audit SHA-256: `67d0e77b7f949a3c336e4fea89d853ed301ff35939fed6760f16b082303cf70a`.

## Native acceptance matrix

### Cross-layer real tmux integration

`tests/integration/delegated_session_test.go` + helper harness use the real qualified Darwin tmux provider, real repository, and daemon service.

Proven:

- delegated start → agent write → output → clean terminal;
- simulated lost provider response after actual Create → retry same operation/ref → exactly one tmux session and one pane;
- explicit `stdin_mode=stream` + `timeout_mode=unlimited` remain the same request identity on replay;
- known old-epoch write retry replays without redelivery;
- unseen stale-epoch write fails `stale_control_generation` before provider delivery;
- current epoch write succeeds and terminal receipt carries the current epoch;
- external provider destruction becomes canonical `provider_lost`, post-loss write is rejected, and no provider session/private state is recreated;
- direct execution and successful B1 persistent start/shutdown produce zero delegated-provider calls.

Final clean harness:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./tests/integration -run 'TestH1' -count=10
PASS — 21.761s
```

The response-loss/epoch/provider-loss lane was additionally stressed after the Darwin wait fix:

```text
-count=30
PASS — 36.541s
```

### Hard daemon restart

Fresh binary-level acceptance:

```text
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./cmd/shellbeam \
  -run '^TestDelegatedRuntimeNativeDaemonHardKillReattachesExactSessionAndFailsClosedOnProviderLoss$' \
  -count=3
PASS — 8.787s
```

This proves daemon death/restart reattaches the same provider/session/generation and continues at the durable input offset. Provider loss afterward fails closed; it is not PID/name takeover or provider recreation.

## Acceptance findings closed before H1 PASS

Task 9 found two bugs that narrower tests had not exposed. Both were converted to regressions before the production fix.

### 1. Lost-response replay omitted explicit input/timeout policy

`Service.Start` constructed a duplicate replay `operation.Intent` without `StdinMode` and `TimeoutMode`. A valid first delegated request with explicit `stream + unlimited` therefore conflicted with its own retry fingerprint.

Fix: replay intent now carries the same request-level stdin/timeout policy as the admitted request. Existing response-loss unit test was strengthened to use the explicit H1 policy and now passes.

### 2. Darwin kqueue EINTR was misclassified as provider loss

`processExitWatcher.Wait` treated any `kevent` error as provider loss. Native stress captured the raw cause:

```text
reason = wait_process_exit
cause  = syscall.EINTR / "interrupted system call"
```

Fix: only `EINTR` is retried. Context cancellation remains cancellation; other errors remain fail-closed. Unit regression proves `EINTR=true`, `EBADF=false`, and the real tmux response-loss/epoch/provider-loss matrix then passed 30/30.

## Restart and shutdown truth

- `Detach` is distinct from destructive `Close`.
- Graceful daemon shutdown cancels the delegated wait loop and detaches the observer without publishing fake `provider_lost`.
- Startup reconciliation is bounded by per-session timeout, concurrency, and total budget.
- Desired durable binding is intersected with fresh exact provider observation; mismatches block reconciliation.
- Missing provider state becomes canonical provider loss; no new provider object is silently created.
- Forward-only output after daemon restart never uses `capture-pane` replay and carries `transport_gap` unless continuity is otherwise proven.
- `daemon_restart_continuity=true` is advertised only after native hard-restart acceptance.
- `host_reboot_continuity=false` remains explicit.

## Anti-goal audit

Exact implementation checkpoint scans found:

```text
handoff.request | TerminalLauncher | ShellIntegration | PrivacyReleaseProof = 0
capture-pane | preferred_terminal | reptyr under delegatedtmux           = 0
```

Root `go.mod` / `go.sum` have no diff from approved Darwin scope base `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`.

Therefore H1 does not implement H2 human handoff, H3 terminal resolver, H4 shell/secret privacy automation, H5 context-exec, arbitrary process takeover, `capture-pane` replay, or a polling terminal watcher.

Exact-checkpoint anti-goal audit SHA-256: `2d678e9ce58d8d8fa7fe04d7d464dcd68cc6ba5eea11681698f96f32cbca5132`.

## Fresh verification

### Native/non-race lane

Raw log: `.build/interactive-handoff-h1/verify-native.log`
SHA-256: `ddde92142dda21e907f7dc0f97ece2b89b4c9431a0510a74b99080d1e2b64646`

Results:

```text
go mod verify                                      PASS
H1 integration x3                                 PASS   5.650s
hard daemon restart x3                            PASS   8.787s
core/delegatedsession                             PASS   0.513s
app/delegatedsession                              PASS   0.818s
adapter/delegatedtmux                             PASS   2.433s
app/daemon                                        PASS   125.405s
api/schema                                        PASS   8.229s
adapter/ipc                                       PASS   10.515s
adapter/mcp                                       PASS   1.512s
H1_FINAL_NATIVE_PASS
```

### Race + devctl lane

Raw log: `.build/interactive-handoff-h1/verify-race-devctl.log`
SHA-256: `7528995397e786cefec7457dd1c234fd6de2d434eb3982538e0e4bc1003ff048`

Results:

```text
core/delegatedsession -race                       PASS   2.144s
app/delegatedsession -race                        PASS   1.493s
adapter/delegatedtmux -race                       PASS   3.694s
app/daemon -race                                  PASS   78.057s
H1 integration -race x3                          PASS   6.708s
devctl check                                      PASS
  receipt: .build/receipts/20260819T042532.557086000Z-check.json
devctl test --dirty --base 887c4b7...             PASS
full cmd/shellbeam selected by devctl              PASS   321.724s
git diff --check                                  PASS
H1_FINAL_RACE_DEVCTL_PASS
```

The pre-attestation `devctl test` run occurred before the final plan-only attestation-mechanics amendment. The immutable implementation checkpoint was therefore recomputed after commit.

### Exact implementation postcommit recompute

Raw log: `.build/interactive-handoff-h1/postcommit-recompute.log`
SHA-256: `bdac230edc50b126d64ca874875e0d9ee1fa88bc7dfef0f3f4e0c982400bd27e`

```text
HEAD                                                62f9a364b3c319d776b0b480b7c86184b09eaa22
clean tree                                          PASS
devctl check                                        PASS
  receipt: .build/receipts/20260819T043732.157676000Z-check.json
devctl test --dirty --base 887c4b7...              PASS
source_fingerprint                                  9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f
```

Task 9 implementation commit hook also reran its selected suites and passed:

```text
adapter/delegatedtmux                               PASS (cached)
app/daemon                                          PASS 117.163s
tests/integration                                   PASS 12.640s
commit-gate source_fingerprint                      9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f
```

## H1 completion gate

| # | Requirement | Verdict |
|---|---|---|
| 1 | exact Darwin H0 provider qualification bound | PASS |
| 2 | one explicit delegated `session_mode`; absent mode preserves legacy semantics | PASS |
| 3 | schema-5 reservation freezes delegated identity before provider creation | PASS |
| 4 | lost response retry creates one provider session/shell | PASS |
| 5 | durable epoch + mutation admission uses idempotency-before-authority | PASS |
| 6 | known old-generation retry replays; unseen stale rejected | PASS |
| 7 | exact public binding/private ref split; private provider facts are not public authority | PASS |
| 8 | agent write/signal/output use qualified Control Mode without `capture-pane` | PASS |
| 9 | restart reconciliation uses desired state + fresh exact provider observation | PASS |
| 10 | provider loss has no PID/name takeover or fabricated child exit | PASS |
| 11 | ordinary direct/B1 successful paths pay zero delegated-provider tax | PASS |
| 12 | modern v2 exposes H1; legacy v1 remains closed | PASS |
| 13 | receipt v5 composable capture + lifecycle-only evidence + agent-only provenance | PASS |
| 14 | delegated reservation never becomes ordinary evidence; explicit evidence fails pre-provider | PASS |
| 15 | H2/H3/H4/H5 runtime features remain unavailable | PASS |
| 16 | native/race/schema/devctl/commit-gate evidence passes | PASS |

## Decision

H1 is **PASS for the approved Darwin/macOS experimental lane** at implementation checkpoint `62f9a364b3c319d776b0b480b7c86184b09eaa22` with source fingerprint `9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f`.

Therefore the authoritative gate at the top of this report opens H2 for `darwin_experimental` only and keeps Linux disabled.

Linux remains `NOT_RUN`/unadvertised. H2 may consume this exact H1 checkpoint only under its existing Darwin-only global constraint. A different provider identity, tmux binary/hash, observation topology, or platform requires its own qualification rather than inheriting this evidence.
