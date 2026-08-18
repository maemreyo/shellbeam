# Interactive Handoff H1 Delegated Session Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the experimental `session_mode="delegated_interactive"` execution core with durable provider identity, authority generations, retry-safe delegated control, exact tmux-provider reconciliation, and daemon-restart continuity, without human handoff/terminal launch/shell privacy automation yet.

**Architecture:** Reuse ShellBeam operation/session/store conventions but keep delegated interactive lifecycle separate from B1.0 per-session supervisor semantics. `internal/core/delegatedsession` owns provider-neutral mode/authority/mutation contracts, `internal/app/delegatedsession` owns orchestration and provider ports, and `internal/adapter/delegatedtmux` implements only the H0-qualified tmux topology/mechanism. H1 exposes one new start mode and epoch-bound agent control while leaving H2–H5 unavailable.

**Tech Stack:** Go 1.26.6 repository toolchain; current ShellBeam v2 MCP/IPC/schema/store/daemon stack; system tmux and exact H0-qualified Control Mode mechanism; optional `github.com/atomicstack/gotmuxcc` only if the tracked H0 evidence explicitly selects it.

**Spec:** `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`; H0 plan `docs/superpowers/plans/2026-08-18-interactive-handoff-h0-tmux-qualification.md` at/after `887c4b7240024bace5ce144624bc458f4b7742cd`.

## Global Constraints

- Current approved execution scope is **Darwin/macOS only**. Linux remains intended but unadvertised and fail-closed until native H0 qualification; no task may infer Linux support from Darwin evidence or cross-builds.
- HARD PRECONDITION: the tracked H0 provider-qualification gate JSON must pass `interactive-handoff-h0 verify-gate --require-h1 --platform darwin`, derive Darwin platform eligibility `allowed=true`, and bind Darwin P0-P15 plus P3/P4/P5/P6/P14/P15 PASS facts and qualified Darwin fence/topology. Aggregate cross-platform `h1_allowed` may remain false while Linux is `NOT_RUN`. A Markdown line is not authority. Otherwise stop before Task 1 production edits.
- `session_mode` absent preserves every legacy `tty`/`persistent` meaning and fingerprint. When `session_mode` is present, `tty` and `persistent` are forbidden.
- `session_mode="delegated_interactive"` never falls back to direct PTY or B1.0 persistent non-TTY.
- H1 begins agent-owned and has no public human-handoff action, GUI terminal auto-launch, shell integration, secret private interval, or context-exec authority.
- H1 delegated receipts are `session_lifecycle_only`: they may report lifecycle/output/authority facts but MUST NOT become ordinary mechanical verification evidence. `Reservation.EvidenceEligible()` is false for delegated mode, and an explicit ordinary evidence contract on delegated start is rejected before provider work.
- Receipt v5 predefines composable capture truth (`capture_quality=complete|partial|incomplete` + bounded monotonic `capture_reasons[]`) and input-authority provenance (`agent_only|human_write_authority_granted`). H1 emits complete/no-reason + `agent_only`; H2/H4 activate later states without a schema fork.
- Provider/session details remain private. Public authority is ShellBeam `session_id` + current `authority_epoch`, never tmux name/PID/pane guess.
- Idempotency lookup precedes current-epoch/current-owner rejection. Known old-epoch retries replay prior durable outcomes; unknown old-epoch mutations fail `stale_control_generation`.
- Every newly admitted delegated control/lifetime mutation is generation-bound. H1 must classify write, signal/kill, provider-authority mutation and provider-driven resize; no forgotten bypass lane.
- Human bytes do not exist in H1; `input_offset` remains agent-submitted-input ordering, not terminal byte history.
- Desired authority, provider-observed authority, and current generation reconcile fail-closed after restart. No PID/process-name takeover.
- Ordinary direct and B1.0 persistent paths pay zero delegated-tmux work when `session_mode` is absent.
- Host reboot continuity remains unsupported.
- No second MCP tool.

- Do not edit `dev/test-impact.toml` preemptively; if fresh `devctl` evidence demonstrates under-selection, stop, document the concrete gap, amend this plan with the exact mapping/test, then continue.

## Responsibility Map

