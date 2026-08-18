# Interactive Handoff H2 Human Authority and Manual Control Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add durable human↔agent ownership transfer, manual local attach, exact ingress fencing, shell-independent HumanControl, manual transfer boundaries, and fail-closed recovery on top of a verified H1 delegated session, without automatic terminal selection or secret/private-output automation.

**Architecture:** `internal/core/interactivehandoff` owns the orthogonal durable handoff dimensions and derived status projections; `internal/app/interactivehandoff` owns transfer orchestration, waits, and HumanControl; H1's delegated provider port is extended only with H0-qualified human-client attach/writability/fence primitives. A private/local attach CLI talks to the existing daemon over same-user IPC; public MCP remains one tool with bounded `handoff.request/wait/abort/inspect` actions.

**Tech Stack:** H1 delegated session core; H0-qualified tmux human-client/fence/control mechanism; existing Unix IPC/peer authentication, Event Journal, atomic store, ULID/ID conventions, Go 1.26.6.

**Spec:** `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `d54b15836e7fc53fae5afc7bbd06013f90d02bf5`; H1 plan `docs/superpowers/plans/2026-08-18-interactive-handoff-h1-delegated-session-core.md`.

## Global Constraints

- HARD PRECONDITION: tracked H1 evidence reports `H2_ALLOWED=true` and exact H1 HEAD/provider binding.
- H2 supports `privacy=standard` + `completion.kind=manual_ready`. `privacy=secret` and shell-aware automatic completion remain `feature_unavailable` until H4.
- Canonical correctness state is orthogonal dimensions, not one giant ownership enum. Derived names are projection only.
- Transfer intent rotates `authority_epoch` durably before next-owner admission.
- Agent ingress and human ingress are separately fenced. Provider read-only/writable flag alone is not a claim about application quiescence.
- Manual `ready` may produce a human-attested `TransferBoundary`; it is not a `PrivacyReleaseProof`.
- HumanControl must never be pane-stdin text. Writable-state control and read-only/fenced-state local control paths must be reachable exactly as H0 qualified.
- `handoff.abort` means revoke/fence further human authority, not rollback bytes already delivered. It does not implicitly kill the session.
- Normal agent write/signal/kill while human-owned is rejected before provider mutation. No model emergency bypass in H2.
- Human attach/switch/re-attach is presentation, not environment synchronization; use H0-qualified `-E`/equivalent semantics.
- Manual attach uses the installed ShellBeam executable/daemon identity, never source checkout paths.
- Human input bytes are not copied into agent input ledger, Event Journal, receipts, logs, telemetry, repro, or evidence.
- No automatic GUI terminal launch/reveal in H2; the public result may return a safe local attach argv/instruction.
- No shell dotfile modifications, no shell readiness hooks, no per-handoff pollers/resident watchers.

- Do not edit `dev/test-impact.toml` preemptively; if fresh `devctl` evidence demonstrates under-selection, stop, document the concrete gap, amend this plan with the exact mapping/test, then continue.

## Responsibility Map

- `internal/core/interactivehandoff`: request vocabulary, canonical orthogonal state, phases, transfer-boundary quality, HumanControl kinds, derived status, validation.
- `internal/adapter/store`: durable handoff binding/state, exact human-client ref metadata, handoff/control idempotency records, wait/restart index.
- `internal/app/interactivehandoff`: request/wait/attach/control/reclaim/abort/reconcile service.
- `internal/app/delegatedsession`: extend provider port with human attach, exact client observation, ingress fence, write-enable/disable only as H0 proved.
- `internal/adapter/delegatedtmux`: implement human-client mechanics; no terminal-emulator launching.
- `internal/adapter/ipc`: public handoff actions + private local attach/control IPC branch with same-user authentication.
- `internal/adapter/mcp`: public one-tool handoff actions, no private local-control transport exposure.
- `cmd/shellbeam`: `shellbeam session attach --handoff-id ...` local UX and private attach wrapper/control surface.

---

### Task 1: Verify H1 gate and freeze H2 authority inputs

**Files:**
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-authority-binding.md`

**Interfaces:**
- Consumes exact H1 session-mode/provider/authority contract and H0 P2/P3/P8/P9 human-control facts.
- Produces the exact H2 provider mechanism and H1 commit baseline.

- [ ] **Step 1: Assert H2 gate.**

```bash
E=docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md
test -f "$E"
rg -n '^H2_ALLOWED[[:space:]]*=[[:space:]]*true$' "$E"
```

