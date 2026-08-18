# Interactive Handoff H4 Shell Readiness and Secret Privacy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add secret-safe human handoff, H0-qualified private-output isolation, fish/zsh/bash typed readiness, independent `TransferBoundary` and `PrivacyReleaseProof`, automatic reclaim, and honest incomplete-output semantics without exposing secret values to the model or durable ordinary ShellBeam state.

**Architecture:** H4 activates the privacy dimensions already present in H2's canonical handoff state. `internal/core/shellintegration` defines shell identity/capability/readiness proof contracts, `internal/app/shellintegration` orchestrates ephemeral integration, and `internal/adapter/shellintegration` implements separately qualified fish/zsh/bash behavior. The delegated tmux adapter implements the exact H0 privacy topology; the output/receipt path records intentional omission rather than replaying or redacting captured private bytes after the fact.

**Tech Stack:** H2 authority/manual control; H1 delegated output/receipt path; H0-qualified tmux privacy topology; fish/zsh/bash native shells; existing local IPC/private command patterns; Go 1.26.6.

**Spec:** `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `5351215de2c02ac61ac82751c1680a35744047af`; H4 may execute after the H2 checkpoint and does not require H3 automatic terminal presentation.

## Global Constraints

- HARD PRECONDITION: H2 evidence reports `H4_ALLOWED=true`; H0 privacy gates P4/P5/P6/P14/P15 remain valid for the exact production provider version/topology.
- Secret human writability is impossible until every model-visible observation path for that delegated session is private from the first possible human byte.
- Privacy is defined over observation topology, not a presumed pane-local primitive. Making private session A private may neither leak A nor silently suppress public B/C.
- No `capture-pane`/history replay across a private interval. Private bytes never become model-visible merely because daemon/control observer reconnects.
- `TransferBoundary` and `PrivacyReleaseProof` are independent. Manual human ready may establish transfer under policy but never by itself releases secret output.
- Public capture resumes only from a new forward-only boundary after human ingress is fenced and `PrivacyReleaseProof` is current.
- Intentional private omission means transcript is not byte-complete: privacy-only omission yields `output_complete=false + capture_quality=partial + capture_reasons=[private_intervals_omitted]`.
- Capture status follows corrected master semantics: `capture_quality=complete|partial|incomplete` plus monotonic `capture_reasons[]`; private omission can coexist with later `transport_gap`/`provider_lost`.
- H4 never upgrades delegated session lifecycle receipts into ordinary verification authority, even when capture is complete. `session_lifecycle_only` remains in force until separately approved context-exec.
- H4 must compose with H2 when H3 is absent. If H3 already exists, run the actual H2+H3+H4 composition lane; otherwise record it `NOT_RUN_COUNTERPART_ABSENT`.
- No secret raw bytes, hashes, deterministic derived values, value length, shell history, or environment values in MCP/IPC result, receipts, Event Journal, telemetry, repro, evidence, state, logs, errors, or notifier argv.
- Requirement `environment_exported_nonempty` returns only satisfied/not-satisfied/unavailable; presence is not credential validity. Agent must run a real post-handoff capability check separately.
- Shell identity is current runtime identity, not `$SHELL` alone. Nested/replaced/unknown shell disables shell-aware automation rather than guessing syntax.
- fish/zsh/bash adapters are independent and ephemeral; no `.zshrc`, `.bashrc`, `config.fish`, user tmux config, or permanent shell mutation.
- Readiness is event-driven/one-shot with zero resident helper after completion. No per-session polling or periodic `env`/process scan.
- Infrastructure notifier/helper environment is minimally allowlisted and excludes the watched secret/unrelated delegated exports.
- H4 does not upgrade interactive transcript/advisory shell exit facts into ordinary verification evidence. H5 owns any stronger context-exec contract.

- Do not edit `dev/test-impact.toml` preemptively; if fresh `devctl` evidence demonstrates under-selection, stop, document the concrete gap, amend this plan with the exact mapping/test, then continue.

## Responsibility Map

- `internal/core/shellintegration`: shell identity/capability level, requirement kind/result, boundary/proof quality, adapter contract validation.
- `internal/app/shellintegration`: current-shell probe, ephemeral install/remove, readiness-event handling, one-shot notifier binding.
- `internal/adapter/shellintegration`: fish/zsh/bash syntax/hooks/probes only, no terminal/provider authority.
- `internal/adapter/delegatedtmux`: exact H0-qualified private observation topology, first-byte arm/release/reconnect semantics.
- `internal/core/interactivehandoff`: activate privacy/capture/proof fields and transitions without collapsing them into owner enum.
- `internal/app/interactivehandoff`: secret request ordering, automatic proof consumption, private abort/reclaim behavior.
- `internal/adapter/store`: privacy interval metadata only (boundaries/quality/timestamps), never transcript/private bytes.
- output/receipt/schema layers: additive capture quality + truthful `output_complete=false` for intentional omission.
- `cmd/shellbeam`: private readiness notification path, doctor capability, native canary acceptance.

---

### Task 1: Verify H2 + H0 privacy gates and bind the exact production privacy topology

**Files:**
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md`
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h4-privacy-binding.md`

**Interfaces:**
- Produces exact privacy topology/mechanism/provider-version inputs used by production H4.

- [ ] **Step 1: Assert H4 and privacy gates.**

```bash
H2=docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md
H0=docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md
test -f "$H2" -a -f "$H0"
rg -n '^H4_ALLOWED[[:space:]]*=[[:space:]]*true$' "$H2"
for gate in P4 P5 P6 P14 P15; do rg -n "^${gate}[[:space:]].*PASS" "$H0"; done
```

- [ ] **Step 2: Record exact topology, not an abstraction shortcut.**

Evidence note records whether production uses per-session observer, shared observer + safe demux, per-pane Control Mode `refresh-client -A`, client `no-output`, or another exact H0-qualified combination, including observer replacement/reconnect rules.

- [ ] **Step 3: Freeze the no-replay/first-byte requirements in the note.**

```text
private-before-human-write
all model-visible paths covered
A private does not suppress B/C
observer replacement has no exposure window
no history/capture-pane recovery
```

- [ ] **Step 4: Verify/commit binding.**

Run docs `devctl check`, staged commit-gate, commit `docs: bind h4 secret privacy topology`.

---

### Task 2: Define shell integration, typed readiness, and independent proof contracts

**Files:**
- Create: `internal/core/shellintegration/types.go`
- Create: `internal/core/shellintegration/requirement.go`
- Create: `internal/core/shellintegration/proof.go`
- Create: `internal/core/shellintegration/types_test.go`
- Create: `internal/core/shellintegration/requirement_test.go`
- Create: `internal/core/shellintegration/proof_test.go`
- Modify: `internal/core/interactivehandoff/types.go`
- Modify: `internal/core/interactivehandoff/state.go`
- Create: `internal/core/interactivehandoff/privacy_test.go`
- Modify: `internal/core/failure/failure.go`

**Interfaces:**
- Produces `ShellIdentity`, `CapabilityLevel`, `Requirement{Kind,Name}`, `RequirementResult`, `BoundaryProof`, `PrivacyReleaseProof`.

- [ ] **Step 1: RED-test closed shell/capability/requirement vocabulary.**

Initial shells:

```text
fish
zsh
bash
unknown
```

Capability levels follow L0–L4 master vocabulary. Initial typed requirement is only `environment_exported_nonempty` with a validated variable **name**; no arbitrary command/script/regex.

- [ ] **Step 2: Define proof objects without secret-bearing payload.**

Conceptual shape:

```go
type BoundaryProof struct {
    HandoffID      string
    AuthorityEpoch delegatedsession.AuthorityEpoch
    Shell          ShellIdentity
    Quality        string // shell_prompt, process_boundary, human_attested
    ObservedAt     time.Time
}

