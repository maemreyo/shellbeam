# Interactive Handoff H5 High-Assurance Context Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close and implement a separately reviewed evidence contract for receipt-producing execution inside the exported process context of a delegated interactive shell, so post-handoff verification commands can regain strong child/output/exit evidence without transmitting credentials through the model; then provide a disciplined promotion path for broader shell/terminal/session providers.

**Architecture:** H5 does **not** treat interactive transcript markers as ordinary receipts. The architecture is a short-lived private re-exec mode of the installed ShellBeam binary armed by a qualified shell adapter as a one-shot prompt hook; the hook emits the exact prompt-boundary notification and launches only the fixed helper invocation in the same shell hook, so there is no proof→later-write race. Because that helper is intentionally the workload bridge, it may inherit the delegated shell's exported environment and cwd. `context_exec_id` is correlation only, never a bearer capability. The helper must be the exact foreground ShellBeam process for the delegated pane, then proves a one-generation private claim, reports inherited cwd, receives exact durably reserved argv, prepares the executable object without starting it, and waits for a daemon ACK after exact v6 child reservation. Only then may it spawn/reap the child and stream a **separate child output channel** (not the mixed delegated pane). The daemon alone canonicalizes/promotes the result under the human-approved evidence contract and current delegated authority generation.

**Tech Stack:** verified H4/H2/H1 delegated handoff stack; same-binary private helper pattern; existing operation/receipt/output/evidence/structured/telemetry pipelines; Unix local IPC/authentication; exact argv process owner; fish/zsh/bash safe-boundary adapters; Go 1.26.6.

**Spec:** Master: `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`. **HARD DESIGN GATE:** Task 1 must produce and obtain review approval for `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md`; Tasks 2+ are unauthorized until that exact spec is approved. If review changes the candidate contract or wire names, amend this plan before implementation.

## Global Constraints

- Current approved execution scope is **Darwin/macOS only**. Linux remains intended but unadvertised and fail-closed until native H0 qualification; no task may infer Linux support from Darwin evidence or cross-builds.
- H5 context-exec is not required for first experimental handoff, but no stable/high-assurance claim may use interactive transcript as a substitute for receipt-producing child evidence.
- HARD PRECONDITION for Task 1: H4 evidence reports `H5_DESIGN_ALLOWED=true`. HARD PRECONDITION for Tasks 2+: separately reviewed context-exec evidence spec is approved with no unresolved blocker.
- Gate authority is intentionally different from H0: H0 `h1_allowed` is machine-derived provider qualification; H5 implementation approval is a **human design approval** bound to an exact spec digest. The implementing agent may verify that approval artifact but must not author/self-set it.
- Initial high-assurance context means **exported process environment + current shell cwd at a qualified safe boundary**. It does not claim transfer of shell-local variables, aliases, functions, job-control state, history, or arbitrary TUI state.
- Initial public execution form is exact `argv` only. Shell-expression behavior requires an explicit argv such as `/bin/sh -lc ...`; H5 does not secretly reinterpret argv through the interactive shell.
- Initial high-assurance child is non-interactive: non-TTY, stdin closed, own process group, helper-owned stdout/stderr pipes. If the child itself requires interactive human input, context-exec fails/degrades and the normal handoff flow is used; pane-mixed output cannot satisfy high-assurance evidence.
- The actual command/argv is never interpolated into the interactive shell launch snippet. The shell launches only the installed private helper with a bounded public correlation ID; helper authentication/claim authority is separate and MUST NOT be derivable from that visible ID.
- The private context helper is an intentional workload bridge and may inherit the delegated shell's exported environment. This is an explicit exception to H4's rule for infrastructure notifiers/helpers; readiness/terminal helpers remain minimally allowlisted and must not gain this authority.
- The daemon never receives or persists inherited environment values/hashes merely because context-exec is used. Environment continuity is provenance, not captured data.
- ShellBeam-internal helper authority, IPC metadata, claim material, and inherited control FDs are helper-private and are stripped/closed before child exec. Workload environment is the user exported context minus exact ShellBeam-internal control material.
- H5 does **not** claim that the workload/model cannot deliberately reveal a secret available in its delegated environment. The guarantee is narrower: ShellBeam does not serialize inherited secret environment values as control metadata merely to execute in that context.
- The receipt must capture requested `argv[0]` and the executable actually resolved/launched under the delegated PATH as an absolute executable identity. Any stronger content/digest claim must match what the approved design can mechanically bind to exec without a TOCTOU overclaim.
- Context-exec requires current agent ownership/epoch and a qualified transfer boundary. For model-visible output/evidence it also requires public capture/privacy-release state; private/ambiguous capture cannot be silently upgraded.
- Reserve exact context-exec identity before shell/helper launch. Lost responses/retries create at most one helper/child execution for the logical request.
- Helper authentication binds a claim identity distinct from public `context_exec_id`, delegated session ID, authority epoch, helper generation, and exact reserved request fingerprint. Same-user peer/executable/ancestry facts may participate only as explicitly approved; `context_exec_id` and PID/parent alone are never bearer authority.
- **2026-08-21 approved integration amendment:** helper peer admission additionally requires the helper to be the direct child of the exact pane shell and to own the pane foreground process group; ancestry alone is insufficient. The provider durably freezes a non-secret pre-launch context expectation (provider generation, shell identity, current pane cwd, public privacy) and the authenticated prompt-launched helper must report the same `getwd()` before final `ContextBinding` is committed or argv is delivered.
- **2026-08-21 approved integration amendment:** executable preparation and child spawn are split. The helper resolves/opens the executable object first, the daemon durably reserves the exact v6 child identity and records `child_reserved`, then an execute ACK authorizes spawn. `child_spawned` is recorded only from explicit helper spawn truth; the helper may report `child_terminal` but only the daemon may set `canonicalized` and `context_exec_child_owned_v1`.
- Child spawn/output/exit/signal/timeout facts remain literal; provider/helper loss may make evidence incomplete/ambiguous but cannot fabricate exit status.
- Existing Resource Enforcement/Hermetic semantics do not automatically transfer into context-exec. Advertise/apply them only after separately proven composition; otherwise capability says unavailable for that context execution.
- No second MCP tool, background workflow engine, shell command scheduler, arbitrary existing PTY takeover, or remote execution.
- Broader providers are capability promotions after native qualification; they cannot weaken H0–H4 semantics.
- H5 context-exec core does not depend on H3. Task 9 terminal-provider promotion runs only when tracked H3 evidence exists and proves the terminal-presentation contracts; otherwise that sublane is `NOT_RUN` while context-exec/Nushell/session-provider work is judged independently.

- Do not edit `dev/test-impact.toml` preemptively; if fresh `devctl` evidence demonstrates under-selection, stop, document the concrete gap, amend this plan with the exact mapping/test, then continue.

## Responsibility Map

- `docs/superpowers/specs/...context-exec...`: separate evidence authority contract; Task 1 only until approved.
- `internal/core/contextexec`: context binding, request/identity, helper/child lifecycle, evidence quality and public result contracts.
- `internal/core/operation`: optional context binding in modern reservation/fingerprint while preserving legacy encodings.
- `internal/adapter/store`: context-exec reservation/helper binding/retry state, no environment values.
- `internal/app/contextexec`: admission/orchestration/recovery/promotion policy.
- `internal/adapter/contextexec`: private authenticated daemon↔helper protocol and short-lived helper runtime.
- `internal/adapter/delegatedtmux` + delegated/shell process facts: exact pane tty/current cwd facts used only as bounded non-secret admission evidence; no environment values.
- `internal/app/shellintegration` + `internal/adapter/shellintegration`: fixed opaque helper launch at qualified safe boundary; no command payload in shell text.
- `cmd/shellbeam`: private `__context_exec` re-exec mode and composition, absent from normal public help.
- IPC/MCP/schema: one-tool `context.exec` action only after evidence contract approval.
- evidence/receipt/output layers: normal authority only when helper/child proof meets approved contract.

---

### Task 1: Draft, attack, approve, and freeze the separate context-exec evidence contract

**Files:**
- Create: `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md`
- Create: `docs/superpowers/evidence/2026-08-18-context-exec-design-review.md`
- Receive from human reviewer (do not self-author): `docs/superpowers/evidence/2026-08-18-context-exec-design-approval.json`
- Read: H4 evidence and master Sections 29/40/57/62.

**Interfaces:**
- Produces the only authority that may unlock Tasks 2+ and exact wire/type names consumed by this plan.

- [ ] **Step 1: Assert H5 design gate from H4.**

```bash
E=docs/superpowers/evidence/2026-08-18-interactive-handoff-h4-secret-privacy.md
test -f "$E"
rg -n '^H5_DESIGN_ALLOWED[[:space:]]*=[[:space:]]*true$' "$E"
```

