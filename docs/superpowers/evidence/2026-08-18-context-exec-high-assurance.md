# H5 Context Execution High-Assurance Native and Adversarial Evidence

**Status:** Task 8 high-assurance matrix and every Step-8 precommit repository gate pass on the qualified Darwin lane. This evidence records `CONTEXT_EXEC_STABLE_GATE=true`; the containing publication commit is followed by the plan-required clean-tree and fresh `devctl check` postconditions.

```text
CONTEXT_EXEC_STABLE_GATE=true
H5_NATIVE_DARWIN=true
H5_NATIVE_LINUX=NOT_RUN
H5_RESOURCE_ENFORCEMENT=unavailable
H5_HERMETIC=unavailable
```

## 1. Frozen design and amendment lineage

- Original approved design: `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md`
- Original approved SHA-256: `b3b10c5481fee65118c5fc062255e6f43b7f357d15a17e1069b1f15abf52a32c`
- Shell-continuity amendment: `docs/superpowers/specs/2026-08-22-context-exec-helper-claim-shell-continuity-amendment-design.md`
- Amendment commit: `9baac5387b6cf7438b93a85482180525b9357f4b`
- Amendment SHA-256: `3891afdf5e7f28713751884f54ae2d84506f6c427f9adab639a5812f422eaa24`
- Amendment implementation plan commit: `816ead2c51de644e2cd8823c6151ea198ef1d52b`
- Pre-Task-8 implementation checkpoint: `45de26c7376a2f5bf1781c8689a7590a038aea0f` (`feat: expose delegated context execution`)

The amendment keeps pre-launch shell qualification strict, but helper authentication no longer reinterprets the expected foreground `shellbeam` helper as the interactive shell. Claim-time shell continuity is instead bound by two internal typed values:

```text
ShellContinuityExpectation = server-owned pre-launch identity
ShellContinuityProof       = claim-time host-peer observation
```

The binder compares both. Claim-time authority still freshly checks live binding, provider generation/ref, authority epoch, owner/agent ingress, privacy state, and CWD directory identity. No public API or helper wire protocol carries shell process identity, helper PID, pane TTY, or raw peer credential data.

## 2. Qualified native lane

```text
platform        = darwin/arm64
go              = go1.26.6
tmux            = /opt/homebrew/bin/tmux
tmux_version    = tmux 3.6a
provider        = tmux_control_mode
native_test     = TestContextExecNativePostSecretWorkflowIsBoundedPrivateAndExactlyOnce
```

The exact native command is:

```text
SHELLBEAM_H0_TMUX=/opt/homebrew/bin/tmux \
go test -v ./cmd/shellbeam \
  -run '^TestContextExecNativePostSecretWorkflowIsBoundedPrivateAndExactlyOnce$' \
  -count=1
```

Fresh final matrix run before repository-wide gates:

```text
PASS: TestContextExecNativePostSecretWorkflowIsBoundedPrivateAndExactlyOnce
native test duration = 44.97s
package duration     = 45.883s
SKIP                 = false
```

The native lane uses an isolated test daemon/runtime and does not restart or mutate the global ShellBeam production runtime.

## 3. Native post-secret workflow and exactly-once behavior

The native acceptance proves this order:

```text
delegated zsh starts
-> secret handoff becomes HUMAN_OWNED + private
-> user exports generated credential
-> exact shell readiness boundary fires
-> privacy is forward-only released
-> context.exec reserves one request
-> helper is delivered at the next prompt
-> helper peer/parent/foreground continuity is proven
-> claim-time non-shell authority is freshly revalidated
-> child executable is prepared and authorized
-> child is spawned
-> child-owned stdout/stderr are captured
-> terminal truth is durably promoted
-> canonical public state is returned
```

Expected result:

```text
lifecycle          = canonicalized
output             = DOCTOR_OK\n
output_complete    = true
evidence_quality   = complete
evidence_authority = context_exec_child_owned_v1
exit               = reaped, code 0
timed_out          = false
```

The same `context_exec_id` and exact request replays the canonical result without a second workload side effect. The test-owned run counter remains exactly one line. Reusing the same `context_exec_id` with changed argv returns `operation_conflict`.

Cross-layer integration adds a real persistent-store replay/restart proof:

```text
TestContextExecHighAssuranceExactReplaySurvivesRestartWithoutSecondHelper = PASS
```

It proves exact replay does not perform fresh authority observation or arm another helper, the durable `helper_requested` record survives store reopen, and restart reconciliation resolves ambiguous helper delivery without re-arming a helper.

## 4. Shell-continuity amendment proof

Focused amendment gates passed before the parent matrix:

```text
internal/core/contextexec       PASS
internal/adapter/contextexec    PASS
internal/app/contextexec        PASS
internal/app/daemon             PASS (132.190s)
cmd/shellbeam                   PASS (319.706s)
git diff --check                PASS
```

