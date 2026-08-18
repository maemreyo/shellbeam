# ShellBeam Human–Agent Interactive Session Handoff Design

**Status:** frozen master architecture; approved for H0 provider-qualification planning only; feature implementation not yet approved

**Execution/design base:** `33fe40999910a08410204993b9edb8f7e58698a5` (`main`, 2026-08-18)

**Scope:** persistent interactive/delegated terminal sessions; automatic local terminal resolution and launch; human/agent ownership handoff; secret-safe local interaction; shell-aware readiness; interactive CLI control; macOS/Linux provider capability discovery; no ChatGPT App/UI dependency

**Companion designs:**

- [ShellBeam V1 Design](./2026-08-13-shellbeam-v1-design.md)
- [Agent Execution Layer Design](./2026-08-13-agent-execution-layer-design.md)
- [A2.5 Environment/Toolchain Fingerprint and Host Process Inspection](./2026-08-15-environment-toolchain-process-inspection-design.md)
- [B1.0 Persistent Runtime and Named Sessions](./2026-08-16-persistent-runtime-named-sessions-design.md)
- [Experimental Safety and Input Observation Providers](./2026-08-14-experimental-provider-design.md)
- [Resource Enforcement Design](./2026-08-18-resource-enforcement-design.md)

---

## 1. Purpose

ShellBeam can already execute commands, own PTYs, accept retry-safe input, capture output, preserve receipts, expose process/environment facts, and keep opt-in **non-TTY** persistent sessions alive across daemon restart through per-session supervisors.

A missing product capability appears when an agent reaches a genuinely human-only interaction inside a local execution context:

```text
Agent is working
    |
    +-- command needs an API key, password, MFA, SSO, hardware key,
    |   license acceptance, destructive confirmation, or complex TUI
    |
    +-- human must interact with the SAME execution context
    |
    +-- context contains cwd, shell state, exported variables, activated
        environments, SSH agents, auth sessions, and running jobs
```

Today an agent may create a PTY that exists only behind `local_shell`. Telling the human to "export the key in that shell" is not a complete UX because the human does not know where that shell is and cannot naturally attach a visible terminal emulator to it.

The desired UX is:

```text
User: "deploy this"

Agent works normally
    |
    +-- human interaction required
    |
    v
ShellBeam automatically opens the terminal family the user is actually using
    |
    v
Human temporarily receives writable control of the exact delegated session
    |
    +-- enters a secret / authenticates / answers a prompt / uses a TUI
    |
    v
ShellBeam proves a safe handback condition
    |
    v
Human terminal may remain OPEN but becomes read-only/observer
    |
    v
Agent continues in the SAME shell/session/context
```

This design calls that product capability **Human–Agent Interactive Session Handoff**. The underlying execution primitive is a **Delegated Interactive Session**.

The feature SHALL preserve ShellBeam's existing evidence-first and fail-closed principles. It must not solve the UX by turning the model into a secret store, by polling shell state indefinitely, by scraping arbitrary terminal UIs, or by making terminal-emulator-specific assumptions part of core execution semantics.

---

## 2. Decision summary

The frozen design is:

1. **tmux is the initial delegated interactive-session provider.** ShellBeam SHALL use tmux Control Mode as the application integration protocol rather than implementing a terminal multiplexer or arbitrary PTY takeover layer.
2. ShellBeam SHALL run delegated sessions on a **ShellBeam-private tmux server/socket**, not the user's normal tmux server, and SHALL start it with a controlled configuration rather than loading arbitrary user tmux configuration.
3. Delegated execution is selected explicitly with `session_mode="delegated_interactive"`. When `session_mode` is present, legacy `tty`/`persistent` fields are absent; when it is absent, existing `tty`/`persistent` semantics remain unchanged. Unsupported peers fail closed before spawn.
4. ShellBeam core SHALL keep three provider dimensions orthogonal:
   - `InteractiveSessionProvider` — tmux/session mechanics and ingress-fence capability;
   - `TerminalLauncherProvider` — Ghostty/iTerm2/WezTerm/kitty/Terminal.app/etc.;
   - `ShellIntegrationProvider` — fish/zsh/bash/Nushell/etc.
   Human-control actions are a separate semantic capability and need not require a fourth Go interface.
5. Terminal selection SHALL be automatic and contextual, with no required static `preferred_terminal`. Existing session affinity wins; then active/recent supported terminal evidence; a bridge-launch terminal is only a freshness-bounded hint, not timeless request origin.
6. Human/agent authority SHALL use a durable `authority_epoch`. A transfer rotates the epoch as soon as transfer intent is durably accepted. Previously accepted mutations replay their durable outcomes; previously unseen stale-epoch mutations never execute.
7. **Owner state, ingress fencing, transfer boundary, privacy release, capture state, and provider-observed authority are distinct correctness dimensions.** Combined names such as `HUMAN_OWNED`, `RECLAIM_PENDING`, or `PRIVATE_ABORTED` are derived status/UX projections, not the canonical durable state.
8. Secret-bearing handoff SHALL create a private output interval before human write authority. `TransferBoundary` is sufficient for a new owner to begin input; `PrivacyReleaseProof` is separately required before model-visible capture resumes.
9. Privacy is defined over **every model-visible observation path**, not over a presumed pane-local tmux primitive. H0 SHALL qualify a topology that prevents private session A from leaking while not silently suppressing unrelated public sessions B/C, including reconnect/observer replacement faults.
10. Readiness SHALL be event-driven and scoped to an active handoff. There are no busy loops, per-session pollers, permanent shell hooks, or capture-pane polling. Shell adapters provide automatic readiness; manual HumanControl remains shell-independent.
11. HumanControl actions (`ready`, `abort`, `status`, and state-appropriate `resume`/`terminate`/optional `request_control`) MUST remain **locally reachable** without sending control text into the delegated pane. The design does not require arbitrary key bindings to work inside a read-only tmux client.
12. Infrastructure attachment is presentation, not environment synchronization. Human attach, switch, daemon reattach, and control observation SHALL preserve the delegated session environment unless a future explicit environment-sync capability says otherwise; the tmux provider must qualify `-E`/equivalent behavior.
13. A human may leave a terminal open after reclaim only when the exact human ingress is fenced. For tmux V1 this may project as a passive read-only observer; ShellBeam does not promise cross-terminal copy-mode/history navigation semantics.
14. Simple interactive CLI input (`y/n`, Enter, menu choice, control bytes) can remain agent-driven while the agent owns the session. Secret, consequential, unknown, or complex interactions can hand off to the human.
15. Arbitrary adoption of an already-running external Ghostty/fish PTY is **not** part of V1. A future Linux-only experimental provider may investigate reptyr-like adoption, but it is not the product foundation and cannot supply macOS parity.
16. The shipped product SHALL have no dependency on the ShellBeam source checkout path, developer Homebrew layout, or this machine's shell/terminal configuration.

---

## 3. Problem scenarios

### 3.1 Exported credential

```text
Agent
  -> runs tunnel-client doctor
  -> receives missing CONTROL_PLANE_API_KEY
  -> requests secret handoff for delegated session S

ShellBeam
  -> opens the user's contextually selected terminal
  -> attaches human client H to S as writable
  -> suppresses model-visible output for S

Human
  -> supplies credential in S

Shell integration
  -> at a safe command/prompt boundary proves:
       CONTROL_PLANE_API_KEY is exported and non-empty
  -> sends only requirement-satisfied metadata

ShellBeam
  -> makes H read-only
  -> restores public output observation from the new boundary
  -> returns S to agent ownership

Agent
  -> runs tunnel-client doctor again in S
  -> verifies the capability, not merely secret presence
```

### 3.2 CLI confirmation

```text
CLI: Continue? [y/N]
```

If the agent has enough context and policy authority, it may send `y\n` itself. ShellBeam does not need to hand off merely because stdin is interactive.

### 3.3 Secret/password/OTP prompt

```text
Password:
One-time code:
Touch your security key...
```

The session switches to human ownership. Secret input is private. Completion is automatic only when ShellBeam has a typed safe completion predicate. Otherwise explicit human handback is required.

### 3.4 Complex TUI

```text
> Development
  Staging
  Production
```

Raw key input is mechanically possible, but ShellBeam SHALL NOT claim semantic understanding of arbitrary TUIs. Unknown or high-risk TUIs default to human handoff.

### 3.5 Human leaves the terminal open

The terminal window does not need to close. After safe reclaim, ShellBeam toggles that exact human tmux client to read-only/observer. The agent can continue while the human watches live output. A later handoff can toggle that client writable again or launch/reveal a new client if the original client no longer exists.

---

## 4. Existing capabilities retained, not reimplemented

This design SHALL reuse rather than recreate:

- existing `local_shell` one-tool MCP surface;
- operation/session IDs, idempotent start semantics, input offsets, kill IDs, receipts, output cursors, Event Journal, evidence, structured results, telemetry, reproduction, and workspace/activity identity;
- current direct process/PTY ownership through `github.com/creack/pty` for non-delegated starts;
- B1.0 persistent non-TTY supervisor semantics;
- A2.5 secret-safe environment-presence concepts and host process inspection;
- capability discovery and fail-closed optional-provider patterns;
- current-user OS authority and Unix-socket peer authentication;
- platform-specific adapters behind inward-facing app/core ports.

In particular, this feature SHALL NOT replace `creack/pty` for ordinary direct PTYs and SHALL NOT turn tmux into ShellBeam's global process runtime.

---

## 5. Goals

The initial design SHALL:

- let an agent create a persistent interactive shell/session that can be shared with a human;
- preserve shell-local execution context across human and agent turns;
- let ShellBeam automatically open a suitable local terminal emulator without requiring a per-user preferred-terminal setting;
- let the human and agent share the same delegated shell while never being writable at the same time under normal ShellBeam control;
- let a human leave the terminal window open while the agent safely resumes;
- support event-driven, bounded readiness checks for shell-aware handoffs;
- keep secret values out of model-visible output, ordinary logs, durable receipts, Event Journal payloads, and shell requirement metadata;
- support agent-driven ordinary interactive input such as yes/no and Enter while the agent owns the session;
- degrade explicitly when the terminal, shell, or session provider lacks a capability;
- avoid permanent modification of `.zshrc`, `.bashrc`, `config.fish`, Nushell config, or tmux config;
- preserve old direct/PTTY/persistent-non-TTY behavior unless the new capability is explicitly selected;
- survive ShellBeam daemon restart when the tmux provider/session remains alive and exact provider identity can be re-proven;
- avoid host-reboot continuity claims;
- remain source-tree-independent for installed users;
- expose enough capability facts that the agent never needs to guess whether a handoff, shell watcher, terminal launcher, or read-only reclaim is supported.