- [ ] **Step 2: Write the evidence contract with these mandatory candidate boundaries.**

The draft must explicitly decide/review:

```text
public action name: context.exec
request identity: context_exec_id
exact target: delegated session_id + authority_epoch
execution payload: argv only in V1
context authority: exported process env + current cwd only
safe-boundary producer requirements
helper launch: fixed installed ShellBeam private argv + opaque ID only
helper auth/generation protocol
reserve-before-helper-launch ordering
exactly-once helper/child behavior
output attribution: helper-owned child pipes/PTY distinct from mixed delegated pane; optional terminal mirror is presentation-only
child process/job-control ownership: non-TTY/closed stdin V1, process group, signal/timeout/reap ownership
exactly-once/retry/epoch binding including transfer-vs-in-flight-child policy
helper authentication: public context_exec_id != claim/bearer authority
internal capability/environment stripping before child exec
actual executable identity resolved/launched under delegated PATH
stdout/stderr canonicalization and output bounds
helper/daemon crash matrix
privacy-release requirement for model-visible output
receipt schema/context provenance and explicit evidence-authority class
which existing evidence consumers may treat result as authoritative
resource-enforcement/hermetic non-inheritance
secret/privacy metadata prohibitions and explicit non-claim that workload cannot reveal its own environment
```

The six first-class review questions are therefore: output attribution, child/job-control ownership, exactly-once+epoch semantics, helper authentication identity, control-environment stripping, and actual executable identity. None may be deferred to Tasks 2+.

- [ ] **Step 3: Attack the design with counterexamples.**

At minimum:

```text
stale epoch helper starts after a new handoff
same context_exec_id helper launches twice after lost response
shell cwd changes between reservation and helper launch
secret env exists but daemon/helper logs it accidentally
private capture still active
helper authenticates but child never spawns
child exits while daemon disconnected
helper dies after child spawn
agent kills while authority transfers to human
nested shell changes after request
argv executable not found
background shell/job/prompt writes to delegated pane while context child runs
context_exec_id appears in pane transcript and a second same-user process tries to claim it
helper-only token/control FD appears in child environment or fd table
PATH resolves a different executable than daemon/user expected
resolved executable path changes between identity capture and exec
```

- [ ] **Step 4: Review evidence-authority claim against current receipt/evidence contracts.**

The design must state exactly why authenticated helper-owned spawn/reap **plus separately owned child output pipes** is stronger than shell transcript markers, and which evidence facts remain weaker than an ordinary daemon-spawned direct command. A design where child stdout/stderr merely inherits the delegated pane is NOT approvable as strong output evidence. No statement may call inherited environment "captured" or "verified values".

The design must also decide transfer concurrency: the conservative V1 candidate is that a new human handoff cannot complete while a context-exec child is active unless that child is first terminal/cancelled under its own authority; no ownership epoch silently reclassifies an ambiguous in-flight child.

- [ ] **Step 5: Obtain explicit review verdict and freeze exact spec commit/hash.**

The implementing agent writes the spec + technical review report, then **hard-stops**. A human reviewer supplies the separate tracked approval artifact; the agent must not create/edit its approval fields. Required closed shape:

```json
{
  "schema_version": 1,
  "approval_kind": "human_design_review",
  "spec_path": "docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md",
  "spec_sha256": "<exact 64-hex digest>",
  "verdict": "APPROVED",
  "context_exec_implementation_allowed": true
}
```

A false/missing/mismatched artifact blocks Tasks 2+. Markdown tests, an agent-authored `true`, or an unbound approval for another spec revision have no authority.

- [ ] **Step 6: Verify/commit design artifacts and hard stop if not approved.**

```bash
git diff --check
go run ./tools/devctl check
git add docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md docs/superpowers/evidence/2026-08-18-context-exec-design-review.md
# Do NOT add/create the human approval artifact unless it was supplied by the human reviewer.
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "docs: design delegated context execution evidence"
```

Tasks 2+ begin only after verifying the externally supplied human approval JSON exists, has `approval_kind=human_design_review`, its `spec_sha256` equals the exact committed spec bytes, verdict is `APPROVED`, and `context_exec_implementation_allowed=true`. If review changes any candidate name/semantics below, amend this plan first and obtain a new approval bound to the new digest.

---

### Task 2: Define context-exec core request, binding, lifecycle, and evidence-quality contracts

**Files:**
- Create: `internal/core/contextexec/types.go`
- Create: `internal/core/contextexec/identity.go`
- Create: `internal/core/contextexec/lifecycle.go`
- Create: `internal/core/contextexec/evidence.go`
- Create: `internal/core/contextexec/types_test.go`
- Create: `internal/core/contextexec/identity_test.go`
- Create: `internal/core/contextexec/evidence_test.go`
- Modify: `internal/core/failure/failure.go`

**Interfaces:**
- Produces `Request`, `ContextBinding`, `HelperBinding`, `Lifecycle`, `EvidenceQuality`, `Result` matching the approved Task-1 spec.
- Core contracts must keep public correlation identity separate from helper claim authority, represent helper-owned child-output attribution explicitly, and carry requested-vs-actual executable identity without persisting bearer material.

- [ ] **Step 1: RED-test exact request shape.**

Candidate approved shape unless Task-1 review amended it:

```go
type Request struct {
    ContextExecID  string
    SessionID      string
    AuthorityEpoch delegatedsession.AuthorityEpoch
    Argv           []string
    TimeoutMS      int64
    MaxOutputBytes int64
}
```

Require non-empty exact argv[0], bounded args/bytes, no command+argv duality, no cwd/env-value fields.

- [ ] **Step 2: Define context provenance without secret values.**

```go
type ContextBinding struct {
    SessionID      string
    AuthorityEpoch delegatedsession.AuthorityEpoch
    ShellIdentity  string
    BoundaryQuality string
    CWDObserved    string
    PrivacyState   string
}
```

Whether cwd is public-safe/normalized must follow approved design/workspace rules; no environment dump/fingerprint containing secret values.

- [ ] **Step 3: Define helper/child lifecycle.**

```text
reserved
helper_requested
helper_authenticated
child_spawned
child_terminal
canonicalized
helper_lost
ambiguous
```

Public result distinguishes spawn evidence, exit evidence, output completeness, timeout/signal evidence, and context evidence quality. The approved contract must expose enough typed state to prove:

```text
public correlation id != helper claim authority
output attribution = helper-owned child pipes (or separately approved equivalent), never mixed pane bytes
requested executable identity != mechanically observed actual executable identity
helper control material is not workload context
```

Do not store raw bearer/claim secret material in `HelperBinding`; persist only the approved non-secret binding/reference/digest fields needed for replay and audit.

- [ ] **Step 4: Add stable failures.**

At least:

```text
context_exec_unavailable
context_exec_stale_generation
context_exec_not_agent_owned
context_exec_privacy_blocked
context_exec_boundary_unproven
context_helper_auth_failed
context_helper_lost
context_exec_ambiguous
```

- [ ] **Step 5: Focused/race/commit.**

Run core tests/race/devctl/commit-gate; commit `feat: define delegated context execution contracts`.

---

### Task 3: Bind context-exec to durable operation identity and exactly-once store state

**Files:**
- Modify: `internal/core/operation/persistence.go`
- Create: `internal/core/operation/context_binding.go`
- Create: `internal/core/operation/context_binding_test.go`
- Create: `internal/adapter/store/context_exec.go`
- Create: `internal/adapter/store/context_exec_test.go`
- Create: `internal/adapter/store/context_exec_restart.go`
- Create: `internal/adapter/store/context_exec_restart_test.go`
- Modify: reservation validation/schema only as approved by Task 1.

**Interfaces:**
- Produces durable `ReserveContextExec`, `BindHelperGeneration`, `AdvanceContextExec`, `LookupContextExec`, recovery candidates.

- [ ] **Step 1: RED exactly-once identity.**

Same `context_exec_id` + exact session/epoch/argv/timeout/output bounds replays; any changed field conflicts. Reservation occurs before the shell receives helper-launch request.

- [ ] **Step 2: Bind context execution into modern operation request/execution identity.**

Legacy/direct/delegated-start fingerprints remain unchanged. Context child gets its own operation/session execution identity linked to parent delegated `session_id + authority_epoch` through explicit context binding.

- [ ] **Step 3: Persist helper generation/capability state privately.**

High-entropy auth capability never argv/public env/public error. State associates one approved helper generation with one context-exec reservation.

- [ ] **Step 4: Fault matrix.**

