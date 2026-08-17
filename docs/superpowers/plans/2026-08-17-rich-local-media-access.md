# Rich Local Media Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. This project is **main-agent-only**; do not dispatch subagents.

**Status:** DRAFT-GATED / PREFLIGHT-UNBOUND. **Do not execute Task 0 until Task -1 binds a current execution-base SHA and the exact approved design source. Do not execute production Tasks 1–10 unless the machine gate exits 0 after deriving `phase_a_pass && parser_toolchain_pass`.**

**Known reviewed design source identity:** `revised-rich-local-media-access-design-v8.md`, 1,657 lines, 79,933 bytes, SHA-256 `7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906`. This identity was verified from the user-provided review artifact in the authoring session, but the bytes are **not yet repository-bound**. Task -1 must materialize an artifact with this exact identity into the repository evidence source path or stop.

**Authoring-time drift snapshot (informational only):** after `git fetch origin` on 2026-08-17, this docs branch was ahead 2 / behind 128 relative to `origin/main`; `origin/main` resolved to `a92a52fba7d123c50d84e5b879e65d176bd25444`, and 22 of 67 declared plan touchpoints differed from the branch base. Task -1 must recompute these facts; this snapshot is never an execution base.

**Goal:** Add one bounded `read_media` image action to ShellBeam so ChatGPT Web can read one explicitly addressed local PNG/JPEG/WebP through the existing `local_shell` MCP tool, with exact address correlation, fail-closed bridge/daemon negotiation, safe daemon-owned filesystem acquisition, and no durable media state.

**Architecture:** Keep MCP as a translator and daemon as the local authority. A negotiated, immutable media contract gates dynamic MCP schema exposure; the daemon validates the public address, acquires one file through a descriptor-relative no-follow localfs adapter, returns a bounded IPC-v2 media envelope, and the bridge enforces the outer response ceiling plus exact `display_address` correlation before constructing `mcp.ImageContent`. Rich media owns no cache, database, resource URI, generic binary channel, or execution authority.

**Tech Stack:** Go; official MCP Go SDK `github.com/modelcontextprotocol/go-sdk v1.7.0`; selected strict-JSON toolchain from Task 0; `golang.org/x/sys/unix`; candidate `golang.org/x/image/webp`; existing Unix-socket HTTP IPC v2; native macOS/Linux tests; Secure MCP Tunnel Phase B revalidation.

## Global Constraints

- Public topology remains one tool, `local_shell`, unless the completed Phase A scorecard forces the already-designed dedicated-tool fallback.
- V1 media kind is image only; accepted formats are PNG/JPEG/WebP, subject to dependency/runtime gates.
- **Raw image cap is 7 MiB = 7,340,032 bytes**, replacing the failed 8 MiB candidate. Phase A observed the official Secure MCP Tunnel reject the 8 MiB raw fixture after base64/envelope expansion with HTTP `413 request_body_too_large` at a 10,485,760-byte tunnel body ceiling; 7 MiB passed the real-host max-payload trial 3/3.
- For 7 MiB, base64 payload length is 9,786,712 bytes. With the frozen 65,536-byte envelope allowance, the media-specific IPC outer ceiling is **9,852,248 bytes**.
- Width <= 16,384; height <= 16,384; pixels <= 40,000,000; one file/call; path <= 1,024 bytes; cwd <= 1,024 bytes; <= 64 logical path components.
- Media concurrency = 2. Cooperative acquisition budget = 5 s. A timed-out caller does not release a slot until its worker actually exits. `media_read_timeout` is non-retryable in V1.
- Admission occurs after strict JSON + lexical path validation and before workspace resolution, base opening, or any other filesystem-backed work.
- `display_address` is exact caller-visible identity: either `{address_kind:"workspace", workspace_id, raw path}` or `{address_kind:"cwd", caller-supplied cwd, raw path}`. Canonical/resolved filesystem paths never replace it.
- The bridge derives `expected_display_address` from the strict-decoded request and rejects any missing/wrong-kind/wrong-base/canonicalized daemon identity as `invalid_daemon_response` before emitting image content.
- The daemon's ordinary legacy-compatible `inspect.server` always omits rich-local-media. Only a compatible new-bridge/new-daemon effective projection may expose it after private opt-in negotiation.
- Raw image bytes/base64/EXIF/GPS/ICC/comments/trailing bytes/canonical paths never enter receipts, session output, telemetry, repro, event journal, activities, manifests, ordinary logs, crash evidence, or content hashes.
- Successful metadata/disclosure must truthfully say original selected file bytes egress to the connected MCP client/model and may contain embedded metadata.
- No streaming, FD passing, public temp file server, generic binary transfer, transform/resize/OCR framework, cache, durable artifact database, or media resource URI in V1.
- Ordinary `start`/`poll`/`write`/`kill`/inspection hot paths perform zero media filesystem/decoder/admission work when media is unused.
- Use TDD for every production behavior: RED test, minimal GREEN, focused verification, then commit. Preserve existing dirty work; no push or PR unless explicitly requested.

## Global Execution Contract

Task -1 records the immutable full SHA as `EXECUTION_BASE`. Every later task uses that SHA, never a moving branch name, for dirty/affected selection and provenance.

For **every production Task 1–10**, the first command is:

```bash
./scripts/check-rich-media-preimplementation-gate.sh
```

Expected: exit `0`. Exit `3` means honest `NOT_RUN`/incomplete evidence; exit `4` means FAIL or invalid/stale provenance. Either non-zero exit stops the task before editing production code.

After the checker exits 0, reload the immutable base from the gate manifest in each fresh shell/session:

```bash
EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
test -n "$EXECUTION_BASE"
```

After each task's focused GREEN command and before staging, run:

```bash
go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
```

If the task changes documentation/test data only and `devctl build --dirty` returns an empty legitimate source selection, preserve that empty result; never broaden it silently. Full fresh repository/release suites belong only to Task 0's parser gate and Task 10's release checkpoint.

Every local commit stages only the intended task scope, checks the staged diff, and invokes the tracked worktree-safe hook with the **exact task-specific commit message written in that task's Commit step**. There is no generic commit-message placeholder in this plan.

Do not install hooks into shared `.git/hooks`; do not stash/reset unrelated work; do not push or open a PR unless explicitly requested.

---

### Task -1: Bind Current Execution Base and Approved Design Source

**Files:**
- Modify: `docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md`
- Create: `docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md`
- Inspect only: `AGENTS.md`
- Inspect only: `docs/superpowers/plans/2026-08-17-rich-local-media-access.md`
- Inspect only: `docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md`

**Interfaces:**
- Consumes: a user-supplied artifact whose exact required identity is SHA-256 `7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906`, 1,657 lines, 79,933 bytes.
- Produces: immutable `EXECUTION_BASE`, repository-bound approved-source bytes/SHA, fresh plan SHA-256, current contributor-contract digest, and a preflight verdict `PASS|FAIL|NOT_RUN`.

- [ ] **Step 1: Require a clean isolated docs worktree and fetch the intended base**

  ```bash
  if [ -n "$(git status --porcelain)" ]; then
    printf '%s\n' 'rich-media Task -1: NOT_RUN: worktree is dirty; repository/index left unchanged' >&2
    exit 3
  fi
  git fetch origin
  EXECUTION_BASE="$(git rev-parse origin/main)"
  test -n "$EXECUTION_BASE"
  git merge-base --is-ancestor "$(git merge-base HEAD "$EXECUTION_BASE")" HEAD
  ```

  If the worktree is dirty, this is a **pre-mutation `NOT_RUN`**. Report it only in the executor response/stdout and stop immediately: do not create/update the preflight file, materialize the approved source, fetch, rebase, stage, commit, stash, reset, or otherwise mutate repository/index state. The dirty-tree path intentionally has no repository evidence commit because preserving unrelated work takes precedence. A dry-run check for this branch compares the pre-call `git status --porcelain=v1` and index/tree identities with the post-call values and requires them to be byte-for-byte unchanged. `origin/main` is only the discovery ref; all later commands use the recorded full SHA.