Also read H0 evidence and require P2/P3/P8/P9 PASS for the mechanism H2 will use.

- [ ] **Step 2: Record immutable H2 inputs.**

Include:

```text
h1_head
provider_id/version
input_fence_mechanism
human writable/read-only control mechanism
writable-state OOB mechanism
fenced/read-only local-control reachability mechanism
attach environment-preservation mechanism
```

- [ ] **Step 3: Verify/commit binding note.**

```bash
git diff --check
go run ./tools/devctl check
git add docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-authority-binding.md
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "docs: bind h2 human authority prerequisites"
```

---

### Task 2: Define orthogonal handoff state and HumanControl core contracts

**Files:**
- Create: `internal/core/interactivehandoff/types.go`
- Create: `internal/core/interactivehandoff/state.go`
- Create: `internal/core/interactivehandoff/control.go`
- Create: `internal/core/interactivehandoff/projection.go`
- Create: `internal/core/interactivehandoff/types_test.go`
- Create: `internal/core/interactivehandoff/state_test.go`
- Create: `internal/core/interactivehandoff/projection_test.go`
- Modify: `internal/core/failure/failure.go`

**Interfaces:**
- Produces `Request`, `Reason`, `Privacy`, `Completion`, `Phase`, `IngressState`, `TransferBoundary`, `HumanControlKind`, `State`, and `DerivedStatus`.

- [ ] **Step 1: RED-test closed request vocabulary.**

Initial H2-valid public request:

```go
type Request struct {
    HandoffID string
    SessionID string
    Reason    Reason
    Privacy   Privacy
    Completion Completion
}
```

Closed values from master spec, but H2 validation accepts only `PrivacyStandard` + `CompletionManualReady`; typed secret/environment readiness is recognized as future vocabulary but returns feature-unavailable at service capability gate.

- [ ] **Step 2: Define canonical orthogonal state.**

Use separate fields equivalent to:

```go
type State struct {
    SchemaVersion    int
    HandoffID        string
    SessionID        string
    Phase            Phase
    AuthorityEpoch   delegatedsession.AuthorityEpoch
    DesiredOwner     delegatedsession.Owner
    ProviderOwner    delegatedsession.Owner
    AgentIngress     IngressState
    HumanIngress     IngressState
    TransferBoundary TransferBoundary
    PrivacyState     PrivacyState
    PrivacyRelease   PrivacyReleaseState
    CaptureState     CaptureState
    HumanClient      *HumanClientRef
    ProviderGeneration string
}
```

In H2, privacy/capture fields remain standard/public; keep them structurally separate so H4 does not need a state migration from a combined enum.

- [ ] **Step 3: RED-test derived status projections.**

Examples:

```text
desired agent + both provider/ingress match        -> AGENT_OWNED
desired human + agent fenced + human writable     -> HUMAN_OWNED
transfer in progress                               -> AGENT_FENCING/HUMAN_CONNECTING/HUMAN_FENCING
abort + both ingress fenced                        -> ABORTED/RECLAIM_BLOCKED derived status
```

Projection may never grant authority; it only describes canonical fields.

- [ ] **Step 4: Define HumanControl signal identity.**

```go
type ControlSignal struct {
    HandoffID      string
    AuthorityEpoch delegatedsession.AuthorityEpoch
    ControlID      string
    Kind           HumanControlKind // ready, abort, status, resume, terminate, request_control
}
```

Validation forbids stale epoch/control ID reuse with different kind.

- [ ] **Step 5: Add H2 failure codes.**

At least:

```text
handoff_conflict
handoff_not_pending
handoff_expired
handoff_client_lost
handoff_reclaim_blocked
human_control_unreachable
human_client_not_proven
```

- [ ] **Step 6: Focused/race test and commit.**

```bash
go test ./internal/core/interactivehandoff -count=1
go test -race ./internal/core/interactivehandoff -count=1
go run ./tools/devctl check
git add internal/core/interactivehandoff internal/core/failure/failure.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: define interactive handoff state"
```

---

### Task 3: Persist handoff state, epoch transitions, and control idempotency