- `internal/core/delegatedsession`: mode, provider identity, binding lifecycle, authority epoch, mutation kinds/identity/admission results, reconciliation facts.
- `internal/core/operation`: bind `session_mode` into request/execution identity and reservation schema without changing legacy encodings.
- `internal/adapter/store`: canonical delegated binding + generation-bound mutation ledger + restart candidates; private provider refs separated from public summaries.
- `internal/app/delegatedsession`: provider port, create/reattach/write/signal/inspect/close orchestration and effective-authority checks.
- `internal/adapter/delegatedtmux`: production provider selected by H0; private server/session/pane/control observer mechanics only.
- `internal/app/daemon`: route delegated start/control/recovery without contaminating direct/B1 paths.
- `api/schema`, IPC, MCP: `session_mode`, `authority_epoch`, delegated capability/result fields with legacy rejection/omission.
- `cmd/shellbeam`: provider composition, lazy capability advertisement, native acceptance.

---

### Task 1: Enforce the H0 handoff gate and freeze H1 provider identity

**Files:**
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- Read: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-provider-binding.md`

**Interfaces:**
- Consumes: Darwin H0 platform eligibility, exact Darwin tmux executable/version/hash, Darwin P0–P15 matrix, Darwin-qualified privacy/control topology, wrapper verdict; Linux remains unqualified and is not consumed by H1 implementation.
- Produces: immutable H1 implementation inputs: provider ID/version, minimum qualified tmux version, control adapter choice, exact required H0 properties.

- [ ] **Step 1: Assert H0 allows H1 before any production source edit.**

Run:

```bash
G=docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json
test -f "$G"
go run ./tools/interactive-handoff-h0 verify-gate --gate-json "$G" --require-h1 --platform darwin
shasum -a 256 "$G"
```

Expected: every command exits 0. If any fails, record the actual H0 verdict and stop H1.

- [ ] **Step 2: Extract exact qualified provider facts without re-deciding H0.**

Record only facts already proven by H0, for example:

```text
provider_id = tmux_control_mode
provider_contract_version = 1
tmux_path = <absolute H0 path>
tmux_version = <H0 version>
input_fence_mechanism = <H0 result>
observation_topology = <H0 result>
control_adapter = raw | gotmuxcc-v0.1.4
```

Do not substitute current-machine discovery for missing H0 evidence.

- [ ] **Step 3: Write the tracked H1 provider-binding evidence note.**

The note must state the exact H0 gate JSON SHA-256 + commit, exact provider/version/topology, all six genuine gates, and this hard rule:

```text
H1 code may implement only the mechanism/topology H0 qualified.
A different tmux version/topology/wrapper requires requalification, not an in-code fallback.
```

- [ ] **Step 4: Verify and commit the evidence binding.**

```bash
git diff --check
go run ./tools/devctl check
git add docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-provider-binding.md
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "docs: bind h1 interactive provider qualification"
```

---

### Task 2: Define delegated-session core mode, authority, mutation, and reconciliation contracts

**Files:**
- Create: `internal/core/delegatedsession/types.go`
- Create: `internal/core/delegatedsession/authority.go`
- Create: `internal/core/delegatedsession/mutation.go`
- Create: `internal/core/delegatedsession/reconcile.go`
- Create: `internal/core/delegatedsession/types_test.go`
- Create: `internal/core/delegatedsession/mutation_test.go`
- Create: `internal/core/delegatedsession/reconcile_test.go`
- Modify: `internal/core/failure/failure.go`

**Interfaces:**
- Produces: `ModeDelegatedInteractive`, `ProviderIdentity`, `Binding`, `AuthorityEpoch`, `Owner`, `MutationKind`, `MutationIdentity`, `AdmissionDecision`, `EffectiveAuthority`.
- Later tasks must import these types rather than reproducing string enums.

- [ ] **Step 1: Write RED validation tests for the closed core vocabulary.**

Use these exact conceptual types:

```go
const ModeDelegatedInteractive = "delegated_interactive"

type AuthorityEpoch uint64

type Owner string
const (
    OwnerNone  Owner = "none"
    OwnerAgent Owner = "agent"
    OwnerHuman Owner = "human"
)

type MutationKind string
const (
    MutationWrite             MutationKind = "write"
    MutationSignal            MutationKind = "signal"
    MutationKill              MutationKind = "kill"
    MutationResize            MutationKind = "resize"
    MutationTransfer          MutationKind = "transfer"
    MutationHumanControl      MutationKind = "human_control"
    MutationProviderAuthority MutationKind = "provider_authority"
)
```

Tests require epoch >= 1, provider ID/version non-empty/positive, exact closed owner/mutation values, and no slash/control/oversized provider IDs.

- [ ] **Step 2: Implement minimal core types and stable failure codes.**

Add failures including:

```text
stale_control_generation
session_control_not_owned
delegated_session_unavailable
delegated_provider_lost
delegated_provider_mismatch
delegated_reconcile_blocked
```

Failure details may contain public session/provider IDs and expected/current epoch numbers, never private socket/pane tokens.

- [ ] **Step 3: Write RED idempotency-before-authority tests.**

Model admission as:

```go
type MutationLookup interface {
    LookupMutation(id MutationIdentity) (MutationRecord, bool, error)
}