```text
reserve -> daemon response lost
reserve -> shell launch signal lost
helper starts -> auth response lost
helper authenticated -> child spawn unknown
child terminal -> canonical ack lost
```

Retry never launches a second child unless approved design explicitly proves original never spawned.

- [ ] **Step 5: Focused/race/store commit.**

Run store/operation tests + devctl/commit-gate; commit `feat: persist context execution identity`.

---

### Task 4: Implement private authenticated short-lived context helper runtime

**Files:**
- Create: `internal/adapter/contextexec/protocol.go`
- Create: `internal/adapter/contextexec/protocol_test.go`
- Create: `internal/adapter/contextexec/auth.go`
- Create: `internal/adapter/contextexec/auth_test.go`
- Create: `internal/adapter/contextexec/client.go`
- Create: `internal/adapter/contextexec/server.go`
- Create: `internal/adapter/contextexec/runtime.go`
- Create: `internal/adapter/contextexec/runtime_test.go`
- Create: `internal/adapter/contextexec/output.go`
- Create: `internal/adapter/contextexec/output_test.go`
- Create: `cmd/shellbeam/command_context_exec.go`
- Create: `cmd/shellbeam/command_context_exec_test.go`
- Modify: private command dispatch in `cmd/shellbeam/command.go`.

**Interfaces:**
- Private helper mode conceptually: `shellbeam __context_exec --context-exec-id <opaque-id>`; exact spelling must match Task-1 approved spec and is absent from normal help.

- [ ] **Step 1: RED protocol/auth tests proving correlation is not authority.**

`context_exec_id` is public correlation only. Presenting the exact visible ID without the separately approved helper-claim proof MUST fail before request fetch/spawn. Also reject wrong/stale/replayed claim generation, malformed version/kind/fields, unsafe peer, and a valid claim bound to a different session/epoch/request fingerprint. PID/parent/same-user alone cannot turn a correlation ID into bearer authority.

- [ ] **Step 2: Ensure request and claim material stay out of presentation argv.**

Helper command line contains only installed binary/private subcommand/public correlation ID. Exact child argv/timeout/output bounds are fetched only after the approved non-bearer claim succeeds. Claim/control capability material MUST travel by the Task-1-approved private mechanism and must not be printable pane text or child argv/environment.

- [ ] **Step 3: Split user context from ShellBeam control context before child exec.**

The helper intentionally inherits the delegated shell's exported user environment + current working directory. Before spawning the workload it constructs the child environment as user context **minus** all ShellBeam-internal control variables/claim material/private IPC metadata, closes helper-only control/listener FDs in the child, and proves those sentinels are absent from `/proc`/child env where the platform permits inspection. It never serializes inherited user env values back to daemon/logs.

- [ ] **Step 4: Resolve actual executable and own a separate child output channel.**

Resolve/launch `argv[0]` under the inherited delegated `PATH` using the exact mechanism approved by Task 1 and mechanically bind the actual absolute executable identity that was launched; do not report only the requested token. V1 child is non-TTY with stdin closed and its own process group. Helper owns dedicated stdout/stderr pipes (or the exact separately approved equivalent), applies bounded canonicalization, signals/timeouts/reaps the child, and sends terminal evidence bound to claim/helper generation. Any optional bytes mirrored to the delegated tmux pane are presentation-only and are NEVER the authoritative output source.

- [ ] **Step 5: Secret/control/output-attribution sentinel tests.**

Launch helper from a test shell with `H5_SECRET=<canary>` exported plus distinct ShellBeam-control env/FD sentinels. The workload may intentionally use the user secret, but must not receive ShellBeam control material. Run concurrent noisy shell/background output in the delegated pane and prove authoritative context-exec output contains only the helper-owned child stream. Daemon logs/protocol metadata/state must not contain secret value/hash/length or claim material.

- [ ] **Step 6: Native/race/commit.**

Run helper tests with real child processes, race, devctl, commit-gate; commit `feat: execute child through delegated context helper`.

---

### Task 5: Launch the fixed helper from qualified shell safe boundaries without command injection

**Files:**
- Modify: `internal/app/shellintegration/ports.go`
- Create: `internal/app/shellintegration/context_exec.go`
- Create: `internal/app/shellintegration/context_exec_test.go`
- Create: `internal/adapter/shellintegration/context_exec.go`
- Create: `internal/adapter/shellintegration/context_exec_test.go`
- Modify fish/zsh/bash adapter files with separately tested fixed-helper launch primitives.

**Interfaces:**
- `LaunchContextHelper(session, contextExecID, epoch)` may emit only a shell-family-safe fixed helper invocation; child command payload never appears in that shell text.

- [ ] **Step 1: RED safe-boundary requirement.**

No helper launch while foreground child/TUI owns terminal or shell identity is ambiguous. Current agent authority + current shell prompt boundary required.

- [ ] **Step 2: RED quoting/path tests.**

Installed ShellBeam executable paths containing spaces/quotes and opaque safe IDs must launch correctly in fish/zsh/bash without allowing ID/path text to become arbitrary command injection. Reject unsafe/unrepresentable path rather than fallback to guessed syntax.

- [ ] **Step 3: Implement fixed launch only.**

The adapter launches the private helper; it never receives `argv` of the target command. It may intentionally let the helper inherit exported environment/cwd, per approved evidence contract.

- [ ] **Step 4: Nested-shell drift.**

Reprobe exact current shell immediately before launch. Changed/unknown shell => context-exec unavailable; no stale zsh snippet into fish, etc.

- [ ] **Step 5: Real fish/zsh/bash helper inheritance tests and commit.**

Prove child sees current cwd and an exported sentinel through helper, but unexported shell-local variable/function is **not claimed** as transferred process context. Commit `feat: launch context helper at safe shell boundary`.

---

### Task 5A: Harden prompt-bound helper claim and add pre-spawn durable authorization

> **Approved amendment, 2026-08-21.** Tasks 4 and 5 were already implemented and committed before integration review exposed three cross-task gaps: ancestry-only peer admission could be raced by a same-user descendant, current cwd had no authoritative daemon-side freeze/check pair, and the helper learned the resolved executable only after spawn although v6 child identity must be durable first. This task is a follow-up hardening slice; it does not change the approved evidence spec or public `context.exec` shape.

**Files:**
- Modify: `internal/core/contextexec/types.go`
- Modify: `internal/core/contextexec/lifecycle.go`
- Modify: `internal/core/operation/context_binding.go`
- Modify: `internal/core/operation/context_binding_test.go`
- Modify: `internal/adapter/store/context_exec.go`
- Modify: `internal/adapter/store/context_exec_test.go`
- Modify: `internal/app/delegatedsession/ports.go`
- Modify: `internal/adapter/delegatedtmux/provider_tmux.go`
- Modify: `internal/adapter/delegatedtmux/provider_session.go`
- Modify: `internal/adapter/delegatedtmux/privacy.go`
- Modify: delegated tmux provider tests that construct/validate `Observation`.
- Modify: `internal/app/shellintegration/identity.go`
- Modify: `internal/app/shellintegration/context_exec.go`
- Modify: `internal/app/shellintegration/context_exec_test.go`
- Modify: `internal/adapter/shellintegration/context_exec.go`
- Modify: fish/zsh/bash context-exec adapter tests.
- Modify: `internal/adapter/contextexec/protocol.go`
- Modify: `internal/adapter/contextexec/protocol_test.go`
- Modify: `internal/adapter/contextexec/client.go`
- Modify: `internal/adapter/contextexec/server.go`
- Modify: `internal/adapter/contextexec/auth_test.go`
- Modify: `internal/adapter/contextexec/peer.go`
- Create: `internal/adapter/contextexec/foreground_darwin.go`
- Create: `internal/adapter/contextexec/foreground_darwin_test.go`
- Create: `internal/adapter/contextexec/foreground_other.go`
- Modify: `internal/adapter/contextexec/runtime.go`
- Modify: `internal/adapter/contextexec/runtime_test.go`
- Modify: `internal/adapter/contextexec/launcher_common.go`
- Modify: `internal/adapter/contextexec/launcher_darwin.go`
- Modify: `internal/adapter/contextexec/launcher_darwin_test.go`
- Modify: `internal/adapter/contextexec/launcher_linux.go`
- Modify: `cmd/shellbeam/command_context_exec.go`
- Modify: `cmd/shellbeam/command_context_exec_test.go`