type PrivacyReleaseProof struct {
    HandoffID      string
    AuthorityEpoch delegatedsession.AuthorityEpoch
    Shell          ShellIdentity
    Boundary       string
    ForwardOnly    bool
    ObservedAt     time.Time
}
```

No field can carry environment value/hash/length/output snippet.

- [ ] **Step 3: RED-test proof independence.**

```text
human-attested ready -> TransferBoundary yes; PrivacyReleaseProof no
qualified shell prompt after secret command -> may supply both under adapter contract
stale epoch proof -> reject
nested shell drift -> proof unavailable
client idle/no input -> no proof
```

- [ ] **Step 4: Activate privacy state transitions in interactivehandoff core.**

Privacy fields stay orthogonal to owner/ingress. A valid owner transfer cannot automatically flip capture public.

- [ ] **Step 5: Add failure codes.**

At least:

```text
private_output_barrier_failed
privacy_release_unproven
shell_integration_unavailable
shell_integration_lost
requirement_unsupported
requirement_not_satisfied
shell_identity_changed
```

- [ ] **Step 6: Focused/race/commit.**

```bash
go test ./internal/core/shellintegration ./internal/core/interactivehandoff -run 'Shell|Requirement|Proof|Privacy' -count=1
go test -race ./internal/core/shellintegration ./internal/core/interactivehandoff -count=1
go run ./tools/devctl check
git add internal/core/shellintegration internal/core/interactivehandoff internal/core/failure/failure.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: define shell readiness privacy proofs"
```

---

### Task 3: Implement H0-qualified private observation arm/release/recovery in delegated tmux adapter

**Files:**
- Create: `internal/adapter/delegatedtmux/privacy.go`
- Create: `internal/adapter/delegatedtmux/privacy_test.go`
- Create: `internal/adapter/delegatedtmux/privacy_recovery.go`
- Create: `internal/adapter/delegatedtmux/privacy_recovery_test.go`
- Modify: `internal/app/delegatedsession/ports.go`
- Modify: `internal/app/delegatedsession/service.go`
- Create: `internal/app/delegatedsession/privacy_test.go`

**Interfaces:**
- Extend provider semantically with:

```go
type PrivacyHandle struct { OpaqueRef string; Generation string }
ArmPrivateObservation(context.Context, ProviderRef, PrivacySpec) (PrivacyHandle, error)
ProvePrivateObservation(context.Context, ProviderRef, PrivacyHandle) (PrivateObservationProof, error)
ReleasePrivateObservation(context.Context, ProviderRef, PrivacyHandle, ForwardBoundary) error
```

Exact implementation follows Task-1 H0 binding; core/app do not mention tmux flags.

- [ ] **Step 1: RED first-byte privacy test.**

Arm privacy, require provider ACK/proof, only then permit test human writable. Emit a deterministic visible secret canary immediately as first human-echo output; public observer/log must never receive it.

- [ ] **Step 2: RED multi-session isolation.**

A private A emits secret canaries while public B/C emit numbered markers. Every B/C public marker remains visible; no A canary appears. "Suppress all control output" fails.

- [ ] **Step 3: Implement exact H0 topology.**

If H0 selected per-pane `refresh-client -A %pane:off`, honor its qualified backpressure/read behavior; if per-session observer or safe daemon demux was selected, implement that exact mechanism. Do not substitute client-global `no-output` unless H0 proved it safe for this topology.

- [ ] **Step 4: Implement forward-only release.**

Release requires a caller-supplied current `PrivacyReleaseProof` boundary; no `capture-pane`, history replay, or buffered pre-boundary output enters public capture.

- [ ] **Step 5: RED observer restart/overlap fault matrix.**

Daemon/control observer restart during private state must create replacement observation private-before-receive. Old/new overlap can never expose the canary.

- [ ] **Step 6: Native stress/race and commit.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/adapter/delegatedtmux -run 'Privacy|Private|MultiSession|Observer' -count=3
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/adapter/delegatedtmux -run 'Privacy|Observer' -count=1
go run ./tools/devctl check
git add internal/adapter/delegatedtmux internal/app/delegatedsession
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: isolate delegated private output"
```