The exact native H5 path then passed after two native-only findings were closed under TDD:

1. **Runtime CWD alias bug.** The helper runtime compared authenticated CWD lexically. macOS logical `/var/...` and physical `/private/var/...` can identify the same directory object. A focused regression first failed with `context_exec_boundary_unproven`; production now uses clean lexical equality first and otherwise requires `os.Stat` on both directories plus `os.SameFile`.
2. **Acceptance fixture mismatch.** The initial fake doctor was a `#!` script. The Darwin strong executor intentionally rejects interpreter chains as unqualified. The acceptance fixture now builds a native Go workload binary, preserving the strong-executor contract instead of weakening production.

The continuity-specific regression suite proves:

- exact helper executable;
- same-user Unix peer credentials;
- helper direct parent equals pre-launch pane-shell PID;
- stable parent process identity matches the pre-launch shell;
- helper is foreground on the expected pane TTY;
- wrapper descendants are rejected even with the correct shell ancestor;
- a direct child not owning the foreground TTY is rejected;
- expectation/proof session, epoch, provider generation, shell runtime identity, pane PID/process identity, TTY, and helper executable are cross-bound;
- foreground `pane_current_command=shellbeam` no longer causes shell-family re-probing at claim time.

Temporary root-cause diagnostics are absent:

```text
SHELLBEAM_H5_AUTH_DIAG      = absent
SHELLBEAM_H5_BIND_DIAG      = absent
SHELLBEAM_H4_AUTOREADY_DIAG = absent
```

## 5. Generation, ownership, privacy, and boundary adversarial matrix

Fresh cross-layer integration result:

```text
TestContextExecHighAssuranceAdmissionMatrixFailsBeforeHelperArm = PASS
```

Cases:

| Case | Expected stable failure | Helper arm |
|---|---|---:|
| stale authority epoch | `context_exec_stale_generation` | 0 |
| human-owned authority | `context_exec_not_agent_owned` | 0 |
| private capture / release pending | `context_exec_privacy_blocked` | 0 |
| unknown shell identity | `context_exec_boundary_unproven` | 0 |
| nested/replaced shell identity | `context_exec_boundary_unproven` | 0 |
| provider generation drift | `context_exec_stale_generation` | 0 |
| CWD directory drift | `context_exec_boundary_unproven` | 0 |

The existing app/store matrix additionally proves lease conflicts, durable expectation replay, one-shot helper generation, and fresh claim-time owner/privacy/provider validation.

## 6. Retry, ACK-loss, crash, and recovery matrix

Fresh store/app matrix passed:

```text
TestContextExecReserveFaultBoundariesRetryWithoutDuplicateReservation
TestContextExecTransitionFaultMatrixReopenRetryIsIdempotent
TestContextExecAuthAndSpawnAckLossReplaySameGenerationAndChild
TestContextExecTerminalAndCanonicalAckLossReplayWithoutInventingSecondChild
TestContextExecRecoveryCandidatesSurviveRestartAndExcludeTerminalStates
TestContextExecRecoveryCandidatesIncludeCanonicalizedStateWhileExactLeaseRemains
TestReserveContextExecConcurrentExactReplayHasOneWinnerAndChangedRequestConflicts
TestContextExecChildReservedMustPersistBeforeExecuteAuthorizationAndSpawn
TestReconcileAppliesConservativeRecoveryMatrixWithoutHelperMutation
TestReconcileAuthorizedChildReservedNeverReturnsDuplicateExecutionAuthorization
```

The conservative restart decisions are:

```text
reserved                       -> resume admission
helper_requested               -> ambiguous delivery
helper_authenticated           -> reconnect same generation
child_reserved before auth ACK -> prepared closed
child_reserved after auth ACK  -> spawn unknown / ambiguous
child_spawned                  -> helper lost
child_terminal                 -> canonical repair + publish + lease release
canonicalized                  -> republish/release as needed
```

No recovery branch invents a second child after execution authorization.

## 7. Child authorization, actual executable, and output attribution

Fresh adversarial tests passed for:

- child reservation persisted before execute authorization;
- changed prepared executable rejected through exact reservation conflict;
- spawn child operation/session/executable drift rejected;
- helper terminal cannot claim daemon canonical authority;
- Darwin executable identity is verified against the mapped executable before detach/continuation;
- path replacement after prepare is rejected before a malicious first instruction;
- child timeout kills the owned process group and proves reap;
- authoritative output comes only from helper-owned child stdout/stderr pipes;
- injected pane noise is not attributed as child output.

The final native lane additionally uses a controlled `PATH` shadow:

```text
delegated shell: PATH="$PWD:$PATH"
requested argv0: fake-doctor
resolved executable: absolute native binary in the delegated CWD
```