- [ ] **Step 2: Synchronize the docs branch only with explicit user authorization**

  Before rewriting local branch history, require the current execution instruction to authorize synchronizing this worktree. If authorization is absent after the clean Step-1 check, do not rebase; set `NOT_RUN`, continue only to Step 5 branch B to persist the evidence-only record, then stop. When authorized:

  ```bash
  git rebase "$EXECUTION_BASE"
  git merge-base --is-ancestor "$EXECUTION_BASE" HEAD
  test "$(git rev-list --count HEAD.."$EXECUTION_BASE")" -eq 0
  ```

  On conflict, resolve only this branch's docs changes against current repository reality; never reset unrelated work. If any production ownership boundary changes materially, abort Task -1 and revise/re-review the plan before Task 0.

- [ ] **Step 3: Materialize and verify the exact approved v8 source**

  Place the supplied bytes at:

  ```text
  docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md
  ```

  Then require:

  ```bash
  test "$(wc -l < docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md | tr -d ' ')" = 1657
  test "$(wc -c < docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md | tr -d ' ')" = 79933
  test "$(shasum -a 256 docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md | awk '{print $1}')" = 7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906
  ```

  Never reconstruct the source from chat memory, conversational revision labels, or a partial diff. Any missing source or line/byte/SHA mismatch is `FAIL`; continue only to Step 5 branch B to persist the evidence-only record, then stop.

- [ ] **Step 4: Re-trace every declared production touchpoint on the bound base with task-order dependencies**

  Parse every `Create/Modify/Test` declaration in task order. Do **not** test each `Modify` independently against `EXECUTION_BASE`; later tasks may legitimately modify a path created by an earlier task, and this docs branch may already own reviewed governance inputs that do not exist on main. Build one deterministic availability state:

  1. Seed `available` with every path that exists on `EXECUTION_BASE`.
  2. Add only branch-owned paths under `docs/superpowers/**` that are tracked at the synchronized preflight `HEAD` but absent on `EXECUTION_BASE`. These are reviewed governance/spec/plan/evidence inputs carried by this docs branch. A branch-owned path outside `docs/superpowers/**` that is absent on `EXECUTION_BASE` is **not** a valid seed and is an immediate `FAIL`; Task -1 must not use branch history to smuggle a production touchpoint past base validation.
  3. Walk declarations from Task -1 through Task 10 in order. For `Create path`, require that `path` is not already available at that task boundary unless the declaration explicitly names an existing branch-owned evidence record that this task is updating; then add it to `available`. For `Modify path` or `Test path`, require that `path` is already available from the base, the reviewed branch-owned docs seed, or a strictly earlier task.
  4. Record provenance for every non-base resolution as either `branch_owned_docs` or `created_by_task=<N>`. A path first created by the same or a later task cannot satisfy an earlier `Modify/Test`.

  The current Task -1 preflight evidence file is a branch-owned evidence record after the first failed attempt, so its Files declaration is `Modify`; the exact approved v8 source remains `Create` until a PASS attempt persists it. `docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md` is valid only through the `branch_owned_docs` seed. `docs/superpowers/evidence/2026-08-17-rich-local-media-phase-a.md` is valid for Task 10 only because Task 0 creates it first.

  Re-open the current IPC/MCP/daemon/bridge/capability/failure symbols named by Tasks 1–10 and confirm the Files/Interfaces/test commands still match current code. The path state machine proves only declaration availability; it does not waive semantic symbol/interface re-tracing.

  Record the authoring-time drift facts only as historical context; the preflight evidence must contain the fresh dependency-aware path inventory and symbol inventory against `EXECUTION_BASE`. If any declaration cannot be resolved by the rules above, or any named production symbol/interface has materially changed, set verdict `FAIL`, continue to Step 5 branch B and commit the honest evidence-only record, then stop. Revise/re-review the plan only after that failed preflight attempt is durably closed; rerun Task -1 from a clean worktree.

- [ ] **Step 5: Write and commit the preflight record for PASS, FAIL, or NOT_RUN**

  `docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md` records at minimum:

  ```text
  verdict = PASS | FAIL | NOT_RUN
  execution_base_sha = exact lowercase 40-hex output of `git rev-parse origin/main` captured before rebase
  branch_head_sha = exact lowercase 40-hex `HEAD` after successful synchronization
  plan_sha256 = exact 64-hex SHA-256 of this plan after synchronization
  approved_source_path = docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md
  approved_source_sha256 = 7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906
  approved_source_lines = 1657
  approved_source_bytes = 79933
  contributor_contract_sha256 = exact 64-hex SHA-256 of the active root `AGENTS.md`
  touchpoint_inventory = PASS | FAIL
  ```

  Persistence is explicitly split by when the attempt stops:

  **A. Dirty at Step 1 -> `NOT_RUN`, zero repository mutation.** The executor reports the verdict/reason outside the worktree (response/stdout) and exits. Do not write, stage, or commit any Task -1 file. This is the only non-PASS branch without a repository evidence commit.

  **B. The worktree passed the Step-1 dirty check, then any later preflight operation (including fetch/base discovery inside Step 1) ends `FAIL` or `NOT_RUN`.** Write an honest `docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md` with the identities that were actually observed plus the exact failure/NOT_RUN reason. Never claim unavailable fields were verified. Before this evidence-only commit, finish or abort any in-progress rebase so the repository is again clean except for Task -1-owned evidence/source files; if that cannot be done without touching unrelated work, fall back to the same zero-mutation external `NOT_RUN` reporting rule as branch A. Commit only the durable preflight evidence through the repository workflow. `VERIFY_BASE` uses the discovered immutable base when available, otherwise the unchanged clean authoring HEAD, so an early fetch/base-discovery failure does not require an unset variable:

  ```bash
  VERIFY_BASE="${EXECUTION_BASE:-$(git rev-parse HEAD)}"
  git diff --check
  go run ./tools/devctl test --dirty --base "$VERIFY_BASE" --json
  go run ./tools/devctl build --dirty --base "$VERIFY_BASE" --json
  git add docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md
  git diff --cached --check
  test "$(git diff --cached --name-only)" = "docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md"
  git -c core.hooksPath=.githooks commit -m "docs: record rich media preflight failure"
  ```

  If this clean attempt materialized `docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md` before later failing, record its observed identity in the preflight evidence, but do not stage it on a non-PASS verdict. After the evidence commit, remove that source file **only if this Task -1 attempt created it from an initially absent path**; never remove or overwrite a file that pre-existed the attempt. The resulting worktree must return to the same clean state it had at Step 1.

  **C. PASS.** Preserve the existing PASS workflow and bind both the preflight record and the exact approved source:

  ```bash
  git diff --check
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md
  git diff --cached --check
  test "$(git diff --cached --name-only)" = "docs/superpowers/evidence/2026-08-17-rich-local-media-preflight.md
docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md"
  git -c core.hooksPath=.githooks commit -m "docs: bind rich media execution inputs"
  ```

  Static acceptance for Task -1 requires all three branches above to have explicit persistence behavior: dirty Step 1 = zero mutation; clean later FAIL/NOT_RUN = evidence-only tracked-hook commit; PASS = evidence + exact approved-source tracked-hook commit. Task 0 may start only from `verdict=PASS` with all recorded identities still matching.

### Task 0: Close the Machine-Verifiable Conjunction Gate and Promote the Runtime Limit