---

### Task 4: Add truthful capture-quality/output/receipt semantics for private omission

**Files:**
- Modify: `internal/adapter/store/delegated_output.go`
- Modify: `internal/adapter/store/delegated_output_test.go`
- Modify: `internal/core/receipt/delegated_v5_test.go`
- Modify: `internal/core/receipt/result_test.go`
- Create: `api/schema/delegated_private_output_test.go`
- Modify: `internal/app/evidence/worker.go`
- Modify: `internal/app/evidence/worker_test.go`
- Modify: `internal/core/evidence/validation.go`
- Modify: `internal/core/evidence/validity_test.go`

**Interfaces:**
- Consume H1 receipt-v5 composable capture contract exactly:

```text
capture_quality = complete | partial | incomplete
capture_reasons = private_intervals_omitted | transport_gap | provider_lost (unique bounded canonical set)
```

- [ ] **Step 1: RED-test H1 complete path remains complete.**

A delegated session with no private interval keeps `output_complete=true + capture_quality=complete + capture_reasons=[]`; existing direct/B1 receipts stay unchanged.

- [ ] **Step 2: RED-test privacy omission is monotonic and composable with later failure.**

After the first private interval begins, even if public capture later resumes:

```text
output_complete=false
capture_quality=partial
capture_reasons=[private_intervals_omitted]
```

