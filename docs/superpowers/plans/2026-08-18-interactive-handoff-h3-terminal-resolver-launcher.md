# Interactive Handoff H3 Terminal Resolver and Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automatically resolve and launch/reveal a qualified local terminal for an H2 handoff using session affinity + freshness, while preserving manual attach fallback and exactly-once GUI side-effect semantics.

**Architecture:** `internal/core/terminalpresentation` owns terminal identity/evidence/freshness/ranking and launch outcome contracts; `internal/app/terminalpresentation` owns resolver/launcher orchestration and launch idempotency; platform adapters supply bounded active/recent/running-terminal evidence and exact launcher argv. H2 remains the authority owner: H3 only chooses presentation and invokes the existing safe local attach target.

**Tech Stack:** H2 manual handoff + local attach; Go 1.26.6; platform-native/process/application discovery behind adapters; existing store/event/capability infrastructure; no ChatGPT App/browser extension/deep link.

**Spec:** `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `5351215de2c02ac61ac82751c1680a35744047af`; H2 plan `docs/superpowers/plans/2026-08-18-interactive-handoff-h2-human-authority-manual-control.md`.

## Global Constraints

- HARD PRECONDITION: H2 evidence reports `H3_ALLOWED=true`.
- Resolver ranking is: existing session client → currently active supported terminal → most recently activated supported terminal → fresh validated bridge/session affinity → exactly one running supported terminal → qualified fallback → manual attach.
- Bridge-launch terminal is a freshness-bounded hint, not per-request origin proof and never timeless preference.
- No required `preferred_terminal` setting and no persisted detailed application-usage history.
- H3 must compose with H2 when H4 is absent, and must not assume secret/privacy fields/providers exist. If H4 is already present, H3 must also pass the combined H2+H3+H4 composition test; if not, its evidence records combined lane `NOT_RUN_COUNTERPART_ABSENT`, not a fake PASS.
- Terminal provider is presentation only: it cannot create sessions, grant ownership, alter shell readiness, or carry model-provided shell snippets.
- Launcher target is exact installed ShellBeam attach argv generated locally. No arbitrary shell interpolation and no developer source path/Homebrew-path assumption.
- Launch/reveal is an external GUI side effect with durable `not_attempted|launching|launched_and_client_proven|launch_failed|launch_outcome_unknown` semantics.
- Unknown launch outcome is never retried blindly. Resolver/launcher must inspect for the expected handoff client or remain unknown/manual.
- Frontmost browser means no active terminal candidate; it must not erase recent terminal evidence.
- Recent activity is event-driven/shared/O(1), not one poller per handoff/terminal/session.
- Platform/provider capability advertises only native-qualified launchers actually usable on the current machine.
- H3 must leave H2 manual fallback operational when all automatic providers are unavailable.
- H3 does not implement shell-aware readiness, secret private intervals, or context-exec.

- Do not edit `dev/test-impact.toml` preemptively; if fresh `devctl` evidence demonstrates under-selection, stop, document the concrete gap, amend this plan with the exact mapping/test, then continue.

## Responsibility Map

- `internal/core/terminalpresentation`: identity, evidence source/quality, freshness, candidates, deterministic ranking, launch state/outcome.
- `internal/app/terminalpresentation`: resolver service, launcher registry port, recent-activity port, launch idempotency orchestration.
- `internal/adapter/terminalpresentation`: platform discovery/activity/launcher adapters in one adapter package with OS-specific files so sibling-adapter barriers are respected.
- `internal/adapter/store`: durable launch-attempt state keyed by handoff ID; minimal optional session↔terminal affinity metadata.
- `internal/app/bridge`: bounded bridge-launch hint capture only if the current local bridge context can validate it.
- `internal/app/interactivehandoff`: call presentation service after handoff durability/authority preconditions, then continue H2 attach lifecycle.
- `cmd/shellbeam`: composition/doctor/native smoke tests; no static terminal preference.

---

### Task 1: Verify H2 gate and freeze the initial promoted terminal matrix

**Files:**
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h3-terminal-preflight.md`

**Interfaces:**
- Produces exact initial provider IDs/platforms and installed-provider evidence used by H3 tests; does not change master ranking.

- [ ] **Step 1: Assert H3 gate.**

```bash
E=docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md
test -f "$E"
rg -n '^H3_ALLOWED[[:space:]]*=[[:space:]]*true$' "$E"
```

- [ ] **Step 2: Inventory local terminal candidates without writing a preference.**