**Files:**
- Modify: `docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md`
- Create: `docs/superpowers/evidence/2026-08-17-rich-local-media-phase-a.md`
- Create: `docs/superpowers/evidence/2026-08-17-rich-local-media-parser-gate.md`
- Create: `docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json`
- Create: `docs/superpowers/evidence/schemas/rich-local-media-preimplementation-gate-v1.schema.json`
- Create: `scripts/check-rich-media-preimplementation-gate.sh`
- Create: `scripts/test-rich-media-preimplementation-gate.sh`
- Create: `tools/rich-media-parser-gate/main.go`
- Create: `tools/rich-media-parser-gate/main_test.go`
- Create: `tools/rich-media-parser-gate/testdata/fixtures.json`
- Inspect only: `docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md`
- Inspect only: `.build/rich-local-media-phase-a/evidence-local-preflight/manifest.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-local-preflight/goldens.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-local-preflight/scorecard.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run1/manifest.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run1/goldens.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run1/scorecard.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run2-413/manifest.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run2-413/goldens.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host-run2-413/scorecard.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host/manifest.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host/goldens.json`
- Inspect only: `.build/rich-local-media-phase-a/evidence-real-host/scorecard.json`
- Inspect only: `.build/rich-local-media-phase-a/phase-a-tunnel.log`

**Interfaces:**
- Consumes: Task -1 PASS record, exact repository-bound approved v8 source, and provenance-bearing Phase A runtime evidence.
- Produces: canonical tracked spec with measured 7 MiB delta; durable PASS/FAIL/NOT_RUN evidence; a versioned gate manifest; `scripts/check-rich-media-preimplementation-gate.sh` whose exit `0` is the **only** authorization for Tasks 1–10.

- [ ] **Step 1: Re-verify Task -1 identities before touching gate evidence**

  Read `EXECUTION_BASE` and recorded SHA values from the preflight record. Require the current plan, approved source, contributor contract and base ancestry to match exactly. A stale/missing identity records Task 0 `NOT_RUN` and stops before evidence normalization.

- [ ] **Step 2: Promote the exact approved source and apply only the measured 7 MiB delta**

  Copy from the repository-bound source, never from chat memory:

  ```bash
  cp docs/superpowers/evidence/sources/2026-08-17-rich-local-media-access-design-v8.md docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md
  ```

  Then change the former 8 MiB candidate to the measured 7 MiB candidate everywhere that controls production/payload/memory/native behavior. Freeze:

  ```text
  raw_image_bytes = 7,340,032
  base64_bytes = 9,786,712
  envelope_budget = 65,536
  maxOuter = 9,852,248
  tunnel_body_cap_observed = 10,485,760
  ```

  Preserve the historical 8 MiB failure only where explicitly labeled rejected-candidate evidence. The later live-tunnel attempt is a real `FAIL/transport-ceiling`; the earlier timed-out/stale-prompt attempts remain `NOT_RUN`. The replacement 7 MiB max case is evidence input `PASS 3/3`, not proof of overall Phase A.

- [ ] **Step 3: Normalize the complete Phase A matrix with provenance**

  Write one row for every frozen Section 21 golden class and annotation configuration. Each row records exact prompt/golden ID, complete `display_address`, expected call count, observed call count, expected/observed byte count, confirmation/disclosure facts, fresh-conversation identifier, manifest/token provenance and verdict.

  The required matrix includes PNG/JPEG/WebP vision, direct explicit, indirect positive, established-address follow-up, negative zero-call/zero-byte, unsupported zero-call/zero-byte, sensitive/unestablished zero-call/zero-byte, collision A 3/3, collision B 3/3, max 7 MiB 3/3, annotation policy, and remembered-approval case or explicit `not_applicable` allowed by the frozen scorecard.

  Missing evidence is `NOT_RUN`, never inferred PASS. Archived scorecards that still say 27/27 `NOT_RUN` remain historical inputs and are not edited to manufacture results.

- [ ] **Step 4: Build the narrow strict-decode parser tracer before any production decoder exists**

  `tools/rich-media-parser-gate` is test-only and imports the exact candidate API/options. `fixtures.json` freezes:

  ```text
  five negative classes: invalid UTF-8, duplicate names, unknown members, wrong-case names, trailing JSON
  all documented valid modern MCP-v2 payload fixtures
  all documented valid IPC-v2 payload fixtures
  representative legacy v1 fixtures
  expected semantic JSON digests after canonical test-only normalization
  ```

  The tracer requires 5/5 negative rejection, 100% valid-v2 semantic equivalence with the pre-candidate baseline, and unchanged legacy-v1 semantics. It writes a hashed report containing exact `go version`, `go env GOEXPERIMENT GOOS GOARCH CGO_ENABLED`, candidate mode, command and exit status. Task 1 later productionizes this already-proven boundary; it does not discover the parser contract for the first time.

- [ ] **Step 5: Run the complete parser/toolchain candidate matrix**

  First re-check official Go status. Stable Go 1.27+ stdlib JSON-v2 is eligible only after official GA. Otherwise the Go 1.26 `GOEXPERIMENT=jsonv2` candidate is eligible only after explicit project acceptance of global experiment risk.

  For the Go 1.26 candidate, the local commands include:

  ```bash
  GOEXPERIMENT=jsonv2 go test ./tools/rich-media-parser-gate -count=1
  GOEXPERIMENT=jsonv2 go run ./tools/rich-media-parser-gate -fixtures ./tools/rich-media-parser-gate/testdata/fixtures.json -out .build/rich-local-media-parser-gate/report.json
  GOEXPERIMENT=jsonv2 go test -race ./internal/adapter/ipc ./internal/adapter/mcp ./tests/contract -count=1
  GOEXPERIMENT=jsonv2 go test -count=1 ./...
  GOCACHE="$(go env GOCACHE)" GOEXPERIMENT=jsonv2 ./scripts/test-hardening.sh
  GOEXPERIMENT=jsonv2 ./scripts/test-security.sh
  GOEXPERIMENT=jsonv2 go build -trimpath -buildvcs=false -o .build/gates/jsonv2/shellbeam ./cmd/shellbeam
  go version
  GOEXPERIMENT=jsonv2 go env GOEXPERIMENT GOOS GOARCH CGO_ENABLED
  ```

  Native macOS and Linux release/checkpoint lanes must use the identical selected mode. If remote/native lanes require push/PR or another external action not explicitly authorized, mark the parser candidate `NOT_RUN`; local success/cross-build cannot substitute.

- [ ] **Step 6: Define a versioned manifest whose verdict is derived, not trusted**

  `rich-local-media-preimplementation-gate-v1.schema.json` requires immutable base/spec/plan/source hashes, the complete Phase A row set, parser candidate identity, strict-tracer report hash, native lane evidence/hashes, and evidence-document hashes. The JSON manifest may contain reported summary fields, but the checker recomputes:

  ```text
  phase_a_pass = every required Phase A row/threshold passes
  parser_toolchain_pass = strict tracer + valid corpus + v1 + race + full repo + identical native release lanes pass
  final_pass = phase_a_pass && parser_toolchain_pass
  ```

  The checker rejects missing/duplicate rows, threshold misses, stale base/spec/plan/source hashes, missing native lanes, parser-mode mismatch, missing evidence files, bad hashes, and any hand-edited summary `true` inconsistent with derived facts.

- [ ] **Step 7: RED/GREEN the checker itself**

  `scripts/test-rich-media-preimplementation-gate.sh` creates bounded temporary fixtures for: missing row, duplicate row, threshold miss, stale base SHA, stale spec SHA, stale plan SHA, wrong approved-source SHA, missing parser tracer report, missing native lane, mismatched `GOEXPERIMENT`, forged top-level `true`, explicit FAIL, explicit NOT_RUN, and one fully valid fixture.

  Expected exits:

  ```text
  0 = complete derived PASS
  3 = honest NOT_RUN / incomplete mandatory evidence
  4 = FAIL or invalid/stale/forged evidence
  ```

- [ ] **Step 8: Always persist and commit the honest Task 0 outcome before authorization**

  Write the two human-readable evidence docs plus the JSON manifest for PASS, FAIL, or NOT_RUN. Do **not** skip verification/commit when the conjunction is false.

  ```bash
  ./scripts/test-rich-media-preimplementation-gate.sh
  rg -n 'TBD|TODO|FIXME' docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md docs/superpowers/evidence/2026-08-17-rich-local-media-*.md
  git diff --check
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add docs/superpowers/specs/2026-08-16-rich-local-media-access-design.md docs/superpowers/evidence docs/superpowers/evidence/schemas scripts/check-rich-media-preimplementation-gate.sh scripts/test-rich-media-preimplementation-gate.sh tools/rich-media-parser-gate
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "docs: record rich media pre-implementation gate"
  ```