**Interfaces:**
- Add `contextexec.ContextExpectation` as the pre-launch durable, non-secret expectation: exact `session_id`, `authority_epoch`, `provider_generation`, exact shell runtime identity, provider-observed current cwd, and `privacy_state=standard`. It deliberately has **no** `BoundaryQuality`; `shell_prompt` is not claimed until the armed hook actually fires and the exact helper authenticates.
- Preserve the existing string shape of `ContextBinding.ShellIdentity`. Add one canonical encoder in `internal/app/shellintegration`: after `core.ShellIdentity.Validate()`, encode exactly `string(shell.Family) + ":" + shell.RuntimeID`; both `ContextExpectation.ShellIdentity` and final `ContextBinding.ShellIdentity` use this value. Do not compare ad-hoc command names to the context shell identity.
- Name that helper `ContextShellIdentity(core.ShellIdentity) (string, error)` and use it in both Task-5A shell arm code and Task-6 authority construction; tests freeze fish/zsh/bash encodings and reject unknown/invalid shell identities.
- Bump private daemon↔helper `ProtocolVersion` from `1` to `2`, `operation.ContextExecStateSchemaVersion` from `1` to `2`, and `contextExecStoreSchemaVersion` from `1` to `2`. Pre-amendment v1 context-exec records are **not migrated**, because they may already claim a prompt boundary before helper authentication. Detect v1 explicitly, leave it immutable, return an internal `ErrLegacyContextExecState`, and have Task 6 map it to stable `context_exec_ambiguous`; never overwrite/relaunch from it.
- `operation.ContextExecState` stores `Expectation contextexec.ContextExpectation`, changes `Context` to `*contextexec.ContextBinding`, and stores `BoundaryObservedAt time.Time`. `reserved`/`helper_requested` require `Context=nil` and zero boundary time; `helper_authenticated` and later require a non-nil context matching the expectation plus non-zero boundary time. `BindHelperGeneration` therefore accepts the final context and boundary observation and commits context + claim verifier atomically.
- `delegatedsession.Observation` adds bounded non-secret `PaneTTY string` and `CWD string`; tmux obtains them from the same exact `display-message` identity query as pane PID/current command using `#{pane_tty}` and `#{pane_current_path}`. These fields remain optional for generic delegated/H4 flows; **H5 admission/arm requires both** and validates pane tty as an exact `/dev/...` device path and cwd as absolute. Missing facts block only context-exec, not ordinary delegated-session support.
- Add read-only `PrivacyProvider.InspectPrivacy(context.Context, ProviderRef) (PrivacyObservation, error)`. The tmux implementation verifies exact provider generation and observer mode against its durable provider-privacy record. `Active=true` blocks context-exec regardless of epoch; no privacy record or a valid inactive record is public/standard; malformed state, generation mismatch, or observer/state disagreement fails closed.
- `shellintegration.ProviderProcessFacts` carries optional `PaneTTY` and `CWD` without tightening its generic `Validate`; `ContextHelperArmRequest.validate` requires both. This preserves existing H4 readiness behavior while H5 shell identity/arm and peer verification use one provider-generation snapshot.
- Replace production `LaunchContextHelper(...)` timing with a one-shot `ArmContextHelper(...)` contract keyed by `context_exec_id + session_id + authority_epoch`, **not** `handoff_id`. The shell adapter installs one ephemeral fish/zsh/bash prompt hook whose only external command is the fixed `shellbeam __context_exec_helper <opaque_launch_id>` invocation; child argv never appears in shell text and the hook removes itself **before** invoking the helper when it fires, so a helper failure cannot auto-relaunch on the next prompt. The returned arm is an internal expectation, not a pre-claimed `BoundaryProof`; successful exact peer authentication of that unconsumed arm supplies `BoundaryQualityShellPrompt` and `BoundaryObservedAt`.
- Tighten `HostPeerVerifier`: exact UID, exact helper executable, `peer.ParentPID == PaneShellPID`, exact parent process identity, and `ForegroundVerifier(peerPID, paneTTY) == nil`. Remove the permissive multi-level ancestry walk for context-helper claims. On Darwin the default foreground verifier compares `unix.Getpgid(peerPID)` with `unix.IoctlGetInt(fd, unix.TIOCGPGRP)` on the exact pane tty opened with `O_NOCTTY|O_CLOEXEC`. Any unavailable/changed tty or mismatch fails before capability issuance.
- Extend `ClaimExpectation` with the frozen `ContextExpectation`, exact pane-shell PID/process identity, pane tty, and the unconsumed arm identity. Extend `ClaimBinder` to `BindClaim(ctx, contextExecID, helper, finalContext, boundaryObservedAt, verifierDigest)`. The server may issue the private capability only after peer proof; after proof it reads `ContextFrame`, constructs the final context, and the binder atomically persists context + boundary time + claim verifier. An unsafe peer does not consume the durable helper generation or receive argv.
- Darwin tty proof opens the exact provider-reported `/dev/...` path with `O_NOFOLLOW|O_NOCTTY|O_CLOEXEC`, verifies it is a character device with `fstat`, then performs `TIOCGPGRP`; a symlink/non-device/path change is fail-closed.


Use this read-only privacy shape:

```go
type PrivacyObservation struct {
    ProviderGeneration string
    Active             bool
    ObservedAt         time.Time
}

type PrivacyProvider interface {
    // existing Arm/Prove/Release methods
    InspectPrivacy(context.Context, core.ProviderRef) (PrivacyObservation, error)
}
```

`InspectPrivacy` must verify the currently installed tmux control observer agrees with the durable privacy record; absence of a record is public only when the current observer is non-private.
- Private protocol v2 sequence becomes:

```text
hello -> challenge+capability -> proof -> context(cwd) -> request
request -> prepared(resolved_executable OR stable prepare failure)
prepared success -> execute(child ids + resolved executable)
execute -> spawn(child ids + resolved executable + literal spawn evidence)
spawn success -> output* -> terminal(child_terminal)
```

Use closed frames:

```go
type ContextFrame struct { CWD string }
type PreparedFrame struct {
    ResolvedExecutable string
    FailureCode        failure.Code // mutually exclusive with ResolvedExecutable
}
type ExecuteFrame struct {
    ChildOperationID   operation.ID
    ChildSessionID     operation.SessionID
    ResolvedExecutable string
}
type SpawnFrame struct {
    ChildOperationID   operation.ID
    ChildSessionID     operation.SessionID
    ResolvedExecutable string
    Spawn              receipt.SpawnEvidence
}
```

`PreparedFrame.FailureCode` is allowed only for deterministic pre-spawn failures such as executable-not-found/unqualified and ends without child reservation; canonical failure records `Spawn.Attempted=false`, `Spawn.Succeeded=false`, and `Spawn.ErrorCode=string(FailureCode)`. A failed `SpawnFrame` after execute authorization is likewise terminal deterministic spawn failure and must carry `Spawn.Attempted=true`, `Spawn.Succeeded=false`, and a stable `ErrorCode`. Disconnect after execute ACK but before a valid spawn frame is ambiguous.

- Add `LifecycleChildReserved = "child_reserved"` between `helper_authenticated` and `child_spawned`. `child_reserved` requires helper binding + exact v6 child operation/session IDs and forbids terminal result. `child_spawned` means explicit helper spawn truth has been accepted, never merely that a reservation exists.
- `operation.ContextExecState` adds monotonic `ExecutionAuthorized bool`: false through first `child_reserved`, true only after the exact child reservation is durable and immediately before execute ACK. Earlier lifecycles and terminal/canonical states validate the only legal values; state/store replay must never regress true→false.
- `operation.ContextExecTransition` adds `AuthorizeExecution bool`; it is legal only as an idempotent same-lifecycle `child_reserved -> child_reserved` transition that changes `ExecutionAuthorized false -> true`. There is no transition that writes false, so replay cannot regress authorization.
- Split helper execution into preparation and start:

```go
type PreparedExecution interface {
    ResolvedExecutable() string
    Start() (*ChildProcess, error)
    Close() error
}

type ChildLauncher interface {
    Qualified() bool
    Prepare(ChildSpec) (PreparedExecution, error)
}
```

`Prepare` resolves under inherited PATH and opens/freezes the exact executable object but creates no child. Darwin `Start` keeps the opened-object identity through the ACK gap, then performs the already-qualified ptrace exec-stop + mapped-vnode comparison before first instruction. Non-qualified platforms stay fail-closed.