**Files:**
- Create: `internal/adapter/store/interactive_handoff_paths.go`
- Create: `internal/adapter/store/interactive_handoffs.go`
- Create: `internal/adapter/store/interactive_handoffs_test.go`
- Create: `internal/adapter/store/human_control_ledger.go`
- Create: `internal/adapter/store/human_control_ledger_test.go`
- Create: `internal/adapter/store/interactive_handoff_restart.go`
- Create: `internal/adapter/store/interactive_handoff_restart_test.go`
- Modify: `internal/app/daemon/store_port.go`

**Interfaces:**
- Produces atomic `ReserveHandoff`, `AdvanceHandoff`, `LoadHandoff`, `ReserveControlSignal`, `CompleteControlSignal`, `ListHandoffRecoveryCandidates`.

- [ ] **Step 1: RED-test handoff idempotency/conflict.**

Same `handoff_id` + exact request replays current state. Same ID + changed session/reason/privacy/completion conflicts. Handoff reservation requires exact live delegated session and current epoch.

- [ ] **Step 2: Implement transfer-intent epoch rotation atomically with desired-state advance.**

When transfer agent→human is accepted, persisted handoff state and delegated binding move from epoch N to N+1 before any human-write provider mutation. No API response may expose human authority before this durability point.

- [ ] **Step 3: RED-test old-epoch control replay semantics.**

Known exact `ready`/`abort` signal from old epoch replays prior outcome; unseen signal from old epoch is stale. Signal from current epoch but impossible phase returns `handoff_not_pending`/ownership error.

- [ ] **Step 4: Persist human-client identity as provider-safe opaque ref only.**

Public state may expose `attached=true`, provider/terminal friendly identity later, but not private tmux socket/client token required for provider control.

- [ ] **Step 5: Fault/restart tests.**

Fault after epoch rotation before provider fence/attach must recover to both ingress denied until reconciliation proves the next safe step.

- [ ] **Step 6: Focused/race/commit.**

```bash
go test ./internal/adapter/store -run 'Handoff|HumanControl' -count=1
go test -race ./internal/adapter/store -run 'Handoff|HumanControl' -count=1
go run ./tools/devctl check
git add internal/adapter/store internal/app/daemon/store_port.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: persist human handoff authority"
```

---

### Task 4: Extend delegated provider port for human attach and exact ingress fencing

**Files:**
- Modify: `internal/app/delegatedsession/ports.go`
- Modify: `internal/app/delegatedsession/service.go`
- Create: `internal/app/delegatedsession/human_control_test.go`
- Create: `internal/adapter/delegatedtmux/human_client.go`
- Create: `internal/adapter/delegatedtmux/human_client_test.go`
- Create: `internal/adapter/delegatedtmux/ingress_fence.go`
- Create: `internal/adapter/delegatedtmux/ingress_fence_test.go`
- Create: `internal/adapter/delegatedtmux/human_control.go`
- Create: `internal/adapter/delegatedtmux/human_control_test.go`

**Interfaces:**
- Extend provider with semantic operations equivalent to:

```go
type HumanAttachResult struct { ClientRef ProviderClientRef; ObservedOwner delegatedsession.Owner }
AttachHuman(context.Context, ProviderRef, HumanAttachSpec) (HumanAttachResult, error)
SetHumanWritable(context.Context, ProviderRef, ProviderClientRef, bool) error
FenceHumanIngress(context.Context, ProviderRef, ProviderClientRef, AuthorityEpoch) (IngressFenceProof, error)
InspectHumanClient(context.Context, ProviderRef, ProviderClientRef) (HumanClientObservation, error)
```

- [ ] **Step 1: RED-test H0 P2/P3 semantics through production adapter.**

Prove exact target client only, no other client made writable, and fence means no **new** human ingress after fence point. Do not assert PTY/application drain.

- [ ] **Step 2: Implement attach with environment preservation.**

Every attach/switch uses H0-qualified `-E`/equivalent. Test a sentinel session environment value remains unchanged after attaching a human client whose process environment has a conflicting value.

- [ ] **Step 3: Implement writable-state OOB controls and fenced/read-only control reachability exactly as H0 selected.**

If H0 selected detach-to-local-control for read-only state, production adapter must expose the transition, not invent arbitrary read-only key bindings.

- [ ] **Step 4: Exact client-loss and provider-mismatch handling.**

Client disappearance is `client_lost`, never success/readiness. Unknown exact target blocks reclaim.