type MutationContext struct {
    CurrentEpoch AuthorityEpoch
    CurrentOwner Owner
}
```

Required matrix:

```text
known exact identity, old epoch      -> replay known outcome
known conflicting payload fingerprint -> operation_conflict
unknown old epoch                    -> stale_control_generation
unknown current epoch, wrong owner   -> session_control_not_owned
unknown current epoch, right owner   -> reserve/admit
```

- [ ] **Step 4: Implement a pure admission function.**

Exact semantic order:

```go
func DecideMutation(known *MutationRecord, incoming MutationIdentity, ctx MutationContext) (AdmissionDecision, error)
```

It must not perform I/O or provider calls.

- [ ] **Step 5: Write/implement pure desired-vs-observed reconciliation.**

Conceptual input:

```go
type ReconcileInput struct {
    Epoch            AuthorityEpoch
    DesiredOwner     Owner
    ObservedOwner    Owner
    ProviderIdentity ProviderIdentity
    ProviderCurrent  bool
}
```

Only an exact current provider + matching desired/observed owner grants effective agent authority in H1. Missing/mismatch => owner none/fenced.

- [ ] **Step 6: Run focused/race gates and commit.**

```bash
go test ./internal/core/delegatedsession -count=1
go test -race ./internal/core/delegatedsession -count=1
go run ./tools/devctl check
git add internal/core/delegatedsession internal/core/failure/failure.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: define delegated session authority contracts"
```

---

### Task 3: Bind `session_mode` into operation identity and durable reservation schema 5

**Files:**
- Modify: `internal/core/delegatedsession/types.go`
- Modify: `internal/core/delegatedsession/types_test.go`
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/project_command.go`
- Create: `internal/core/operation/delegated_identity.go`
- Create: `internal/core/operation/delegated_intent_test.go`
- Modify: `internal/core/operation/persistent_typed_intent_test.go`
- Modify: `internal/core/operation/evidence.go`
- Modify: `internal/core/operation/evidence_test.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/adapter/store/reservation.go`
- Create: `internal/adapter/store/delegated_reservation_test.go`

**Interfaces:**
- Consumes: `delegatedsession.ModeDelegatedInteractive`.
- Produces: request/execution fingerprint binding and `operation.Reservation{SchemaVersion:5, SessionMode, AuthorityEpoch}` for delegated starts.

- [ ] **Step 1: RED-test legacy fingerprint byte stability.**

Capture existing direct, direct-TTY, and B1 persistent fingerprint golden values before edits. Assert adding an empty `SessionMode` field changes none of them.

- [ ] **Step 2: RED-test one semantic source of truth.**

Required validation:

```text
session_mode absent + legacy tty/persistent -> existing rules
session_mode delegated_interactive + tty present/true -> invalid_input
session_mode delegated_interactive + persistent present/true -> invalid_input
session_mode delegated_interactive + session_name -> valid
unknown session_mode -> feature_unavailable/invalid closed enum before reservation
session_mode delegated_interactive + explicit evidence contract -> invalid_input before reservation/provider work
delegated reservation + test/build/format/generate/release intent -> EvidenceEligible() == false
delegated typed project-command reservation -> EvidenceEligible() == false
```

At Go struct level use an explicit `SessionMode string` plus parse/validation through `delegatedsession`, not Boolean inference. Both raw `Intent` and typed `TypedRequestIntent` carry this field so project-command admission cannot lose mode identity before resolution.

- [ ] **Step 3: Implement delegated request/execution fingerprint encoding.**

Use a new versioned encoding for delegated mode and bind at least:

```text
mode
command|argv/project command identity
cwd/workspace binding
timeout/stdin/resource/trace/evidence policy already in execution identity
session_name
```

Do not route delegated intent through `persistent_identity.go`.

- [ ] **Step 4: Add reservation schema 5 validation/round-trip and evidence-authority closure.**

Schema 5 is used only when `SessionMode=delegated_interactive`; it requires request/execution fingerprints, provider-neutral session identity, and initial `AuthorityEpoch=1`. Schemas 1–4 remain readable with existing meanings. Update `Reservation.EvidenceEligible()` so schema-5 delegated reservations are always false regardless intent/project-command metadata; explicit ordinary evidence contracts are rejected during delegated admission rather than persisted.

