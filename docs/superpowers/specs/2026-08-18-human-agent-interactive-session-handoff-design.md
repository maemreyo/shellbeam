# ShellBeam Human–Agent Interactive Session Handoff Design

**Status:** draft for review and external debate; not implementation-approved

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

The recommended design is:

1. **tmux is the initial delegated interactive-session provider.** ShellBeam SHALL use tmux Control Mode as the application integration protocol rather than implementing a terminal multiplexer or arbitrary PTY takeover layer.
2. ShellBeam SHALL run delegated sessions on a **ShellBeam-private tmux server/socket**, not the user's normal tmux server, and SHALL start it with a controlled configuration rather than loading arbitrary user tmux configuration.
3. Existing `tty=true, persistent=false` remains the current direct PTY path. Existing `persistent=true, tty=false` remains B1.0 persistent non-TTY supervision. A negotiated future capability MAY define `persistent=true, tty=true` as the new delegated persistent interactive path; older daemons continue to reject it.
4. ShellBeam core SHALL separate three provider dimensions:
   - `InteractiveSessionProvider` — tmux/session mechanics;
   - `TerminalLauncherProvider` — Ghostty/iTerm2/WezTerm/kitty/Terminal.app/etc.;
   - `ShellIntegrationProvider` — fish/zsh/bash/Nushell/etc.
5. Terminal selection SHALL be **automatic and contextual**, with no required static `preferred_terminal`. Exact request-origin terminal identity wins when available; frontmost/recent-terminal activity is a fallback, not the sole source.
6. Human/agent input ownership SHALL be explicit. A human client is writable only during `HUMAN_OWNED`; after safe reclaim it becomes tmux `read-only`/observer even if the window stays open.
7. Secret-bearing handoff SHALL create a **private output interval**. Model-visible output capture is disabled for the delegated pane while the human may type secret material. No private bytes, hashes, or deterministic secret-derived values are persisted or returned.
8. Readiness SHALL be event-driven and scoped to an active handoff. There are no busy loops, per-session pollers, or permanent shell hooks. Safe-boundary hooks are installed temporarily and removed immediately after completion/cancel/expiry.
9. Supported shell adapters MAY prove typed predicates such as "environment variable is exported and non-empty" without returning its value. Unsupported shells degrade to manual completion; ShellBeam never guesses Bash syntax for an unknown shell.
10. Simple interactive CLI input (`y/n`, Enter, menu choice, control bytes) can remain agent-driven while the agent owns the session. Secret, consequential, unknown, or complex interactions can hand off to the human.
11. Arbitrary adoption of an already-running external Ghostty/fish PTY is **not** part of V1. A future Linux-only experimental provider may investigate reptyr-like adoption, but it is not the product foundation and cannot supply macOS parity.
12. The shipped product SHALL have no dependency on the ShellBeam source checkout path, developer Homebrew layout, or this machine's shell/terminal configuration.

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

### 7.7 Capability verification

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

Conceptual port:

```go
type InteractiveSessionProvider interface {
    Probe(ctx context.Context) Capability
    Create(ctx context.Context, spec SessionSpec) (ProviderSession, error)
    AttachControl(ctx context.Context, ref ProviderRef) (ControlHandle, error)
    Write(ctx context.Context, h ControlHandle, input []byte) error
    SetOutputMode(ctx context.Context, h ControlHandle, mode OutputMode) error
    SetHumanClientWritable(ctx context.Context, client ClientRef, writable bool) error
    Inspect(ctx context.Context, ref ProviderRef) (ProviderState, error)
    Close(ctx context.Context, ref ProviderRef) error
}
```

The exact Go shape belongs in the implementation plan. Core semantics SHALL NOT mention tmux commands directly.

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

No adapter may silently run syntax for a different shell family.

---

## 11. Session classes and compatibility

The product SHALL distinguish these execution classes:

| Request | Meaning before this feature | Meaning after negotiated feature |
|---|---|---|
| `tty=false, persistent=false` | direct pipe process | unchanged |
| `tty=true, persistent=false` | direct PTY process | unchanged |
| `tty=false, persistent=true` | B1.0 persistent non-TTY supervisor | unchanged |
| `tty=true, persistent=true` | explicitly rejected | delegated persistent interactive session, only after capability negotiation |

This mapping is attractive because it reuses the already-reserved semantic gap rather than adding a second overlapping persistence flag.

Hard rule:

```text
old bridge/daemon or unqualified provider
+ persistent=true
+ tty=true
=> feature_unavailable / persistent_tty_unsupported BEFORE spawn
```

There is no fallback to direct PTY because that would promise a handoff/continuity capability the direct PTY cannot provide.

A later review may instead choose an explicit `interaction_mode=delegated` field. That naming decision is intentionally listed as an open debate item in Section 61; all deeper semantics in this document are independent of the final wire spelling.

---

## 12. Delegated-session start semantics

Conceptual request if the `persistent+tty` spelling is retained:

```json
{
  "action": "start",
  "operation_id": "op-tunnel-shell",
  "command": "exec fish",
  "cwd": "/absolute/project/path",
  "tty": true,
  "persistent": true,
  "session_name": "tunnel-dev"
}
```

Rules:

- capability negotiation MUST prove delegated PTY support before the request is admitted;
- ordinary request fingerprint/idempotency semantics still apply;
- the reservation is durable before provider/session creation;
- retry of the same `operation_id` MUST resolve the same delegated session or exact failure; it never creates a second shell;
- changed delegated/direct mode under the same `operation_id` is a conflict;
- tmux server/session/pane details remain private provider metadata;
- public identity remains ShellBeam `session_id`; tmux IDs are provider facts, not public control authority;
- `session_name` retains the B1.0 human-friendly alias semantics and does not become authorization;
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

### 13.3 `handoff.cancel`

Cancels a pending handoff under a stable request identity. Cancellation removes ephemeral shell integrations and revokes human write ownership where possible. It does not kill the delegated session unless a separate normal session kill is requested.

### 13.4 `inspect.handoff`

A bounded read-only action returns current handoff metadata when event replay alone is inconvenient.

No action returns human keystrokes or secret content.

---

## 14. Handoff state machine

```text
AGENT_OWNED
    |
    | handoff.request accepted + durably bound
    v
HANDOFF_REQUESTED
    |
    | provider/session qualified
    | terminal selected
    | privacy barrier armed if required
    v
HUMAN_CONNECTING
    |
    | exact terminal client attached
    | designated client made writable
    v
HUMAN_OWNED
    |
    | completion predicate satisfied at safe boundary
    | OR manual-ready accepted
    v
RECLAIM_PENDING
    |
    | shell integration cleaned
    | human client toggled read-only
    | private output barrier ended at a fresh boundary
    v
AGENT_OWNED
```

Exceptional states:

```text
CANCELLED
EXPIRED
CLIENT_LOST
SESSION_LOST
PROVIDER_FAILED
RECLAIM_BLOCKED
```

A state transition is durable before a later external mutation relies on it. Handoff events are journaled as metadata-only state transitions.

---

## 15. Input ownership and arbitration

The central invariant is:

> At most one ShellBeam-recognized actor class has writable interactive input authority at a time.

### 15.1 Agent-owned

- agent `write` input is accepted under existing retry/input semantics extended for delegated sessions;
- designated human terminal clients are read-only/observer;
- ordinary terminal output remains visible to human observers;
- the model may inspect bounded session/handoff state.

### 15.2 Human-owned

- designated human client is writable;
- model-originated `write` to that session fails with a typed ownership failure;
- model-originated interactive key injection is not queued for later replay;
- normal agent `kill`/signal behavior SHALL NOT silently race human interaction; exact emergency/kill policy must be explicit in implementation planning;
- human input bytes are not copied into ShellBeam's agent input ledger.

### 15.3 Reclaim while the window remains open

On reclaim ShellBeam targets the exact tmux human client and enables the tmux read-only client flag. Read-only is an arbitration/convenience primitive inside the same trusted user boundary, not a sandbox.