- [ ] **Step 5: Native production-path H0 subset tests.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/adapter/delegatedtmux -run 'Human|Fence|Control|Environment' -count=3
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/adapter/delegatedtmux -run 'Human|Fence|Control' -count=1
```

- [ ] **Step 6: Commit.**

```bash
go run ./tools/devctl check
git add internal/app/delegatedsession internal/adapter/delegatedtmux
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: fence human delegated ingress"
```

---

### Task 5: Implement handoff request/wait/ready/abort/reclaim orchestration

**Files:**
- Create: `internal/app/interactivehandoff/ports.go`
- Create: `internal/app/interactivehandoff/service.go`
- Create: `internal/app/interactivehandoff/transfer.go`
- Create: `internal/app/interactivehandoff/control.go`
- Create: `internal/app/interactivehandoff/wait.go`
- Create: `internal/app/interactivehandoff/service_test.go`
- Create: `internal/app/interactivehandoff/transfer_test.go`
- Create: `internal/app/interactivehandoff/control_test.go`
- Create: `internal/app/daemon/handoff_port.go`
- Create: `internal/app/daemon/handoff_actions.go`
- Create: `internal/app/daemon/handoff_actions_test.go`

**Interfaces:**
- Public service methods: `Request`, `Wait`, `Abort`, `Inspect`.
- Private/local methods: `AttachLocalHuman`, `HumanControl`.

- [ ] **Step 1: RED-test agent→human ordering.**

Exact H2 sequence:

```text
reserve handoff + rotate epoch
deny new agent ingress
FenceAgentIngress/prove provider no longer accepts agent mutation
manual TransferBoundary policy for human grant
attach exact human client read-only first
prove exact client
make exact client writable
observe provider human owner
publish derived HUMAN_OWNED
```

No step may be skipped after partial failure.

- [ ] **Step 2: Implement manual-ready human→agent ordering.**

```text
accept generation-bound ready
record human-attested TransferBoundary
stop/fence new human ingress
prove exact ingress fence
make exact human client read-only or detach under H0-qualified fallback
reconcile provider desired agent authority
publish agent authority at next epoch
```

H2 has no private capture barrier, so capture remains public throughout standard handoff.

- [ ] **Step 3: RED-test abort.**

Abort while human-owned fences further human input and leaves desired owner none/fenced until an admissible local `resume` or explicit terminate path. It does not imply command/application reversal and does not kill by itself.

- [ ] **Step 4: Implement event-driven wait.**

Use daemon condition/event channel with advertised max wait; no polling/ticker per handoff. Timeout returns current state, retry-safe.

- [ ] **Step 5: Reject normal agent control while human owns the session.**

H1 write/kill path must consult effective authority; human-owned returns `session_control_not_owned` before mutation reservation/provider call.

- [ ] **Step 6: Focused/race tests and commit.**

```bash
go test ./internal/app/interactivehandoff ./internal/app/daemon -run 'Handoff|HumanOwned' -count=1
go test -race ./internal/app/interactivehandoff ./internal/app/daemon -run 'Handoff' -count=1
go run ./tools/devctl check
git add internal/app/interactivehandoff internal/app/daemon
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: arbitrate human agent handoff"
```

---

### Task 6: Add private local attach/control IPC and `shellbeam session attach`

**Files:**
- Create: `internal/adapter/ipc/handoff_local.go`
- Create: `internal/adapter/ipc/handoff_local_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Create: `cmd/shellbeam/command_session.go`
- Create: `cmd/shellbeam/command_session_test.go`
- Create: `cmd/shellbeam/session_attach.go`
- Create: `cmd/shellbeam/session_attach_test.go`
- Modify: `cmd/shellbeam/command.go`

**Interfaces:**
- Local CLI: `shellbeam session attach --handoff-id <id>`.
- Private IPC supports exact attach request and generation-bound HumanControl signal; it is not an MCP action branch exposed to the model.

- [ ] **Step 1: RED-test CLI parsing and source-tree independence.**

Only exact handoff ID accepted; no arbitrary session/pane/socket argv. Installed executable resolves runtime through normal config/daemon endpoint discovery.

- [ ] **Step 2: Implement attach bootstrap.**

Flow:

```text
CLI connects same-user IPC
resolve durable handoff
request provider human attachment
bind exact client ref to handoff
enter local terminal attach loop/provider client
```

The attach helper receives no secret values and never gets arbitrary model command text.

- [ ] **Step 3: Implement local HumanControl surface.**

Writable-state OOB path sends `ready|abort|status`; fenced/read-only path exposes `resume|terminate|status` using H0-qualified reachability. Every mutation sends exact `handoff_id + authority_epoch + control_id`.