- Helper terminal frames after a successful spawn carry `LifecycleChildTerminal` and no promoted evidence authority. `Result.Validate` gets an explicit `LifecycleChildTerminal` branch requiring exact helper, resolved executable, successful spawn, reap/exit truth, helper-owned output attribution, and empty evidence authority. The daemon/app layer in Task 6 validates and transforms that stored terminal result into `LifecycleCanonicalized` + `context_exec_child_owned_v1` only after durable child identity and attribution checks.
- `LifecycleCanonicalized` also supports a **deterministic no-child failure** variant for prepare/spawn failure: `FailureCode != ""`, `EvidenceAuthority=""`, `EvidenceQualityUnproven`, no fabricated exit/output completeness, and spawn evidence matching the failure stage. This variant is durable/replayable but is never strong evidence. Successful-child canonicalization keeps the existing strict `context_exec_child_owned_v1` requirements.
- `operation.ContextExecState.Validate` mirrors that split: canonical successful-child state requires child operation/session IDs and `ExecutionAuthorized=true`; canonical prepare failure permits no child IDs and `ExecutionAuthorized=false`; canonical explicit spawn failure requires the reserved child IDs and `ExecutionAuthorized=true` but no successful child/reap claim. All canonical variants require the authenticated helper/final context.

- [ ] **Step 1: RED exact-foreground helper authentication.**

Add a unit topology where the connecting process has the exact ShellBeam executable, UID, and exact pane-shell direct parent but a non-foreground PGID. It must fail before `NewCapability`/`BindClaim`. Replace the existing wrapper-ancestry positive case with a direct-parent positive case plus injected foreground verifier success; add a negative wrapper-descendant case even when executable/UID/shell identity are otherwise correct.

```go
verifier := HostPeerVerifier{
    ExpectedHelperExecutable: helper,
    ParentPID: paneShellPID,
    ParentIdentity: paneShellIdentity,
    PaneTTY: "/dev/ttys042",
    Foreground: func(peerPID int, paneTTY string) error {
        if peerPID != expectedForegroundPID || paneTTY != "/dev/ttys042" {
            return errors.New("not foreground")
        }
        return nil
    },
    // existing credential/process observers; peer.ParentPID must equal ParentPID
}
```

Run:

```bash
go test ./internal/adapter/contextexec -run 'TestHostPeerVerifier.*Foreground|TestServerRejects.*UnsafePeer' -count=1
```

Expected RED: the current ancestry walk accepts a wrapper/background descendant or the new direct-parent/foreground fields do not exist.

- [ ] **Step 2: GREEN Darwin foreground verifier and tmux tty/cwd facts.**

Extend the tmux facts query atomically to:

```text
#{session_id}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_current_command}|#{pane_tty}|#{pane_current_path}|#{pane_dead}|#{pane_dead_status}|#{socket_path}|#{pid}|#{version}
```

Validate `PaneTTY` is an absolute `/dev/...` path with no NUL/newline and `CWD` is absolute. Implement Darwin foreground proof with `unix.Getpgid`, `unix.Open(..., unix.O_RDONLY|unix.O_NOCTTY|unix.O_CLOEXEC, 0)`, and `unix.IoctlGetInt(fd, unix.TIOCGPGRP)`. Never infer foreground from `pane_current_command` text.

Run:

```bash
go test ./internal/app/delegatedsession ./internal/adapter/delegatedtmux ./internal/app/shellintegration ./internal/adapter/contextexec -count=1
go test ./internal/adapter/delegatedtmux -run 'Privacy.*Inspect|Inspect.*Privacy' -count=1
go test -race ./internal/adapter/contextexec ./internal/adapter/delegatedtmux -count=1
```

- [ ] **Step 3: RED single-hook prompt-bound launch with no proof→write gap.**

Replace tests that permit `BoundaryProof` to be supplied and a later helper write to occur. The negative test must demonstrate that an adapter cannot expose a production API which accepts a stale prompt proof and later writes the helper command. Positive fish/zsh/bash tests assert one installed one-shot hook contains only the fixed helper invocation plus adapter-local self-removal mechanics; target argv, environment values, H4 notifier commands, and claim material must be absent.

The app-facing contract is:

```go
type ContextHelperArmRequest struct {
    ContextExecID  string
    SessionID      string
    Authority      delegated.EffectiveAuthority
    Facts          ProviderProcessFacts
    ExpectedShell  core.ShellIdentity
    OpaqueLaunchID string
}

type ContextHelperArm struct {
    ContextExecID      string
    SessionID          string
    AuthorityEpoch     delegated.AuthorityEpoch
    ProviderGeneration string
    Shell              core.ShellIdentity
    PaneShellPID       int
    PaneTTY            string
    OpaqueLaunchID     string
    ArmedAt            time.Time
}

func (s *Service) ArmContextHelper(ctx context.Context, req ContextHelperArmRequest) (ContextHelperArm, error)
```

The arm is consumed at most once. It does not itself prove a prompt boundary; the final boundary is recorded only when a peer carrying the same opaque launch ID is the exact foreground direct child of the exact pane shell and completes claim authentication.

Run:

```bash
go test ./internal/app/shellintegration ./internal/adapter/shellintegration -run ContextHelper -count=1
```

- [ ] **Step 4: RED authenticated cwd check before request delivery.**

Add `KindContext` / `ContextFrame{CWD string}`. After successful proof the client sends `os.Getwd()`. The server compares `filepath.Clean(frame.CWD)` with `state.Expectation.CWDObserved`; mismatch returns `context_exec_boundary_unproven` and must not send `RequestFrame` or bind helper generation. On equality it constructs the final `ContextBinding` from the expectation with `BoundaryQuality="shell_prompt"` and atomically passes that context plus the current boundary observation time into `BindHelperGeneration`.

```go
type ContextFrame struct {
    ProtocolVersion int         `json:"protocol_version"`
    Kind            MessageKind `json:"kind"`
    CWD             string      `json:"cwd"`
}
```

Run:

```bash
go test ./internal/adapter/contextexec -run 'CWD|ContextFrame|Authenticate' -count=1
```

Expected RED: current auth has no post-proof cwd frame and would deliver request without this equality proof.
Also RED-test that `ClaimExpectation` with changed expectation/session/epoch/provider generation cannot reach `BindClaim`, and that `BindClaim` persists final context/boundary atomically rather than authenticating first and patching context later.

- [ ] **Step 5: RED durable `child_reserved` transition before execute authorization.**

Add lifecycle/store tests proving:

```text
helper_authenticated -> child_reserved -> child_spawned -> child_terminal -> canonicalized
```
Also update Task-3 state tests: `reserved` and `helper_requested` carry a valid `ContextExpectation` but no final context; `BindHelperGeneration` is the only transition that may install `ContextBinding` + `BoundaryObservedAt`, and it must exactly match expectation session/epoch/shell/cwd/privacy. Exact request replay preserves the first expectation and cannot replace it after provider drift.


`child_reserved` is accepted only after the exact schema-v6 child `operation.Reservation` exists and matches context exec ID, parent session, epoch, request fingerprint, frozen cwd, and resolved executable execution fingerprint. `child_spawned` before `child_reserved` is rejected. Replay of the same child reservation/transition is idempotent; changed resolved executable or child identity conflicts.
Add fault/reopen tests for the second `child_reserved` transition that flips only `ExecutionAuthorized=false -> true`; regression to false or setting it before a child reservation is rejected.
Add reopen tests for a synthetic v1 context-exec record: lookup must classify it as legacy/ambiguous, leave bytes untouched, and never permit ReserveContextExec/BindHelperGeneration/child launch to reuse it. Add schema-v2 roundtrip tests for `ContextExpectation`, nullable final `Context`, `BoundaryObservedAt`, and `ExecutionAuthorized`.

Run:

```bash
go test ./internal/core/contextexec ./internal/core/operation ./internal/adapter/store -run ContextExec -count=1
```

Expected RED: lifecycle has no `child_reserved` state.

- [ ] **Step 6: RED helper prepare→daemon ACK→spawn ordering.**

Add protocol/runtime tests with a fake launcher whose `Prepare` records the resolved executable but whose `Start` fails the test if called before an `ExecuteFrame` has been accepted. The server callback order must be observable as:

```text
bind helper generation
receive prepared resolved executable
reserve exact v6 child operation
persist child_reserved
send execute ACK
helper Start
receive explicit spawn frame
persist child_spawned
receive output/terminal
```

A dropped connection after `PreparedFrame` but before execute ACK must close the prepared object without starting a child. A dropped connection after execute ACK but before `SpawnFrame` is **spawn-unknown** and cannot authorize a second helper/child.
A deterministic `PreparedFrame.FailureCode` must never call `Start` or reserve child IDs; a `SpawnFrame` with `Spawn.Attempted=true, Spawn.Succeeded=false` must produce a durable canonical failure with empty evidence authority. Add protocol-version mismatch tests proving v1 helper/daemon pairs fail before claim/request delivery.

Run:

```bash
go test ./internal/adapter/contextexec ./internal/adapter/store -run 'Prepared|ChildReserved|ExecuteFrame|SpawnFrame|Ack' -count=1
```

- [ ] **Step 7: GREEN split launcher/runtime and preserve Darwin TOCTOU proof across the ACK gap.**