Then inject a transport gap and provider loss. Final state must be:

```text
output_complete=false
capture_quality=incomplete
capture_reasons=[private_intervals_omitted, transport_gap, provider_lost]  # canonical order
```

No later public output or stronger failure may erase an earlier reason.

- [ ] **Step 3: Apply H1 receipt-v5 composable contract; do not create a new receipt version.**

H4 only mutates delegated capture state/reasons under H1's v5 schema. Privacy-only omission is `partial`; any transport/provider uncertainty promotes quality to `incomplete` while retaining `private_intervals_omitted`. Never overload `output_complete=true` to mean "complete except intentional omission".

- [ ] **Step 4: Preserve lifecycle-only evidence authority independently from capture status.**

`internal/app/evidence/worker.go` and `internal/core/evidence/validation.go` must continue rejecting delegated `session_lifecycle_only` receipts as ordinary mechanical verification whether capture quality is complete, partial, or incomplete. Add a regression where a complete delegated transcript with test intent still produces no evidence record, plus private/incomplete cases. Interactive transcript remains advisory; callers may explicitly inspect lifecycle/capture truth but cannot promote it to complete verification merely because child exit was success.

- [ ] **Step 5: Schema/store/receipt focused tests and commit.**

```bash
go test ./internal/core/receipt ./internal/adapter/store ./internal/app/evidence ./internal/core/evidence ./api/schema -run 'Capture|Private|Receipt|Delegated|Evidence' -count=1
go run ./tools/devctl check
git add internal/core/receipt internal/adapter/store internal/app/evidence internal/core/evidence api/schema
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: report private output omission honestly"
```

---

### Task 5: Implement shell identity probing and adapter-neutral orchestration

**Files:**
- Create: `internal/app/shellintegration/ports.go`
- Create: `internal/app/shellintegration/service.go`
- Create: `internal/app/shellintegration/service_test.go`
- Create: `internal/app/shellintegration/identity.go`
- Create: `internal/app/shellintegration/identity_test.go`
- Create: `internal/adapter/shellintegration/detect_unix.go`
- Create: `internal/adapter/shellintegration/detect_unix_test.go`

**Interfaces:**
- `ShellProbe.Probe(session/provider facts) ShellIdentityObservation`.
- `Adapter` installs/removes one ephemeral watcher and translates a closed requirement into one-shot local readiness signal.

- [ ] **Step 1: RED-test `$SHELL` is insufficient.**

Provider/process identity showing live fish while login `$SHELL=/bin/zsh` must resolve fish. Nested/replacement shell mismatch returns changed/unknown until reprobed.

- [ ] **Step 2: Implement exact process/provider-backed current-shell observation.**

Use delegated provider/process facts scoped to the ShellBeam-owned pane; no whole-system continuous process scan.

- [ ] **Step 3: RED-test adapter selection.**

Known exact fish/zsh/bash selects matching adapter. Unknown/nested ambiguous => raw manual H2 control only; never chooses bash syntax as fallback.

- [ ] **Step 4: Implement app lifecycle.**

Install one watcher only for active handoff/closed requirement; remove on satisfied/abort/expiry/client/provider loss. No resident helper.

- [ ] **Step 5: Focused/race/commit.**

```bash
go test ./internal/app/shellintegration ./internal/adapter/shellintegration -run 'Identity|Probe|Lifecycle' -count=1
go test -race ./internal/app/shellintegration ./internal/adapter/shellintegration -run 'Lifecycle' -count=1
go run ./tools/devctl check
git add internal/app/shellintegration internal/adapter/shellintegration
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: detect delegated shell runtime"
```

---