---

## 6. Non-goals

The initial design does NOT:

- attach to or steal an arbitrary already-running terminal/PTY that ShellBeam did not create;
- make reptyr a required dependency or a cross-platform foundation;
- add a ChatGPT App, custom ChatGPT UI component, browser extension, or web-to-local deep link;
- require a user to choose and save a preferred terminal emulator;
- provide cryptographic isolation of secrets from an agent that already has unrestricted same-user `local_shell` authority;
- inspect or return arbitrary process-environment values;
- parse arbitrary TUI screens into semantic widgets;
- automatically approve destructive CLI confirmations;
- make terminal history a trusted evidence source;
- claim byte-complete output across intentionally private human intervals;
- install tmux or terminal emulators silently;
- require tmux for ordinary direct ShellBeam commands;
- modify user dotfiles permanently;
- support Windows/ConPTY in the initial implementation;
- promise full PowerShell integration merely because PowerShell may be installed on macOS/Linux;
- provide host-reboot persistence;
- make tmux client read-only flags a security boundary against the same trusted OS user;
- turn the delegated interactive shell into a general scheduler/workflow engine;
- silently migrate a running direct PTY into delegated mode after the fact.

---

## 7. Terminology

### 7.1 Delegated interactive session

A ShellBeam-owned persistent interactive execution context whose PTY/session lifetime is provided by a qualified interactive-session provider and which can be attached to both an agent control client and a human terminal client.

### 7.2 Human–agent handoff

A bounded ownership transition that temporarily grants writable terminal input authority to one designated human client and later returns input authority to the agent.

### 7.3 Terminal launcher

A platform/provider adapter that can identify and launch/reveal a terminal emulator with an exact ShellBeam-generated argv target. Examples: Ghostty, iTerm2, WezTerm, kitty, Terminal.app.

### 7.4 Shell integration

Ephemeral session-local logic for a known interactive shell runtime. It may observe safe command boundaries, evaluate closed typed readiness predicates, and send a boolean/result event to ShellBeam.

### 7.5 Private output interval

A declared time interval during which terminal output for a delegated pane is intentionally not delivered to model-visible capture because human input may contain secrets or other private material.

### 7.6 Ownership

`AGENT_OWNED` and `HUMAN_OWNED` are ShellBeam control-plane arbitration states. They are not Unix user/security identities. Both parties operate inside the current local user's trust boundary.

### 7.7 Authority epoch

A monotonically changing authorization lifetime for delegated-session mutations. `input_offset` remains the ordering/idempotency coordinate for agent-submitted input; `authority_epoch` determines whether a previously unseen control mutation still has authority to execute.

### 7.8 Ingress fence

A proof that no **new** input from a specified authority can be admitted after the fence point. It does not claim that already-delivered bytes have drained through the kernel PTY or foreground application.

### 7.9 Transfer boundary

A proof or explicitly typed attestation sufficient to let the next owner begin input without racing the previous interaction. Its quality may be `shell_boundary`, `process_boundary`, `provider_ordered`, or `human_attested`; the exact admitted qualities are capability/policy decisions.

### 7.10 Privacy release proof

A proof that future output may become model-visible again without exposing bytes from a private interval. A human `ready` signal by itself may establish a transfer boundary for a manual flow but SHALL NOT by itself establish privacy release for a secret flow.

### 7.11 HumanControl capability

A shell-independent local control surface for human actions such as ready, abort, status, resume, terminate, or optional request-control. Required actions must remain locally reachable in the authority states where they are needed without injecting control text into the delegated pane.

### 7.12 Capability verification

A real post-handoff command proving that the desired authority works, for example `tunnel-client doctor`. It is stronger than merely observing that an environment variable exists.

---

## 8. Alternatives considered

### A. tmux Control Mode provider — **recommended**

ShellBeam creates delegated sessions inside a private tmux server and controls them through tmux's application/control protocol.

Advantages:

- mature PTY/session multiplexing;
- detach/reattach and multiple clients already solved;
- stable session/window/pane IDs;
- explicit client read-only flags;
- control-mode asynchronous output and flow control;
- available on macOS and Linux;
- avoids reimplementing terminal multiplexing and attach semantics.

Costs:

- external runtime dependency;
- TERM/terminfo behavior differs from direct terminal execution;
- tmux is trusted provider infrastructure and its socket is effectively full authority for the same user;
- ShellBeam must qualify supported tmux versions and control-mode behavior.

### B. Build a native ShellBeam PTY broker/multiplexer — rejected for V1

ShellBeam could keep PTY masters itself and implement attachable human clients, screen state, resize arbitration, replay, detach/reconnect, terminal launch, and private intervals.

This duplicates a mature multiplexer, greatly enlarges correctness/security surface, and conflicts with reuse-first provider design.

### C. Adopt arbitrary external shells with reptyr/ptrace-like mechanics — deferred/experimental

This is attractive for "take over the exact shell already running in Ghostty" but is platform-specific, invasive, and not a portable macOS/Linux contract. It may later exist as a Linux-only experimental provider with explicit weaker semantics. It SHALL NOT define the core abstraction.

---

## 9. High-level architecture

```text
                             Agent / MCP
                                 |
                           local_shell actions
                                 |
                                 v
+------------------------------------------------------------------+
|                         ShellBeam daemon                         |
|                                                                  |
|  Delegated Session Service                                       |
|      |                                                           |
|      +-- Handoff Controller / ownership state                     |
|      +-- Capability Resolver                                      |
|      +-- Private-output barriers                                  |
|      +-- Event Journal / bounded status                           |
|      |                                                           |
|      +---- InteractiveSessionProvider -----------------------+     |
|      |                                                     |     |
|      |                                                   tmux     |
|      |                                                     |     |
|      +---- TerminalResolver --> TerminalLauncherProvider    |     |
|      |       | Ghostty / iTerm2 / WezTerm / kitty / ...    |     |
|      |                                                     |     |
|      +---- ShellIntegrationProvider                        |     |
|              fish / zsh / bash / nu / degraded             |     |
+-------------------------------------------------------------|----+
                                                              |
                                           private tmux socket |
                                                              v
                                                  +----------------+
                                                  | private tmux   |
                                                  | server         |
                                                  +-------+--------+
                                                          |
                                                      pane/session
                                                          |
                                                          v
                                                interactive shell
                                                          |
                                         +----------------+----------------+
                                         |                                 |
                                  Agent control client              Human terminal
                                                                   writable OR read-only
```

Terminal emulator choice and shell language are orthogonal. There SHALL NOT be product classes such as `GhosttyFishAdapter` or `WezTermZshAdapter`.

---

## 10. Provider decomposition

### 10.1 `InteractiveSessionProvider`

Conceptual semantic capabilities:

```text
probe/create/inspect/close exact provider session
attach model-visible control observer(s)
write agent input under current authority
set/observe exact human-client writability
FenceHumanIngress(session, client, authority_epoch) -> IngressFenceProof
establish/release privacy observation under qualified topology
preserve delegated environment across attach/switch/reattach
```

The exact Go shape belongs in H1 planning after H0 qualification. Core semantics SHALL NOT mention tmux commands directly and SHALL NOT require the provider to claim application-level quiescence that it cannot prove.

### 10.2 `TerminalLauncherProvider`

Conceptual capabilities:

```text
identify_running_instance
launch_argv
reveal_existing_client?   # optional
bundle/application identity
platform
quality
```

A launcher accepts a prevalidated argv target. It does not receive arbitrary shell snippets from the model.

### 10.3 `ShellIntegrationProvider`

Conceptual capabilities:

```text
shell_identity
safe_boundary_events
requirement_predicates[]
private_secret_prompt?
ephemeral_install
cleanup
```

Shell integration produces automatic readiness/transfer/privacy evidence where qualified. It does not own manual HumanControl. No adapter may silently run syntax for a different shell family.

### 10.4 HumanControl semantics

HumanControl is a first-class semantic capability even if implemented by the interactive-session provider and local attach wrapper rather than a fourth Go interface.

Required actions are state-dependent:

```text
human writable:        ready, abort, status
human fenced/private:  resume, terminate, status
agent-owned observer:  status, optional request_control
```

"Reachable" means the user has a local control path that cannot be mistaken for delegated pane stdin. H0/H2 may qualify tmux-native bindings while writable, detach-to-local-control while read-only/fenced, or an attach-side ingress/control gate. The master design does not precommit the transport.

---

## 11. Session classes and compatibility

Legacy execution remains defined by existing fields when `session_mode` is absent:

| Legacy request | Meaning |
|---|---|
| `tty=false, persistent=false` | direct pipe process |
| `tty=true, persistent=false` | direct PTY process |
| `tty=false, persistent=true` | B1.0 persistent non-TTY supervisor |
| `tty=true, persistent=true` | remains rejected |

The new delegated class is explicit:

```text
session_mode = delegated_interactive
```

Rules:

```text
session_mode absent
    -> existing tty/persistent schema and behavior

session_mode present
    -> legacy tty/persistent fields MUST be absent
    -> named mode is the single semantic source of truth

session_mode=delegated_interactive
    -> negotiated capability required
    -> no fallback to direct PTY or B1.0 persistent non-TTY
```

An old peer that does not recognize `session_mode` fails closed under schema/capability negotiation before spawn. Future modes such as `adopted_interactive` or `remote_delegated` may obtain explicit vocabulary without overloading Boolean axes.

---

## 12. Delegated-session start semantics

Conceptual request:

```json
{
  "action": "start",
  "operation_id": "op-tunnel-shell",
  "command": "exec fish",
  "cwd": "/absolute/project/path",
  "session_mode": "delegated_interactive",
  "session_name": "tunnel-dev"
}
```

Rules:

- capability negotiation MUST prove delegated-interactive support before the request is admitted;
- ordinary request fingerprint/idempotency semantics still apply;
- the reservation is durable before provider/session creation;
- retry of the same `operation_id` MUST resolve the same delegated session or exact failure; it never creates a second shell;
- changed delegated/direct mode under the same `operation_id` is a conflict;
- tmux server/session/pane details remain private provider metadata;
- public identity remains ShellBeam `session_id`; tmux IDs are provider facts, not public control authority;
- `session_name` retains the B1.0 human-friendly alias semantics and does not become authorization;
- the delegated session has a durable `authority_epoch` initialized at creation and rotated on accepted ownership-transfer intent;
- the session is persistent across ShellBeam daemon restart only while the exact provider session remains alive and can be re-proven;
- host reboot is outside the guarantee.

---

## 13. New handoff actions inside `local_shell`