Refactor the launcher so `Prepare` resolves/opens and freezes identity, while `Start` performs child creation. Keep the target file descriptor/object alive until `Start` or `Close`; on Darwin, after `cmd.Start()` the traced child must still stop at exec, mapped vnode must equal the identity captured during `Prepare`, then detach/continue. Extend the existing path-swap attack to swap the pathname **after `Prepare` and before `Start`**; malicious first instruction must never execute. Shebang remains fail-closed.

Run:

```bash
go test ./internal/adapter/contextexec -run 'DarwinPlatformLauncher|Runtime' -count=1
go test -race ./internal/adapter/contextexec -count=1
```

- [ ] **Step 8: GREEN helper reports terminal truth; daemon authority remains absent.**

Change helper runtime terminal construction to:

```go
result.Lifecycle = contextexec.LifecycleChildTerminal
result.EvidenceAuthority = ""
```

and validate that the private helper cannot produce a canonicalized success result or `context_exec_child_owned_v1`. Add a negative test that any successful-child terminal frame attempting either claim is rejected by the server/app boundary. Also test deterministic prepare/spawn failures end through the dedicated failure path without inventing a child terminal. Task 6 will own all canonicalization transitions.

Run:

```bash
go test ./internal/core/contextexec ./internal/adapter/contextexec -run 'Terminal|Authority|Canonical' -count=1
```

- [ ] **Step 9: Full hardening verification and commit.**

Run:

```bash
git diff --name-only --diff-filter=ACM -z -- '*.go' | xargs -0 gofmt -w
go test ./internal/core/contextexec ./internal/core/operation ./internal/adapter/store ./internal/app/delegatedsession ./internal/adapter/delegatedtmux ./internal/app/shellintegration ./internal/adapter/shellintegration ./internal/adapter/contextexec ./cmd/shellbeam -count=1
go test -race ./internal/adapter/contextexec ./internal/adapter/delegatedtmux ./internal/adapter/shellintegration -count=1
GOOS=linux GOARCH=amd64 go test -c ./internal/adapter/contextexec -o /tmp/shellbeam-contextexec-linux.test
GOOS=linux GOARCH=amd64 go build -o /tmp/shellbeam-linux ./cmd/shellbeam
rm -f /tmp/shellbeam-contextexec-linux.test /tmp/shellbeam-linux
go run ./tools/devctl check
git diff --check
git add internal/core/contextexec internal/core/operation internal/adapter/store internal/app/delegatedsession internal/adapter/delegatedtmux internal/app/shellintegration internal/adapter/shellintegration internal/adapter/contextexec cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "fix: harden context exec pre-spawn authority"
```

---

### Task 6: Orchestrate admission, helper/child lifecycle, recovery, and evidence promotion

**Files:**
- Modify: `internal/core/operation/context_binding.go`
- Modify: `internal/core/operation/context_binding_test.go`
- Create: `internal/app/contextexec/ports.go`
- Create: `internal/app/contextexec/service.go`
- Create: `internal/app/contextexec/service_test.go`
- Create: `internal/app/contextexec/reconcile.go`
- Create: `internal/app/contextexec/reconcile_test.go`
- Create: `internal/app/daemon/context_exec.go`
- Create: `internal/app/daemon/context_exec_test.go`
- Modify: `internal/app/daemon/handoff_port.go`
- Modify: `internal/app/interactivehandoff/service.go`
- Modify: `internal/app/interactivehandoff/service_test.go`
- Modify: `internal/adapter/store/context_exec.go`
- Modify: `internal/adapter/store/interactive_handoffs.go`
- Modify: context-exec/handoff store tests for the shared lease transaction.
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `internal/core/receipt/result.go`
- Modify: `internal/app/evidence/worker.go`
- Modify: `internal/app/daemon/evidence_worker.go`
- Modify: `internal/app/structuredresult/worker.go`
- Modify: `internal/app/daemon/telemetry_worker.go`

**Interfaces:**
- Internal app method `Execute(ctx, contextexec.Request)` returns the durable current/canonical context-exec state; Task 7 later projects it through IPC/MCP. Task 6 does not add public schema fields early.
- `ContextExecStore` is the narrow durable port over existing Task-3 primitives:

```go
type Durability string

const (
    NoDurableChange Durability = "none"
    DurableChange   Durability = "durable"
    AmbiguousChange Durability = "ambiguous"
)

type MutationResult struct {
    Durability Durability
    Err        error
}

type ContextExecStore interface {
    ReserveContextExec(context.Context, operation.ContextExecState) (operation.ContextExecState, bool, MutationResult)
    LookupContextExec(context.Context, string) (operation.ContextExecState, bool, error)
    AdvanceContextExec(context.Context, string, operation.ContextExecTransition) (operation.ContextExecState, MutationResult)
    BindHelperGeneration(context.Context, string, contextexec.HelperBinding, contextexec.ContextBinding, time.Time, string) (operation.ContextExecState, MutationResult)
    ReserveOperation(context.Context, operation.Reservation) (operation.Reservation, bool, MutationResult)
    AcquireContextExecLease(context.Context, operation.SessionID, delegated.AuthorityEpoch, string, string) (operation.ContextExecLease, bool, MutationResult)
    ReleaseContextExecLease(context.Context, operation.ContextExecLease) MutationResult
    FindContextExecLease(context.Context, operation.SessionID, delegated.AuthorityEpoch) (operation.ContextExecLease, bool, error)
    ListContextExecRecoveryCandidates(context.Context) ([]operation.ContextExecState, error)
}
```

- `ContextAuthority` returns one exact current admission snapshot: live delegated binding/provider ref + provider generation, effective agent authority, `InspectPrivacy` truth, exact shell identity, pane PID/identity, pane tty, current cwd. It must be recomputed before reservation and revalidated immediately before arming the one-shot helper hook; Task 6 never reconstructs it from stale public projection or require a handoff-ID lookup.
- H5 admission and helper arm are keyed only by durable delegated `session_id + authority_epoch`; H4 handoff IDs may appear only inside provider-private privacy records and existing handoff APIs, never as a required `context.exec` input or synthesized lookup.
- `HelperRuntime` owns only private transport mechanics (private listener, one-shot arm, peer/protocol exchange). Every durable callback comes from the app service; the adapter may not silently advance lifecycle on its own.
- `internal/app/daemon/context_exec.go` provides the adapter from the repository methods that return `daemon.StoreResult` into the local `contextexec.MutationResult`; `internal/app/contextexec` must not import `internal/app/daemon`.
- `ContextExecAvailable` in daemon composition is an **internal fact** requiring the exact Task-5A runtime dependencies. Task 7 is responsible for exposing that fact through `capability.Catalog`; Task 6 must not prematurely change the public capability schema.
- Add `operation.ContextExecLease{SessionID, AuthorityEpoch, ContextExecID, RequestFingerprint}` with validation in `internal/core/operation/context_binding.go`, persisted under `context-exec/leases/v1/` keyed by an internal domain-separated digest of exact `session_id + authority_epoch` (do not place raw session IDs into path construction). `AcquireContextExecLease` and `ReserveHandoff` both execute under the repository's existing `delegatedSessionMu`: lease acquisition revalidates live/current agent binding before create; `ReserveHandoff` refuses to rotate the epoch while a lease exists. This is the atomic race boundary—no app-level check may substitute for it. Acquire the lease after request reservation/revalidation but **before** `helper_requested`/shell mutation. Release only after deterministic no-spawn failure, proved child terminal/canonicalization, or explicit cancellation+reap; ambiguous/spawn-unknown state retains the lease until reconciliation proves safe release.

- [ ] **Step 1: RED replay-first admission and internal availability gate.**

For an exact existing `context_exec_id`, lookup/fingerprint comparison happens before any fresh shell/provider observation or helper arm. Exact replay returns durable state/result; changed request returns `operation_conflict`. For a new request require, in one fresh authority snapshot:

```text
live delegated session
provider generation current
agent effective owner + agent ingress writable
request epoch == current epoch
provider `InspectPrivacy.Active == false` with exact current generation
no active ownership-transfer phase
qualified exact fish/zsh/bash shell identity
current pane tty + current cwd
Task-5A helper runtime composed/qualified
no active context child lease for session_id + authority_epoch
```

Write table tests for each failed gate and assert `ArmContextHelper` call count remains zero.

Run:

```bash
go test ./internal/app/contextexec ./internal/app/daemon -run 'Admission|Replay|Unavailable' -count=1
```

- [ ] **Step 2: GREEN reserve before the one-shot helper hook.**