Record installed/running identity by platform application/binary identity. On macOS inspect known bundle IDs/paths through reviewed OS APIs; on Linux record executable/desktop identity through qualified desktop/process APIs. Raw `TERM_PROGRAM` alone is insufficient.

- [ ] **Step 3: Select the initial native qualification matrix from available providers.**

Target at minimum for a broad experimental claim:

```text
macOS: Ghostty + one additional terminal if installed/qualifiable
Linux: at least one promoted terminal on a native Linux lane
```

If the host lacks a target, mark that provider `NOT_RUN`, not PASS. H3 capability can still expose the proven subset; cross-platform stable claim remains blocked until required native evidence exists.

- [ ] **Step 4: Record exact external launcher interfaces/version facts.**

For each promoted provider, archive its documented/help output and exact application/executable identity under `.build/interactive-handoff-h3/`; tracked preflight records hashes/versions and the exact argv shape H3 will test. Do not copy source-machine absolute paths into product contracts.

- [ ] **Step 5: Verify/commit preflight.**

Run docs gate + commit-gate and commit `docs: freeze h3 terminal provider preflight`.

---

### Task 2: Define terminal evidence, freshness, ranking, and launch contracts

**Files:**
- Create: `internal/core/terminalpresentation/types.go`
- Create: `internal/core/terminalpresentation/rank.go`
- Create: `internal/core/terminalpresentation/launch.go`
- Create: `internal/core/terminalpresentation/types_test.go`
- Create: `internal/core/terminalpresentation/rank_test.go`
- Create: `internal/core/terminalpresentation/launch_test.go`
- Modify: `internal/core/failure/failure.go`

**Interfaces:**
- Produces `TerminalIdentity`, `Evidence`, `Candidate`, `Resolution`, `LaunchState`, `LaunchOutcome`.

- [ ] **Step 1: RED-test identity and evidence contracts.**

Conceptual types:

```go
type EvidenceSource string
const (
    SourceExistingClient EvidenceSource = "existing_client"
    SourceActive         EvidenceSource = "active"
    SourceRecent         EvidenceSource = "recent"
    SourceBridgeAffinity EvidenceSource = "bridge_affinity"
    SourceSingleRunning  EvidenceSource = "single_running"
    SourceFallback       EvidenceSource = "fallback"
)

type Evidence struct {
    Identity   TerminalIdentity
    Source     EvidenceSource
    ObservedAt time.Time
    FreshUntil time.Time
    Quality    string
}
```

Identity is known provider/app family + platform/bundle/executable identity, never arbitrary user/model path.

- [ ] **Step 2: RED-test deterministic ranking.**

Matrix includes:

```text
existing exact client beats all
active supported terminal beats recent
frontmost browser contributes no terminal candidate
recent fresh Ghostty beats stale bridge-launch Terminal.app
fresh bridge affinity beats single-running fallback only when no active/recent candidate
multiple running unsupported/ambiguous -> no single-running winner
qualified fallback only after all evidence lanes absent
```

Tie within same rank uses newest observation then stable provider ID, never random map order.

- [ ] **Step 3: Implement freshness as data, not hidden timers.**

Pure ranking rejects expired evidence using supplied `now`. App layer decides provider-specific freshness duration from qualified constants; no core clock calls.

- [ ] **Step 4: Define launch state machine and failures.**

```text
not_attempted
launching
launched_and_client_proven
launch_failed
launch_outcome_unknown
```

Failures include `terminal_launcher_unavailable`, `terminal_launch_failed`, `terminal_launch_unknown`, `terminal_identity_ambiguous`.

- [ ] **Step 5: Focused/race/commit.**

```bash
go test ./internal/core/terminalpresentation -count=1
go test -race ./internal/core/terminalpresentation -count=1
go run ./tools/devctl check
git add internal/core/terminalpresentation internal/core/failure/failure.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: define terminal presentation resolution"
```

---

### Task 3: Implement app resolver and minimal recent-terminal activity registry

**Files:**
- Create: `internal/app/terminalpresentation/ports.go`
- Create: `internal/app/terminalpresentation/resolver.go`
- Create: `internal/app/terminalpresentation/recent.go`
- Create: `internal/app/terminalpresentation/resolver_test.go`
- Create: `internal/app/terminalpresentation/recent_test.go`
- Create: `internal/adapter/terminalpresentation/activity_darwin.go`
- Create: `internal/adapter/terminalpresentation/activity_darwin_test.go`
- Create: `internal/adapter/terminalpresentation/activity_linux.go`
- Create: `internal/adapter/terminalpresentation/activity_linux_test.go`
- Create: `internal/adapter/terminalpresentation/running.go`
- Create: `internal/adapter/terminalpresentation/running_test.go`