- [ ] **Step 5: RED-test changed mode under same operation ID conflicts.**

Cases:

```text
direct -> delegated same operation_id       conflict
delegated -> direct same operation_id       conflict
delegated same bytes/name/mode              replay
delegated changed session_name               conflict
```

- [ ] **Step 6: Run focused/race/store gates and commit.**

```bash
go test ./internal/core/operation ./internal/adapter/store -run 'Delegated|Reservation|Fingerprint' -count=1
go test -race ./internal/core/operation ./internal/adapter/store -run 'Delegated|Reservation' -count=1
go run ./tools/devctl check
git add internal/core/operation internal/adapter/store/reservation.go internal/adapter/store/delegated_reservation_test.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: persist delegated session intent"
```

---

### Task 4: Add canonical delegated binding and generation-bound mutation ledger

**Files:**
- Modify: `internal/core/delegatedsession/types.go`
- Modify: `internal/core/delegatedsession/types_test.go`
- Modify: `internal/core/delegatedsession/mutation.go`
- Modify: `internal/core/delegatedsession/mutation_test.go`
- Create: `internal/adapter/store/delegated_session_paths.go`
- Create: `internal/adapter/store/delegated_sessions.go`
- Create: `internal/adapter/store/delegated_sessions_test.go`
- Create: `internal/adapter/store/delegated_mutations.go`
- Create: `internal/adapter/store/delegated_mutations_test.go`
- Create: `internal/adapter/store/delegated_restart.go`
- Create: `internal/adapter/store/delegated_restart_test.go`
- Modify: `internal/adapter/store/repository.go`
- Modify: `internal/app/daemon/store_port.go`

**Interfaces:**
- Produces: durable public-safe binding + private provider ref, `Lookup/Reserve/CompleteDelegatedMutation`, `ListDelegatedRecoveryCandidates`.

- [ ] **Step 1: RED-test binding creation and provider-private/public split.**

Canonical public-safe fields:

```text
schema_version
session_id
operation_id
session_name?
session_mode
authority_epoch
desired_owner
provider_id/provider_version
lifecycle
created_at/updated_at
```

Provider-private fields such as tmux socket/session/window/pane/control token live under private state and never appear in public list/receipt/Event Journal projections.

- [ ] **Step 2: Implement atomic binding reserve/advance.**

Creation requires an existing schema-5 delegated reservation. Retry with exact same binding is idempotent; changed provider identity/operation/session is conflict.

- [ ] **Step 3: RED-test mutation ledger identity.**

Use:

```go
type MutationIdentity struct {
    SessionID     string
    Epoch         delegatedsession.AuthorityEpoch
    Kind          delegatedsession.MutationKind
    IdempotencyID string
    Offset        int64
    Fingerprint   string
}
```

For writes, `IdempotencyID` is empty and `Offset` is the existing agent input offset. For kill/signal/transfer/provider mutations, use a stable request/control ID and `Offset=-1` internally.

- [ ] **Step 4: Implement durable reserve-before-provider-delivery.**

Ledger states:

```text
reserved
delivered
completed
failed
outcome_unknown
```

Exact known retry replays the stored state/result; conflicting fingerprint under same logical identity fails.

- [ ] **Step 5: Add bounded retention and restart candidate tests.**

The ledger must be bounded per delegated session and retain enough completed identities to satisfy retry semantics across daemon restart. Reclamation cannot drop entries still inside the public retry horizon advertised by the daemon.

- [ ] **Step 6: Focused/race/fault tests and commit.**

```bash
go test ./internal/adapter/store -run 'Delegated' -count=1
go test -race ./internal/adapter/store -run 'Delegated' -count=1
go run ./tools/devctl check
git add internal/adapter/store internal/app/daemon/store_port.go
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: persist delegated session authority"
```

---

### Task 5: Implement the H0-qualified tmux provider behind a narrow app port