This design keeps one MCP tool and adds action branches.

### 13.1 `handoff.request`

Conceptual shape:

```json
{
  "action": "handoff.request",
  "handoff_id": "handoff_...",
  "session_id": "...",
  "reason": "credential_required",
  "privacy": "secret",
  "completion": {
    "kind": "environment_exported_nonempty",
    "name": "CONTROL_PLANE_API_KEY"
  }
}
```

Closed `reason` family initially:

```text
credential_required
authentication_required
human_confirmation
complex_interactive_ui
manual_intervention
```

Closed `privacy` family:

```text
standard
secret
```

Initial completion strategies:

```text
environment_exported_nonempty
manual_ready
```

No caller-supplied arbitrary shell predicate, regex, script, command, or output scraper is allowed as a completion condition.

`handoff_id` is the stable idempotency key for the human-interaction request. ShellBeam durably binds it before terminal launch so a lost tool response cannot cause an unbounded number of duplicate terminal windows on retry.

### 13.2 `handoff.wait`

Conceptual shape:

```json
{
  "action": "handoff.wait",
  "handoff_id": "handoff_...",
  "yield_time_ms": 30000
}
```

This waits on daemon state/event notification, not a polling loop. The hard maximum wait is capability-advertised. Timeout returns current handoff state and is retry-safe.

### 13.3 `handoff.abort`

Aborts further human handoff authority under a stable request identity. Abort is not rollback: bytes already sent to the PTY/application cannot be undone. For a secret/private handoff, abort fences further human ingress but does **not** by itself return control to the agent or release model-visible capture. The handoff remains fail-closed until an admissible transfer/privacy boundary is proven, the human resumes locally, or the session is explicitly terminated. It does not kill the delegated session by itself.

### 13.4 `inspect.handoff`

A bounded read-only action returns current handoff metadata when event replay alone is inconvenient.

No action returns human keystrokes or secret content.

---

## 14. Canonical handoff state model

The canonical durable state is **orthogonal dimensions**, not one giant cross-product enum.

Conceptual record:

```text
HandoffState
    phase
    authority_epoch

    desired_owner
    provider_owner_state

    agent_ingress
    human_ingress

    transfer_boundary

    privacy_state
    privacy_release
    capture_state

    human_client
    provider_generation
```

Hard distinction:

```text
OWNER
!= INGRESS FENCE
!= TRANSFER BOUNDARY
!= PRIVACY RELEASE
!= CAPTURE STATE
!= PROVIDER AUTHORITY
```

Names such as `AGENT_OWNED`, `HUMAN_OWNED`, `AGENT_FENCING`, `HUMAN_FENCING`, `RECLAIM_PENDING`, `RECLAIM_BLOCKED`, or `PRIVATE_ABORTED` MAY be exposed as derived UX/status projections over this canonical record. They are not the source of correctness truth.

Typical transfer projection:

```text
AGENT_OWNED(epoch=N)
    |
    | transfer intent durably accepted; epoch becomes N+1
    v
AGENT_FENCING
    | agent ingress fenced
    | admissible transfer boundary established
    v
HUMAN_CONNECTING
    | secret privacy established first if required
    v
HUMAN_OWNED
    |
    +-- ready --> HUMAN_FENCING --> transfer/provider reconciliation --> agent authority
    |
    +-- abort --> HUMAN_FENCING --> derived PRIVATE_ABORTED if privacy remains private
```

For secret flows, agent input authority may become safe before model-visible output becomes safe. Public capture therefore follows `PrivacyReleaseProof`, not merely the owner projection.

A durable desired transition and an external provider mutation are not atomic. On restart/recovery ShellBeam reconciles durable desired authority, current epoch, and fresh provider observation; mismatch means fail closed rather than "probably owned".

---

## 15. Input ownership, generations, and arbitration

The central invariant is:

> At most one ShellBeam-recognized actor class has admitted writable interactive input authority at a time, and no previously unseen mutation from an expired authority generation may execute.

### 15.1 Idempotency before authority

Delegated control mutation admission follows this order:

```text
mutation M arrives
    |
    v
lookup exact durable mutation identity
    |
    +-- known -> replay exact prior outcome; do not execute again
    |
    +-- unknown
           |
           v
       authority_epoch == current?
           | no -> stale_control_generation
           v yes
       current authority permits mutation?
           | no -> session_control_not_owned
           v yes
       durably reserve mutation
           |
           v
       provider delivery
```

For agent input, `input_offset` remains the ordering/idempotency coordinate of **agent-submitted** input and is not a terminal byte offset. Human bytes are intentionally absent from that ledger. `authority_epoch` is the authorization lifetime.

Every previously unseen mutation capable of changing delegated-session control or lifetime SHALL pass its applicable generation/authority policy. The implementation plan MUST enumerate at least write, resize, signal, kill, handoff transitions, HumanControl signals, and provider-authority mutations so one lane cannot bypass generation semantics.

### 15.2 Epoch rotation

When ownership-transfer intent is durably accepted, ShellBeam rotates `authority_epoch` **before** granting the next actor authority. Previously accepted old-epoch mutations remain replayable from durable state; previously unseen old-epoch mutations are stale immediately.

### 15.3 Ingress fencing

`FenceHumanIngress`/equivalent proves only:

```text
no NEW human input can be admitted after the fence point
```

It does not claim that pre-fence bytes have drained through tmux, the PTY, kernel tty queues, or the foreground application. Agent ingress has the symmetric requirement when transferring to the human.

The provider must qualify this ingress-fence semantic. If native tmux primitives cannot prove it, H0 is an architecture fork gate and ShellBeam may need an attach-side ingress gate before requalification.

### 15.4 Transfer boundary

A transfer boundary separately establishes when the new owner may begin input. Qualified producers may include shell/prompt boundaries, foreground-process boundaries, provider-ordered boundaries, or explicit human attestation under a policy that permits it.

Human `ready` can therefore support unknown-shell/TUI manual handback without pretending to be a secret-output proof.

### 15.5 Provider authority reconciliation

Conceptually:

```text
EffectiveAuthority =
    Intersect(
        DurableDesiredAuthority,
        ProviderObservedAuthority,
        CurrentAuthorityEpoch
    )
```

On mismatch:

```text
agent_write  = denied
human_write  = denied unless explicitly re-proven safe
public_capture = denied when privacy is ambiguous
```

No PID/name/process guess substitutes for exact provider proof.

---

## 16. Why the user does not need to close the terminal

Terminal-window lifetime and write ownership are separate.

```text
Human terminal process alive
        |
        +-- HUMAN_OWNED  => writable
        |
        +-- AGENT_OWNED  => attached observer, read-only
```

Therefore completion is an ownership transition, not a terminal detach event.

A later handoff SHOULD reuse/reveal the existing attached terminal client when the terminal provider can do so reliably. Otherwise it may launch another exact client and keep all non-designated clients fenced/read-only. In V1, `observer` means **passive live observation**; ShellBeam does not promise tmux copy-mode, history navigation, arbitrary key bindings, or terminal-emulator scroll behavior as a cross-provider contract.

---

## 17. Terminal resolution: session affinity plus freshness

A static `preferred_terminal=ghostty` is not the primary design because users may switch among terminal emulators throughout the day.

The resolver SHALL use contextual evidence in this order:

1. **Existing human client for this delegated session**, if still alive and revealable.
2. **Currently active supported terminal application**, when the foreground application is itself a terminal.
3. **Most recently activated supported terminal application**, from a qualified event-driven local activity registry.
4. **Fresh validated bridge/session affinity hint**, when available.
5. **Single unambiguous running supported terminal**, when exactly one candidate exists.
6. **Qualified platform fallback launcher**, when the platform provides a deterministic safe default.
7. Otherwise return `terminal_launcher_unavailable` and expose the exact local attach command as degraded UX.

The resolver SHALL NOT simply ask "what app is frontmost?" because during a ChatGPT Web request the frontmost application is commonly the browser.

### 17.1 Bridge-launch terminal hint

The long-lived ShellBeam bridge/tunnel process may inherit terminal facts and process ancestry from the terminal in which it was launched. That is **bridge-launch affinity**, not proof of the terminal that originated a later ChatGPT request.

A bounded hint may include:

```text
observed_at
terminal_identity
terminal version?
controlling tty identity?
evidence_source
freshness
```

These are UX-selection hints, never execution authorization or timeless preference. A raw environment string such as `TERM_PROGRAM` is not sufficient to authorize execution of an application path.

### 17.2 Recent-terminal activity

On platforms with a qualified native activation-event API, ShellBeam may keep a tiny in-memory registry of recent supported terminal activations. The event source is shared and event-driven; it is not one watcher per terminal or per session and performs no timer polling.

Persistent detailed app-usage history is unnecessary. At most the minimal current/recent terminal identity needed for UX selection should be retained, with freshness metadata, and it may remain memory-only.

---

## 18. Terminal launcher providers

Terminal providers are presentation adapters only. They do not own shell semantics or session authority.

Initial product targets SHOULD include provider qualification for common terminals, for example:

### macOS target set

```text
Ghostty
iTerm2
WezTerm
Terminal.app
kitty (when installed and launcher mechanics qualify)
```

### Linux target set

```text
Ghostty
WezTerm
kitty
Alacritty
GNOME Terminal
Konsole
```

This list is a target matrix, not a claim that every adapter must land in the first implementation slice. Capability discovery reports the actual qualified subset on the current machine.

A launcher SHALL:

- resolve a known installed/running terminal identity;
- launch a new window/tab or reveal an existing designated client where supported;
- invoke an exact local ShellBeam attach argv;
- never interpolate model-provided text into a shell command;
- never hardcode `/Users/trung.ngo`, Homebrew prefixes, source-tree paths, or application locations;
- report exact launch quality and failure reason;
- avoid repeated launch on retry of the same `handoff_id` when a prior launch is already proven;
- treat unknown launch outcome as unknown rather than blindly spawning duplicates.

---

## 19. Human terminal attach command

The terminal provider launches the installed ShellBeam executable into a local human-client attach mode conceptually like:

```text
shellbeam session attach --handoff-id <opaque-id>
```

The user normally never types this command; the terminal launcher supplies it automatically.

Rules:

- the launcher resolves the current installed ShellBeam executable using runtime identity (for example `os.Executable()` or an equivalent installed-path mechanism), not the repository path;
- the local attach command authenticates to the existing daemon with normal same-user local IPC rules;
- attachment is presentation, not environment synchronization: attach/switch/reattach MUST preserve the delegated session environment unless an explicit future environment-sync capability is selected; tmux qualification must prove `attach-session -E`, `switch-client -E`, or equivalent behavior;
- `handoff_id` selects an already-durably-bound pending handoff and cannot create arbitrary execution;
- the attach helper does not receive secret values;
- terminal title/status may identify the ShellBeam session/handoff in human-friendly text but never display secret values.

