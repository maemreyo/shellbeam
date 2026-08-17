# Rich Local Media Parser / Toolchain Gate Evidence

## Verdict

```text
parser_toolchain_status = NOT_RUN
parser_toolchain_pass = false
candidate = none
```

## Current toolchain facts

```text
host_go = go1.26.5 darwin/arm64
GOTOOLCHAIN = auto
GOEXPERIMENT = <empty>
GOOS = darwin
GOARCH = arm64
CGO_ENABLED = 1
```

Official Go status checked on 2026-08-17:

- `https://go.dev/doc/go1.27` states Go 1.27 is not yet released.
- `https://go.dev/dl/` exposes Go 1.27 release candidates, not a Go 1.27 GA toolchain.

Therefore the preferred stable Go 1.27 JSON-v2 candidate is unavailable.

The approved design permits a Go 1.26 `GOEXPERIMENT=jsonv2` fallback only after explicit project acceptance of the global experiment risk and only if the identical mode is used across development, tests, CI, packaging, native macOS/Linux release lanes, and release artifacts. No such explicit risk acceptance has been given in the current execution authorization, so the experimental candidate was **not run**.

## Test-only tracer preparation

The repository now contains a narrow candidate tracer guarded by:

```go
//go:build goexperiment.jsonv2
```

It imports `encoding/json/v2`, uses `jsonv2.RejectUnknownMembers(true)`, freezes the five required ambiguity rejections, a small documented valid-v2 semantic-equivalence corpus, and the existing v1 JSON fixture files. An untagged source-contract test proves ordinary repository tests can inspect the candidate source without enabling the experiment.

Task-0 infrastructure tests ran in the ordinary host mode only. A separate ignored build-tag packaging probe confirmed the Go toolchain recognizes `goexperiment.jsonv2`; this was **not** counted as a parser candidate run.

## Required candidate matrix

| Check | Status | Provenance |
|---|---|---|
| five-rejections | NOT_RUN | candidate ineligible without stable Go 1.27 or explicit Go 1.26 experiment acceptance |
| valid-v2-semantic | NOT_RUN | same |
| legacy-v1 | NOT_RUN | same |
| error-code-compat | NOT_RUN | same |
| ordinary-build | NOT_RUN | same |
| full-tests | NOT_RUN | same |
| race | NOT_RUN | same |
| macos-native | NOT_RUN | same |
| linux-native | NOT_RUN | same |
| shell-acceptance | NOT_RUN | same |
| non-media-json-regression | NOT_RUN | same |
| mode-consistency | NOT_RUN | same |

## Derived decision

```text
go127_ga = false
explicit_go126_experiment_acceptance = false
parser_toolchain_pass = false
parser_toolchain_status = NOT_RUN
```

Do not infer a PASS from source compilation probes or from ordinary Go 1.26 repository tests. One eligible candidate must run the complete frozen matrix before production Tasks 1–10 can be unlocked.