**Files:**
- Create: `internal/app/delegatedsession/ports.go`
- Create: `internal/app/delegatedsession/service.go`
- Create: `internal/app/delegatedsession/service_test.go`
- Create: `internal/adapter/delegatedtmux/provider.go`
- Create: `internal/adapter/delegatedtmux/provider_session.go`
- Create: `internal/adapter/delegatedtmux/provider_tmux.go`
- Create: `internal/adapter/delegatedtmux/provider_test.go`
- Create: `internal/adapter/delegatedtmux/control.go`
- Create: `internal/adapter/delegatedtmux/control_test.go`
- Create: `internal/adapter/delegatedtmux/private_state.go`
- Create: `internal/adapter/delegatedtmux/private_state_test.go`
- Create: `internal/adapter/delegatedtmux/socket_unix.go`
- Create: `internal/adapter/delegatedtmux/socket_unix_test.go`
- Create: `internal/adapter/delegatedtmux/process_unix.go`
- Create: `internal/adapter/delegatedtmux/process_unix_test.go`
- Modify: `go.mod` and `go.sum` **only when** the Task-1 H1 provider-binding note records `control_adapter=gotmuxcc-v0.1.4`; when it records `control_adapter=raw`, Step 6 must assert both files are unchanged from the H1 base.

**Interfaces:**
- Produces an app-owned port equivalent to:

```go
type Provider interface {
    Identity() ProviderIdentity
    ProviderRefForSession(sessionID string, createdAt time.Time) (ProviderRef, error) // pure: no probe/I/O
    Create(context.Context, CreateRequest) (CreateResult, error) // request carries the pre-reserved ProviderRef
    Reattach(context.Context, ProviderRef, OutputSink) (Observation, error)
    Write(context.Context, ProviderRef, []byte) error
    Signal(context.Context, ProviderRef, string) error
    Inspect(context.Context, ProviderRef) (Observation, error)
    Close(context.Context, ProviderRef) error
}
```

H1 `Observation.Owner` is agent or none; human attach/fence APIs are deliberately deferred to H2. `CreateRequest` and `Reattach` carry an app-owned `OutputSink`; after daemon restart a fresh observer must bind a fresh sink before it can become current, so output continuation never depends on an in-memory pre-restart callback.

- [ ] **Step 1: RED-test no provider work on non-delegated service construction/start.**

Instrument fake provider call counters and prove direct/B1 calls never invoke probe/create/attach/inspect.

- [ ] **Step 2: Implement private lazy tmux server/session creation exactly as H0 qualified.**

Requirements:

```text
private socket/config
exact qualified tmux version check
stable server/session/window/pane IDs
controlled environment established only at create boundary
model-visible control observer topology from H0
no user tmux config
no capture-pane transport
```

- [ ] **Step 3: Implement agent write/signal and live output transport.**

Provider writes only after app service supplies a current admitted mutation. Provider does not independently invent authorization.

- [ ] **Step 4: Implement exact reattach/inspect proof.**

Provider observation must prove server identity/generation/session/pane match private binding. Missing/ambiguous/mismatched state returns typed loss/mismatch, never searches by friendly name or PID.