- [ ] **Step 4: RED-test stale/duplicate local controls.**

Lost local response + retry must replay exact prior outcome; old handoff/epoch signal cannot complete a newer handoff.

- [ ] **Step 5: Ensure command is local UX, not public MCP second tool.**

Public `shellbeam --help` may document `session attach`; private provider/socket details remain absent.

- [ ] **Step 6: Focused/native tests and commit.**

```bash
go test ./internal/adapter/ipc ./cmd/shellbeam -run 'SessionAttach|HandoffLocal' -count=1
go test -race ./internal/adapter/ipc -run 'HandoffLocal' -count=1
go run ./tools/devctl check
git add internal/adapter/ipc cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: add local interactive handoff attach"
```

---

### Task 7: Expose bounded public handoff actions in the existing MCP tool

**Files:**
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/interactive_handoff_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_fields.go`
- Modify: `internal/adapter/ipc/protocol_v2_decode.go`
- Modify: `internal/adapter/ipc/client_v2_request.go`
- Modify: `internal/adapter/ipc/response_v2.go`
- Modify: `internal/adapter/ipc/server_v2_unix.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/input_fields.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/request.go`
- Modify: `internal/adapter/mcp/server.go`
- Create: `internal/adapter/ipc/interactive_handoff_test.go`
- Create: `internal/adapter/mcp/interactive_handoff_test.go`
- Modify: `internal/core/capability/catalog.go`
- Create: `cmd/shellbeam/interactive_handoff.go`
- Create: `cmd/shellbeam/interactive_handoff_test.go`

**Interfaces:**
- Public actions: `handoff.request`, `handoff.wait`, `handoff.abort`, `inspect.handoff`.
- Public H2 capability advertises manual standard handoff only; secret/shell-aware flags false/unavailable.

- [ ] **Step 1: RED closed-schema tests.**

`handoff.request` requires stable handoff/session IDs, closed reason/privacy/completion objects. H2 runtime rejects `privacy=secret` and automatic readiness capability even if schema vocabulary recognizes future values.

- [ ] **Step 2: Define bounded public state projection.**

Return IDs, current epoch, derived status, ingress/transfer-boundary quality, attached boolean, failure code, timestamps, and safe attach instruction/argv where appropriate. Never return human keystrokes/provider tokens/private tmux IDs.

- [ ] **Step 3: Add one-tool action dispatch and legacy closure.**

MCP v1 rejects new action; v2 routes within existing local_shell tool. `inspect.server` truthfully reports manual handoff capability only when H1+H2 composition is present.

- [ ] **Step 4: Schema/adapter tests and commit.**

```bash
go test ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/core/capability -run 'Handoff|Delegated' -count=1
go test -race ./internal/adapter/ipc ./internal/adapter/mcp -run 'Handoff' -count=1
go run ./tools/devctl check
git add api/schema internal/adapter/ipc internal/adapter/mcp internal/core/capability cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: expose manual interactive handoff"
```

---

### Task 8: Restart/recovery, expiry, client loss, and Event Journal integration

**Files:**
- Create: `internal/app/interactivehandoff/reconcile.go`
- Create: `internal/app/interactivehandoff/reconcile_test.go`
- Create: `internal/app/daemon/handoff_reconcile.go`
- Create: `internal/app/daemon/handoff_reconcile_test.go`
- Modify: `internal/core/observation/events.go` or current event-kind file.
- Modify: `internal/adapter/store/events.go`/handoff event persistence as current architecture requires.
- Create: `cmd/shellbeam/interactive_handoff_acceptance_test.go`

**Interfaces:**
- Startup reconciliation derives effective authority from durable desired state + current epoch + fresh provider/human-client observation.

- [ ] **Step 1: RED fault matrix at every ownership boundary.**

Faults:

```text
after epoch rotate before agent fence
after agent fence before human attach
after attach before human writable
after ready before human fence
after human fence before read-only/detach ACK
after provider owner reconciled before response
human client closes while human-owned
provider/control observer dies
```

- [ ] **Step 2: Implement fail-closed reconciliation.**

Mismatch means both write lanes denied until exact state re-proven. Client disappearance never implies successful handoff completion.

- [ ] **Step 3: Add bounded handoff expiry using central mechanism.**

Expiry stops waiting/temporary local authority, fences as safely possible, does not kill the delegated session by default, and creates no per-handoff ticker.

- [ ] **Step 4: Add metadata-only lifecycle events exactly once.**

Events may describe request/attached/human-owned/reclaim/aborted/client-lost/expired states without keystrokes or private provider refs.

- [ ] **Step 5: Native daemon-crash acceptance and commit.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./cmd/shellbeam -run 'InteractiveHandoff' -count=3
go test -race ./internal/app/interactivehandoff ./internal/app/daemon -run 'Handoff.*Reconcile|Handoff.*Fault' -count=1
go run ./tools/devctl check
git add internal/app/interactivehandoff internal/app/daemon internal/core/observation internal/adapter/store cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: recover manual human handoffs"
```