- [ ] **Step 9: Run the authorization checker after the evidence commit**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  ```

  Exit `0` authorizes Task 1. Exit `3` or `4` stops the plan with capability unadvertised and the honest evidence retained in history.

### Task 1: Freeze Strict JSON and the Reproducible Build Mode

**Files:**
- Create: `internal/adapter/jsonstrict/decode.go`
- Create: `internal/adapter/jsonstrict/decode_test.go`
- Create: `internal/adapter/jsonstrict/decode_fuzz_test.go`
- Create: `scripts/check-json-mode.sh`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/discovery_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `Makefile`
- Modify: `.github/workflows/checkpoint.yml`
- Modify: `.github/workflows/nightly.yml`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: the exact parser mode selected by Task 0.
- Produces: `jsonstrict.Decode(data []byte, dst any) error`; one checked build-mode guard used consistently by dev/CI/test/release.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED strict-decoder tests**

  Test the exact five security properties against a small struct and against representative MCP/IPC-v2 inputs:

  ```go
  func TestDecodeRejectsAmbiguousJSON(t *testing.T) {
      type payload struct { Action string `json:"action"` }
      cases := [][]byte{
          []byte("{\"action\":\"x\",\"action\":\"y\"}"),
          []byte("{\"Action\":\"x\"}"),
          []byte("{\"action\":\"x\",\"unknown\":1}"),
          append([]byte("{\"action\":\""), 0xff, '"', '}'),
          []byte("{\"action\":\"x\"} {}"),
      }
      for _, in := range cases {
          var got payload
          if err := Decode(in, &got); err == nil { t.Fatalf("accepted %q", in) }
      }
  }
  ```

  Also snapshot all current valid MCP-v2 and IPC-v2 fixtures before changing production decoding.

- [ ] **Step 2: Run RED**

  ```bash
  go test ./internal/adapter/jsonstrict ./internal/adapter/mcp ./internal/adapter/ipc -run 'TestDecodeRejectsAmbiguousJSON|TestCompatibilityV1Fixtures|TestDiscoveryCurrentClientGetsV2ToolAndCatalog' -count=1
  ```

  Expected: `internal/adapter/jsonstrict` does not exist / `Decode` undefined.

- [ ] **Step 3: Implement the selected strict decoder**

  For a selected stdlib JSON-v2 mode, the boundary is:

  ```go
  package jsonstrict

  import jsonv2 "encoding/json/v2"

  func Decode(data []byte, dst any) error {
      return jsonv2.Unmarshal(data, dst, jsonv2.RejectUnknownMembers(true))
  }
  ```

  JSON-v2 defaults reject invalid UTF-8, duplicate names, and case-insensitive member matching; `RejectUnknownMembers(true)` closes unknown members, and `Unmarshal` consumes one complete JSON value. Do not enable `AllowInvalidUTF8`, `AllowDuplicateNames`, or case-insensitive matching. Preserve protocol-v1 decoding unchanged.

- [ ] **Step 4: Route modern MCP/IPC-v2 decode through the one boundary**

  Change `mcp.decodeInput`/modern validation and `ipc.strictDecodeV2` to call `jsonstrict.Decode`. Retain branch-specific field-set validation where it communicates cross-action errors, but do not maintain a second permissive JSON parser for media/v2 security decisions.

- [ ] **Step 5: Add the whole-build reproducibility guard**

  `scripts/check-json-mode.sh` reads the Task-0 frozen mode and fails when `go version` / `go env GOEXPERIMENT` disagree. Wire it before `build`, `test`, `hardening`, `security`, `devctl-verify`, and the three GitHub workflows. For the experimental Go 1.26 mode, CI workflow jobs set:

  ```yaml
  env:
    GOEXPERIMENT: jsonv2
  ```

  For the stable Go 1.27+ mode, the guard requires no `jsonv2` experiment flag.

- [ ] **Step 6: GREEN and regression verification**

  ```bash
  ./scripts/check-json-mode.sh
  go test ./internal/adapter/jsonstrict ./internal/adapter/mcp ./internal/adapter/ipc ./tests/contract -count=1
  ```

  Expected: all strict negatives reject; existing valid v2 semantics and v1 fixtures remain green. Do not rerun a fresh full repository/hardening suite here; Task 0 already gated the selected mode and Task 10 owns release-scale verification.

- [ ] **Step 7: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/adapter/jsonstrict internal/adapter/mcp/input.go internal/adapter/mcp/discovery_test.go internal/adapter/ipc/protocol_v2.go internal/adapter/ipc/protocol_v2_test.go scripts/check-json-mode.sh Makefile .github/workflows/checkpoint.yml .github/workflows/nightly.yml .github/workflows/release.yml
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "build: freeze strict json mode"
  ```

### Task 2: Introduce the Core Media Contract and Dynamic Schema Fragments

**Files:**
- Create: `internal/core/media/contract.go`
- Create: `internal/core/media/contract_test.go`
- Create: `internal/core/media/path.go`
- Create: `internal/core/media/path_test.go`
- Create: `api/schema/mcp-read-media-input-v1.json`
- Create: `api/schema/mcp-read-media-output-v1.json`
- Modify: `api/schema/embed.go`
- Modify: `api/schema/embed_test.go`
- Modify: `tests/contract/schema_contract_test.go`
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`

**Interfaces:**
- Produces: `media.DisplayAddress`, `media.LogicalPath`, `media.Limits`, `media.File`, `media.Result`, exact V1 constants used by daemon/IPC/bridge/MCP, and stable media failure constants available to later tasks.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED contract/path tests**

  Cover workspace/CWD exclusivity, exact raw display preservation, empty/absolute/backslash/NUL/dot/dotdot/empty-component/trailing-slash paths, 1,024-byte boundaries, 64-component boundary, and invalid/overlong CWD.

- [ ] **Step 2: Define the immutable core values**

  Use focused types:

  ```go
  type AddressKind string
  const (
      AddressWorkspace AddressKind = "workspace"
      AddressCWD       AddressKind = "cwd"
      MaxImageBytes                = 7 << 20
      MaxWidth                     = 16384
      MaxHeight                    = 16384
      MaxPixels              int64 = 40_000_000
      MaxOuterResponseBytes        = 9_852_248
  )

  type LogicalPath struct {
      Raw        string
      Components []string
  }

  type Limits struct {
      MaxImageBytes int
      MaxWidth      int
      MaxHeight     int
      MaxPixels     int64
  }

  func V1Limits() Limits {
      return Limits{
          MaxImageBytes: MaxImageBytes,
          MaxWidth: MaxWidth,
          MaxHeight: MaxHeight,
          MaxPixels: MaxPixels,
      }
  }

  type DisplayAddress struct {
      AddressKind AddressKind `json:"address_kind"`
      WorkspaceID string      `json:"workspace_id,omitempty"`
      CWD         string      `json:"cwd,omitempty"`
      Path        string      `json:"path"`
  }

  type File struct {
      MIMEType string
      Format   string
      Width    int
      Height   int
      Data     []byte
  }

  type Result struct {
      SchemaVersion  int            `json:"schema_version"`
      Kind           string         `json:"kind"`
      DisplayAddress DisplayAddress `json:"display_address"`
      MIMEType       string         `json:"mime_type"`
      Format         string         `json:"format"`
      ByteSize       int            `json:"byte_size"`
      Width          int            `json:"width"`
      Height         int            `json:"height"`
      Data           []byte         `json:"data"`
  }
  ```

  `ParseLogicalPath` validates the raw string without `filepath.Clean` and returns a `LogicalPath` whose `Components` is a fresh immutable-by-convention slice. `DisplayAddress.Validate` requires exactly one base form and preserves the exact caller strings.

- [ ] **Step 3: Freeze the media failure taxonomy before adapters use it**

  Add exact Go constants and public specs:

  ```go
  const (
      InvalidDaemonResponse   Code = "invalid_daemon_response"
      MediaPathNotFound       Code = "media_path_not_found"
      MediaPathUnsafe         Code = "media_path_unsafe"
      MediaNotRegular         Code = "media_not_regular"
      MediaTooLarge           Code = "media_too_large"
      MediaTypeUnsupported    Code = "media_type_unsupported"
      MediaInvalidImage       Code = "media_invalid_image"
      MediaDimensionsExceeded Code = "media_dimensions_exceeded"
      MediaSourceChanged      Code = "media_source_changed"
      MediaReadTimeout        Code = "media_read_timeout"
      MediaReadFailed         Code = "media_read_failed"
  )
  ```

  `MediaSourceChanged` is retryable. `MediaReadTimeout` is non-retryable. `MediaReadFailed` is non-retryable by default; only an explicitly typed transient cause may be projected retryable, and the test must name that cause. Safe details never contain canonical paths or OS errors.

- [ ] **Step 4: Add media-only schema fragments, not unconditional public exposure**

  The base `mcp-input-v2.json` remains the ordinary non-media schema. Add embedded media fragments that the MCP adapter composes only when the bridge effective catalog says media is available. Input requires exactly one of `{workspace_id,path}` or `{cwd,path}` and closes unknown properties. Output describes safe structured metadata only; raw bytes remain solely in `ImageContent`.

- [ ] **Step 5: GREEN and schema inventory**

  ```bash
  go test ./internal/core/media ./internal/core/failure ./api/schema ./tests/contract -count=1
  ```

- [ ] **Step 6: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/core/media internal/core/failure api/schema tests/contract
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: define rich media contracts"
  ```