### Task 6: Implement fish, zsh, and bash ephemeral readiness adapters

**Files:**
- Create: `internal/adapter/shellintegration/fish.go`
- Create: `internal/adapter/shellintegration/fish_test.go`
- Create: `internal/adapter/shellintegration/zsh.go`
- Create: `internal/adapter/shellintegration/zsh_test.go`
- Create: `internal/adapter/shellintegration/bash.go`
- Create: `internal/adapter/shellintegration/bash_test.go`
- Create: `internal/adapter/shellintegration/notifier.go`
- Create: `internal/adapter/shellintegration/notifier_test.go`
- Create: `cmd/shellbeam/command_handoff_notify.go`
- Create: `cmd/shellbeam/command_handoff_notify_test.go`
- Modify: private command dispatch in `cmd/shellbeam/command.go` without adding notifier to normal public help.

**Interfaces:**
- Each adapter supports current-shell-safe prompt boundary + `environment_exported_nonempty(name)` returning only boolean metadata.
- Private notifier sends `handoff_id + epoch + adapter event + satisfied bool + proof metadata`, no secret.

- [ ] **Step 1: RED fish hook-preservation/removal tests.**

Adapter adds uniquely named temporary fish function/event hook, preserves preexisting user functions/events, checks variable exported/non-empty only at safe prompt boundary, emits one event, removes itself. No config.fish mutation.

- [ ] **Step 2: RED zsh composable-hook tests.**

Use additive `precmd`/`preexec` mechanism appropriate to qualified zsh without overwriting existing functions/hook arrays. Remove exact ShellBeam hook after terminal event. No `.zshrc` mutation.

- [ ] **Step 3: RED bash prompt-hook preservation tests.**

Compose with existing `PROMPT_COMMAND`/qualified hook mechanism, preserve array/string form according to supported bash versions, remove only ShellBeam fragment. No `.bashrc` mutation.

- [ ] **Step 4: Implement minimally allowlisted notifier environment.**

Notifier invocation is constructed from installed ShellBeam executable + opaque handoff/event IDs. Its environment contains only required local runtime/IPC identity and safe executable-resolution values. Explicit tests export a secret into delegated shell then prove notifier process environment/argv/log does not contain it.

- [ ] **Step 5: Native shell version matrix.**

Run real fish/zsh/bash processes for supported versions: unset/empty/non-exported/exported-nonempty, existing hooks, command failure, nested shell drift, abort during watcher, daemon loss, repeated 100 install/remove cycles.

- [ ] **Step 6: Focused/race/commit.**

```bash
go test ./internal/adapter/shellintegration ./cmd/shellbeam -run 'Fish|Zsh|Bash|Notifier' -count=1
go test -race ./internal/adapter/shellintegration -run 'Notifier|Lifecycle' -count=1
go run ./tools/devctl check
git add internal/adapter/shellintegration cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: add ephemeral shell readiness adapters"
```

---

### Task 7: Orchestrate secret handoff, automatic readiness, abort, and separate privacy release

**Files:**
- Modify: `internal/app/interactivehandoff/service.go`
- Create: `internal/app/interactivehandoff/privacy.go`
- Create: `internal/app/interactivehandoff/privacy_test.go`
- Create: `internal/app/interactivehandoff/readiness.go`
- Create: `internal/app/interactivehandoff/readiness_test.go`
- Create: `internal/app/interactivehandoff/private_abort_test.go`
- Modify: `internal/app/daemon/handoff_actions.go`
- Create: `internal/app/daemon/handoff_privacy_test.go`

**Interfaces:**
- Enables public `privacy=secret` and `completion.kind=environment_exported_nonempty` only when provider privacy + exact shell adapter capability intersect.

- [ ] **Step 1: RED secret handoff start ordering.**

Exact order:

```text
durable handoff bind + epoch rotate
agent ingress fenced
shell identity/relevant integration prepared
provider private observation armed
private observation ACK/proof current
human client attached/proven
human client writable
publish HUMAN_OWNED/private
```

Any privacy-arm failure prevents human writability.

- [ ] **Step 2: RED automatic readiness/reclaim.**

At qualified shell safe boundary:

