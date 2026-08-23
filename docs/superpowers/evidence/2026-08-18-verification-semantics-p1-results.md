# P1 Verification Semantics — Completion Evidence

> Status: Task 9 practical benchmark complete on committed Task-8 implementation; Task 10 final checkpoint/merge fields are intentionally pending until their receipts exist.

## Execution base

- Base `origin/main`: `f84c2a2aa28a05a598f152ef7b4670930c3a9691`.
- Task-8 semantic-matrix HEAD before this benchmark: `9da70b203ba662662d7b3781aee9405cb5af7a05`.
- Task-8 correctness fix: `cfbfc91` (`fix: preserve typed verification evidence authority`).
- Task-8 matrix commit: `9da70b2` (`test: verify p1 evidence sufficiency semantics`).
- Task-8 affected-selection source fingerprint: `84f55195ce318e6f6a713bd34ab458ea6ac1ee3571ef077168dc512b1547bfaa`.

## Task 8 gate evidence

- Real-daemon matrix `go test ./cmd/shellbeam -run 'Verification(Semantics|Sufficiency)' -count=1`: PASS (`cmd/shellbeam` 39.011s).
- Full P1 package set: PASS; slowest observed packages were `internal/adapter/store` 297.129s and `cmd/shellbeam` 421.379s.
- Race set: PASS; `internal/adapter/store` 276.155s.
- `devctl check`: PASS.
- First combined wrapper timed out only while the long dirty gate was still running; it is not counted as dirty-gate proof.
- Standalone dirty rerun: PASS, `selection=affected`, `exit_code=0`, source fingerprint `84f55195…bfaa`, 2026-08-19T04:40:02Z → 04:45:27Z.

### Correctness defects discovered by the real-daemon matrix

1. Zero-parameter typed command bindings serialized `Parameters` as `[]` at bind time but as `null` after cloning, producing different binding digests for the same semantic command. Binder now freezes zero parameters as canonical `nil` before the authority digest is created.
2. No-tax terminal provenance intentionally leaves post-generation unreconciled. A candidate with unknown freshness can now satisfy `require_current` only when its frozen source generation exactly equals the current obligation generation; explicit stale evidence is never promoted, and unknown generation mismatch remains `unknown` rather than being mislabeled `stale`.

## Task 9 real-daemon practical campaign

All six required fixture commands exited 0. Each fixture used a disposable `/tmp` Git repository, private state/runtime directories, a real ShellBeam daemon, public IPC v2, an explicit external policy activation, and cleanup on exit. The benchmark script does not write daemon/build state into the developer repository.

Task-9 post-campaign gates: Markdown contract command exited 0; `devctl test --dirty` terminal PASS with `selection=affected`, `exit_code=0`, source fingerprint `e27aeed86c9d4fc87780600997850f39a74640c3b21c1e61dc53411b412fac64`, and `cmd/shellbeam` 306.380s (2026-08-19T05:11:27Z → 05:17:09Z).

| Scenario | Public IPC calls | Inspect bytes | Wall ms | Verification executions | Gate | Mandatory misses | Wasteful obligations |
|---|---:|---:|---:|---:|---|---:|---:|
| `docs-only` | 5 | 6528 | 4059 | 0 | `indeterminate` | 0 | 0 |
| `local-go` | 5 | 6305 | 2654 | 0 | `indeterminate` | 0 | 0 |
| `shared-go` | 5 | 7355 | 2342 | 0 | `indeterminate` | 0 | 0 |
| `fail-pass` | 16 | 5059 | 3096 | 2 | `blocked` | 0 | 0 |
| `leak` | 11 | 4840 | 2884 | 1 | `indeterminate` | 0 | 0 |
| `first-policy` | 6 | 4419 | 2743 | 0 | `indeterminate` | 0 | 0 |

`model_tool_calls` counts public semantic IPC calls made by the scenario harness; fixture setup, binary build, daemon startup, and doctor readiness polling are excluded. Poll/evidence/telemetry IPC calls are included when used.

### Scenario-specific results

- `docs-only`: required set `['docs-contract']` exactly matched expectation; zero verification executions; no Go full-suite/load/race/browser obligation was invented.
- `local-go`: required set `['go-local']` exactly matched expectation; zero verification executions.
- `shared-go`: required set `['go-dependents', 'go-shared']` exactly matched expectation, demonstrating reverse-importer blast-radius widening without extra mandatory rules.
- `fail-pass`: two real typed executions produced two immutable evidence IDs and folded to `inconsistent`; gate remained `blocked`. Telemetry: op-bench-fail-pass-fail: cpu=0+1ms rss=1327104 peak_proc=1; op-bench-fail-pass-pass: cpu=0+1ms rss=1327104 peak_proc=1.
- `leak`: literal command PASS plus `require_quiescence=true` remained `unknown` with `quiescence_unknown` because P1 has no qualified lifecycle completion provider in this runtime. Telemetry: op-bench-leak: cpu=1+2ms rss=1916928 peak_proc=1.
- `first-policy`: policy state progressed `absent → proposal_pending → effective` only through explicit activation after a later source cut.

