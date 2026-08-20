# Delegated Context Execution Evidence Design

**Status:** Task-1 design candidate approved in chat for authoring; implementation remains blocked pending an independently supplied human approval artifact bound to the exact committed spec digest.

```text
PUBLIC_ACTION=context.exec
REQUEST_IDENTITY=context_exec_id
CONTEXT_EXEC_AVAILABLE=false
IMPLEMENTATION_ALLOWED=false
```

## 1. Purpose and authority boundary

Delegated interactive sessions preserve shell-local context, but their pane transcript is mixed interactive state and remains `session_lifecycle_only` evidence. Context execution exists to run one child from the current delegated shell context while preserving a stronger causal boundary for child identity, stdout/stderr, and exit evidence.

This design does **not** upgrade delegated pane output into mechanical verification. A context-exec result can become mechanical evidence only when the authenticated helper owns the child spawn/reap and separately owns the child stdout/stderr channels. Shell transcript markers, prompt scraping, or inherited pane output are not sufficient.

V1 is intentionally narrow:

```text
one public action inside local_shell: context.exec
one delegated target: session_id + authority_epoch
argv-only execution request
current exported process environment + current cwd are the inherited context authority
non-TTY child
closed stdin
separate stdout/stderr pipes
one short-lived helper per context_exec reservation
no resident helper
no pane transcript as evidence source
```

## 2. Alternatives rejected

### 2.1 Transcript wrapper / prompt markers — rejected

Executing inside the delegated pane and attributing bytes between shell pre/post hooks is not strong evidence. Prompt output, shell jobs, asynchronous hooks, human activity, and other pane writers can interleave. Capture completeness cannot prove causal attribution.

### 2.2 Short-lived authenticated helper + helper-owned child pipes — selected

The daemon durably reserves the operation before helper launch. At a proved safe shell boundary, ShellBeam requests a fixed private helper invocation from the exact delegated shell. The helper inherits the shell's exported environment and cwd, then launches a separately owned child whose stdout/stderr and lifecycle are controlled by the helper.

### 2.3 Resident context agent — rejected for V1

A long-lived in-shell agent lengthens credential/control-capability lifetime, complicates authority-epoch semantics, introduces resident-resource creep, and creates unnecessary replay/recovery surface. V1 therefore uses a short-lived helper per reserved operation.

## 3. Public request contract

The public action is:

```text
context.exec
```

The public correlation identity is:

```text
context_exec_id
```

Candidate V1 request shape:

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

Rules:

- `context_exec_id` is a public idempotency/correlation identity, not a bearer secret and not helper authentication authority.
- `session_id + authority_epoch` identifies the exact delegated execution authority targeted by the request.
- `argv` is required and non-empty; `argv[0]` is the requested executable name/path.
- V1 has no shell `command` string, cwd override, environment-value override, stdin payload, PTY request, or TTY mode.
- Argument count/bytes, timeout, and output bytes are bounded by closed implementation limits.
- Same `context_exec_id` with an identical full request replays. Any changed field conflicts.

## 4. Admission and ownership preconditions

`context.exec` is admissible only when all of the following are current and proven:

```text
delegated session lifecycle = live
provider generation = current
provider owner = agent
desired owner = agent
agent ingress = writable
authority_epoch = exact request epoch
shell identity = current, qualified, and unchanged
safe boundary = current and qualified
privacy capture = public
privacy release = not pending
no active context-exec child lease on the same delegated authority epoch
```

A private interval or pending privacy release returns `context_exec_privacy_blocked`. Unknown, nested, replaced, or otherwise unqualified shell state returns `context_exec_boundary_unproven` or `context_exec_stale_generation`; ShellBeam never guesses Bash syntax or silently degrades to transcript evidence.

## 5. Context authority and provenance

The helper must be launched by the **current delegated shell** at a qualified safe boundary. The context inherited by the helper is therefore exactly the shell process's:

```text
exported environment
current working directory
```

V1 does **not** claim authority over or preservation of:

```text
unexported variables
shell functions
aliases
shell history
interactive shell parser state
job-table state
```

No design or receipt may say that environment values were "captured" or "verified". The daemon does not read or persist the inherited exported values.

Candidate non-secret context provenance:

```go
type ContextBinding struct {
    SessionID       string
    AuthorityEpoch  delegatedsession.AuthorityEpoch
    ShellIdentity   string
    BoundaryQuality string
    CWDObserved     string
    PrivacyState    string
}
```

`CWDObserved` is local/private context metadata. Any public projection must follow existing workspace/path redaction and normalization rules. Environment values, hashes, deterministic derived values, lengths, or fingerprints are forbidden.

## 6. Safe-boundary producer requirements

A safe boundary must be produced by the qualified current-shell adapter for fish, zsh, or bash and must bind:

```text
handoff/delegated session identity
provider generation
shell runtime identity
boundary quality
observation epoch/time
```

The daemon must revalidate the exact provider generation and shell identity immediately before helper-launch delivery. If cwd or shell identity changes between reservation/boundary proof and helper authentication, the claim fails and no child spawns.

The helper-launch hook is ephemeral, composable with existing shell hooks, modifies no dotfiles, leaves no resident helper, and follows the same no-guess shell policy already qualified by H4.

## 7. Reserve-before-launch and exactly-once semantics

Durable reservation occurs **before** any helper-launch bytes are delivered to the delegated shell.

Lifecycle candidate:

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

Exactly-once rules:

1. `ReserveContextExec` binds the complete request identity before provider mutation.
2. A reservation may bind at most one approved helper generation.
3. Same request replay returns the durable current/result state and does not launch a second child.
4. A changed request under the same `context_exec_id` is a conflict.
5. If helper-launch delivery was ambiguously accepted, retry does not blindly send a second launch.
6. If the system cannot prove that the first helper never started, the operation becomes `ambiguous` rather than risking duplicate child execution.
7. A second helper generation cannot claim an operation already bound to a first generation unless the durable state proves the first never authenticated/spawned and the approved recovery transition explicitly permits rebinding.

## 8. Public identity versus helper claim authority

`context_exec_id` and the pane-visible opaque launch id are not bearer authority.

The shell receives only a fixed installed helper invocation plus an opaque non-secret launch reference, conceptually:

```text
shellbeam __context_exec_helper <opaque_launch_id>
```

The helper connects to a daemon-owned private local control channel. Authentication has two layers:

1. the daemon proves the connecting peer belongs to the exact delegated shell/provider generation and is the expected installed ShellBeam helper executable;
2. only then the daemon issues a high-entropy, one-generation claim capability over the private channel, and the helper proves possession by challenge/response.

The claim capability:

```text
must not appear in argv
must not appear in process environment
must not appear in pane transcript
must not appear in public state/errors/logs
must not be persisted in reusable bearer form
must not be inherited by the child
```

Durable `HelperBinding` stores only approved non-secret identity/generation/audit material. A same-user process that knows `context_exec_id` or `<opaque_launch_id>` cannot claim the reservation.

## 9. Internal control isolation before child exec

The helper inherits the delegated shell exported environment because that is the feature's context authority. Before spawning the child it strips only a **closed ShellBeam-internal control namespace** and closes all helper-only/control file descriptors.

This is not secret scrubbing of user context. ShellBeam must not remove arbitrary user variables such as tokens, SSH agent paths, virtual-environment variables, or application configuration.

The workload may intentionally reveal its own inherited environment, for example by running `printenv` or printing a credential. That is workload output and **not** a claim failure of ShellBeam's control-plane privacy. The design makes no claim that arbitrary workloads cannot reveal their own environment.

## 10. Actual executable identity and TOCTOU resistance

The actual executable is resolved **inside the helper under the inherited delegated `PATH`**, not under daemon PATH and not from a guessed login shell environment.

The result distinguishes:

```text
requested executable identity
resolved/mechanically observed executable identity
```