```text
closed requirement satisfied boolean
BoundaryProof produced
PrivacyReleaseProof produced only by qualified boundary
human ingress fenced
exact human client read-only/detached
provider authority reconciled agent
private observation released from new forward-only boundary
public capture resumes
```

Order may allow agent authority before capture release only if canonical fields record capture still private; never release capture first.

- [ ] **Step 3: RED manual ready under secret/unknown shell.**

Manual ready may establish transfer under permitted policy but `privacy_release` stays unproven and capture stays private. Poll/read-output reports private omission; no history replay. If workflow needs visible output, user must resume/reach a qualified boundary/terminate.

- [ ] **Step 4: RED secret abort.**

Abort fences human ingress, desired owner none, agent ingress fenced, capture remains private. Local `resume` may re-enable human after revalidation; explicit terminate closes session. Abort alone never publicizes buffered/application-late output.

- [ ] **Step 5: Requirement validity remains separate.**

After successful handback, public state says requirement satisfied only. Agent must run actual capability command; H4 does not turn presence into credential-valid evidence.

- [ ] **Step 6: Focused/race/commit.**

```bash
go test ./internal/app/interactivehandoff ./internal/app/daemon -run 'Privacy|Secret|Readiness|Abort' -count=1
go test -race ./internal/app/interactivehandoff ./internal/app/daemon -run 'Privacy|Readiness' -count=1
go run ./tools/devctl check
git add internal/app/interactivehandoff internal/app/daemon
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: automate secret handoff readiness"
```

---

### Task 8: Public schema/capability/doctor privacy projection

**Files:**
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/interactive_handoff_privacy_test.go`
- Modify: IPC/MCP handoff mapping files.
- Modify: `internal/core/capability/catalog.go`
- Modify: `cmd/shellbeam/interactive_handoff.go`
- Modify: `cmd/shellbeam/doctor.go`
- Create: `cmd/shellbeam/doctor_handoff_privacy_test.go`

**Interfaces:**
- Capability advertises provider privacy, shell integration levels per shell, requirement kinds, and capture-quality support; no secret values.

- [ ] **Step 1: RED schema tests for secret/typed requirement.**

Variable name is bounded safe identifier; no caller-provided script/regex/expected secret value. `handoff.wait/inspect` output has proof qualities/status only.

- [ ] **Step 2: Add capability intersection.**

Examples:

```text
qualified tmux privacy + fish adapter + H2 = secret/full handoff available
privacy provider + unknown shell            = secret manual private interaction; automatic requirement unavailable
shell adapter + no privacy provider         = standard readiness only; secret unavailable
```

- [ ] **Step 3: Doctor prints actionable provider/shell levels and privacy topology availability without environment values/private tokens/history.**

- [ ] **Step 4: Legacy/one-tool/schema tests and commit.**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/core/capability ./cmd/shellbeam -run 'Handoff|Privacy|Shell|Doctor' -count=1
go run ./tools/devctl check
git add api/schema internal/adapter/ipc internal/adapter/mcp internal/core/capability cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: expose secret handoff capability"
```

---

### Task 9: Native secret-canary, restart, resource, and H4 checkpoint acceptance