Construct only the pre-launch `ContextExpectation` from the fresh snapshot and reserve it before any shell mutation. A reserved state must not yet claim `shell_prompt`. Then revalidate provider generation, owner/epoch, shell identity, pane tty/cwd, and `InspectPrivacy`. Only after that second proof may the service atomically acquire the exact session+epoch context-exec lease; only a held lease permits persisting `helper_requested` and calling the Task-5A one-shot arm. If arm delivery is ambiguous, transition to `ambiguous`; never send a second hook unless provider evidence proves non-delivery.

```go
state := operation.ContextExecState{
    SchemaVersion: operation.ContextExecStateSchemaVersion,
    Request: req,
    RequestFingerprint: fingerprint,
    Expectation: contextexec.ContextExpectation{
        SessionID: req.SessionID,
        AuthorityEpoch: req.AuthorityEpoch,
        ProviderGeneration: authority.ProviderGeneration,
        ShellIdentity: authority.Shell.RuntimeID,
        CWDObserved: authority.CWD,
        PrivacyState: "standard",
    },
    Context: nil,
    Lifecycle: contextexec.LifecycleReserved,
    CreatedAt: now,
    UpdatedAt: now,
}
```

- [ ] **Step 3: GREEN authenticated helper claim + cwd equality.**

The private server expects the exact opaque launch/helper generation and uses Task-5A foreground peer verification. After proof, `ContextFrame.CWD` must equal `state.Expectation.CWDObserved`, the exact direct-parent shell identity must equal `state.Expectation.ShellIdentity`, and the arm/provider generation must equal `state.Expectation.ProviderGeneration`. Only then construct the final `ContextBinding`, persist it + `BoundaryObservedAt` + claim verifier via `BindHelperGeneration`, and send `RequestFrame`. A helper whose cwd, shell, provider generation, foreground ownership, or privacy truth drifted must fail before it sees argv.

Add tests where human `cd`/nested-shell change occurs between reservation and helper auth; no child reservation/spawn may exist afterward.

- [ ] **Step 4: GREEN prepared executable -> exact v6 child reservation -> execute ACK.**

When the server receives `PreparedFrame.ResolvedExecutable`, create/replay the exact child operation identity and reservation:

```go
binding := &operation.ContextExecBinding{
    ContextExecID: state.Request.ContextExecID,
    ParentSessionID: operation.SessionID(state.Request.SessionID),
    AuthorityEpoch: state.Request.AuthorityEpoch,
    RequestFingerprint: state.RequestFingerprint,
}
execFP, err := binding.ExecutionFingerprint(state.Context.CWDObserved, prepared.ResolvedExecutable)
reservation := operation.Reservation{
    SchemaVersion: operation.ContextExecReservationSchemaVersion,
    OperationID: childOperationID,
    SessionID: childSessionID,
    RequestFingerprint: state.RequestFingerprint,
    ExecutionFingerprint: execFP,
    ExecutionMode: operation.ExecutionModeArgv,
    Executable: prepared.ResolvedExecutable,
    Argv: append([]string(nil), state.Request.Argv...),
    CWD: state.Context.CWDObserved,
    TimeoutMS: state.Request.TimeoutMS,
    DaemonIncarnation: incarnation,
    ContextExec: binding,
}
```

Persist `ReserveOperation`, then `AdvanceContextExec(...LifecycleChildReserved...)`, and only then send `ExecuteFrame`. Add `operation.DeriveContextChildIDs(requestFingerprint)` and derive IDs exactly as full lowercase hex SHA-256 of these byte strings: operation domain `"shellbeam-context-child-operation-v1\x00" + requestFingerprint`, session domain `"shellbeam-context-child-session-v1\x00" + requestFingerprint`; prefix the digests with `cxop_` and `cxs_` respectively. The resulting 69/68-byte IDs satisfy the existing 128-byte grammar. Tests cover determinism, domain separation, parseability, and changed-fingerprint divergence. Same prepared replay must resolve to the same child reservation; changed executable/path must conflict.

- [ ] **Step 5: RED/GREEN explicit spawn truth and active lease.**

On `SpawnFrame`, validate exact child IDs + resolved executable against `child_reserved`, then persist `child_spawned`. The durable execution lease already spans `helper_requested`, `helper_authenticated`, `child_reserved`, and `child_spawned`; handoff epoch rotation cannot commit while it exists. Add store-level concurrency tests racing `AcquireContextExecLease` against `ReserveHandoff`: exactly one wins. Add service races for transfer intent arriving (a) before lease acquisition, (b) after helper auth but before execute ACK, and (c) after execute ACK/spawn. If handoff wins (a), context exec becomes stale before shell mutation; if context lease wins, the handoff request is blocked until safe lease release. No case silently reclassifies the child under a new epoch.

Run:

```bash
go test ./internal/app/contextexec ./internal/app/daemon ./internal/app/interactivehandoff -run 'ContextExec|Handoff.*Context' -count=1
go test -race ./internal/app/contextexec ./internal/app/daemon ./internal/adapter/store -count=1
```

- [ ] **Step 6: RED recovery matrix with `child_reserved` as spawn-unknown after ACK.**

Implement explicit recovery decisions:

```text
reserved + no helper mutation proof        -> exact replay may continue admission
helper_requested + delivery unproven       -> ambiguous; no blind second hook
helper_authenticated, no child reservation -> same generation may reconnect only under approved bound
child_reserved before execute ACK          -> prepared child may be closed; no spawn claim
child_reserved after execute ACK/lost spawn -> ambiguous/spawn-unknown; no duplicate child
child_spawned + helper lost                 -> helper_lost/ambiguous unless exact cleanup/reap proof exists
child_terminal                              -> idempotently validate/persist terminal truth
canonicalized success/failure               -> exact replay returns final result, no helper launch; release lease only when no child remains possible
```

The durable state adds mandatory monotonic `ExecutionAuthorized bool`. It is `false` when `child_reserved` is first persisted. Immediately before sending `ExecuteFrame`, persist the same lifecycle with `ExecutionAuthorized=true`; `child_spawned` requires it. If the authorization write is ambiguous, do not send the ACK. If the durable bit is true but ACK delivery is unknown, recovery treats the execution as spawn-unknown and never authorizes a duplicate child. The bit contains no capability/secret and gets reopen/fault-boundary idempotence tests.

- [ ] **Step 7: GREEN daemon-only canonicalization and evidence promotion.**

For successful spawn, accept a helper terminal result only when it is `LifecycleChildTerminal`, has no promoted evidence authority, and exactly matches request fingerprint, frozen context, helper generation, reserved executable, helper-owned pipe attribution, byte counts, spawn evidence, and exact reap/exit truth. Persist `child_terminal`, then construct the daemon-owned canonical copy:

```go
canonical := terminal.Clone()
canonical.Lifecycle = contextexec.LifecycleCanonicalized
canonical.EvidenceAuthority = contextexec.EvidenceAuthorityContextExecChildOwnedV1
```

Validate and persist `canonicalized` before scheduling any evidence/structured/telemetry worker. Mixed pane bytes, delegated session receipt, environment values, or helper-provided authority are never inputs to this promotion.
For deterministic prepare failure or explicit failed `SpawnFrame`, canonicalize the no-child failure variant with empty evidence authority and release the execution lease after that state is durable. Evidence/structured/telemetry workers must not treat that failure result as mechanical child evidence.

- [ ] **Step 8: Evidence/receipt projection stays scoped to attributable facts.**

Extend receipt/result handling only as needed to represent the canonical context-exec child operation and requested-vs-resolved executable provenance. Existing evidence consumers may use spawn/exit/output facts only when authority is exactly `context_exec_child_owned_v1` and output is complete for their contract. No resource/hermetic/artifact authority is inferred. Do not persist/project raw inherited environment or a deterministic derivative of it.

- [ ] **Step 9: Compose runtime internally without exposing Task-7 public capability early.**

In `cmd/shellbeam/command_daemon.go`, compose the private context runtime only when all of these are concrete and qualified: delegated tmux runtime, fish/zsh/bash shell integration, private runtime dir, installed executable identity, process observer, Darwin strong child launcher, and foreground verifier. Pass it through `daemon.Options`. `ContextExecAvailable` is false if any element is absent; ordinary delegated/handoff support remains unaffected.

- [ ] **Step 10: Focused/race/devctl/commit.**

Run:

```bash
git diff --name-only --diff-filter=ACM -z -- '*.go' | xargs -0 gofmt -w
go test ./internal/app/contextexec ./internal/app/daemon ./internal/app/interactivehandoff ./internal/adapter/store ./internal/app/evidence ./internal/app/structuredresult ./internal/core/receipt ./cmd/shellbeam -count=1
go test -race ./internal/app/contextexec ./internal/app/daemon -count=1
go run ./tools/devctl check
git diff --check
git add internal/core/operation internal/app/contextexec internal/app/daemon internal/app/interactivehandoff internal/adapter/store internal/core/receipt internal/app/evidence internal/app/structuredresult cmd/shellbeam
git diff --cached --check
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "feat: orchestrate high assurance context execution"
```