For high-assurance V1, path lookup must bind execution to the same executable object whose identity was observed. A vulnerable `resolve path -> inspect path -> later execve(path)` sequence is not sufficient because the path may change between observation and exec.

The approved implementation must use descriptor-bound execution (`fexecve`/`execveat` or a separately qualified platform-equivalent mechanism) after opening and validating the executable object. If the advertised platform cannot prove this binding, it must not advertise strong context-exec evidence authority.

`argv[0]` not found returns a stable spawn failure without falling back to shell evaluation.

## 11. Child process and job-control ownership

V1 child semantics are deliberately non-interactive:

```text
TTY = false
stdin = closed
separate process group
stdout = helper-owned pipe
stderr = helper-owned pipe
timeout/signal/reap = helper-owned
```

The helper owns the child process group from spawn through terminal reap. Shell jobs, prompts, hooks, human terminal input, and background pane writers cannot write into the child stdout/stderr evidence pipes.

A future optional terminal mirror may copy child output to the delegated terminal **only as presentation**. It is never an evidence source and cannot affect canonical child stdout/stderr.

## 12. Output attribution, bounds, and canonicalization

Canonical context-exec output comes exclusively from helper-owned child stdout/stderr channels.

Requirements:

- stdout and stderr are separately framed and attributed;
- ordering metadata is explicit if a combined presentation is offered;
- byte counts are monotonic and bounded;
- truncation/output-limit semantics are explicit and cannot be reported as complete capture;
- helper/control protocol bytes never enter child output;
- delegated pane bytes never enter child canonical output;
- private session-history replay/capture-pane is never used.

Output evidence may be authoritative only if the result's output completeness contract is satisfied for the requested verification use.

## 13. Privacy and human handoff concurrency

Context execution may start only while capture is public and no privacy release is pending.

An active context-exec child holds an execution lease on its exact `session_id + authority_epoch`. Conservative V1 transfer policy:

```text
human handoff / ownership epoch transition cannot complete while a context child is active
```

The transition may complete only after the context child is:

```text
proved terminal, or
explicitly cancelled and reaped under its original authority
```

No ownership epoch silently reclassifies an ambiguous in-flight child. Because ownership/private transition cannot cross an active context child, model-visible child stdout/stderr cannot accidentally continue into a secret interval.

## 14. Daemon/helper crash matrix

### 14.1 Reserve, response lost

Replay of the exact public request resolves the durable reservation; no duplicate helper is launched.

### 14.2 Reserve, helper-launch signal lost

Retry may relaunch only when durable/provider evidence proves the first helper launch was not accepted. Otherwise state becomes/stays ambiguous.

### 14.3 Helper starts, auth response lost

The same bound helper generation may retry authentication idempotently. A second generation cannot steal the reservation.

### 14.4 Helper authenticated, child spawn unknown

If the daemon/helper cannot prove whether spawn occurred, the operation is `ambiguous`; it must not spawn a second child merely to recover.

### 14.5 Child terminal, canonical acknowledgement lost

The same helper generation may replay the bounded terminal report/result idempotently.

### 14.6 Daemon disconnect while child active

The helper must not let an unobserved child continue indefinitely. Conservative V1: it terminates the exact child process group and reaps it if the daemon control channel cannot be re-established within the approved short recovery bound. Any bounded in-memory terminal/result replay is generation-bound and is not a durable secret spool.

### 14.7 Helper dies after child spawn

Where exact child/process-group identity remains proved, the daemon may attempt safe termination. Evidence becomes `helper_lost`/`ambiguous` as applicable; no exit status is invented.

## 15. Failure vocabulary

At minimum the implementation contract contains stable failures for:

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

Errors/logs are bounded and must not include raw environment, helper claim capabilities, pane transcript, private output, or secret-derived material.

## 16. Evidence authority model

Delegated session receipts remain:

```text
evidence_authority = session_lifecycle_only
```

regardless of capture quality.

A separately authenticated context-exec result may use the new explicit authority class:

```text
context_exec_child_owned_v1
```

This class is stronger than a delegated shell transcript because ShellBeam has mechanically attributed:

```text
exact reserved request identity
authenticated helper generation
exact child spawn under that helper
requested versus actual executable identity
helper-owned stdout/stderr pipes
exact child terminal reap/exit evidence
```

It is still not automatically equivalent to every property of an ordinary daemon-spawned direct command. In particular, inherited delegated environment/cwd are context provenance rather than hermetic inputs, and context-exec does not by itself prove resource enforcement, artifact identity, filesystem isolation, network isolation, or deterministic environment values.

Existing mechanical evidence consumers may accept `context_exec_child_owned_v1` only for facts that their contract actually requires and only when all mandatory context-exec validations pass. Artifact verification or stronger evidence classes still require their existing independent contracts/proofs.

No evidence consumer may derive authority from pane transcript markers, shell prompt text, or delegated session capture completeness.

## 17. Security and privacy non-claims

The design forbids ShellBeam from persisting or projecting:

```text
raw inherited environment
secret values
secret hashes / deterministic secret derivatives
secret lengths
helper bearer capability
private pane history
human keystrokes
```

It also explicitly does **not** claim:

```text
that the child cannot print its own inherited secrets
that inherited context is hermetic
that exported environment values were captured/verified
that shell functions/aliases are reproduced
that context-exec is safe while private capture is active
that transcript evidence becomes mechanical evidence
that context_exec_id authenticates a helper
```

## 18. Resource and non-inheritance requirements

The child receives the user's inherited delegated context minus the closed ShellBeam internal-control namespace and helper/control file descriptors.

The helper must not leak:

```text
daemon control socket descriptor
helper auth capability/challenge state
internal generation/control variables
observer/control tmux descriptors
unrelated inherited ShellBeam service descriptors
```

V1 context-exec evidence authority does not imply resource enforcement or hermeticity. If a caller requires those properties, capability/admission must require the separately proven resource/hermetic contract rather than inferring it from context-exec.

## 19. Counterexample attack matrix

The design must survive these attacks; failure is fail-closed and never duplicate-by-default:

| Counterexample | Required behavior |
|---|---|
| stale epoch helper starts after new handoff | reject helper/child; stale generation |
| same context_exec_id launches twice after lost response | durable replay; at most one child generation |
| cwd changes between reservation and helper launch | claim/boundary mismatch; no child |
| secret env exists and daemon/helper logging path handles errors | no env value/hash/length in log/error |
| private capture still active | reject `context_exec_privacy_blocked` |
| helper authenticates but child never spawns | explicit spawn failure; no fake exit |
| child exits while daemon disconnected | bounded same-generation terminal recovery or conservative termination truth; no invented result |
| helper dies after child spawn | exact process-group cleanup if proved; `helper_lost`/`ambiguous` |
| agent requests human transfer while child active | transfer cannot complete until terminal/cancel+reap |
| nested shell changes after request | shell/runtime generation mismatch; no guessed syntax |
| argv executable not found | stable spawn failure; no shell fallback |
| background shell/job/prompt writes to pane | cannot enter helper-owned stdout/stderr evidence |
| context_exec_id is visible and second same-user process claims it | claim fails; public id has no bearer authority |
| helper token/control FD would reach child | stripped/closed before exec; validation failure if not provable |
| PATH resolves another executable than daemon expected | helper's inherited PATH is authoritative; requested and actual identities are distinct |
| executable path changes between identity capture and exec | descriptor-bound execution prevents path TOCTOU |

## 20. Approval and freeze gate

This document authorizes **no implementation by itself**.

Tasks 2+ are blocked until a human reviewer supplies the separate tracked artifact:

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

The implementing agent must not create or edit approval fields. The artifact must bind the exact committed spec bytes. A missing, false, malformed, or digest-mismatched artifact blocks Tasks 2+.

Until that gate passes:

```text
H5_DESIGN_ALLOWED=true
CONTEXT_EXEC_AVAILABLE=false
CONTEXT_EXEC_IMPLEMENTATION_ALLOWED=false
```
