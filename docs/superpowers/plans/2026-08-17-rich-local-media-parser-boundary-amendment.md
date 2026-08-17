# Rich Local Media Parser Boundary Amendment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the rejected whole-build Go 1.26 JSON-v2 experiment candidate with an exact pinned library-boundary candidate while preserving the five strict JSON properties and keeping Phase A unchanged.

**Architecture:** `internal/core/jsonstrict.Decode` remains the only reviewed modern strict-decoding boundary and uses the exact pinned `github.com/go-json-experiment/json` module. The parser tracer and machine pre-implementation checker identify that module explicitly, require global `GOEXPERIMENT` to be empty, and keep native-lane equality mandatory. This amendment does not authorize media production work unless the conjunction checker itself returns exit `0`.

**Tech Stack:** Go 1.26.5, `github.com/go-json-experiment/json@v0.0.0-20260623181947-01eb4420fa68`, Python-based gate checker, JSON Schema, existing `devctl` verification.

## Global Constraints

- Preserve all existing Phase A thresholds and fresh-conversation requirements verbatim.
- Do not touch current dirty bridge/IPC/media runtime work while implementing this amendment.
- Candidate identity is exactly `go1.26-pinned-json-library-boundary`.
- Global `GOEXPERIMENT` must be empty for this candidate.
- Exact module version is `v0.0.0-20260623181947-01eb4420fa68`.
- Protocol-v1 decoding remains unchanged.
- The five strict rejection properties remain invalid UTF-8, duplicate names, unknown members, wrong-case names, and trailing JSON.
- Parser PASS still requires both macOS and Linux native lanes PASS in the identical selected mode. Cross-build is not native PASS.
- No push or PR.

---

### Task 1: Amend the canonical parser contract

**Files:**
- Modify: `docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md`
- Modify: `docs/superpowers/plans/2026-08-17-rich-local-media-access.md`

**Interfaces:**
- Produces: the approved candidate identity and exact build/module invariants consumed by Task 2.

- [ ] **Step 1: Replace the conditional Go 1.26 whole-build experiment fallback with the pinned library-boundary candidate.**
- [ ] **Step 2: Mark `go1.26-jsonv2-experiment` rejected on the current dependency graph and require a future amendment to reopen it.**
- [ ] **Step 3: Update all parser-gate cross-references and Task-0 command examples to run without global `GOEXPERIMENT=jsonv2`.**
- [ ] **Step 4: Run markdown/source consistency scans.**

```bash
rg -n 'GOEXPERIMENT=jsonv2|go1.26-jsonv2-experiment|go1.26-pinned-json-library-boundary|github.com/go-json-experiment/json' docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md docs/superpowers/plans/2026-08-17-rich-local-media-access.md
git diff --check -- docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md docs/superpowers/plans/2026-08-17-rich-local-media-access.md
```

### Task 2: Make the parser tracer and checker understand the pinned library boundary

**Files:**
- Modify: `tools/rich-media-parser-gate/main.go`
- Modify: `tools/rich-media-parser-gate/candidate_test.go`
- Modify: `tools/rich-media-parser-gate/main_test.go`
- Modify: `scripts/check-json-mode.sh`
- Modify: `scripts/test-json-mode.sh`
- Modify: `scripts/check-rich-media-preimplementation-gate.sh`
- Modify: `scripts/test-rich-media-preimplementation-gate.sh`
- Modify: `docs/superpowers/evidence/schemas/rich-local-media-preimplementation-gate-v1.schema.json`

**Interfaces:**
- Produces: parser report fields `candidate_mode`, `module_version`, and `goexperiment`, plus machine validation of exact candidate identity across native lanes.

- [ ] **Step 1: Write RED tests for the new candidate identity.**

Required cases:

```text
go1.26.5 + GOEXPERIMENT='' + exact module => PASS candidate selection
go1.26.5 + GOEXPERIMENT=jsonv2 => reject
go1.26.5 + wrong module version => reject
checker valid library-boundary fixture => exit 0
checker mismatched module => exit 4
checker non-empty GOEXPERIMENT => exit 4
```

- [ ] **Step 2: Run RED.**

```bash
go test ./tools/rich-media-parser-gate -count=1
./scripts/test-json-mode.sh
./scripts/test-rich-media-preimplementation-gate.sh
```

- [ ] **Step 3: Implement minimal GREEN.**

The tracer imports `github.com/go-json-experiment/json`, has no `goexperiment.jsonv2` build tag, records the exact module version, and rejects any non-empty global `GOEXPERIMENT`. The checker accepts a passing candidate only when `candidate=go1.26-pinned-json-library-boundary`, `mode=library-boundary`, `go_version` is Go 1.26.x, `goexperiment=''`, `module_version` is exact, tracer identity matches, and both native lanes match all identity fields.

- [ ] **Step 4: Run focused GREEN and race.**

```bash
./scripts/check-json-mode.sh
go test ./tools/rich-media-parser-gate -count=1
go test -race ./internal/adapter/ipc ./internal/adapter/mcp ./tests/contract -count=1
./scripts/test-json-mode.sh
./scripts/test-rich-media-preimplementation-gate.sh
```

### Task 3: Regenerate honest parser evidence and gate state

**Files:**
- Modify: `docs/superpowers/evidence/2026-08-17-rich-local-media-parser-gate.md`
- Modify: `docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json`
- Modify/Create: `docs/superpowers/evidence/reports/parser-go126-library-boundary/*`

**Interfaces:**
- Produces: durable PASS/FAIL/NOT_RUN parser evidence. Final top-level verdict remains derived from parser evidence AND unchanged Phase A evidence.

- [ ] **Step 1: Run the exact local parser matrix without global experiments.**

```bash
./scripts/check-json-mode.sh
go test ./tools/rich-media-parser-gate -count=1
go run ./tools/rich-media-parser-gate -fixtures ./tools/rich-media-parser-gate/testdata/fixtures.json -out .build/rich-local-media-parser-gate/report.json
go test -race ./internal/adapter/ipc ./internal/adapter/mcp ./tests/contract -count=1
go test ./... -count=1
./scripts/test-hardening.sh
./scripts/test-security.sh
go build -trimpath -buildvcs=false -o .build/gates/library-boundary/shellbeam ./cmd/shellbeam
```

- [ ] **Step 2: Run native macOS evidence and attempt a real Linux native lane only if an actually executable Linux environment is locally available.** Cross-build remains compile-only and cannot be recorded as native PASS.
- [ ] **Step 3: Persist the honest parser state.** If all mandatory parser rows and both native lanes PASS, set parser PASS. Otherwise record FAIL/NOT_RUN exactly; do not forge conjunction.
- [ ] **Step 4: Run the machine checker.**

```bash
./scripts/check-rich-media-preimplementation-gate.sh
```

Expected final exit is `0` only if both parser and the unchanged Phase A matrix PASS; otherwise `3` or `4` is retained honestly.