**Interfaces:**
- `ActivitySource` emits supported terminal activation events.
- `RunningSource` returns bounded supported running identities on demand.
- `Resolver.Resolve(ctx, Request) Resolution` consumes existing client/session affinity + provider evidence.

- [ ] **Step 1: RED-test shared event-driven registry.**

One platform source may feed a bounded in-memory `RecentRegistry`; no per-session goroutine/ticker. Registry retains only current/recent supported identity + timestamp/quality and drops stale entries by bounded write/read logic.

- [ ] **Step 2: Implement Darwin activity source using the exact native mechanism qualified in Task 1.**

It must emit app activation events rather than poll frontmost app. Browser events may update general foreground state but must not clear last-supported-terminal record until freshness expires.

- [ ] **Step 3: Implement Linux activity source only for qualified desktop environments/APIs.**

Unsupported Wayland/X11/desktop combinations return capability unavailable; no periodic process/window scanning fallback.

- [ ] **Step 4: Implement bounded running-terminal discovery.**

Return only recognized provider identities, no arbitrary executable path as launch authority. Multiple candidates stay multiple; resolver applies single-running rule only when exactly one qualified candidate exists.

- [ ] **Step 5: App resolver tests with fake clock/evidence providers.**

Prove ranking from core and no implicit fallback preference.

- [ ] **Step 6: Focused/race/commit.**

```bash
go test ./internal/app/terminalpresentation ./internal/adapter/terminalpresentation -run 'Resolver|Recent|Activity|Running' -count=1
go test -race ./internal/app/terminalpresentation ./internal/adapter/terminalpresentation -run 'Recent|Activity' -count=1
go run ./tools/devctl check
git add internal/app/terminalpresentation internal/adapter/terminalpresentation
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: resolve recent local terminal context"
```

---

### Task 4: Add validated bridge-launch affinity as a low-priority fresh hint

**Files:**
- Create: `internal/core/terminalpresentation/affinity.go`
- Create: `internal/core/terminalpresentation/affinity_test.go`
- Create: `internal/app/bridge/terminal_affinity.go`
- Create: `internal/app/bridge/terminal_affinity_test.go`
- Modify: `cmd/shellbeam/command_mcp.go` or bridge composition file only where local launch context is initially captured.
- Create: `cmd/shellbeam/terminal_affinity_test.go`

**Interfaces:**
- Produces `BridgeAffinityHint{Identity, ObservedAt, FreshUntil, EvidenceSource}` supplied to resolver; never execution authorization.

- [ ] **Step 1: RED-test terminology/precedence.**

No exported/public field named `request_origin_terminal`. Tests call it bridge/session affinity and prove active/recent evidence outranks it.

- [ ] **Step 2: Capture only locally validated known terminal identity at bridge startup.**

Environment/process ancestry can be input evidence but must resolve to a known provider identity; raw `TERM_PROGRAM` or arbitrary app path is rejected as executable authority.

- [ ] **Step 3: Add freshness bound.**

Persisting a timeless user preference is forbidden. The hint may remain memory-only; if daemon/bridge restart loses it, resolver degrades normally.

- [ ] **Step 4: Focused tests and commit.**

```bash
go test ./internal/core/terminalpresentation ./internal/app/bridge ./cmd/shellbeam -run 'Affinity|Terminal' -count=1
go run ./tools/devctl check
git add internal/core/terminalpresentation internal/app/bridge cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: add fresh bridge terminal affinity"
```

---

### Task 5: Implement exact terminal launcher providers and safe attach argv

**Files:**
- Create: `internal/app/terminalpresentation/launcher.go`
- Create: `internal/app/terminalpresentation/launcher_test.go`
- Create: `internal/adapter/terminalpresentation/launcher.go`
- Create: `internal/adapter/terminalpresentation/launcher_test.go`
- Create: `internal/adapter/terminalpresentation/providers.go`
- Create: `internal/adapter/terminalpresentation/providers_test.go`
- Create: `cmd/shellbeam/terminal_launcher_test.go`

**Interfaces:**
- Launcher consumes only `TerminalIdentity` + prevalidated argv slice for `shellbeam session attach --handoff-id ...`.
- Returns `Attempted`, `Outcome`, and optional process/application correlation facts; no shell command string.

- [ ] **Step 1: RED-test attach argv construction.**

Use installed executable identity (`os.Executable()` or injected exact equivalent) and exact argument vector:

```text
<shellbeam-executable>
session
attach
--handoff-id
<handoff-id>
```