---

### Task 9: H2 manual end-to-end acceptance and checkpoint

**Files:**
- Create: `tests/integration/interactive_handoff_manual_test.go`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md`

**Interfaces:**
- Produces `H3_ALLOWED` and `H4_ALLOWED` gates. H3 and H4 may be planned/executed independently after the same H2 checkpoint, but neither may assume the other exists.

- [ ] **Step 1: Native manual UX matrix.**

Prove:

```text
agent-owned delegated shell
handoff.request standard/manual
returned local attach command
real local attach to exact shell
agent writes rejected while human-owned
human raw input reaches same shell
local Ready -> human ingress fenced -> read-only/passive or qualified detach -> agent resumes
terminal process may remain open
second handoff reuses/proves exact client where supported
```

- [ ] **Step 2: OOB/abort/stale-generation matrix.**

Prove no control text is sent into pane stdin; abort does not kill; stale ready from prior handoff cannot complete next handoff; duplicate ready/abort is idempotent.

- [ ] **Step 3: Environment preservation/privacy anti-leak scan.**

Attach a terminal process with conflicting environment sentinel and prove delegated session environment unchanged. Assert human input sentinel does not appear in ShellBeam public logs/receipts/events/metadata; H2 does **not** claim secret-private output because terminal echo remains public.

- [ ] **Step 4: Fresh verification.**

```bash
go mod verify
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/core/interactivehandoff ./internal/app/interactivehandoff ./internal/app/delegatedsession ./internal/adapter/delegatedtmux ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/core/interactivehandoff ./internal/app/interactivehandoff ./internal/adapter/delegatedtmux ./internal/app/daemon -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
git diff --check
```

- [ ] **Step 5: Anti-goal scan.**

```bash
rg -n 'preferred_terminal|capture-pane.*(loop|poll)|\.zshrc|\.bashrc|config\.fish' internal cmd api || true
rg -n 'privacy=secret|environment_exported_nonempty' internal/app/interactivehandoff api/schema | head
```

Runtime H2 must still reject secret/automatic readiness even if future vocabulary appears in schema tests.

- [ ] **Step 6: Write evidence and final H2 commit.**

Evidence includes exact H1/H0 identity, manual lifecycle, epoch transitions, H0 fence/control mechanism, native platform status, restart/client-loss matrix, and:

```text
H3_ALLOWED=true|false
H4_ALLOWED=true|false
SECRET_HANDOFF_AVAILABLE=false
```

Stage exact scope, run commit-gate, commit `test: verify manual human agent handoff`, then require clean tree + fresh `devctl check`.

---

## H2 Completion Gate

H2 is complete only when one exact committed tree proves:

1. H1 delegated session core is verified and current;
2. canonical handoff state remains orthogonal and derived statuses grant no authority;
3. accepted transfer intent rotates epoch before next-owner provider mutation;
4. agent and human ingress never overlap under ShellBeam control;
5. H0-qualified `FenceHumanIngress` is used as ingress proof, not application-quiescence fiction;
6. manual human `ready` produces a transfer boundary without pretending to release secret privacy;
7. exact human client is attached with environment-preserving semantics and only it becomes writable;
8. local HumanControl remains reachable in writable and fenced/read-only states without pane stdin;
9. normal agent write/kill is rejected while human-owned;
10. abort fences further human authority but is not rollback or implicit kill;
11. restart/client-loss/provider-loss recover fail-closed from durable state + exact observation;
12. handoff wait/expiry are event-driven/bounded with no resident per-handoff watcher;
13. public MCP remains one tool and private local-control transport is not exposed as a second model tool;
14. H2 does not advertise secret privacy, shell readiness, or automatic terminal launch;
15. native/race/schema/privacy-metadata/devctl gates pass and H3/H4 gates are recorded.