A manual fallback may print the attach command when automatic launch is unavailable.

---

## 20. Shell runtime detection

Shell identity is independent from terminal identity.

Examples that must compose without special-case cross-products:

```text
Ghostty + fish
Ghostty + zsh
WezTerm + fish
iTerm2 + bash
kitty + Nushell
```

ShellBeam SHALL NOT use `$SHELL` alone as proof of the currently active interactive shell. `$SHELL` is commonly the login shell and may differ from the process actually parsing input.

For a ShellBeam-created delegated session, the preferred authority is the exact shell executable chosen/observed at session creation plus provider/process facts. If the user starts a nested or replacement shell, ShellBeam must re-probe before injecting integration syntax or degrade safely.

Unknown shell means:

```text
interactive_io = available
shell_aware_readiness = unavailable
```

It never means "try Bash syntax and hope".

---

## 21. Shell integration capability levels

The capability model SHOULD distinguish:

```text
L0  PTY_ONLY
    raw input/output only

L1  INTERACTIVE
    ordinary keys, y/n, Enter, Ctrl bytes

L2  SHELL_AWARE
    known shell identity + safe command/prompt boundary

L3  REQUIREMENT_AWARE
    closed typed readiness predicates with no secret values

L4  FULL_HANDOFF
    terminal auto-launch + ownership arbitration + safe reclaim
```

Examples:

```text
qualified fish + qualified terminal + tmux     -> L4
qualified zsh + qualified terminal + tmux      -> L4
bash with shell integration but no auto-launch -> L3
unknown shell + tmux                           -> L1
no delegated-session provider                  -> unavailable
```

Capabilities are computed from actual provider intersection rather than from a hardcoded platform table.

---

## 22. Shell-specific handling

Different shells require different integration mechanics.

### 22.1 fish

A fish adapter may use temporary event functions and `set -q`/export-state checks. The authoritative readiness event must occur at a safe post-command/prompt boundary, not merely at the instant a variable assignment mutates shell state.

A native variable-change event MAY be used as an optimization/advisory signal, but ownership must not be reclaimed until the shell reaches the required safe boundary.

### 22.2 zsh

Use composable temporary zsh hook mechanisms such as `precmd`/`preexec` integration. Do not overwrite the user's existing functions. Install and remove only ShellBeam's own hook entry.

### 22.3 bash

Bash integration must handle the actual supported `PROMPT_COMMAND` representation/version semantics and compose with existing prompt tooling. It may not replace user prompt commands wholesale.

### 22.4 Nushell

Nushell has a different typed environment/config/hook model and is not a Bourne-shell syntax variant. Support requires an independently version-qualified adapter. Until qualified, it degrades rather than receiving Bash/zsh snippets.

### 22.5 other shells

PowerShell, xonsh, elvish, dash/sh interactive modes, and other runtimes are capability-gated separately. Presence on the machine does not imply shell-aware handoff support.

---

## 23. Ephemeral integration only

ShellBeam SHALL NOT modify persistent user configuration files merely to support handoff.

Forbidden product behavior includes automatically editing:

```text
~/.zshrc
~/.bashrc
~/.bash_profile
~/.config/fish/config.fish
Nushell config files
user tmux config
terminal emulator config
```

The shell adapter installs session-local ephemeral functions/hooks only in the delegated shell. On completion, cancellation, expiry, or session teardown it removes its own integration where possible.

Existing hooks are composed, not replaced.

---

## 24. Requirement predicates, transfer boundaries, and privacy release

A requirement watcher is created only for an active handoff.

Initial environment predicate:

```text
environment_exported_nonempty(name)
```

It can report only:

```text
satisfied = true | false
quality = exact_shell_adapter | manual
safe_boundary = true | false
```

It SHALL NOT report:

```text
value
length beyond the boolean non-empty predicate
hash
digest
prefix/suffix
character class
command history line containing the value
```

The variable name itself may be known to the requesting agent because the failing CLI already identified the prerequisite. The privacy invariant concerns values and deterministic secret-derived material.

### 24.1 Transfer boundary is not privacy release

A completion/readiness event answers one or more typed questions; it is not automatically all authority/privacy proof at once.

```text
TransferBoundary
    -> sufficient for the next owner to begin input under the selected policy

PrivacyReleaseProof
    -> sufficient for future output to become model-visible again
```

A shell prompt after a known command may produce both. A foreground-process exit may produce a process boundary. A human `ready` action may be accepted as a `human_attested` transfer boundary for manual/unknown-shell flows, but **human attestation alone SHALL NOT release secret/private capture** because an application may emit sensitive output later.

### 24.2 Presence is not validity

```text
secret material present != capability works
```

After reclaim, the agent SHOULD run the real capability probe (`tunnel-client doctor`, `aws sts get-caller-identity`, etc.) when appropriate. A failed probe can trigger another handoff without exposing the credential.

---

## 25. Event-driven resource model

The feature SHALL have no periodic shell polling.

Forbidden patterns:

```text
while true; check env; sleep 100ms
one watcher process per delegated session
one timer loop per environment variable
capture-pane every 500ms
continuous process-table scans to infer terminal choice
```

Required shape:

```text
handoff active
    |
    +-- install one temporary shell integration
    |
    +-- shell naturally reaches safe boundary
            |
            +-- O(1) typed predicate check
            +-- one local readiness notification
            +-- integration removes itself
```

The readiness notification may invoke the installed ShellBeam binary once to send a tiny local IPC event. No helper process remains resident after the event.

A notifier/control helper launched from a secret-bearing delegated shell MUST NOT inherit the delegated shell environment wholesale. The shell adapter SHALL launch it with a minimal allowlisted environment sufficient only for local IPC/runtime resolution; the watched credential and unrelated shell exports are excluded. If a shell/provider combination cannot construct such an environment safely, it SHALL use an already-open local IPC descriptor or degrade to manual completion rather than spawning a helper that inherits secret-bearing state.

`handoff.wait` waits on a daemon condition/event channel and consumes effectively zero CPU while idle. Manual HumanControl is shell-independent and SHALL NOT rely on typing `shellbeam handoff ready` into pane stdin; its transport is qualified separately and leaves no permanent watcher.

---

## 26. Secret-bearing handoff and private output intervals

This is a hard privacy requirement.

### 26.1 Why ordinary capture is unsafe

If a human types:

```text
export CONTROL_PLANE_API_KEY=<secret>
```

into a normal echoed terminal line, the PTY may render the typed bytes. A model-visible Control Mode observer could receive those bytes even though the model never explicitly asked to read the environment. Therefore merely "not calling env" is insufficient.

### 26.2 Privacy is an observation-path contract

The master design SHALL NOT assume that tmux provides a pane-scoped "make private" primitive. For tmux, `no-output` is a **client-scoped** Control Mode flag. H0 must therefore qualify the observer topology/mechanism rather than smuggling a topology assumption into core semantics.

For every private delegated session:

- every model-visible observation path is identified;
- privacy is established **before** any such path can receive private bytes;
- making private session A private cannot leak A through another observer/path;
- making A private cannot silently suppress correctly public B/C output;
- observer replacement, overlap, reconnect, and daemon recovery cannot create an exposure window;
- no private interval is reconstructed from tmux history, `capture-pane`, or replay.

H0 may qualify, for example:

```text
A. one model-visible Control Mode observer per delegated session
B. shared observer plus a daemon privacy demux that drops private pane output before persistence/model exposure
C. another provider mechanism with equivalent proven semantics
```

The master design freezes the isolation requirements, not A/B/C.

### 26.3 Entering a secret interval

Before granting human write authority for `privacy=secret`:

```text
agent ingress fenced
    -> privacy observation established and acknowledged
    -> exact human ingress enabled
```

During the private interval:

- terminal/pane interaction remains visible locally to the human;
- model-visible output for the private session is suppressed under the qualified topology;
- human input bytes are not recorded in the agent input ledger;
- no raw private bytes are persisted in ordinary output logs, receipts, Event Journal, evidence, repro, or telemetry;
- terminal history remains local provider state and is never a recovery source across the private barrier.

### 26.4 Ready/reclaim

Human ready and automatic readiness are **completion candidates**, not automatic privacy release.

Safe control transfer requires:

```text
old-owner IngressFenceProof
+ admissible TransferBoundary
+ ProviderAuthorityReconciled
```

Public capture resumes only after:

```text
PrivacyReleaseProof
+ a new forward-only observation boundary
+ privacy topology re-established as public
```

Therefore a manual/unknown TUI may return input authority to the agent under a permitted human-attested transfer boundary while capture remains private until an independent privacy-release proof exists. This is preferable to either exposing output unsafely or making manual fallback impossible.

### 26.5 Abort handoff

`handoff.abort` means **stop further human authority**, not "undo interaction" and not "return to agent".

For a secret interval, after abort and human ingress fencing, the derived status may be `PRIVATE_ABORTED` with:

```text
human_write = false
agent_write = false unless/until transfer authority is separately proven
model_capture = false
```

The flow exits only through an admissible transfer/privacy proof, local resume, or explicit local termination. Already-sent bytes may remain in PTY/application state and are never presumed reversible.

### 26.6 Crash/restart during a private interval

If the daemon/control observer restarts while a secret interval is active, durable privacy remains private. Every replacement model-visible observation path must be private **from its first possible output byte** before reconciliation. An observer must not attach publicly, receive output, and only then enable suppression.

If exact privacy topology/authority cannot be re-proven, agent input and public capture remain denied and the transcript records an explicit private/ambiguous omission. Privacy wins over completeness.

---

## 27. Secret-entry UX

Where a shell adapter can safely provide it, ShellBeam SHOULD prefer a session-local hidden-input helper instead of asking the human to type a literal `export NAME=secret` line.

Conceptual UX:

```text
ShellBeam requires CONTROL_PLANE_API_KEY.
Paste the value below. Input will not be echoed.

Secret: ********
```

The helper executes inside the interactive shell so it can set/export the variable in the parent shell context using shell-native semantics.

This is shell-specific and must be independently qualified. If unavailable, the private-output barrier still protects normal ShellBeam capture while the human uses the shell manually.

ShellBeam never receives the secret as an MCP argument.

---

## 28. Interactive CLI behavior

Interactive input capability does not imply every prompt should be human-owned.

### 28.1 Agent-manageable prompts

Examples:

```text
Continue? [y/N]
Press Enter to continue
Choose [1-3]
```