Handoff ID is validated before launcher receives it. No source checkout path or string interpolation into `/bin/sh -c`.

- [ ] **Step 2: Implement the promoted provider registry from recorded native interfaces.**

`providers.go` contains only provider definitions whose exact argv/application identity was frozen in Task-1 preflight. Tests compare each registered provider against Task-1 fixture/help evidence. Provider not installed/currently unsupported => unavailable, not guessed alternate syntax. Adding a provider not present in the preflight requires amending the tracked preflight before code.

- [ ] **Step 3: Implement optional reveal-existing-client only where native qualification proves exact target.**

If a provider cannot reveal an exact existing ShellBeam client, it may launch a new safe attach client under launch idempotency; it must not focus an arbitrary terminal window and call that reuse.

- [ ] **Step 4: Native launcher smoke tests.**

Real smoke opens the promoted terminal with an H2 test handoff, proves exact client attachment, and cleans up test GUI/client without killing delegated session. CI contract tests may validate argv without GUI but cannot replace native qualification.

- [ ] **Step 5: Focused/native/commit.**

```bash
go test ./internal/app/terminalpresentation ./internal/adapter/terminalpresentation ./cmd/shellbeam -run 'Launch|Ghostty|ITerm|WezTerm|Terminal' -count=1
go run ./tools/devctl check
git add internal/app/terminalpresentation internal/adapter/terminalpresentation cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: launch qualified local terminals"
```

---

### Task 6: Persist idempotent GUI launch outcome and reconcile ambiguous attempts

**Files:**
- Create: `internal/adapter/store/terminal_launch.go`
- Create: `internal/adapter/store/terminal_launch_test.go`
- Create: `internal/app/terminalpresentation/launch_service.go`
- Create: `internal/app/terminalpresentation/launch_service_test.go`
- Modify: `internal/app/interactivehandoff/ports.go`

**Interfaces:**
- Produces `EnsurePresented(handoffID, resolution, attachArgv)` with durable launch state.

- [ ] **Step 1: RED-test launch state transitions.**

```text
not_attempted -> launching -> proven
not_attempted -> launching -> failed
not_attempted -> launching -> outcome_unknown
proven retry -> reveal/reuse/no new GUI launch
failed retry -> replay exact failure until new handoff
unknown retry -> inspect exact expected client; never blind relaunch
```

- [ ] **Step 2: Persist reservation before GUI side effect.**

Record handoff ID, provider identity, attach-target fingerprint, attempt identity, and state before invoking launcher.

- [ ] **Step 3: Reconcile launch ambiguity using H2 exact client proof.**

If an exact H2 human client for this handoff exists, promote to proven. If absence cannot distinguish failed vs delayed launch, remain unknown/manual.

- [ ] **Step 4: Race/retry tests and commit.**

```bash
go test ./internal/adapter/store ./internal/app/terminalpresentation -run 'TerminalLaunch|EnsurePresented' -count=1
go test -race ./internal/adapter/store ./internal/app/terminalpresentation -run 'TerminalLaunch' -count=1
go run ./tools/devctl check
git add internal/adapter/store internal/app/terminalpresentation internal/app/interactivehandoff
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: make terminal launch retry safe"
```

---

### Task 7: Integrate automatic presentation into handoff without changing authority semantics

**Files:**
- Modify: `internal/app/interactivehandoff/service.go`
- Create: `internal/app/interactivehandoff/presentation_test.go`
- Modify: `internal/core/capability/catalog.go`
- Create/Modify: `cmd/shellbeam/interactive_handoff.go`
- Modify: `cmd/shellbeam/doctor.go`
- Create: `cmd/shellbeam/doctor_terminal_test.go`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/terminal_presentation_test.go`

**Interfaces:**
- `handoff.request` after durable bind may call presentation service; manual attach command remains returned on unavailable/unknown cases.

- [ ] **Step 1: RED-test authority ordering is unchanged.**

No GUI launch may occur before durable handoff bind/epoch transition state required by H2. Launcher failure cannot grant human ownership.

- [ ] **Step 2: Implement resolver→launch/reveal after durable request.**

If `existing client` wins, reveal/keep exact client where supported. Otherwise launch selected provider. If unavailable/unknown, return degraded manual attach state without corrupting handoff lifecycle.

- [ ] **Step 3: Capability intersection.**

Advertise terminal resolution sources and exact qualified launcher list. Absence of H3 does not hide H2 manual handoff capability.

- [ ] **Step 4: Doctor diagnostics.**

Show provider IDs/availability/failure reasons/freshness source support, never app-usage history or arbitrary paths/secrets.

- [ ] **Step 5: Focused/race/commit.**

```bash
go test ./internal/app/interactivehandoff ./internal/app/terminalpresentation ./internal/core/capability ./cmd/shellbeam -run 'Presentation|Terminal|Doctor' -count=1
go test -race ./internal/app/interactivehandoff ./internal/app/terminalpresentation -run 'Presentation|Launch' -count=1
go run ./tools/devctl check
git add internal/app/interactivehandoff internal/app/terminalpresentation internal/core/capability cmd/shellbeam api/schema
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: auto present human handoff terminal"
```

---

### Task 8: H3 native UX, resource, resolver, and checkpoint acceptance

**Files:**
- Create: `tests/integration/terminal_presentation_test.go`
- Create: `tests/integration/interactive_handoff_h3_composition_test.go`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h3-terminal-presentation.md`

