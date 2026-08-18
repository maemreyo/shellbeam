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
