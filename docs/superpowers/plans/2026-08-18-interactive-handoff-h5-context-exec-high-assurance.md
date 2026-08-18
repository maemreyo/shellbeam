# Interactive Handoff H5 High-Assurance Context Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close and implement a separately reviewed evidence contract for receipt-producing execution inside the exported process context of a delegated interactive shell, so post-handoff verification commands can regain strong child/output/exit evidence without transmitting credentials through the model; then provide a disciplined promotion path for broader shell/terminal/session providers.

**Architecture:** H5 does **not** treat interactive transcript markers as ordinary receipts. The candidate architecture is a short-lived private re-exec mode of the installed ShellBeam binary launched by a qualified shell adapter at a safe prompt boundary; because that helper is intentionally the workload bridge, it may inherit the delegated shell's exported environment and cwd. `context_exec_id` is correlation only, never a bearer capability. The helper proves a separately reviewed claim identity to the daemon, fetches exact durably reserved argv over local IPC, owns a **separate child output channel** (not the mixed delegated pane), spawns/reaps the child, and exits. The daemon promotes the result only under the human-approved evidence contract and current delegated authority generation.

**Tech Stack:** verified H4/H2/H1 delegated handoff stack; same-binary private helper pattern; existing operation/receipt/output/evidence/structured/telemetry pipelines; Unix local IPC/authentication; exact argv process owner; fish/zsh/bash safe-boundary adapters; Go 1.26.6.

**Spec:** Master: `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` frozen at `5351215de2c02ac61ac82751c1680a35744047af`. **HARD DESIGN GATE:** Task 1 must produce and obtain review approval for `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md`; Tasks 2+ are unauthorized until that exact spec is approved. If review changes the candidate contract or wire names, amend this plan before implementation.

## Global Constraints

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

### Task 6: Orchestrate admission, helper/child lifecycle, recovery, and evidence promotion

**Files:**
- Create: `internal/app/contextexec/ports.go`
- Create: `internal/app/contextexec/service.go`
- Create: `internal/app/contextexec/service_test.go`
- Create: `internal/app/contextexec/reconcile.go`
- Create: `internal/app/contextexec/reconcile_test.go`
- Create: `internal/app/daemon/context_exec.go`
- Create: `internal/app/daemon/context_exec_test.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: `internal/core/receipt/result.go`
- Modify: `internal/app/evidence/worker.go`
- Modify: `internal/app/daemon/evidence_worker.go`
- Modify: `internal/app/structuredresult/worker.go`
- Modify: `internal/app/daemon/telemetry_worker.go`

**Interfaces:**
- Public app method `Execute(ctx, Request)` returns existing-style operation view/result with explicit context provenance/evidence quality.

- [ ] **Step 1: RED admission gates.**

Require:

```text
live delegated session
agent is effective owner
request epoch == current epoch
qualified shell identity + safe boundary
privacy release/capture public when model-visible result requested
no active ownership transfer
context-exec capability composed
```

Fail before helper launch otherwise.

- [ ] **Step 2: Implement reserve→launch→claim→spawn→canonicalize ordering.**

Every external mutation follows durable state; response loss uses stored/helper observation, not duplicate launch. The daemon never treats `context_exec_id` alone as authentication. Canonical terminalization must bind the approved helper claim generation, actual executed identity, helper-owned child-output record, spawn/reap facts, and request fingerprint before evidence promotion.

- [ ] **Step 3: RED authority transfer race.**

If human handoff intent rotates epoch before unseen context helper launch, stale execution cannot start. If helper/child was already durably admitted/spawned under old epoch, approved design determines whether transfer waits/fences or records in-flight child; do not silently apply new human ownership to an ambiguous running child.

- [ ] **Step 4: Recovery/failure truth.**

Helper loss after child spawn may be ambiguous/incomplete; no fabricated exit. Helper reconnect/recovery is only as strong as approved design. Context-exec cannot be auto-restarted under a new helper generation unless non-spawn is proven.

- [ ] **Step 5: Promote only approved evidence fields from attributable channels.**

Child spawn/exit/output can feed ordinary evidence only when helper claim/generation, actual executed identity, dedicated child-output attribution, and canonical record all satisfy the human-approved contract. Mixed tmux-pane bytes, shell prompt/background-job output, or a lifecycle-only delegated receipt are never substituted for the helper-owned child stream. Context environment remains provenance; no claim that a specific secret value was observed. If output attribution or executed identity becomes ambiguous, downgrade/fail closed rather than emitting ordinary mechanical verification authority.

- [ ] **Step 6: Focused/race/commit.**

Run context app/daemon/evidence tests/race/devctl; commit `feat: orchestrate high assurance context execution`.

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