### Task 3: Add Descriptor-Relative Safe Local File Acquisition

**Files:**
- Create: `internal/adapter/localfs/media_unix.go`
- Create: `internal/adapter/localfs/media_test.go`
- Create: `internal/adapter/localfs/media_race_test.go`
- Create: `internal/adapter/localfs/media_fuzz_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `media.LogicalPath`, `media.Limits`.
- Produces: `type Reader struct{}` and `func (Reader) Read(ctx context.Context, base string, path media.LogicalPath, limits media.Limits) (media.File, error)`.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED native safety tests**

  Cover regular PNG/JPEG, intermediate/final symlink, FIFO, Unix socket, directory, missing file, permission denial, source rename/replace/write mutation, exact 7 MiB and 7 MiB+1, deterministic race hooks, cancellation before/between every controllable traversal/stat/read/config stage, and cancellation between read chunks. Add an injected blocking syscall/read hook proving a kernel-blocked worker retains its FD + capacity slot until it actually returns. Use channels/barriers; do not use arbitrary sleeps for TOCTOU/cancellation assertions.

- [ ] **Step 2: Pin the reviewed WebP dependency before importing it**

  Re-query `golang.org/x/image` at execution time. If the approved version remains `v0.45.0`, record checksum/license/advisories, then:

  ```bash
  go get golang.org/x/image@v0.45.0
  go mod tidy
  go mod verify
  govulncheck ./...
  ```

  If WebP dependency review fails, remove WebP from the V1 allowlist/spec before continuing rather than selecting another broad image framework.

- [ ] **Step 3: Implement pinned-base no-follow traversal**

  Open the caller-authorized base directory, then walk each frozen component with `unix.Openat`. Intermediates use:

  ```go
  unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
  ```

  Final component uses:

  ```go
  unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
  ```

  Immediately `Fstat` the final descriptor and reject anything not a regular file before content read. Convert syscall/OS failures to the Task-2 stable media failure taxonomy at the adapter boundary without exposing raw error strings.

- [ ] **Step 4: Context-aware bounded read + stable-source tuple + image config validation**

  Check `ctx.Err()` before and after each controllable `openat`, `fstat`, read chunk, second `fstat`, and `DecodeConfig` stage. A syscall already blocked in the kernel is not magically cancellable: it keeps its descriptor and daemon media slot until return; no replacement worker is started.

  Capture `{dev, ino, mode/type, size, mtime, ctime}`. Read at most `MaxImageBytes+1` using a fixed bounded chunk buffer (for example 64 KiB), checking context between chunks and handling short reads/EINTR without losing the byte ceiling:

  ```go
  for len(data) <= limits.MaxImageBytes {
      if err := ctx.Err(); err != nil { return media.File{}, err }
      n, readErr := file.Read(buf)
      if n > 0 { data = append(data, buf[:n]...) }
      if len(data) > limits.MaxImageBytes { return media.File{}, failure.MediaTooLarge }
      if readErr == io.EOF { break }
      if readErr != nil { return media.File{}, mapReadFailure(readErr) }
      if err := ctx.Err(); err != nil { return media.File{}, err }
  }
  ```

  Then `Fstat` again and require tuple equality. Detect/validate only PNG/JPEG/WebP from encoded content, enforce width/height/pixel caps, and use header/config decode rather than full pixel decode. Do not require a filename extension to prove content type.

- [ ] **Step 5: GREEN and fuzz smoke**

  ```bash
  go test ./internal/adapter/localfs ./internal/core/media -count=1
  go test -race ./internal/adapter/localfs -count=1
  go test ./internal/adapter/localfs -run '^$' -fuzz FuzzDecodeConfig -fuzztime=2s
  ```

- [ ] **Step 6: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/adapter/localfs internal/core/media go.mod go.sum
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: acquire local images safely"
  ```

### Task 4: Add Daemon Media Admission, Resolution, and Cooperative Timeout

**Files:**
- Create: `internal/app/daemon/media.go`
- Create: `internal/app/daemon/media_test.go`
- Create: `internal/app/daemon/media_admission_test.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `cmd/shellbeam/command_daemon.go`

**Interfaces:**
- Consumes: `MediaReader.Read`, existing `WorkspaceResolver.ResolveAddress`.
- Produces: `func (s *Service) ReadMedia(context.Context, MediaRequest) (media.Result, error)`; optional media dependency is nil when unsupported.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED ordering/admission tests**

  Build deterministic fakes that count resolver/base-reader entry. With capacity 2, A/B acquire slots then block; C must return `capacity_exceeded` while resolver/reader counters remain exactly 2. Repeat with context cancellation between controllable localfs stages and with an injected kernel/blocking reader: cooperative cancellation exits/releases the slot when the worker can observe context, while the injected blocked worker retains the slot until actual return. Caller timeout never releases capacity early.

- [ ] **Step 2: Add the daemon request/port**

  ```go
  type MediaReader interface {
      Read(context.Context, string, media.LogicalPath, media.Limits) (media.File, error)
  }

  type MediaRequest struct {
      WorkspaceID string
      CWD         string
      Path        string
  }
  ```

  `ReadMedia` first builds/validates the exact `display_address` and logical path, then acquires a media slot. Workspace lookup or explicit-CWD base selection happens only after admission.

- [ ] **Step 3: Implement bounded worker lifecycle**

  Launch at most one worker per acquired slot. The worker owns the slot until its actual return. Propagate a cancellable context into `MediaReader.Read`; localfs observes it between controllable steps/chunks. The caller waits for result or the 5 s cooperative budget; on budget expiry cancel the worker context and return `media_read_timeout` with `retryable=false`, but do not manufacture a replacement worker or release the token early. A worker blocked inside a kernel call keeps the slot until it returns.

- [ ] **Step 4: Wire localfs only into daemon composition**

  `runDaemon` injects `localfs.Reader{}` into daemon options/service construction. No bridge/MCP package imports `localfs`, `os`, or `x/sys/unix`.

- [ ] **Step 5: GREEN**

  ```bash
  go test ./internal/app/daemon ./internal/adapter/localfs ./cmd/shellbeam -run 'TestMedia|TestAdmission|TestTimeout|TestWorkspace' -count=1
  go test -race ./internal/app/daemon ./internal/adapter/localfs -count=1
  ```

- [ ] **Step 6: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/app/daemon cmd/shellbeam/command_daemon.go
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: add bounded daemon media reads"
  ```