- [ ] **Step 5: Run provider-native tests against the exact H0 tmux binary.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/adapter/delegatedtmux -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/adapter/delegatedtmux -count=1
```

Tests must re-prove the H1 subset of P0/P1/P4/P5/P11/P12 used by production code rather than trusting fixture mocks only.

- [ ] **Step 6: Commit provider/app port.**

```bash
go run ./tools/devctl check
git add internal/app/delegatedsession internal/adapter/delegatedtmux go.mod go.sum
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: add qualified delegated tmux provider"
```

---

### Task 6: Route delegated start/output/write/kill through daemon with epoch admission

**Files:**
- Create: `internal/app/daemon/delegated_port.go`
- Create: `internal/app/daemon/delegated_start.go`
- Create: `internal/app/daemon/delegated_start_test.go`
- Create: `internal/app/daemon/delegated_control.go`
- Create: `internal/app/daemon/delegated_control_test.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/actions.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/types.go`
- Create: `internal/adapter/store/delegated_output.go`
- Create: `internal/adapter/store/delegated_output_test.go`

**Interfaces:**
- `StartRequest.SessionMode` selects delegated service.
- `WriteRequest.AuthorityEpoch` and `KillRequest.AuthorityEpoch` are required only for delegated sessions.
- Start/poll/control views return current `AuthorityEpoch` for delegated sessions.

- [ ] **Step 1: RED-test admission and no fallback.**

Cases:

```text
H1 capability absent + delegated start -> feature_unavailable before reservation/provider work
delegated + tty/persistent legacy field -> invalid_input
delegated + explicit ordinary evidence contract -> invalid_input before reservation/provider work
unknown session_mode -> feature_unavailable before reservation/provider work
direct/B1 -> exact existing route
```

- [ ] **Step 2: Implement delegated start ordering.**

Exact order:

```text
validate negotiated mode
freeze operation/session reservation schema 5
derive deterministic provider ref from the frozen session identity (pure; no provider I/O)
reserve delegated binding epoch=1 desired_owner=agent plus that provider ref
create provider session using the exact pre-reserved provider ref
prove provider current/agent authority
mark canonical session running
publish start view epoch=1
```

Ambiguity after provider create never creates a second provider session on retry.

- [ ] **Step 3: RED-test idempotency-before-authority on daemon write/kill.**

Include response-loss retry where a write/kill accepted at epoch 1 is retried after simulated epoch 2: exact known retry replays; unseen epoch-1 mutation is stale.

- [ ] **Step 4: Implement reserve -> provider delivery -> mutation completion.**

On provider ambiguity after durable reserve, persist `outcome_unknown`; retry inspects/reconciles instead of blindly redelivering.

- [ ] **Step 5: Integrate canonical output and terminal truth.**

H1 output is public/complete unless transport/provider loss occurs. Do not introduce private-interval semantics yet. Terminal receipt records literal provider/child facts ShellBeam can prove; provider loss does not fabricate child exit code.

- [ ] **Step 6: Run focused/race/no-tax gates and commit.**

```bash
go test ./internal/app/daemon ./internal/app/delegatedsession -run 'Delegated' -count=1
go test -race ./internal/app/daemon ./internal/app/delegatedsession -run 'Delegated' -count=1
go test ./internal/app/daemon -run 'Direct|Persistent' -count=1
go run ./tools/devctl check
git add internal/app/daemon internal/app/delegatedsession internal/adapter/store
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: route delegated interactive sessions"
```

---

### Task 7: Add delegated receipt v5 plus modern schema/IPC/MCP/capability support with legacy closure

**Files:**
- Create: `internal/core/receipt/capture_quality.go`
- Create: `internal/core/receipt/capture_quality_test.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `internal/core/receipt/result.go`
- Create: `internal/core/receipt/delegated_v5_test.go`
- Create: `internal/app/daemon/delegated_evidence_test.go`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/delegated_sessions_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_fields.go`
- Modify: `internal/adapter/ipc/response_v2.go`
- Create: `internal/adapter/ipc/delegated_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/input_fields.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/request.go`
- Modify: `internal/adapter/mcp/server.go`
- Create: `internal/adapter/mcp/delegated_test.go`
- Modify: `internal/core/capability/catalog.go`
- Create: `internal/core/capability/delegated_test.go`
- Create: `cmd/shellbeam/delegated_sessions.go`
- Create: `cmd/shellbeam/delegated_sessions_test.go`

**Interfaces:**
- Receipt schema v5 is the delegated modern receipt. It binds `session_mode`, terminal `authority_epoch`, `evidence_authority=session_lifecycle_only`, monotonic `input_authority_provenance`, and composable capture truth while leaving receipt schemas 1–4 unchanged.
- Closed capture vocabulary is frozen now:

```text
capture_quality = complete | partial | incomplete
capture_reasons = [] | [private_intervals_omitted] | any unique bounded set containing transport_gap or provider_lost (optionally plus private_intervals_omitted)
input_authority_provenance = agent_only | human_write_authority_granted
evidence_authority = session_lifecycle_only
```

- H1 emits only `output_complete=true + capture_quality=complete + capture_reasons=[] + input_authority_provenance=agent_only`. H2/H4 may monotonically add later provenance/reasons without receipt-version fork.
- New capability: `delegated_interactive` with provider/version, daemon-restart continuity, host-reboot false, current H1 limits.
- Modern start supports `session_mode`; modern delegated write/kill requires `authority_epoch`.
- Modern result includes delegated authority/capture/provenance fields only for delegated sessions.

- [ ] **Step 1: RED receipt-v5 validation/result tests, including simultaneous causes.**

Receipt v5 requires `session_mode=delegated_interactive`, positive authority epoch, request/execution fingerprints, `evidence_authority=session_lifecycle_only`, valid provenance, and valid composable capture state. Test at least:

```text
complete + [] + output_complete=true -> valid
partial + [private_intervals_omitted] + output_complete=false -> valid
incomplete + [transport_gap] -> valid
incomplete + [provider_lost] -> valid
incomplete + [private_intervals_omitted, transport_gap, provider_lost] -> valid
complete + any reason -> invalid
partial + transport/provider reason -> invalid
incomplete without transport_gap/provider_lost -> invalid
duplicate/unknown/unsorted reasons -> invalid or canonicalized before persistence, never ambiguous
```

`receipt.Result.Output` gains `capture_quality` + `capture_reasons`; result/receipt project evidence authority and input-authority provenance. Direct/B1 results remain field-compatible under existing schemas.

- [ ] **Step 2: RED closed-schema tests.**

Assert:

```text
v2 delegated start accepted
v2 delegated + tty/persistent rejected
unknown session_mode rejected
legacy v1 delegated field rejected
ordinary v2 start unchanged
delegated start + explicit ordinary evidence contract rejected
delegated lifecycle receipt always projects evidence_authority=session_lifecycle_only
write/kill authority_epoch optional for legacy session syntax but schema-bounded positive integer when present
```

Runtime later enforces epoch required for an actual delegated target.

- [ ] **Step 3: Add capability projection only when production provider composition is qualified/current.**

Capability discovery must be side-effect free: it may use startup qualification state/configured provider identity, but `inspect.server` must not start tmux.

- [ ] **Step 4: Add IPC/MCP lossless mapping and failure projection.**

Do not expose private provider refs/socket/pane IDs in result or errors.

- [ ] **Step 5: One-tool and legacy closure tests.**

Add daemon/evidence-worker RED tests proving a delegated schema-5 reservation with test/build intent produces **no ordinary evidence record**, even when its lifecycle receipt is terminal success with complete capture. This is independent from capture completeness.

Ensure one MCP tool registration remains; v1 output omits all H1 fields/features.

- [ ] **Step 6: Run receipt/schema/adapter/capability tests and commit.**

```bash
go test ./internal/core/receipt ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./internal/core/capability -run 'Delegated|Capture|Evidence|Persistent|MCP' -count=1
go test -race ./internal/adapter/ipc ./internal/adapter/mcp -run 'Delegated' -count=1
go run ./tools/devctl check
git add internal/core/receipt internal/app/daemon/delegated_evidence_test.go api/schema internal/adapter/ipc internal/adapter/mcp internal/core/capability cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: expose delegated interactive core"
```

---

### Task 8: Reconcile delegated sessions across daemon restart and provider loss

**Files:**
- Create: `internal/app/delegatedsession/reattach.go`
- Create: `internal/app/delegatedsession/reattach_test.go`
- Create: `internal/app/daemon/delegated_reconcile.go`
- Create: `internal/app/daemon/delegated_reconcile_test.go`
- Modify: `internal/app/daemon/persistent_startup.go` or the shared startup reconciliation coordinator without weakening B1 behavior.
- Modify: `internal/app/daemon/shutdown.go`
- Create: `cmd/shellbeam/delegated_runtime_acceptance_test.go`

**Interfaces:**
- Startup reconciliation consumes canonical delegated recovery candidates and provider observations; yields exact current agent authority or fenced/lost state.

- [ ] **Step 1: RED restart matrix.**

Cases:

```text
exact provider/session alive + epoch match -> reattach same session/epoch
provider absent                       -> provider_lost; no recreate
provider generation mismatch          -> reconcile_blocked
private ref corrupt                   -> ambiguous/fenced
child/session terminal while daemon absent -> publish only provable terminal facts
```

- [ ] **Step 2: Implement bounded startup reconciliation.**

Reuse existing startup concurrency/budget patterns; do not introduce one polling goroutine per delegated session.

- [ ] **Step 3: Graceful daemon shutdown detaches control ownership but leaves tmux-owned delegated session alive.**

Direct and B1 shutdown behavior must remain byte-for-byte semantically unchanged.

- [ ] **Step 4: Native hard-kill daemon acceptance.**

Build real binary, start delegated shell, write a marker, hard-kill daemon only, restart, prove same ShellBeam session/provider identity/epoch and continue input. Then kill provider and prove fail-closed/no guessed continuation.

- [ ] **Step 5: Repeat/race gates and commit.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./cmd/shellbeam -run 'DelegatedRuntime' -count=3
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/app/delegatedsession ./internal/app/daemon -run 'Delegated.*Reconcile' -count=1
go run ./tools/devctl check
git add internal/app/delegatedsession internal/app/daemon cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: recover delegated interactive sessions"
```