If agent policy/context permits, the agent may answer through normal delegated-session input while `AGENT_OWNED`.

ShellBeam's role is reliable input delivery and ownership enforcement, not deciding whether `yes` is wise.

### 28.2 Consequential confirmations

Examples:

```text
Delete production database? yes/no
Overwrite all existing files?
Rotate live credentials?
```

Human approval/policy remains an agent/product safety concern. ShellBeam MUST NOT auto-classify a textual `yes/no` prompt as harmless merely because the syntax is simple.

### 28.3 Secret-bearing prompts

Password, OTP, recovery code, private key passphrase, and similar prompts default to human handoff/private output.

### 28.4 Unknown/full-screen TUI

ShellBeam can transport raw keys but does not claim semantic UI understanding. Unknown TUI interaction defaults to human ownership when confident agent control is unavailable.

---

## 29. Command-boundary evidence inside a delegated shell

A delegated shell is different from an ordinary ShellBeam `start` because commands typed into the shell are children of that shell, not independent daemon-spawned operations.

This design SHALL be explicit about the evidence difference.

### 29.1 Initial interactive guarantee

For supported shell integrations ShellBeam may observe advisory command boundaries such as:

```text
agent command submitted
shell pre-exec boundary
shell post-exec/prompt boundary
exit status
```

These facts help the agent know an interactive command completed. They are not automatically equivalent to the existing process-runtime receipt unless a separately implemented context-execution path proves equivalent ownership/evidence semantics.

### 29.2 Verification boundary

Mechanical verification/evidence that requires ordinary ShellBeam receipt guarantees MUST NOT silently treat an interactive shell transcript as equivalent.

A later implementation may introduce a receipt-producing **context-exec** primitive that executes a child from inside the delegated shell environment while reporting exact command identity and exit evidence to the daemon. That is valuable but is large enough to require explicit plan/spec treatment if it cannot be kept inside this design's proven boundaries.

### 29.3 Why this matters

The feature's purpose is context continuity and human handoff, not weakening ShellBeam's evidence model. Any implementation that merely scrapes a prompt and calls it a normal receipt is rejected.

---

## 30. Environment/context continuity

The value of the delegated shell is that shell-local state survives across turns:

```text
cwd
exported environment variables
shell functions/aliases
activated virtual environments
SSH_AUTH_SOCK / agent context
cloud CLI login context
kubectl context
runtime manager activation
interactive program state
```

ShellBeam does not reconstruct those values from daemon `os.Environ()`.

A command executed **inside the shell** naturally receives that shell's child environment. This is why a tunnel process can use an API key the model never received.

The product SHALL distinguish:

```text
normal workflow does not transfer the secret to the model
```

from the stronger claim:

```text
the model can never obtain the secret despite unrestricted same-user shell authority
```

The second claim is outside the current ShellBeam trust model and MUST NOT be advertised.

---

## 31. Private tmux provider design

### 31.1 Dedicated server/socket

ShellBeam SHALL use a private tmux server identity/socket under the per-user ShellBeam runtime area rather than the user's default tmux server.

Reasons:

- no collision with user session names;
- ShellBeam can use stable controlled options;
- user `tmux kill-server` on the default server does not accidentally define ShellBeam lifecycle;
- ShellBeam does not need to parse arbitrary user tmux configuration;
- provider discovery/cleanup can reason about one owned server.

Socket/runtime permissions must follow existing user-only local-state rules.

### 31.2 Controlled configuration

The private provider SHOULD start tmux with no arbitrary user config (`-f` controlled/empty configuration or equivalent) and then set only reviewed options.

H0 qualification must cover:

```text
Control Mode and stable IDs
exact client identity
read-only/write-enable control
FenceHumanIngress feasibility
privacy suppression topology/scope
private-from-first-byte observer attach/reconnect
multi-session privacy isolation
observer replacement/overlap faults
flow control/backpressure and ACK/output ordering
attach/switch/reattach environment preservation (-E/equivalent)
terminal sizing/ignore-size behavior
session/pane lifecycle and daemon restart reattachment
HumanControl reachability in writable and fenced/read-only states
resource/socket/process/goroutine leak behavior
```

### 31.3 Lazy startup

No tmux server starts merely because ShellBeam daemon starts. The provider starts lazily on first delegated-session use or an explicit doctor/qualification action. Ordinary command admission performs zero tmux work.

### 31.4 Control Mode, not `capture-pane` polling

Control Mode output is the live transport. `capture-pane` may be used only for explicitly safe bounded resynchronization outside private intervals. It is never a periodic transport loop and never crosses a private interval.

### 31.5 Human client flags and reachability

The designated human client is toggled writable/read-only by exact client identity when the provider qualifies that behavior. Read-only is only an ingress/arbitration primitive, not a proof that previously delivered bytes are quiescent and not a security boundary.

Required HumanControl actions must remain locally reachable in their required state. H0/H2 may use tmux-native OOB bindings while writable and detach to a ShellBeam local control surface while fenced/read-only. A ShellBeam-owned ingress proxy is a fallback architecture fork only if simpler qualified mechanisms cannot meet the contract.

### 31.6 Attachment does not synchronize environment

Human attach, observer/control attach, switch, and daemon recovery attach to an existing delegated session SHALL preserve its session environment. The tmux provider must use `attach-session -E`, `switch-client -E`, or an equivalent qualified mechanism wherever tmux would otherwise apply `update-environment`.

---

## 32. tmux client/library boundary

The semantic dependency is **tmux Control Mode**, not any one Go wrapper library.

`github.com/atomicstack/gotmuxcc/gotmuxcc` is a candidate implementation dependency because it targets persistent Control Mode connections, command correlation, notifications, output, and flow control. It SHALL pass a dependency/provider qualification gate before adoption.

Qualification MUST include:

```text
license
maintenance/security posture
API stability
supported tmux versions
Darwin + Linux behavior
malformed protocol handling
command correlation
flow control/backpressure
no-output/private interval behavior
client flag/read-only control
large output
pane/session death
control connection death/reconnect
resource/socket/goroutine leaks
binary size / transitive dependency impact
removal/replacement plan
```

If the library fails, ShellBeam may implement a **thin Control Mode protocol adapter**. It SHALL NOT respond by implementing its own terminal multiplexer.

---

## 33. Direct PTY and tmux coexistence

`github.com/creack/pty` remains the process adapter for ordinary ShellBeam-owned PTYs.

```text
direct one-shot TTY
    -> creack/pty

delegated persistent interactive session
    -> tmux provider
```

The two paths have different lifecycle/authority semantics and must not be conflated in one generic "PTY helper" abstraction.

---

## 34. Daemon restart and provider reattachment

Delegated interactive sessions may outlive the ShellBeam daemon because the private tmux server owns the interactive shell/PTY.

Reattachment SHALL be fail-closed:

- canonical ShellBeam durable state identifies delegated `session_id`, `authority_epoch`, desired owner/ingress/privacy/capture dimensions, and provider identity/generation;
- provider-private metadata binds exact tmux server/session/pane identity plus a high-entropy ShellBeam session marker/token where needed;
- daemon restart reconnects only when the exact live provider session matches the canonical binding;
- recovery obtains fresh provider observation rather than inferring authority from the last persisted phase;
- process name/PID/TTY guesses never substitute for provider proof;
- every replacement model-visible observer for a durable private session is private from first output byte under the qualified privacy topology;
- attach/switch/reattach preserves delegated session environment;
- if desired authority, provider-observed authority, current epoch, or privacy state disagree, agent write/public capture remain fenced until exact reconciliation succeeds;
- private intervals are never reconstructed from tmux history.

Conceptually:

```text
durable desired state
+ fresh provider observation
+ current authority_epoch
= derived effective authority/capture
```

Mismatch produces a fenced/ambiguous state, not heuristic continuation. Host reboot remains outside the continuity guarantee.

---

## 35. Provider loss

If the private tmux server dies:

- ShellBeam records provider/session loss separately from child exit evidence it can actually prove;
- it does not fabricate an exit code;
- it does not attach to a new tmux server/session with the same friendly name and call that continuation;
- subsequent writes fail closed;
- a new delegated session requires a new start identity unless the original operation semantics explicitly permit replay to the same durable terminal result.

---

## 36. Terminal app close vs session close

Closing Ghostty/iTerm2/WezTerm/another human terminal client MUST NOT by itself terminate the delegated session.

```text
human terminal client exits
    -> client detached/lost
    -> tmux session continues
    -> agent may continue if it owns the session
```

If the human closes the terminal while `HUMAN_OWNED` before completion, the handoff becomes `CLIENT_LOST` or returns to a conservative pending state. ShellBeam does not infer success from client disappearance.

---

## 37. Product installation and source-tree independence

The current development checkout is an implementation workspace only. Product semantics SHALL work when a user has only installed ShellBeam binaries/state and no source code.

No production code may assume:

```text
/Users/trung.ngo/Documents/zaob-dev/shellbeam
/Users/trung.ngo/Documents/zaob-dev/shellbeam-worktrees
/opt/homebrew/bin/fish
/opt/homebrew/bin/tmux
/Applications/Ghostty.app at one fixed path
one package manager
one login shell
one terminal emulator
```

Runtime discovery uses:

- installed executable identity;
- PATH/executable resolution under reviewed rules;
- platform application/bundle identity;
- actual process identity where needed;
- XDG/macOS per-user runtime/state locations;
- capability probes.

ShellBeam SHALL NOT silently install tmux. `doctor`/capability inspection reports missing provider prerequisites and actionable remediation.

---

## 38. Capability discovery

The effective capability is an intersection of daemon/core support and local providers.

Conceptual shape:

```json
{
  "interactive_handoff": {
    "version": 1,
    "maturity": "experimental",
    "delegated_session": {
      "available": true,
      "provider": "tmux_control_mode",
      "daemon_restart_continuity": true,
      "host_reboot_continuity": false
    },
    "terminal_resolution": {
      "bridge_affinity_hint": true,
      "recent_activity": true,
      "auto_launch": true
    },
    "shell_integrations": [
      {
        "shell": "fish",
        "level": "requirement_aware",
        "safe_boundary": true,
        "environment_exported_nonempty": true
      }
    ],
    "authority": {
      "authority_epoch": true,
      "human_ingress_fence": true
    },
    "human_control": {
      "local_ready": true,
      "local_abort": true,
      "fenced_state_reachable": true
    },
    "privacy": {
      "secret_private_interval": true,
      "privacy_release_separate": true,
      "observer_topology_qualified": true,
      "human_input_persisted": false
    },
    "limits": {
      "active_delegated_sessions": 8,
      "concurrent_human_handoffs": 4,
      "requirements_per_handoff": 1,
      "max_wait_ms": 60000
    }
  }
}
```