The public/durable result must preserve the named requested token while the resolved executable is proven to be the exact expected file object with `os.SameFile`.

A visible `context_exec_id` is correlation only. It cannot authenticate a helper. Private one-shot claim authority plus peer credentials, exact helper executable, stable direct-parent identity, and foreground TTY proof are all required before request delivery.

## 8. Environment, control, and privacy anti-leak matrix

The native workload receives the user-owned post-secret environment value because the purpose of post-secret context execution is to execute in that exact user context. The same native workload exits non-zero if any of these internal control variables are visible:

```text
SHELLBEAM_CONTEXT_EXEC_RUNTIME_DIR
SHELLBEAM_CONTEXT_EXEC_SOCKET
SHELLBEAM_CONTEXT_EXEC_CLAIM
SHELLBEAM_CONTEXT_EXEC_GENERATION
SHELLBEAM_CONTEXT_EXEC_LAUNCH_ID
```

The native canary test records no actual canary value, encoding, hash, or derived length in this evidence. Generated variants are required absent from:

| Surface | Result |
|---|---|
| context.exec public response | PASS |
| durable state tree | PASS |
| daemon logs/errors | PASS |
| Event Journal public response | PASS |
| evidence inspection public response | PASS |
| telemetry inspection public response | PASS |
| repro create response | PASS |
| repro inspection, when materialized | PASS |
| helper wire request / durable state continuity canary | PASS |
| IPC provider generation / shell identity / CWD / helper generation fields | PASS |

Repro materialization is allowed to return the explicit stable `repro_materialization_unavailable` result; that unavailable response is also scanned for the canary. If a capsule is materialized, `inspect.repro` is scanned as well.

Continuity metadata test canary `proc_shell_ctx_canary` is placed in the internal pre-launch process identity and required absent from helper request serialization and durable context-exec state.

## 9. Resource and hermetic truthfulness

The server capability is intentionally:

```text
resource_enforcement = unavailable
hermetic              = unavailable
```

The native acceptance rejects any overclaim. Bounded timeout, output limits, process-group ownership, and strong executable identity do not imply hermetic execution or general resource enforcement.

## 10. Fresh matrix receipts before final repository gates

Focused cross-layer integration:

```text
go test -v ./tests/integration -run '^TestContextExecHighAssurance' -count=1
PASS
package = 1.590s
```

Fault/retry/crash store + app matrix:

```text
internal/adapter/store  PASS (2.828s)
internal/app/contextexec PASS (0.487s)
```

Peer/non-bearer/executable/output + IPC privacy matrix:

```text
internal/adapter/contextexec PASS (1.350s)
internal/adapter/ipc         PASS (0.611s)
```

Final native matrix after user-secret surface scans, control-env checks, and controlled PATH shadow:

```text
cmd/shellbeam exact native H5 PASS
test duration    = 44.97s
package duration = 45.883s
SKIP             = false
```

## 11. Final gate ledger

Every exact Task-8 Step-8 precommit gate has a fresh terminal PASS:

```text
go mod verify                                                    = PASS (all modules verified)
parent package aggregate                                         = PASS (exit 0; cmd/shellbeam 263.839s; daemon 112.109s)
race: core/app/adapter/daemon                                   = PASS (exit 0; daemon 90.318s)
go run ./tools/devctl check                                     = PASS (.build/receipts/20260822T150951.401929000Z-check.json)
go run ./tools/devctl test --dirty --base 33fe40999910a08410204993b9edb8f7e58698a5 --json = PASS (.build/receipts/20260822T151611.788333000Z-test.json; selection=affected)
git diff --check                                                = PASS
git diff --cached --check                                       = PASS
pre-publication commit-gate                                     = PASS (.build/receipts/20260822T152241.173577000Z-commit-gate.json; selection=affected)
```

The first dirty-impact attempt recorded `.build/receipts/20260822T151023.568889000Z-test.json` with one macOS terminal-presentation failure because the external `lsappinfo` process was killed. The exact focused test immediately passed, the whole terminal-presentation package passed repeatedly, and the exact dirty-impact command then passed with the receipt above. No H5 package failed in that first run.

The publication commit containing this evidence is created only after a final commit-gate on the exact staged bytes. Per the Task-8 plan, the clean-tree check and a fresh `go run ./tools/devctl check` are postcommit execution postconditions and therefore are not pre-recorded as historical facts in this precommit artifact.

```text
CONTEXT_EXEC_STABLE_GATE=true
publication commit message = test: verify high assurance context execution
postcommit required         = clean tree + fresh devctl check
```

The stable gate applies only to the already-qualified H5 provider/shell lane described above. It does not promote broader providers, Resource Enforcement, or Hermetic execution.