---

### Task 7: Add one-tool `context.exec` schema/capability surface and doctor diagnostics

**Files:**
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/ipc-v2.json`
- Create: `api/schema/context_exec_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/protocol_v2_fields.go`
- Modify: `internal/adapter/ipc/protocol_v2_decode.go`
- Modify: `internal/adapter/ipc/client_v2_request.go`
- Modify: `internal/adapter/ipc/response_v2.go`
- Modify: `internal/adapter/ipc/server_v2_unix.go`
- Create: `internal/adapter/ipc/context_exec_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/input_fields.go`
- Modify: `internal/adapter/mcp/call.go`
- Modify: `internal/adapter/mcp/request.go`
- Modify: `internal/adapter/mcp/server.go`
- Create: `internal/adapter/mcp/context_exec_test.go`
- Modify: `internal/core/capability/catalog.go`
- Create: `internal/core/capability/context_exec_test.go`
- Modify: `cmd/shellbeam/doctor.go`
- Create: `cmd/shellbeam/doctor_context_exec_test.go`

**Interfaces:**
- Public action name and request fields must exactly match approved Task-1 design. Candidate is `context.exec` with `context_exec_id`, `session_id`, `authority_epoch`, `argv`, timeout/output bounds.

- [ ] **Step 1: RED closed-schema/legacy tests.**

No cwd/env payload/command string if approved V1 is argv-only. v1 clients reject/omit context-exec. One MCP tool registration remains.

- [ ] **Step 2: Capability intersection.**

Context-exec available only for exact shell adapters/provider/authority/evidence contract composition. Resource/hermetic support reported separately, not inherited by implication.

- [ ] **Step 3: Doctor diagnostics.**

Show context-exec availability, shell adapters, evidence quality, helper protocol version, and blockers without environment values/private capability tokens.

- [ ] **Step 4: Schema/adapter/commit.**

Run schema/IPC/MCP/capability tests, devctl/commit-gate; commit `feat: expose delegated context execution`.

---

### Task 8: High-assurance native end-to-end and adversarial evidence matrix

**Files:**
- Create: `tests/integration/context_exec_test.go`
- Create: `cmd/shellbeam/context_exec_acceptance_test.go`
- Create: `docs/superpowers/evidence/2026-08-18-context-exec-high-assurance.md`

**Interfaces:**
- Produces exact `CONTEXT_EXEC_STABLE_GATE=true|false` evidence; does not imply broader-provider promotion.

- [ ] **Step 1: Primary post-secret workflow.**

End-to-end:

```text
delegated shell
secret H4 handoff exports fake credential
privacy safely released
agent requests context.exec argv for fake doctor command
helper inherits exported credential + cwd
child succeeds
model receives bounded command output + literal spawn/exit receipt
secret never appears in ShellBeam result/state/logs
```

- [ ] **Step 2: Exactly-once/retry matrix.**

Lost public response, helper auth response, child terminal ack; same ID never causes duplicate side effect. Changed argv under same ID conflicts.

- [ ] **Step 3: Generation/ownership matrix.**

Stale epoch, transfer-to-human race, human-owned request, private capture, unknown shell, nested shell change all fail before new child spawn.

- [ ] **Step 4: Helper/daemon/child crash matrix.**

Fault every boundary from durable reserve through terminal canonicalization. Record exact success/failure/ambiguous behavior required by approved evidence spec.

- [ ] **Step 5: Environment/privacy/control-capability anti-leak.**

Scan MCP/IPC/output/receipt/Event Journal/evidence/repro/telemetry/state/logs/helper protocol metadata/argv for deterministic user-secret canary and common encodings/hashes. Add separate ShellBeam-control env/FD/claim sentinels and prove the workload cannot observe them. The child may consume the user secret but must not echo it in the test.

- [ ] **Step 6: Output-attribution, non-bearer-ID, and actual-executable adversarial matrix.**

Run noisy delegated-pane background output concurrently with a context-exec child and prove the authoritative receipt/output contains only the dedicated child channel. Replay the visible `context_exec_id` without valid claim proof and require pre-spawn rejection. Prepend a controlled shadow directory to delegated `PATH`, execute a named binary, and require the receipt to report the exact absolute executable actually launched; changing the requested token or executable binding under the same logical identity conflicts/fails according to the approved spec.

- [ ] **Step 7: Resource/hermetic truth.**

If context-exec does not explicitly compose Resource Enforcement/Hermetic provider under a separately proven contract, capability/evidence says unavailable/unproven; test prevents accidental inheritance of those claims.

- [ ] **Step 8: Fresh gates and exact checkpoint.**

```bash
go mod verify
go test ./internal/core/contextexec ./internal/app/contextexec ./internal/adapter/contextexec ./internal/app/shellintegration ./internal/adapter/shellintegration ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
go test -race ./internal/core/contextexec ./internal/app/contextexec ./internal/adapter/contextexec ./internal/app/daemon -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
git diff --check
```

Write tracked evidence with approved spec hash, helper protocol, shell matrix, evidence authority, canary/crash/retry results, and `CONTEXT_EXEC_STABLE_GATE`. Stage/commit `test: verify high assurance context execution`; postcommit clean tree + fresh devctl check.

---

### Task 9: Promote Nushell and additional terminal/session providers only through existing qualification contracts

**Files:**
- Create: provider-specific qualification evidence under `docs/superpowers/evidence/`.
- Create/modify adapter files only for providers selected for promotion after Task 8.
- Create: `internal/adapter/shellintegration/nushell.go`
- Create: `internal/adapter/shellintegration/nushell_test.go`
- Modify: `internal/adapter/terminalpresentation/providers.go`
- Modify: `internal/adapter/terminalpresentation/providers_test.go`
- For any additional interactive-session provider: create only its qualification evidence here; after a full H0-equivalent P0–P15 PASS, write a separate provider implementation plan before production adapter code.

**Interfaces:**
- Promotions reuse existing core/app ports; no provider may create a new weaker semantic path.

- [ ] **Step 1: Nushell qualification.**

Prove exact current-shell detection, ephemeral safe-boundary/readiness hooks, environment-exported-nonempty without value capture, fixed context-helper launch, cleanup/no config mutation, private notifier rules. Only then advertise corresponding L2/L3/H4/H5 capabilities.

- [ ] **Step 2: Additional terminal provider qualification.**

Prove exact installed/running identity, safe attach argv launch, idempotent GUI outcome, native smoke, no preference requirement. Add only to actual capability subset.

- [ ] **Step 3: Additional session-provider qualification.**

Run a full equivalent of H0 P0–P15 including ingress fence, privacy topology, restart, environment-preserving attach, resource/leak stress. Arbitrary external PTY adoption remains outside stable V1 unless a separately reviewed experimental design says otherwise.

- [ ] **Step 4: Provider-specific commit and evidence.**

Each provider is one independently reviewable commit/evidence gate; one provider failure cannot weaken core/tmux provider guarantees.

---

## H5 Completion Gate

The high-assurance context-exec portion of H5 is complete only when:

1. a separately reviewed evidence contract explicitly approves implementation and exact wire/authority semantics;
2. context means exported process environment + current shell cwd only, with no claim about unexported shell-local state;
3. public request is exact/bounded and child command payload never travels through shell text;
4. fixed private helper launch occurs only at qualified current-shell safe boundary under current agent authority/epoch;
5. helper intentionally inherits delegated exported env/cwd but never serializes environment values/hashes to daemon/model state;
6. helper auth binds exact context-exec/session/epoch/generation/request fingerprint;
7. durable reservation precedes helper launch and retries cannot duplicate child execution;
8. helper owns literal child spawn/output/signal/timeout/reap and loss never fabricates terminal truth;
9. stale generation/human ownership/private capture/nested-shell drift blocks new context child spawn;
10. only approved helper/child evidence is promoted to ordinary evidence pipeline; transcript markers remain advisory;
11. post-secret capability command can produce a real receipt without secret transmission through MCP/model state;
12. Resource Enforcement/Hermetic claims are separate and never inherited implicitly;
13. one-tool/legacy/no-background-orchestrator boundaries remain intact;
14. native retry/crash/privacy/race/schema/devctl evidence passes and `CONTEXT_EXEC_STABLE_GATE=true` is recorded.

Broader provider promotion is additive: Nushell/terminal/session providers become available only when their own native qualification passes; failure/absence does not invalidate a proven H5 context-exec core on already-qualified providers.