### Task 5: Add Stateless Media Capability Negotiation

**Files:**
- Create: `internal/core/capability/media.go`
- Create: `internal/core/capability/media_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`

**Interfaces:**
- Produces: immutable consumer/daemon media-support declarations, fixed V1 daemon/bridge support declarations, and deterministic intersection/fingerprint.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED negotiation/catalog tests**

  Test MIME intersection, lower bridge/daemon raw-byte limits, schema mismatch, deterministic fingerprint, clone isolation, ordinary baseline omission, and exact V1 support values. Add `V1MediaSupport()` as the single reviewed daemon/bridge support declaration so limits cannot drift between processes.

- [ ] **Step 2: Add the media capability model**

  ```go
  type MediaSupport struct {
      SchemaVersion int      `json:"schema_version"`
      Kinds         []string `json:"kinds"`
      MIMETypes     []string `json:"mime_types"`
      MaxImageBytes int      `json:"max_image_bytes"`
      MaxWidth      int      `json:"max_width"`
      MaxHeight     int      `json:"max_height"`
      MaxPixels     int64    `json:"max_pixels"`
  }

  type NegotiatedMedia struct {
      Contract    MediaSupport `json:"contract"`
      Fingerprint string       `json:"fingerprint"`
  }
  ```

  `NegotiateMedia(consumer, daemon MediaSupport) (NegotiatedMedia, bool)` returns a deterministic sorted intersection and SHA-256 fingerprint of the closed contract. No persistent negotiation state is introduced.

- [ ] **Step 3: Extend public catalog without changing legacy daemon projection**

  Add `FeatureRichLocalMedia` and optional `Media *MediaSupport` to `capability.Catalog`. `Baseline` sets the feature unavailable and `Media=nil`; `Clone` deep-copies the optional media value/slices. The daemon's ordinary `CapabilityCatalog` remains baseline/no-media.

- [ ] **Step 4: GREEN and commit**

  ```bash
  go test ./internal/core/capability ./internal/core/failure -count=1
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/core/capability
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: define negotiated media capability"
  ```

### Task 6: Extend IPC v2 with Private Negotiation and Bounded Media Results

**Files:**
- Modify: `api/schema/ipc-v2.json`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_test.go`
- Modify: `internal/adapter/ipc/client_unix.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Modify: `internal/adapter/ipc/ipc_integration_test.go`
- Create: `internal/adapter/ipc/media_test.go`
- Modify: `internal/app/bridge/client_port.go`
- Modify: `cmd/shellbeam/command_daemon.go`

**Interfaces:**
- Consumes: `capability.MediaSupport`, `capability.NegotiatedMedia`, `daemon.MediaRequest`, `media.Result`.
- Produces: private actions `capabilities.negotiate` and `read_media`; wire-neutral bridge request/response fields used by the IPC adapter; media response outer bound enforced before JSON/base64 decode.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED IPC contract tests**

  Add fixtures for negotiation success/unsupported schema, read without opt-in, wrong fingerprint, malformed media envelope, exact `maxOuter=9_852_248`, `maxOuter+1`, malformed base64, duplicate/invalid/trailing JSON, a short body, and a custom response body that returns a valid-looking JSON prefix plus an injected non-EOF read error. Every transport-error case must return failure with zero `Response.Media`; ordinary IPC remains unchanged.

- [ ] **Step 2: Add transport-neutral media fields to the bridge port, then extend closed IPC-v2 types**

  First extend `internal/app/bridge/client_port.go`; this is a DTO-only change, not media behavior:

  ```go
  type Request struct {
      // existing fields...
      ConsumerMedia            *capability.MediaSupport
      MediaContractFingerprint string
      Media                    *daemon.MediaRequest
  }

  type Response struct {
      // existing fields...
      NegotiatedMedia *capability.NegotiatedMedia
      Media           *media.Result
  }
  ```

  Then add fields only to the IPC branches that use them:

  ```go
  type RequestV2 struct {
      // existing fields...
      ConsumerMedia            *capability.MediaSupport `json:"consumer_media,omitempty"`
      MediaContractFingerprint string                   `json:"media_contract_fingerprint,omitempty"`
      Media                    *daemon.MediaRequest     `json:"media,omitempty"`
  }

  type ResponseV2 struct {
      // existing fields...
      NegotiatedMedia *capability.NegotiatedMedia `json:"negotiated_media,omitempty"`
      Media           *media.Result                `json:"media,omitempty"`
  }
  ```

  `capabilities.negotiate` accepts only the bounded consumer declaration. `read_media` carries the same declaration plus the negotiated fingerprint and media request, allowing the daemon to recompute the intersection statelessly before admission. Missing/invalid opt-in returns `feature_unavailable` before filesystem work.

- [ ] **Step 3: Add optional IPC server interfaces instead of widening legacy `Actions`**

  ```go
  type MediaActions interface {
      ReadMedia(context.Context, daemon.MediaRequest) (media.Result, error)
      MediaSupport() capability.MediaSupport
  }
  ```

  `handleV2` type-asserts `MediaActions` only for the private negotiation/media branches. Existing v1 and ordinary v2 fakes/callers require no media method. Extend `cmd/shellbeam/command_daemon.go`'s `daemonActions` with `ReadMedia` delegating to the daemon service and `MediaSupport` returning `capability.V1MediaSupport()`. Do not add media to `InspectServer`; that projection remains legacy-compatible/no-media.

- [ ] **Step 4: Enforce the media outer response ceiling in the client before decode**

  In `CallV2`, preserve current decoding for ordinary actions. For an outstanding `read_media` request:

  ```go
  limited := io.LimitReader(resp.Body, media.MaxOuterResponseBytes+1)
  body, readErr := io.ReadAll(limited)
  if len(body) > media.MaxOuterResponseBytes {
      return out, failure.New(failure.InvalidDaemonResponse, nil, errors.New("media response too large"))
  }
  if readErr != nil {
      return out, failure.New(failure.InvalidDaemonResponse, nil, errors.New("media response read failed"))
  }
  if err := jsonstrict.Decode(body, &out); err != nil { ... }
  ```

  Ordering is normative: bounded read -> oversize classification -> **read-error check** -> strict JSON/base64 decode -> envelope/request correlation. A valid-looking prefix accompanied by a transport read error is never decoded/rendered. Validate response kind/version/action/request ID before returning it to the bridge.

- [ ] **Step 5: GREEN, skew integration, commit**

  ```bash
  go test ./api/schema ./internal/adapter/ipc ./internal/core/capability -run 'Test.*Media|Test.*Negotiate|TestIPCV2|TestCompatibility' -count=1
  go test ./internal/adapter/ipc -count=1
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add api/schema/ipc-v2.json internal/adapter/ipc internal/app/bridge/client_port.go cmd/shellbeam/command_daemon.go
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: transport negotiated media over ipc"
  ```

### Task 7: Make the Bridge Own Effective Capability and Exact Media Correlation