If the provider cannot prove the exact client became read-only, ownership does not silently transition to `AGENT_OWNED`; it remains `RECLAIM_BLOCKED` or detaches that client under an explicitly documented fallback.

### 15.4 Multiple clients

Only the designated handoff client may be writable during `HUMAN_OWNED`. Other ShellBeam-known human clients remain read-only. Same-UID users with direct access to the private tmux socket remain inside ShellBeam's existing trusted-user threat model; ShellBeam does not claim protection against a local user deliberately bypassing arbitration.

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

A later handoff SHOULD reuse/reveal the existing attached terminal client when the terminal provider can do so reliably. Otherwise it may launch another exact client and keep all non-designated clients read-only.

---

## 17. Terminal resolution: no required preferred-terminal setting

A static `preferred_terminal=ghostty` is not the primary design because users may switch among several terminal emulators throughout the day.

The resolver SHALL use contextual evidence in this order:

1. **Existing handoff client for this delegated session**, if still alive and revealable.
2. **Exact request-origin terminal hint** from the local bridge/tunnel process context, when locally validated and supported.
3. **Currently active supported terminal application**, when the foreground application is itself a terminal.
4. **Most recently activated supported terminal application**, from one event-driven local activity registry.
5. **Single unambiguous running supported terminal**, when exactly one candidate exists.
6. **Qualified platform fallback launcher**, when the platform provides a deterministic safe default.
7. Otherwise return `terminal_launcher_unavailable` and expose the exact local attach command as degraded UX.

The resolver SHALL NOT simply ask "what app is frontmost?" because during a ChatGPT Web request the frontmost application is commonly the browser.

### 17.1 Request-origin hint

The ShellBeam MCP bridge/tunnel child often inherits terminal facts and process ancestry from the terminal in which the tunnel was started. A private compatibility extension MAY report bounded terminal-origin hints such as:

```text
terminal program family
terminal version?
controlling tty identity?
locally observed process/application identity?
```

These are UX-selection hints, never execution authorization.

A raw environment string such as `TERM_PROGRAM` is not sufficient by itself to authorize execution of an application path. The terminal provider resolves only known application identities/binaries through local platform discovery.

### 17.2 Recent-terminal activity

On platforms with a qualified native activation-event API, ShellBeam may keep a tiny in-memory registry of recent supported terminal activations. The event source is shared and event-driven; it is not one watcher per terminal or per session and performs no timer polling.

Persistent storage of a detailed app-usage history is unnecessary. At most the minimal current/recent terminal identity needed for UX selection should be retained, and it may remain memory-only.

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

## 24. Requirement predicates and readiness

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

### 24.1 Presence is not validity

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

`handoff.wait` waits on a daemon condition/event channel and consumes effectively zero CPU while idle.

---

## 26. Secret-bearing handoff and private output intervals

This is a hard privacy requirement.

### 26.1 Why ordinary capture is unsafe

If a human types:

```text
export CONTROL_PLANE_API_KEY=<secret>
```

into a normal echoed terminal line, the PTY may render the typed bytes. A control-mode observer could then receive those bytes as pane output even though the model never explicitly asked to read the environment.

Therefore merely "not calling env" is insufficient.

### 26.2 Secret handoff behavior

Before granting `HUMAN_OWNED` for `privacy=secret`, ShellBeam SHALL establish a private-output barrier for that pane/control client.

During the private interval:

- terminal/pane interaction remains visible locally to the human;
- model-visible `%output`/equivalent capture is disabled or discarded before persistence;
- human input bytes are not recorded in the agent input ledger;
- no raw private bytes are persisted in ordinary output logs, receipts, Event Journal, evidence, repro, or telemetry;
- terminal history remains local provider state and is never used as a recovery source across the private barrier.

### 26.3 Ending the private interval

The barrier ends only after:

1. a typed completion predicate or manual-ready signal succeeds;
2. a safe shell/process boundary is established for the selected completion strategy;
3. the human client is successfully made read-only or detached;
4. public output capture is resumed from a **new forward-only boundary**.