CPU/RSS/process-peak values are recorded only where `inspect.telemetry` returned non-`unavailable` metrics. A numeric leaked-resource count is **not available** from the P1 verification surface without a qualified lifecycle provider; the benchmark therefore records it as unavailable rather than inferring zero from process absence.

## Scenario 0: historical baseline vs P1 semantics

Historical baseline for the four-Markdown shape recorded a `full` checkpoint selection and approximately 480,000 ms for the first cold/local observation; historical commit-gate selection was affected `contract:markdown`.

Current P1 `docs-only` practical result:

- `inspect.verification` response: 6528 bytes.
- Harness wall time including activation/inspection: 4059 ms.
- Verification executions automatically launched by P1: 0.
- Mandatory obligations: `['docs-contract']` only.
- Full-suite/special-suite frequency inside the P1 scenario: 0.

**This is an obligation-selection improvement, not a checkpoint runtime speedup claim.** P1 V1 does not teach `devctl verify` to consume these obligations and does not auto-run verification commands, so the historical ~8-minute checkpoint and the P1 inspection wall time are not equivalent workloads.

## Roadmap scenario coverage (0–20)

| # | Scenario | Evidence in this branch | Result |
|---:|---|---|---|
| 0 | docs-only four-Markdown shape | Task 9 `docs-only`; Stage-A practical docs measurement | PASS — docs only, no invented broad suites |
| 1 | one-file local Go behavior change | Task 9 `local-go` | PASS |
| 2 | shared package with wider import blast radius | Task 9 `shared-go`; `TestVerificationSemanticsAffectedObligations` | PASS — reverse importers widen affected paths |
| 3 | config/nonlocal path classification | `TestVerificationSemanticsAffectedObligations` security classified vs outside path | PASS — explicit classification only; no invented gap outside it |
| 4 | delegated UUID/integration assumption | `TestDelegatedOwnershipVerifiesIntegrationAssumptionWithoutProviderStress` | PASS — deterministic integration evidence, no provider stress |
| 5 | application-owned concurrency/race rule | policy/obligation selector + typed project-command contracts; no scheduler auto-run | SUPPORTED SEMANTICS — execution only when policy declares/binds command; no automatic race execution |
| 6 | authorization-sensitive path, no scale target | `TestVerificationSufficiencyPolicyGapIsAdvisoryNotGate` | PASS — security classification/gap does not invent scale verification |
| 7 | no performance target | `TestVerificationSufficiencyNoPerformanceTargetLeavesLoadNotTriggered` | PASS — load `not_triggered` |
| 8 | declared performance requirement | generic provider-availability/environment sufficiency contracts | UNAVAILABLE unless explicitly qualified provider evidence exists; no threshold invented |
| 9 | FAIL→PASS | Task 9 `fail-pass`; `TestVerificationSufficiencyCompatibleFailThenPassIsInconsistent` | PASS — inconsistent |
| 10 | unavailable native platform + waiver | `TestVerificationSufficiencyWaiverPreservesUnavailableNativeEvidence` | PASS — waiver distinct from evidence satisfaction |
| 11 | partial affected analysis | `TestVerificationSufficiencyPartialAffectedSurfaceKeepsMandatoryObligation` | PASS — conservative widening |
| 12 | leaked descendant / lifecycle proof | Task 9 `leak`; `TestVerificationSufficiencyRealDaemonNeverInventsLifecycleCompletion` | PASS — explicit unknown without qualified lifecycle proof |
| 13 | persistent ownership transfer | `TestQuiescenceSourceSubtractsOnlyExactPersistentBinding` | PASS — exact typed transfer only |
| 14 | starter template update | `TestPinnedPolicyUnaffectedByStarterTemplateChange` | PASS — pinned policy unchanged |
| 15 | P1 → weaker P2 proposal | `TestProposedPolicyCannotChangeEffectiveClassificationProjection` + activation retry test | PASS — P1 remains effective until external later-cut activation |
| 16 | first policy | Task 9 `first-policy`; `TestFirstPolicyRequiresExternalActivationSubsequentCut` | PASS |
| 17 | raw test/build evidence | `TestVerificationSufficiencyRawTestEvidenceDoesNotElevateProviderClass` | PASS |
| 18 | diagnostic rerun | `TestRerunIntentFrozenBeforeExecutionAndDoesNotEraseContradiction` | PASS |
| 19 | source mutation between FAIL/PASS | `TestSourceMutationSeparatesEvidenceCohortsWithoutRewritingHistory` | PASS — distinct generations/cohorts |
| 20 | provider execution semantics | `TestProviderExecutionSemanticsNeverChoosesUniversalConcurrencyRealDaemon` | PASS — facts visible, no worker/provider-choice decision |