**Files:**
- Create: `tests/integration/interactive_handoff_secret_test.go`
- Create: `tests/integration/interactive_handoff_h4_composition_test.go`
- Create: `cmd/shellbeam/interactive_handoff_secret_acceptance_test.go`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h4-secret-privacy.md`

**Interfaces:**
- Produces `H4_COMPLETE=true|false` and `H5_DESIGN_ALLOWED=true|false`; H3 may be independently absent/present.

- [ ] **Step 1: Full deterministic secret-canary scan.**

Type a visible canary such as `export SHELLBEAM_H4_SECRET=<canary>` during private interval and assert literal canary plus common encodings/hashes do **not** appear in:

```text
MCP results
IPC results
canonical/public output
receipts
Event Journal
handoff/delegated metadata
telemetry
repro/evidence
state files
logs/errors
provider reconnect/resync output
notifier argv/environment
```

Do not persist the real canary in tracked evidence; tracked report records generated test identity/result only.

- [ ] **Step 2: Multi-session/observer-overlap matrix.**

Private A + noisy public B/C under 100 repeated arm/release/reconnect cycles. A never leaks; B/C are not silently suppressed. Fault old/new observer overlap at every H0 P15 point.

- [ ] **Step 3: Shell matrix.**

For fish/zsh/bash: exported/non-empty satisfied, empty/unset not satisfied, existing hooks preserved, nested shell drift degrades, watcher removed on abort/expiry/success, no dotfile residue, 100 cycles no helper/hook creep.

- [ ] **Step 4: Daemon restart inside private interval.**

Run actual H2+H4 with H3 absent and prove secret/privacy capability works while automatic terminal presentation remains absent/manual fallback remains valid. If tracked H3 evidence already exists, also run actual H2+H3+H4 composition and require capability/result coexistence with no cross-feature assumption; otherwise record combined lane `NOT_RUN_COUNTERPART_ABSENT`.

Hard-kill daemon while human-private interaction is active. Replacement observer is private-before-receive; no capture-pane/history reconstruction; authority/privacy state reconciles fail-closed.

- [ ] **Step 5: Output/evidence truth.**

Privacy-only session receipt/result must show `output_complete=false + capture_quality=partial + capture_reasons=[private_intervals_omitted]`; if later transport/provider failure occurs, quality becomes `incomplete` while preserving all prior reasons. Evidence/verification consumers still refuse ordinary authority because the receipt remains `session_lifecycle_only`, even for complete capture.

- [ ] **Step 6: Fresh gates.**

```bash
go mod verify
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/core/shellintegration ./internal/app/shellintegration ./internal/adapter/shellintegration ./internal/adapter/delegatedtmux ./internal/app/interactivehandoff ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/app/shellintegration ./internal/adapter/shellintegration ./internal/adapter/delegatedtmux ./internal/app/interactivehandoff ./internal/app/daemon -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
git diff --check
```

- [ ] **Step 7: Anti-goal/privacy scans.**

```bash
rg -n 'capture-pane|\.zshrc|\.bashrc|config\.fish|preferred_terminal' internal cmd api || true
rg -n 'env\s*$|printenv|set\s+-x.*VALUE' internal/adapter/shellintegration || true
```

Review all notifier/help/log/error formatting for secret-derived material.

- [ ] **Step 8: Write evidence and final H4 commit.**

Tracked evidence records exact H0/H2 provider identities, privacy topology, shell/version matrix, proof ordering, canary PASS/FAIL counts, restart/resource results, capture quality and:

```text
H4_COMPLETE=true|false
H5_DESIGN_ALLOWED=true|false
CONTEXT_EXEC_AVAILABLE=false
```

Stage exact scope, commit-gate, commit `test: verify secret interactive handoff`; require postcommit clean tree + fresh devctl check.

---

## H4 Completion Gate

H4 is complete only when:

1. H0 privacy topology is exact/current and all required privacy gates remain proven in production path;
2. private observation is established before any secret human write and every model-visible path is covered;
3. private A never leaks and never silently suppresses unrelated public B/C;
4. reconnect/observer replacement preserves privacy from first byte with no history replay;
5. TransferBoundary and PrivacyReleaseProof remain distinct in state and tests;
6. manual ready never releases secret capture by itself;
7. public capture resumes only after human ingress fence + current privacy proof + forward-only boundary;
8. capture truth is composable: privacy-only omission is `partial + private_intervals_omitted`, and later transport/provider failures promote to `incomplete` without erasing prior reasons;
9. delegated receipts remain `session_lifecycle_only` and cannot become ordinary evidence merely because capture is complete;
10. actual H2+H4/no-H3 composition passes; if H3 is present, actual H2+H3+H4 composition also passes, otherwise combined lane is `NOT_RUN_COUNTERPART_ABSENT`;
11. fish/zsh/bash adapters identify current shell, preserve user hooks, modify no dotfiles, and leave no resident helper;
12. unknown/nested shell never receives guessed syntax and degrades to manual behavior;
13. environment-export readiness emits only boolean/proof metadata and never value/hash/length;
14. notifier/helper environment excludes secret-bearing delegated exports;
15. abort during private interaction fences both write lanes/public capture until safe local resolution;
16. credential presence remains distinct from real capability verification;
17. interactive transcript remains weaker/advisory evidence and context-exec remains unavailable;
18. native canary/restart/multi-session/resource/race/schema/devctl gates pass and H5 design gate is recorded.