ShellBeam SHALL NOT run `capture-pane` or any replay operation that can reintroduce bytes from inside the private interval into model-visible output.

### 26.4 Crash/restart during a private interval

If the daemon/control observer restarts while a secret interval is active, the recovered session must preserve an explicit privacy barrier. It may resume observing only future output after re-establishing ownership/boundary state. It SHALL NOT reconstruct the missing interval from tmux history.

If safe recovery cannot be proven, the transcript remains incomplete with an explicit privacy/ambiguity reason; privacy wins over completeness.

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

The provider must qualify:

- control mode;
- stable IDs;
- client flags/read-only toggle;
- output suppression/private interval mechanism;
- flow control;
- required formats;
- supported `TERM`/terminfo behavior;
- session/pane lifecycle queries;
- daemon restart reattachment mechanics.

### 31.3 Lazy startup

No tmux server starts merely because ShellBeam daemon starts. The provider starts lazily on first delegated-session use or an explicit doctor/qualification action.

Ordinary command admission performs zero tmux work.

### 31.4 Control mode, not `capture-pane` polling

Control Mode output is the live transport. `capture-pane` may be used only for explicitly safe bounded resynchronization outside private intervals. It is never a periodic transport loop.

### 31.5 Human client flags

The designated human client is toggled writable/read-only by exact client identity. When read-only, it should also avoid perturbing active pane sizing where supported/appropriate.

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

- canonical ShellBeam durable binding identifies the delegated `session_id` and provider identity;
- provider-private metadata binds exact tmux server/session/pane identity plus a high-entropy ShellBeam session marker/token where needed;
- daemon restart reconnects only when the exact live provider session matches the canonical binding;
- process name/PID/TTY guesses never substitute for provider proof;
- if the provider session is absent, incompatible, or ambiguous, ShellBeam reports `SESSION_LOST`/ambiguous rather than signaling a guessed process;
- active private-output intervals remain privacy barriers across recovery;
- human-client writable/read-only state must be re-established conservatively before model input resumes.