Exact limits are implementation-plan decisions but must be hard, bounded, and advertised.

Capability absence never degrades ordinary ShellBeam execution.

---

## 39. Resource and performance invariants

This subsystem is specifically prohibited from becoming a background resource leak.

### 39.1 Ordinary path

For a normal direct command when delegated handoff is unused:

```text
tmux work                         = 0
terminal resolution work          = 0
shell integration work            = 0
handoff watcher work              = 0
GUI activation decision work      = no per-command work
```

A single already-running platform activation event source may exist when the platform provider is enabled, but it performs event-driven O(1) bookkeeping and no polling.

### 39.2 Delegated sessions

Initial hard design targets:

```text
private tmux servers per ShellBeam state root     <= 1
active delegated sessions                         bounded (target 8)
concurrent human-writable handoffs                 bounded (target 4)
active readiness predicates per handoff            <= 1
resident helper process per requirement            0
poll timers per requirement                        0
```

The exact target constants may change during planning, but "unbounded" is not an allowed result.

### 39.3 Expiry

Pending handoffs have a bounded TTL. Expiry removes temporary shell integration and ends any waiters. Expiry does not silently kill the underlying delegated session.

Use one central expiry mechanism/queue where practical rather than one goroutine/ticker per handoff.

### 39.4 Leak acceptance

Native stress acceptance MUST include repeated delegated-session creation/handoff/reclaim/close cycles and prove no monotonic creep in:

```text
tmux sessions/panes/clients
ShellBeam goroutines
open file descriptors
Unix sockets
helper processes
shell hooks/functions
terminal activity subscriptions
state files
```

---

## 40. Output truth and evidence semantics

Existing direct PTY semantics aim at byte-correct complete capture. Secret handoff intentionally creates an exception that must be explicit rather than hidden.

Delegated-session status SHALL distinguish at least:

```text
complete_public_capture
private_intervals_omitted
transport_incomplete
provider_lost
```

For any transcript with intentionally omitted private bytes:

```text
output_complete = false
capture_quality = private_intervals_omitted
```

The legacy Boolean remains mathematically truthful: the transcript is not byte-complete. The additive quality explains that the incompleteness is intentional privacy rather than transport failure. Direct-session legacy meaning remains unchanged.

Private omission is not execution failure. Evidence consumers that require complete command output must reject or explicitly tolerate `private_intervals_omitted` rather than silently treating hidden output as absent.

---

## 41. Event Journal and durable metadata

Handoff events/durable metadata may include:

```text
handoff_id
session_id
authority_epoch
phase/derived status
desired owner and ingress fence states
provider generation and provider-observed authority quality
reason / privacy class
terminal provider id/version
shell provider id/version/quality
transfer-boundary kind/quality
privacy-release state/quality
capture state/quality
requirement satisfied boolean
human client attached/read-only boolean
private interval started/ended
failure code
timestamps
```

They MUST NOT include:

```text
human keystrokes
secret values
secret hashes
terminal history containing private bytes
raw environment
password/OTP text
```

Friendly terminal application identity is acceptable operational metadata when needed for diagnostics, but app-usage history should remain minimal/freshness-bounded.

---

## 42. Failure taxonomy

Initial stable failure concepts SHOULD include or map into existing `feature_unavailable` where appropriate:

```text
delegated_session_unavailable
interactive_provider_unqualified
interactive_provider_lost
terminal_launcher_unavailable
terminal_launch_failed
terminal_launch_unknown
shell_integration_unavailable
shell_integration_lost
handoff_conflict
handoff_not_pending
handoff_expired
handoff_client_lost
session_control_not_owned
stale_control_generation
ingress_fence_unproven
transfer_boundary_unproven
handoff_reclaim_blocked
private_output_barrier_failed
privacy_release_unproven
privacy_topology_ambiguous
requirement_unsupported
requirement_not_satisfied
```

Requirements:

- provider absence fails before claiming delegated semantics;
- exact retry of a previously accepted mutation replays prior outcome before current-epoch authorization is considered;
- a previously unseen stale-epoch mutation never executes;
- unknown launch outcome is not success;
- shell integration failure degrades to manual HumanControl where safe rather than executing wrong shell syntax;
- inability to establish private-from-first-byte observation prevents secret human write authority;
- inability to prove ingress fencing prevents silent ownership transfer;
- inability to prove privacy release prevents public capture even if input authority can otherwise transfer;
- no failure branch exposes secret values for diagnostics.

---

## 43. Security and privacy model

### 43.1 Existing current-user trust boundary

ShellBeam runs with the current OS user's authority. The private tmux socket is protected by user-local filesystem permissions. Anyone who has equivalent same-user authority and direct access to the socket should be considered trusted at the same level as ShellBeam itself.

The tmux read-only flag prevents accidental concurrent input in the supported UX; it is not an adversarial boundary against the socket owner/current user.

### 43.2 Secret goal

The hard product goal is:

> A normal human handoff can grant an already-established local execution context the authority to use a secret without transmitting that secret through MCP/model-visible state.

The design does not claim:

> An unrestricted same-user agent can never deliberately discover a secret from local OS resources.

That stronger property would require a different sandbox/capability-security architecture.

### 43.3 Child environment inheritance

A process intentionally launched as workload from the delegated shell may inherit exported credentials. That is the intended capability delegation. Child-process environment inheritance must not accidentally expand to unrelated daemon-spawned commands outside the delegated context.

ShellBeam infrastructure is different from workload. Attach clients, readiness notifiers, shell-control helpers, terminal-launch helpers, and equivalent control-plane processes MUST NOT inherit the delegated shell environment wholesale merely because they are invoked from that shell. Their environment is provider-defined and minimally allowlisted; secret-bearing variables are excluded.

### 43.4 Model output redaction by prevention

The preferred privacy mechanism is to prevent private interval bytes from entering model-visible capture at all, not to store them and attempt heuristic redaction later.

---

## 44. Platform matrix

### 44.1 macOS

Required native qualification includes:

- tmux Control Mode provider;
- GUI/user-session terminal discovery and launch;
- event-driven recent-terminal activation source if used;
- exact app/bundle identity mapping;
- Ghostty and at least one additional common terminal adapter before claiming broad auto-launch UX;
- fish/zsh/bash shell integration tests;
- read-only client reclaim while the GUI terminal remains open.

### 44.2 Linux

Required native qualification includes:

- tmux Control Mode provider;
- terminal launch under supported desktop/session environments;
- no claim that one X11/Wayland detection mechanism works universally;
- exact degraded behavior when recent/frontmost terminal observation is unavailable;
- bash/fish/zsh integration;
- at least one qualified terminal launcher in CI/native acceptance environments.

### 44.3 Unsupported combinations

Unsupported terminal/shell combinations remain explicit capability states. ShellBeam shall prefer a manual attach plus shell-independent local HumanControl fallback over guessing.

---

## 45. Terminal-size and resize arbitration

Multiple attached clients can have different terminal dimensions. The provider SHALL use tmux's client/window sizing semantics deliberately rather than letting an observer window unexpectedly resize a running TUI.

Recommended policy:

- agent control client does not impose a terminal size unless explicitly required;
- designated writable human client may define size during human ownership;
- read-only observer clients use ignore-size behavior where available;
- ownership transition may preserve the current pane size rather than oscillating on every attach/detach.

Exact policy is provider-qualified and covered by native tests.

---

## 46. Terminal and shell configuration isolation

### 46.1 tmux config

ShellBeam private tmux provider does not source arbitrary user tmux configuration by default.

### 46.2 shell config

The interactive shell MAY run its normal user shell startup semantics because preserving the user's expected shell environment is part of the feature value. However:

- ShellBeam must know whether it started an interactive/login/non-login shell;
- shell startup failure is surfaced;
- untrusted repository files are not silently sourced merely because the session is delegated;
- project capability manifests do not become shell startup scripts.

### 46.3 terminal config

Terminal emulator configuration remains user-owned. Launcher adapters should use documented launch interfaces and avoid rewriting terminal preferences.

---

## 47. Nested shells and runtime drift

If the user runs:

```text
fish -> zsh
bash -> nu
exec another-shell
```

inside a delegated pane, the shell adapter bound to the original shell may no longer be valid.

Before installing a new requirement watcher, ShellBeam must revalidate the current interactive shell identity. If exact identity cannot be proven, shell-aware readiness is disabled for that handoff and manual completion is used.

No stale fish hook is injected into a zsh parser.

---

## 48. Human completion and local control strategies

Human completion is shell-independent at the semantic layer.

### 48.1 Automatic typed readiness

A qualified shell adapter may emit a typed requirement result and boundary evidence, for example exported/non-empty environment presence at a safe prompt boundary. This is a convenience/automation path, not the only way a handoff can complete.

### 48.2 Local HumanControl

The human can signal state through a ShellBeam-local control path that does **not** send text into delegated pane stdin.

Required semantic actions by state include:

```text
human writable:        ready, abort, status
human fenced/private:  resume, terminate, status
agent-owned observer:  status, optional request_control
```

A provider need not make every action an arbitrary key binding inside a read-only tmux client. "Locally reachable" may mean tmux-native OOB controls while writable and detaching to a ShellBeam local control surface while fenced/read-only. Only if simpler mechanisms fail qualification should H0/H2 introduce an attach-side ingress proxy.

HumanControl signals SHALL be bound to at least:

```text
session_id
handoff_id
authority_epoch
```

so a stale `ready`/`resume` from an older handoff cannot complete a later generation. Signals are retry-safe/idempotent under their stable identity.

### 48.3 Manual ready quality

A human `ready` action may establish a `human_attested` `TransferBoundary` under a policy that permits manual handback, including unknown shells/TUIs. It does not by itself establish `PrivacyReleaseProof` for secret/private output.

### 48.4 Chat-level "done"

The agent may ask the human to say they are finished when no local completion primitive is available. This is the least automatic fallback and does not silently upgrade proof quality. A chat acknowledgement cannot bypass ingress/provider/privacy gates.

### 48.5 Foreground child still running

If the human interaction happens inside a long-running foreground child and the shell prompt does not return, automatic shell-prompt completion may be impossible. Local HumanControl remains available independently of shell parsing. ShellBeam MUST NOT guess completion merely because terminal input became idle.

---

## 49. Automatic terminal opening and handback flow

Normative sequence:

```text
1. Agent requests handoff with stable handoff_id under authority_epoch N.
2. Daemon durably binds handoff to exact delegated session.
3. Transfer intent is accepted; authority_epoch rotates to N+1.
4. Reject new old-epoch agent mutations; replay already-known old mutations only.
5. Fence agent ingress and establish an admissible transfer boundary.
6. Validate provider/session and required HumanControl/privacy capabilities.
7. Resolve terminal context using session affinity + freshness.
8. Create/reveal one terminal client for this handoff without environment sync.
9. For secret privacy, establish every model-visible observation path private and ACK it BEFORE human write enable.
10. Identify exact human client and enable only that human ingress.
11. Derived status may publish HUMAN_OWNED.
12. Wait event-driven for automatic readiness or local HumanControl.

READY PATH
13. Treat readiness as a completion candidate.
14. Fence human ingress; prove exact ingress fence.
15. Establish an admissible TransferBoundary for agent control.
16. Reconcile provider authority + epoch; then agent input may resume under the next epoch/owner transition.
17. Independently require PrivacyReleaseProof before public capture resumes.
18. Resume model-visible output only from a new forward-only boundary.

ABORT PATH
13a. Fence human ingress.
14a. Do not infer rollback, agent ownership, or privacy release.
15a. Remain fail-closed/private until local resume, explicit local termination, or separately admissible transfer/privacy proofs.
```

A failed step cannot be skipped merely to make UX appear seamless. Control authority and capture visibility may transition at different times because `TransferBoundary != PrivacyReleaseProof`.

---

## 50. Terminal launch idempotency

Terminal launch is an external GUI side effect and must respect retry ambiguity.

For each `handoff_id`, durable state distinguishes:

```text
not_attempted
launching
launched_and_client_proven
launch_failed
launch_outcome_unknown
```

Retry behavior:

- `launched_and_client_proven` -> reuse/reveal; do not launch another window;
- `launch_failed` -> replay exact failure unless caller explicitly starts a new handoff;
- `launch_outcome_unknown` -> inspect for the exact expected client/session when possible; do not blindly duplicate.

Friendly duplicate-prevention heuristics cannot become session authority.

---

## 51. Capability verification examples

### Tunnel client

```text
requirement: CONTROL_PLANE_API_KEY exported/non-empty
human completes private handoff
agent reclaims
agent runs tunnel-client doctor
PASS -> capability verified
FAIL -> handoff again or report invalid credential
```

### AWS SSO

```text
human performs aws sso login / browser/device auth
completion may be manual if foreground flow does not return shell prompt
agent verifies aws sts get-caller-identity
```

### SSH agent

```text
human unlocks/adds key inside delegated context
agent verifies intended git/ssh operation
```

### Destructive confirmation

```text
Agent sees consequential yes/no
Agent does not treat syntax as safe
Handoff human confirmation if policy requires
```

These are examples of the generic pattern, not hardcoded integrations.

---

## 52. Resource-enforcement interaction

The current hard Resource Enforcement V1 explicitly excludes persistent sessions. This design MUST NOT silently apply direct-operation resource-limit semantics to delegated interactive sessions.

Until a separately reviewed lifetime design exists:

```text
delegated interactive session + hard limits
=> resource_limit_unsupported / feature_unavailable before spawn
```

The tmux server itself is provider infrastructure and must not accidentally inherit per-operation hard limits intended for ordinary child trees.

---

## 53. Hermetic/sandbox interaction

Delegated interactive sessions are intentionally human/local and do not automatically satisfy verification-only hermetic execution semantics.

A hermetic provider that forbids interactive PTY/human input remains separate. ShellBeam must not label a command verified/hermetic merely because it was run inside a delegated session.

---

## 54. Telemetry and observation

Permitted bounded metrics include:

```text
handoff request count
handoff latency by state (no secret content)
terminal provider selected
shell capability level
provider failure codes
private interval count/duration
reclaim success/failure
session/client leak counters
```

Forbidden telemetry includes human keystrokes, secret content, private terminal transcript, environment values, password length, or secret-derived hashes.

Performance telemetry must not introduce polling solely to measure the feature.

---

## 55. Testing strategy

### 55.1 Core authority/state tests

Cover the orthogonal durable dimensions rather than only a combined enum:

- owner projection versus agent/human ingress states;
- `authority_epoch` rotation at durable transfer intent;
- idempotency-before-authority: known old-epoch retry replays exact outcome;
- unseen stale-epoch mutation fails `stale_control_generation`;
- generation policy for write, resize, signal, kill, handoff transition, HumanControl, and provider-authority mutations;
- durable desired versus fresh provider-observed authority mismatch fails closed;
- ready establishes only its declared transfer/privacy qualities;
- abort does not imply rollback/reclaim/privacy release;
- client/provider loss during every material phase;
- no agent writes admitted while human authority is effective.

### 55.2 H0 tmux protocol/provider qualification matrix

Native/protocol fixtures SHALL cover all H0 probes:

```text
P0  private server/socket/config
P1  stable exact client identity
P2  exact read-only/write-enable control
P3  FenceHumanIngress proof

P4  privacy suppression scope/topology
P5  private-from-first-byte observer attach
P6  private recovery without history replay
P7  attach/switch/reattach without environment mutation

P8  HUMAN_OWNED OOB HumanControl
P9  fenced/read-only HumanControl reachability

P10 resize/client-size isolation
P11 crash/reconnect
P12 control ACK/output ordering semantics
P13 repeated attach/fence/reclaim leak/resource stress
P14 multi-session privacy isolation
P15 observer replacement/overlap privacy fault
```

P3/P4/P5/P6/P14/P15 are genuine architecture gates. A failure requires a changed provider mechanism/topology and requalification; it cannot be rationalized as "probably safe".

For P14, run at least:

```text
A = private/noisy secret canary session
B = noisy public session
C = noisy public session
```

and prove both:

```text
A never reaches any model-visible path
B/C remain correctly observable and are not silently suppressed by A's privacy state
```

P15 faults observer/control replacement and overlap at every attach/reconnect boundary and proves no first-byte exposure window.

### 55.3 Shell adapter matrix

At minimum native coverage for supported versions of:

```text
fish
zsh
bash
```

Nushell enters the required matrix only after its adapter is promoted from experimental.

Tests include:

- existing user hook preservation;
- session-local install/remove;
- exported/non-empty readiness;
- empty/unset variable remains unsatisfied;
- exact boundary quality emitted;
- `TransferBoundary` and `PrivacyReleaseProof` are independently represented;
- nested shell drift -> degrade;
- no persistent dotfile mutation;
- no secret value in hook argv/output/state;
- notifier/control helper environment is minimally allowlisted and excludes watched secret material.

### 55.4 Secret-canary tests

Use deterministic fake secrets and assert the literal secret and common encodings/hashes do not appear in:

```text
MCP results
canonical/public output
Event Journal
receipts
handoff metadata
telemetry
repro/evidence
state files
logs
provider reconnect/resync output
```

Deliberately type a visible `export KEY=<canary>` during a private interval to prove suppression rather than relying only on hidden-input UX. Include delayed post-input output so human `ready` cannot accidentally be treated as privacy release.

### 55.5 Terminal resolver tests

Cover:

- existing client for this session beats all other candidates;
- active supported terminal beats recent/fresh bridge affinity;
- browser frontmost does not erase recent terminal context;
- stale bridge-launch ancestry does not become timeless preference;
- multiple installed/running terminals;
- no stored preference required;
- unsupported terminal degrades;
- launcher retry does not duplicate proven client;
- unknown launch outcome remains unknown;
- exact app identity, not arbitrary env path execution.

### 55.6 Native GUI smoke tests

Each promoted terminal launcher requires native evidence on its target OS. CI may contract-test command generation without opening a GUI, but release qualification must include real launch/attach/reveal behavior for the advertised provider.

### 55.7 Resource/leak stress

At least 100 repeated delegated-session/handoff/fence/reclaim/close cycles under native tmux should verify bounded:

```text
CPU while idle
RSS trend
FD count
goroutine count
tmux control clients/human clients/panes/sessions
helper process count
runtime files/sockets
shell hook residue
terminal activity subscriptions
```

No test may substitute "sleep and assume" for inspecting the relevant resources.

### 55.8 Restart/fault matrix

Fault at every important boundary:

```text
after durable transfer intent/epoch rotation before ingress fence
after GUI launch before exact client proof
after private observer attach before human writable
after human writable before readiness watcher
inside private output interval
after ready before human ingress fence
after ingress fence before transfer boundary
after transfer boundary before provider reconciliation
after provider reconciliation before privacy release
after privacy release before public observer resume
during observer replacement/overlap
after final reclaim before response
```

Each fault must have deterministic authority-safe and privacy-safe recovery.

---

## 56. Dependency/provider qualification gate

Before any public delegated-interactive capability is implemented, H0 must record exact evidence for P0-P15 in Section 55.2, including:

```text
exact tmux executable/version identity
minimum/maximum tested versions
Control Mode protocol features actually used
Darwin evidence
Linux evidence
private socket/config behavior
exact client identity and write/read-only control
FenceHumanIngress result
privacy observer topology and scope
private-from-first-byte behavior
multi-session privacy isolation
observer replacement/overlap behavior
attach/switch/reattach -E/equivalent environment preservation
HumanControl reachability by authority state
flow control and ACK/output ordering
terminfo/default-terminal/resize behavior
restart/reconnect behavior
resource/leak behavior
```

The architecture does **not** precommit whether privacy uses one model-visible Control Mode client per delegated session, a shared observer plus daemon privacy demux, or another qualified mechanism.

If P3 (`FenceHumanIngress`) fails under native tmux semantics, H0 must investigate an attach-side ShellBeam ingress gate and then re-run qualification. If privacy gates P4/P5/P6/P14/P15 fail, H0 must change observer topology/provider mechanism and requalify. No dependency/topology is accepted merely because it makes the first prototype shorter.

If a Go Control Mode wrapper such as `gotmuxcc` is considered, record exact module version/SHA, license, maintenance/security review, malformed-protocol behavior, removal/replacement plan, and prove it does not weaken any P0-P15 result. The architecture depends on Control Mode semantics, not the wrapper.

---

## 57. Rollout strategy

Implementation is a DAG of separately reviewable slices, not one H0->H5 mega-plan.

### H0 — provider qualification only

- execute P0-P15 from Section 55.2;
- decide HOW tmux satisfies the frozen master semantics;
- record PASS/FAIL and any architecture fork;
- **no public MCP delegated-interactive capability and no feature implementation beyond qualification fixtures/probes.**

### H1 — delegated-session core protocol

Only after H0 PASS for a viable provider mechanism:

- `session_mode="delegated_interactive"` schema/capability contract;
- delegated session durable identity/provider binding;
- `authority_epoch` and idempotency-before-authority;
- mutation generation taxonomy/policy;
- desired-versus-observed provider reconciliation;
- no automatic terminal launch or shell-aware privacy automation yet.

### H2 — human authority + manual attach/control

- local attach with no environment synchronization;
- exact human/agent ingress fencing;
- local HumanControl ready/abort/status/resume/terminate reachability;
- manual transfer boundaries;
- derived status projections and fail-closed recovery.

### H3 — automatic terminal resolver/launcher

- session affinity + active/recent terminal evidence;
- freshness-bounded bridge-launch hint;
- promoted launcher adapters;
- idempotent GUI launch/reveal behavior.

### H4 — shell-aware readiness + secret privacy

- fish/zsh/bash adapters;
- typed `TransferBoundary` and `PrivacyReleaseProof` producers;
- qualified private-output topology;
- secret-canary and multi-session privacy acceptance;
- automatic reclaim without closing the human terminal where proven.

### H5 — high-assurance context execution + broader providers

- receipt-producing context execution if a separately reviewed evidence contract justifies it;
- Nushell/other shell adapters;
- additional terminal/session providers.

Context-exec is **not** required for the first experimental handoff UX, but it is a candidate stable/high-assurance gate because important post-secret commands otherwise remain in weaker interactive-transcript evidence semantics.

---

## 58. Compatibility and one-tool boundary

No second MCP tool is introduced.

Legacy clients/bridges:

- continue using ordinary `start/poll/write/kill` and legacy `tty`/`persistent` fields;
- do not receive delegated-interactive capability;
- legacy `persistent=true + tty=true` remains rejected.

New delegated request:

```text
session_mode=delegated_interactive
```

When `session_mode` is present, legacy `tty`/`persistent` fields are absent. There is no precedence rule between old and new spelling because a request has one semantic source of truth.

New bridge + old daemon:

- unknown/new schema or negotiation reports delegated handoff unavailable before spawn;
- ordinary execution remains unchanged.

Old bridge + new daemon:

- ordinary behavior remains unchanged;
- the new daemon does not infer that legacy TTY starts should be delegated.

---

## 59. Operational diagnostics

`doctor`/inspection SHOULD make failures actionable without exposing sensitive state.

Example:

```text
Interactive handoff
  delegated sessions: available
  provider: tmux control mode
  private tmux server: lazy/not running

Terminal launchers
  Ghostty: available
  WezTerm: available
  iTerm2: not installed

Recent terminal resolution
  available: yes
  current effective candidate: Ghostty

Shell integration
  fish: full
  zsh: full
  bash: full
  nu: unsupported in this build

Secret private intervals
  available: yes
```

Diagnostics do not print environment values, tmux private tokens, or secret-bearing command history.

---

## 60. Design invariants

The following are hard invariants for any implementation plan:

1. No required static preferred-terminal setting; terminal resolution uses session affinity + freshness.
2. Terminal, shell, and interactive-session provider identities remain orthogonal.
3. Unknown shell never receives guessed shell syntax.
4. No permanent dotfile or user tmux-config mutation.
5. No shell polling loop, capture-pane polling transport, or per-requirement resident watcher.
6. `session_mode="delegated_interactive"` is explicit; when present, legacy `tty`/`persistent` fields are absent.
7. Owner, ingress fence, transfer boundary, privacy release, capture state, and provider authority are distinct correctness dimensions.
8. `authority_epoch` rotates when ownership-transfer intent is durably accepted.
9. Idempotency is checked before current authority: exact accepted retry replays prior outcome; unseen stale-generation mutation never executes.
10. Every delegated control/lifetime mutation is covered by an explicit generation/authority policy.
11. Secret human write cannot begin before every model-visible observation path is private from first possible byte.
12. Secret values/hashes never enter ordinary model-visible or durable ShellBeam state.
13. `TransferBoundary` may allow new input authority; `PrivacyReleaseProof` separately governs resumption of model-visible output.
14. Human `ready` alone is not a secret privacy-release proof.
15. Human `abort` revokes further human authority but does not imply rollback, agent reclaim, or privacy release.
16. HumanControl actions required by a state remain locally reachable without injecting them into delegated pane stdin.
17. Human and agent ingress are never concurrently admitted under ShellBeam-controlled UX.
18. Human terminal may remain open after reclaim only when exact human ingress is fenced; V1 observer means passive live observation, not guaranteed tmux UI navigation.
19. Read-only client control is an arbitration primitive, not a security boundary or PTY-quiescence proof.
20. Private intervals are never replayed into model output after reconnect/recovery.
21. Privacy topology must isolate private A without silently suppressing public B/C, including observer replacement/overlap.
22. Attachment/switch/reattach is presentation, not environment synchronization; existing delegated session environment is preserved.
23. Presence of a credential is not proof of validity; capability verification remains separate.
24. Ordinary direct commands pay zero delegated-session provider work.
25. Missing/unqualified tmux, terminal, shell, HumanControl, fence, or privacy capability is explicit and fail-closed.
26. Source checkout paths and developer-machine package layouts never enter product contracts.
27. Arbitrary existing external PTY adoption is outside V1.
28. Interactive shell transcript is not silently upgraded to ordinary receipt/evidence authority.
29. Host reboot continuity is not claimed.
30. Resource Enforcement/Hermetic semantics are not silently extended to delegated sessions.

---

## 61. Resolved design decisions and H0-open qualification choices

Semantic debate is frozen at the master-architecture level.

### 61.1 Resolved master decisions

```text
wire mode                = session_mode=delegated_interactive
legacy coexistence       = session_mode present => tty/persistent absent
provider foundation      = optional qualified tmux Control Mode
Go wrapper               = qualification choice, protocol semantics owned by ShellBeam
output truth              = output_complete=false + additive capture_quality for private omission
initial stable shells     = fish/zsh/bash; Nushell separately qualified
terminal support          = capability-qualified, not a fixed count gate
kill while human-owned    = no ordinary model bypass; transfer/fence first
Linux recent-terminal     = optional provider, not correctness dependency
context-exec              = not experimental-handoff prerequisite; candidate stable/high-assurance gate
HumanControl              = shell-independent and locally reachable
privacy                   = TransferBoundary != PrivacyReleaseProof
canonical state           = orthogonal durable dimensions; combined states are derived projections
attachment                = presentation, not environment synchronization
```

### 61.2 H0 choices deliberately left open

H0 must measure rather than speculate about:

1. whether native tmux can prove `FenceHumanIngress`; if not, whether an attach-side ingress gate is required;
2. which privacy observer topology satisfies P4/P5/P6/P14/P15;
3. which OOB HumanControl transport is simplest while preserving reachability in writable and fenced/read-only states;
4. whether a candidate Go Control Mode library passes the exact provider qualification gate;
5. exact supported tmux version range and optional use of newer-version primitives.

These are implementation/provider qualification choices. A failure may force a provider mechanism/topology change, but does not reopen the frozen semantic contract unless H0 proves the contract itself impossible.

---

## 62. Definition of done for the design/implementation program

The feature is not complete merely because an agent can type into tmux.

A stable release claim requires all of the following:

1. H0 P0-P15 has native evidence on the advertised platforms/provider versions, with genuine gates passing;
2. ordinary direct ShellBeam start path shows no tmux/handoff work when unused;
3. public schema/IPC/capability negotiation uses explicit `session_mode=delegated_interactive` and rejects unsupported mode before spawn;
4. start retry cannot duplicate delegated shells;
5. authority epoch/idempotency-before-authority prevents stale control mutations across handoff cycles;
6. mutation generation taxonomy covers write/resize/signal/kill/handoff/HumanControl/provider-authority lanes;
7. durable desired versus provider-observed authority reconciles fail-closed across daemon restart;
8. automatic terminal resolution works without required preferred-terminal settings for advertised providers;
9. terminal launcher retry cannot create duplicate windows after a proven attach;
10. terminal attach/switch/reattach does not mutate delegated session environment;
11. fish/zsh/bash adapters preserve existing hooks and leave no permanent dotfile changes;
12. unsupported/nested shells degrade safely to shell-independent HumanControl;
13. human and agent ingress never overlap under ShellBeam-controlled UX;
14. human terminal can remain open only with exact ingress fence; observer contract remains honest/passive;
15. yes/no/Enter/raw interactive input works while agent-owned;
16. secret handoff establishes private-from-first-byte observation before human write access;
17. secret canary never appears in model-visible/durable ordinary ShellBeam state;
18. private A does not leak and does not silently suppress public B/C;
19. observer replacement/reconnect creates no exposure window;
20. `TransferBoundary` and `PrivacyReleaseProof` remain independently represented/enforced;
21. manual ready supports unknown shell/TUI transfer without being treated as secret privacy release;
22. abort fences human authority without pretending to undo already-sent bytes or reclaim automatically;
23. HumanControl required actions remain locally reachable in writable and fenced/read-only states;
24. readiness performs no polling and leaves no resident helper process;
25. capability validity is rechecked separately from secret presence where appropriate;
26. output/evidence quality records `output_complete=false` plus intentional private omission quality;
27. tmux server/client/session/provider loss fails closed without PID/name-based takeover;
28. closing a GUI terminal client does not kill the delegated session by definition;
29. stress tests show no meaningful resource creep after repeated handoffs;
30. installed-product tests pass without source checkout and developer-specific package paths;
31. `doctor` exposes actionable capability/provider/H0 state without secrets;
32. no second MCP tool or ChatGPT App dependency is introduced;
33. interactive transcript facts are not mislabeled as ordinary verification evidence;
34. arbitrary external-PTY adoption remains outside the stable V1 contract;
35. host reboot continuity remains explicitly unsupported.

---

## 63. Final design position

The product abstraction is not "Ghostty integration" and not "API-key export support".

It is:

> **Delegated Interactive Sessions with Human–Agent Handoff** — a transferable interactive-authority protocol over a ShellBeam-owned local execution context: ShellBeam can present the same context in the terminal the user is actually using, delegate bounded input authority to a human, fence and reconcile that authority across retries/restarts, preserve private output independently from control transfer, and safely continue without reconstructing shell state or transmitting credentials through the model.

The reuse boundary is equally important:

```text
ShellBeam owns
  identity
  authority arbitration
  privacy semantics
  capability negotiation
  evidence honesty
  lifecycle metadata
  terminal/shell provider composition

Qualified providers own
  PTY/session multiplexing (tmux)
  terminal presentation/launch
  shell-specific event syntax
```

That keeps ShellBeam focused on engineering-state semantics and authority rather than becoming another terminal emulator or multiplexer.