**Files:**
- Modify: `internal/app/bridge/client_port.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `internal/app/bridge/handler_test.go`
- Create: `internal/app/bridge/media.go`
- Create: `internal/app/bridge/media_test.go`
- Modify: `cmd/shellbeam/command_mcp.go`

**Interfaces:**
- Produces: immutable bridge effective catalog; startup negotiation that degrades safely to media-unavailable; request-derived expected address; validated `Response.Media` ready for MCP translation.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED bridge negotiation/skew tests**

  Test new bridge + old daemon, old/no-media construction, new/new intersection, lower limit, negotiation malformed response, and `inspect.server` effective projection. Negotiation unsupported must not prevent ordinary MCP startup.

- [ ] **Step 2: Preserve `New(client)` as no-media compatibility and add negotiated bootstrap**

  ```go
  func New(client DaemonClient) *Handler
  func NewNegotiated(ctx context.Context, client DaemonClient, consumer capability.MediaSupport) (*Handler, error)
  func (h *Handler) EffectiveCatalog() capability.Catalog
  ```

  `NewNegotiated` reads the daemon's ordinary no-media catalog, attempts private `capabilities.negotiate`, and builds an effective catalog. Unsupported negotiation leaves media unavailable; transport/protocol corruption that makes ordinary daemon identity unknowable remains an error.

- [ ] **Step 3: Intercept public `inspect.server` at the bridge**

  For modern public inspection, return `h.EffectiveCatalog()` rather than leaking the daemon's private/raw projection. This catalog is the same source later used to compose MCP schema and call gate.

- [ ] **Step 4: Derive and preserve the expected complete address before dispatch**

  For `read_media`, derive:

  ```go
  expected := media.DisplayAddress{
      AddressKind: media.AddressWorkspace,
      WorkspaceID: in.WorkspaceID,
      Path:        in.Path,
  }
  // or AddressCWD with exact caller-supplied CWD + raw Path
  ```

  Store it in the bridge request context/value, not in daemon-returned metadata.

- [ ] **Step 5: Validate daemon media before any MCP rendering**

  Require exact display-address equality, one base form, byte-size equality, `len(data)<=7 MiB`, accepted MIME/format, and dimension/pixel bounds. Missing/wrong workspace, wrong CWD, wrong kind, normalized path, canonical CWD substitution, invalid bytes/metadata, or MIME mismatch returns `invalid_daemon_response` and zero image.

- [ ] **Step 6: Wire negotiated bootstrap into `runMCP` and GREEN**

  `cmd/shellbeam/command_mcp.go` constructs the fixed bridge consumer support and calls `NewNegotiated`; no filesystem dependency enters the MCP process.

  ```bash
  go test ./internal/app/bridge ./internal/adapter/ipc ./cmd/shellbeam -run 'Test.*Media|Test.*Negotiat|TestMCPV2Bridge' -count=1
  ```

- [ ] **Step 7: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/app/bridge cmd/shellbeam/command_mcp.go
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: validate negotiated media at bridge"
  ```

### Task 8: Dynamically Expose `read_media` and Render Native `ImageContent`

**Files:**
- Modify: `internal/adapter/mcp/server.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/server_test.go`
- Modify: `internal/adapter/mcp/discovery_test.go`
- Create: `internal/adapter/mcp/media_test.go`
- Modify: `scripts/test-mcp-local.sh`

**Interfaces:**
- Consumes: bridge effective catalog + validated `media.Result`.
- Produces: conditionally composed one-tool schema, accurate egress disclosure, and native `mcpgo.ImageContent`.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write RED metadata/schema/rendering tests**

  Prove:

  ```text
  old/unnegotiated catalog -> no read_media branch
  negotiated catalog -> exactly one local_shell tool with read_media branch
  tool schema == effective catalog == public inspect.server call gate
  disclosure mentions original selected bytes + possible embedded metadata + MCP client/model egress
  success returns one ImageContent with raw []byte and exact MIME
  StructuredContent contains display_address/metadata but never data/base64/token
  invalid bridge response returns error text/structured content only
  ```

- [ ] **Step 2: Compose media schema only when effective media is available**

  Keep embedded base v2 schema unchanged. Build a fresh copy of its `oneOf` and append `mcp-read-media-input-v1.json` only when `catalog.Features[FeatureRichLocalMedia]==Available` and `catalog.Media!=nil`. Compose the media output branch similarly. Never mutate the embedded/raw shared slice in place.

- [ ] **Step 3: Add conditional disclosure without weakening shell annotations**

  When media is available, append to the tool description/instructions a concise normative disclosure equivalent to:

  ```text
  read_media reads the original selected local image file bytes and sends those bytes to the connected MCP client/model; encoded files may include embedded metadata such as EXIF, GPS, ICC profiles, comments, application metadata, or trailing bytes.
  ```

  Preserve production-like `local_shell` tool annotations (`readOnly=false`, `destructive=true`, `openWorld=true`, `idempotent=false`) unless Task-0 Phase A evidence explicitly selects a different approved configuration.

- [ ] **Step 4: Decode/forward the media public action**

  Extend the MCP input struct with `Path` and the mutually exclusive workspace/CWD fields already used by the address contract. `requestFromInput` builds the bridge media request without normalization.

- [ ] **Step 5: Render native image content only after bridge validation**

  Task 0 must freeze exactly one of the two already-scored annotation policies: `omitted` or `user_assistant`. Do **not** add a runtime user setting for this. Implement only the frozen branch:

  ```go
  content := &mcpgo.ImageContent{
      Data:     result.Data,
      MIMEType: result.MIMEType,
  }
  // Include this assignment only when Task 0 froze `user_assistant`.
  content.Annotations = &mcpgo.Annotations{Audience: []mcpgo.Role{"user", "assistant"}}
  ```

  When Task 0 froze `omitted`, delete the annotation assignment and leave `Annotations=nil`. Tests must assert the selected production policy exactly. Structured content contains only safe metadata and exact `display_address`.

- [ ] **Step 6: GREEN through the official SDK transport**

  ```bash
  go test ./internal/adapter/mcp ./internal/app/bridge -run 'Test.*Media|TestMetadata|TestInMemoryConformance|TestDiscovery' -count=1
  ./scripts/test-mcp-local.sh
  ```

- [ ] **Step 7: Commit**

  ```bash
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add internal/adapter/mcp scripts/test-mcp-local.sh
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "feat: expose native local image reads"
  ```

### Task 9: Prove Privacy, Failure Mapping, and No Hot-Path Regression

**Files:**
- Create: `tests/integration/media_integration_test.go`
- Create: `tests/contract/media_privacy_test.go`
- Modify: `internal/observability/redact_test.go`
- Modify: `scripts/test-security.sh`
- Modify: `scripts/test-hardening.sh`

**Interfaces:**
- Consumes: full daemon→IPC→bridge→MCP production path.
- Produces: integration evidence that media bytes live only in bounded transient result memory and do not alter ordinary execution behavior.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Write a privacy sentinel fixture**

  Generate an accepted image containing distinctive low-entropy EXIF/comment-like byte strings and a distinctive canonical path component. Assert the returned `ImageContent` preserves original encoded bytes, while state dir, receipts, session output, telemetry, repro, event journal, structured metadata, ordinary logs, and test evidence contain none of the sentinels or canonical path.

- [ ] **Step 2: Add end-to-end error matrix assertions**

  Exercise every V1 media error and snapshot exact code/retryability. For `invalid_daemon_response`, explicitly inject wrong workspace, wrong CWD, wrong kind, missing/both bases, normalized path, canonicalized CWD, malformed data, version skew, short response bodies, and valid-looking JSON prefixes followed by transport read errors; every case must emit zero `ImageContent`.

- [ ] **Step 3: Prove ordinary execution does no media work**

  Instrument a fake media reader/admission counter and run representative `start`, `poll`, `write`, `kill`, and inspection calls. Counters stay zero. Existing command latency/budget tests remain unchanged.

- [ ] **Step 4: Wire mandatory security/hardening scopes**

  Add media packages/tests to `scripts/test-security.sh` and `scripts/test-hardening.sh`; keep `govulncheck` optional only according to existing script policy, while module verify/tidy/redaction tests remain mandatory.

- [ ] **Step 5: GREEN and commit**

  ```bash
  go test ./tests/integration ./tests/contract ./internal/observability -run 'Test.*Media|Test.*Privacy|Test.*Redact' -count=1
  sh -n ./scripts/test-hardening.sh ./scripts/test-security.sh
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add tests internal/observability scripts/test-security.sh scripts/test-hardening.sh
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "test: prove rich media privacy boundaries"
  ```

  Task 10 runs the modified hardening/security scripts as part of release-scale verification.

### Task 10: Native Filesystem, Memory, Full Regression, and Phase B Revalidation