Host reboot remains outside the continuity guarantee.

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
      "origin_hint": true,
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
    "privacy": {
      "secret_private_interval": true,
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

If existing `output_complete` cannot represent this without semantic ambiguity, the new capability needs an additive capture-quality field. It MUST NOT label a transcript byte-complete when bytes were intentionally suppressed for privacy.

Private omission is not execution failure. It is a transcript/evidence-quality fact.

Evidence consumers that require complete command output must reject or explicitly tolerate `private_intervals_omitted` rather than silently treating hidden output as absent.

---

## 41. Event Journal and durable metadata

Handoff events may include:

```text
handoff_id
session_id
reason
privacy class
state transition
terminal provider id/version
shell provider id/version/quality
completion kind
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

Friendly terminal application identity is acceptable operational metadata when needed for diagnostics, but app-usage history should remain minimal.

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
handoff_reclaim_blocked
private_output_barrier_failed
requirement_unsupported
requirement_not_satisfied
```

Requirements:

- provider absence fails before claiming delegated semantics;
- unknown launch outcome is not success;
- shell integration failure degrades to manual completion where safe, rather than executing wrong shell syntax;
- inability to establish a secret private-output barrier prevents secret handoff from becoming writable;
- inability to make the human client read-only prevents silent agent reclaim;
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

Unsupported terminal/shell combinations remain explicit capability states. ShellBeam shall prefer a manual attach/manual-ready fallback over guessing.

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

## 48. Human completion strategies

### 48.1 Automatic typed readiness

Used when a qualified shell adapter can prove a closed condition at a safe boundary, such as exported/non-empty environment presence.

### 48.2 Manual local ready

The human can signal completion through a ShellBeam-local mechanism when automatic shell readiness is unavailable.

Conceptual UX:

```text
shellbeam handoff ready
```

The attach environment can carry the opaque handoff identity so the user does not need to type it.

### 48.3 Chat-level "done"

The agent may also ask the human to say they are finished when no local completion primitive is reliable. This is the least automatic fallback and does not upgrade the capability claim.

### 48.4 Foreground child still running

If the human interaction happens inside a long-running foreground child and the shell prompt does not return, automatic shell-prompt completion may be impossible. A typed external capability signal or manual handback is required. ShellBeam MUST NOT guess completion merely because terminal input became idle.

---

## 49. Automatic terminal opening flow

Normative sequence:

```text
1. Agent requests handoff with stable handoff_id.
2. Daemon durably binds handoff to exact delegated session.
3. Daemon validates session ownership/provider capability.
4. For secret privacy, arm private-output barrier BEFORE writable human attach.
5. Resolve terminal context automatically.
6. Create/reveal one terminal client for this handoff.
7. Attach client to exact private tmux session/pane.
8. Identify exact tmux client.
9. Make only that client writable.
10. Publish HUMAN_OWNED.
11. Wait event-driven for completion.
12. Enter RECLAIM_PENDING.
13. Disable/remove shell watcher.
14. Make exact human client read-only.
15. End private-output barrier at safe forward boundary.
16. Publish AGENT_OWNED.
17. Agent verifies real capability and continues.
```

A failed step cannot be skipped merely to make UX appear seamless.

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

### 55.1 Pure state-machine tests

Cover:

- every ownership transition;
- duplicate/retry `handoff_id`;
- cancel/expiry;
- client lost during each state;
- provider lost;
- read-only reclaim failure;
- private barrier failure;
- shell watcher success/failure;
- manual fallback;
- no agent writes accepted while human-owned.

### 55.2 tmux protocol/provider tests

Use parser/protocol fixtures for deterministic unit coverage and native tmux tests for actual behavior:

- create/destroy private server/session;
- stable IDs;
- Control Mode command correlation;
- output and flow control;
- `no-output`/private barrier behavior;
- writable/read-only client toggle;
- multiple clients;
- terminal resize arbitration;
- pane/session/client death;
- daemon/control connection reconnect;
- no `capture-pane` replay across privacy barrier.

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
- safe boundary required;
- nested shell drift -> degrade;
- no persistent dotfile mutation;
- no secret value in hook argv/output/state;
- notifier/control helper environment is minimally allowlisted and excludes the watched secret even after it is exported.

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

The test should deliberately type a visible `export KEY=<canary>` during a private interval to prove output suppression rather than relying only on hidden-input UX.

### 55.5 Terminal resolver tests

Cover:

- exact request-origin terminal beats recent-terminal fallback;
- browser frontmost does not erase recent terminal context;
- multiple installed/running terminals;
- no stored preference required;
- unsupported terminal degrades;
- launcher retry does not duplicate proven client;
- unknown launch outcome remains unknown;
- exact app identity, not arbitrary env path execution.

### 55.6 Native GUI smoke tests

Each promoted terminal launcher requires native evidence on its target OS. CI may contract-test command generation without opening a GUI, but release qualification must include real launch/attach/reveal behavior for the advertised provider.

### 55.7 Resource/leak stress

At least 100 repeated handoff cycles under native tmux should verify bounded:

```text
CPU while idle
RSS trend
FD count
goroutine count
tmux clients/panes/sessions
helper process count
runtime files/sockets
shell hook residue
```

No test may substitute "sleep and assume" for inspecting the relevant resources.

### 55.8 Restart/fault matrix

Fault at every important boundary:

```text
after durable handoff bind before GUI launch
after GUI launch before client proof
after human writable before requirement watcher
inside private output interval
after readiness before read-only toggle
after read-only toggle before public capture resume
after reclaim before response
```

Each fault must have a deterministic privacy-safe recovery state.

---

## 56. Dependency/provider qualification gate

Before implementation can claim the tmux path production-capable, an A0-style qualification must record:

```text
exact tmux executable/version identity
Control Mode protocol features used
minimum/maximum tested versions
Darwin evidence
Linux evidence
private socket/config behavior
read-only client semantics
no-output/private interval semantics
flow control
terminfo/default-terminal behavior
restart/reconnect behavior
resource/leak behavior
```

If a Go wrapper is used, record exact module version/SHA, license, maintenance review, and removal plan.

No dependency is added merely because it makes the first prototype shorter.

---

## 57. Rollout strategy

Recommended staged rollout:

### H0 — provider qualification only

- prove tmux Control Mode mechanics;
- prove private server/config/socket;
- prove read-only client reclaim;
- prove no-output private interval;
- no public MCP capability yet.

### H1 — delegated persistent TTY core

- create/inspect/control delegated interactive sessions;
- no automatic human handoff yet;
- capability-gated and experimental.

### H2 — human ownership + manual attach

- exact handoff state machine;
- local `shellbeam session attach`;
- read-only reclaim;
- manual-ready fallback.

### H3 — automatic terminal resolver/launcher

- request-origin terminal hint;
- recent terminal event provider;
- promoted launcher adapters.

### H4 — shell-aware readiness + secret privacy

- fish/zsh/bash adapters;
- private output intervals;
- secret-canary acceptance;
- automatic safe reclaim without closing terminal.

### H5 — broader shells/terminals and context-exec evidence, only if justified

- Nushell/other shell adapters;
- additional terminal providers;
- stronger receipt-producing command semantics if needed.

Each stage remains removable/disableable through capability intersection.

---

## 58. Compatibility and one-tool boundary

No second MCP tool is introduced.

Older clients/bridges:

- continue using ordinary start/poll/write/kill;
- do not receive delegated interactive capability;
- cannot accidentally send `persistent=true + tty=true` unless they already do, in which case existing rejection remains safe.

New bridge + old daemon:

- negotiation reports delegated handoff unavailable;
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

1. No required static preferred-terminal setting.
2. Terminal, shell, and session provider identities are orthogonal.
3. Unknown shell never receives guessed shell syntax.
4. No permanent dotfile mutation.
5. No shell polling loop.
6. No `capture-pane` polling transport.
7. Secret handoff cannot become writable before private output suppression is established.
8. Secret values/hashes never enter ordinary model-visible state.
9. ShellBeam notifier/control helpers never inherit a secret-bearing delegated shell environment wholesale.
10. Human and agent are not writable concurrently under ShellBeam-controlled UX.
11. Human terminal may remain open after reclaim only when exact client write-disable is proven.
12. Read-only client control is an arbitration primitive, not a security claim.
13. Private intervals are never replayed into model output after reconnect.
14. Presence of a credential is not proof of validity; capability verification remains separate.
15. Ordinary direct commands pay zero delegated-session provider work.
16. Missing tmux/terminal/shell capability is explicit, not guessed or silently downgraded.
17. `persistent=true + tty=true` never falls back to direct PTY when delegated semantics were requested.
18. Source checkout paths and developer-machine package layouts never enter product contracts.
19. Arbitrary existing external PTY adoption is outside V1.
20. Interactive shell transcript is not silently upgraded to ordinary receipt/evidence authority.
21. Host reboot continuity is not claimed.

---

## 61. Open debate questions

These are deliberately left for review rather than hidden as implementation choices.

### Q1. Wire spelling for delegated sessions

Should the new mode use the natural currently-rejected combination:

```text
persistent=true + tty=true
```

or add an explicit field such as:

```text
interaction_mode=delegated
```

Recommendation: prefer `persistent+tty` if capability negotiation and receipt schemas remain unambiguous; choose an explicit field only if future interactive providers need semantics not captured by those axes.

### Q2. Is tmux an acceptable external product dependency?

Recommendation: yes as an optional qualified provider, not an ordinary-command dependency. Do not silently install it.

### Q3. Go Control Mode library

Should ShellBeam adopt `gotmuxcc` after qualification or own a small protocol adapter?

Recommendation: qualify the library first; the architecture depends on the protocol, not the wrapper.

### Q4. Public output semantics

Should intentional private intervals set existing `output_complete=false`, or should a new additive capture-quality field distinguish intentional privacy from transport loss while preserving legacy meaning?

Recommendation: add explicit capture quality rather than overload a boolean ambiguously; preserve legacy direct-session semantics exactly.

### Q5. Initial terminal support gate

How many GUI terminal launchers must be qualified before the feature is considered generally useful rather than experimental?

Recommendation: Ghostty + one other macOS terminal and at least one Linux terminal for first experimental release; broaden before stable claim.

### Q6. Initial shell matrix

Recommendation: fish/zsh/bash are the stable initial adapters; Nushell is separate/version-qualified; unknown shells retain manual fallback.

### Q7. Receipt-producing commands inside delegated context

Is advisory shell command boundary + exit status enough for the first handoff release, or must a context-exec child path preserve ordinary receipt semantics before agent continuation is considered complete?

Recommendation: do not block the human-handoff UX on a full context-exec subsystem, but prohibit using advisory interactive output as mechanical verification evidence. Add context-exec only under a separately reviewed evidence contract.

### Q8. Human-owned kill semantics

Should a model-requested `kill` be rejected while human-owned, or should there be a narrow emergency termination action?

Recommendation: reject ordinary model mutations during human ownership in the first version; the human/local CLI can terminate explicitly. Add emergency semantics only with an explicit policy design.

### Q9. Linux recent-terminal detection

Wayland/X11/desktop variability may make "most recently active terminal" less uniformly observable than macOS.

Recommendation: make request-origin terminal identity the primary cross-platform mechanism; recent-activity detection is a qualified optional provider and manual/single-terminal fallback remains explicit.

---

## 62. Definition of done for the design/implementation program

The feature is not complete merely because an agent can type into tmux.

A stable release claim requires all of the following:

1. delegated session provider is version/capability-qualified on native macOS and Linux;
2. ordinary direct ShellBeam start path shows no tmux/handoff work when unused;
3. public schema/IPC/capability negotiation rejects unsupported delegated TTY before spawn;
4. start retry cannot duplicate delegated shells;
5. automatic terminal resolution works without a required preferred-terminal setting for the advertised providers;
6. terminal launcher retry cannot create duplicate windows after a proven attach;
7. fish/zsh/bash adapters preserve existing hooks and leave no permanent dotfile changes;
8. unsupported/nested shells degrade safely;
9. human writable ownership and agent writable ownership never overlap under normal ShellBeam control;
10. human terminal can remain open and is proven read-only after reclaim;
11. yes/no/Enter/raw interactive input works while agent-owned;
12. secret handoff establishes private output before human write access;
13. secret canary never appears in model-visible/durable ordinary ShellBeam state;
14. automatic exported/non-empty readiness works without closing the terminal for qualified shells;
15. readiness performs no polling and leaves no resident helper process;
16. capability validity is rechecked by the agent separately from secret presence where appropriate;
17. private output intervals remain private across daemon reconnect/recovery;
18. output/evidence quality honestly records intentional private omission;
19. tmux server/client/session/provider loss fails closed without PID/name-based takeover;
20. closing a GUI terminal client does not kill the delegated session by definition;
21. host reboot remains explicitly unsupported;
22. stress tests show no meaningful resource creep after repeated handoffs;
23. installed-product tests pass from a location with no source checkout and non-Homebrew paths;
24. no production path contains developer-machine absolute paths;
25. `doctor` exposes actionable capability/provider state without secrets;
26. no second MCP tool or ChatGPT App dependency is introduced;
27. interactive transcript facts are not mislabeled as ordinary verification evidence;
28. external arbitrary-PTY adoption remains visibly outside the stable V1 contract.

---

## 63. Final design position

The product abstraction is not "Ghostty integration" and not "API-key export support".

It is:

> **Delegated Interactive Sessions with Human–Agent Handoff** — a local execution context that ShellBeam owns from creation, can present in the terminal the user is actually using, can temporarily delegate to a human for sensitive/interactive work, and can safely reclaim for the agent without reconstructing shell state or transferring credentials through the model.

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
