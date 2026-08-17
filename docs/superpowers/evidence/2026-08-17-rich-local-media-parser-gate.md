# Rich Local Media Parser / Toolchain Gate Evidence

## Verdict

```text
parser_toolchain_status = FAIL
parser_toolchain_pass = false
candidate = go1.26-jsonv2-experiment
explicit_go126_experiment_acceptance = true
```

The execution authorization following the prior gate report explicitly requested continuing with the proposed next step, so the Go 1.26 global `GOEXPERIMENT=jsonv2` risk was accepted for this parser-gate experiment. This acceptance does not authorize production Tasks 1–10 unless the machine conjunction checker later returns exit `0`.

## Toolchain facts

```text
host_go = go1.26.5 darwin/arm64
candidate_mode = experimental
GOEXPERIMENT = jsonv2
GOOS = darwin
GOARCH = arm64
CGO_ENABLED = 1
```

Official Go status was rechecked on 2026-08-17: Go 1.27 is still not GA; official release notes say it is not yet released and the download page lists `go1.27rc2` as unstable. Therefore the stable Go 1.27 candidate remains unavailable and this was the only eligible candidate allowed by the approved design.

## Strict tracer result

Tracked report:

```text
path = docs/superpowers/evidence/reports/parser-go126-jsonv2/strict-tracer-report.json
sha256 = 0284736fa9e54fa738b4c681007b0757bc75c37d3df7fd77f383444b960d0436
verdict = PASS
exit_status = 0
five ambiguity classes = 5/5 PASS
modern-v2 semantic corpus = 18/18 PASS
legacy-v1 corpus = 4/4 PASS
```

The fixture corpus is schema v2 and freezes semantic SHA-256 values after deterministic test-only JSON normalization. It includes ten documented MCP-v2 payloads, eight complete IPC-v2 envelopes derived from repository schema tests, and four existing IPC-v1 fixture files.

## Candidate matrix

| Check | Status | Durable provenance |
|---|---|---|
| five-rejections | PASS | strict tracer report `0284736fa9e54fa738b4c681007b0757bc75c37d3df7fd77f383444b960d0436` |
| valid-v2-semantic | PASS | strict tracer report `0284736fa9e54fa738b4c681007b0757bc75c37d3df7fd77f383444b960d0436` |
| legacy-v1 | PASS | strict tracer report `0284736fa9e54fa738b4c681007b0757bc75c37d3df7fd77f383444b960d0436` |
| error-code-compat | PASS | `reports/parser-go126-jsonv2/race.log` |
| ordinary-build | **FAIL** | `reports/parser-go126-jsonv2/macos-build.log` |
| full-tests | **FAIL** | `reports/parser-go126-jsonv2/full-tests.log` |
| race | PASS | `reports/parser-go126-jsonv2/race.log` |
| macos-native | **FAIL** | `reports/parser-go126-jsonv2/macos-build.log` + matrix summary `0873fb619af2d8fefc2b8576e5e3eca5d82590edffaf09a8ab53d8ec3bb9f629` |
| linux-native | NOT_RUN | candidate already disqualified by full-repo/native-macOS hard failure |
| shell-acceptance | **FAIL** | production shell binary cannot build in the selected mode |
| non-media-json-regression | **FAIL** | A/B `lsp-normal.log` PASS vs `lsp-jsonv2.log` FAIL |
| mode-consistency | NOT_RUN | no native Linux/release-lane convergence after candidate hard failure |

Supplementary matrix evidence:

- hardening: FAIL because `tests/integration` cannot build under the same dependency skew;
- security mandatory checks: PASS; `govulncheck` was NOT_RUN because it is not installed;
- the first full `go test ./...` invocation exposed the compile failure, then the wrapper reached its 120 s bound while unrelated packages continued. The compile failure, not the wrapper timeout, is the candidate-failure reason.

## Reproducible non-media regression

Dependency identities:

```text
go.lsp.dev/protocol v1.0.1
github.com/go-json-experiment/json v0.0.0-20260623181947-01eb4420fa68
```

A/B compile proof:

```text
go test ./internal/adapter/codeintel/lsp -run '^$' -count=1
=> PASS

GOEXPERIMENT=jsonv2 go test ./internal/adapter/codeintel/lsp -run '^$' -count=1
=> FAIL
```

Experiment failure:

```text
codec_helpers.go:183: assignment mismatch: 2 variables but tok.Int returns 1 value
codec_helpers.go:209: assignment mismatch: 2 variables but tok.Uint returns 1 value
```

`go.lsp.dev/protocol` imports `github.com/go-json-experiment/json/jsontext`. That module normally provides `Token.Int/Uint` with `(value, error)` signatures, but when `goexperiment.jsonv2` is enabled its build-tagged alias layer targets the Go toolchain JSON-v2 API. `go.lsp.dev/protocol v1.0.1` is not compatible with that experiment mode on this bound repository/toolchain.

This is exactly a forbidden **non-media global JSON regression**. Task 0 does not authorize changing `go.mod`, `go.sum`, or code-intelligence production dependencies to rescue a parser candidate, so no dependency mutation was made.

## Derived decision

```text
go127_ga = false
explicit_go126_experiment_acceptance = true
candidate = go1.26-jsonv2-experiment
parser_toolchain_pass = false
parser_toolchain_status = FAIL
```

The only currently eligible parser candidate has failed a mandatory gate. Production Tasks 1–10 remain blocked. A future retry requires either an official Go 1.27 GA candidate or a separately approved design/plan change that creates another eligible parser/toolchain path without weakening the strict JSON contract.
