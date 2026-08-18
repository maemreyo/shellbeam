# Verification Semantics P1 Practical Baseline

This record freezes the historical practical baseline used by the P1 implementation plan. It is evidence about an observed docs-only checkpoint, not a promise that future checkpoint runtimes or selections remain identical.

```text
scenario: docs_only_four_markdown_specs
historical operation: checkpoint-verify-specs-20260818
historical source fingerprint: 8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e
checkpoint selection: full
checkpoint elapsed: approximately 8 minutes on first cold/local run
pre-commit selection: affected -> contract:markdown
success criterion: preserve documentation correctness evidence while P1 inspection does not require broad Go package verification when policy + affected authority prove docs-only applicability
```

The approximately eight-minute checkpoint observation is historical and machine-local. It is not a stable performance target, SLO, or future runtime guarantee.

The corresponding benchmark helper is `scripts/benchmark-verification-p1.sh`. Its default `baseline` mode is non-mutating and prints the pinned historical JSON record. `--measure-current` may be used later only against a caller-prepared state; it does not edit repository files itself.

## Stage-A real-daemon acceptance — 2026-08-19 ICT

Stage A was exercised against temporary attached Git repositories through the real daemon IPC surface. The acceptance suite is `cmd/shellbeam/verification_semantics_test.go`; it does not substitute in-memory verification services for the daemon scenarios.

### Integrated semantic matrix

`go test ./cmd/shellbeam -run VerificationSemantics -count=1` passed the Stage-A matrix covering:

- explicit `policy_state=absent` with no silent starter selection;
- first-policy proposal followed by a distinct source-generation cut and explicit activation;
- same-generation activation rejection;
- immutable/auditable activation authority and same-intent retry semantics;
- proposed P2 classifier changes unable to contaminate the effective P1 classification projection before activation;
- proposed policy unable to self-grant activation/waiver authority;
- superseded activation replay unable to roll the effective index backward;
- docs-only policy selection without inventing a Go verification obligation;
- transitive Go importer relations with authority and coverage reported independently;
- declared security-sensitive classification producing a policy gap when no rule owns it, while `password.go` outside that declaration produces no invented security gap;
- waiver changing obligation disposition only, never evidence truth;
- partial relation-provider coverage unable to narrow a mandatory obligation;
- manifest-v2 fixed zero-parameter direct argv binding supporting an activated requirement;
- manifest-v1 and shell-form project commands remaining advisory-only for P1 starter binding;
- starter rendered-TOML round trip preserving profile provenance while repository-authored bytes and later starter-template changes do not alter the pinned effective digest;
- verification inspection never spawning project commands/providers.

Relation-ID derivation semantics remain owned by the core contract test `TestRelationIDIncludesDerivationSemantics`: basis, provider identity, derivation authority, coverage, and canonical provenance change identity; provenance ordering/deduplication and display-only timestamp/caveats do not.

### Integration defect found by the real-daemon matrix

The first real-daemon policy-gap fixture exposed a Stage-A integration defect hidden by the earlier unit fixture. The Git delta adapter places current dirty paths in `DeltaSample.Changes`; `ResolvedPaths` means paths that were dirty in the previous sample and are now resolved. `AffectedService` was incorrectly using only `ResolvedPaths`, so a current dirty workspace could produce an empty affected surface even while `git status` reported mutations.

A focused RED test reproduced the contract mismatch with current modified/replaced paths plus a resolved transition. The production fix now conservatively selects the normalized union of current `Changes.{OldPath,NewPath}` and `ResolvedPaths`. Commit:

```text
70f9704 fix: include current dirty paths in affected surface
```

The focused affected package and race tests passed after the fix.

### Practical Task-0 comparison

Historical baseline remains unchanged:

```text
scenario: docs_only_four_markdown_specs
historical operation: checkpoint-verify-specs-20260818
historical source fingerprint: 8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e
historical checkpoint selection: full
historical checkpoint elapsed: approximately 8 minutes on first cold/local run
```

A real-daemon Stage-A inspection of the same four-Markdown docs-only shape produced this one-run measurement:

```text
required rule: docs-contract
required provider class: static_format_check
Go rule disposition: not_triggered
affected relation count: 4
mechanical relations: 4
complete relations: 4
model-visible serialized inspection bytes: 4407
tool-call count: 1
inspection wall time: 58.261 ms
```

This measurement proves only the Stage-A inspection/obligation-selection shape. It does **not** claim an end-to-end runtime saving; actual evidence sufficiency/provider selection remains Stage B.

### Fresh Stage-A GREEN stack

Operation:

```text
operation_id: p1-task7-full-stage-a-green-20260819
session_id: 01M0AZYJWBCZ9DVPRK60N6RR4Z
result: terminal / exit 0
execution fingerprint: 99d456fba4d8ec606bb810eabfa61bf277f180e0aca6899fe41b94014d352426
source fingerprint reported by dirty affected: e372d7883153989bba09f013e36708f8d66ece1baa6079b048f902f74b53a22c
```

Fresh gates:

```text
real-daemon verification semantics: PASS
full Stage-A P1 packages: PASS
race app/verification + adapter/verification + adapter/store: PASS
devctl check: PASS
traceability: PASS core=24/24 roadmap=4/4 review=11/11 deferred=7/7
devctl test --dirty --base origin/main --json: PASS (selection=affected)
```

The full package run included the long real store/cmd suites (`internal/adapter/store` 358.845s; `cmd/shellbeam` 399.182s), and the dirty-affected run completed `cmd/shellbeam` in 420.437s. These are observed local timings, not performance targets.

### Stage-A checkpoint handoff before closure checkbox

The first full checkpoint was run after the acceptance/progress commits and before the final Step-6 bookkeeping commit:

```text
operation_id: p1-stage-a-checkpoint-handoff-1-20260819
session_id: 01M0BKBWNCQ3WVMC2WQCN6G0FH
HEAD: 92c9084
base: origin/main
selection: full
status: passed
exit_code: 0
source_fingerprint: 38817156a132c2f7fc6d451b6afcbec9643c39ac721a834899a3a09280102a20
started_at: 2026-08-18T23:30:07.250005Z
finished_at: 2026-08-18T23:35:30.265437Z
traceability: PASS core=24/24 roadmap=4/4 review=11/11 deferred=7/7
```

Stage-A limitations remain explicit: it projects affected surface, policy authority, obligations, and policy gaps but does not execute verification providers, evaluate evidence sufficiency, or claim `gate_status=clear`; those belong to Stage B. Manifest-v1 and shell-form project commands remain advisory-only for P1 typed binding, and bounded/partial Go relation coverage can widen but never narrow a mandatory obligation.