**Interfaces:**
- Produces terminal presentation maturity/capability evidence; H4 remains independently gated by H2, but final feature UX can consume both H3 and H4 when present.

- [ ] **Step 1: Resolver truth matrix.**

Native/fixture tests prove exact precedence, browser-frontmost behavior, freshness expiry, bridge-hint downgrade, multiple terminals ambiguity, single-running fallback, and no stored preference.

- [ ] **Step 2: GUI retry matrix.**

Lost handoff response, lost launcher response, duplicate `handoff.request`, existing client, unknown outcome, provider unavailable. No proven case launches duplicate client/window.

- [ ] **Step 3: Resource test.**

100 handoff resolution cycles prove no per-handoff watcher/timer/process leak. Recent activity uses one shared provider source; ordinary non-handoff commands do zero terminal-resolution work.

- [ ] **Step 4: Native promoted-provider smoke.**

Record PASS/FAIL/NOT_RUN separately for each advertised provider/platform. A provider is advertised only with native evidence appropriate to its launch/reveal claim.

- [ ] **Step 5: Fresh verification.**

Before the generic verification commands, run the actual H2+H3/no-H4 composition test. It must prove manual handoff remains advertised, terminal presentation adds only presentation capability, and secret/privacy capability remains unavailable. If tracked H4 evidence already exists, the same test binary must additionally instantiate production H4 composition and prove H2+H3+H4 capability/result fields coexist without precedence assumptions. Record `H3_H4_COMBINED=PASS` only in that case; otherwise `NOT_RUN_COUNTERPART_ABSENT`.

```bash
go mod verify
go test ./internal/core/terminalpresentation ./internal/app/terminalpresentation ./internal/adapter/terminalpresentation ./internal/app/interactivehandoff ./cmd/shellbeam -count=1
go test -race ./internal/core/terminalpresentation ./internal/app/terminalpresentation ./internal/adapter/terminalpresentation -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
git diff --check
```

- [ ] **Step 6: Anti-goal scan and evidence commit.**

```bash
rg -n 'preferred_terminal|request_origin_terminal|osascript.*loop|sleep .*terminal|capture-pane' internal cmd api || true
```

Evidence records exact H2 base, provider matrix, ranking/freshness constants, GUI idempotency/recovery, resource behavior, native lanes, and no secret/shell-readiness claim. Stage/commit `test: verify automatic terminal presentation`; postcommit clean tree + fresh devctl check.

---

## H3 Completion Gate

H3 is complete only when:

1. H2 manual handoff remains the authority source and fallback;
2. resolver uses frozen session-affinity/freshness ordering with no static preferred terminal;
3. bridge-launch context is named/treated as a fresh hint, not request-origin proof;
4. active/recent providers are event-driven/bounded and unsupported desktop APIs degrade explicitly;
5. launcher receives only validated known terminal identity + exact ShellBeam attach argv;
6. no model text is interpolated into shell launch commands;
7. launch/reveal side effects are durably idempotent and unknown outcomes never blind-retry;
8. exact existing client reuse/reveal is claimed only when provider proves it;
9. capability advertises only qualified current-machine provider subset;
10. frontmost browser does not erase recent terminal evidence;
11. ordinary execution pays zero H3 work and no watcher/process/resource creep appears;
12. actual H2+H3/no-H4 composition passes and H3 capability/IPC fields make no H4 assumptions;
13. if H4 is already present, actual H2+H3+H4 composition passes; otherwise combined lane is honestly `NOT_RUN_COUNTERPART_ABSENT`;
14. required native/race/devctl/provider smoke evidence passes with unavailable lanes honestly `NOT_RUN`.