## P1 truth boundary retained

- Tests/builds are evidence providers, not the verification ontology.
- Waiver is a disposition and never becomes `evidence_status=satisfied`.
- Cost/telemetry is projected after sufficiency and cannot turn insufficient evidence into sufficient evidence.
- `inspect.verification` contains no `task_complete`, `work_complete`, or `safe_to_finish` truth.
- Missing lifecycle proof is not interpreted as cleanup complete.
- Raw `VerificationKind` does not automatically become a semantic `ProviderClass`.
- Proposed policy classifications do not affect the current gate until separately activated.

## Commit sequence through Task 8

```text
56ae801 test: freeze verification semantics baseline
7f88c4d feat: define verification semantics contracts
bc5d740 feat: load verification policy proposals
1ea5b80 feat: persist verification policy authority
ce995a6 feat: derive verification affected surface
428ee74 feat: derive verification obligations
650b127 feat: expose verification obligations
60c3cd5 fix: include current dirty paths in affected surface
3077e05 test: verify p1 verification obligations
c5e9eb5 docs: record p1 stage a progress
06d0e95 docs: close p1 stage a
8d6db7a docs: fix p1 stage b task ordering
bdb3ff9 feat: adapt evidence for verification semantics
57f37be feat: preserve contradictory verification evidence
7ed0e11 docs: record p1 stage b verification progress
e8a12ce docs: fix p1 sufficiency quiescence ordering
c53f20c feat: evaluate verification evidence sufficiency
f5ba1df feat: verify operation quiescence
9e63d85 feat: project verification economics
a84d56c feat: fold verification gate status
cf80f94 feat: inspect verification sufficiency
cfbfc91 fix: preserve typed verification evidence authority
9da70b2 test: verify p1 evidence sufficiency semantics
```

## Task 10 completion evidence

The implementation tree was first proven before bookkeeping on `c4a25ade1ba7ed965ff01abf5b946a0b43f497d3`:

- traceability: `PASS core=24/24 roadmap=4/4 review=11/11 deferred=7/7`;
- forbidden production semantic scan: no matches; matches in the broad scan were tests/evidence describing the forbidden behavior;
- full P1 package set: PASS (`internal/adapter/store` 237.942s; `cmd/shellbeam` 360.806s);
- race set: PASS (`internal/adapter/store` 146.663s);
- `devctl check`: PASS;
- Task-10 dirty gate: `selection=affected`, `status=passed`, `exit_code=0`, source fingerprint `96b896903dd40937cb1eacdd33bb1340b2b5c9977699c9521310bfde0457a3f0`;
- preliminary full checkpoint: `.build/receipts/20260819T053154.364094000Z-verify.json`, `command=verify`, `selection=full`, `status=passed`, `exit_code=0`, source fingerprint `96b896903dd40937cb1eacdd33bb1340b2b5c9977699c9521310bfde0457a3f0`, 2026-08-19T05:31:54Z → 05:32:44Z;
- VCS scope before bookkeeping: clean, `0 behind / 24 ahead`, merge-base and `origin/main` both `f84c2a2aa28a05a598f152ef7b4670930c3a9691`, branch diff check PASS.

### Final-authority boundary

A repository file cannot contain the exact receipt/fingerprint of a checkpoint over its own final committed bytes without changing those bytes and invalidating the receipt. Therefore the authoritative P1 completion proof is deliberately **post-bookkeeping**:

1. commit this completion bookkeeping;
2. run one final `devctl verify --checkpoint` on that exact committed HEAD;
3. require `command=verify`, `selection=full`, `status=passed`, `exit_code=0`;
4. bind the final HEAD and source fingerprint from that durable receipt directly in the merge/deploy handoff and in P4-A Task 0.

The final durable receipt is the source of truth for the final checkpoint. This document records the pre-bookkeeping proof and the authority rule; it does not fabricate a self-referential final receipt. Merge and deployment evidence likewise belongs to the post-commit handoff because recording it here would create another source change after the claimed final checkpoint.