---

### Task 9: H1 native acceptance, anti-goal audit, and exact checkpoint

**Files:**
- Create: `tests/integration/delegated_session_test.go`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md`

**Interfaces:**
- Produces H1 PASS/FAIL and the exact commit/fingerprint required by H2.

- [ ] **Step 1: Native H1 end-to-end matrix.**

Prove on required platforms:

```text
delegated start -> agent write -> output -> clean terminal
lost start response -> retry -> exactly one tmux session/shell
known old-epoch retry replays; unseen stale epoch rejected
provider loss -> no PID/name takeover
hard daemon restart -> exact reattach
ordinary direct/B1 path -> zero delegated provider calls
```

- [ ] **Step 2: Mutation-taxonomy audit.**

Search every delegated provider mutation callsite and map it in the evidence report to `write`, `signal/kill`, `resize`, `transfer`, `human_control`, or `provider_authority`. H1 may leave H2-only kinds unavailable, but no callsite may bypass a stated policy.

- [ ] **Step 3: Anti-goal scans.**

```bash
rg -n 'handoff\.request|TerminalLauncher|ShellIntegration|PrivacyReleaseProof' internal api cmd || true
rg -n 'capture-pane|preferred_terminal|reptyr' internal/adapter/delegatedtmux || true
git diff c3fc3d57dfbb5707e1b521e6acaaf79b33300bea --stat
```

Expected: no H2/H3/H4 public feature implemented and no arbitrary takeover/polling transport.

- [ ] **Step 4: Fresh verification.**

```bash
go mod verify
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./internal/core/delegatedsession ./internal/app/delegatedsession ./internal/adapter/delegatedtmux ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./internal/core/delegatedsession ./internal/app/delegatedsession ./internal/adapter/delegatedtmux ./internal/app/daemon -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base 887c4b7240024bace5ce144624bc458f4b7742cd --json
git diff --check
```

- [ ] **Step 5: Write evidence, stage exact H1 scope, commit-gate, and checkpoint.**

Evidence records H0 machine-gate digest, exact provider/tmux identity, H1 capability/version, reservation schema 5, receipt schema 5 composable capture/provenance/evidence-authority contract, proof that delegated terminal success never promotes to ordinary evidence, authority semantics, native lanes, no-tax evidence, and `H2_ALLOWED=true|false`.

```bash
git add \
  internal/core/delegatedsession internal/core/operation internal/core/receipt internal/core/capability internal/core/failure/failure.go \
  internal/app/delegatedsession internal/app/daemon \
  internal/adapter/delegatedtmux internal/adapter/store internal/adapter/ipc internal/adapter/mcp \
  api/schema cmd/shellbeam tests/integration/delegated_session_test.go \
  docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-provider-binding.md \
  docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md