**Files:**
- Create: `scripts/test-media-native.sh`
- Create: `docs/testing/rich-local-media.md`
- Modify: `.github/workflows/checkpoint.yml`
- Modify: `.github/workflows/nightly.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `scripts/e2e-tunnel-user.sh`
- Modify: `docs/superpowers/evidence/2026-08-17-rich-local-media-phase-a.md` only to link Phase B evidence, not overwrite Phase A
- Create: `docs/superpowers/evidence/2026-08-17-rich-local-media-phase-b.md`

**Interfaces:**
- Produces: native macOS/Linux safe-open proof, <=128 MiB two-read incremental RSS evidence, full repository regression evidence, and production ChatGPT Phase B scorecard.

- [ ] **Precondition: Recheck the machine pre-implementation gate**

  ```bash
  ./scripts/check-rich-media-preimplementation-gate.sh
  EXECUTION_BASE="$(python3 -c 'import json; print(json.load(open("docs/superpowers/evidence/2026-08-17-rich-local-media-preimplementation-gate.json"))["execution_base_sha"])')"
  test -n "$EXECUTION_BASE"
  ```

  Expected: checker exit `0`. Any other exit stops this task before editing production code.

- [ ] **Step 1: Add the native media gate script**

  `scripts/test-media-native.sh` runs localfs symlink/special-file/mutation tests, daemon admission/timeout tests, IPC outer-bound tests, and bridge/MCP media integration. It prints OS/arch/toolchain/parser mode and exits non-zero on any unavailable mandatory native guarantee; compile-only cross-build is not labeled native PASS.

- [ ] **Step 2: Add the two-concurrent-max RSS measurement**

  On both native macOS and Linux, record idle daemon+bridge RSS, one 7 MiB read peak, and two concurrent 7 MiB read peak. Compute combined incremental peak above same-build idle baseline. Gate:

  ```text
  two_concurrent_7MiB_incremental_RSS <= 128 MiB
  ```

  If it fails, first remove accidental copies; if still failing, lower the raw cap and rerun real ChatGPT E2E before considering streaming/FD passing.

- [ ] **Step 3: Run full local verification**

  ```bash
  gofmt -w internal api cmd tests
  make fmt-check
  go vet ./...
  go mod tidy -diff
  go mod verify
  go test -count=1 ./...
  GOCACHE="$(go env GOCACHE)" ./scripts/test-hardening.sh
  ./scripts/test-security.sh
  ./scripts/test-media-native.sh
  go run ./tools/devctl verify --checkpoint --base "$EXECUTION_BASE" --json
  go build -trimpath -buildvcs=false -o .build/checkpoints/rich-media/shellbeam ./cmd/shellbeam
  git diff --check
  ```

- [ ] **Step 4: Require native CI/release agreement**

  `checkpoint`, `nightly`, and `release-candidate` must pass on `ubuntu-latest` and `macos-latest` with identical parser mode and the media native gate. Capture artifacts/receipts; do not substitute a cross-build for native filesystem behavior.

- [ ] **Step 5: Execute Phase B in the frozen rollout order**

  1. Deploy negotiation-aware **bridge first** while old daemon remains active; prove ordinary execution works and media is absent.
  2. Deploy new daemon; prove private negotiation/intersection makes media effective.
  3. Restart the MCP endpoint after the production bridge/daemon pair is active so the MCP server snapshots the newly negotiated effective catalog; verify the process is using the production binary under test before Refresh.
  4. Refresh the ChatGPT developer app metadata.
  5. Start fresh conversations.
  6. Rerun the **complete** Section 21 golden/confirmation/disclosure/address-collision/format/max-payload/remembered-approval scorecard against production code.
  7. Record exact production environment, tool arguments, complete displayed address, confirmation behavior, byte counts, and visible-token answers.

  Any hard scorecard failure removes/keeps the media capability unadvertised; do not ship a partial PASS.

- [ ] **Step 6: Final verification commit**

  Only after all local/native/CI/Phase-B gates are evidenced:

  ```bash
  git status --short
  git diff --check
  go run ./tools/devctl test --dirty --base "$EXECUTION_BASE" --json
  go run ./tools/devctl build --dirty --base "$EXECUTION_BASE" --json
  git add scripts/test-media-native.sh docs/testing/rich-local-media.md .github/workflows/checkpoint.yml .github/workflows/nightly.yml .github/workflows/release.yml scripts/e2e-tunnel-user.sh docs/superpowers/evidence
  git diff --cached --check
  git -c core.hooksPath=.githooks commit -m "test: verify rich local media access"
  ```

## Execution Order and Stop Conditions

```text
Task -1 bind current base + approved source
  ├─ FAIL / NOT_RUN -> STOP; Task 0 forbidden
  └─ PASS
      Task 0 normalize Phase A + run parser tracer/native matrix
        commit PASS / FAIL / NOT_RUN evidence
        run machine checker
          ├─ exit 3/4 -> STOP; capability absent
          └─ exit 0
              Task 1 strict JSON/build mode
              Task 2 core contract/schema fragments
              Task 3 localfs acquisition
              Task 4 daemon admission/read lifecycle
              Task 5 capability negotiation
              Task 6 private IPC negotiation/media transport
              Task 7 bridge effective catalog/correlation
              Task 8 MCP dynamic exposure/ImageContent
              Task 9 privacy/integration/hot-path proof
              Task 10 native/full regression/Phase B
```

Additional stop conditions:

- If Task -1 cannot materialize an artifact matching SHA-256 `7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906`, stop; never reconstruct the approved design from chat memory.
- If Task -1 finds stale Files/Interfaces/symbol mappings after synchronizing to `EXECUTION_BASE`, revise/re-review this plan and rerun Task -1 before Task 0.
- If the complete Phase A one-tool scorecard fails a hard gate and the dedicated `read_local_image` candidate passes the full matrix, revise the bound spec/plan topology before Task 1.
- If PNG does not enter model vision, stop the feature.
- If only a subset of PNG/JPEG/WebP passes, revise the allowlist before production code.
- If no parser/toolchain candidate passes the strict tracer + corpus + full/native matrix, checker remains nonzero and Task 1 is forbidden.
- If Go 1.26 experiment risk has not been explicitly accepted while Go 1.27 remains unavailable as an official GA candidate, record parser gate `NOT_RUN`; do not infer consent.
- If WebP dependency review fails, revise V1 to PNG/JPEG; do not select another broad decoder framework silently.
- If the 7 MiB ceiling fails again through the official tunnel in production Phase B, lower the explicit product limit and rerun the affected full matrix; do not ship an undocumented effective limit.

## Plan Self-Review Checklist

Before executing Task -1, after Task -1 rebinding, after Task 0, and again before Task 10:

- [ ] The repository-bound approved source exactly matches SHA-256 `7d719f5add41354ca14716a78b32ca0a6744e09e8225387b48737dce475c6906`, and every acceptance criterion in the bound canonical spec maps to at least one task/test above.
- [ ] `display_address` is complete and exact at MCP request, daemon result, IPC envelope, bridge correlation, structured metadata, and UI evidence.
- [ ] No task adds a generic filesystem/binary/media framework surface.
- [ ] Legacy daemon `inspect.server` never exposes media.
- [ ] Public schema exposure is dynamic and driven by bridge effective negotiation, not a static unconditional schema change.
- [ ] Media outer ceiling is enforced before JSON/base64 decode, and any non-EOF body read error fails before decode/rendering.
- [ ] Localfs checks context between every controllable traversal/stat/read/config stage and between read chunks; a blocked syscall retains FD + capacity until return.
- [ ] Timeout retains capacity until worker exit.
- [ ] Privacy tests include original-file metadata egress but exclude durable/logging persistence.
- [ ] Build mode is identical in dev/CI/test/release; the narrow strict-decode tracer, valid-v2 semantic corpus, v1 compatibility, race and native lanes are all represented in Task 0 evidence.
- [ ] Exact raw max is 7 MiB everywhere; no stale 8 MiB production assumption remains.
- [ ] Every Task 1–10 starts with the machine gate checker; every task commit uses affected `devctl` verification plus `git -c core.hooksPath=.githooks commit`; no subagents, push, or PR are introduced implicitly.