if rg -q '^control_adapter[[:space:]]*=[[:space:]]*gotmuxcc-v0\.1\.4$' docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-provider-binding.md; then
  git add go.mod go.sum
else
  git diff --exit-code HEAD -- go.mod go.sum
fi
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "test: verify delegated interactive core"
```

- [ ] **Step 6: Postcommit proof.**

Require clean tree and fresh `go run ./tools/devctl check`; record exact HEAD and source fingerprint in the tracked H1 evidence. Do not begin H2 unless `H2_ALLOWED=true`.

---

## H1 Completion Gate

H1 is complete only when one exact committed tree proves:

1. H0 required provider gates are PASS and exact qualified mechanism is bound;
2. delegated mode has one explicit `session_mode` source of truth and old semantics/fingerprints remain unchanged when absent;
3. schema-5 reservation freezes delegated identity before provider creation;
4. retry/lost response creates exactly one provider session/shell;
5. authority epoch starts durably and all admitted delegated mutations obey idempotency-before-authority;
6. known old-generation retries replay while unseen stale mutations cannot execute;
7. provider binding/ref is exact and provider-private facts never become public authority;
8. agent write/signal/output work through H0-qualified Control Mode without `capture-pane` transport;
9. restart reconciliation is desired-state + fresh exact provider observation, fail-closed on mismatch;
10. provider loss never becomes PID/name takeover or fabricated child exit;
11. ordinary direct and B1 persistent execution do zero delegated-tmux work;
12. modern v2 capability/schema expose H1 only; legacy remains closed;
13. delegated receipt v5 uses composable `capture_quality + capture_reasons`, lifecycle-only evidence authority, and `agent_only` input-authority provenance on healthy H1 output; simultaneous later omission/failure causes are representable without schema fork;
14. delegated reservations are never ordinary `EvidenceEligible`; explicit ordinary evidence contracts fail before provider work and terminal success cannot create a mechanical evidence record;
15. H2/H3/H4/H5 features remain unavailable;
16. required native/race/schema/devctl/commit-gate evidence passes and `H2_ALLOWED=true` is recorded.
